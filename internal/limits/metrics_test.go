package limits

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/qname"
)

// R-LIM-030, Q-LIM-112: every round of the charge buffer that ends counts once in quader_limits_flushes_total, also a round that
// had nothing to send, so a test can wait for the round after a request instead of a time.
func TestR_LIM_030_EveryRoundOfTheChargeBufferThatEndedIsCounted(t *testing.T) {
	reg := prometheus.NewRegistry()
	x := NewIndex(builtinSnapshot())
	mem := cluster.NewMemoryLimits()
	e := NewEngine(Options{
		Store: &countingStore{mem: mem}, Index: func() *Index { return x }, Replica: "r1", Metrics: NewMetrics(reg),
		LeaseTTL: time.Minute, Share: func() int { return 1 },
	})
	flushes := func() float64 {
		t.Helper()
		mfs, err := reg.Gather()
		if err != nil {
			t.Fatal(err)
		}
		for _, mf := range mfs {
			if mf.GetName() == "quader_limits_flushes_total" {
				return mf.GetMetric()[0].GetCounter().GetValue()
			}
		}
		t.Fatal("quader_limits_flushes_total is not registered")
		return 0
	}
	if got := flushes(); got != 0 {
		t.Fatalf("before the first round: %v", got)
	}
	c := e.Begin(anonCaller("198.51.100.7"), qname.Repo{})
	c.AddCached(10)
	e.Flush(context.Background())
	e.Flush(context.Background())
	if got := flushes(); got != 2 {
		t.Fatalf("after two rounds, one of them empty: %v, want 2", got)
	}
}
