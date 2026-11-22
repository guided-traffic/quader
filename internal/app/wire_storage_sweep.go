package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/jobs"
)

// The orphan sweep per connection with the adoption of an object of a live blob (storage.md R-STOR-026): package B of the build
// plan of several connections (docs/developer/blob-store.md), with the sessions on their connection (R-STOR-054, internal/uploads)
// and collection per copy with the release (R-STOR-057, internal/blob/gc.go). The sweep runs on the collector that wireBlob builds
// on worker and all, which holds the replica's connections.

// defaultSweepSchedule is the fallback schedule of the task orphan-sweep; maintenance.tasks.orphan-sweep has the same default and wins.
const defaultSweepSchedule = "0 5 * * 6"

// registerStorageSweep registers the kind and the task orphan-sweep: queue maintenance, exclusive under its own name, so that one sweep
// runs at a time; it may run beside a collection run, both resting on the row locks (R-STOR-024). The handler finds the collector at
// run time (a.gc); only worker and all build one, and only they claim a job.
func (a *App) registerStorageSweep(r *jobs.Registry) {
	r.RegisterKind(blob.SweepTask, jobs.KindSpec{
		Queue: "maintenance", DedupMode: jobs.Exclusive,
		Handler: a.gc.handler("orphan sweep", func(ctx context.Context, c *blob.Collector, run *jobs.Run) error {
			err := c.RunSweepJob(ctx, run.Args, run)
			if errors.Is(err, blob.ErrHandedOver) {
				// a sweep in its settle wait at the drain gives its job up: released, not failed (Q-STOR-217, Q-ARCH-501)
				return fmt.Errorf("%w: %w", jobs.ErrReleased, err)
			}
			return err
		}),
	})
	spec := jobs.TaskSpec{Schedule: defaultSweepSchedule, DedupKey: blob.SweepTask, Mutating: true}
	if t, ok := a.cfg.Maintenance.Tasks[blob.SweepTask]; ok && t.Schedule != "" {
		spec.Schedule, spec.Disabled = t.Schedule, !t.Enabled
	}
	r.RegisterTask(blob.SweepTask, spec)
}

// wireStorageSweep builds what the sweep's handler needs at run time from the blob area: nothing beyond the collector, which wireBlob
// sets.
func (a *App) wireStorageSweep(_ context.Context, _ blobArea) error { return nil }
