package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/remotecuration"
)

// The queue's order, bounds and retries (remote-curation.md R-CUR-011) over a store in memory and a fixed clock. Unit tier,
// milliseconds, no data.

type fixture struct {
	t     *testing.T
	store *memStore
	svc   *Service
	now   time.Time
}

func newFixture(t *testing.T, st Settings) *fixture {
	t.Helper()
	f := &fixture{t: t, now: time.Date(2026, 11, 23, 22, 0, 0, 0, time.UTC)}
	f.store = newMemStore(func() time.Time { return f.now })
	svc, err := New(Options{
		Store: f.store, Maintenance: f.store, Settings: func() Settings { return st }, Now: func() time.Time { return f.now },
		Jitter: func() float64 { return 0.5 },
	})
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f
}

func (f *fixture) add(name string, origin remotecuration.Origin) Row {
	f.t.Helper()
	r, err := f.svc.Add(context.Background(), nil, NewRow{RepoID: 1, Name: name, Version: "1.0.0", Origin: origin})
	if err != nil {
		f.t.Fatalf("add %s: %v", name, err)
	}
	return r
}

func (f *fixture) finish(r Row, res Result) settlement {
	f.t.Helper()
	cur := f.store.byID(r.ID)
	s, err := f.svc.Finish(context.Background(), cur, cur.JobID, "web/npm-remote", res)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func TestR_CUR_011_ThePerRepositoryBoundAndTheOrderOfWaitingRows(t *testing.T) {
	f := newFixture(t, Settings{PerRepository: 2, MaxWaiting: 100, MaxAttempts: 5})
	a := f.add("a", remotecuration.OriginPerson)
	b := f.add("b", remotecuration.OriginBulk)
	c := f.add("c", remotecuration.OriginBulk)
	d := f.add("d", remotecuration.OriginProbe)
	e := f.add("e", remotecuration.OriginPerson)
	if s := f.store.byID(a.ID).State; s != remotecuration.StateRunning {
		t.Fatalf("the first row is %s, want running at its insert", s)
	}
	if s := f.store.byID(b.ID).State; s != remotecuration.StateRunning {
		t.Fatalf("the second row is %s, want running: two places", s)
	}
	for _, r := range []Row{c, d, e} {
		if s := f.store.byID(r.ID).State; s != remotecuration.StateWaiting {
			t.Fatalf("row %s is %s beyond the bound of two, want waiting", r.Name, s)
		}
	}
	// The place a's end frees goes to e, a person's row, before the older bulk and probe rows.
	f.finish(a, Result{Outcome: remotecuration.OutcomeCompleted})
	if s := f.store.byID(e.ID).State; s != remotecuration.StateRunning {
		t.Errorf("the person's row is %s after a place freed, want running before the bulk's and the probe's", s)
	}
	if s := f.store.byID(c.ID).State; s != remotecuration.StateWaiting {
		t.Errorf("the bulk's row is %s, want waiting", s)
	}
	// Then by creation: c before d.
	f.finish(b, Result{})
	if s := f.store.byID(c.ID).State; s != remotecuration.StateRunning {
		t.Errorf("the older bulk row is %s, want running", s)
	}
	if s := f.store.byID(d.ID).State; s != remotecuration.StateWaiting {
		t.Errorf("the probe's row is %s, want waiting", s)
	}
}

func TestR_CUR_011_TheBoundOfWaitingRows(t *testing.T) {
	f := newFixture(t, Settings{PerRepository: 1, MaxWaiting: 2, MaxAttempts: 5})
	f.add("a", remotecuration.OriginPerson) // runs
	f.add("b", remotecuration.OriginPerson) // waits
	f.add("c", remotecuration.OriginPerson) // waits
	_, err := f.svc.Add(context.Background(), nil, NewRow{RepoID: 1, Name: "d", Version: "1", Origin: remotecuration.OriginPerson})
	var full *FullError
	if !errors.As(err, &full) || full.Waiting != 2 || !errors.Is(err, ErrFull) {
		t.Fatalf("a third waiting row: %v, want ErrFull with the count 2", err)
	}
	if _, err := f.svc.Add(context.Background(), nil, NewRow{RepoID: 1, Name: "b", Version: "1.0.0", Origin: remotecuration.OriginPerson}); !errors.Is(err, ErrAlreadyQueued) {
		t.Fatalf("a version that waits: %v, want ErrAlreadyQueued", err)
	}
	if active, isFull, err := f.svc.Queued(context.Background(), 1, "d", "1"); err != nil || active || !isFull {
		t.Errorf("Queued of a new version: active %v, full %v, %v; want the bound met", active, isFull, err)
	}
}

func TestR_CUR_011_TheBackoffAndRetryAfter(t *testing.T) {
	for _, c := range []struct {
		attempt    int
		retryAfter time.Duration
		jitter     float64
		want       time.Duration
	}{
		{1, 0, 0.5, 30 * time.Second},
		{2, 0, 0.5, time.Minute},
		{3, 0, 0.5, 2 * time.Minute},
		{1, 0, 0, 24 * time.Second},                  // -20 %
		{1, 0, 1, 36 * time.Second},                  // +20 %
		{20, 0, 0.5, time.Hour},                      // the cap
		{3, 90 * time.Second, 0.5, 90 * time.Second}, // the upstream's Retry-After
	} {
		if got := backoff(c.attempt, c.retryAfter, c.jitter); got != c.want {
			t.Errorf("backoff(%d, %s, %v) = %s, want %s", c.attempt, c.retryAfter, c.jitter, got, c.want)
		}
	}
}

func TestR_CUR_011_ARetryableFailureWaitsUntilTheAttemptsAreSpent(t *testing.T) {
	f := newFixture(t, Settings{PerRepository: 4, MaxWaiting: 100, MaxAttempts: 3})
	r := f.add("a", remotecuration.OriginPerson)
	fail := Result{Failed: true, Failure: &remotecuration.Failure{Class: remotecuration.ClassUpstreamError, Step: remotecuration.StepStatus, Status: 503}}
	s := f.finish(r, fail)
	if s.State != remotecuration.StateWaiting || !s.NotBefore.Equal(f.now.Add(30*time.Second)) {
		t.Fatalf("after attempt 1: %s, next %s; want waiting 30 s", s.State, s.NotBefore)
	}
	// The dispatch starts the row when its time came, not before.
	if res, err := f.svc.Dispatch(context.Background(), nil); err != nil || res.Started != 0 {
		t.Fatalf("a dispatch before the time: %+v, %v", res, err)
	}
	f.now = f.now.Add(31 * time.Second)
	if res, err := f.svc.Dispatch(context.Background(), nil); err != nil || res.Started != 1 {
		t.Fatalf("a dispatch after the time: %+v, %v", res, err)
	}
	f.finish(r, fail)
	f.now = f.now.Add(2 * time.Minute)
	if _, err := f.svc.Dispatch(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if s := f.finish(r, fail); s.State != remotecuration.StateFailed {
		t.Fatalf("after the third of three attempts: %s, want failed", s.State)
	}
	if got := f.store.byID(r.ID); got.Attempts != 3 || got.Failure == nil || got.Failure.Status != 503 {
		t.Errorf("the failed row: attempts %d, failure %+v", got.Attempts, got.Failure)
	}

	t.Run("a failure no attempt overcomes ends the row at once", func(t *testing.T) {
		r := f.add("b", remotecuration.OriginPerson)
		s := f.finish(r, Result{Failed: true, Failure: &remotecuration.Failure{Class: remotecuration.ClassUpstreamAuth, Status: 401}})
		if s.State != remotecuration.StateFailed {
			t.Errorf("upstream_auth: %s, want failed", s.State)
		}
	})
	t.Run("a retry waits again with its attempts reset", func(t *testing.T) {
		got, err := f.svc.Retry(context.Background(), nil, 1, r.ID, "", nil)
		if err != nil || got.State != remotecuration.StateRunning || got.Attempts != 1 {
			t.Fatalf("retry: %+v, %v; want running at its first attempt", got, err)
		}
		if _, err := f.svc.Retry(context.Background(), nil, 1, r.ID, "", nil); !errors.Is(err, ErrNotRetryable) {
			t.Errorf("a retry of a running row: %v, want ErrNotRetryable", err)
		}
	})
	t.Run("a cancelled run ends cancelled, a done run keeps its last refusal", func(t *testing.T) {
		c := f.add("c", remotecuration.OriginPerson)
		if s := f.finish(c, Result{Cancelled: true}); s.State != remotecuration.StateCancelled {
			t.Errorf("cancelled run: %s", s.State)
		}
		d := f.add("d", remotecuration.OriginPerson)
		refusal := &remotecuration.Failure{Class: remotecuration.ClassGateRefused, Step: remotecuration.StepGate, Code: "vulnerability_blocked"}
		s := f.finish(d, Result{Outcome: remotecuration.OutcomeRefused, Files: 1, Refused: 1, Failure: refusal})
		if s.State != remotecuration.StateDone || s.Outcome != remotecuration.OutcomeRefused || s.Failure != refusal {
			t.Errorf("a refused run: %+v", s)
		}
	})
}

func TestR_CUR_011_AClientsFailureIsWrittenOncePerFileAndMinute(t *testing.T) {
	f := newFixture(t, DefaultSettings())
	fail := remotecuration.Failure{Class: remotecuration.ClassUpstreamError, Step: remotecuration.StepStatus, Status: 502}
	f.svc.ClientFailure(context.Background(), 1, "left-pad", "1.0.0", "left-pad/-/left-pad-1.0.0.tgz", fail)
	f.svc.ClientFailure(context.Background(), 1, "left-pad", "1.0.0", "left-pad/-/left-pad-1.0.0.tgz", fail)
	f.svc.ClientFailure(context.Background(), 1, "a", "1", "a/-/a-1.tgz", remotecuration.Failure{Class: remotecuration.ClassUpstreamNotFound})
	if len(f.store.written) != 1 {
		t.Fatalf("%d writes, want one: the second within the minute is throttled, a 404 is the negative cache's", len(f.store.written))
	}
	if w := f.store.written[0]; w.Failure.File != "left-pad/-/left-pad-1.0.0.tgz" || w.Failure.Attempt != 1 {
		t.Errorf("the written failure: %+v", w.Failure)
	}
	f.now = f.now.Add(time.Minute)
	f.svc.ClientFailure(context.Background(), 1, "left-pad", "1.0.0", "left-pad/-/left-pad-1.0.0.tgz", fail)
	if len(f.store.written) != 2 {
		t.Errorf("%d writes after a minute, want two", len(f.store.written))
	}
}

// fakePlanner names the versions of every package and the packages of one section.
type fakePlanner struct {
	versions []PlannedVersion
	packages []string
}

func (p fakePlanner) Versions(_ context.Context, _ int64, _ string, _ bool, maxVersions int) ([]PlannedVersion, error) {
	if maxVersions > 0 && len(p.versions) > maxVersions {
		return p.versions[:maxVersions], nil
	}
	return p.versions, nil
}

func (p fakePlanner) Packages(context.Context, int64, string, string) ([]string, string, bool, error) {
	return p.packages, "", true, nil
}

func TestR_CUR_010_ABulkIsExpandedWithinItsBounds(t *testing.T) {
	vs := []PlannedVersion{{"5.0.0", 10}, {"4.0.0", 10}, {"3.0.0", 10}, {"2.0.0", 0}, {"1.0.0", 10}}
	run := func(t *testing.T, st Settings, spec BulkSpec, planner Planner) (*fixture, Bulk) {
		t.Helper()
		f := newFixture(t, st)
		f.svc.o.Planner = planner
		b, err := f.svc.StartBulk(context.Background(), nil, spec)
		if err != nil {
			t.Fatal(err)
		}
		for range 5 {
			if _, err := f.svc.Dispatch(context.Background(), func(error) remotecuration.Failure { return remotecuration.Failure{} }); err != nil {
				t.Fatal(err)
			}
		}
		got, _, _, _ := f.store.GetBulk(context.Background(), b.ID, 1)
		return f, got
	}
	big := Settings{PerRepository: 2, MaxWaiting: 100, MaxAttempts: 5}

	t.Run("maxVersions", func(t *testing.T) {
		f, b := run(t, big, BulkSpec{RepoID: 1, Level: LevelPackage, Subject: "@acme/a", MaxVersions: 2, MaxBytes: 1 << 30}, fakePlanner{versions: vs})
		if b.State != BulkQueued || b.VersionsQueued != 2 || len(f.store.rows) != 2 {
			t.Fatalf("bulk %+v with %d rows, want queued with two", b, len(f.store.rows))
		}
		for _, r := range f.store.rows {
			if r.Origin != remotecuration.OriginBulk || r.BulkID != b.ID || r.Name != "@acme/a" {
				t.Errorf("row %+v", r)
			}
		}
	})
	t.Run("maxBytes of the announced sizes", func(t *testing.T) {
		_, b := run(t, big, BulkSpec{RepoID: 1, Level: LevelPackage, Subject: "@acme/a", MaxVersions: 100, MaxBytes: 25}, fakePlanner{versions: vs})
		if b.VersionsQueued != 2 || b.Failure == nil || b.Failure.Class != remotecuration.ClassBound || b.Failure.Code != CodeBulkMaxBytes {
			t.Fatalf("bulk %+v, want two versions and the bound noted", b)
		}
	})
	t.Run("the expansion pauses at the bound of waiting rows", func(t *testing.T) {
		st := Settings{PerRepository: 1, MaxWaiting: 2, MaxAttempts: 5}
		_, b := run(t, st, BulkSpec{RepoID: 1, Level: LevelPackage, Subject: "@acme/a", MaxVersions: 100, MaxBytes: 1 << 30}, fakePlanner{versions: vs})
		if b.State != BulkExpanding || b.VersionsQueued != 3 {
			t.Fatalf("bulk %+v, want expanding with three rows (one runs, two wait)", b)
		}
	})
	t.Run("a section expands every package of it", func(t *testing.T) {
		f, b := run(t, big, BulkSpec{RepoID: 1, Level: LevelSection, Subject: "@acme/*", MaxVersions: 100, MaxBytes: 1 << 30},
			fakePlanner{versions: vs[:2], packages: []string{"@acme/a", "@acme/b"}})
		if b.State != BulkQueued || b.VersionsQueued != 4 || len(f.store.rows) != 4 {
			t.Fatalf("bulk %+v with %d rows, want four", b, len(f.store.rows))
		}
	})
	t.Run("without a planner the bulk fails in words", func(t *testing.T) {
		_, b := run(t, big, BulkSpec{RepoID: 1, Level: LevelPackage, Subject: "x", MaxVersions: 1, MaxBytes: 1}, nil)
		if b.State != BulkFailed || b.Failure == nil || b.Failure.Code != CodeCatalogueUnavailable {
			t.Fatalf("bulk %+v", b)
		}
	})
}
