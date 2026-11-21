package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/shipped"
)

// Declaration is an anchor of the Helm values (signatures.anchors, R-SIG-018): its key, title, kind and material (the material of a
// ConfigMap already read by the caller), and an optional note.
type Declaration struct {
	Key      string
	Title    string
	Kind     string
	Material anchors.Material
	Note     string
}

// declaredRow is a stored anchor of a declared scope.
type declaredRow struct {
	ID       int64
	Key      string
	Title    string
	Kind     string
	Material json.RawMessage
	Enabled  bool
	Note     string
}

func declaredRows(ctx context.Context, qs *q.Queries, scope string) (map[string]declaredRow, error) {
	rows, err := qs.SignatureAnchorsOfScope(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("read the %s anchors: %w", scope, err)
	}
	out := make(map[string]declaredRow, len(rows))
	for _, r := range rows {
		out[r.Key] = declaredRow{ID: r.ID, Key: r.Key, Title: r.Title, Kind: r.Kind, Material: r.Material, Enabled: r.Enabled, Note: r.Note}
	}
	return out, nil
}

// ApplyShipped applies the release's shipped anchors in quader migrate's transaction (R-SIG-017, R-SIG-016): each by its key with the
// release's title, kind and material, an administrator's enabled kept; a shipped row the release no longer ships is removed. The anchor
// epoch rises, the change area is announced and the trust job woken when anything changed. The release writes no audit event.
func (s *Store) ApplyShipped(ctx context.Context, tx meta.Tx, list []shipped.Anchor) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	have, err := declaredRows(ctx, qs, anchors.ScopeShipped)
	if err != nil {
		return err
	}
	changed := false
	var narrowed []int64
	for _, a := range list {
		material, err := encodeMaterial(a.Material)
		if err != nil {
			return fmt.Errorf("shipped anchor %s: %w", a.Key, err)
		}
		cur, ok := have[a.Key]
		delete(have, a.Key)
		switch {
		case !ok:
			ids, err := qs.SignatureAnchorInsert(ctx, q.SignatureAnchorInsertParams{
				Scope: anchors.ScopeShipped, Key: a.Key, Title: a.Title, Kind: a.Kind, Material: material, Enabled: true,
			})
			if err != nil {
				return fmt.Errorf("store the shipped anchor %s: %w", a.Key, err)
			}
			changed = changed || len(ids) > 0
		case cur.Title != a.Title || cur.Kind != a.Kind || !sameMaterial(cur.Material, material):
			if _, err := qs.SignatureAnchorUpdate(ctx, q.SignatureAnchorUpdateParams{
				ID: cur.ID, Title: a.Title, Kind: a.Kind, Material: material, Enabled: cur.Enabled, Note: cur.Note,
			}); err != nil {
				return fmt.Errorf("update the shipped anchor %s: %w", a.Key, err)
			}
			changed = true
			if !sameMaterial(cur.Material, material) {
				narrowed = append(narrowed, cur.ID)
			}
		}
	}
	for _, gone := range have {
		if _, err := qs.SignatureAnchorDelete(ctx, gone.ID); err != nil {
			return fmt.Errorf("remove the shipped anchor %s: %w", gone.Key, err)
		}
		changed = true
	}
	if !changed {
		return nil
	}
	return s.changedNarrowing(ctx, qs, tx, narrowed)
}

// changedNarrowing raises the anchor epoch as changed does and records it on the anchors whose material a declaration replaced, which
// the gate of stage 3 counts for no older trust row (R-SIG-066).
func (s *Store) changedNarrowing(ctx context.Context, qs *q.Queries, tx meta.Tx, narrowed []int64) error {
	epoch, err := s.changed(ctx, qs, tx)
	if err != nil {
		return err
	}
	for _, id := range narrowed {
		if err := qs.SignatureAnchorNarrowed(ctx, q.SignatureAnchorNarrowedParams{ID: id, Epoch: epoch}); err != nil {
			return err
		}
	}
	return nil
}

// sameMaterial compares two stored forms of material, whatever spelling PostgreSQL's jsonb gave the stored one.
func sameMaterial(stored, want json.RawMessage) bool {
	b, err := encodeMaterial(storedMaterial(stored))
	return err == nil && string(b) == string(want)
}

