//go:build integration

package blob

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	storagekeys "github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/blob/summary"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/secrets"
)

// TestR_STOR_084_ARotationOfTheRootTouchesNoObject is R-STOR-084's case: an encrypted connection holding three blobs, PostgreSQL of the
// stack, an in-memory store and two root keys. The rotation reseals the rows of storage_keys and nothing else: after the walk of the
// task every row lies under the new root key, the objects keep their entity tags, the blobs download, and a replica whose key ring holds
// only the new root key reads them.
func TestR_STOR_084_ARotationOfTheRootTouchesNoObject(t *testing.T) {
	p := newPG(t)
	ctx := context.Background()
	mem := blobtest.New()
	prefix := "vault"
	cfg := blobtest.Storage(t, t.TempDir())
	cfg.Upload.BlobWaitTimeout = 10 * time.Second
	cfg.Connections = []config.StorageConnection{config.StorageConnection{ID: "vault", Bucket: "vault", Prefix: &prefix, Encrypted: true}.WithDefaults()}
	// replica is a replica with the key ring ring: its holder of the storage keys, its store, the connection recorded and its keys held
	replica := func(ring *secrets.KeyRing) *blob.Store {
		t.Helper()
		h := newHolder(t, p, ring)
		conns, err := blob.NewConnections(blob.NewConnection(cfg.Connections[0], mem))
		if err != nil {
			t.Fatal(err)
		}
		s, err := blob.New(cfg, conns, p.db, blob.WithLogger(quiet), blob.WithWaitPoll(25*time.Millisecond), blob.WithStorageKeys(h))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.RecordConnections(ctx); err != nil {
			t.Fatal(err)
		}
		c := conns.First()
		if err := h.Ensure(ctx, storagekeys.Connection{ID: c.ID(), KeyedNames: c.KeyedNames(), Prefix: c.Prefix(), Objects: c.Objects()}); err != nil {
			t.Fatal(err)
		}
		return s
	}
	first := replica(rootRing(t, "k1", map[string]byte{"k1": 1}))
	blobs := map[blob.Digest][]byte{}
	etags := map[string]string{}
	for i := range 3 {
		data := memData(1024, byte(i))
		d := commit(t, first, data)
		blobs[d] = data
	}
	for _, key := range mem.Keys(prefix + "/") {
		if strings.Contains(key, "/keyed/") {
			info, err := mem.Head(ctx, key)
			if err != nil {
				t.Fatal(err)
			}
			etags[key] = info.ETag
		}
	}
	if len(etags) != 3 || len(mem.Keys(prefix+"/sha256/")) != 0 {
		t.Fatalf("objects below keyed/: %d, below sha256/: %d; want three and none", len(etags), len(mem.Keys(prefix+"/sha256/")))
	}
	if n := p.count(t, "blob_copies WHERE connection_id = 'vault' AND storage_key_id LIKE 'sk-vault-1-%' AND stored_size = 1120"); n != 3 {
		t.Fatalf("%d copy rows name the storage key with the stored size 1,120, want 3", n)
	}
	if n := p.count(t, "storage_connections WHERE id = 'vault' AND encrypted AND keyed_names"); n != 1 {
		t.Fatal("the connection's row does not record encrypted and keyed_names")
	}

	t.Run("R-STOR-088 R-STOR-061 the connections' list shows keyedNames, the storage key with its three copies, and the stored bytes", func(t *testing.T) {
		s, err := summary.Compute(ctx, p.db, summary.Options{})
		if err == nil {
			err = summary.Save(ctx, p.db, s)
		}
		if err != nil {
			t.Fatal(err)
		}
		views, err := summary.List(ctx, p.db, 5*time.Minute, nil)
		if err != nil {
			t.Fatal(err)
		}
		var v *summary.View
		for i := range views {
			if views[i].ID == "vault" {
				v = &views[i]
			}
		}
		if v == nil || !v.Encrypted || !v.KeyedNames || v.Figures == nil {
			t.Fatalf("the connection in the list: %+v", v)
		}
		if v.Figures.PresentCopies != 3 || v.Figures.PresentBytes != 3*1120 {
			t.Errorf("present %d copies of %d stored bytes, want 3 of 3,360 (S(1 KiB) each)", v.Figures.PresentCopies, v.Figures.PresentBytes)
		}
		var storageKey string
		for _, k := range v.Keys {
			if strings.HasPrefix(k.ID, "sk-vault-1-") && k.State == "active" {
				storageKey = k.ID
			}
		}
		if storageKey == "" || v.Figures.KeyCopies[storageKey] != 3 {
			t.Errorf("the storage keys %+v with the copies %v, want the active generation 1 naming the three copies", v.Keys, v.Figures.KeyCopies)
		}
	})

	// steps 1 and 2: the new key joins the ring, then becomes the active one; step 3: the walk of secrets-reencrypt over the sealed
	// columns of the storage keys (the code path of the task, resealStorageKeys)
	rotated := rootRing(t, "k2", map[string]byte{"k1": 1, "k2": 2})
	writes := func() int {
		return mem.Count("put") + mem.Count("copy") + mem.Count("delete") + mem.Count("create_multipart") + mem.Count("get")
	}
	before := writes()
	rows := p.count(t, "storage_keys WHERE connection_id = 'vault'")
	if res, err := storagekeys.Reseal(ctx, p.db, nil, rotated, 0); err != nil || res.Resealed != rows {
		t.Fatalf("the walk resealed %+v (%v), want the %d rows of the connection", res, err, rows)
	}
	t.Run("R-STOR-084 after the walk every row of storage_keys is sealed under the new root key and every object keeps its entity tag", func(t *testing.T) {
		if n := p.count(t, "storage_keys WHERE split_part(sealed_key, ':', 2) <> 'k2'"); n != 0 {
			t.Errorf("%d rows of storage_keys not under the new root key", n)
		}
		for key, tag := range etags {
			info, err := mem.Head(ctx, key)
			if err != nil || info.ETag != tag {
				t.Errorf("%s: entity tag %q (%v), was %q", key, info.ETag, err, tag)
			}
		}
		if n := writes(); n != before {
			t.Errorf("the walk sent %d requests to the store, want none", n-before)
		}
	})
	t.Run("R-STOR-084 the blobs download, and a replica with only the new root key reads them", func(t *testing.T) {
		for _, s := range []*blob.Store{first, replica(rootRing(t, "k2", map[string]byte{"k2": 2}))} {
			for d, data := range blobs {
				r, err := s.Open(ctx, d, int64(len(data)), blob.OpenOptions{})
				if err != nil {
					t.Fatal(err)
				}
				got, err := io.ReadAll(r)
				_ = r.Close()
				if err != nil || !bytes.Equal(got, data) {
					t.Fatalf("%s reads back other bytes", d.Hex())
				}
			}
		}
	})
}
