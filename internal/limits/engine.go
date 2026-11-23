package limits

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
)

// The counter names of the figures in Valkey (R-LIM-017).
const (
	ctrRequestsHour  = "rh"
	ctrCachedHour    = "ch"
	ctrUncachedHour  = "uh"
	ctrUpstreamHour  = "up"
	ctrBytesDay      = "bd"
	ctrRefusedHour   = "rf"
	hour             = time.Hour
	day              = 24 * time.Hour
	renewLeasesAfter = 20 * time.Second
)

// burstCounter names the counter of the request burst window: the window is part of the name, so a profile that changes
// its rate starts a counter of its own instead of reading one of another length.
func burstCounter(w time.Duration) string { return "rb-" + strconv.FormatInt(w.Milliseconds(), 10) }

// burstWindow is the window of the request figure: burst ÷ rate seconds, at least one second (R-LIM-004).
func burstWindow(f Figures) time.Duration {
	rps := f[RequestsPerSecond]
	if rps <= 0 {
		return 0
	}
	w := time.Duration(float64(f.Burst()) / float64(rps) * float64(time.Second))
	return max(time.Second, w.Truncate(time.Millisecond))
}

// UsageSink receives the consumption the flush found, for the usage store (R-LIM-027); internal/limits/usage implements it.
type UsageSink interface {
	Add(d UsageDelta)
}

// UsageDelta is the consumption of one subject at one place and serving repository since the last flush.
type UsageDelta struct {
	At      time.Time
	Subject Subject
	Place   Place
	RepoID  int64
	// ViaRepoID is the virtual repository of another project the request reached the serving repository through, when that
	// is a well-known repository of the project upstream (Q-LIM-63); 0 otherwise.
	ViaRepoID         int64
	Requests, Refused int64
	Cached, Uncached  int64
	Upstream          int64
	Background        int64
	// Events are the limits met, per figure, as limited (enforce) or would_limit (audit).
	Events []EventDelta
	// Limits are the budget key's effective figures, for the utilisation of the overview.
	Limits Figures
}

// EventDelta is the count of one figure's limited or would-limit decisions.
type EventDelta struct {
	Figure string
	Mode   Mode
	Count  int64
}

// BlockRecorder writes a block that the flush set, asynchronously, and audits it (R-LIM-025); internal/limits/store
// implements it.
type BlockRecorder interface {
	Started(ctx context.Context, b Block)
}

// Block is one block of a budget key.
type Block struct {
	ID       string
	Subject  Subject
	Place    Place
	Started  time.Time
	Until    time.Time
	Refused  int64
	Limit    int64
	LiftedAt *time.Time
	LiftedBy string
}

// MarkerValue is the value of the block marker: the block's ID and its end in Unix milliseconds, so stage 7 answers the
// remaining time without a further command.
func (b Block) MarkerValue() string { return b.ID + "|" + strconv.FormatInt(b.Until.UnixMilli(), 10) }

// Options configure [NewEngine].
type Options struct {
	// Store holds the counters of the cluster (*cluster.LimitCounters); nil counts in the replica's memory only.
	Store cluster.LimitStore
	// Available reports whether the store can be used now; nil is always.
	Available func() bool
	// Degraded counts a decision that fell back to the replica's share.
	Degraded func()
	// Share is n, the live replicas of the role in the replica's pool (R-ARCH-034); nil is 1.
	Share func() int
	// Index is the installed index; nil, or a nil index, is a replica that has not loaded the limits yet: every subject
	// is unlimited and nothing is refused.
	Index func() *Index
	// Replica names the replica in lease members.
	Replica string
	// FlushInterval is limits.flush_interval (1 s); LeaseTTL limits.parallel.lease (60 s); ParallelWait
	// limits.parallel.wait (10 s); MinPacing limits.pacing.min_bytes_per_second (65,536).
	FlushInterval time.Duration
	LeaseTTL      time.Duration
	ParallelWait  time.Duration
	MinPacing     int64
	Usage         UsageSink
	Blocks        BlockRecorder
	Metrics       *Metrics
	Log           *slog.Logger
	Now           func() time.Time
	// BaseURL is the instance's base URL for the "Your limits" link of a refusal; empty leaves it out.
	BaseURL func(ctx context.Context) string
}

// Engine decides and counts the download limits of one replica.
type Engine struct {
	o     Options
	local *cluster.MemoryLimits
	m     *Metrics
	ids   atomic.Uint64

	fallback atomic.Bool

	mu     sync.Mutex
	active map[*Charge]struct{}
	// due are the block markers a flush found due; the next flush sets them.
	due map[string]*dueBlock
	// wouldRefused remembers the window in which a would-limit of refusedPerHour was recorded per budget key.
	wouldRefused map[string]int64
	pacers       map[string]*pacer
	// unsent are the lease drops that did not reach Valkey, with the end of each lease there: every flush sends them again
	// until one reaches Valkey or the lease has ended on its own (R-LIM-024).
	unsent map[unsentDrop]time.Time

	stop chan struct{}
	done chan struct{}
	once sync.Once
}

