package cluster

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Scope is where a limiter counts (R-ARCH-034).
type Scope string

const (
	// ScopeCluster counts across all replicas in Valkey and falls back to a
	// share of the budget per replica while Valkey cannot be used.
	ScopeCluster Scope = "cluster"
	// ScopeReplica counts in the replica's memory; it protects the replica
	// itself and never uses Valkey.
	ScopeReplica Scope = "replica"
)

// Mode is how one decision was made; it is the mode label of
// quader_ratelimit_decisions_total.
type Mode string

// The decision modes.
const (
	ModeCluster  Mode = "cluster"
	ModeFallback Mode = "fallback"
	ModeReplica  Mode = "replica"
)

// Verdict is the outcome of one check.
type Verdict struct {
	Allowed bool
	// Limit is the burst of the policy; during the fallback the replica's share.
	Limit int
	// Remaining is what the bucket still admits, at least 0.
	Remaining int
	// RetryAfter is, when the request is refused, the time after which the next
	// request would be admitted if the caller sent nothing more, rounded up to
	// whole seconds and at least one second. Zero when allowed.
	RetryAfter time.Duration
	// Reset is the time until the current window ends (the t of the RateLimit
	// field).
	Reset time.Duration
	// Window is the window of the policy, burst divided by rate (at least 1 s).
	Window time.Duration
	Mode   Mode

	// back is what [Limiter.GiveBack] undoes: the units an admitted take counted on its own, nil where nothing can be given
	// back (a refusal, or a take that was counted together with other windows and is all or nothing).
	back *takeBack
}

// takeBack is where an admitted take counted its units: a window in Valkey, or the token bucket of the replica.
type takeBack struct {
	w      WindowTake
	bucket string
	units  int
	local  bool
}

// windowStore is the counter storage of the cluster scope. *Valkey is the
// production implementation (valkey_ratelimit.go); the unit tests use a map.
type windowStore interface {
	// take counts n units in the current window if the estimate with them stays within limit, and returns the counters
	// without them and whether they were counted.
	take(ctx context.Context, w WindowTake, n int64) (st WindowState, err error)
	// giveBack subtracts n units an admitted take counted from the current window, never below 0 (Q-LIM-110).
	giveBack(ctx context.Context, w WindowTake, n int64) error
	// hit counts one event whatever the limit and returns the counters with it.
	hit(ctx context.Context, limiter, bucket string, idx int64, ttl time.Duration) (cur, prev int64, err error)
	read(ctx context.Context, limiter, bucket string, idx int64, ttl time.Duration) (cur, prev int64, err error)
	count(ctx context.Context, limiter, bucket string, idx int64, ttl time.Duration) error
	available() bool
	degraded()
}

type valkeyWindows struct{ v *Valkey }

func (w valkeyWindows) take(ctx context.Context, win WindowTake, n int64) (WindowState, error) {
	return w.v.windowTake(ctx, win, n)
}

func (w valkeyWindows) giveBack(ctx context.Context, win WindowTake, n int64) error {
	return w.v.windowGiveBack(ctx, win, n)
}

func (w valkeyWindows) hit(ctx context.Context, limiter, bucket string, idx int64, ttl time.Duration) (int64, int64, error) {
	return w.v.windowHit(ctx, limiter, bucket, idx, ttl)
}

func (w valkeyWindows) read(ctx context.Context, limiter, bucket string, idx int64, ttl time.Duration) (int64, int64, error) {
	return w.v.windowRead(ctx, limiter, bucket, idx, ttl)
}

func (w valkeyWindows) count(ctx context.Context, limiter, bucket string, idx int64, ttl time.Duration) error {
	return w.v.windowCount(ctx, limiter, bucket, idx, ttl)
}

func (w valkeyWindows) available() bool { return w.v.Available() }
func (w valkeyWindows) degraded()       { w.v.CountDegraded(MechanismRateLimit) }

