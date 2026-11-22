package blobcrypt

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"math"
	"sync"
	"testing"
)

// The writer's sections: a part encrypted twice is the same bytes twice, a retry that seeks back sends what was hashed, the
// parts of a multipart upload put together are the object a single request writes, and parts sealed at once agree
// (R-STOR-080's nonces, Q-STOR-301).
func TestSectionsAreDeterministicAndCompose(t *testing.T) {
	t.Parallel()
	k, _ := NewKey(rand.Reader)
	seed, _ := NewSeed(rand.Reader)
	plain := rule(3*SegmentSize + 77)
	final := &Final{Size: int64(len(plain)), Digest: sha256.Sum256(plain)}
	whole := sealWhole(t, k, seed, plain)

	// Two parts of 128 KiB and 64 KiB plus the rest with the final segment, read at once, as max_concurrent_parts sends them.
	cuts := []struct {
		first, plain int64
		final        *Final
	}{{0, 2 * SegmentSize, nil}, {2, SegmentSize + 77, final}}
	parts := make([][]byte, len(cuts))
	var wg sync.WaitGroup
	for i, c := range cuts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			src := io.NewSectionReader(bytes.NewReader(plain), c.first*SegmentSize, c.plain)
			s, err := NewSection(k, seed, src, c.first, c.plain, c.final)
			if err != nil {
				t.Error(err)
				return
			}
			parts[i], err = io.ReadAll(s)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if !bytes.Equal(cat(parts...), whole) {
		t.Fatal("the parts put together differ from the object written in one request")
	}

	// A retry: read part of the body, seek back, read it whole again.
	s, err := NewSection(k, seed, bytes.NewReader(plain), 0, int64(len(plain)), final)
	if err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 70000)
	if _, err := io.ReadFull(s, head); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	again, err := io.ReadAll(s)
	if err != nil || !bytes.Equal(again, whole) || !bytes.Equal(head, whole[:70000]) || s.Size() != int64(len(whole)) {
		t.Fatal("a pass after a seek back yields other bytes")
	}
	if pos, err := s.Seek(-int64(FinalSegmentSize), io.SeekEnd); err != nil || pos != int64(len(whole)-FinalSegmentSize) {
		t.Fatalf("seek from the end: %d %v", pos, err)
	}
	tail, _ := io.ReadAll(s)
	if !bytes.Equal(tail, whole[len(whole)-FinalSegmentSize:]) {
		t.Fatal("the final segment read after a seek differs")
	}

	// Another seed is other bytes for the same plaintext: a part a later request sends again gets other nonces.
	seed2, _ := NewSeed(rand.Reader)
	if bytes.Equal(sealWhole(t, k, seed2, plain)[:NonceSize], whole[:NonceSize]) {
		t.Fatal("two seeds gave the same nonce")
	}
}

func TestSectionsRefuseRunsThatBreakTheLayout(t *testing.T) {
	t.Parallel()
	k, _ := NewKey(rand.Reader)
	for _, c := range []struct {
		first, plain int64
		final        *Final
	}{
		{0, 0, nil},                       // an empty run
		{0, 100, nil},                     // a short segment without the final segment after it
		{0, SegmentSize, &Final{Size: 1}}, // a final segment that does not end the run
		{-1, SegmentSize, nil},
		{1, SegmentSize, &Final{Size: SegmentSize}},
		{1, math.MaxInt64 - SegmentSize + 1, nil}, // first·65,536 + plain would wrap below the limit
		{0, MaxPlainSize + SegmentSize, nil},
	} {
		if _, err := NewSection(k, [32]byte{}, bytes.NewReader(nil), c.first, c.plain, c.final); err == nil {
			t.Errorf("the run %+v was accepted", c)
		}
	}
	short := bytes.NewReader(make([]byte, 10)) // a source that holds less than the run says
	s, err := NewSection(k, [32]byte{}, short, 0, SegmentSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(s); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("a short source gave %v", err)
	}
}

// The staging tails of an upload session (R-STOR-081): sealed under the tail key with the session offset and their own
// marks, read back segment by segment, then compared by size and SHA-256.
func TestTailSegments(t *testing.T) {
	t.Parallel()
	k, _ := NewKey(rand.Reader)
	seed, _ := NewSeed(rand.Reader)
	const offset = 131072
	plain := rule(SegmentSize + 9)
	sum := sha256.Sum256(plain)
	s, err := NewTailSection(k, seed, offset, bytes.NewReader(plain), int64(len(plain)))
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := io.ReadAll(s)
	read := func(stored []byte, offset int64, sum [32]byte) ([]byte, error) {
		r, err := NewTailReader(bytes.NewReader(stored), k, offset, int64(len(plain)), sum, int64(len(stored)))
		if err != nil {
			return nil, err
		}
		var c countingReader
		err = c.drain(r)
		return c.released, err
	}
	if got, err := read(stored, offset, sum); err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("the tail does not read back: %v", err)
	}
	first := stored[:FullSegmentSize]

	t.Run("a tail read at another offset", func(t *testing.T) {
		got, err := read(stored, offset+SegmentSize, sum)
		wantRefused(t, got, err, ReasonSegment)
	})
	t.Run("a tail cut at its segment boundary or extended", func(t *testing.T) {
		for _, changed := range [][]byte{first, cat(stored, []byte{1})} {
			got, err := read(changed, offset, sum)
			wantRefused(t, got, err, ReasonSize)
		}
	})
	t.Run("the last segment replaced by one not marked last", func(t *testing.T) {
		// Made with the data key: the second segment sealed with the mark of a tail segment that is not the last.
		aead, _ := k.aead(labelTails)
		nonce := make([]byte, NonceSize)
		forged := aead.Seal(bytes.Clone(nonce), nonce, plain[SegmentSize:], tailAD(nil, markTail, offset, 1))
		got, err := read(cat(first, forged), offset, sum)
		wantRefusedAfter(t, got, err, ReasonSegment, plain[:SegmentSize])
	})
	t.Run("a tail whose row names another SHA-256", func(t *testing.T) {
		other := sum
		other[0] ^= 1
		got, err := read(stored, offset, other)
		wantRefusedAfter(t, got, err, ReasonDigest, plain[:SegmentSize])
	})
	t.Run("a tail segment is no blob segment, and a blob segment no tail segment", func(t *testing.T) {
		blob := sealWhole(t, k, seed, plain)
		dk := DataKey{Key: k, Digest: sum, Size: int64(len(plain))}
		r, _ := NewReader(bytes.NewReader(cat(stored, make([]byte, len(blob)-len(stored)))), dk, true)
		var c countingReader
		wantRefused(t, c.released, c.drain(r), ReasonSegment)
		got, err := read(blob[:len(stored)], offset, sum)
		wantRefused(t, got, err, ReasonSegment)
	})
	if _, err := NewTailSection(k, seed, offset, bytes.NewReader(nil), 0); err == nil {
		t.Error("an empty tail was sealed")
	}
}