type dueBlock struct {
	block Block
	key   string
}

// unsentDrop names a lease in Valkey whose drop is still to be sent.
type unsentDrop struct {
	bucket, member string
}

// NewEngine creates the engine.
func NewEngine(o Options) *Engine {
	if o.FlushInterval <= 0 {
		o.FlushInterval = time.Second
	}
	if o.LeaseTTL <= 0 {
		o.LeaseTTL = time.Minute
	}
	if o.ParallelWait < 0 {
		o.ParallelWait = 0
	}
	if o.MinPacing <= 0 {
		o.MinPacing = 65536
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Metrics == nil {
		o.Metrics = NewMetrics(nil)
	}
	local := cluster.NewMemoryLimits()
	local.Now = o.Now
	return &Engine{
		o: o, local: local, m: o.Metrics, active: map[*Charge]struct{}{}, due: map[string]*dueBlock{},
		wouldRefused: map[string]int64{}, pacers: map[string]*pacer{}, unsent: map[unsentDrop]time.Time{},
		stop: make(chan struct{}), done: make(chan struct{}),
	}
}

func (e *Engine) now() time.Time { return e.o.Now() }
func (e *Engine) nextID() uint64 { return e.ids.Add(1) }

func (e *Engine) index() *Index {
	if e.o.Index == nil {
		return nil
	}
	return e.o.Index()
}

// Mode is the instance's mode now.
func (e *Engine) Mode() Mode { return e.index().Settings().Mode }

func (e *Engine) share() int64 {
	if e.o.Share == nil {
		return 1
	}
	return int64(max(1, e.o.Share()))
}

// Fallback reports whether the last batch ran on the replica's share (quader_ratelimit_fallback{limiter="limits"}).
func (e *Engine) Fallback() bool { return e.fallback.Load() }

// run sends a batch to the cluster's counters, or runs it on the replica's local state while Valkey cannot be used. local
// says the results are the replica's own, whose limits are the share (R-LIM-019).
func (e *Engine) run(ctx context.Context, b *cluster.LimitBatch) (res []cluster.LimitResult, local bool, err error) {
	if b.Len() == 0 {
		return nil, false, nil
	}
	if e.o.Store != nil && (e.o.Available == nil || e.o.Available()) {
		res, err = e.o.Store.Run(ctx, b)
		if err == nil {
			e.fallback.Store(false)
			return res, false, nil
		}
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
	}
	if e.o.Store != nil && e.o.Degraded != nil {
		e.o.Degraded()
	}
	if e.o.Store != nil {
		e.fallback.Store(true)
		e.localTakes(b)
	}
	res, err = e.local.Run(ctx, b)
	return res, e.o.Store != nil, err
}

// localTakes fits the takes of a batch to the replica's own state (R-LIM-019): each request window at the replica's share of
// its limit, and without the window of a class of R-API-016, which decides on its own share (R-ARCH-034). The class's windows
// are the last of a take, so the indexes of the others stay.
func (e *Engine) localTakes(b *cluster.LimitBatch) {
	for i, op := range b.Ops {
		if op.Kind != cluster.OpTake {
			continue
		}
		ws := make([]cluster.WindowTake, 0, len(op.Windows))
		for _, w := range op.Windows {
			if !w.Class {
				w.Limit = e.scaled(w.Limit, true)
				ws = append(ws, w)
			}
		}
		b.Ops[i].Windows = ws
	}
}

// scaled is a limit during the fallback: the replica's share, at least 1 (R-LIM-019).
func (e *Engine) scaled(limit int64, local bool) int64 {
	if !local || limit <= 0 {
		return limit
	}
	return max(1, limit/e.share())
}

// scaledUp is the share of parallel downloads: ⌈limit ÷ n⌉.
func (e *Engine) scaledUp(limit int64, local bool) int64 {
	if !local || limit <= 0 {
		return limit
	}
	n := e.share()
	return (limit + n - 1) / n
}

// Begin derives the subject of a request and resolves its rule (R-LIM-001, R-LIM-013). It counts nothing; stage 7 decides
// the request with [Engine.Check].
func (e *Engine) Begin(caller Caller, repo qname.Repo) *Charge {
	s, _ := SubjectOf(caller)
	return e.chargeFor(s, repo)
}

func (e *Engine) chargeFor(s Subject, repo qname.Repo) *Charge {
	res, key, mode := e.resolve(s, repo)
	c := &Charge{
		e: e, Subject: s, Resolution: res, Key: key, Mode: mode, Addressed: repo,
		id: e.nextID(), started: e.now(),
	}
	c.pendRequests.Store(1)
	e.track(c)
	return c
}

// resolve is the rule of a subject at the repository a request names (R-LIM-013): its resolution, its budget key and the mode.
func (e *Engine) resolve(s Subject, repo qname.Repo) (Resolution, string, Mode) {
	x := e.index()
	var res Resolution
	if x != nil {
		res = x.Resolve(s, repo, false)
	} else {
		res.Place = InstancePlace()
	}
	return res, BudgetKey(s, res.Place), x.Settings().Mode
}

// Stage is what stage 7 decided.
type Stage struct {
	// Err is the refusal to answer; nil lets the request on.
	Err error
	// Policies are the RateLimit fields of the request figures, in enforce only (R-LIM-020, Q-LIM-26).
	Policies []Policy
}

// Policy is one member of RateLimit-Policy and RateLimit.
type Policy struct {
	Name      string
	Quota     int64
	Window    time.Duration
	Remaining int64
	Reset     time.Duration
	// Unit is the quota unit qu of a figure that does not count requests: content-bytes, concurrent-requests.
	Unit string
}

// refusalPolicy names the policy of a figure that refuses beside the request figures (R-LIM-020); ok is false for the request
// figures, whose policies stage 7 sends on every answer.
func refusalPolicy(f Figure) (name, unit string, ok bool) {
	switch f {
	case CachedBytesPerHour:
		return "cached-hour", "content-bytes", true
	case UncachedBytesPerHour:
		return "uncached-hour", "content-bytes", true
	case UpstreamRequestsPerHour:
		return "upstream-hour", "", true
	case BytesPerDay:
		return "day", "content-bytes", true
	case ParallelDownloads:
		return "parallel", "concurrent-requests", true
	case RefusedPerHour:
		return "refused-hour", "", true
	}
	return "", "", false
}

// refuse records the refusal's RateLimit policy on the charge and returns its error (R-LIM-020): the counting writer of stage 7
// adds the policy to the 429 it writes.
func (e *Engine) refuse(c *Charge, rf refusal, quota int64) *errs.Error {
	err := rf.err()
	f, _ := ParseFigure(rf.figure)
	if name, unit, ok := refusalPolicy(f); ok && c != nil {
		w := rf.window
		if w <= 0 {
			w = time.Second
		}
		c.mu.Lock()
		c.refusal = &Policy{Name: name, Quota: quota, Window: w, Reset: err.RetryAfter, Unit: unit}
		c.mu.Unlock()
	}
	return err
}

// ClassCheck adds the window of a class of R-API-016 to the take of stage 7 and returns the function that decides it from the
// take's result.
type ClassCheck func(take *cluster.LimitOp) func(res cluster.LimitResult, err error) error

// window is one request window of the take of stage 7.
type window struct {
	i     int // its index in the take, -1 when the figure is not set
	fig   Figure
	w     time.Duration
	limit int64
	name  string
}

// addWindow adds a request window to the take.
func addWindow(take *cluster.LimitOp, ctr, bucket string, w time.Duration, at time.Time, limit int64) int {
	idx, el := cluster.WindowIndex(at, w)
	take.Windows = append(take.Windows, cluster.WindowTake{Figure: ctr, Bucket: bucket, Window: w, Index: idx, Elapsed: el, Limit: limit})
	return len(take.Windows) - 1
}

// Check is stage 7 for a request on a protocol path (R-LIM-005, R-LIM-017): one round trip that takes the request windows of
// the budget key, reads its block marker and decides the class of R-API-016 that is in force, all in one take (Q-LIM-57): the
// request is counted in its windows only when none of them, no block and no class refuses it, so a refused request fills no
// window and the Retry-After of its refusal is the time after which the caller's next request fits. A request the request
// figures refuse is not a refused request of R-LIM-008 (Q-LIM-58). In enforce the block and the request figures refuse; in audit
// they are recorded as would_limit and nothing refuses (Q-LIM-4: no class is in force).
func (e *Engine) Check(ctx context.Context, c *Charge, at time.Time, class ClassCheck) Stage {
	if c != nil && c.Subject.Kind == KindSystem {
		// The image scanning's pull is system work: counted in the usage store, checked against nothing (Q-LIM-66).
		return Stage{}
	}
	if at.IsZero() {
		at = e.now()
	}
	f := c.Figures()
	bucket := cluster.LimitBucket(c.Key)
	take := cluster.LimitOp{Kind: cluster.OpTake, Bucket: bucket, Marker: true, Now: e.now(), N: 1}
	rb := window{i: -1, fig: RequestsPerSecond, w: burstWindow(f), limit: f.Burst(), name: "requests"}
	rh := window{i: -1, fig: RequestsPerHour, w: hour, limit: f[RequestsPerHour], name: "requests-hour"}
	if !c.deferred.Load() {
		if rb.w > 0 {
			rb.i = addWindow(&take, burstCounter(rb.w), bucket, rb.w, at, rb.limit)
		}
		if rh.limit > 0 {
			rh.i = addWindow(&take, ctrRequestsHour, bucket, hour, at, rh.limit)
		}
	}
	var decideClass func(cluster.LimitResult, error) error
	if class != nil {
		decideClass = class(&take)
	}
	var b cluster.LimitBatch
	b.Add(take)
	res, local, err := e.run(ctx, &b)
	var st Stage
	if err != nil { // the request's context ended
		st.Err = err
		return st
	}
	r := res[0]
	if decideClass != nil {
		var classErr error
		if local {
			classErr = cluster.ErrUnavailable // the class falls back to its own share
		}
		if cerr := decideClass(r, classErr); cerr != nil {
			st.Err = cerr
			if local && r.Admitted {
				e.local.Return(b.Ops[0]) // the class refused on its own share after the windows took the request
			}
		}
	}
	e.checkBlock(ctx, c, r.Value, &st, c.Mode == ModeEnforce)
	for _, w := range []window{rb, rh} {
		if w.i >= 0 {
			e.checkWindow(ctx, c, at, w, r.Windows[w.i], local, &st)
		}
	}
	return st
}

// checkBlock decides the block of the budget key from the marker the take read (R-LIM-025).
func (e *Engine) checkBlock(ctx context.Context, c *Charge, mk string, st *Stage, enforce bool) {
	if mk == "" {
		return
	}
	retry, ok := markerRemaining(mk, e.now())
	if !ok {
		return
	}
	e.decided(c, "refusedPerHour", enforce)
	if enforce && st.Err == nil {
		f := c.Figures()
		c.mu.Lock()
		c.outcome, c.limiter = "blocked", "limits.refusedPerHour"
		c.mu.Unlock()
		st.Err = e.refuse(c, refusal{
			code: CodeCallerBlocked, figure: RefusedPerHour.String(), limit: f[RefusedPerHour], window: hour,
			place: c.Resolution.Place, retry: retry, signed: c.Subject.Kind != KindAnonymous, baseURL: e.baseURL(ctx),
		}, f[RefusedPerHour])
	}
}

// checkWindow decides one request window from what the take found in it (R-LIM-020): the RateLimit policy in enforce, and a
// window the request does not fit refuses it with the time after which the next request fits. The refusal is not a refused
// request of R-LIM-008, in enforce or in audit (Q-LIM-58): a caller that keeps sending is held to its figure's rate, and
// refusedPerHour does not block it for that.
func (e *Engine) checkWindow(ctx context.Context, c *Charge, at time.Time, w window, s cluster.WindowState, local bool, st *Stage) {
	enforce := c.Mode == ModeEnforce
	_, el := cluster.WindowIndex(at, w.w)
	lim := e.scaled(w.limit, local)
	est := cluster.Estimate(s.Cur, s.Prev, el) + 1
	if enforce {
		remaining := lim - int64(est)
		if !s.Fits {
			remaining = lim - int64(est-1)
		}
		st.Policies = append(st.Policies, Policy{
			Name: w.name, Quota: lim, Window: w.w, Remaining: max(0, remaining), Reset: time.Duration((1 - el) * float64(w.w)),
		})
	}
	if s.Fits {
		e.m.decision(w.fig.String(), c.Mode, "admitted")
		return
	}
	e.decided(c, w.fig.String(), enforce)
	if enforce && st.Err == nil {
		st.Err = refusal{
			code: errs.CodeRateLimited, figure: w.fig.String(), limit: w.limit, window: w.w, place: c.Resolution.Place,
			retry: cluster.RoomAfter(s.Cur, s.Prev, el, w.w, lim, 1), signed: c.Subject.Kind != KindAnonymous, baseURL: e.baseURL(ctx),
		}.err()
	}
}

// decided records a decision that limited (enforce) or would have limited (audit) the request.
func (e *Engine) decided(c *Charge, figure string, enforce bool) {
	outcome := "would_limit"
	if enforce {
		outcome = "limited"
	}
	e.m.decision(figure, c.Mode, outcome)
	c.note(figure, enforce)
}

func (e *Engine) baseURL(ctx context.Context) string {
	if e.o.BaseURL == nil {
		return ""
	}
	return strings.TrimSuffix(e.o.BaseURL(ctx), "/")
}

// markerRemaining reads a block marker's end and returns the time left; ok is false for a marker that has ended or does not
// parse (it is then ignored, and expires on its own).
func markerRemaining(v string, now time.Time) (time.Duration, bool) {
	_, until, found := strings.Cut(v, "|")
	if !found {
		return 0, false
	}
	ms, err := strconv.ParseInt(until, 10, 64)
	if err != nil {
		return 0, false
	}
	left := time.UnixMilli(ms).Sub(now)
	return left, left > 0
}

// Admit is the check before a download starts (R-LIM-021, R-LIM-024): the budget key's cachedBytesPerHour and bytesPerDay
// windows and a parallel lease, in one round trip, before the status line. A request that holds a lease already (a miss that
// took it at step 9a) passes without a second check. In audit nothing is refused and a would-limit is recorded.
func (e *Engine) Admit(ctx context.Context, c *Charge) error {
	if c == nil || c.bg || c.Subject.Kind == KindSystem {
		return nil // no figure and no parallel lease for system work (Q-LIM-66)
	}
	c.download.Store(true)
	c.mu.Lock()
	held := c.lease != nil
	c.mu.Unlock()
	if held {
		return nil
	}
	f := c.Figures()
	bucket := cluster.LimitBucket(c.Key)
	now := e.now()
	var b cluster.LimitBatch
	chOp, bdOp := -1, -1
	if f[CachedBytesPerHour] > 0 {
		idx, _ := cluster.WindowIndex(now, hour)
		chOp = b.Add(cluster.LimitOp{Kind: cluster.OpWindowRead, Figure: ctrCachedHour, Bucket: bucket, Window: hour, Index: idx})
	}
	if f[BytesPerDay] > 0 {
		idx, _ := cluster.WindowIndex(now, day)
		bdOp = b.Add(cluster.LimitOp{Kind: cluster.OpWindowRead, Figure: ctrBytesDay, Bucket: bucket, Window: day, Index: idx})
	}
	parOp := -1
	if f[ParallelDownloads] > 0 {
		parOp = e.addLease(&b, c, now)
	}
	res, local, err := e.run(ctx, &b)
	if err != nil {
		return err
	}
	enforce := c.Mode == ModeEnforce
	for _, chk := range []struct {
		op  int
		fig Figure
		w   time.Duration
		ctr string
	}{{chOp, CachedBytesPerHour, hour, ctrCachedHour}, {bdOp, BytesPerDay, day, ctrBytesDay}} {
		if chk.op < 0 {
			continue
		}
		_, el := cluster.WindowIndex(now, chk.w)
		lim := e.scaled(f[chk.fig], local)
		r := res[chk.op]
		if cluster.Estimate(r.Cur, r.Prev, el) < float64(lim) {
			e.m.decision(chk.fig.String(), c.Mode, "admitted")
			continue
		}
		e.decided(c, chk.fig.String(), enforce)
		if enforce {
			e.dropLease(ctx, c)
			return e.refuse(c, refusal{
				code: CodeDownloadLimitReached, figure: chk.fig.String(), limit: f[chk.fig], window: chk.w,
				place: c.Resolution.Place, retry: cluster.RoomAfter(r.Cur, r.Prev, el, chk.w, lim, 1),
				signed: c.Subject.Kind != KindAnonymous, baseURL: e.baseURL(ctx),
			}, lim)
		}
	}
	if parOp >= 0 {
		return e.parallel(ctx, c, res[parOp].Count, local)
	}
	return nil
}

// addLease adds the request's lease to a batch.
func (e *Engine) addLease(b *cluster.LimitBatch, c *Charge, now time.Time) int {
	c.mu.Lock()
	if c.lease == nil {
		c.lease = &lease{key: c.Key, member: e.memberOf(c), taken: now, renewed: now}
	}
	l := *c.lease
	c.mu.Unlock()
	return b.Add(cluster.LimitOp{
		Kind: cluster.OpLeaseTake, Bucket: cluster.LimitBucket(l.key), Member: l.member,
		Until: now.Add(e.o.LeaseTTL), Now: now, TTL: 2 * e.o.LeaseTTL,
	})
}

// parallel decides a lease that was taken with count leases in the key's set (R-LIM-024): admitted at most
// parallelDownloads; otherwise the download waits, asking once per second up to limits.parallel.wait, then 429. While the
// downloads that wait on the endpoint hold its share of the request slots (WaitingShare), the download does not wait and is
// refused at once (Q-LIM-60). In audit the lease is kept and a would-limit recorded.
func (e *Engine) parallel(ctx context.Context, c *Charge, count int64, local bool) error {
	f := c.Figures()
	lim := e.scaledUp(f[ParallelDownloads], local)
	if count <= lim {
		e.m.decision(ParallelDownloads.String(), c.Mode, "admitted")
		return nil
	}
	if c.Mode != ModeEnforce {
		e.decided(c, ParallelDownloads.String(), false)
		return nil
	}
	c.mu.Lock()
	gate := c.gate
	c.mu.Unlock()
	if e.o.ParallelWait > 0 {
		if !gate.enter() {
			e.dropLease(ctx, c)
			return e.refuseParallel(ctx, c)
		}
		defer gate.leave()
	}
	deadline := e.now().Add(e.o.ParallelWait)
	for {
		e.dropLease(ctx, c)
		if !e.now().Before(deadline) {
			break
		}
		t := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
		var b cluster.LimitBatch
		op := e.addLease(&b, c, e.now())
		res, local, err := e.run(ctx, &b)
		if err != nil {
			e.dropLease(context.WithoutCancel(ctx), c)
			return err
		}
		if res[op].Count <= e.scaledUp(f[ParallelDownloads], local) {
			e.m.decision(ParallelDownloads.String(), c.Mode, "admitted")
			return nil
		}
	}
	return e.refuseParallel(ctx, c)
}

// refuseParallel is the 429 too_many_parallel_downloads with a Retry-After of one second (R-LIM-020).
func (e *Engine) refuseParallel(ctx context.Context, c *Charge) error {
	f := c.Figures()
	e.decided(c, ParallelDownloads.String(), true)
	return e.refuse(c, refusal{
		code: CodeTooManyParallelDownloads, figure: ParallelDownloads.String(), limit: f[ParallelDownloads],
		place: c.Resolution.Place, retry: time.Second, signed: c.Subject.Kind != KindAnonymous, baseURL: e.baseURL(ctx),
	}, e.scaledUp(f[ParallelDownloads], false))
}

// dropLease gives a lease up at once (ZREM): a refused or waiting download does not hold a slot.
func (e *Engine) dropLease(ctx context.Context, c *Charge) {
	c.mu.Lock()
	l := c.lease
	c.lease = nil
	c.mu.Unlock()
	if l == nil {
		return
	}
	e.dropLocal([]lease{*l})
	if e.o.Store == nil {
		return
	}
	var b cluster.LimitBatch
	b.Add(e.dropOp(*l))
	if _, local, err := e.run(ctx, &b); err != nil || local {
		e.keepUnsent([]lease{*l})
	}
}

// dropOp is the drop of a lease (ZREM). A lease may be held in two places (R-LIM-019, R-LIM-024, Q-LIM-95): the batch that
// took it ran in Valkey or on the replica's share, and while Valkey answers late or not at all a replica's batches go to one
// or the other from one check to the next. A take that ran into valkey.command_timeout is decided on the share, but Valkey
// may still run it when it answers again. So a lease is given up in both places: on the share at once, in Valkey by a drop
// that is sent again at every flush until one reaches Valkey or the lease has ended there on its own. Without that, the
// downloads that ended keep their places for limits.parallel.lease and fill the share or the cluster's count, and a caller is
// refused because Valkey failed.
func (e *Engine) dropOp(l lease) cluster.LimitOp {
	return cluster.LimitOp{Kind: cluster.OpLeaseDrop, Bucket: cluster.LimitBucket(l.key), Member: l.member, TTL: 2 * e.o.LeaseTTL}
}

// dropLocal gives leases up on the replica's share.
func (e *Engine) dropLocal(ls []lease) {
	if len(ls) == 0 {
		return
	}
	var b cluster.LimitBatch
	for _, l := range ls {
		b.Add(e.dropOp(l))
	}
	_, _ = e.local.Run(context.Background(), &b)
}

// renewLocal moves the end of leases that the replica's share holds.
func (e *Engine) renewLocal(ls []lease, now time.Time) {
	if len(ls) == 0 {
		return
	}
	var b cluster.LimitBatch
	for _, l := range ls {
		b.Add(cluster.LimitOp{Kind: cluster.OpLeaseRenew, Bucket: cluster.LimitBucket(l.key), Member: l.member, Until: now.Add(e.o.LeaseTTL)})
	}
	_, _ = e.local.Run(context.Background(), &b)
}

// keepUnsent remembers drops that did not reach Valkey, until the end of each lease there: its last renewal plus
// limits.parallel.lease. Beyond cluster.MaxFallbackBuckets a drop is not kept; its lease ends on its own.
func (e *Engine) keepUnsent(ls []lease) {
	if e.o.Store == nil || len(ls) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, l := range ls {
		if len(e.unsent) >= cluster.MaxFallbackBuckets {
			return
		}
		e.unsent[unsentDrop{bucket: cluster.LimitBucket(l.key), member: l.member}] = l.renewed.Add(e.o.LeaseTTL)
	}
}

// takeUnsent returns the drops still to be sent and forgets those whose lease has ended in Valkey.
func (e *Engine) takeUnsent(now time.Time) []unsentDrop {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]unsentDrop, 0, len(e.unsent))
	for d, end := range e.unsent {
		if !now.Before(end) {
			delete(e.unsent, d)
			continue
		}
		out = append(out, d)
	}
	return out
}

