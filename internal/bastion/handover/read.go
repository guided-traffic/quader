package handover

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"hash"
	"io"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// firstChunk is how much plaintext Open reads before it returns: one segment of the stored format, so that on an encrypted
// copy the first segment has authenticated, and on a plain one the first bytes have arrived.
const firstChunk = blobcrypt.SegmentSize

// Read is an open direct read: the plaintext of the handed blob, verified as it is read. Read yields bytes only after the
// checks of their part passed (a plain body's bytes as they arrive; an encrypted body's segment after its tag, the last
// segment's only after the final segment and the digest), and the end only after the digest of the whole: a plain body
// whose SHA-256 is not the handed digest ends in [ErrDigest], an encrypted body in the format's refusal, ErrDigest
// included. Every other error is the read breaking off (a cut, a stall, a later segment that does not open). Close destroys
// the data key; it is called at every end.
type Read struct {
	body io.ReadCloser
	src  io.Reader
	key  *heldKey
	enc  bool
	buf  []byte
	err  error
}

func newRead(body io.ReadCloser, h iface.Handover, key *heldKey) *Read {
	r := &Read{body: body, key: key, enc: h.Encrypted}
	limited := io.LimitReader(body, h.StoredSize)
	if !h.Encrypted {
		want, _ := hex.DecodeString(h.SHA256)
		r.src = &plainReader{body: limited, left: h.Size, want: want, sum: sha256.New()}
		return r
	}
	var digest [32]byte
	raw, _ := hex.DecodeString(h.SHA256)
	copy(digest[:], raw)
	rd, err := blobcrypt.NewReader(&exact{r: body, left: h.StoredSize}, blobcrypt.DataKey{Key: key.key, Digest: digest, Size: h.Size}, true)
	if err != nil {
		r.err = err
		r.src = eofReader{}
		return r
	}
	r.src = &encryptedReader{rd: rd}
	return r
}

// first reads the first chunk of plaintext before Open returns: on an encrypted copy the first segment's plaintext, which
// the format releases only after its tag (and on an object of one segment only after the final segment and the digest); on
// a plain copy the first 64 KiB, or the whole body and its digest when it is not longer. So a blob of one segment is
// verified whole before Open returns, whatever pieces the transport delivers it in. A failure here is a failure before the
// first byte.
func (r *Read) first() error {
	if r.err != nil {
		return failure(CauseOther, "the data key does not open the object", nil)
	}
	r.buf = make([]byte, 0, firstChunk)
	for len(r.buf) < firstChunk {
		n, err := r.src.Read(r.buf[len(r.buf):firstChunk])
		r.buf = r.buf[:len(r.buf)+n]
		switch {
		case errors.Is(err, io.EOF):
			r.err = io.EOF // the whole blob, verified
			return nil
		case err != nil:
			return firstFailure(err)
		case r.enc && len(r.buf) > 0:
			return nil
		}
	}
	return nil
}

// firstFailure classifies a failure of the first bytes.
func firstFailure(err error) *Error {
	switch {
	case errors.Is(err, ErrDigest), errors.Is(err, blobcrypt.ErrDigest):
		e := failure(CauseOther, "the body is not the handed blob", ErrDigest)
		e.Digest = true
		return e
	case isRefusal(err):
		return failure(CauseOther, "the first segment does not open under the handed key", errors.New("refused"))
	}
	return failure(CauseNetwork, "the body broke off before its first byte", scrubbed(err))
}

// isRefusal reports a refusal of the stored format.
func isRefusal(err error) bool {
	_, ok := blobcrypt.ReasonOf(err)
	return ok
}

// Encrypted reports whether the read opens an encrypted copy.
func (r *Read) Encrypted() bool { return r.enc }

// Read implements io.Reader.
func (r *Read) Read(p []byte) (int, error) {
	if len(r.buf) > 0 {
		n := copy(p, r.buf)
		r.buf = r.buf[n:]
		return n, nil
	}
	if r.err != nil {
		return 0, r.err
	}
	n, err := r.src.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = laterFailure(err)
	}
	return n, err
}

// laterFailure is the error of a read that broke off after its first byte, without the link: the digest at the end, or
// the read breaking off.
func laterFailure(err error) error {
	switch {
	case errors.Is(err, ErrDigest), errors.Is(err, blobcrypt.ErrDigest):
		return ErrDigest
	case isRefusal(err):
		return errors.Join(errors.New("handover: a later segment does not open"), err)
	}
	return errors.Join(errors.New("handover: the body broke off"), scrubbed(err))
}

// Close ends the read: the body is closed and the data key destroyed. It is safe to call more than once.
func (r *Read) Close() error {
	err := r.body.Close()
	r.key.destroy()
	r.buf = nil
	return err
}

// plainReader yields a plain body as it arrives and ends in io.EOF only after its SHA-256 equals the handed digest.
type plainReader struct {
	body io.Reader
	left int64
	want []byte
	sum  hash.Hash
	err  error
}

func (p *plainReader) Read(b []byte) (int, error) {
	if p.err != nil {
		return 0, p.err
	}
	n, err := p.body.Read(b)
	p.sum.Write(b[:n])
	p.left -= int64(n)
	if errors.Is(err, io.EOF) {
		switch {
		case p.left > 0:
			err = io.ErrUnexpectedEOF
		case subtle.ConstantTimeCompare(p.sum.Sum(nil), p.want) != 1:
			err = ErrDigest
		}
	}
	if err != nil {
		p.err = err
	}
	return n, err
}

// encryptedReader is the format's reader; it exists so that both readers are one type for [Read].
type encryptedReader struct{ rd *blobcrypt.Reader }

func (e *encryptedReader) Read(b []byte) (int, error) { return e.rd.Read(b) }

// exact yields at most left bytes of r: the format's reader checks that nothing follows the final segment, and the body of
// an answer is the stored size by its Content-Length.
type exact struct {
	r    io.Reader
	left int64
}

func (e *exact) Read(b []byte) (int, error) {
	if e.left <= 0 {
		return 0, io.EOF
	}
	if int64(len(b)) > e.left {
		b = b[:e.left]
	}
	n, err := e.r.Read(b)
	e.left -= int64(n)
	if errors.Is(err, io.EOF) && e.left > 0 {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }
