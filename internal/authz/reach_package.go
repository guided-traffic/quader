package authz

import (
	"context"

	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// The read shape of a subject on one package of a repository (bastion.md R-BAS-080): the question the main installation answers
// about a caller for a Bastion that serves it one package of a repository, which a registry token and every reader narrowed by
// package read far more often than the whole repository. A package shape is full when the decision point would allow every read of
// the package through the repository - every version and every path of it, its metadata, its artifacts and its wiki, and for a
// remote repository its fill - and partial otherwise; a none shape on the repository is partial on every package (Q-BAS-243), so the
// wire tells no hidden package from a missing one. The rules are those of the decision point made per package (Q-BAS-240):
// TestR_BAS_080_PackageShapeAgreesWithTheDecisionPoint checks a full package shape against evaluate over the reach fixture.

// PackageReacher is the form of the decision point that classifies packages of one repository by reach. The engine implements it next
// to [Reacher].
type PackageReacher interface {
	// PackageReachOn classifies the packages, named as authz.Coordinate.Package carries them, of the repository key for the subject on
	// the request's index: ReachFull or ReachPartial for each, never ReachNone. An empty name is partial. A credential with a registry
	// grant is limited to its grant by the caller (authz.Subject holds no grant).
	PackageReachOn(ctx context.Context, idx *Index, sub Subject, repo qname.Repo, pkgs []string) (map[string]Reach, error)
}

var _ PackageReacher = (*Engine)(nil)

// PackageReachOn implements [PackageReacher].
//
//   - A full reach on the repository is full on every package; a none reach is partial on every package.
//   - A local or remote repository is full on P when all five hold: (1) the endpoint exposes everything; (2) no path pattern of the
//     subject's grants or token scope can tell two paths of the repository apart (readsByPath, decided without content:metadata-read,
//     which never reads an artifact); (3) the read of P is allowed as [ReadScope.MayRead] decides it on the package document's path,
//     without content:metadata-read; (4) the wiki of P is readable as [ReadScope.MayReadWiki] decides it; (5) for a remote repository,
//     the populate check of R-AUTHZ-008 passes for P. Conditions 2 and 3 together cover every version: a version is decided on the
//     package document's path and on its file's path (Q-AUTHZ-255), and without a path pattern both decisions come from the package
//     patterns alone. The fill is decided on P without a path, which a grant or a scope with a path pattern does not match, so a fill
//     narrowed by path is partial.
//   - A virtual repository is full on P when its read gate admits metadata and artifacts, and every member, recursively (as
//     reachVirtual), is full on P with the virtual repository as the addressed one: the gate of a nested virtual repository is asked as
//     a member's read, a member the index does not hold makes it partial.
func (e *Engine) PackageReachOn(_ context.Context, idx *Index, sub Subject, repo qname.Repo, pkgs []string) (map[string]Reach, error) {
	out := make(map[string]Reach, len(pkgs))
	if len(pkgs) == 0 {
		return out, nil
	}
	c := e.newReachCtx(idx, sub)
	whole := e.reachOf(c, repo).Reach
	r := idx.repo(repo)
	for _, p := range pkgs {
		switch {
		case whole == ReachFull:
			out[p] = ReachFull
		case whole == ReachNone || p == "" || r == nil:
			out[p] = ReachPartial
		case r.Class == repository.Virtual:
			out[p] = fullOr(e.virtualPackageFull(c, r, p))
		default:
			out[p] = fullOr(e.leafPackageFull(c, r, p, qname.Repo{}))
		}
	}
	return out, nil
}

func fullOr(full bool) Reach {
	if full {
		return ReachFull
	}
	return ReachPartial
}

// leafPackageFull is the rule of a local or remote repository for the package pkg; addressed is the virtual repository the client
// named, "" for a request on the repository itself.
func (e *Engine) leafPackageFull(c *reachCtx, r *repoInfo, pkg string, addressed qname.Repo) bool {
	idx, sub := c.st.idx, c.st.sub
	if r.Class == repository.Virtual || !c.allEndpoint || e.readsByPath(c.art, sub, r) {
		return false // conditions 1 and 2
	}
	read := []Action{Read}
	// Condition 3 on the state without content:metadata-read, condition 4 on the full state: the read scope's own rows, so the shape
	// is what a listing decides of the package (no path pattern applies, condition 2).
	art := &readScope{e: e, idx: idx, sub: sub, addressed: addressed, st: c.art, read: read, allEndpoint: true, byPath: map[qname.Repo]bool{r.Name: false}}
	if !art.MayRead(r.Name, pkg, "") {
		return false
	}
	wiki := &readScope{e: e, idx: idx, sub: sub, addressed: addressed, st: c.st, read: read, allEndpoint: true, byPath: map[qname.Repo]bool{r.Name: false}}
	if !wiki.MayReadWiki(r.Name, pkg, "") {
		return false
	}
	if r.Class == repository.Remote {
		// Condition 5: the fill of the package, decided without a path, so that a fill narrowed by path patterns is refused.
		d, _ := e.populate(idx, sub, Coordinate{Repository: r.Name, Package: pkg, Kind: KindArtifact, Actions: read, Addressed: addressed})
		if d.Outcome != Allow {
			return false
		}
	}
	return true
}

// virtualPackageFull is the rule of a virtual repository for the package pkg: its gate for metadata and for artifacts, and every
// member full on pkg as a read through it.
func (e *Engine) virtualPackageFull(c *reachCtx, v *repoInfo, pkg string) bool {
	for _, k := range []RequestKind{KindMetadata, KindArtifact} {
		if e.evaluate(c.st.idx, c.st.sub, Coordinate{Repository: v.Name, Kind: k, Actions: []Action{Read}}).Outcome != Allow {
			return false
		}
	}
	return e.membersFullOn(c, v, v, pkg, 0)
}

// membersFullOn reports whether every member of r, recursively, is full on pkg as a read through the addressed virtual repository v.
func (e *Engine) membersFullOn(c *reachCtx, v, r *repoInfo, pkg string, depth int) bool {
	if depth > maxReachDepth {
		return false
	}
	idx, sub := c.st.idx, c.st.sub
	for _, mk := range r.members {
		m := idx.repo(mk)
		switch {
		case m == nil:
			return false
		case m.Class == repository.Virtual:
			// A nested virtual repository is a member like another: the resolver skips it when its own gate refuses the read as a
			// read through the addressed repository (MemberUsable, R-AUTHZ-015).
			for _, k := range []RequestKind{KindMetadata, KindArtifact} {
				if e.evaluate(idx, sub, Coordinate{Repository: m.Name, Kind: k, Actions: []Action{Read}, Addressed: v.Name}).Outcome != Allow {
					return false
				}
			}
			if !e.membersFullOn(c, v, m, pkg, depth+1) {
				return false
			}
		default:
			if !e.leafPackageFull(c, m, pkg, v.Name) {
				return false
			}
		}
	}
	return true
}
