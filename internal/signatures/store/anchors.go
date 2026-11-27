package store

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
)

// AnchorRemovedEntry is the journal entry of signature.anchor.removed (Q-SIG-42): the anchor of the id, scope and key goes again.
type AnchorRemovedEntry struct {
	ID           int64    `json:"id"`
	Scope        string   `json:"scope"`
	Key          string   `json:"key"`
	ProjectID    string   `json:"projectId,omitempty"`
	Fingerprints []string `json:"fingerprints"`
}

// AnchorState is what a narrowing change of an anchor journals of each side: whether it is switched on and its material.
type AnchorState struct {
	Enabled  bool            `json:"enabled"`
	Material json.RawMessage `json:"material"`
}

// AnchorUpdatedEntry is the journal entry of signature.anchor.updated that narrowed (switched off, or material removed, Q-SIG-42).
type AnchorUpdatedEntry struct {
	ID     int64       `json:"id"`
	Key    string      `json:"key"`
	Kind   string      `json:"kind"`
	Before AnchorState `json:"before"`
	After  AnchorState `json:"after"`
}

// Anchors lists anchors (R-SIG-034): of every scope of the installation but project (q.Scope ""), of one scope, of every project
// (ScopeProject), or of one project (q.ProjectID), in scope order and then by title.
func (s *Store) Anchors(ctx context.Context, aq signatures.AnchorQuery) ([]signatures.AnchorRecord, error) {
	var out []signatures.AnchorRecord
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		var err error
		out, err = s.anchorRecords(ctx, qs, 0, aq.Scope, aq.ProjectID)
		return err
	})
	return out, err
}

// Anchor answers one anchor, NotFound when there is none.
func (s *Store) Anchor(ctx context.Context, id int64) (signatures.AnchorRecord, error) {
	var out signatures.AnchorRecord
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		var err error
		out, err = s.anchorRecord(ctx, qs, id)
		return err
	})
	return out, err
}

func (s *Store) anchorRecord(ctx context.Context, qs *q.Queries, id int64) (signatures.AnchorRecord, error) {
	recs, err := s.anchorRecords(ctx, qs, id, "", "")
	if err != nil {
		return signatures.AnchorRecord{}, err
	}
	if len(recs) == 0 {
		return signatures.AnchorRecord{}, errs.E(errs.NotFound, errs.CodeNotFound, "No trust anchor has this ID.")
	}
	return recs[0], nil
}

