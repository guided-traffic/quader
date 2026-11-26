package queue

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/formats/versionkey"
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

// R-CUR-011, R-OPS-049: a client's failure is written once per download and minute: the second failure of one tarball and the failure of
// a second layer of one image within the minute are throttled; a 404 is the negative cache's; a file of no name is a download of its own.
func TestR_CUR_011_AClientsFailureIsWrittenOncePerDownloadAndMinute(t *testing.T) {
	f := newFixture(t, DefaultSettings())
	fail := remotecuration.Failure{Class: remotecuration.ClassUpstreamError, Step: remotecuration.StepStatus, Status: 502}
	f.svc.ClientFailure(context.Background(), 1, "left-pad", "1.0.0", "left-pad/-/left-pad-1.0.0.tgz", fail)
	f.svc.ClientFailure(context.Background(), 1, "left-pad", "1.0.0", "left-pad/-/left-pad-1.0.0.tgz", fail)
	f.svc.ClientFailure(context.Background(), 1, "a", "1", "a/-/a-1.tgz", remotecuration.Failure{Class: remotecuration.ClassUpstreamNotFound})
	if len(f.store.written) != 1 {
		t.Fatalf("%d writes, want one: the second within the minute is throttled, a 404 is the negative cache's", len(f.store.written))
	}
	f.svc.ClientFailure(context.Background(), 1, "library/ubuntu", "", "library/ubuntu/_blobs/sha256:01", fail)
	f.svc.ClientFailure(context.Background(), 1, "library/ubuntu", "", "library/ubuntu/_blobs/sha256:02", fail)
	f.svc.ClientFailure(context.Background(), 1, "", "", "dists/x/by-hash/SHA256/01", fail)
	f.svc.ClientFailure(context.Background(), 1, "", "", "dists/x/by-hash/SHA256/02", fail)
	if len(f.store.written) != 4 || f.store.written[1].Name != "library/ubuntu" || f.store.written[3].Name != "dists/x/by-hash/SHA256/02" {
		t.Fatalf("an image's two layers are one write, two files of no name two: %+v", f.store.written)
	}
	f.store.written = f.store.written[:1]
	if w := f.store.written[0]; w.Failure.File != "left-pad/-/left-pad-1.0.0.tgz" || w.Failure.Attempt != 1 {
		t.Errorf("the written failure: %+v", w.Failure)
	}
	f.now = f.now.Add(time.Minute)
	f.svc.ClientFailure(context.Background(), 1, "left-pad", "1.0.0", "left-pad/-/left-pad-1.0.0.tgz", fail)
	if len(f.store.written) != 2 {
		t.Errorf("%d writes after a minute, want two", len(f.store.written))
	}
}

// fakePlanner names the versions of every package and the packages of one section. It takes the versions as the planner of a worker
// does, by a Picker over testOrder: a version with a "-" is a pre-release.
type fakePlanner struct {
	versions []PlannedVersion
	packages []string
}

func (p fakePlanner) Versions(_ context.Context, _ int64, _ string, sel Selection) ([]PlannedVersion, error) {
	pick := Picker{Selection: sel, Order: testOrder}
	var out []PlannedVersion
	for _, v := range p.versions {
		take, full := pick.Take(v.Version, strings.Contains(v.Version, "-"))
		if full {
			break
		}
		if take {
			out = append(out, v)
		}
	}
	return out, nil
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

// testOrder is a format's order as a range reads it: MAJOR.MINOR.PATCH with an optional pre-release after a "-", which sorts below its
// release; a missing part is 0 for the canon, so that "5.4" and "5.4.0" meet as NuGet's "1.0" and "1.0.0" do; anything else is a tag
// the canon accepts that is no version (the class NotVersion, as OCI's "latest").
var testOrder = remotecuration.Order{View: testView{}, Canon: testCanon{}}

type (
	testView  struct{}
	testCanon struct{}
)

func testParse(v string) (nums [3]int, pre string, ok bool) {
	core, pre, _ := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) > 3 {
		return nums, "", false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nums, "", false
		}
		nums[i] = n
	}
	return nums, pre, true
}

func (testCanon) Canonical(v string) (string, bool) {
	nums, pre, ok := testParse(v)
	if !ok {
		return v, v != ""
	}
	c := fmt.Sprintf("%d.%d.%d", nums[0], nums[1], nums[2])
	if pre != "" {
		c += "-" + pre
	}
	return c, true
}

func (testView) Compare(a, b string) int {
	na, pa, oka := testParse(a)
	nb, pb, okb := testParse(b)
	switch {
	case !oka && !okb:
		return strings.Compare(a, b)
	case !oka:
		return -1
	case !okb:
		return 1
	}
	for i := range na {
		if c := cmp.Compare(na[i], nb[i]); c != 0 {
			return c
		}
	}
	switch {
	case pa == pb:
		return strings.Compare(a, b)
	case pa == "":
		return 1
	case pb == "":
		return -1
	}
	return strings.Compare(pa, pb)
}

func (testView) SortKey(v string) []byte {
	_, _, ok := testParse(v)
	return versionkey.Of(nil, ok, v)
}

