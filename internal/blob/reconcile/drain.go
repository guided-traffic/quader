package reconcile

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The drain (R-STOR-056, Q-STOR-236, Q-STOR-259): for a connection D with drain: true the run walks D's copies; a blob that lacks
// a present copy on a connection with drain: false is queued there; a copy on D whose blob has a present copy on every such
// connection of the worker, one of them verified within its connection's scrub_period, is deleted: the verified copy is
// confirmed with HeadObject right before, then under the blob's lock the survivors are read again and D's row is set deleting,
// then the object is deleted under the hand-over deadline of R-STOR-024, then the row. The deletion pause stops it, the last
// copy of a blob is never deleted, and a connection that holds no copy is drained.

// drainStep advances the walk of each draining connection the worker reaches by one batch. It reports whether a walk read a
// batch.
func (rn *run) drainStep(ctx context.Context) (bool, error) {
	var progressed bool
	for _, d := range rn.conns.List() {
		if !d.Drain() {
			continue
		}
		if _, rejecting := d.Rejecting(); rejecting {
			continue
		}
		ds := rn.drainOf(d.ID())
		if ds.After == "" && !ds.WalkEndedAt.IsZero() && rn.now().Sub(ds.WalkEndedAt) < PassInterval {
			continue // the last walk found nothing to do; the next waits for the pass interval
		}
		paused, err := rn.deletionsPaused(ctx)
		if err != nil || paused {
			return progressed, err
		}
		did, err := rn.drainBatch(ctx, d, ds)
		if err != nil {
			return progressed, err
		}
		progressed = progressed || did
	}
	return progressed, nil
}

func (rn *run) drainOf(conn string) *drainState {
	ds := rn.st.Drains[conn]
	if ds == nil {
		ds = &drainState{}
		rn.st.Drains[conn] = ds
	}
	return ds
}

// candidate is a copy on the draining connection whose deletion the survivors allow.
type candidate struct {
	digest blob.Digest
	key    string // the object's name on the draining connection
}

// drainBatch walks one batch of the draining connection's copies.
func (rn *run) drainBatch(ctx context.Context, d *blob.Connection, ds *drainState) (bool, error) {
	var page []q.ReconcileDrainPageRow
	byDigest := map[blob.Digest][]q.ReconcileCopiesOfRow{}
	err := rn.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		page, err = qs.ReconcileDrainPage(ctx, q.ReconcileDrainPageParams{ConnectionID: d.ID(), After: ds.After, PageSize: drainBatch})
		if err != nil || len(page) == 0 {
			return err
		}
		digests := make([][]byte, len(page))
		for i, p := range page {
			digests[i] = p.Sha256
		}
		rows, err := qs.ReconcileCopiesOf(ctx, digests)
		for _, r := range rows {
			byDigest[digestOf(r.Sha256)] = append(byDigest[digestOf(r.Sha256)], r)
		}
		return err
	})
	if err != nil {
		return false, err
	}
	if len(page) == 0 {
		return false, rn.endWalk(ctx, d, ds)
	}
	gaps := map[string][][]byte{}
	var cands []candidate
	for _, p := range page {
		dg := digestOf(p.Sha256)
		missing, ok := rn.survivorsLack(byDigest[dg])
		switch {
		case !ok:
		case len(missing) > 0:
			for _, t := range missing {
				gaps[t] = append(gaps[t], p.Sha256)
			}
		default:
			if rn.confirmSurvivor(ctx, dg, byDigest[dg]) {
				cands = append(cands, candidate{digest: dg, key: p.ObjectKey})
			}
		}
	}
	if err := rn.queueDrainGaps(ctx, gaps, ds); err != nil {
		return true, err
	}
	if err := rn.deleteCandidates(ctx, d, ds, cands); err != nil {
		return true, err
	}
	ds.After = page[len(page)-1].ObjectKey
	if len(page) < drainBatch {
		return true, rn.endWalk(ctx, d, ds)
	}
	return true, nil
}

