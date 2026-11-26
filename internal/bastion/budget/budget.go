// Package budget holds the download budgets of a Bastion's place (R-BAS-096, the owner's Q-LIM-305): the eleven figures of a
// rule of usage-and-limits.md R-LIM-004, charged per subject on everything the Bastion serves, hits included, in the mode of the
// Bastion (R-LIM-044, Q-LIM-309: Audit counts and refuses nothing of the budgets). The flood protection per address, the
// anonymous ceiling, the slots and the throughput are not this package's: they hold in both modes.
//
// The subject is the main installation's word for a signed-in caller (Q-BAS-328: the key and the rule of the check answer and
// the marking) and the Bastion's own for an anonymous caller (its address, the address and kind rules of the place, Q-LIM-302).
// A [Charge] is one request's subject; the engine asks it at the steps of the request: [Charge.Request] after the credential is
// decided, [Charge.Upstream] before a request to the main installation, [Charge.Transfer] before a download starts, and it
// reports the bytes it writes ([Charge.Served]) and a refused request ([Charge.Refused]); [Charge.End] ends it.
//
// The counters are the Bastion's shared state (shared.State.Budget and the limiters, R-BAS-034): with Valkey the whole
// Bastion's, without it each replica's share (H-2286). An hour or a day is a sliding estimate over two fixed windows counted from
// the Unix epoch, as the throughput's hour is (R-BAS-097); the request rate is a token bucket of the limiters of R-ARCH-034.
package budget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/netip"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/bastion/shared"
	"github.com/guided-traffic/quader/internal/bastion/usage"
)

// The codes of the refusals of R-LIM-020 at a Bastion (R-BAS-096).
const (
	CodeRateLimited              = iface.CodeRateLimited
	CodeDownloadLimitReached     = "download_limit_reached"
	CodeUpstreamLimitReached     = "upstream_limit_reached"
	CodeTooManyParallelTransfers = "too_many_parallel_transfers"
	CodeCallerBlocked            = "caller_blocked"
)

// The figures of a rule, as iface.FigureNames names them.
const (
	FigureRequestsPerSecond       = "requestsPerSecond"
	FigureRequestsPerHour         = "requestsPerHour"
	FigureCachedBytesPerHour      = "cachedBytesPerHour"
	FigureUncachedBytesPerHour    = "uncachedBytesPerHour"
	FigureUpstreamRequestsPerHour = "upstreamRequestsPerHour"
	FigureParallelTransfers       = "parallelTransfers"
	FigureBytesPerDay             = "bytesPerDay"
	FigureRefusedPerHour          = "refusedPerHour"
)

// The rules of the waits and the pace (Q-BAS-329, Q-BAS-330).
const (
	// LeaseWait is how long a transfer waits for a lease of its subject's parallel transfers before it is refused.
	LeaseWait = 10 * time.Second
	// leasePoll is how often a waiting transfer asks again.
	leasePoll = 100 * time.Millisecond
	// FloorBytesPerSecond is the pace below which a transfer is never slowed: 64 KiB a second.
	FloorBytesPerSecond = 64 << 10
	// Slice is the window of the pace: a tenth of a second.
	Slice = 100 * time.Millisecond
	// BlockWindow is the window of a block (R-LIM-025 at a Bastion, Q-BAS-472): a block holds for the rest of the quarter hour
	// it began in and the whole next one, 15 to 30 minutes.
	BlockWindow = 15 * time.Minute
	// flushBytes is how many bytes a transfer counts before it adds them to the shared counters: the counters are asked at most
	// once a mebibyte, and once at the end.
	flushBytes = 1 << 20
	// limitedWindow is how long a refused subject counts as limited in the replica's status.
	limitedWindow = 5 * time.Minute
	// maxLimited bounds the subjects the status remembers.
	maxLimited = 100_000
	// maxBlocks bounds the blocks of anonymous addresses the status lists.
	maxBlocks = iface.MaxStatusBlocks
)

const (
	hour = time.Hour
	day  = 24 * time.Hour
)

