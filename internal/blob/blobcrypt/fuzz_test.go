package blobcrypt

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

// The five fuzz targets of storage.md R-STOR-079. Their seeds are the vectors, so every go test runs them as plain tests;
// the nightly job fuzz-stored-format runs each for five minutes (R-STOR-091, make fuzz-stored-format).

// fuzzCase is a vector case the targets read under: its record, keys, copy, object and plaintext.
type fuzzCase struct {
	meta   map[string]string
	keys   KeyLookup
	want   Want
	stored []byte
	plain  []byte
	dek    Key
}

func fuzzCases(t testing.TB, names ...string) []fuzzCase {
	t.Helper()
	byName := map[string]vectorCase{}
	for _, c := range loadVectors(t).Cases {
		byName[c.Name] = c
	}
	out := make([]fuzzCase, 0, len(names))
	for _, n := range names {
		c := byName[n]
		object := c.file(t, c.Object)
		out = append(out, fuzzCase{
			meta: c.meta(t), keys: c.keys(t), want: c.want(t, object), stored: object, plain: c.plain(t),
			dek: mustKey(t, unhex(t, c.DataKey)),
		})
	}
	return out
}

// wantNamedOrNil fails unless err is nil or a refusal with a reason.
func wantNamedOrNil(t *testing.T, err error) {
	t.Helper()
	if _, ok := ReasonOf(err); err != nil && !ok {
		t.Fatalf("an error without a reason: %v", err)
	}
}

// FuzzPlainSize: the inverse of S never panics and agrees with S.
func FuzzPlainSize(f *testing.F) {
	limit, _ := StoredSize(MaxPlainSize)
	for _, s := range []int64{0, 67, 68, 69, 96, 97, 65631, 65632, 65633, 65660, 65661, 196789, limit, limit + 1, -1, math.MaxInt64, math.MinInt64} {
		f.Add(s, s-68)
	}
	f.Fuzz(func(t *testing.T, stored, plain int64) {
		l, err := PlainSize(stored)
		if err != nil {
			if !errors.Is(err, ErrSize) {
				t.Fatalf("the inverse of %d failed without the reason size: %v", stored, err)
			}
		} else {
			back, err := StoredSize(l)
			if err != nil || back != stored {
				t.Fatalf("the inverse of %d is %d, whose S is %d (%v)", stored, l, back, err)
			}
			if prev, err := StoredSize(l - 1); l > 0 && (err != nil || prev >= stored) {
				t.Fatalf("S is not increasing at %d", l)
			}
		}
		s, err := StoredSize(plain)
		if (err == nil) != (plain >= 0 && plain <= MaxPlainSize) {
			t.Fatalf("S(%d) gave %v", plain, err)
		}
		if err == nil {
			if back, err := PlainSize(s); err != nil || back != plain {
				t.Fatalf("S(%d) = %d inverts to %d (%v)", plain, s, back, err)
			}
		}
	})
}

// FuzzOpenRecord: any metadata map yields a data key or one of the named errors, and a data key only when the record is a
// genuine one of the content a (the vectors a and older-a), whose data key it then is: no input makes a record that opens
// to another key.
func FuzzOpenRecord(f *testing.F) {
	genuine := fuzzCases(f, "a", "older-a")
	a := genuine[0]
	for _, c := range loadVectors(f).Cases {
		if c.Record == "" {
			continue
		}
		m := c.meta(f)
		f.Add(m[MetaFormat], m[MetaKey], m[MetaWrap], m[MetaCommitment], uint8(0x0f), "quader-other", "x")
	}
	f.Add("", "", "", "", uint8(0), "", "")
	held := holds(mustStorageKey(f, "sk-default-1-0a1b2c3d", seq(0x20, 32)), mustStorageKey(f, "sk-default-2-0a1b2c3d", seq(0x20, 32)))
	f.Fuzz(func(t *testing.T, format, key, wrap, commit string, present uint8, extraKey, extraValue string) {
		meta := map[string]string{extraKey: extraValue}
		for i, kv := range [][2]string{{MetaFormat, format}, {MetaKey, key}, {MetaWrap, wrap}, {MetaCommitment, commit}} {
			if present&(1<<i) != 0 {
				meta[kv[0]] = kv[1]
			}
		}
		dk, err := Open(meta, held, a.want)
		wantNamedOrNil(t, err)
		if err == nil && ((!dk.Key.Equal(a.dek) && !dk.Key.Equal(genuine[1].dek)) || dk.Digest != a.want.Digest || dk.Size != a.want.Size) {
			t.Fatal("a record opened to another data key than a genuine one")
		}
	})
}

