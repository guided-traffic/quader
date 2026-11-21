package items

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/guided-traffic/quader/internal/blob"
)

// Content is the committed bytes as a storage hook reads them (signatures.md R-SIG-011, Q-SIG-47): from the replica's spool where the
// commit had one, otherwise from the object store (a link of stored content). A hook reads only what it needs, bounded.
type Content interface {
	// Size is the length of the content.
	Size() int64
	// Bytes returns the whole content when it is at most max bytes long, and an error otherwise; it reads nothing then.
	Bytes(max int64) ([]byte, error)
	// ReaderAt reads parts of the content.
	ReaderAt() (io.ReaderAt, error)
}

// committed is the [Content] of one commit.
type committed struct {
	ctx   context.Context
	blobs Blobs
	sp    *Spooled
}

// contentOf is the content of a commit of sp.
func (s *Store) contentOf(ctx context.Context, sp *Spooled) Content {
	return committed{ctx: ctx, blobs: s.blobs, sp: sp}
}

func (c committed) Size() int64 { return c.sp.Size }

func (c committed) Bytes(max int64) ([]byte, error) {
	if c.sp.Size > max {
		return nil, fmt.Errorf("items: the content is %d bytes, more than %d", c.sp.Size, max)
	}
	if c.sp.Readable() {
		return io.ReadAll(c.sp.Reader())
	}
	if c.blobs == nil {
		return nil, fmt.Errorf("items: the content of a link cannot be read here")
	}
	rc, err := c.blobs.Open(c.ctx, c.sp.SHA256, c.sp.Size, blob.OpenOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(io.LimitReader(rc, max+1))
}

func (c committed) ReaderAt() (io.ReaderAt, error) {
	if ra, ok := c.sp.ReaderAt(); ok {
		return ra, nil
	}
	if c.blobs == nil {
		return nil, fmt.Errorf("items: the content of a link cannot be read here")
	}
	return rangedBlob{c}, nil
}

// rangedBlob reads parts of a stored blob with ranged reads of the object store.
type rangedBlob struct{ c committed }

func (r rangedBlob) ReadAt(p []byte, off int64) (int, error) {
	if off >= r.c.sp.Size {
		return 0, io.EOF
	}
	n := min(int64(len(p)), r.c.sp.Size-off)
	rc, err := r.c.blobs.Open(r.c.ctx, r.c.sp.SHA256, r.c.sp.Size, blob.OpenOptions{Range: &blob.ByteRange{Offset: off, Length: n}})
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

// SpooledContent is the [Content] of a spooled body that no commit of the item store wrote: the document a remote's fill stores in its
// own transaction (the proxy's metadata fill), read from the spool alone.
func SpooledContent(sp *Spooled) Content { return committed{sp: sp} }