func (s *Store) anchorRecords(ctx context.Context, qs *q.Queries, id int64, scope, project string) ([]signatures.AnchorRecord, error) {
	st, err := qs.SignatureEpoch(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := qs.SignatureAnchors(ctx, q.SignatureAnchorsParams{ID: id, Scope: scope, ProjectID: project})
	if err != nil {
		return nil, err
	}
	out := make([]signatures.AnchorRecord, 0, len(rows))
	for _, r := range rows {
		rec := signatures.AnchorRecord{
			ID: r.ID, Scope: r.Scope, ProjectKey: r.ProjectKey, Key: r.Key, Title: r.Title, Kind: r.Kind,
			Material: r.Material, Enabled: r.Enabled, Note: r.Note, Version: r.Version,
			CreatedAt: timeOf(r.CreatedAt.Valid, r.CreatedAt.Time), UpdatedAt: timeOf(r.UpdatedAt.Valid, r.UpdatedAt.Time),
			CreatedBy: r.CreatedByName, Fingerprints: fingerprintsOf(r.Kind, storedMaterial(r.Material)), Signatures: r.Signatures,
			Applied: r.Enabled && (r.Scope != anchors.ScopeProject || st.ProjectAnchors),
		}
		rec.ProjectID = r.ProjectID
		out = append(out, rec)
	}
	return out, nil
}

// CreateAnchor validates and stores an anchor of scope installation or project (R-SIG-015, R-SIG-019): a refused member is Invalid
// (validation_failed) with the member's pointer and the code invalid_value or secret_material; a taken key Conflict (anchor_exists); a
// project anchor while the switch is off Forbidden (project_anchors_disabled). It records signature.anchor.created, raises the anchor
// epoch and wakes the trust job.
func (s *Store) CreateAnchor(ctx context.Context, actor audit.Actor, in signatures.AnchorInput) (signatures.AnchorRecord, error) {
	var fe fieldErrors
	project := in.ProjectID
	switch {
	case in.Scope == anchors.ScopeInstallation && project != "":
		fe.add("/scope", anchors.CodeInvalidValue, "An anchor of the installation names no project.")
	case in.Scope == anchors.ScopeProject && !uuidText.MatchString(project):
		fe.add("/scope", anchors.CodeInvalidValue, "A project anchor names its project.")
	case in.Scope != anchors.ScopeInstallation && in.Scope != anchors.ScopeProject:
		fe.add("/scope", anchors.CodeInvalidValue, "Only anchors of the installation and of a project are created through the API.")
	}
	checkAnchorTexts(&fe, &in.Key, &in.Title, &in.Note)
	if !anchors.KindValid(in.Kind) {
		fe.add("/kind", anchors.CodeInvalidValue, "The kind is one of ca, openpgp, public-key and sigstore.")
	}
	var material json.RawMessage
	if len(fe) == 0 {
		m, bad := decodeMaterial(in.Material)
		if bad != nil {
			fe = append(fe, *bad)
		} else if s.validateMaterial(&fe, in.Kind, m) != nil {
			var err error
			if material, err = encodeMaterial(m); err != nil {
				return signatures.AnchorRecord{}, err
			}
		}
	}
	if err := fe.err(); err != nil {
		return signatures.AnchorRecord{}, err
	}
	enabled := in.Enabled == nil || *in.Enabled
	var out signatures.AnchorRecord
	err := s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		if in.Scope == anchors.ScopeProject {
			if err := projectAnchorsOn(ctx, qs); err != nil {
				return err
			}
		}
		ids, err := qs.SignatureAnchorInsert(ctx, q.SignatureAnchorInsertParams{
			Scope: in.Scope, ProjectID: project, Key: in.Key, Title: in.Title, Kind: in.Kind, Material: material, Enabled: enabled,
			Note: in.Note, Actor: principalOf(actor),
		})
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return errs.E(errs.Conflict, errs.CodeAnchorExists, "An anchor with this key exists in the scope.")
		}
		if out, err = s.anchorRecord(ctx, qs, ids[0]); err != nil {
			return err
		}
		if err := s.o.Audit.RecordTx(ctx, tx, createdEvent(actor, out)); err != nil {
			return err
		}
		_, err = s.changed(ctx, qs, tx)
		return err
	})
	if err != nil {
		return signatures.AnchorRecord{}, err
	}
	return out, nil
}

func createdEvent(actor audit.Actor, a signatures.AnchorRecord) audit.Event {
	det := map[string]any{"id": a.ID, "scope": a.Scope, "kind": a.Kind, "title": a.Title, "fingerprints": a.Fingerprints}
	if a.ProjectKey != "" {
		det["project"] = a.ProjectKey
	}
	return audit.Event{
		Type: audit.EventSignatureAnchorCreated, Actor: actor, Outcome: audit.Success, Project: a.ProjectKey,
		Subject: audit.Subject{Kind: "signature_anchor", ID: idText(a.ID), Name: a.Key}, Details: details(det),
	}
}

// projectAnchorsOn answers Forbidden (project_anchors_disabled) while the switch is off (R-SIG-019); it locks the settings row, so a
// switch turned off meanwhile waits for the write.
func projectAnchorsOn(ctx context.Context, qs *q.Queries) error {
	st, err := qs.SignatureLockSettings(ctx)
	if err != nil {
		return err
	}
	if !st.ProjectAnchors {
		return errs.E(errs.Forbidden, errs.CodeProjectAnchorsDisabled,
			"An administrator has not allowed project owners to add trust anchors for their project.")
	}
	return nil
}

