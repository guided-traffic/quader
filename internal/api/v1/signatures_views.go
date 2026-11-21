package v1

import (
	"cmp"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/api/edge"
	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/files"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/malware"
	"github.com/guided-traffic/quader/internal/packages/view"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/signatures"
)

// The reads of what an artifact carries (signatures.md R-SIG-034, R-SIG-035): the signatures of a version per repository and of
// one stored file, the download of a signature's original, and the member `signatures` (the badge) of a stored file and of a
// version. What a reader sees is the attachments of the repositories it reads (R-SIG-004): the addressed repository, and for a
// virtual repository the members that hold the subject and that the caller may read. The trust rows are those of the scope of the
// addressed repository's project, the installation's for a repository of the project upstream (R-SIG-008, Q-SIG-26). Every text a
// signature carries is returned as text: JSON needs no escaping here, and the screens render it as text (R-SIG-034).

// sigSubject is one subject of an answer: the file of the answer whose content it is, or the OCI platform whose manifest it is.
type sigSubject struct {
	digest   [32]byte
	path     string
	platform string
}

// sigSubjects are the subjects one repository's entry asks for, with the npm subject names of a registry signature (Q-SIG-50).
type sigSubjects struct {
	list  []sigSubject
	names []string
}

func (ss *sigSubjects) add(d [32]byte, path, platform string) {
	for i, s := range ss.list {
		if s.digest == d {
			if ss.list[i].path == "" {
				ss.list[i].path = path
			}
			return
		}
	}
	ss.list = append(ss.list, sigSubject{digest: d, path: path, platform: platform})
}

func (ss sigSubjects) digests() [][32]byte {
	out := make([][32]byte, len(ss.list))
	for i, s := range ss.list {
		out[i] = s.digest
	}
	return out
}

func (ss sigSubjects) of(d [32]byte) sigSubject {
	for _, s := range ss.list {
		if s.digest == d {
			return s
		}
	}
	return sigSubject{}
}

// scopeProject is the project whose scope judges what a repository serves (Q-SIG-26): its project, the installation's ("") for a
// repository of the project upstream addressed directly.
func scopeProject(repo repository.Repository) string {
	if repo.Upstream != nil {
		return ""
	}
	return repo.ProjectID
}

// signatureObjectURL is the relative URL of the download of a signature's original.
func signatureObjectURL(d [32]byte) string {
	return "/api/v1/signatures/objects/" + hex.EncodeToString(d[:])
}

// roleOrder orders the entries of an answer: the roles that carry the badge first, then the registry's.
func roleOrder(role string) int {
	switch role {
	case signatures.RolePublisher:
		return 0
	case signatures.RoleProvenance:
		return 1
	case signatures.RoleRegistry:
		return 2
	}
	return 3
}

// countedOf is what the badge rule reads of the views.
func countedOf(views []signatures.View) []signatures.Counted {
	out := make([]signatures.Counted, 0, len(views))
	for _, v := range views {
		c := signatures.Counted{Role: v.Signature.Role, State: v.Signature.State}
		if v.Trust != nil {
			c.Trust, c.Stale = v.Trust.State, v.Trust.Stale
		}
		out = append(out, c)
	}
	return out
}

// sigEntries renders the views of one entry: the signatures of the roles publisher, provenance and registry, and apart the
// attestations (R-SIG-024), each in role order, then by the time Quader received it.
func sigEntries(views []signatures.View, ss sigSubjects, names func(int64) string) (sigs, atts []gen.SignatureEntry) {
	sorted := slices.Clone(views)
	slices.SortStableFunc(sorted, func(a, b signatures.View) int {
		return cmp.Or(cmp.Compare(roleOrder(a.Signature.Role), roleOrder(b.Signature.Role)),
			a.Signature.ReceivedAt.Compare(b.Signature.ReceivedAt), cmp.Compare(a.Signature.ID, b.Signature.ID))
	})
	sigs, atts = []gen.SignatureEntry{}, []gen.SignatureEntry{}
	for _, v := range sorted {
		e := sigEntry(v, ss.of(v.Signature.Subject), names)
		if v.Signature.Role == signatures.RoleAttestation {
			atts = append(atts, e)
			continue
		}
		sigs = append(sigs, e)
	}
	return sigs, atts
}

