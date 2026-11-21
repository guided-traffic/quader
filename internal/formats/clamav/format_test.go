package clamav

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/formats/clamav/rows"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// TestR_CLAMAV_001_TheDescriptor: the format is clamav, local only, system only, with the layout clamav, the three databases as its
// package names compared as written, the client freshclam, no settings, no presets and no ownership (R-CLAMAV-001). Milliseconds.
func TestR_CLAMAV_001_TheDescriptor(t *testing.T) {
	d := Format{}.Descriptor()
	if d.ID != "clamav" || d.DisplayName != "ClamAV definitions" || len(d.Classes) != 1 || d.Classes[0] != formats.Local || !d.SystemOnly ||
		d.Layout != "clamav" || d.ProtocolBasePath != "/api/clamav/{project}/{key}/" || len(d.UpstreamPresets) != 0 || d.PatternSubject != formats.SubjectPath ||
		d.PriorityMembers || d.PinsAndClaims || d.Caps || d.Wiki || d.OwnershipDefault != "" || d.SettingsSchema != nil ||
		len(d.Clients) != 1 || d.Clients[0].ID != "freshclam" || d.VersionFetch != formats.FetchLeadFile {
		t.Fatalf("descriptor %+v", d)
	}
	for name, want := range map[string]bool{"main": true, "daily": true, "bytecode": true, "Daily": false, "safebrowsing": false, "": false} {
		if _, ok := d.Names.Normalize(name); ok != want {
			t.Errorf("Normalize(%q) = %v", name, ok)
		}
	}
	if err := formats.Validate(Format{}); err != nil {
		t.Fatal(err)
	}
}

// uploadingFormat is a system-only test format that also has an upload route.
type uploadingFormat struct{ Format }

func (uploadingFormat) Routes() []formats.Route {
	return append(Format{}.Routes(), formats.Route{
		Name: "clamav.put", Method: http.MethodPut, Template: "/{db}.cvd", Examples: []string{"daily.cvd"},
		Action: formats.ActionDeploy, Kind: formats.Upload, Handler: func(context.Context, *formats.Request, http.ResponseWriter) error { return nil },
	})
}

func (f uploadingFormat) Match(m, p string) (*formats.Route, formats.Params, error) {
	return formats.MatchRoutes(f.Routes(), m, p)
}

// TestR_CLAMAV_001_RegisterRefusesASystemOnlyFormatThatClientsCouldWrite: a system-only format with the class remote or virtual is
// refused by the catalog, one with a route of the kind Upload by the contract check (Q-CLAMAV-4, ADR-225). Milliseconds.
func TestR_CLAMAV_001_RegisterRefusesASystemOnlyFormatThatClientsCouldWrite(t *testing.T) {
	for _, class := range []catalog.Class{catalog.Remote, catalog.Virtual} {
		e := formats.CatalogEntry(Format{})
		e.ID = "sysonly" + catalog.ID(class)
		e.Classes = []catalog.Class{catalog.Local, class}
		func() {
			defer func() {
				if p := recover(); p == nil || !strings.Contains(p.(string), "system only") {
					t.Errorf("%s: the catalog took it (%v)", class, p)
				}
			}()
			catalog.NewRegistry().Register(e)
		}()
	}
	if err := formats.Validate(uploadingFormat{}); err == nil || !strings.Contains(err.Error(), "system only") {
		t.Fatalf("an upload route of a system-only format: %v", err)
	}
}

// TestR_CLAMAV_004_TheRoutesFreshclamAsks: <db>.cvd and <db>-<version>.cvd.sign of the three databases match, as GET and HEAD of a GET
// route; a .cld, another database, a patch file, a path below a version and a write match no route (R-CLAMAV-004). Milliseconds.
func TestR_CLAMAV_004_TheRoutesFreshclamAsks(t *testing.T) {
	f := Format{}
	for path, want := range map[string]string{
		"main.cvd": RouteContainer, "daily.cvd": RouteContainer, "bytecode.cvd": RouteContainer,
		"daily-28190.cvd.sign": RouteSignature, "main-63.cvd.sign": RouteSignature,
	} {
		r, p, err := f.Match(http.MethodGet, path)
		if err != nil || r.Name != want || p.Get(paramDB) == "" {
			t.Errorf("GET %s: %v %v", path, r, err)
		}
		if r.Kind != formats.Metadata || r.Action != formats.ActionRead {
			t.Errorf("%s: kind %v action %v", path, r.Kind, r.Action)
		}
	}
	for _, path := range []string{"daily.cld", "safebrowsing.cvd", "daily-28190.cdiff", "daily/28190/daily.cvd", "daily-028190.cvd.sign", "test.cvd"} {
		if _, _, err := f.Match(http.MethodGet, path); errs.KindOf(err) != errs.NotFound {
			t.Errorf("GET %s matched: %v", path, err)
		}
	}
	if _, _, err := f.Match(http.MethodPut, "daily.cvd"); errs.KindOf(err) != errs.MethodNotAllowed {
		t.Errorf("PUT daily.cvd: %v", err)
	}
}

