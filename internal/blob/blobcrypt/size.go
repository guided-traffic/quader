package blobcrypt

import (
	"errors"
	"strconv"
)

// errOutOfRange is a plaintext size outside 0 to MaxPlainSize handed to a writer or to StoredSize: a misuse, not a finding.
var errOutOfRange = errors.New("blobcrypt: the plaintext size is outside 0 to 4 TiB (storage.md R-STOR-076)")

// segments is ⌈L / 65,536⌉, the number of data segments of a plaintext of L bytes.
func segments(plain int64) int64 { return (plain + SegmentSize - 1) / SegmentSize }

// StoredSize is S(L) = L + 28·⌈L / 65,536⌉ + 68, the stored size of a plaintext of L bytes (R-STOR-076). A size outside 0 to
// MaxPlainSize is refused: a writer seals no more.
func StoredSize(plain int64) (int64, error) {
	if plain < 0 || plain > MaxPlainSize {
		return 0, errOutOfRange
	}
	return plain + SegmentOverhead*segments(plain) + FinalSegmentSize, nil
}

// PlainSize is the inverse of S: the plaintext size of an object of stored bytes, or a refusal with ReasonSize for a size no
// plaintext size produces (R-STOR-076). It reads no byte.
func PlainSize(stored int64) (int64, error) {
	notInFormat := func() error {
		return refuse(ReasonSize, strconv.FormatInt(stored, 10)+" bytes is no stored size of the format")
	}
	maxStored := MaxPlainSize + SegmentOverhead*segments(MaxPlainSize) + FinalSegmentSize
	if stored < FinalSegmentSize || stored > maxStored {
		return 0, notInFormat()
	}
	d := stored - FinalSegmentSize
	if d == 0 {
		return 0, nil
	}
	n := (d + FullSegmentSize - 1) / FullSegmentSize
	r := d - FullSegmentSize*(n-1)
	if r < SegmentOverhead+1 || r > FullSegmentSize {
		return 0, notInFormat()
	}
	return d - SegmentOverhead*n, nil
}

// tailSegments is the number of segments of a staging tail of plaintext bytes: ⌈m / 65,536⌉ for the m ≥ 1 a tail holds.
func tailSegments(plain int64) int64 { return segments(plain) }

// TailStoredSize is the stored size of a staging tail that holds plain bytes (R-STOR-081): m + 28·⌈m / 65,536⌉, with no
// final segment. A tail holds at least one byte (Q-STOR-321).
func TailStoredSize(plain int64) (int64, error) {
	if plain < 1 || plain > MaxPlainSize {
		return 0, errors.New("blobcrypt: a staging tail holds 1 byte to 4 TiB, not " + strconv.FormatInt(plain, 10))
	}
	return plain + SegmentOverhead*tailSegments(plain), nil
}

// Range is what a ranged read of the plaintext bytes First to Last of an object fetches (R-STOR-082, Q-STOR-317): the data
// segments FirstSegment to LastSegment and, when the window reaches the last data segment, the final segment with them, as
// the Length stored bytes from Offset. A Range comes from PlanRange.
type Range struct {
	First, Last               int64 // the plaintext window, both included
	FirstSegment, LastSegment int64 // the data segments that hold it
	Final                     bool  // the final segment is read with them
	Offset, Length            int64 // the stored bytes to request
	plain                     int64 // the object's plaintext size
}

// errRange is a window outside the plaintext: the caller answers 416, nothing is wrong with the object.
var errRange = errors.New("blobcrypt: the range is outside the plaintext")

// PlanRange plans the ranged read of the plaintext bytes first to last (both included) of an object of plain bytes.
func PlanRange(plain, first, last int64) (Range, error) {
	if plain < 1 || plain > MaxPlainSize || first < 0 || last < first || last >= plain {
		return Range{}, errRange
	}
	n := segments(plain)
	r := Range{First: first, Last: last, FirstSegment: first / SegmentSize, LastSegment: last / SegmentSize, plain: plain}
	r.Final = r.LastSegment == n-1
	r.Offset = r.FirstSegment * FullSegmentSize
	end := (r.LastSegment + 1) * FullSegmentSize
	if r.Final {
		end = plain + SegmentOverhead*n + FinalSegmentSize
	}
	r.Length = end - r.Offset
	return r, nil
}
