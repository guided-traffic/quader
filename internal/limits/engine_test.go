package limits

import (
	"context"
	"errors"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
)

// countingStore runs batches in memory and counts the round trips, as quader_valkey_command_duration_seconds_count does.
// down refuses every batch without running it (an open circuit, an unreachable Valkey); late runs the batch and then
// answers with a timeout, as a Valkey that held the commands longer than valkey.command_timeout (CLIENT PAUSE) and ran
// them after the client had given up.
type countingStore struct {
	mem   *cluster.MemoryLimits
	trips atomic.Int64
	down  atomic.Bool
	late  atomic.Bool
}

func (s *countingStore) Run(ctx context.Context, b *cluster.LimitBatch) ([]cluster.LimitResult, error) {
	if s.down.Load() {
		return nil, cluster.ErrUnavailable
	}
	s.trips.Add(1)
	res, err := s.mem.Run(ctx, b)
	if s.late.Load() {
		return nil, context.DeadlineExceeded
	}
	return res, err
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
	t.Run("R-LIM-005 R-LIM-020 in enforce a burst of 1,000 and 50 per second admits at most 1,100 of 2,200 in 2 s", func(t *testing.T) {
		r := newRig(t, ModeEnforce, BuiltinProfiles()[ProfileAnonymous])
		admitted := 0
		for i := range 2200 {
			if i == 1100 {
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
		if admitted > 1100 || admitted < 1000 {
			t.Fatalf("admitted %d, want at most 1,100", admitted)
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
		for range 1200 { // beyond the burst of 1,000
			c := r.e.Begin(anonCaller("198.51.100.7"), qname.Repo{})
			st := r.e.Check(context.Background(), c, time.Time{}, nil)
			if st.Err != nil || len(st.Policies) != 0 {
				t.Fatalf("audit refused or sent fields: %v %v", st.Err, st.Policies)
			}
			r.e.Finish(c)
		}
		r.e.Flush(context.Background())
		req, _, _, _, _, ev := r.usage.sum()
		if req != 1200 || ev["audit:requestsPerSecond"] == 0 {
			t.Fatalf("requests %d events %v", req, ev)
		}
	})
}

func TestR_LIM_017_ARefusedRequestFillsNoRequestWindow(t *testing.T) {
	tight := Figures{RequestsPerSecond: 1, RequestsBurst: 1}
	call := func(r *rig) error {
		c := r.e.Begin(anonCaller("198.51.100.7"), qname.Repo{})
		defer r.e.Finish(c)
		return r.e.Check(context.Background(), c, time.Time{}, nil).Err
	}
	t.Run("R-LIM-017 R-LIM-020 a caller at one request a second that retries every second is admitted again", func(t *testing.T) {
		r := newRig(t, ModeEnforce, tight)
		admitted := 0
		for range 10 {
			if call(r) == nil {
				admitted++
			}
			r.clk.add(time.Second)
		}
		// an admitted request weighs on the next window, so every second retry is admitted; counted refusals admitted one
		if admitted < 5 {
			t.Fatalf("%d of 10 retries a second apart were admitted, want every second one", admitted)
		}
	})
	t.Run("R-LIM-020 a caller that waits the Retry-After of its refusal is admitted", func(t *testing.T) {
		r := newRig(t, ModeEnforce, tight)
		if err := call(r); err != nil {
			t.Fatal(err)
		}
		for range 5 { // hammering in between changes nothing
			if err := call(r); codeOf(err) != errs.CodeRateLimited {
				t.Fatalf("err = %v, want rate_limited", err)
			}
		}
		e, _ := errs.As(call(r))
		r.clk.add(e.RetryAfter)
		if err := call(r); err != nil {
			t.Fatalf("refused after the Retry-After of %s: %v", e.RetryAfter, err)
		}
	})
	t.Run("R-LIM-019 on the replica's share a refused request fills no window either", func(t *testing.T) {
		r := newRig(t, ModeEnforce, Figures{RequestsPerSecond: 2, RequestsBurst: 2}) // a share of 1 on each of 2 replicas
		r.store.down.Store(true)
		admitted := 0
		for range 10 {
			if call(r) == nil {
				admitted++
			}
			_ = call(r)
			r.clk.add(time.Second)
		}
		if admitted < 5 {
			t.Fatalf("%d of 10 admitted on the share of one a second with a refused request between, want every second one", admitted)
		}
	})
}

// R-LIM-008, R-LIM-025, Q-LIM-58 (the owner's decision of 2026-11-20): a 429 of a request figure is not a refused request, in
// enforce, in audit and on the replica's share; the refusals R-LIM-008 names still count and block.
func TestR_LIM_008_ARequestFiguresRefusalIsNoRefusedRequest(t *testing.T) {
	f := Figures{RequestsPerSecond: 1, RequestsBurst: 1, RefusedPerHour: 5}
	const addr = "198.51.100.9"
	call := func(r *rig) error {
		c := r.e.Begin(anonCaller(addr), qname.Repo{})
		defer r.e.Finish(c)
		return r.e.Check(context.Background(), c, time.Time{}, nil).Err
	}
	// hammer sends 20 requests at once and runs the two flushes that count the refusals and set a marker that came due.
	hammer := func(r *rig) (refusedByFigure int) {
		for range 20 {
			if codeOf(call(r)) == errs.CodeRateLimited {
				refusedByFigure++
			}
		}
		r.e.Flush(context.Background())
		r.e.Flush(context.Background())
		return refusedByFigure
	}
	t.Run("in enforce a hammering caller is held to its figure, counts no refused request and is not blocked", func(t *testing.T) {
		r := newRig(t, ModeEnforce, f)
		if n := hammer(r); n != 19 {
			t.Fatalf("%d of 20 requests at once refused rate_limited, want 19", n)
		}
		if _, refused, _, _, _, _ := r.usage.sum(); refused != 0 {
			t.Fatalf("%d refused requests counted for the request figure's refusals, want 0", refused)
		}
		if len(r.blocks.b) != 0 {
			t.Fatalf("19 refusals of the request figure at refusedPerHour 5 blocked the caller: %v", r.blocks.b)
		}
		r.clk.add(2 * time.Second)
		if err := call(r); err != nil {
			t.Fatalf("the caller after waiting: %v, want admitted", err)
		}
	})
	t.Run("in audit the figure's would-be refusals count no refused request and record no would_limit of refusedPerHour", func(t *testing.T) {
		r := newRig(t, ModeAudit, f)
		hammer(r)
		_, refused, _, _, _, ev := r.usage.sum()
		if refused != 0 || ev["audit:refusedPerHour"] != 0 {
			t.Fatalf("refused %d, events %v, want no refused request and no would_limit of refusedPerHour", refused, ev)
		}
		if ev["audit:requestsPerSecond"] == 0 {
			t.Fatalf("the request figure's would_limit is missing: %v", ev)
		}
	})
	t.Run("R-LIM-019 on the replica's share the figure's refusals count no refused request", func(t *testing.T) {
		r := newRig(t, ModeEnforce, Figures{RequestsPerSecond: 2, RequestsBurst: 2, RefusedPerHour: 5}) // a share of 1 on each of 2
		r.store.down.Store(true)
		if n := hammer(r); n != 19 {
			t.Fatalf("%d of 20 requests at once refused on the share of one, want 19", n)
		}
		if _, refused, _, _, _, _ := r.usage.sum(); refused != 0 || len(r.blocks.b) != 0 {
			t.Fatalf("on the share: %d refused requests, %d blocks, want none", refused, len(r.blocks.b))
		}
	})
	t.Run("the refusals R-LIM-008 names still count, and block past refusedPerHour", func(t *testing.T) {
		r := newRig(t, ModeEnforce, f)
		hammer(r)
		r.clk.add(2 * time.Second)
		for range 6 { // a refusal of the decision point, as the dispatcher marks a 403
			c := r.e.Begin(anonCaller(addr), qname.Repo{})
			c.MarkRefused()
			r.e.Finish(c)
		}
		r.e.Flush(context.Background())
		r.e.Flush(context.Background())
		if _, refused, _, _, _, _ := r.usage.sum(); refused != 6 {
			t.Fatalf("%d refused requests counted, want the 6 of the decision point and none of the request figure", refused)
		}
		if err := call(r); codeOf(err) != CodeCallerBlocked {
			t.Fatalf("after 6 refusals of the decision point at refusedPerHour 5: %v, want caller_blocked", err)
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

func TestR_LIM_024_ParallelTransfers(t *testing.T) {
	f := BuiltinProfiles()[ProfileAnonymous]
	f[ParallelTransfers] = 4 // a figure the test fills with four transfers
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
	if err := r.e.Admit(ctx, c); codeOf(err) != CodeTooManyParallelTransfers {
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
	t.Run("R-LIM-024 with the endpoint's share of waiting downloads held, the next is refused at once and another caller is served", func(t *testing.T) {
		r := newRig(t, ModeEnforce, f)
		r.e.o.ParallelWait = 10 * time.Second
		gate := NewWaitGate(4) // four request slots: one may wait
		begin := func(addr string) *Charge {
			c := r.e.Begin(anonCaller(addr), qname.Repo{})
			c.SetWaitGate(gate)
			return c
		}
		for range 4 {
			if err := r.e.Admit(ctx, begin("198.51.100.10")); err != nil {
				t.Fatal(err)
			}
		}
		waitCtx, stop := context.WithCancel(ctx)
		waited := make(chan error, 1)
		go func() { waited <- r.e.Admit(waitCtx, begin("198.51.100.10")) }()
		for deadline := time.Now().Add(5 * time.Second); gate.Waiting() != 1; time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal("the fifth download does not wait")
			}
		}
		start := time.Now()
		if err := r.e.Admit(ctx, begin("198.51.100.10")); codeOf(err) != CodeTooManyParallelTransfers || time.Since(start) > 500*time.Millisecond {
			t.Fatalf("the sixth download with the share held: %v after %s, want too_many_parallel_transfers at once", err, time.Since(start))
		}
		if err := r.e.Admit(ctx, begin("198.51.100.11")); err != nil {
			t.Fatalf("another caller's download beside the held share: %v", err)
		}
		stop()
		if err := <-waited; !errors.Is(err, context.Canceled) {
			t.Fatalf("the waiting download ended with %v", err)
		}
		if gate.Waiting() != 0 {
			t.Fatalf("%d still wait after the wait ended", gate.Waiting())
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
		if err := r.e.Admit(ctx, c); codeOf(err) != CodeTooManyParallelTransfers {
			t.Fatalf("the third download on the share: %v", err)
		}
		if !r.e.Fallback() {
			t.Fatal("the fallback is not reported")
		}
	})
	// A Valkey that answers late or not at all for a while sends some batches of a replica to Valkey and some to its share,
	// one after the other: a download's lease can be taken in one and its end flushed to the other. A lease is given up in
	// every place it may be, or the downloads that ended keep their places for limits.parallel.lease and a caller is refused
	// because Valkey failed (R-LIM-019).
	admitN := func(t *testing.T, r *rig, n int) []*Charge {
		t.Helper()
		var cs []*Charge
		for i := range n {
			c := r.e.Begin(anonCaller("198.51.100.9"), qname.Repo{})
			if err := r.e.Admit(ctx, c); err != nil {
				t.Fatalf("download %d of %d: %v", i+1, n, err)
			}
			cs = append(cs, c)
		}
		return cs
	}
	finish := func(r *rig, cs []*Charge) {
		for _, c := range cs {
			r.e.Finish(c)
		}
	}
	t.Run("R-LIM-019 R-LIM-024 downloads that ended on the replica's share are given up there when the flush reaches Valkey", func(t *testing.T) {
		r := newRig(t, ModeEnforce, f)
		r.store.down.Store(true)
		cs := admitN(t, r, 2) // the share: ceil(4 / 2)
		finish(r, cs)
		r.store.down.Store(false)
		r.e.Flush(ctx) // reaches Valkey
		r.store.down.Store(true)
		admitN(t, r, 2) // the share is free again
	})
	t.Run("R-LIM-019 R-LIM-024 downloads that ended while the flush could not reach Valkey are given up in Valkey at a later flush", func(t *testing.T) {
		r := newRig(t, ModeEnforce, f)
		cs := admitN(t, r, 4) // the leases are in Valkey
		finish(r, cs)
		r.store.down.Store(true)
		r.e.Flush(ctx) // runs on the share
		r.store.down.Store(false)
		r.e.Flush(ctx)
		admitN(t, r, 4)
	})
	t.Run("R-LIM-019 R-LIM-024 a lease whose take Valkey ran after the timeout is given up in Valkey and on the share", func(t *testing.T) {
		r := newRig(t, ModeEnforce, f)
		r.store.late.Store(true)
		cs := admitN(t, r, 2) // decided on the share; Valkey ran the takes too
		if !r.e.Fallback() {
			t.Fatal("a late answer is not a fallback")
		}
		finish(r, cs)
		r.store.late.Store(false)
		r.e.Flush(ctx)
		admitN(t, r, 4) // nothing is left in Valkey
		r.store.down.Store(true)
		admitN(t, r, 2) // nothing is left on the share
	})
	t.Run("R-LIM-019 R-LIM-024 a lease given up at once, as a waiting download does before it asks again, leaves the share when the drop reaches Valkey", func(t *testing.T) {
		r := newRig(t, ModeEnforce, f)
		r.store.down.Store(true)
		c := admitN(t, r, 1)[0]
		r.store.down.Store(false)
		r.e.dropLease(ctx, c)
		r.store.down.Store(true)
		admitN(t, r, 2)
	})
	t.Run("R-LIM-024 a drop that never reaches Valkey is not kept beyond the lease's end", func(t *testing.T) {
		r := newRig(t, ModeEnforce, f)
		cs := admitN(t, r, 4)
		finish(r, cs)
		r.store.down.Store(true)
		r.e.Flush(ctx)
		unsent := func() int { r.e.mu.Lock(); defer r.e.mu.Unlock(); return len(r.e.unsent) }
		if n := unsent(); n != 4 {
			t.Fatalf("%d drops wait for Valkey, want 4", n)
		}
		r.clk.add(time.Minute + time.Second)
		r.e.Flush(ctx)
		if n := unsent(); n != 0 {
			t.Fatalf("%d drops still wait after the leases' end, want 0", n)
		}
	})
}

// R-LIM-024, R-LIM-039: a replica that dies with downloads running neither drops nor renews their leases; they end at their score,
// limits.parallel.lease after their last renewal, and the next download of the subject on another replica is admitted then. The
// crash is a replica's engine that is left alone: no Finish, no flush, no Stop.
func TestR_LIM_024_ACrashedReplicasLeasesEndAtTheirScore(t *testing.T) {
	ctx := context.Background()
	f := BuiltinProfiles()[ProfileAnonymous]
	f[ParallelTransfers] = 4 // a figure the crashed replica's transfers fill
	r := newRig(t, ModeEnforce, f)
	other := NewEngine(Options{
		Store: r.store, Index: func() *Index { return r.x }, Replica: "r2", Now: r.clk.now, LeaseTTL: time.Minute,
		Share: func() int { return 2 },
	})
	for i := range 4 { // the crashed replica's downloads
		if err := r.e.Admit(ctx, r.e.Begin(anonCaller("198.51.100.12"), qname.Repo{})); err != nil {
			t.Fatalf("download %d on the replica that crashes: %v", i+1, err)
		}
	}
	admit := func() error { return other.Admit(ctx, other.Begin(anonCaller("198.51.100.12"), qname.Repo{})) }
	r.clk.add(time.Minute - time.Second)
	if err := admit(); codeOf(err) != CodeTooManyParallelTransfers {
		t.Fatalf("a second before the leases end: %v, want too_many_parallel_transfers", err)
	}
	r.clk.add(time.Second)
	if err := admit(); err != nil {
		t.Fatalf("at the leases' end: %v", err)
	}
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

// R-LIM-022, Q-LIM-110 (decision 19 of the owner, Q-LIM-57, carried to step 9a): an upstream request step 9a refuses is counted
// nowhere, not in the budget key's window, not in the ceiling's, not in the usage store: a caller that retries is admitted again
// after the Retry-After of its refusal, and a refusal by one budget leaves the other untouched. One the parallel lease refuses
// after the figures admitted it is given back.
func TestR_LIM_022_ARefusedUpstreamRequestIsCountedNowhere(t *testing.T) {
	ctx := context.Background()
	two := BuiltinProfiles()[ProfileAnonymous]
	two[UpstreamRequestsPerHour] = 2
	fetch := func(r *rig, addr string, repo int64) error {
		c := r.e.Begin(anonCaller(addr), qname.Repo{})
		defer r.e.Finish(c)
		return r.e.MayFetch(ctx, c, repo, false)
	}
	t.Run("a caller that retries is admitted once the window has room, and the usage store counts what went upstream", func(t *testing.T) {
		r := newRig(t, ModeEnforce, two)
		for i := range 2 {
			if err := fetch(r, "198.51.100.40", 5); err != nil {
				t.Fatalf("upstream request %d: %v", i, err)
			}
		}
		for range 6 { // hammering in between changes nothing
			if err := fetch(r, "198.51.100.40", 5); codeOf(err) != CodeUpstreamLimitReached {
				t.Fatalf("a refused retry: %v", err)
			}
		}
		// Half into the next hour the two of this hour weigh one: room for one more. Six refusals counted would weigh four.
		r.clk.add(90 * time.Minute)
		if err := fetch(r, "198.51.100.40", 5); err != nil {
			t.Fatalf("refused half into the next hour: %v", err)
		}
		r.e.Flush(ctx)
		if _, _, _, _, up, _ := r.usage.sum(); up != 3 {
			t.Fatalf("the usage store counts %d upstream requests, want the 3 admitted", up)
		}
	})
	t.Run("a request the ceiling refuses leaves the caller's own window untouched", func(t *testing.T) {
		r := newRig(t, ModeEnforce, two) // the anonymous ceiling of repository 21 is 3 an hour
		for i := range 3 {
			if err := fetch(r, "203.0.113."+strconv.Itoa(10+i), 21); err != nil {
				t.Fatalf("ceiling request %d: %v", i, err)
			}
		}
		for range 4 {
			if err := fetch(r, "203.0.113.50", 21); codeOf(err) != CodeUpstreamLimitReached {
				t.Fatalf("beyond the ceiling: %v", err)
			}
		}
		for i := range 2 {
			if err := fetch(r, "203.0.113.50", 5); err != nil {
				t.Fatalf("the refused caller's own upstream request %d at another remote: %v", i, err)
			}
		}
	})
	t.Run("a request the caller's own figure refuses leaves the ceiling untouched", func(t *testing.T) {
		r := newRig(t, ModeEnforce, two)
		for range 2 {
			if err := fetch(r, "203.0.113.60", 5); err != nil {
				t.Fatal(err)
			}
		}
		for range 4 {
			if err := fetch(r, "203.0.113.60", 21); codeOf(err) != CodeUpstreamLimitReached {
				t.Fatalf("beyond the caller's own figure: %v", err)
			}
		}
		for i := range 3 {
			if err := fetch(r, "203.0.113."+strconv.Itoa(70+i), 21); err != nil {
				t.Fatalf("another caller's request %d within the ceiling: %v", i, err)
			}
		}
	})
	t.Run("a download the parallel lease refuses gives its upstream request back, in Valkey and on the replica's share", func(t *testing.T) {
		one := two
		one[ParallelTransfers] = 1
		for _, down := range []bool{false, true} {
			f := one
			if down {
				f[UpstreamRequestsPerHour] = 4 // a share of 2 on each of 2 replicas
			}
			r := newRig(t, ModeEnforce, f)
			r.store.down.Store(down)
			first := r.e.Begin(anonCaller("198.51.100.41"), qname.Repo{})
			if err := r.e.MayFetch(ctx, first, 5, true); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				c := r.e.Begin(anonCaller("198.51.100.41"), qname.Repo{})
				if err := r.e.MayFetch(ctx, c, 5, true); codeOf(err) != CodeTooManyParallelTransfers {
					t.Fatalf("down %v, a second download beside the first: %v", down, err)
				}
				r.e.Finish(c)
			}
			r.e.Finish(first)
			r.e.Flush(ctx)
			if err := fetch(r, "198.51.100.41", 5); err != nil {
				t.Fatalf("down %v, the second upstream request of the hour after three refused downloads: %v", down, err)
			}
			r.e.Flush(ctx)
			if _, _, _, _, up, _ := r.usage.sum(); up != 2 {
				t.Fatalf("down %v, the usage store counts %d upstream requests, want the 2 that went upstream", down, up)
			}
		}
	})
	t.Run("in audit the request goes upstream and is counted where it went, its would-limit recorded", func(t *testing.T) {
		r := newRig(t, ModeAudit, two)
		for i := range 4 {
			if err := fetch(r, "198.51.100.42", 5); err != nil {
				t.Fatalf("audit, upstream request %d: %v", i, err)
			}
		}
		r.e.Flush(ctx)
		if _, _, _, _, up, ev := r.usage.sum(); up != 4 || ev["audit:upstreamRequestsPerHour"] != 2 {
			t.Fatalf("audit: %d upstream requests, events %v; want 4 and two would-limits", up, ev)
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

// TestR_LIM_003_ATokenRequestIsRecordedOnceUnderTheSubjectItsHandlerAuthenticated: the registry's token service is anonymous at
// stage 7 and charged to the subject its handler authenticated (Retarget). The usage store records the token request once, under
// that subject and never under the address, whether or not a flush of the charge buffer fell between stage 7 and the handler; a
// token request whose credential failed is one request and one refused request of the address; and no charge is left in the
// buffer after the request ended.
func TestR_LIM_003_ATokenRequestIsRecordedOnceUnderTheSubjectItsHandlerAuthenticated(t *testing.T) {
	ctx := context.Background()
	const addr = "198.51.100.30"
	token := Caller{Kind: CallerServiceAccount, PrincipalID: "sa-puller", TechnicalTokenID: "tt-1", Client: netip.MustParseAddr(addr)}
	for _, tc := range []struct {
		name         string
		flushBetween bool
		caller       Caller
		refused      bool
	}{
		{name: "a valid technical token", caller: token},
		{name: "a valid technical token, a flush between stage 7 and the handler", flushBetween: true, caller: token},
		{name: "a credential that failed", caller: anonCaller(addr), refused: true},
		{name: "a credential that failed, a flush between stage 7 and the handler", flushBetween: true, caller: anonCaller(addr), refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, ModeEnforce, BuiltinProfiles()[ProfileAnonymous])
			c := r.e.Begin(anonCaller(addr), qname.Repo{})
			c.DeferRequestFigures()
			if st := r.e.Check(ctx, c, time.Time{}, nil); st.Err != nil {
				t.Fatal(st.Err)
			}
			if tc.flushBetween {
				r.e.Flush(ctx)
			}
			if tc.refused {
				c.MarkRefused()
			}
			c.Retarget(ctx, tc.caller)
			c.AddCached(920)
			r.e.Finish(c)
			r.e.Flush(ctx)
			r.e.Flush(ctx)
			subject, _ := SubjectOf(tc.caller)
			want := usageCounts{requests: 1, cached: 920}
			if tc.refused {
				want.refused = 1
			}
			if rows := r.usage.rows(); len(rows) != 1 || rows[usageRow{subject: subject.String()}] != want {
				t.Fatalf("recorded %+v, want %+v under %s alone", rows, want, subject)
			}
			if n := r.e.activeCharges(); n != 0 {
				t.Fatalf("%d charges left in the buffer after the token request ended", n)
			}
		})
	}
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
