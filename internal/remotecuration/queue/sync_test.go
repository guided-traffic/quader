package queue

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/remotecuration"
)

// Downloads kept in sync (remote-curation.md R-CUR-027) over the store in memory and a fixed clock. Unit tier, milliseconds, no data.

// The syncs in memory.

func (t memTx) InsertSync(_ context.Context, s Sync) (Sync, bool, error) {
	for _, x := range t.m.syncs {
		if x.RepoID == s.RepoID && x.Level == s.Level && x.Subject == s.Subject {
			return Sync{}, false, nil
		}
	}
	t.m.seq++
	s.ID = fmt.Sprintf("sync-%03d", t.m.seq)
	s.CreatedAt = t.m.now()
	c := s
	t.m.syncs[s.ID] = &c
	return c, true, nil
}

func (t memTx) SyncOfSubject(_ context.Context, repoID int64, level Level, subject string) (Sync, bool, error) {
	for _, x := range t.m.syncs {
		if x.RepoID == repoID && x.Level == level && x.Subject == subject {
			return *x, true, nil
		}
	}
	return Sync{}, false, nil
}

func (t memTx) SyncCount(_ context.Context, repoID int64) (n int, err error) {
	for _, x := range t.m.syncs {
		if x.RepoID == repoID {
			n++
		}
	}
	return n, nil
}

func (t memTx) LockSync(_ context.Context, _ int64, id string) (Sync, bool, error) {
	x, ok := t.m.syncs[id]
	if !ok {
		return Sync{}, false, nil
	}
	return *x, true, nil
}

func (t memTx) LockDueSync(_ context.Context, id string, now time.Time) (Sync, bool, error) {
	x, ok := t.m.syncs[id]
	if !ok || x.Paused || x.NextRunAt.After(now) {
		return Sync{}, false, nil
	}
	return *x, true, nil
}

func (t memTx) SyncRan(_ context.Context, id string, now, next time.Time, outcome SyncOutcome) error {
	x := t.m.syncs[id]
	x.LastRunAt, x.NextRunAt, x.LastOutcome = &now, next, outcome
	return nil
}

func (t memTx) SetSyncPaused(_ context.Context, _ int64, id string, paused bool) (Sync, bool, error) {
	x, ok := t.m.syncs[id]
	if !ok {
		return Sync{}, false, nil
	}
	x.Paused = paused
	return *x, true, nil
}

func (t memTx) DeleteSync(_ context.Context, _ int64, id string) error {
	delete(t.m.syncs, id)
	for _, b := range t.m.bulks {
		if b.SyncID == id {
			b.SyncID = ""
		}
	}
	return nil
}

func (t memTx) LastRun(_ context.Context, syncID string) (Bulk, bool, error) {
	var last *Bulk
	for _, b := range t.m.bulks {
		if b.SyncID == syncID && (last == nil || b.ID > last.ID) {
			last = b
		}
	}
	if last == nil {
		return Bulk{}, false, nil
	}
	return *last, true, nil
}

func (t memTx) StoredBytes(_ context.Context, repoID int64) (int64, error) {
	return t.m.usage[repoID], nil
}

func (m *memStore) ListSyncs(context.Context, BulkFilter) ([]Sync, map[string]Bulk, map[string]map[string]int64, error) {
	return nil, nil, nil, nil
}

func (m *memStore) GetSync(_ context.Context, _ int64, id string) (Sync, *Bulk, map[string]int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	x, ok := m.syncs[id]
	if !ok {
		return Sync{}, nil, nil, false, nil
	}
	b, found, _ := memTx{m}.LastRun(context.Background(), id)
	if !found {
		return *x, nil, nil, true, nil
	}
	return *x, &b, nil, true, nil
}