// survivorsLack reads a blob's copies for the drain: ok is false when the blob is not live or has no present copy anywhere
// (nothing to move and nothing that allows a deletion); missing names the connections with drain: false that lack a present
// copy, which the drain queues; with none missing, the blob's survivors are complete.
func (rn *run) survivorsLack(copies []q.ReconcileCopiesOfRow) (missing []string, ok bool) {
	if len(copies) == 0 || !copies[0].Live {
		return nil, false
	}
	present := map[string]bool{}
	for _, c := range copies {
		if c.State == blob.CopyPresent {
			present[c.ConnectionID] = true
		}
	}
	if len(present) == 0 {
		return nil, false
	}
	for _, t := range rn.targets() {
		if !present[t.ID()] {
			missing = append(missing, t.ID())
		}
	}
	return missing, true
}

// verifiedSurvivor is the first copy, in list order, on a connection with drain: false that is present and verified within its
// connection's scrub_period (Q-STOR-234, Q-STOR-236).
func (rn *run) verifiedSurvivor(copies []blob.Copy) (blob.Copy, *blob.Connection, bool) {
	for _, t := range rn.targets() {
		for _, c := range copies {
			if c.Connection == t.ID() && c.State == blob.CopyPresent && !c.VerifiedAt.IsZero() && rn.now().Sub(c.VerifiedAt) < t.ScrubPeriod() {
				return c, t, true
			}
		}
	}
	return blob.Copy{}, nil, false
}

// confirmSurvivor confirms the verified survivor's existence and size with HeadObject right before a deletion (R-STOR-056). A
// survivor the store does not have is marked missing; one it cannot be asked about now keeps the copy on the draining connection
// for a later walk.
func (rn *run) confirmSurvivor(ctx context.Context, d blob.Digest, rows []q.ReconcileCopiesOfRow) bool {
	copies := make([]blob.Copy, len(rows))
	for i, r := range rows {
		copies[i] = blob.Copy{Connection: r.ConnectionID, ObjectKey: r.ObjectKey, StoredSize: r.StoredSize, State: r.State}
		if r.VerifiedAt.Valid {
			copies[i].VerifiedAt = r.VerifiedAt.Time
		}
	}
	sv, conn, ok := rn.verifiedSurvivor(copies)
	if !ok {
		return false
	}
	if _, rejecting := conn.Rejecting(); rejecting {
		return false
	}
	info, err := conn.Objects().Head(ctx, sv.ObjectKey)
	switch {
	case errors.Is(err, blob.ErrNotFound):
		rn.store.MarkCopyDamaged(ctx, d, sv, blob.CopyMissing)
		return false
	case err != nil:
		return false
	case info.Size >= 0 && info.Size != sv.StoredSize:
		rn.store.MarkCopyDamaged(ctx, d, sv, blob.CopyCorrupt)
		return false
	}
	return true
}

// queueDrainGaps queues the blobs the draining connection holds and a connection with drain: false lacks.
func (rn *run) queueDrainGaps(ctx context.Context, gaps map[string][][]byte, ds *drainState) error {
	if len(gaps) == 0 {
		return nil
	}
	var n int64
	for _, digests := range gaps {
		n += int64(len(digests))
	}
	ds.Progress = true
	rn.add(func(r *Result) { r.Queued += n })
	return rn.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		for t, digests := range gaps {
			if err := qs.ReconcileQueueGaps(ctx, q.ReconcileQueueGapsParams{ConnectionID: t, Digests: digests}); err != nil {
				return err
			}
		}
		return nil
	})
}

// deleteCandidates claims each candidate under its blob's lock, deletes the claimed objects on the draining connection under the
// hand-over deadline and drops the rows of the deletes that succeeded. A delete that failed keeps its row claimed; the next run
// finishes it after handover_delay (finishClaims).
func (rn *run) deleteCandidates(ctx context.Context, d *blob.Connection, ds *drainState, cands []candidate) error {
	if len(cands) == 0 {
		return nil
	}
	at := rn.now()
	var claimedKeys []candidate
	for _, c := range cands {
		ok, err := rn.claim(ctx, d, c.digest)
		if err != nil {
			return err
		}
		if ok {
			claimedKeys = append(claimedKeys, c)
		}
	}
	n, err := rn.deleteClaimed(ctx, d, claimedKeys, at)
	ds.Deleted += n
	if n > 0 {
		ds.Progress = true
		rn.add(func(r *Result) { r.CopiesDeleted += n })
	}
	return err
}