// sentUnsent forgets drops that reached Valkey.
func (e *Engine) sentUnsent(ds []unsentDrop) {
	e.mu.Lock()
	for _, d := range ds {
		delete(e.unsent, d)
	}
	e.mu.Unlock()
}

// MayFetch is step 9a of R-REPO-025 (R-LIM-022): the request would contact the upstream of the remote repository repoID; the
// budget key needs room in upstreamRequestsPerHour and uncachedBytesPerHour, and the remote's ceiling of the caller's class
// room in both of its figures. An artifact GET also takes the parallel lease. The figures are decided and the upstream request
// counted in one take, for the budget key and the ceiling, so an upstream request a figure refuses is counted nowhere; one the
// parallel lease refuses after the take admitted it is given back (Q-LIM-110). A nil charge (a background request, a replica
// without the limits) passes.
func (e *Engine) MayFetch(ctx context.Context, c *Charge, repoID int64, artifactGet bool) error {
	if c == nil || c.Subject.Kind == KindSystem {
		return nil
	}
	x := e.index()
	f := c.Figures()
	class := CeilingSignedIn
	if c.Subject.Kind == KindAnonymous {
		class = CeilingAnonymous
	}
	ceil, hasCeil := x.Ceiling(repoID, class)
	now := e.now()
	hIdx, el := cluster.WindowIndex(now, hour)
	take := cluster.LimitOp{Kind: cluster.OpTake, N: 1}
	var chks []fetchCheck
	add := func(key string, fig Figure, limit int64, ceiling bool) {
		ctr, peek := ctrUpstreamHour, false
		if fig == UncachedBytesPerHour {
			ctr, peek = ctrUncachedHour, true // the bytes are counted where they are read; the request needs room in them
		}
		take.Windows = append(take.Windows, cluster.WindowTake{
			Figure: ctr, Bucket: cluster.LimitBucket(key), Window: hour, Index: hIdx, Elapsed: el, Limit: limit, Peek: peek,
		})
		chks = append(chks, fetchCheck{i: len(take.Windows) - 1, fig: fig, limit: limit, ceiling: ceiling})
	}
	for _, fig := range []Figure{UpstreamRequestsPerHour, UncachedBytesPerHour} {
		if f[fig] > 0 {
			add(c.Key, fig, f[fig], false)
		}
	}
	if hasCeil {
		if ceil.UpstreamRequestsPerHour > 0 {
			add(ceil.Key(), UpstreamRequestsPerHour, ceil.UpstreamRequestsPerHour, true)
		}
		if ceil.UncachedBytesPerHour > 0 {
			add(ceil.Key(), UncachedBytesPerHour, ceil.UncachedBytesPerHour, true)
		}
		c.mu.Lock()
		c.ceiling = ceil.Key()
		c.mu.Unlock()
	}
	var b cluster.LimitBatch
	takeOp, parOp := -1, -1
	if len(take.Windows) > 0 {
		takeOp = b.Add(take)
	}
	if artifactGet && !c.bg && f[ParallelDownloads] > 0 {
		c.download.Store(true)
		c.mu.Lock()
		held := c.lease != nil
		c.mu.Unlock()
		if !held {
			parOp = e.addLease(&b, c, now)
		}
	}
	res, local, err := e.run(ctx, &b)
	if err != nil {
		return err
	}
	if takeOp >= 0 {
		if err := e.decideFetch(ctx, c, res[takeOp], chks, class, el, local); err != nil {
			return err
		}
	}
	if parOp >= 0 {
		if err := e.parallel(ctx, c, res[parOp].Count, local); err != nil {
			if takeOp >= 0 && res[takeOp].Admitted {
				e.giveBack(ctx, b.Ops[takeOp], local)
			}
			return err
		}
	}
	// Only an upstream request that goes on is one (R-LIM-006): the usage store and the metric learn it now.
	e.m.upstream(c.Subject.Kind)
	c.upstreamCounted.Add(1)
	return nil
}

