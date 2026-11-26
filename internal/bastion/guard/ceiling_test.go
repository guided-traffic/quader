package guard

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/bastion/shared"
)

// ceilingOf is a ceiling over a shared state of the given replicas and the clock, with the figures perSecond, burst and
// perHour (0 for none).
func ceilingOf(t *testing.T, clk *clock, replicas int, perSecond, burst, perHour int64) *Ceiling {
	t.Helper()
	c := settings.Builtin()
	ceil := iface.Ceiling{}
	if perSecond > 0 {
		ceil.RequestsPerSecond, ceil.Burst = &perSecond, &burst
	}
	if perHour > 0 {
		ceil.RequestsPerHour = &perHour
	}
	c.Limits.AnonymousCeiling = ceil
	a, err := settings.FromConfiguration(c, settings.SourceMain)
	if err != nil {
		t.Fatal(err)
	}
	return NewCeiling(shared.NewLocal(replicas, clk.now), func(context.Context) *settings.Applied { return a })
}

func newClock() *clock { return &clock{t: time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)} }

// TestR_BAS_094_TheCeilingHoldsAllAnonymousCallersTogether: the ceiling is one key for every anonymous caller of the Bastion
// (Q-BAS-326): at 10 a second with a burst of 20 the 21st request at once is refused with the wait of one token; a refused
// request takes nothing, so after 100 ms exactly one more passes; the hour refuses once it is full, and a request the second's
// bucket refused does not count toward it.
func TestR_BAS_094_TheCeilingHoldsAllAnonymousCallersTogether(t *testing.T) {
	clk := newClock()
	c := ceilingOf(t, clk, 1, 10, 20, 0)
	admitted := 0
	for range 100 {
		if ok, _ := c.Take(ctx); ok {
			admitted++
		}
	}
	if admitted != 20 {
		t.Fatalf("%d of 100 requests at once admitted, want the burst of 20", admitted)
	}
	if ok, wait := c.Take(ctx); ok || wait != time.Second {
		t.Fatalf("a request above the burst: admitted %v, Retry-After %s; want refused with 1 s", ok, wait)
	}
	clk.advance(100 * time.Millisecond)
	if ok, _ := c.Take(ctx); !ok {
		t.Fatal("the token of 100 ms was taken by the refused requests")
	}
	if ok, _ := c.Take(ctx); ok {
		t.Fatal("two requests passed on one token")
	}

	t.Run("the hour", func(t *testing.T) {
		clk := newClock()
		c := ceilingOf(t, clk, 1, 10, 20, 50)
		admitted := 0
		for range 600 { // a minute at 10 a second, each second's bucket refusing nothing after the burst
			if ok, _ := c.Take(ctx); ok {
				admitted++
			}
			clk.advance(100 * time.Millisecond)
		}
		if admitted != 50 {
			t.Fatalf("%d admitted within the hour, want 50", admitted)
		}
		if ok, wait := c.Take(ctx); ok || wait != ceilingHourRetry {
			t.Fatalf("the 51st of the hour: admitted %v, Retry-After %s", ok, wait)
		}
	})
	t.Run("a refusal of the second takes nothing from the hour", func(t *testing.T) {
		clk := newClock()
		c := ceilingOf(t, clk, 1, 1, 1, 3)
		for range 50 {
			c.Take(ctx) // one admitted, 49 refused by the second's bucket
		}
		for range 2 {
			clk.advance(time.Second)
			if ok, _ := c.Take(ctx); !ok {
				t.Fatal("the hour was taken by requests the second refused")
			}
		}
		clk.advance(time.Second)
		if ok, _ := c.Take(ctx); ok {
			t.Fatal("a 4th request passed an hour of 3")
		}
	})
	t.Run("without a figure nothing is refused", func(t *testing.T) {
		c := ceilingOf(t, newClock(), 1, 0, 0, 0)
		for range 10_000 {
			if ok, _ := c.Take(ctx); !ok {
				t.Fatal("an unlimited ceiling refused")
			}
		}
	})
}

