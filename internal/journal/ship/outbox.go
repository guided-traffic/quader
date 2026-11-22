package ship

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// DB is what the outbox needs of the database; *postgres.DB implements it.
type DB interface {
	meta.Runner
	Read(ctx context.Context, fn func(q.DBTX) error) error
}

// Outbox is [journal.Outbox] over revocation_outbox and revocation_outbox_shipped (R-HA-054, storage.md R-STOR-059). The
// shipper job holds a singleton lease, so a plain read in id order is enough (queries/backup.sql, ClaimRevocationOutbox).
type Outbox struct{ db DB }

var _ journal.Outbox = (*Outbox)(nil)

// NewOutbox builds the outbox over a database.
func NewOutbox(db DB) *Outbox { return &Outbox{db: db} }

// nilUUID is the position before every outbox row.
const nilUUID = "00000000-0000-0000-0000-000000000000"

// Claim implements [journal.Outbox].
func (o *Outbox) Claim(ctx context.Context, after string, limit int) ([]journal.OutboxRow, error) {
	if after == "" {
		after = nilUUID
	}
	var rows []q.ClaimRevocationOutboxRow
	err := o.db.Read(ctx, func(d q.DBTX) (err error) {
		rows, err = q.New(d).ClaimRevocationOutbox(ctx, q.ClaimRevocationOutboxParams{After: after, RowLimit: int32(min(limit, 1000))}) //nolint:gosec // bounded above
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]journal.OutboxRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, journal.OutboxRow{ID: r.ID, At: r.At.Time.UTC(), Event: r.Event, Entry: r.Entry, Shipped: r.Shipped})
	}
	return out, nil
}

// MarkShipped implements [journal.Outbox].
func (o *Outbox) MarkShipped(ctx context.Context, marks []journal.Shipped) error {
	if len(marks) == 0 {
		return nil
	}
	p := q.InsertRevocationOutboxShippedParams{OutboxIds: make([]string, len(marks)), ConnectionIds: make([]string, len(marks))}
	for i, m := range marks {
		p.OutboxIds[i], p.ConnectionIds[i] = m.ID, m.Connection
	}
	return o.db.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errors.New("the transaction is not a PostgreSQL transaction")
		}
		return q.New(ptx).InsertRevocationOutboxShipped(ctx, p)
	})
}

// Delete implements [journal.Outbox].
func (o *Outbox) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	err := o.db.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errors.New("the transaction is not a PostgreSQL transaction")
		}
		_, err := q.New(ptx).DeleteRevocationOutboxRows(ctx, ids)
		return err
	})
	if err != nil {
		return fmt.Errorf("deleting revocation_outbox rows: %w", err)
	}
	return nil
}

// Quarantine implements [Quarantiner]: the row is recorded in system_state journal_quarantined and leaves the outbox in one
// transaction.
func (o *Outbox) Quarantine(ctx context.Context, r journal.OutboxRow, reason string) error {
	entry, err := json.Marshal(map[string]string{"id": r.ID, "event": r.Event, "at": r.At.UTC().Format(time.RFC3339Nano), "reason": reason})
	if err != nil {
		return err
	}
	return o.db.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errors.New("the transaction is not a PostgreSQL transaction")
		}
		qs := q.New(ptx)
		if err := qs.JournalQuarantine(ctx, string(entry)); err != nil {
			return err
		}
		_, err := qs.DeleteRevocationOutboxRows(ctx, []string{r.ID})
		return err
	})
}

// Pending implements [journal.Outbox]: the number of rows and the age of the oldest (quader_revocation_journal_pending).
func (o *Outbox) Pending(ctx context.Context) (int64, time.Duration, error) {
	var (
		n      int64
		oldest []time.Time
	)
	err := o.db.Read(ctx, func(d q.DBTX) error {
		qs := q.New(d)
		var err error
		if n, err = qs.CountRevocationOutbox(ctx); err != nil {
			return err
		}
		ts, err := qs.OldestRevocationOutbox(ctx)
		for _, t := range ts {
			oldest = append(oldest, t.Time)
		}
		return err
	})
	if err != nil || n == 0 || len(oldest) == 0 {
		return n, 0, err
	}
	return n, max(time.Since(oldest[0]), 0), nil
}
