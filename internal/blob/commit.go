package blob

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
)

// LinkFunc adds the reference to the blob inside the commit transaction: it
// inserts or updates the item and runs everything that must commit with it. The
// guard trigger of the reference column re-checks that the blob is live under
// the same lock. It runs once per transaction attempt and must be safe to run
// again.
type LinkFunc func(ctx context.Context, tx meta.Tx) error

// CommitResult says what a Commit did.
type CommitResult struct {
	// Wrote reports whether this call wrote the object.
	Wrote bool
	// Inserted reports whether this call inserted the blob row.
	Inserted bool
	// Deduplicated reports that the row already existed and live: no object was
	// written and no row was inserted.
	Deduplicated bool
	// Rounds is the number of rounds of the protocol loop.
	Rounds int
}

type action uint8

const (
	actDone action = iota
	actPut
	actRetry
	actWaitDeleting
	actRepair
)

// errRollback ends a transaction attempt without committing; the action says
// what to do next.
var errRollback = errors.New("rollback")

// Commit runs the commit protocol of R-STOR-005 and R-STOR-053: it makes sure a live blob row
// exists whose object lies on storage.replication.min_copies connections, and runs link in the transaction that
// holds the row's FOR SHARE lock. The guarantee: a committed reference points at a live row that had at least min_copies
// present copies when the reference was committed, each written before its row. The link round records a copy row for every
// object this commit wrote and a queue row for every other connection that holds every blob. No lock is held across a
// transfer to the object store.
//
// link may be nil to commit the blob alone.
func (s *Store) Commit(ctx context.Context, sp *Spooled, link LinkFunc) (CommitResult, error) {
	res, err := s.CommitAll(ctx, []*Spooled{sp}, link)
	return res[0], err
}

// member is one distinct blob of a [Store.CommitAll] and what the protocol has
// done for it so far.
type member struct {
	sp      *Spooled
	res     CommitResult
	putDone bool
	putAt   time.Time   // the end of the earliest of the writes, from which the insert bound counts (R-STOR-053)
	writes  []copyWrite // the objects this commit wrote, one per connection
	act     action
	ins     bool
}