// Options configure a [Budgets].
type Options struct {
	// Limiter holds the subjects' request rates (shared.State.Limiter); Counters their windows, leases, paces and blocks
	// (shared.State.Budget).
	Limiter  shared.Limiter
	Counters shared.Bytes
	// Usage receives what every request costs, for the reporting batch (R-LIM-045); nil keeps nothing.
	Usage   *usage.Buffer
	Metrics *metrics.Metrics
	Log     *slog.Logger
	// MaxInFlight is the replica's request slots (server.max_in_flight): the waiting leases hold at most a quarter of them
	// (Q-BAS-329).
	MaxInFlight int
	// Now and Sleep are the clock; nil means time.Now and a timer. Tests inject both.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

// Budgets are a replica's budgets of its Bastion's place. They are safe for concurrent use.
type Budgets struct {
	o       Options
	waiting atomic.Int64

	mu      sync.Mutex
	limited map[string]time.Time
	blocks  map[string]iface.StatusBlock
}

// New returns the budgets.
func New(o Options) *Budgets {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Sleep == nil {
		o.Sleep = sleep
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return &Budgets{o: o, limited: map[string]time.Time{}, blocks: map[string]iface.StatusBlock{}}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Refusal is a budget's refusal (R-LIM-020): the code, the figure, its limit and window, and the wait.
type Refusal struct {
	Code       string
	Figure     string
	Limit      int64
	Window     time.Duration
	RetryAfter time.Duration
}

// Subject is who a request is charged to at the Bastion's place.
type Subject struct {
	// Key is the subject key, Credential the presented token's ID, Rule the rule of the place.
	Key, Credential, Rule string
	// Address is the anonymous caller's address prefix, empty for a signed-in caller.
	Address string
}

// SubjectOf resolves the subject of a request (R-BAS-096, Q-BAS-328, Q-LIM-302): a signed-in caller by the key and rule the main
// installation named, a signed-in caller it named none for by the kind rule of users under its principal (Q-BAS-470), an
// anonymous caller by its address prefix and the address or kind rule of the place. addrKey is the guard's key of the address
// (an IPv4 address, or the /64 of an IPv6 one).
func SubjectOf(place settings.Place, sub iface.Subject, anonymous bool, addr netip.Addr, addrKey string) Subject {
	if anonymous || sub.Kind != iface.SubjectToken {
		return Subject{Key: "anonymous:" + addrKey, Rule: place.Anonymous(addr), Address: addrKey}
	}
	if l := sub.Limits; l != nil && l.Valid() {
		if _, ok := place.Rules[l.Rule]; ok {
			return Subject{Key: l.Key, Credential: sub.TokenID, Rule: l.Rule}
		}
	}
	who := sub.PrincipalID
	if who == "" {
		who = sub.TokenID
	}
	return Subject{Key: "user:" + who, Credential: sub.TokenID, Rule: place.Kinds.User}
}

// Charge is one request's subject at the place, with the configuration the request read. It is not safe for concurrent use
// except [Charge.Served] and [Charge.Pace], which the writer of the request calls.
type Charge struct {
	b       *Budgets
	s       Subject
	repo    string
	fig     iface.Figures
	enforce bool
	hash    string

	mu       sync.Mutex
	cached   int64 // bytes served from the cache, not yet added to the counters
	uncached int64
	lease    *lease
	pace     *pacer
	refused  bool
}

// Charge starts the charge of a request of the subject on repo (the qualified repository name, empty where the request names
// none) with the configuration a.
func (b *Budgets) Charge(a *settings.Applied, s Subject, repo string) *Charge {
	sum := sha256.Sum256([]byte(s.Key))
	return &Charge{
		b: b, s: s, repo: repo, fig: a.Place.Rules[s.Rule], enforce: a.Enforce, hash: hex.EncodeToString(sum[:16]),
	}
}

// Subject returns the subject charged.
func (c *Charge) Subject() Subject { return c.s }

// Enforce reports whether the charge refuses (Enforce) or only counts (Audit).
func (c *Charge) Enforce() bool { return c.enforce }

func (c *Charge) usage(d usage.Delta) {
	if c.b.o.Usage == nil {
		return
	}
	d.At, d.Subject, d.Credential, d.Repository, d.Rule = c.b.o.Now(), c.s.Key, c.s.Credential, c.repo, c.s.Rule
	c.b.o.Usage.Add(d)
}

// decide turns a figure that is met into a refusal in Enforce and into a would-limit in Audit: the event is counted either way, a
// refusal only in Enforce (R-LIM-044).
func (c *Charge) decide(r *Refusal) *Refusal {
	if r == nil {
		return nil
	}
	mode := metrics.BudgetWouldLimit
	if c.enforce {
		mode = metrics.BudgetLimited
	}
	if c.b.o.Metrics != nil {
		c.b.o.Metrics.BudgetEvents.WithLabelValues(r.Figure, mode).Inc()
	}
	if !c.enforce {
		c.usage(usage.Delta{WouldLimit: r.Figure})
		return nil
	}
	c.usage(usage.Delta{Limited: r.Figure})
	c.b.markLimited(c.s.Key)
	return r
}

func (b *Budgets) markLimited(key string) {
	now := b.o.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.limited) >= maxLimited {
		for k, t := range b.limited {
			if now.Sub(t) > limitedWindow {
				delete(b.limited, k)
			}
		}
		if len(b.limited) >= maxLimited {
			return
		}
	}
	b.limited[key] = now
}

// Limited is the number of subjects a budget refused on this replica within the last five minutes (R-LIM-045's status).
func (b *Budgets) Limited() int64 {
	now := b.o.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	var n int64
	for k, t := range b.limited {
		if now.Sub(t) > limitedWindow {
			delete(b.limited, k)
			continue
		}
		n++
	}
	return n
}

// Blocks are the blocks of anonymous addresses this replica set that have not ended, the newest first, at most
// iface.MaxStatusBlocks.
func (b *Budgets) Blocks() []iface.StatusBlock {
	now := b.o.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]iface.StatusBlock, 0, len(b.blocks))
	for k, bl := range b.blocks {
		if !bl.Until.After(now) {
			delete(b.blocks, k)
			continue
		}
		out = append(out, bl)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Until.After(out[j].Until) })
	return out[:min(len(out), maxBlocks)]
}

