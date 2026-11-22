//go:build integration

package blob

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
)

// memStore is a store over in-memory connections and the database of p, its connections recorded (R-STOR-063: PostgreSQL of
// the stack with blobtest stores in process, objects of 1 KiB).
func memStore(tb testing.TB, p *pgEnv, mutate func(*config.Storage), stores ...*blobtest.Store) (*blob.Store, error) {
	tb.Helper()
	cfg := blobtest.Storage(tb, tb.TempDir())
	cfg.Upload.BlobWaitTimeout = 10 * time.Second
	ids := []string{"primary", "secondary", "archive"}
	cfg.Connections = nil
	for i := range stores {
		pre := "quader"
		cfg.Connections = append(cfg.Connections, config.StorageConnection{ID: ids[i], Bucket: "bucket-" + ids[i], Prefix: &pre}.WithDefaults())
	}
	if mutate != nil {
		mutate(&cfg)
	}
	var list []*blob.Connection
	for i, s := range stores {
		list = append(list, blob.NewConnection(cfg.Connections[i], s))
	}
	conns, err := blob.NewConnections(list...)
	if err != nil {
		tb.Fatal(err)
	}
	s, err := blob.New(cfg, conns, p.db, blob.WithLogger(quiet), blob.WithWaitPoll(25*time.Millisecond))
	if err != nil {
		tb.Fatal(err)
	}
	return s, s.RecordConnections(context.Background())
}

func commit(tb testing.TB, s *blob.Store, data []byte) blob.Digest {
	tb.Helper()
	sp, err := s.Ingest(context.Background(), bytes.NewReader(data), blob.IngestOptions{ContentLength: int64(len(data))})
	if err != nil {
		tb.Fatal(err)
	}
	defer sp.Close()
	if _, err := s.Commit(context.Background(), sp, nil); err != nil {
		tb.Fatal(err)
	}
	return sp.SHA256
}

func memData(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*13) + seed
	}
	return b
}

