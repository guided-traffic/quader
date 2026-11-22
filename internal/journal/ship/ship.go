// Package ship writes the rows of revocation_outbox to the journal in object storage (R-HA-054 "Outbox and shipping"), on
// every connection that is not draining (storage.md R-STOR-059).
package ship

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"

	"github.com/guided-traffic/quader/internal/journal"
)

// DefaultBatch is the number of rows one claim reads.
const DefaultBatch = 100

// Target is one connection the journal goes to: its id, its store (through the connection's guard, so the circuit breaker and
// the request metrics see the writes) and its prefix, below which the objects go to <prefix>/_journal/revocations/.
type Target struct {
	Connection string
	Store      journal.Store
	Prefix     string
}

// Options configure [New].
type Options struct {
	Outbox journal.Outbox
	// Targets are the connections without drain: true of the shipper's configuration (storage.md R-STOR-059). Empty with
	// Discard set is the journal switched off.
	Targets []Target
	// Discard deletes the rows without writing an object: backup.revocation_journal.enabled is false, and a row must not
	// pile up for a journal nobody reads.
	Discard bool
	// OnQuarantine is called for every row set aside because it cannot be encoded (the metric
	// quader_revocation_journal_quarantined_total counts them). Optional.
	OnQuarantine func(r journal.OutboxRow, err error)
	// Batch is the largest number of rows one claim reads; zero means [DefaultBatch].
	Batch int
	Log   *slog.Logger
}

// Quarantiner is an outbox that can set a row aside: the row leaves the queue and stays recorded where an operator and the
// post-restore find it. [Outbox] implements it.
type Quarantiner interface {
	Quarantine(ctx context.Context, r journal.OutboxRow, reason string) error
}

// Shipper is the [journal.Shipper] over an outbox and the stores of the connections.
type Shipper struct{ o Options }

var _ journal.Shipper = (*Shipper)(nil)

// New builds a shipper. It panics without an outbox or, when not discarding, without a target: both are wiring errors.
func New(o Options) *Shipper {
	if o.Outbox == nil || (len(o.Targets) == 0 && !o.Discard) {
		panic("ship: New needs an outbox and a target")
	}
	if o.Batch <= 0 {
		o.Batch = DefaultBatch
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return &Shipper{o: o}
}

// run is the state of one Ship call.
type run struct {
	s       *Shipper
	failing map[string]error // the targets whose write failed in this run: they are not asked again until the next run
	total   int
}

// Ship walks the outbox once in id order. For each row it writes the object to every target that does not hold it yet; a target
// whose write fails is left out for the rest of the run, so one connection that is down delays only its own copies (storage.md
// R-STOR-059, Q-STOR-241). A row that every target holds is deleted; a row that a target still lacks stays, with the targets
// that hold it recorded, and is written to the others by a later run. A row whose object was written and whose deletion did not
// commit is written again under the same key with the same bytes. Ship returns the number of rows deleted and an error when a
// target failed; it never deletes an object.
func (s *Shipper) Ship(ctx context.Context) (int, error) {
	r := &run{s: s, failing: map[string]error{}}
	after := ""
	for {
		rows, err := s.o.Outbox.Claim(ctx, after, s.o.Batch)
		if err != nil {
			return r.total, fmt.Errorf("journal: reading the outbox: %w", err)
		}
		if len(rows) == 0 {
			return r.total, r.err()
		}
		if err := r.batch(ctx, rows); err != nil {
			return r.total, err
		}
		if !s.o.Discard && len(r.failing) == len(s.o.Targets) {
			return r.total, r.err() // every target failed: nothing more can be shipped in this run
		}
		if len(rows) < s.o.Batch {
			return r.total, r.err()
		}
		after = rows[len(rows)-1].ID
	}
}

// batch ships one claimed batch and records the result: the marks of the rows that stay, the deletion of the rows that every
// target holds.
func (r *run) batch(ctx context.Context, rows []journal.OutboxRow) error {
	o := r.s.o
	done := make([]string, 0, len(rows))
	var marks []journal.Shipped
	var stop error
	for _, row := range rows {
		if o.Discard {
			done = append(done, row.ID)
			continue
		}
		obj := journal.NewObject(row)
		body, err := obj.Encode()
		if err != nil {
			if err := r.quarantine(ctx, row, fmt.Errorf("journal: row %s cannot be encoded: %w", row.ID, err)); err != nil {
				stop = err
				break
			}
			continue
		}
		holds := slices.Clone(row.Shipped)
		var wrote []string
		for _, t := range o.Targets {
			if slices.Contains(holds, t.Connection) || r.failing[t.Connection] != nil {
				continue
			}
			key := journal.Key(t.Prefix, obj)
			if err := t.Store.Put(ctx, key, body); err != nil {
				r.failing[t.Connection] = fmt.Errorf("journal: writing %s on connection %s: %w", key, t.Connection, err)
				o.Log.Warn("journal: a connection did not take an object; its copies wait for the next run", "connection", t.Connection, "key", key, "error", err)
				continue
			}
			wrote = append(wrote, t.Connection)
			holds = append(holds, t.Connection)
		}
		if r.everyTarget(holds) {
			done = append(done, row.ID)
			continue
		}
		for _, c := range wrote {
			marks = append(marks, journal.Shipped{ID: row.ID, Connection: c})
		}
	}
	if len(marks) > 0 {
		if err := o.Outbox.MarkShipped(ctx, marks); err != nil {
			return fmt.Errorf("journal: recording the connections that hold an object: %w", err)
		}
	}
	if len(done) > 0 {
		if err := o.Outbox.Delete(ctx, done); err != nil {
			return fmt.Errorf("journal: deleting shipped rows: %w", err)
		}
		r.total += len(done)
	}
	return stop
}

// everyTarget reports whether every target holds a row's object.
func (r *run) everyTarget(holds []string) bool {
	for _, t := range r.s.o.Targets {
		if !slices.Contains(holds, t.Connection) {
			return false
		}
	}
	return true
}

// quarantine sets aside a row that cannot become an object. One such row must not hold back the revocations behind it. It is
// set aside, not dropped: it is counted, recorded in system_state and listed by post-restore, so the revocation it recorded is
// known to be missing from the journal (audit finding L4). An outbox that cannot do that keeps the row and fails, as before.
func (r *run) quarantine(ctx context.Context, row journal.OutboxRow, cause error) error {
	q, ok := r.s.o.Outbox.(Quarantiner)
	if !ok {
		return cause
	}
	if err := q.Quarantine(ctx, row, cause.Error()); err != nil {
		return fmt.Errorf("journal: quarantining row %s: %w", row.ID, err)
	}
	r.s.o.Log.Error("journal: an outbox row cannot be encoded and was set aside", "id", row.ID, "event", row.Event, "error", cause)
	if r.s.o.OnQuarantine != nil {
		r.s.o.OnQuarantine(row, cause)
	}
	return nil
}

// err joins the failures of the targets of this run, in the order of their ids.
func (r *run) err() error {
	ids := make([]string, 0, len(r.failing))
	for id := range r.failing {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	errs := make([]error, 0, len(ids))
	for _, id := range ids {
		errs = append(errs, r.failing[id])
	}
	return errors.Join(errs...)
}
