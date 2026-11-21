package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/api/v1/apitest"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/signatures"
)

// Unit tests of the management operations of signatures (R-SIG-034) with the permissions of R-SIG-021, through the shared step and
// the response validator, against a store in memory. The integration test TestR_SIG_034 covers the store behind them.

// sigEnv is the shared document and a response validator of the test function: the validator records what the function's
// requests reached, the parsed documents behind both are built once per test binary (testDocument, newValidator).
type sigEnv struct {
	doc *Document
	val *apitest.Validator
}

func newSigEnv(t *testing.T) sigEnv {
	t.Helper()
	return sigEnv{doc: testDocument(t), val: newValidator(t)}
}

// setup builds the server of one caller on the environment's document, as newSetup does, behind the response validator.
func (e sigEnv) setup(t *testing.T, st *fakeSigStore, c sigCaller, opts ...func(*Options)) setup {
	t.Helper()
	authn := func(*http.Request) (httpserver.Principal, error) {
		return httpserver.Principal{
			Kind: httpserver.KindUser, ID: "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10", Name: c.name,
			Credential: httpserver.Credential{Method: httpserver.MethodBearer, TokenID: "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a11"},
		}, nil
	}
	if c.anon {
		authn = nil
	}
	o := Options{
		Document: e.doc, Identity: Identity{Replica: "quader-web-1", Role: "web"}, Ready: func() bool { return true },
		Access: sigAccess{c}, Authz: AuthzServices{Projects: sigProjects{}},
		Signatures: SignatureServices{Store: st, Enabled: true, Repos: sigRepos()},
	}
	o.API.Config = apiConfigForTest()
	o.API.Config.MaxRequestBodyBytes = 1 << 20
	for _, f := range opts {
		f(&o)
	}
	router := httpserver.NewClientRouter()
	New(o).Register(router)
	res, err := httpserver.NewResolver(nil, "https://quader.example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	chain := httpserver.Chain{Router: router, Resolver: res, Authenticate: authn, Replica: "quader-web-1", ReplicaHeader: httpserver.ReplicaAuthenticated}
	return setup{handler: e.val.Handler(t, chain.Handler()), router: router, doc: e.doc}
}

// sigRepos are the repositories of the tests: web/npm-local (local, project web), upstream/npmjs (a remote of the project upstream)
// and web/npm-all (virtual over both).
func sigRepos() packagesReader {
	local := repository.Repository{ID: "1", Name: qname.Repo{Project: "web", Key: "npm-local"}, Class: repository.Local, Format: "npm", ProjectID: "p-web"}
	remote := repository.Repository{ID: "2", Name: qname.Repo{Project: "upstream", Key: "npmjs"}, Class: repository.Remote, Format: "npm", ProjectID: "p-upstream", Upstream: &repository.UpstreamFacts{}}
	all := repository.Assemble(repository.Repository{ID: "3", Name: qname.Repo{Project: "web", Key: "npm-all"}, Class: repository.Virtual, Format: "npm", ProjectID: "p-web"}, []repository.Repository{local, remote}, nil, 0)
	return packagesReader{repos: map[string]repository.Repository{"npm-local": local, "npmjs": remote, "npm-all": all}}
}

func sigBody(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("%d %s: %v", w.Code, w.Body.String(), err)
	}
}