func optionalStrings(v []string) *[]string {
	if len(v) == 0 {
		return nil
	}
	return &v
}

// sigEntry renders one signature. The signer's JSON is the read's (signatures.Signer); a document that does not decode leaves the
// claims out and keeps the verdict.
func sigEntry(v signatures.View, subj sigSubject, names func(int64) string) gen.SignatureEntry {
	sg := v.Signature
	var who signatures.Signer
	if len(sg.Signer) > 0 {
		_ = json.Unmarshal(sg.Signer, &who)
	}
	e := gen.SignatureEntry{
		Id: sg.ID, Role: gen.SignatureRole(sg.Role), Scheme: gen.SignatureScheme(cmp.Or(v.Scheme, signatures.SchemeUnknown)),
		State: gen.SignatureIntegrityState(sg.State), Reason: optionalText(sg.Reason), Detail: optionalText(who.Detail),
		Trust: sigTrustRep(v.Trust), Signer: optionalText(who.Subject), Issuer: optionalText(who.Issuer), Identity: optionalText(who.Identity),
		OidcIssuer: optionalText(who.OIDCIssuer), AlternativeNames: optionalStrings(who.AlternativeNames), Serial: optionalText(who.Serial),
		Chain: optionalStrings(who.Chain), Ci: sigCIRep(who.CI), KeyIds: append([]string{}, sg.KeyIDs...), KeyId: optionalText(who.KeyID),
		Fingerprint: optionalText(who.Fingerprint), Algorithm: optionalText(who.Algorithm), HashAlgorithm: optionalText(who.HashAlgorithm),
		ClaimedAt: optionalTime(sg.ClaimedAt), ProvenAt: optionalTime(sg.ProvenAt), ReceivedAt: sg.ReceivedAt, CheckedAt: optionalTime(sg.CheckedAt),
		CertificateNotBefore: optionalTime(who.CertificateNotBefore), CertificateNotAfter: optionalTime(who.CertificateNotAfter),
		Log: make([]gen.SignatureLogEntry, 0, len(who.Log)), Timestamps: make([]gen.SignatureTimestamp, 0, len(who.Timestamps)),
		PredicateType: optionalText(cmp.Or(sg.PredicateType, who.PredicateType)), StatementSubjects: optionalStrings(who.StatementSubjects),
		SigningScheme: optionalText(who.SigningScheme), SigningAgent: optionalText(who.SigningAgent), Expiry: optionalTime(who.Expiry),
		ServiceIndex: optionalText(who.ServiceIndex), Owners: optionalStrings(who.Owners), DockerReference: optionalText(who.DockerReference),
		SchemeName: optionalText(who.SchemeName), Cut: optionalText(who.Cut), Subject: hex.EncodeToString(sg.Subject[:]),
		SubjectName: optionalText(sg.SubjectName), Path: optionalText(subj.path), Platform: optionalText(subj.platform),
		MediaType: optionalText(v.MediaType), Size: v.ObjectSize, Original: signatureObjectURL(sg.Object),
		Attachments: make([]gen.SignatureAttachment, 0, len(v.Attachments)),
	}
	e.Type = cmp.Or(sg.PredicateType, who.PredicateType, v.MediaType, cmp.Or(v.Scheme, signatures.SchemeUnknown))
	if e.KeyIds == nil {
		e.KeyIds = []string{}
	}
	if b := v.IssuedBy; b != nil {
		// A signature this installation made (R-SIG-054, R-SIG-055): the identity that made it, never the badge.
		e.IssuedBy = &gen.SignatureIssuedBy{Identity: b.Identity, Subject: b.Subject, Fingerprint: b.Fingerprint, State: gen.SignatureIssuedByState(b.State)}
		if e.KeyId == nil && len(sg.KeyIDs) > 0 {
			e.KeyId = optionalText(sg.KeyIDs[0])
		}
	}
	switch sg.TimeProof {
	case "timestamp", "log":
		tp := gen.SignatureEntryTimeProof(sg.TimeProof)
		e.TimeProof = &tp
	}
	switch who.CommitmentType {
	case "author", "repository":
		ct := gen.SignatureEntryCommitmentType(who.CommitmentType)
		e.CommitmentType = &ct
	}
	for _, l := range who.Log {
		e.Log = append(e.Log, gen.SignatureLogEntry{Index: l.Index, IntegratedTime: optionalTime(l.IntegratedTime), LogId: optionalText(l.LogID), Proof: optionalText(l.Proof)})
	}
	for _, t := range who.Timestamps {
		e.Timestamps = append(e.Timestamps, gen.SignatureTimestamp{Time: t.Time, Authority: optionalText(t.Authority)})
	}
	atts := slices.Clone(v.Attachments)
	slices.SortFunc(atts, func(a, b signatures.Attachment) int {
		return cmp.Or(strings.Compare(names(a.RepoID), names(b.RepoID)), strings.Compare(a.Location, b.Location))
	})
	for _, a := range atts {
		e.Attachments = append(e.Attachments, gen.SignatureAttachment{
			Repository: names(a.RepoID), Source: gen.SignatureAttachmentSource(a.Source), Location: a.Location, Name: optionalText(a.Name),
		})
	}
	if len(e.Attachments) > 0 {
		first := e.Attachments[0]
		e.Repository, e.Source, e.Location = first.Repository, string(first.Source), first.Location
	}
	return e
}

