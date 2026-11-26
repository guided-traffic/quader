package server

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/guard"
	bmetrics "github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/bastion/shared"
	"github.com/guided-traffic/quader/internal/bastion/throughput"
	"github.com/guided-traffic/quader/internal/bastion/usage"
)

// TestR_LIM_045_TheStatusCarriesTheCacheTheThroughputAndTheBlocksByTheirCodes: the state a replica reports reads its cache's gauges
// and its requests, its throughput, and lists the blocks it set as canonical prefixes with a reason of the closed list; it passes
// the interface's bounds. No data beyond counters.
func TestR_LIM_045_TheStatusCarriesTheCacheTheThroughputAndTheBlocksByTheirCodes(t *testing.T) {
	m, err := bmetrics.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	st := shared.NewLocal(1, nil)
	g, err := guard.New(guard.Options{State: st, Metrics: m})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	ctx := context.Background()
	for range 20 {
		g.FailedAuth(ctx, "203.0.113.9")
	}
	holder := settings.New(settings.Options{})
	m.CacheBytes.WithLabelValues(bmetrics.AreaArtifacts).Set(1000)
	m.CacheCapacity.WithLabelValues(bmetrics.AreaArtifacts).Set(5000)
	m.Requests.WithLabelValues(bmetrics.ClassArtifact, bmetrics.ResultHit).Add(3)
	m.Requests.WithLabelValues(bmetrics.ClassArtifact, bmetrics.ResultMiss).Add(1)
	r := &reporting{
		replica: "bastion-0", m: m, usage: usage.New(0), guard: g, state: st, now: time.Now,
		pacer: throughput.New(throughput.Options{Bytes: st.Bytes, Figures: holder.Current, Metrics: m}),
	}
	s := r.status()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if s.Replica != "bastion-0" || s.Cache.Bytes != 1000 || s.Cache.CapacityBytes != 5000 || s.Cache.Hits != 3 || s.Cache.Misses != 1 {
		t.Fatalf("the status %+v", s)
	}
	if len(s.Blocks) != 1 || s.Blocks[0].Address != "203.0.113.9/32" || s.Blocks[0].Reason != bmetrics.BlockFailedAuth || s.Blocked != 1 {
		t.Fatalf("the blocks %+v (blocked %d)", s.Blocks, s.Blocked)
	}
}