// fetchCheck is one figure of step 9a: its window's index in the take, its limit, and whether it is the remote's ceiling.
type fetchCheck struct {
	i       int
	fig     Figure
	limit   int64
	ceiling bool
}

// decideFetch decides the figures of step 9a from what the take found in each window: the first that has no room refuses the
// request in enforce; in audit every one is recorded as would_limit and the request goes on.
func (e *Engine) decideFetch(ctx context.Context, c *Charge, r cluster.LimitResult, chks []fetchCheck, class CeilingClass, el float64,
	local bool,
) error {
	enforce := c.Mode == ModeEnforce
	for _, k := range chks {
		s := r.Windows[k.i]
		name := k.fig.String()
		if k.ceiling {
			name = "ceiling." + ceilingAPIName(class)
		}
		if s.Fits {
			e.m.decision(name, c.Mode, "admitted")
			continue
		}
		e.decided(c, name, enforce)
		if enforce {
			e.dropLease(ctx, c)
			lim := e.scaled(k.limit, local)
			rf := refusal{
				code: CodeUpstreamLimitReached, figure: k.fig.String(), limit: k.limit, window: hour, place: c.Resolution.Place,
				retry: cluster.RoomAfter(s.Cur, s.Prev, el, hour, lim, 1), signed: c.Subject.Kind != KindAnonymous, baseURL: e.baseURL(ctx),
			}
			if k.ceiling {
				rf.ceiling = class
			}
			return &BudgetError{Err: e.refuse(c, rf, lim)}
		}
	}
	return nil
}

