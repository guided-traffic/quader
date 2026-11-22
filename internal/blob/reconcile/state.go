package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

var errForeignTx = errors.New("reconcile: the transaction is not one of internal/meta/postgres")

// queries returns the sqlc queries on the transaction behind tx; the statements live in
// internal/meta/postgres/queries/reconcile.sql and blob_copies.sql.
func queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errForeignTx
	}
	return q.New(ptx), nil
}

// state is where the pass and the drains stand between runs (Q-STOR-257): a run ends after storage.reconcile.run_duration and
// the next scheduled run, a job of its own, continues from here. It is one row of system_state, written after every batch, so
// a run released at a replica's drain loses nothing either.
type state struct {
	Pass   *passState             `json:"pass,omitempty"`
	Drains map[string]*drainState `json:"drains,omitempty"`
}

// passState is a full pass in progress.
type passState struct {
	StartedAt time.Time `json:"startedAt"`
	// After is the hex digest of the last blob of the last batch; empty before the first.
	After string `json:"after"`
	// Targets are the connections that were targets from the pass's start: only they end it as a full pass.
	Targets []string `json:"targets"`
	// Gaps counts per target the gaps the pass found; a full pass without a gap and with an empty queue fills a connection.
	Gaps  map[string]int64 `json:"gaps"`
	Blobs int64            `json:"blobs"`
}

// drainState is a drain's walk over the copies of one draining connection.
type drainState struct {
	// After is the object name of the last copy of the last batch; empty at the start of a walk.
	After string `json:"after"`
	// Deleted counts the copies this drain deleted, for the event system.storage.connection_drained.
	Deleted int64 `json:"deleted"`
	// Progress reports that the current walk deleted or queued something.
	Progress bool `json:"progress"`
	// WalkEndedAt is when the last walk without progress ended: the next one waits for the pass interval.
	WalkEndedAt time.Time `json:"walkEndedAt,omitzero"`
}

func (r *Reconciler) loadState(ctx context.Context) (*state, error) {
	var raw string
	err := r.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		raw, err = qs.ReconcileState(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	st := &state{}
	if err := json.Unmarshal([]byte(raw), st); err != nil {
		r.log.Warn("the stored position of the reconciliation is unreadable; it starts again", "error", err)
		st = &state{}
	}
	if st.Drains == nil {
		st.Drains = map[string]*drainState{}
	}
	return st, nil
}

func (r *Reconciler) saveState(ctx context.Context, st *state) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return r.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		return qs.ReconcileStateSet(ctx, string(b))
	})
}