func (m *memStore) DueSyncs(_ context.Context, now time.Time) ([]Sync, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Sync
	for _, x := range m.syncs {
		if !x.Paused && !x.NextRunAt.After(now) {
			out = append(out, Sync{ID: x.ID, RepoID: x.RepoID})
		}
	}
	slices.SortFunc(out, func(a, b Sync) int { return compareStrings(a.ID, b.ID) })
	return out, nil
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (m *memStore) SyncGauge(context.Context) ([]GaugeRow, error) { return nil, nil }

// fakeRepositories is the repository's side of a sync: its state and the versions it stores as version rows.
type fakeRepositories struct {
	state SyncState
	fixed []string
}

func (f *fakeRepositories) SyncState(context.Context, int64) (SyncState, error) { return f.state, nil }

func (f *fakeRepositories) Fixed(_ context.Context, _ int64, _ string, versions []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, v := range versions {
		if slices.Contains(f.fixed, v) {
			out[v] = true
		}
	}
	return out, nil
}

// refusing is a decider that delivers every version but the ones it names.
type refusing []string

func (r refusing) Decide(_ context.Context, _, _, version string) remotecuration.Decision {
	if slices.Contains(r, version) {
		return remotecuration.Decision{Reason: remotecuration.ReasonBlocked}
	}
	return remotecuration.Delivered()
}

func (r refusing) Package(context.Context, string, string) remotecuration.Decision {
	return remotecuration.Delivered()
}

type syncFixture struct {
	*fixture
	repos *fakeRepositories
	runs  []SyncRun
}

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()
	f := &syncFixture{
		fixture: newFixture(t, Settings{PerRepository: 2, MaxWaiting: 100, MaxAttempts: 5, MaxSyncs: 2}),
		repos:   &fakeRepositories{state: SyncState{Found: true, Runnable: true, Interval: time.Hour}},
	}
	f.svc.o.Syncs = f.repos
	f.svc.o.Planner = fakePlanner{versions: []PlannedVersion{{"2.0.0", 10}, {"1.0.0", 10}}}
	f.svc.o.OnSyncRun = func(_ context.Context, _ meta.Tx, run SyncRun) error {
		f.runs = append(f.runs, run)
		return nil
	}
	return f
}

func (f *syncFixture) dispatch() DispatchResult {
	f.t.Helper()
	res, err := f.svc.Dispatch(context.Background(), func(error) remotecuration.Failure { return remotecuration.Failure{} })
	if err != nil {
		f.t.Fatal(err)
	}
	return res
}

func (f *syncFixture) sync(id string) Sync {
	f.t.Helper()
	s, _, _, found, err := f.store.GetSync(context.Background(), 1, id)
	if err != nil || !found {
		f.t.Fatalf("sync %s: found %v, %v", id, found, err)
	}
	return s
}

// endRuns ends every bulk as the dispatch's settle does once their rows ended.
func (f *syncFixture) endRuns() {
	for _, b := range f.store.bulks {
		b.State = BulkDone
	}
}

