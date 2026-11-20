package authz

import (
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/repository"
)

// The system subjects (R-AUTHZ-002): work of the product itself, of the principal kind system. None is a row of the principal
// table, a grant, a role, a membership or an owner record, and none is internal or external. The decision point decides each by a
// fixed rule of its own, without grants, policies, visibility levels or exposure rules, so no administrator's change can widen or
// narrow it (Q-OCI-228). A subject that has no rule here is refused everything.
//
// A further system subject is a constant, a rule function and one case in systemRuleOf.

// SystemImageScanner is the subject of a scan token: the scanner's pull of one image on the scan listener of packages
// (formats/oci.md R-OCI-037, architecture.md R-ARCH-037).
const SystemImageScanner = "image-scanner"

// systemRule decides one action of a system subject on a repository the index knows.
type systemRule func(sub Subject, r *repoInfo, a Action) bool

// systemRuleOf is the fixed rule of a system subject by its name.
func systemRuleOf(name string) (systemRule, bool) {
	switch name {
	case SystemImageScanner:
		return imageScannerReads, true
	}
	return nil, false
}

// imageScannerReads is the rule of system:image-scanner (R-AUTHZ-015, Q-OCI-228): read of the one repository its scan token
// names, while that repository is of the format oci, is local or remote and is not disabled; never populate, a write or a
// capability. A repository being deleted is not in the index's repositories the request may name. The level of the repository and
// the block of an image do not apply: the scan reads for the installation's own records (Q-VULN-166).
func imageScannerReads(sub Subject, r *repoInfo, a Action) bool {
	return a == Read && sub.SystemRepo == r.Name && r.Format == "oci" &&
		(r.Class == repository.Local || r.Class == repository.Remote) && !r.disabled
}

// isSystem reports whether the subject is a system subject.
func isSystem(sub Subject) bool { return sub.Kind == principal.KindSystem }

// decideSystem is the decision on a coordinate of a system subject: its rule for every action, nothing else. A refusal is the 404
// of a repository the caller cannot see (R-AUTHZ-016), whatever the repository's hideExistence says.
func (e *Engine) decideSystem(idx *Index, sub Subject, c Coordinate, actions []Action, epoch int64) Decision {
	r := idx.repo(c.Repository)
	rule, ok := systemRuleOf(sub.Name)
	for _, a := range actions {
		if r == nil || !ok || !rule(sub, r, a) {
			return Decision{Outcome: DenyHidden, Reason: ReasonNoGrant, Missing: a, Target: targetOf(c), Epoch: epoch}
		}
	}
	return Decision{Outcome: Allow, Reason: ReasonAllowSystem, Epoch: epoch, Target: targetOf(c), Sources: []string{"system:" + sub.Name}}
}
