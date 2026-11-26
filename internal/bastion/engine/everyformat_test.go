package engine

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/formats"
	_ "github.com/guided-traffic/quader/internal/formats/all" // every format, for the route examples of R-BAS-050
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/secrets"
)

// The Bastion's side of "Every format" (bastion.md R-BAS-050 to R-BAS-053, R-BAS-056, R-BAS-057, R-BAS-059), against the
// double of the main installation of support_test.go. Payloads are bytes.

// registryError reads the distribution's error body of an answer below /v2/ (Q-BAS-169) and checks its API version header.
func registryError(t *testing.T, w *httptest.ResponseRecorder) (code, detail string) {
	t.Helper()
	if got := w.Header().Get("Docker-Distribution-Api-Version"); got != "registry/2.0" {
		t.Errorf("Docker-Distribution-Api-Version = %q, want registry/2.0", got)
	}
	var body struct {
		Errors []struct {
			Code, Message string
			Detail        struct{ Code string }
		} `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Errors) != 1 {
		t.Fatalf("no distribution error body: %v %s", err, w.Body)
	}
	return body.Errors[0].Code, body.Errors[0].Detail.Code
}

// registryDetail is the Quader code an answer of the Bastion's own below /v2/ carries in detail.
func registryDetail(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	_, d := registryError(t, w)
	return d
}

// TestR_BAS_050_EveryFormatsReadRoutesReachTheReadFlow: an example of every read route of every format with routes,
// addressed through the Bastion, reaches the read flow and is forwarded with the caller's credential; the registry-root
// format's at /v2/<projectKey>/<repoKey>/…, every other format's at /api/<format>/<projectKey>/<repoKey>/…. The Bastion holds
// no knowledge of a format: a format to come is served the same way (Q-BAS-179).
func TestR_BAS_050_EveryFormatsReadRoutesReachTheReadFlow(t *testing.T) {
	s := newSiteHarness(t, options{})
	// The main installation answers every route of web/repo 404 for a full reader; nothing of it is kept.
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		return resp(http.StatusNotFound, marking(iface.ClassPass, iface.ReadFull, "web/repo", tokenSubject("full", nil)), nil, ""), nil
	}
	seen := 0
	for _, f := range formats.List() {
		d := f.Descriptor()
		for _, rt := range f.Routes() {
			if rt.Action != formats.ActionRead || (rt.Method != http.MethodGet && rt.Method != http.MethodHead) {
				continue
			}
			prefix := "/api/" + string(d.ID) + "/web/repo"
			if d.RegistryRoot {
				prefix = "/v2/web/repo"
			}
			path := prefix + "/" + strings.TrimPrefix(rt.Examples[0], "/")
			if rt.Examples[0] == "" {
				path = prefix
			}
			before := len(s.main.requests())
			w := s.do(rt.Method, path, nil, "Authorization", bearer(s.full))
			reqs := s.main.requests()
			if len(reqs) == before {
				t.Errorf("%s %s (%s): no request reached the main installation: %d %s", rt.Method, path, rt.Name, w.Code, w.Body)
				continue
			}
			last := reqs[len(reqs)-1]
			if last.Kind != mainclient.KindForward || last.Target != path || last.Header.Get("Authorization") != bearer(s.full) {
				t.Errorf("%s %s (%s): forwarded as %s %q with %q", rt.Method, path, rt.Name, last.Kind, last.Target, last.Header.Get("Authorization"))
			}
			seen++
		}
	}
	if seen < 30 {
		t.Fatalf("only %d read routes were sent", seen)
	}
}

// registryMain answers the core routes of the registry root as the main installation does after package B: the token
// service and the catalog pass, the version check with a credential 200; every answer is marked.
func registryMain(s *siteHarness) {
	viaSite := s.main.doFn
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		sub := iface.Subject{Kind: iface.SubjectAnonymous}
		if f.Request.Header.Get("Authorization") != "" || f.Body != nil {
			sub = tokenSubject("full", nil)
		}
		switch p := f.Request.URL.Path; {
		case f.Probe && p == "/v2/_token":
			s.t.Error("the token service's POST was probed: it is a read route with a body of the format table")
			return nil, mainclient.ErrUnreachable
		case p == "/v2/_token":
			// Even a marking that would let the answer be kept must not: the token service's answer is never kept.
			return resp(http.StatusOK, marking(iface.ClassShared, iface.ReadFull, "", sub), http.Header{"Content-Type": {"application/json"}, "Etag": {`"t1"`}},
				`{"token":"qdr_rgt_issued","access_token":"qdr_rgt_issued","expires_in":300}`), nil
		case p == "/v2/_catalog":
			return resp(http.StatusOK, marking(iface.ClassPass, "", "", sub), http.Header{"Content-Type": {"application/json"}}, `{"repositories":[]}`), nil
		case p == "/v2/" || p == "/v2":
			return resp(http.StatusOK, marking(iface.ClassPass, "", "", sub), http.Header{"Docker-Distribution-Api-Version": {"registry/2.0"}}, `{}`), nil
		}
		return viaSite(f)
	}
}

// TestR_BAS_051_TheRegistryRootIsAPathFamilyOfTheBastion: the version check without a credential is the Bastion's own 401
// with the registry's challenge on its base URL; with one it is forwarded. The token service is forwarded with the caller's
// credential, passed on and never kept or collapsed, by GET with Basic and by POST with the form, whose password decides the
// slot class and is checked before anything is sent. A path below /v2/ that names no repository answers NAME_UNKNOWN in the
// distribution's shape without a request.
func TestR_BAS_051_TheRegistryRootIsAPathFamilyOfTheBastion(t *testing.T) {
	s := newSiteHarness(t, options{})
	registryMain(s)
	challenge := `Bearer realm="https://packages.example.com/v2/_token",service="packages.example.com"`

	t.Run("the version check without a credential is the Bastion's own challenge", func(t *testing.T) {
		for _, m := range []string{http.MethodGet, http.MethodHead} {
			w := s.do(m, "/v2/", nil)
			if w.Code != http.StatusUnauthorized || w.Header().Get("Www-Authenticate") != challenge || w.Header().Get("Docker-Distribution-Api-Version") != "registry/2.0" {
				t.Fatalf("%s /v2/: %d %v", m, w.Code, w.Header())
			}
		}
		if n := s.main.count(); n != 0 {
			t.Fatalf("the version check without a credential cost %d requests", n)
		}
		if code, detail := registryError(t, s.get("/v2/")); code != "UNAUTHORIZED" || detail != "credentials_required" {
			t.Fatalf("code %s detail %s", code, detail)
		}
	})
	t.Run("the version check with a credential is forwarded", func(t *testing.T) {
		before := s.main.count(mainclient.KindForward)
		if w := s.get("/v2/", "Authorization", bearer(s.full)); w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		if s.main.count(mainclient.KindForward) != before+1 {
			t.Fatal("not forwarded")
		}
	})
	t.Run("GET of the token service is forwarded with Basic and kept nowhere", func(t *testing.T) {
		basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:"+s.full))
		target := "/v2/_token?scope=repository%3Alibrary%2Fnginx%3Apull&service=packages.example.com"
		for i := 0; i < 2; i++ {
			before := s.main.count(mainclient.KindForward)
			w := s.get(target, "Authorization", basic)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "qdr_rgt_issued") {
				t.Fatalf("token answer %d: %d %s", i, w.Code, w.Body)
			}
			reqs := s.main.requests()
			last := reqs[len(reqs)-1]
			if s.main.count(mainclient.KindForward) != before+1 || last.Header.Get("Authorization") != basic || last.Target != target {
				t.Fatalf("token request %d was not forwarded with the caller's credential: %+v", i, last)
			}
		}
		if s.cache.entryCount() != 0 || s.cache.objectCount() != 0 {
			t.Fatal("the token service's answer is in the cache")
		}
		s.e.answers.mem.retain(func(_ string, v *answerVal) bool {
			if b, _ := json.Marshal(v); strings.Contains(string(b), "qdr_rgt_issued") {
				t.Errorf("an answer holds the issued token: %s", b)
			}
			return true
		})
	})
	t.Run("POST of the token service is forwarded with its form, without a probe", func(t *testing.T) {
		before := len(s.main.requests())
		form := "grant_type=password&username=alice&password=" + s.full + "&service=packages.example.com&scope=repository%3Alibrary%2Fnginx%3Apull"
		w := s.do(http.MethodPost, "/v2/_token", strings.NewReader(form), "Content-Type", "application/x-www-form-urlencoded")
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "qdr_rgt_issued") {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		reqs := s.main.requests()[before:]
		if len(reqs) != 1 || reqs[0].Probe || string(reqs[0].Body) != form || reqs[0].Class != mainclient.Authenticated {
			t.Fatalf("the forward: %+v", reqs)
		}
	})
	t.Run("a malformed token in the form is refused without a request and counted", func(t *testing.T) {
		before, failed := s.main.count(), s.guard.failedCount()
		w := s.do(http.MethodPost, "/v2/_token", strings.NewReader("grant_type=password&username=a&password=qdr_pat_nope"), "Content-Type", "application/x-www-form-urlencoded")
		if w.Code != http.StatusUnauthorized || registryDetail(t, w) != "invalid_token" {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		if s.main.count() != before || s.guard.failedCount() != failed+1 {
			t.Fatalf("requests %d, failures %d", s.main.count()-before, s.guard.failedCount()-failed)
		}
	})
	t.Run("a form above 16 KiB is refused without a request", func(t *testing.T) {
		before := s.main.count()
		w := s.do(http.MethodPost, "/v2/_token", strings.NewReader("password="+strings.Repeat("a", 17<<10)), "Content-Type", "application/x-www-form-urlencoded")
		if w.Code != http.StatusRequestEntityTooLarge || s.main.count() != before {
			t.Fatalf("%d, %d requests", w.Code, s.main.count()-before)
		}
	})
	t.Run("the catalog is forwarded per request", func(t *testing.T) {
		before := s.main.count(mainclient.KindForward)
		for i := 0; i < 2; i++ {
			if w := s.get("/v2/_catalog", "Authorization", bearer(s.full)); w.Code != http.StatusOK {
				t.Fatalf("%d", w.Code)
			}
		}
		if s.main.count(mainclient.KindForward) != before+2 {
			t.Fatal("the catalog was not forwarded per request")
		}
	})
	t.Run("a path that names no repository answers NAME_UNKNOWN without a request", func(t *testing.T) {
		before := s.main.count()
		for _, p := range []string{"/v2/web/docker", "/v2/%77eb/docker/app/manifests/1"} {
			w := s.get(p, "Authorization", bearer(s.full))
			if code, detail := registryError(t, w); w.Code != http.StatusNotFound || code != "NAME_UNKNOWN" || detail != "not_found" {
				t.Fatalf("%s: %d %s %s", p, w.Code, code, detail)
			}
		}
		if s.main.count() != before {
			t.Fatal("a path that names no repository reached the main installation")
		}
	})
	t.Run("an anonymous token request follows the anonymous switch", func(t *testing.T) {
		s.setAnonymous(false)
		w := s.get("/v2/_token?service=packages.example.com")
		if w.Code != http.StatusUnauthorized || registryDetail(t, w) != "credentials_required" || w.Header().Get("Www-Authenticate") != `Basic realm="quader"` {
			t.Fatalf("anonymous off: %d %v %s", w.Code, w.Header(), w.Body)
		}
		s.setAnonymous(true)
		// Answered by the Bastion itself (R-BAS-094): TestR_BAS_094_TheBastionAnswersAnAnonymousTokenRequestItself.
		before := s.main.count()
		if w := s.get("/v2/_token?service=packages.example.com"); w.Code != http.StatusOK || s.main.count() != before {
			t.Fatalf("anonymous on: %d, %d requests", w.Code, s.main.count()-before)
		}
	})
	t.Run("no registry root in the table: nothing below /v2/ is served", func(t *testing.T) {
		s.main.mu.Lock()
		s.main.hs.Formats = iface.HandshakeFormats{{ID: "npm", RateLimitedStatus: 429, ErrorShape: iface.ErrorShapeNPM}}
		s.main.mu.Unlock()
		s.link.RequestHandshake()
		s.tick()
		before := s.main.count()
		w := s.get("/v2/web/docker-local/app/manifests/1", "Authorization", bearer(s.full))
		if w.Code != http.StatusNotFound || registryDetail(t, w) != "not_found" || s.main.count() != before {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	})
}

// registryTokens issues a registry token of a user and one of the anonymous principal with the token service's own
// encoder and a key the Bastion does not have (Q-BAS-185).
func registryTokens(t *testing.T) (user, anonymous string) {
	t.Helper()
	signer := registrySigner(t)
	grant := []registrytoken.Entry{{Repo: qname.Repo{Project: "web", Key: "docker"}, Name: "app", Actions: registrytoken.Pull}}
	user = issueRegistryToken(t, signer, "0192f0c4-0000-7000-8000-000000000001", epoch, grant...)
	anonymous, err := signer.Issue(registrytoken.Claims{Anonymous: true, Endpoint: "default", IssuedAt: epoch, ExpiresAt: epoch.Add(5 * time.Minute), Grant: registrytoken.Grant{Entries: grant}})
	if err != nil {
		t.Fatal(err)
	}
	return user, anonymous
}

// registrySigner is the main installation's registry token signer with a key ring of the test (lifetime 5 minutes): the
// tokens of the engine's tests come from the real encoder, so the claims the Bastion reads without verifying them are the
// token service's (R-BAS-052, Q-BAS-246).
func registrySigner(t *testing.T) *registrytoken.Signer {
	t.Helper()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i + 7)
	}
	p := filepath.Join(t.TempDir(), "k1.key")
	if err := os.WriteFile(p, []byte(base64.StdEncoding.EncodeToString(raw)), 0o600); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.Load(config.Secrets{EncryptionKeyFiles: []string{p}, ActiveKeyID: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	signer, err := registrytoken.NewSigner(ring, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

// issueRegistryToken issues a user's registry token for the Quader token tokenID, issued at the test clock's time and valid
// for 5 minutes, with the grant's entries. Two tokens of one Quader token issued in the same second differ by nothing, as
// at the main installation; a test that needs two takes two seconds or two token IDs.
func issueRegistryToken(t *testing.T, signer *registrytoken.Signer, tokenID string, issued time.Time, grant ...registrytoken.Entry) string {
	t.Helper()
	tok, err := signer.Issue(registrytoken.Claims{
		PrincipalKind: "user", PrincipalID: "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10",
		Binding:  registrytoken.Binding{TokenID: tokenID, TokenType: "pat", SecretSHA256: sha256.Sum256([]byte("qdr_pat_x"))},
		Endpoint: "default", IssuedAt: issued.Truncate(time.Second), ExpiresAt: issued.Truncate(time.Second).Add(5 * time.Minute),
		Grant: registrytoken.Grant{Entries: grant},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// TestR_BAS_052_RegistryTokensAtTheBastion: below /v2/ a registry token is a credential checked by its form, forwarded
// unchanged, its shape capped at partial; elsewhere its prefix is unknown. A malformed one is refused and counted, an
// anonymous one is the anonymous caller, one past the expiry its claims name is refused without a request and without a
// count, also by a replica that never saw it (Q-BAS-252), and insufficient_scope is no statement about the credential.
func TestR_BAS_052_RegistryTokensAtTheBastion(t *testing.T) {
	user, anon := registryTokens(t)
	s := newSiteHarness(t, options{})
	manifest := "/v2/web/docker/app/manifests/sha256:" + strings.Repeat("ab", 32)
	exp := epoch.Add(5 * time.Minute)
	scoped := false
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		body := `{"schemaVersion":2}`
		h := http.Header{"Content-Type": {"application/vnd.oci.image.manifest.v1+json"}, "Etag": {`"sha256:` + sha([]byte(body)) + `"`}}
		switch f.Request.Header.Get("Authorization") {
		case "Bearer " + user:
			if scoped {
				return resp(http.StatusUnauthorized, marking(iface.ClassPass, "", "web/docker", iface.Subject{Kind: iface.SubjectInvalid}),
					http.Header{"Www-Authenticate": {`Bearer realm="https://packages.example.com/v2/_token",service="packages.example.com",scope="repository:app:pull",error="insufficient_scope"`}}, `{"errors":[]}`), nil
			}
			// A main installation that did not cap the shape: the Bastion caps it itself.
			return resp(http.StatusOK, marking(iface.ClassArtifact, iface.ReadFull, "web/docker", tokenSubject("reg", &exp)), h, body), nil
		case "":
			return resp(http.StatusOK, marking(iface.ClassArtifact, iface.ReadFull, "web/docker", iface.Subject{Kind: iface.SubjectAnonymous}), h, body), nil
		}
		return resp(http.StatusUnauthorized, marking(iface.ClassPass, "", "", iface.Subject{Kind: iface.SubjectInvalid}), nil, ""), nil
	}

	t.Run("a well-formed token is forwarded below /v2/ and its shape is at most partial", func(t *testing.T) {
		before := s.main.count(mainclient.KindForward)
		if w := s.get(manifest, "Authorization", bearer(user)); w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		reqs := s.main.requests()
		if s.main.count(mainclient.KindForward) != before+1 || reqs[len(reqs)-1].Header.Get("Authorization") != bearer(user) {
			t.Fatal("not forwarded unchanged")
		}
		if sh := s.e.answers.shape(t.Context(), credentialKey(user), "web/docker"); sh == nil || sh.Read != iface.ReadPartial {
			t.Fatalf("the shape of a registry token is %+v, want partial", sh)
		}
	})
	t.Run("on another family its prefix is unknown", func(t *testing.T) {
		before, failed := s.main.count(), s.guard.failedCount()
		w := s.get("/api/npm/web/npm-all/lodash", "Authorization", bearer(user))
		if w.Code != http.StatusUnauthorized || s.main.count() != before || s.guard.failedCount() != failed+1 {
			t.Fatalf("%d, %d requests, %d failures", w.Code, s.main.count()-before, s.guard.failedCount()-failed)
		}
	})
	t.Run("a malformed token is refused with the registry's challenge and counted", func(t *testing.T) {
		for _, tok := range []string{iface.RegistryTokenPrefix + "!!", iface.RegistryTokenPrefix + strings.Repeat("a", 5<<10) + ".b"} {
			before, failed := s.main.count(), s.guard.failedCount()
			w := s.get(manifest, "Authorization", bearer(tok))
			if w.Code != http.StatusUnauthorized || registryDetail(t, w) != "invalid_token" ||
				w.Header().Get("Www-Authenticate") != `Bearer realm="https://packages.example.com/v2/_token",service="packages.example.com",error="invalid_token"` {
				t.Fatalf("%d %v %s", w.Code, w.Header(), w.Body)
			}
			if s.main.count() != before || s.guard.failedCount() != failed+1 {
				t.Fatalf("%d requests, %d failures", s.main.count()-before, s.guard.failedCount()-failed)
			}
		}
	})
	t.Run("an anonymous token is the anonymous caller", func(t *testing.T) {
		other := strings.Replace(manifest, "abab", "cdcd", 1)
		before := len(s.main.requests())
		if w := s.get(other, "Authorization", bearer(anon)); w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		reqs := s.main.requests()[before:]
		if len(reqs) != 1 || reqs[0].Header.Get("Authorization") != "" || reqs[0].Class != mainclient.Anonymous {
			t.Fatalf("forwarded as %+v", reqs)
		}
		if sh := s.e.answers.shape(t.Context(), anonymousKey, "web/docker"); sh == nil || sh.Read != iface.ReadFull {
			t.Fatalf("the anonymous caller's shape is %+v", sh)
		}
		// A second anonymous pull with another anonymous token shares the anonymous caller's answers and entry.
		_, anon2 := registryTokens(t)
		before = s.main.count()
		if w := s.get(other, "Authorization", bearer(anon2)); w.Code != http.StatusOK || s.main.count() != before {
			t.Fatalf("a second anonymous token: %d, %d requests", w.Code, s.main.count()-before)
		}
	})
	t.Run("insufficient_scope is replayed, keeps no answer and counts nothing", func(t *testing.T) {
		scoped = true
		defer func() { scoped = false }()
		failed := s.guard.failedCount()
		other := strings.Replace(manifest, "abab", "efef", 1)
		w := s.get(other, "Authorization", bearer(user))
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Header().Get("Www-Authenticate"), `error="insufficient_scope"`) {
			t.Fatalf("%d %v", w.Code, w.Header())
		}
		if s.guard.failedCount() != failed {
			t.Fatal("insufficient_scope counted as a failed authentication")
		}
		if a := s.e.answers.credential(t.Context(), credentialKey(user)); a == nil || a.Subject.Kind == iface.SubjectInvalid {
			t.Fatalf("the credential answer after insufficient_scope: %+v", a)
		}
	})
	t.Run("a token past the expiry its claims name is refused without a request and without a count", func(t *testing.T) {
		s.clock.advance(6 * time.Minute)
		// The replica that saw the token, and one that never did (H-1295, closed by Q-BAS-252): the expiry is the claims'.
		other := newSiteHarness(t, options{clock: s.clock})
		for name, h := range map[string]*siteHarness{"the replica that saw it": s, "a replica that never saw it": other} {
			before, failed := h.main.count(), h.guard.failedCount()
			w := h.get(manifest, "Authorization", bearer(user))
			if w.Code != http.StatusUnauthorized || !strings.Contains(w.Header().Get("Www-Authenticate"), `error="invalid_token"`) {
				t.Fatalf("%s: %d %v", name, w.Code, w.Header())
			}
			if h.main.count() != before || h.guard.failedCount() != failed {
				t.Fatalf("%s: %d requests, %d failures", name, h.main.count()-before, h.guard.failedCount()-failed)
			}
		}
	})
}

// TestR_BAS_053_ContainerImagesFollowTheRouteKinds: the registry's ETag "sha256:<hex>" is a content address, so a manifest by
// digest is kept as an artifact and revalidated with its ETag as given; a cached answer replays the headers the format
// declares (a HEAD of a tag from the cache carries Docker-Content-Digest, a tags list its Link) and drops what no list
// names.
func TestR_BAS_053_ContainerImagesFollowTheRouteKinds(t *testing.T) {
	s := newSiteHarness(t, options{})
	manifest := []byte(`{"schemaVersion":2,"layers":[]}`)
	digest := "sha256:" + sha(manifest)
	byDigest, byTag, tags := "/v2/web/docker/app/manifests/"+digest, "/v2/web/docker/app/manifests/1", "/v2/web/docker/app/tags/list?n=2"
	s.main.checkFn = func(req iface.CheckRequest) (iface.CheckAnswer, error) {
		return iface.CheckAnswer{Subject: tokenSubject("full", nil), Repositories: map[string]iface.RepositoryRead{"web/docker": {Read: iface.ReadFull}}}, nil
	}
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		sub := tokenSubject("full", nil)
		h := http.Header{
			"Content-Type": {"application/vnd.oci.image.manifest.v1+json"}, "Docker-Content-Digest": {digest},
			"Docker-Distribution-Api-Version": {"registry/2.0"}, "X-Undeclared": {"dropped"},
		}
		switch f.Request.URL.RequestURI() {
		case byDigest:
			h.Set("Etag", `"`+digest+`"`)
			if f.IfNoneMatch == `"`+digest+`"` {
				return resp(http.StatusNotModified, marking(iface.ClassArtifact, iface.ReadFull, "web/docker", sub), h, ""), nil
			}
			return resp(http.StatusOK, marking(iface.ClassArtifact, iface.ReadFull, "web/docker", sub), h, string(manifest)), nil
		case byTag:
			h.Set("Etag", `"`+digest+`"`)
			return resp(http.StatusOK, marking(iface.ClassShared, iface.ReadFull, "web/docker", sub), h, string(manifest)), nil
		case tags:
			h.Set("Etag", `"tags-1"`)
			h.Set("Link", `</v2/web/docker/app/tags/list?n=2&last=2>; rel="next"`)
			return resp(http.StatusOK, marking(iface.ClassShared, iface.ReadFull, "web/docker", sub), h, `{"name":"app","tags":["1","2"]}`), nil
		}
		return resp(http.StatusNotFound, marking(iface.ClassPass, "", "", sub), nil, ""), nil
	}
	for _, p := range []string{byDigest, byTag, tags} {
		if w := s.get(p, "Authorization", bearer(s.full)); w.Code != http.StatusOK {
			t.Fatalf("%s: %d", p, w.Code)
		}
	}
	if ent, ok := s.e.loadEntry(t.Context(), byDigest); !ok || ent.Kind != "artifact" || ent.SHA256 != sha(manifest) {
		t.Fatalf("the manifest by digest is not kept under its content address: %+v", ent)
	}
	before := s.main.count()
	w := s.do(http.MethodHead, byTag, nil, "Authorization", bearer(s.full))
	if w.Code != http.StatusOK || w.Header().Get("Docker-Content-Digest") != digest || w.Header().Get("X-Undeclared") != "" {
		t.Fatalf("HEAD of the tag from the cache: %d %v", w.Code, w.Header())
	}
	w = s.get(tags, "Authorization", bearer(s.full))
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Link"), `rel="next"`) || w.Header().Get("Docker-Distribution-Api-Version") != "registry/2.0" {
		t.Fatalf("the tags list from the cache: %d %v", w.Code, w.Header())
	}
	if s.main.count() != before {
		t.Fatal("a warm read cost a request")
	}
	// After the period the Bastion revalidates the artifact with the registry's spelling of its ETag.
	s.clock.advance(s.cfg.Bastion.Cache.ArtifactRevalidateAfter + time.Second)
	s.clock.advance(s.cfg.Bastion.Answers.Period)
	if w := s.get(byDigest, "Authorization", bearer(s.full)); w.Code != http.StatusOK || w.Body.String() != string(manifest) {
		t.Fatalf("after the period: %d", w.Code)
	}
	var inm []string
	for _, r := range s.main.requests() {
		if r.Kind == mainclient.KindConditional {
			inm = append(inm, r.INM)
		}
	}
	if !slices.Contains(inm, `"`+digest+`"`) {
		t.Fatalf("the conditional requests named %q, want the ETag as given", inm)
	}
}

// TestR_BAS_053_TheEnginesHeaderListsAreTheCatalogs holds the engine's header lists to the catalog's (Q-BAS-181): what a
// cached answer replays is the core's representation headers, and every per-request header of the core is either dropped
// from every answer or framing of the one answer passed through, never replayed from the cache.
func TestR_BAS_053_TheEnginesHeaderListsAreTheCatalogs(t *testing.T) {
	if !slices.Equal(representationHeaders(), catalog.RepresentationHeaders()) {
		t.Fatalf("the engine replays %v, the catalog names %v", representationHeaders(), catalog.RepresentationHeaders())
	}
	if !slices.Equal(perRequestHeaders(), catalog.PerRequestHeaders()) {
		t.Fatalf("the engine's per-request headers %v, the catalog's %v", perRequestHeaders(), catalog.PerRequestHeaders())
	}
	passedOn := []string{"Content-Length", "Content-Range", "Accept-Ranges", "Date", "Age", "Retry-After", "Quader-Resolved-From"}
	all := http.Header{}
	for _, h := range catalog.PerRequestHeaders() {
		all.Set(h, "x")
		if !neverReplayed(h) && !slices.Contains(passedOn, h) {
			t.Errorf("the per-request header %s is neither dropped nor framing of an answer passed through", h)
		}
	}
	for _, h := range []string{"X-Quader-Replica", "Cache-Status", "Set-Cookie", "X-Request-Id"} {
		if !neverReplayed(h) {
			t.Errorf("%s is replayed", h)
		}
		if h != "Set-Cookie" && !slices.Contains(catalog.PerRequestHeaders(), h) {
			t.Errorf("%s is dropped by the engine but is no per-request header of the catalog", h)
		}
	}
	if kept := keepHeader(all, []string{"Retry-After", "Quader-Resolved-From"}); len(kept) != 0 {
		t.Errorf("a cached answer replays the per-request headers %v", kept)
	}
}

// TestR_BAS_022_AHitCarriesTheContentHeadersOfRARCH007 closes H-1250: an answer served from the cache carries the content
// headers every content answer of the main installation carries, as the miss did.
func TestR_BAS_022_AHitCarriesTheContentHeadersOfRARCH007(t *testing.T) {
	s := newSiteHarness(t, options{})
	body := payload(300)
	viaSite := s.main.doFn
	s.site.artifacts[tgz] = body
	content := map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"Content-Security-Policy":      "sandbox; default-src 'none'",
		"Cross-Origin-Resource-Policy": "same-site",
	}
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		r, err := viaSite(f)
		for k, v := range content {
			r.Header.Set(k, v)
		}
		return r, err
	}
	miss := s.get(tgz, "Authorization", bearer(s.full))
	before := s.main.count()
	hit := s.get(tgz, "Authorization", bearer(s.full))
	if s.main.count() != before {
		t.Fatal("the second request was no hit")
	}
	for k, v := range content {
		if miss.Header().Get(k) != v || hit.Header().Get(k) != v {
			t.Errorf("%s: miss %q, hit %q, want %q", k, miss.Header().Get(k), hit.Header().Get(k), v)
		}
	}
}

// TestR_BAS_056_TheFormatsRateLimitStatusAndIfModifiedSince: the Bastion's own refusals of its limits and its block list on
// the family of a format that declares 503 for a rate limit are 503 with Retry-After, 429 elsewhere; freshclam's check,
// Range: bytes=0-511 with If-Modified-Since, is answered from the entry: 304 when the entry is not newer, 206 otherwise.
func TestR_BAS_056_TheFormatsRateLimitStatusAndIfModifiedSince(t *testing.T) {
	s := newSiteHarness(t, options{})
	daily := "/api/clamav/upstream/clamav/daily.cvd"
	t.Run("the limits and the block list take the format's status", func(t *testing.T) {
		refused := guard.Verdict{Status: 429, Code: iface.CodeRateLimited, RetryAfter: 7 * time.Second, Reason: metrics.RefusedRateLimited}
		blocked := guard.Verdict{Status: 429, Code: iface.CodeAddressBlocked, RetryAfter: time.Minute, Reason: metrics.RefusedBlocked}
		for _, c := range []struct {
			name string
			set  func(guard.Verdict)
			v    guard.Verdict
		}{
			{"request limit", func(v guard.Verdict) { s.guard.request = v }, refused},
			{"block list", func(v guard.Verdict) { s.guard.blocked = v }, blocked},
		} {
			c.set(c.v)
			w := s.get(daily)
			if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" || problemCode(w) != c.v.Code {
				t.Errorf("%s on the ClamAV definitions: %d %v %s", c.name, w.Code, w.Header(), w.Body)
			}
			if w := s.get("/api/npm/web/npm-all/lodash"); w.Code != http.StatusTooManyRequests {
				t.Errorf("%s on npm: %d", c.name, w.Code)
			}
			c.set(guard.Allow())
		}
	})
	t.Run("freshclam's check is answered from the entry", func(t *testing.T) {
		container := payload(2000)
		lm := epoch.Add(-time.Hour).Format(http.TimeFormat)
		s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
			h := http.Header{"Content-Type": {"application/octet-stream"}, "Etag": {`"` + sha(container) + `"`}, "Last-Modified": {lm}}
			return resp(http.StatusOK, marking(iface.ClassShared, iface.ReadFull, "upstream/clamav", iface.Subject{Kind: iface.SubjectAnonymous}), h, string(container)), nil
		}
		check := func(since time.Time) *httptest.ResponseRecorder {
			return s.get(daily, "Range", "bytes=0-511", "If-Modified-Since", since.Format(http.TimeFormat))
		}
		if w := check(epoch.Add(-2 * time.Hour)); w.Code != http.StatusPartialContent || w.Body.Len() != 512 {
			t.Fatalf("the cold check: %d, %d bytes", w.Code, w.Body.Len())
		}
		before := s.main.count()
		if w := check(epoch); w.Code != http.StatusNotModified || w.Header().Get("Last-Modified") != lm {
			t.Fatalf("the check with the build time: %d %v", w.Code, w.Header())
		}
		if w := check(epoch.Add(-2 * time.Hour)); w.Code != http.StatusPartialContent || w.Body.Len() != 512 || w.Body.String() != string(container[:512]) {
			t.Fatalf("the check with an older date: %d, %d bytes", w.Code, w.Body.Len())
		}
		if s.main.count() != before {
			t.Fatal("the warm checks reached the main installation")
		}
	})
}

// TestR_BAS_057_ABoundHostIsClassifiedAsItsRepository: a Bastion whose host the main installation binds to a repository
// reads every path below /v2/ but the core routes as that repository, as the main installation's dispatcher does
// (formats/oci.md R-OCI-029, Q-BAS-195); the binding comes with the handshake.
func TestR_BAS_057_ABoundHostIsClassifiedAsItsRepository(t *testing.T) {
	s := newSiteHarness(t, options{})
	s.main.mu.Lock()
	s.main.hs.BoundRepository = "web/docker-hub"
	s.main.mu.Unlock()
	s.link.RequestHandshake()
	s.tick()
	var repos []string
	s.main.checkFn = func(req iface.CheckRequest) (iface.CheckAnswer, error) {
		repos = append(repos, req.Repositories...)
		return iface.CheckAnswer{Subject: tokenSubject("full", nil), Repositories: map[string]iface.RepositoryRead{req.Repositories[0]: {Read: iface.ReadFull}}}, nil
	}
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		return resp(http.StatusOK, marking(iface.ClassShared, iface.ReadFull, "web/docker-hub", tokenSubject("full", nil)),
			http.Header{"Etag": {`"m1"`}, "Content-Type": {"application/json"}}, `{}`), nil
	}
	for _, p := range []string{"/v2/library/alpine/manifests/3", "/v2/web/docker-local/app/manifests/1"} {
		before := s.main.count(mainclient.KindForward)
		if w := s.get(p, "Authorization", bearer(s.full)); w.Code != http.StatusOK || s.main.count(mainclient.KindForward) != before+1 {
			t.Fatalf("%s: %d", p, w.Code)
		}
	}
	if sh := s.e.answers.shape(t.Context(), credentialKey(s.full), "web/docker-hub"); sh == nil || sh.Read != iface.ReadFull {
		t.Fatalf("the shape on the bound repository: %+v", sh)
	}
	if w := s.get("/v2/_catalog", "Authorization", bearer(s.full)); w.Code != http.StatusOK {
		t.Fatalf("the catalog on a bound host: %d", w.Code)
	}
	if len(repos) != 0 {
		t.Fatalf("checks named %v", repos)
	}
}

// TestR_BAS_059_ACountedEntryIsReportedOnEveryServe: the Bastion reports a serve from its cache as a download exactly when
// the main installation's marking said its fill counted one (Q-BAS-177), a document included; an artifact the main
// installation did not count is not reported.
func TestR_BAS_059_ACountedEntryIsReportedOnEveryServe(t *testing.T) {
	s := newSiteHarness(t, options{})
	doc, plain, art := "/v2/web/docker/app/manifests/1", "/v2/web/docker/app/manifests/2", "/v2/web/docker/app/blobs/sha256:"+sha([]byte("blob"))
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		sub := tokenSubject("full", nil)
		switch f.Request.URL.Path {
		case doc:
			m := marking(iface.ClassShared, iface.ReadFull, "web/docker", sub)
			m.Counted = true
			return resp(http.StatusOK, m, http.Header{"Etag": {`"m1"`}}, `{"tag":1}`), nil
		case plain:
			return resp(http.StatusOK, marking(iface.ClassShared, iface.ReadFull, "web/docker", sub), http.Header{"Etag": {`"m2"`}}, `{"tag":2}`), nil
		}
		return resp(http.StatusOK, marking(iface.ClassArtifact, iface.ReadFull, "web/docker", sub), http.Header{"Etag": {`"` + sha([]byte("blob")) + `"`}}, "blob"), nil
	}
	for _, p := range []string{doc, plain, art} {
		if w := s.get(p, "Authorization", bearer(s.full)); w.Code != 200 {
			t.Fatalf("%s: %d", p, w.Code)
		}
	}
	if n := len(s.dl.list()); n != 0 {
		t.Fatalf("a fill was reported %d times; the main installation counted it", n)
	}
	for _, p := range []string{doc, plain, art} {
		s.get(p, "Authorization", bearer(s.full))
	}
	s.do(http.MethodHead, doc, nil, "Authorization", bearer(s.full))
	ev := s.dl.list()
	if len(ev) != 1 || ev[0].Path != doc || ev[0].Repository != "web/docker" || ev[0].SHA256 != sha([]byte(`{"tag":1}`)) {
		t.Fatalf("download events %+v, want the counted document's hit only", ev)
	}
}
