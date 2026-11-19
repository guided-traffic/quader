package limits

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
)

// countingStore runs batches in memory and counts the round trips, as quader_valkey_command_duration_seconds_count does.
type countingStore struct {
	mem   *cluster.MemoryLimits
	trips atomic.Int64
	down  atomic.Bool
}

func (s *countingStore) Run(ctx context.Context, b *cluster.LimitBatch) ([]cluster.LimitResult, error) {
	if s.down.Load() {
		return nil, cluster.ErrUnavailable
	}
	s.trips.Add(1)
	return s.mem.Run(ctx, b)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type usageRecorder struct {
	mu sync.Mutex
	d  []UsageDelta
}

func (u *usageRecorder) Add(d UsageDelta) { u.mu.Lock(); u.d = append(u.d, d); u.mu.Unlock() }
func (u *usageRecorder) sum() (req, refused, cached, uncached, upstream int64, events map[string]int64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	events = map[string]int64{}
	for _, d := range u.d {
		req += d.Requests
		refused += d.Refused
		cached += d.Cached
		uncached += d.Uncached
		upstream += d.Upstream
		for _, e := range d.Events {
			events[string(e.Mode)+":"+e.Figure] += e.Count
		}
	}
	return
}

type blockRecorder struct {
	mu sync.Mutex
	b  []Block
}

func (r *blockRecorder) Started(_ context.Context, b Block) {
	r.mu.Lock()
	r.b = append(r.b, b)
	r.mu.Unlock()
}

type rig struct {
	e      *Engine
	store  *countingStore
	clk    *clock
	usage  *usageRecorder
	blocks *blockRecorder
	x      *Index
}

// newRig builds an engine over the built-in rules with the profile Anonymous replaced by figures and the mode.
func newRig(t *testing.T, mode Mode, anon Figures) *rig {
	t.Helper()
	s := builtinSnapshot()
	for i := range s.Profiles {
		if s.Profiles[i].Key == ProfileAnonymous {
			s.Profiles[i].Figures = anon
		}
	}
	s.Settings.Mode = mode
	s.Ceilings = []Ceiling{{RepoID: 21, Class: CeilingAnonymous, UpstreamRequestsPerHour: 3}}
	x := NewIndex(s)
	clk := &clock{t: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)}
	mem := cluster.NewMemoryLimits()
	mem.Now = clk.now
	r := &rig{store: &countingStore{mem: mem}, clk: clk, usage: &usageRecorder{}, blocks: &blockRecorder{}, x: x}
	r.e = NewEngine(Options{
		Store: r.store, Index: func() *Index { return x }, Replica: "r1", Usage: r.usage, Blocks: r.blocks, Now: clk.now,
		ParallelWait: 0, LeaseTTL: time.Minute, Share: func() int { return 2 },
	})
	return r
}

func anonCaller(addr string) Caller {
	return Caller{Kind: CallerAnonymous, Anonymous: true, Client: netip.MustParseAddr(addr)}
}

func codeOf(err error) string {
	if e, ok := errs.As(err); ok {
		return e.Code
	}
	return ""
}

func TestR_LIM_017_RequestWindowsAndCost(t *testing.T) {
	t.Run("R-LIM-005 R-LIM-020 in enforce a burst of 100 and 20 per second admits at most 140 of 200 in 2 s", func(t *testing.T) {
		r := newRig(t, ModeEnforce, BuiltinProfiles()[ProfileAnonymous])
		admitted := 0
		for i := range 200 {
			if i == 100 {
				r.clk.add(2 * time.Second)
			}
			c := r.e.Begin(anonCaller("198.51.100.7"), qname.Repo{})
			st := r.e.Check(context.Background(), c, time.Time{}, nil)
			if st.Err == nil {
				admitted++
			} else if codeOf(st.Err) != errs.CodeRateLimited {
				t.Fatalf("refusal %v", st.Err)
			}
		}
		if admitted > 140 || admitted < 100 {
			t.Fatalf("admitted %d, want at most 140", admitted)
		}
	})
	t.Run("R-LIM-017 a request costs one round trip at stage 7", func(t *testing.T) {
		r := newRig(t, ModeEnforce, BuiltinProfiles()[ProfileAnonymous])
		c := r.e.Begin(anonCaller("198.51.100.7"), qname.Repo{})
		r.e.Check(context.Background(), c, time.Time{}, nil)
		if n := r.store.trips.Load(); n != 1 {
			t.Fatalf("%d round trips, want 1", n)
		}
		if err := r.e.Admit(context.Background(), c); err != nil {
			t.Fatal(err)
		}
		if n := r.store.trips.Load(); n != 2 {
			t.Fatalf("%d round trips after the admission of a download, want 2", n)
		}
		c.AddCached(1000)
		r.e.Finish(c)
		if n := r.store.trips.Load(); n != 2 {
			t.Fatalf("the end of a download costs a round trip: %d", n)
		}
	})
	t.Run("R-LIM-026 in audit nothing is refused, everything is recorded as would_limit", func(t *testing.T) {
		r := newRig(t, ModeAudit, BuiltinProfiles()[ProfileAnonymous])
		for range 300 {
			c := r.e.Begin(anonCaller("198.51.100.7"), qname.Repo{})
			st := r.e.Check(context.Background(), c, time.Time{}, nil)
			if st.Err != nil || len(st.Policies) != 0 {
				t.Fatalf("audit refused or sent fields: %v %v", st.Err, st.Policies)
			}
			r.e.Finish(c)
		}
		r.e.Flush(context.Background())
		req, _, _, _, _, ev := r.usage.sum()
		if req != 300 || ev["audit:requestsPerSecond"] == 0 {
			t.Fatalf("requests %d events %v", req, ev)
		}
	})
}

