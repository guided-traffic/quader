package cache

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// The objects of a cache with a key (R-BAS-061). Every one but the empty access markers is an object of the stored format of
// storage.md R-STOR-076: a data key of its own, drawn for the write and destroyed after it, wrapped under the storage key of
// the cache key with the record of R-STOR-077, which lies in front of the body (Q-BAS-212). Every sealed byte is
// internal/blob/blobcrypt's; this file only frames, names and reads.

// errChanged: the object changed between the read of its record and the read of a range of it (another replica wrote it
// again). It is no finding about the object; the caller serves a miss.
var errChanged = errors.New("cache: the object changed between the read of its record and the read of its range")

// putSealedFile stores the spooled, verified file at path under key, sealed.
func (b *backend) putSealedFile(ctx context.Context, key, path string, size int64, sum []byte) error {
	f, err := os.Open(path) //nolint:gosec // a file the cache spooled itself
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	var d [32]byte
	copy(d[:], sum)
	return b.putSealed(ctx, key, f, size, d)
}

// putSealedBytes stores payload under key, sealed and bound to its name (an entry, a file of the audit spool, the probe).
func (b *backend) putSealedBytes(ctx context.Context, key string, payload []byte) error {
	plain := bound(key, payload)
	return b.putSealed(ctx, key, bytes.NewReader(plain), int64(len(plain)), sha256.Sum256(plain))
}

// putSealed writes the size plain bytes of src, whose SHA-256 is digest, as an object of the stored format: the record in
// front, then the segments and the final segment, sealed as the store reads them (one segment in memory).
func (b *backend) putSealed(ctx context.Context, key string, src io.ReaderAt, size int64, digest [32]byte) error {
	dk, err := blobcrypt.NewKey(rand.Reader)
	if err != nil {
		return err
	}
	defer dk.Destroy()
	seed, err := blobcrypt.NewSeed(rand.Reader)
	if err != nil {
		return err
	}
	sec, err := blobcrypt.NewSection(dk, seed, src, 0, size, &blobcrypt.Final{Size: size, Digest: digest})
	if err != nil {
		return err
	}
	rec, err := blobcrypt.Wrap(b.seal.storage, blobcrypt.DataKey{Key: dk, Digest: digest, Size: size}, rand.Reader)
	if err != nil {
		return err
	}
	head := encodeFrame(rec)
	body := &framed{head: head, body: sec, size: int64(len(head)) + sec.Size(), bpos: -1}
	return b.s.putStream(ctx, key, body, body.size)
}

// openRecord reads the record in front of an object whose stored size is total (from the store's answer, or a stat when it
// gave none) and runs steps 1 to 6 of R-STOR-077: against the content address digest where the caller knows it (the bytes of
// an artifact or a document), against the record's own digest otherwise (an entry, a file of the spool, the probe), which the
// body's final segment and SHA-256 are then held to. It returns the data key and the frame's length.
func (b *backend) openRecord(ctx context.Context, key string, r io.Reader, total int64, digest []byte) (blobcrypt.DataKey, int64, error) {
	if total < 0 {
		size, _, err := b.s.stat(ctx, key)
		if err != nil {
			return blobcrypt.DataKey{}, 0, err
		}
		total = size
	}
	meta, n, err := readFrame(r, total)
	if err != nil {
		return blobcrypt.DataKey{}, 0, err
	}
	stored := total - n
	want := blobcrypt.Want{ObjectSize: stored}
	if digest != nil {
		plain, err := blobcrypt.PlainSize(stored)
		if err != nil {
			return blobcrypt.DataKey{}, 0, err
		}
		want = blobcrypt.Want{Known: true, Size: plain, StoredSize: stored, ObjectSize: stored}
		copy(want.Digest[:], digest)
	}
	dk, err := blobcrypt.Open(meta, b.seal.lookup, want)
	return dk, n, err
}

// getSealed opens the plaintext of the object at key, whole or a range (R-STOR-082's reads). A whole read checks every segment
// and the final segment with its length and digest; the SHA-256 of the plaintext is the verifying reader's, which
// [Cache.Open] puts around it. A range reads the record, then only the segments of its window, each authenticated before its
// bytes leave; damage outside the window is not seen (H-1300).
func (b *backend) getSealed(ctx context.Context, key, sha string, offset, length int64) (io.ReadCloser, error) {
	digest, err := hex.DecodeString(sha) // the layout validated it
	if err != nil {
		return nil, err
	}
	if offset > 0 || length >= 0 {
		return b.getSealedRange(ctx, key, digest, offset, length)
	}
	rc, total, err := b.s.get(ctx, key, 0, -1)
	if err != nil {
		return nil, err
	}
	dk, _, err := b.openRecord(ctx, key, rc, total, digest)
	if err != nil {
		_ = rc.Close()
		return nil, corruptOf(err)
	}
	r, err := blobcrypt.NewReader(rc, dk, false)
	dk.Key.Destroy() // the reader holds the segment cipher it derived; the data key is not needed after it
	if err != nil {
		_ = rc.Close()
		return nil, err
	}
	return &sealedBody{r: r, body: rc}, nil
}

