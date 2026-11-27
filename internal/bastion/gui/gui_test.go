package gui

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/catalogue"
	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/legal"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/config"
)

// The web interface's handler against a reader of the catalogue in memory and a build of three files per locale: every case runs
// in microseconds, the package in well under a second.

// reader is a catalogue in memory: a state, packages and documents by path.
type reader struct {
	mu    sync.Mutex
	state catalogue.State
	pkgs  []catalogue.Entry
	docs  map[string][]byte
	hits  []catalogue.DocHit
	// reads counts every read, which a request that must not read counts against.
	reads int
	// revision is what Packages answers as the revision of the packages.
	revision string
}

func (r *reader) State() catalogue.State { r.mu.Lock(); defer r.mu.Unlock(); return r.state }

func (r *reader) ConfirmedAt() time.Time { return r.State().ConfirmedAt }

func (r *reader) served() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	switch {
	case r.state.Decided && !r.state.Enabled:
		return catalogue.ErrOff
	case !r.state.Ready:
		return catalogue.ErrNotReady
	}
	return nil
}

func (r *reader) PublicSet() (iface.CataloguePublic, error) {
	return iface.CataloguePublic{
		Projects: []iface.CatalogueProject{
			{Key: "acme", DisplayName: "Acme platform", Description: "The packages Acme publishes."},
			{Key: "tools", DisplayName: "tools"},
			{Key: "upstream", DisplayName: "Upstream"},
			{Key: "web", DisplayName: "Web"},
		},
		Repositories: []iface.CatalogueRepository{
			{Name: "acme/npm", Format: "npm", Class: "local", Wiki: true},
			{Name: "upstream/npmjs", Format: "npm", Class: "remote"},
			{Name: "web/galaxy", Format: "ansible", Class: "local", Wiki: true},
		},
	}, r.served()
}

func (r *reader) Packages() ([]catalogue.Entry, string, error) {
	if err := r.served(); err != nil {
		return nil, "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]catalogue.Entry(nil), r.pkgs...), r.revision, nil
}

// Files is the repositories of the documents of a root folder (files/<project>/<key>/folder).
func (r *reader) Files() ([]string, error) {
	if err := r.served(); err != nil {
		return nil, err
	}
	out := []string{}
	for path := range r.docs {
		if p, err := iface.ParseCataloguePath(path); err == nil && p.Kind == iface.CatalogueFolder && p.File == "" {
			out = append(out, p.Project+"/"+p.Repository)
		}
	}
	return out, nil
}

func (r *reader) Search(q catalogue.Query) (catalogue.Page, error) {
	if err := r.served(); err != nil {
		return catalogue.Page{}, err
	}
	if q.Sort == "bad" {
		return catalogue.Page{}, catalogue.ErrQuery
	}
	var out []catalogue.Entry
	for _, e := range r.pkgs {
		inProject := q.Project == ""
		for _, repo := range e.Repositories {
			inProject = inProject || strings.HasPrefix(repo, q.Project+"/")
		}
		if inProject && (q.Q == "" || strings.Contains(e.Name, q.Q)) {
			out = append(out, e)
		}
	}
	p := catalogue.Page{Items: out}
	if q.Limit > 0 && len(out) > q.Limit {
		p.Items, p.Next = out[:q.Limit], "c1"
	}
	return p, nil
}

func (r *reader) Package(id string) (catalogue.Entry, bool, error) {
	if err := r.served(); err != nil {
		return catalogue.Entry{}, false, err
	}
	for _, e := range r.pkgs {
		if e.ID == id {
			return e, true, nil
		}
	}
	return catalogue.Entry{}, false, nil
}

func (r *reader) Lookup(format, name string) (catalogue.Entry, bool, error) {
	if err := r.served(); err != nil {
		return catalogue.Entry{}, false, err
	}
	for _, e := range r.pkgs {
		if e.Format == format && (e.Name == name || e.Key == name) {
			return e, true, nil
		}
	}
	return catalogue.Entry{}, false, nil
}

func (r *reader) Versions(_ context.Context, id, after string, limit int) (catalogue.VersionPage, error) {
	if err := r.served(); err != nil {
		return catalogue.VersionPage{}, err
	}
	all := []json.RawMessage{json.RawMessage(`{"version":"1.1.0"}`), json.RawMessage(`{"version":"1.0.0"}`)}
	if after == "1.1.0" {
		return catalogue.VersionPage{Items: all[1:]}, nil
	}
	if limit == 1 {
		return catalogue.VersionPage{Items: all[:1], Next: "1.1.0"}, nil
	}
	return catalogue.VersionPage{Items: all}, nil
}

func (r *reader) Document(_ context.Context, path string) (catalogue.Document, bool, error) {
	if err := r.served(); err != nil {
		return catalogue.Document{}, false, err
	}
	raw, ok := r.docs[path]
	if !ok {
		return catalogue.Document{}, false, nil
	}
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	sum := sha256.Sum256(raw)
	return catalogue.Document{Path: path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(raw)), Raw: raw, Gzip: b.Bytes()}, true, nil
}

