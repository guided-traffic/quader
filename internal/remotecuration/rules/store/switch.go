package store

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
)

// The switches of a package and a version (remote-curation.md R-CUR-024) and the change of the delivery mode (R-CUR-025): a switch is
// one operation of the server, which decides against the rules as stored in its own transaction what the subject inherits and writes
// a rule only where the asked state departs from it (Q-CUR-200, Q-CUR-202); allow-stored writes one allowing package rule for every
// stored package the mode decides, before the mode changes to onlyAllowed (Q-CUR-204).

// errNoCompile is a rule set that does not compile: the store cannot have written it.
var errNoCompile = errors.New("rules: the stored rule set does not compile")

// canon is the format's folding of names and versions (R-CUR-006 "Canonical forms").
func (s *Store) canon(format string) rules.Canon {
	return rules.Canon{
		Name:    func(n string) (string, bool) { return s.o.Catalog.NormalizeName(format, n) },
		Version: func(v string) (string, bool) { return s.o.Catalog.CanonicalVersion(format, v) },
	}
}

func modeOf(m rc.Mode) rc.Mode {
	if !m.Valid() {
		return rc.AllExceptBlocked
	}
	return m
}

// compileSubject compiles what can decide one package and its versions (Q-CUR-207): the package's own rules and the sections, read in
// the transaction. A package's switch, a version's and the examination rows a write comes to deliver need nothing else, so their cost
// does not grow with the repository's other rules. It returns the package's rule rows too.
func (s *Store) compileSubject(ctx context.Context, qs *q.Queries, t Target, name string) (*rules.Set, []q.DeliveryRulesOfPackageRow, error) {
	own, err := qs.DeliveryRulesOfPackage(ctx, q.DeliveryRulesOfPackageParams{RepoID: t.ID, Name: name})
	if err != nil {
		return nil, nil, err
	}
	sections, err := qs.DeliveryRuleSections(ctx, t.ID)
	if err != nil {
		return nil, nil, err
	}
	rs := make([]rules.Rule, 0, len(own)+len(sections))
	for _, row := range sections {
		rs = append(rs, rules.Rule{Kind: rc.KindSection, Pattern: row.Pattern.String, Effect: rc.Effect(row.Effect)})
	}
	for _, row := range own {
		rs = append(rs, rules.Rule{Kind: rc.RuleKind(row.Kind), Name: row.Name.String, Version: row.Version.String, Effect: rc.Effect(row.Effect)})
	}
	set, err := rules.Compile(modeOf(t.Mode), 0, rs, s.o.Compile, s.canon(t.Format))
	if err != nil {
		return nil, nil, errNoCompile
	}
	return set, own, nil
}

