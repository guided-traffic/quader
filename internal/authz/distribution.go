package authz

import (
	"slices"

	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/qname"
)

// A Bastion's distribution is a ceiling of every decision through it (R-AUTHZ-043, Q-AUTHZ-342): a subject that carries a Bastion
// ([Subject.Bastion]: a forward, the caller of a check, the catalogue's renderer) reads only the repositories that Bastion
// distributes, the addressed one and the one the coordinate decides, whoever the subject is. Like an endpoint's exposure it binds
// administrators and content:bypass, it never grants, it refuses every action but read, and its refusal is the answer of a
// repository that does not exist. Lists, counts, shapes and a virtual repository's members follow it, so a member the Bastion does
// not distribute takes no part in the virtual repository's resolution through it (Q-AUTHZ-341).

// ReasonDistribution is the reason of a refusal by a Bastion's distribution (R-AUTHZ-043).
const ReasonDistribution = "deny.distribution"

// addDistribution puts the rows of repository_bastion on the repositories they name. A row whose repository the index does not
// know (deleted, or not active) is left out, which only hides content.
func (b *builder) addDistribution(rows []q.AuthzBuildDistributionRow) {
	for _, row := range rows {
		r := b.byID[row.RepositoryID]
		if r == nil || slices.Contains(r.bastions, row.BastionID) {
			continue
		}
		r.bastions = append(r.bastions, row.BastionID)
		if r.artifacts == LevelPublic {
			if b.x.publicThrough == nil {
				b.x.publicThrough = map[string]bool{}
			}
			b.x.publicThrough[row.BastionID] = true
		}
	}
	for _, r := range b.list {
		slices.Sort(r.bastions)
	}
}

// distributedThrough reports whether the subject's Bastion distributes the repository: always for a subject without one.
func (r *repoInfo) distributedThrough(bastion string) bool {
	return bastion == "" || slices.Contains(r.bastions, bastion)
}

// distributes reports whether the Bastion distributes the repository key: true for no Bastion, false for a key the index does
// not know.
func (x *Index) distributes(bastion string, key qname.Repo) bool {
	if bastion == "" {
		return true
	}
	r := x.repo(key)
	return r != nil && r.distributedThrough(bastion)
}

// PublicDistributed reports whether the Bastion registration (its principal ID) distributes some repository whose artifacts are
// public: the fact behind the handshake's anonymousReads of that registration (bastion.md R-BAS-010, Q-BAS-522). The default
// endpoint's ceiling (R-EP-010) is the caller's to apply.
func (x *Index) PublicDistributed(bastion string) bool { return x.publicThrough[bastion] }

// distributionRefuses applies the Bastion's distribution (R-AUTHZ-043) to a coordinate: the addressed repository and the
// repository the coordinate decides must both be distributed through the subject's Bastion, and only read passes. The refusal is the
// one a repository the index does not know gets for this subject (refusal with no repository), so neither its status nor its body
// tells a distributed repository from one that does not exist; only the reason differs, which no answer carries.
func (e *Engine) distributionRefuses(idx *Index, sub Subject, c Coordinate, actions []Action, epoch int64) (Decision, bool) {
	if sub.Bastion == "" {
		return Decision{}, false
	}
	refused := !idx.distributes(sub.Bastion, c.Repository) || (!c.Addressed.IsZero() && !idx.distributes(sub.Bastion, c.Addressed))
	missing := Read
	for _, a := range actions {
		if a != Read {
			refused, missing = true, a
			break
		}
	}
	if !refused {
		return Decision{}, false
	}
	d := e.refusal(sub, c, nil, ReasonNoGrant, missing, epoch)
	d.Reason = ReasonDistribution
	return d, true
}

// distributedKeys are the keys of keys the subject's Bastion distributes, in their order; every key for a subject without one.
func (idx *Index) distributedKeys(sub Subject, keys []qname.Repo) []qname.Repo {
	if sub.Bastion == "" {
		return keys
	}
	out := make([]qname.Repo, 0, len(keys))
	for _, k := range keys {
		if idx.distributes(sub.Bastion, k) {
			out = append(out, k)
		}
	}
	return out
}
