package downloadlinks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The store of the switch (R-STOR-014, Q-STOR-280): the one row download_link_settings. A change records
// storage.download_links.changed with before and after in its transaction and notifies the change area, which every replica's
// snapshot follows; a change that switches the delivery off carries the journal entry, so that a restore to an earlier point keeps
// links off.

// Notifier announces a change area in a transaction; *cluster.Changes implements it.
type Notifier interface {
	Notify(tx meta.Tx, area string, version int, payload any) error
}

// StoreOptions configure [NewStore].
type StoreOptions struct {
	Runner  meta.Runner
	Changes Notifier
	Audit   audit.Recorder
	Log     *slog.Logger
}

// Store reads and writes the row.
type Store struct{ o StoreOptions }

// NewStore creates the store.
func NewStore(o StoreOptions) *Store {
	if o.Audit == nil {
		o.Audit = audit.Nop{}
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Store{o: o}
}

var errForeignTx = errors.New("downloadlinks: not a PostgreSQL transaction")

func queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errForeignTx
	}
	return q.New(ptx), nil
}

func (s *Store) in(ctx context.Context, fn func(qs *q.Queries, tx meta.Tx) error) error {
	return s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		return fn(qs, tx)
	})
}

// Settings reads the row.
func (s *Store) Settings(ctx context.Context) (Settings, error) {
	var out Settings
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		r, err := qs.DownloadLinkSettings(ctx)
		if err != nil {
			return err
		}
		addrs, err := decodeAddresses(r.PublicAddresses)
		if err != nil {
			return err
		}
		out = Settings{
			Enabled: r.Enabled, MinSize: r.MinSize, LinkLifeSeconds: int(r.LinkLifeSeconds), PublicAddresses: addrs, Version: r.Version,
			UpdatedAt: r.UpdatedAt.Time.UTC(), UpdatedBy: r.UpdatedBy, UpdatedByName: r.UpdatedByName,
		}
		return nil
	})
	return out, err
}

// Entry is the journal entry of storage.download_links.changed that switched the delivery off; the replay switches it off again.
type Entry struct {
	Enabled bool `json:"enabled"`
}

// Update stores next as the row when its revision is version (a stale revision is errs.Stale, the 412 of If-Match). The caller
// validated next (Validate). A write that changes nothing records nothing and keeps the revision. It returns the row as stored.
func (s *Store) Update(ctx context.Context, actor audit.Actor, next Settings, version int64) (Settings, error) {
	err := s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		r, err := qs.DownloadLinkSettingsForUpdate(ctx)
		if err != nil {
			return err
		}
		if r.Version != version {
			return errs.Stale("The download link settings", r.Version)
		}
		addrs, err := decodeAddresses(r.PublicAddresses)
		if err != nil {
			return err
		}
		cur := Settings{Enabled: r.Enabled, MinSize: r.MinSize, LinkLifeSeconds: int(r.LinkLifeSeconds), PublicAddresses: addrs}
		if cur.equal(next) {
			return nil
		}
		n, err := qs.UpdateDownloadLinkSettings(ctx, q.UpdateDownloadLinkSettingsParams{
			Enabled: next.Enabled, MinSize: next.MinSize, LinkLifeSeconds: int32(next.LinkLifeSeconds), //nolint:gosec // validated: 10 to 3,600
			PublicAddresses: json.RawMessage(encodeAddresses(next.PublicAddresses)), Actor: actor.ID, Version: version,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return errs.Stale("The download link settings", r.Version)
		}
		if s.o.Changes != nil {
			if err := s.o.Changes.Notify(tx, cluster.AreaDownloadLinkSettings, 1, map[string]int64{"version": version + 1}); err != nil {
				return err
			}
		}
		ev := audit.Event{
			Type: audit.EventStorageDownloadLinksChanged, Actor: actor, Outcome: audit.Success,
			Details: marshal(map[string]any{"before": cur.state(), "after": next.state()}),
		}
		if cur.Enabled && !next.Enabled {
			ev.Journal = marshal(Entry{Enabled: false}) // switching off narrows; switching on and the details widen (R-STOR-014)
		}
		return s.o.Audit.RecordTx(ctx, tx, ev)
	})
	if err != nil {
		return Settings{}, err
	}
	return s.Settings(ctx)
}

func marshal(v any) json.RawMessage {
	b, _ := json.Marshal(v) // maps of strings, numbers and booleans always marshal
	return b
}

// The replay of a switch turned off (R-STOR-014): a restore to t brings the row back as it was at t, and the replay switches the
// delivery off again when it was switched off after t. It writes no audit event (Q-AUTHZ-232); every replica's snapshot follows the
// change area.
func init() {
	journal.Register(journal.Handler{Domain: "storage", Events: []string{audit.EventStorageDownloadLinksChanged}, Apply: replaySwitchOff})
}

func replaySwitchOff(ctx context.Context, tx meta.Tx, env journal.Env, o journal.Object) (journal.Result, error) {
	var e Entry
	if !o.Into(&e) || e.Enabled {
		return o.Malformed("is not an entry that switched the delivery by signed links off"), nil
	}
	qs, err := queries(tx)
	if err != nil {
		return journal.Result{}, err
	}
	n, err := qs.DisableDownloadLinks(ctx)
	if err != nil {
		return journal.Result{}, err
	}
	if n == 0 {
		return o.Result(journal.AlreadyInEffect, "downloadLinks.enabled"), nil
	}
	if err := env.Notify(tx, cluster.AreaDownloadLinkSettings, 1, map[string]string{"what": "replay"}); err != nil {
		return journal.Result{}, err
	}
	return o.Result(journal.Applied, "downloadLinks.enabled"), nil
}
