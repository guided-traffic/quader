package blob

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// The conformance check of an encrypted connection (R-STOR-091, the pass against a real store): what the writer and the reader of
// R-STOR-080 to R-STOR-082 ask of a store, with a storage key drawn for the check and forgotten after it. It writes and reads only
// probe objects, never the database, so it runs wherever the conformance check runs: `quader admin storage check`, the start of
// nothing (the start check has its own probe of metadata), and the job s3-conformance.

// sealedProbePlain is the probe's plaintext: two data segments, the second short, so that a ranged read crosses a segment boundary.
const sealedProbePlain = blobcrypt.SegmentSize + 100

func checkSealedObject(ctx context.Context, e *ConformanceEnv) error {
	raw := payload(blobcrypt.KeySize)
	sk, err := blobcrypt.NewStorageKey("sk-probe-1-0c0ffee0", raw)
	clear(raw)
	if err != nil {
		return err
	}
	defer sk.Key.Destroy()
	lookup := func(id string) (*blobcrypt.StorageKey, error) {
		if id == sk.ID {
			return sk, nil
		}
		return nil, nil
	}
	plain := payload(sealedProbePlain)
	want := blobcrypt.Want{Known: true, Digest: sha256.Sum256(plain), Size: int64(len(plain))}
	dek, err := blobcrypt.NewKey(rand.Reader)
	if err != nil {
		return err
	}
	defer dek.Destroy()
	dk := blobcrypt.DataKey{Key: dek, Digest: want.Digest, Size: want.Size}
	seed, err := blobcrypt.NewSeed(rand.Reader)
	if err != nil {
		return err
	}
	sec, err := blobcrypt.NewSection(dek, seed, bytes.NewReader(plain), 0, want.Size, &blobcrypt.Final{Size: want.Size, Digest: want.Digest})
	if err != nil {
		return err
	}
	want.StoredSize = sec.Size()
	rec, err := blobcrypt.Wrap(sk, dk, rand.Reader)
	if err != nil {
		return err
	}
	key := e.Key("sealed")
	sum, err := sendChecksum(e.Opts.PayloadChecksum, sec)
	if err != nil {
		return err
	}
	if err := e.Store.Put(ctx, key, sec, sec.Size(), sum, rec.Metadata()); err != nil {
		return fmt.Errorf("write an object of the stored format with its record: %w", err)
	}
	info, err := e.Store.Head(ctx, key)
	if err != nil {
		return err
	}
	if info.Metadata[blobcrypt.MetaFormat] == "" {
		return note("skipped: the store keeps no metadata, and a connection on it cannot be encrypted (R-STOR-075)")
	}
	if info.Size != want.StoredSize || !sameRecord(info.Metadata, rec.Metadata()) {
		return fmt.Errorf("HeadObject answered %d bytes and another record than the %d bytes and the record written", info.Size, want.StoredSize)
	}
	if err := readSealedProbe(ctx, e.Store, key, lookup, want, plain); err != nil {
		return err
	}
	// the finalize's copy with the record (R-STOR-081): the copy carries a new wrap of the same data key
	rec2, err := blobcrypt.Wrap(sk, dk, rand.Reader)
	if err != nil {
		return err
	}
	dst := e.Key("sealed-copy")
	if err := e.Store.Copy(ctx, key, dst, want.StoredSize, rec2.Metadata()); err != nil {
		return fmt.Errorf("copy with a replaced record: %w", err)
	}
	info, err = e.Store.Head(ctx, dst)
	if err != nil {
		return err
	}
	if !sameRecord(info.Metadata, rec2.Metadata()) {
		return note("the copy kept its source's record (copyReplacesMetadata: false): an upload session's finalize streams on this store (Q-STOR-306)")
	}
	return readSealedProbe(ctx, e.Store, dst, lookup, want, plain)
}

// sameRecord reports whether got carries the four keys of the record want.
func sameRecord(got, want map[string]string) bool {
	for _, k := range []string{blobcrypt.MetaFormat, blobcrypt.MetaKey, blobcrypt.MetaWrap, blobcrypt.MetaCommitment} {
		if got[k] != want[k] {
			return false
		}
	}
	return true
}

// readSealedProbe reads the probe whole and the plaintext bytes 65,530 to 65,540, which cross the boundary of its two data segments,
// through the record the store answers with each read.
func readSealedProbe(ctx context.Context, s ObjectStore, key string, lookup blobcrypt.KeyLookup, want blobcrypt.Want, plain []byte) error {
	body, info, err := s.Get(ctx, key, nil)
	if err != nil {
		return err
	}
	got, err := readOpened(body, info, lookup, want, nil)
	if err != nil {
		return fmt.Errorf("a whole read: %w", err)
	}
	if !bytes.Equal(got, plain) {
		return errors.New("a whole read returned other plaintext than was written")
	}
	plan, err := blobcrypt.PlanRange(want.Size, blobcrypt.SegmentSize-6, blobcrypt.SegmentSize+4)
	if err != nil {
		return err
	}
	body, info, err = s.Get(ctx, key, &ByteRange{Offset: plan.Offset, Length: plan.Length})
	if err != nil {
		return err
	}
	got, err = readOpened(body, info, lookup, want, &plan)
	if err != nil {
		return fmt.Errorf("a ranged read: %w", err)
	}
	if !bytes.Equal(got, plain[plan.First:plan.Last+1]) {
		return errors.New("a ranged read returned other plaintext than the window's")
	}
	return nil
}

func readOpened(body io.ReadCloser, info ObjectInfo, lookup blobcrypt.KeyLookup, want blobcrypt.Want, plan *blobcrypt.Range) ([]byte, error) {
	defer body.Close()
	want.ObjectSize = info.Size
	dk, err := blobcrypt.Open(info.Metadata, lookup, want)
	if err != nil {
		return nil, err
	}
	defer dk.Key.Destroy()
	var r *blobcrypt.Reader
	if plan == nil {
		r, err = blobcrypt.NewReader(body, dk, true)
	} else {
		r, err = blobcrypt.NewRangeReader(body, dk, *plan)
	}
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}