// TestR_CLAMAV_004_TheEndpointServesTheVersionInUse: daily.cvd serves the version the set in use names, not the newest stored one; a
// database without a version in use answers 404, a detached signature is served while it is stored (R-CLAMAV-004, Q-MAL-99). Range
// and the conditionals are the item store's, held at the integration tier.
// Milliseconds, containers of hundreds of bytes.
func TestR_CLAMAV_004_TheEndpointServesTheVersionInUse(t *testing.T) {
	ctx := context.Background()
	mem := newMem()
	h := formattest.NewHost(Format{}, formattest.HostOptions{Data: mem})
	for _, v := range []int{28189, 28190} {
		if err := commitVersion(ctx, h, "daily", v, true); err != nil {
			t.Fatal(err)
		}
	}
	mem.markInUse("daily", 28189)
	res, err := h.Serve(ctx, http.MethodGet, "daily.cvd", nil, false)
	if err != nil || res.Code != http.StatusOK || !bytes.Equal(res.Body.Bytes(), container("daily", 28189)) {
		t.Fatalf("daily.cvd: %v %d", err, res.Code)
	}
	if paths := h.ServedPaths(); len(paths) != 1 || paths[0] != "daily/28189/daily.cvd" {
		t.Fatalf("served %v", paths)
	}
	for path, want := range map[string]int{"main.cvd": 404, "daily-28190.cvd.sign": 200, "daily-28191.cvd.sign": 404} {
		if res, err := h.Serve(ctx, http.MethodGet, path, nil, false); err != nil || res.Code != want {
			t.Errorf("%s: %v %d", path, err, res.Code)
		}
	}
}

// TestR_CLAMAV_005_NoAnswerIsA403OrA429: a refusal of the decision point answers 404, a refusal of a limit 503 with its Retry-After,
// a 401 carries the Basic challenge, and every other error keeps its status; the body names the code (R-CLAMAV-005, Q-CLAMAV-3).
// Milliseconds.
func TestR_CLAMAV_005_NoAnswerIsA403OrA429(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		header string
		value  string
	}{
		{"no credential", errs.E(errs.Unauthenticated, errs.CodeUnauthenticated, "Sign in.", errs.WithChallenge(`Bearer realm="quader"`)), 401, "WWW-Authenticate", `Basic realm="quader"`},
		{"a refusal of the decision point", errs.E(errs.Forbidden, errs.CodeForbidden, "No."), 404, "", ""},
		{"a request figure", errs.E(errs.RateLimited, errs.CodeRateLimited, "Later.", errs.WithRetryAfter(90*time.Second)), 503, "Retry-After", "90"},
		{"not found", errs.E(errs.NotFound, errs.CodeNotFound, "No."), 404, "", ""},
		{"a method", errs.E(errs.MethodNotAllowed, errs.CodeMethodNotAllowed, "GET.", errs.WithAllow("GET", "HEAD")), 405, "Allow", "GET, HEAD"},
		{"misdirected", errs.E(errs.Misdirected, errs.CodeMisdirectedRequest, "Host."), 421, "", ""},
		{"unavailable", errs.E(errs.Unavailable, errs.CodeUnavailable, "Busy.", errs.WithRetryAfter(time.Second)), 503, "Retry-After", "1"},
	} {
		w := httptest.NewRecorder()
		Format{}.WriteError(w, httptest.NewRequest(http.MethodGet, "/api/clamav/upstream/clamav/daily.cvd", nil), tc.err)
		if w.Code != tc.status || (tc.header != "" && w.Header().Get(tc.header) != tc.value) {
			t.Errorf("%s: %d %s=%q", tc.name, w.Code, tc.header, w.Header().Get(tc.header))
		}
		e, _ := errs.As(tc.err)
		if !strings.Contains(w.Body.String(), `"code":"`+e.Code+`"`) {
			t.Errorf("%s: body %s", tc.name, w.Body.String())
		}
	}
}

// TestR_CLAMAV_006_TheSnippetIsOneLineWithTheAddress: the configure snippet is PrivateMirror with the address written out and without
// its trailing slash; where the repository is not anonymously readable the token stands as the password of the address
// (R-CLAMAV-006, Q-REPO-415). Milliseconds.
func TestR_CLAMAV_006_TheSnippetIsOneLineWithTheAddress(t *testing.T) {
	funcs := template.FuncMap{
		"trimslash":  func(s string) string { return strings.TrimSuffix(s, "/") },
		"schemeless": func(s string) string { return strings.TrimPrefix(s, "https:") },
	}
	base := "https://quader.example.com/api/clamav/upstream/clamav/"
	for requires, want := range map[bool]string{
		false: "PrivateMirror https://quader.example.com/api/clamav/upstream/clamav\n",
		true:  "PrivateMirror https://clamav:${QUADER_TOKEN}@quader.example.com/api/clamav/upstream/clamav\n",
	} {
		tm := template.Must(template.New("c").Funcs(funcs).Parse(Format{}.SetupTemplates()[0].Template))
		var b bytes.Buffer
		if err := tm.Execute(&b, formats.SetupData{RegistryURL: base, RequiresAuth: requires}); err != nil || b.String() != want {
			t.Errorf("requires auth %v: %q, %v", requires, b.String(), err)
		}
	}
}

