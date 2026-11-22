package blobcrypt

import (
	"crypto/cipher"
	"errors"
	"io"
	"strconv"
)

// Final is what an object's final segment carries: its plaintext size and the SHA-256 of its plaintext.
type Final struct {
	Size   int64
	Digest [32]byte
}

// Section is the stored form of a run of segments, sealed as it is read (R-STOR-080): an object's data segments, with or
// without its final segment, or a staging tail. It holds one segment in memory, never the ciphertext of the run. Its nonces
// derive from the write's seed, so every pass yields the same bytes and a retry that seeks back sends what a payload
// checksum was taken over (Q-STOR-301). A Section serves one goroutine; several Sections of one object may be read at once.
type Section struct {
	aead   cipher.AEAD
	nonces *nonces
	src    io.ReaderAt
	tail   bool
	offset int64 // a tail's session offset o
	first  int64 // an object's index of the run's first data segment
	plain  int64 // the plaintext the run's data or tail segments hold
	segs   int64
	final  *Final
	size   int64
	pos    int64
	cur    int64 // the segment in buf, counted within the run; -1 for none
	buf    []byte
	pbuf   []byte
	ad     []byte
}

// NewSection returns the stored bytes of an object's data segments first, first+1, … that hold the plain bytes src holds
// from its offset 0, followed by the final segment when final is not nil, sealed under the segment key of dataKey with the
// nonces of seed. A run without the final segment holds full segments only (a part of a multipart upload); a run with it
// ends the object: first·65,536 + plain is final.Size. The empty object is NewSection(k, seed, nil, 0, 0, &Final{Size: 0, …}).
func NewSection(dataKey Key, seed [32]byte, src io.ReaderAt, first, plain int64, final *Final) (*Section, error) {
	if first < 0 || plain < 0 || first > segments(MaxPlainSize) || plain > MaxPlainSize || first*SegmentSize+plain > MaxPlainSize {
		return nil, errOutOfRange
	}
	if final == nil && (plain == 0 || plain%SegmentSize != 0) {
		return nil, errors.New("blobcrypt: a run without the final segment holds whole segments; a short one is the object's last")
	}
	if final != nil && first*SegmentSize+plain != final.Size {
		return nil, errors.New("blobcrypt: the run does not end at the final segment's size " + strconv.FormatInt(final.Size, 10))
	}
	aead, err := dataKey.aead(labelSegments)
	if err != nil {
		return nil, err
	}
	s := &Section{aead: aead, nonces: newNonces(seed), src: src, first: first, plain: plain, segs: segments(plain), cur: -1}
	s.size = plain + SegmentOverhead*s.segs
	if final != nil {
		f := *final
		s.final = &f
		s.size += FinalSegmentSize
	}
	return s, nil
}

// NewTailSection returns the stored bytes of a staging tail that starts at the session offset offset and holds the plain
// bytes src holds from its offset 0, sealed under the tail key of dataKey with the nonces of seed (R-STOR-081, Q-STOR-321).
func NewTailSection(dataKey Key, seed [32]byte, offset int64, src io.ReaderAt, plain int64) (*Section, error) {
	if offset < 0 || plain < 1 || offset > MaxPlainSize || plain > MaxPlainSize-offset {
		return nil, errors.New("blobcrypt: a staging tail holds 1 byte or more within the first 4 TiB of the session")
	}
	aead, err := dataKey.aead(labelTails)
	if err != nil {
		return nil, err
	}
	s := &Section{aead: aead, nonces: newNonces(seed), src: src, tail: true, offset: offset, plain: plain, segs: tailSegments(plain), cur: -1}
	s.size = plain + SegmentOverhead*s.segs
	return s, nil
}

// Size is the number of stored bytes the section yields.
func (s *Section) Size() int64 { return s.size }

// Read yields the next stored bytes.
func (s *Section) Read(p []byte) (int, error) {
	if s.pos >= s.size {
		return 0, io.EOF
	}
	k, off := s.locate(s.pos)
	if k != s.cur {
		if err := s.fill(k); err != nil {
			return 0, err
		}
	}
	n := copy(p, s.buf[off:])
	s.pos += int64(n)
	return n, nil
}

// Seek sets the position of the next Read; a retry seeks to the start and reads the same bytes again.
func (s *Section) Seek(offset int64, whence int) (int64, error) {
	var pos int64
	switch whence {
	case io.SeekStart:
		pos = offset
	case io.SeekCurrent:
		pos = s.pos + offset
	case io.SeekEnd:
		pos = s.size + offset
	default:
		return 0, errors.New("blobcrypt: seek with an unknown whence")
	}
	if pos < 0 {
		return 0, errors.New("blobcrypt: seek before the start of the section")
	}
	s.pos = pos
	return pos, nil
}

// locate returns the segment of stored position pos, counted within the run (segs for the final segment), and the position
// within that segment's stored bytes.
func (s *Section) locate(pos int64) (k, off int64) {
	data := s.plain + SegmentOverhead*s.segs
	if pos >= data {
		return s.segs, pos - data
	}
	k = pos / FullSegmentSize
	return k, pos - k*FullSegmentSize
}

// fill seals segment k of the run into buf.
func (s *Section) fill(k int64) error {
	var nonce []byte
	if k == s.segs {
		i := s.first + s.segs
		nonce = s.nonces.segment(nil, markFinal, i)
		var t [FinalPlainSize]byte
		be64(t[:0], s.final.Size)
		copy(t[8:], s.final.Digest[:])
		s.ad = segmentAD(s.ad[:0], markFinal, i)
		s.seal(nonce, t[:], k)
		return nil
	}
	n := min(SegmentSize, s.plain-k*SegmentSize)
	if cap(s.pbuf) < SegmentSize {
		s.pbuf = make([]byte, SegmentSize)
	}
	p := s.pbuf[:n]
	if err := readFullAt(s.src, p, k*SegmentSize); err != nil {
		s.cur = -1
		return err
	}
	switch {
	case !s.tail:
		nonce = s.nonces.segment(nil, markData, s.first+k)
		s.ad = segmentAD(s.ad[:0], markData, s.first+k)
	case k == s.segs-1:
		nonce = s.nonces.tail(nil, markTailLast, s.offset, k)
		s.ad = tailAD(s.ad[:0], markTailLast, s.offset, k)
	default:
		nonce = s.nonces.tail(nil, markTail, s.offset, k)
		s.ad = tailAD(s.ad[:0], markTail, s.offset, k)
	}
	s.seal(nonce, p, k)
	return nil
}

func (s *Section) seal(nonce, plain []byte, k int64) {
	if cap(s.buf) < FullSegmentSize {
		s.buf = make([]byte, 0, FullSegmentSize)
	}
	s.buf = append(s.buf[:0], nonce...)
	s.buf = s.aead.Seal(s.buf, nonce, plain, s.ad)
	s.cur = k
}

// readFullAt reads len(p) bytes of src at off.
func readFullAt(src io.ReaderAt, p []byte, off int64) error {
	if len(p) == 0 {
		return nil
	}
	if src == nil {
		return errors.New("blobcrypt: a section with plaintext has no source")
	}
	n, err := src.ReadAt(p, off)
	if n == len(p) {
		return nil
	}
	if err == nil || errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return errors.Join(errors.New("blobcrypt: reading the plaintext at offset "+strconv.FormatInt(off, 10)), err)
}
