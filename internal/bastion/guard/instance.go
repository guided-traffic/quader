package guard

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/bastion/shared"
)

// The names of the limiters (R-BAS-034: bastion.<name>). With Valkey they are the limiters of R-ARCH-034 and
// the labels of quader_ratelimit_fallback.
const (
	limiterRequestsAuthenticated = "bastion.requests_authenticated"
	limiterRequestsAnonymous     = "bastion.requests_anonymous"
	limiterRequestsGUI           = "bastion.requests_gui"
	limiterMissesAuthenticated   = "bastion.misses_authenticated"
	limiterMissesAnonymous       = "bastion.misses_anonymous"
	limiterFailedAuth            = "bastion.failed_auth"
	limiterRefusals              = "bastion.refusals"
)

// missWindow is the period of the miss limits: they are per minute (R-BAS-035).
const missWindow = time.Minute

// missRetryAfter is the Retry-After of a refused miss: a quarter of the window lets the estimate of the
// admitted misses fall by a quarter, which a client that waits sees. A refused miss takes nothing from the
// budget (Q-BAS-127), so a retry that comes too early costs the client nothing but the refusal.
const missRetryAfter = missWindow / 4

// gaugeInterval is how often the number of blocked addresses is read again, so that a block that ends
// leaves the gauge while no request arrives.
const gaugeInterval = 5 * time.Second

// Options configure [New].
type Options struct {
	// Figures returns the configuration a decision is made with (R-BAS-093): the one the request read at its start, else the
	// one the replica applies now (settings.Holder.For). nil decides with the built-in configuration (Q-BAS-327).
	Figures func(ctx context.Context) *settings.Applied
	// State is the shared state: local, or in Valkey (internal/bastion/shared).
	State   *shared.State
	Metrics *metrics.Metrics
	Log     *slog.Logger
	// Now is the clock; nil means time.Now. Tests inject one.
	Now func() time.Time
}

// Instance is the guard of a replica. It implements [Guard]; Close stops the loop that keeps
// quader_bastion_blocked_addresses current.
type Instance struct {
	o   Options
	log *slog.Logger
	now func() time.Time

	// blocking serializes the decision to block an address within the process, per address, so that the
	// refusals of a burst that cross the threshold together set one block and not several levels at once, and
	// the round trips of one address's decision hold no other address back (Q-BAS-128).
	blocking keyedLock

	// set are the blocks and timeouts this replica set, for its status (R-LIM-045): by reason and address.
	setMu sync.Mutex
	set   map[string]iface.StatusBlock

	stop chan struct{}
	done chan struct{}
	once sync.Once
}

var _ Guard = (*Instance)(nil)

// New builds the guard.
func New(o Options) (*Instance, error) {
	if o.State == nil || o.Metrics == nil {
		return nil, errors.New("guard: State and Metrics are required")
	}
	if o.Figures == nil {
		builtin, err := settings.FromConfiguration(settings.Builtin(), settings.SourceBuiltin)
		if err != nil {
			return nil, err
		}
		o.Figures = func(context.Context) *settings.Applied { return builtin }
	}
	g := &Instance{o: o, log: o.Log, now: o.Now, set: map[string]iface.StatusBlock{}, stop: make(chan struct{}), done: make(chan struct{})}
	if g.log == nil {
		g.log = slog.New(slog.DiscardHandler)
	}
	if g.now == nil {
		g.now = time.Now
	}
	go g.loop()
	return g, nil
}

// flood returns the flood figures of a decision and whether the address key is exempt from them.
func (g *Instance) flood(ctx context.Context, addr string) (settings.Flood, bool) {
	a := g.o.Figures(ctx)
	return a.Flood, a.Exempt(addr)
}

// Close stops the background loop. It may be called again.
func (g *Instance) Close() error {
	g.once.Do(func() { close(g.stop) })
	<-g.done
	return nil
}

func (g *Instance) loop() {
	defer close(g.done)
	t := time.NewTicker(gaugeInterval)
	defer t.Stop()
	for {
		select {
		case <-g.stop:
			return
		case <-t.C:
			g.refreshGauge(context.Background())
		}
	}
}

func (g *Instance) refreshGauge(ctx context.Context) {
	if n, err := g.o.State.Blocks.Size(ctx); err == nil {
		g.o.Metrics.Blocked.Set(float64(n))
	}
}

