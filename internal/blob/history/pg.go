package history

import (
	"context"
	"errors"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// PG is the [Store] over PostgreSQL (queries/storage_history.sql).
type PG struct{ DB *postgres.DB }

var _ Store = PG{}

// unix is a time as the statements take it: seconds since the epoch, 0 for the zero time.
func unix(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return float64(t.UnixNano()) / 1e9
}

func (p PG) exec(ctx context.Context, f func(*q.Queries) error) error {
	return p.DB.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errors.New("history: the transaction is not one of internal/meta/postgres")
		}
		return f(q.New(ptx))
	})
}

// AddCounts implements [Store] with one upsert.
func (p PG) AddCounts(ctx context.Context, counts []Count) error {
	arg := q.HistoryAddCountsParams{}
	for _, c := range counts {
		arg.Hours = append(arg.Hours, c.Hour.Unix())
		arg.Connections = append(arg.Connections, c.Connection)
		arg.Kinds = append(arg.Kinds, c.Kind)
		arg.Reasons = append(arg.Reasons, c.Reason)
		arg.Counts = append(arg.Counts, c.N)
		arg.Bytes = append(arg.Bytes, c.Bytes)
	}
	return p.exec(ctx, func(qs *q.Queries) error { return qs.HistoryAddCounts(ctx, arg) })
}

// OpenEvent implements [Store].
func (p PG) OpenEvent(ctx context.Context, replica string, e Event, dayCap int) (int64, bool, error) {
	var id int64
	capped := false
	err := p.exec(ctx, func(qs *q.Queries) error {
		ids, err := qs.HistoryOpenEvent(ctx, q.HistoryOpenEventParams{
			ConnectionID: e.Connection, Kind: e.Kind, Replica: replica, StartedUnix: unix(e.Started), Code: e.Code, Step: e.Step,
			DayCap: int32(dayCap), //nolint:gosec // a small constant
		})
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			id = ids[0]
			return nil
		}
		capped = true
		return qs.HistoryCountOnLastEvent(ctx, q.HistoryCountOnLastEventParams{ConnectionID: e.Connection, Kind: e.Kind, Replica: replica})
	})
	return id, capped, err
}

// UpdateEvent implements [Store].
func (p PG) UpdateEvent(ctx context.Context, id int64, seen time.Time, occurrences int, code string, ended time.Time) error {
	return p.exec(ctx, func(qs *q.Queries) error {
		return qs.HistoryUpdateEvent(ctx, q.HistoryUpdateEventParams{
			ID: id, SeenUnix: unix(seen), Occurrences: int32(occurrences), Code: code, EndedUnix: unix(ended), //nolint:gosec // occurrences of one interval
		})
	})
}

// EndReplicaEvents implements [Store].
func (p PG) EndReplicaEvents(ctx context.Context, replica string) error {
	return p.exec(ctx, func(qs *q.Queries) error {
		_, err := qs.HistoryEndReplicaEvents(ctx, replica)
		return err
	})
}
