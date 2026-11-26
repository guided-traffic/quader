package fills

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/remotecuration"
)

const person = "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a20"

// layer is a part of an image: a blob of name in the layout of R-OCI-007 with the unit and the cause given.
func layer(i int, unit string, open, resolve bool, cause proxy.DownloadCause) proxy.DownloadStart {
	return proxy.DownloadStart{
		RepoID: 3, Repo: "upstream/docker-hub", Format: "oci", Name: "guidedtraffic/stackit-s3-provisioner",
		Path: fmt.Sprintf("guidedtraffic/stackit-s3-provisioner/_blobs/sha256:%064x", i), Total: int64(100 + i),
		Cause: cause, Unit: unit, Open: open, Resolve: resolve,
	}
}

// R-OPS-049: the files of one unit's key are one download. A client's pull of an image (31 tiny layers, the parts one after the other)
// stays one running unit between its layers and for proxy.UnitLinger after the last, has no total (its files are named by nobody in
// advance), sums its bytes and counts its files, and ends with the last file's end; a file that failed fails it with its class, code and
// path. Units of a version's files end with their last file, without a linger.
func TestR_OPS_049_TheFilesOfOneKeyAreOneDownload(t *testing.T) {
	c := newClock()
	tr := NewTracker(TrackerOptions{Replica: "quader-packages-1", Now: c.now})
	client := proxy.DownloadCause{Origin: proxy.DownloadOriginClient, Principal: person}
	key := "c|3|guidedtraffic/stackit-s3-provisioner|" + person

	for i := range 31 {
		d := tr.Begin(layer(i, key, true, true, client))
		read(t, d, 10)
		c.add(100 * time.Millisecond)
		d.End(OutcomeCompleted, nil)
		if s := tr.snapshot(); len(s.Running) != 1 || len(s.Ended) != 0 {
			t.Fatalf("after layer %d: running %d, ended %d", i, len(s.Running), len(s.Ended))
		}
	}
	s := tr.snapshot()
	if e := s.Running[0]; e.Parts != 31 || e.Bytes != 310 || e.Total != nil || e.Unit != key || !e.resolve || e.By != person {
		t.Fatalf("the running unit: %+v", e)
	}
	c.add(proxy.UnitLinger)
	s = tr.snapshot()
	if len(s.Running) != 0 || len(s.Ended) != 1 {
		t.Fatalf("after the linger: running %d, ended %d", len(s.Running), len(s.Ended))
	}
	if f := s.Ended[0]; f.Outcome != OutcomeCompleted || f.Parts != 31 || f.Bytes != 310 || f.DurationMs != 3100 || f.Replica != "quader-packages-1" {
		t.Errorf("the ended unit: %+v", f)
	}

	// A second pull whose fourth layer fails: the unit fails with that file's failure and path.
	var failed string
	for i := range 6 {
		st := layer(100+i, key, true, true, client)
		d := tr.Begin(st)
		if i == 3 {
			failed = st.Path
			d.End(OutcomeFailed, &remotecuration.Failure{Class: remotecuration.ClassNetwork, Code: "upstream_unreachable"})
			continue
		}
		d.End(OutcomeCompleted, nil)
	}
	c.add(proxy.UnitLinger)
	f := tr.snapshot().Ended[0]
	if f.Outcome != OutcomeFailed || f.FailureClass != "network" || f.FailureCode != "upstream_unreachable" || f.FailurePath != failed || f.Parts != 6 {
		t.Errorf("the failed unit: %+v", f)
	}

	// Two files of one version fetched at once are one unit, which ends with its last file; the next file begins a new one.
	npm := proxy.DownloadStart{
		RepoID: 1, Repo: "web/npm-remote", Format: "nuget", Name: "serilog", Version: "4.2.0", Total: 50,
		Cause: client, Unit: "c|1|serilog|4.2.0|" + person,
	}
	a, b := npm, npm
	a.Path, b.Path = "serilog/4.2.0/serilog.4.2.0.nupkg", "symbols/serilog.4.2.0.snupkg"
	da, db := tr.Begin(a), tr.Begin(b)
	if s := tr.snapshot(); len(s.Running) != 1 || s.Running[0].Total == nil || *s.Running[0].Total != 100 {
		t.Fatalf("a version's two files: %+v", s.Running)
	}
	da.End(OutcomeCompleted, nil)
	db.End(OutcomeCancelled, nil)
	if s := tr.snapshot(); len(s.Running) != 0 || len(s.Ended) != 1 || s.Ended[0].Outcome != OutcomeCancelled || s.Ended[0].Parts != 2 {
		t.Fatalf("the version's unit at its last file's end: %+v", s)
	}
}