// TestR_CLAMAV_007_TheViewOrdersAsIntegersAndListsTheHistoryLast: Compare is a total order over versions as integers; Stored lists the
// stored versions newest first and a version whose files the cleanup removed after them, and Describe shows its state (R-CLAMAV-007,
// R-MAL-034). Milliseconds.
func TestR_CLAMAV_007_TheViewOrdersAsIntegersAndListsTheHistoryLast(t *testing.T) {
	v := viewAdapter{}
	if v.Compare("9", "28189") >= 0 || v.Compare("28190", "28189") <= 0 || v.Compare("x", "9") >= 0 || v.Compare("28190", "28190") != 0 {
		t.Fatal("Compare is not the integer order")
	}
	ctx := context.Background()
	mem := newMem()
	h := formattest.NewHost(Format{}, formattest.HostOptions{Data: mem})
	if err := seedInUse("daily", true, 28188, 28189, 28190)(h); err != nil {
		t.Fatal(err)
	}
	rq, _ := h.NewRequest(ctx, http.MethodGet, "daily.cvd", nil)
	if err := h.Remove(ctx, rq, "delete", ContainerPath("daily", 28189, "daily.cvd"), SignaturePath("daily", 28189)); err != nil {
		t.Fatal(err)
	}
	st, err := v.Stored(ctx, h, formats.StoredQuery{Repo: h.Ref(), Package: "daily", Limit: 10})
	if err != nil || strings.Join(st.Versions, ",") != "28190,28188,28189" {
		t.Fatalf("stored %v, %v", st.Versions, err)
	}
	var states []string
	vs, _, _ := h.Packages().Versions(ctx, h.Ref(), "daily")
	views := make([]formats.ViewVersion, 0, len(vs))
	for _, r := range vs {
		views = append(views, formats.ViewVersion{ID: r.ID, Repo: h.Ref(), Package: "daily", Version: r.Version})
	}
	got, err := v.Describe(ctx, h, views, formats.ViewDetails, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range vs {
		states = append(states, r.Version+"="+got[r.ID].Metadata["state"].(string))
	}
	if strings.Join(states, ",") != "28188=superseded,28189=files_removed,28190=in_use" {
		t.Fatalf("states %v", states)
	}
	md := got[vs[2].ID].Metadata
	if md["functionalityLevelText"] != "ClamAV 0.103 or later" || md["signatures"] != 2 || md["detachedSignature"].(map[string]any)["signatures"] != 2 {
		t.Fatalf("metadata %+v", md)
	}
	if st, _ := v.Stored(ctx, h, formats.StoredQuery{Repo: h.Ref(), Package: "main", Limit: 10}); len(st.Versions) != 0 {
		t.Fatalf("an empty database lists %v", st.Versions)
	}
}

// TestR_CLAMAV_002_ACommitWithoutItsMetadataIsRefused: OnCommit refuses a commit at a path that is no path of the layout and a container
// without its metadata or with metadata of another version (R-CLAMAV-002). Milliseconds.
func TestR_CLAMAV_002_ACommitWithoutItsMetadataIsRefused(t *testing.T) {
	ctx := context.Background()
	h := formattest.NewHost(Format{}, formattest.HostOptions{Data: newMem()})
	rq, _ := h.NewRequest(ctx, http.MethodGet, "daily.cvd", nil)
	b := container("daily", 5)
	for name, c := range map[string]formats.Commit{
		"a path outside the layout": {Path: formats.Path{Path: "daily.cvd"}, Payload: metadataOf("daily", 5, b)},
		"no metadata":               {Path: formats.Path{Path: ContainerPath("daily", 5, "daily.cvd")}},
		"another version":           {Path: formats.Path{Path: ContainerPath("daily", 5, "daily.cvd")}, Payload: metadataOf("daily", 6, b)},
		"a signature without facts": {Path: formats.Path{Path: SignaturePath("daily", 5)}, Payload: rows.Container{DB: "daily", Version: 5}},
	} {
		sp, _ := h.Ingest().Spool(ctx, rq, bytes.NewReader(b), formats.IngestOptions{})
		if _, err := h.Ingest().Commit(ctx, rq, sp, c); err == nil {
			t.Errorf("%s was committed", name)
		}
	}
}