func sigTrustRep(t *signatures.Trust) *gen.SignatureTrust {
	if t == nil {
		return nil
	}
	out := &gen.SignatureTrust{
		State: gen.SignatureTrustState(t.State), Anchors: make([]gen.SignatureAnchorRef, 0, len(t.Anchors)), ComputedAt: t.ComputedAt, Stale: t.Stale,
	}
	for _, a := range t.Anchors {
		out.Anchors = append(out.Anchors, gen.SignatureAnchorRef{Id: a.ID, Title: a.Title, Scope: gen.SignatureAnchorScope(a.Scope)})
	}
	out.Block = sigBlockRef(t.Block)
	return out
}

func sigBlockRef(b *signatures.BlockRecord) *gen.SignatureBlockRef {
	if b == nil {
		return nil
	}
	return &gen.SignatureBlockRef{Id: b.ID, Kind: gen.SignatureBlockKind(b.Kind), Reason: b.Reason, CreatedAt: b.CreatedAt}
}

func sigCIRep(c *signatures.SignerCI) *gen.SignatureCertificateCI {
	if c == nil {
		return nil
	}
	return &gen.SignatureCertificateCI{
		Issuer: optionalText(c.Issuer), BuildSignerUri: optionalText(c.BuildSignerURI), BuildSignerDigest: optionalText(c.BuildSignerDigest),
		RunnerEnvironment: optionalText(c.RunnerEnvironment), SourceRepositoryUri: optionalText(c.SourceRepositoryURI),
		SourceRepositoryDigest: optionalText(c.SourceRepositoryDigest), SourceRepositoryRef: optionalText(c.SourceRepositoryRef),
		SourceRepositoryOwner: optionalText(c.SourceRepositoryOwner), BuildConfigUri: optionalText(c.BuildConfigURI),
		BuildTrigger: optionalText(c.BuildTrigger), RunInvocationUri: optionalText(c.RunInvocationURI),
	}
}

// ---- origin (R-SIG-010)

// signatureOrigin is the origin of a stored file: the recorded upload, else the remote's upstream and the fill's time, else the
// principal the item names (an item stored before the origins were recorded); nil for none.
func signatureOrigin(ctx context.Context, sv SignatureServices, it items.Item) (*gen.SignatureOrigin, error) {
	if it.ID == 0 {
		return nil, nil
	}
	o, ok, err := sv.Store.Origin(ctx, it.ID)
	if err != nil {
		return nil, err
	}
	at := it.CreatedAt
	switch {
	case ok:
		out := &gen.SignatureOrigin{
			Kind: gen.Uploaded, PrincipalKind: optionalText(o.PrincipalKind), PrincipalName: optionalText(o.PrincipalName),
			At: optionalTime(o.UploadedAt), Ci: originCIRep(o.CI),
		}
		return out, nil
	case it.UpstreamURL != "":
		return &gen.SignatureOrigin{Kind: gen.Fetched, UpstreamUrl: &it.UpstreamURL, At: optionalTime(at)}, nil
	case it.CreatedBy != "":
		kind, name, found := strings.Cut(it.CreatedBy, ":")
		if !found {
			kind, name = "", it.CreatedBy
		}
		return &gen.SignatureOrigin{Kind: gen.Uploaded, PrincipalKind: optionalText(kind), PrincipalName: optionalText(name), At: optionalTime(at)}, nil
	}
	return nil, nil
}

