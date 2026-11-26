package budget

import (
	"context"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/bastion/shared"
	"github.com/guided-traffic/quader/internal/bastion/usage"
)

// The budgets of a Bastion's place (R-BAS-096, R-LIM-044, Q-BAS-329, Q-BAS-330) on the local shared state with a clock the test
// drives; no data beyond byte counts.

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }

func (c *clock) sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
	return nil
}

func n(v int64) *int64 { return &v }

type fixture struct {
	b   *Budgets
	use *usage.Buffer
	clk *clock
	a   *settings.Applied
}

// newFixture builds the budgets with the figures f for every rule of the built-in place, in Enforce or Audit.
func newFixture(t *testing.T, f iface.Figures, enforce bool) *fixture {
	t.Helper()
	c := settings.Builtin()
	for id := range c.Limits.Place.Rules {
		c.Limits.Place.Rules[id] = f
	}
	if !enforce {
		c.Limits.Mode = iface.ModeAudit
	}
	a, err := settings.FromConfiguration(c, settings.SourceMain)
	if err != nil {
		t.Fatal(err)
	}
	clk := &clock{t: time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)}
	st := shared.NewLocal(1, clk.now)
	m, err := metrics.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	use := usage.New(0)
	b := New(Options{Limiter: st.Limiter, Counters: st.Budget, Usage: use, Metrics: m, MaxInFlight: 1024, Now: clk.now, Sleep: clk.sleep})
	return &fixture{b: b, use: use, clk: clk, a: a}
}

func (f *fixture) anonymous(addr string) *Charge {
	a := netip.MustParseAddr(addr)
	return f.b.Charge(f.a, SubjectOf(f.a.Place, iface.Subject{}, true, a, guard.AddressKey(a)), "web/npm-all")
}

// TestR_BAS_096_TheBudgetsFiguresAreTheInterfaces: the label values of the budgets' metric are the eleven figures of the wire.
func TestR_BAS_096_TheBudgetsFiguresAreTheInterfaces(t *testing.T) {
	if !slices.Equal(metrics.FigureNames(), iface.FigureNames()) {
		t.Fatalf("metrics.FigureNames %v, iface.FigureNames %v", metrics.FigureNames(), iface.FigureNames())
	}
}

// TestR_BAS_096_EachBudgetRefusesInEnforce: each figure refuses with its code once it is met, and a refused request counts in no
// request window.
func TestR_BAS_096_EachBudgetRefusesInEnforce(t *testing.T) {
	ctx := context.Background()
	t.Run("requests a second", func(t *testing.T) {
		f := newFixture(t, iface.Figures{RequestsPerSecond: n(1), RequestsBurst: n(2)}, true)
		var codes []string
		for range 3 {
			c := f.anonymous("203.0.113.7")
			if r := c.Request(ctx); r != nil {
				codes = append(codes, r.Code+"/"+r.Figure)
			} else {
				codes = append(codes, "ok")
			}
		}
		if want := []string{"ok", "ok", "rate_limited/requestsPerSecond"}; !slices.Equal(codes, want) {
			t.Fatalf("got %v, want %v", codes, want)
		}
		if r := f.anonymous("203.0.113.8").Request(ctx); r != nil {
			t.Fatalf("another address is another subject, refused %+v", r)
		}
	})
	t.Run("requests an hour", func(t *testing.T) {
		f := newFixture(t, iface.Figures{RequestsPerHour: n(2)}, true)
		for i := range 2 {
			if r := f.anonymous("203.0.113.7").Request(ctx); r != nil {
				t.Fatalf("request %d refused: %+v", i, r)
			}
		}
		r := f.anonymous("203.0.113.7").Request(ctx)
		if r == nil || r.Code != CodeRateLimited || r.Figure != FigureRequestsPerHour || r.RetryAfter <= 0 || r.RetryAfter > time.Hour {
			t.Fatalf("the third request: %+v", r)
		}
		// The refused request took nothing: half an hour on, the estimate leaves room for one.
		f.clk.sleep(ctx, 90*time.Minute)
		if r := f.anonymous("203.0.113.7").Request(ctx); r != nil {
			t.Fatalf("a request in the next hour: %+v", r)
		}
	})
	t.Run("upstream requests an hour", func(t *testing.T) {
		f := newFixture(t, iface.Figures{UpstreamRequestsPerHour: n(1)}, true)
		if r := f.anonymous("203.0.113.7").Upstream(ctx); r != nil {
			t.Fatal(r)
		}
		if r := f.anonymous("203.0.113.7").Upstream(ctx); r == nil || r.Code != CodeUpstreamLimitReached {
			t.Fatalf("the second upstream request: %+v", r)
		}
	})
	for _, tc := range []struct {
		name   string
		fig    iface.Figures
		cached bool
		code   string
		figure string
	}{
		{"cached bytes an hour", iface.Figures{CachedBytesPerHour: n(100)}, true, CodeDownloadLimitReached, FigureCachedBytesPerHour},
		{"uncached bytes an hour", iface.Figures{UncachedBytesPerHour: n(100)}, false, CodeUpstreamLimitReached, FigureUncachedBytesPerHour},
		{"bytes a day", iface.Figures{BytesPerDay: n(100)}, true, CodeDownloadLimitReached, FigureBytesPerDay},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.fig, true)
			c := f.anonymous("203.0.113.7")
			if r := c.Transfer(ctx, tc.cached); r != nil {
				t.Fatal(r)
			}
			c.Served(ctx, tc.cached, 100)
			c.End(ctx)
			r := f.anonymous("203.0.113.7").Transfer(ctx, tc.cached)
			if r == nil || r.Code != tc.code || r.Figure != tc.figure || r.Limit != 100 {
				t.Fatalf("the second transfer: %+v", r)
			}
			// The other kind of bytes is another figure: a hit after the fill's uncached bytes, a fill after the hits.
			if tc.figure != FigureBytesPerDay {
				if r := f.anonymous("203.0.113.7").Transfer(ctx, !tc.cached); r != nil {
					t.Fatalf("the other kind of transfer: %+v", r)
				}
			}
		})
	}
}

