package app

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/limits"
)

// modeGauge reads quader_limits_mode from reg; -1 when the family is not there.
func modeGauge(t *testing.T, reg *prometheus.Registry) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == "quader_limits_mode" && len(f.GetMetric()) == 1 {
			return f.GetMetric()[0].GetGauge().GetValue()
		}
	}
	return -1
}

// TestR_LIM_030_TheFirstIndexSetsTheModeGauge: quader_limits_mode shows the mode of the index the replica installed from its
// first load on, not from the first reload a minute later, and the reload loop reads nothing the wiring still writes (the hook
// was set after the index had started, so a change arriving during the wiring raced with it). The loop is kicked while the test
// reads; run with -race. Milliseconds, no data, no database: the snapshot is a function.
func TestR_LIM_030_TheFirstIndexSetsTheModeGauge(t *testing.T) {
	reg := prometheus.NewRegistry()
	snapshot := func(context.Context) (limits.Snapshot, error) {
		return limits.Snapshot{Settings: limits.Settings{Mode: limits.ModeEnforce}}, nil
	}
	idx := newLimitsIndex(snapshot, slog.New(slog.NewTextHandler(io.Discard, nil)), reg, limits.NewMetrics(reg))
	idx.register(nil)
	if err := idx.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Stop(context.Background()) })
	if got := modeGauge(t, reg); got != 1 {
		t.Fatalf("quader_limits_mode after the first load = %v, want 1 (enforce)", got)
	}
	// A change arrives while the replica is still being wired: the loop installs again.
	deadline := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
		idx.reload()
		if got := modeGauge(t, reg); got != 1 {
			t.Fatalf("quader_limits_mode after a reload = %v, want 1", got)
		}
	}
}