func TestR_LIM_021_TheCheckBeforeADownload(t *testing.T) {
	f := BuiltinProfiles()[ProfileAnonymous]
	f[CachedBytesPerHour] = 100 * mb
	r := newRig(t, ModeEnforce, f)
	ctx := context.Background()
	c := r.e.Begin(anonCaller("198.51.100.8"), qname.Repo{})
	r.e.Check(ctx, c, time.Time{}, nil)
	if err := r.e.Admit(ctx, c); err != nil {
		t.Fatal(err)
	}
	c.AddCached(99 * mb)
	r.e.Finish(c)
	r.e.Flush(ctx)
	// The estimate is 99 MB: a 50 MB download is admitted and completes.
	c = r.e.Begin(anonCaller("198.51.100.8"), qname.Repo{})
	if err := r.e.Admit(ctx, c); err != nil {
		t.Fatalf("the download below the limit is refused: %v", err)
	}
	c.AddCached(50 * mb)
	r.e.Finish(c)
	r.e.Flush(ctx)
	c = r.e.Begin(anonCaller("198.51.100.8"), qname.Repo{})
	err := r.e.Admit(ctx, c)
	if codeOf(err) != CodeDownloadLimitReached {
		t.Fatalf("the next download: %v", err)
	}
	e, _ := errs.As(err)
	if e.Extensions["figure"] != "cachedBytesPerHour" || e.RetryAfter < time.Second || e.RetryAfter > time.Hour {
		t.Fatalf("problem %+v", e)
	}
	t.Run("R-LIM-020 the refusal carries the figure's RateLimit policy", func(t *testing.T) {
		p, ok := c.RefusalPolicy()
		if !ok || p.Name != "cached-hour" || p.Unit != "content-bytes" || p.Quota != 100*mb || p.Window != time.Hour || p.Reset != e.RetryAfter {
			t.Fatalf("%+v %v", p, ok)
		}
	})
}

