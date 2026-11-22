package reconcile

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The queue step of a run (R-STOR-056): the due rows of blob_copy_queue for the connections the worker reaches, oldest first,
// each copied from a present copy on another reachable connection and recorded within the insert bound.

// outcome is how the recording of one written copy ended.
type outcome uint8

const (
	recorded outcome = iota // the copy row is present and the queue row gone
	gone                    // the blob is not live any more: the object is left to the target's orphan sweep
	tooLate                 // the insert bound passed between the write and the record: write again
	claimed                 // the target's copy row is claimed for deletion by a drain
)

// copyBatch takes one batch of due queue rows and copies them, storage.reconcile.concurrency at a time. It reports whether it
// took a row; an error is the database's.
func (rn *run) copyBatch(ctx context.Context) (bool, error) {
	reach := rn.reachable()
	if len(reach) == 0 {
		return false, nil
	}
	slots := max(1, rn.cfg.Concurrency)
	var rows []q.ReconcileDueCopiesRow
	if err := rn.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err = qs.ReconcileDueCopies(ctx, q.ReconcileDueCopiesParams{Connections: ids(reach), PageSize: int32(slots * copyBatchPerSlot)}) //nolint:gosec // small
		return err
	}); err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	sem := make(chan struct{}, slots)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	for _, row := range rows {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := rn.copyRow(ctx, row.ConnectionID, digestOf(row.Sha256)); err != nil && ctx.Err() == nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return true, firstErr
}

// copyRow is the copy of one queue row (connection conn, blob d): a row that needs no copy is deleted, otherwise the blob is
// copied from the first source that works and recorded, or the row's failure is counted with its back-off.
func (rn *run) copyRow(ctx context.Context, conn string, d blob.Digest) error {
	dst, ok := rn.conns.Get(conn)
	if !ok {
		return nil // the batch took only configured connections
	}
	size, live, copies, err := rn.readBlob(ctx, d)
	if err != nil {
		return err
	}
	switch own := copyState(copies, conn); {
	case !live, dst.Drain(), own == blob.CopyPresent:
		return rn.drop(ctx, conn, d)
	case own == blob.CopyDeleting:
		return rn.fail(ctx, conn, d, ReasonClaimed, errors.New("the copy row is claimed for deletion by a drain"))
	case !anyPresent(copies):
		// no connection holds the blob: the commit's repair or the recheck of a missing blob brings a copy back, and the pass
		// queues the gap again once one is present (Q-STOR-258)
		return rn.drop(ctx, conn, d)
	}
	srcs, reason := rn.sources(copies, dst)
	if len(srcs) == 0 {
		return rn.fail(ctx, conn, d, reason, errors.New("no other connection that the worker reaches holds a present copy"))
	}
	return rn.copyFromSources(ctx, target{digest: d, size: size}, srcs, dst)
}

// copyFromSources tries the sources in order until one copy is recorded.
func (rn *run) copyFromSources(ctx context.Context, b target, srcs []blob.Copy, dst *blob.Connection) error {
	var (
		lastErr             error
		damaged, unanswered int
	)
	for _, src := range srcs {
		srcConn, _ := rn.conns.Get(src.Connection)
		out, m, err := rn.copyAndRecord(ctx, b, src, srcConn, dst)
		var se *sourceError
		var te *targetError
		switch {
		case err == nil:
			return rn.done(ctx, out, b, m, dst)
		case errors.Is(err, blob.ErrCircuitOpen) && errors.As(err, &te):
			// the target's circuit opened since the batch was taken (another copy's failure, a probe that is out): the row waits
			// without spending an attempt, as every row of a connection whose circuit is open does
			return nil
		case errors.As(err, &te):
			return rn.fail(ctx, dst.ID(), b.digest, te.reason(), err)
		case errors.As(err, &se):
			lastErr = err
			if se.status == "" {
				unanswered++
				continue
			}
			damaged++
			rn.log.Warn("the source of a copy is damaged; it is marked and the next source is tried", "sha256", b.digest.Hex(),
				"source", src.Connection, "connection", dst.ID(), "status", se.status, "error", se.err)
			rn.store.MarkCopyDamaged(ctx, b.digest, src, se.status)
		default:
			return err
		}
	}
	reason := ReasonUnavailable
	if damaged > 0 && unanswered == 0 {
		reason = ReasonSourceDamaged
	}
	return rn.fail(ctx, dst.ID(), b.digest, reason, lastErr)
}

