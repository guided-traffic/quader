package authz

import (
	"context"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/qname"

	"github.com/guided-traffic/quader/internal/repository"
)

// The reach of a subject on a repository: whether the subject may read the whole of it, something of it, or
// nothing (R-BAS-012). It is the question the main installation answers about a caller for a Bastion: a full
// reader may be handed a document that is the same for every full reader, and every artifact of the
// repository, without a question per item. The rules are those of the decision point, so a full reach never
// meets a refused read and a none reach never an allowed one; TestR_BAS_012_ShapeAgreesWithTheDecisionPoint
// checks both against evaluate over the fixtures of this package.

// Reach is how much of a repository a subject may read.
type Reach int

const (
	// ReachNone: the subject may read nothing through the repository.
	ReachNone Reach = iota
	// ReachPartial is every other case in which the subject may read something.
	ReachPartial
	// ReachFull: the subject may read every package and every version reachable through the repository, and
	// every artifact; for a remote repository the subject may also populate it (R-AUTHZ-008).
	ReachFull
)

// String names the reach for logs and test failures.
func (r Reach) String() string {
	switch r {
	case ReachFull:
		return "full"
	case ReachPartial:
		return "partial"
	}
	return "none"
}

// ReachResult is the reach of one repository key and, for ReachNone, the outcome a read of the repository
// would be refused with.
type ReachResult struct {
	Reach Reach
	// Denial is DenyUnauthenticated, DenyVisible or DenyHidden when Reach is ReachNone: the refusal a read of
	// the repository by this subject gets (R-AUTHZ-016). It is zero (Allow) otherwise.
	Denial Outcome
}

// Reacher is the form of the decision point that classifies repositories by reach. The engine implements
// it next to [Decider].
type Reacher interface {
	// ReachOn classifies keys for the subject on the request's index. A key the index does not know is
	// ReachNone with the refusal of a missing repository. An error is a refusal to decide (503, 412).
	ReachOn(ctx context.Context, idx *Index, sub Subject, keys []qname.Repo) (map[qname.Repo]ReachResult, error)
}

var _ Reacher = (*Engine)(nil)

// maxReachDepth bounds the recursion through nested virtual repositories, as Index.reaches does.
const maxReachDepth = 16

// ReachOn implements [Reacher].
//
//   - A local or remote repository is full when the subject's read shape of its packages is [ReadAll] with
//     the capability content:metadata-read left out (it never reads an artifact, Q-AUTHZ-172) and, for a
//     remote repository, the populate check of R-AUTHZ-008 passes, and the subject reads the repository's wiki (R-BAS-012); it is none
//     when the shape is [ReadNone]
//     or the endpoint does not expose the repository; partial otherwise. An endpoint with exposure rules or a
//     token scope that narrows makes the shape [ReadRows], so the reach is partial at most.
//   - A virtual repository is none when the read gate of R-AUTHZ-009 rule 1 refuses (this includes the
//     anonymous rule 6), and none when no member is readable; full when the gate also admits an artifact read
//     and every member, recursively, is full on the request's terms (the virtual repository is the addressed
//     one: the exposure and the token scope are decided through it, Q-EP-11, Q-AUTHZ-163; the gate of a nested
//     virtual repository is asked as a member's read); partial otherwise.
//   - An anonymous subject is none until a visibility level admits it.
func (e *Engine) ReachOn(_ context.Context, idx *Index, sub Subject, keys []qname.Repo) (map[qname.Repo]ReachResult, error) {
	out := make(map[qname.Repo]ReachResult, len(keys))
	c := e.newReachCtx(idx, sub)
	for _, k := range keys {
		out[k] = e.reachOf(c, k)
	}
	return out, nil
}

type reachCtx struct {
	st, art     *evalState
	allEndpoint bool
}

// newReachCtx derives what the reach of a subject is decided on, once for every key it is asked about.
func (e *Engine) newReachCtx(idx *Index, sub Subject) *reachCtx {
	st := e.newEvalState(idx, sub)
	// The same state without content:metadata-read: the shape of what the grants alone let the subject read,
	// which is what an artifact read is decided on.
	art := *st
	art.metadata = false
	return &reachCtx{st: st, art: &art, allEndpoint: e.exposesEverything(sub.Endpoint)}
}

// reachNone builds the refusal a read of key gets, from the decision point itself.
func (e *Engine) reachNone(c *reachCtx, key qname.Repo) ReachResult {
	d := e.evaluate(c.st.idx, c.st.sub, Coordinate{Repository: key, Kind: KindMetadata, Actions: []Action{Read}})
	if d.Outcome == Allow {
		// Allowed at the gate and still nothing readable: the packages are what is refused, which a read
		// answers as a missing one.
		return ReachResult{Reach: ReachNone, Denial: DenyHidden}
	}
	return ReachResult{Reach: ReachNone, Denial: d.Outcome}
}

func (e *Engine) reachOf(c *reachCtx, key qname.Repo) ReachResult {
	idx, sub := c.st.idx, c.st.sub
	r := idx.repo(key)
	if r == nil {
		return e.reachNone(c, key)
	}
	if r.Class == repository.Virtual {
		return e.reachVirtual(c, r)
	}
	if len(e.ExposedOn(idx, sub, []qname.Repo{key})) == 0 {
		return e.reachNone(c, key)
	}
	switch e.leafReach(c, r, qname.Repo{}) {
	case ReachNone:
		return e.reachNone(c, key)
	case ReachFull:
		return ReachResult{Reach: ReachFull}
	}
	return ReachResult{Reach: ReachPartial}
}

