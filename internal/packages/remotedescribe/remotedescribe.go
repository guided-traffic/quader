// Package remotedescribe is the task remote-describe (npm R-NPM-050, Debian R-DEB-037): it queues the job that describes a package a
// remote repository cached (npm-remote-describe, deb-remote-describe) for every package that holds a cached version or file without its
// row, so that what a remote cached before those jobs existed, and a version whose job failed, is described without waiting for a
// change at the upstream. It reads and requests nothing of an upstream: the jobs read the stored documents, tarballs and package files.
// A further Debian entry queues deb-contents, the reading of a package file's data archive (R-DEB-039, Q-DEB-144), for every package of
// a local or a remote Debian repository with a file the current reader has not read: the task walks stored files, whatever class holds
// them.
// An OCI entry queues oci-describe (R-OCI-040, Q-OCI-271) for every manifest of a local or a remote OCI repository stored before its
// blobs were kept or whose stored configuration is not read: it reads the stored manifest and configuration, never an upstream.
// An Ansible Galaxy entry queues ansible-reindex (R-ANS-044, R-ANS-053, Q-ANS-99) for every local or remote Ansible Galaxy repository
// with a version whose documentation or changelog an older generator made or never read: the reindex reads the stored archives.
// An entry of internal/packages/dependents (dependents.Fill, api-and-ui.md R-API-086) writes the rows of who depends on what for every
// version of npm, NuGet, Debian and Ansible Galaxy whose dependencies the current extractor has not read, from the formats' stored rows;
// it queues no job and reports none queued, so the run's bounds are its batches.
//
// A worker that starts queues a run when such a package exists (the start-up rule of R-HA-022's background data migrations); the
// scheduler runs it once a day, and an operator runs it with POST /api/v1/system/tasks/remote-describe/run (R-OPS-019). A run walks the
// packages in batches by id with a cursor it saves, queues the jobs below the priority of the jobs a fill queues, and stops while many
// describe jobs wait or when it queued its cap: it then queues the run after it with the cursor, after a pause. A package whose row
// exists is not selected, and the jobs coalesce per package, so a second run over a described store queues nothing.
package remotedescribe

import (
	"context"
	"encoding/json"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// TaskName is the task and its job kind (queue maintenance, coalesce: a run may queue the run after it while it runs).
const TaskName = "remote-describe"

// Schedule is the task's cron expression: once a day; maintenance.tasks.remote-describe overrides it (R-STOR-025).
const Schedule = "23 4 * * *"

// The bounds of a run.
const (
	// Batch is the number of packages one step selects and queues, in one transaction.
	Batch = 100
	// MaxPending is the number of waiting describe jobs (of both kinds, whoever queued them) at which a run stops and continues later.
	MaxPending = 200
	// MaxPerRun is the number of jobs one run queues before it continues later.
	MaxPerRun = 1000
	// Priority is the priority of the jobs a run queues: after the jobs a fill queues (the default, 100; a lower number runs first).
	Priority = 200
	// ContinueAfter is the pause before the run that continues a run that stopped.
	ContinueAfter = 5 * time.Minute
	timeout       = 30 * time.Minute
)

// Port is a format's part: the selection and the queueing of one batch of packages after the cursor (the data port's QueueUndescribed),
// and whether any package is left to describe.
type Port interface {
	QueueUndescribed(ctx context.Context, after int64, limit, priority int) (next int64, queued int, done bool, err error)
	HasUndescribed(ctx context.Context) (bool, error)
}

// Format is a format of the task: its job kind, which the pending count reads, and its port.
type Format struct {
	Kind string
	Port Port
}

// Jobs queues a job in a transaction; *jobs.Queue implements it.
type Jobs interface {
	Enqueue(ctx context.Context, tx meta.Tx, e jobs.EnqueueSpec) (jobs.Result, error)
}

// Options configure [New].
type Options struct {
	DB      meta.Runner
	Jobs    Jobs
	Formats []Format
	// Pending counts the waiting jobs of the kinds; nil reads the job table.
	Pending func(ctx context.Context, kinds []string) (int64, error)
}

// Task is the task of one replica.
type Task struct{ o Options }

// New creates the task.
func New(o Options) (*Task, error) {
	if o.DB == nil || o.Jobs == nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "remotedescribe: the task needs a transaction runner and a job queue")
	}
	t := &Task{o: o}
	if t.o.Pending == nil {
		t.o.Pending = t.pending
	}
	return t, nil
}