func TestR_CUR_027_ADueSyncStartsABulkOnceAtATime(t *testing.T) {
	ctx := context.Background()
	spec := SyncSpec{RepoID: 1, Level: LevelPackage, Subject: "@acme/a", MaxVersions: 2, MaxBytes: 1 << 30, CreatedBy: "maintainer"}

	t.Run("the first run starts at once, the next an interval later, never two at once", func(t *testing.T) {
		f := newSyncFixture(t)
		sy, b, err := f.svc.CreateSync(ctx, nil, spec)
		if err != nil {
			t.Fatal(err)
		}
		if b.Origin != remotecuration.OriginSync || b.SyncID != sy.ID || b.StartedBy != "maintainer" || b.RowOrigin() != remotecuration.OriginSync {
			t.Fatalf("first run %+v, want a bulk of the origin sync started by its creator", b)
		}
		if !sy.NextRunAt.Equal(f.now.Add(time.Hour)) || sy.LastOutcome != SyncStarted {
			t.Fatalf("sync %+v, want the next run an hour later", sy)
		}
		if res := f.dispatch(); res.Syncs != 0 {
			t.Fatalf("a dispatch before the time looked at %d syncs", res.Syncs)
		}
		f.now = f.now.Add(time.Hour)
		if res := f.dispatch(); res.Syncs != 1 || res.SyncRuns != 0 {
			t.Fatalf("dispatch %+v, want one look and no run while the first run has not ended", res)
		}
		if got := f.sync(sy.ID); got.LastOutcome != SyncOverlapped || !got.NextRunAt.Equal(f.now.Add(time.Minute)) {
			t.Fatalf("sync %+v, want overlapped and a look at the next dispatch", got)
		}
		f.endRuns()
		f.now = f.now.Add(time.Minute)
		if res := f.dispatch(); res.SyncRuns != 1 {
			t.Fatalf("dispatch %+v, want the run once the previous one ended", res)
		}
		if len(f.runs) != 1 || f.runs[0].Bulk.StartedBy != "" || f.runs[0].Sync.ID != sy.ID {
			t.Fatalf("runs %+v, want one run of the system", f.runs)
		}
		if got := f.sync(sy.ID); got.LastOutcome != SyncStarted || !got.NextRunAt.Equal(f.now.Add(time.Hour)) {
			t.Fatalf("sync %+v, want started and the next run an interval later", got)
		}
	})

	t.Run("what holds a run, in order, and when it looks again", func(t *testing.T) {
		until := time.Date(2026, 11, 23, 23, 30, 0, 0, time.UTC)
		cases := []struct {
			name    string
			prepare func(f *syncFixture)
			want    SyncOutcome
			next    func(f *syncFixture) time.Time
		}{
			{"disabled", func(f *syncFixture) { f.repos.state.Runnable = false }, SyncDisabled, nil},
			{"paused by the repository", func(f *syncFixture) { f.repos.state.Paused = true }, SyncPausedAll, nil},
			{"limited", func(f *syncFixture) {
				f.store.limits[1] = Limit{RepoID: 1, Since: f.now, Until: until, Waits: 1}
			}, SyncLimited, func(*syncFixture) time.Time { return until }},
			{"bounded", func(f *syncFixture) {
				f.repos.state.MaxBytes = 100
				f.store.usage[1] = 100
			}, SyncBounded, nil},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				f := newSyncFixture(t)
				sy, _, err := f.svc.CreateSync(ctx, nil, spec)
				if err != nil {
					t.Fatal(err)
				}
				f.endRuns()
				f.now = f.now.Add(time.Hour)
				c.prepare(f)
				if res := f.dispatch(); res.SyncRuns != 0 || res.Syncs != 1 {
					t.Fatalf("dispatch %+v, want a look and no run", res)
				}
				got := f.sync(sy.ID)
				want := f.now.Add(time.Hour)
				if c.next != nil {
					want = c.next(f)
				}
				if got.LastOutcome != c.want || !got.NextRunAt.Equal(want) {
					t.Fatalf("sync %+v, want %s and the next look at %s", got, c.want, want)
				}
			})
		}
	})

	t.Run("a paused sync is not looked at; resumed, it runs at the next dispatch", func(t *testing.T) {
		f := newSyncFixture(t)
		sy, _, _ := f.svc.CreateSync(ctx, nil, spec)
		f.endRuns()
		if _, err := f.svc.PauseSync(ctx, nil, 1, sy.ID, true); err != nil {
			t.Fatal(err)
		}
		f.now = f.now.Add(2 * time.Hour)
		if res := f.dispatch(); res.Syncs != 0 {
			t.Fatalf("dispatch %+v looked at a paused sync", res)
		}
		if _, err := f.svc.PauseSync(ctx, nil, 1, sy.ID, false); err != nil {
			t.Fatal(err)
		}
		if res := f.dispatch(); res.SyncRuns != 1 {
			t.Fatalf("dispatch %+v, want the run of the resumed sync", res)
		}
	})

	t.Run("Run now waits for the previous run; one sync per subject; at most MaxSyncs", func(t *testing.T) {
		f := newSyncFixture(t)
		sy, first, _ := f.svc.CreateSync(ctx, nil, spec)
		var re *RunningError
		if _, _, err := f.svc.RunSync(ctx, nil, 1, sy.ID, "maintainer"); !errors.As(err, &re) || re.Bulk.ID != first.ID {
			t.Fatalf("Run now during the first run: %v, want sync_running naming it", err)
		}
		f.endRuns()
		got, b, err := f.svc.RunSync(ctx, nil, 1, sy.ID, "maintainer")
		if err != nil || b.StartedBy != "maintainer" || !got.NextRunAt.Equal(f.now.Add(time.Hour)) {
			t.Fatalf("Run now: %+v %+v %v", got, b, err)
		}
		var se *SyncedError
		if _, _, err := f.svc.CreateSync(ctx, nil, spec); !errors.As(err, &se) || se.Sync.ID != sy.ID {
			t.Fatalf("a second sync of the subject: %v, want already_synced naming the first", err)
		}
		other := spec
		other.Subject = "@acme/b"
		if _, _, err := f.svc.CreateSync(ctx, nil, other); err != nil {
			t.Fatal(err)
		}
		other.Subject = "@acme/c"
		var tm *TooManySyncsError
		if _, _, err := f.svc.CreateSync(ctx, nil, other); !errors.As(err, &tm) || tm.Max != 2 {
			t.Fatalf("the third sync of MaxSyncs 2: %v", err)
		}
		gone, err := f.svc.DeleteSync(ctx, nil, 1, sy.ID)
		if err != nil || gone.ID != sy.ID || f.store.bulks[b.ID].SyncID != "" {
			t.Fatalf("remove: %+v %v; the run %+v stays as a bulk", gone, err, f.store.bulks[b.ID])
		}
	})
}