// compileRepository compiles every rule of the repository: a section's write, and allow-stored.
func (s *Store) compileRepository(ctx context.Context, qs *q.Queries, t Target) (*rules.Set, error) {
	list, err := qs.DeliveryRulesOfRepository(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	rs := make([]rules.Rule, len(list))
	for i, row := range list {
		rs[i] = rules.Rule{Kind: rc.RuleKind(row.Kind), Pattern: row.Pattern.String, Name: row.Name.String, Version: row.Version.String, Effect: rc.Effect(row.Effect)}
	}
	set, err := rules.Compile(modeOf(t.Mode), 0, rs, s.o.Compile, s.canon(t.Format))
	if err != nil {
		return nil, errNoCompile
	}
	return set, nil
}

// SwitchInput is a switch as a request turns it (R-CUR-024): a package (Version "") or one version, the state asked for, and for a
// package whether its version rules are removed with it.
type SwitchInput struct {
	Name, Version string
	Delivered     bool
	ResetVersions bool
	Note          string
}

// Switched is the answer of a switch: the subject's decision after the write (a version's, or the package level's), the subject's
// rule after it (nil where it inherits), the rules it removed, and the rule set's revision.
type Switched struct {
	Decision rc.Decision
	Rule     *Stored
	Removed  []Stored
	Revision int64
}

// switchWrite collects what a switch writes, for its events and its journal entry.
type switchWrite struct {
	events  []audit.Event
	narrow  rules.Narrowing
	widened bool
	removed []Stored
}

func (w *switchWrite) event(t Target, c Caller, event, id string, r rules.Rule, previous rc.Effect, note, requestID string) error {
	ev, err := eventOf(t, c, event, id, r, previous, note, requestID)
	if err != nil {
		return err
	}
	w.events = append(w.events, ev)
	return nil
}

// removedRule records a removed rule: its event, and the narrowing of a removed allow or the widening of a removed block.
func (w *switchWrite) removedRule(t Target, c Caller, st Stored) error {
	w.removed = append(w.removed, st)
	if st.Effect == rc.Allow {
		w.narrow.AllowsRemoved = append(w.narrow.AllowsRemoved, rules.SubjectOfRule(st.Rule))
	} else {
		w.widened = true
	}
	return w.event(t, c, audit.EventRemoteRuleDeleted, st.ID, st.Rule, st.Effect, st.Note, st.RequestID)
}

// SwitchTx turns the switch of a package or a version in the caller's transaction (R-CUR-024): under the revision's row lock it reads
// the mode as stored and compiles the package's rules and the sections; a state that equals what the subject inherits (a version: the
// package level; a package: the sections, else the mode) removes the subject's own rule, the other state writes the subject's rule with
// that effect, replacing one of the other effect. With ResetVersions a package's version rules are removed in the same transaction.
// Every rule written or removed records its event; the blocks added and the allows removed record one repository.narrowed.
func (s *Store) SwitchTx(ctx context.Context, tx meta.Tx, t Target, c Caller, in SwitchInput) (Switched, error) {
	kind, effect := rc.KindPackage, rc.Block
	if in.Version != "" {
		kind = rc.KindVersion
		if in.ResetVersions {
			return Switched{}, errs.E(errs.Unprocessable, errs.CodeValidation, "resetVersions is for a package's switch; a version has no version rules.",
				errs.WithFields(errs.FieldError{Pointer: "/resetVersions", Code: errs.CodeNotApplicable, Detail: "a version has no version rules"}))
		}
	}
	if in.Delivered {
		effect = rc.Allow
	}
	r, err := s.Canonical(t, Input{Kind: kind, Name: in.Name, Version: in.Version, Effect: effect, Note: in.Note})
	if err != nil {
		return Switched{}, err
	}
	qs, err := queries(tx)
	if err != nil {
		return Switched{}, err
	}
	locked, err := qs.DeliveryRulesRaiseRevisionMode(ctx, t.ID)
	if err != nil {
		return Switched{}, err
	}
	t.Mode = modeOf(rc.Mode(locked.Mode))
	set, own, err := s.compileSubject(ctx, qs, t, r.Name)
	if err != nil {
		return Switched{}, err
	}
	inherited := set.LevelInherited(r.Name)
	if kind == rc.KindVersion {
		inherited = set.Inherited(r.Name, r.Version)
	}
	var have *q.DeliveryRulesOfPackageRow
	for i := range own {
		if rc.RuleKind(own[i].Kind) == kind && own[i].Version.String == r.Version {
			have = &own[i]
		}
	}
	var w switchWrite
	var ruleID string
	switch {
	case in.Delivered == inherited.Delivered && have != nil:
		// The asked state is what the subject inherits: its own rule goes (Q-CUR-200).
		if _, err := qs.DeliveryRuleDelete(ctx, q.DeliveryRuleDeleteParams{RepoID: t.ID, ID: have.ID.String()}); err != nil {
			return Switched{}, err
		}
		if err := w.removedRule(t, c, storedOf(q.DeliveryRuleGetRow(*have))); err != nil {
			return Switched{}, err
		}
	case in.Delivered != inherited.Delivered && have == nil:
		if err := s.withinLimits(ctx, qs, t.ID, kind, 1); err != nil {
			return Switched{}, err
		}
		ruleID = uuid.Must(uuid.NewV7()).String()
		if err := qs.DeliveryRuleInsert(ctx, q.DeliveryRuleInsertParams{
			ID: ruleID, RepoID: t.ID, Kind: string(kind), Name: r.Name, Version: r.Version, Effect: string(effect), Note: in.Note, CreatedBy: c.PrincipalID,
		}); err != nil {
			return Switched{}, err
		}
		if err := w.event(t, c, audit.EventRemoteRuleCreated, ruleID, r, "", in.Note, ""); err != nil {
			return Switched{}, err
		}
		w.wrote(r, "")
	case in.Delivered != inherited.Delivered:
		ruleID = have.ID.String()
		if rc.Effect(have.Effect) != effect {
			if err := qs.DeliveryRuleReplace(ctx, q.DeliveryRuleReplaceParams{ID: ruleID, Effect: string(effect), Note: in.Note}); err != nil {
				return Switched{}, err
			}
			if err := w.event(t, c, audit.EventRemoteRuleUpdated, ruleID, r, rc.Effect(have.Effect), in.Note, ""); err != nil {
				return Switched{}, err
			}
			w.wrote(r, rc.Effect(have.Effect))
		}
	}
	if in.ResetVersions {
		rows, err := qs.DeliveryRulesDeleteVersions(ctx, q.DeliveryRulesDeleteVersionsParams{RepoID: t.ID, Name: r.Name})
		if err != nil {
			return Switched{}, err
		}
		for _, row := range rows {
			st := storedOf(q.DeliveryRuleGetRow{
				ID: row.ID, Kind: row.Kind, Pattern: row.Pattern, Name: row.Name, Version: row.Version, Effect: row.Effect, Note: row.Note,
				RequestID: row.RequestID, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt,
			})
			if err := w.removedRule(t, c, st); err != nil {
				return Switched{}, err
			}
		}
	}
	out := Switched{Removed: w.removed, Revision: locked.DeliveryRulesRevision}
	if out.Removed == nil {
		out.Removed = []Stored{}
	}
	if len(w.events) > 0 {
		if err := s.applied(ctx, tx, qs, t, c, &w, r); err != nil {
			return Switched{}, err
		}
	}
	after, _, err := s.compileSubject(ctx, qs, t, r.Name)
	if err != nil {
		return Switched{}, err
	}
	out.Decision = after.Level(r.Name)
	if kind == rc.KindVersion {
		out.Decision = after.Decide(r.Name, r.Version)
	}
	if ruleID != "" {
		rows, err := qs.DeliveryRuleGet(ctx, q.DeliveryRuleGetParams{RepoID: t.ID, ID: ruleID})
		if err != nil {
			return Switched{}, err
		}
		if len(rows) > 0 {
			st := storedOf(rows[0])
			out.Rule = &st
		}
	}
	return out, nil
}

// wrote records a rule written with the effect, over a rule of the previous effect ("" for a new one): a block that was none narrows,
// an allow that was none widens.
func (w *switchWrite) wrote(r rules.Rule, previous rc.Effect) {
	if r.Effect == rc.Block && previous != rc.Block {
		w.narrow.BlocksAdded = append(w.narrow.BlocksAdded, rules.SubjectOfRule(r))
	}
	if r.Effect == rc.Allow && previous != rc.Allow {
		w.widened = true
	}
}

// applied records what a switch changed: the rules' events in batches, one repository.narrowed with every block added and allow
// removed, the examination rows of the package's versions it comes to deliver (R-CUR-016), and the change of the area repo_config.
func (s *Store) applied(ctx context.Context, tx meta.Tx, qs *q.Queries, t Target, c Caller, w *switchWrite, r rules.Rule) error {
	if err := audit.RecordAllTx(ctx, s.o.Recorder, tx, w.events); err != nil {
		return err
	}
	if !w.narrow.Empty() {
		if err := s.narrowed(ctx, tx, t, c, &w.narrow); err != nil {
			return err
		}
	}
	if w.widened {
		if err := s.examinationsDelivered(ctx, qs, t, rules.Rule{Kind: rc.KindPackage, Name: r.Name}); err != nil {
			return err
		}
	}
	return s.changed(ctx, tx, t)
}

// allowStoredBatch is the most names one insert of allow-stored carries.
const allowStoredBatch = 10000

// AllowStoredTx writes one allowing package rule for every package the repository stores whose package level the mode decides (no
// package rule, no section matches), in the caller's transaction (R-CUR-025): before the mode changes to onlyAllowed, so no client is
// refused between the two writes. In mode onlyAllowed it answers 409 conflict; where the rules would exceed proxy.delivery.max_rules,
// 422 too_many_rules and nothing is written. One statement inserts each batch of rules, every rule records its own event (Q-CUR-208),
// the revision rises once. No examination row changes: the mode delivers these packages already, and a version's block stays narrower.
func (s *Store) AllowStoredTx(ctx context.Context, tx meta.Tx, t Target, c Caller) (created int, revision int64, err error) {
	qs, err := queries(tx)
	if err != nil {
		return 0, 0, err
	}
	locked, err := qs.DeliveryRulesRaiseRevisionMode(ctx, t.ID)
	if err != nil {
		return 0, 0, err
	}
	t.Mode = modeOf(rc.Mode(locked.Mode))
	if t.Mode == rc.OnlyAllowed {
		return 0, 0, errs.E(errs.Conflict, errs.CodeConflict,
			"The repository delivers only what is allowed already; its stored packages are allowed before the mode changes to it.")
	}
	set, err := s.compileRepository(ctx, qs, t)
	if err != nil {
		return 0, 0, err
	}
	names, _, err := s.unruled(ctx, qs, t, set, -1)
	if err != nil {
		return 0, 0, err
	}
	if len(names) == 0 {
		return 0, locked.DeliveryRulesRevision, nil
	}
	if err := s.withinLimits(ctx, qs, t.ID, rc.KindPackage, len(names)); err != nil {
		return 0, 0, err
	}
	events := make([]audit.Event, 0, len(names))
	for start := 0; start < len(names); start += allowStoredBatch {
		batch := names[start:min(start+allowStoredBatch, len(names))]
		ids := make([]string, len(batch))
		for i, n := range batch {
			ids[i] = uuid.Must(uuid.NewV7()).String()
			ev, err := eventOf(t, c, audit.EventRemoteRuleCreated, ids[i], rules.Rule{Kind: rc.KindPackage, Name: n, Effect: rc.Allow}, "", "", "")
			if err != nil {
				return 0, 0, err
			}
			events = append(events, ev)
		}
		if err := qs.DeliveryRuleInsertMany(ctx, q.DeliveryRuleInsertManyParams{RepoID: t.ID, CreatedBy: c.PrincipalID, Ids: ids, Names: batch}); err != nil {
			return 0, 0, err
		}
	}
	if err := audit.RecordAllTx(ctx, s.o.Recorder, tx, events); err != nil {
		return 0, 0, err
	}
	if err := s.changed(ctx, tx, t); err != nil {
		return 0, 0, err
	}
	return len(names), locked.DeliveryRulesRevision, nil
}

// storedPage is the names one read of the stored packages carries.
const storedPage = 1000

// unruled reads the names of the packages the repository stores whose package level the mode decides, in name order and folded by
// the format's name rule, each once; at most limit names when limit is not negative, and whole is false when more were left.
func (s *Store) unruled(ctx context.Context, qs *q.Queries, t Target, set *rules.Set, limit int) (names []string, whole bool, err error) {
	seen := map[string]bool{}
	after := ""
	for {
		page, err := qs.DeliveryStoredPackagesPage(ctx, q.DeliveryStoredPackagesPageParams{RepoID: t.ID, AfterName: after, MaxRows: storedPage})
		if err != nil {
			return nil, false, err
		}
		for _, n := range page {
			folded, ok := s.o.Catalog.NormalizeName(t.Format, n)
			if !ok || seen[folded] || !set.ModeDecides(folded) {
				continue
			}
			if limit >= 0 && len(names) >= limit {
				return names, false, nil
			}
			seen[folded] = true
			names = append(names, folded)
		}
		if len(page) < storedPage {
			return names, true, nil
		}
		after = page[len(page)-1]
	}
}

// Summary is what a change of the mode says before it is saved (R-CUR-025): the counts of the rules, the bound, and the stored
// packages whose package level the mode decides.
type Summary struct {
	Mode                   rc.Mode
	Allow, Block, Sections int
	MaxRules               int
	// Unruled is the count of those packages, at most MaxRules+1 (Counted false when the read stopped there); Names the first 100.
	Unruled int
	Names   []string
	Counted bool
}

// summaryNames is the most names the summary carries.
const summaryNames = 100

// Summary reads the summary of a repository against the replica's rule set (R-CUR-025).
func (s *Store) Summary(ctx context.Context, t Target, set *rules.Set) (Summary, error) {
	allow, block, sections := set.Counts()
	out := Summary{Mode: set.Mode(), Allow: allow, Block: block, Sections: sections, MaxRules: s.o.MaxRules}
	err := s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		names, whole, err := s.unruled(ctx, qs, t, set, s.o.MaxRules+1)
		if err != nil {
			return err
		}
		out.Unruled, out.Counted = len(names), whole
		out.Names = names[:min(len(names), summaryNames)]
		return nil
	})
	return out, err
}
