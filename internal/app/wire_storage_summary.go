package app

import (
	"context"
	"errors"
	"time"

	"github.com/guided-traffic/quader/internal/blob/summary"
	"github.com/guided-traffic/quader/internal/jobs"
)

// The storage summary and the object-store connections of the API (storage.md R-STOR-028, R-STOR-061; operations-and-observability.md
// R-OPS-023): package F of the build plan of several connections (docs/developer/blob-store.md), built in internal/blob/summary.

// registerStorageSummary registers the kind and the task storage-summary: queue maintenance, exclusive, hourly by default
// (maintenance.tasks.storage-summary). The task writes only its own snapshot, no content, so it is not mutating and runs in read-only
// maintenance mode too (R-OPS-024). The handler needs the database and the worker's configuration only.
func (a *App) registerStorageSummary(r *jobs.Registry) {
	r.RegisterKind(summary.Task, jobs.KindSpec{
		Queue: "maintenance", DedupMode: jobs.Exclusive, MaxAttempts: 3,
		Handler: func(ctx context.Context, run *jobs.Run) error {
			db := a.database()
			if db == nil {
				return jobs.Permanent(errors.New("storage-summary: this replica has no database"))
			}
			return summary.RunJob(ctx, db, summary.Options{Periods: a.scrubPeriods()}, run)
		},
	})
	spec := jobs.TaskSpec{Schedule: summary.DefaultSchedule}
	if t, ok := a.cfg.Maintenance.Tasks[summary.Task]; ok && t.Schedule != "" {
		spec.Schedule, spec.Disabled = t.Schedule, !t.Enabled
	}
	r.RegisterTask(summary.Task, spec)
}

// scrubPeriods is the scrub_period of every connection of this replica's configuration (R-STOR-050), the period the summary judges a
// copy's verification by.
func (a *App) scrubPeriods() map[string]time.Duration {
	out := map[string]time.Duration{}
	for _, el := range a.cfg.Storage.Connections {
		el = el.WithDefaults()
		out[el.ID] = el.ScrubPeriod
	}
	return out
}

// registerSummaryGauges adds the gauges of the latest storage summary to the gauges read from PostgreSQL on every replica with the
// main pool: quader_storage_summary_age_seconds, quader_storage_blobs_damaged, quader_storage_copies and the backlog and fill of every
// connection (R-OPS-008, R-STOR-056, R-STOR-060).
func (a *App) registerSummaryGauges() {
	if a.sub.Shell || !a.sub.MainPool {
		return
	}
	a.tele.metrics.DBGauges().Add(summary.GaugeSource(a.database))
}

// storageConnections is the service of the connections' list (R-STOR-061): the rows, the registry and the latest summary from
// PostgreSQL, joined with this replica's own connections.
func (a *App) storageConnections(ctx context.Context) ([]summary.View, error) {
	db := a.database()
	if db == nil {
		return nil, errors.New("the database is not open on this replica")
	}
	conns, _ := a.objectStores.get()
	return summary.List(ctx, db, a.cfg.System.HeartbeatStaleAfter, summary.LocalOf(conns))
}

// storageSummary is the service of GET /api/v1/storage/summary (R-STOR-028): the latest snapshot, never a computation.
func (a *App) storageSummary(ctx context.Context) (summary.Summary, error) {
	db := a.database()
	if db == nil {
		return summary.Summary{}, errors.New("the database is not open on this replica")
	}
	return summary.Latest(ctx, db)
}
