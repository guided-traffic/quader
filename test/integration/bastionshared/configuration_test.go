//go:build integration

package bastionshared

import (
	"context"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/settings"
)

// TestR_BAS_093_ANewFloodFigureHoldsAcrossTheReplicasWithValkey: the replicas count an address in Valkey under the limiters'
// names, and a configuration that changes a figure while they run replaces the limiter under its name (Q-BAS-449), so the new
// figure holds for both replicas together at once and a changed figure is never an error that admits everything.
func TestR_BAS_093_ANewFloodFigureHoldsAcrossTheReplicasWithValkey(t *testing.T) {
	s := newSetup(t)
	a, b := s.pair(t)
	f := defaults()
	f.Anonymous = iface.FloodClass{RequestsPerSecond: 1, Burst: 6, MissesPerMinute: 30}
	f.Blocking.Threshold = 100000
	if _, err := s.figures.Apply(ctx, withFlood(2, f)); err != nil {
		t.Fatal(err)
	}
	addr := address()
	admitted := 0
	for i := range 40 {
		r := a
		if i%2 == 1 {
			r = b
		}
		if r.g.Request(ctx, addr, false).Allowed {
			admitted++
		}
	}
	if admitted != 6 {
		t.Fatalf("%d of 40 requests at once admitted over two replicas, want the new burst of 6", admitted)
	}
}

// TestR_BAS_094_WithValkeyTheReplicasShareOneCeiling: with Valkey the anonymous ceiling is one key of the Bastion's prefix,
// so two replicas admit its burst together, not each its own (R-BAS-094).
func TestR_BAS_094_WithValkeyTheReplicasShareOneCeiling(t *testing.T) {
	s := newSetup(t)
	a, b := s.pair(t)
	perSecond, burst := int64(1), int64(8)
	c := settings.Builtin()
	c.Revision = 2
	c.Limits.AnonymousCeiling = iface.Ceiling{RequestsPerSecond: &perSecond, Burst: &burst}
	if _, err := s.figures.Apply(ctx, c); err != nil {
		t.Fatal(err)
	}
	figures := func(context.Context) *settings.Applied { return s.figures.Current() }
	ca, cb := guard.NewCeiling(a.st, figures), guard.NewCeiling(b.st, figures)
	admitted := 0
	for range 20 {
		for _, c := range []*guard.Ceiling{ca, cb} {
			if ok, _ := c.Take(ctx); ok {
				admitted++
			}
		}
	}
	if admitted != 8 {
		t.Fatalf("two replicas admitted %d of 40 at once, want the shared burst of 8", admitted)
	}
}
