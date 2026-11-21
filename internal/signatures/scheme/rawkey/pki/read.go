package pki

import (
	"context"
	"errors"
	"fmt"
	"hash"
	"io"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// Open opens a stored blob: a blob that is not stored is a signature that could not be read (the job handles a missing subject before
// the check, so what is missing here is part of the object or its content), and any other error is one of the object store.
func Open(ctx context.Context, in scheme.Input, digest [32]byte) (scheme.Content, error) {
	if in.Blobs == nil {
		return nil, Malformed("no blob store was handed to the check")
	}
	c, err := in.Blobs.Content(ctx, digest)
	if errors.Is(err, scheme.ErrBlobMissing) {
		return nil, Malformed("a blob the signature names is not stored")
	}
	if err != nil {
		return nil, Transient(err)
	}
	return c, nil
}

// Blob reads a stored blob whole when it is at most max bytes long; a longer one is beyond the bounds of R-SIG-013.
func Blob(ctx context.Context, in scheme.Input, digest [32]byte, max int64) ([]byte, error) {
	c, err := Open(ctx, in, digest)
	if err != nil {
		return nil, err
	}
	if c.Size() > max {
		return nil, TooLarge("a blob of the signature is %d bytes, more than the bound of %d", c.Size(), max)
	}
	b, err := c.Bytes(max)
	if err != nil {
		return nil, Transient(err)
	}
	return b, nil
}

// Object reads the signature object as the job hands it: its bytes, or the whole item blob that holds it, bounded by
// signatures.max_object_size. An object inside a blob (a locator) is read by the scheme that knows the locator.
func Object(ctx context.Context, in scheme.Input) ([]byte, error) {
	if len(in.Raw) > 0 {
		if int64(len(in.Raw)) > in.Limits.MaxObjectSize {
			return nil, TooLarge("the object is larger than signatures.max_object_size")
		}
		return in.Raw, nil
	}
	if in.Locator != "" {
		return nil, Malformed("the object's locator %q is not one this scheme reads", in.Locator)
	}
	return Blob(ctx, in, in.ItemBlob, in.Limits.MaxObjectSize)
}

// Stream hashes a stored blob once into h, bounded by signatures.max_content_size (above it the content is not read: unsupported,
// limits_exceeded).
func Stream(ctx context.Context, in scheme.Input, digest [32]byte, h hash.Hash) error {
	c, err := Open(ctx, in, digest)
	if err != nil {
		return err
	}
	if c.Size() > in.Limits.MaxContentSize {
		return Unsupported(signatures.ReasonLimitsExceeded, "the content is %d bytes, more than signatures.max_content_size", c.Size())
	}
	r, err := c.Reader()
	if err != nil {
		return Transient(err)
	}
	defer r.Close()
	n, err := io.Copy(h, &ctxReader{ctx: ctx, r: r})
	if err != nil {
		if ctx.Err() != nil {
			return Transient(ctx.Err())
		}
		return Transient(err)
	}
	if n != c.Size() {
		return Transient(fmt.Errorf("the content stream ended after %d of %d bytes", n, c.Size()))
	}
	return nil
}

// ctxReader stops a stream when the check's context ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
