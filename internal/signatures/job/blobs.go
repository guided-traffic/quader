package job

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// BlobSource is the stored blobs as the verifiers read them (scheme.Blobs) over the item store's blob store (R-SIG-006, R-SIG-013): a
// blob is looked up by its row, read whole or in ranges from the object store, and never written to disk. A digest without a row, or
// whose object is gone, is scheme.ErrBlobMissing.
func BlobSource(s *blob.Store) scheme.Blobs { return blobSource{s: s} }

type blobSource struct{ s *blob.Store }

func (b blobSource) Content(ctx context.Context, digest [32]byte) (scheme.Content, error) {
	if b.s == nil {
		return nil, errors.New("signatures: no blob store")
	}
	row, err := b.s.Lookup(ctx, blob.Digest(digest))
	switch {
	case errors.Is(err, blob.ErrBlobNotFound):
		return nil, scheme.ErrBlobMissing
	case err != nil:
		return nil, err
	case row.State != "live" || row.ContentStatus == "missing":
		return nil, scheme.ErrBlobMissing
	}
	return storedBlob{ctx: ctx, s: b.s, d: blob.Digest(digest), size: row.Size}, nil
}

// storedBlob is one stored blob.
type storedBlob struct {
	ctx  context.Context
	s    *blob.Store
	d    blob.Digest
	size int64
}

func (c storedBlob) Size() int64 { return c.size }

func (c storedBlob) open(r *blob.ByteRange) (io.ReadCloser, error) {
	rc, err := c.s.Open(c.ctx, c.d, c.size, blob.OpenOptions{Range: r})
	if errors.Is(err, blob.ErrNotFound) {
		return nil, scheme.ErrBlobMissing
	}
	return rc, err
}

func (c storedBlob) Bytes(max int64) ([]byte, error) {
	if c.size > max {
		return nil, fmt.Errorf("signatures: the blob is %d bytes, more than %d", c.size, max)
	}
	rc, err := c.open(nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(io.LimitReader(rc, max+1))
}

func (c storedBlob) Reader() (io.ReadCloser, error) { return c.open(nil) }

func (c storedBlob) ReaderAt() (io.ReaderAt, error) { return c, nil }

// ReadAt reads a part of the blob with a ranged read of the object store.
func (c storedBlob) ReadAt(p []byte, off int64) (int, error) {
	if off >= c.size {
		return 0, io.EOF
	}
	n := min(int64(len(p)), c.size-off)
	rc, err := c.open(&blob.ByteRange{Offset: off, Length: n})
	if err != nil {
		return 0, err
	}
	defer func() { _ = rc.Close() }()
	var buf bytes.Buffer
	got, err := io.CopyN(&buf, rc, n)
	copy(p, buf.Bytes())
	if err == nil && got < int64(len(p)) {
		err = io.EOF
	}
	return int(got), err
}
