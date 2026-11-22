//go:build integration

package blob

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/blob/scrub"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/observability"
)

// memConns is two in-memory connections, primary and secondary, each behind a guard with the test's clock, over the database of a
// test (R-STOR-063: PostgreSQL of the stack with two blobtest stores in process, objects of 1 KiB). seed writes every blob to both
// (min_copies 2), store to the first writable one.
type memConns struct {
	p        *pgEnv
	a, b     *blobtest.Store
	ga, gb   *blob.Guard
	cfg      config.Storage
	conns    *blob.Connections
	seed     *blob.Store
	store    *blob.Store
	clockMu  sync.Mutex
	clockNow time.Time
}

// newMemConns builds the connections with a circuit that opens after threshold failures; collection runs with grace_period 0,
// delete_settle 1 s and handover_delay 4 s.
func newMemConns(t *testing.T, p *pgEnv, threshold int, mutate ...func(*config.Storage)) *memConns {
	t.Helper()
	m := &memConns{p: p, a: blobtest.New(), b: blobtest.New(), clockNow: time.Now()}
	cfg := blobtest.Storage(t, t.TempDir())
	cfg.Upload.BlobWaitTimeout = 10 * time.Second
	cfg.GC.GracePeriod, cfg.GC.DeleteSettle, cfg.GC.HandoverDelay, cfg.GC.BatchSize = 0, time.Second, 4*time.Second, 100
	pre := "quader"
	cfg.Connections = []config.StorageConnection{
		config.StorageConnection{ID: "primary", Bucket: "bucket-primary", Prefix: &pre}.WithDefaults(),
		config.StorageConnection{ID: "secondary", Bucket: "bucket-secondary", Prefix: &pre}.WithDefaults(),
	}
	breaker := config.StorageS3Breaker{FailureThreshold: threshold, OpenDuration: time.Minute}
	cfg.ObjectStore.CircuitBreaker = breaker
	for _, f := range mutate {
		f(&cfg)
	}
	m.ga = blob.NewGuard(m.a, breaker, blob.WithGuardConnection("primary"), blob.WithGuardClock(m.clock))
	m.gb = blob.NewGuard(m.b, breaker, blob.WithGuardConnection("secondary"), blob.WithGuardClock(m.clock))
	var err error
	if m.conns, err = blob.NewConnections(blob.NewConnection(cfg.Connections[0], m.ga), blob.NewConnection(cfg.Connections[1], m.gb)); err != nil {
		t.Fatal(err)
	}
	m.cfg = cfg
	build := func(minCopies int) *blob.Store {
		c := cfg
		c.Replication.MinCopies = minCopies
		c.Spool.Dir = t.TempDir()
		s, err := blob.New(c, m.conns, p.db, blob.WithLogger(quiet), blob.WithWaitPoll(25*time.Millisecond))
		if err == nil {
			err = s.RecordConnections(context.Background())
		}
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	m.seed, m.store = build(2), build(1)
	return m
}

func (m *memConns) clock() time.Time { m.clockMu.Lock(); defer m.clockMu.Unlock(); return m.clockNow }

// closeCircuits moves the guards' clock past the open duration and lifts the faults: the next call of each guard is its probe.
func (m *memConns) closeCircuits() {
	m.a.SetFault(nil)
	m.b.SetFault(nil)
	m.clockMu.Lock()
	m.clockNow = m.clockNow.Add(2 * time.Minute)
	m.clockMu.Unlock()
}

// tripSecondary opens the circuit of the secondary's guard with failed calls and leaves the store failing.
func (m *memConns) tripSecondary(t *testing.T) { t.Helper(); trip(t, m.b, m.gb) }

// trip opens the circuit of a guard with failed calls and leaves its store failing.
func trip(t *testing.T, s *blobtest.Store, g *blob.Guard) {
	t.Helper()
	s.SetFault(func(op, key string) error { return transient(op, key) })
	for i := 0; i < 100 && !g.Open(); i++ {
		_, _ = g.Head(context.Background(), "quader/_probe/trip")
	}
	if !g.Open() {
		t.Fatal("the circuit did not open")
	}
}

func (m *memConns) collector(opts ...blob.CollectorOption) *blob.Collector {
	return blob.NewCollector(m.cfg, m.conns, m.p.db, append([]blob.CollectorOption{blob.WithCollectorLogger(quiet)}, opts...)...)
}

// key is the name of a blob's object on a connection with the prefix quader.
func key(d blob.Digest) string { return blob.Key("quader", d) }

func transient(op, key string) error {
	return &blob.StoreError{Op: op, Key: key, Code: "RequestTimeout", Status: 504, Kind: blob.KindTransient}
}

// upload commits data on s without a reference and returns the error instead of failing the test (for a goroutine).
func upload(s *blob.Store, data []byte) error {
	sp, err := s.Ingest(context.Background(), bytes.NewReader(data), blob.IngestOptions{ContentLength: int64(len(data))})
	if err != nil {
		return err
	}
	defer sp.Close()
	_, err = s.Commit(context.Background(), sp, nil)
	return err
}

func counterValue(tb testing.TB, m *observability.Metrics, name, label, value string) float64 {
	tb.Helper()
	fams, err := m.Registry().Gather()
	if err != nil {
		tb.Fatal(err)
	}
	for _, f := range fams {
		if f.GetName() != name {
			continue
		}
		for _, s := range f.GetMetric() {
			for _, l := range s.GetLabel() {
				if l.GetName() == label && l.GetValue() == value {
					return s.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

func TestR_STOR_057_CollectionWithSeveralConnections(t *testing.T) {
	ctx := context.Background()
	t.Run("R-STOR-057 an unreferenced blob on both connections is collected: both objects go, then the rows", func(t *testing.T) {
		p := newPG(t)
		m := newMemConns(t, p, 5)
		d := commit(t, m.seed, memData(1024, 1))
		if p.count(t, "blob_copies WHERE sha256 = $1", d.Bytes()) != 2 {
			t.Fatal("the seed did not write two copies")
		}
		res, err := m.collector().Run(ctx, blob.RunOptions{Mode: blob.GCFull})
		if err != nil {
			t.Fatal(err)
		}
		if res.Deleted != 1 || res.CopiesReleased != 0 || res.Errors != 0 {
			t.Fatalf("result %+v", res)
		}
		if _, ok := m.a.Data(key(d)); ok {
			t.Error("the primary's object remains")
		}
		if _, ok := m.b.Data(key(d)); ok {
			t.Error("the secondary's object remains")
		}
		if _, _, ok := p.state(t, d); ok || p.count(t, "blob_copies WHERE sha256 = $1", d.Bytes()) != 0 {
			t.Fatal("the blob row or its copy rows remain after the settle wait")
		}
	})

	t.Run("R-STOR-057 with the secondary's circuit open the run deletes on the primary, releases the secondary's copy, and the sweep removes it there after min_age", func(t *testing.T) {
		p := newPG(t)
		m := newMemConns(t, p, 1)
		dx, dy := memData(1024, 2), memData(1024, 3)
		x, y := commit(t, m.seed, dx), commit(t, m.seed, dy)
		m.tripSecondary(t)
		metrics := observability.NewMetrics("worker")
		c := m.collector(blob.WithCollectorMetrics(blob.NewMetrics(metrics)))
		var once sync.Once
		uploaded := make(chan error, 1)
		c.Fault = func(point string) error {
			if point == "record:after" { // both rows are deleting now: an upload of y waits for its row to go, then stores it
				once.Do(func() { go func() { uploaded <- upload(m.store, dy) }() })
			}
			return nil
		}
		res, err := c.Run(ctx, blob.RunOptions{Mode: blob.GCFull})
		if err != nil {
			t.Fatal(err)
		}
		if res.Deleted != 2 || res.CopiesReleased != 2 {
			t.Fatalf("result %+v, want two blobs deleted and two copies released", res)
		}
		if got := counterValue(t, metrics, "quader_gc_copies_released_total", "connection", "secondary"); got != 2 {
			t.Fatalf("quader_gc_copies_released_total{connection=secondary} = %v", got)
		}
		select {
		case err := <-uploaded:
			if err != nil {
				t.Fatalf("the upload of the same content started during the collection: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("the upload of the same content did not end")
		}
		if _, _, ok := p.state(t, x); ok {
			t.Fatal("the row of x remains")
		}
		if st, _, ok := p.state(t, y); !ok || st != "live" || p.count(t, "blob_copies WHERE sha256 = $1 AND connection_id = 'primary' AND state = 'present'", y.Bytes()) != 1 {
			t.Fatal("the upload of y did not store it on the primary")
		}
		for _, d := range []blob.Digest{x, y} {
			if _, ok := m.b.Data(key(d)); !ok {
				t.Fatalf("the released object %s is not on the secondary any more", d.Hex()[:8])
			}
		}
		// the secondary answers again; its sweep after min_age removes the released x and adopts y, which is live again
		m.closeCircuits()
		later := func() time.Time { return time.Now().Add(m.cfg.Orphans.MinAge + time.Hour) }
		sres, err := blob.NewCollector(m.cfg, m.conns, p.db, blob.WithCollectorLogger(quiet), blob.WithCollectorClock(later)).
			Sweep(ctx, blob.SweepOptions{Connection: "secondary"})
		if err != nil {
			t.Fatal(err)
		}
		if len(sres.Connections) != 1 || sres.Connections[0].OrphansDeleted != 1 || sres.Connections[0].Adopted != 1 {
			t.Fatalf("sweep %+v", sres)
		}
		if _, ok := m.b.Data(key(x)); ok {
			t.Fatal("the sweep left the released object of x")
		}
		if _, ok := m.b.Data(key(y)); !ok || p.count(t, "blob_copies WHERE sha256 = $1 AND connection_id = 'secondary' AND state = 'present'", y.Bytes()) != 1 {
			t.Fatal("the object of y, live again, was not adopted on the secondary")
		}
	})

	t.Run("R-STOR-057 R-STOR-056 R-STOR-027 a delete on the secondary that times out keeps the blob deleting, the reconciliation and the scrub pass over the copy it claimed, and the next run deletes it", func(t *testing.T) {
		p := newPG(t)
		m := newMemConns(t, p, 100) // the circuit stays closed: the outcome of each delete is unknown, not refused
		d := commit(t, m.seed, memData(1024, 4))
		m.b.SetFault(func(op, key string) error {
			if op == "delete" || op == "delete_many" {
				return transient(op, key)
			}
			return nil
		})
		res, err := m.collector().Run(ctx, blob.RunOptions{Mode: blob.GCFull})
		if err != nil {
			t.Fatal(err)
		}
		if res.Deleted != 0 || res.Errors == 0 || res.CopiesReleased != 0 {
			t.Fatalf("result %+v", res)
		}
		if st, _, ok := p.state(t, d); !ok || st != "deleting" {
			t.Fatalf("the blob is %q, want deleting", st)
		}
		if p.count(t, "blob_copies WHERE sha256 = $1 AND connection_id = 'secondary'", d.Bytes()) != 1 ||
			p.count(t, "blob_copies WHERE sha256 = $1 AND connection_id = 'primary'", d.Bytes()) != 0 {
			t.Fatal("want the secondary's copy row kept and the primary's gone")
		}
		m.b.SetFault(nil)
		// The copy left in state deleting is collection's, since its blob is deleting, however old the claim: the reconciliation
		// finishes only the drain's claims, on copies of live blobs, and the scrub reads only present and missing copies. Both pass
		// over it, never verified and claimed an hour ago; the object and the claim stay for collection's next run.
		p.exec(t, `UPDATE quader.blob_copies SET state_changed_at = now() - interval '1 hour', verified_at = NULL WHERE sha256 = $1`, d.Bytes())
		reconcileRun(t, newReconciler(t, p, m.seed, m.cfg, nil))
		sres, err := scrubber(t, p, m.seed).Run(ctx, scrub.RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range sres.Connections {
			if c.Read != 0 {
				t.Fatalf("the scrub read %d copies on %s, want none", c.Read, c.Connection)
			}
		}
		if p.count(t, `blob_copies WHERE sha256 = $1 AND connection_id = 'secondary' AND state = 'deleting'
			AND state_changed_at < now() - interval '59 minutes'`, d.Bytes()) != 1 {
			t.Fatal("the reconciliation or the scrub took the copy collection claimed")
		}
		if _, ok := m.b.Data(key(d)); !ok {
			t.Fatal("the reconciliation deleted the object of the copy collection claimed")
		}
		time.Sleep(m.cfg.GC.DeleteSettle + 200*time.Millisecond) // the run handed its claim over: the next takes it after delete_settle
		res, err = m.collector().Run(ctx, blob.RunOptions{Mode: blob.GCIncremental})
		if err != nil {
			t.Fatal(err)
		}
		if res.Deleted != 1 {
			t.Fatalf("the next run: %+v", res)
		}
		if _, ok := m.b.Data(key(d)); ok {
			t.Fatal("the secondary's object remains")
		}
		if _, _, ok := p.state(t, d); ok {
			t.Fatal("the blob row remains")
		}
	})

	t.Run("R-STOR-057 with every circuit open the run stops before it releases anything, as with one connection", func(t *testing.T) {
		p := newPG(t)
		m := newMemConns(t, p, 1)
		d := commit(t, m.seed, memData(1024, 5))
		m.tripSecondary(t)
		trip(t, m.a, m.ga)
		res, err := m.collector().Run(ctx, blob.RunOptions{Mode: blob.GCFull})
		if err == nil || res.CopiesReleased != 0 {
			t.Fatalf("result %+v, err %v: want the run stopped and nothing released", res, err)
		}
		if p.count(t, "blob_copies WHERE sha256 = $1", d.Bytes()) != 2 {
			t.Fatal("a copy row went while no connection could be asked")
		}
	})
}

// session is an upload session row of the test in state, with bytes staged on a connection.
func (p *pgEnv) session(tb testing.TB, conn, state string, age time.Duration) string {
	tb.Helper()
	id := uuid.NewString()
	p.exec(tb, `INSERT INTO upload_sessions (id, repo_id, addressed_repo_id, binding, received, hash_state, part_size, s3_upload_id, state,
		created_at, last_activity_at, connection_id)
		VALUES ($1::uuid, $2, $2, 'b', 10, '\x00'::bytea, 16, '', $5, now() - make_interval(secs => $3::float8),
		        now() - make_interval(secs => $3::float8), $4)`,
		id, p.repo, age.Seconds(), conn, state)
	return id
}

func TestR_STOR_054_TheSessionStepWorksOnEachSessionsConnection(t *testing.T) {
	p := newPG(t)
	m := newMemConns(t, p, 1)
	ctx := context.Background()
	tail := func(s *blobtest.Store, id string) string {
		k := blob.SessionKey("quader", id, "tail-"+uuid.NewString())
		s.Set(k, []byte("0123456789"), time.Now())
		return k
	}
	onPrimary := p.session(t, "primary", "aborted", time.Minute)
	keyPrimary := tail(m.a, onPrimary)
	young := p.session(t, "secondary", "aborted", time.Minute)
	keyYoung := tail(m.b, young)
	old := p.session(t, "secondary", "aborted", m.cfg.Uploads.SessionMaxAge+time.Hour)
	keyOld := tail(m.b, old)
	m.tripSecondary(t)

	res, err := m.collector(blob.WithUploadSessions(m.cfg.Uploads)).Run(ctx, blob.RunOptions{Mode: blob.GCIncremental})
	if err != nil {
		t.Fatal(err)
	}
	if res.UploadSessionsCleaned != 2 {
		t.Fatalf("result %+v, want the primary's session and the old one cleaned", res)
	}
	if _, ok := m.a.Data(keyPrimary); ok || p.count(t, "upload_sessions WHERE id = $1::uuid", onPrimary) != 0 {
		t.Fatal("the session on the reachable primary was not removed with its objects")
	}
	if p.count(t, "upload_sessions WHERE id = $1::uuid", young) != 1 {
		t.Fatal("a young session on the unreachable secondary lost its row")
	}
	if p.count(t, "upload_sessions WHERE id = $1::uuid", old) != 0 {
		t.Fatal("a session past session_max_age on the unreachable secondary kept its row")
	}
	for _, k := range []string{keyYoung, keyOld} {
		if _, ok := m.b.Data(k); !ok {
			t.Fatalf("the object %s on the unreachable secondary is gone", k)
		}
	}
}
