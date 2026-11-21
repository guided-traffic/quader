package v1

import (
	"context"
	"encoding/hex"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/files"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/signatures"
	sgate "github.com/guided-traffic/quader/internal/signatures/gate"
)

// The refused downloads of the gates (R-SIG-073) and the impact of a change of anchors, blocks or the switch (R-SIG-071).

func signatureRefusalsSpec() ListSpec {
	return ListSpec{Filters: []string{"repository", "reason"}, Sorts: []string{"lastAt"}, DefaultSort: "-lastAt"}
}

// gateReasons are the reasons of a refusal (R-SIG-068), the filter's values.
func gateReasons() []string {
	return []string{
		string(sgate.ReasonMissing), string(sgate.ReasonUntrusted), string(sgate.ReasonNotChosen), string(sgate.ReasonInvalid),
		string(sgate.ReasonBlocked), string(sgate.ReasonNotChecked),
	}
}

// ListSignatureRefusals implements GET /api/v1/signatures/refusals. The scope is malware's (R-MAL-022): a holder of
// content:metadata-read reads every repository in full; a caller with read on some repositories sees the entries there whose path it
// may read.
func (s *Server) ListSignatureRefusals(w http.ResponseWriter, r *http.Request, _ gen.ListSignatureRefusalsParams) {
	ctx := r.Context()
	sv, err := s.signatureServices()
	if err == nil && sv.GateStore == nil {
		err = unavailable("The gate of the signatures")
	}
	var lq ListQuery
	if err == nil {
		lq, err = ParseList(r, signatureRefusalsSpec(), s.o.API.Config)
	}
	var rq signatures.RefusalsQuery
	if err == nil {
		rq, err = refusalsQuery(ctx, sv, lq)
	}
	var page Page[gen.SignatureRefusal]
	if err == nil {
		page, err = s.refusalsPage(ctx, sv, lq, rq)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	WritePage(w, r, page)
}

// refusalsQuery reads the filters and the cursor (the last refusal, the repository and the artifact of the last entry read).
func refusalsQuery(ctx context.Context, sv SignatureServices, lq ListQuery) (signatures.RefusalsQuery, error) {
	rq := signatures.RefusalsQuery{Limit: lq.FetchLimit(), Reason: lq.First("reason")}
	if rq.Reason != "" && !slices.Contains(gateReasons(), rq.Reason) {
		return rq, badParam("reason", "must be a reason of the gate")
	}
	if name := lq.First("repository"); name != "" {
		key, err := qname.ParseRepo(name)
		if err != nil {
			return rq, badParam("repository", "must be a repository name <project>/<key>")
		}
		if sv.Repos == nil {
			return rq, unavailable("The repository snapshot")
		}
		repo, err := sv.Repos.ByName(ctx, key)
		if err != nil {
			if errs.KindOf(err) == errs.NotFound {
				return rq, badParam("repository", "names no repository")
			}
			return rq, err
		}
		rq.RepoID = repo.NumericID()
	}
	if lq.After != nil {
		if len(lq.After) != 3 {
			return rq, invalidCursor()
		}
		at, err := time.Parse(time.RFC3339Nano, lq.After[0])
		repo, ierr := strconv.ParseInt(lq.After[1], 10, 64)
		subject, herr := hex.DecodeString(lq.After[2])
		if err != nil || ierr != nil || herr != nil || len(subject) != 32 {
			return rq, invalidCursor()
		}
		rq.AfterAt, rq.AfterRepo, rq.AfterSubject = at, repo, [32]byte(subject)
	}
	return rq, nil
}

func (s *Server) refusalsPage(ctx context.Context, sv SignatureServices, lq ListQuery, rq signatures.RefusalsQuery) (Page[gen.SignatureRefusal], error) {
	holders, err := sv.GateStore.RefusalHolders(ctx)
	if err != nil {
		return Page[gen.SignatureRefusal]{}, err
	}
	ctx, sc, err := s.malwareScopeOf(ctx, MalwareServices{Decider: sv.Decider}, holderRepos(holders))
	if err != nil {
		return Page[gen.SignatureRefusal]{}, err
	}
	rq.RepoIDs = append([]int64{}, sc.ids...)
	rows, err := sv.GateStore.Refusals(ctx, rq)
	if err != nil {
		return Page[gen.SignatureRefusal]{}, err
	}
	// The cursor is the last entry the store read, whether or not the caller sees it: a page of hidden entries must not end the list.
	var next *string
	if len(rows) > lq.Limit {
		rows = rows[:lq.Limit]
		last := rows[len(rows)-1]
		c := lq.EncodeCursor([]string{last.LastAt.UTC().Format(time.RFC3339Nano), strconv.FormatInt(last.RepoID, 10), hex.EncodeToString(last.Subject[:])})
		next = &c
	}
	out := Page[gen.SignatureRefusal]{Items: []gen.SignatureRefusal{}, NextCursor: next}
	for _, f := range rows {
		seen, err := sc.visible(ctx, holderItems([]signatures.Holder{f.Holder}, sc))
		if err != nil {
			return Page[gen.SignatureRefusal]{}, err
		}
		if len(seen) == 0 {
			continue
		}
		out.Items = append(out.Items, s.refusalRep(ctx, f))
	}
	return out, nil
}

func (s *Server) refusalRep(ctx context.Context, f signatures.Refusal) gen.SignatureRefusal {
	repo := qname.Repo{Project: f.ProjectKey, Key: f.RepoKey}
	pkg, version := s.classifiedPath(ctx, repo, f.Path)
	out := gen.SignatureRefusal{
		Repository: repo.String(), Format: f.Format, Path: f.Path, Package: pkg, Version: version, Sha256: hex.EncodeToString(f.Subject[:]),
		Reason: gen.SignatureGateReason(f.Reason), FirstAt: f.FirstAt, LastAt: f.LastAt, Count: f.Count,
	}
	if f.Release != nil {
		out.Release = &gen.SignatureRefusalRelease{Reason: f.Release.Reason, ReleasedBy: optionalText(f.Release.ReleasedByName), ReleasedAt: f.Release.ReleasedAt}
	}
	return out
}

// ---- the impact (R-SIG-071)

// CountSignatureImpact implements POST /api/v1/signatures/impact.
func (s *Server) CountSignatureImpact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sv, err := s.gateServices()
	var body gen.SignatureImpactRequest
	if err == nil {
		err = DecodeJSON(w, r, &body, s.bodyLimit())
	}
	var ch sgate.Change
	if err == nil {
		ch, err = impactChange(body)
	}
	if err == nil {
		err = s.requireImpactRight(ctx, sv, ch)
	}
	var gated []sgate.Gated
	if err == nil {
		gated, err = gatedRepositories(ctx, sv)
	}
	var imp sgate.Impact
	if err == nil {
		imp, err = sv.Gate.Impact(ctx, ch, gated, sv.PreviewLimit)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	out := gen.SignatureImpact{Artifacts: imp.Artifacts, Repositories: imp.Repositories, Complete: imp.Complete, Bound: gen.SignatureImpactBound("exact")}
	if imp.Upper {
		out.Bound = gen.SignatureImpactBound("upper")
	}
	out.ByRepository = make([]struct {
		Artifacts  int    `json:"artifacts"`
		Repository string `json:"repository"`
	}, 0, len(imp.ByRepository))
	for _, b := range imp.ByRepository {
		out.ByRepository = append(out.ByRepository, struct {
			Artifacts  int    `json:"artifacts"`
			Repository string `json:"repository"`
		}{Artifacts: b.Artifacts, Repository: b.Name.String()})
	}
	writeJSON(w, r, http.StatusOK, out)
}

// impactChange reads the one change of a request: an anchor with its change, a block, or project anchors switched off.
func impactChange(b gen.SignatureImpactRequest) (sgate.Change, error) {
	n := 0
	for _, set := range []bool{b.Anchor != nil, b.Block != nil, b.ProjectAnchors != nil} {
		if set {
			n++
		}
	}
	invalid := func(pointer, detail string) error {
		return errs.E(errs.Invalid, errs.CodeValidation, "The request names no valid change: "+detail+".",
			errs.WithFields(errs.FieldError{Pointer: pointer, Code: "invalid_value", Detail: detail}))
	}
	switch {
	case n != 1:
		return sgate.Change{}, invalid("", "exactly one of anchor, block and projectAnchors")
	case b.Anchor != nil:
		if b.Change == nil {
			return sgate.Change{}, invalid("/change", "remove, disable or narrow with anchor")
		}
		return sgate.Change{Anchor: *b.Anchor, AnchorChange: string(*b.Change)}, nil
	case b.Change != nil:
		return sgate.Change{}, invalid("/change", "only with anchor")
	case b.Block != nil:
		blk := &sgate.Block{Kind: string(b.Block.Kind), Value: b.Block.Value}
		if b.Block.Issuer != nil {
			blk.Issuer = *b.Block.Issuer
		}
		return sgate.Change{Block: blk}, nil
	case *b.ProjectAnchors:
		return sgate.Change{}, invalid("/projectAnchors", "only false: switching project anchors on widens and refuses nothing")
	}
	return sgate.Change{ProjectAnchorsOff: true}, nil
}

// requireImpactRight asks the change's own right (R-SIG-071): security-settings:write, or for an anchor of a project that project's
// Owner. The operation's declaration admitted a holder of the capability or an Owner of some project; an anchor that does not exist is
// 404.
func (s *Server) requireImpactRight(ctx context.Context, sv SignatureServices, ch sgate.Change) error {
	if s.o.Access == nil {
		return errs.E(errs.Forbidden, errs.CodeForbidden, "The operation is not permitted.")
	}
	capability := string(authz.SecuritySettingsWrite)
	if ch.Anchor != 0 {
		rec, err := sv.Store.Anchor(ctx, ch.Anchor)
		if err != nil {
			return err
		}
		if rec.Scope == sgate.ScopeProject {
			if held, err := s.holds(ctx, capability); err != nil || held {
				return err
			}
			if pa, ok := s.o.Access.(ProjectAccess); ok {
				if owns, err := pa.Owns(ctx, rec.ProjectKey); err != nil || owns {
					return err
				}
			}
		}
	}
	return s.o.Access.Require(ctx, capability)
}

// gatedRepositories are the repositories whose gate is on, from the replica's snapshot, each with the members a virtual repository's
// gate reads. A system repository of upstream is never gated (R-SIG-064).
func gatedRepositories(ctx context.Context, sv SignatureServices) ([]sgate.Gated, error) {
	all, err := sv.Repos.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []sgate.Gated
	for _, r := range all {
		if !r.SignatureGate().OnlyVerified || r.Upstream.System() {
			continue
		}
		g := sgate.Gated{Repo: files.GateRepo(r)}
		if r.Class == repository.Virtual {
			for _, m := range r.Members() {
				g.Members = append(g.Members, files.GateRepo(m))
			}
		}
		out = append(out, g)
	}
	return out, nil
}