// giveBack undoes an admitted take whose request a later check of the same step refused (Q-LIM-110): on the replica's share at
// once, in Valkey by one more round trip, which takes back what the take added unless its window has ended. A give-back that
// does not reach Valkey leaves the unit counted until its window ends.
func (e *Engine) giveBack(ctx context.Context, op cluster.LimitOp, local bool) {
	if local || e.o.Store == nil {
		e.local.Return(op)
		return
	}
	op.Kind = cluster.OpGiveBack
	var b cluster.LimitBatch
	b.Add(op)
	_, _ = e.o.Store.Run(context.WithoutCancel(ctx), &b)
}

func ceilingAPIName(c CeilingClass) string {
	if c == CeilingSignedIn {
		return "signedIn"
	}
	return "anonymous"
}

// BudgetError is the refusal of step 9a: the remote engine serves an expired document stale within maxStaleSeconds instead
// of it (R-LIM-022), and answers it otherwise.
type BudgetError struct{ Err *errs.Error }

func (b *BudgetError) Error() string { return b.Err.Error() }

// Unwrap returns the 429.
func (b *BudgetError) Unwrap() error { return b.Err }

// Finish ends a request: its lease is given up at the next flush, its deltas are flushed, and the request log learns the
// subject, the place, the rule and the outcome (R-LIM-030).
func (e *Engine) Finish(c *Charge) {
	if c == nil {
		return
	}
	c.finished.Store(true)
}

