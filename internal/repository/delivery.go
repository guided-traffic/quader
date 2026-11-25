package repository

import (
	"context"
	"strconv"

	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
)

// The delivery rules in the repository snapshot (remote-curation.md R-CUR-006 "Propagation"): every replica's snapshot holds the
// compiled rule set of each remote repository, built from remote.delivery.mode of its document and the rows of remote_delivery_rules,
// so the decision of R-CUR-001 reads no database (R-ARCH-027). A write of a rule records the change area repo_config in its
// transaction, as a write of a virtual repository's member rows does, and every replica rebuilds its snapshot within the area's bound
// (H-132).

// storedRule is one rule row as the snapshot reads it, with the range of a package's allow (R-CUR-026).
type storedRule struct {
	kind, pattern, name, version, effect string
	rng                                  rc.Range
}

// rule is the row as the rule set compiles it.
func (s storedRule) rule() rules.Rule {
	return rules.Rule{Kind: rc.RuleKind(s.kind), Pattern: s.pattern, Name: s.name, Version: s.version, Effect: rc.Effect(s.effect), Range: s.rng}
}

// rangeOf is the range of a row's three columns (R-CUR-026).
func rangeOf(from, to string, inclusive bool) rc.Range {
	return rc.Range{From: from, To: to, ToInclusive: inclusive}.Normal()
}

// DeliveryCanon is what the rule set of a repository of the format folds names and versions with: the format's name rule and its
// version canon (R-CUR-006 "Canonical forms"), and its order of versions, which a range of a package's allow is compared by (R-CUR-026).
// The catalog's entry is read once here, so a decision reads no registry.
func DeliveryCanon(cat *catalog.Registry, format string) rules.Canon {
	e, ok := catalog.Or(cat).Lookup(format)
	c := rules.Canon{
		Name:    func(s string) (string, bool) { return s, true },
		Version: func(string) (string, bool) { return "", false },
	}
	if !ok {
		return c
	}
	if e.Names != nil {
		c.Name = e.Names.Normalize
	}
	if e.VersionCanon != nil {
		c.Version = e.VersionCanon.Canonical
	}
	c.Order = e.VersionOrder
	return c
}

// CompileSection compiles a folded section pattern with the one matcher of name patterns (Q-STOR-9).
func CompileSection(pattern string) (rules.Matcher, error) {
	p, err := CompilePattern(SubjectName, pattern)
	if err != nil {
		return nil, err
	}
	return p.Match, nil
}

// compileDelivery builds the rule set of a remote repository from its mode and rows; a repository of another class has none. A
// section that does not compile cannot have been written by the rules' store, and the repository is then loaded disabled (fail
// closed), as one whose patterns do not compile.
func compileDelivery(r *Repository, list []storedRule, cat *catalog.Registry) error {
	r.delivery = nil
	if r.Class != Remote {
		return nil
	}
	mode := rc.AllExceptBlocked
	if r.remote != nil {
		mode = rc.Mode(r.remote.Delivery.Mode)
	}
	rs := make([]rules.Rule, len(list))
	for i, s := range list {
		rs[i] = s.rule()
	}
	set, err := rules.Compile(mode, r.deliveryRevision, rs, CompileSection, DeliveryCanon(cat, r.Format))
	if err != nil {
		r.Disabled = true
		return err
	}
	r.delivery = set
	return nil
}

// WithRules returns the remote repository with the rule set the snapshot would compile from the mode and the rules, for the tests of
// the packages that consume the model without a database (the conformance kit, a format's own tests), as [Build] is. The rules are in
// their canonical forms. A repository of another class is returned unchanged.
func WithRules(r Repository, mode rc.Mode, list []rules.Rule, canon rules.Canon) (Repository, error) {
	if r.Class != Remote {
		return r, nil
	}
	set, err := rules.Compile(mode, r.deliveryRevision+1, list, CompileSection, canon)
	if err != nil {
		return r, err
	}
	r.delivery = set
	return r, nil
}

// rulesByRepository reads every rule row for the snapshot, grouped by repository.
func rulesByRepository(ctx context.Context, qs *q.Queries) (map[int64][]storedRule, error) {
	rows, err := qs.DeliveryRulesAll(ctx)
	if err != nil {
		return nil, err
	}
	out := map[int64][]storedRule{}
	for _, row := range rows {
		out[row.RepoID] = append(out[row.RepoID], storedRule{
			kind: row.Kind, pattern: row.Pattern.String, name: row.Name.String, version: row.Version.String, effect: row.Effect,
			rng: rangeOf(row.FromVersion.String, row.ToVersion.String, row.ToInclusive),
		})
	}
	return out, nil
}

// rulesOfRepository reads the rule rows of one repository.
func rulesOfRepository(ctx context.Context, qs *q.Queries, id int64) ([]storedRule, error) {
	rows, err := qs.DeliveryRulesOfRepository(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]storedRule, len(rows))
	for i, row := range rows {
		out[i] = storedRule{
			kind: row.Kind, pattern: row.Pattern.String, name: row.Name.String, version: row.Version.String, effect: row.Effect,
			rng: rangeOf(row.FromVersion.String, row.ToVersion.String, row.ToInclusive),
		}
	}
	return out, nil
}

// Decider is the decision of R-CUR-001 over the replica's snapshot ([remotecuration.Decider], Q-CUR-110): the repository's compiled
// rule set, looked up by ID. A repository the snapshot does not hold, and every repository that is not a remote, delivers everything:
// what is not a remote of this replica has no rules to refuse with.
type Decider struct {
	Reader Reader
}

var _ rc.Decider = Decider{}

func (d Decider) set(ctx context.Context, repoID string) *rules.Set {
	if d.Reader == nil {
		return nil
	}
	id, err := strconv.ParseInt(repoID, 10, 64)
	if err != nil {
		return nil
	}
	r, err := d.Reader.ByID(ctx, id)
	if err != nil {
		return nil
	}
	return r.Delivery()
}

// Decide implements [remotecuration.Decider].
func (d Decider) Decide(ctx context.Context, repoID, name, version string) rc.Decision {
	return d.set(ctx, repoID).Decide(name, version)
}

// Package implements [remotecuration.Decider].
func (d Decider) Package(ctx context.Context, repoID, name string) rc.Decision {
	return d.set(ctx, repoID).Package(name)
}
