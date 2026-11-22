package blob

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// GCMode selects the candidates of a run (R-STOR-025).
type GCMode string

// The two collection tasks.
const (
	// GCIncremental takes the digests in blob_gc_queue older than the grace period.
	GCIncremental GCMode = "gc"
	// GCFull sweeps every live blob older than the grace period in keyset order.
	GCFull GCMode = "gc-full"
)

// Cursor is the keyset position of a full sweep: the last digest it finished.
// It is the job's checkpoint, so a run a lost replica returns to the queue
// continues where it stopped. The zero Cursor starts at the beginning.
type Cursor struct {
	After string `json:"after,omitempty"`
}

// GCResult is the result of a run (R-STOR-025).
type GCResult struct {
	DryRun         bool  `json:"dryRun"`
	TrashPurged    int   `json:"trashPurged"`
	Candidates     int   `json:"candidates"`
	Kept           int   `json:"kept"`
	Held           int   `json:"held"`
	Deleted        int   `json:"deleted"`
	BytesReclaimed int64 `json:"bytesReclaimed"`
	Errors         int   `json:"errors"`
	Paused         bool  `json:"paused,omitempty"`
	// CopiesReleased is the copies whose delete was never sent, because their connection is not configured on this replica or its
	// circuit was open while another connection was writable: their rows went, and the objects are their connection's orphans
	// (R-STOR-057, Q-STOR-237, Q-STOR-260).
	CopiesReleased int `json:"copiesReleased,omitempty"`
	// HoldSkipped is a run that found a valid global hold of a running backup before it claimed its candidates and ended
	// without them (R-BAK-007 "Skipping a run").
	HoldSkipped bool `json:"holdSkipped,omitempty"`
	// The session step of the run (R-STOR-011 Expiry): sessions aborted for their idle time or age (a dry run counts those it would
	// abort), and sessions whose row went, with their objects or, past the maximum age on a connection that cannot be asked, without
	// them (R-STOR-054). What no session stands for in a staging area is the orphan sweep's (R-STOR-026).
	UploadSessionsAborted int `json:"uploadSessionsAborted,omitempty"`
	UploadSessionsCleaned int `json:"uploadSessionsCleaned,omitempty"`
}

// RunOptions are the parameters of one run.
type RunOptions struct {
	Mode GCMode
	// DryRun reports what would be deleted and changes nothing.
	DryRun bool
	// Cursor is where a full sweep continues.
	Cursor Cursor
	// Checkpoint receives the cursor after every batch of a full sweep.
	Checkpoint func(Cursor)
	// Draining is closed when the replica begins its shutdown; nil never is. The settle wait ends at it with ErrHandedOver
	// (Q-STOR-217).
	Draining <-chan struct{}
}

// Collector deletes blobs that no reference uses (R-STOR-024), every copy on its own connection (R-STOR-057), and runs the orphan
// sweep of every connection (R-STOR-026, sweep.go). It is safe to run on any number of workers at once: its correctness rests on
// the row locks of the per-blob protocol, and the exclusive job key only avoids duplicate work.
type Collector struct {
	db      DB
	conns   *Connections
	storage config.Storage // the storage section: storage.orphans, storage.uploads and upload.commit_deadline for the sweep
	cfg     config.StorageGC
	log     *slog.Logger
	metrics *Metrics
	now     func() time.Time
	poll    time.Duration
	// uploads, when set, makes the run expire and clean up the upload sessions (WithUploadSessions).
	uploads *config.StorageUploads
	// installation is the id of this installation, which a connection's marker must name before the orphan sweep acts there
	// (WithInstallation, Q-STOR-262); empty compares the marker's connection only.
	installation string

	// Paused reports the deletion pause of R-HA-025. The default reads the
	// system_state key deletions_paused_at.
	Paused func(ctx context.Context) (bool, error)
	// Fault is a test hook called at named points of a run. A non-nil error ends
	// the run at once as if the process had been killed there: no hand-over, no
	// bookkeeping. It returns ErrFault wrapped. The settle wait calls it at
	// "settle:counted", after each pass, so a test can hold the run there.
	Fault func(point string) error
}

// CollectorOption configures a Collector.
type CollectorOption func(*Collector)

// WithCollectorLogger sets the logger.
func WithCollectorLogger(l *slog.Logger) CollectorOption { return func(c *Collector) { c.log = l } }

// WithCollectorMetrics sets the metrics.
func WithCollectorMetrics(m *Metrics) CollectorOption { return func(c *Collector) { c.metrics = m } }

