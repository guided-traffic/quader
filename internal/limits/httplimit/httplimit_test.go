package httplimit_test

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/limits/httplimit"
	"github.com/guided-traffic/quader/internal/qname"
)

// R-LIM-020, Q-LIM-26: the 429 of a figure that refuses beside the request figures carries that figure's member of RateLimit-Policy
// and RateLimit, with the quota unit of a figure that does not count requests. Each of the six is refused by the engine on in-memory
// counters and its fields are read from the header SetRefusalPolicy writes; milliseconds.
func TestR_LIM_020_TheRateLimitPolicyOfEveryFigureThatRefusesBesideTheRequestFigures(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	caller := limits.Caller{Kind: limits.CallerAnonymous, Anonymous: true, Client: netip.MustParseAddr("198.51.100.20")}
	engine := func(fig limits.Figure, limit int64) *limits.Engine {
		var f limits.Figures
		f[fig] = limit
		x := limits.NewIndex(limits.Snapshot{
			Profiles: []limits.Profile{{ID: "p-one", Key: "one", Name: "one", Figures: f}},
			Rules: []limits.Rule{{
				ID: "r-anon", Place: limits.InstancePlace(), WhoKind: limits.WhoEvery, WhoRef: limits.EveryAnonymous, Profile: "one",
			}},
			Settings: limits.Settings{Mode: limits.ModeEnforce, BlockSeconds: 900, Version: 1},
		})
		return limits.NewEngine(limits.Options{
			Store: cluster.NewMemoryLimits(), Index: func() *limits.Index { return x }, Replica: "r1", LeaseTTL: time.Minute,
			Share: func() int { return 1 }, Now: func() time.Time { return at },
		})
	}
	// spend ends a request with what add charges it and flushes it to the counters.
	spend := func(e *limits.Engine, add func(*limits.Charge)) {
		c := e.Begin(caller, qname.Repo{})
		add(c)
		e.Finish(c)
		e.Flush(ctx)
	}
	for _, tc := range []struct {
		fig    limits.Figure
		limit  int64
		policy string
		refuse func(e *limits.Engine, c *limits.Charge) error
	}{
		{limits.CachedBytesPerHour, 10, `"cached-hour";q=10;w=3600;qu="content-bytes"`, func(e *limits.Engine, c *limits.Charge) error {
			spend(e, func(c *limits.Charge) { c.AddCached(10) })
			return e.Admit(ctx, c)
		}},
		{limits.BytesPerDay, 10, `"day";q=10;w=86400;qu="content-bytes"`, func(e *limits.Engine, c *limits.Charge) error {
			spend(e, func(c *limits.Charge) { c.AddCached(10) })
			return e.Admit(ctx, c)
		}},
		{limits.ParallelTransfers, 1, `"parallel";q=1;w=1;qu="concurrent-requests"`, func(e *limits.Engine, c *limits.Charge) error {
			if err := e.Admit(ctx, e.Begin(caller, qname.Repo{})); err != nil {
				t.Fatalf("the first download: %v", err)
			}
			return e.Admit(ctx, c)
		}},
		{limits.UncachedBytesPerHour, 10, `"uncached-hour";q=10;w=3600;qu="content-bytes"`, func(e *limits.Engine, c *limits.Charge) error {
			spend(e, func(c *limits.Charge) { c.AddUncached(10) })
			return e.MayFetch(ctx, c, 5, false)
		}},
		{limits.UpstreamRequestsPerHour, 1, `"upstream-hour";q=1;w=3600`, func(e *limits.Engine, c *limits.Charge) error {
			if err := e.MayFetch(ctx, e.Begin(caller, qname.Repo{}), 5, false); err != nil {
				t.Fatalf("the first upstream request: %v", err)
			}
			return e.MayFetch(ctx, c, 5, false)
		}},
		{limits.RefusedPerHour, 1, `"refused-hour";q=1;w=3600`, func(e *limits.Engine, c *limits.Charge) error {
			for range 2 {
				spend(e, func(c *limits.Charge) { c.MarkRefused() })
			}
			e.Flush(ctx) // sets the marker of the block found due
			return e.Check(ctx, c, time.Time{}, nil).Err
		}},
	} {
		t.Run(tc.fig.String(), func(t *testing.T) {
			e := engine(tc.fig, tc.limit)
			c := e.Begin(caller, qname.Repo{})
			if err := tc.refuse(e, c); err == nil {
				t.Fatal("the figure did not refuse")
			}
			h := http.Header{}
			httplimit.SetRefusalPolicy(h, c)
			name := tc.policy[:strings.Index(tc.policy, ";")]
			if got := h.Values("RateLimit-Policy"); len(got) != 1 || got[0] != tc.policy {
				t.Fatalf("RateLimit-Policy %q, want %q", got, tc.policy)
			}
			if got := h.Values("RateLimit"); len(got) != 1 || !strings.HasPrefix(got[0], name+";r=0;t=") || strings.HasSuffix(got[0], "t=0") {
				t.Fatalf("RateLimit %q, want %s with r=0 and the time of the Retry-After", got, name)
			}
		})
	}
}
