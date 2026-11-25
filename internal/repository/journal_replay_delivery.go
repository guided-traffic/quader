package repository

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
)

// The replay of the delivery parts of repository.narrowed (remote-curation.md R-CUR-006 "Journal", R-CUR-021): the mode set to
// onlyAllowed again, the journaled blocks added and the journaled allows removed on the restored rule set. Every step removes what a
// remote delivers; a rule set that would exceed the bounds of a repository disables it instead (fail closed).

// checkDeliveryParts reads the delivery parts as a write of the rules or of the mode writes them; it returns why an entry is
// malformed.
func checkDeliveryParts(e narrowedEntry) string {
	if e.DeliveryMode != "" && e.DeliveryMode != string(rc.OnlyAllowed) {
		return "deliveryMode is not onlyAllowed"
	}
	if e.Rules == nil {
		return ""
	}
	if len(e.Rules.BlocksAdded)+len(e.Rules.AllowsRemoved)+len(e.Rules.RangesNarrowed) > rules.DefaultMaxRules*2 {
		return "the rules part is longer than a repository can hold"
	}
	for _, list := range [][]rules.Subject{e.Rules.BlocksAdded, e.Rules.AllowsRemoved} {
		for _, s := range list {
			if !s.Valid() {
				return "a subject of the rules part is not one of a rule"
			}
		}
	}
	for _, n := range e.Rules.RangesNarrowed {
		if !n.Valid() {
			return "a range of the rules part names no package or bounds nothing"
		}
	}
	return ""
}

// deliveryMode replays the part deliveryMode: remote.delivery.mode is onlyAllowed again, every other member of the section kept.
func (r *replayer) deliveryMode(mode string) {
	if mode == "" || r.class != Remote {
		return
	}
	var section map[string]json.RawMessage
	if raw, ok := r.doc["remote"]; ok && json.Unmarshal(raw, &section) != nil {
		r.suspend(partDeliveryMode)
		return
	}
	if section == nil {
		section = map[string]json.RawMessage{}
	}
	var delivery map[string]json.RawMessage
	if raw, ok := section["delivery"]; ok && json.Unmarshal(raw, &delivery) != nil {
		r.suspend(partDeliveryMode)
		return
	}
	if delivery == nil {
		delivery = map[string]json.RawMessage{}
	}
	var cur string
	_ = json.Unmarshal(delivery["mode"], &cur)
	if cur == string(rc.OnlyAllowed) {
		return
	}
	delivery["mode"], _ = json.Marshal(string(rc.OnlyAllowed))
	section["delivery"], _ = json.Marshal(delivery)
	r.setMember("remote", section)
	r.applied(partDeliveryMode)
}

// deliveryRules replays the part rules: each journaled block is the subject's rule (a missing rule added, an allow turned into a
// block), each journaled allow that the restored set still holds is removed. Beyond the bounds of a repository (the rules of
// proxy.delivery.max_rules at its default, 100 sections) the repository is disabled. A change raises the rule set's revision, so no
// rendition under the restored rules is answered as current.
func (r *replayer) deliveryRules(n *rules.Narrowing) error {
	if n.Empty() || r.class != Remote {
		return nil
	}
	changed := false
	for _, s := range n.AllowsRemoved {
		removed, err := r.removeAllow(s)
		if err != nil {
			return err
		}
		changed = changed || removed
	}
	for _, s := range n.BlocksAdded {
		rows, err := r.qs.DeliveryRuleBySubject(r.ctx, subjectParams(r.id, s))
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			if rows[0].Effect == string(rc.Block) {
				continue
			}
			if err := r.qs.DeliveryRuleReplace(r.ctx, q.DeliveryRuleReplaceParams{ID: rows[0].ID.String(), Effect: string(rc.Block), Note: rows[0].Note}); err != nil {
				return err
			}
			changed = true
			continue
		}
		counts, err := r.qs.DeliveryRuleCounts(r.ctx, r.id)
		if err != nil {
			return err
		}
		if counts.Rules >= rules.DefaultMaxRules || s.Kind == rc.KindSection && counts.Sections >= rules.MaxSections {
			r.suspend(partRules)
			return nil
		}
		if err := r.qs.DeliveryRuleInsert(r.ctx, q.DeliveryRuleInsertParams{
			ID: uuid.Must(uuid.NewV7()).String(), RepoID: r.id, Kind: string(s.Kind), Pattern: s.Pattern, Name: s.Name, Version: s.Version,
			Effect: string(rc.Block),
		}); err != nil {
			return err
		}
		changed = true
	}
	for _, rn := range n.RangesNarrowed {
		done, stop, err := r.narrowRange(rn)
		if err != nil || stop {
			return err
		}
		changed = changed || done
	}
	if !changed {
		return nil
	}
	if _, err := r.qs.DeliveryRulesRaiseRevision(r.ctx, r.id); err != nil {
		return err
	}
	r.touched = true // the document is written again, which tells repo_config: every replica rebuilds its rule set
	r.applied(partRules)
	return nil
}

func subjectParams(id int64, s rules.Subject) q.DeliveryRuleBySubjectParams {
	return q.DeliveryRuleBySubjectParams{RepoID: id, Kind: string(s.Kind), Pattern: s.Pattern, Name: s.Name, Version: s.Version}
}

