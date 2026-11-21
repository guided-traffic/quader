package gate

import (
	"cmp"
	"context"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/signatures"
)

// The impact of a change of anchors, blocks or the switch before it is saved (signatures.md R-SIG-071, Q-SIG-90): how many artifacts of
// repositories whose gate is on stop passing. It is the gate's own decision twice, under the snapshot of now and under the snapshot the
// change would make, over the artifacts whose counting signatures the change can touch: whatever the gate subtracts at once (R-SIG-066),
// the count subtracts the same way, and a release or another counting signature that still passes an artifact keeps it out of the count.

// The changes of an anchor (R-SIG-071).
const (
	AnchorRemove  = "remove"
	AnchorDisable = "disable"
	AnchorNarrow  = "narrow"
)

// Change is one change before it is saved: an anchor removed, switched off or narrowed (its material changed); a block added; or project
// anchors switched off. Exactly one is set.
type Change struct {
	Anchor       int64
	AnchorChange string
	Block        *Block
	// ProjectAnchorsOff is the switch for project Owners turned off (R-SIG-019).
	ProjectAnchorsOff bool
}

// ImpactQuery is what the store reads candidates by: the trust rows naming one of AnchorIDs, the signatures carrying one of KeyIDs, or
// with Identity the signatures that carry a Sigstore identity.
type ImpactQuery struct {
	AnchorIDs []int64
	KeyIDs    []string
	Identity  bool
}

// Candidate is an artifact attached in a repository whose counting signatures a change can touch; Identity and OIDCIssuer are read for a
// block of an identity, which the gate matches itself.
type Candidate struct {
	RepoID     int64
	Subject    [32]byte
	Identity   string
	OIDCIssuer string
}

// Gated is a repository whose gate is on, with the repositories whose attachments its gate reads: itself, or a virtual repository's
// members.
type Gated struct {
	Repo    Repo
	Members []Repo
}

// RepositoryImpact is the count of one repository.
type RepositoryImpact struct {
	Name      qname.Repo
	Artifacts int
}

// Impact is the answer: the artifacts that stop passing and in how many repositories, at most MaxImpactRepositories of them the most
// first; Complete false when the candidates were cut at the limit (the counts are lower bounds); Upper when the change was counted as a
// removal (a narrowed material, an upper bound).
type Impact struct {
	Artifacts    int
	Repositories int
	ByRepository []RepositoryImpact
	Complete     bool
	Upper        bool
}

// MaxImpactRepositories is the most repositories an impact names.
const MaxImpactRepositories = 20

// Impact counts what the change makes the gates of the repositories refuse (R-SIG-071). An anchor or a block the snapshot does not know
// is no error here: the count is 0.
func (g *Gate) Impact(ctx context.Context, ch Change, gated []Gated, limit int) (Impact, error) {
	out := Impact{Complete: true, ByRepository: []RepositoryImpact{}}
	if !g.Enabled() || len(gated) == 0 {
		return out, nil
	}
	before, err := g.o.Store.GateSnapshot(ctx)
	if err != nil {
		return out, err
	}
	after, q, upper := before.with(ch)
	out.Upper = upper
	if len(q.AnchorIDs) == 0 && len(q.KeyIDs) == 0 && !q.Identity {
		return out, nil
	}
	views := make([]view, len(gated))
	var all []int64
	for i, gr := range gated {
		views[i] = viewOf(gr.Repo, gr.Members)
		all = append(all, views[i].repoIDs...)
	}
	slices.Sort(all)
	all = slices.Compact(all)
	cands, err := g.o.Store.GateImpact(ctx, q, all, limit+1)
	if err != nil {
		return out, err
	}
	if len(cands) > limit {
		cands, out.Complete = cands[:limit], false
	}
	if ch.Block != nil && ch.Block.Kind == signatures.BlockIdentity {
		cands = slices.DeleteFunc(cands, func(c Candidate) bool {
			return !signatures.BlockMatches(ch.Block.Kind, ch.Block.Value, ch.Block.Issuer, nil, c.OIDCIssuer, c.Identity)
		})
	}
	for _, vw := range views {
		n, err := g.lost(ctx, vw, before, after, cands)
		if err != nil {
			return out, err
		}
		if n > 0 {
			out.Artifacts += n
			out.ByRepository = append(out.ByRepository, RepositoryImpact{Name: vw.repo.Name, Artifacts: n})
		}
	}
	out.Repositories = len(out.ByRepository)
	slices.SortStableFunc(out.ByRepository, func(a, b RepositoryImpact) int {
		return cmp.Or(cmp.Compare(b.Artifacts, a.Artifacts), cmp.Compare(a.Name.String(), b.Name.String()))
	})
	if len(out.ByRepository) > MaxImpactRepositories {
		out.ByRepository = out.ByRepository[:MaxImpactRepositories]
	}
	return out, nil
}

