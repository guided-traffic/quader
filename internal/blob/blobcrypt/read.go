package blobcrypt

import (
	"crypto/cipher"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"hash"
	"io"
	"strconv"
)

// Reader is the plaintext of a stored object, of a window of one, or of a staging tail, read from its stored bytes: step 7
// of R-STOR-077. It hands out no byte of a segment before that segment authenticated at its place, and holds the window's
// bytes of the last segment back until the end has been checked: the final segment with the copy's length and digest and,
// where it hashes, the SHA-256 of the plaintext. A refusal is an *Error with its reason; a body that ends early is a
// transport error (io.ErrUnexpectedEOF in its chain), which marks nothing.
type Reader struct {
	aead   cipher.AEAD
	body   io.Reader
	tail   bool
	offset int64    // a tail's session offset o
	plain  int64    // the object's plaintext size L, or the tail's
	digest [32]byte // the copy's digest, or the tail's SHA-256
	next   int64    // the next segment to read
	last   int64    // the last data or tail segment to read
	final  bool     // the final segment follows the last data segment
	lo, hi int64    // the plaintext window [lo, hi) that leaves
	hash   hash.Hash
	sbuf   []byte
	pbuf   []byte
	ad     []byte
	out    []byte // released, not yet read
	held   []byte // the window's bytes of the last segment, until the end is checked
	err    error  // what Read returns once out is drained
}

// NewReader returns the plaintext of a whole object whose record Open returned as dk, from body, which yields the object's
// stored bytes from the first (R-STOR-082). With verify false it skips only the SHA-256 of the plaintext
// (storage.download.verify); every segment and the final segment, with its length and digest, are checked whatever it says.
func NewReader(body io.Reader, dk DataKey, verify bool) (*Reader, error) {
	if dk.Size < 0 || dk.Size > MaxPlainSize {
		return nil, errOutOfRange
	}
	aead, err := dk.Key.aead(labelSegments)
	if err != nil {
		return nil, err
	}
	r := &Reader{aead: aead, body: body, plain: dk.Size, digest: dk.Digest, last: segments(dk.Size) - 1, final: true, hi: dk.Size}
	if verify {
		r.hash = sha256.New()
	}
	return r, nil
}

// NewRangeReader returns the window rg of an object whose record Open returned as dk (with the total of the ranged answer as
// the object size), from body, which yields the rg.Length stored bytes from rg.Offset (R-STOR-082, Q-STOR-317). Damage outside
// the window is not seen (H-1300).
func NewRangeReader(body io.Reader, dk DataKey, rg Range) (*Reader, error) {
	want, err := PlanRange(dk.Size, rg.First, rg.Last)
	if err != nil {
		return nil, err
	}
	if want != rg {
		return nil, errors.New("blobcrypt: the range was planned for another plaintext size")
	}
	aead, err := dk.Key.aead(labelSegments)
	if err != nil {
		return nil, err
	}
	return &Reader{
		aead: aead, body: body, plain: dk.Size, digest: dk.Digest,
		next: rg.FirstSegment, last: rg.LastSegment, final: rg.Final, lo: rg.First, hi: rg.Last + 1,
	}, nil
}

// NewTailReader returns the plaintext of a staging tail that starts at the session offset offset, holds plain bytes whose
// SHA-256 is sum, and whose object the store answered with objectSize bytes (R-STOR-081): every segment authenticates, then
// the size and the SHA-256 are compared.
func NewTailReader(body io.Reader, dataKey Key, offset, plain int64, sum [32]byte, objectSize int64) (*Reader, error) {
	stored, err := TailStoredSize(plain)
	if err != nil {
		return nil, err
	}
	if offset < 0 || offset > MaxPlainSize-plain {
		return nil, errors.New("blobcrypt: a staging tail lies within the first 4 TiB of the session")
	}
	if objectSize != stored {
		return nil, refuse(ReasonSize, "a tail of "+strconv.FormatInt(plain, 10)+" bytes is stored as "+strconv.FormatInt(stored, 10)+
			", the store answered "+strconv.FormatInt(objectSize, 10))
	}
	aead, err := dataKey.aead(labelTails)
	if err != nil {
		return nil, err
	}
	return &Reader{
		aead: aead, body: body, tail: true, offset: offset, plain: plain, digest: sum,
		last: tailSegments(plain) - 1, hi: plain, hash: sha256.New(),
	}, nil
}

