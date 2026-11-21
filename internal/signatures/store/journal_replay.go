package store

import (
	"context"
	"encoding/json"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
)

// The replay of what narrowed trust (R-SIG-041, Q-SIG-42). A restore to t brings the anchors, the blocks and the switch back as they
// were at t; the replay re-applies what narrowed after t:
//
//   - signature.anchor.removed removes the anchor of the id, scope and key again;
//   - signature.anchor.updated that narrowed switches the anchor off or sets its narrower material again, by the update rule of
//     R-HA-054 (journal.ApplyUpdate): applied when the journaled state narrows the restored one, skipped when the change only widened,
//     and suspended (switched off) otherwise;
//   - signature.signer.blocked blocks the signer again;
//   - signature.settings.changed that turned the switch off turns it off again.
//
// An anchor added after t is lost, which narrows. The trust rows are transient: the trust job recomputes every one after the restore
// (R-SIG-041). The replay writes no audit event (Q-AUTHZ-232).

func init() {
	journal.Register(journal.Handler{Domain: "signatures", Events: []string{audit.EventSignatureAnchorRemoved}, Apply: replayAnchorRemoved})
	journal.Register(journal.Handler{Domain: "signatures", Events: []string{audit.EventSignatureAnchorUpdated}, Apply: replayAnchorUpdated})
	journal.Register(journal.Handler{Domain: "signatures", Events: []string{audit.EventSignatureSignerBlocked}, Apply: replayBlock})
	journal.Register(journal.Handler{Domain: "signatures", Events: []string{audit.EventSignatureSettingsChanged}, Apply: replaySwitchOff})
}

// replayed raises the anchor epoch and announces the change area after a replay changed something.
func replayed(ctx context.Context, qs *q.Queries, tx meta.Tx, env journal.Env) error {
	epoch, err := qs.SignatureRaiseEpoch(ctx)
	if err != nil {
		return err
	}
	return env.Notify(tx, cluster.AreaSignatureAnchors, 1, map[string]int64{"epoch": epoch})
}

func replayAnchorRemoved(ctx context.Context, tx meta.Tx, env journal.Env, o journal.Object) (journal.Result, error) {
	var e AnchorRemovedEntry
	if !o.Into(&e) || e.ID <= 0 || !anchorKey.MatchString(e.Key) {
		return o.Malformed("is not a signature.anchor.removed entry"), nil
	}
	qs, err := queries(tx)
	if err != nil {
		return journal.Result{}, err
	}
	n, err := qs.SignatureJournalRemoveAnchor(ctx, q.SignatureJournalRemoveAnchorParams{ID: e.ID, Scope: e.Scope, Key: e.Key})
	if err != nil {
		return journal.Result{}, err
	}
	subject := "signature anchor " + e.Key
	if n == 0 {
		return o.Result(journal.AlreadyInEffect, subject), nil
	}
	if err := replayed(ctx, qs, tx, env); err != nil {
		return journal.Result{}, err
	}
	return o.Result(journal.Applied, subject), nil
}

// anchorNarrower is the comparator of R-HA-054's update rule for an anchor: after narrows current when it is switched off, or switched
// on with fingerprints all of which current has and current is switched on too.
type anchorNarrower struct{ kind string }

func (n anchorNarrower) Narrows(current, after AnchorState) bool {
	if !after.Enabled {
		return true
	}
	if !current.Enabled {
		return false
	}
	a := fingerprintValues(fingerprintsOf(n.kind, storedMaterial(after.Material)))
	c := fingerprintValues(fingerprintsOf(n.kind, storedMaterial(current.Material)))
	return subsetOf(a, c)
}

