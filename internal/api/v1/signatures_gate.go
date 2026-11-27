package v1

import (
	"context"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/files"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/malware"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/signatures"
	sgate "github.com/guided-traffic/quader/internal/signatures/gate"
	"github.com/guided-traffic/quader/internal/signatures/gateset"
)

// The API of the gate per repository, stage 3 of signatures.md (R-SIG-070 to R-SIG-073): the preview before the gate is switched on, the
// impact of a change of anchors, blocks or the switch, the releases of a repository's gate and the list of refused downloads. The gate
// itself (internal/signatures/gate) judges; the store (internal/signatures/store) keeps the releases and the refusals.

// SignatureGateStore is what the operations of the gate need of the store beyond SignatureStore.
type SignatureGateStore interface {
	// Releases answers the releases of the repositories, newest first, at most limit; subject narrows to one artifact.
	Releases(ctx context.Context, repoIDs []int64, subject *[32]byte, limit int) ([]signatures.GateRelease, error)
	// Release grants a release (Conflict already_released when one passes the artifact) and records signature.release.granted.
	Release(ctx context.Context, actor audit.Actor, in signatures.GateReleaseInput) (signatures.GateRelease, error)
	// Withdraw withdraws a release (NotFound when none) and records signature.release.withdrawn, journaled.
	Withdraw(ctx context.Context, actor audit.Actor, repoID int64, repository string, subject [32]byte) error
	// RefusalHolders answers the repositories with a refused download; Refusals one page of them (Limit+1 rows when more follow).
	RefusalHolders(ctx context.Context) ([]signatures.Holder, error)
	Refusals(ctx context.Context, q signatures.RefusalsQuery) ([]signatures.Refusal, error)
	// BlobItem answers a live stored file of the repositories with the blob.
	BlobItem(ctx context.Context, repoIDs []int64, blob [32]byte) (repoID int64, path string, ok bool, err error)
}

// The bounds of the gate's operations: the releases one list answers, and how long a preview reads (R-SIG-072).
const (
	maxReleasesListed = 1000
	previewTime       = 10 * time.Second
)

// gateServices answers the services of an operation of the gate: 503 without the store, 409 signatures_not_enabled while the signatures
// are off (no gate exists then, R-SIG-064).
func (s *Server) gateServices() (SignatureServices, error) {
	sv, err := s.signatureServices()
	if err != nil {
		return sv, err
	}
	if sv.GateStore == nil || sv.Repos == nil {
		return sv, unavailable("The gate of the signatures")
	}
	if !sv.Enabled || !sv.Gate.Enabled() {
		return sv, errs.E(errs.Conflict, errs.CodeSignaturesNotEnabled,
			"config.signatures.enabled is false in the Helm configuration: no repository's gate applies.")
	}
	return sv, nil
}

// gateRepository resolves a repository an operation of the gate names, and refuses one whose gate cannot be on: a Debian repository and
// a system repository of upstream (422 not_applicable, R-SIG-061).
func gateRepository(ctx context.Context, sv SignatureServices, name qname.Repo) (repository.Repository, error) {
	repo, err := sv.Repos.ByName(ctx, name)
	if err != nil {
		return repo, err
	}
	if repo.Format == "deb" || repo.Upstream.System() {
		return repo, errs.E(errs.Invalid, errs.CodeNotApplicable,
			"This repository has no signature gate: a Debian repository's packages carry no publisher's signature, and a system repository serves the product's own content.")
	}
	return repo, nil
}

// readableOf answers those of the repositories the caller reads, in full or in part, or all of them for a holder of
// content:metadata-read (malware's scope of finds, R-MAL-022).
func (s *Server) readableOf(ctx context.Context, sv SignatureServices, repos []repository.Repository) (context.Context, []repository.Repository, error) {
	if len(repos) == 0 {
		return ctx, nil, nil
	}
	holders := make([]malware.Holder, len(repos))
	for i, r := range repos {
		holders[i] = malware.Holder{RepoID: r.NumericID(), ProjectKey: r.Name.Project, RepoKey: r.Name.Key}
	}
	ctx, sc, err := s.malwareScopeOf(ctx, MalwareServices{Decider: sv.Decider}, holders)
	if err != nil {
		return ctx, nil, err
	}
	var out []repository.Repository
	for _, r := range repos {
		if sc.all[r.NumericID()] || sc.part[r.NumericID()] != (qname.Repo{}) {
			out = append(out, r)
		}
	}
	return ctx, out, nil
}

