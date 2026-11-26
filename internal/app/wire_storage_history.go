package app

import (
	"context"
	"errors"

	"github.com/guided-traffic/quader/internal/blob/history"
	"github.com/guided-traffic/quader/internal/jobs"
)

// The history of the stored content (storage.md R-STOR-108 to R-STOR-110, ADR-315): each replica's recorder is wired with the blob area
// (wire_blob.go); the task storage-history keeps the three tables bounded.

// registerStorageHistory registers the kind and the task storage-history: queue maintenance, exclusive, hourly by default
// (maintenance.tasks.storage-history, Q-STOR-521). It deletes what is older than storage.history.retention, ends the intervals of a
// replica that is gone and resolves the findings no copy holds any more; it writes no content, so it is not mutating and runs in
// read-only maintenance mode too (R-OPS-024). The handler needs the database and the configuration only.
func (a *App) registerStorageHistory(r *jobs.Registry) {
	r.RegisterKind(history.Task, jobs.KindSpec{
		Queue: "maintenance", DedupMode: jobs.Exclusive, MaxAttempts: 3,
		Handler: func(ctx context.Context, run *jobs.Run) error {
			db := a.database()
			if db == nil {
				return jobs.Permanent(errors.New("storage-history: this replica has no database"))
			}
			return history.RunTask(ctx, db, history.TaskOptions{Retention: a.cfg.Storage.History.Retention}, run)
		},
	})
	spec := jobs.TaskSpec{Schedule: history.DefaultSchedule}
	if t, ok := a.cfg.Maintenance.Tasks[history.Task]; ok && t.Schedule != "" {
		spec.Schedule, spec.Disabled = t.Schedule, !t.Enabled
	}
	r.RegisterTask(history.Task, spec)
}
