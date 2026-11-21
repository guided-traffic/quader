package v1

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/project"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/signatures"
)

// The test doubles of the signatures operations (R-SIG-034, R-SIG-021): a store in memory with the answers of the port's contract,
// a project service that knows project keys, an Access that holds the capabilities and the project roles of one caller, and a
// decision point whose read scope is a table.

func sigT0() time.Time { return time.Date(2026, 11, 21, 9, 12, 44, 0, time.UTC) }

// fakeSigStore implements SignatureStore.
type fakeSigStore struct {
	mu       sync.Mutex
	settings signatures.Settings
	anchors  map[int64]signatures.AnchorRecord
	blocks   []signatures.BlockRecord
	// views answers Views and Counts: every view whose attachment is in the query's repositories and whose subject or subject
	// name the query asks for.
	views   []signatures.View
	origins map[int64]signatures.StoredOrigin
	objects map[[32]byte][]byte
	holders map[[32]byte][]signatures.Holder
	// versionBlobs answers VersionBlobs; versionBlobCalls counts its calls.
	versionBlobs     []signatures.VersionBlob
	versionBlobCalls int
	findings         []signatures.Finding
	fHolders         []signatures.Holder

	// recorded calls
	viewQueries   []signatures.ViewQuery
	findingsQuery []signatures.FindingsQuery
	created       []signatures.AnchorInput
	patched       []signatures.AnchorPatch
	patchVersions []int64
	deleted       []int64
	blocked       []signatures.BlockInput
	switched      []bool
}

func newFakeSigStore() *fakeSigStore {
	return &fakeSigStore{
		settings: signatures.Settings{Version: 3, AnchorEpoch: 7, UpdatedAt: sigT0(), TrustRoot: signatures.TrustRootState{Source: signatures.TrustRootRelease, FetchedAt: sigT0(), TUFVersion: 12}},
		anchors:  map[int64]signatures.AnchorRecord{},
		origins:  map[int64]signatures.StoredOrigin{},
		objects:  map[[32]byte][]byte{},
		holders:  map[[32]byte][]signatures.Holder{},
	}
}

func (f *fakeSigStore) Settings(context.Context) (signatures.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.settings, nil
}

func (f *fakeSigStore) UpdateSettings(_ context.Context, _ audit.Actor, on bool, version int64) (signatures.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if version != f.settings.Version {
		return f.settings, errs.Stale("The settings", f.settings.Version)
	}
	f.switched = append(f.switched, on)
	f.settings.ProjectAnchors, f.settings.Version = on, f.settings.Version+1
	return f.settings, nil
}

func (f *fakeSigStore) Anchors(_ context.Context, q signatures.AnchorQuery) ([]signatures.AnchorRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []signatures.AnchorRecord
	for _, a := range f.anchors {
		switch {
		case q.ProjectID != "":
			if a.Scope != "project" || a.ProjectID != q.ProjectID {
				continue
			}
		case q.Scope == "":
			if a.Scope == "project" {
				continue
			}
		case a.Scope != q.Scope:
			continue
		}
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b signatures.AnchorRecord) int { return int(a.ID - b.ID) })
	return out, nil
}

func (f *fakeSigStore) Anchor(_ context.Context, id int64) (signatures.AnchorRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.anchors[id]
	if !ok {
		return a, errs.E(errs.NotFound, errs.CodeNotFound, "no anchor")
	}
	return a, nil
}

func (f *fakeSigStore) CreateAnchor(_ context.Context, _ audit.Actor, in signatures.AnchorInput) (signatures.AnchorRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if in.Scope == "project" && !f.settings.ProjectAnchors {
		return signatures.AnchorRecord{}, errs.E(errs.Forbidden, errs.CodeProjectAnchorsDisabled, "switched off")
	}
	if string(in.Material) == `{"ca":{"certificates":"-----BEGIN PRIVATE KEY-----"}}` {
		return signatures.AnchorRecord{}, errs.E(errs.Invalid, errs.CodeValidation, "refused",
			errs.WithFields(errs.FieldError{Pointer: "/ca/certificates", Code: "secret_material", Detail: "a private key"}))
	}
	f.created = append(f.created, in)
	id := int64(100 + len(f.anchors))
	a := signatures.AnchorRecord{
		ID: id, Scope: in.Scope, ProjectID: in.ProjectID, Key: in.Key, Title: in.Title, Kind: in.Kind, Material: in.Material,
		Enabled: true, Note: in.Note, Version: 1, CreatedAt: sigT0(), UpdatedAt: sigT0(), CreatedBy: "alice", Applied: true,
	}
	if in.Scope == "project" {
		a.ProjectKey = "web"
	}
	f.anchors[id] = a
	return a, nil
}

