package blob

import (
	"context"
	"encoding/json"
	"fmt"
)

// This file is the whole of garbage collection's contact with the job
// framework (R-STOR-025). internal/blob is infrastructure and may not import
// internal/jobs, which is domain; the kinds are therefore registered where both
// are visible (internal/jobs imports internal/blob, as it imports
// internal/cluster for the coordination kinds), with this adapter as the
// handler body:
//
//	jobs.RegisterKind("gc", jobs.KindSpec{
//		Queue: "maintenance", DedupMode: jobs.Exclusive,
//		Handler: func(ctx context.Context, run *jobs.Run) error {
//			return collector.RunJob(ctx, blob.GCIncremental, run.Args, run)
//		},
//	})
//	jobs.RegisterKind("gc-full", jobs.KindSpec{ /* the same with blob.GCFull */ })
//	jobs.RegisterTask("gc", jobs.TaskSpec{Schedule: "*/30 * * * *", DedupKey: "gc"})
//	jobs.RegisterTask("gc-full", jobs.TaskSpec{Schedule: "0 2 * * 0", DedupKey: "gc"})
//
// Both tasks share the dedup key "gc" in exclusive mode, so at most one
// collection run is queued or running whichever task started it.
//
// The orphan sweep (R-STOR-026) is the kind and task orphan-sweep, queue
// maintenance, exclusive under its own name, with RunSweepJob as the handler
// body (internal/app/wire_storage_sweep.go). It may run beside a collection run:
// both rest on the row locks, not on the job key.

// GCJobArgs are the arguments of a collection job.
type GCJobArgs struct {
	// DryRun reports what would be deleted and changes nothing.
	DryRun bool `json:"dryRun"`
}

// JobRun is the part of the job framework's run handle that a collection run
// uses. *jobs.Run implements it.
type JobRun interface {
	// LoadCheckpoint decodes the checkpoint of an earlier attempt; false if none.
	LoadCheckpoint(v any) (bool, error)
	// SaveCheckpoint persists the cursor; an error (a lost lease) ends the run.
	SaveCheckpoint(ctx context.Context, v any) error
	// SetResult sets the result stored when the handler returns nil.
	SetResult(v any) error
	// Draining is closed when the replica begins its shutdown.
	Draining() <-chan struct{}
}

// RunJob is the handler body of the kinds gc and gc-full: it reads the job's
// arguments and, for a full sweep, the checkpoint of a previous attempt, runs the
// collection, saves the keyset position after every batch and stores the
// result. A job released at shutdown resumes from its checkpoint on another
// worker; the incremental run needs none, because its candidates are the
// remaining queue entries.
func (c *Collector) RunJob(ctx context.Context, mode GCMode, args json.RawMessage, run JobRun) error {
	var a GCJobArgs
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &a); err != nil {
			return fmt.Errorf("garbage collection arguments: %w", err)
		}
	}
	var cur Cursor
	if mode == GCFull {
		if _, err := run.LoadCheckpoint(&cur); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	res, err := c.Run(ctx, RunOptions{
		Mode: mode, DryRun: a.DryRun, Cursor: cur, Draining: run.Draining(),
		Checkpoint: func(next Cursor) {
			if serr := run.SaveCheckpoint(ctx, next); serr != nil {
				cancel(fmt.Errorf("save checkpoint: %w", serr))
			}
		},
	})
	if cause := context.Cause(ctx); err != nil && cause != nil && ctx.Err() != nil {
		err = cause
	}
	if rerr := run.SetResult(res); rerr != nil && err == nil {
		err = rerr
	}
	return err
}

// RunSweepJob is the handler body of the kind orphan-sweep (R-STOR-026): it reads the job's arguments, continues from the
// checkpoint of an earlier attempt of the same job or, with resume (the default), from that of the latest run that did not walk to
// its end (Q-STOR-261), saves the cursor after every prefix and stores the result.
func (c *Collector) RunSweepJob(ctx context.Context, args json.RawMessage, run JobRun) error {
	var a SweepJobArgs
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &a); err != nil {
			return fmt.Errorf("orphan sweep arguments: %w", err)
		}
	}
	minAge, err := a.minAge()
	if err != nil {
		return err
	}
	var cur SweepCursor
	found, err := run.LoadCheckpoint(&cur)
	if err != nil {
		return err
	}
	if !found && a.resume() {
		if cur, err = c.resumePoint(ctx, a.Connection, a.DryRun); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	res, err := c.Sweep(ctx, SweepOptions{
		DryRun: a.DryRun, MinAge: minAge, Connection: a.Connection, Cursor: cur, Draining: run.Draining(),
		Checkpoint: func(next SweepCursor) {
			if serr := run.SaveCheckpoint(ctx, next); serr != nil {
				cancel(fmt.Errorf("save checkpoint: %w", serr))
			}
		},
	})
	if cause := context.Cause(ctx); err != nil && cause != nil && ctx.Err() != nil {
		err = cause
	}
	if rerr := run.SetResult(res); rerr != nil && err == nil {
		err = rerr
	}
	return err
}
