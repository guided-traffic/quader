package backup

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/backup/target"
	"github.com/guided-traffic/quader/internal/blob"
)

// SourceConnection is one connection of the backup's configuration as the backup reads it (R-BAK-030).
type SourceConnection struct {
	// ID is the connection's id, Drain its drain flag.
	ID    string
	Drain bool
	// Objects is the connection's client with the backup's read credential (backup.source.connections or the connection's
	// own), usually behind its guard, so an unreachable connection opens its circuit and is skipped fast.
	Objects blob.ObjectStore
	// Endpoint and Bucket name the connection's bucket for a server-side copy into the target.
	Endpoint, Bucket string
	// ServerSide is true when the connection's endpoint is the target's (R-BAK-006).
	ServerSide bool
}

// CopyRow is a present copy of blob_copies as the copy phase reads it.
type CopyRow struct {
	Digest     format.Digest
	Connection string
	Key        string
	StoredSize int64
	Verified   bool
}

// CopyReader reads the present copies of digests at READ COMMITTED. [PGFacts] implements it.
type CopyReader interface {
	PresentCopies(ctx context.Context, digests []format.Digest) ([]CopyRow, error)
}

// StoreSource is the [BlobSource] over the copies of the connections (R-BAK-006, R-BAK-030). It only reads: it writes no copy
// row and marks no copy, because the backup role writes backup_run and backup_blob_pin only (R-BAK-015). The clients are the
// ones internal/blob/s3 builds; this adapter lives here because the infrastructure layer may not import this package.
type StoreSource struct {
	rows  CopyReader
	conns map[string]sourceConn
}

type sourceConn struct {
	SourceConnection
	position int
}

var _ BlobSource = (*StoreSource)(nil)

// NewStoreSource reads the copies rows finds on the connections conns, in the order of the configuration.
func NewStoreSource(rows CopyReader, conns []SourceConnection) *StoreSource {
	s := &StoreSource{rows: rows, conns: make(map[string]sourceConn, len(conns))}
	for i, c := range conns {
		s.conns[c.ID] = sourceConn{SourceConnection: c, position: i + 1}
	}
	return s
}

// Copies implements BlobSource: the present copies on the connections of this configuration; a copy on a connection the
// backup does not configure (a detached one) is left out.
func (s *StoreSource) Copies(ctx context.Context, digests []format.Digest) (map[format.Digest][]SourceCopy, error) {
	rows, err := s.rows.PresentCopies(ctx, digests)
	if err != nil {
		return nil, err
	}
	out := make(map[format.Digest][]SourceCopy, len(digests))
	for _, r := range rows {
		c, ok := s.conns[r.Connection]
		if !ok {
			continue
		}
		out[r.Digest] = append(out[r.Digest], SourceCopy{
			Connection: r.Connection, Key: r.Key, StoredSize: r.StoredSize, Verified: r.Verified,
			ServerSide: c.ServerSide, Position: c.position, Drain: c.Drain,
		})
	}
	return out, nil
}

func (s *StoreSource) conn(c SourceCopy) (sourceConn, error) {
	sc, ok := s.conns[c.Connection]
	if !ok {
		return sourceConn{}, fmt.Errorf("the connection %s of a copy is not configured", c.Connection)
	}
	return sc, nil
}

// sourceErr gives a store error of a copy the meaning the copy phase reads: a missing object, or a connection that did not
// answer; anything else (an AccessDenied of a wrong read login) passes as it is and fails the run.
func sourceErr(err error) error {
	switch {
	case errors.Is(err, blob.ErrNotFound):
		return ErrBlobNotFound
	case errors.Is(err, blob.ErrCircuitOpen), Temporary(err):
		return fmt.Errorf("%w: %w", ErrCopyUnavailable, err)
	}
	return err
}

// Size implements BlobSource: HeadObject of the copy's object.
func (s *StoreSource) Size(ctx context.Context, c SourceCopy) (int64, error) {
	sc, err := s.conn(c)
	if err != nil {
		return 0, err
	}
	info, err := sc.Objects.Head(ctx, c.Key)
	if err != nil {
		return 0, sourceErr(err)
	}
	if info.Size < 0 {
		return 0, fmt.Errorf("%w: connection %s did not report the size of %s", ErrCopyUnavailable, c.Connection, c.Key)
	}
	return info.Size, nil
}

// Open implements BlobSource: a ranged GetObject; a negative length reads to the end. A read of the body that fails is a
// failure of the connection.
func (s *StoreSource) Open(ctx context.Context, c SourceCopy, offset, length int64) (io.ReadCloser, error) {
	sc, err := s.conn(c)
	if err != nil {
		return nil, err
	}
	rc, _, err := sc.Objects.Get(ctx, c.Key, &blob.ByteRange{Offset: offset, Length: length})
	if err != nil {
		return nil, sourceErr(err)
	}
	return sourceBody{rc}, nil
}

// sourceBody marks a failed read of a copy's body as a failure of its connection.
type sourceBody struct{ io.ReadCloser }

func (b sourceBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = fmt.Errorf("%w: %w", ErrCopyUnavailable, err)
	}
	return n, err
}

// Ref implements BlobSource.
func (s *StoreSource) Ref(c SourceCopy) target.ObjectRef {
	sc := s.conns[c.Connection]
	return target.ObjectRef{Endpoint: sc.Endpoint, Bucket: sc.Bucket, Key: c.Key, Size: c.StoredSize}
}