func (f *fakeSigStore) UpdateAnchor(_ context.Context, _ audit.Actor, id int64, p signatures.AnchorPatch, version int64) (signatures.AnchorRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.anchors[id]
	if !ok {
		return a, errs.E(errs.NotFound, errs.CodeNotFound, "no anchor")
	}
	if a.Scope == "configuration" || (a.Scope == "shipped" && (p.Title != nil || p.Note != nil || p.Material != nil)) {
		return a, errs.E(errs.Conflict, errs.CodeAnchorManaged, "managed")
	}
	if a.Scope == "project" && !f.settings.ProjectAnchors {
		return a, errs.E(errs.Forbidden, errs.CodeProjectAnchorsDisabled, "switched off")
	}
	f.patched, f.patchVersions = append(f.patched, p), append(f.patchVersions, version)
	if p.Title != nil {
		a.Title = *p.Title
	}
	if p.Enabled != nil {
		a.Enabled = *p.Enabled
	}
	if p.Material != nil {
		a.Material = p.Material
	}
	a.Version++
	f.anchors[id] = a
	return a, nil
}

func (f *fakeSigStore) DeleteAnchor(_ context.Context, _ audit.Actor, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.anchors[id]
	if !ok {
		return errs.E(errs.NotFound, errs.CodeNotFound, "no anchor")
	}
	if a.Scope == "shipped" || a.Scope == "configuration" {
		return errs.E(errs.Conflict, errs.CodeAnchorManaged, "managed")
	}
	f.deleted = append(f.deleted, id)
	delete(f.anchors, id)
	return nil
}

func (f *fakeSigStore) Blocks(context.Context) ([]signatures.BlockRecord, error) {
	return f.blocks, nil
}

func (f *fakeSigStore) CreateBlock(_ context.Context, _ audit.Actor, in signatures.BlockInput) (signatures.BlockRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blocked = append(f.blocked, in)
	b := signatures.BlockRecord{ID: int64(len(f.blocked)), Kind: in.Kind, Value: in.Value, Issuer: in.Issuer, Reason: in.Reason, CreatedAt: sigT0(), CreatedBy: "alice"}
	f.blocks = append(f.blocks, b)
	return b, nil
}

func (f *fakeSigStore) DeleteBlock(_ context.Context, _ audit.Actor, id int64) error {
	for i, b := range f.blocks {
		if b.ID == id {
			f.blocks = slices.Delete(f.blocks, i, i+1)
			return nil
		}
	}
	return errs.E(errs.NotFound, errs.CodeNotFound, "no block")
}

func (f *fakeSigStore) FindingHolders(context.Context, []string) ([]signatures.Holder, error) {
	return f.fHolders, nil
}