// TestR_BAS_094_WithoutValkeyEachReplicaHoldsItsShareOfTheCeiling: without Valkey the ceiling is counted per replica with the
// figures divided by bastion.replicas (Q-BAS-34): two replicas that both take requests admit the burst together, one replica
// that takes every request admits its half (H-2356).
func TestR_BAS_094_WithoutValkeyEachReplicaHoldsItsShareOfTheCeiling(t *testing.T) {
	clk := newClock()
	a, b := ceilingOf(t, clk, 2, 10, 20, 0), ceilingOf(t, clk, 2, 10, 20, 0)
	n := 0
	for range 100 {
		for _, c := range []*Ceiling{a, b} {
			if ok, _ := c.Take(ctx); ok {
				n++
			}
		}
	}
	if n != 20 {
		t.Fatalf("two replicas admitted %d at once, want the burst of 20 between them", n)
	}
	one := ceilingOf(t, newClock(), 2, 10, 20, 0)
	n = 0
	for range 100 {
		if ok, _ := one.Take(ctx); ok {
			n++
		}
	}
	if n != 10 {
		t.Fatalf("one replica of two admitted %d at once, want its share of 10", n)
	}
}

// TestR_BAS_096_PagesOfTheWebInterfaceAreAClassOfTheirOwn: the class gui of Q-LIM-307 has its own bucket per address, so a
// reader of pages and a package client behind one address do not share a budget; its refusals count toward the block.
func TestR_BAS_096_PagesOfTheWebInterfaceAreAClassOfTheirOwn(t *testing.T) {
	r := newRig(t, 1, nil) // gui 5 a second with a burst of 20, anonymous 10 with 40
	const addr = "203.0.113.7"
	pages := 0
	for range 100 {
		if r.g.Page(ctx, addr).Allowed {
			pages++
		}
	}
	if pages != 20 {
		t.Fatalf("%d pages at once, want the gui burst of 20", pages)
	}
	if v := r.g.Page(ctx, addr); v.Allowed || v.Code != CodeRateLimited || v.RetryAfter != time.Second {
		t.Fatalf("a page above the burst: %+v", v)
	}
	if !r.g.Request(ctx, addr, false).Allowed {
		t.Fatal("the pages took from the anonymous class")
	}
	r2 := newRig(t, 1, func(o *rigOptions) { o.Flood.ExemptNetworks = []string{"203.0.113.0/24"} })
	for range 1000 {
		if !r2.g.Page(ctx, addr).Allowed {
			t.Fatal("a page of an exempt address was refused")
		}
	}
}

// TestR_BAS_093_TheGuardDecidesWithTheFiguresOfTheConfiguration: the guard reads its figures through the configuration a
// request carries, so a new configuration changes them without a restart, and a request keeps the figures it started with.
func TestR_BAS_093_TheGuardDecidesWithTheFiguresOfTheConfiguration(t *testing.T) {
	clk := newClock()
	h := settings.New(settings.Options{})
	g, err := New(Options{Figures: h.For, State: shared.NewLocal(1, clk.now), Metrics: newRig(t, 1, nil).m, Now: clk.now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	strict := settings.Builtin()
	strict.Revision = 7
	strict.Limits.Flood.Anonymous = iface.FloodClass{RequestsPerSecond: 1, Burst: 2, MissesPerMinute: 1}
	before := settings.With(ctx, h.Current())
	if _, err := h.Apply(ctx, strict, nil); err != nil {
		t.Fatal(err)
	}
	const addr = "198.51.100.4"
	if !g.Miss(ctx, addr, false).Allowed || g.Miss(ctx, addr, false).Allowed {
		t.Fatal("the new figure of one miss a minute does not hold")
	}
	if !g.Miss(before, addr, false).Allowed {
		t.Fatal("a request that read the built-in figures was decided with the new ones")
	}
}