func withAnchors(st *fakeSigStore) *fakeSigStore {
	st.anchors[1] = signatures.AnchorRecord{
		ID: 1, Scope: "shipped", Key: "sigstore-public", Title: "Public Sigstore", Kind: "sigstore",
		Material: mustJSON(map[string]any{"sigstore": map[string]any{"issuer": "", "identityPattern": ".*", "trustRoot": "public"}}),
		Enabled:  true, Applied: true, Version: 1, CreatedAt: sigT0(), UpdatedAt: sigT0(), Signatures: 41,
	}
	st.anchors[2] = signatures.AnchorRecord{
		ID: 2, Scope: "configuration", Key: "gitlab-ci", Title: "GitLab CI signing CA", Kind: "ca",
		Material: mustJSON(map[string]any{"ca": map[string]any{"certificates": "-----BEGIN CERTIFICATE-----", "subject": map[string]string{"O": "Example GmbH"}}}),
		Enabled:  true, Applied: true, Version: 1, CreatedAt: sigT0(), UpdatedAt: sigT0(), Note: "from the values",
		Fingerprints: []signatures.Fingerprint{{Kind: "sha256", Value: "ab12", Subject: "CN=Example CA", NotAfter: sigT0()}},
	}
	st.anchors[3] = signatures.AnchorRecord{
		ID: 3, Scope: "installation", Key: "release", Title: "Release key", Kind: "openpgp",
		Material: mustJSON(map[string]any{"openpgp": map[string]any{"keys": "-----BEGIN PGP PUBLIC KEY BLOCK-----"}}),
		Enabled:  false, Applied: false, Version: 4, CreatedAt: sigT0(), UpdatedAt: sigT0(), CreatedBy: "alice", Note: "rotated in 2027",
	}
	st.anchors[4] = signatures.AnchorRecord{
		ID: 4, Scope: "project", ProjectID: "p-web", ProjectKey: "web", Key: "team", Title: "Team key", Kind: "openpgp",
		Material: mustJSON(map[string]any{"openpgp": map[string]any{"keys": "-----BEGIN PGP PUBLIC KEY BLOCK-----"}}),
		Enabled:  true, Applied: true, Version: 2, CreatedAt: sigT0(), UpdatedAt: sigT0(), CreatedBy: "olga",
	}
	st.anchors[5] = signatures.AnchorRecord{
		ID: 5, Scope: "project", ProjectID: "p-api", ProjectKey: "api", Key: "other", Title: "Other team", Kind: "openpgp",
		Material: mustJSON(map[string]any{"openpgp": map[string]any{"keys": "x"}}), Enabled: true, Applied: true, Version: 1, CreatedAt: sigT0(), UpdatedAt: sigT0(),
	}
	return st
}

const sigNewAnchor = `{"key":"release-2","title":"Release key 2","kind":"openpgp","openpgp":{"keys":"-----BEGIN PGP PUBLIC KEY BLOCK-----"}}`