// KindSpec is the registration of the kind, with h as the handler: the composition root passes one that finds the replica's [Task].
func KindSpec(h jobs.Handler) jobs.KindSpec {
	return jobs.KindSpec{Queue: "maintenance", DedupMode: jobs.Coalesce, MaxAttempts: 3, Timeout: timeout, Handler: h}
}

// Cursor is where a run is: the format by its place in the task's list and the last package id read. A run that continues another
// starts from the cursor in its arguments; a scheduled or an operator's run starts at the beginning.
type Cursor struct {
	Format int   `json:"format"`
	After  int64 `json:"after"`
}

// Result is what a run records.
type Result struct {
	// State is "done" (the run reached the end of every format) or "continued" (it stopped at a bound and queued the run after it).
	State  string `json:"state"`
	Queued int    `json:"queued"`
}

// EnqueueIfNeeded queues a run when a format holds a package left to describe. A worker calls it when it starts; it reports whether a
// run was queued.
func (t *Task) EnqueueIfNeeded(ctx context.Context) (bool, error) {
	for _, f := range t.o.Formats {
		left, err := f.Port.HasUndescribed(ctx)
		if err != nil {
			return false, err
		}
		if left {
			return true, t.o.DB.InTx(ctx, func(tx meta.Tx) error {
				_, err := t.o.Jobs.Enqueue(ctx, tx, jobs.EnqueueSpec{Kind: TaskName, DedupKey: TaskName})
				return err
			})
		}
	}
	return false, nil
}

// Run is the handler of the kind.
func (t *Task) Run(ctx context.Context, run *jobs.Run) error {
	var cur Cursor
	if ok, err := run.LoadCheckpoint(&cur); err != nil {
		return err
	} else if !ok && len(run.Args) > 0 {
		_ = json.Unmarshal(run.Args, &cur) //nolint:errcheck // a run without a cursor starts at the beginning
	}
	res, err := t.Pass(ctx, &cur, func(c Cursor) error { return run.SaveCheckpoint(ctx, c) })
	if err != nil {
		return err
	}
	return run.SetResult(res)
}

// Pass walks the formats from the cursor, saving it after every batch through save, until the end or a bound: MaxPending waiting
// describe jobs, or MaxPerRun jobs queued by the pass. At a bound it queues the run after it, with the cursor, after ContinueAfter.
func (t *Task) Pass(ctx context.Context, cur *Cursor, save func(Cursor) error) (Result, error) {
	kinds := make([]string, len(t.o.Formats))
	for i, f := range t.o.Formats {
		kinds[i] = f.Kind
	}
	res := Result{State: "done"}
	for cur.Format < len(t.o.Formats) {
		pending, err := t.o.Pending(ctx, kinds)
		if err != nil {
			return res, err
		}
		if pending >= MaxPending || res.Queued >= MaxPerRun {
			res.State = "continued"
			return res, t.o.DB.InTx(ctx, func(tx meta.Tx) error {
				_, err := t.o.Jobs.Enqueue(ctx, tx, jobs.EnqueueSpec{Kind: TaskName, DedupKey: TaskName, Args: *cur, Delay: ContinueAfter})
				return err
			})
		}
		next, queued, done, err := t.o.Formats[cur.Format].Port.QueueUndescribed(ctx, cur.After, Batch, Priority)
		if err != nil {
			return res, err
		}
		res.Queued += queued
		cur.After = next
		if done {
			cur.Format, cur.After = cur.Format+1, 0
		}
		if err := save(*cur); err != nil {
			return res, err
		}
	}
	return res, nil
}

// pending reads the waiting jobs of the kinds from the job table.
func (t *Task) pending(ctx context.Context, kinds []string) (int64, error) {
	var n int64
	err := t.o.DB.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errs.E(errs.Internal, errs.CodeInternal, "remotedescribe: the transaction is not a PostgreSQL transaction")
		}
		var err error
		n, err = q.New(ptx).RemoteDescribePending(ctx, kinds)
		return err
	})
	return n, err
}