// name is a counter's name: what it counts and the subject's hash, the characters of a key part of Valkey.
func (c *Charge) name(what string) string { return what + "-" + c.hash }

// estimate is the sliding estimate of a window over its two counters, prev x (1 - elapsed) + cur, after adding n (0 reads).
func (c *Charge) estimate(ctx context.Context, what string, w time.Duration, n int64) (est float64, cur, prev int64, elapsed float64) {
	now := c.b.o.Now()
	idx := now.UnixNano() / int64(w)
	cur, prev = c.b.o.Counters.Add(ctx, c.name(what), idx, 2*w+time.Hour, n)
	elapsed = float64(now.UnixNano()%int64(w)) / float64(w)
	return float64(prev)*(1-elapsed) + float64(cur), cur, prev, elapsed
}

// windowWait is the time until the sliding estimate prev x (1 - elapsed) + cur falls below limit while nothing is added: within
// the current window as the previous one's weight falls, else in the next one, whose previous counter is cur.
func windowWait(cur, prev, elapsed, limit float64, w time.Duration) time.Duration {
	d := float64(w)
	if prev > 0 && cur < limit {
		if x := 1 - (limit-cur)/prev; x < 1 {
			return time.Duration((max(x, elapsed) - elapsed) * d)
		}
	}
	y := 0.0
	if cur > 0 {
		y = max(0, 1-limit/cur)
	}
	return min(time.Duration(((1-elapsed)+y)*d), w)
}