// TestR_BAS_096_TheSixtyFifthTransferWaitsAndIsRefusedAfterTenSeconds: at a figure of 64 parallel transfers the 65th waits for a
// lease and is refused after 10 s; once a transfer ends, the next takes its lease.
func TestR_BAS_096_TheSixtyFifthTransferWaitsAndIsRefusedAfterTenSeconds(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, iface.Figures{ParallelTransfers: n(64)}, true)
	var held []*Charge
	for i := range 64 {
		c := f.anonymous("203.0.113.7")
		if r := c.Transfer(ctx, true); r != nil {
			t.Fatalf("transfer %d: %+v", i, r)
		}
		held = append(held, c)
	}
	began := f.clk.now()
	r := f.anonymous("203.0.113.7").Transfer(ctx, true)
	if r == nil || r.Code != CodeTooManyParallelTransfers || r.Limit != 64 {
		t.Fatalf("the 65th transfer: %+v", r)
	}
	if waited := f.clk.now().Sub(began); waited < LeaseWait || waited > LeaseWait+leasePoll {
		t.Fatalf("the 65th transfer waited %s, want %s", waited, LeaseWait)
	}
	held[0].End(ctx)
	if r := f.anonymous("203.0.113.7").Transfer(ctx, true); r != nil {
		t.Fatalf("a transfer after one ended: %+v", r)
	}
}

// TestR_BAS_096_TheWaitingLeasesHoldAtMostAQuarterOfTheSlots: with every lease taken and a quarter of the replica's 8 slots
// waiting, a further transfer is refused at once.
func TestR_BAS_096_TheWaitingLeasesHoldAtMostAQuarterOfTheSlots(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, iface.Figures{ParallelTransfers: n(1)}, true)
	f.b.o.MaxInFlight = 8
	if r := f.anonymous("203.0.113.7").Transfer(ctx, true); r != nil {
		t.Fatal(r)
	}
	f.b.waiting.Add(2) // two transfers wait already: a quarter of 8
	began := f.clk.now()
	if r := f.anonymous("203.0.113.7").Transfer(ctx, true); r == nil || r.Code != CodeTooManyParallelTransfers {
		t.Fatalf("a third waiting transfer: %+v", r)
	}
	if f.clk.now() != began {
		t.Fatalf("the refusal waited %s, want none", f.clk.now().Sub(began))
	}
}

// TestR_BAS_096_AHitIsPacedToItsSubjectsFigureNeverBelowTheFloor: 3 MB at 10 MB/s take three slices of 1 MB, the first at once; 64 KiB at a figure of
// 1 byte a second takes a second, the floor's.
func TestR_BAS_096_AHitIsPacedToItsSubjectsFigureNeverBelowTheFloor(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		rate     int64
		bytes    int
		min, max time.Duration
	}{
		{"at the figure", 10_000_000, 3_000_000, 180 * time.Millisecond, 320 * time.Millisecond},
		{"at the floor", 1, 64 << 10, 900 * time.Millisecond, 1100 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, iface.Figures{CachedBytesPerSecond: n(tc.rate)}, true)
			c := f.anonymous("203.0.113.7")
			if r := c.Transfer(ctx, true); r != nil {
				t.Fatal(r)
			}
			began := f.clk.now()
			for left := tc.bytes; left > 0; left -= 32 << 10 {
				c.Pace(ctx, min(left, 32<<10))
			}
			if took := f.clk.now().Sub(began); took < tc.min || took > tc.max {
				t.Fatalf("%d bytes took %s, want %s to %s", tc.bytes, took, tc.min, tc.max)
			}
		})
	}
}