// originCIRep reads the claim snapshot of a CI upload (R-SIG-010: the issuer's name and the allowlisted claims of R-CI-020, by
// their claim names). A claim GitLab sends as a string or a number is shown as text; ref_protected as a boolean.
func originCIRep(raw json.RawMessage) *gen.SignatureOriginCI {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var m map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if dec.Decode(&m) != nil {
		return nil
	}
	text := func(keys ...string) *string {
		for _, k := range keys {
			switch v := m[k].(type) {
			case string:
				if v != "" {
					return &v
				}
			case json.Number:
				s := v.String()
				return &s
			case bool:
				s := strconv.FormatBool(v)
				return &s
			}
		}
		return nil
	}
	out := &gen.SignatureOriginCI{
		Issuer: text("issuer"), ProjectPath: text("project_path"), ProjectId: text("project_id"), NamespacePath: text("namespace_path"),
		PipelineId: text("pipeline_id"), JobId: text("job_id"), Ref: text("ref"), RefType: text("ref_type"),
		PipelineSource: text("pipeline_source"), Environment: text("environment"), Sha: text("sha"), UserLogin: text("user_login"),
	}
	switch v := m["ref_protected"].(type) {
	case bool:
		out.RefProtected = &v
	case string:
		if b, err := strconv.ParseBool(v); err == nil {
			out.RefProtected = &b
		}
	}
	return out
}

// ---- one version (R-SIG-034)

// sigItemLookup resolves the stored file at a path of a physical repository for the caller: found false for a path the repository
// does not hold (or the caller may not read). Nil where the files service is not wired: the entries then ask for the subjects of
// the repository the version was read from and carry no origin.
type sigItemLookup func(ctx context.Context, repo qname.Repo, path string) (items.Item, bool, error)

// sigVersion is what the version's answer reads of the package view: the version as the caller sees it and the topology of the
// repositories it was read in.
type sigVersion struct {
	detail view.VersionDetail
	topo   view.Topology
}

// GetPackageVersionSignatures implements GET /api/v1/packages/{packageId}/versions/{version}/signatures: the package view decides
// which repositories hold the version for the caller (R-API-029), this answer reads their attachments.
func (s *Server) GetPackageVersionSignatures(w http.ResponseWriter, r *http.Request, id gen.PackageId, version gen.PackageVersionPath, params gen.GetPackageVersionSignaturesParams) {
	sv, err := s.signatureServices()
	var ps PackageServices
	if err == nil {
		ps, err = s.packageServices()
	}
	var format, name string
	if err == nil {
		format, name, err = packageIdentity(id)
	}
	if v, derr := url.PathUnescape(version); derr == nil {
		version = v
	}
	var only *qname.Repo
	if err == nil && params.Repository != nil {
		n, perr := qname.ParseRepo(*params.Repository)
		if perr != nil {
			err = badParam("repository", "must be a repository name <project>/<key>")
		}
		only = &n
	}
	var in sigVersion
	ctx := r.Context()
	if err == nil {
		var sc view.Scope
		ctx, sc, err = s.packageScope(r, ps, view.ScopeRequest{Formats: []string{format}, ExactName: name})
		found := false
		if err == nil {
			in.detail, found, err = ps.View.Version(ctx, sc, format, name, version)
		}
		if err == nil && !found {
			err = errs.E(errs.NotFound, errs.CodeNotFound, "The version does not exist.")
		}
		in.topo = sc.Topology()
	}
	var out gen.SignatureVersionReport
	if err == nil {
		out, err = s.versionSignatureReport(ctx, sv, in, only, s.sigItems(r))
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, out)
}

// sigItems is the item lookup of the version's answer through the files service, decided for the caller as the items API decides
// a path (R-STOR-016); nil without the service.
func (s *Server) sigItems(r *http.Request) sigItemLookup {
	if s.o.Files == nil {
		return nil
	}
	return func(ctx context.Context, repo qname.Repo, path string) (items.Item, bool, error) {
		c, err := s.filesCaller(r.WithContext(ctx), repo)
		if err != nil {
			return items.Item{}, false, err
		}
		it, err := s.o.Files.ItemInfo(ctx, c, repo, path)
		if err == nil {
			return it, it.Kind == items.KindFile, nil
		}
		if k := errs.KindOf(err); k == errs.NotFound || k == errs.Forbidden {
			return items.Item{}, false, nil
		}
		return items.Item{}, false, err
	}
}