func TestR_CUR_027_ASyncsExpansionLeavesOutWhatIsStoredAndWhatIsNotDelivered(t *testing.T) {
	ctx := context.Background()
	vs := []PlannedVersion{{"5.0.0", 10}, {"4.0.0", 10}, {"3.0.0", 10}, {"2.0.0", 10}, {"1.0.0", 10}}
	run := func(t *testing.T, start func(f *syncFixture) Bulk) (*syncFixture, Bulk) {
		t.Helper()
		f := newSyncFixture(t)
		f.svc.o.Planner = fakePlanner{versions: vs}
		f.svc.o.Decider = refusing{"3.0.0"}
		f.repos.fixed = []string{"4.0.0", "1.0.0"}
		b := start(f)
		f.dispatch()
		got, _, _, _ := f.store.GetBulk(ctx, b.ID, 1)
		return f, got
	}
	queued := func(f *syncFixture) []string {
		var out []string
		for _, r := range f.store.rows {
			out = append(out, r.Version)
		}
		return out
	}

	t.Run("a sync's run of the newest four queues the new ones the rules deliver", func(t *testing.T) {
		f, b := run(t, func(f *syncFixture) Bulk {
			_, b, err := f.svc.CreateSync(ctx, nil, SyncSpec{RepoID: 1, Level: LevelPackage, Subject: "@acme/a", MaxVersions: 4, MaxBytes: 1 << 30})
			if err != nil {
				t.Fatal(err)
			}
			return b
		})
		if got := queued(f); !slices.Equal(got, []string{"5.0.0", "2.0.0"}) {
			t.Fatalf("queued %v, want 5.0.0 and 2.0.0: 4.0.0 is stored, 3.0.0 not delivered, 1.0.0 not among the newest four", got)
		}
		if b.VersionsFound != 4 || b.VersionsQueued != 2 || b.VersionsRefused != 1 || b.State != BulkQueued {
			t.Fatalf("run %+v, want four found, two queued, one refused", b)
		}
		for _, r := range f.store.rows {
			if r.Origin != remotecuration.OriginSync {
				t.Errorf("row %+v, want the origin sync", r)
			}
		}
	})
	t.Run("a person's bulk of the same leaves out nothing", func(t *testing.T) {
		f, b := run(t, func(f *syncFixture) Bulk {
			b, err := f.svc.StartBulk(ctx, nil, BulkSpec{RepoID: 1, Level: LevelPackage, Subject: "@acme/a", MaxVersions: 4, MaxBytes: 1 << 30})
			if err != nil {
				t.Fatal(err)
			}
			return b
		})
		if got := queued(f); len(got) != 4 || b.VersionsRefused != 0 {
			t.Fatalf("queued %v, refused %d, want the newest four", got, b.VersionsRefused)
		}
	})
}
