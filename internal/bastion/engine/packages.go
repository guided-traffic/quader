package engine

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/answers"
	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// The answers per package of R-BAS-084 and R-BAS-085: the caller's shape on one package of a repository, recorded from a
// marking whose repository is the request's and from the check's packages, while the caller's shape on the repository is
// partial. A package answer full lets the caller read the entries whose last marking named that package, under the rules of
// a full answer on the repository: the four validity rules, the feed (token, principal and grants entries drop it; a
// content entry makes the entry stale, not the answer, Q-BAS-249), the entry's freshness. It is renewed by use only
// (Q-BAS-248).

// packageAnswer returns the caller's usable package answer for an entry of the repository repo whose package is pkg, or nil:
// none is needed for a caller whose shape on the repository is full, an entry without a package has none, and an entry of
// another repository than the request's is never judged by an answer on the request's (R-BAS-085 step 2).
func (e *engine) packageAnswer(ctx context.Context, q *rq, repo, pkg string, shape *answerVal) *answerVal {
	if pkg == "" || repo != q.repo || q.repo == "" || isFull(shape) {
		return nil
	}
	return e.answers.pkg(ctx, q.cred.key, q.repo, pkg)
}

// putPackage records the caller's package answer on (repo, pkg), obtained at o for the subject sub (R-BAS-084), and returns
// it; it records nothing for a name the interface does not carry. A registry token is full only where its unverified grant
// names the pair with pull (capPackage, Q-BAS-246), and its answer ends at the expiry its claims name when that is earlier
// than the one reported, so no answer of a registry token outlives the token whatever the main installation said (rule 2,
// never extended, also not in the grace period).
func (e *engine) putPackage(ctx context.Context, cred credential, repo, pkg string, read iface.Read, o time.Time, sub iface.Subject) *answerVal {
	if repo == "" || !iface.ValidPackage(pkg) {
		return nil
	}
	base := answerBase(answers.Package, o, sub, repo)
	if cred.registry && !cred.anonymous {
		if exp, ok := iface.RegistryTokenExpiry(cred.token); ok && (base.TokenExpiresAt == nil || exp.Before(*base.TokenExpiresAt)) {
			base.TokenExpiresAt = &exp
		}
	}
	v := &answerVal{Answer: base, Read: capPackage(cred, repo, pkg, read)}
	e.answers.put(ctx, answers.Package, packageKey(cred.key, repo, pkg), v)
	return v
}

// checkPackage returns the element of a check's packages for the pair the Bastion asked about, and whether there is one. The
// answer holds one element per requested pair in request order (R-BAS-083); the pair is compared all the same, so an element
// the Bastion did not ask for is never recorded.
func checkPackage(ans iface.CheckAnswer, repo, pkg string) (iface.PackageRead, bool) {
	for _, p := range ans.Packages {
		if p.Repository == repo && p.Package == pkg {
			return p, true
		}
	}
	return iface.PackageRead{}, false
}

// checkPackages are the packages of a check for the pair (repo, pkg): one element, or none for an entry without a package or
// with a name the main installation would refuse (it answers such a pair 422, which fails the whole check, Q-BAS-255).
func checkPackages(repo, pkg string) iface.CheckPackages {
	if pkg == "" || !iface.ValidPackage(pkg) {
		return nil
	}
	return iface.CheckPackages{{Repository: repo, Package: pkg}}
}