// WithCollectorClock sets the clock whose monotonic reading bounds the delete
// of a claimed row; tests use it.
func WithCollectorClock(now func() time.Time) CollectorOption {
	return func(c *Collector) { c.now = now }
}

// WithInstallation sets the id of this installation: the orphan sweep walks a connection only while its marker <prefix>/_connection
// is absent or names this installation and that connection (R-STOR-051, Q-STOR-262).
func WithInstallation(id string) CollectorOption { return func(c *Collector) { c.installation = id } }

// WithCollectorPoll sets how often the end of a run looks for rows to settle.
func WithCollectorPoll(d time.Duration) CollectorOption { return func(c *Collector) { c.poll = d } }

// NewCollector builds a collector over the replica's connections: it deletes each copy of a blob on the copy's connection, with
// DeleteObjects where that connection's batch_delete is true (Q-STOR-222). cfg is the storage section: collection reads storage.gc,
// the orphan sweep storage.orphans, storage.uploads and storage.upload.commit_deadline.
func NewCollector(cfg config.Storage, conns *Connections, db DB, opts ...CollectorOption) *Collector {
	c := &Collector{db: db, conns: conns, storage: cfg, cfg: cfg.GC, log: slog.Default(), now: time.Now}
	for _, o := range opts {
		o(c)
	}
	if c.poll <= 0 {
		c.poll = max(10*time.Millisecond, min(250*time.Millisecond, cfg.GC.DeleteSettle/10))
	}
	c.Paused = c.defaultPaused
	return c
}

func (c *Collector) defaultPaused(ctx context.Context) (bool, error) {
	var paused bool
	err := c.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.DeletionsPaused(ctx)
		paused = len(rows) > 0
		return err
	})
	return paused, err
}

type claimResult uint8

const (
	outKept claimResult = iota
	outGone
	outHeld
	outClaimed
)

type claim struct {
	start time.Time // clock reading before the claim transaction began
	size  int64
}

type candidate struct {
	digest Digest
	seen   *time.Time
}

// run is the state of one Run, and of the deletions of one orphan sweep (sweep.go).
type run struct {
	// sessionStart is when the session step of this run began (inBudget).
	sessionStart time.Time
	c            *Collector
	opts         RunOptions
	res          GCResult
	claimed      map[Digest]claim // claimed by this run, not every copy deleted yet
	pending      []copyGone       // copies deleted or released, their rows and object_deleted_at not yet recorded
	attempted    []Digest         // the claimed blobs whose deletes ran, for step (5)
}

// copyGone is a copy that step (4) is done with: its object deleted, or released because its delete was never sent (Q-STOR-237).
type copyGone struct {
	d        Digest
	conn     string
	released bool
}

// Run performs one collection run (R-STOR-024, R-STOR-025): it finishes
// deleting rows, then processes candidates in batches, then waits up to
// storage.gc.delete_settle for the rows it deleted to become removable.
//
// Cancelling ctx is a graceful stop: the S3 calls are cancelled and awaited,
// the deletes that succeeded are recorded and the other claims are handed over
// so the next run finishes them after delete_settle instead of handover_delay.
// A returned error does not mean nothing happened; the result says what did.
func (c *Collector) Run(ctx context.Context, o RunOptions) (res GCResult, err error) {
	if o.Mode == "" {
		o.Mode = GCIncremental
	}
	r := &run{c: c, opts: o, claimed: map[Digest]claim{}}
	r.res.DryRun = o.DryRun
	defer func() {
		if !errors.Is(err, ErrFault) {
			r.finalize(ctx)
		}
		c.metrics.gc(r.res.Deleted, r.res.Kept, r.res.Held, r.res.BytesReclaimed)
		if err == nil && !r.res.Paused && !o.DryRun {
			c.metrics.gcSucceeded(time.Now())
		}
		res = r.res
	}()

	if r.res.Paused, err = r.paused(ctx); err != nil || r.res.Paused {
		return r.res, err
	}
	// The session step comes first: it frees the staging objects, and a failure of it (the store unreachable) is counted and
	// logged, never the reason the blob collection below does not run.
	if serr := r.uploadSessions(ctx); serr != nil {
		if stopsRun(ctx, serr) {
			return r.res, serr
		}
		r.res.Errors++
		c.log.Warn("the upload session step of the collection run failed", "error", serr)
	}
	if r.res.Paused { // the pause was set while the session step ran
		return r.res, nil
	}
	if !o.DryRun {
		if err = r.finishRows(ctx); err != nil || r.res.Paused {
			return r.res, err
		}
	}
	if r.res.HoldSkipped, err = r.globalHold(ctx); err != nil {
		return r.res, err
	}
	if !r.res.HoldSkipped {
		if err = r.candidates(ctx); err != nil || r.res.Paused {
			return r.res, err
		}
	}
	if !o.DryRun {
		err = r.settle(ctx)
	}
	return r.res, err
}

