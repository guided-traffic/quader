package blobcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"hash"
)

// Format is the identifier of the format, the value of quader-format and the first bytes of every additional data.
const Format = "quader-blob-v1"

// The sizes of the format (R-STOR-076).
const (
	// KeySize is the size of a data key, a storage key and a name key.
	KeySize = 32
	// NonceSize is the size of the nonce stored in front of each segment.
	NonceSize = 12
	// TagSize is the size of the GCM tag behind each segment.
	TagSize = 16
	// SegmentSize is the plaintext of a full segment: every data segment but an object's last holds this much.
	SegmentSize = 64 << 10
	// SegmentOverhead is what a segment adds to its plaintext: its nonce and its tag.
	SegmentOverhead = NonceSize + TagSize
	// FullSegmentSize is the stored size of a full segment.
	FullSegmentSize = SegmentSize + SegmentOverhead
	// FinalPlainSize is the plaintext of the final segment: u64(L) and the SHA-256 of the plaintext.
	FinalPlainSize = 8 + sha256.Size
	// FinalSegmentSize is the stored size of the final segment.
	FinalSegmentSize = FinalPlainSize + SegmentOverhead
	// MaxPlainSize is the most plaintext one object holds, 4 TiB (Q-STOR-305).
	MaxPlainSize int64 = 1 << 42
	// MaxSegmentsPerKey is the most distinct segments one data key may seal, half the 2^32 invocations NIST SP 800-38D §8.3
	// allows one key with random 96-bit nonces; an upload session counts them (R-STOR-081, Q-STOR-318).
	MaxSegmentsPerKey int64 = 1 << 31
)

// The marks of the additional data: what a segment is, so that none passes for another kind.
const (
	markData     byte = 0x00 // a data segment of an object
	markFinal    byte = 0x01 // the final segment of an object
	markTail     byte = 0x02 // a segment of a staging tail
	markTailLast byte = 0x03 // the last segment of a staging tail
)

// The labels of the derivations: FMT followed by a space and the purpose.
const (
	labelSegments   = Format + " segments"
	labelCommitment = Format + " commitment"
	labelTails      = Format + " staging tails"
	labelWrap       = Format + " wrap"
	labelName       = Format + " name"
	labelNonce      = Format + " nonce"
)

// derive is HKDF-SHA-256 with 32 bytes of output; an absent salt is the empty string (R-STOR-076's notation).
func derive(ikm, salt []byte, info string) []byte {
	k, err := hkdf.Key(sha256.New, ikm, salt, info, KeySize)
	if err != nil {
		// HKDF-SHA-256 fails only for an output longer than 255 hash lengths; 32 bytes is one.
		panic("blobcrypt: HKDF-SHA-256 of 32 bytes failed: " + err.Error())
	}
	return k
}

// newGCM returns AES-256-GCM with the standard 12-byte nonce and 16-byte tag under key, and clears key.
func newGCM(key []byte) (cipher.AEAD, error) {
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// be64 appends u64(x) for an x that the caller checked to be zero or positive.
func be64(dst []byte, x int64) []byte {
	return binary.BigEndian.AppendUint64(dst, uint64(x)) //nolint:gosec // G115: every caller passes a size, an offset or an index it checked to be >= 0
}

// segmentAD appends the additional data of an object's segment: FMT ‖ u8(mark) ‖ u64(i).
func segmentAD(dst []byte, mark byte, i int64) []byte {
	dst = append(dst, Format...)
	dst = append(dst, mark)
	return be64(dst, i)
}

// tailAD appends the additional data of a tail segment: FMT ‖ u8(mark) ‖ u64(o) ‖ u64(j) (R-STOR-081).
func tailAD(dst []byte, mark byte, offset, j int64) []byte {
	dst = append(dst, Format...)
	dst = append(dst, mark)
	dst = be64(dst, offset)
	return be64(dst, j)
}

// wrapAD appends the additional data of a wrap: FMT ‖ " wrap" ‖ u8(len(id)) ‖ id. The caller checked the identifier, which
// is at most 64 bytes.
func wrapAD(dst []byte, id string) []byte {
	dst = append(dst, labelWrap...)
	dst = append(dst, byte(len(id))) //nolint:gosec // G115: ValidKeyID holds an identifier to 64 bytes before any wrap
	return append(dst, id...)
}

// nonces derives the nonces of one write from its seed (R-STOR-080): the first 12 bytes of HMAC-SHA-256(seed, FMT ‖ " nonce"
// ‖ u8(mark) ‖ u64(i)) for an object's segment, and of HMAC-SHA-256(seed, FMT ‖ " nonce" ‖ u8(mark) ‖ u64(o) ‖ u64(j)) for a
// tail segment (Q-STOR-321). One value serves one goroutine.
type nonces struct {
	mac hash.Hash
	msg []byte
	sum []byte
}

func newNonces(seed [32]byte) *nonces {
	return &nonces{mac: hmac.New(sha256.New, seed[:])}
}

func (n *nonces) segment(dst []byte, mark byte, i int64) []byte {
	n.msg = append(n.msg[:0], labelNonce...)
	n.msg = append(n.msg, mark)
	n.msg = be64(n.msg, i)
	return n.derive(dst)
}

func (n *nonces) tail(dst []byte, mark byte, offset, j int64) []byte {
	n.msg = append(n.msg[:0], labelNonce...)
	n.msg = append(n.msg, mark)
	n.msg = be64(n.msg, offset)
	n.msg = be64(n.msg, j)
	return n.derive(dst)
}

func (n *nonces) derive(dst []byte) []byte {
	n.mac.Reset()
	n.mac.Write(n.msg)
	n.sum = n.mac.Sum(n.sum[:0])
	return append(dst, n.sum[:NonceSize]...)
}