// lost counts the candidates of one gate's view that pass under before and not under after, and that no release of the repository passes.
func (g *Gate) lost(ctx context.Context, vw view, before, after *Snapshot, cands []Candidate) (int, error) {
	var subjects [][32]byte
	for _, c := range cands {
		if slices.Contains(vw.repoIDs, c.RepoID) {
			subjects = append(subjects, c.Subject)
		}
	}
	subjects = unique(subjects)
	if len(subjects) == 0 {
		return 0, nil
	}
	rows, _, err := g.read(ctx, view{repo: vw.repo, repoIDs: vw.repoIDs}, subjects)
	if err != nil {
		return 0, err
	}
	bySubject := map[[32]byte][]Row{}
	for _, r := range rows {
		bySubject[r.Subject] = append(bySubject[r.Subject], r)
	}
	var chosenBefore, chosenAfter map[int64]bool
	if !vw.any {
		chosenBefore, chosenAfter = before.Chosen(vw.chosen, vw.repo.ProjectID), after.Chosen(vw.chosen, vw.repo.ProjectID)
	}
	var gone [][32]byte
	for _, s := range subjects {
		ds := [][32]byte{s}
		if Judge(before, chosenBefore, ds, bySubject[s], nil, false).Outcome != Pass {
			continue
		}
		if Judge(after, chosenAfter, ds, bySubject[s], nil, false).Outcome != Pass {
			gone = append(gone, s)
		}
	}
	if len(gone) == 0 {
		return 0, nil
	}
	released, err := g.o.Store.GateReleased(ctx, vw.repo.ID, gone)
	if err != nil {
		return 0, err
	}
	return len(gone) - len(released), nil
}

// with answers the snapshot the change makes, the query of its candidates, and whether the count is an upper bound. The block's epoch is
// above every row's, as a block created now is (R-SIG-066); a narrowed material counts as a removal.
func (s *Snapshot) with(ch Change) (*Snapshot, ImpactQuery, bool) {
	next := &Snapshot{Anchors: maps.Clone(s.Anchors), Blocks: slices.Clone(s.Blocks), ProjectAnchors: s.ProjectAnchors, Epoch: s.Epoch}
	var q ImpactQuery
	upper := false
	switch {
	case ch.Anchor != 0:
		a, ok := next.Anchors[ch.Anchor]
		if !ok {
			return next, q, false
		}
		q.AnchorIDs = []int64{ch.Anchor}
		switch ch.AnchorChange {
		case AnchorDisable:
			a.Enabled = false
			next.Anchors[ch.Anchor] = a
		case AnchorNarrow:
			upper = true
			delete(next.Anchors, ch.Anchor)
		default:
			delete(next.Anchors, ch.Anchor)
		}
	case ch.Block != nil:
		b := *ch.Block
		b.Epoch = math.MaxInt64
		next.Blocks = append(next.Blocks, b)
		if b.Kind == signatures.BlockIdentity {
			q.Identity = true
		} else {
			q.KeyIDs = keyForms(b.Value)
		}
	case ch.ProjectAnchorsOff:
		next.ProjectAnchors = false
		for id, a := range s.Anchors {
			if a.Scope == ScopeProject && s.inForce(a) {
				q.AnchorIDs = append(q.AnchorIDs, id)
			}
		}
		slices.Sort(q.AnchorIDs)
	}
	return next, q, upper
}

// keyForms are the spellings a stored key ID of the block's value may have: a hex fingerprint or key ID matches regardless of case
// (signatures.BlockMatches), and the gate's subtraction decides the match.
func keyForms(v string) []string {
	out := []string{v}
	for _, f := range []string{strings.ToLower(v), strings.ToUpper(v)} {
		if !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}