// versionSignatureReport answers the entries of the repositories that hold the version for the caller, or only the one asked for
// (404 when it holds none for the caller).
func (s *Server) versionSignatureReport(ctx context.Context, sv SignatureServices, in sigVersion, only *qname.Repo, lookup sigItemLookup) (gen.SignatureVersionReport, error) {
	d := in.detail
	out := gen.SignatureVersionReport{PackageId: d.PackageID, Version: d.Version, Repositories: []gen.SignatureRepositoryReport{}}
	physical := map[int64]bool{}
	for _, e := range d.Repositories {
		if e.Class != string(repository.Virtual) {
			physical[in.topo.ID(e.Repo)] = true
		}
	}
	names := func(id int64) string { return in.topo.Name(id).String() }
	for _, e := range d.Repositories {
		if only != nil && e.Repo != *only {
			continue
		}
		id := in.topo.ID(e.Repo)
		repo, _ := in.topo.Repository(id)
		holders := []int64{id}
		members := []string{}
		if e.Class == string(repository.Virtual) {
			holders = holders[:0]
			for _, m := range in.topo.Members(id) {
				if physical[m] {
					holders = append(holders, m)
					members = append(members, names(m))
				}
			}
		}
		rep, err := s.repositorySignatures(ctx, sv, in, holders, scopeProject(repo), lookup, names)
		if err != nil {
			return out, err
		}
		rep.Repository, rep.Class, rep.Members = e.Repo.String(), gen.SignatureRepositoryReportClass(e.Class), members
		out.Repositories = append(out.Repositories, rep)
	}
	if only != nil && len(out.Repositories) == 0 {
		return out, errs.E(errs.NotFound, errs.CodeNotFound, "No repository of this name holds the version for you.")
	}
	return out, nil
}

// repositorySignatures reads one entry: the subjects of the version in each holding repository, their attachments there, and the
// origin of the first file a holder stores.
func (s *Server) repositorySignatures(ctx context.Context, sv SignatureServices, in sigVersion, holders []int64, project string, lookup sigItemLookup, names func(int64) string) (gen.SignatureRepositoryReport, error) {
	rep := gen.SignatureRepositoryReport{Signatures: []gen.SignatureEntry{}, Attestations: []gen.SignatureEntry{}, Covering: []gen.SignatureCovering{}, State: gen.SignatureBadgeState(signatures.BadgeNone)}
	var ss sigSubjects
	var first items.Item
	perHolder := make(map[int64]sigSubjects, len(holders))
	for _, h := range holders {
		got, it, err := versionSubjects(ctx, in, in.topo.Name(h), lookup)
		if err != nil {
			return rep, err
		}
		perHolder[h] = got
		for _, sub := range got.list {
			ss.add(sub.digest, sub.path, sub.platform)
		}
		ss.names = got.names
		if first.ID == 0 {
			first = it
		}
	}
	var views []signatures.View
	if len(holders) > 0 && (len(ss.list) > 0 || len(ss.names) > 0) {
		var err error
		views, err = sv.Store.Views(ctx, signatures.ViewQuery{RepoIDs: holders, Subjects: ss.digests(), SubjectNames: ss.names, ProjectID: project})
		if err != nil {
			return rep, err
		}
	}
	state, stale := signatures.Badge(countedOf(views))
	rep.State, rep.Stale = gen.SignatureBadgeState(state), stale
	rep.Signatures, rep.Attestations = sigEntries(views, ss, names)
	for _, h := range holders {
		hr, ok := in.topo.Repository(h)
		if !ok {
			continue
		}
		cov, err := s.covering(ctx, sv, h, in.topo.Name(h), hr.Format, string(hr.Class), project, perHolder[h])
		if err != nil {
			return rep, err
		}
		rep.Covering = append(rep.Covering, cov...)
	}
	origin, err := signatureOrigin(ctx, sv, first)
	rep.Origin = origin
	return rep, err
}