// leafReach classifies a local or remote repository on the request's terms; addressed is the virtual
// repository the client named, "" for a request on the repository itself.
func (e *Engine) leafReach(c *reachCtx, r *repoInfo, addressed qname.Repo) Reach {
	sub := c.st.sub
	if r.Class == repository.Virtual {
		return ReachNone
	}
	if e.readShape(c.st, sub, r, c.allEndpoint) == ReadNone {
		return ReachNone
	}
	if e.readShape(c.art, sub, r, c.allEndpoint) != ReadAll {
		return ReachPartial
	}
	// A full read includes the wiki: a document shared between callers of a full read never carries a README to a caller without it
	// (R-BAS-012, Q-BAS-149). A subject that reads every artifact and not the wiki is partial. The wiki is read in full only where its
	// shape is ReadAll: the question about the repository alone is allowed by a grant or a token scope narrowed by package, which a
	// coordinate without a package matches (R-AUTHZ-005), so it would let the level of the artifacts stand for the wiki's.
	wiki := Coordinate{Repository: r.Name, Kind: KindMetadata, Actions: []Action{Read}, Part: PartWiki, Addressed: addressed}
	if e.readShapeOf(c.st, sub, r, c.allEndpoint, PartWiki) != ReadAll || e.evaluateWith(c.st.idx, sub, c.st, wiki).Outcome != Allow {
		return ReachPartial
	}
	if r.Class == repository.Remote {
		d, _ := e.populate(c.st.idx, sub, Coordinate{
			Repository: r.Name, Kind: KindArtifact, Actions: []Action{Read}, Addressed: addressed,
		})
		if d.Outcome != Allow {
			return ReachPartial
		}
		// The fill of a frozen mirror needs its action on every package and path: the question about the repository alone is
		// allowed by a deploy grant narrowed by package or path, whatever its patterns say.
		if a, _ := populateAction(r); a != Read && !e.actsEverywhere(c.st, sub, r, a) {
			return ReachPartial
		}
	}
	return ReachFull
}

// actsEverywhere reports whether the subject holds the action on every package and every path of the repository: content:bypass,
// or the repository's account, a project role or a grant without a pattern that gives it (a contribution's grant or inline grant
// without a pattern for a CI subject), under a token scope with an element that admits it on the repository without a pattern. It
// never answers true where a decision of the action on some package or path could refuse. The populate check of a frozen mirror
// asks it: a coordinate without a package and a path matches every grant whatever its patterns say (R-AUTHZ-005).
func (e *Engine) actsEverywhere(st *evalState, sub Subject, r *repoInfo, a Action) bool {
	if st.bypass {
		return true
	}
	if !e.scopeAdmitsEverywhere(sub.Scope, a, r) {
		return false
	}
	if st.isCI {
		return e.ciActsEverywhere(st.ci, st.idx, r, a)
	}
	if roleActions(st.roleIn(r.proj), r).Has(a) {
		return true
	}
	if g, ok := st.idx.accountGrantOf(st.sub, r.ID); ok && g.actions.Has(a) {
		return true
	}
	for _, p := range st.pset {
		for _, g := range st.idx.grantsFor(r.ID, p) {
			if g.actions.Has(a) && g.res.unrestrictedPackages() {
				return true
			}
		}
	}
	return false
}

// scopeAdmitsEverywhere reports whether a token scope admits the action on every package and path of the repository: no scope,
// inherit, or an element without package and path patterns that names the repository and lists the action.
func (e *Engine) scopeAdmitsEverywhere(sc *scope.Scope, a Action, r *repoInfo) bool {
	if sc == nil || sc.Inherit {
		return true
	}
	for _, el := range sc.Repositories {
		acts, bad := ParseActions(el.Actions)
		if len(bad) > 0 || !acts.Has(a) || len(el.Packages.Include)+len(el.Packages.Exclude)+len(el.Paths.Include)+len(el.Paths.Exclude) > 0 {
			continue
		}
		if e.compile.selectorsMatch(el.Repositories, instanceContext, r.repoView) {
			return true
		}
	}
	return false
}

func (e *Engine) reachVirtual(c *reachCtx, v *repoInfo) ReachResult {
	idx, sub := c.st.idx, c.st.sub
	gate := e.evaluate(idx, sub, Coordinate{Repository: v.Name, Kind: KindMetadata, Actions: []Action{Read}})
	if gate.Outcome != Allow {
		return ReachResult{Reach: ReachNone, Denial: gate.Outcome}
	}
	artGate := e.evaluate(idx, sub, Coordinate{Repository: v.Name, Kind: KindArtifact, Actions: []Action{Read}})
	all, any := true, false
	var walk func(r *repoInfo, depth int)
	walk = func(r *repoInfo, depth int) {
		if depth > maxReachDepth {
			all = false
			return
		}
		for _, mk := range r.members {
			m := idx.repo(mk)
			switch {
			case m == nil:
				all = false
			case m.Class == repository.Virtual:
				// A nested virtual repository is a member like another: the resolver skips it when its own gate
				// refuses the read as a read through the addressed repository (MemberUsable, R-AUTHZ-015).
				nested := Coordinate{Repository: m.Name, Kind: KindMetadata, Actions: []Action{Read}, Addressed: v.Name}
				if e.evaluate(idx, sub, nested).Outcome != Allow {
					all = false
					continue
				}
				nested.Kind = KindArtifact
				if e.evaluate(idx, sub, nested).Outcome != Allow {
					all = false
				}
				walk(m, depth+1)
			default:
				switch e.leafReach(c, m, v.Name) {
				case ReachFull:
					any = true
				case ReachPartial:
					any, all = true, false
				default:
					all = false
				}
			}
		}
	}
	walk(v, 0)
	switch {
	case !any:
		return ReachResult{Reach: ReachNone, Denial: DenyHidden}
	case all && artGate.Outcome == Allow:
		return ReachResult{Reach: ReachFull}
	}
	return ReachResult{Reach: ReachPartial}
}
