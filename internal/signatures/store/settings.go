package store

import (
	"context"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/shipped"
)

// SettingsEntry is the journal entry of signature.settings.changed that turned the switch off (Q-SIG-42): the replay turns it off again.
type SettingsEntry struct {
	ProjectAnchors bool `json:"projectAnchors"`
}

// Settings answers the switch, the anchor epoch, the state of the public Sigstore root and the backlog (R-SIG-034): the root is
// "refreshed" when the task stored one, else the release's snapshot with its build time and TUF version.
func (s *Store) Settings(ctx context.Context) (signatures.Settings, error) {
	var out signatures.Settings
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		r, err := qs.SignatureSettings(ctx)
		if err != nil {
			return err
		}
		out = settingsOf(r)
		return nil
	})
	return out, err
}

func settingsOf(r q.SignatureSettingsRow) signatures.Settings {
	out := signatures.Settings{
		ProjectAnchors: r.ProjectAnchors, AnchorEpoch: r.AnchorEpoch, Version: r.Version,
		UpdatedAt: timeOf(r.UpdatedAt.Valid, r.UpdatedAt.Time), UpdatedBy: r.UpdatedByName,
		Backlog: signatures.Backlog{Pending: r.Pending, Stale: r.Stale},
		TrustRoot: signatures.TrustRootState{
			LastError: r.TrustRootError, LastErrorAt: timeOf(r.TrustRootErrorAt.Valid, r.TrustRootErrorAt.Time),
		},
	}
	if r.RootStored {
		out.TrustRoot.Source = signatures.TrustRootRefreshed
		out.TrustRoot.FetchedAt = timeOf(r.RootFetchedAt.Valid, r.RootFetchedAt.Time)
		out.TrustRoot.TUFVersion = r.RootTufVersion
	} else {
		rel := shipped.Release()
		out.TrustRoot.Source = signatures.TrustRootRelease
		out.TrustRoot.FetchedAt = rel.BuiltAt.UTC()
		out.TrustRoot.TUFVersion = rel.TUFVersion
	}
	return out
}

// UpdateSettings changes the switch under the row's version (R-SIG-019): a version that is not the row's any more is errs.Stale. A
// change of the value records signature.settings.changed (journaled when it was switched off, Q-SIG-42), raises the anchor epoch,
// announces the change area and wakes the trust job, which drops or computes the project rows (R-SIG-012).
func (s *Store) UpdateSettings(ctx context.Context, actor audit.Actor, projectAnchors bool, version int64) (signatures.Settings, error) {
	err := s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		cur, err := qs.SignatureLockSettings(ctx)
		if err != nil {
			return err
		}
		n, err := qs.SignatureUpdateSwitch(ctx, q.SignatureUpdateSwitchParams{
			ProjectAnchors: projectAnchors, Actor: principalOf(actor), Version: version,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return errs.Stale("The signature settings", cur.Version)
		}
		if cur.ProjectAnchors == projectAnchors {
			return nil
		}
		ev := audit.Event{
			Type: audit.EventSignatureSettingsChanged, Actor: actor, Outcome: audit.Success,
			Details: details(map[string]any{
				"before": map[string]bool{"projectAnchors": cur.ProjectAnchors}, "after": map[string]bool{"projectAnchors": projectAnchors},
			}),
		}
		if !projectAnchors {
			ev.Journal = details(SettingsEntry{ProjectAnchors: false}) // switching off narrows; switching on widens (Q-SIG-42)
		}
		if err := s.o.Audit.RecordTx(ctx, tx, ev); err != nil {
			return err
		}
		_, err = s.changed(ctx, qs, tx)
		return err
	})
	if err != nil {
		return signatures.Settings{}, err
	}
	return s.Settings(ctx)
}
