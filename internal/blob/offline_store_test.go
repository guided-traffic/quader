package blob_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/blob/offline"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/secrets"
)

// The offline tool reads what the store's writer wrote (storage.md R-STOR-089): the store is the first reader of the stored format,
// the offline tool the second, and this test holds the two to one layout — the keyed names, the records and the segments of a
// multipart write whose parts end at segment boundaries.

// offlineKeys writes a key ring and the export of the fixture's storage key and name key, as quader admin storage keys export
// prints them, and loads them for the offline reader.
func offlineKeys(t *testing.T, f *sealedFixture) *offline.Keys {
	t.Helper()
	dir := t.TempDir()
	root := make([]byte, 32)
	_, _ = rand.Read(root)
	file := filepath.Join(dir, "2026-12.key")
	if err := os.WriteFile(file, []byte(base64.StdEncoding.EncodeToString(root)), 0o600); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.Load(config.Secrets{EncryptionKeyFiles: []string{file}, ActiveKeyID: "2026-12"})
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	doc := keys.Document{Format: keys.DocumentFormat, InstallationID: "0b7c3f5e-0000-4000-8000-000000000001", ExportedAt: created, RootKeyIDs: []string{"2026-12"}}
	for _, k := range []struct {
		id  string
		key blobcrypt.Key
	}{{f.sk.ID, f.sk.Key}, {"nk-archive-0a1b2c3d", f.nk}} {
		raw, err := k.key.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := ring.Seal(keys.Column(), secrets.Row(k.id), raw)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := keys.ParseID(k.id)
		doc.Keys = append(doc.Keys, keys.DocumentKey{KeyID: k.id, Connection: id.Connection, Kind: id.Kind, Generation: id.Generation, State: keys.StateActive, CreatedAt: created, Sealed: sealed})
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	export := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(export, b, 0o600); err != nil {
		t.Fatal(err)
	}
	ks, err := offline.LoadKeys(export, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ks.Close)
	return ks
}

func TestR_STOR_089_TheOfflineToolReadsWhatTheStoreWrote(t *testing.T) {
	ctx := context.Background()
	f := newSealedFixture(t, func(c *config.Storage) {
		c.ObjectStore.Multipart.Threshold = 64 << 10
		c.ObjectStore.Multipart.PartSize = 64 << 10
	}, nil)
	small, large := content(1024, 1), content(2*blobcrypt.SegmentSize+1, 2)
	spSmall, spLarge := f.commit(t, small), f.commit(t, large)
	if n := f.archive.Count("upload_part"); n != 3 {
		t.Fatalf("the large blob went in %d parts, not in the three of 64 KiB that the boundaries of this test need", n)
	}
	ks := offlineKeys(t, f)
	r := offline.NewReader(offline.Bucket(f.archive, offline.Info{Connection: "archive", Prefix: "quader", Encrypted: true, KeyedNames: true}), ks)
	got := map[string]offline.Result{}
	verifyAll := func() offline.Totals {
		clear(got)
		tot, err := r.Verify(ctx, 2, func(res offline.Result) error { got[res.Name] = res; return nil })
		if err != nil {
			t.Fatal(err)
		}
		return tot
	}

	nSmall, nLarge := f.name(t, spSmall.SHA256), f.name(t, spLarge.SHA256)
	if tot := verifyAll(); tot.OK != 2 || tot.Objects != 2 || got[nSmall].SHA256 != spSmall.SHA256.Hex() || got[nLarge].SHA256 != spLarge.SHA256.Hex() ||
		*got[nLarge].Size != int64(len(large)) || got[nLarge].KeyID != f.sk.ID {
		t.Fatalf("verify of the store's objects = %+v, totals %+v", got, tot)
	}
	out := filepath.Join(t.TempDir(), "large.bin")
	if res, err := r.Get(ctx, nLarge, out); err != nil || !res.Sound() {
		t.Fatalf("get = %+v, %v", res, err)
	}
	if b, err := os.ReadFile(out); err != nil || !bytes.Equal(b, large) {
		t.Fatalf("get wrote %d bytes, %v", len(b), err)
	}

	// The first byte of the second data segment, which the second part of the multipart write carried, changed behind the store's back.
	info, err := f.archive.Head(ctx, nLarge)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := f.archive.Data(nLarge)
	stored[blobcrypt.FullSegmentSize+blobcrypt.NonceSize] ^= 1
	if err := f.archive.Put(ctx, nLarge, bytes.NewReader(stored), int64(len(stored)), blob.Checksum{}, info.Metadata); err != nil {
		t.Fatal(err)
	}
	if tot := verifyAll(); got[nLarge].Status != string(blobcrypt.ReasonSegment) || got[nSmall].Status != offline.StatusOK || tot.Findings != 1 {
		t.Errorf("after the change: %+v", got)
	}
}