func (r *run) fault(point string) error {
	if r.c.Fault == nil {
		return nil
	}
	if err := r.c.Fault(point); err != nil {
		return fmt.Errorf("%w at %s: %w", ErrFault, point, err)
	}
	return nil
}

// globalHold reports a valid 'all' hold of a running backup (R-BAK-007 "Skipping a run"). It is an optimisation only: the
// per-blob check in claim still guards the race, so a hold that starts right after this read costs nothing in safety.
func (r *run) globalHold(ctx context.Context) (bool, error) {
	var held bool
	err := r.c.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		held, err = qs.BackupGlobalHoldValid(ctx)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("read the backup hold: %w", err)
	}
	if held {
		r.c.log.Info("garbage collection skips its candidates: a running backup holds every blob")
	}
	return held, nil
}

// paused reads the deletion pause before a batch of state changes.
func (r *run) paused(ctx context.Context) (bool, error) {
	p, err := r.c.Paused(ctx)
	if err != nil {
		return false, fmt.Errorf("read deletion pause: %w", err)
	}
	if p {
		r.c.log.Info("garbage collection paused: deletions are paused")
	}
	return p, nil
}

// candidates processes the candidate batches of the mode.
func (r *run) candidates(ctx context.Context) error {
	after := []byte{}
	if r.opts.Mode == GCFull && r.opts.Cursor.After != "" {
		b, err := hex.DecodeString(r.opts.Cursor.After)
		if err != nil {
			return fmt.Errorf("garbage collection cursor: %w", err)
		}
		after = b
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch, err := r.nextBatch(ctx, after)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		if !r.opts.DryRun {
			if p, err := r.paused(ctx); err != nil || p {
				r.res.Paused = p
				return err
			}
		}
		if err := r.process(ctx, batch); err != nil {
			return err
		}
		after = batch[len(batch)-1].digest.Bytes()
		if r.opts.Mode == GCFull && r.opts.Checkpoint != nil {
			r.opts.Checkpoint(Cursor{After: hex.EncodeToString(after)})
		}
	}
}