// SearchDocs answers the hits whose name holds the text, as the reader's own tests hold its rule.
func (r *reader) SearchDocs(q catalogue.DocQuery) (catalogue.DocPage, error) {
	if err := r.served(); err != nil {
		return catalogue.DocPage{}, err
	}
	if len(q.Q) < 2 {
		return catalogue.DocPage{}, catalogue.ErrQuery
	}
	p := catalogue.DocPage{Items: []catalogue.DocHit{}}
	if !r.state.Docs {
		return p, nil
	}
	for _, d := range r.hits {
		if strings.Contains(d.Name, q.Q) && (q.Project == "" || d.Project == q.Project) {
			p.Items = append(p.Items, d)
		}
	}
	return p, nil
}

// DocEntries answers the hits while docs is on, none while it is off, as the reader's own tests hold it.
func (r *reader) DocEntries() ([]catalogue.DocHit, error) {
	if err := r.served(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.state.Docs {
		return []catalogue.DocHit{}, nil
	}
	return append([]catalogue.DocHit{}, r.hits...), nil
}

var confirmed = time.Date(2026, 11, 26, 9, 30, 0, 0, time.UTC)

func onState() catalogue.State {
	return catalogue.State{Enabled: true, Docs: true, Decided: true, Ready: true, ConfirmedAt: confirmed, StaleAfter: time.Hour}
}

const pkgID = "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10"

func newReader() *reader {
	return &reader{
		state: onState(),
		pkgs: []catalogue.Entry{
			{ID: pkgID, Format: "npm", Name: "@acme/ui", Key: "@acme/ui", Repositories: []string{"acme/npm"}, Summary: json.RawMessage(`{"id":"` + pkgID + `","name":"@acme/ui"}`)},
			{ID: "r1", Format: "ansible", Name: "acme.web", Key: "web", Repositories: []string{"web/galaxy"}, Summary: json.RawMessage(`{"id":"r1","name":"acme.web"}`)},
		},
		hits: []catalogue.DocHit{{
			Format: "ansible", PackageID: "r1", Package: "acme.web", Version: "1.0.0", Project: "web", Repository: "galaxy",
			Type: "module", Name: "web_server", ShortDescription: "Serves the web",
		}},
		docs: map[string][]byte{
			"packages/" + pkgID + "/versions/1.0.0%2Bbuild.1":       []byte(`{"version":"1.0.0+build.1"}`),
			"packages/" + pkgID + "/versions/1.0.0/vulnerabilities": []byte(`{"repositories":[]}`),
			"packages/" + pkgID + "/vulnerabilities":                []byte(`{"packageId":"` + pkgID + `","versions":[{"version":"1.0.0","grade":{"grade":"B"}}]}`),
			"files/tools/downloads/folder":                          []byte(`{"items":[{"path":"tools","kind":"folder"}]}`),
			"files/tools/downloads/folder/tools":                    []byte(`{"items":[{"path":"agent.tar.gz","kind":"file"}]}`),
			"files/tools/downloads/file/tools/agent.tar.gz":         []byte(`{"path":"tools/agent.tar.gz","kind":"file"}`),
			"signing/chain.pem":                                     []byte("-----BEGIN CERTIFICATE-----\n"),
			"index/0":                                               []byte(`{"items":[]}`),
		},
	}
}

func bundleFS() fstest.MapFS {
	m := fstest.MapFS{"index.html": {Data: []byte("<style nonce=\"QUADER_CSP_NONCE\"></style>placeholder")}}
	for _, l := range []string{"en", "de"} {
		m[l+"/index.html"] = &fstest.MapFile{Data: []byte(`<html lang="` + l + `"><quader-bastion-root ngCspNonce="QUADER_CSP_NONCE"></quader-bastion-root></html>`)}
		m[l+"/main-ABCD1234.js"] = &fstest.MapFile{Data: []byte("console.log(1)")}
		m[l+"/favicon.svg"] = &fstest.MapFile{Data: []byte("<svg/>")}
	}
	return m
}

func legalPages(t *testing.T) *legal.Pages {
	t.Helper()
	p, err := legal.New(config.UILegal{Imprint: config.UILegalText{De: "# Impressum\n\nBeispiel GmbH"}}, "https://bastion.example.com")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func newGUI(t *testing.T, r *reader, mod func(*Options)) *Handler {
	t.Helper()
	o := Options{Catalogue: r, Legal: legalPages(t), BaseURL: "https://bastion.example.com", Assets: bundleFS()}
	if mod != nil {
		mod(&o)
	}
	h, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func do(h http.Handler, method, target string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader("body"))
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// problemCode reads the code of a problem answer, or "" for another body.
func problemCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/problem+json") {
		return ""
	}
	var p struct {
		Type string `json:"type"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("problem body %q: %v", rec.Body.String(), err)
	}
	if p.Type != "about:blank" {
		t.Errorf("problem type %q, want about:blank (Q-BAS-321)", p.Type)
	}
	return p.Code
}

// addresses is one address of every kind the web interface serves.
var addresses = []string{
	"/", "/ui", "/ui/", "/ui/en/", "/ui/de/projects/acme/packages/npm/@acme/ui", "/ui/en/main-ABCD1234.js",
	"/ui/data/v1/state", "/ui/data/v1/legal?locale=en", "/ui/data/v1/packages", "/ui/data/v1/packages/lookup?format=npm&name=x",
	"/ui/data/v1/packages/" + pkgID + "/versions", "/ui/data/v1/signing/chain.pem", "/ui/data/v1/search/documentation?q=web",
	"/ui/data/v2/x",
}

// TestR_BAS_116_EveryAddressReadsOnlyAndAnswersAnUnsafeMethod405: GET and HEAD only; every other method is 405 with Allow, its body
// unread and nothing of the catalogue read (W-35's B.7).
func TestR_BAS_116_EveryAddressReadsOnlyAndAnswersAnUnsafeMethod405(t *testing.T) {
	r := newReader()
	h := newGUI(t, r, nil)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, "PROPFIND"} {
		for _, a := range addresses {
			rec := do(h, m, a)
			if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" || problemCode(t, rec) != "method_not_allowed" {
				t.Errorf("%s %s = %d Allow %q %s", m, a, rec.Code, rec.Header().Get("Allow"), rec.Body.String())
			}
		}
	}
	if r.reads != 0 {
		t.Errorf("an unsafe method read the catalogue %d times", r.reads)
	}
	if !Owns("/") || !Owns("/ui") || !Owns("/ui/x") || Owns("/uix") || Owns("/api/npm/a/b/x") || Owns("/v2/") || Owns("/files/a") {
		t.Error("Owns claims the wrong paths")
	}
}

// TestR_BAS_117_EveryAnswerCarriesTheHeadersAndNoCookie: the policy of a page and of data, frames, the referrer, the resource
// policy, nosniff on every answer, no cookie and no header of the product's own (W-35's B.7, B.13).
func TestR_BAS_117_EveryAnswerCarriesTheHeadersAndNoCookie(t *testing.T) {
	h := newGUI(t, newReader(), nil)
	nonce := regexp.MustCompile(`'nonce-([A-Za-z0-9]{16,})'`)
	for _, a := range addresses {
		for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			rec := do(h, m, a, "Accept-Language", "de")
			hd := rec.Header()
			if hd.Get("X-Frame-Options") != "DENY" || hd.Get("Cross-Origin-Resource-Policy") != "same-origin" ||
				hd.Get("Referrer-Policy") != "no-referrer" || hd.Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("%s %s: headers %v", m, a, hd)
			}
			if len(hd.Values("Set-Cookie")) > 0 || hd.Get("Server") != "" {
				t.Errorf("%s %s: Set-Cookie %q Server %q", m, a, hd.Values("Set-Cookie"), hd.Get("Server"))
			}
			for k := range hd {
				if strings.HasPrefix(strings.ToLower(k), "quader-") {
					t.Errorf("%s %s: header %s", m, a, k)
				}
			}
			csp := hd.Get("Content-Security-Policy")
			if strings.HasPrefix(hd.Get("Content-Type"), "text/html") && rec.Code != http.StatusFound {
				n := nonce.FindStringSubmatch(csp)
				if n == nil || !strings.HasPrefix(csp, "default-src 'none'; script-src 'self';") || !strings.Contains(csp, "form-action 'none'") ||
					!strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "require-trusted-types-for 'script'") {
					t.Errorf("%s %s: page policy %q", m, a, csp)
				} else if m == http.MethodGet && !strings.Contains(rec.Body.String(), n[1]) {
					t.Errorf("%s %s: the nonce is not in the page", m, a)
				}
			} else if csp != "default-src 'none'; frame-ancestors 'none'" {
				t.Errorf("%s %s: policy %q", m, a, csp)
			}
		}
	}
}

// TestR_BAS_117_TheStateDecidesOffNotReadyAndServing: off answers every address 404 not_found; not ready (also undecided) answers
// the data 503 not_ready with Retry-After 30 and a page with that status, while the legal pages and the bundle's files are
// served; serving, `/` and `/ui` redirect to `/ui/` and `/ui/` to the locale (W-35's B.2b, B.6a).
func TestR_BAS_117_TheStateDecidesOffNotReadyAndServing(t *testing.T) {
	r := newReader()
	h := newGUI(t, r, nil)

	r.state = catalogue.State{Enabled: false, Decided: true}
	for _, a := range addresses {
		if rec := do(h, http.MethodGet, a); rec.Code != 404 || problemCode(t, rec) != "not_found" ||
			!strings.Contains(rec.Body.String(), textNotFound) || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("off: GET %s = %d %s", a, rec.Code, rec.Body.String())
		}
	}

	for name, st := range map[string]catalogue.State{
		"undecided":          {},
		"on without a state": {Enabled: true, Decided: true},
	} {
		r.state = st
		for _, a := range []string{"/ui/data/v1/state", "/ui/data/v1/packages", "/ui/data/v1/signing/chain.pem"} {
			rec := do(h, http.MethodGet, a)
			if rec.Code != 503 || problemCode(t, rec) != "not_ready" || rec.Header().Get("Retry-After") != "30" ||
				!strings.Contains(rec.Body.String(), textNotReady) {
				t.Errorf("%s: GET %s = %d %v %s", name, a, rec.Code, rec.Header(), rec.Body.String())
			}
		}
		if rec := do(h, http.MethodGet, "/ui/en/projects/acme/packages"); rec.Code != 503 || rec.Header().Get("Retry-After") != "30" ||
			!strings.Contains(rec.Body.String(), "quader-bastion-root") {
			t.Errorf("%s: a page = %d %s", name, rec.Code, rec.Body.String())
		}
		for _, a := range []string{"/ui/data/v1/legal?locale=de", "/ui/en/main-ABCD1234.js"} {
			if rec := do(h, http.MethodGet, a); rec.Code != 200 {
				t.Errorf("%s: GET %s = %d %s", name, a, rec.Code, rec.Body.String())
			}
		}
		if rec := do(h, http.MethodGet, "/"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/ui/" {
			t.Errorf("%s: GET / = %d %q", name, rec.Code, rec.Header().Get("Location"))
		}
	}

	r.state = onState()
	for _, a := range []string{"/", "/ui"} {
		if rec := do(h, http.MethodGet, a); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/ui/" ||
			rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("GET %s = %d %v", a, rec.Code, rec.Header())
		}
	}
	if rec := do(h, http.MethodGet, "/ui/projects/acme?x=1", "Accept-Language", "de-DE,de;q=0.9"); rec.Code != http.StatusFound ||
		rec.Header().Get("Location") != "/ui/de/projects/acme?x=1" || rec.Header().Get("Vary") != "Accept-Language" {
		t.Errorf("the locale's redirect = %d %v", rec.Code, rec.Header())
	}
	if rec := do(h, http.MethodGet, "/ui/en/projects/web/packages/ansible/acme.web"); rec.Code != 200 ||
		!strings.Contains(rec.Body.String(), `lang="en"`) {
		t.Errorf("a page = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/ui/en/chunk-XXXXXXXX.js"); rec.Code != 404 || problemCode(t, rec) != "not_found" {
		t.Errorf("a file the bundle lacks = %d %s", rec.Code, rec.Body.String())
	}
}

// fakeGuard refuses what its fields say and counts the classes it was asked.
type fakeGuard struct {
	blocked, limited bool
	pages            int
}

func (g *fakeGuard) Blocked(context.Context, string) guard.Verdict {
	if g.blocked {
		return guard.Verdict{Status: 429, Code: guard.CodeAddressBlocked, RetryAfter: 90 * time.Second, Quiet: true}
	}
	return guard.Allow()
}

func (g *fakeGuard) Page(context.Context, string) guard.Verdict {
	g.pages++
	if g.limited {
		return guard.Verdict{Status: 429, Code: guard.CodeRateLimited, RetryAfter: 1500 * time.Millisecond}
	}
	return guard.Allow()
}

// TestR_BAS_117_EveryRequestCountsInTheClassGui: the block list, then the class gui, for a page, a file and the data alike; a
// refusal is 429 with Retry-After and the class's RateLimit fields, and a blocked address is marked quiet (Q-LIM-307, R-BAS-037).
func TestR_BAS_117_EveryRequestCountsInTheClassGui(t *testing.T) {
	g := &fakeGuard{}
	quiet := 0
	r := newReader()
	h := newGUI(t, r, func(o *Options) {
		o.Guard, o.Quiet = g, func(context.Context) { quiet++ }
		o.Flood = func(context.Context) settings.Rate { return settings.Rate{RequestsPerSecond: 20, Burst: 100} }
	})
	for _, a := range []string{"/ui/en/", "/ui/en/main-ABCD1234.js", "/ui/data/v1/packages"} {
		if rec := do(h, http.MethodGet, a); rec.Code != 200 {
			t.Errorf("GET %s = %d", a, rec.Code)
		}
	}
	if g.pages != 3 {
		t.Errorf("the class gui was asked %d times, want 3", g.pages)
	}
	g.limited = true
	reads := r.reads
	rec := do(h, http.MethodGet, "/ui/data/v1/packages")
	if rec.Code != 429 || problemCode(t, rec) != "rate_limited" || rec.Header().Get("Retry-After") != "2" ||
		rec.Header().Get("RateLimit-Policy") != `"pages";q=100;w=5` || rec.Header().Get("RateLimit") != `"pages";r=0;t=2` {
		t.Errorf("limited = %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
	if r.reads != reads {
		t.Error("a refused request read the catalogue")
	}
	g.blocked = true
	if rec := do(h, http.MethodGet, "/ui/data/v1/state"); rec.Code != 429 || problemCode(t, rec) != "address_blocked" || quiet != 1 {
		t.Errorf("blocked = %d %s, quiet %d", rec.Code, rec.Body.String(), quiet)
	}
}

// TestR_BAS_118_TheDataAreTheMainInstallationsShapesFromTheCatalogue: the state, the legal pages, the search with its cursor, the
// lookup by name and by key, a page of versions, the projects that show a package or files, the repositories with files, a document by a path in any spelling a browser writes, ETag and 304, gzip, and the
// refusals: an index page, a path outside the grammar, an unknown parameter, a query the reader refuses.
func TestR_BAS_118_TheDataAreTheMainInstallationsShapesFromTheCatalogue(t *testing.T) {
	r := newReader()
	h := newGUI(t, r, nil)
	get := func(target string, hdr ...string) *httptest.ResponseRecorder {
		return do(h, http.MethodGet, target, hdr...)
	}

	rec := get("/ui/data/v1/state")
	if rec.Code != 200 || rec.Body.String() != `{"confirmedAt":"2026-11-26T09:30:00Z","staleAfterSeconds":3600,"stale":false,"docs":true,"timeFormat":"dmy-weekday"}` ||
		rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("ETag") == "" {
		t.Errorf("state = %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
	if again := get("/ui/data/v1/state", "If-None-Match", rec.Header().Get("ETag")); again.Code != 304 || again.Body.Len() != 0 {
		t.Errorf("state again = %d", again.Code)
	}

	// The project upstream holds no package and no folder the catalogue shows and is not offered (Q-BAS-399); the project tools
	// holds a generic repository's files and no package, and is (Q-BAS-400).
	if rec := get("/ui/data/v1/projects"); rec.Code != 200 || rec.Body.String() != `{"items":[{"key":"acme","displayName":"Acme platform","description":"The packages Acme publishes."},{"key":"tools","displayName":"tools","description":""},{"key":"web","displayName":"Web","description":""}]}` {
		t.Errorf("projects = %d %s", rec.Code, rec.Body.String())
	}
	if rec := get("/ui/data/v1/files"); rec.Code != 200 || rec.Body.String() != `{"items":["tools/downloads"]}` {
		t.Errorf("files = %d %s", rec.Code, rec.Body.String())
	}
	// A folder and a file by their paths of the grammar, as the files pages read them (R-UI-229).
	if rec := get("/ui/data/v1/files/tools/downloads/folder/tools"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"agent.tar.gz"`) {
		t.Errorf("folder = %d %s", rec.Code, rec.Body.String())
	}
	if rec := get("/ui/data/v1/files/tools/downloads/file/tools/agent.tar.gz"); rec.Code != 200 {
		t.Errorf("file = %d %s", rec.Code, rec.Body.String())
	}

	rec = get("/ui/data/v1/legal?locale=en")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"imprint":{"html":"\u003ch1`) || !strings.Contains(rec.Body.String(), `"lang":"de"`) ||
		!strings.Contains(rec.Body.String(), `"privacy":null`) {
		t.Errorf("legal = %d %s", rec.Code, rec.Body.String())
	}
	if rec := get("/ui/data/v1/legal?locale=fr"); rec.Code != 400 {
		t.Errorf("legal fr = %d", rec.Code)
	}

	rec = get("/ui/data/v1/packages?limit=1&include=vulnerabilities")
	if rec.Code != 200 || rec.Body.String() != `{"items":[{"id":"`+pkgID+`","name":"@acme/ui"}],"nextCursor":"c1"}` {
		t.Errorf("search = %d %s", rec.Code, rec.Body.String())
	}
	if rec := get("/ui/data/v1/packages?q=web"); rec.Code != 200 || rec.Body.String() != `{"items":[{"id":"r1","name":"acme.web"}],"nextCursor":null}` {
		t.Errorf("search q = %d %s", rec.Code, rec.Body.String())
	}
	for _, bad := range []string{"?namePrefix=a", "?limit=101", "?limit=x", "?sort=bad"} {
		if rec := get("/ui/data/v1/packages" + bad); rec.Code != 400 || problemCode(t, rec) != "malformed_request" {
			t.Errorf("search %s = %d %s", bad, rec.Code, rec.Body.String())
		}
	}

	for _, q := range []string{"format=npm&name=%40acme%2Fui", "format=ansible&name=web", "format=ansible&name=acme.web"} {
		if rec := get("/ui/data/v1/packages/lookup?" + q); rec.Code != 200 || !strings.HasPrefix(rec.Body.String(), `{"id":`) {
			t.Errorf("lookup %s = %d %s", q, rec.Code, rec.Body.String())
		}
	}
	if rec := get("/ui/data/v1/packages/lookup?format=npm&name=web"); rec.Code != 404 || problemCode(t, rec) != "not_found" {
		t.Errorf("lookup of another format = %d", rec.Code)
	}

	rec = get("/ui/data/v1/packages/" + pkgID + "/versions?limit=1")
	if rec.Code != 200 || rec.Body.String() != `{"items":[{"version":"1.1.0"}],"nextCursor":"1.1.0"}` {
		t.Errorf("versions = %d %s", rec.Code, rec.Body.String())
	}
	if rec := get("/ui/data/v1/packages/" + pkgID + "/versions?cursor=1.1.0&include=digest"); rec.Body.String() != `{"items":[{"version":"1.0.0"}],"nextCursor":null}` {
		t.Errorf("versions after = %d %s", rec.Code, rec.Body.String())
	}
	if rec := get("/ui/data/v1/packages/nope/versions"); rec.Code != 404 {
		t.Errorf("versions of a package not shown = %d", rec.Code)
	}

	// A browser writes the version's "+" as itself or as %2b; the grammar's one spelling is %2B.
	for _, spelling := range []string{"1.0.0+build.1", "1.0.0%2bbuild.1", "1.0.0%2Bbuild.1"} {
		rec := get("/ui/data/v1/packages/" + pkgID + "/versions/" + spelling)
		if rec.Code != 200 || rec.Body.String() != `{"version":"1.0.0+build.1"}` || rec.Header().Get("Content-Type") != "application/json" {
			t.Errorf("document %s = %d %s", spelling, rec.Code, rec.Body.String())
		}
	}
	doc := "/ui/data/v1/packages/" + pkgID + "/versions/1.0.0/vulnerabilities"
	rec = get(doc, "Accept-Encoding", "br, gzip")
	if rec.Code != 200 || rec.Header().Get("Content-Encoding") != "gzip" || !strings.HasSuffix(rec.Header().Get("ETag"), `-gzip"`) ||
		!strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("gzip = %d %v", rec.Code, rec.Header())
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); string(b) != `{"repositories":[]}` {
		t.Errorf("gzip body %q", b)
	}
	plain := get(doc, "Accept-Encoding", "gzip;q=0")
	if plain.Header().Get("Content-Encoding") != "" || plain.Body.String() != `{"repositories":[]}` {
		t.Errorf("gzip;q=0 = %v %s", plain.Header(), plain.Body.String())
	}
	if again := get(doc, "If-None-Match", plain.Header().Get("ETag")); again.Code != 304 {
		t.Errorf("document again = %d", again.Code)
	}
	if head := do(h, http.MethodHead, doc); head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") != "19" {
		t.Errorf("HEAD = %d %v", head.Code, head.Header())
	}
	if rec := get("/ui/data/v1/signing/chain.pem"); rec.Header().Get("Content-Type") != "application/pem-certificate-chain" ||
		rec.Header().Get("Content-Disposition") != "attachment" {
		t.Errorf("chain = %v", rec.Header())
	}
	for _, a := range []string{
		"/ui/data/v1/index/0", "/ui/data/v1/packages/" + pkgID + "/versions/..", "/ui/data/v1/packages/a%2Fb", "/ui/data/v1/state/x",
		"/ui/data/v1/", "/ui/data/v1/nothing", "/ui/data/v1/packages/" + pkgID + "/versions/9.9.9", "/ui/data/x",
	} {
		if rec := get(a); rec.Code != 404 || problemCode(t, rec) != "not_found" {
			t.Errorf("GET %s = %d %s", a, rec.Code, rec.Body.String())
		}
	}
}

