package formathost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/signatures"
	sigstore "github.com/guided-traffic/quader/internal/signatures/store"
)

// DiscoveryItems lists the live files discovery asks the formats about; the signature store implements it.
type DiscoveryItems interface {
	DiscoveryItems(ctx context.Context, after int64, limit int) ([]sigstore.DiscoveryItem, error)
}

// Discoverer is the discovery of the job signature-verify (signatures.md R-SIG-011): it asks the format of each live file after the
// cursor what the file carries or points at, exactly as the commit hook does, and records it at the priority of discovery. It finds what
// was stored before stage 1, what a restore loaded, and a signature row removed since. A detector's error never stops it (Q-SIG-47); a
// store error does, and the next run starts at the same cursor.
type Discoverer struct {
	Deps  Deps
	Items DiscoveryItems
	DB    meta.Runner
	Blobs *blob.Store
}

// Discover implements the job's port: it looks at at most limit files after the item after and answers the last item it looked at.
func (x *Discoverer) Discover(ctx context.Context, after int64, limit int) (int64, error) {
	if x.Items == nil || x.DB == nil || x.Deps.Signatures == nil {
		return after, nil
	}
	items, err := x.Items.DiscoveryItems(ctx, after, limit)
	if err != nil {
		return after, err
	}
	last := after
	for _, it := range items {
		if f, ok := x.Deps.lookup(it.Format); ok && declares(f) {
			ev := discoveryEvent(it, storedContent{ctx: ctx, blobs: x.Blobs, digest: blob.Digest(it.Blob), size: it.Size})
			if err := x.DB.InTx(ctx, func(tx meta.Tx) error {
				return x.Deps.recordAt(ctx, tx, f, ev, false, signatures.PriorityDiscovery)
			}); err != nil {
				return last, fmt.Errorf("item %d: %w", it.ID, err)
			}
		}
		last = it.ID
	}
	return last, nil
}

// declares reports whether a format declares signatures: a format whose ecosystem has none (formats.NoSignatures, the ClamAV
// definitions, Q-SIG-96) detects nothing, so discovery opens no transaction and reads no content of its files.
func declares(f formats.Format) bool {
	sa := f.Signatures()
	return sa != nil && sa.Declaration().None == ""
}

// discoveryEvent is the event of a stored file as its commit would have handed it to the detector.
func discoveryEvent(it sigstore.DiscoveryItem, content formats.SignatureContent) formats.SignatureEvent {
	repo := formats.RepoRef{
		ID: it.RepoID, Name: qname.Repo{Project: it.ProjectKey, Key: it.RepoKey}, Class: formats.Class(it.Class), Format: formats.ID(it.Format),
	}
	origin := formats.OriginUpload
	if it.UpstreamURL != "" || it.Class == "remote" {
		origin = formats.OriginFill
	}
	return formats.SignatureEvent{
		Origin: origin, Repo: repo, Item: formats.ItemRef{ID: it.ID, Repo: repo, Path: it.Path}, Path: formats.Path{Path: it.Path},
		MediaType: it.MediaType, Digests: formats.Digests{SHA256: append([]byte(nil), it.Blob[:]...)}, Content: content,
	}
}

// storedContent is a stored file's bytes as a detector reads them, from the object store: whole when small enough, or in ranges.
type storedContent struct {
	ctx    context.Context
	blobs  *blob.Store
	digest blob.Digest
	size   int64
}

func (c storedContent) Size() int64 { return c.size }

func (c storedContent) Bytes(max int64) ([]byte, error) {
	if c.size > max {
		return nil, fmt.Errorf("signatures: the content is %d bytes, more than %d", c.size, max)
	}
	if c.blobs == nil {
		return nil, errors.New("signatures: no blob store")
	}
	rc, err := c.blobs.Open(c.ctx, c.digest, c.size, blob.OpenOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(io.LimitReader(rc, max+1))
}

func (c storedContent) ReaderAt() (io.ReaderAt, error) {
	if c.blobs == nil {
		return nil, errors.New("signatures: no blob store")
	}
	return c, nil
}

// ReadAt reads a part of the content with a ranged read of the object store.
func (c storedContent) ReadAt(p []byte, off int64) (int, error) {
	if off >= c.size {
		return 0, io.EOF
	}
	n := min(int64(len(p)), c.size-off)
	rc, err := c.blobs.Open(c.ctx, c.digest, c.size, blob.OpenOptions{Range: &blob.ByteRange{Offset: off, Length: n}})
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