// nextBatch reads the next candidates after a digest.
func (r *run) nextBatch(ctx context.Context, after []byte) ([]candidate, error) {
	grace := r.c.cfg.GracePeriod.Seconds()
	limit := int32(min(r.c.cfg.BatchSize, math.MaxInt32)) //nolint:gosec // bounded by the min
	var out []candidate
	err := r.c.db.InTx(ctx, func(tx meta.Tx) error {
		out = out[:0]
		if r.opts.Mode == GCFull {
			refs, err := postgres.LoadBlobRefs(ctx, tx)
			if err != nil {
				return err
			}
			cs, err := refs.FullSweep(ctx, tx, grace, after, limit)
			if err != nil {
				return err
			}
			for _, c := range cs {
				var d Digest
				copy(d[:], c.SHA256)
				out = append(out, candidate{digest: d, seen: c.EnqueuedAt})
			}
			return nil
		}
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.BlobQueueCandidates(ctx, q.BlobQueueCandidatesParams{GraceSecs: grace, After: after, Batch: limit})
		if err != nil {
			return err
		}
		for _, row := range rows {
			var d Digest
			copy(d[:], row.Sha256)
			seen := row.EnqueuedAt
			out = append(out, candidate{digest: d, seen: &seen})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read gc candidates: %w", err)
	}
	return out, nil
}

// loadRefs reads the referencing columns of blobs from the catalog. A batch
// reads them once and uses them for all its candidates (R-STOR-024 rule 3).
func (r *run) loadRefs(ctx context.Context) (*postgres.BlobRefs, error) {
	var refs *postgres.BlobRefs
	err := r.c.db.InTx(ctx, func(tx meta.Tx) error {
		var err error
		refs, err = postgres.LoadBlobRefs(ctx, tx)
		return err
	})
	return refs, err
}

// process handles one batch: dry-run accounting, or claim each candidate and
// delete the objects of the claimed ones.
func (r *run) process(ctx context.Context, batch []candidate) error {
	r.res.Candidates += len(batch)
	refs, err := r.loadRefs(ctx)
	if err != nil {
		return fmt.Errorf("read the tables that reference blobs: %w", err)
	}
	if r.opts.DryRun {
		return r.dryRun(ctx, batch, refs)
	}
	var claimed []Digest
	for _, cand := range batch {
		if err := ctx.Err(); err != nil {
			return err
		}
		out, size, start, err := r.claim(ctx, cand, refs)
		if err != nil {
			return err
		}
		switch out {
		case outClaimed:
			r.claimed[cand.digest] = claim{start: start, size: size}
			claimed = append(claimed, cand.digest)
		case outHeld:
			r.res.Held++
		default:
			r.res.Kept++
		}
	}
	return r.deleteObjects(ctx, claimed)
}

func (r *run) dryRun(ctx context.Context, batch []candidate, refs *postgres.BlobRefs) error {
	for _, cand := range batch {
		var ref, live, held bool
		var size int64
		err := r.c.db.InTx(ctx, func(tx meta.Tx) error {
			qs, err := queries(tx)
			if err != nil {
				return err
			}
			rows, err := qs.BlobStateSize(ctx, cand.digest.Bytes())
			if err != nil {
				return err
			}
			live = len(rows) > 0 && rows[0].State == "live"
			if !live {
				return nil
			}
			size = rows[0].Size
			if ref, err = refs.Referenced(ctx, tx, cand.digest.Bytes()); err != nil || ref {
				return err
			}
			held, err = qs.BackupBlobHeld(ctx, cand.digest.Bytes())
			return err
		})
		if err != nil {
			return err
		}
		if live && !ref && held {
			r.res.Held++
		} else if live && !ref {
			r.res.Deleted++
			r.res.BytesReclaimed += size
		} else {
			r.res.Kept++
		}
	}
	return nil
}

// claim is steps (1) to (3) for one candidate in one transaction.
func (r *run) claim(ctx context.Context, cand candidate, refs *postgres.BlobRefs) (out claimResult, size int64, start time.Time, err error) {
	if err := r.fault("claim:before"); err != nil {
		return 0, 0, time.Time{}, err
	}
	// The reading is no later than the transaction's start, which step (3)
	// stores as state_changed_at.
	start = r.c.now()
	err = r.c.db.InTx(ctx, func(tx meta.Tx) error {
		out = outKept
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.LockBlobUpdate(ctx, cand.digest.Bytes())
		if err != nil {
			return err
		}
		if err := r.fault("claim:locked"); err != nil {
			return err
		}
		dequeue := func() error {
			if cand.seen == nil {
				return nil // a full sweep found the digest in no queue
			}
			return qs.DequeueBlob(ctx, q.DequeueBlobParams{Sha256: cand.digest.Bytes(), EnqueuedAt: *cand.seen})
		}
		if len(rows) == 0 || rows[0].State != "live" {
			out = outGone
			return dequeue()
		}
		size = rows[0].Size
		ref, err := refs.Referenced(ctx, tx, cand.digest.Bytes())
		if err != nil {
			return err
		}
		if err := r.fault("claim:checked"); err != nil {
			return err
		}
		if ref {
			return dequeue()
		}
		// Step (2b): a separate statement after the reference check (R-STOR-024, R-BAK-007). A held blob stays live and keeps
		// its queue entry.
		held, err := qs.BackupBlobHeld(ctx, cand.digest.Bytes())
		if err != nil {
			return err
		}
		if err := r.fault("claim:held-checked"); err != nil {
			return err
		}
		if held {
			out = outHeld
			return nil
		}
		if err := qs.ClaimBlob(ctx, cand.digest.Bytes()); err != nil {
			return err
		}
		if err := qs.ClaimBlobCopies(ctx, cand.digest.Bytes()); err != nil {
			return err
		}
		out = outClaimed
		return dequeue()
	})
	if err != nil {
		return 0, 0, start, err
	}
	if err := r.fault("claim:committed"); err != nil {
		return 0, 0, start, err
	}
	return out, size, start, nil
}

// deleteObjects is steps (4) and (5) for the digests claimed in this batch, each copy on its connection (R-STOR-057).
// The hand-over rule: a row this run claimed is deleted only while less than
// handover_delay - delete_settle has passed on the run's own clock since the
// reading before its claim, and the calls get that instant as their deadline.
func (r *run) deleteObjects(ctx context.Context, digests []Digest) error {
	if len(digests) == 0 {
		return nil
	}
	earliest := r.claimed[digests[0]].start
	for _, d := range digests {
		if s := r.claimed[d].start; s.Before(earliest) {
			earliest = s
		}
	}
	deadline := earliest.Add(r.c.cfg.HandoverDelay - r.c.cfg.DeleteSettle)
	if !r.c.now().Before(deadline) {
		r.c.log.Warn("garbage collection claimed rows too long ago to delete them; a later run finishes them", "rows", len(digests))
		return nil
	}
	if p, err := r.paused(ctx); err != nil || p {
		r.res.Paused = p
		return err
	}
	if err := r.fault("delete:before"); err != nil {
		return err
	}
	copies, err := r.c.collectCopies(ctx, digests)
	if err != nil {
		return fmt.Errorf("read the copies of the claimed blobs: %w", err)
	}
	r.attempted = append(r.attempted, digests...)
	dctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	gone, err := r.c.removeCopies(dctx, copies, &r.res)
	r.pending = append(r.pending, gone...)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil // the hand-over deadline: the unfinished rows are handed over
		}
		return err
	}
	if err := r.fault("delete:after"); err != nil {
		return err
	}
	return r.record(ctx)
}