// UpdateAnchor changes an anchor under its version (0: no check) (R-SIG-016, R-SIG-034): a shipped anchor takes only Enabled, an anchor
// of the Helm values nothing (Conflict anchor_managed); a project anchor while the switch is off is Forbidden. It records
// signature.anchor.updated, journaled when the change narrows (switched off, or material whose fingerprints are not all kept,
// Q-SIG-42), raises the anchor epoch and wakes the trust job.
func (s *Store) UpdateAnchor(ctx context.Context, actor audit.Actor, id int64, p signatures.AnchorPatch, version int64) (signatures.AnchorRecord, error) {
	var out signatures.AnchorRecord
	err := s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		cur, err := lockAnchor(ctx, qs, id)
		if err != nil {
			return err
		}
		if version != 0 && version != cur.Version {
			return errs.Stale("The trust anchor", cur.Version)
		}
		if err := patchAllowed(ctx, qs, cur, p); err != nil {
			return err
		}
		next, err := s.patched(cur, p)
		if err != nil {
			return err
		}
		if next.Title == cur.Title && next.Note == cur.Note && next.Enabled == cur.Enabled && string(next.Material) == string(cur.Material) {
			out, err = s.anchorRecord(ctx, qs, id)
			return err
		}
		if _, err := qs.SignatureAnchorUpdate(ctx, q.SignatureAnchorUpdateParams{
			ID: id, Title: next.Title, Kind: cur.Kind, Material: next.Material, Enabled: next.Enabled, Note: next.Note,
		}); err != nil {
			return err
		}
		if out, err = s.anchorRecord(ctx, qs, id); err != nil {
			return err
		}
		if err := s.o.Audit.RecordTx(ctx, tx, updatedEvent(actor, cur, next, out.ProjectKey)); err != nil {
			return err
		}
		epoch, err := s.changed(ctx, qs, tx)
		if err != nil {
			return err
		}
		return markNarrowed(ctx, qs, cur.ID, epoch, cur.Enabled, next.Enabled, cur.Material, next.Material)
	})
	if err != nil {
		return signatures.AnchorRecord{}, err
	}
	return out, nil
}

// markNarrowed records on an anchor the epoch of a write that narrowed it for the gate of stage 3 (R-SIG-066): it was switched off, or
// its material changed in any way. A change of the material counts whole, as a removal, because the gate cannot tell without
// cryptography which signers the new material still knows (fail closed until the trust job computed the rows again).
func markNarrowed(ctx context.Context, qs *q.Queries, id, epoch int64, wasOn, isOn bool, before, after json.RawMessage) error {
	if !wasOn || (isOn && string(before) == string(after)) {
		return nil
	}
	return qs.SignatureAnchorNarrowed(ctx, q.SignatureAnchorNarrowedParams{ID: id, Epoch: epoch})
}

// anchorRow is an anchor as a change reads it.
type anchorRow struct {
	ID        int64
	Scope     string
	ProjectID string
	Key       string
	Title     string
	Kind      string
	Material  json.RawMessage
	Enabled   bool
	Note      string
	Version   int64
}

func lockAnchor(ctx context.Context, qs *q.Queries, id int64) (anchorRow, error) {
	rows, err := qs.SignatureAnchorLock(ctx, id)
	if err != nil {
		return anchorRow{}, err
	}
	if len(rows) == 0 {
		return anchorRow{}, errs.E(errs.NotFound, errs.CodeNotFound, "No trust anchor has this ID.")
	}
	r := rows[0]
	return anchorRow{
		ID: r.ID, Scope: r.Scope, ProjectID: r.ProjectID, Key: r.Key, Title: r.Title, Kind: r.Kind, Material: r.Material,
		Enabled: r.Enabled, Note: r.Note, Version: r.Version,
	}, nil
}

// patchAllowed refuses what the anchor's scope does not let the API change (R-SIG-016, R-SIG-019).
func patchAllowed(ctx context.Context, qs *q.Queries, cur anchorRow, p signatures.AnchorPatch) error {
	managed := errs.E(errs.Conflict, errs.CodeAnchorManaged,
		"The anchor is shipped with Quader or declared in the Helm configuration: only a shipped anchor's switch can be changed.")
	other := p.Title != nil || p.Note != nil || p.Material != nil
	switch cur.Scope {
	case anchors.ScopeConfiguration:
		if other || p.Enabled != nil {
			return managed
		}
	case anchors.ScopeShipped:
		if other {
			return managed
		}
	case anchors.ScopeProject:
		return projectAnchorsOn(ctx, qs)
	}
	return nil
}

