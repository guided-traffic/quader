package scrub

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
)

// The scrub over encrypted copies (storage.md R-STOR-087) at the unit tier: an encrypted connection and a plain one over in-memory
// stores, the fake ledger, the replica's keys of blobtest.Keys, blobs of a kibibyte.

func sealedConnection(id string, s *blobtest.Store, ks *blobtest.Keys) *blob.Connection {
	p := "quader"
	c := blob.NewConnection(config.StorageConnection{ID: id, Bucket: "bucket-" + id, Prefix: &p, Encrypted: true, ScrubPeriod: 720 * time.Hour}.WithDefaults(), s)
	c.UseKeys(ks)
	return c
}

// putSealed writes b onto the encrypted connection c through its writer and records its present copy and its due entry.
func putSealed(t *testing.T, l *fakeLedger, c *blob.Connection, b []byte) (blob.Digest, blob.CopyWrite) {
	t.Helper()
	d := blob.Digest(sha256.Sum256(b))
	cw, err := c.WriteSealedStream(context.Background(), d, int64(len(b)), bytes.NewReader(b), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	l.copies[d] = append(l.copies[d], CopyRow{Copy: blob.Copy{Connection: c.ID(), ObjectKey: cw.Key, StoredSize: cw.StoredSize, StorageKeyID: cw.StorageKeyID, State: blob.CopyPresent}})
	l.due[c.ID()] = append(l.due[c.ID()], Due{Digest: d, Key: cw.Key, StoredSize: cw.StoredSize, CreatedUs: 1})
	return d, cw
}

// putPlain stores b on a plain connection's store and records its present copy (not due).
func putPlain(l *fakeLedger, id string, s *blobtest.Store, b []byte) {
	d := blob.Digest(sha256.Sum256(b))
	s.Set(blob.Key("quader", d), b, time.Now())
	l.copies[d] = append(l.copies[d], CopyRow{Copy: blob.Copy{Connection: id, ObjectKey: blob.Key("quader", d), StoredSize: int64(len(b)), State: blob.CopyPresent}})
}

func readSealed(t *testing.T, c *blob.Connection, d blob.Digest, size int64, cp blob.Copy) []byte {
	t.Helper()
	body, _, err := c.ReadBlob(context.Background(), d, size, cp, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// forge is an object of the stored format sealed under the active key of archive and bound to d whose plaintext is other.
func forge(ks *blobtest.Keys, d blob.Digest, other []byte) ([]byte, blob.Metadata) {
	sk, err := ks.Active(context.Background(), "archive")
	if err != nil {
		panic(err)
	}
	dk, err := blobcrypt.NewKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	seed, err := blobcrypt.NewSeed(rand.Reader)
	if err != nil {
		panic(err)
	}
	sec, err := blobcrypt.NewSection(dk, seed, bytes.NewReader(other), 0, int64(len(other)), &blobcrypt.Final{Size: int64(len(other)), Digest: d})
	if err != nil {
		panic(err)
	}
	body, err := io.ReadAll(sec)
	if err != nil {
		panic(err)
	}
	rec, err := blobcrypt.Wrap(sk, blobcrypt.DataKey{Key: dk, Digest: d, Size: int64(len(other))}, rand.Reader)
	if err != nil {
		panic(err)
	}
	return body, rec.Metadata()
}

func TestR_STOR_087_TheScrubOverEncryptedCopies(t *testing.T) {
	ctx := context.Background()
	t.Run("R-STOR-087 a whole encrypted copy is read through the connection's reader, every segment and the digest, and stamped verified", func(t *testing.T) {
		l, store, ks := newLedger(), blobtest.New(), blobtest.NewKeys()
		ks.Draw("archive", 1)
		d, _ := putSealed(t, l, sealedConnection("archive", store, ks), data(1024, 41))
		res, err := newScrubber(t, l, newClock(), sealedConnection("archive", store, ks)).Run(ctx, RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if r := result(t, res, "archive"); r.Read != 1 || r.Verified != 1 || r.Corrupt != 0 {
			t.Fatalf("archive = %+v", r)
		}
		if verified, _, _ := l.recorded(); !slices.Equal(verified, []string{"archive/" + d.Hex()[:6]}) {
			t.Fatalf("verified = %v", verified)
		}
	})

	// every reason of R-STOR-082 that marks, each made by a change behind Quader's back, is a corrupt copy repaired from the plain
	// connection with a new data key under the same name
	mutations := []struct {
		reason string
		change func(body []byte, md blob.Metadata, ks *blobtest.Keys, d blob.Digest) ([]byte, blob.Metadata)
	}{
		{"segment", func(b []byte, md blob.Metadata, _ *blobtest.Keys, _ blob.Digest) ([]byte, blob.Metadata) {
			b[100] ^= 1
			return b, md
		}},
		{"final", func(b []byte, md blob.Metadata, _ *blobtest.Keys, _ blob.Digest) ([]byte, blob.Metadata) {
			b[len(b)-5] ^= 1
			return b, md
		}},
		{"size", func(b []byte, md blob.Metadata, _ *blobtest.Keys, _ blob.Digest) ([]byte, blob.Metadata) {
			return b[:len(b)-1], md
		}},
		{"format", func(b []byte, md blob.Metadata, _ *blobtest.Keys, _ blob.Digest) ([]byte, blob.Metadata) {
			delete(md, blobcrypt.MetaFormat)
			return b, md
		}},
		{"record", func(b []byte, md blob.Metadata, _ *blobtest.Keys, _ blob.Digest) ([]byte, blob.Metadata) {
			md[blobcrypt.MetaWrap] = flip(md[blobcrypt.MetaWrap])
			return b, md
		}},
		{"commitment", func(b []byte, md blob.Metadata, _ *blobtest.Keys, _ blob.Digest) ([]byte, blob.Metadata) {
			md[blobcrypt.MetaCommitment] = flip(md[blobcrypt.MetaCommitment])
			return b, md
		}},
		// a holder of the storage key seals other bytes of the blob's size under a data key of its own, bound to the blob's digest: every
		// tag passes and only the plaintext's SHA-256 at the end refuses it (H-1301)
		{"digest", func(_ []byte, _ blob.Metadata, ks *blobtest.Keys, d blob.Digest) ([]byte, blob.Metadata) {
			return forge(ks, d, data(1024, 43))
		}},
	}
	for _, mu := range mutations {
		t.Run("R-STOR-087 R-STOR-082 a copy refused for "+mu.reason+" is marked corrupt and replaced from the plain connection under a new data key", func(t *testing.T) {
			l, store, plain, ks := newLedger(), blobtest.New(), blobtest.New(), blobtest.NewKeys()
			ks.Draw("archive", 1)
			archive := sealedConnection("archive", store, ks)
			good := data(1024, 42)
			d, cw := putSealed(t, l, archive, good)
			putPlain(l, "plain", plain, good)
			before, _ := store.Data(cw.Key)
			body, md := mu.change(bytes.Clone(before), store.MetadataOf(cw.Key), ks, d)
			store.Set(cw.Key, body, time.Now())
			store.SetMetadata(cw.Key, md)
			newKey := ks.Draw("archive", 2) // the repair wraps under the active storage key of its time
			res, err := newScrubber(t, l, newClock(), archive, connection("plain", plain, 50<<20)).Run(ctx, RunOptions{Connection: "archive"})
			if err != nil {
				t.Fatal(err)
			}
			if r := result(t, res, "archive"); r.Corrupt != 1 || r.Repaired != 1 {
				t.Fatalf("archive = %+v", r)
			}
			_, damaged, whole := l.recorded()
			if !slices.Equal(damaged, []string{"archive/" + d.Hex()[:6] + "/corrupt"}) || !slices.Equal(whole, []string{"archive/" + d.Hex()[:6]}) {
				t.Fatalf("damaged = %v, whole = %v", damaged, whole)
			}
			after, _ := store.Data(cw.Key)
			row := l.copies[d][0]
			if bytes.Equal(after, before) || row.StorageKeyID != newKey || row.State != blob.CopyPresent {
				t.Fatalf("the repair kept the old bytes or the row names %q in state %s", row.StorageKeyID, row.State)
			}
			if got := readSealed(t, archive, d, 1024, row.Copy); !bytes.Equal(got, good) {
				t.Fatal("the repaired copy does not read back")
			}
		})
	}

	t.Run("R-STOR-087 Q-STOR-311 a copy whose record names a storage key the replica does not hold is not marked, counted, and stays due", func(t *testing.T) {
		l, store, ks := newLedger(), blobtest.New(), blobtest.NewKeys()
		lost := ks.Draw("archive", 1)
		archive := sealedConnection("archive", store, ks)
		putSealed(t, l, archive, data(1024, 43))
		ks.Forget("archive", lost)
		res, err := newScrubber(t, l, newClock(), archive).Run(ctx, RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if r := result(t, res, "archive"); r.UnknownKey != 1 || r.Corrupt != 0 || r.Verified != 0 || r.Repaired != 0 {
			t.Fatalf("archive = %+v", r)
		}
		if verified, damaged, whole := l.recorded(); len(verified)+len(damaged)+len(whole) != 0 {
			t.Fatalf("verified %v, damaged %v, whole %v: an unknown key changed a state", verified, damaged, whole)
		}
	})

	t.Run("R-STOR-087 a plain copy is repaired from an encrypted one, read through its reader and written as plain bytes", func(t *testing.T) {
		l, store, plain, ks := newLedger(), blobtest.New(), blobtest.New(), blobtest.NewKeys()
		ks.Draw("archive", 1)
		archive := sealedConnection("archive", store, ks)
		good := data(1024, 44)
		d, _ := putSealed(t, l, archive, good)
		putPlain(l, "plain", plain, good)
		l.due["plain"] = []Due{{Digest: d, Key: blob.Key("quader", d), StoredSize: 1024, CreatedUs: 1}}
		plain.Set(blob.Key("quader", d), data(1024, 45), time.Now())
		res, err := newScrubber(t, l, newClock(), archive, connection("plain", plain, 50<<20)).Run(ctx, RunOptions{Connection: "plain"})
		if err != nil {
			t.Fatal(err)
		}
		if r := result(t, res, "plain"); r.Corrupt != 1 || r.Repaired != 1 {
			t.Fatalf("plain = %+v", r)
		}
		if got, _ := plain.Data(blob.Key("quader", d)); !bytes.Equal(got, good) {
			t.Fatal("the plain copy was not replaced with the blob's bytes")
		}
	})
}

// flip changes the first character of a base64url value to another character of the alphabet.
func flip(v string) string {
	b := []byte(v)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	return string(b)
}
