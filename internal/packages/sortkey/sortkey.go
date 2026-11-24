// Package sortkey is the background data migration version_sort_key (ha-and-helm.md R-HA-022, Q-HA-492; api-and-ui.md R-API-029,
// Q-API-430): it fills the sort key and the pre-release flag of the version rows written without them. Migration 00116 added both as
// nullable columns, because the key is the format's code's (formats.ViewAdapter.SortKey) and PostgreSQL cannot compute it; the rows
// stored before that step, and those a replica of the release before it writes during a rolling upgrade (R-HA-020), have neither.
// Until the fill has reached them, the reads give such a row the key its format gives it (Q-API-430), so the version list and the
// latest version stay right meanwhile; the fill makes them cheap again.
//
// The fill reads the rows without a key in keyset batches of jobs.background_migrations.batch_size by id, computes each key with the
// function the composition root passes (the host's formathost.VersionOrder, the one the package writer stores), writes them and saves
// its cursor, so a run released at shutdown resumes where it stopped. It records its completion only after a pass that leaves no row
// without a key: a replica of the release before can write such a row behind the cursor while it is live, and the next run, queued
// with a delay, takes it. The data say when the old writers are gone, so the fill names no release it waits for (Q-HA-492).
package sortkey

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// JobKind is the kind of the fill, named data_migration_<name> as R-HA-022 names a background data migration. Queue maintenance; dedup
// mode coalesce, so that a run that leaves rows to fill can queue the run after it, and two runs never run together.
const JobKind = postgres.BackgroundKindPrefix + Name

// Name is the migration's name in GET /api/v1/system/migrations and `quader migrate status`.
const Name = "version_sort_key"

// StateKey is the system_state key (R-OPS-024) that records the completion; its value is the [Result] of the run that completed it.
// While it is absent, or a row without a key exists, a worker that starts queues the fill.
const StateKey = "version_sort_key_filled"

// Release is the release the fill was written for, which the report shows; nothing waits for it (Q-HA-492).
const Release = "0.123.0"

// Background declares the fill as a background data migration of R-HA-022 (Q-HA-114): the report lists it with its state, and a
// contract step may depend on it (Q-HA-115), such as the step that makes the columns NOT NULL. A completed run's count is the rows it
// filled, read from the completion record, which outlives the job rows.
func Background() postgres.BackgroundMigration {
	return postgres.BackgroundMigration{
		Name:    Name,
		Release: Release,
		DoneKey: StateKey,
		Processed: func(done []byte) (int64, bool) {
			var r Result
			if err := json.Unmarshal(done, &r); err != nil {
				return 0, false
			}
			return r.Filled, true
		},
	}
}

// The states a run records.
const (
	// StateFilled: the run's pass left no row without a key; the completion is recorded.
	StateFilled = "filled"
	// StateWaiting: rows without a key are left after the pass (written behind the cursor by a replica of the release before, or held
	// by a writer's transaction); the run queued the run after it and recorded no completion.
	StateWaiting = "waiting"
)

// Result is what a run records as its result and, when it completed the fill, as the value of [StateKey].
type Result struct {
	State string `json:"state"`
	// Filled counts the rows the run wrote a key to.
	Filled int64 `json:"filled"`
}

// checkpoint is the cursor a run saves after every batch: the id of the last row read and the rows filled so far.
type checkpoint struct {
	After  int64 `json:"after"`
	Filled int64 `json:"filled"`
}

// KeyFunc returns the sort key and the pre-release answer of a version of the format with the ID: the host's formathost.VersionOrder.
type KeyFunc func(format, version string) (key []byte, prerelease bool)

// Jobs queues a job in a transaction. *jobs.Queue implements it.
type Jobs interface {
	Enqueue(ctx context.Context, tx meta.Tx, e jobs.EnqueueSpec) (jobs.Result, error)
}

// Options configure [New].
type Options struct {
	// DB runs the transactions of the batches and of the completion.
	DB meta.Runner
	// Jobs queues the fill at the start of a worker and the run after one that left rows.
	Jobs Jobs
	// Key computes a row's key and flag.
	Key KeyFunc
	// BatchSize and Pause are jobs.background_migrations.batch_size and pause_between_batches.
	BatchSize int
	Pause     time.Duration
	// Wait is how long a run that left rows waits before the run after it; zero is one minute.
	Wait time.Duration
	Log  *slog.Logger
}

// Fill is the background migration. One value per replica; its handler runs on a worker.
type Fill struct{ o Options }

// New creates the fill.
func New(o Options) (*Fill, error) {
	if o.DB == nil || o.Jobs == nil || o.Key == nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "sortkey: the fill needs a transaction runner, a job queue and the formats' key")
	}
	if o.BatchSize < 1 {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "sortkey: the fill needs a batch size of at least 1")
	}
	if o.Wait <= 0 {
		o.Wait = time.Minute
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return &Fill{o: o}, nil
}

// JobSpec is the registration of the kind, with h as the handler: the composition root passes one that finds the replica's [Fill] at
// run time.
func JobSpec(h jobs.Handler) jobs.KindSpec {
	return jobs.KindSpec{Queue: "maintenance", DedupMode: jobs.Coalesce, Handler: h}
}