// patched is the anchor after the patch, its members validated.
func (s *Store) patched(cur anchorRow, p signatures.AnchorPatch) (anchorRow, error) {
	next := cur
	var fe fieldErrors
	checkAnchorTexts(&fe, nil, p.Title, p.Note)
	if p.Title != nil {
		next.Title = *p.Title
	}
	if p.Note != nil {
		next.Note = *p.Note
	}
	if p.Enabled != nil {
		next.Enabled = *p.Enabled
	}
	if p.Material != nil {
		m, bad := decodeMaterial(p.Material)
		if bad != nil {
			fe = append(fe, *bad)
		} else if s.validateMaterial(&fe, cur.Kind, m) != nil {
			b, err := encodeMaterial(m)
			if err != nil {
				return anchorRow{}, err
			}
			next.Material = b
		}
	}
	if err := fe.err(); err != nil {
		return anchorRow{}, err
	}
	return next, nil
}

// narrows reports whether the change from before to after narrows what the anchor trusts (Q-SIG-42): it is switched off, or a
// fingerprint of its material is not kept.
func narrows(kind string, before, after AnchorState) bool {
	if !before.Enabled {
		return false
	}
	if !after.Enabled {
		return true
	}
	b := fingerprintValues(fingerprintsOf(kind, storedMaterial(before.Material)))
	a := fingerprintValues(fingerprintsOf(kind, storedMaterial(after.Material)))
	return !subsetOf(b, a)
}

func updatedEvent(actor audit.Actor, cur, next anchorRow, projectKey string) audit.Event {
	state := func(a anchorRow) map[string]any {
		return map[string]any{
			"title": a.Title, "enabled": a.Enabled, "note": a.Note,
			"fingerprints": fingerprintsOf(a.Kind, storedMaterial(a.Material)),
		}
	}
	ev := audit.Event{
		Type: audit.EventSignatureAnchorUpdated, Actor: actor, Outcome: audit.Success, Project: projectKey,
		Subject: audit.Subject{Kind: "signature_anchor", ID: idText(cur.ID), Name: cur.Key},
		Details: details(map[string]any{"id": cur.ID, "before": state(cur), "after": state(next)}),
	}
	before := AnchorState{Enabled: cur.Enabled, Material: cur.Material}
	after := AnchorState{Enabled: next.Enabled, Material: next.Material}
	if narrows(cur.Kind, before, after) {
		ev.Journal = details(AnchorUpdatedEntry{ID: cur.ID, Key: cur.Key, Kind: cur.Kind, Before: before, After: after})
	}
	return ev
}

// DeleteAnchor removes an anchor of scope installation or project (R-SIG-016; Conflict anchor_managed for the others). An
// administrator removes a project anchor whatever the switch says; the caller decides who may. It records signature.anchor.removed
// with its journal entry, raises the anchor epoch and wakes the trust job.
func (s *Store) DeleteAnchor(ctx context.Context, actor audit.Actor, id int64) error {
	return s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		cur, err := lockAnchor(ctx, qs, id)
		if err != nil {
			return err
		}
		if cur.Scope == anchors.ScopeShipped || cur.Scope == anchors.ScopeConfiguration {
			return errs.E(errs.Conflict, errs.CodeAnchorManaged,
				"The anchor is shipped with Quader or declared in the Helm configuration and cannot be removed through the API.")
		}
		rec, err := s.anchorRecord(ctx, qs, id)
		if err != nil {
			return err
		}
		if _, err := qs.SignatureAnchorDelete(ctx, id); err != nil {
			return err
		}
		if err := s.o.Audit.RecordTx(ctx, tx, removedEvent(actor, cur, rec.ProjectKey)); err != nil {
			return err
		}
		_, err = s.changed(ctx, qs, tx)
		return err
	})
}

func removedEvent(actor audit.Actor, cur anchorRow, projectKey string) audit.Event {
	fps := fingerprintsOf(cur.Kind, storedMaterial(cur.Material))
	det := map[string]any{"id": cur.ID, "scope": cur.Scope, "fingerprints": fps}
	if projectKey != "" {
		det["project"] = projectKey
	}
	return audit.Event{
		Type: audit.EventSignatureAnchorRemoved, Actor: actor, Outcome: audit.Success, Project: projectKey,
		Subject: audit.Subject{Kind: "signature_anchor", ID: idText(cur.ID), Name: cur.Key},
		Details: details(det),
		Journal: details(AnchorRemovedEntry{
			ID: cur.ID, Scope: cur.Scope, Key: cur.Key, ProjectID: cur.ProjectID, Fingerprints: slices.Clip(fingerprintValues(fps)),
		}),
	}
}