// LimiterOptions configure [NewLimiterSet].
type LimiterOptions struct {
	// Valkey carries the cluster-scoped counters; nil means every limiter runs
	// its fallback.
	Valkey *Valkey
	// Registry receives quader_ratelimit_decisions_total,
	// quader_ratelimit_valkey_failures_total and quader_ratelimit_fallback; nil
	// registers nothing.
	Registry prometheus.Registerer
	// LiveReplicas returns the number n of live replicas of this replica's role
	// in the replica registry, refreshed with the heartbeat (R-ARCH-034). Values
	// below 1 count as 1; nil means 1.
	LiveReplicas func() int
	// Now is the clock; nil means time.Now. Tests set it.
	Now func() time.Time
	// store replaces the Valkey counters in unit tests.
	store windowStore
}

// MaxFallbackBuckets bounds the buckets one limiter keeps in memory while it
// runs its fallback or has replica scope. When it is reached, idle buckets are
// dropped first; a bucket that is dropped starts full, which admits at most one
// extra burst of its caller.
const MaxFallbackBuckets = 100_000

// LimiterSet creates the named limiters of a replica and exports their metrics.
type LimiterSet struct {
	store windowStore
	n     func() int
	now   func() time.Time

	decisions *prometheus.CounterVec
	// failures counts a limiter's calls to Valkey that failed (quader_ratelimit_valkey_failures_total): each held its request for
	// at most valkey.command_timeout before the replica's share decided (R-ARCH-031, R-ARCH-034).
	failures *prometheus.CounterVec

	mu       sync.Mutex
	limiters map[string]*Limiter
	// fallbacks are further mechanisms that report quader_ratelimit_fallback under a name of their own (the download limits,
	// R-LIM-019): 1 while fn reports the replica's share.
	fallbacks map[string]func() bool
}

// AddFallback reports a further mechanism in quader_ratelimit_fallback{limiter=name}: 1 while fn returns true.
func (ls *LimiterSet) AddFallback(name string, fn func() bool) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.fallbacks == nil {
		ls.fallbacks = map[string]func() bool{}
	}
	ls.fallbacks[name] = fn
}

// NewLimiterSet creates the factory of the limiters of one replica.
func NewLimiterSet(o LimiterOptions) (*LimiterSet, error) {
	ls := &LimiterSet{store: o.store, n: o.LiveReplicas, now: o.Now, limiters: map[string]*Limiter{}}
	if ls.store == nil && o.Valkey != nil {
		ls.store = valkeyWindows{o.Valkey}
	}
	if ls.now == nil {
		ls.now = time.Now
	}
	ls.decisions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "quader_ratelimit_decisions_total",
		Help: "Rate-limit checks by limiter, mode (cluster, fallback, replica) and outcome (admitted, refused).",
	}, []string{"limiter", "mode", "outcome"})
	ls.failures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "quader_ratelimit_valkey_failures_total",
		Help: "Calls of a cluster-scoped limiter to Valkey that failed, by limiter; each waited at most valkey.command_timeout.",
	}, []string{"limiter"})
	if o.Registry != nil {
		if err := o.Registry.Register(ls.decisions); err != nil {
			return nil, fmt.Errorf("cluster: registering quader_ratelimit_decisions_total: %w", err)
		}
		if err := o.Registry.Register(ls.failures); err != nil {
			return nil, fmt.Errorf("cluster: registering quader_ratelimit_valkey_failures_total: %w", err)
		}
		if err := o.Registry.Register(fallbackCollector{ls}); err != nil {
			return nil, fmt.Errorf("cluster: registering quader_ratelimit_fallback: %w", err)
		}
	}
	return ls, nil
}

var limiterNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

// NewLimiter registers a limiter: name is lower case words separated by "_" or
// "." (api_read, auth.credential_failures), rate the sustained requests per
// second and burst the most requests one window admits. The window is burst
// divided by rate, at least one second (R-ARCH-034). It panics on a bad or
// duplicate name or a window above 12 hours, because each is a programming
// error that start-up must surface.
func (ls *LimiterSet) NewLimiter(name string, scope Scope, rate float64, burst int) *Limiter {
	return ls.NewLimiterShared(name, scope, rate, burst, nil)
}