func (f *fakeSigStore) Findings(_ context.Context, q signatures.FindingsQuery) ([]signatures.Finding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.findingsQuery = append(f.findingsQuery, q)
	out := f.findings
	if len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (f *fakeSigStore) Views(_ context.Context, q signatures.ViewQuery) ([]signatures.View, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.viewQueries = append(f.viewQueries, q)
	var out []signatures.View
	for _, v := range f.views {
		asked := slices.Contains(q.Subjects, v.Signature.Subject) || (v.Signature.SubjectName != "" && slices.Contains(q.SubjectNames, v.Signature.SubjectName))
		if !asked {
			continue
		}
		var atts []signatures.Attachment
		for _, a := range v.Attachments {
			if slices.Contains(q.RepoIDs, a.RepoID) {
				atts = append(atts, a)
			}
		}
		if len(atts) == 0 {
			continue
		}
		v.Attachments = atts
		out = append(out, v)
	}
	return out, nil
}

func (f *fakeSigStore) Counts(ctx context.Context, q signatures.ViewQuery) (map[[32]byte][]signatures.Counted, error) {
	views, _ := f.Views(ctx, q)
	out := map[[32]byte][]signatures.Counted{}
	for _, v := range views {
		out[v.Signature.Subject] = append(out[v.Signature.Subject], countedOf([]signatures.View{v})...)
	}
	return out, nil
}

func (f *fakeSigStore) ObjectHolders(_ context.Context, d [32]byte) ([]signatures.Holder, error) {
	return f.holders[d], nil
}

func (f *fakeSigStore) ObjectBytes(_ context.Context, d [32]byte) ([]byte, string, error) {
	b, ok := f.objects[d]
	if !ok {
		return nil, "", errs.E(errs.NotFound, errs.CodeNotFound, "no object")
	}
	return b, "text/html", nil
}

func (f *fakeSigStore) Origin(_ context.Context, itemID int64) (signatures.StoredOrigin, bool, error) {
	o, ok := f.origins[itemID]
	return o, ok, nil
}

// sigProjects is the project service of the tests: it knows the projects web (uuid p-web) and api (uuid p-api).
type sigProjects struct{ project.Service }

func (sigProjects) Get(_ context.Context, key string) (project.Project, error) {
	switch key {
	case "web", "api":
		return project.Project{ID: "p-" + key, Key: key}, nil
	}
	return project.Project{}, errs.E(errs.NotFound, errs.CodeNotFound, "no project")
}

// sigCaller is a caller of R-SIG-021's table: its capabilities and its role per project ("owner", "reader").
type sigCaller struct {
	name  string
	caps  []authz.Capability
	roles map[string]string
	anon  bool
}

func (c sigCaller) holds(p string) bool { return slices.Contains(c.caps, authz.Capability(p)) }

// sigAccess is the Access of one caller, with the project questions of the shared step (ProjectAccess) and the question form
// (Prober).
type sigAccess struct{ c sigCaller }

func (a sigAccess) Require(ctx context.Context, p string) error {
	if httpserver.PrincipalFrom(ctx).Anonymous {
		return errs.E(errs.Unauthenticated, errs.CodeUnauthenticated, "credentials required", errs.WithChallenge("Bearer"))
	}
	if a.c.holds(p) {
		return nil
	}
	return errs.E(errs.Forbidden, errs.CodeForbidden, "not permitted")
}

func (a sigAccess) Holds(_ context.Context, p string) (bool, error) { return a.c.holds(p), nil }

func (a sigAccess) Memberships(context.Context) ([]authz.Membership, error) {
	var out []authz.Membership
	for k, r := range a.c.roles {
		role := authz.ProjectRoleReader
		if r == "owner" {
			role = authz.ProjectRoleOwner
		}
		out = append(out, authz.Membership{Key: k, Role: role})
	}
	return out, nil
}

func (a sigAccess) Owns(_ context.Context, project string) (bool, error) {
	return a.c.roles[project] == "owner", nil
}

// The callers of R-SIG-021.
func sigCallers() map[string]sigCaller {
	return map[string]sigCaller{
		"admin":          {name: "admin", caps: authz.RoleCapabilities(authz.Role("admin"))},
		"security-admin": {name: "security-admin", caps: authz.RoleCapabilities(authz.Role("security-admin"))},
		"auditor":        {name: "auditor", caps: authz.RoleCapabilities(authz.Role("auditor"))},
		"owner":          {name: "owner", roles: map[string]string{"web": "owner"}},
		"member":         {name: "member", roles: map[string]string{"web": "reader"}},
		"anonymous":      {name: "anonymous", anon: true},
	}
}

// sigDecider is the decision point of the reads that decide holders (the download of an original, the findings): the read shape
// of each repository is a table, and a repository read in part allows the paths listed.
type sigDecider struct {
	authz.Decider
	shapes  map[qname.Repo]authz.ReadShape
	allowed map[string]bool
}

func (d sigDecider) Begin(context.Context, authz.Subject, ...qname.Repo) (*authz.Index, error) {
	return authz.BuildIndex(&authz.Snapshot{}, nil, nil), nil
}

func (d sigDecider) Know(_ context.Context, idx *authz.Index, _ authz.Subject, _ qname.Repo) (*authz.Index, error) {
	return idx, nil
}

func (d sigDecider) ReadScopeOn(_ context.Context, _ *authz.Index, _ authz.Subject, _ qname.Repo, _ []qname.Repo) (authz.ReadScope, error) {
	return sigReadScope(d), nil
}

func (d sigDecider) PathsAllowedOn(_ context.Context, _ *authz.Index, _ authz.Subject, _ authz.Coordinate, _ authz.Action, paths []string, _ authz.Classifier) ([]bool, error) {
	out := make([]bool, len(paths))
	for i, p := range paths {
		out[i] = d.allowed[p]
	}
	return out, nil
}

type sigReadScope sigDecider

func (s sigReadScope) Shape(k qname.Repo) authz.ReadShape          { return s.shapes[k] }
func (s sigReadScope) MayRead(qname.Repo, string, string) bool     { return false }
func (s sigReadScope) MayReadWiki(qname.Repo, string, string) bool { return false }
func (s sigReadScope) VersionsByPath(qname.Repo) bool              { return true }

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// VersionBlobs answers the fake's version blobs of the repositories and versions asked.
func (f *fakeSigStore) VersionBlobs(_ context.Context, repoIDs []int64, _ string, versions []string) ([]signatures.VersionBlob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.versionBlobCalls++
	var out []signatures.VersionBlob
	for _, b := range f.versionBlobs {
		if slices.Contains(repoIDs, b.RepoID) && slices.Contains(versions, b.Version) {
			out = append(out, b)
		}
	}
	return out, nil
}