// record ends step (4) and records step (5) for the copies that are done: their rows go, and a blob without a copy row left gets
// object_deleted_at. A blob with a copy whose delete had no known outcome keeps its claim; finalize hands it over.
func (r *run) record(ctx context.Context) error {
	if len(r.pending) == 0 && len(r.attempted) == 0 {
		return nil
	}
	recorded, released, bytes, err := r.c.recordDeleted(ctx, r.pending, r.attempted)
	if err != nil {
		return err
	}
	r.res.Deleted += len(recorded)
	r.res.BytesReclaimed += bytes
	r.res.CopiesReleased += released
	for _, d := range recorded {
		delete(r.claimed, d)
	}
	r.pending, r.attempted = r.pending[:0], r.attempted[:0]
	return r.fault("record:after")
}

// recordDeleted deletes the rows of the copies that are done and records step (5) for the blobs among attempted without a copy
// row left. It returns those blobs, the number of copies it released and the bytes reclaimed.
func (c *Collector) recordDeleted(ctx context.Context, gone []copyGone, attempted []Digest) (recorded []Digest, released int, total int64, err error) {
	byConn := map[string]int{}
	err = c.db.InTx(ctx, func(tx meta.Tx) error {
		recorded, total = recorded[:0], 0
		clear(byConn)
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		if err := deleteGoneCopies(ctx, qs, gone, byConn); err != nil {
			return err
		}
		if len(attempted) == 0 {
			return nil
		}
		rows, err := qs.RecordBlobsDeleted(ctx, digestBytes(attempted))
		if err != nil {
			return err
		}
		for _, row := range rows {
			var d Digest
			copy(d[:], row.Sha256)
			recorded = append(recorded, d)
			total += row.Size
		}
		return nil
	})
	if err != nil {
		return nil, 0, 0, err
	}
	for conn, n := range byConn {
		released += n
		c.metrics.copiesReleased(conn, n)
	}
	return recorded, released, total, nil
}

// deleteGoneCopies deletes the copy rows of gone in one statement and counts the released ones it deleted per connection.
func deleteGoneCopies(ctx context.Context, qs *q.Queries, gone []copyGone, released map[string]int) error {
	if len(gone) == 0 {
		return nil
	}
	digests, conns := make([][]byte, len(gone)), make([]string, len(gone))
	wasReleased := map[string]bool{}
	for i, g := range gone {
		digests[i], conns[i] = g.d.Bytes(), g.conn
		if g.released {
			wasReleased[string(g.d[:])+"/"+g.conn] = true
		}
	}
	rows, err := qs.DeleteCollectedCopies(ctx, q.DeleteCollectedCopiesParams{Digests: digests, Connections: conns})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if wasReleased[string(row.Sha256)+"/"+row.ConnectionID] {
			released[row.ConnectionID]++
		}
	}
	return nil
}

func digestBytes(ds []Digest) [][]byte {
	out := make([][]byte, len(ds))
	for i := range ds {
		out[i] = ds[i].Bytes()
	}
	return out
}

// claimedCopy is a copy row of a claimed blob that step (4) deletes.
type claimedCopy struct {
	d    Digest
	conn string
	key  string
}

// collectCopies reads the copy rows of claimed blobs (step 4) in every state: a copy a drain claimed before collection claimed
// the blob is deleted as well, which is harmless (R-STOR-056).
func (c *Collector) collectCopies(ctx context.Context, digests []Digest) ([]claimedCopy, error) {
	var out []claimedCopy
	err := c.db.InTx(ctx, func(tx meta.Tx) error {
		out = out[:0]
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.CollectCopies(ctx, digestBytes(digests))
		if err != nil {
			return err
		}
		for _, row := range rows {
			var d Digest
			copy(d[:], row.Sha256)
			out = append(out, claimedCopy{d: d, conn: row.ConnectionID, key: row.ObjectKey})
		}
		return nil
	})
	return out, err
}

