//go:build integration

package proxy

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/fills"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/proxy/proxytest"
	"github.com/guided-traffic/quader/internal/remotecuration"
)

// recordingTracker passes every download to a replica's tracker and keeps its start and its outcome, so that a case reads what the
// engine reported.
type recordingTracker struct {
	*fills.Tracker
	mu       sync.Mutex
	starts   []proxy.DownloadStart
	outcomes []string
	failures []*remotecuration.Failure
}

func (r *recordingTracker) Begin(s proxy.DownloadStart) proxy.Download {
	r.mu.Lock()
	r.starts = append(r.starts, s)
	r.mu.Unlock()
	return recordedEnd{Download: r.Tracker.Begin(s), r: r}
}

type recordedEnd struct {
	proxy.Download
	r *recordingTracker
}

func (d recordedEnd) End(outcome string, f *remotecuration.Failure) {
	d.r.mu.Lock()
	d.r.outcomes, d.r.failures = append(d.r.outcomes, outcome), append(d.r.failures, f)
	d.r.mu.Unlock()
	d.Download.End(outcome, f)
}

func (r *recordingTracker) seen() ([]proxy.DownloadStart, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]proxy.DownloadStart(nil), r.starts...), append([]string(nil), r.outcomes...)
}

func (r *recordingTracker) reset() {
	r.mu.Lock()
	r.starts, r.outcomes, r.failures = nil, nil, nil
	r.mu.Unlock()
}