// requireGateRead answers 404 unless the caller reads the repository or holds content:metadata-read: the read of the releases and the
// second half of the permission of a release (the triage rule of R-VULN-013).
func (s *Server) requireGateRead(ctx context.Context, sv SignatureServices, repo repository.Repository) (context.Context, error) {
	ctx, seen, err := s.readableOf(ctx, sv, []repository.Repository{repo})
	if err != nil {
		return ctx, err
	}
	if len(seen) == 0 {
		return ctx, errs.E(errs.NotFound, errs.CodeRepositoryNotFound, "no repository is named "+strconv.Quote(repo.Name.String()))
	}
	return ctx, nil
}

// gateView is a repository's gate as the API judges it: the repository, and for a virtual repository the members the caller reads,
// whose attachments its gate reads (R-SIG-065).
func (s *Server) gateView(ctx context.Context, sv SignatureServices, repo repository.Repository) (context.Context, sgate.Repo, []sgate.Repo, error) {
	gr := files.GateRepo(repo)
	if repo.Class != repository.Virtual {
		return ctx, gr, nil, nil
	}
	ctx, seen, err := s.readableOf(ctx, sv, repo.Members())
	if err != nil {
		return ctx, gr, nil, err
	}
	members := make([]sgate.Repo, len(seen))
	for i, m := range seen {
		members[i] = files.GateRepo(m)
	}
	return ctx, gr, members, nil
}

// ---- the preview (R-SIG-072)

// PreviewRepositorySignatureGate implements GET /api/v1/projects/{projectKey}/repositories/{repoKey}/signatures/preview.
func (s *Server) PreviewRepositorySignatureGate(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey gen.RepoKey, params gen.PreviewRepositorySignatureGateParams) {
	ctx := r.Context()
	sv, err := s.gateServices()
	var repo repository.Repository
	if err == nil {
		repo, err = gateRepository(ctx, sv, qname.Repo{Project: projectKey, Key: repoKey})
	}
	setting := repo.SignatureGate()
	if err == nil && params.Anchors != nil {
		setting.Anchors, err = previewAnchors(*params.Anchors)
	}
	var gr sgate.Repo
	var members []sgate.Repo
	if err == nil {
		ctx, gr, members, err = s.gateView(ctx, sv, repo)
	}
	var p sgate.Preview
	if err == nil {
		setting.OnlyVerified = true
		gr.Setting = setting
		p, err = sv.Gate.Preview(ctx, sgate.PreviewRequest{Repo: gr, Members: members, Limit: sv.PreviewLimit, Deadline: time.Now().Add(previewTime)})
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, s.previewRep(ctx, p))
}

// previewAnchors reads the parameter anchors: references "<scope>/<key>" separated by commas, at most gateset.MaxAnchors, none twice;
// empty is any anchor.
func previewAnchors(v string) ([]string, error) {
	out := []string{}
	if strings.TrimSpace(v) == "" {
		return out, nil
	}
	for _, ref := range strings.Split(v, ",") {
		ref = strings.TrimSpace(ref)
		if _, _, ok := gateset.ParseRef(ref); !ok {
			return nil, badParam("anchors", "must be references <scope>/<key> separated by commas")
		}
		for _, have := range out {
			if have == ref {
				return nil, badParam("anchors", "names "+ref+" twice")
			}
		}
		out = append(out, ref)
	}
	if len(out) > gateset.MaxAnchors {
		return nil, badParam("anchors", "must name at most "+strconv.Itoa(gateset.MaxAnchors)+" anchors")
	}
	return out, nil
}