// versionSubjects are the subjects of the version in one physical repository: the blobs of the files at the paths the package view
// read the version with, as that repository stores them; for an OCI tag the manifest the metadata names and each platform's
// manifest, while the repository's tag points at that manifest (R-SIG-024); for npm the registry signature's subject name. first is
// the first stored file, the item whose origin the entry shows.
func versionSubjects(ctx context.Context, in sigVersion, repo qname.Repo, lookup sigItemLookup) (sigSubjects, items.Item, error) {
	d := in.detail
	var ss sigSubjects
	var first items.Item
	for _, f := range d.Files {
		if lookup == nil {
			if dg, ok := blob.ParseDigest(f.SHA256); ok {
				ss.add(dg, f.Path, "")
			}
			continue
		}
		it, ok, err := lookup(ctx, repo, f.Path)
		if err != nil {
			return ss, first, err
		}
		if !ok {
			continue
		}
		ss.add(it.Digest, f.Path, "")
		if first.ID == 0 {
			first = it
		}
	}
	if digest, ok := ociDigest(d.Metadata["digest"]); ok && (lookup == nil || ss.of(digest).digest == digest) {
		ss.add(digest, "", "")
		for _, p := range ociPlatforms(d.Metadata["platforms"]) {
			ss.add(p.digest, "", p.platform)
		}
	}
	if d.Format == "npm" {
		ss.names = []string{d.Name + "@" + d.Version}
	}
	return ss, first, nil
}

// ociDigest reads a "sha256:<hex>" of the OCI format's metadata (R-OCI-031).
func ociDigest(v any) ([32]byte, bool) {
	s, _ := v.(string)
	hexPart, found := strings.CutPrefix(s, "sha256:")
	if !found {
		return [32]byte{}, false
	}
	d, ok := blob.ParseDigest(hexPart)
	return d, ok
}

// ociPlatforms reads the platforms of an index from the OCI format's metadata: each child's platform and digest.
func ociPlatforms(v any) []sigSubject {
	list, _ := v.([]map[string]any)
	if list == nil {
		if anyList, ok := v.([]any); ok {
			for _, x := range anyList {
				if m, ok := x.(map[string]any); ok {
					list = append(list, m)
				}
			}
		}
	}
	var out []sigSubject
	for _, m := range list {
		if d, ok := ociDigest(m["digest"]); ok {
			p, _ := m["platform"].(string)
			out = append(out, sigSubject{digest: d, platform: p})
		}
	}
	return out
}

// ---- one stored file (R-SIG-034)

// GetFileSignatures implements GET /api/v1/projects/{projectKey}/repositories/{repoKey}/signatures: the file is decided as the item
// (R-STOR-016); a folder answers 404, as a path of a virtual repository does (it holds no item).
func (s *Server) GetFileSignatures(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKeyText gen.RepoKey, params gen.GetFileSignaturesParams) {
	ctx := r.Context()
	repoKey := qname.Repo{Project: projectKey, Key: repoKeyText}
	sv, err := s.signatureServices()
	var svc *files.Service
	if err == nil {
		svc, err = s.filesService()
	}
	var path string
	if err == nil {
		path, err = CheckItemPath(params.Path, PathOptions{})
	}
	var it items.Item
	if err == nil {
		var c files.Caller
		if c, err = s.filesCaller(r, repoKey); err == nil {
			it, err = svc.ItemInfo(ctx, c, repoKey, path)
		}
	}
	if err == nil && it.Kind != items.KindFile {
		err = errs.E(errs.NotFound, errs.CodeNotFound, "A folder carries no signatures.")
	}
	var out gen.SignatureFileReport
	if err == nil {
		out, err = s.fileSignatureReport(ctx, sv, repoKey, it)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, out)
}