// R-OPS-049: a queue row's run holds its unit: the unit runs between its files and the planning's documents, its total is what the run
// named (a file that began counts with its length, one found stored leaves the total), the run's failure decides its outcome, and the
// release ends it at once. A run that downloaded nothing shows nowhere.
func TestR_OPS_049_AQueueRowsRunIsOneDownload(t *testing.T) {
	c := newClock()
	tr := NewTracker(TrackerOptions{Replica: "quader-worker-1", Now: c.now})
	key := "q|01937d4d-1a2b-7c3d-9e4f-5a6b7c8d9e0f|job-1"
	cause := proxy.DownloadCause{
		Origin: proxy.DownloadOriginQueue, QueueOrigin: remotecuration.OriginPerson, QueueRow: "01937d4d-1a2b-7c3d-9e4f-5a6b7c8d9e0f",
		Version: "1.20.14", Unit: key, Principal: person,
	}
	nothing := tr.Hold("q|other|job-2")
	nothing.Expect("x", 10)
	nothing.Release()

	h := tr.Hold(key)
	// The planning reads the index before the run names the files: the unit runs, with no total yet.
	index := layer(0, key, false, false, cause)
	index.Version = "1.20.14"
	d := tr.Begin(index)
	read(t, d, 100)
	d.End(OutcomeCompleted, nil)
	c.add(10 * time.Second)
	if s := tr.snapshot(); len(s.Running) != 1 || s.Running[0].Total != nil || s.Running[0].Version != "1.20.14" {
		t.Fatalf("the planning: %+v", s.Running)
	}
	h.Expect(index.Path, 100)
	for i := 1; i <= 4; i++ {
		h.Expect(layer(i, key, false, false, cause).Path, int64(100+i))
	}
	if s := tr.snapshot(); s.Running[0].Total == nil || *s.Running[0].Total != 100+101+102+103+104 {
		t.Fatalf("the named total: %+v", s.Running[0].Total)
	}
	h.Settle(layer(2, key, false, false, cause).Path) // found stored
	for _, i := range []int{1, 3, 4} {
		d := tr.Begin(layer(i, key, false, false, cause))
		read(t, d, 100+i)
		d.End(OutcomeCompleted, nil)
		c.add(time.Minute) // a gap between two files does not end a held unit
		if s := tr.snapshot(); len(s.Running) != 1 || len(s.Ended) != 0 {
			t.Fatalf("between the files: %+v", s)
		}
	}
	h.Fail(remotecuration.Failure{Class: remotecuration.ClassUpstreamNotFound, Code: "upstream_not_found", File: "x/_blobs/sha256:5"})
	h.Release()
	s := tr.snapshot()
	if len(s.Running) != 0 || len(s.Ended) != 1 {
		t.Fatalf("after the release: %+v", s)
	}
	f := s.Ended[0]
	if f.Outcome != OutcomeFailed || f.FailurePath != "x/_blobs/sha256:5" || f.Parts != 4 || f.Bytes != 100+101+103+104 || f.Total == nil ||
		*f.Total != 100+101+103+104 || f.QueueRow == nil || f.QueueOrigin != "person" || f.Version != "1.20.14" || f.Unit != key {
		t.Errorf("the run's unit: %+v total %v", f, f.Total)
	}
}

// fakeResolver answers every query with the version it holds for the path.
type fakeResolver struct {
	versions map[string]string
	asked    int
}

func (r *fakeResolver) ResolveVersions(_ context.Context, qs []VersionQuery) ([]string, error) {
	out := make([]string, len(qs))
	for i, q := range qs {
		r.asked++
		out[i] = r.versions[q.Path]
	}
	return out, nil
}

// R-OPS-049: the publication asks the version of a client's pull of an image until it is found, and never after.
func TestR_OPS_049_APullsVersionIsResolvedByThePublication(t *testing.T) {
	c := newClock()
	tr := NewTracker(TrackerOptions{Replica: "r", Now: c.now})
	sink := &fakeSink{}
	res := &fakeResolver{versions: map[string]string{}}
	p := NewPublisher(PublisherOptions{Tracker: tr, Sink: sink, Resolver: res, Interval: 2 * time.Second, Now: c.now})
	client := proxy.DownloadCause{Origin: proxy.DownloadOriginClient, Principal: person}
	first := layer(0, "c|3|n|"+person, true, true, client)
	tr.Begin(first)
	p.Tick(context.Background())
	if v := sink.pubs[0].Running[0].Version; v != "" || res.asked != 1 {
		t.Fatalf("not found yet: %q, asked %d", v, res.asked)
	}
	res.versions[first.Path] = "1.20.14"
	p.Tick(context.Background())
	p.Tick(context.Background())
	if v := sink.pubs[2].Running[0].Version; v != "1.20.14" || res.asked != 2 {
		t.Fatalf("found: %q, asked %d", v, res.asked)
	}
}