// removeCopies sends the deletes of step (4), one connection's copies together and the connections at once, and returns the
// copies it is done with: a delete that succeeded (a key that is gone counts), and a copy released without a request because its
// connection is not configured on this replica or its circuit is open while another connection is writable (Q-STOR-237,
// Q-STOR-260). A delete whose outcome is unknown keeps its row and is counted in res.Errors; a later run repeats it under the
// hand-over rules. An error that stops the run (the context, every circuit open) is returned.
func (c *Collector) removeCopies(ctx context.Context, copies []claimedCopy, res *GCResult) ([]copyGone, error) {
	byConn := map[string][]claimedCopy{}
	var ids []string
	for _, cp := range copies {
		if _, ok := byConn[cp.conn]; !ok {
			ids = append(ids, cp.conn)
		}
		byConn[cp.conn] = append(byConn[cp.conn], cp)
	}
	slices.Sort(ids)
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		gone     []copyGone
		firstErr error
		down     error // no connection is writable: the run stops its object-store phase, as with one store
	)
	for _, id := range ids {
		group := byConn[id]
		conn, ok := c.conns.Get(id)
		if !ok {
			mu.Lock()
			gone = append(gone, released(group)...)
			mu.Unlock()
			continue
		}
		if after, rejecting := conn.Rejecting(); rejecting {
			if !c.releasable(conn) {
				down = &circuitOpenError{retry: after}
				continue
			}
			mu.Lock()
			gone = append(gone, released(group)...)
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(conn *Connection, group []claimedCopy) {
			defer wg.Done()
			out := c.deleteOn(ctx, conn, group)
			stop := out.stop
			if errors.Is(stop, ErrCircuitOpen) && ctx.Err() == nil && c.releasable(conn) {
				// the circuit opened before these were sent: no request for them can arrive late
				for _, g := range out.unsent {
					g.released = true
					out.done = append(out.done, g)
				}
				stop = nil
			}
			mu.Lock()
			defer mu.Unlock()
			gone = append(gone, out.done...)
			res.Errors += out.unknown
			if stop != nil && firstErr == nil {
				firstErr = stop
			}
		}(conn, group)
	}
	wg.Wait()
	if firstErr == nil {
		firstErr = down
	}
	return gone, firstErr
}

func released(group []claimedCopy) []copyGone {
	out := make([]copyGone, len(group))
	for i, cp := range group {
		out[i] = copyGone{d: cp.d, conn: cp.conn, released: true}
	}
	return out
}

// releasable reports whether a copy on conn, whose circuit is open, may be released (Q-STOR-260): only while another connection of
// this replica is writable. With every circuit open, as with the one connection of a plain installation when it is down, no
// uploader could store the content anyway, and a release would only leave deleted content for the sweep; the run stops instead.
func (c *Collector) releasable(conn *Connection) bool {
	for _, w := range c.conns.Writable() {
		if w != conn {
			return true
		}
	}
	return false
}

// deleted is what the deletes on one connection came to: the copies done (deleted, or gone already), the copies whose delete was
// never sent because the circuit refused it or the run stopped first, the number of deletes whose outcome is unknown or that the store
// refused (their rows stay), and the error that stopped the deletes (the context, an open circuit).
type deleted struct {
	done, unsent []copyGone
	unknown      int
	stop         error
}

// deleteOn deletes the objects of one connection's copies: DeleteObjects in chunks of 1,000 where the connection's batch_delete is
// true, else delete_concurrency parallel DeleteObject.
func (c *Collector) deleteOn(ctx context.Context, conn *Connection, group []claimedCopy) deleted {
	if conn.BatchDelete() {
		return c.deleteBatches(ctx, conn, group)
	}
	var (
		out deleted
		mu  sync.Mutex
		wg  sync.WaitGroup
	)
	sem := make(chan struct{}, max(1, c.cfg.DeleteConcurrency))
	for _, cp := range group {
		g := copyGone{d: cp.d, conn: cp.conn}
		mu.Lock()
		stopped := out.stop != nil || ctx.Err() != nil
		if stopped {
			out.unsent = append(out.unsent, g)
			if out.stop == nil {
				out.stop = ctx.Err()
			}
		}
		mu.Unlock()
		if stopped {
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(cp claimedCopy, g copyGone) {
			defer func() { <-sem; wg.Done() }()
			derr := conn.Objects().Delete(ctx, cp.key)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case derr == nil || errors.Is(derr, ErrNotFound):
				out.done = append(out.done, g)
			case errors.Is(derr, ErrCircuitOpen):
				out.unsent = append(out.unsent, g) // the guard refused it before a request left
				if out.stop == nil {
					out.stop = derr
				}
			case stopsRun(ctx, derr):
				out.unknown++
				if out.stop == nil {
					out.stop = derr
				}
			default:
				out.unknown++
				c.log.Warn("delete failed", "connection", conn.ID(), "sha256", cp.d.Hex(), "error", derr)
			}
		}(cp, g)
	}
	wg.Wait()
	return out
}