// ApplyConfiguration applies the anchors of the Helm values in quader migrate's transaction (R-SIG-018): each declaration is checked
// as on save (anchors.Validate), and an invalid one is an error naming its key and the reason, which stops the migration; then each is
// stored by its key in the scope configuration and one no longer declared is removed, each recorded (signature.anchor.created,
// .updated, .removed) with actor (system:configuration). The anchor epoch rises when anything changed.
func (s *Store) ApplyConfiguration(ctx context.Context, tx meta.Tx, actor audit.Actor, list []Declaration) error {
	materials, err := s.checkDeclarations(list)
	if err != nil {
		return err
	}
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	have, err := declaredRows(ctx, qs, anchors.ScopeConfiguration)
	if err != nil {
		return err
	}
	changed := false
	var narrowed []int64
	for i, d := range list {
		cur, ok := have[d.Key]
		delete(have, d.Key)
		if ok && !sameMaterial(cur.Material, materials[i]) {
			narrowed = append(narrowed, cur.ID)
		}
		did, err := s.applyDeclaration(ctx, qs, tx, actor, d, materials[i], cur, ok)
		if err != nil {
			return fmt.Errorf("signatures.anchors: anchor %q: %w", d.Key, err)
		}
		changed = changed || did
	}
	for _, gone := range have {
		row := anchorRow{ID: gone.ID, Scope: anchors.ScopeConfiguration, Key: gone.Key, Kind: gone.Kind, Material: gone.Material}
		if _, err := qs.SignatureAnchorDelete(ctx, gone.ID); err != nil {
			return fmt.Errorf("signatures.anchors: remove anchor %q: %w", gone.Key, err)
		}
		if err := s.o.Audit.RecordTx(ctx, tx, removedEvent(actor, row, "")); err != nil {
			return err
		}
		changed = true
	}
	if !changed {
		return nil
	}
	return s.changedNarrowing(ctx, qs, tx, narrowed)
}

// checkDeclarations applies the checks on save to every declaration and answers their stored material: an error names the anchor's key
// and the reason (R-SIG-018).
func (s *Store) checkDeclarations(list []Declaration) ([]json.RawMessage, error) {
	seen := map[string]bool{}
	out := make([]json.RawMessage, 0, len(list))
	for _, d := range list {
		var fe fieldErrors
		checkAnchorTexts(&fe, &d.Key, &d.Title, &d.Note)
		if !anchors.KindValid(d.Kind) {
			fe.add("/kind", anchors.CodeInvalidValue, "the kind is one of ca, openpgp, public-key and sigstore")
		} else if len(fe) == 0 {
			s.validateMaterial(&fe, d.Kind, d.Material)
		}
		if len(fe) > 0 {
			return nil, fmt.Errorf("signatures.anchors: anchor %q: %s %s", d.Key, fe[0].Pointer, fe[0].Detail)
		}
		if seen[d.Key] {
			return nil, fmt.Errorf("signatures.anchors: anchor %q is declared twice", d.Key)
		}
		seen[d.Key] = true
		m, err := encodeMaterial(d.Material)
		if err != nil {
			return nil, fmt.Errorf("signatures.anchors: anchor %q: %w", d.Key, err)
		}
		out = append(out, m)
	}
	return out, nil
}

// applyDeclaration stores one declared anchor and records what changed; it reports whether anything did.
func (s *Store) applyDeclaration(ctx context.Context, qs *q.Queries, tx meta.Tx, actor audit.Actor, d Declaration, material json.RawMessage, cur declaredRow, exists bool) (bool, error) {
	if !exists {
		ids, err := qs.SignatureAnchorInsert(ctx, q.SignatureAnchorInsertParams{
			Scope: anchors.ScopeConfiguration, Key: d.Key, Title: d.Title, Kind: d.Kind, Material: material, Enabled: true, Note: d.Note,
		})
		if err != nil || len(ids) == 0 {
			return false, err
		}
		rec, err := s.anchorRecord(ctx, qs, ids[0])
		if err != nil {
			return false, err
		}
		return true, s.o.Audit.RecordTx(ctx, tx, createdEvent(actor, rec))
	}
	if cur.Title == d.Title && cur.Kind == d.Kind && cur.Note == d.Note && cur.Enabled && sameMaterial(cur.Material, material) {
		return false, nil
	}
	if _, err := qs.SignatureAnchorUpdate(ctx, q.SignatureAnchorUpdateParams{
		ID: cur.ID, Title: d.Title, Kind: d.Kind, Material: material, Enabled: true, Note: d.Note,
	}); err != nil {
		return false, err
	}
	before := anchorRow{
		ID: cur.ID, Scope: anchors.ScopeConfiguration, Key: cur.Key, Title: cur.Title, Kind: cur.Kind,
		Material: cur.Material, Enabled: cur.Enabled, Note: cur.Note,
	}
	after := before
	after.Title, after.Kind, after.Material, after.Enabled, after.Note = d.Title, d.Kind, material, true, d.Note
	return true, s.o.Audit.RecordTx(ctx, tx, updatedEvent(actor, before, after, ""))
}
