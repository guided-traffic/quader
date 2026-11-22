// Package reconcile is the reconciliation between the object-store connections (storage.md R-STOR-056): the worker job that
// brings every blob onto every connection that is not draining. It works from the queue blob_copy_queue, which the commit and
// every mark of a damaged copy fill, and from a pass over the live blobs that queues what no row stands for; it copies a blob
// server-side within one endpoint and streams it through the worker across providers, records each copy within the insert
// bound of R-STOR-005, empties a draining connection, retires a drained one, and copies the revocation journal to a connection
// that lacks part of it.
//
// It is infrastructure: it imports no domain package. The composition root registers the job kind and the task
// (internal/app/wire_storage_reconcile.go) and passes the handler [Reconciler.RunJob].
package reconcile

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
)

// Kind is the job kind and the maintenance task of the reconciliation (R-STOR-056): queue maintenance, exclusive under its
// name, scheduled every minute and signalled after a commit that queued a gap.
const Kind = "storage-reconcile"

// DefaultSchedule is the task's schedule when maintenance.tasks names none.
const DefaultSchedule = "* * * * *"

const (
	// passBatch is the live blobs one batch of the pass reads (R-STOR-056).
	passBatch = 1000
	// PassInterval is how long after a full pass the next starts when no connection is filling (Q-STOR-257).
	PassInterval = time.Hour
	// copyBatchPerSlot is the queue rows one batch of the copy step takes per slot of storage.reconcile.concurrency.
	copyBatchPerSlot = 8
	// drainBatch is the copies one batch of a drain walks.
	drainBatch = 500
	// recordAttempts bounds the writes of one copy that the insert bound sends back.
	recordAttempts = 3
	// lastErrorBytes bounds the reason a failed queue row keeps.
	lastErrorBytes = 300
)

// Options configure a [Reconciler].
type Options struct {
	// Store is the worker's blob store: its connections, and the marks of a damaged source.
	Store *blob.Store
	// DB runs the reconciliation's statements; *postgres.DB implements it.
	DB blob.DB
	// Storage is the storage configuration: storage.reconcile, gc.delete_settle and gc.handover_delay, upload.commit_deadline and
	// object_store.multipart.part_size.
	Storage config.Storage
	Metrics *Metrics
	Log     *slog.Logger
	// StaleAfter is system.heartbeat_stale_after: a replica whose heartbeat is older configures nothing (retirement).
	StaleAfter time.Duration
	// JournalDir is the directory of the revocation journal below a connection's prefix (internal/journal.Dir); empty skips the
	// journal pass.
	JournalDir string
	// Drained is called in the transaction that records a draining connection as drained: the audit event
	// system.storage.connection_drained with the copies the drain deleted.
	Drained func(ctx context.Context, tx meta.Tx, conn string, copiesDeleted int64) error
	// Now replaces the clock of the insert bound and the throttle; tests use it.
	Now func() time.Time
	// AfterWrite is called after each copy's object write and before its row is recorded; tests use it to stretch the time
	// between them.
	AfterWrite func(conn string, d blob.Digest)
}

// Reconciler runs the reconciliation. One run at a time per installation: the job is exclusive (Q-STOR-235).
type Reconciler struct {
	o     Options
	store *blob.Store
	db    blob.DB
	cfg   config.StorageReconcile
	log   *slog.Logger
	now   func() time.Time
}