func (s *Server) previewRep(ctx context.Context, p sgate.Preview) gen.SignatureGatePreview {
	out := gen.SignatureGatePreview{
		Examined: p.Examined, Complete: p.Complete, Artifacts: p.Artifacts, Pass: p.Pass, Pending: p.Pending,
		Refused: gen.SignatureGateRefusedCounts{
			Missing: p.Refused[sgate.ReasonMissing], Untrusted: p.Refused[sgate.ReasonUntrusted], NotChosen: p.Refused[sgate.ReasonNotChosen],
			Invalid: p.Refused[sgate.ReasonInvalid], Blocked: p.Refused[sgate.ReasonBlocked], NotChecked: p.Refused[sgate.ReasonNotChecked],
		},
		Examples: make([]gen.SignatureGateExample, 0, len(p.Examples)),
	}
	for _, e := range p.Examples {
		pkg, version := s.classifiedPath(ctx, e.Holder.Name, e.Path)
		out.Examples = append(out.Examples, gen.SignatureGateExample{
			Repository: e.Holder.Name.String(), Path: e.Path, Package: pkg, Version: version, Reason: gen.SignatureGateReason(e.Reason),
		})
	}
	return out
}

// classifiedPath is the package and the version the repository's format gives a path (the files classifier), nil for none.
func (s *Server) classifiedPath(ctx context.Context, repo qname.Repo, path string) (pkg, version *string) {
	if s.o.Files == nil || path == "" {
		return nil, nil
	}
	c := s.o.Files.Classified(ctx, authz.Coordinate{Repository: repo, Path: path})
	return optionalText(c.Package), optionalText(c.Version)
}

// ---- the releases (R-SIG-070)

