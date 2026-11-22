package blobcrypt

import (
	"errors"
	"testing"
)

// S and its inverse as properties over every size around the segment boundaries and at the limit, without allocating a
// byte of data (R-STOR-076).
func TestR_STOR_076_StoredSizeAndItsInverseAgree(t *testing.T) {
	t.Parallel()
	t.Run("the boundary table and the sizes not in the format", func(t *testing.T) {
		for plain, stored := range map[int64]int64{
			0: 68, 1: 97, 65535: 65631, 65536: 65632, 65537: 65661, 16 << 20: 16784452, 100 << 20: 104902468,
			10 << 30: 10742005828, 1 << 42: 4399925559364,
		} {
			if got, err := StoredSize(plain); err != nil || got != stored {
				t.Errorf("S(%d) is %d (%v), the table says %d", plain, got, err, stored)
			}
			if got, err := PlainSize(stored); err != nil || got != plain {
				t.Errorf("the inverse of %d is %d (%v), the table says %d", stored, got, err, plain)
			}
		}
		for _, stored := range []int64{0, 67, 69, 96, 65633, 65660, -1, 4399925559365, 1 << 62} {
			if _, err := PlainSize(stored); !errors.Is(err, ErrSize) {
				t.Errorf("%d bytes is not in the format, the inverse gave %v", stored, err)
			}
		}
		for _, plain := range []int64{-1, MaxPlainSize + 1} {
			if _, err := StoredSize(plain); err == nil {
				t.Errorf("S(%d) was computed: a writer refuses it", plain)
			}
		}
		if _, err := NewSection(Key{}, [32]byte{}, nil, 0, 0, &Final{Size: MaxPlainSize + 1}); err == nil {
			t.Error("a writer accepted a plaintext of 2^42 + 1 bytes")
		}
	})

	// Around each of the first four boundaries and the last three before the limit, the plaintext sizes a to b within 100 of
	// it: S is strictly increasing over them, and of the stored sizes S(a) to S(b) exactly b - a + 1 are in the format, each
	// the S of one of them; the other sizes there are refused with the reason size.
	t.Run("every size in windows around the boundaries", func(t *testing.T) {
		n := segments(MaxPlainSize)
		for _, k := range []int64{0, 1, 2, 3, n - 2, n - 1, n} {
			a, b := max(0, k*SegmentSize-100), min(MaxPlainSize, k*SegmentSize+100)
			sa, _ := StoredSize(a)
			sb, _ := StoredSize(b)
			valid := int64(0)
			for s := sa; s <= sb; s++ {
				l, err := PlainSize(s)
				if err != nil {
					if !errors.Is(err, ErrSize) {
						t.Fatalf("the inverse of %d failed with %v", s, err)
					}
					continue
				}
				valid++
				if back, err := StoredSize(l); err != nil || back != s || l < a || l > b {
					t.Fatalf("the inverse of %d is %d, whose S is %d (%v)", s, l, back, err)
				}
			}
			if valid != b-a+1 {
				t.Fatalf("between S(%d) and S(%d) %d sizes are in the format, want %d", a, b, valid, b-a+1)
			}
			for l := a; l < b; l++ {
				s, _ := StoredSize(l)
				if next, _ := StoredSize(l + 1); next <= s {
					t.Fatalf("S(%d) = %d is not below S(%d) = %d", l, s, l+1, next)
				}
			}
		}
	})

	t.Run("around every boundary up to the limit, sampled", func(t *testing.T) {
		for k := int64(1); k <= segments(MaxPlainSize); k = k*3 + 1 {
			for _, l := range []int64{k*SegmentSize - 1, k * SegmentSize, k*SegmentSize + 1} {
				if l > MaxPlainSize {
					continue
				}
				s, err := StoredSize(l)
				if err != nil {
					t.Fatal(err)
				}
				if back, err := PlainSize(s); err != nil || back != l {
					t.Fatalf("S(%d) = %d inverts to %d (%v)", l, s, back, err)
				}
			}
		}
	})
}

func TestRangePlansTheSegmentsOfTheWindow(t *testing.T) {
	t.Parallel()
	const l = 196609
	for _, c := range []struct {
		first, last       int64
		firstSeg, lastSeg int64
		final             bool
		offset, length    int64
	}{
		{65530, 65540, 0, 1, false, 0, 2 * FullSegmentSize},                          // storage.md R-STOR-082's example
		{196599, 196608, 2, 3, true, 2 * FullSegmentSize, FullSegmentSize + 29 + 68}, // the last 10 bytes
		{0, 0, 0, 0, false, 0, FullSegmentSize},
		{196608, 196608, 3, 3, true, 3 * FullSegmentSize, 29 + 68},
	} {
		rg, err := PlanRange(l, c.first, c.last)
		if err != nil || rg.FirstSegment != c.firstSeg || rg.LastSegment != c.lastSeg || rg.Final != c.final ||
			rg.Offset != c.offset || rg.Length != c.length {
			t.Errorf("the plan of %d-%d is %+v (%v)", c.first, c.last, rg, err)
		}
	}
	for _, w := range [][2]int64{{-1, 3}, {5, 4}, {0, l}} {
		if _, err := PlanRange(l, w[0], w[1]); err == nil {
			t.Errorf("the window %v was planned", w)
		}
	}
	if _, err := PlanRange(0, 0, 0); err == nil {
		t.Error("a window of the empty object was planned")
	}
}