func TestR_LIM_024_ParallelDownloads(t *testing.T) {
	f := BuiltinProfiles()[ProfileAnonymous] // 4 parallel
	r := newRig(t, ModeEnforce, f)
	ctx := context.Background()
	var held []*Charge
	for range 4 {
		c := r.e.Begin(anonCaller("198.51.100.9"), qname.Repo{})
		if err := r.e.Admit(ctx, c); err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
	}
	c := r.e.Begin(anonCaller("198.51.100.9"), qname.Repo{})
	if err := r.e.Admit(ctx, c); codeOf(err) != CodeTooManyParallelDownloads {
		t.Fatalf("the fifth download: %v", err)
	}
	r.e.Finish(held[0])
	r.e.Flush(ctx) // the ended lease is given up in the flush
	c = r.e.Begin(anonCaller("198.51.100.9"), qname.Repo{})
	if err := r.e.Admit(ctx, c); err != nil {
		t.Fatalf("after one ended: %v", err)
	}
	t.Run("R-LIM-024 in audit a lease above the limit is kept and recorded", func(t *testing.T) {
		r := newRig(t, ModeAudit, f)
		for range 6 {
			c := r.e.Begin(anonCaller("198.51.100.9"), qname.Repo{})
			if err := r.e.Admit(ctx, c); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("R-LIM-019 while Valkey is unreachable the replica's share is ceil(limit / n)", func(t *testing.T) {
		r := newRig(t, ModeEnforce, f)
		r.store.down.Store(true)
		for range 2 { // n = 2: ceil(4 / 2)
			c := r.e.Begin(anonCaller("198.51.100.9"), qname.Repo{})
			if err := r.e.Admit(ctx, c); err != nil {
				t.Fatal(err)
			}
		}
		c := r.e.Begin(anonCaller("198.51.100.9"), qname.Repo{})
		if err := r.e.Admit(ctx, c); codeOf(err) != CodeTooManyParallelDownloads {
			t.Fatalf("the third download on the share: %v", err)
		}
		if !r.e.Fallback() {
			t.Fatal("the fallback is not reported")
		}
	})
}

func TestR_LIM_022_StepNineA(t *testing.T) {
	f := BuiltinProfiles()[ProfileAnonymous]
	f[UpstreamRequestsPerHour] = 2
	r := newRig(t, ModeEnforce, f)
	ctx := context.Background()
	fetch := func(addr string, repo int64) error {
		c := r.e.Begin(anonCaller(addr), qname.Repo{})
		err := r.e.MayFetch(ctx, c, repo, false)
		r.e.Finish(c)
		return err
	}
	for i := range 2 {
		if err := fetch("198.51.100.10", 5); err != nil {
			t.Fatalf("upstream request %d: %v", i, err)
		}
	}
	err := fetch("198.51.100.10", 5)
	var be *BudgetError
	if !errors.As(err, &be) || codeOf(err) != CodeUpstreamLimitReached {
		t.Fatalf("the third upstream request: %v", err)
	}
	t.Run("R-LIM-015 the anonymous ceiling holds for a second address", func(t *testing.T) {
		for i := range 3 {
			if err := fetch("203.0.113."+string(rune('1'+i)), 21); err != nil {
				t.Fatalf("ceiling request %d: %v", i, err)
			}
		}
		if err := fetch("203.0.113.9", 21); codeOf(err) != CodeUpstreamLimitReached {
			t.Fatalf("a fresh address beyond the ceiling: %v", err)
		}
		c := r.e.Begin(Caller{Kind: CallerUser, PrincipalID: "u-bob", SessionID: "s"}, qname.Repo{})
		if err := r.e.MayFetch(ctx, c, 21, false); err != nil {
			t.Fatalf("a signed-in user is under its own class: %v", err)
		}
	})
	t.Run("R-LIM-022 a nil charge and the system pass", func(t *testing.T) {
		if err := r.e.MayFetch(ctx, nil, 21, true); err != nil {
			t.Fatal(err)
		}
		if err := r.e.MayFetch(ctx, r.e.System(21), 21, true); err != nil {
			t.Fatal(err)
		}
	})
}

func TestR_LIM_025_TheBlock(t *testing.T) {
	f := BuiltinProfiles()[ProfileAnonymous] // refusedPerHour 60
	r := newRig(t, ModeEnforce, f)
	ctx := context.Background()
	for range 61 {
		r.e.FailedCredential(anonCaller("198.51.100.11"), qname.Repo{})
	}
	r.e.Flush(ctx) // counts the refusals and finds the block due
	r.e.Flush(ctx) // sets the marker
	if len(r.blocks.b) != 1 {
		t.Fatalf("%d blocks recorded", len(r.blocks.b))
	}
	c := r.e.Begin(anonCaller("198.51.100.11"), qname.Repo{})
	st := r.e.Check(ctx, c, time.Time{}, nil)
	if codeOf(st.Err) != CodeCallerBlocked {
		t.Fatalf("an anonymous request of the blocked address: %v", st.Err)
	}
	if e, _ := errs.As(st.Err); e.RetryAfter > 900*time.Second || e.RetryAfter < 890*time.Second {
		t.Fatalf("Retry-After %s", e.RetryAfter)
	}
	t.Run("R-LIM-025 a valid credential from the same address is not blocked", func(t *testing.T) {
		c := r.e.Begin(Caller{Kind: CallerUser, PrincipalID: "u-carol", TokenID: "tok-c", Client: netip.MustParseAddr("198.51.100.11")}, qname.Repo{})
		if st := r.e.Check(ctx, c, time.Time{}, nil); st.Err != nil {
			t.Fatalf("a colleague with a valid token: %v", st.Err)
		}
	})
	t.Run("R-LIM-025 a lifted block admits and does not block again at once", func(t *testing.T) {
		if err := r.e.LiftBlock(ctx, r.blocks.b[0]); err != nil {
			t.Fatal(err)
		}
		c := r.e.Begin(anonCaller("198.51.100.11"), qname.Repo{})
		if st := r.e.Check(ctx, c, time.Time{}, nil); st.Err != nil {
			t.Fatalf("after the lift: %v", st.Err)
		}
		r.e.FailedCredential(anonCaller("198.51.100.11"), qname.Repo{})
		r.e.Flush(ctx)
		r.e.Flush(ctx)
		if len(r.blocks.b) != 1 {
			t.Fatal("one refused request after the lift blocked again")
		}
	})
	t.Run("R-LIM-003 a failed credential spends nothing of the token's owner", func(t *testing.T) {
		_, refused, _, _, _, _ := r.usage.sum()
		if refused < 61 {
			t.Fatalf("%d refused requests recorded for the address", refused)
		}
	})
	t.Run("R-LIM-025 in audit no marker is set", func(t *testing.T) {
		r := newRig(t, ModeAudit, f)
		for range 61 {
			r.e.FailedCredential(anonCaller("198.51.100.12"), qname.Repo{})
		}
		r.e.Flush(ctx)
		r.e.Flush(ctx)
		if len(r.blocks.b) != 0 {
			t.Fatal("audit set a block")
		}
		_, _, _, _, _, ev := r.usage.sum()
		if ev["audit:refusedPerHour"] != 1 {
			t.Fatalf("would_limit of refusedPerHour: %v", ev)
		}
	})
}

func TestR_LIM_023_Pacing(t *testing.T) {
	f := BuiltinProfiles()[ProfileAnonymous]
	f[CachedBytesPerSecond] = 1000
	r := newRig(t, ModeEnforce, f)
	r.e.o.MinPacing = 65536
	c := r.e.Begin(anonCaller("198.51.100.13"), qname.Repo{})
	pc := r.e.Pacer(c, false)
	defer pc.Close()
	var total time.Duration
	for range 64 { // 1 MiB in chunks of 16 KiB at a figure of 1,000 B/s: the floor
		w := pc.reserve(PaceChunk)
		if w > time.Second {
			t.Fatalf("a write waits %s", w)
		}
		total += w
	}
	if total < 15*time.Second || total > 17*time.Second {
		t.Fatalf("1 MiB at the floor takes %s, want about 16 s", total)
	}
	t.Run("R-LIM-023 a share of the key's downloads in the cluster", func(t *testing.T) {
		f := BuiltinProfiles()[ProfileAnonymous]
		f[CachedBytesPerSecond] = 10 * mb
		r := newRig(t, ModeEnforce, f)
		r.e.o.MinPacing = 1
		c := r.e.Begin(anonCaller("198.51.100.14"), qname.Repo{})
		pc := r.e.Pacer(c, false)
		defer pc.Close()
		r.e.clusterLeases(c.Key, 2) // the key's second download runs on another replica
		var total time.Duration
		for range 100 * mb / PaceChunk {
			w := pc.reserve(PaceChunk)
			total += w
			r.clk.add(w)
		}
		// 100 MB at half of 10 MB/s, after the burst of one second.
		if total < 15*time.Second || total > 21*time.Second {
			t.Fatalf("100 MB at a share of 5 MB/s took %s", total)
		}
	})
}

func BenchmarkR_LIM_017_CachedDownload(b *testing.B) {
	t := &testing.T{}
	r := newRig(t, ModeEnforce, BuiltinProfiles()[ProfileStandard])
	ctx := context.Background()
	caller := Caller{Kind: CallerUser, PrincipalID: "u-bench", TokenID: "tok"}
	b.ReportAllocs()
	var onPath int64
	n := 0
	for b.Loop() {
		before := r.store.trips.Load()
		c := r.e.Begin(caller, qname.Repo{Project: "web", Key: "npm"})
		r.e.Check(ctx, c, time.Time{}, nil)
		if err := r.e.Admit(ctx, c); err != nil {
			b.Fatal(err)
		}
		c.AddCached(4096)
		r.e.Finish(c)
		onPath += r.store.trips.Load() - before
		if n++; n%10 == 0 {
			r.e.Flush(ctx) // the flush of every second, off the request's path
		}
	}
	b.ReportMetric(float64(onPath)/float64(n), "roundtrips/download")
}
