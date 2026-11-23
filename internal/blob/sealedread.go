package blob

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// The reader of a connection (R-STOR-062, R-STOR-082): on a plain connection the stored bytes, refused when the object carries a
// record of the stored format; on an encrypted connection the record opened (steps 1 to 6 of R-STOR-077) before the first byte and
// the segments authenticated (step 7) as they are read. A refusal is a *blobcrypt.Error with its reason, counted as
// quader_storage_encrypted_read_failures_total{connection,reason}; which reason marks a copy is blobcrypt.Reason.MarksCorrupt, so a
// further reason is a constant and a case of that package and nothing here.

// Read returns the bytes of the blob that a copy on this connection holds, as one function per connection (R-STOR-062). On a plain
// connection they are the stored bytes; a full read (rng nil) is not checked here, and the caller verifies the digest and the
// stored size, as the download, the scrub and the streamed copies do. On an encrypted connection they are the decrypted and
// authenticated plaintext, whole or of the plaintext window rng: the record is opened without the blob's digest (Read does not
// know it), so it binds the plaintext to the digest its wrap carries, and the caller's own digest check binds it to the blob, as it
// does on a plain connection. The SHA-256 of the plaintext is the caller's to compute; the final segment's length and digest are
// checked whatever. The ObjectInfo is the store's: its Size is the stored object's (the total of a ranged answer). Use
// [Connection.ReadBlob] where the blob's digest and size are known.
func (c *Connection) Read(ctx context.Context, cp Copy, rng *ByteRange) (io.ReadCloser, ObjectInfo, error) {
	if c.Encrypted() {
		return c.readSealed(ctx, cp, blobcrypt.Want{}, rng, false)
	}
	return c.readPlain(ctx, cp, rng)
}

// ReadBlob is Read of a copy of the blob d of size bytes (the blob row's): on an encrypted connection the record must bind that
// digest and size (step 4 of R-STOR-077) and the copy's stored size (step 5), and verify asks the reader to compare the SHA-256
// of a whole read's plaintext as well (storage.download.verify, R-STOR-082). On a plain connection it is Read.
func (c *Connection) ReadBlob(ctx context.Context, d Digest, size int64, cp Copy, rng *ByteRange, verify bool) (io.ReadCloser, ObjectInfo, error) {
	if c.Encrypted() {
		return c.readSealed(ctx, cp, blobcrypt.Want{Known: true, Digest: d, Size: size, StoredSize: cp.StoredSize}, rng, verify)
	}
	return c.readPlain(ctx, cp, rng)
}

// readPlain is the reader of a plain connection: the stored bytes, refused as format when the object carries a record (an encrypted
// object on a connection that is not encrypted is not Quader's writing of the blob there, and its ciphertext must never reach a
// reader, also not a ranged one).
func (c *Connection) readPlain(ctx context.Context, cp Copy, rng *ByteRange) (io.ReadCloser, ObjectInfo, error) {
	body, info, err := c.objects.Get(ctx, cp.ObjectKey, rng)
	if err != nil {
		return nil, info, err
	}
	if _, sealed := info.Metadata[blobcrypt.MetaFormat]; sealed {
		_ = body.Close()
		return nil, info, c.refused(fmt.Errorf("%w: connection %s is not encrypted, and the object %s carries a record of the stored format",
			blobcrypt.ErrFormat, c.ID(), cp.ObjectKey))
	}
	return body, info, nil
}

// refused counts a refusal of the stored format on this connection and returns it; any other error passes uncounted.
func (c *Connection) refused(err error) error {
	if reason, ok := blobcrypt.ReasonOf(err); ok {
		_, m := c.sealing()
		m.encryptedReadFailure(c.ID(), string(reason))
	}
	return err
}