// claim sets the draining connection's copy deleting in one transaction that holds the blob's lock and finds it live, after
// reading its copies again: every connection with drain: false must still hold a present copy and one of them must still be
// verified within its period, so a mark that came in since the walk read them is seen.
func (rn *run) claim(ctx context.Context, d *blob.Connection, dg blob.Digest) (bool, error) {
	var ok bool
	err := rn.db.InTx(ctx, func(tx meta.Tx) error {
		ok = false
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		locked, err := qs.LockBlobForMark(ctx, dg.Bytes())
		if err != nil || len(locked) == 0 || locked[0] != "live" {
			return err
		}
		rows, err := qs.BlobCopies(ctx, dg.Bytes())
		if err != nil {
			return err
		}
		copies := copiesOf(rows)
		for _, t := range rn.targets() {
			if copyState(copies, t.ID()) != blob.CopyPresent {
				return nil
			}
		}
		if _, _, verified := rn.verifiedSurvivor(copies); !verified {
			return nil
		}
		n, err := qs.ReconcileClaimDrainCopy(ctx, q.ReconcileClaimDrainCopyParams{Sha256: dg.Bytes(), ConnectionID: d.ID()})
		ok = n > 0
		return err
	})
	return ok, err
}

// deleteClaimed deletes the objects of claimed copies on one connection, with DeleteObjects when its batch_delete is true, only
// until the hand-over deadline counted from at, and drops the rows of the deletes that succeeded. It reports how many.
func (rn *run) deleteClaimed(ctx context.Context, d *blob.Connection, cs []candidate, at time.Time) (int64, error) {
	if len(cs) == 0 {
		return 0, nil
	}
	window := rn.o.Storage.GC.HandoverDelay - rn.o.Storage.GC.DeleteSettle
	if window < time.Second {
		window = time.Second
	}
	dctx, cancel := context.WithDeadline(ctx, at.Add(window))
	defer cancel()
	deleted := rn.deleteObjects(dctx, d, cs)
	if len(deleted) == 0 {
		return 0, nil
	}
	var n int64
	err := rn.db.InTx(ctx, func(tx meta.Tx) error {
		n = 0
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		for _, c := range deleted {
			k, err := qs.ReconcileDropDrainedCopy(ctx, q.ReconcileDropDrainedCopyParams{Sha256: c.digest.Bytes(), ConnectionID: d.ID()})
			if err != nil {
				return err
			}
			n += k
		}
		return nil
	})
	return n, err
}