// TestR_LIM_044_AuditRefusesNothingOfTheBudgetsAndCountsWhatItWouldHave: in Audit every figure met is counted as would_limit in the
// usage, nothing is refused, nothing is paced, and no block is set.
func TestR_LIM_044_AuditRefusesNothingOfTheBudgetsAndCountsWhatItWouldHave(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, iface.Figures{RequestsPerHour: n(1), CachedBytesPerHour: n(10), CachedBytesPerSecond: n(1), RefusedPerHour: n(1)}, false)
	for range 3 {
		c := f.anonymous("203.0.113.7")
		if r := c.Request(ctx); r != nil {
			t.Fatalf("Audit refused a request: %+v", r)
		}
		if r := c.Transfer(ctx, true); r != nil {
			t.Fatalf("Audit refused a transfer: %+v", r)
		}
		if c.Pace(ctx, 1<<20) != 0 {
			t.Fatal("Audit paced a transfer")
		}
		c.Served(ctx, true, 100)
		c.Refused(ctx)
		c.End(ctx)
	}
	rep := f.use.Drain()
	if len(rep.Rows) != 1 {
		t.Fatalf("rows %+v", rep.Rows)
	}
	row := rep.Rows[0]
	if row.Requests != 3 || row.CachedBytes != 300 || row.Refused != 3 || len(row.Limited) != 0 ||
		row.WouldLimit[FigureRequestsPerHour] != 2 || row.WouldLimit[FigureCachedBytesPerHour] != 2 || row.WouldLimit[FigureRefusedPerHour] != 2 {
		t.Fatalf("the row %+v", row)
	}
	if in, _ := f.anonymous("203.0.113.7").blocked(ctx); in {
		t.Fatal("Audit set a block")
	}
}

// TestR_BAS_096_RefusedRequestsAboveTheirFigureBlockTheSubject: the refused request past refusedPerHour blocks the subject; its
// requests are refused caller_blocked for the rest of the quarter hour and the next, and the block is listed for the status.
func TestR_BAS_096_RefusedRequestsAboveTheirFigureBlockTheSubject(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, iface.Figures{RefusedPerHour: n(2)}, true)
	for range 3 {
		c := f.anonymous("203.0.113.7")
		c.Refused(ctx)
		c.End(ctx)
	}
	r := f.anonymous("203.0.113.7").Request(ctx)
	if r == nil || r.Code != CodeCallerBlocked || r.RetryAfter <= 15*time.Minute || r.RetryAfter > 30*time.Minute {
		t.Fatalf("a request of the blocked subject: %+v", r)
	}
	if bl := f.b.Blocks(); len(bl) != 1 || bl[0].Address != "203.0.113.7/32" || bl[0].Reason != metrics.BlockRefusedRequests {
		t.Fatalf("the status's blocks %+v", bl)
	}
	if f.b.Limited() != 1 {
		t.Fatalf("limited %d, want 1", f.b.Limited())
	}
	f.clk.sleep(ctx, 30*time.Minute)
	if r := f.anonymous("203.0.113.7").Request(ctx); r != nil {
		t.Fatalf("a request after the block: %+v", r)
	}
}

// TestR_BAS_096_TheSubjectsOfARequest: a technical token's two secrets share the key the main installation named; a caller it named
// none for is charged by the users' kind rule under its principal; an anonymous /64 is one subject, and an address rule wins over
// the kind rule for an address inside it.
func TestR_BAS_096_TheSubjectsOfARequest(t *testing.T) {
	c := settings.Builtin()
	c.Limits.Place.Rules["partners"] = iface.Figures{}
	c.Limits.Place.AddressRules = []iface.AddressRule{{Prefix: "198.51.100.0/24", Rule: "partners"}}
	a, err := settings.FromConfiguration(c, settings.SourceMain)
	if err != nil {
		t.Fatal(err)
	}
	lim := &iface.SubjectLimits{Key: "technical_token:0193-tt", Rule: settings.BuiltinRuleSignedIn}
	one := SubjectOf(a.Place, iface.Subject{Kind: iface.SubjectToken, TokenID: "secret-1", Limits: lim}, false, netip.Addr{}, "")
	two := SubjectOf(a.Place, iface.Subject{Kind: iface.SubjectToken, TokenID: "secret-2", Limits: lim}, false, netip.Addr{}, "")
	if one.Key != two.Key || one.Key != "technical_token:0193-tt" || one.Credential == two.Credential {
		t.Fatalf("two secrets of one token: %+v and %+v", one, two)
	}
	none := SubjectOf(a.Place, iface.Subject{Kind: iface.SubjectToken, TokenID: "t1", PrincipalID: "p1"}, false, netip.Addr{}, "")
	if none.Key != "user:p1" || none.Rule != a.Place.Kinds.User {
		t.Fatalf("a token without limits: %+v", none)
	}
	x, y := netip.MustParseAddr("2001:db8:1:2::1"), netip.MustParseAddr("2001:db8:1:2:ffff::9")
	sx := SubjectOf(a.Place, iface.Subject{}, true, x, guard.AddressKey(x))
	sy := SubjectOf(a.Place, iface.Subject{}, true, y, guard.AddressKey(y))
	if sx.Key != sy.Key || sx.Key != "anonymous:2001:db8:1:2::/64" || sx.Rule != settings.BuiltinRuleAnonymous {
		t.Fatalf("one /64: %+v and %+v", sx, sy)
	}
	p := netip.MustParseAddr("198.51.100.20")
	if s := SubjectOf(a.Place, iface.Subject{}, true, p, guard.AddressKey(p)); s.Rule != "partners" {
		t.Fatalf("an address inside an address rule: %+v", s)
	}
}
