package sortkey

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/jobs"
)

// TestR_HA_022_TheFillIsASingletonOnTheMaintenanceQueue: the kind is named as R-HA-022 names a background migration, runs on the
// maintenance queue and coalesces, so a run that leaves rows can queue the one after it while two never run together (Q-HA-492).
func TestR_HA_022_TheFillIsASingletonOnTheMaintenanceQueue(t *testing.T) {
	spec := JobSpec(func(context.Context, *jobs.Run) error { return nil })
	if spec.Queue != "maintenance" || spec.DedupMode != jobs.Coalesce || spec.Handler == nil {
		t.Errorf("spec = queue %q, mode %q", spec.Queue, spec.DedupMode)
	}
	if JobKind != "data_migration_version_sort_key" {
		t.Errorf("kind = %q", JobKind)
	}
	r := jobs.NewRegistry()
	r.RegisterKind(JobKind, spec) // panics on a name the registry refuses
	if _, err := New(Options{BatchSize: 1}); err == nil {
		t.Error("a fill without a runner, a queue and a key was built")
	}
}

// TestR_HA_022_TheFillIsDeclaredAsABackgroundMigration: the declaration the report of GET /api/v1/system/migrations and the contract
// guard read (Q-HA-114) names the kind's migration and the completion record, whose filled rows are the count of a completed run.
func TestR_HA_022_TheFillIsDeclaredAsABackgroundMigration(t *testing.T) {
	m := Background()
	if m.Name != Name || m.Kind() != JobKind || m.DoneKey != StateKey || m.Release == "" {
		t.Fatalf("declaration = %+v", m)
	}
	raw, err := json.Marshal(Result{State: StateFilled, Filled: 42})
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := m.Processed(raw); !ok || n != 42 {
		t.Errorf("processed = %d, %v", n, ok)
	}
	if _, ok := m.Processed([]byte("{")); ok {
		t.Error("an unreadable record was counted")
	}
}

// TestR_HA_022_ThePauseOfTheFillEndsWithTheRun: the pause between two batches returns at once when the run ends.
func TestR_HA_022_ThePauseOfTheFillEndsWithTheRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := pause(ctx, time.Hour); err == nil || time.Since(start) > time.Second {
		t.Errorf("pause after the end of the run: %v after %s", err, time.Since(start))
	}
	if err := pause(context.Background(), 0); err != nil {
		t.Errorf("no pause: %v", err)
	}
}
