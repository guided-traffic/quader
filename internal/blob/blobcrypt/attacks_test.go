package blobcrypt

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"testing"
)

// The attacks of storage.md R-STOR-078, one subtest per row of its table, each a change to an object of 196,609 bytes (three
// full data segments and one of one byte, then the final segment) or to its record, refused with its reason on a whole read
// and, where the row says so, on a ranged read; the counting reader shows what left before the refusal. The attacker can
// write the bucket and holds no storage key unless the row says otherwise. Every test fails if its defence is taken out:
// the read would succeed or end with another reason.
func TestR_STOR_078_EveryAttackOfTheTableIsRefusedWithItsReason(t *testing.T) {
	t.Parallel()
	sk := newStorageKey(t, "sk-archive-1-3f9a0c2e")
	keys := holds(sk)
	o := newObject(t, sk, rule(196609))
	other := newObject(t, sk, bytes.Repeat([]byte{0x5a}, 196609)) // another blob of the same size, under the same storage key
	n := segments(o.dk.Size)                                      // 4 data segments; the final segment is index 4
	seg := func(i int64) []byte { return o.segment(i) }

	t.Run("the object as written reads whole and by every range", func(t *testing.T) {
		got, err := readWhole(o.meta, keys, o.want(o.stored), o.stored, true)
		if err != nil || !bytes.Equal(got, o.plain) {
			t.Fatalf("the object does not read back: %v", err)
		}
		for _, w := range [][2]int64{{0, 0}, {65530, 65540}, {196599, 196608}, {0, 196608}, {131072, 131072}} {
			got, err := readRange(o.meta, keys, o.want(o.stored), o.stored, w[0], w[1])
			if err != nil || !bytes.Equal(got, o.plain[w[0]:w[1]+1]) {
				t.Fatalf("the range %v does not read back: %v", w, err)
			}
		}
	})

	t.Run("truncation at a segment boundary, the final segment cut off", func(t *testing.T) {
		for _, cut := range [][]byte{o.stored[:len(o.stored)-FinalSegmentSize], o.stored[:3*FullSegmentSize]} {
			got, err := readWhole(o.meta, keys, o.want(cut), cut, true)
			wantRefused(t, got, err, ReasonSize)
			got, err = readRange(o.meta, keys, o.want(cut), cut, 0, 10)
			wantRefused(t, got, err, ReasonSize)
		}
	})

	t.Run("truncation inside a segment, or bytes appended", func(t *testing.T) {
		for _, changed := range [][]byte{o.stored[:len(o.stored)-1], o.stored[:FullSegmentSize+100], cat(o.stored, []byte{0})} {
			got, err := readWhole(o.meta, keys, o.want(changed), changed, true)
			wantRefused(t, got, err, ReasonSize)
			got, err = readRange(o.meta, keys, o.want(changed), changed, 70000, 70010)
			wantRefused(t, got, err, ReasonSize)
		}
	})

	t.Run("segments reordered", func(t *testing.T) {
		swapped := cat(seg(0), seg(2), seg(1), seg(3), seg(n))
		got, err := readWhole(o.meta, keys, o.want(swapped), swapped, true)
		wantRefusedAfter(t, got, err, ReasonSegment, o.plain[:SegmentSize])
		got, err = readRange(o.meta, keys, o.want(swapped), swapped, 65536, 65540)
		wantRefused(t, got, err, ReasonSegment)
	})

	t.Run("a segment dropped, or another repeated in its place", func(t *testing.T) {
		dropped := cat(seg(0), seg(2), seg(3), seg(n))
		got, err := readWhole(o.meta, keys, o.want(dropped), dropped, true)
		wantRefused(t, got, err, ReasonSize)
		got, err = readRange(o.meta, keys, o.want(dropped), dropped, 0, 1)
		wantRefused(t, got, err, ReasonSize)
		repeated := cat(seg(0), seg(2), seg(2), seg(3), seg(n)) // the size holds; the index refuses
		got, err = readWhole(o.meta, keys, o.want(repeated), repeated, true)
		wantRefusedAfter(t, got, err, ReasonSegment, o.plain[:SegmentSize])
	})

	t.Run("a segment of another object put in", func(t *testing.T) {
		mixed := cat(seg(0), other.segment(1), seg(2), seg(3), seg(n))
		got, err := readWhole(o.meta, keys, o.want(mixed), mixed, true)
		wantRefusedAfter(t, got, err, ReasonSegment, o.plain[:SegmentSize])
		got, err = readRange(o.meta, keys, o.want(mixed), mixed, 65536, 70000)
		wantRefused(t, got, err, ReasonSegment)
		final := cat(seg(0), seg(1), seg(2), seg(3), other.segment(n)) // a forged final segment: another object's
		got, err = readWhole(o.meta, keys, o.want(final), final, true)
		wantRefusedAfter(t, got, err, ReasonFinal, o.plain[:3*SegmentSize])
		got, err = readRange(o.meta, keys, o.want(final), final, 196600, 196608) // segment 2's genuine bytes leave, segment 3's are held
		wantRefusedAfter(t, got, err, ReasonFinal, o.plain[196600:196608])
	})

	t.Run("a data segment presented as final, or the final segment as data", func(t *testing.T) {
		// A data segment sealed at the final segment's index, holding exactly what the final segment holds: only the mark
		// tells them apart.
		asFinal := cat(seg(0), seg(1), seg(2), seg(3), o.sealAt(t, markData, n, finalPlain(o.dk.Size, o.dk.Digest)))
		got, err := readWhole(o.meta, keys, o.want(asFinal), asFinal, true)
		wantRefusedAfter(t, got, err, ReasonFinal, o.plain[:3*SegmentSize])
		// An object of 40 bytes whose data segment is replaced by a final segment at index 0 of the same size.
		small := newObject(t, sk, rule(FinalPlainSize))
		asData := cat(small.sealAt(t, markFinal, 0, finalPlain(small.dk.Size, small.dk.Digest)), small.segment(1))
		got, err = readWhole(small.meta, keys, small.want(asData), asData, true)
		wantRefused(t, got, err, ReasonSegment)
		got, err = readRange(small.meta, keys, small.want(asData), asData, 0, 39)
		wantRefused(t, got, err, ReasonSegment)
	})

	t.Run("a changed length or digest in the final segment, made with the data key", func(t *testing.T) {
		for _, fp := range [][]byte{finalPlain(o.dk.Size+1, o.dk.Digest), finalPlain(o.dk.Size, other.dk.Digest)} {
			forged := cat(seg(0), seg(1), seg(2), seg(3), o.sealAt(t, markFinal, n, fp))
			got, err := readWhole(o.meta, keys, o.want(forged), forged, false)
			wantRefusedAfter(t, got, err, ReasonFinal, o.plain[:3*SegmentSize])
			got, err = readRange(o.meta, keys, o.want(forged), forged, 196608, 196608)
			wantRefused(t, got, err, ReasonFinal)
		}
	})

	t.Run("a whole object put under another blob's name", func(t *testing.T) {
		got, err := readWhole(other.meta, keys, o.want(other.stored), other.stored, true)
		wantRefused(t, got, err, ReasonRecord)
		got, err = readRange(other.meta, keys, o.want(other.stored), other.stored, 0, 10)
		wantRefused(t, got, err, ReasonRecord)
	})

	t.Run("the record of another object put on this body", func(t *testing.T) {
		got, err := readWhole(other.meta, keys, o.want(o.stored), o.stored, true)
		wantRefused(t, got, err, ReasonRecord)
		// Unchecked, as by a reader without a database, the other data key fails the first segment.
		unchecked := Want{ObjectSize: int64(len(o.stored))}
		got, err = readWhole(other.meta, keys, unchecked, o.stored, true)
		wantRefused(t, got, err, ReasonSegment)
	})

	t.Run("a changed record: format, key identifier, wrap or commitment", func(t *testing.T) {
		rec := o.meta
		otherKey := newStorageKey(t, "sk-archive-2-77aa00bb")
		both := holds(sk, otherKey)
		for _, c := range []struct {
			name   string
			meta   map[string]string
			reason Reason
		}{
			{"another format", withMeta(rec, MetaFormat, "quader-blob-v2"), ReasonFormat},
			{"no format: a plain object", map[string]string{}, ReasonFormat},
			{"an identifier this connection does not hold", withMeta(rec, MetaKey, "sk-archive-1-00000000"), ReasonUnknownKey},
			{"the identifier of another key of this connection", withMeta(rec, MetaKey, otherKey.ID), ReasonRecord},
			{"no identifier", withMeta(rec, MetaKey, ""), ReasonRecord},
			{"an identifier that cannot be one", withMeta(rec, MetaKey, "SK_ARCHIVE"), ReasonRecord},
			{"one bit of the wrap", withMeta(rec, MetaWrap, b64().EncodeToString(flip(mustDecode(t, rec[MetaWrap]), 60))), ReasonRecord},
			{"the wrap's salt", withMeta(rec, MetaWrap, b64().EncodeToString(flip(mustDecode(t, rec[MetaWrap]), 0))), ReasonRecord},
			{"a wrap cut short", withMeta(rec, MetaWrap, rec[MetaWrap][:150]), ReasonRecord},
			{"no wrap", withMeta(rec, MetaWrap, ""), ReasonRecord},
			{"one bit of the commitment", withMeta(rec, MetaCommitment, b64().EncodeToString(flip(mustDecode(t, rec[MetaCommitment]), 5))), ReasonCommitment},
			{"no commitment", withMeta(rec, MetaCommitment, ""), ReasonCommitment},
		} {
			got, err := readWhole(c.meta, both, o.want(o.stored), o.stored, true)
			if c.reason != ReasonFormat || len(c.meta) > 0 {
				wantRefused(t, got, err, c.reason)
			} else {
				wantRefused(t, got, err, ReasonFormat)
			}
			got, err = readRange(c.meta, both, o.want(o.stored), o.stored, 0, 0)
			wantRefused(t, got, err, c.reason)
		}
		// Keys below quader- that this version does not know are ignored.
		extra := withMeta(rec, "quader-wrap-2", "anything")
		if got, err := readWhole(extra, keys, o.want(o.stored), o.stored, true); err != nil || !bytes.Equal(got, o.plain) {
			t.Fatalf("an unknown key below quader- refused the object: %v", err)
		}
	})

	t.Run("a record made under a storage key of another connection or another installation", func(t *testing.T) {
		elsewhere := newStorageKey(t, "sk-mirror-1-0badf00d")
		foreign := newObject(t, elsewhere, o.plain)
		got, err := readWhole(foreign.meta, keys, o.want(foreign.stored), foreign.stored, true)
		wantRefused(t, got, err, ReasonUnknownKey)
		if reason, _ := ReasonOf(err); reason.MarksCorrupt() {
			t.Fatal("an unknown storage key marks the copy corrupt")
		}
		sameID := newStorageKey(t, sk.ID) // another installation drew a key with the same identifier
		twin := newObject(t, sameID, o.plain)
		got, err = readWhole(twin.meta, keys, o.want(twin.stored), twin.stored, true)
		wantRefused(t, got, err, ReasonRecord)
	})

	t.Run("the object copied to another connection of the same installation", func(t *testing.T) {
		b := holds(newStorageKey(t, "sk-backup-1-12345678"))
		got, err := readWhole(o.meta, b, o.want(o.stored), o.stored, true)
		wantRefused(t, got, err, ReasonUnknownKey)
		got, err = readRange(o.meta, b, o.want(o.stored), o.stored, 5, 6)
		wantRefused(t, got, err, ReasonUnknownKey)
	})

	t.Run("a ciphertext that opens under two data keys: the commitment", func(t *testing.T) {
		// A holder of the storage key wraps a second data key for this body and keeps the first key's commitment.
		second, err := NewKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		rec, err := Wrap(sk, DataKey{Key: second, Digest: o.dk.Digest, Size: o.dk.Size}, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		rec.Commitment, _ = o.dk.Commitment()
		got, err := readWhole(rec.Metadata(), keys, o.want(o.stored), o.stored, true)
		wantRefused(t, got, err, ReasonCommitment)
		got, err = readRange(rec.Metadata(), keys, o.want(o.stored), o.stored, 100, 200)
		wantRefused(t, got, err, ReasonCommitment)
	})

	t.Run("parts of two sealings of one upload session spliced", func(t *testing.T) {
		// A part sent again with other bytes: the same data key and index, a new seed, other plaintext. Every tag passes.
		resent := bytes.Clone(o.plain[SegmentSize : 2*SegmentSize])
		resent[7] ^= 0xff
		seed, _ := NewSeed(rand.Reader)
		part, err := NewSection(o.dk.Key, seed, bytes.NewReader(resent), 1, SegmentSize, nil)
		if err != nil {
			t.Fatal(err)
		}
		spliced := make([]byte, part.Size())
		if _, err := part.Read(spliced); err != nil {
			t.Fatal(err)
		}
		mixed := cat(seg(0), spliced, seg(2), seg(3), seg(n))
		got, err := readWhole(o.meta, keys, o.want(mixed), mixed, true)
		wantRefusedAfter(t, got, err, ReasonDigest, cat(o.plain[:SegmentSize], resent, o.plain[2*SegmentSize:3*SegmentSize]))
		// storage.download.verify: false skips exactly this check (R-STOR-082).
		if _, err := readWhole(o.meta, keys, o.want(mixed), mixed, false); err != nil {
			t.Fatalf("without the plaintext's SHA-256 the splice is not seen, as specified; got %v", err)
		}
	})

	t.Run("one bit changed anywhere", func(t *testing.T) {
		for _, at := range bitPositions(o) {
			changed := flip(o.stored, int(at.pos))
			got, err := readWhole(o.meta, keys, o.want(changed), changed, true)
			wantRefusedAfter(t, got, err, at.reason, o.plain[:at.released])
		}
	})

	t.Run("an older genuine object of the same content put back", func(t *testing.T) {
		older := newObject(t, sk, o.plain)
		if bytes.Equal(older.stored, o.stored) {
			t.Fatal("two writes of one content gave the same bytes")
		}
		got, err := readWhole(older.meta, keys, o.want(older.stored), older.stored, true)
		if err != nil || !bytes.Equal(got, o.plain) {
			t.Fatalf("the older object does not open to the same plaintext: %v", err)
		}
	})
}

type bitPosition struct {
	pos      int64
	reason   Reason
	released int64 // the plaintext released before the refusal
}

// bitPositions are the first and last byte of each segment's nonce, ciphertext and tag, and of the final segment.
func bitPositions(o object) []bitPosition {
	var out []bitPosition
	n := segments(o.dk.Size)
	for i := int64(0); i <= n; i++ {
		start := i * FullSegmentSize
		size := int64(len(o.segment(i)))
		reason, released := ReasonSegment, i*SegmentSize
		if i == n {
			start, reason, released = int64(len(o.stored))-FinalSegmentSize, ReasonFinal, (n-1)*SegmentSize
		}
		if i == n-1 {
			released = (n - 1) * SegmentSize // the last data segment is held, never released before the end
		}
		for _, off := range []int64{0, NonceSize - 1, NonceSize, size - TagSize - 1, size - TagSize, size - 1} {
			if off >= 0 && off < size {
				out = append(out, bitPosition{pos: start + off, reason: reason, released: min(released, (n-1)*SegmentSize)})
			}
		}
	}
	return out
}

func mustDecode(t testing.TB, s string) []byte {
	t.Helper()
	b, err := b64().DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The reader hands out no byte it has not authenticated: for every segment of an object that fails, a whole read releases
// exactly the segments before it, and a ranged read releases nothing of a window that touches it.
func TestR_STOR_078_NoByteLeavesBeforeItsSegmentAuthenticated(t *testing.T) {
	t.Parallel()
	sk := newStorageKey(t, "sk-default-1-0a1b2c3d")
	o := newObject(t, sk, rule(3*SegmentSize+10))
	n := segments(o.dk.Size)
	for k := int64(0); k < n; k++ {
		changed := flip(o.stored, int(k*FullSegmentSize+NonceSize+5))
		got, err := readWhole(o.meta, holds(sk), o.want(changed), changed, true)
		wantRefusedAfter(t, got, err, ReasonSegment, o.plain[:min(k, n-1)*SegmentSize])
		// A window that starts 3 bytes before the changed segment releases those 3 authenticated bytes and nothing of it.
		first := max(0, k*SegmentSize-3)
		got, err = readRange(o.meta, holds(sk), o.want(changed), changed, first, k*SegmentSize+3)
		wantRefusedAfter(t, got, err, ReasonSegment, o.plain[first:k*SegmentSize])
		// A window that does not touch the changed segment is served: the damage is not seen (H-1300).
		if k > 0 {
			got, err = readRange(o.meta, holds(sk), o.want(changed), changed, 0, 9)
			if err != nil || !bytes.Equal(got, o.plain[:10]) {
				t.Fatalf("a window before the damage did not read: %v", err)
			}
		}
	}
	// Damage in the final segment holds the last data segment back on a whole read and on a ranged read that reaches it.
	changed := flip(o.stored, len(o.stored)-3)
	got, err := readWhole(o.meta, holds(sk), o.want(changed), changed, false)
	wantRefusedAfter(t, got, err, ReasonFinal, o.plain[:(n-1)*SegmentSize])
	got, err = readRange(o.meta, holds(sk), o.want(changed), changed, 3*SegmentSize, 3*SegmentSize+9)
	wantRefused(t, got, err, ReasonFinal)
}

// A store that answers more bytes than the size it declared: the bytes after the final segment are refused as size, and the
// last data segment, held back, never leaves; a body that ends early is a transport error with no reason, which marks nothing.
func TestABodyOfAnotherLengthThanItsSizeIsCaught(t *testing.T) {
	t.Parallel()
	sk := newStorageKey(t, "sk-default-1-0a1b2c3d")
	o := newObject(t, sk, rule(SegmentSize+5))
	dk, err := Open(o.meta, holds(sk), o.want(o.stored))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		body   []byte
		reason Reason
	}{{cat(o.stored, []byte{0}), ReasonSize}, {o.stored[:len(o.stored)-1], ""}} {
		r, err := NewReader(bytes.NewReader(c.body), dk, true)
		if err != nil {
			t.Fatal(err)
		}
		var cr countingReader
		err = cr.drain(r)
		if reason, ok := ReasonOf(err); err == nil || reason != c.reason || ok != (c.reason != "") {
			t.Fatalf("a body of %d bytes for %d gave %v", len(c.body), len(o.stored), err)
		}
		if !bytes.Equal(cr.released, o.plain[:SegmentSize]) {
			t.Fatalf("%d bytes left, want the first segment's only", len(cr.released))
		}
	}
}

// A plain object on an encrypted connection: no record, a body of any bytes. It is refused at step 1 before a byte leaves.
func TestR_STOR_077_AnObjectWithoutRecordIsRefusedAsFormat(t *testing.T) {
	t.Parallel()
	body := []byte("plain content written by someone else")
	sum := sha256.Sum256(body)
	got, err := readWhole(nil, holds(), Want{Known: true, Digest: sum, Size: int64(len(body)), ObjectSize: int64(len(body))}, body, true)
	wantRefused(t, got, err, ReasonFormat)
}