func replayAnchorUpdated(ctx context.Context, tx meta.Tx, env journal.Env, o journal.Object) (journal.Result, error) {
	var e AnchorUpdatedEntry
	if !o.Into(&e) || e.ID <= 0 || !anchors.KindValid(e.Kind) {
		return o.Malformed("is not a signature.anchor.updated entry"), nil
	}
	qs, err := queries(tx)
	if err != nil {
		return journal.Result{}, err
	}
	subject := "signature anchor " + e.Key
	rows, err := qs.SignatureAnchorLock(ctx, e.ID)
	if err != nil {
		return journal.Result{}, err
	}
	if len(rows) == 0 || rows[0].Key != e.Key || rows[0].Kind != e.Kind {
		return o.Result(journal.SubjectUnknown, subject), nil
	}
	cur := rows[0]
	current := AnchorState{Enabled: cur.Enabled, Material: cur.Material}
	if len(e.After.Material) == 0 || !json.Valid(e.After.Material) {
		return o.Malformed("names no material"), nil
	}
	if current.Enabled == e.After.Enabled && sameMaterial(current.Material, normalized(e.After.Material)) {
		return o.Result(journal.AlreadyInEffect, subject), nil
	}
	write := func(enabled bool, material json.RawMessage) error {
		_, err := qs.SignatureAnchorUpdate(ctx, q.SignatureAnchorUpdateParams{
			ID: cur.ID, Title: cur.Title, Kind: cur.Kind, Material: material, Enabled: enabled, Note: cur.Note,
		})
		return err
	}
	outcome, detail, err := journal.ApplyUpdate[AnchorState](anchorNarrower{kind: e.Kind}, current, e.Before, e.After, journal.UpdateActions{
		Apply: func() error { return write(e.After.Enabled, normalized(e.After.Material)) },
		Suspend: func() (json.RawMessage, error) {
			return details(e.After), write(false, cur.Material)
		},
	})
	if err != nil {
		return journal.Result{}, err
	}
	if outcome != journal.Skipped {
		if err := replayed(ctx, qs, tx, env); err != nil {
			return journal.Result{}, err
		}
	}
	r := o.Result(outcome, subject)
	r.Detail = detail
	return r, nil
}

// normalized is journaled material in the stored form.
func normalized(m json.RawMessage) json.RawMessage {
	b, err := encodeMaterial(storedMaterial(m))
	if err != nil {
		return m
	}
	return b
}

func replayBlock(ctx context.Context, tx meta.Tx, env journal.Env, o journal.Object) (journal.Result, error) {
	var e BlockEntry
	if !o.Into(&e) {
		return o.Malformed("is not a signature.signer.blocked entry"), nil
	}
	if checkBlock(blockInput(e)) != nil {
		return o.Malformed("names no valid block"), nil //nolint:nilerr // a forged or broken entry is skipped, not an error of the replay
	}
	qs, err := queries(tx)
	if err != nil {
		return journal.Result{}, err
	}
	n, err := qs.SignatureJournalBlock(ctx, q.SignatureJournalBlockParams{Kind: e.Kind, Value: e.Value, Issuer: e.Issuer, Reason: e.Reason})
	if err != nil {
		return journal.Result{}, err
	}
	subject := "signature block " + e.Kind + ":" + e.Value
	if n == 0 {
		return o.Result(journal.AlreadyInEffect, subject), nil
	}
	if err := replayed(ctx, qs, tx, env); err != nil {
		return journal.Result{}, err
	}
	return o.Result(journal.Applied, subject), nil
}

func replaySwitchOff(ctx context.Context, tx meta.Tx, env journal.Env, o journal.Object) (journal.Result, error) {
	var e SettingsEntry
	if !o.Into(&e) || e.ProjectAnchors {
		return o.Malformed("is not an entry that turned the switch off"), nil
	}
	qs, err := queries(tx)
	if err != nil {
		return journal.Result{}, err
	}
	n, err := qs.SignatureJournalSwitchOff(ctx)
	if err != nil {
		return journal.Result{}, err
	}
	if n == 0 {
		return o.Result(journal.AlreadyInEffect, "signatures.projectAnchors"), nil
	}
	if err := env.Notify(tx, cluster.AreaSignatureAnchors, 1, map[string]string{"what": "replay"}); err != nil {
		return journal.Result{}, err
	}
	return o.Result(journal.Applied, "signatures.projectAnchors"), nil
}

// blockInput is a journaled block as the checks of a new one read it.
func blockInput(e BlockEntry) signatures.BlockInput {
	return signatures.BlockInput{Kind: e.Kind, Value: e.Value, Issuer: e.Issuer, Reason: e.Reason}
}