// NewLimiterShared is NewLimiter for a limiter whose fallback share divides by a
// number of its own instead of the live replicas of the role: share returns the
// live replicas that serve what the limiter guards, for example the replicas of
// an endpoint's pool (R-EP-015). Values below 1 count as 1; nil is NewLimiter.
func (ls *LimiterSet) NewLimiterShared(name string, scope Scope, rate float64, burst int, share func() int) *Limiter {
	if !limiterNamePattern.MatchString(name) {
		panic(fmt.Sprintf("cluster: invalid limiter name %q", name))
	}
	if scope != ScopeCluster && scope != ScopeReplica {
		panic(fmt.Sprintf("cluster: limiter %q has scope %q", name, scope))
	}
	if rate <= 0 || burst < 1 {
		panic(fmt.Sprintf("cluster: limiter %q needs a positive rate and a burst of 1 or more", name))
	}
	w := time.Duration(float64(burst) / rate * float64(time.Second))
	if w < time.Second {
		w = time.Second
	}
	if w > maxLimiterWindow {
		panic(fmt.Sprintf("cluster: limiter %q has a window of %s, the longest is %s", name, w, maxLimiterWindow))
	}
	// Whole milliseconds, so the window index is an integer.
	w = w.Truncate(time.Millisecond)
	l := &Limiter{
		set: ls, name: name, keyName: strings.ReplaceAll(name, ".", "-"), scope: scope,
		rate: rate, burst: burst, window: w, buckets: map[string]*bucket{}, shareFn: share,
	}
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if _, dup := ls.limiters[name]; dup {
		panic(fmt.Sprintf("cluster: limiter %q is registered twice", name))
	}
	ls.limiters[name] = l
	return l
}

// share returns n, the number of live replicas of the role, at least 1.
func (ls *LimiterSet) share() int {
	if ls.n == nil {
		return 1
	}
	return max(1, ls.n())
}

// Limiter is one named limiter. It is safe for concurrent use.
type Limiter struct {
	set     *LimiterSet
	name    string
	keyName string
	scope   Scope
	rate    float64
	burst   int
	window  time.Duration

	inFallback atomic.Bool
	// shareFn is the n of the fallback share when the limiter has its own
	// (NewLimiterShared); nil means the set's.
	shareFn func() int

	mu      sync.Mutex
	buckets map[string]*bucket
}

// share returns the n the fallback divides the budget by, at least 1.
func (l *Limiter) share() int {
	if l.shareFn != nil {
		return max(1, l.shareFn())
	}
	return l.set.share()
}

// bucket is the local state of one bucket key: a token bucket for Take, and
// sliding-window counters for the count-only form.
type bucket struct {
	tokens   float64
	updated  time.Time
	idx      int64
	cur, pre int64
}

// Name is the limiter's name.
func (l *Limiter) Name() string { return l.name }

// Burst is the most requests one window admits.
func (l *Limiter) Burst() int { return l.burst }

// Window is the window of the policy.
func (l *Limiter) Window() time.Duration { return l.window }

// Rate is the sustained requests per second.
func (l *Limiter) Rate() float64 { return l.rate }