// New builds the reconciler.
func New(o Options) (*Reconciler, error) {
	if o.Store == nil || o.DB == nil {
		return nil, errors.New("reconcile: needs the blob store and the database")
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Reconciler{o: o, store: o.Store, db: o.DB, cfg: o.Storage.Reconcile, log: o.Log, now: o.Now}, nil
}

// JobRun is the part of the job framework's run handle a run uses; *jobs.Run implements it.
type JobRun interface {
	// SetResult sets the result stored when the handler returns nil.
	SetResult(v any) error
	// Draining is closed when the replica begins its shutdown.
	Draining() <-chan struct{}
}

// RunJob is the handler body of the kind storage-reconcile: one run, its result stored with the job.
func (r *Reconciler) RunJob(ctx context.Context, run JobRun) error {
	res, err := r.Run(ctx, run.Draining())
	if rerr := run.SetResult(res); rerr != nil && err == nil {
		err = rerr
	}
	return err
}

// Result is what one run did.
type Result struct {
	// Copied counts the copies recorded, ServerSide and Streamed by mode; Bytes are the streamed bytes.
	Copied     int   `json:"copied"`
	ServerSide int   `json:"serverSide"`
	Streamed   int   `json:"streamed"`
	Bytes      int64 `json:"bytes"`
	// Failed counts the copies that failed and wait for their back-off; Dropped the queue rows that needed no copy.
	Failed  int `json:"failed"`
	Dropped int `json:"dropped"`
	// Queued counts the gaps the pass and the drains found.
	Queued int64 `json:"queued"`
	// PassBlobs counts the live blobs the pass walked in this run; PassEnded reports a full pass that ended.
	PassBlobs int64 `json:"passBlobs"`
	PassEnded bool  `json:"passEnded"`
	// Filled, Drained and Retired name the connections that turned active, drained and retired.
	Filled  []string `json:"filled,omitempty"`
	Drained []string `json:"drained,omitempty"`
	Retired []string `json:"retired,omitempty"`
	// CopiesDeleted counts the copies the drains deleted; JournalCopied the journal objects copied to a connection.
	CopiesDeleted int64 `json:"copiesDeleted"`
	JournalCopied int   `json:"journalCopied"`
	// Ended says why the run ended: idle (nothing more to do now), run_duration, draining or canceled.
	Ended string `json:"ended"`
}

// run is one run's state.
type run struct {
	*Reconciler
	conns *blob.Connections
	cp    *copier
	st    *state

	mu  sync.Mutex
	res Result
}

func (rn *run) add(f func(*Result)) {
	rn.mu.Lock()
	f(&rn.res)
	rn.mu.Unlock()
}

// Run is one run (R-STOR-056): it takes due queue rows and copies them, advances the pass and the drains a batch at a time, and
// ends when the queue holds no due row and neither the pass nor a drain can go on now, after storage.reconcile.run_duration, or
// when draining closes. Its position is saved after every batch.
func (r *Reconciler) Run(ctx context.Context, draining <-chan struct{}) (Result, error) {
	st, err := r.loadState(ctx)
	if err != nil {
		return Result{}, err
	}
	conns := r.store.Connections()
	rn := &run{
		Reconciler: r, conns: conns, st: st,
		cp: newCopier(int64(r.o.Storage.ObjectStore.Multipart.PartSize), r.cfg.ServerSide != "never",
			newLimiter(int64(r.cfg.BytesPerSecond), r.now), r.now, r.log),
	}
	if err := rn.prepare(ctx); err != nil {
		return rn.res, err
	}
	deadline := r.now().Add(r.cfg.RunDuration)
	rn.res.Ended = "idle"
	for {
		switch {
		case ctx.Err() != nil:
			rn.res.Ended = "canceled"
		case closed(draining):
			rn.res.Ended = "draining"
		case !r.now().Before(deadline):
			rn.res.Ended = "run_duration"
		}
		if rn.res.Ended != "idle" {
			break
		}
		progressed, err := rn.step(ctx)
		if err != nil {
			return rn.finish(ctx), err
		}
		if !progressed {
			break
		}
	}
	return rn.finish(ctx), nil
}

// step is one round: a batch of the queue, a batch of the pass, a batch of each drain. It reports whether any of them did
// something.
func (rn *run) step(ctx context.Context) (bool, error) {
	copied, err := rn.copyBatch(ctx)
	if err != nil {
		return false, err
	}
	passed, err := rn.passStep(ctx)
	if err != nil {
		return false, err
	}
	drained, err := rn.drainStep(ctx)
	if err != nil {
		return false, err
	}
	if passed || drained {
		if err := rn.saveState(ctx, rn.st); err != nil {
			return false, err
		}
	}
	return copied || passed || drained, nil
}

// prepare runs once at the start of a run: a connection with drain: true is filling no longer, and the drain's claims nobody
// finished are finished.
func (rn *run) prepare(ctx context.Context) error {
	var draining []string
	for _, c := range rn.conns.List() {
		if c.Drain() {
			draining = append(draining, c.ID())
		}
	}
	if len(draining) > 0 {
		if err := rn.db.InTx(ctx, func(tx meta.Tx) error {
			qs, err := queries(tx)
			if err != nil {
				return err
			}
			return qs.ReconcileUnfill(ctx, draining)
		}); err != nil {
			return err
		}
	}
	return rn.finishClaims(ctx)
}

// finish retires what can be retired, saves the position and returns the result. It runs on a context of its own, so that a
// run cut short still keeps what it did.
func (rn *run) finish(ctx context.Context) Result {
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := rn.retire(fctx); err != nil {
		rn.log.Warn("the retirement of drained connections failed; the next run tries again", "error", err)
	}
	if err := rn.saveState(fctx, rn.st); err != nil {
		rn.log.Warn("the position of the reconciliation could not be saved; the next run repeats part of the work", "error", err)
	}
	rn.mu.Lock()
	defer rn.mu.Unlock()
	return rn.res
}

func closed(ch <-chan struct{}) bool {
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// insertBound is how long after its object write a copy's row may be recorded (R-STOR-005, R-STOR-056): the commit deadline and
// never more than half of delete_settle, so that a collection delete that may still be in flight never meets a copy recorded
// after it.
func (r *Reconciler) insertBound() time.Duration {
	return min(r.o.Storage.Upload.CommitDeadline, r.o.Storage.GC.DeleteSettle/2)
}

// reachable is the connections the run reads from and writes to now: configured on this worker with a closed circuit.
func (rn *run) reachable() []*blob.Connection {
	var out []*blob.Connection
	for _, c := range rn.conns.List() {
		if _, rejecting := c.Rejecting(); !rejecting {
			out = append(out, c)
		}
	}
	return out
}

// targets is the connections of the worker that hold every blob: without drain: true (R-STOR-052).
func (rn *run) targets() []*blob.Connection { return rn.conns.Targets() }

func ids(cs []*blob.Connection) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID()
	}
	return out
}