// ListSignatureReleases implements GET /api/v1/projects/{projectKey}/repositories/{repoKey}/signatures/releases.
func (s *Server) ListSignatureReleases(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey gen.RepoKey) {
	ctx := r.Context()
	sv, err := s.signatureServices()
	if err == nil && (sv.GateStore == nil || sv.Repos == nil) {
		err = unavailable("The gate of the signatures")
	}
	var repo repository.Repository
	if err == nil {
		repo, err = sv.Repos.ByName(ctx, qname.Repo{Project: projectKey, Key: repoKey})
	}
	if err == nil {
		ctx, err = s.requireGateRead(ctx, sv, repo)
	}
	var rels []signatures.GateRelease
	if err == nil {
		rels, err = sv.GateStore.Releases(ctx, []int64{repo.NumericID()}, nil, maxReleasesListed)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	out := gen.SignatureReleaseList{Items: make([]gen.SignatureRelease, 0, len(rels))}
	for _, rel := range rels {
		out.Items = append(out.Items, releaseRep(repo.Name, rel))
	}
	writeJSON(w, r, http.StatusOK, out)
}

func releaseRep(repo qname.Repo, rel signatures.GateRelease) gen.SignatureRelease {
	return gen.SignatureRelease{
		Repository: repo.String(), Sha256: hex.EncodeToString(rel.Subject[:]), Reason: rel.Reason,
		RefusedReason: gen.SignatureGateRefusedReason(rel.RefusedReason), ReleasedBy: optionalText(rel.ReleasedByName), ReleasedAt: rel.ReleasedAt,
	}
}

// releaseTarget resolves the repository of a write of a release and checks the second half of its permission: vulnerabilities:triage
// is the operation's declaration, the read of the repository or content:metadata-read is asked here (404 without it).
func (s *Server) releaseTarget(ctx context.Context, projectKey, repoKey string) (context.Context, SignatureServices, repository.Repository, error) {
	sv, err := s.gateServices()
	var repo repository.Repository
	if err == nil {
		repo, err = gateRepository(ctx, sv, qname.Repo{Project: projectKey, Key: repoKey})
	}
	if err == nil {
		ctx, err = s.requireGateRead(ctx, sv, repo)
	}
	return ctx, sv, repo, err
}

// CreateSignatureRelease implements POST /api/v1/projects/{projectKey}/repositories/{repoKey}/signatures/releases.
func (s *Server) CreateSignatureRelease(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey gen.RepoKey) {
	ctx, sv, repo, err := s.releaseTarget(r.Context(), projectKey, repoKey)
	var body gen.SignatureReleaseRequest
	if err == nil {
		err = DecodeJSON(w, r, &body, s.bodyLimit())
	}
	var d blob.Digest
	if err == nil {
		var ok bool
		if d, ok = blob.ParseDigest(body.Sha256); !ok {
			err = errs.E(errs.Invalid, errs.CodeValidation, "sha256 must be 64 lower-case hexadecimal digits.",
				errs.WithFields(errs.FieldError{Pointer: "/sha256", Code: "pattern", Detail: "64 lower-case hexadecimal digits"}))
		}
	}
	if err == nil && !validReleaseReason(body.Reason) {
		err = errs.E(errs.Invalid, errs.CodeValidation, "reason must be 1 to 1000 characters of text.",
			errs.WithFields(errs.FieldError{Pointer: "/reason", Code: "length", Detail: "1 to 1000 characters, no control characters"}))
	}
	var refused string
	if err == nil {
		refused, err = s.releasable(ctx, sv, repo, d)
	}
	var rel signatures.GateRelease
	if err == nil {
		rel, err = sv.GateStore.Release(ctx, signatureActor(r), signatures.GateReleaseInput{
			RepoID: repo.NumericID(), Repository: repo.Name.String(), Subject: d, Reason: body.Reason, RefusedReason: refused,
		})
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, releaseRep(repo.Name, rel))
}

// releasable decides whether the repository's gate passes the artifact (R-SIG-070): it judges the stored item with the digest as the gate
// switched on with the stored anchors (Q-SIG-196), and answers the reason it does not pass, "pending" for one that waits (Q-SIG-197).
// 404 when the repository (a virtual one: a member the caller reads) holds no item with the digest, 409 already_released when a release
// passes it, 409 nothing_to_release when the gate passes it.
func (s *Server) releasable(ctx context.Context, sv SignatureServices, repo repository.Repository, d blob.Digest) (string, error) {
	have, err := sv.GateStore.Releases(ctx, []int64{repo.NumericID()}, (*[32]byte)(&d), 1)
	if err != nil {
		return "", err
	}
	if len(have) > 0 {
		return "", errs.E(errs.Conflict, errs.CodeAlreadyReleased,
			"A release already passes this artifact in this repository; withdraw it first to grant another.")
	}
	ctx, gr, members, err := s.gateView(ctx, sv, repo)
	if err != nil {
		return "", err
	}
	holders := map[int64]sgate.Repo{gr.ID: gr}
	ids := []int64{gr.ID}
	if repo.Class == repository.Virtual {
		holders, ids = map[int64]sgate.Repo{}, nil
		for _, m := range members {
			holders[m.ID], ids = m, append(ids, m.ID)
		}
	}
	holder, path, ok, err := sv.GateStore.BlobItem(ctx, ids, d)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errs.E(errs.NotFound, errs.CodeNotFound, "The repository holds no stored file with this SHA-256.")
	}
	gr.Setting.OnlyVerified = true
	got, err := sv.Gate.Assess(ctx, sgate.Assessment{Repo: gr, Members: members, Items: []sgate.AssessItem{{Holder: holders[holder], Path: path, Blob: d}}})
	if err != nil {
		return "", err
	}
	switch a := got[0]; {
	case !a.Artifact || a.Outcome == sgate.Pass:
		return "", errs.E(errs.Conflict, errs.CodeNothingToRelease,
			"The repository's signature gate passes this artifact: a release is for an artifact the gate refuses or holds pending.")
	case a.Outcome == sgate.Pending:
		return refusedPending, nil
	default:
		return string(a.Reason), nil
	}
}

// refusedPending is the reason of an artifact that waits for a check (R-SIG-068, R-SIG-073).
const refusedPending = "pending"

// DeleteSignatureRelease implements DELETE /api/v1/projects/{projectKey}/repositories/{repoKey}/signatures/releases/{sha256}.
func (s *Server) DeleteSignatureRelease(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey gen.RepoKey, sha256 string) {
	ctx, sv, repo, err := s.releaseTarget(r.Context(), projectKey, repoKey)
	var d blob.Digest
	if err == nil {
		d, err = parseMalwareDigest(sha256)
	}
	if err == nil {
		err = sv.GateStore.Withdraw(ctx, signatureActor(r), repo.NumericID(), repo.Name.String(), d)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