// FuzzReadObject: any body under a valid record yields the plaintext or a named error, and no byte of a segment that failed:
// what leaves is a prefix of the genuine plaintext made of whole segments, without the last one.
func FuzzReadObject(f *testing.F) {
	cases := fuzzCases(f, "empty", "a", "l1", "l65537")
	for i, c := range cases {
		f.Add(c.stored, uint8(i))
	}
	for _, name := range []string{"final-cut", "cut-inside", "appended", "other-segment", "data-as-final", "final-length", "spliced", "bit-in-segment", "bit-in-final"} {
		f.Add(fuzzCases(f, name)[0].stored, uint8(1)) // the negative vectors of a, under the genuine record of a
	}
	f.Fuzz(func(t *testing.T, body []byte, which uint8) {
		c := cases[int(which)%len(cases)]
		want := c.want
		want.ObjectSize = int64(len(body))
		released, err := readWhole(c.meta, c.keys, want, body, true)
		wantNamedOrNil(t, err)
		if err == nil && !bytes.Equal(released, c.plain) {
			t.Fatal("a body read to other bytes than the plaintext")
		}
		n := segments(int64(len(c.plain)))
		if err != nil && (len(released)%SegmentSize != 0 || int64(len(released)) > max(0, n-1)*SegmentSize || !bytes.HasPrefix(c.plain, released)) {
			t.Fatalf("%d bytes left before the refusal %v that are not whole authenticated segments before the last", len(released), err)
		}
	})
}

// FuzzReadRange: any range over a vector object, with one byte of the object changed or none and any total the store
// answers, yields exactly the window's bytes or an error, and no byte that was not authenticated.
func FuzzReadRange(f *testing.F) {
	cases := fuzzCases(f, "l196609", "l65537", "a")
	f.Add(int64(65530), int64(65540), uint32(0), uint8(0), int8(0), uint8(0))
	f.Add(int64(196599), int64(196608), uint32(196700), uint8(1), int8(0), uint8(0))
	f.Add(int64(0), int64(196608), uint32(70000), uint8(0x80), int8(0), uint8(0))
	f.Add(int64(0), int64(0), uint32(0), uint8(0), int8(-1), uint8(1))
	f.Add(int64(0), int64(0), uint32(5), uint8(4), int8(0), uint8(2))
	f.Fuzz(func(t *testing.T, first, last int64, flipAt uint32, flipBits uint8, totalDelta int8, which uint8) {
		c := cases[int(which)%len(cases)]
		stored := c.stored
		if flipBits != 0 {
			stored = bytes.Clone(stored)
			stored[int(flipAt)%len(stored)] ^= flipBits
		}
		want := c.want
		want.ObjectSize = int64(len(stored)) + int64(totalDelta)
		dk, err := Open(c.meta, c.keys, want)
		wantNamedOrNil(t, err)
		if err != nil {
			return
		}
		rg, err := PlanRange(dk.Size, first, last)
		if err != nil {
			return
		}
		r, err := NewRangeReader(bytes.NewReader(stored[rg.Offset:rg.Offset+rg.Length]), dk, rg)
		if err != nil {
			t.Fatal(err)
		}
		var cr countingReader
		err = cr.drain(r)
		wantNamedOrNil(t, err)
		window := c.plain[first : last+1]
		if err == nil && !bytes.Equal(cr.released, window) {
			t.Fatal("a range read to other bytes than its window")
		}
		if !bytes.HasPrefix(window, cr.released) {
			t.Fatal("a range released bytes that are not the window's")
		}
	})
}

// FuzzOpenTail: a staging tail object yields its plaintext or a named error, and what leaves before an error is a prefix of
// the plaintext without the last segment.
func FuzzOpenTail(f *testing.F) {
	var tail vectorCase
	for _, c := range loadVectors(f).Cases {
		if c.Kind == "tail" {
			tail = c
		}
	}
	stored, plain := tail.file(f, tail.Object), tail.plain(f)
	k := mustKey(f, unhex(f, tail.DataKey))
	sum := [32]byte(unhex(f, tail.Plaintext.SHA256))
	f.Add(stored, int32(0))
	f.Add(stored[:FullSegmentSize], int32(0))
	f.Add(stored, int32(SegmentSize))
	f.Add(flip(stored, FullSegmentSize+20), int32(0))
	f.Fuzz(func(t *testing.T, body []byte, offsetDelta int32) {
		r, err := NewTailReader(bytes.NewReader(body), k, *tail.Offset+int64(offsetDelta), int64(len(plain)), sum, int64(len(body)))
		if err != nil {
			wantNamedOrNil(t, err)
			return
		}
		var cr countingReader
		err = cr.drain(r)
		wantNamedOrNil(t, err)
		if err == nil && !bytes.Equal(cr.released, plain) {
			t.Fatal("a tail read to other bytes than its plaintext")
		}
		if err != nil && (len(cr.released)%SegmentSize != 0 || len(cr.released) >= len(plain) || !bytes.HasPrefix(plain, cr.released)) {
			t.Fatalf("%d bytes of a refused tail left that are not whole authenticated segments before the last", len(cr.released))
		}
	})
}

func mustStorageKey(t testing.TB, id string, raw []byte) *StorageKey {
	t.Helper()
	sk, err := NewStorageKey(id, raw)
	if err != nil {
		t.Fatal(err)
	}
	return sk
}