// maxFrame is the longest frame: an identifier of 64 characters. A range reads this much to find the frame of any object.
const maxFrame = int64(frameFixed + 64)

func (b *backend) getSealedRange(ctx context.Context, key string, digest []byte, offset, length int64) (io.ReadCloser, error) {
	rc, total, err := b.s.get(ctx, key, 0, maxFrame)
	if err != nil {
		return nil, err
	}
	dk, n, err := b.openRecord(ctx, key, rc, total, digest)
	_ = rc.Close()
	if err != nil {
		return nil, corruptOf(err)
	}
	defer dk.Key.Destroy() // NewRangeReader derives its cipher at once
	last := dk.Size - 1
	if length >= 0 {
		last = min(last, offset+length-1)
	}
	if offset >= dk.Size || last < offset {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	rg, err := blobcrypt.PlanRange(dk.Size, offset, last)
	if err != nil {
		return nil, err
	}
	body, whole, err := b.s.get(ctx, key, n+rg.Offset, rg.Length)
	if err != nil {
		return nil, err
	}
	if total >= 0 && whole >= 0 && whole != total {
		_ = body.Close()
		return nil, errChanged
	}
	r, err := blobcrypt.NewRangeReader(body, dk, rg)
	if err != nil {
		_ = body.Close()
		return nil, err
	}
	return &sealedBody{r: r, body: body}, nil
}

// readSealedBytes reads an object whose plaintext is bound to key and returns its payload, at most limit bytes. Every segment,
// the final segment and the SHA-256 of the plaintext are checked before a byte is returned; a refusal, or a plaintext bound
// to another name, is an error that [refused] recognises.
func (b *backend) readSealedBytes(ctx context.Context, key string, limit int64) ([]byte, error) {
	rc, total, err := b.s.get(ctx, key, 0, -1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	dk, _, err := b.openRecord(ctx, key, rc, total, nil)
	if err != nil {
		return nil, err
	}
	r, err := blobcrypt.NewReader(rc, dk, true)
	dk.Key.Destroy()
	if err != nil {
		return nil, err
	}
	plain, err := readLimited(r, key, limit+int64(len(key))+1)
	if err != nil {
		return nil, err
	}
	payload, ok := unbind(key, plain)
	if !ok {
		return nil, errUnbound
	}
	return payload, nil
}

// errUnbound: an object of the format, made under the cache key, whose plaintext is bound to another name: an entry moved to
// another cache key's name, or an object of one family put under a name of another.
var errUnbound = errors.New("cache: the object's plaintext is bound to another name")

// plainSize is the plaintext size of a stored object of size bytes with the frame of the cache key's identifier; a size the
// format gives no plaintext size is [ErrCorrupt].
func (b *backend) plainSize(size int64) (int64, error) {
	n, err := blobcrypt.PlainSize(size - frameSize(b.seal.id))
	if err != nil {
		return 0, corruptOf(err)
	}
	return n, nil
}

// sealedBody is the plaintext of a sealed object; a refusal from the format's reader is [ErrCorrupt].
type sealedBody struct {
	r    io.Reader
	body io.Closer
}

func (s *sealedBody) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = corruptOf(err)
	}
	return n, err
}

func (s *sealedBody) Close() error { return s.body.Close() }

// framed is the stored object as the store reads it: the frame, then the section. It seeks, so that the bucket can read it
// once for the payload checksum and again to send it, and the SDK can retry; every pass yields the same bytes.
type framed struct {
	head []byte
	body io.ReadSeeker
	size int64
	pos  int64
	bpos int64 // the body's position, -1 when unknown
}

func (f *framed) Read(p []byte) (int, error) {
	if f.pos >= f.size {
		return 0, io.EOF
	}
	h := int64(len(f.head))
	if f.pos < h {
		n := copy(p, f.head[f.pos:])
		f.pos += int64(n)
		return n, nil
	}
	if off := f.pos - h; f.bpos != off {
		if _, err := f.body.Seek(off, io.SeekStart); err != nil {
			return 0, err
		}
		f.bpos = off
	}
	n, err := f.body.Read(p)
	f.pos += int64(n)
	f.bpos += int64(n)
	return n, err
}

func (f *framed) Seek(offset int64, whence int) (int64, error) {
	var pos int64
	switch whence {
	case io.SeekStart:
		pos = offset
	case io.SeekCurrent:
		pos = f.pos + offset
	case io.SeekEnd:
		pos = f.size + offset
	default:
		return 0, errors.New("cache: seek with an unknown whence")
	}
	if pos < 0 {
		return 0, errors.New("cache: seek before the start of the object")
	}
	f.pos = pos
	return pos, nil
}