// readSealed is the reader of an encrypted connection (R-STOR-082). A whole read is one GetObject; a ranged read of the plaintext
// window [rng.Offset, rng.Offset+rng.Length) is one ranged GetObject of the window's segments, with the final segment when the window
// reaches the last data segment (Q-STOR-317). The record comes with the answer; steps 1 to 6 run before the reader is returned, so a
// refusal comes before the first byte and the caller falls over. want carries what the caller knows of the blob.
func (c *Connection) readSealed(ctx context.Context, cp Copy, want blobcrypt.Want, rng *ByteRange, verify bool) (io.ReadCloser, ObjectInfo, error) {
	keys, _ := c.sealing()
	if keys == nil {
		return nil, ObjectInfo{}, fmt.Errorf("blob: this replica holds no storage keys of connection %s", c.ID())
	}
	plain := want.Size
	if !want.Known {
		var err error
		if plain, err = blobcrypt.PlainSize(cp.StoredSize); err != nil {
			return nil, ObjectInfo{}, c.refused(fmt.Errorf("the copy of %s on connection %s: %w", cp.ObjectKey, c.ID(), err))
		}
	}
	var plan blobcrypt.Range
	var get *ByteRange
	if rng != nil {
		last := plain - 1
		if rng.Length >= 0 {
			last = min(last, rng.Offset+rng.Length-1)
		}
		if rng.Offset < 0 || rng.Offset >= plain || last < rng.Offset {
			// the answer a plain store gives a range beyond the object
			return nil, ObjectInfo{}, &StoreError{Op: "GetObject", Key: cp.ObjectKey, Code: "InvalidRange", Status: 416, Kind: KindOther}
		}
		var err error
		if plan, err = blobcrypt.PlanRange(plain, rng.Offset, last); err != nil {
			return nil, ObjectInfo{}, c.refused(err)
		}
		get = &ByteRange{Offset: plan.Offset, Length: plan.Length}
	}
	body, info, err := c.objects.Get(ctx, cp.ObjectKey, get)
	if err != nil {
		return nil, info, err
	}
	fail := func(err error) (io.ReadCloser, ObjectInfo, error) {
		_ = body.Close()
		return nil, info, c.refused(err)
	}
	size := info.Size
	if size < 0 {
		size = cp.StoredSize // the answer did not say: the segments and the final segment decide
	}
	if size != cp.StoredSize {
		return fail(fmt.Errorf("%w: the object %s on connection %s has %d bytes, its copy row says %d", blobcrypt.ErrSize, cp.ObjectKey, c.ID(),
			size, cp.StoredSize))
	}
	want.ObjectSize = size
	dk, err := blobcrypt.Open(info.Metadata, keys.Lookup(ctx, c.ID()), want)
	if err != nil {
		return fail(err) // a failed lookup carries no reason and counts nothing; destroyed_key has its reason, counts and marks (R-STOR-101)
	}
	var r *blobcrypt.Reader
	if rng == nil {
		r, err = blobcrypt.NewReader(body, dk, verify)
	} else {
		r, err = blobcrypt.NewRangeReader(body, dk, plan)
	}
	if err != nil {
		dk.Key.Destroy()
		return fail(err)
	}
	return &sealedBody{r: r, src: body, key: dk.Key, conn: c}, info, nil
}

// sealedBody is the plaintext of one read of an encrypted connection: it counts a refusal of the body once and destroys the read's
// data key when it is closed.
type sealedBody struct {
	r       *blobcrypt.Reader
	src     io.ReadCloser
	key     blobcrypt.Key
	conn    *Connection
	counted bool
}

func (b *sealedBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && !b.counted {
		if _, ok := blobcrypt.ReasonOf(err); ok {
			b.counted = true
			_ = b.conn.refused(err)
		}
	}
	return n, err
}

func (b *sealedBody) Close() error {
	b.key.Destroy()
	return b.src.Close()
}

// sealedDownload is a download's reader of a copy on an encrypted connection (Store.Open): a refusal of the body is evidence about
// the copy and marks it corrupt as its reason says (R-STOR-082); a whole read that verified the plaintext's SHA-256 stamps the copy
// verified (Q-STOR-233). The hold-back of R-STOR-013 is the reader's: the plaintext of the last segment stays back until the final
// segment and, verified, the digest have passed, so a client never receives a whole object that failed.
type sealedDownload struct {
	body   io.ReadCloser
	store  *Store
	ctx    context.Context
	digest Digest
	copy   Copy
	stamp  bool // a whole, verified read: its end stamps the copy verified
	undo   func()
	done   bool
}

func (v *sealedDownload) Read(p []byte) (int, error) {
	n, err := v.body.Read(p)
	switch {
	case err == nil || v.done:
	case errors.Is(err, io.EOF):
		v.done = true
		if v.stamp {
			v.store.stampVerified(v.ctx, v.digest, v.copy)
		}
	default:
		if reason, ok := blobcrypt.ReasonOf(err); ok {
			v.done = true
			if reason.MarksCorrupt() {
				v.store.markCopy(v.ctx, v.digest, v.copy.Connection, CopyCorrupt)
			}
			return n, fmt.Errorf("%w: connection %s: %w", ErrCorrupt, v.copy.Connection, err)
		}
	}
	return n, err
}

func (v *sealedDownload) Close() error {
	err := v.body.Close()
	if v.undo != nil {
		v.undo()
	}
	return err
}