// deleteBatches is deleteOn with DeleteObjects.
func (c *Collector) deleteBatches(ctx context.Context, conn *Connection, group []claimedCopy) deleted {
	var out deleted
	for len(group) > 0 {
		n := min(len(group), 1000)
		chunk := group[:n]
		group = group[n:]
		keys := make([]string, n)
		for i, cp := range chunk {
			keys[i] = cp.key
		}
		bad, derr := conn.Objects().DeleteMany(ctx, keys)
		switch {
		case derr == nil:
		case errors.Is(derr, ErrCircuitOpen):
			out.unsent = append(out.unsent, notGone(chunk)...)
			out.unsent = append(out.unsent, notGone(group)...)
			out.stop = derr
			return out
		case stopsRun(ctx, derr):
			out.unknown += n
			out.unsent = append(out.unsent, notGone(group)...)
			out.stop = derr
			return out
		default:
			out.unknown += n
			c.log.Warn("batch delete failed", "connection", conn.ID(), "keys", n, "error", derr)
			continue
		}
		refused := map[string]bool{}
		for _, f := range bad {
			refused[f.Key] = true
		}
		for i, cp := range chunk {
			if refused[keys[i]] {
				out.unknown++
				c.log.Warn("the store did not delete an object", "connection", conn.ID(), "key", keys[i])
				continue
			}
			out.done = append(out.done, copyGone{d: cp.d, conn: cp.conn})
		}
	}
	return out
}

func notGone(group []claimedCopy) []copyGone {
	out := make([]copyGone, len(group))
	for i, cp := range group {
		out[i] = copyGone{d: cp.d, conn: cp.conn}
	}
	return out
}

// stopsRun reports an error that ends the run instead of one candidate.
func stopsRun(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, ErrCircuitOpen) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// finishRows finishes the rows earlier runs left: step (6) for rows whose delete
// settled, steps (4) to (6) for rows nobody finished within handover_delay.
func (r *run) finishRows(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := r.finishEligible(ctx)
		if err != nil || n == 0 {
			return err
		}
	}
}