// retryAfter rounds a wait up to whole seconds, at least one (Retry-After is whole seconds).
func retryAfter(d time.Duration) time.Duration {
	return max((d+time.Second-1)/time.Second, 1) * time.Second
}

func refuse(code, reason string, after time.Duration) Verdict {
	return Verdict{Status: http.StatusTooManyRequests, Code: code, RetryAfter: retryAfter(after), Reason: reason}
}

// Blocked implements [Guard]. A failure of the shared state admits: the guard never refuses on its own fault.
func (g *Instance) Blocked(ctx context.Context, addr string) Verdict {
	if _, exempt := g.flood(ctx, addr); exempt {
		return Allow()
	}
	b, ok, err := g.o.State.Blocks.Blocked(ctx, addr)
	if err != nil || !ok {
		return Allow()
	}
	v := refuse(CodeAddressBlocked, metrics.RefusedBlocked, b.Until.Sub(g.now()))
	v.Quiet = true
	return v
}

// Request implements [Guard].
func (g *Instance) Request(ctx context.Context, addr string, c Credentialed) Verdict {
	f, exempt := g.flood(ctx, addr)
	if exempt {
		return Allow()
	}
	name, rate := limiterRequestsAnonymous, f.Anonymous.Rate
	if c {
		name, rate = limiterRequestsAuthenticated, f.Authenticated.Rate
	}
	return g.request(ctx, f, addr, name, rate)
}

// Page implements [Guard]: the class gui of the web interface's pages (Q-LIM-307).
func (g *Instance) Page(ctx context.Context, addr string) Verdict {
	f, exempt := g.flood(ctx, addr)
	if exempt {
		return Allow()
	}
	return g.request(ctx, f, addr, limiterRequestsGUI, f.GUI)
}

// request takes a token of the address's bucket of a class; a refusal counts toward the block threshold.
func (g *Instance) request(ctx context.Context, f settings.Flood, addr, name string, r settings.Rate) Verdict {
	ok, wait, err := g.o.State.Limiter.Allow(ctx, name, addr, rateOf(r))
	if err != nil || ok {
		return Allow()
	}
	g.refused(ctx, f, addr, metrics.RefusedRateLimited)
	return refuse(CodeRateLimited, metrics.RefusedRateLimited, wait)
}

func rateOf(r settings.Rate) shared.Rate {
	return shared.Rate{PerSecond: float64(r.RequestsPerSecond), Burst: r.Burst}
}

// Miss implements [Guard]. The limit is a count per minute: the admitted events of the address inside the
// sliding minute, this one included, may not exceed it. A refused miss is not counted (Q-BAS-127).
func (g *Instance) Miss(ctx context.Context, addr string, c Credentialed) Verdict {
	f, exempt := g.flood(ctx, addr)
	if exempt {
		return Allow()
	}
	name, limit := limiterMissesAnonymous, f.Anonymous.MissesPerMinute
	if c {
		name, limit = limiterMissesAuthenticated, f.Authenticated.MissesPerMinute
	}
	ok, _, err := g.o.State.Limiter.AddIfWithin(ctx, name, addr, missWindow, limit)
	if err != nil || ok {
		return Allow()
	}
	g.refused(ctx, f, addr, metrics.RefusedMissLimited)
	return refuse(CodeRateLimited, metrics.RefusedMissLimited, missRetryAfter)
}

// AuthTimeout implements [Guard].
func (g *Instance) AuthTimeout(ctx context.Context, addr string) Verdict {
	f, exempt := g.flood(ctx, addr)
	if exempt {
		return Allow()
	}
	b, ok, err := g.o.State.Timeouts.Blocked(ctx, addr)
	if err != nil || !ok {
		return Allow()
	}
	g.refused(ctx, f, addr, metrics.RefusedAuthTimeout)
	return refuse(CodeAuthTimeout, metrics.RefusedAuthTimeout, b.Until.Sub(g.now()))
}