// CommitAll is the commit protocol for several blobs in one link transaction
// (R-STOR-005, R-ARCH-009): every blob has a live row whose object is in the
// bucket, and link runs once, in the one transaction that holds the FOR SHARE
// lock of every row, so that the references it writes either all exist or none
// does. Blobs with the same digest are one blob. The rows are locked and
// inserted in digest order, so that two commits over overlapping sets never wait
// on each other's uncommitted insert in opposite orders. The results are in the
// order of sps.
//
// A failure after any object write queues the digests that may have been left
// without a reference for collection, as a failed Commit does.
func (s *Store) CommitAll(ctx context.Context, sps []*Spooled, link LinkFunc) ([]CommitResult, error) {
	ms, index := distinctMembers(sps)
	attempts := s.cfg.Upload.CommitAttempts
	if attempts < 1 {
		attempts = 1
	}
	// How long after its own object write a commit may insert the row of an
	// object no row announced: the commit deadline, and never more than half of
	// delete_settle. A collector that dropped a row between our write and our
	// insert may have deleted the key we wrote after the write; its drop
	// happens delete_settle after that delete, so an insert inside half of it
	// cannot follow such a drop (R-STOR-005, R-STOR-024).
	window := min(s.cfg.Upload.CommitDeadline, s.cfg.GC.DeleteSettle/2)

	results := func() []CommitResult {
		out := make([]CommitResult, len(sps))
		for i, at := range index {
			out[i] = ms[at].res
		}
		return out
	}
	fail := func(err error) ([]CommitResult, error) {
		for _, m := range ms {
			if m.putDone || m.res.Wrote {
				s.enqueueAfterFailure(ctx, m.sp.SHA256)
			}
		}
		return results(), err
	}

	for round := 1; round <= attempts; round++ {
		for _, m := range ms {
			m.res.Rounds = round
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		done, err := s.commitRound(ctx, ms, link, window)
		if err != nil {
			return fail(err)
		}
		if done {
			for _, m := range ms {
				m.res.Inserted = m.ins
				m.res.Deduplicated = !m.ins && !m.res.Wrote
			}
			return results(), nil
		}
		for _, m := range ms {
			if err := s.settle(ctx, m); err != nil {
				return fail(err)
			}
		}
	}
	return fail(errs.E(errs.Unavailable, errs.CodeUnavailable, "the upload could not be committed; retry",
		errs.WithRetryAfter(retryAfter), errs.WithCause(ErrCommitAttempts)))
}

// distinctMembers is the blobs of sps without repeats, sorted by digest, and for
// each element of sps the position of its blob among them.
func distinctMembers(sps []*Spooled) (ms []*member, index []int) {
	seen := make(map[Digest]bool, len(sps))
	for _, sp := range sps {
		if !seen[sp.SHA256] {
			seen[sp.SHA256] = true
			ms = append(ms, &member{sp: sp})
		}
	}
	slices.SortFunc(ms, func(a, b *member) int { return bytes.Compare(a.sp.SHA256[:], b.sp.SHA256[:]) })
	at := make(map[Digest]int, len(ms))
	for i, m := range ms {
		at[m.sp.SHA256] = i
	}
	index = make([]int, len(sps))
	for i, sp := range sps {
		index[i] = at[sp.SHA256]
	}
	return ms, index
}

// settle does what the last round found a blob needs before the next round.
func (s *Store) settle(ctx context.Context, m *member) error {
	switch m.act {
	case actPut:
		writes, err := s.writeObjects(ctx, m.sp)
		if len(writes) > 0 {
			m.res.Wrote = true // the objects are left to the orphan sweep when the commit fails (R-STOR-053)
		}
		if err != nil {
			return err
		}
		m.writes, m.putDone = writes, true
		m.putAt = writes[0].at
		for _, w := range writes[1:] {
			if w.at.Before(m.putAt) {
				m.putAt = w.at
			}
		}
	case actRepair:
		writes, err := s.writeObjects(ctx, m.sp)
		if len(writes) > 0 {
			m.res.Wrote = true
		}
		if err != nil {
			return err
		}
		return s.repair(ctx, m.sp.SHA256, writes)
	case actWaitDeleting:
		// A GC delete may still land on the key: the objects we wrote (if any)
		// are not trustworthy, so write them again after the row is gone.
		m.putDone, m.writes = false, nil
		return s.waitGone(ctx, m.sp.SHA256)
	}
	return nil
}

// repair records the objects the repair case wrote for a live blob without a present copy (R-STOR-053): their copy rows
// become present and the blob, by the derivation of R-STOR-052, ok.
func (s *Store) repair(ctx context.Context, d Digest, writes []copyWrite) error {
	var dm damage
	err := s.db.InTx(ctx, func(tx meta.Tx) error {
		dm = damage{}
		state, found, err := s.rows.LockForMark(ctx, tx, d)
		if err != nil || !found || state != "live" {
			return err // a collection claimed it meanwhile: the next round sees the claim
		}
		if err := s.recordWrites(ctx, tx, d, writes); err != nil {
			return err
		}
		dm, err = s.rederive(ctx, tx, d)
		return err
	})
	if err == nil {
		s.report(ctx, d, dm)
	}
	return err
}

// commitRound is one pass of the protocol's loop body in one transaction. It
// reports done when every blob had a live row and link ran; otherwise each
// member's act says what it needs.
func (s *Store) commitRound(ctx context.Context, ms []*member, link LinkFunc, window time.Duration) (done bool, err error) {
	err = s.db.InTx(ctx, func(tx meta.Tx) error {
		pending := false
		for _, m := range ms {
			m.act, m.ins = actDone, false // the function may run again
		}
		for _, m := range ms {
			canInsert := m.putDone && s.now().Sub(m.putAt) <= window
			state, status, found, err := s.rows.LockShare(ctx, tx, m.sp.SHA256)
			if err != nil {
				return err
			}
			switch {
			case !found:
				if !canInsert {
					m.act, pending = actPut, true
					continue
				}
				won, err := s.rows.InsertLive(ctx, tx, m.sp)
				if err != nil {
					return err
				}
				if !won { // a concurrent uploader won; read again
					m.act, pending = actRetry, true
					continue
				}
				m.ins = true
				if err := s.recordWrites(ctx, tx, m.sp.SHA256, m.writes); err != nil {
					return err
				}
			case state == "deleting":
				m.act, pending = actWaitDeleting, true
			case status != "ok":
				m.act, pending = actRepair, true
			case m.putDone:
				// a deduplication hit after our own write (a concurrent uploader inserted the row): the objects written within
				// the insert bound are copies too (R-STOR-053); an older one may have met a collection's delete and is left to the
				// orphan sweep
				var fresh []copyWrite
				for _, w := range m.writes {
					if s.now().Sub(w.at) <= window {
						fresh = append(fresh, w)
					}
				}
				if len(fresh) > 0 {
					if err := s.recordWrites(ctx, tx, m.sp.SHA256, fresh); err != nil {
						return err
					}
				}
			}
		}
		if pending {
			return errRollback
		}
		if link != nil {
			return link(ctx, tx)
		}
		return nil
	})
	switch {
	case errors.Is(err, errRollback):
		return false, nil
	case err != nil && sqlState(err) == sqlStateAS001:
		// The guard saw a row that is not live (a collector claimed it between
		// our lock and the reference): start the protocol again.
		for _, m := range ms {
			m.act = actRetry
		}
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

// LinkExisting runs link in one transaction that holds the FOR SHARE lock of the row of a blob the store already holds, with no
// object write: the commit of an item for content that is stored (deploy by checksum, R-STOR-008). A blob that has no row, is
// being deleted or whose content is not ok is [ErrBlobNotFound]: a digest names nothing a caller may link unless the object is live.
// The guard trigger of the reference column re-checks liveness under the same lock; a collector that claimed the row between the lock
// and the reference makes the call answer ErrBlobNotFound as well. link must be safe to run again.
func (s *Store) LinkExisting(ctx context.Context, d Digest, link LinkFunc) error {
	err := s.db.InTx(ctx, func(tx meta.Tx) error {
		state, status, found, err := s.rows.LockShare(ctx, tx, d)
		if err != nil {
			return err
		}
		if !found || state == "deleting" || status != "ok" {
			return ErrBlobNotFound
		}
		return link(ctx, tx)
	})
	if err != nil && sqlState(err) == sqlStateAS001 {
		return ErrBlobNotFound
	}
	return err
}

// waitGone polls until the blob row has disappeared, for at most
// storage.upload.blob_wait_timeout.
func (s *Store) waitGone(ctx context.Context, d Digest) error {
	deadline := s.now().Add(s.cfg.Upload.BlobWaitTimeout)
	for {
		var exists bool
		if err := s.db.InTx(ctx, func(tx meta.Tx) error {
			var err error
			exists, err = s.rows.Exists(ctx, tx, d)
			return err
		}); err != nil {
			return err
		}
		if !exists {
			return nil
		}
		if !s.now().Before(deadline) {
			return errs.E(errs.Unavailable, errs.CodeUnavailable, "the content is being removed by garbage collection; retry",
				errs.WithRetryAfter(retryAfter), errs.WithCause(ErrBlobBusy))
		}
		t := time.NewTimer(s.wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// enqueueAfterFailure puts the digest in blob_gc_queue after a failure that may
// have left an object or a reference-less row, so that the incremental
// collection finds it (R-STOR-005). It is best effort and uses a context of its
// own because the request context may be what failed.
func (s *Store) enqueueAfterFailure(ctx context.Context, d Digest) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.db.InTx(ctx, func(tx meta.Tx) error {
		return s.rows.Enqueue(ctx, tx, d)
	}); err != nil {
		s.log.Warn("could not queue a digest for collection after a failed commit", "sha256", d.Hex(), "error", err)
	}
}

// Lookup returns the blob row of a digest, ErrBlobNotFound when there is none.
func (s *Store) Lookup(ctx context.Context, d Digest) (Row, error) {
	var r Row
	var found bool
	err := s.db.InTx(ctx, func(tx meta.Tx) error {
		var err error
		r, found, err = s.rows.Row(ctx, tx, d)
		return err
	})
	if err != nil {
		return Row{}, err
	}
	if !found {
		return Row{}, ErrBlobNotFound
	}
	return r, nil
}