// done counts what the recording of a written copy ended with.
func (rn *run) done(ctx context.Context, out outcome, b target, m made, dst *blob.Connection) error {
	switch out {
	case recorded:
		rn.o.Metrics.copy(dst.ID(), m.mode, b.size)
		rn.add(func(r *Result) {
			r.Copied++
			r.Bytes += m.bytes
			if m.mode == modeServer {
				r.ServerSide++
			} else {
				r.Streamed++
			}
		})
		return nil
	case gone:
		rn.add(func(r *Result) { r.Dropped++ })
		return nil
	case claimed:
		return rn.fail(ctx, dst.ID(), b.digest, ReasonClaimed, errors.New("the copy row was claimed for deletion by a drain"))
	}
	return rn.fail(ctx, dst.ID(), b.digest, ReasonUnavailable, errors.New("the insert bound passed after every write of the copy"))
}

// copyAndRecord writes the copy and records it; a record that the insert bound refuses writes the object again
// (R-STOR-056), at most recordAttempts times.
func (rn *run) copyAndRecord(ctx context.Context, b target, src blob.Copy, srcConn, dst *blob.Connection) (outcome, made, error) {
	for range recordAttempts {
		m, err := rn.cp.copyBlob(ctx, b, src, srcConn, dst)
		if err != nil {
			return 0, m, err
		}
		if rn.o.AfterWrite != nil {
			rn.o.AfterWrite(dst.ID(), b.digest)
		}
		out, err := rn.record(ctx, dst, b, m, src)
		if err != nil || out != tooLate {
			return out, m, err
		}
		rn.log.Info("the insert bound passed between a copy's write and its record; the object is written again",
			"sha256", b.digest.Hex(), "connection", dst.ID())
	}
	return tooLate, made{}, nil
}

// record records a written copy in one transaction (R-STOR-056, Q-STOR-256): under the blob's lock, which must find it live,
// the target's copy row becomes present (inserted, or a missing, corrupt or present row overwritten), the queue row goes, the
// source of a streamed copy is stamped verified (a read to its end matched the digest), and the blob's content state follows.
// A record later than the insert bound after the write is refused, and the caller writes again.
func (rn *run) record(ctx context.Context, dst *blob.Connection, b target, m made, src blob.Copy) (outcome, error) {
	var out outcome
	var healed int64
	err := rn.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		locked, err := qs.LockBlobForMark(ctx, b.digest.Bytes())
		if err != nil {
			return err
		}
		if len(locked) == 0 || locked[0] != "live" {
			out = gone
			return qs.ReconcileDeleteQueued(ctx, q.ReconcileDeleteQueuedParams{ConnectionID: dst.ID(), Sha256: b.digest.Bytes()})
		}
		if rn.now().Sub(m.at) > rn.insertBound() {
			out = tooLate
			return nil
		}
		copies, err := qs.BlobCopies(ctx, b.digest.Bytes())
		if err != nil {
			return err
		}
		for _, c := range copies {
			if c.ConnectionID == dst.ID() && c.State == blob.CopyDeleting {
				out = claimed
				return nil
			}
		}
		if err := qs.PutBlobCopy(ctx, q.PutBlobCopyParams{
			Sha256: b.digest.Bytes(), ConnectionID: dst.ID(), ObjectKey: dst.ObjectKey(b.digest), StoredSize: b.size, Verified: m.verified,
		}); err != nil {
			return err
		}
		if err := qs.ReconcileDeleteQueued(ctx, q.ReconcileDeleteQueuedParams{ConnectionID: dst.ID(), Sha256: b.digest.Bytes()}); err != nil {
			return err
		}
		if m.mode == modeStream {
			if err := qs.StampBlobCopyVerified(ctx, q.StampBlobCopyVerifiedParams{Sha256: b.digest.Bytes(), ConnectionID: src.Connection}); err != nil {
				return err
			}
		}
		healed, err = qs.SetBlobContentStatus(ctx, q.SetBlobContentStatusParams{Sha256: b.digest.Bytes(), Status: blob.StatusOK})
		out = recorded
		return err
	})
	if err == nil && healed > 0 {
		rn.log.Info("a blob has a present copy again", "sha256", b.digest.Hex(), "connection", dst.ID())
	}
	return out, err
}

// drop deletes a queue row that needs no copy.
func (rn *run) drop(ctx context.Context, conn string, d blob.Digest) error {
	rn.add(func(r *Result) { r.Dropped++ })
	return rn.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		return qs.ReconcileDeleteQueued(ctx, q.ReconcileDeleteQueuedParams{ConnectionID: conn, Sha256: d.Bytes()})
	})
}

