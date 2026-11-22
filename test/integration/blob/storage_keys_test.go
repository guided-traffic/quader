//go:build integration

package blob

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	storagekeys "github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/listq"
	"github.com/guided-traffic/quader/internal/secrets"
)

// rootRing loads a key ring of the root keys ids, each 32 bytes of its fill byte.
func rootRing(t *testing.T, active string, ids map[string]byte) *secrets.KeyRing {
	t.Helper()
	dir := t.TempDir()
	var files []string
	for id, fill := range ids {
		p := filepath.Join(dir, id+".key")
		if err := os.WriteFile(p, []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32))), 0o600); err != nil {
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

func newHolder(t *testing.T, p *pgEnv, ring *secrets.KeyRing) *storagekeys.Holder {
	t.Helper()
	h, err := storagekeys.New(storagekeys.Options{DB: p.db, Ring: ring, Log: quiet})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return h
}

// sqlState is the SQLSTATE of a statement's error.
func sqlStateOf(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// TestR_STOR_083_TheStorageKeysInPostgreSQL runs what only PostgreSQL shows of the keys (R-STOR-083, R-STOR-084): the migration's
// rules, two replicas that draw at the same moment, the re-encryption of both sealed columns at a rotation of the root, and a new
// generation through the change area. PostgreSQL of the stack, in-memory locations, no object of a blob.
func TestR_STOR_083_TheStorageKeysInPostgreSQL(t *testing.T) {
	p := newPG(t)
	ring := rootRing(t, "k1", map[string]byte{"k1": 1})
	ctx := context.Background()

	t.Run("R-STOR-083 the migration: no foreign key on storage_keys, the identifier names the row, one active key per connection and kind, the session columns", func(t *testing.T) {
		if n := p.scalarInt(t, `SELECT count(*) FROM pg_constraint WHERE conrelid = 'quader.storage_keys'::regclass AND contype = 'f'`); n != 0 {
			t.Errorf("storage_keys has %d foreign keys, want none", n)
		}
		insert := func(id, conn, kind string, gen int, state string) error {
			_, err := p.admin.Exec(ctx, `INSERT INTO storage_keys (key_id, connection_id, kind, generation, sealed_key, state) VALUES ($1, $2, $3, $4, 'v1:k1:AAAA', $5)`,
				id, conn, kind, gen, state)
			return err
		}
		if err := insert("sk-migr-1-0000aaaa", "migr", "storage", 1, "active"); err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct {
			id, conn, kind string
			gen            int
			want           string
		}{
			{"sk-other-1-0000aaab", "migr", "storage", 1, "23514"}, // the identifier names another connection
			{"sk-migr-2-0000aaac", "migr", "storage", 3, "23514"},  // another generation
			{"nk-migr-0000aaad", "migr", "storage", 1, "23514"},    // another kind
			{"nk-migr-0000aaae", "migr", "name", 2, "23514"},       // a name key has generation 1
			{"sk-migr-2-0000aaaf", "migr", "storage", 2, "23505"},  // a second active storage key
			{"sk-migr-1-0000aab0", "migr", "storage", 1, "23505"},  // generation 1 twice
		} {
			if got := sqlStateOf(insert(c.id, c.conn, c.kind, c.gen, "active")); got != c.want {
				t.Errorf("%s (%s %d): SQLSTATE %q, want %q", c.id, c.kind, c.gen, got, c.want)
			}
		}
		for _, col := range []string{
			"storage_connections.keyed_names", "upload_sessions.sealed_data_key", "upload_sessions.sealed_segments",
			"upload_session_parts.stored_size", "upload_session_parts.stored_sha256",
		} {
			tbl, name, _ := strings.Cut(col, ".")
			if p.scalarInt(t, `SELECT count(*) FROM information_schema.columns WHERE table_schema = 'quader' AND table_name = $1 AND column_name = $2`, tbl, name) != 1 {
				t.Errorf("no column %s", col)
			}
		}
		p.exec(t, `DELETE FROM storage_keys WHERE connection_id = 'migr'`)
	})

	t.Run("R-STOR-083 two replicas that start at the same moment draw one storage key and one name key", func(t *testing.T) {
		store := blobtest.New()
		var arrived sync.WaitGroup
		arrived.Add(2)
		released := make(chan struct{})
		var once sync.Once
		go func() { arrived.Wait(); once.Do(func() { close(released) }) }()
		store.SetFault(func(op, key string) error {
			if op == "list" && strings.HasSuffix(key, "/sha256/") {
				arrived.Done() // both replicas have read no row and list the location before either inserts
				select {
				case <-released:
				case <-time.After(20 * time.Second):
					return errors.New("the other replica never listed")
				}
			}
			return nil
		})
		holders := []*storagekeys.Holder{newHolder(t, p, ring), newHolder(t, p, ring)}
		errs := make([]error, len(holders))
		var wg sync.WaitGroup
		for i, h := range holders {
			wg.Go(func() {
				errs[i] = h.Ensure(ctx, storagekeys.Connection{ID: "race", KeyedNames: true, Prefix: "quader", Objects: store})
			})
		}
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			t.Fatal(err)
		}
		if n := p.count(t, "storage_keys WHERE connection_id = 'race'"); n != 2 {
			t.Fatalf("%d rows after two replicas drew at once, want a storage key and a name key", n)
		}
		s1, a1, n1 := holders[0].Held("race")
		s2, a2, n2 := holders[1].Held("race")
		if len(s1) != 1 || len(s2) != 1 || a1 != a2 || n1 != n2 || a1 == "" || n1 == "" {
			t.Fatalf("the replicas hold %v %s %s and %v %s %s", s1, a1, n1, s2, a2, n2)
		}
		k1, _ := holders[0].Active(ctx, "race")
		k2, _ := holders[1].Active(ctx, "race")
		if !k1.Key.Equal(k2.Key) {
			t.Fatal("two keys under one identifier")
		}
	})

	t.Run("R-STOR-084 a rotation of the root reseals every storage key and an open session's data key, the keys inside unchanged", func(t *testing.T) {
		h := newHolder(t, p, ring)
		if err := h.Ensure(ctx, storagekeys.Connection{ID: "rotate", KeyedNames: true, Prefix: "rotate", Objects: blobtest.New()}); err != nil {
			t.Fatal(err)
		}
		if _, err := storagekeys.NewGeneration(ctx, p.db, nil, ring, nil, "rotate", nil); err != nil {
			t.Fatal(err)
		}
		h.FlushArea(cluster.AreaStorageKeys, "test")
		a, err := h.Active(ctx, "rotate") // generation 2, opened under the old root key
		if err != nil {
			t.Fatal(err)
		}
		dk, _ := blobcrypt.NewKey(bytes.NewReader(bytes.Repeat([]byte{4}, 32)))
		const session = "0b7c3f5e-0000-4000-8000-0000000000aa"
		sealed, err := storagekeys.SealSessionKey(ring, session, dk)
		if err != nil {
			t.Fatal(err)
		}
		p.exec(t, `INSERT INTO storage_connections (id, bucket, prefix, encrypted) VALUES ('rotate', 'b', 'rotate', true)`)
		p.exec(t, `INSERT INTO upload_sessions (id, repo_id, addressed_repo_id, hash_state, part_size, s3_upload_id, state, connection_id, sealed_data_key)
			VALUES ($1, $2, $2, '\x00', 65536, 'u', 'open', 'rotate', $3)`, session, p.repo, sealed)
		values := p.count(t, "storage_keys") + 1

		rotated := rootRing(t, "k2", map[string]byte{"k1": 1, "k2": 2})
		res, err := storagekeys.Reseal(ctx, p.db, nil, rotated, 2) // pages of two
		if err != nil || res != (storagekeys.ResealResult{Resealed: values}) {
			t.Fatalf("walk %+v %v, want %d values resealed", res, err, values)
		}
		if n := p.count(t, "storage_keys WHERE split_part(sealed_key, ':', 2) <> 'k2'"); n != 0 {
			t.Errorf("%d rows of storage_keys not under the new root key", n)
		}
		if n := p.count(t, "upload_sessions WHERE split_part(sealed_data_key, ':', 2) <> 'k2'"); n != 0 {
			t.Error("the session's data key is not under the new root key")
		}
		st, err := rotated.StatusWithCounts(ctx, listq.SealedCounter{DB: p.db.Main()})
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range st.Keys {
			if want := map[string]int64{"k1": 0, "k2": values}[k.KeyID]; k.SealedValues == nil || *k.SealedValues != want {
				t.Errorf("the key ring's status counts %v values under %s, want %d", k.SealedValues, k.KeyID, want)
			}
		}
		only := newHolder(t, p, rootRing(t, "k2", map[string]byte{"k2": 2}))
		if err := only.Load(ctx, "rotate"); err != nil {
			t.Fatalf("a replica with only the new root key: %v", err)
		}
		b, _ := only.Active(ctx, "rotate")
		if b == nil || a.ID != b.ID || !a.Key.Equal(b.Key) {
			t.Fatalf("the active key differs after the rotation: %v %v", a, b)
		}
		var now string
		if err := p.admin.QueryRow(ctx, `SELECT sealed_data_key FROM upload_sessions WHERE id = $1`, session).Scan(&now); err != nil {
			t.Fatal(err)
		}
		if got, err := storagekeys.OpenSessionKey(rotated, session, now); err != nil || !got.Equal(dk) {
			t.Fatalf("the session's data key after the rotation: %v", err)
		}
		again, err := storagekeys.Reseal(ctx, p.db, nil, rotated, 0)
		if err != nil || again != (storagekeys.ResealResult{Current: values}) {
			t.Errorf("a second walk %+v %v", again, err)
		}
	})

	t.Run("R-STOR-083 a new generation reaches a running replica through the change area storage_keys", func(t *testing.T) {
		clu := config.Cluster{
			LeaseRetryInterval: 500 * time.Millisecond, EpochPollInterval: time.Second, EpochMaxStaleness: 20 * time.Second,
			ChangeRetention: time.Hour, SignalCoalesceWindow: 20 * time.Millisecond,
		}
		writer, err := cluster.NewChanges(cluster.ChangesOptions{DB: p.db.ControlPool(), Config: clu, WatchdogTimeout: 30 * time.Second, Log: quiet})
		if err != nil {
			t.Fatal(err)
		}
		reader, err := cluster.NewChanges(cluster.ChangesOptions{DB: p.db.ControlPool(), Config: clu, WatchdogTimeout: 30 * time.Second, Log: quiet})
		if err != nil {
			t.Fatal(err)
		}
		h := newHolder(t, p, ring)
		if err := h.Ensure(ctx, storagekeys.Connection{ID: "notify", Prefix: "notify", Objects: blobtest.New()}); err != nil {
			t.Fatal(err)
		}
		reader.AddListener(h)
		if err := reader.Start(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = reader.Stop(context.Background()) })
		before, _ := h.Active(ctx, "notify")
		g, err := storagekeys.NewGeneration(ctx, p.db, nil, ring, nil, "notify", func(_ context.Context, tx meta.Tx, g storagekeys.Generation) error {
			return writer.Notify(tx, cluster.AreaStorageKeys, 1, map[string]string{"connection": g.Connection})
		})
		if err != nil {
			t.Fatal(err)
		}
		if k, _ := h.Active(ctx, "notify"); k.ID != before.ID {
			t.Fatalf("the replica wraps under %s before the change reached it", k.ID)
		}
		if err := reader.PollNow(ctx); err != nil {
			t.Fatal(err)
		}
		if k, _ := h.Active(ctx, "notify"); k.ID != g.KeyID {
			t.Fatalf("after the poll the replica wraps under %s, want %s", k.ID, g.KeyID)
		}
		if n := p.count(t, `cluster_change WHERE area = 'storage_keys' AND payload::text LIKE '%"notify"%'`); n != 1 {
			t.Errorf("%d changes of the area name the connection, want 1", n)
		}
	})
}
