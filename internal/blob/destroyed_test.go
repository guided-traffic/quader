package blob_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"testing"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/observability"
)

// destroyedKeys are the replica's keys of archive whose lookup answers destroyed_key for the identifier in destroyed, as
// keys.Holder answers for a row the destruction set destroyed (R-STOR-101).
type destroyedKeys struct {
	heldKeys
	destroyed *string
}

func (k destroyedKeys) Lookup(ctx context.Context, connection string) blobcrypt.KeyLookup {
	held := k.heldKeys.Lookup(ctx, connection)
	return func(id string) (*blobcrypt.StorageKey, error) {
		if connection == k.conn && id == *k.destroyed {
			return nil, fmt.Errorf("%w: the storage key %s of connection %s was destroyed", blobcrypt.ErrDestroyedKey, id, connection)
		}
		return held(id)
	}
}

// destroyedTestKeys are blobtest.Keys whose lookup answers destroyed_key for one identifier.
type destroyedTestKeys struct {
	*blobtest.Keys
	destroyed string
}

func (k destroyedTestKeys) Lookup(ctx context.Context, connection string) blobcrypt.KeyLookup {
	held := k.Keys.Lookup(ctx, connection)
	return func(id string) (*blobcrypt.StorageKey, error) {
		if id == k.destroyed {
			return nil, fmt.Errorf("%w: the storage key %s of connection %s was destroyed", blobcrypt.ErrDestroyedKey, id, connection)
		}
		return held(id)
	}
}

func TestR_STOR_101_TheOrphanSweepLeavesAnObjectWhoseRecordNamesADestroyedKey(t *testing.T) {
	t.Run("R-STOR-101 an object without a copy row under a destroyed key is not placed: reported foreign, neither adopted nor deleted", func(t *testing.T) {
		ctx := context.Background()
		p, keyed := "quader", true
		s, ks := blobtest.New(), blobtest.NewKeys()
		id := ks.Draw("archive", 1)
		c := blob.NewConnection(config.StorageConnection{ID: "archive", Bucket: "a", Prefix: &p, Encrypted: true, KeyedNames: &keyed}, s)
		c.UseKeys(ks)
		data := content(1024, 71)
		cw, err := c.WriteSealedStream(ctx, blob.Digest(sha256.Sum256(data)), int64(len(data)), bytes.NewReader(data), 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		c.UseKeys(destroyedTestKeys{Keys: ks, destroyed: id})
		if _, _, _, ok, err := blob.PlaceByRecord(ctx, c, cw.Key); ok || err != nil {
			t.Fatalf("placed %v, %v: a destroyed key is a record that does not open, not a failure to retry", ok, err)
		}
		if _, ok := s.Data(cw.Key); !ok {
			t.Fatal("the object is gone")
		}
	})
}

func TestR_STOR_101_AReadThatMeetsADestroyedKeyFallsOverAndMarksTheCopy(t *testing.T) {
	t.Run("R-STOR-101 a record of a destroyed key falls over to the second copy, marks the first corrupt and counts destroyed_key", func(t *testing.T) {
		cfg := blobtest.Storage(t, t.TempDir())
		p := "quader"
		cfg.Replication.MinCopies = 2
		cfg.Connections = []config.StorageConnection{
			config.StorageConnection{ID: "archive", Bucket: "a", Prefix: &p, Encrypted: true}.WithDefaults(),
			config.StorageConnection{ID: "plain", Bucket: "b", Prefix: &p}.WithDefaults(),
		}
		conns, err := blob.NewConnections(blob.NewConnection(cfg.Connections[0], blobtest.New()), blob.NewConnection(cfg.Connections[1], blobtest.New()))
		if err != nil {
			t.Fatal(err)
		}
		raw := make([]byte, blobcrypt.KeySize)
		_, _ = rand.Read(raw)
		sk, err := blobcrypt.NewStorageKey("sk-archive-1-0a1b2c3d", raw)
		if err != nil {
			t.Fatal(err)
		}
		nk, _ := blobcrypt.NewKey(rand.Reader)
		none := ""
		k := destroyedKeys{heldKeys: heldKeys{conn: "archive", keys: map[string]*blobcrypt.StorageKey{sk.ID: sk}, active: sk.ID, name: &nk}, destroyed: &none}
		db, metrics := newFakeDB(), observability.NewMetrics("web")
		store, err := blob.New(cfg, conns, db, blob.WithRows(db), blob.WithStorageKeys(k), blob.WithMetrics(blob.NewMetrics(metrics)))
		if err != nil {
			t.Fatal(err)
		}
		data := content(1024, 7)
		sp, err := store.Ingest(context.Background(), bytes.NewReader(data), blob.IngestOptions{ContentLength: int64(len(data))})
		if err != nil {
			t.Fatal(err)
		}
		defer sp.Close()
		if _, err := store.Commit(context.Background(), sp, noLink); err != nil {
			t.Fatal(err)
		}
		*k.destroyed = sk.ID
		r, err := store.Open(context.Background(), sp.SHA256, sp.Size, blob.OpenOptions{})
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("the read did not fall over to the plain copy: %v", err)
		}
		if cp, _ := db.copyOf(sp.SHA256, "archive"); cp.State != blob.CopyCorrupt {
			t.Errorf("the encrypted copy is %s, want corrupt", cp.State)
		}
		labels := map[string]string{"connection": "archive", "reason": string(blobcrypt.ReasonDestroyedKey)}
		if n := counter(t, metrics, "quader_storage_encrypted_read_failures_total", labels); n != 1 || blobcrypt.ReasonDestroyedKey != "destroyed_key" {
			t.Errorf("destroyed_key counted %v times", n)
		}
	})
}