// finishEligible does one pass over eligible rows and returns how many it
// changed. A row whose delete settled is dropped (step 6). A row nobody finished
// within handover_delay is taken over with a compare-and-set claim, so that two
// runs that read the same row cannot both delete its object, and is then
// finished under the same hand-over deadline as a row this run claimed itself.
func (r *run) finishEligible(ctx context.Context) (int, error) {
	if p, err := r.paused(ctx); err != nil || p {
		r.res.Paused = p
		return 0, err
	}
	type row struct {
		d       Digest
		size    int64
		deleted bool
	}
	var rows []row
	err := r.c.db.InTx(ctx, func(tx meta.Tx) error {
		rows = rows[:0]
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rs, err := qs.EligibleBlobs(ctx, q.EligibleBlobsParams{
			HandoverSecs: r.c.cfg.HandoverDelay.Seconds(), SettleSecs: r.c.cfg.DeleteSettle.Seconds(),
			Batch: int32(min(r.c.cfg.BatchSize, math.MaxInt32)), //nolint:gosec // bounded by the min
		})
		if err != nil {
			return err
		}
		for _, x := range rs {
			e := row{size: x.Size, deleted: x.ObjectDeleted}
			copy(e.d[:], x.Sha256)
			rows = append(rows, e)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	var toDelete, toDrop []Digest
	for _, x := range rows {
		if _, mine := r.claimed[x.d]; mine {
			continue
		}
		if x.deleted {
			toDrop = append(toDrop, x.d)
		} else {
			toDelete = append(toDelete, x.d)
		}
	}
	changed := 0
	if len(toDelete) > 0 {
		if err := r.fault("finish:delete"); err != nil {
			return changed, err
		}
		taken, err := r.takeOver(ctx, toDelete)
		if err != nil {
			return changed, err
		}
		if err := r.deleteObjects(ctx, taken); err != nil {
			return changed, err
		}
		changed += len(taken)
	}
	if len(toDrop) > 0 {
		if err := r.drop(ctx, toDrop); err != nil {
			return changed, err
		}
		changed += len(toDrop)
	}
	return changed, nil
}

// takeOver claims rows another run left, with a compare-and-set that renews
// their state_changed_at, and registers the ones this run won as its own claims.
// The clock reading before the transaction is the start of the hand-over rule,
// as for a claim.
func (r *run) takeOver(ctx context.Context, digests []Digest) ([]Digest, error) {
	start := r.c.now()
	var taken []Digest
	err := r.c.db.InTx(ctx, func(tx meta.Tx) error {
		taken = taken[:0]
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.TakeOverBlobs(ctx, q.TakeOverBlobsParams{
			Digests: digestBytes(digests), HandoverSecs: r.c.cfg.HandoverDelay.Seconds(),
		})
		if err != nil {
			return err
		}
		for _, x := range rows {
			var d Digest
			copy(d[:], x.Sha256)
			taken = append(taken, d)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Sizes are only needed for the result, which record() takes from the
	// database; a taken row is registered with the reading before the claim.
	for _, d := range taken {
		r.claimed[d] = claim{start: start}
	}
	return taken, nil
}

// drop is step (6).
func (r *run) drop(ctx context.Context, digests []Digest) error {
	if err := r.fault("drop:before"); err != nil {
		return err
	}
	return r.c.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		return qs.DropBlobRows(ctx, q.DropBlobRowsParams{Digests: digestBytes(digests), SettleSecs: r.c.cfg.DeleteSettle.Seconds()})
	})
}

// settle waits up to delete_settle so that the rows this run deleted (and the
// rows handed over to it) are removed before it ends: an upload waiting for such
// a row is not left until the next scheduled run. The wait ends with a pass that
// started after its deadline, so a pass delayed past the deadline cannot be the
// last one (Q-STOR-216). A shutdown of the replica ends the wait with
// ErrHandedOver (Q-STOR-217).
func (r *run) settle(ctx context.Context) error {
	// A row this run deleted is removable delete_settle after its delete, which
	// lies at most that far before now; two polls of slack cover the last one.
	deadline := r.c.now().Add(r.c.cfg.DeleteSettle + 2*r.c.poll)
	for {
		// Read before the pass: when the deadline had passed, the pass below ran after it, so every row this run deleted has settled
		// in it. A stall between the pass and the check must not end the wait on a pass from before the deadline.
		expired := !r.c.now().Before(deadline)
		if _, err := r.finishEligible(ctx); err != nil || r.res.Paused {
			return err
		}
		var soon int64
		err := r.c.db.InTx(ctx, func(tx meta.Tx) error {
			qs, err := queries(tx)
			if err != nil {
				return err
			}
			soon, err = qs.CountDeletingSoon(ctx, (r.c.cfg.HandoverDelay - r.c.cfg.DeleteSettle).Seconds())
			return err
		})
		if err != nil {
			return err
		}
		if err := r.fault("settle:counted"); err != nil {
			return err
		}
		if soon == 0 || expired {
			return nil
		}
		t := time.NewTimer(r.c.poll)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-r.opts.Draining:
			// The rows deleted by this run are removable delete_settle after their delete, whichever run finishes them; a
			// stopping replica has nothing to add by waiting, and its job resumes on another worker at once (Q-STOR-217).
			t.Stop()
			return ErrHandedOver
		case <-t.C:
		}
	}
}

// finalize records the deletes that succeeded and hands the other claims over.
// It uses a context of its own: the run's may be the thing that ended.
func (r *run) finalize(ctx context.Context) {
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := r.record(fctx); err != nil {
		r.c.log.Warn("could not record deleted objects; the next run repeats the delete", "error", err)
	}
	if len(r.claimed) > 0 {
		left := make([]Digest, 0, len(r.claimed))
		for d := range r.claimed {
			left = append(left, d)
		}
		back := (r.c.cfg.HandoverDelay - r.c.cfg.DeleteSettle).Seconds()
		err := r.c.db.InTx(fctx, func(tx meta.Tx) error {
			qs, err := queries(tx)
			if err != nil {
				return err
			}
			return qs.HandOverBlobs(fctx, q.HandOverBlobsParams{Digests: digestBytes(left), BackSecs: back})
		})
		if err != nil {
			r.c.log.Warn("could not hand claimed rows over; they wait for handover_delay", "rows", len(left), "error", err)
		}
	}
	var deleting int64
	_ = r.c.db.InTx(fctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		deleting, err = qs.CountDeletingBlobs(fctx)
		return err
	})
	r.c.metrics.setDeleting(int(deleting))
}