// fileSignatureReport reads the attachments of the file's blob in its repository, judged in its project's scope, and its origin. A
// tarball of an npm repository also asks for the registry signature of its version, by the subject name its path classifies to.
func (s *Server) fileSignatureReport(ctx context.Context, sv SignatureServices, repoKey qname.Repo, it items.Item) (gen.SignatureFileReport, error) {
	out := gen.SignatureFileReport{
		Repository: repoKey.String(), Path: it.Path, Sha256: it.Digest.Hex(), State: gen.SignatureBadgeState(signatures.BadgeNone),
		Signatures: []gen.SignatureEntry{}, Attestations: []gen.SignatureEntry{}, Covering: []gen.SignatureCovering{},
	}
	if sv.Repos == nil {
		return out, unavailable("The repository snapshot")
	}
	repo, err := sv.Repos.ByName(ctx, repoKey)
	if err != nil {
		return out, err
	}
	var ss sigSubjects
	ss.add(it.Digest, it.Path, "")
	if repo.Format == "npm" && s.o.Files != nil {
		if c := s.o.Files.Classified(ctx, authz.Coordinate{Repository: repoKey, Path: it.Path}); c.Package != "" && c.Version != "" {
			ss.names = []string{c.Package + "@" + c.Version}
		}
	}
	views, err := sv.Store.Views(ctx, signatures.ViewQuery{
		RepoIDs: []int64{repo.NumericID()}, Subjects: ss.digests(), SubjectNames: ss.names, ProjectID: scopeProject(repo),
	})
	if err != nil {
		return out, err
	}
	state, stale := signatures.Badge(countedOf(views))
	out.State, out.Stale = gen.SignatureBadgeState(state), stale
	out.Signatures, out.Attestations = sigEntries(views, ss, func(int64) string { return repoKey.String() })
	if out.Covering, err = s.covering(ctx, sv, repo.NumericID(), repoKey, repo.Format, string(repo.Class), scopeProject(repo), ss); err != nil {
		return out, err
	}
	out.Origin, err = signatureOrigin(ctx, sv, it)
	return out, err
}

// ---- the original (R-SIG-003)

