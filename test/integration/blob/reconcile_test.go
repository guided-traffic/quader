//go:build integration

package blob

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/blob/reconcile"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The reconciliation against PostgreSQL of the stack with in-memory stores (storage.md R-STOR-056, R-STOR-063): objects of
// 1 KiB, at most twenty.

// reconStore is a store over the given object stores as the connections primary, secondary and archive, recorded in p's
// database, each with the payload checksum enforced as the in-memory store enforces it, and the configuration it was built with.
func reconStore(tb testing.TB, p *pgEnv, mutate func(*config.Storage), stores ...blob.ObjectStore) (*blob.Store, config.Storage) {
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
		c := blob.NewConnection(cfg.Connections[i], s)
		c.SetCapabilities(blob.Capabilities{PayloadChecksumEnforced: true})
		list = append(list, c)
	}
	conns, err := blob.NewConnections(list...)
	if err != nil {
		tb.Fatal(err)
	}
	s, err := blob.New(cfg, conns, p.db, blob.WithLogger(quiet), blob.WithWaitPoll(25*time.Millisecond))
	if err != nil {
		tb.Fatal(err)
	}
	if err := s.RecordConnections(context.Background()); err != nil {
		tb.Fatal(err)
	}
	return s, cfg
}

func newReconciler(tb testing.TB, p *pgEnv, s *blob.Store, cfg config.Storage, mutate func(*reconcile.Options)) *reconcile.Reconciler {
	tb.Helper()
	o := reconcile.Options{Store: s, DB: p.db, Storage: cfg, Log: quiet, StaleAfter: time.Minute, JournalDir: "_journal/revocations"}
	if mutate != nil {
		mutate(&o)
	}
	r, err := reconcile.New(o)
	if err != nil {
		tb.Fatal(err)
	}
	return r
}

func reconcileRun(tb testing.TB, r *reconcile.Reconciler) reconcile.Result {
	tb.Helper()
	res, err := r.Run(context.Background(), nil)
	if err != nil {
		tb.Fatalf("run: %v (%+v)", err, res)
	}
	return res
}

// fakeClock is a clock a test moves.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func (p *pgEnv) gauges(tb testing.TB) map[string]q.ReconcileGaugesRow {
	tb.Helper()
	rows, err := q.New(p.db.Main()).ReconcileGauges(context.Background(), 60)
	if err != nil {
		tb.Fatal(err)
	}
	out := map[string]q.ReconcileGaugesRow{}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}

const liveReplica = `INSERT INTO replica (instance_uid, name, version, revision, roles, state, secret_key_ids, active_secret_key_id, object_store, started_at, heartbeat_at)
	VALUES (gen_random_uuid(), 'r1', 'v', 'r', '{worker}', 'ready', '{}', 'k', $1::jsonb, now(), now())`

