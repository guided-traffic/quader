package reconcile

import (
	"context"
	"encoding/hex"
	"slices"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The pass (R-STOR-056, Q-STOR-228): a walk over the live blobs by digest in batches of passBatch with one lookup of their
// copies per batch. It queues the gaps of every connection with drain: false in the worker's configuration: a live blob that
// lacks a present copy on the connection while another connection holds one (Q-STOR-257). It finds what no queue row stands
// for (a new connection, a blob committed by a replica whose configuration lacked the connection during a rollout, a lost row)
// and waits at its position while a connection's queue holds storage.reconcile.queue_limit rows.

// passStep advances the pass by one batch, starting a full pass when one is due. It reports whether it walked a batch.
func (rn *run) passStep(ctx context.Context) (bool, error) {
	targets := ids(rn.targets())
	if rn.st.Pass == nil {
		due, err := rn.passDue(ctx, targets)
		if err != nil || !due {
			return false, err
		}
		if err := rn.startPass(ctx, targets); err != nil {
			return false, err
		}
	}
	p := rn.st.Pass
	full, err := rn.queueFull(ctx, targets)
	if err != nil || full {
		return false, err
	}
	after, err := hex.DecodeString(p.After)
	if err != nil {
		after = nil
	}
	var page []q.ReconcilePassPageRow
	gaps := map[string][][]byte{}
	err = rn.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		if page, err = qs.ReconcilePassPage(ctx, q.ReconcilePassPageParams{After: after, PageSize: passBatch}); err != nil {
			return err
		}
		for _, row := range page {
			if len(row.Present) == 0 {
				continue // no connection holds it: not the reconciliation's (the commit's repair, the recheck)
			}
			for _, t := range targets {
				if !slices.Contains(row.Present, t) {
					gaps[t] = append(gaps[t], row.Sha256)
				}
			}
		}
		for t, ds := range gaps {
			if err := qs.ReconcileQueueGaps(ctx, q.ReconcileQueueGapsParams{ConnectionID: t, Digests: ds}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	var found int64
	for t, ds := range gaps {
		p.Gaps[t] += int64(len(ds))
		found += int64(len(ds))
	}
	p.Blobs += int64(len(page))
	rn.add(func(r *Result) { r.Queued += found; r.PassBlobs += int64(len(page)) })
	if len(page) > 0 {
		p.After = hex.EncodeToString(page[len(page)-1].Sha256)
	}
	if len(page) < passBatch {
		if err := rn.endPass(ctx, targets); err != nil {
			return false, err
		}
	}
	return true, nil
}

// passDue reports whether a full pass starts now (Q-STOR-257): a target has never ended one, is filling with an empty queue
// (the gaps of the last pass are copied, so the next may find none and fill it), or the last one ended PassInterval ago.
func (rn *run) passDue(ctx context.Context, targets []string) (bool, error) {
	if len(targets) == 0 {
		return false, nil
	}
	var due bool
	err := rn.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		due, err = qs.ReconcilePassDue(ctx, q.ReconcilePassDueParams{Targets: targets, IntervalSeconds: PassInterval.Seconds()})
		return err
	})
	return due, err
}

func (rn *run) startPass(ctx context.Context, targets []string) error {
	rn.st.Pass = &passState{StartedAt: rn.now().UTC(), Targets: targets, Gaps: map[string]int64{}}
	return rn.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		return qs.ReconcilePassStarted(ctx, targets)
	})
}

// queueFull reports whether a target's queue holds queue_limit rows: the pass waits at its position until the copies caught up.
func (rn *run) queueFull(ctx context.Context, targets []string) (bool, error) {
	limit := int32(max(1, rn.cfg.QueueLimit)) //nolint:gosec // a configured count
	var full bool
	err := rn.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		for _, t := range targets {
			if full, err = qs.ReconcileQueueFull(ctx, q.ReconcileQueueFullParams{ConnectionID: t, QueueLimit: limit}); err != nil || full {
				return err
			}
		}
		return nil
	})
	return full, err
}

// endPass ends a full pass: the targets that were targets since its start record last_full_pass_at, those it found no gap on
// and whose queue is empty are filled (R-STOR-051), and the journal pass runs (R-STOR-059).
func (rn *run) endPass(ctx context.Context, targets []string) error {
	p := rn.st.Pass
	var whole []string
	for _, t := range p.Targets {
		if slices.Contains(targets, t) {
			whole = append(whole, t)
		}
	}
	var filled []string
	err := rn.db.InTx(ctx, func(tx meta.Tx) error {
		filled = nil
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		if err := qs.ReconcilePassEnded(ctx, whole); err != nil {
			return err
		}
		for _, t := range whole {
			if p.Gaps[t] > 0 {
				continue
			}
			n, err := qs.ReconcileFilled(ctx, t)
			if err != nil {
				return err
			}
			if n > 0 {
				filled = append(filled, t)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, t := range filled {
		rn.log.Info("a connection holds every live blob and is active", "connection", t)
	}
	rn.log.Info("a full pass of the reconciliation ended", "blobs", p.Blobs, "gaps", p.Gaps)
	rn.add(func(r *Result) { r.PassEnded, r.Filled = true, append(r.Filled, filled...) })
	rn.st.Pass = nil
	n, err := rn.journalPass(ctx)
	if err != nil {
		rn.log.Warn("the journal pass did not finish; the next full pass tries again", "error", err)
	}
	rn.add(func(r *Result) { r.JournalCopied += n })
	return nil
}
