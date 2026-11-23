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
	if len(e.Rules.BlocksAdded)+len(e.Rules.AllowsRemoved) > rules.DefaultMaxRules*2 {
		return "the rules part is longer than a repository can hold"
	}
	for _, list := range [][]rules.Subject{e.Rules.BlocksAdded, e.Rules.AllowsRemoved} {
		for _, s := range list {
			if !s.Valid() {
				return "a subject of the rules part is not one of a rule"
			}
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
		rows, err := r.qs.DeliveryRuleBySubject(r.ctx, subjectParams(r.id, s))
		if err != nil {
			return err
		}
		if len(rows) == 0 || rows[0].Effect != string(rc.Allow) {
			continue
		}
		if _, err := r.qs.DeliveryRuleDelete(r.ctx, q.DeliveryRuleDeleteParams{RepoID: r.id, ID: rows[0].ID.String()}); err != nil {
			return err
		}
		changed = true
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
