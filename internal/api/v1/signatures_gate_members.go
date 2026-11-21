package v1

import (
	"context"
	"strings"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/files"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/signatures"
	sgate "github.com/guided-traffic/quader/internal/signatures/gate"
	"github.com/guided-traffic/quader/internal/signatures/gateset"
)

// The members of stage 3 on the answers of R-SIG-034 (R-SIG-073): refused and refusedReason on the member signatures of a stored file,
// of a version and of the rows of the version list, and gate on the signatures answer of a file and of a version per repository. Each is
// what the repository's own gate decides now (sgate.Gate.Assess), with the settings and anchors as they are; absent while no gate
// applies.

// gateFile is a stored file one repository's gate judges: the repository that holds it (the repository itself, or a member of a virtual
// one), its path and its blob.
type gateFile struct {
	holder repository.Repository
	path   string
	blob   [32]byte
}

// gateApplies reports whether the repository's own gate is on and judged: the signatures are on, its member is on, and it is no system
// repository of upstream (R-SIG-064).
func gateApplies(sv SignatureServices, repo repository.Repository) bool {
	return sv.Enabled && sv.Gate.Enabled() && repo.SignatureGate().OnlyVerified && !repo.Upstream.System()
}

// gateJudge judges files in the repository's own gate; members are a virtual repository's holders whose attachments its gate reads. ok is
// false while no gate applies.
func gateJudge(ctx context.Context, sv SignatureServices, repo repository.Repository, members []repository.Repository, fs []gateFile) ([]sgate.Assessed, bool, error) {
	if !gateApplies(sv, repo) || len(fs) == 0 {
		return nil, false, nil
	}
	a := sgate.Assessment{Repo: files.GateRepo(repo), Items: make([]sgate.AssessItem, len(fs))}
	for _, m := range members {
		a.Members = append(a.Members, files.GateRepo(m))
	}
	for i, f := range fs {
		a.Items[i] = sgate.AssessItem{Holder: files.GateRepo(f.holder), Path: f.path, Blob: f.blob}
	}
	got, err := sv.Gate.Assess(ctx, a)
	return got, err == nil, err
}

// gateSummary is the decision of a set of files (a version): refused when one artifact is, with the first one's reason; else pending when
// one is; else pass, by a release when one artifact passed by it, else by the first artifact's signature (none when nothing is an
// artifact).
type gateSummary struct {
	outcome   sgate.Outcome
	reason    sgate.Reason
	released  bool
	signature int64
}

func summarize(as []sgate.Assessed) gateSummary {
	out := gateSummary{outcome: sgate.Pass}
	for _, a := range as {
		if !a.Artifact {
			continue
		}
		switch a.Outcome {
		case sgate.Refused:
			if out.outcome != sgate.Refused {
				out = gateSummary{outcome: sgate.Refused, reason: a.Reason}
			}
		case sgate.Pending:
			if out.outcome == sgate.Pass {
				out = gateSummary{outcome: sgate.Pending}
			}
		default:
			if a.Released {
				out.released = true
			} else if out.signature == 0 {
				out.signature = a.Signature
			}
		}
	}
	if out.outcome != sgate.Pass || out.released {
		out.signature = 0
	}
	out.released = out.released && out.outcome == sgate.Pass
	return out
}

// refusedMember is the pair refused and refusedReason of a summary: the reason of a refusal, "pending" for one that waits, none for a
// pass.
func (g gateSummary) refusedMember() (*bool, *string) {
	refused := g.outcome == sgate.Refused
	switch g.outcome {
	case sgate.Refused:
		r := string(g.reason)
		return &refused, &r
	case sgate.Pending:
		r := refusedPending
		return &refused, &r
	}
	return &refused, nil
}

// gateMemberOf is the member refused of one repository's files: nil, nil while no gate applies.
func gateMemberOf(ctx context.Context, sv SignatureServices, repo repository.Repository, members []repository.Repository, fs []gateFile) (*bool, *string, error) {
	got, ok, err := gateJudge(ctx, sv, repo, members, fs)
	if err != nil || !ok {
		return nil, nil, err
	}
	refused, reason := summarize(got).refusedMember()
	return refused, reason, nil
}

// gateViewRep is the member gate of an entry: the accepted anchors and the summary; nil while no gate applies.
func gateViewRep(ctx context.Context, sv SignatureServices, repo repository.Repository, members []repository.Repository, fs []gateFile) (*gen.SignatureGateView, error) {
	if !gateApplies(sv, repo) {
		return nil, nil
	}
	got, _, err := gateJudge(ctx, sv, repo, members, fs)
	if err != nil {
		return nil, err
	}
	anchors, err := gateAnchors(ctx, sv, repo)
	if err != nil {
		return nil, err
	}
	sum := summarize(got)
	out := &gen.SignatureGateView{OnlyVerified: true, Anchors: anchors, Decision: gen.SignatureGateViewDecision("pass")}
	switch sum.outcome {
	case sgate.Refused:
		r := gen.SignatureGateReason(sum.reason)
		out.Decision, out.Reason = gen.SignatureGateViewDecision("refused"), &r
	case sgate.Pending:
		out.Decision = gen.SignatureGateViewDecision("pending")
	default:
		switch {
		case sum.released:
			out.PassedBy = &gen.SignatureGatePassedBy{Kind: gen.SignatureGatePassedByKind("release")}
		case sum.signature != 0:
			id := sum.signature
			out.PassedBy = &gen.SignatureGatePassedBy{Kind: gen.SignatureGatePassedByKind("signature"), Signature: &id}
		}
	}
	return out, nil
}

// gateAnchors renders the references of the repository's member signatures.anchors with the anchors they name in its scope; a reference
// that names none any more is removed (R-SIG-061).
func gateAnchors(ctx context.Context, sv SignatureServices, repo repository.Repository) ([]gen.SignatureGateAnchor, error) {
	refs := repo.SignatureGate().Anchors
	out := make([]gen.SignatureGateAnchor, 0, len(refs))
	if len(refs) == 0 {
		return out, nil
	}
	byRef := map[string]signatures.AnchorRecord{}
	recs, err := sv.Store.Anchors(ctx, signatures.AnchorQuery{})
	if err != nil {
		return nil, err
	}
	for _, ref := range refs {
		if strings.HasPrefix(ref, gateset.ScopeProject+"/") && repo.ProjectID != "" {
			project, err := sv.Store.Anchors(ctx, signatures.AnchorQuery{ProjectID: repo.ProjectID})
			if err != nil {
				return nil, err
			}
			recs = append(recs, project...)
			break
		}
	}
	for _, a := range recs {
		byRef[a.Scope+"/"+a.Key] = a
	}
	for _, ref := range refs {
		scope, _, _ := gateset.ParseRef(ref)
		e := gen.SignatureGateAnchor{Ref: ref, Scope: gen.SignatureAnchorScope(scope), Removed: true}
		if a, ok := byRef[ref]; ok {
			kind := gen.SignatureAnchorKind(a.Kind)
			e.Title, e.Kind, e.Removed = optionalText(a.Title), &kind, false
		}
		out = append(out, e)
	}
	return out, nil
}

// repositoryOf resolves a repository of the snapshot by name; ok is false for one gone since the answer read it.
func repositoryOf(ctx context.Context, sv SignatureServices, name qname.Repo) (repository.Repository, bool) {
	if sv.Repos == nil {
		return repository.Repository{}, false
	}
	r, err := sv.Repos.ByName(ctx, name)
	return r, err == nil
}
