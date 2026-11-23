package offline_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/blob/offline"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/secrets"
)

// keyRing writes a key ring of one root key into a new directory and returns the directory and the loaded ring.
func keyRing(t *testing.T, id string) (string, *secrets.KeyRing) {
	t.Helper()
	dir := t.TempDir()
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	file := filepath.Join(dir, id+".key")
	if err := os.WriteFile(file, []byte(base64.StdEncoding.EncodeToString(raw)), 0o600); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.Load(config.Secrets{EncryptionKeyFiles: []string{file}, ActiveKeyID: id})
	if err != nil {
		t.Fatal(err)
	}
	return dir, ring
}

// entry is one key of an export written by a test: a sealed key, or (raw nil) a destroyed one.
type entry struct {
	id  string
	raw []byte
}

// writeExport seals the keys with ring and writes the export of storage.md R-STOR-085 (a destroyed key as R-STOR-103 lists it).
func writeExport(t *testing.T, ring *secrets.KeyRing, entries ...entry) string {
	t.Helper()
	created := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	list := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		id, ok := keys.ParseID(e.id)
		if !ok {
			t.Fatalf("no identifier: %s", e.id)
		}
		k := map[string]any{"keyId": e.id, "connection": id.Connection, "kind": id.Kind, "generation": id.Generation, "state": "active", "createdAt": created}
		if e.raw == nil {
			k["state"], k["retiredAt"], k["destroyedAt"] = "destroyed", created.Add(time.Hour), created.Add(time.Hour)
		} else {
			sealed, err := ring.Seal(keys.Column(), secrets.Row(e.id), e.raw)
			if err != nil {
				t.Fatal(err)
			}
			k["sealed"] = sealed
		}
		list = append(list, k)
	}
	doc := map[string]any{"format": keys.DocumentFormat, "installationId": "0b7c3f5e-0000-4000-8000-000000000001", "exportedAt": created, "rootKeyIds": ring.KeyIDs(), "keys": list}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// randomKey is 32 random bytes.
func randomKey() []byte {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return b
}

// storageKey makes the storage key of id from raw.
func storageKey(t *testing.T, id string, raw []byte) *blobcrypt.StorageKey {
	t.Helper()
	sk, err := blobcrypt.NewStorageKey(id, raw)
	if err != nil {
		t.Fatal(err)
	}
	return sk
}

// putSealed writes plain as an object of the stored format at name, wrapped under sk, as the store's writer writes one in a single
// request (R-STOR-080), and returns its digest.
func putSealed(t *testing.T, s *blobtest.Store, name string, sk *blobcrypt.StorageKey, plain []byte) [32]byte {
	t.Helper()
	dek, err := blobcrypt.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := blobcrypt.NewSeed(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	d := sha256.Sum256(plain)
	sec, err := blobcrypt.NewSection(dek, seed, bytes.NewReader(plain), 0, int64(len(plain)), &blobcrypt.Final{Size: int64(len(plain)), Digest: d})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := blobcrypt.Wrap(sk, blobcrypt.DataKey{Key: dek, Digest: d, Size: int64(len(plain))}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(context.Background(), name, sec, sec.Size(), blob.Checksum{}, rec.Metadata()); err != nil {
		t.Fatal(err)
	}
	return d
}

// keyedName is the keyed name of d below the prefix quader.
func keyedName(t *testing.T, nk blobcrypt.Key, d [32]byte) string {
	t.Helper()
	n, err := blobcrypt.KeyedName(nk, "quader", d)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// collect runs a walk and returns its results by name.
func collect(t *testing.T, walk func(func(offline.Result) error) (offline.Totals, error)) (map[string]offline.Result, offline.Totals) {
	t.Helper()
	out := map[string]offline.Result{}
	tot, err := walk(func(r offline.Result) error { out[r.Name] = r; return nil })
	if err != nil {
		t.Fatal(err)
	}
	return out, tot
}

// content is n bytes of a pattern that starts at seed.
func content(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i) + seed
	}
	return b
}