// TestR_SIG_021_Permissions: the operations of R-SIG-034 answer as R-SIG-021 says for an administrator, a security-admin, an
// auditor, an Owner of web with the switch on and off, a member of web and an anonymous caller.
func TestR_SIG_021_Permissions(t *testing.T) {
	env := newSigEnv(t)
	type op struct{ name, method, path, body string }
	ops := []op{
		{"read the switch", "GET", "/api/v1/signatures/settings", ""},
		{"list the anchors", "GET", "/api/v1/signatures/anchors", ""},
		{"list every project's anchors", "GET", "/api/v1/signatures/anchors?scope=project", ""},
		{"read an anchor", "GET", "/api/v1/signatures/anchors/1", ""},
		{"create an anchor", "POST", "/api/v1/signatures/anchors", sigNewAnchor},
		{"remove a project anchor as administrator", "DELETE", "/api/v1/signatures/anchors/4", ""},
		{"list the blocks", "GET", "/api/v1/signatures/blocks", ""},
		{"block a signer", "POST", "/api/v1/signatures/blocks", `{"kind":"keyid","value":"SHA256:abc","reason":"leaked"}`},
		{"read the project's anchors", "GET", "/api/v1/projects/web/signatures/anchors", ""},
		{"create a project anchor", "POST", "/api/v1/projects/web/signatures/anchors", sigNewAnchor},
		{"remove a project anchor", "DELETE", "/api/v1/projects/web/signatures/anchors/4", ""},
	}
	// want[caller][op] with the switch on; the Owner's row with the switch off follows.
	want := map[string][]int{
		"admin":          {200, 200, 200, 200, 201, 204, 200, 201, 200, 201, 204},
		"security-admin": {200, 200, 200, 200, 201, 204, 200, 201, 200, 201, 204},
		"auditor":        {200, 200, 200, 200, 403, 403, 200, 403, 200, 403, 403},
		"owner":          {403, 403, 403, 403, 403, 403, 403, 403, 200, 201, 204},
		"member":         {403, 403, 403, 403, 403, 403, 403, 403, 200, 403, 403},
		"anonymous":      {401, 401, 401, 401, 401, 401, 401, 401, 401, 401, 401},
	}
	for name, c := range sigCallers() {
		for i, o := range ops {
			t.Run("R-SIG-021 "+name+" may "+o.name+": "+http.StatusText(want[name][i]), func(t *testing.T) {
				st := withAnchors(newFakeSigStore())
				st.settings.ProjectAnchors = true
				s := env.setup(t, st, c)
				w := s.do(o.method, o.path, o.body, nil)
				if w.Code != want[name][i] {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
			})
		}
	}
	t.Run("R-SIG-019 with the switch off the Owner's writes answer 403 project_anchors_disabled and the reads stay", func(t *testing.T) {
		st := withAnchors(newFakeSigStore())
		s := env.setup(t, st, sigCallers()["owner"])
		problem(t, s.do("POST", "/api/v1/projects/web/signatures/anchors", sigNewAnchor, nil), 403, errs.CodeProjectAnchorsDisabled)
		problem(t, s.do("PATCH", "/api/v1/projects/web/signatures/anchors/4", `{"title":"x"}`, map[string]string{"If-Match": `"2"`, "Content-Type": MediaMergePatch}), 403, errs.CodeProjectAnchorsDisabled)
		problem(t, s.do("DELETE", "/api/v1/projects/web/signatures/anchors/4", "", nil), 403, errs.CodeProjectAnchorsDisabled)
		if w := s.get("/api/v1/projects/web/signatures/anchors"); w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if len(st.deleted) != 0 {
			t.Fatalf("deleted %v", st.deleted)
		}
	})
	t.Run("R-SIG-019 with the switch off an administrator removes a project anchor through the project's route", func(t *testing.T) {
		st := withAnchors(newFakeSigStore())
		s := env.setup(t, st, sigCallers()["security-admin"])
		if w := s.do("DELETE", "/api/v1/projects/web/signatures/anchors/4", "", nil); w.Code != 204 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("R-SIG-021 the Owner of web reaches no anchor of another project, and a caller of no project gets 404 for it", func(t *testing.T) {
		st := withAnchors(newFakeSigStore())
		st.settings.ProjectAnchors = true
		s := env.setup(t, st, sigCallers()["owner"])
		problem(t, s.do("DELETE", "/api/v1/projects/web/signatures/anchors/5", "", nil), 404, errs.CodeNotFound)
		problem(t, s.do("DELETE", "/api/v1/projects/web/signatures/anchors/3", "", nil), 404, errs.CodeNotFound)
		if w := s.get("/api/v1/projects/api/signatures/anchors"); w.Code != 404 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
}

// TestR_SIG_034_TheSwitch: the settings' shape and ETag, If-Match and the recent authentication of R-SIG-019.
func TestR_SIG_034_TheSwitch(t *testing.T) {
	env := newSigEnv(t)
	admin := sigCallers()["admin"]
	patchHdr := func(tag string) map[string]string {
		h := map[string]string{"Content-Type": MediaMergePatch}
		if tag != "" {
			h["If-Match"] = tag
		}
		return h
	}
	t.Run("R-SIG-034 the settings carry the switch, the epoch, the trust root and the backlog with an ETag", func(t *testing.T) {
		st := newFakeSigStore()
		st.settings.Backlog = signatures.Backlog{Pending: 5, Stale: 2}
		st.settings.TrustRoot.LastError, st.settings.TrustRoot.LastErrorAt = "tuf: signature threshold not met", sigT0()
		w := env.setup(t, st, admin).get("/api/v1/signatures/settings")
		var got map[string]any
		sigBody(t, w, &got)
		if w.Code != 200 || w.Header().Get("ETag") != `"3"` || got["enabled"] != true || got["projectAnchors"] != false || got["anchorEpoch"] != 7.0 {
			t.Fatalf("%d %s %s", w.Code, w.Header().Get("ETag"), w.Body.String())
		}
		tr := got["trustRoot"].(map[string]any)
		if tr["source"] != "release" || tr["tufVersion"] != 12.0 || tr["lastError"] != "tuf: signature threshold not met" {
			t.Fatalf("%v", tr)
		}
		if b := got["backlog"].(map[string]any); b["pending"] != 5.0 || b["stale"] != 2.0 {
			t.Fatalf("%v", b)
		}
	})
	t.Run("R-API-012 the switch needs If-Match: 428 without, 412 for an old tag, 200 with the current one", func(t *testing.T) {
		st := newFakeSigStore()
		s := env.setup(t, st, admin)
		problem(t, s.do("PATCH", "/api/v1/signatures/settings", `{"projectAnchors":true}`, patchHdr("")), 428, errs.CodePreconditionRequired)
		if w := s.do("PATCH", "/api/v1/signatures/settings", `{"projectAnchors":true}`, patchHdr(`"2"`)); w.Code != 412 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		w := s.do("PATCH", "/api/v1/signatures/settings", `{"projectAnchors":true}`, patchHdr(`"3"`))
		var got map[string]any
		sigBody(t, w, &got)
		if w.Code != 200 || got["projectAnchors"] != true || w.Header().Get("ETag") != `"4"` || len(st.switched) != 1 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("R-SIG-019 a member the patch does not declare answers 400", func(t *testing.T) {
		w := env.setup(t, newFakeSigStore(), admin).do("PATCH", "/api/v1/signatures/settings", `{"anchorEpoch":9}`, patchHdr(`"3"`))
		if w.Code != 400 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("R-SIG-019 R-API-007 a session without a recent sign-in is refused the switch", func(t *testing.T) {
		st := newFakeSigStore()
		val := newValidator(t)
		s := newSetup(t, func(o *Options) {
			o.Access, o.Auth.Sessions = sigAccess{admin}, staleSessions{}
			o.API.Config = apiConfigForTest()
			o.Signatures = SignatureServices{Store: st, Enabled: true}
		}, sessionCaller)
		s.handler = val.Handler(t, s.handler)
		problem(t, s.do("PATCH", "/api/v1/signatures/settings", `{"projectAnchors":true}`, patchHdr(`"3"`)), 401, "reauthentication_required")
		if len(st.switched) != 0 {
			t.Fatal("the switch changed")
		}
	})
	t.Run("R-SIG-034 without the store every operation answers 503", func(t *testing.T) {
		s := env.setup(t, nil, admin, func(o *Options) { o.Signatures = SignatureServices{} })
		for _, p := range []string{
			"/api/v1/signatures/settings", "/api/v1/signatures/anchors", "/api/v1/signatures/anchors/1", "/api/v1/signatures/blocks",
			"/api/v1/signatures/findings", "/api/v1/projects/web/signatures/anchors", "/api/v1/signatures/objects/" + strings.Repeat("a", 64),
			"/api/v1/packages/x/versions/1.0.0/signatures",
		} {
			if w := s.get(p); w.Code != 503 {
				t.Errorf("%s: %d %s", p, w.Code, w.Body.String())
			}
		}
	})
}

// TestR_SIG_034_Anchors: the shapes of the anchors, the checks of a create and the merge patch.
func TestR_SIG_034_Anchors(t *testing.T) {
	env := newSigEnv(t)
	admin := sigCallers()["admin"]
	t.Run("R-SIG-015 an anchor carries its scope, its kind's material, fingerprints, applied and the signatures it knows", func(t *testing.T) {
		s := env.setup(t, withAnchors(newFakeSigStore()), admin)
		w := s.get("/api/v1/signatures/anchors")
		var got struct {
			Items []map[string]any `json:"items"`
		}
		sigBody(t, w, &got)
		if len(got.Items) != 3 {
			t.Fatalf("%s", w.Body.String())
		}
		ca := got.Items[1]
		if ca["scope"] != "configuration" || ca["kind"] != "ca" || ca["ca"].(map[string]any)["subject"].(map[string]any)["O"] != "Example GmbH" ||
			ca["note"] != "from the values" || ca["createdBy"] != nil || len(ca["fingerprints"].([]any)) != 1 {
			t.Fatalf("%v", ca)
		}
		if off := got.Items[2]; off["enabled"] != false || off["applied"] != false || off["openpgp"] == nil {
			t.Fatalf("%v", off)
		}
		w = s.get("/api/v1/signatures/anchors?scope=project")
		sigBody(t, w, &got)
		if len(got.Items) != 2 || got.Items[0]["project"] != "web" {
			t.Fatalf("%s", w.Body.String())
		}
		if w := s.get("/api/v1/signatures/anchors?scope=everything"); w.Code != 400 {
			t.Fatalf("%d", w.Code)
		}
	})
	t.Run("R-SIG-015 a create stores exactly the kind's member and answers 201 with Location and ETag", func(t *testing.T) {
		st := newFakeSigStore()
		w := env.setup(t, st, admin).do("POST", "/api/v1/signatures/anchors", sigNewAnchor, nil)
		if w.Code != 201 || !strings.HasSuffix(w.Header().Get("Location"), "/api/v1/signatures/anchors/100") || w.Header().Get("ETag") != `"1"` {
			t.Fatalf("%d %v %s", w.Code, w.Header(), w.Body.String())
		}
		if in := st.created[0]; in.Scope != "installation" || in.ProjectID != "" || string(in.Material) != `{"openpgp":{"keys":"-----BEGIN PGP PUBLIC KEY BLOCK-----"}}` {
			t.Fatalf("%+v", in)
		}
	})
	t.Run("R-SIG-015 a create answers 422 at the member for the wrong kind's material, none, a bad key and a refused material", func(t *testing.T) {
		s := env.setup(t, newFakeSigStore(), admin)
		for body, pointer := range map[string]string{
			`{"key":"k","title":"t","kind":"openpgp","publicKey":{"keys":[{"pem":"x"}]}}`:               "/openpgp",
			`{"key":"k","title":"t","kind":"openpgp","openpgp":{"keys":"x"},"ca":{"certificates":"y"}}`: "/ca",
			`{"key":"Bad Key","title":"t","kind":"openpgp","openpgp":{"keys":"x"}}`:                     "/key",
			`{"key":"k","title":"","kind":"openpgp","openpgp":{"keys":"x"}}`:                            "/title",
			`{"key":"k","title":"t","kind":"x509","openpgp":{"keys":"x"}}`:                              "/kind",
			`{"key":"k","title":"t","kind":"ca","ca":{"certificates":"-----BEGIN PRIVATE KEY-----"}}`:   "/ca/certificates",
		} {
			m := problem(t, s.do("POST", "/api/v1/signatures/anchors", body, nil), 422, errs.CodeValidation)
			b, _ := json.Marshal(m["errors"])
			if !strings.Contains(string(b), `"`+pointer+`"`) {
				t.Errorf("%s: %s", body, b)
			}
		}
	})
	t.Run("R-SIG-017 a shipped anchor is switched off by a patch of enabled alone, under its version", func(t *testing.T) {
		st := withAnchors(newFakeSigStore())
		s := env.setup(t, st, admin)
		w := s.do("PATCH", "/api/v1/signatures/anchors/1", `{"enabled":false}`, map[string]string{"If-Match": `"1"`, "Content-Type": MediaMergePatch})
		if w.Code != 200 || w.Header().Get("ETag") != `"2"` {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		p := st.patched[0]
		if p.Enabled == nil || *p.Enabled || p.Title != nil || p.Note != nil || p.Material != nil || st.patchVersions[0] != 1 {
			t.Fatalf("%+v %v", p, st.patchVersions)
		}
		problem(t, s.do("PATCH", "/api/v1/signatures/anchors/1", `{"title":"x"}`, map[string]string{"If-Match": `"2"`, "Content-Type": MediaMergePatch}), 409, errs.CodeAnchorManaged)
		problem(t, s.do("DELETE", "/api/v1/signatures/anchors/2", "", nil), 409, errs.CodeAnchorManaged)
	})
	t.Run("R-API-013 a patch of one member of the material merges into the stored material", func(t *testing.T) {
		st := withAnchors(newFakeSigStore())
		s := env.setup(t, st, admin)
		st.anchors[6] = signatures.AnchorRecord{
			ID: 6, Scope: "installation", Key: "ca", Title: "CA", Kind: "ca", Version: 1, CreatedAt: sigT0(), UpdatedAt: sigT0(),
			Material: mustJSON(map[string]any{"ca": map[string]any{"certificates": "PEM", "subject": map[string]string{"O": "Example GmbH"}}}),
		}
		w := s.do("PATCH", "/api/v1/signatures/anchors/6", `{"ca":{"subject":null},"note":"no restriction"}`, map[string]string{"If-Match": "*", "Content-Type": MediaMergePatch})
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		p := st.patched[0]
		if string(p.Material) != `{"ca":{"certificates":"PEM"}}` || p.Note == nil || *p.Note != "no restriction" || st.patchVersions[0] != 0 {
			t.Fatalf("%s %+v", p.Material, p)
		}
		problem(t, s.do("PATCH", "/api/v1/signatures/anchors/6", `{"openpgp":{"keys":"x"}}`, map[string]string{"If-Match": "*", "Content-Type": MediaMergePatch}), 422, errs.CodeValidation)
		problem(t, s.do("PATCH", "/api/v1/signatures/anchors/6", `{"ca":null}`, map[string]string{"If-Match": "*", "Content-Type": MediaMergePatch}), 422, errs.CodeValidation)
		if w := s.do("PATCH", "/api/v1/signatures/anchors/6", `{"title":"y"}`, map[string]string{"If-Match": `"1"`, "Content-Type": MediaMergePatch}); w.Code != 412 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("R-SIG-037 a project's list carries its anchors, the switch and the installation's applied anchors without material and notes", func(t *testing.T) {
		st := withAnchors(newFakeSigStore())
		w := env.setup(t, st, sigCallers()["member"]).get("/api/v1/projects/web/signatures/anchors")
		var got struct {
			ProjectAnchors bool             `json:"projectAnchors"`
			Items          []map[string]any `json:"items"`
			Installation   []map[string]any `json:"installation"`
		}
		sigBody(t, w, &got)
		if got.ProjectAnchors || len(got.Items) != 1 || got.Items[0]["key"] != "team" || got.Items[0]["openpgp"] == nil {
			t.Fatalf("%s", w.Body.String())
		}
		if len(got.Installation) != 2 {
			t.Fatalf("%s", w.Body.String())
		}
		for _, a := range got.Installation {
			if a["note"] != nil || a["createdBy"] != nil || a["ca"] != nil || a["sigstore"] != nil || a["fingerprints"] == nil {
				t.Fatalf("%v", a)
			}
		}
	})
	t.Run("R-SIG-019 a project anchor is created in the project's scope", func(t *testing.T) {
		st := newFakeSigStore()
		st.settings.ProjectAnchors = true
		w := env.setup(t, st, sigCallers()["owner"]).do("POST", "/api/v1/projects/web/signatures/anchors", sigNewAnchor, nil)
		if w.Code != 201 || st.created[0].Scope != "project" || st.created[0].ProjectID != "p-web" {
			t.Fatalf("%d %s %+v", w.Code, w.Body.String(), st.created)
		}
	})
}

// TestR_SIG_020_Blocks: the checks and the shape of a block.
func TestR_SIG_020_Blocks(t *testing.T) {
	env := newSigEnv(t)
	admin := sigCallers()["security-admin"]
	t.Run("R-SIG-020 a block answers 201 with Location and is listed with its signatures", func(t *testing.T) {
		st := newFakeSigStore()
		s := env.setup(t, st, admin)
		w := s.do("POST", "/api/v1/signatures/blocks", `{"kind":"identity","issuer":"https://gitlab.example.com","value":"^https://gitlab.example.com/web/.+$","reason":"The job was compromised."}`, nil)
		if w.Code != 201 || !strings.HasSuffix(w.Header().Get("Location"), "/api/v1/signatures/blocks/1") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var got struct {
			Items []map[string]any `json:"items"`
		}
		sigBody(t, s.get("/api/v1/signatures/blocks"), &got)
		if len(got.Items) != 1 || got.Items[0]["issuer"] != "https://gitlab.example.com" || got.Items[0]["signatures"] != 0.0 {
			t.Fatalf("%v", got)
		}
		if w := s.do("DELETE", "/api/v1/signatures/blocks/1", "", nil); w.Code != 204 {
			t.Fatalf("%d", w.Code)
		}
		problem(t, s.do("DELETE", "/api/v1/signatures/blocks/1", "", nil), 404, errs.CodeNotFound)
	})
	t.Run("R-SIG-020 a block answers 422 for an identity without issuer, an empty value and a reason that is too long", func(t *testing.T) {
		s := env.setup(t, newFakeSigStore(), admin)
		for body, pointer := range map[string]string{
			`{"kind":"identity","value":"x","reason":"r"}`:                              "/issuer",
			`{"kind":"keyid","value":" ","reason":"r"}`:                                 "/value",
			`{"kind":"keyid","value":"k","reason":"` + strings.Repeat("r", 1001) + `"}`: "/reason",
			`{"kind":"keyid","value":"k","reason":"line\u0007bell"}`:                    "/reason",
		} {
			m := problem(t, s.do("POST", "/api/v1/signatures/blocks", body, nil), 422, errs.CodeValidation)
			b, _ := json.Marshal(m["errors"])
			if !strings.Contains(string(b), `"`+pointer+`"`) {
				t.Errorf("%s: %s", body, b)
			}
		}
	})
}

// sigViewFor builds a view of one signature attached in the repositories given.
func sigViewFor(id int64, subject [32]byte, role string, state signatures.State, trust *signatures.Trust, repos ...int64) signatures.View {
	v := signatures.View{
		Signature: signatures.Signature{
			ID: id, Object: [32]byte{byte(id)}, Subject: subject, Role: role, State: state, ReceivedAt: sigT0(),
			Signer: mustJSON(signatures.Signer{
				Subject: "CN=<b>Release</b>", Identity: "https://gitlab.example.com/web/ui//.gitlab-ci.yml@refs/heads/main",
				CI:  &signatures.SignerCI{BuildSignerURI: "https://gitlab.example.com/web/ui//.gitlab-ci.yml@refs/heads/main"},
				Log: []signatures.LogEntry{{Index: 7, IntegratedTime: sigT0(), Proof: "proof"}},
			}),
			KeyIDs: []string{"ab12"}, TimeProof: "log", ProvenAt: sigT0(),
		},
		Scheme: signatures.SchemeSigstoreBundle, MediaType: "application/vnd.dev.sigstore.bundle.v0.3+json", ObjectSize: 4211, Trust: trust,
	}
	for _, r := range repos {
		v.Attachments = append(v.Attachments, signatures.Attachment{RepoID: r, SignatureID: id, Location: "loc-" + string(rune('0'+r)), Source: "referrer"})
	}
	return v
}

func TestR_SIG_034_Objects(t *testing.T) {
	env := newSigEnv(t)
	obj := [32]byte{9}
	local := qname.Repo{Project: "web", Key: "npm-local"}
	run := func(t *testing.T, c sigCaller, dec sigDecider) *httptest.ResponseRecorder {
		st := newFakeSigStore()
		st.objects[obj] = []byte("<html>original</html>")
		st.holders[obj] = []signatures.Holder{{RepoID: 1, ProjectKey: "web", RepoKey: "npm-local", Format: "npm", Path: "ui/-/ui-1.0.0.tgz"}}
		s := env.setup(t, st, c, func(o *Options) { o.Signatures.Decider = dec })
		return s.get(signatureObjectURL(obj))
	}
	t.Run("R-SIG-034 a reader of a repository that holds an attachment downloads the original as an attachment that no browser renders", func(t *testing.T) {
		w := run(t, sigCallers()["member"], sigDecider{shapes: map[qname.Repo]authz.ReadShape{local: authz.ReadAll}})
		h := w.Header()
		if w.Code != 200 || w.Body.String() != "<html>original</html>" || h.Get("Content-Type") != "text/html" ||
			h.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(h.Get("Content-Disposition"), "attachment;") ||
			!strings.Contains(h.Get("Content-Security-Policy"), "sandbox") {
			t.Fatalf("%d %v %s", w.Code, h, w.Body.String())
		}
	})
	t.Run("R-SIG-034 a reader of a repository read in part downloads it when the holder's path is readable", func(t *testing.T) {
		w := run(t, sigCallers()["member"], sigDecider{shapes: map[qname.Repo]authz.ReadShape{local: authz.ReadRows}, allowed: map[string]bool{"ui/-/ui-1.0.0.tgz": true}})
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("R-SIG-034 every other caller gets 404, whether the object exists or not", func(t *testing.T) {
		problem(t, run(t, sigCallers()["member"], sigDecider{shapes: map[qname.Repo]authz.ReadShape{}}), 404, errs.CodeNotFound)
		problem(t, run(t, sigCallers()["member"], sigDecider{shapes: map[qname.Repo]authz.ReadShape{local: authz.ReadRows}}), 404, errs.CodeNotFound)
		problem(t, env.setup(t, newFakeSigStore(), sigCallers()["admin"], func(o *Options) { o.Signatures.Decider = sigDecider{} }).get(signatureObjectURL([32]byte{1})), 404, errs.CodeNotFound)
	})
}

func TestR_SIG_038_Findings(t *testing.T) {
	env := newSigEnv(t)
	local := qname.Repo{Project: "web", Key: "npm-local"}
	st := newFakeSigStore()
	st.fHolders = []signatures.Holder{{RepoID: 1, ProjectKey: "web", RepoKey: "npm-local"}, {RepoID: 2, ProjectKey: "upstream", RepoKey: "npmjs"}}
	block := &signatures.BlockRecord{ID: 3, Kind: "keyid", Reason: "leaked", CreatedAt: sigT0()}
	st.findings = []signatures.Finding{
		{
			SignatureID: 11, Kind: "invalid", Scheme: "openpgp", Role: "publisher", Reason: "digest_mismatch", Signer: mustJSON(signatures.Signer{KeyID: "ABCD"}), FoundAt: sigT0(),
			Holders: []signatures.Holder{{RepoID: 1, ProjectKey: "web", RepoKey: "npm-local", Format: "npm", Path: "ui/-/ui-1.0.0.tgz"}},
		},
		{
			SignatureID: 10, Kind: "blocked", Scheme: "npm-registry", Role: "registry", Block: block, SubjectName: "ui@1.0.0", FoundAt: sigT0(),
			Holders: []signatures.Holder{{RepoID: 2, ProjectKey: "upstream", RepoKey: "npmjs", Format: "npm"}},
		},
	}
	dec := sigDecider{shapes: map[qname.Repo]authz.ReadShape{local: authz.ReadAll}}
	s := env.setup(t, st, sigCallers()["member"], func(o *Options) { o.Signatures.Decider = dec })
	t.Run("R-SIG-038 a caller sees the entries of the repositories it reads, and a hidden entry does not end the list", func(t *testing.T) {
		w := s.get("/api/v1/signatures/findings?limit=1")
		var got struct {
			Items      []map[string]any `json:"items"`
			NextCursor *string          `json:"nextCursor"`
		}
		sigBody(t, w, &got)
		if w.Code != 200 || len(got.Items) != 1 || got.NextCursor == nil || got.Items[0]["keyId"] != "ABCD" || got.Items[0]["firstRepository"] != "web/npm-local" {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if q := st.findingsQuery[0]; len(q.RepoIDs) != 1 || q.RepoIDs[0] != 1 || q.Limit != 2 {
			t.Fatalf("%+v", q)
		}
		w = s.get("/api/v1/signatures/findings?cursor=" + *got.NextCursor)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if q := st.findingsQuery[1]; q.AfterID != 11 || !q.AfterAt.Equal(sigT0()) {
			t.Fatalf("%+v", q)
		}
	})
	t.Run("R-SIG-038 an entry held only without a path is not shown in a repository read in part", func(t *testing.T) {
		st.findings = st.findings[1:]
		dec := sigDecider{shapes: map[qname.Repo]authz.ReadShape{{Project: "upstream", Key: "npmjs"}: authz.ReadRows}}
		w := env.setup(t, st, sigCallers()["member"], func(o *Options) { o.Signatures.Decider = dec }).get("/api/v1/signatures/findings")
		var got struct {
			Items []map[string]any `json:"items"`
		}
		sigBody(t, w, &got)
		if len(got.Items) != 0 {
			t.Fatalf("%s", w.Body.String())
		}
	})
	t.Run("R-API-011 an unknown kind answers 400", func(t *testing.T) {
		if w := s.get("/api/v1/signatures/findings?kind=expired"); w.Code != 400 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
}