// deleteObjects deletes the objects and returns those whose delete succeeded.
func (rn *run) deleteObjects(ctx context.Context, d *blob.Connection, cs []candidate) []candidate {
	objects := d.Objects()
	if d.BatchDelete() {
		var out []candidate
		for start := 0; start < len(cs); start += 1000 {
			chunk := cs[start:min(start+1000, len(cs))]
			keys := make([]string, len(chunk))
			for i, c := range chunk {
				keys[i] = c.key
			}
			failed, err := objects.DeleteMany(ctx, keys)
			if err != nil {
				rn.log.Warn("a drain's batch delete failed; the claims are finished by a later run", "connection", d.ID(), "error", err)
				continue
			}
			bad := map[string]bool{}
			for _, f := range failed {
				bad[f.Key] = true
			}
			for _, c := range chunk {
				if !bad[c.key] {
					out = append(out, c)
				}
			}
		}
		return out
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out []candidate
	)
	sem := make(chan struct{}, max(1, rn.o.Storage.GC.DeleteConcurrency))
	for _, c := range cs {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := objects.Delete(ctx, c.key); err != nil {
				rn.log.Warn("a drain's delete failed; the claim is finished by a later run", "connection", d.ID(), "key", c.key, "error", err)
				return
			}
			mu.Lock()
			out = append(out, c)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// endWalk ends a walk: the next starts at once when this one deleted or queued something, otherwise after the pass interval;
// a draining connection that holds no copy any more is drained.
func (rn *run) endWalk(ctx context.Context, d *blob.Connection, ds *drainState) error {
	ds.After = ""
	if ds.Progress {
		ds.WalkEndedAt = time.Time{}
	} else {
		ds.WalkEndedAt = rn.now().UTC()
	}
	ds.Progress = false
	return rn.markDrained(ctx, d.ID())
}

// markDrained records a draining connection that holds no copy as drained (R-STOR-051) with the event
// system.storage.connection_drained in the same transaction.
func (rn *run) markDrained(ctx context.Context, conn string) error {
	ds := rn.drainOf(conn)
	var drained bool
	err := rn.db.InTx(ctx, func(tx meta.Tx) error {
		drained = false
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		n, err := qs.ReconcileMarkDrained(ctx, conn)
		if err != nil || n == 0 {
			return err
		}
		drained = true
		if rn.o.Drained != nil {
			return rn.o.Drained(ctx, tx, conn, ds.Deleted)
		}
		return nil
	})
	if err != nil || !drained {
		return err
	}
	rn.log.Info("a draining connection holds no copy any more and is drained", "connection", conn, "copies_deleted", ds.Deleted)
	rn.add(func(r *Result) { r.Drained = append(r.Drained, conn) })
	delete(rn.st.Drains, conn)
	return nil
}

// finishClaims finishes the drain's claims nobody finished within handover_delay (a run that ended between a claim and its
// delete): each is taken over, its object deleted under the hand-over deadline, its row dropped. Then each draining connection
// that holds no copy any more is drained.
func (rn *run) finishClaims(ctx context.Context) error {
	paused, err := rn.deletionsPaused(ctx)
	if err != nil || paused {
		return err
	}
	var rows []q.ReconcileTakeStaleDrainClaimsRow
	err = rn.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err = qs.ReconcileTakeStaleDrainClaims(ctx, q.ReconcileTakeStaleDrainClaimsParams{
			HandoverSeconds: rn.o.Storage.GC.HandoverDelay.Seconds(), PageSize: drainBatch,
		})
		return err
	})
	if err != nil {
		return err
	}
	at := rn.now()
	byConn := map[string][]candidate{}
	for _, r := range rows {
		byConn[r.ConnectionID] = append(byConn[r.ConnectionID], candidate{digest: digestOf(r.Sha256), key: r.ObjectKey})
	}
	for id, cs := range byConn {
		c, ok := rn.conns.Get(id)
		if !ok {
			continue // claimed again after the next handover_delay by a worker that configures it
		}
		if _, rejecting := c.Rejecting(); rejecting {
			continue
		}
		n, err := rn.deleteClaimed(ctx, c, cs, at)
		if err != nil {
			return err
		}
		rn.drainOf(id).Deleted += n
		rn.add(func(r *Result) { r.CopiesDeleted += n })
	}
	for _, c := range rn.conns.List() {
		if c.Drain() {
			if err := rn.markDrained(ctx, c.ID()); err != nil {
				return err
			}
		}
	}
	return nil
}

// retire retires each drained connection that no live replica has in its configuration (R-STOR-051).
func (rn *run) retire(ctx context.Context) error {
	var retired []string
	err := rn.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		retired, err = qs.ReconcileRetire(ctx, rn.o.StaleAfter.Seconds())
		return err
	})
	if err != nil {
		return err
	}
	for _, id := range retired {
		rn.log.Info("a drained connection is in no live replica's configuration and is retired; its id is never used again", "connection", id)
		delete(rn.st.Drains, id)
	}
	rn.add(func(r *Result) { r.Retired = append(r.Retired, retired...) })
	return nil
}

// deletionsPaused reports the deletion pause of R-HA-025, which stops the drain's deletions.
func (rn *run) deletionsPaused(ctx context.Context) (bool, error) {
	var paused bool
	err := rn.db.InTx(ctx, func(tx meta.Tx) error {
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