func TestR_STOR_056_TheReconciliation(t *testing.T) {
	ctx := context.Background()
	t.Run("R-STOR-056 the gaps of ten uploads wait without spending attempts while the second connection's circuit is open and one run copies them all, verified, once it closed", func(t *testing.T) {
		p := newPG(t)
		a, b := blobtest.New(), blobtest.New()
		clock := &fakeClock{t: time.Now()}
		guard := blob.NewGuard(b, config.StorageS3Breaker{FailureThreshold: 1, OpenDuration: time.Minute}, blob.WithGuardConnection("secondary"), blob.WithGuardClock(clock.now))
		s, cfg := reconStore(t, p, nil, a, guard)
		for i := range 10 {
			commit(t, s, memData(1024, byte(10+i)))
		}
		if n := p.scalarInt(t, `SELECT count(*) FROM blob_copy_queue WHERE connection_id = 'secondary'`); n != 10 {
			t.Fatalf("%d queue rows, want 10", n)
		}
		if g := p.gauges(t)["secondary"]; g.Queued != 10 || g.OldestSeconds < 0 {
			t.Fatalf("the gauges of secondary: %+v", g)
		}
		b.SetFault(func(op, _ string) error { return &blob.StoreError{Op: op, Status: 503, Kind: blob.KindTransient} })
		_ = guard.Put(ctx, "quader/_probe/x", bytes.NewReader(nil), 0, blob.Checksum{}, nil) // opens the circuit
		r := newReconciler(t, p, s, cfg, nil)
		if res := reconcileRun(t, r); res.Copied != 0 || res.Failed != 0 {
			t.Fatalf("while the circuit is open: %+v", res)
		}
		if n := p.scalarInt(t, `SELECT count(*) FROM blob_copy_queue WHERE connection_id = 'secondary' AND attempts = 0`); n != 10 {
			t.Fatalf("%d rows without an attempt, want 10", n)
		}
		b.SetFault(nil)
		clock.add(2 * time.Minute)
		if res := reconcileRun(t, r); res.Copied != 10 || res.Streamed != 10 {
			t.Fatalf("after the circuit closed: %+v", res)
		}
		if n := p.scalarInt(t, `SELECT count(*) FROM blob_copies WHERE connection_id = 'secondary' AND state = 'present' AND verified_at IS NOT NULL`); n != 10 {
			t.Fatalf("%d verified copies on secondary, want 10", n)
		}
		if n := p.scalarInt(t, `SELECT count(*) FROM blob_copy_queue`); n != 0 {
			t.Fatalf("%d queue rows left", n)
		}
		if len(b.Keys("quader/sha256/")) != 10 {
			t.Fatalf("the second store holds %d blob objects", len(b.Keys("quader/sha256/")))
		}
	})
	t.Run("R-STOR-056 a connection added to an installation of twenty blobs is filling, receives every blob and turns active after the first full pass without a gap", func(t *testing.T) {
		p := newPG(t)
		a, b := blobtest.New(), blobtest.New()
		one, _ := reconStore(t, p, nil, a)
		for i := range 20 {
			commit(t, one, memData(1024, byte(40+i)))
		}
		s, cfg := reconStore(t, p, nil, a, b)
		if g := p.gauges(t); !g["primary"].Detached || g["secondary"].Detached {
			t.Fatalf("without a live replica the connection that holds copies is not detached, or the empty one is: %+v", g)
		}
		if _, err := p.admin.Exec(ctx, liveReplica, `{"connections":[{"id":"primary","circuit":"closed","drain":false},{"id":"secondary","circuit":"closed","drain":false}]}`); err != nil {
			t.Fatal(err)
		}
		if g := p.gauges(t); !g["secondary"].Filling || g["primary"].Detached {
			t.Fatalf("the connection added is not filling, or a configured one is detached: %+v", g)
		}
		r := newReconciler(t, p, s, cfg, nil)
		for run := 0; run < 3 && p.scalarInt(t, `SELECT count(*) FROM storage_connections WHERE id = 'secondary' AND filled_at IS NOT NULL`) == 0; run++ {
			reconcileRun(t, r)
		}
		if n := p.scalarInt(t, `SELECT count(*) FROM blob_copies WHERE connection_id = 'secondary' AND state = 'present'`); n != 20 || len(b.Keys("quader/sha256/")) != 20 {
			t.Fatalf("secondary holds %d copies and %d objects, want 20", n, len(b.Keys("quader/sha256/")))
		}
		if p.scalarInt(t, `SELECT count(*) FROM storage_connections WHERE filled_at IS NOT NULL AND last_full_pass_at IS NOT NULL`) != 2 {
			t.Fatal("the connections are not active after a full pass without a gap")
		}
		if g := p.gauges(t); g["secondary"].Filling || g["primary"].Filling {
			t.Fatalf("still filling: %+v", g)
		}
	})
	t.Run("R-STOR-056 a drain moves what only the first holds, keeps a copy beside an unverified survivor until the scrub verified it, then the first is drained and every blob reads from the second", func(t *testing.T) {
		p := newPG(t)
		a, b := blobtest.New(), blobtest.New()
		s, cfg := reconStore(t, p, nil, a, b)
		data := map[blob.Digest][]byte{}
		var both []blob.Digest
		for i := range 3 {
			v := memData(1024, byte(70+i))
			d := commit(t, s, v)
			data[d], both = v, append(both, d)
		}
		reconcileRun(t, newReconciler(t, p, s, cfg, nil))
		unverified := both[1]
		if _, err := p.admin.Exec(ctx, `UPDATE blob_copies SET verified_at = NULL WHERE sha256 = $1 AND connection_id = 'secondary'`, unverified.Bytes()); err != nil {
			t.Fatal(err)
		}
		only := memData(1024, 90)
		onlyFirst := commit(t, s, only)
		data[onlyFirst] = only
		if _, err := p.admin.Exec(ctx, `DELETE FROM blob_copy_queue WHERE sha256 = $1`, onlyFirst.Bytes()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.admin.Exec(ctx, liveReplica, `{"connections":[{"id":"primary","circuit":"closed","drain":true},{"id":"secondary","circuit":"closed","drain":false}]}`); err != nil {
			t.Fatal(err)
		}
		sd, cfgD := reconStore(t, p, func(c *config.Storage) { c.Connections[0].Drain = true }, a, b)
		clock := &fakeClock{t: time.Now()}
		var events []int64
		r := newReconciler(t, p, sd, cfgD, func(o *reconcile.Options) {
			o.Now = clock.now
			o.Drained = func(_ context.Context, _ meta.Tx, conn string, deleted int64) error {
				if conn == "primary" {
					events = append(events, deleted)
				}
				return nil
			}
		})
		// the deletion pause of R-HA-025 stops the drain's deletions, not its copies
		if _, err := p.admin.Exec(ctx, `INSERT INTO system_state (key, value) VALUES ('deletions_paused_at', '"2026-11-22T00:00:00Z"')
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`); err != nil {
			t.Fatal(err)
		}
		if res := reconcileRun(t, r); res.CopiesDeleted != 0 || len(a.Keys("quader/sha256/")) != 4 {
			t.Fatalf("while deletions are paused: %+v, %d objects on the first", res, len(a.Keys("quader/sha256/")))
		}
		if _, err := p.admin.Exec(ctx, `DELETE FROM system_state WHERE key = 'deletions_paused_at'`); err != nil {
			t.Fatal(err)
		}
		res := reconcileRun(t, r)
		if keys := a.Keys("quader/sha256/"); len(keys) != 1 || keys[0] != blob.Key("quader", unverified) {
			t.Fatalf("the first holds %v after the drain's run (%+v), want only the blob whose survivor is not verified", keys, res)
		}
		if p.scalarInt(t, `SELECT count(*) FROM blob_copies WHERE sha256 = $1 AND connection_id = 'secondary' AND state = 'present'`, onlyFirst.Bytes()) != 1 {
			t.Fatal("the blob only the first held was not copied before its deletion")
		}
		if len(events) != 0 || p.scalarInt(t, `SELECT count(*) FROM storage_connections WHERE id = 'primary' AND drained_at IS NULL`) != 1 {
			t.Fatal("drained while it holds a copy")
		}
		// the scrub verifies the survivor; the next walk waits for the pass interval after one that found nothing to do
		if _, err := p.admin.Exec(ctx, `UPDATE blob_copies SET verified_at = now() WHERE sha256 = $1 AND connection_id = 'secondary'`, unverified.Bytes()); err != nil {
			t.Fatal(err)
		}
		clock.add(reconcile.PassInterval + time.Minute)
		reconcileRun(t, r)
		if keys := a.Keys("quader/sha256/"); len(keys) != 0 {
			t.Fatalf("the first still holds %v", keys)
		}
		if n := p.scalarInt(t, `SELECT count(*) FROM blob_copies WHERE connection_id = 'primary'`); n != 0 {
			t.Fatalf("%d copy rows on the first", n)
		}
		if len(events) != 1 || events[0] != 4 {
			t.Fatalf("drained events %v, want one with 4 copies deleted", events)
		}
		if p.scalarInt(t, `SELECT count(*) FROM storage_connections WHERE id = 'primary' AND drained_at IS NOT NULL AND retired_at IS NULL`) != 1 {
			t.Fatal("not drained, or retired while a live replica configures it")
		}
		for d, v := range data {
			rc, err := sd.Open(ctx, d, int64(len(v)), blob.OpenOptions{})
			if err != nil {
				t.Fatalf("%s: %v", d.Hex(), err)
			}
			got, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil || !bytes.Equal(got, v) {
				t.Fatalf("%s reads %d bytes (%v)", d.Hex(), len(got), err)
			}
		}
		if _, err := p.admin.Exec(ctx, `DELETE FROM replica`); err != nil {
			t.Fatal(err)
		}
		if res := reconcileRun(t, r); len(res.Retired) != 1 || res.Retired[0] != "primary" {
			t.Fatalf("once no live replica configures it: %+v", res)
		}
	})
	t.Run("R-STOR-056 a run paused after its copy write for longer than the insert bound writes the object again before it records the row", func(t *testing.T) {
		p := newPG(t)
		a, b := blobtest.New(), blobtest.New()
		s, cfg := reconStore(t, p, nil, a, b)
		d := commit(t, s, memData(1024, 100))
		clock := &fakeClock{t: time.Now()}
		paused := false
		r := newReconciler(t, p, s, cfg, func(o *reconcile.Options) {
			o.Now = clock.now
			o.AfterWrite = func(string, blob.Digest) {
				if !paused {
					paused = true
					clock.add(min(cfg.Upload.CommitDeadline, cfg.GC.DeleteSettle/2) + time.Second)
				}
			}
		})
		if res := reconcileRun(t, r); res.Copied != 1 {
			t.Fatalf("%+v", res)
		}
		if b.Count("put") != 2 {
			t.Fatalf("%d writes, want 2: the first was older than the insert bound when its row was to be recorded", b.Count("put"))
		}
		if p.scalarInt(t, `SELECT count(*) FROM blob_copies WHERE sha256 = $1 AND connection_id = 'secondary' AND state = 'present'`, d.Bytes()) != 1 {
			t.Fatal("no present copy row")
		}
	})
}