func TestR_STOR_051_AConnectionsLifeAndItsFixedProperties(t *testing.T) {
	ctx := context.Background()
	t.Run("R-STOR-051 a connection that holds copies refuses another prefix naming the count; one that holds none takes the new prefix", func(t *testing.T) {
		p := newPG(t)
		a, b := blobtest.New(), blobtest.New()
		s, err := memStore(t, p, nil, a, b)
		if err != nil {
			t.Fatal(err)
		}
		commit(t, s, memData(1024, 1))
		moved := func(c *config.Storage) {
			other := "elsewhere"
			c.Connections[0].Prefix = &other
			c.Connections[1].Prefix = &other
		}
		_, err = memStore(t, p, moved, a, b)
		var refused *blob.Refusal
		if !errors.As(err, &refused) || !strings.Contains(err.Error(), "connection primary holds 1 copies in bucket-primary/quader") ||
			!strings.Contains(err.Error(), "add a connection with another id and set drain: true on this one") {
			t.Fatalf("got %v", err)
		}
		// the second connection holds no copy (min_copies 1, nothing copied yet): it takes the element's values
		onlySecond := func(c *config.Storage) {
			other := "elsewhere"
			c.Connections[0].ID, c.Connections[0].Bucket, c.Connections[0].Prefix = "secondary", "bucket-secondary", &other
		}
		_, err = memStore(t, p, onlySecond, b)
		if err != nil {
			t.Fatalf("a connection without copies refused a new prefix: %v", err)
		}
		var prefix string
		if err := p.admin.QueryRow(ctx, `SELECT prefix FROM storage_connections WHERE id = 'secondary'`).Scan(&prefix); err != nil || prefix != "elsewhere" {
			t.Fatalf("the row of secondary has the prefix %q (%v)", prefix, err)
		}
	})
	t.Run("R-STOR-051 a connection removed while it holds the only copy is detached: its blob answers content_unavailable, and the element put back serves it again", func(t *testing.T) {
		p := newPG(t)
		a, b := blobtest.New(), blobtest.New()
		s, err := memStore(t, p, func(c *config.Storage) { c.Connections[0].ID = "archive" }, a, b)
		if err != nil {
			t.Fatal(err)
		}
		data := memData(1024, 2)
		d := commit(t, s, data)
		without, err := memStore(t, p, func(c *config.Storage) { c.Connections[0].ID = "secondary" }, b)
		if err != nil {
			t.Fatalf("a replica without the connection that holds the only copy did not start: %v", err)
		}
		if _, err := without.Open(ctx, d, int64(len(data)), blob.OpenOptions{}); !errors.Is(err, blob.ErrNotFound) {
			t.Fatalf("the blob only archive holds: %v, want content_unavailable", err)
		}
		var status string
		if err := p.admin.QueryRow(ctx, `SELECT content_status FROM blobs WHERE sha256 = $1`, d.Bytes()).Scan(&status); err != nil || status != "ok" {
			t.Fatalf("the blob is %q: a detached copy counts and no read marks it (%v)", status, err)
		}
		back, err := memStore(t, p, func(c *config.Storage) { c.Connections[0].ID = "archive" }, a, b)
		if err != nil {
			t.Fatal(err)
		}
		r, err := back.Open(ctx, d, int64(len(data)), blob.OpenOptions{})
		if err != nil {
			t.Fatalf("the element put back: %v", err)
		}
		_ = r.Close()
	})
	t.Run("R-STOR-051 forget-connection is refused while a live replica configures the connection, then makes its only blob missing, retires the id and deletes no object", func(t *testing.T) {
		p := newPG(t)
		a, b := blobtest.New(), blobtest.New()
		s, err := memStore(t, p, func(c *config.Storage) { c.Connections[0].ID = "archive" }, a, b)
		if err != nil {
			t.Fatal(err)
		}
		d := commit(t, s, memData(1024, 3))
		live := `INSERT INTO replica (instance_uid, name, version, revision, roles, state, secret_key_ids, active_secret_key_id, object_store, started_at, heartbeat_at)
			VALUES (gen_random_uuid(), 'r1', 'v', 'r', '{worker}', 'ready', '{}', 'k', '{"connections":[{"id":"archive","circuit":"closed","drain":false}]}', now(), now())`
		if _, err := p.admin.Exec(ctx, live); err != nil {
			t.Fatal(err)
		}
		if _, err := blob.ForgetConnection(ctx, p.db, "archive", time.Minute, nil); !errors.Is(err, blob.ErrConnectionConfigured) {
			t.Fatalf("while a replica configures it: %v", err)
		}
		if _, err := p.admin.Exec(ctx, `DELETE FROM replica`); err != nil {
			t.Fatal(err)
		}
		rep, err := blob.ForgetConnection(ctx, p.db, "archive", time.Minute, nil)
		if err != nil || rep.Copies != 1 || rep.BlobsMissing != 1 || rep.Queued != 0 { // the gap the commit queued is the other connection's
			t.Fatalf("%+v %v", rep, err)
		}
		var status string
		if err := p.admin.QueryRow(ctx, `SELECT content_status FROM blobs WHERE sha256 = $1`, d.Bytes()).Scan(&status); err != nil || status != "missing" {
			t.Fatalf("the blob only archive held is %q (%v)", status, err)
		}
		if _, ok := a.Data(blob.Key("quader", d)); !ok {
			t.Fatal("forget-connection deleted an object")
		}
		_, err = memStore(t, p, func(c *config.Storage) { c.Connections[0].ID = "archive" }, a)
		var refused *blob.Refusal
		if !errors.As(err, &refused) || !strings.Contains(err.Error(), "connection id archive was retired on") {
			t.Fatalf("a configuration naming the retired id: %v", err)
		}
	})
	t.Run("Q-STOR-250 the copies the migration recorded for the blobs that existed are bound to the element default at its first start", func(t *testing.T) {
		p := newPG(t)
		d := blob.Digest{0xab, 0xcd}
		stmts := []string{
			`INSERT INTO blobs (sha256, sha1, md5, sha512, size) VALUES ($1, decode(repeat('00', 20), 'hex'), decode(repeat('00', 16), 'hex'), decode(repeat('00', 64), 'hex'), 3)`,
			`INSERT INTO storage_connections (id, bucket, prefix) VALUES ('default', '', '')`,
			`INSERT INTO blob_copies (sha256, connection_id, object_key, stored_size, state) VALUES ($1, 'default', 'sha256/ab/cd/' || encode($1, 'hex'), 3, 'present')`,
		}
		for i, q := range stmts {
			args := []any{d.Bytes()}
			if i == 1 {
				args = nil
			}
			if _, err := p.admin.Exec(ctx, q, args...); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
		m := blobtest.New()
		m.Set(blob.Key("quader", d), []byte("abc"), time.Now())
		s, err := memStore(t, p, func(c *config.Storage) { c.Connections[0].ID = "default" }, m)
		if err != nil {
			t.Fatal(err)
		}
		var bucket, key string
		if err := p.admin.QueryRow(ctx, `SELECT sc.bucket, c.object_key FROM storage_connections sc JOIN blob_copies c ON c.connection_id = sc.id WHERE sc.id = 'default'`).
			Scan(&bucket, &key); err != nil || bucket != "bucket-primary" || key != blob.Key("quader", d) {
			t.Fatalf("bucket %q key %q (%v)", bucket, key, err)
		}
		copies, err := s.Copies(ctx, d)
		if err != nil || len(copies) != 1 || copies[0].ObjectKey != blob.Key("quader", d) {
			t.Fatalf("%+v %v", copies, err)
		}
	})
	t.Run("R-STOR-052 a commit writes the copy row and the queue row, and collection's drop of the blob row takes both", func(t *testing.T) {
		p := newPG(t)
		s, err := memStore(t, p, nil, blobtest.New(), blobtest.New())
		if err != nil {
			t.Fatal(err)
		}
		d := commit(t, s, memData(1024, 4))
		var copies, queued int
		if err := p.admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM blob_copies WHERE sha256 = $1 AND connection_id = 'primary' AND state = 'present'),
			(SELECT count(*) FROM blob_copy_queue WHERE sha256 = $1 AND connection_id = 'secondary')`, d.Bytes()).Scan(&copies, &queued); err != nil || copies != 1 || queued != 1 {
			t.Fatalf("copies %d queued %d (%v)", copies, queued, err)
		}
		if _, err := p.admin.Exec(ctx, `UPDATE blobs SET state = 'deleting', object_deleted_at = now() - interval '1 hour' WHERE sha256 = $1`, d.Bytes()); err != nil {
			t.Fatal(err)
		}
		c := blob.NewCollector(blobtest.Storage(t, t.TempDir()).GC, "quader", blobtest.New(), p.db, blob.WithCollectorLogger(quiet))
		if _, err := c.Run(ctx, blob.RunOptions{Mode: blob.GCIncremental}); err != nil {
			t.Fatal(err)
		}
		if err := p.admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM blob_copies WHERE sha256 = $1), (SELECT count(*) FROM blob_copy_queue WHERE sha256 = $1),
			(SELECT count(*) FROM blobs WHERE sha256 = $1)`, d.Bytes()).Scan(&copies, &queued, new(int)); err != nil || copies != 0 || queued != 0 {
			t.Fatalf("after the drop: copies %d queued %d (%v)", copies, queued, err)
		}
	})
}
