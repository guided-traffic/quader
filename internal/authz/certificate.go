package authz

import (
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// The certificate subject (R-AUTHZ-002, formats/clamav.md R-CLAMAV-010, Q-CLAMAV-9): a request without a token on an endpoint that
// takes the client certificate its listener verified as a credential (R-EP-010 item 5). Like a system subject it is never a row of the
// principal table, a grant, a role, a membership or an owner record, and never internal or external (R-AUTHZ-030). The decision point
// decides it by one fixed rule that no binding, policy or level of another repository widens: read of a repository that admits
// certificate readers while the part read is internal or public; never populate, a write or a capability. Unlike a system subject the
// endpoint's exposure bounds it (R-EP-013), and the level and the switch apply: at private and while the switch is off it reads nothing.

// isCertificate reports whether the subject is the certificate subject.
func isCertificate(sub Subject) bool { return sub.Kind == principal.KindCertificate }

// certificateReads is the fixed rule of the certificate subject on one action of a repository the index knows: read, of a repository
// that admits certificate readers, at the level internal or public of the part read.
func certificateReads(r *repoInfo, a Action, part Part) bool {
	if r == nil || a != Read || !r.certificateReaders {
		return false
	}
	level := r.artifacts
	if part == PartWiki {
		level = r.wiki
	}
	return level == LevelInternal || level == LevelPublic
}

// decideCertificate is the decision on a coordinate of the certificate subject: the endpoint's exposure first, as for every subject
// (R-EP-013), then its rule for every action. A refusal is the 404 of a repository the caller cannot see (R-AUTHZ-016).
func (e *Engine) decideCertificate(idx *Index, sub Subject, c Coordinate, actions []Action, epoch int64) Decision {
	if d, refused := e.ceilingRefuses(idx, sub, c, actions, epoch); refused {
		return d
	}
	r := idx.repo(c.Repository)
	for _, a := range actions {
		if !certificateReads(r, a, c.Part) {
			return Decision{Outcome: DenyHidden, Reason: ReasonNoGrant, Missing: a, Target: targetOf(c), Epoch: epoch}
		}
	}
	return Decision{Outcome: Allow, Reason: ReasonAllowCertificate, Epoch: epoch, Target: targetOf(c), Sources: []string{sourceCertificate}}
}

// sourceCertificate is the decision source of the certificate subject's rule (R-AUTHZ-015, R-AUTHZ-029).
const sourceCertificate = "certificate"

// clientsKeys are the keys of the system repositories of the project upstream whose declaration lets clients in (wellknown.Declaration
// .Clients, R-REPO-075): while the switch of one is on, it admits certificate readers (Q-CLAMAV-13). Today that is clamav alone.
func (b *builder) clientsKeys() map[string]bool {
	out := map[string]bool{}
	for _, d := range wellknown.Declared(b.reg) {
		if d.Clients {
			out[d.Key] = true
		}
	}
	return out
}