// The range of a package's bulk (R-CUR-010, Q-CUR-190 to Q-CUR-192): both bounds, each alone, a bound included or below, bounds the
// upstream does not list, two spellings of one version, strings that are no version, and the composition with the pre-releases, the
// newest maxVersions and maxBytes. Unit tier, milliseconds, seven version strings.
func TestR_CUR_010_ABulkTakesARangeOfVersionsInTheFormatsOrder(t *testing.T) {
	listed := []PlannedVersion{{"6.0.0-rc.1", 10}, {"5.4.7", 10}, {"5.4.0", 10}, {"5.0.0", 10}, {"4.2.0", 10}, {"main", 10}, {"latest", 10}}
	big := Settings{PerRepository: 2, MaxWaiting: 100, MaxAttempts: 5}
	for _, tc := range []struct {
		name     string
		rng      remotecuration.Range
		noPre    bool
		max      int
		maxBytes int64
		want     []string
		bound    string
	}{
		{name: "no bound takes every string, versions or not", want: []string{"6.0.0-rc.1", "5.4.7", "5.4.0", "5.0.0", "4.2.0", "main", "latest"}},
		{name: "from a version, included", rng: remotecuration.Range{From: "5.0.0"}, want: []string{"6.0.0-rc.1", "5.4.7", "5.4.0", "5.0.0"}},
		{name: "up to a version, included", rng: remotecuration.Range{To: "5.4.7", ToInclusive: true}, want: []string{"5.4.7", "5.4.0", "5.0.0", "4.2.0"}},
		{name: "below a version", rng: remotecuration.Range{To: "5.4.7"}, want: []string{"5.4.0", "5.0.0", "4.2.0"}},
		{name: "both bounds", rng: remotecuration.Range{From: "5.0.0", To: "5.4.7", ToInclusive: true}, want: []string{"5.4.7", "5.4.0", "5.0.0"}},
		{name: "bounds the upstream does not list", rng: remotecuration.Range{From: "5.1.0", To: "5.5.0"}, want: []string{"5.4.7", "5.4.0"}},
		{name: "a bound in another spelling meets its version", rng: remotecuration.Range{From: "5.4", To: "5.4", ToInclusive: true}, want: []string{"5.4.0"}},
		{name: "the range first, then the pre-releases", rng: remotecuration.Range{From: "5.0.0"}, noPre: true, want: []string{"5.4.7", "5.4.0", "5.0.0"}},
		{name: "then the newest maxVersions", rng: remotecuration.Range{From: "4.0.0"}, noPre: true, max: 2, want: []string{"5.4.7", "5.4.0"}},
		{name: "then maxBytes", rng: remotecuration.Range{To: "5.4.7", ToInclusive: true}, maxBytes: 25, want: []string{"5.4.7", "5.4.0"}, bound: CodeBulkMaxBytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, big)
			f.svc.o.Planner = fakePlanner{versions: listed}
			spec := BulkSpec{
				RepoID: 1, Level: LevelPackage, Subject: "acme", MaxVersions: 100, MaxBytes: 1 << 30, IncludePrereleases: !tc.noPre, Range: tc.rng,
			}
			if tc.max > 0 {
				spec.MaxVersions = tc.max
			}
			if tc.maxBytes > 0 {
				spec.MaxBytes = tc.maxBytes
			}
			// The API stores a bound in its canonical form.
			spec.Range.From, _ = testOrder.Rank(spec.Range.From)
			spec.Range.To, _ = testOrder.Rank(spec.Range.To)
			b, err := f.svc.StartBulk(context.Background(), nil, spec)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.svc.Dispatch(context.Background(), func(error) remotecuration.Failure { return remotecuration.Failure{} }); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, r := range f.store.rows {
				got = append(got, r.Version)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("queued %v, want %v", got, tc.want)
			}
			stored, _, _, _ := f.store.GetBulk(context.Background(), b.ID, 1)
			want := spec.Range
			want.ToInclusive = want.To == "" || want.ToInclusive // a range without an upper bound keeps the column's default
			if stored.Range() != want {
				t.Errorf("the bulk keeps the range %+v, want %+v", stored.Range(), want)
			}
			code := ""
			if stored.Failure != nil {
				code = stored.Failure.Code
			}
			if code != tc.bound {
				t.Errorf("the bulk's bound %q, want %q", code, tc.bound)
			}
		})
	}
	t.Run("a range counts the strings it leaves out because they are no version", func(t *testing.T) {
		for _, tc := range []struct {
			rng  remotecuration.Range
			want int
		}{{remotecuration.Range{}, 0}, {remotecuration.Range{From: "1.0.0"}, 2}} {
			p := Picker{Selection: Selection{Range: tc.rng, IncludePrereleases: true}, Order: testOrder}
			for _, v := range listed {
				p.Take(v.Version, false)
			}
			if p.NotVersions != tc.want {
				t.Errorf("range %+v: %d strings that are no version, want %d", tc.rng, p.NotVersions, tc.want)
			}
		}
	})
	t.Run("a bound is a version of the format, and a range holds one", func(t *testing.T) {
		if _, ok := testOrder.Rank("latest"); ok {
			t.Error("latest is ranked")
		}
		for _, tc := range []struct {
			rng   remotecuration.Range
			empty bool
		}{
			{remotecuration.Range{From: "5.0.0", To: "4.0.0", ToInclusive: true}, true},
			{remotecuration.Range{From: "5.0.0", To: "5.0.0"}, true},
			{remotecuration.Range{From: "5.0.0", To: "5.0.0", ToInclusive: true}, false},
			{remotecuration.Range{From: "5.0.0"}, false},
		} {
			if got := tc.rng.Empty(testOrder); got != tc.empty {
				t.Errorf("range %+v empty %v, want %v", tc.rng, got, tc.empty)
			}
		}
	})
}