// TestR_OPS_050_TwoReplicasShowOneRunningDownload: the downloads from upstreams of a real engine against the stack's PostgreSQL
// (R-OPS-049 to R-OPS-051). One artifact of 64 KiB and rows of a few bytes; the publishers run by hand, so no case waits for an
// interval but the one second before a speed exists.
func TestR_OPS_050_TwoReplicasShowOneRunningDownload(t *testing.T) {
	e := newEnv(t)
	up := e.upstream()
	rem := e.remote("npm-remote", up)
	repoID := e.number("SELECT id FROM repositories WHERE key = 'npm-remote'")
	trA := &recordingTracker{Tracker: fills.NewTracker(fills.TrackerOptions{Replica: "quader-packages-a"})}
	a := e.newReplica("a", replicaOpts{downloads: trA})
	b := e.newReplica("b", replicaOpts{})
	storeA, storeB := fills.NewStore(a.db), fills.NewStore(b.db)
	pubA := fills.NewPublisher(fills.PublisherOptions{
		Tracker: trA.Tracker, Sink: storeA, InstanceUID: uuid.NewString(), Replica: "quader-packages-a",
		Interval: time.Second, Log: quiet,
	})
	ctx := context.Background()
	body := proxytest.Body("held", 64<<10)

	t.Run("R-OPS-049 one download per body from the upstream, whatever waits for it, and none for anything else", func(t *testing.T) {
		up.Set("/pkg/one/-/one-1.tgz", proxytest.Route{Body: body, Type: "application/octet-stream", Delay: 300 * time.Millisecond})
		up.Set("/pkg/one", proxytest.Route{Body: []byte(`{"name":"one"}`), Type: "application/json"})
		up.Set("/pkg/nolength/-/nolength-1.tgz", proxytest.Route{Handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(body[:1024])
			w.(http.Flusher).Flush()
			_, _ = w.Write(body[1024:2048])
		}})
		up.Set("/pkg/bad/-/bad-1.tgz", proxytest.Route{Body: body[:4096], Type: "application/octet-stream"})

		var wg sync.WaitGroup
		for range 3 {
			wg.Go(func() {
				if s := a.artifact(rem, "pkg/one/-/one-1.tgz"); s.status != http.StatusOK {
					t.Errorf("miss: %d %v", s.status, s.err)
				}
			})
		}
		wg.Wait()
		starts, outcomes := trA.seen()
		if len(starts) != 1 || starts[0].Total != 64<<10 || starts[0].Name != "one" || starts[0].RepoID != repoID ||
			starts[0].Cause.Origin != proxy.DownloadOriginClient || len(outcomes) != 1 || outcomes[0] != fills.OutcomeCompleted {
			t.Fatalf("three concurrent misses: %+v %v", starts, outcomes)
		}
		// A hit, a HEAD, a 404 and a document record none.
		trA.reset()
		a.artifact(rem, "pkg/one/-/one-1.tgz")
		a.artifactWith(rem, http.MethodHead, "pkg/nolength/-/nolength-1.tgz", nil)
		a.artifact(rem, "pkg/missing/-/missing-1.tgz")
		a.metadata(rem, "pkg/one")
		if starts, _ := trA.seen(); len(starts) != 0 {
			t.Fatalf("a hit, a HEAD, a 404 and a document: %+v", starts)
		}
		// A body without a length records no total and its bytes.
		if s := a.artifact(rem, "pkg/nolength/-/nolength-1.tgz"); s.status != http.StatusOK {
			t.Fatalf("no length: %d %v", s.status, s.err)
		}
		// A body that does not match the announced digest fails with the class integrity.
		a.artifact(rem, "pkg/bad/-/bad-1.tgz", func(rq *proxy.Request) {
			rq.Expected, rq.AlwaysVerify = proxy.Digests{SHA256: proxytest.SHA256([]byte("other"))}, true
		})
		starts, outcomes = trA.seen()
		trA.mu.Lock()
		failures := trA.failures
		trA.mu.Unlock()
		if len(starts) != 2 || starts[0].Total != -1 || outcomes[0] != fills.OutcomeCompleted || outcomes[1] != fills.OutcomeFailed ||
			failures[1] == nil || failures[1].Class != remotecuration.ClassIntegrity {
			t.Fatalf("no length, a mismatch: %+v %v %+v", starts, outcomes, failures)
		}
		pubA.Tick(ctx) // the ends go out; the next case starts from an empty buffer
	})

	t.Run("R-OPS-050 another replica reads a running download with its bytes, total and speed; it ends completed", func(t *testing.T) {
		release := make(chan struct{})
		up.Set("/pkg/held/-/held-1.tgz", proxytest.Route{Handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			_, _ = w.Write(body[:16<<10])
			w.(http.Flusher).Flush()
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			_, _ = w.Write(body[16<<10:])
		}})
		done := make(chan served, 1)
		go func() { done <- a.artifact(rem, "pkg/held/-/held-1.tgz") }()

		var got fills.Entry
		waitFor(t, "the running download with a speed", 10*time.Second, func() bool {
			pubA.Tick(ctx)
			v, err := storeB.ReadUpstreamDownloads(ctx)
			if err != nil || len(v.Live) != 1 || len(v.Live[0].Downloads) != 1 {
				return false
			}
			got = v.Live[0].Downloads[0]
			return got.Bytes >= 16<<10 && got.Speed != nil
		})
		if got.Total == nil || *got.Total != 64<<10 || got.Name != "held" || got.RepoID != repoID || got.Origin != "client" || got.ElapsedMs < 1000 {
			t.Fatalf("%+v", got)
		}
		close(release)
		if s := <-done; s.status != http.StatusOK || len(s.body) != len(body) {
			t.Fatalf("the held download: %d %v", s.status, s.err)
		}
		pubA.Tick(ctx)
		v, err := storeB.ReadUpstreamDownloads(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Live) != 0 {
			t.Fatalf("after the end the live row stays: %+v", v.Live)
		}
		f := v.Finished[0]
		if f.Name != "held" || f.Outcome != fills.OutcomeCompleted || f.Bytes != 64<<10 || f.DurationMs < 1000 || f.Replica != "quader-packages-a" ||
			v.Repos[repoID].Key != "npm-remote" || v.Repos[repoID].Project != "it" {
			t.Fatalf("%+v %+v", f, v.Repos)
		}
	})

	t.Run("R-OPS-051 the list keeps the newest 100; a stale row becomes interrupted and its later end replaces it; a removed repository's rows go", func(t *testing.T) {
		// The rows lie two hours back, so the swept row (its last write a minute ago) and the later ones are among the newest 100.
		t0 := time.Now().UTC().Add(-2 * time.Hour)
		var many []fills.Finished
		for i := range 101 {
			many = append(many, fills.Finished{
				ID: uuid.NewString(), RepoID: repoID, Name: fmt.Sprintf("n%03d", i), StartedAt: t0,
				FinishedAt: t0.Add(time.Duration(i) * time.Millisecond), Outcome: fills.OutcomeCompleted, Origin: "client", Replica: "r",
			})
		}
		if err := storeA.Publish(ctx, fills.Publication{InstanceUID: uuid.NewString(), Replica: "r", Ended: many}); err != nil {
			t.Fatal(err)
		}
		if n := e.number("SELECT count(*) FROM upstream_download_finished"); n != fills.MaxFinished {
			t.Fatalf("%d rows", n)
		}
		if n := e.number("SELECT count(*) FROM upstream_download_finished WHERE name = 'n000'"); n != 0 {
			t.Fatal("the oldest stays")
		}

		// A replica that stopped: its row is set back by a minute, read as old, swept as interrupted.
		stale, entry := uuid.NewString(), fills.Entry{
			ID: uuid.NewString(), RepoID: repoID, Name: "stalled", Bytes: 2048, StartedAt: t0.Add(time.Second),
			ElapsedMs: 4000, Origin: "queue", QueueOrigin: "bulk", QueueRow: uuid.NewString(),
		}
		if err := storeA.Publish(ctx, fills.Publication{InstanceUID: stale, Replica: "quader-worker-x", Running: []fills.Entry{entry}}); err != nil {
			t.Fatal(err)
		}
		e.exec("UPDATE upstream_download_live SET written_at = now() - interval '1 minute' WHERE instance_uid = $1", stale)
		v, err := storeB.ReadUpstreamDownloads(ctx)
		if err != nil || len(v.Live) != 1 || v.Live[0].Age < time.Minute {
			t.Fatalf("the stale row: %v %+v", err, v.Live)
		}
		if n, err := storeB.Sweep(ctx, 30*time.Second); err != nil || n != 1 {
			t.Fatalf("sweep: %d %v", n, err)
		}
		if o := e.text("SELECT outcome || ' ' || bytes || ' ' || duration_ms || ' ' || queue_origin FROM upstream_download_finished WHERE id = $1", entry.ID); o != "interrupted 2048 4000 bulk" {
			t.Fatalf("swept: %q", o)
		}
		late := fills.Interrupted("quader-worker-x", t0.Add(time.Minute), []fills.Entry{entry})
		late[0].Outcome, late[0].Bytes = fills.OutcomeCompleted, 4096
		if err := storeA.Publish(ctx, fills.Publication{InstanceUID: stale, Replica: "quader-worker-x", Ended: late}); err != nil {
			t.Fatal(err)
		}
		if o := e.text("SELECT outcome || ' ' || bytes FROM upstream_download_finished WHERE id = $1", entry.ID); o != "completed 4096" {
			t.Fatalf("the later end: %q", o)
		}

		// The bound is 100 units, not 100 rows: a unit three replicas wrote (a client's pull whose layers they served) keeps its three
		// rows while it is among the newest 100 units, and the read merges them into one download (R-OPS-049, Q-OPS-429).
		e.exec("DELETE FROM upstream_download_finished")
		t1 := time.Now().UTC().Add(-3 * time.Hour) // older than the removed repository's row below, which must stay among the newest 100
		var units []fills.Finished
		for i := range 99 {
			units = append(units, fills.Finished{
				ID: uuid.NewString(), RepoID: repoID, Name: fmt.Sprintf("u%03d", i), StartedAt: t1,
				FinishedAt: t1.Add(time.Duration(i) * time.Millisecond), Outcome: fills.OutcomeCompleted, Origin: "client", Replica: "r",
			})
		}
		pull := "c|" + strconv.FormatInt(repoID, 10) + "|library/ubuntu|"
		for i, replica := range []string{"quader-packages-a", "quader-packages-b", "quader-packages-c"} {
			units = append(units, fills.Finished{
				ID: uuid.NewString(), Unit: pull, RepoID: repoID, Name: "library/ubuntu", Version: "26.10", Parts: 10, Bytes: 1000,
				StartedAt: t1.Add(time.Second), FinishedAt: t1.Add(time.Second + time.Duration(i)*time.Millisecond), Outcome: fills.OutcomeCompleted,
				Origin: "client", Replica: replica,
			})
		}
		if err := storeA.Publish(ctx, fills.Publication{InstanceUID: uuid.NewString(), Replica: "r", Ended: units}); err != nil {
			t.Fatal(err)
		}
		if n := e.number("SELECT count(*) FROM upstream_download_finished"); n != 102 {
			t.Fatalf("100 units in %d rows, want 102", n)
		}
		newer := fills.Finished{
			ID: uuid.NewString(), RepoID: repoID, Name: "newer", StartedAt: t1.Add(time.Minute), FinishedAt: t1.Add(time.Minute),
			Outcome: fills.OutcomeCompleted, Origin: "client", Replica: "r",
		}
		if err := storeA.Publish(ctx, fills.Publication{InstanceUID: uuid.NewString(), Replica: "r", Ended: []fills.Finished{newer}}); err != nil {
			t.Fatal(err)
		}
		if n := e.number("SELECT count(*) FROM upstream_download_finished WHERE unit = $1", pull); n != 3 {
			t.Fatalf("the pull keeps %d of its 3 rows", n)
		}
		if n := e.number("SELECT count(*) FROM upstream_download_finished WHERE name = 'u000'"); n != 0 {
			t.Fatal("the oldest unit stays")
		}
		v, err = storeB.ReadUpstreamDownloads(ctx)
		if err != nil {
			t.Fatal(err)
		}
		merged := fills.Merge(v, time.Second)
		if len(merged.Finished) != fills.MaxFinished || merged.Finished[0].Name != "newer" || merged.Finished[1].Bytes != 3000 || merged.Finished[1].Parts != 30 {
			t.Fatalf("the read: %d units, %+v", len(merged.Finished), merged.Finished[:2])
		}

		// A removed repository takes its rows with it.
		e.remote("gone", up)
		gone := e.number("SELECT id FROM repositories WHERE key = 'gone'")
		row := fills.Finished{
			ID: uuid.NewString(), RepoID: gone, Name: "g", StartedAt: t0, FinishedAt: t0.Add(time.Hour), Outcome: fills.OutcomeCompleted,
			Origin: "mirror", Replica: "r",
		}
		if err := storeA.Publish(ctx, fills.Publication{InstanceUID: stale, Replica: "r", Ended: []fills.Finished{row}}); err != nil {
			t.Fatal(err)
		}
		if n := e.number("SELECT count(*) FROM upstream_download_finished WHERE repo_id = $1", gone); n != 1 {
			t.Fatalf("%d rows of the repository before its removal", n)
		}
		e.exec("DELETE FROM repositories WHERE id = $1", gone)
		if n := e.number("SELECT count(*) FROM upstream_download_finished WHERE repo_id = $1", gone); n != 0 {
			t.Fatalf("%d rows of a removed repository", n)
		}
	})
}