// Read yields the next authenticated plaintext of the window.
func (r *Reader) Read(p []byte) (int, error) {
	for len(r.out) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		r.err = r.step()
	}
	n := copy(p, r.out)
	r.out = r.out[n:]
	return n, nil
}

// step reads one segment, or the end once every segment is read.
func (r *Reader) step() error {
	if r.next > r.last {
		return r.finish()
	}
	k := r.next
	plain, err := r.open(k)
	if err != nil {
		return err
	}
	r.next++
	if r.hash != nil {
		r.hash.Write(plain)
	}
	start := k * SegmentSize
	w := plain[max(r.lo, start)-start : min(r.hi, start+int64(len(plain)))-start]
	if k == r.last && (r.final || r.hash != nil) {
		r.held = w
		return nil
	}
	r.out = w
	return nil
}

// open reads segment k's stored bytes and authenticates them with its index and mark.
func (r *Reader) open(k int64) ([]byte, error) {
	n := min(SegmentSize, r.plain-k*SegmentSize)
	if cap(r.sbuf) < FullSegmentSize {
		r.sbuf = make([]byte, FullSegmentSize)
		r.pbuf = make([]byte, SegmentSize)
	}
	stored := r.sbuf[:n+SegmentOverhead]
	if err := r.readFull(stored, "segment "+strconv.FormatInt(k, 10)); err != nil {
		return nil, err
	}
	switch {
	case !r.tail:
		r.ad = segmentAD(r.ad[:0], markData, k)
	case k == r.last:
		r.ad = tailAD(r.ad[:0], markTailLast, r.offset, k)
	default:
		r.ad = tailAD(r.ad[:0], markTail, r.offset, k)
	}
	plain, err := r.aead.Open(r.pbuf[:0], stored[:NonceSize], stored[NonceSize:], r.ad)
	if err != nil {
		return nil, refuse(ReasonSegment, "segment "+strconv.FormatInt(k, 10)+" does not authenticate at its place")
	}
	return plain, nil
}

// finish checks the end: the final segment, the SHA-256 of the plaintext, and that the body holds nothing more; then it
// releases what was held.
func (r *Reader) finish() error {
	if r.final {
		if err := r.openFinal(); err != nil {
			return err
		}
	}
	if r.hash != nil {
		var sum [32]byte
		if subtle.ConstantTimeCompare(r.hash.Sum(sum[:0]), r.digest[:]) != 1 {
			return refuse(ReasonDigest, "the SHA-256 of the plaintext is not the digest")
		}
	}
	var one [1]byte
	switch _, err := io.ReadFull(r.body, one[:]); {
	case err == nil:
		return refuse(ReasonSize, "the body holds more than the object's stored bytes")
	case !errors.Is(err, io.EOF):
		return err
	}
	r.out, r.held = r.held, nil
	return io.EOF
}

// openFinal authenticates the final segment at index n and compares its length and digest with the copy's.
func (r *Reader) openFinal() error {
	stored := make([]byte, FinalSegmentSize)
	if err := r.readFull(stored, "the final segment"); err != nil {
		return err
	}
	n := segments(r.plain)
	r.ad = segmentAD(r.ad[:0], markFinal, n)
	var t [FinalPlainSize]byte
	if _, err := r.aead.Open(t[:0], stored[:NonceSize], stored[NonceSize:], r.ad); err != nil {
		return refuse(ReasonFinal, "the final segment does not authenticate at index "+strconv.FormatInt(n, 10))
	}
	if binary.BigEndian.Uint64(t[:8]) != uint64(r.plain) || subtle.ConstantTimeCompare(t[8:], r.digest[:]) != 1 { //nolint:gosec // G115: the plaintext size is 0 to 2^42
		return refuse(ReasonFinal, "the final segment carries another length or digest than the copy's")
	}
	return nil
}

// readFull reads len(p) bytes of the body; a body that ends early is a transport error.
func (r *Reader) readFull(p []byte, what string) error {
	if _, err := io.ReadFull(r.body, p); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return errors.Join(errors.New("blobcrypt: the body ended or failed in "+what), err)
	}
	return nil
}