func value(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// blockIndex is the quarter hour of t.
func blockIndex(t time.Time) int64 { return t.UnixNano() / int64(BlockWindow) }

// blocked reports the subject's block and its remaining time (R-LIM-025): a block set in a quarter hour holds through the next.
func (c *Charge) blocked(ctx context.Context) (bool, time.Duration) {
	now := c.b.o.Now()
	idx := blockIndex(now)
	cur, prev := c.b.o.Counters.Add(ctx, c.name("blk"), idx, 2*BlockWindow+time.Minute, 0)
	switch {
	case cur > 0:
		return true, time.Unix(0, (idx+2)*int64(BlockWindow)).Sub(now)
	case prev > 0:
		return true, time.Unix(0, (idx+1)*int64(BlockWindow)).Sub(now)
	}
	return false, 0
}

// rateLimiter is the limiter of a rule's request rate: one per rule, since a limiter's figures are its name's (Q-BAS-449).
func rateLimiter(rule string) string {
	sum := sha256.Sum256([]byte(rule))
	return "bastion.budget_rate_" + hex.EncodeToString(sum[:6])
}

// Request charges one request (R-BAS-096: every request of the subject, after the credential is decided): a block, the request
// rate with its burst, the requests of the hour. A refused request counts in no request window (R-LIM-008, Q-LIM-58). It returns
// the refusal in Enforce, nil when the request is admitted (and in Audit always).
func (c *Charge) Request(ctx context.Context) *Refusal {
	c.usage(usage.Delta{Requests: 1})
	if value(c.fig.RefusedPerHour) > 0 {
		if in, left := c.blocked(ctx); in {
			if r := c.decide(&Refusal{Code: CodeCallerBlocked, Figure: FigureRefusedPerHour, Limit: value(c.fig.RefusedPerHour), Window: hour, RetryAfter: left}); r != nil {
				return r
			}
		}
	}
	if rps := value(c.fig.RequestsPerSecond); rps > 0 {
		burst := max(value(c.fig.RequestsBurst), rps)
		ok, wait, err := c.b.o.Limiter.Allow(ctx, rateLimiter(c.s.Rule), c.s.Key, shared.Rate{PerSecond: float64(rps), Burst: int(burst)})
		if err == nil && !ok {
			if r := c.decide(&Refusal{Code: CodeRateLimited, Figure: FigureRequestsPerSecond, Limit: burst, Window: time.Duration(max(1, (burst+rps-1)/rps)) * time.Second, RetryAfter: wait}); r != nil {
				return r
			}
		}
	}
	if limit := value(c.fig.RequestsPerHour); limit > 0 {
		est, cur, prev, el := c.estimate(ctx, "req", hour, 1)
		if est > float64(limit) {
			if c.enforce {
				c.undo(ctx, "req", hour)
			}
			if r := c.decide(&Refusal{Code: CodeRateLimited, Figure: FigureRequestsPerHour, Limit: limit, Window: hour, RetryAfter: windowWait(float64(cur-1), float64(prev), el, float64(limit), hour)}); r != nil {
				return r
			}
		}
	}
	return nil
}

// Count counts a request and the upstream requests it sent before its subject was known (Q-BAS-471): in the windows and the usage,
// deciding nothing.
func (c *Charge) Count(ctx context.Context, upstream int) {
	c.usage(usage.Delta{Requests: 1, Upstream: int64(upstream)})
	if rps := value(c.fig.RequestsPerSecond); rps > 0 {
		_, _, _ = c.b.o.Limiter.Allow(ctx, rateLimiter(c.s.Rule), c.s.Key, shared.Rate{PerSecond: float64(rps), Burst: int(max(value(c.fig.RequestsBurst), rps))})
	}
	if value(c.fig.RequestsPerHour) > 0 {
		c.estimate(ctx, "req", hour, 1)
	}
	if upstream > 0 && value(c.fig.UpstreamRequestsPerHour) > 0 {
		c.estimate(ctx, "up", hour, int64(upstream))
	}
}

// undo takes back the one the window's counter was raised by for a request it then refused.
func (c *Charge) undo(ctx context.Context, what string, w time.Duration) {
	idx := c.b.o.Now().UnixNano() / int64(w)
	c.b.o.Counters.Release(context.WithoutCancel(ctx), c.name(what), idx, 2*w+time.Hour, 1)
}

// Upstream charges a request the Bastion sends to the main installation for the caller (upstreamRequestsPerHour).
func (c *Charge) Upstream(ctx context.Context) *Refusal {
	c.usage(usage.Delta{Upstream: 1})
	limit := value(c.fig.UpstreamRequestsPerHour)
	if limit <= 0 {
		return nil
	}
	est, cur, prev, el := c.estimate(ctx, "up", hour, 1)
	if est <= float64(limit) {
		return nil
	}
	if c.enforce {
		c.undo(ctx, "up", hour)
	}
	return c.decide(&Refusal{Code: CodeUpstreamLimitReached, Figure: FigureUpstreamRequestsPerHour, Limit: limit, Window: hour, RetryAfter: windowWait(float64(cur-1), float64(prev), el, float64(limit), hour)})
}

// Transfer is the check before a download starts (R-LIM-021 at a Bastion): the hour figure of its bytes (cached for a hit,
// uncached for a fill the caller leads), the day's bytes, then a lease of the subject's parallel transfers, which waits at most
// [LeaseWait] while the waiting leases of the replica hold at most a quarter of its request slots (Q-BAS-329). An admitted
// transfer is paced to the subject's bytes per second of its kind, never below [FloorBytesPerSecond] (Q-BAS-330). The lease is
// held until [Charge.End].
func (c *Charge) Transfer(ctx context.Context, cached bool) *Refusal {
	what, fig, code, limit, rate := "unc", FigureUncachedBytesPerHour, CodeUpstreamLimitReached, value(c.fig.UncachedBytesPerHour), value(c.fig.UncachedBytesPerSecond)
	if cached {
		what, fig, code, limit, rate = "cac", FigureCachedBytesPerHour, CodeDownloadLimitReached, value(c.fig.CachedBytesPerHour), value(c.fig.CachedBytesPerSecond)
	}
	if limit > 0 {
		if est, cur, prev, el := c.estimate(ctx, what, hour, 0); est >= float64(limit) {
			if r := c.decide(&Refusal{Code: code, Figure: fig, Limit: limit, Window: hour, RetryAfter: windowWait(float64(cur), float64(prev), el, float64(limit), hour)}); r != nil {
				return r
			}
		}
	}
	if limit := value(c.fig.BytesPerDay); limit > 0 {
		if est, cur, prev, el := c.estimate(ctx, "day", day, 0); est >= float64(limit) {
			if r := c.decide(&Refusal{Code: CodeDownloadLimitReached, Figure: FigureBytesPerDay, Limit: limit, Window: day, RetryAfter: windowWait(float64(cur), float64(prev), el, float64(limit), day)}); r != nil {
				return r
			}
		}
	}
	if r := c.takeLease(ctx); r != nil {
		return r
	}
	if c.enforce && rate > 0 {
		c.mu.Lock()
		c.pace = &pacer{c: c, rate: rate, kind: what}
		c.mu.Unlock()
	}
	return nil
}

// lease is a held transfer of the subject: the counter it was claimed in.
type lease struct {
	name string
	idx  int64
}

func leaseTTL() time.Duration { return 2*hour + time.Minute }

// takeLease claims one of the subject's parallel transfers, waiting for one at most LeaseWait.
func (c *Charge) takeLease(ctx context.Context) *Refusal {
	limit := value(c.fig.ParallelTransfers)
	if limit <= 0 || c.lease != nil {
		return nil
	}
	refusal := &Refusal{Code: CodeTooManyParallelTransfers, Figure: FigureParallelTransfers, Limit: limit, RetryAfter: time.Second}
	if c.claimLease(ctx, limit) {
		return nil
	}
	if !c.enforce {
		return c.decide(refusal)
	}
	if quarter := int64(c.b.o.MaxInFlight / 4); c.b.waiting.Add(1) > max(quarter, 1) {
		c.b.waiting.Add(-1)
		return c.decide(refusal)
	}
	defer c.b.waiting.Add(-1)
	deadline := c.b.o.Now().Add(LeaseWait)
	for c.b.o.Now().Before(deadline) {
		if err := c.b.o.Sleep(ctx, leasePoll); err != nil {
			return c.decide(refusal)
		}
		if c.claimLease(ctx, limit) {
			return nil
		}
	}
	return c.decide(refusal)
}

// claimLease claims one transfer in the counter of the current hour: a lease outlives no more than two hours of counters, and a
// transfer that began in the previous hour is not counted in this one (H-2369).
func (c *Charge) claimLease(ctx context.Context, limit int64) bool {
	l := &lease{name: c.name("par"), idx: c.b.o.Now().UnixNano() / int64(hour)}
	if c.b.o.Counters.Claim(ctx, l.name, l.idx, leaseTTL(), 1, limit) == 0 {
		return false
	}
	c.lease = l
	return true
}

// Served counts n bytes the request wrote to the caller: from the cache, or as they came from the main installation or the
// object store (uncached). The counters are raised once a mebibyte and at the end.
func (c *Charge) Served(ctx context.Context, cached bool, n int64) {
	if n <= 0 {
		return
	}
	c.mu.Lock()
	if cached {
		c.cached += n
	} else {
		c.uncached += n
	}
	full := c.cached+c.uncached >= flushBytes
	c.mu.Unlock()
	if full {
		c.flush(ctx)
	}
}

// flush adds the counted bytes to the hour and day counters and the usage.
func (c *Charge) flush(ctx context.Context) {
	c.mu.Lock()
	cached, uncached := c.cached, c.uncached
	c.cached, c.uncached = 0, 0
	c.mu.Unlock()
	if cached+uncached == 0 {
		return
	}
	ctx = context.WithoutCancel(ctx)
	c.usage(usage.Delta{Cached: cached, Uncached: uncached})
	if cached > 0 && value(c.fig.CachedBytesPerHour) > 0 {
		c.estimate(ctx, "cac", hour, cached)
	}
	if uncached > 0 && value(c.fig.UncachedBytesPerHour) > 0 {
		c.estimate(ctx, "unc", hour, uncached)
	}
	if value(c.fig.BytesPerDay) > 0 {
		c.estimate(ctx, "day", day, cached+uncached)
	}
}

// Refused counts a refused request of the subject (R-LIM-008: a refusal of the decision point or a failed credential, never a
// refusal of a figure), and blocks the subject once its refused requests of the hour pass refusedPerHour (R-LIM-025).
func (c *Charge) Refused(ctx context.Context) {
	if c.refused {
		return
	}
	c.refused = true
	c.usage(usage.Delta{Refused: 1})
	limit := value(c.fig.RefusedPerHour)
	if limit <= 0 {
		return
	}
	est, _, _, _ := c.estimate(ctx, "ref", hour, 1)
	if est <= float64(limit) {
		return
	}
	if c.decide(&Refusal{Code: CodeCallerBlocked, Figure: FigureRefusedPerHour, Limit: limit, Window: hour}) == nil {
		return
	}
	now := c.b.o.Now()
	idx := blockIndex(now)
	if c.b.o.Counters.Claim(context.WithoutCancel(ctx), c.name("blk"), idx, 2*BlockWindow+time.Minute, 1, 1) == 0 {
		return // blocked already
	}
	if c.b.o.Metrics != nil {
		c.b.o.Metrics.Blocks.WithLabelValues(metrics.BlockRefusedRequests).Inc()
	}
	if c.s.Address != "" {
		c.b.mu.Lock()
		if len(c.b.blocks) < 10*maxBlocks {
			c.b.blocks[c.s.Address] = iface.StatusBlock{
				Address: canonical(c.s.Address), Until: time.Unix(0, (idx+2)*int64(BlockWindow)).UTC(), Reason: metrics.BlockRefusedRequests,
			}
		}
		c.b.mu.Unlock()
	}
	c.b.o.Log.Info("bastion caller blocked", "subject", c.s.Key, "refused", int64(est))
}

// canonical writes an address key as the canonical prefix the status carries: an IPv4 address as its /32.
func canonical(addrKey string) string {
	if p, err := netip.ParsePrefix(addrKey); err == nil {
		return p.Masked().String()
	}
	if a, err := netip.ParseAddr(addrKey); err == nil {
		return netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()).String()
	}
	return addrKey
}

// End ends the charge: the bytes not yet counted are counted and the lease is given back. It may be called again.
func (c *Charge) End(ctx context.Context) {
	c.flush(ctx)
	c.mu.Lock()
	l := c.lease
	c.lease = nil
	c.mu.Unlock()
	if l != nil {
		c.b.o.Counters.Release(context.WithoutCancel(ctx), l.name, l.idx, leaseTTL(), 1)
	}
}

// Pace waits until n bytes of the transfer may be written (Q-BAS-330) and returns how long it waited. It paces only an admitted
// transfer in Enforce whose figure is set; everything else is written at once.
func (c *Charge) Pace(ctx context.Context, n int) time.Duration {
	c.mu.Lock()
	p := c.pace
	c.mu.Unlock()
	if p == nil || n <= 0 {
		return 0
	}
	return p.take(ctx, int64(n))
}
