package repository

import (
	"context"

	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// The repositories of the built-in project upstream (R-REPO-066 to R-REPO-073, ADR-209): what a repository of it carries beside
// the read model of every repository. The names, the kinds, the states and the declarations are internal/wellknown's.

// WarningMemberInactive is the warning of a write of a virtual repository whose member list or pin rules name a well-known
// repository that is not active: the member is accepted and takes no part until an administrator switches it on (R-REPO-071,
// R-API-083). Its detail names the member and its state.
const WarningMemberInactive = "member_inactive"

// UpstreamFacts are the facts of a repository of the built-in project upstream, read from its row of upstream_repository with every
// read of the repository (the snapshot, the lookup of a key the snapshot lacks, the management reads). Repository.Upstream is nil
// for every repository of another project.
type UpstreamFacts struct {
	// Kind is preset or system (R-REPO-066).
	Kind wellknown.Kind
	// State is the state of a preset repository derived from what is stored (R-REPO-068, Q-REPO-375); empty for a system
	// repository, which has none (R-REPO-070).
	State wellknown.State
	// ClientsEnabled is upstream_repository.clients_enabled: the switch "ClamAV definition updates" of upstream/clamav, which opens
	// the system repository to clients at its level (R-REPO-075, Q-REPO-414); false for every other repository.
	ClientsEnabled bool
}

// Retired reports whether the product no longer declares the repository (R-REPO-067 step 5): no new member names it
// (R-REPO-071).
func (f *UpstreamFacts) Retired() bool { return f != nil && f.State == wellknown.StateRetired }

// System reports whether the repository is a system repository of the product (scanner-trivy, clamav): it is never a member of a
// virtual repository (R-REPO-070, R-REPO-075, Q-REPO-380), and its items are never scanned nor asked about by the malware gate
// (R-MAL-001, Q-MAL-100).
func (f *UpstreamFacts) System() bool { return f != nil && f.Kind == wellknown.KindSystem }

// Closed reports whether the repository answers no client: a system repository, unless an administrator switched its clients on
// (upstream/clamav, R-REPO-075). A closed repository answers every protocol request as one that does not exist, for every caller.
func (f *UpstreamFacts) Closed() bool { return f.System() && !f.ClientsEnabled }

// RefusesHandDelete reports whether no caller deletes an item of the repository (Q-REPO-416): a local system repository of the
// project upstream, which only the product writes and cleans (upstream/clamav). scanner-trivy is a remote, whose cached items a holder
// of delete removes.
func (r Repository) RefusesHandDelete() bool { return r.Upstream.System() && r.Class == Local }

// upstreamFactsByID reads every row of upstream_repository, keyed by repository ID.
func upstreamFactsByID(ctx context.Context, qs *q.Queries) (map[int64]q.UpstreamRepository, error) {
	rows, err := qs.ListUpstreamRepositoryFacts(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]q.UpstreamRepository, len(rows))
	for _, row := range rows {
		out[row.RepositoryID] = row
	}
	return out, nil
}

// upstreamFactsOf reads the row of one repository, keyed as upstreamFactsByID keys it; an empty map for a repository without one.
func upstreamFactsOf(ctx context.Context, qs *q.Queries, id int64) (map[int64]q.UpstreamRepository, error) {
	rows, err := qs.GetUpstreamRepositoryFacts(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]q.UpstreamRepository, len(rows))
	for _, row := range rows {
		out[row.RepositoryID] = row
	}
	return out, nil
}

// withUpstream sets r.Upstream from the row of r in facts, and leaves it nil for a repository without one; the database keeps the
// rows to repositories of the project upstream (AI010), and so does this. The state is derived from r.Disabled, which a stored
// configuration that does not load also sets (fail closed).
func withUpstream(r *Repository, facts map[int64]q.UpstreamRepository) {
	row, ok := facts[r.rid]
	if !ok || !wellknown.IsProject(r.Name.Project) {
		r.Upstream = nil
		return
	}
	kind := wellknown.Kind(row.Kind)
	r.Upstream = &UpstreamFacts{
		Kind: kind, State: wellknown.StateOf(kind, r.Disabled, row.FirstSwitchedOnAt.Valid, row.RetiredAt.Valid),
		ClientsEnabled: kind == wellknown.KindSystem && row.ClientsEnabled,
	}
}