// GetSignatureObject implements GET /api/v1/signatures/objects/{sha256}: the bytes for a caller who reads a repository that holds
// an attachment of a signature of the object (or holds content:metadata-read), 404 for every other caller whether the object
// exists or not. The media type is the format's and so the uploader's choice: the answer is an attachment with nosniff and a
// sandboxing policy, so that no browser renders it as a page of Quader.
func (s *Server) GetSignatureObject(w http.ResponseWriter, r *http.Request, sha256 string) {
	ctx := r.Context()
	sv, err := s.signatureServices()
	var d blob.Digest
	if err == nil {
		var ok bool
		if d, ok = blob.ParseDigest(sha256); !ok {
			err = badParam("sha256", "must be 64 lower-case hexadecimal digits")
		}
	}
	if err == nil {
		err = s.anonymousGate(ctx)
	}
	var holders []signatures.Holder
	if err == nil {
		holders, err = sv.Store.ObjectHolders(ctx, d)
	}
	var seen int
	if err == nil {
		ctx, seen, err = s.signatureHoldersSeen(ctx, sv, holders)
	}
	if err == nil && seen == 0 {
		err = errs.E(errs.NotFound, errs.CodeNotFound, "No repository you can read holds this signature.")
	}
	var raw []byte
	var mediaType string
	if err == nil {
		raw, mediaType, err = sv.Store.ObjectBytes(ctx, d)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	if _, _, perr := mime.ParseMediaType(mediaType); mediaType == "" || perr != nil {
		mediaType = "application/octet-stream"
	}
	h := w.Header()
	h.Set("Content-Type", mediaType)
	h.Set("Content-Length", strconv.Itoa(len(raw)))
	h.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", d.Hex()+".sig"))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw) //nolint:gosec // G705: an attachment with nosniff and a sandboxing policy; the bytes are the stored original
}

// signatureHoldersSeen counts the holders the caller may see, by malware's scope of finds (R-MAL-022): a repository read in full
// shows every holder, one read in part the holders whose path the caller may read; a holder without a path (the repository holds
// only the attachment) is seen only where the repository is read in full.
func (s *Server) signatureHoldersSeen(ctx context.Context, sv SignatureServices, holders []signatures.Holder) (context.Context, int, error) {
	if len(holders) == 0 {
		return ctx, 0, nil
	}
	ctx, sc, err := s.malwareScopeOf(ctx, MalwareServices{Decider: sv.Decider}, holderRepos(holders))
	if err != nil {
		return ctx, 0, err
	}
	seen, err := sc.visible(ctx, holderItems(holders, sc))
	return ctx, len(seen), err
}

// holderRepos are the repositories of holders in the form malware's scope classifies.
func holderRepos(holders []signatures.Holder) []malware.Holder {
	out := make([]malware.Holder, 0, len(holders))
	for _, h := range holders {
		out = append(out, malware.Holder{RepoID: h.RepoID, ProjectKey: h.ProjectKey, RepoKey: h.RepoKey})
	}
	return out
}

// holderItems are holders as malware's scope decides items; a holder without a path in a repository read only in part is left out.
func holderItems(holders []signatures.Holder, sc *malwareScope) []malware.Item {
	out := make([]malware.Item, 0, len(holders))
	for _, h := range holders {
		if h.Path == "" && !sc.all[h.RepoID] {
			continue
		}
		out = append(out, malware.Item{RepoID: h.RepoID, ProjectKey: h.ProjectKey, RepoKey: h.RepoKey, Format: h.Format, Path: h.Path})
	}
	return out
}

// ---- the member signatures (R-SIG-034)

// signaturesOn reports whether answers carry the member: the store is wired and signatures.enabled is true.
func (s *Server) signaturesOn() bool {
	sv := s.o.Signatures
	return sv.Store != nil && sv.Enabled && sv.Repos != nil
}

// signatureCounts reads, in one call, what the badge needs of subjects attached in one repository, judged in its project's scope;
// nil without the member.
func (s *Server) signatureCounts(ctx context.Context, repoKey qname.Repo, subjects [][32]byte, names []string) (map[[32]byte][]signatures.Counted, error) {
	if !s.signaturesOn() || (len(subjects) == 0 && len(names) == 0) {
		return nil, nil
	}
	sv := s.o.Signatures
	repo, err := sv.Repos.ByName(ctx, repoKey)
	if err != nil {
		if errs.KindOf(err) == errs.NotFound {
			return nil, nil
		}
		return nil, err
	}
	counts, err := sv.Store.Counts(ctx, signatures.ViewQuery{RepoIDs: []int64{repo.NumericID()}, Subjects: subjects, SubjectNames: names, ProjectID: scopeProject(repo)})
	if err != nil {
		return nil, err
	}
	if counts == nil {
		counts = map[[32]byte][]signatures.Counted{}
	}
	return counts, nil
}

// signatureMember renders the member of one file of the repository key; nil for a folder and without the member.
func (s *Server) signatureMember(ctx context.Context, repoKey qname.Repo, it items.Item) (*edge.SignatureView, error) {
	if it.Kind != items.KindFile {
		return nil, nil
	}
	counts, err := s.signatureCounts(ctx, repoKey, [][32]byte{it.Digest}, nil)
	if err != nil || counts == nil {
		return nil, err
	}
	state, stale := signatures.Badge(counts[it.Digest])
	return &edge.SignatureView{State: state, Stale: stale}, nil
}

// addSignatureStatus adds the member to each file of a version and to the version (R-SIG-034, R-SIG-035), from one call: the files
// of the repository the version was read from, and for an OCI tag its manifest and its platforms' manifests. A npm version's
// registry signature is asked by its subject name; it carries no badge (R-SIG-009) and so changes nothing here.
func (s *Server) addSignatureStatus(ctx context.Context, d view.VersionDetail, out *gen.PackageVersion) error {
	if !s.signaturesOn() {
		return nil
	}
	var ss sigSubjects
	for _, f := range d.Files {
		if dg, ok := blob.ParseDigest(f.SHA256); ok {
			ss.add(dg, f.Path, "")
		}
	}
	if digest, ok := ociDigest(d.Metadata["digest"]); ok {
		ss.add(digest, "", "")
		for _, p := range ociPlatforms(d.Metadata["platforms"]) {
			ss.add(p.digest, "", p.platform)
		}
	}
	counts, err := s.signatureCounts(ctx, d.Repo, ss.digests(), nil)
	if err != nil || counts == nil {
		return err
	}
	var all []signatures.Counted
	for _, sub := range ss.list {
		all = append(all, counts[sub.digest]...)
	}
	state, stale := signatures.Badge(all)
	out.Signatures = &gen.SignatureStatus{State: gen.SignatureBadgeState(state), Stale: stale}
	for i, f := range d.Files {
		if i >= len(out.Files) {
			break
		}
		if dg, ok := blob.ParseDigest(f.SHA256); ok {
			fs, fstale := signatures.Badge(counts[dg])
			out.Files[i].Signatures = &gen.SignatureStatus{State: gen.SignatureBadgeState(fs), Stale: fstale}
		}
	}
	return nil
}
