//go:build integration

package bastionshared

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/budget"
	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/settings"
)

// TestR_BAS_096_WithValkeyTheReplicasShareTheBudgets: with Valkey the budgets of a subject are counters of the Bastion's prefix,
// so two replicas admit together what the figures hold and no more: three requests of an hour, 100 cached bytes, one parallel
// transfer (R-BAS-096, H-2286). Counters only; the leases' wait runs on the test's clock.
func TestR_BAS_096_WithValkeyTheReplicasShareTheBudgets(t *testing.T) {
	s := newSetup(t)
	a, b := s.pair(t)
	one := func(v int64) *int64 { return &v }
	c := settings.Builtin()
	c.Revision = 2
	for id := range c.Limits.Place.Rules {
		c.Limits.Place.Rules[id] = iface.Figures{RequestsPerHour: one(3), CachedBytesPerHour: one(100), ParallelTransfers: one(1)}
	}
	applied, err := settings.FromConfiguration(c, settings.SourceMain)
	if err != nil {
		t.Fatal(err)
	}
	sleep := func(_ context.Context, d time.Duration) error { s.clk.advance(d); return nil }
	ba := budget.New(budget.Options{Limiter: a.st.Limiter, Counters: a.st.Budget, Metrics: a.m, MaxInFlight: 1024, Now: s.clk.now, Sleep: sleep})
	bb := budget.New(budget.Options{Limiter: b.st.Limiter, Counters: b.st.Budget, Metrics: b.m, MaxInFlight: 1024, Now: s.clk.now, Sleep: sleep})
	addr := netip.MustParseAddr(address())
	subject := budget.SubjectOf(applied.Place, iface.Subject{}, true, addr, guard.AddressKey(addr))
	at := func(x *budget.Budgets) *budget.Charge { return x.Charge(applied, subject, "web/npm-all") }

	// Requests: a, b, a admitted; the fourth, at b, refused. Without Valkey each replica would hold 1.5 of the 3.
	var got []bool
	for _, x := range []*budget.Budgets{ba, bb, ba, bb} {
		got = append(got, at(x).Request(ctx) == nil)
	}
	if !got[0] || !got[1] || !got[2] || got[3] {
		t.Fatalf("admitted %v, want the first three of the two replicas together", got)
	}
	// Cached bytes: 100 served through a, the next hit at b refused before it starts.
	ch := at(ba)
	if r := ch.Transfer(ctx, true); r != nil {
		t.Fatal(r)
	}
	ch.Served(ctx, true, 100)
	// The lease of a's transfer is held: b's transfer waits its 10 s and is refused.
	other := at(bb)
	if r := other.Transfer(ctx, false); r == nil || r.Code != budget.CodeTooManyParallelTransfers {
		t.Fatalf("a second transfer while a holds the one lease: %+v", r)
	}
	ch.End(ctx)
	if r := at(bb).Transfer(ctx, true); r == nil || r.Code != budget.CodeDownloadLimitReached {
		t.Fatalf("a hit at b after a served the hour's cached bytes: %+v", r)
	}
	if r := at(bb).Transfer(ctx, false); r != nil {
		t.Fatalf("a fill at b once the lease is free: %+v", r)
	}
}