func (l *Limiter) bucketKey(bucket string) string {
	sum := sha256.Sum256([]byte(l.name + "\x00" + bucket))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (l *Limiter) windowIndex(now time.Time) (idx int64, elapsed float64) {
	w := l.window.Milliseconds()
	ms := now.UnixMilli()
	return ms / w, float64(ms%w) / float64(w)
}

// usesValkey reports whether a check should try the shared counters.
func (l *Limiter) usesValkey() bool {
	return l.scope == ScopeCluster && l.set.store != nil && l.set.store.available()
}

// Allow is Take reduced to its verdict: it counts the request, then reports
// whether it was admitted.
func (l *Limiter) Allow(ctx context.Context, bucket string) bool { return l.Take(ctx, bucket).Allowed }

// Take decides one request of bucket and counts it if it is admitted
// (R-ARCH-034, Q-LIM-61): a refused request uses up nothing, so a caller that
// waits the Retry-After of its refusal is admitted then, and a caller that keeps
// sending gets the configured rate and no more. While Valkey cannot be used the
// check runs against a local token bucket with the rate divided by the number of
// live replicas, which takes nothing when it refuses either; it never fails and
// never waits for more than valkey.command_timeout.
func (l *Limiter) Take(ctx context.Context, bucket string) Verdict {
	return l.TakeAt(ctx, bucket, time.Time{})
}

// TakeAt is Take for a request that arrived at at. In the cluster scope it is
// counted in, and decided against, the window it arrived in (Q-HA-66): a request
// that a stage before the limiter held, the token lookup during a database
// switchover for one, would otherwise be charged to the window it is released
// in, where the held requests of a client below its rate add up to a burst it
// never sent. A zero or future at is now; the local buckets decide at now.
func (l *Limiter) TakeAt(ctx context.Context, bucket string, at time.Time) Verdict {
	return l.TakeNAt(ctx, bucket, 1, at)
}

// MaxTakeUnits bounds the units one TakeNAt charges; a caller groups what it counts into units of a size that keeps n
// small (R-BAS-016, Q-BAS-117).
const MaxTakeUnits = 100

// TakeNAt is TakeAt for a request that is charged n units instead of one: a batch of download events is
// charged by the units of events it holds (R-BAS-016, Q-BAS-117). A request is admitted, and its n units
// counted, when the window holds them all, so n above the burst is never admitted; a refused one counts
// nothing. n below 1 counts as 1; n above MaxTakeUnits is refused.
func (l *Limiter) TakeNAt(ctx context.Context, bucket string, n int, at time.Time) Verdict {
	n = max(1, n)
	if n > MaxTakeUnits {
		mode := ModeCluster
		if l.scope == ScopeReplica {
			mode = ModeReplica
		}
		return l.record(Verdict{Limit: l.burst, Window: l.window, Reset: l.window, RetryAfter: wholeSeconds(l.window), Mode: mode})
	}
	now := l.set.now()
	if at.IsZero() || at.After(now) {
		at = now
	}
	if l.scope == ScopeReplica {
		return l.record(l.takeLocal(bucket, now, 1, n, ModeReplica))
	}
	if l.usesValkey() {
		w := l.windowTake(bucket, at)
		st, err := l.set.store.take(ctx, w, int64(n))
		if err == nil {
			l.inFallback.Store(false)
			v := l.windowVerdict(st, w.Elapsed, n)
			if v.Allowed {
				v.back = &takeBack{w: w, units: n}
			}
			return l.record(v)
		}
		l.failedCall()
	} else if l.set.store != nil {
		l.set.store.degraded()
	}
	l.inFallback.Store(true)
	return l.record(l.takeLocal(bucket, now, l.share(), n, ModeFallback))
}

// windowTake is the limiter's window of a request of bucket that arrived at at.
func (l *Limiter) windowTake(bucket string, at time.Time) WindowTake {
	idx, el := l.windowIndex(at)
	return WindowTake{
		Class: true, Figure: l.keyName, Bucket: l.bucketKey(bucket), Window: l.window, Index: idx, Elapsed: el,
		Limit: int64(l.burst),
	}
}

// BatchTake is TakeAt for a check that shares its take with other windows: it adds the limiter's window to op, an OpTake (the
// class of R-API-016 is decided with the request windows of the download limits, R-LIM-017, and counted only when every
// window of the take admits the request), and returns the function that decides once the batch ran, from the take's result
// or, when the batch failed or Valkey cannot be used, from the replica's share. A limiter of replica scope decides at once and
// adds nothing. A verdict decided in the take gives nothing back ([Limiter.GiveBack]): the take is all or nothing; one decided
// on the replica's share does, for a request the take's other windows refused there.
func (l *Limiter) BatchTake(op *LimitOp, bucket string, at time.Time) func(res LimitResult, err error) Verdict {
	now := l.set.now()
	if at.IsZero() || at.After(now) {
		at = now
	}
	if l.scope == ScopeReplica {
		v := l.record(l.takeLocal(bucket, now, 1, 1, ModeReplica))
		return func(LimitResult, error) Verdict { return v }
	}
	w := l.windowTake(bucket, at)
	i := len(op.Windows)
	op.Windows = append(op.Windows, w)
	return func(res LimitResult, err error) Verdict {
		if err == nil && i < len(res.Windows) {
			l.inFallback.Store(false)
			return l.record(l.windowVerdict(res.Windows[i], w.Elapsed, 1))
		}
		if l.set.store != nil {
			l.set.store.degraded()
		}
		l.inFallback.Store(true)
		return l.record(l.takeLocal(bucket, now, l.share(), 1, ModeFallback))
	}
}

// GiveBack undoes a take the limiter admitted on its own, for a request that a check after it refused (R-ARCH-034, Q-LIM-61,
// Q-LIM-110): an endpoint's class of R-EP-015 runs before stage 7, so a request stage 7 refuses is given back to it. In Valkey
// one round trip subtracts the units from the window they were counted in (never below 0, never in a window that has ended);
// on the replica's token bucket its tokens are returned. A refused verdict, and one decided in a take with other windows,
// give nothing back. A give-back that does not reach Valkey leaves the units counted until their window ends.
func (l *Limiter) GiveBack(ctx context.Context, v Verdict) {
	b := v.back
	if b == nil || !v.Allowed {
		return
	}
	if b.local {
		l.refundLocal(b.bucket, b.units)
		return
	}
	if l.set.store == nil {
		return
	}
	if err := l.set.store.giveBack(context.WithoutCancel(ctx), b.w, int64(b.units)); err != nil {
		l.failedCall()
	}
}

// refundLocal returns units tokens to the replica's bucket of key, at most to its capacity.
func (l *Limiter) refundLocal(key string, units int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[l.bucketKey(key)]
	if !ok {
		return
	}
	n := 1
	if l.scope != ScopeReplica {
		n = l.share()
	}
	b.tokens = math.Min(float64(max(1, l.burst/n)), b.tokens+float64(units))
}

// windowVerdict applies the sliding-window rule to what a take found: the request of units is admitted when previous x
// (1 - elapsed / W) + current + units <= burst, and the counters are those without it.
func (l *Limiter) windowVerdict(st WindowState, elapsed float64, units int) Verdict {
	est := float64(st.Prev)*(1-elapsed) + float64(st.Cur) + float64(units)
	v := Verdict{
		Allowed: st.Fits, Limit: l.burst, Window: l.window, Mode: ModeCluster,
		Remaining: max(0, int(math.Floor(float64(l.burst)-est))),
		Reset:     time.Duration((1 - elapsed) * float64(l.window)),
	}
	if !v.Allowed {
		v.Remaining = max(0, int(math.Floor(float64(l.burst)-est+float64(units))))
		v.RetryAfter = wholeSeconds(l.retryAfter(st.Cur, st.Prev, elapsed, units))
	}
	return v
}

// retryAfter is the time after which a request of units would be admitted if the
// caller sent nothing more, computed from the counters of the check, which do not
// hold the refused request. It solves the admission rule for the rest of the
// current window, and when the current window cannot admit again, for the next
// one, whose previous counter is cur.
func (l *Limiter) retryAfter(cur, prev int64, elapsed float64, units int) time.Duration {
	burst, w, u := float64(l.burst), float64(l.window), float64(units)
	// Within the current window the next request counts as cur+units and is admitted
	// when prev x (1 - x') <= burst - cur - units, that is from x' = 1 - need/prev.
	if need := burst - float64(cur) - u; need >= 0 {
		if prev == 0 {
			return 0
		}
		if x := 1 - need/float64(prev); x < 1 {
			return time.Duration((max(x, elapsed) - elapsed) * w)
		}
	}
	// Next window: previous = cur, current = 0, the next request counts as units.
	y := 0.0
	if c := float64(cur); c > 0 {
		y = max(0, 1-(burst-u)/c)
	}
	return time.Duration(((1 - elapsed) + y) * w)
}

func wholeSeconds(d time.Duration) time.Duration {
	s := time.Duration(math.Ceil(d.Seconds())) * time.Second
	return max(s, time.Second)
}

// takeLocal is the token bucket of the fallback and of the replica scope, taking
// units tokens. The bucket has rate/n and burst max(1, burst/n).
func (l *Limiter) takeLocal(key string, now time.Time, n, units int, mode Mode) Verdict {
	rate := l.rate / float64(n)
	capacity := float64(max(1, l.burst/n))
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.local(key, now)
	if b.updated.IsZero() {
		b.tokens, b.updated = capacity, now
	} else if dt := now.Sub(b.updated).Seconds(); dt > 0 {
		b.tokens, b.updated = math.Min(capacity, b.tokens+dt*rate), now
	}
	v := Verdict{Limit: int(capacity), Window: l.window, Mode: mode, Reset: l.window}
	if u := float64(units); b.tokens >= u {
		b.tokens -= u
		v.Allowed = true
		v.back = &takeBack{bucket: key, units: units, local: true}
	} else {
		v.RetryAfter = wholeSeconds(time.Duration((u - b.tokens) / rate * float64(time.Second)))
	}
	v.Remaining = int(math.Floor(b.tokens))
	return v
}

// local returns the bucket of key, creating it. The caller holds l.mu.
func (l *Limiter) local(key string, now time.Time) *bucket {
	hashed := l.bucketKey(key)
	if b, ok := l.buckets[hashed]; ok {
		return b
	}
	if len(l.buckets) >= MaxFallbackBuckets {
		l.sweep(now)
	}
	b := &bucket{}
	l.buckets[hashed] = b
	return b
}

// sweep drops buckets that are idle for two windows, and when that frees
// nothing, an arbitrary bucket, so the map stays bounded. The caller holds l.mu.
func (l *Limiter) sweep(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.updated) > 2*l.window {
			delete(l.buckets, k)
		}
	}
	for k := range l.buckets {
		if len(l.buckets) < MaxFallbackBuckets {
			return
		}
		delete(l.buckets, k)
	}
}

