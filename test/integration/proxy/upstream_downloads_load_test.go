//go:build integration

package proxy

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/fills"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/proxy/proxytest"
	"github.com/guided-traffic/quader/internal/repository"
)

// countingSink counts what a publisher writes: the statements of Store.Publish and Store.Sweep and the rows they carry.
type countingSink struct {
	fills.Sink
	publications, upserts, deletes, inserts, trims, sweeps atomic.Int64
	ended, running                                         atomic.Int64
	maxRunning                                             atomic.Int64
}

func (c *countingSink) Publish(ctx context.Context, p fills.Publication) error {
	c.publications.Add(1)
	switch {
	case len(p.Running) > 0:
		c.upserts.Add(1)
	case p.DeleteLive:
		c.deletes.Add(1)
	}
	if len(p.Ended) > 0 {
		c.inserts.Add(1)
		c.trims.Add(1)
	}
	c.ended.Add(int64(len(p.Ended)))
	c.running.Add(int64(len(p.Running)))
	for {
		m := c.maxRunning.Load()
		if int64(len(p.Running)) <= m || c.maxRunning.CompareAndSwap(m, int64(len(p.Running))) {
			break
		}
	}
	return c.Sink.Publish(ctx, p)
}

func (c *countingSink) Sweep(ctx context.Context, olderThan time.Duration) (int, error) {
	c.sweeps.Add(1)
	return c.Sink.Sweep(ctx, olderThan)
}

