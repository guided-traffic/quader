package summary

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/meta/postgres"
)

// JobRun is the part of the job framework's run handle a run of the task uses. *jobs.Run implements it.
type JobRun interface {
	// SetResult sets the result stored when the handler returns nil.
	SetResult(v any) error
}

// Result is what a run stores with its job: when it computed, how long it took and how much it counted.
type Result struct {
	ComputedAt   time.Time `json:"computedAt"`
	DurationMs   int64     `json:"durationMs"`
	Blobs        int64     `json:"blobs"`
	Connections  int       `json:"connections"`
	Repositories int       `json:"repositories"`
}

// RunJob is the body of the handler of the kind storage-summary (R-OPS-023): it computes the summary and replaces the snapshot. The task
// takes no parameters. A run that a drain or a cancellation ends has written nothing; the next run computes again.
func RunJob(ctx context.Context, db *postgres.DB, o Options, run JobRun) error {
	s, err := Compute(ctx, db, o)
	if err != nil {
		return err
	}
	if err := Save(ctx, db, s); err != nil {
		return err
	}
	return run.SetResult(Result{
		ComputedAt: s.ComputedAt, DurationMs: s.DurationMs, Blobs: s.Blobs.Count, Connections: len(s.Connections), Repositories: len(s.Repositories),
	})
}