// TestR_BAS_116_TheBundleIsTheBastionsOwnAndBuildsWithoutTheWebBuild: the embedded tree holds the committed placeholder, which
// starts the handler without a web build, and names nothing of the main installation.
// TestR_BAS_125_TheDocumentationIsSearchedInTheMainInstallationsShape: /ui/data/v1/search/documentation answers the reader's hits in
// the main installation's shape of a hit (DocumentationHit) in a list of R-API-010, takes q, project, cursor and limit and refuses
// any other parameter, a text the reader refuses and a limit outside 1 to 100; with docs off it finds nothing.
func TestR_BAS_125_TheDocumentationIsSearchedInTheMainInstallationsShape(t *testing.T) {
	r := newReader()
	h := newGUI(t, r, nil)
	rec := do(h, http.MethodGet, "/ui/data/v1/search/documentation?q=web&project=web&limit=10")
	want := `{"items":[{"format":"ansible","packageId":"r1","package":"acme.web","version":"1.0.0","project":"web","repository":"galaxy",` +
		`"type":"module","name":"web_server","shortDescription":"Serves the web"}],"nextCursor":null}`
	if rec.Code != 200 || rec.Body.String() != want || rec.Header().Get("ETag") == "" {
		t.Errorf("search = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/ui/data/v1/search/documentation?q=web&project=acme"); rec.Code != 200 || rec.Body.String() != `{"items":[],"nextCursor":null}` {
		t.Errorf("another project = %d %s", rec.Code, rec.Body.String())
	}
	for _, q := range []string{"q=w", "q=web&kind=x", "q=web&limit=0", "q=web&limit=101"} {
		if rec := do(h, http.MethodGet, "/ui/data/v1/search/documentation?"+q); rec.Code != 400 || problemCode(t, rec) != "malformed_request" {
			t.Errorf("%s = %d %s", q, rec.Code, rec.Body.String())
		}
	}
	r.state.Docs = false
	if rec := do(h, http.MethodGet, "/ui/data/v1/search/documentation?q=web"); rec.Code != 200 || rec.Body.String() != `{"items":[],"nextCursor":null}` {
		t.Errorf("docs off = %d %s", rec.Code, rec.Body.String())
	}
}

// TestR_BAS_126_DocsOffTakesTheChangelogOutOfTheContentsAtOnce: a Debian package's contents carry its changelog and copyright while
// docs is on, as stored; with docs off they are answered without them at once, before the builder's next run.
func TestR_BAS_126_DocsOffTakesTheChangelogOutOfTheContentsAtOnce(t *testing.T) {
	r := newReader()
	contents := "packages/" + pkgID + "/versions/1.0.0/contents"
	r.docs[contents] = []byte(`{"changelog":"acme (1.0) stable","changelogTruncated":false,"copyright":"MIT","files":[{"path":"/usr/bin/acme"}]}`)
	h := newGUI(t, r, nil)
	if rec := do(h, http.MethodGet, dataRoot+contents); rec.Code != 200 || !strings.Contains(rec.Body.String(), "acme (1.0) stable") {
		t.Errorf("docs on = %d %s", rec.Code, rec.Body.String())
	}
	r.state.Docs = false
	rec := do(h, http.MethodGet, dataRoot+contents)
	if rec.Code != 200 || rec.Body.String() != `{"files":[{"path":"/usr/bin/acme"}]}` || rec.Header().Get("ETag") == "" {
		t.Errorf("docs off = %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
}

func TestR_BAS_116_TheBundleIsTheBastionsOwnAndBuildsWithoutTheWebBuild(t *testing.T) {
	h, err := New(Options{Catalogue: newReader(), Legal: legalPages(t), BaseURL: "https://bastion.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	rec := do(h, http.MethodGet, "/ui/en/")
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "QUADER_CSP_NONCE") {
		t.Fatalf("the embedded bundle = %d %s", rec.Code, rec.Body.String())
	}
	if h.Placeholder() && strings.Contains(strings.ToLower(rec.Body.String()), "main installation") {
		t.Error("the placeholder names the main installation")
	}
	if _, err := New(Options{Legal: legalPages(t)}); err == nil {
		t.Error("New without a catalogue")
	}
}

// TestR_BAS_116_TheEmbeddedBundleCarriesItsNotices: a binary built with the web build serves the Bastion's application in both
// locales with the notices of its bundled software (R-UI-001, Q-UI-144), and no script of it holds the session or the writes. The
// web job of CI builds the UI and sets QUADER_UI_BUILT=1, where a placeholder is a failure; elsewhere the test is skipped.
func TestR_BAS_116_TheEmbeddedBundleCarriesItsNotices(t *testing.T) {
	h, err := New(Options{Catalogue: newReader(), Legal: legalPages(t), BaseURL: "https://bastion.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if h.Placeholder() {
		if os.Getenv("QUADER_UI_BUILT") != "" {
			t.Fatal("QUADER_UI_BUILT is set, but internal/bastion/gui holds no web build (run make web-embed)")
		}
		t.Skip("no web build is embedded (make web-embed); the web job of the release workflow requires it with QUADER_UI_BUILT=1")
	}
	for _, l := range []string{"en", "de"} {
		rec := do(h, http.MethodGet, "/ui/"+l+"/3rdpartylicenses.txt")
		body := rec.Body.String()
		if rec.Code != 200 || !strings.Contains(body, "Package: material-icons") || !strings.Contains(body, "OFL-1.1") {
			t.Errorf("%s notices = %d (%d bytes)", l, rec.Code, len(body))
		}
		page := do(h, http.MethodGet, "/ui/"+l+"/")
		if page.Code != 200 || !strings.Contains(page.Body.String(), "<quader-root") {
			t.Errorf("%s page = %d", l, page.Code)
		}
	}
	err = fs.WalkDir(Bundle(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".js") {
			return err
		}
		b, err := fs.ReadFile(Bundle(), p)
		// The marks of the main installation's session and acts: its CSRF header, which every write carries, and the read of who is
		// signed in. The build's own check (web/scripts/bastion-bundle-rule.mjs) holds the modules; this holds the bytes shipped.
		for _, mark := range []string{"X-CSRF-Token", "/api/v1/auth/whoami"} {
			if err == nil && strings.Contains(string(b), mark) {
				t.Errorf("%s holds %q", p, mark)
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestR_BAS_128_TheDataNameTheProjectsGradeTheVersionsAndWriteTheInstallationsTime: the state names the form of a time of the
// configuration the replica applies (the factory form without one), the repositories carry their format and class for the page Set up
// client and nothing of the wiki, and a package's open findings per version are the catalogue's document.
func TestR_BAS_128_TheDataNameTheProjectsGradeTheVersionsAndWriteTheInstallationsTime(t *testing.T) {
	r := newReader()
	form := "iso"
	h := newGUI(t, r, func(o *Options) { o.TimeFormat = func() string { return form } })
	if rec := do(h, http.MethodGet, "/ui/data/v1/state"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"timeFormat":"iso"`) {
		t.Errorf("state = %d %s", rec.Code, rec.Body.String())
	}
	form = ""
	if rec := do(h, http.MethodGet, "/ui/data/v1/state"); !strings.Contains(rec.Body.String(), `"timeFormat":"dmy-weekday"`) {
		t.Errorf("state without a form = %s", rec.Body.String())
	}
	want := `{"items":[{"name":"acme/npm","format":"npm","class":"local"},{"name":"upstream/npmjs","format":"npm","class":"remote"},` +
		`{"name":"web/galaxy","format":"ansible","class":"local"}]}`
	if rec := do(h, http.MethodGet, "/ui/data/v1/repositories"); rec.Code != 200 || rec.Body.String() != want {
		t.Errorf("repositories = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/ui/data/v1/packages/"+pkgID+"/vulnerabilities"); rec.Code != 200 ||
		!strings.Contains(rec.Body.String(), `"grade":{"grade":"B"}`) {
		t.Errorf("the package's findings = %d %s", rec.Code, rec.Body.String())
	}
	r.state = catalogue.State{Enabled: false, Decided: true}
	if rec := do(h, http.MethodGet, "/ui/data/v1/repositories"); rec.Code != 404 {
		t.Errorf("repositories while off = %d", rec.Code)
	}
}

// TestR_MAL_042_TheBastionAnswersTodaysSignaturesFromTheCatalogue: the data address of today's signatures answers the main
// installation's shape from the catalogue's state document, with no time of the import, no engine and no set called stale, and 404
// while the state holds no set (the main installation's scanning off) or the catalogue no state.
func TestR_MAL_042_TheBastionAnswersTodaysSignaturesFromTheCatalogue(t *testing.T) {
	r := newReader()
	h := newGUI(t, r, nil)
	if rec := do(h, http.MethodGet, "/ui/data/v1/malware/signatures"); rec.Code != 404 {
		t.Errorf("no state = %d %s", rec.Code, rec.Body.String())
	}
	r.docs["state"] = []byte(`{"builtAt":"2026-11-26T09:00:00Z","malware":{"version":"main-63.daily-28201.bytecode-339","daily":28201,` +
		`"builtAt":"2026-11-26T07:58:00Z","scanner":"malware-scanner-clamav","cycleDays":30,"overdueAfterDays":7}}`)
	want := `{"version":"main-63.daily-28201.bytecode-339","daily":28201,"builtAt":"2026-11-26T07:58:00Z","importedAt":null,"stale":false,` +
		`"scanner":"malware-scanner-clamav","engine":null,"cycleDays":30,"overdueAfterDays":7}`
	if rec := do(h, http.MethodGet, "/ui/data/v1/malware/signatures"); rec.Code != 200 || rec.Body.String() != want || rec.Header().Get("ETag") == "" {
		t.Errorf("signatures = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/ui/data/v1/malware/signatures?x=1"); rec.Code != 400 {
		t.Errorf("a parameter = %d", rec.Code)
	}
	// The catalogue's state document itself stays the reader's (R-BAS-112).
	if rec := do(h, http.MethodGet, "/ui/data/v1/state"); strings.Contains(rec.Body.String(), "malware") {
		t.Errorf("the Bastion's state names the catalogue's: %s", rec.Body.String())
	}
	// Today's vulnerability database beside it (R-VULN-052): the main installation's shape, no time of its confirmation, nothing stale.
	if rec := do(h, http.MethodGet, "/ui/data/v1/vulnerabilities/database"); rec.Code != 404 {
		t.Errorf("no database = %d %s", rec.Code, rec.Body.String())
	}
	r.docs["state"] = []byte(`{"builtAt":"2026-11-26T09:00:00Z","malware":null,` +
		`"vulnerabilities":{"scanner":"vuln-scanner-trivy","digest":"sha256:5a0c","builtAt":"2026-11-26T06:12:40Z"}}`)
	if rec := do(h, http.MethodGet, "/ui/data/v1/vulnerabilities/database"); rec.Code != 200 ||
		rec.Body.String() != `{"scanner":"vuln-scanner-trivy","digest":"sha256:5a0c","builtAt":"2026-11-26T06:12:40Z","syncedAt":null,"stale":false}` {
		t.Errorf("database = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/ui/data/v1/malware/signatures"); rec.Code != 404 {
		t.Errorf("the scanning off = %d %s", rec.Code, rec.Body.String())
	}
}