// fail counts a failed copy (R-STOR-056): attempts, the next attempt after min(2^attempts minutes, retry_max_backoff), the
// reason, and quader_storage_reconcile_failures_total. A run cut short counts nothing: its row stays as it was.
func (rn *run) fail(ctx context.Context, conn string, d blob.Digest, reason string, cause error) error {
	if cut := ctx.Err(); cut != nil {
		return cut
	}
	msg := reason
	if cause != nil {
		msg += ": " + cause.Error()
	}
	if len(msg) > lastErrorBytes {
		msg = msg[:lastErrorBytes]
	}
	rn.o.Metrics.failure(conn, reason)
	rn.add(func(r *Result) { r.Failed++ })
	rn.log.Warn("a copy of the reconciliation failed; it is tried again after its back-off", "sha256", d.Hex(), "connection", conn,
		"reason", reason, "error", cause)
	return rn.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		return qs.ReconcileQueueFailed(ctx, q.ReconcileQueueFailedParams{
			ConnectionID: conn, Sha256: d.Bytes(), MaxBackoffSeconds: rn.cfg.RetryMaxBackoff.Seconds(), LastError: msg,
		})
	})
}

// readBlob reads a blob's size, whether it is live, and its copies.
func (rn *run) readBlob(ctx context.Context, d blob.Digest) (size int64, live bool, copies []blob.Copy, err error) {
	err = rn.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.BlobStateSize(ctx, d.Bytes())
		if err != nil || len(rows) == 0 {
			return err
		}
		size, live = rows[0].Size, rows[0].State == "live"
		cs, err := qs.BlobCopies(ctx, d.Bytes())
		copies = copiesOf(cs)
		return err
	})
	return size, live, copies, err
}

// sources is the copies a copy onto dst may be made from (R-STOR-056): present, on another connection the worker reaches,
// verified before not verified, then by list order. With none, reason says whether a present copy lies on a connection whose
// circuit is open on the worker (unavailable) or only on connections the worker does not configure (no_source).
func (rn *run) sources(copies []blob.Copy, dst *blob.Connection) ([]blob.Copy, string) {
	var out []blob.Copy
	reason := ReasonNoSource
	for _, cp := range copies {
		if cp.Connection == dst.ID() || cp.State != blob.CopyPresent || cp.Retired {
			continue
		}
		c, ok := rn.conns.Get(cp.Connection)
		if !ok {
			continue
		}
		if _, rejecting := c.Rejecting(); rejecting {
			reason = ReasonUnavailable
			continue
		}
		out = append(out, cp)
	}
	slices.SortStableFunc(out, func(a, b blob.Copy) int {
		if av, bv := !a.VerifiedAt.IsZero(), !b.VerifiedAt.IsZero(); av != bv {
			if av {
				return -1
			}
			return 1
		}
		ca, _ := rn.conns.Get(a.Connection)
		cb, _ := rn.conns.Get(b.Connection)
		return ca.Position() - cb.Position()
	})
	return out, reason
}

// copyState is the state of the copy row of conn among copies, empty when there is none.
func copyState(copies []blob.Copy, conn string) string {
	for _, c := range copies {
		if c.Connection == conn {
			return c.State
		}
	}
	return ""
}

// anyPresent reports whether a copy on a connection that is not retired is present: the blob has a source somewhere.
func anyPresent(copies []blob.Copy) bool {
	for _, c := range copies {
		if c.State == blob.CopyPresent && !c.Retired {
			return true
		}
	}
	return false
}

func copiesOf(rows []q.BlobCopiesRow) []blob.Copy {
	out := make([]blob.Copy, 0, len(rows))
	for _, r := range rows {
		c := blob.Copy{
			Connection: r.ConnectionID, ObjectKey: r.ObjectKey, StoredSize: r.StoredSize, StorageKeyID: r.StorageKeyID, State: r.State,
			Retired: r.Retired,
		}
		// a NULL timestamp is the zero time (this package imports no driver type, R-ARCH-002)
		if r.VerifiedAt.Valid {
			c.VerifiedAt = r.VerifiedAt.Time
		}
		if r.CheckedAt.Valid {
			c.CheckedAt = r.CheckedAt.Time
		}
		out = append(out, c)
	}
	return out
}

func digestOf(b []byte) blob.Digest {
	var d blob.Digest
	copy(d[:], b)
	return d
}
