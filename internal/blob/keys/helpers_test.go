package keys_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/blob/keys/keystest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/secrets"
)

// ringOf loads a key ring of the root keys ids, each 32 bytes made from its fill byte, with active as the active one.
func ringOf(t *testing.T, active string, ids map[string]byte) *secrets.KeyRing {
	t.Helper()
	dir := t.TempDir()
	var files []string
	for id, fill := range ids {
		raw := bytes.Repeat([]byte{fill}, 32)
		p := filepath.Join(dir, id+".key")
		if err := os.WriteFile(p, []byte(base64.StdEncoding.EncodeToString(raw)), 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, p)
	}
	r, err := secrets.Load(config.Secrets{EncryptionKeyFiles: files, ActiveKeyID: active})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// clock is a settable clock for the re-read's bound.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// env is one database, one location and one key ring, shared by the holders of several replicas.
type env struct {
	db    *keystest.DB
	store *blobtest.Store
	ring  *secrets.KeyRing
	clock *clock
	logs  *bytes.Buffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	return &env{
		db: keystest.New(), store: blobtest.New(), ring: ringOf(t, "k1", map[string]byte{"k1": 1}),
		clock: &clock{t: time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)}, logs: &bytes.Buffer{},
	}
}

// holder is the holder of one replica.
func (e *env) holder(t *testing.T) *keys.Holder {
	t.Helper()
	h, err := keys.New(keys.Options{
		DB: e.db, Rows: e.db, Ring: e.ring, Now: e.clock.now,
		Log: slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return h
}

// archive is the encrypted connection of the tests, with keyed names.
func (e *env) archive() keys.Connection {
	return keys.Connection{ID: "archive", KeyedNames: true, Prefix: "quader", Objects: e.store}
}

// opened opens a row's sealed value with the ring, for a comparison of keys.
func (e *env) opened(t *testing.T, r keys.Row) []byte {
	t.Helper()
	b, err := e.ring.Open(keys.Column(), secrets.Row(r.KeyID), r.Sealed)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// sealedRow draws a key and seals it for a row, as another replica would have.
func (e *env) sealedRow(t *testing.T, id, kind string, gen int) keys.Row {
	t.Helper()
	k, err := blobcrypt.NewKey(bytes.NewReader(bytes.Repeat([]byte{byte(gen) + 7}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := k.Bytes()
	sealed, err := e.ring.Seal(keys.Column(), secrets.Row(id), raw)
	if err != nil {
		t.Fatal(err)
	}
	return keys.Row{KeyID: id, Connection: "archive", Kind: kind, Generation: gen, Sealed: sealed, State: keys.StateActive, CreatedAt: time.Now()}
}

// record wraps a data key of a blob of 1 KiB under sk and returns the object's metadata and the Want a reader of it holds.
func record(t *testing.T, sk *blobcrypt.StorageKey) (map[string]string, blobcrypt.Want) {
	t.Helper()
	dk, err := blobcrypt.NewKey(bytes.NewReader(bytes.Repeat([]byte{9}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	d := blobcrypt.DataKey{Key: dk, Digest: [32]byte{1, 2, 3}, Size: 1024}
	rec, err := blobcrypt.Wrap(sk, d, bytes.NewReader(bytes.Repeat([]byte{5}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := blobcrypt.StoredSize(1024)
	return rec.Metadata(), blobcrypt.Want{Known: true, Digest: d.Digest, Size: 1024, StoredSize: stored, ObjectSize: stored}
}

var ctx = context.Background()