// Enqueue queues the fill unless its completion is recorded and no row without a key exists, in one transaction. A completion that
// rows without a key followed (a rollback to the release before migration 00116 and an upgrade again, a restore of a backup taken
// before it) is removed first, so the report shows the fill pending until it has filled them. A worker calls it when it starts. It
// reports whether a run is queued.
func (f *Fill) Enqueue(ctx context.Context) (bool, error) {
	var queued bool
	err := f.o.DB.InTx(ctx, func(tx meta.Tx) error {
		queued = false
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		st, err := qs.VersionSortKeyState(ctx, StateKey)
		if err != nil {
			return err
		}
		unkeyed, err := qs.VersionSortKeyUnkeyed(ctx)
		if err != nil {
			return err
		}
		if len(st) > 0 && !unkeyed {
			return nil
		}
		if len(st) > 0 {
			if err := qs.VersionSortKeyReopen(ctx, StateKey); err != nil {
				return err
			}
		}
		if _, err := f.o.Jobs.Enqueue(ctx, tx, jobs.EnqueueSpec{Kind: JobKind}); err != nil {
			return err
		}
		queued = true
		return nil
	})
	return queued, err
}

// Run is the handler of the kind. It is idempotent: a row is written only while it has no key, and a run on a database without such
// a row records the completion at once.
func (f *Fill) Run(ctx context.Context, run *jobs.Run) error {
	var cp checkpoint
	if _, err := run.LoadCheckpoint(&cp); err != nil {
		return err
	}
	for {
		n, err := f.batch(ctx, &cp)
		if err != nil {
			return err
		}
		if n == 0 {
			break
		}
		if err := run.SaveCheckpoint(ctx, cp); err != nil {
			return err
		}
		if err := run.SaveProgress(ctx, cp.Filled, 0, "versions"); err != nil {
			return err
		}
		if n < f.o.BatchSize {
			break
		}
		if err := pause(ctx, f.o.Pause); err != nil {
			return err
		}
	}
	unkeyed, err := q.New(run.DB()).VersionSortKeyUnkeyed(ctx)
	if err != nil {
		return err
	}
	if unkeyed {
		if err := f.o.DB.InTx(ctx, func(tx meta.Tx) error {
			_, err := f.o.Jobs.Enqueue(ctx, tx, jobs.EnqueueSpec{Kind: JobKind, Delay: f.o.Wait})
			return err
		}); err != nil {
			return err
		}
		f.o.Log.Info("version rows without a sort key are left after a pass of the fill; the next run follows",
			"filled", cp.Filled, "next_run_in", f.o.Wait.String())
		return run.SetResult(Result{State: StateWaiting, Filled: cp.Filled})
	}
	res := Result{State: StateFilled, Filled: cp.Filled}
	if err := f.record(ctx, res); err != nil {
		return err
	}
	f.o.Log.Info("the sort keys of the versions stored without one are filled", "filled", res.Filled)
	return run.SetResult(res)
}

// batch reads, computes and writes one batch in one transaction and advances the cursor. It returns the number of rows read.
func (f *Fill) batch(ctx context.Context, cp *checkpoint) (int, error) {
	var read int
	var filled int64
	var after int64
	err := f.o.DB.InTx(ctx, func(tx meta.Tx) error {
		read, filled, after = 0, 0, cp.After
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.VersionSortKeyBatch(ctx, q.VersionSortKeyBatchParams{After: cp.After, Batch: int32(min(f.o.BatchSize, 1<<30))}) //nolint:gosec // bounded above
		if err != nil || len(rows) == 0 {
			return err
		}
		p := q.VersionSortKeyFillParams{
			Ids: make([]int64, len(rows)), SortKeys: make([][]byte, len(rows)), Prereleases: make([]bool, len(rows)),
		}
		for i, r := range rows {
			key, pre := f.o.Key(r.Format, r.Version)
			if len(key) == 0 {
				return errs.E(errs.Internal, errs.CodeInternal, "sortkey: the format "+r.Format+" gave an empty key")
			}
			p.Ids[i], p.SortKeys[i], p.Prereleases[i] = r.ID, key, pre
		}
		if filled, err = qs.VersionSortKeyFill(ctx, p); err != nil {
			return err
		}
		read, after = len(rows), rows[len(rows)-1].ID
		return nil
	})
	if err != nil {
		return 0, err
	}
	cp.After, cp.Filled = after, cp.Filled+filled
	return read, nil
}

// record writes the completion.
func (f *Fill) record(ctx context.Context, res Result) error {
	raw, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return f.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		return qs.VersionSortKeyDone(ctx, q.VersionSortKeyDoneParams{Key: StateKey, Value: string(raw)})
	})
}

// queries binds the statements to the PostgreSQL transaction behind tx.
func queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "sortkey: the transaction is not a PostgreSQL transaction")
	}
	return q.New(ptx), nil
}

// pause waits between two batches, or until the run ends.
func pause(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