// The replay of a range of a package's allow (remote-curation.md R-CUR-026, Q-CUR-225): a restore keeps the narrower of the range it
// restored and the range written after the restore point, and a removed allow with a range is removed only where its package level
// without it delivers nothing more: removing it where the sections or the mode deliver would widen what the repository delivers.

// replayedSet compiles the restored rules of the repository with its mode as the replay left it, to decide what a package inherits.
func (r *replayer) replayedSet() (*rules.Set, error) {
	rows, err := rulesOfRepository(r.ctx, r.qs, r.id)
	if err != nil {
		return nil, err
	}
	rs := make([]rules.Rule, len(rows))
	for i, s := range rows {
		rs[i] = s.rule()
	}
	return rules.Compile(r.deliveryModeOfDoc(), 0, rs, CompileSection, DeliveryCanon(r.cat, r.format))
}

// deliveryModeOfDoc is remote.delivery.mode of the document as the replay left it; allExceptBlocked where it says none.
func (r *replayer) deliveryModeOfDoc() rc.Mode {
	var section struct {
		Delivery struct {
			Mode rc.Mode `json:"mode"`
		} `json:"delivery"`
	}
	if raw, ok := r.doc["remote"]; ok {
		_ = json.Unmarshal(raw, &section)
	}
	if !section.Delivery.Mode.Valid() {
		return rc.AllExceptBlocked
	}
	return section.Delivery.Mode
}

// removeAllow replays one journaled removed allow: the restored allow of the subject is removed, unless it carries a range and the
// package level without it delivers (Q-CUR-225).
func (r *replayer) removeAllow(s rules.Subject) (bool, error) {
	rows, err := r.qs.DeliveryRuleBySubject(r.ctx, subjectParams(r.id, s))
	if err != nil {
		return false, err
	}
	if len(rows) == 0 || rows[0].Effect != string(rc.Allow) {
		return false, nil
	}
	if rows[0].FromVersion.Valid || rows[0].ToVersion.Valid {
		set, err := r.replayedSet()
		if err != nil {
			return false, nil //nolint:nilerr // a restored set that does not compile disables the repository when the snapshot reads it
		}
		if set.LevelInherited(s.Name).Delivered {
			return false, nil
		}
	}
	if _, err := r.qs.DeliveryRuleDelete(r.ctx, q.DeliveryRuleDeleteParams{RepoID: r.id, ID: rows[0].ID.String()}); err != nil {
		return false, err
	}
	return true, nil
}

// narrowRange replays one journaled range (Q-CUR-225): a restored allow of the package keeps the narrower of its range and the
// journaled one, and becomes a block where the two hold no version in common; a package without a rule of its own whose package level
// delivers gets an allow with the journaled range; a block stays. stop is true where the repository was disabled: the format has no
// order to compare the range by, or the rules would exceed the bounds of a repository.
func (r *replayer) narrowRange(n rules.RangeNarrowing) (changed, stop bool, err error) {
	view, ok := r.cat.VersionOrderOf(r.format)
	if !ok {
		r.suspend(partRules)
		return false, true, nil
	}
	o := rc.Order{View: view}
	journaled := n.Range()
	rows, err := r.qs.DeliveryRuleBySubject(r.ctx, subjectParams(r.id, rules.Subject{Kind: rc.KindPackage, Name: n.Name}))
	if err != nil {
		return false, false, err
	}
	if len(rows) > 0 {
		row := rows[0]
		if row.Effect != string(rc.Allow) {
			return false, false, nil
		}
		restored := rc.Range{From: row.FromVersion.String, To: row.ToVersion.String, ToInclusive: row.ToInclusive}.Normal()
		next := journaled
		if restored.Set() {
			next = restored.Intersect(o, journaled)
		}
		if next == restored {
			return false, false, nil
		}
		p := q.DeliveryRuleReplaceParams{ID: row.ID.String(), Effect: string(rc.Allow), Note: row.Note}
		if next.Empty(o) {
			p.Effect = string(rc.Block)
		} else {
			p.FromVersion, p.ToVersion, p.ToInclusive = next.From, next.To, next.ToInclusive
		}
		return true, false, r.qs.DeliveryRuleReplace(r.ctx, p)
	}
	set, err := r.replayedSet()
	if err != nil || !set.LevelInherited(n.Name).Delivered {
		return false, false, nil //nolint:nilerr // a restored set that does not compile disables the repository when the snapshot reads it
	}
	counts, err := r.qs.DeliveryRuleCounts(r.ctx, r.id)
	if err != nil {
		return false, false, err
	}
	if counts.Rules >= rules.DefaultMaxRules {
		r.suspend(partRules)
		return false, true, nil
	}
	return true, false, r.qs.DeliveryRuleInsert(r.ctx, q.DeliveryRuleInsertParams{
		ID: uuid.Must(uuid.NewV7()).String(), RepoID: r.id, Kind: string(rc.KindPackage), Name: n.Name, Effect: string(rc.Allow),
		FromVersion: journaled.From, ToVersion: journaled.To, ToInclusive: journaled.ToInclusive,
	})
}