// TestMeasureTheWriteLoadOfTheDownloadOverview is a measurement, not a test (CLAUDE.md, "Tests stay runnable"): it runs only with
// QUADER_TEST_MEASURE_UPSTREAM_DOWNLOADS set and writes its numbers to the log, which docs/developer/upstream-downloads.md keeps. It
// measures what the overview of R-OPS-049 to R-OPS-051 writes to PostgreSQL per download and what it adds to a fetch, at the stack's
// PostgreSQL, with a real engine:
//
//	QUADER_TEST_MEASURE_UPSTREAM_DOWNLOADS=4x500 test/integration/run.sh -run TestMeasureTheWriteLoadOfTheDownloadOverview -v ./test/integration/proxy/
//
// The value is <rounds>x<fetches>: each round sends that many misses of distinct 1 KiB artifacts, 32 at a time, first through a
// replica without the overview (the engine's path before T-26: no tracker) and then through one with it (its tracker and its publisher
// at the default interval of 2 s), each against a remote of its own on the same upstream. Then 300 downloads held open at once fill a
// live row past its 200, and the publications of that row are measured alone.
func TestMeasureTheWriteLoadOfTheDownloadOverview(t *testing.T) {
	spec := os.Getenv("QUADER_TEST_MEASURE_UPSTREAM_DOWNLOADS")
	if spec == "" {
		t.Skip("a measurement: set QUADER_TEST_MEASURE_UPSTREAM_DOWNLOADS to <rounds>x<fetches>, such as 4x500")
	}
	var rounds, perRound int
	if _, err := fmt.Sscanf(spec, "%dx%d", &rounds, &perRound); err != nil || rounds < 1 || perRound < 1 {
		t.Fatalf("QUADER_TEST_MEASURE_UPSTREAM_DOWNLOADS=%q: <rounds>x<fetches>", spec)
	}
	const concurrency = 32
	e := newEnv(t)
	up := e.upstream()
	wide := func(s *repository.RemoteSettings) { s.Limits.MaxConcurrentFetches = 1000 }
	remOff, remOn := e.remote("remote-off", up, wide), e.remote("remote-on", up, wide)
	tracker := fills.NewTracker(fills.TrackerOptions{Replica: "quader-packages-on"})
	off := e.newReplica("off", replicaOpts{})
	on := e.newReplica("on", replicaOpts{downloads: tracker})
	sink := &countingSink{Sink: fills.NewStore(on.db)}
	pub := fills.NewPublisher(fills.PublisherOptions{Tracker: tracker, Sink: sink, InstanceUID: uuid.NewString(), Replica: "quader-packages-on", Log: quiet})
	ctx := context.Background()
	if err := pub.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pub.Stop(ctx) })

	body := proxytest.Body("small", 1024)
	total := rounds * perRound
	for i := range total {
		up.Set(fmt.Sprintf("/pkg/p%05d/-/p%05d-1.tgz", i, i), proxytest.Route{Body: body, Type: "application/octet-stream"})
	}
	wal := func() int64 { return e.number("SELECT pg_current_wal_lsn() - '0/0'::pg_lsn") }
	fetch := func(r *replica, rem proxy.Remote, from, n int) (lat []time.Duration, took time.Duration, walBytes int64) {
		w0, t0 := wal(), time.Now()
		next := atomic.Int64{}
		next.Store(int64(from))
		var mu sync.Mutex
		var wg sync.WaitGroup
		for range concurrency {
			wg.Go(func() {
				for {
					i := int(next.Add(1) - 1)
					if i >= from+n {
						return
					}
					s0 := time.Now()
					s := r.artifact(rem, fmt.Sprintf("pkg/p%05d/-/p%05d-1.tgz", i, i))
					d := time.Since(s0)
					if s.status != http.StatusOK {
						t.Errorf("fetch %d: %d %v", i, s.status, s.err)
						return
					}
					mu.Lock()
					lat = append(lat, d)
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		return lat, time.Since(t0), wal() - w0
	}
	pct := func(l []time.Duration, p float64) time.Duration {
		slices.Sort(l)
		return l[min(len(l)-1, int(float64(len(l))*p))].Round(100 * time.Microsecond)
	}
	var allOff, allOn []time.Duration
	var walOff, walOn int64
	var tookOff, tookOn time.Duration
	for r := range rounds {
		lo, to, wo := fetch(off, remOff, r*perRound, perRound)
		ln, tn, wn := fetch(on, remOn, r*perRound, perRound)
		allOff, allOn = append(allOff, lo...), append(allOn, ln...)
		walOff, walOn, tookOff, tookOn = walOff+wo, walOn+wn, tookOff+to, tookOn+tn
		t.Logf("MEASURE round %d: %d misses without the overview in %s (WAL %d B), with it in %s (WAL %d B); fetch p50 %s / %s, p99 %s / %s",
			r+1, perRound, to.Round(time.Millisecond), wo, tn.Round(time.Millisecond), wn, pct(lo, .5), pct(ln, .5), pct(lo, .99), pct(ln, .99))
	}
	time.Sleep(3 * time.Second) // the last ends go out with the next publication
	t.Logf("MEASURE %d misses each, %d at a time: without the overview %s, %.0f/s, WAL %.0f B per miss, fetch p50 %s p95 %s p99 %s",
		total, concurrency, tookOff.Round(time.Millisecond), float64(total)/tookOff.Seconds(), float64(walOff)/float64(total), pct(allOff, .5), pct(allOff, .95), pct(allOff, .99))
	t.Logf("MEASURE with the overview %s, %.0f/s, WAL %.0f B per miss, fetch p50 %s p95 %s p99 %s",
		tookOn.Round(time.Millisecond), float64(total)/tookOn.Seconds(), float64(walOn)/float64(total), pct(allOn, .5), pct(allOn, .95), pct(allOn, .99))
	t.Logf("MEASURE the publisher over the run: %d publications (%d upserts of the live row, %d deletes, %d inserts and %d trims of the finished rows, %d sweeps), "+
		"%d ended downloads written (%.2f per miss), at most %d running in one publication",
		sink.publications.Load(), sink.upserts.Load(), sink.deletes.Load(), sink.inserts.Load(), sink.trims.Load(), sink.sweeps.Load(),
		sink.ended.Load(), float64(sink.ended.Load())/float64(total), sink.maxRunning.Load())
	t.Logf("MEASURE the tables after the run: %s", e.overviewTables())

	// The live row at its bound: 300 downloads held open, each a 64 KiB body sent in 1 KiB steps every 100 ms (about 6 s).
	held := proxytest.Body("held", 64<<10)
	for i := range 300 {
		up.Set(fmt.Sprintf("/pkg/h%03d/-/h%03d-1.tgz", i, i), proxytest.Route{Body: held, Type: "application/octet-stream", Chunk: 1024, ChunkDelay: 100 * time.Millisecond})
	}
	p0, u0 := sink.publications.Load(), sink.upserts.Load()
	w0 := wal()
	var wg sync.WaitGroup
	for i := range 300 {
		wg.Go(func() {
			if s := on.artifact(remOn, fmt.Sprintf("pkg/h%03d/-/h%03d-1.tgz", i, i)); s.status != http.StatusOK {
				t.Errorf("held %d: %d %v", i, s.status, s.err)
			}
		})
	}
	var rowBytes, omitted int64
	waitFor(t, "a live row with 200 downloads", 30*time.Second, func() bool {
		rowBytes = e.number("SELECT coalesce(max(pg_column_size(downloads)), 0) FROM " + schemaName + ".upstream_download_live")
		omitted = e.number("SELECT coalesce(max(omitted), 0) FROM " + schemaName + ".upstream_download_live")
		return e.number("SELECT coalesce(max(jsonb_array_length(downloads)), 0) FROM " + schemaName + ".upstream_download_live") == fills.MaxPublished
	})
	textBytes := e.number("SELECT coalesce(max(octet_length(downloads::text)), 0) FROM " + schemaName + ".upstream_download_live")
	wg.Wait()
	time.Sleep(3 * time.Second)
	held300 := wal() - w0
	t.Logf("MEASURE 300 downloads held open about 6 s: the live row holds %d (%d omitted), %d B of JSON text, %d B stored (pg_column_size); "+
		"%d publications, %d of them upserts; WAL of the whole phase %d B (the fills' own included)",
		fills.MaxPublished, omitted, textBytes, rowBytes, sink.publications.Load()-p0, sink.upserts.Load()-u0, held300)

	// The publications alone, without a fetch beside them: the WAL of one upsert of a full live row, and of one insert and trim of 100
	// ended downloads, on the store the publisher uses.
	store := fills.NewStore(on.db)
	entries := make([]fills.Entry, fills.MaxPublished)
	for i := range entries {
		speed := int64(1 << 20)
		tot := int64(64 << 10)
		entries[i] = fills.Entry{ID: uuid.NewString(), RepoID: e.number("SELECT id FROM " + schemaName + ".repositories WHERE key = 'remote-on'"), Name: fmt.Sprintf("h%03d", i),
			Version: "1.0.0", Path: fmt.Sprintf("pkg/h%03d/-/h%03d-1.tgz", i, i), Bytes: 32 << 10, Total: &tot, Speed: &speed,
			StartedAt: time.Now().UTC(), ElapsedMs: 3000, Origin: "client", By: uuid.NewString()}
	}
	instance := uuid.NewString()
	const reps = 50
	w1 := wal()
	for range reps {
		if err := store.Publish(ctx, fills.Publication{InstanceUID: instance, Replica: "r", Running: entries}); err != nil {
			t.Fatal(err)
		}
	}
	perUpsert := (wal() - w1) / reps
	ended := func() []fills.Finished {
		out := make([]fills.Finished, fills.MaxFinished)
		now := time.Now().UTC()
		for i := range out {
			tot := int64(1024)
			out[i] = fills.Finished{ID: uuid.NewString(), RepoID: entries[0].RepoID, Name: fmt.Sprintf("p%05d", i), Version: "1", Path: "pkg/x/-/x-1.tgz",
				Bytes: 1024, Total: &tot, StartedAt: now, FinishedAt: now.Add(time.Duration(i) * time.Microsecond), DurationMs: 3, Outcome: "completed",
				Origin: "client", Replica: "r"}
		}
		return out
	}
	w2 := wal()
	for range reps {
		if err := store.Publish(ctx, fills.Publication{InstanceUID: instance, Replica: "r", Ended: ended()}); err != nil {
			t.Fatal(err)
		}
	}
	perEnded := (wal() - w2) / reps
	t.Logf("MEASURE a publication alone (%d each): an upsert of a live row of %d downloads %d B of WAL; an insert of %d ended downloads with the trim %d B of WAL (%d B per row)",
		reps, fills.MaxPublished, perUpsert, fills.MaxFinished, perEnded, perEnded/fills.MaxFinished)
	t.Logf("MEASURE the tables at the end: %s", e.overviewTables())
}

// overviewTables reads the size, the rows and the dead rows of the overview's two tables (the statistics may lag a second).
func (e *env) overviewTables() string {
	out := ""
	for _, tbl := range []string{"upstream_download_live", "upstream_download_finished"} {
		size := e.number("SELECT pg_total_relation_size($1::regclass)", schemaName+"."+tbl)
		rows := e.number("SELECT count(*) FROM " + schemaName + "." + tbl)
		dead := e.number("SELECT coalesce(n_dead_tup, 0) FROM pg_stat_user_tables WHERE relname = $1", tbl)
		vac := e.number("SELECT coalesce(autovacuum_count, 0) FROM pg_stat_user_tables WHERE relname = $1", tbl)
		hot := e.number("SELECT coalesce(n_tup_hot_upd, 0) FROM pg_stat_user_tables WHERE relname = $1", tbl)
		upd := e.number("SELECT coalesce(n_tup_upd, 0) FROM pg_stat_user_tables WHERE relname = $1", tbl)
		out += fmt.Sprintf("%s %d rows, %s with indexes and TOAST, %d dead, %d of %d updates HOT, %d autovacuums; ", tbl, rows, kib(size), dead, hot, upd, vac)
	}
	return out
}

func kib(n int64) string { return strconv.FormatFloat(float64(n)/1024, 'f', 0, 64) + " KiB" }