// FailedAuth implements [Guard]. The failure that brings the count of the window to max_failures starts the
// timeout, so the next credential without a usable answer is the first one refused: max_failures failures are
// answered by the main installation, the following one is not (R-BAS-036).
func (g *Instance) FailedAuth(ctx context.Context, addr string) {
	f, exempt := g.flood(ctx, addr)
	if exempt {
		return
	}
	fa := f.FailedAuth
	n, err := g.o.State.Limiter.Add(ctx, limiterFailedAuth, addr, fa.Window)
	if err != nil || n < fa.MaxFailures {
		return
	}
	if _, in, _ := g.o.State.Timeouts.Blocked(ctx, addr); in {
		return
	}
	_ = g.o.State.Timeouts.Set(ctx, addr, fa.Timeout, 1)
	g.o.Metrics.Blocks.WithLabelValues(metrics.BlockFailedAuth).Inc()
	g.remember(addr, metrics.BlockFailedAuth, fa.Timeout)
	g.log.Info("bastion failed-authentication timeout", "address", addr, "failures", n, "duration", fa.Timeout.String())
}

// refused counts a refusal of R-BAS-035 or R-BAS-036 toward the block threshold and blocks the address that
// collects more than flood.blocking.threshold of them within its window (R-BAS-037). The block lasts its duration,
// doubled for each earlier block within 24 h up to its longest.
func (g *Instance) refused(ctx context.Context, f settings.Flood, addr, reason string) {
	b := f.Blocking
	n, err := g.o.State.Limiter.Add(ctx, limiterRefusals, addr, b.Window)
	if err != nil || n <= b.Threshold {
		return
	}
	unlock := g.blocking.lock(addr)
	defer unlock()
	if _, already, _ := g.o.State.Blocks.Blocked(ctx, addr); already {
		return
	}
	prev, _ := g.o.State.Blocks.Level(ctx, addr)
	level := prev + 1
	d := blockDuration(b.Duration, b.MaxDuration, level)
	_ = g.o.State.Blocks.Set(ctx, addr, d, level)
	g.o.Metrics.Blocks.WithLabelValues(metrics.BlockRefusals).Inc()
	g.remember(addr, metrics.BlockRefusals, d)
	g.refreshGauge(ctx)
	g.log.Info("bastion address blocked", "address", addr, "reason", reason, "duration", d.String(), "count", level, "refusals", n)
}

// maxRemembered bounds the blocks a replica remembers for its status; a block past it is still set, only not listed.
const maxRemembered = 10 * iface.MaxStatusBlocks

// remember keeps a block this replica set for its status, with the reason of the closed list of R-BAS-096.
func (g *Instance) remember(addr, reason string, d time.Duration) {
	b := iface.StatusBlock{Address: statusAddress(addr), Until: g.now().Add(d).UTC(), Reason: reason}
	g.setMu.Lock()
	defer g.setMu.Unlock()
	if len(g.set) >= maxRemembered {
		g.pruneLocked()
		if len(g.set) >= maxRemembered {
			return
		}
	}
	g.set[reason+"|"+addr] = b
}

func (g *Instance) pruneLocked() {
	now := g.now()
	for k, b := range g.set {
		if !b.Until.After(now) {
			delete(g.set, k)
		}
	}
}

// Blocks are the blocks and failed-authentication timeouts this replica set that have not ended (R-LIM-045's status): the
// address as a canonical prefix, the end, and the reason refusals or failed_auth.
func (g *Instance) Blocks() []iface.StatusBlock {
	g.setMu.Lock()
	defer g.setMu.Unlock()
	g.pruneLocked()
	out := make([]iface.StatusBlock, 0, len(g.set))
	for _, b := range g.set {
		out = append(out, b)
	}
	return out
}

// statusAddress writes a guard's address key as a canonical prefix: an IPv4 address as its /32, the /64 of an IPv6 one as it is.
func statusAddress(key string) string {
	if p, err := netip.ParsePrefix(key); err == nil {
		return p.Masked().String()
	}
	if a, err := netip.ParseAddr(key); err == nil {
		a = a.Unmap()
		return netip.PrefixFrom(a, a.BitLen()).String()
	}
	return key
}

// blockDuration is the duration doubled level-1 times, held to max.
func blockDuration(base, ceiling time.Duration, level int) time.Duration {
	d := base
	for i := 1; i < level; i++ {
		if d >= ceiling {
			break
		}
		d *= 2
	}
	return min(d, ceiling)
}