// FailedCredential records a request whose credential the authentication stage refused (R-LIM-003): one request and one
// refused request of the address of its client, through the buffer. It is checked against nothing.
func (e *Engine) FailedCredential(caller Caller, repo qname.Repo) {
	caller.Anonymous, caller.Kind = true, CallerAnonymous
	c := e.Begin(caller, repo)
	c.MarkRefused()
	c.finished.Store(true)
}

// retarget moves a token request to the subject its handler authenticated (R-LIM-003): the request is counted in the new budget
// key's request windows at once, and the usage store records the charge's one request under the new subject once it has ended.
func (e *Engine) retarget(ctx context.Context, c *Charge, s Subject) {
	res, key, mode := e.resolve(s, c.Addressed)
	c.mu.Lock()
	c.Subject, c.Resolution, c.Key, c.Mode = s, res, key, mode
	c.mu.Unlock()
	f := res.Figures
	var b cluster.LimitBatch
	bucket := cluster.LimitBucket(key)
	now := e.now()
	if bw := burstWindow(f); bw > 0 {
		idx, _ := cluster.WindowIndex(now, bw)
		b.Add(cluster.LimitOp{Kind: cluster.OpWindowAdd, Figure: burstCounter(bw), Bucket: bucket, Window: bw, Index: idx, N: 1})
	}
	if f[RequestsPerHour] > 0 {
		idx, _ := cluster.WindowIndex(now, hour)
		b.Add(cluster.LimitOp{Kind: cluster.OpWindowAdd, Figure: ctrRequestsHour, Bucket: bucket, Window: hour, Index: idx, N: 1})
	}
	_, _, _ = e.run(ctx, &b)
}

// System returns the charge of the installation's own work on a repository (R-LIM-001, Q-LIM-19): its upstream requests and
// uncached bytes are recorded in the usage store under the subject system and checked against nothing.
func (e *Engine) System(repoID int64) *Charge {
	if e == nil {
		return nil
	}
	c := &Charge{
		e: e, Subject: System(), Resolution: Resolution{Place: InstancePlace()}, Key: BudgetKey(System(), InstancePlace()),
		Mode: e.Mode(), id: e.nextID(), started: e.now(),
	}
	c.served.Store(repoID)
	e.track(c)
	return c
}

// track adds a charge to the buffer the flush walks.
func (e *Engine) track(c *Charge) {
	e.mu.Lock()
	e.active[c] = struct{}{}
	e.mu.Unlock()
}

// NewBlockID returns the ID of a new block (UUIDv7).
func NewBlockID() string { return uuid.Must(uuid.NewV7()).String() }