// R-OPS-049, R-OPS-051, R-OPS-052: the read merges the records of one unit's key. A client's pull that two replicas served is one running
// unit with the sum of the bytes, the files and the speeds, and it takes in a finished record of the same key that ended within MergeGap;
// a record of the key that began later than that is a second unit. A queue row's run is one unit whatever the time between its records.
// The outcome of finished records is failed before interrupted before cancelled before completed; the newest 100 units are kept; a stale
// live row is left out; runningCount counts the units and the ones a replica left out.
func TestR_OPS_049_TheReadMergesTheRecordsOfOneUnit(t *testing.T) {
	now := time.Date(2026, 11, 26, 9, 5, 0, 0, time.UTC)
	key := "c|3|n|" + person
	speed := func(n int64) *int64 { return &n }
	total := func(n int64) *int64 { return &n }
	v := View{
		Now: now,
		Live: []Live{
			{Replica: "packages-a", Age: time.Second, Omitted: 2, Downloads: []Entry{
				{ID: "a1", Unit: key, Name: "n", Path: "n/_manifests/sha256:1", Parts: 3, Bytes: 300, StartedAt: now.Add(-20 * time.Second), ElapsedMs: 19000, Speed: speed(10), Origin: "client", By: person},
			}},
			{Replica: "packages-b", Age: 500 * time.Millisecond, Downloads: []Entry{
				{ID: "b1", Unit: key, Name: "n", Version: "1.20.14", Parts: 2, Bytes: 200, StartedAt: now.Add(-15 * time.Second), ElapsedMs: 14500, Speed: speed(5), Origin: "client"},
			}},
			{Replica: "stalled", Age: time.Minute, Downloads: []Entry{{ID: "s1", Unit: "p|s1", Name: "gone"}}},
		},
		Finished: []Finished{
			{ID: "c1", Unit: key, Name: "n", Parts: 4, Bytes: 400, StartedAt: now.Add(-25 * time.Second), FinishedAt: now.Add(-18 * time.Second), Outcome: OutcomeCompleted},
			{ID: "d1", Unit: key, Name: "n", Version: "1.0", Parts: 1, Bytes: 7, StartedAt: now.Add(-time.Hour), FinishedAt: now.Add(-time.Hour + time.Second), Outcome: OutcomeCompleted, Replica: "packages-a"},
			{ID: "q1", Unit: "q|row|job", Name: "img", Parts: 2, Bytes: 20, Total: total(20), StartedAt: now.Add(-10 * time.Minute), FinishedAt: now.Add(-9 * time.Minute), Outcome: OutcomeCompleted, Replica: "worker-a"},
			{ID: "q2", Unit: "q|row|job", Name: "img", Parts: 1, Bytes: 5, Total: total(5), StartedAt: now.Add(-5 * time.Minute), FinishedAt: now.Add(-4 * time.Minute), Outcome: OutcomeInterrupted, Replica: "worker-b"},
			{ID: "q3", Unit: "q|row|job", Name: "img", Parts: 1, Bytes: 1, Total: total(1), StartedAt: now.Add(-4 * time.Minute), FinishedAt: now.Add(-3 * time.Minute), Outcome: OutcomeFailed, FailureClass: "network", FailurePath: "img/_blobs/x", Replica: "worker-b"},
		},
	}
	u := Merge(v, 2*time.Second)
	if len(u.Running) != 1 || u.RunningCount != 3 {
		t.Fatalf("running %d, count %d", len(u.Running), u.RunningCount)
	}
	r := u.Running[0]
	if r.ID != "c1" || r.Bytes != 900 || r.Parts != 9 || r.Speed == nil || *r.Speed != 15 || r.Version != "1.20.14" || r.Replica != "packages-a" ||
		r.Total != nil || r.Elapsed < 25*time.Second || !r.StartedAt.Equal(now.Add(-25*time.Second)) || r.By != "" {
		t.Errorf("the merged pull: %+v", r)
	}
	if len(u.Finished) != 2 {
		t.Fatalf("finished: %+v", u.Finished)
	}
	q := u.Finished[0]
	if q.ID != "q1" || q.Outcome != OutcomeFailed || q.FailurePath != "img/_blobs/x" || q.Bytes != 26 || q.Parts != 4 || q.Total == nil || *q.Total != 26 ||
		!q.FinishedAt.Equal(now.Add(-3*time.Minute)) || q.DurationMs != 7*60*1000 || q.Replica != "worker-b" {
		t.Errorf("the queue row's run: %+v", q)
	}
	if d := u.Finished[1]; d.ID != "d1" || d.Version != "1.0" {
		t.Errorf("the pull of an hour ago: %+v", d)
	}

	// The newest 100 units are kept.
	v = View{Now: now}
	for i := range MaxFinished + 5 {
		at := now.Add(-time.Duration(i) * time.Minute)
		v.Finished = append(v.Finished, Finished{ID: fmt.Sprintf("f%03d", i), Unit: fmt.Sprintf("p|f%03d", i), StartedAt: at, FinishedAt: at, Outcome: OutcomeCompleted})
	}
	if u := Merge(v, 2*time.Second); len(u.Finished) != MaxFinished || u.Finished[0].ID != "f000" || u.Finished[MaxFinished-1].ID != "f099" {
		t.Errorf("the newest 100: %d", len(u.Finished))
	}
}