// Exceeded reports whether the failures counted for bucket in the sliding
// window have used up the burst: previous x (1 - elapsed / W) + current >= burst,
// so a limit of n failures admits n checks and refuses the next (Q-AUTH-20). It
// reads the counters and writes nothing. It is the check of the count-only
// form, made before a request is processed (R-ARCH-034). While Valkey cannot
// be used it asks the local counters, whose limit is max(1, burst/n).
func (l *Limiter) Exceeded(ctx context.Context, bucket string) bool {
	now := l.set.now()
	idx, el := l.windowIndex(now)
	if l.scope == ScopeCluster && l.usesValkey() {
		cur, prev, err := l.set.store.read(ctx, l.keyName, l.bucketKey(bucket), idx, 2*l.window)
		if err == nil {
			l.inFallback.Store(false)
			return float64(prev)*(1-el)+float64(cur) >= float64(l.burst)
		}
		l.failedCall()
	} else if l.scope == ScopeCluster && l.set.store != nil {
		l.set.store.degraded()
	}
	limit := l.burst
	if l.scope == ScopeCluster {
		l.inFallback.Store(true)
		limit = max(1, l.burst/l.share())
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.local(bucket, now)
	cur, prev := b.rolled(idx)
	exceeded := float64(prev)*(1-el)+float64(cur) >= float64(limit)
	l.countOutcome(l.localMode(), !exceeded)
	return exceeded
}

// Count records one failure of bucket: MULTI, INCR of the current window's key,
// EXPIRE NX, EXEC. While Valkey cannot be used it counts locally.
func (l *Limiter) Count(ctx context.Context, bucket string) {
	now := l.set.now()
	idx, _ := l.windowIndex(now)
	if l.scope == ScopeCluster && l.usesValkey() {
		if err := l.set.store.count(ctx, l.keyName, l.bucketKey(bucket), idx, 2*l.window); err == nil {
			l.inFallback.Store(false)
			return
		}
		l.failedCall()
	} else if l.scope == ScopeCluster && l.set.store != nil {
		l.set.store.degraded()
	}
	if l.scope == ScopeCluster {
		l.inFallback.Store(true)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.local(bucket, now)
	cur, _ := b.rolled(idx)
	b.cur = cur + 1
	b.updated = now
}

// rolled moves the sliding-window counters to window idx and returns them.
func (b *bucket) rolled(idx int64) (cur, prev int64) {
	switch b.idx {
	case idx:
	case idx - 1:
		b.pre, b.cur, b.idx = b.cur, 0, idx
	default:
		b.pre, b.cur, b.idx = 0, 0, idx
	}
	return b.cur, b.pre
}

func (l *Limiter) localMode() Mode {
	if l.scope == ScopeReplica {
		return ModeReplica
	}
	return ModeFallback
}

// failedCall records a call to Valkey that was made and failed: the mechanism ran degraded, and the limiter's own counter of the
// calls that held a request (quader_ratelimit_valkey_failures_total) grows. A check that does not try Valkey, because it cannot be
// used (the circuit is open), counts as degraded only.
func (l *Limiter) failedCall() {
	l.set.store.degraded()
	l.set.failures.WithLabelValues(l.name).Inc()
}

func (l *Limiter) record(v Verdict) Verdict {
	l.countOutcome(v.Mode, v.Allowed)
	return v
}

func (l *Limiter) countOutcome(mode Mode, admitted bool) {
	outcome := "refused"
	if admitted {
		outcome = "admitted"
	}
	l.set.decisions.WithLabelValues(l.name, string(mode), outcome).Inc()
}

// fallbackCollector exports quader_ratelimit_fallback{limiter}: 1 while a
// cluster-scoped limiter answers from its local share, which is the case from
// its first fallback decision until a decision used the shared counters again,
// and whenever Valkey cannot be used at all.
type fallbackCollector struct{ ls *LimiterSet }

var fallbackDesc = prometheus.NewDesc("quader_ratelimit_fallback",
	"1 while a cluster-scoped limiter answers from per-replica shares because Valkey is unavailable.", []string{"limiter"}, nil)

func (c fallbackCollector) Describe(ch chan<- *prometheus.Desc) { ch <- fallbackDesc }

func (c fallbackCollector) Collect(ch chan<- prometheus.Metric) {
	c.ls.mu.Lock()
	ls := make([]*Limiter, 0, len(c.ls.limiters))
	for _, l := range c.ls.limiters {
		ls = append(ls, l)
	}
	extra := make(map[string]func() bool, len(c.ls.fallbacks))
	for name, fn := range c.ls.fallbacks {
		extra[name] = fn
	}
	c.ls.mu.Unlock()
	for name, fn := range extra {
		v := 0.0
		if fn() {
			v = 1
		}
		ch <- prometheus.MustNewConstMetric(fallbackDesc, prometheus.GaugeValue, v, name)
	}
	down := c.ls.store != nil && !c.ls.store.available()
	for _, l := range ls {
		if l.scope != ScopeCluster {
			continue
		}
		v := 0.0
		if l.inFallback.Load() || down {
			v = 1
		}
		ch <- prometheus.MustNewConstMetric(fallbackDesc, prometheus.GaugeValue, v, l.name)
	}
}
