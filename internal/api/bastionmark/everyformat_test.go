package bastionmark

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/api/dispatch"
	"github.com/guided-traffic/quader/internal/api/edge"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// The main installation's marking of a Bastion serving every format (bastion.md R-BAS-051, R-BAS-052, R-BAS-059). The tests
// carry the decisions' IDs: the requirements are proven once the Bastion's side uses what is marked here.

// registryExpiry is the expiry of the registry tokens of the rig: five minutes, shorter than the token t-1 they were
// issued for.
var registryExpiry = time.Unix(1_800_000_300, 0).UTC()

// registryGrant is the grant of a registry token of the rig: pull on one image of web/mk-local.
func registryGrant() *httpserver.RegistryGrant {
	return &httpserver.RegistryGrant{Entries: []httpserver.RegistryEntry{{Repo: qr("mk-local"), Name: "left-pad", Pull: true}}}
}

// TestR_BAS_052_ARegistryTokensShapeIsAtMostPartial: the decision point computes a registry token's shape for the token it
// was issued for, and the marking caps it at partial (R-BAS-052): a document is the caller's private copy, an artifact is
// still an artifact but carries partial, so a Bastion asks the main installation per URL. The subject is the token behind
// the registry token with the registry token's own expiry. A Quader token presented directly is full as before, and an
// anonymous registry token is capped as well.
func TestR_BAS_052_ARegistryTokensShapeIsAtMostPartial(t *testing.T) {
	cases := []struct {
		name, cred, path string
		read             iface.Read
		class            iface.Class
		subject          string
	}{
		{"a registry token's document", "registry", "/api/mk/web/mk-local/d/left-pad", iface.ReadPartial, iface.ClassPrivate, "token=t-1; principal=p-1; expires=1800000300"},
		{"a registry token's download", "registry", "/api/mk/web/mk-local/f/x.bin", iface.ReadPartial, iface.ClassArtifact, "token=t-1; principal=p-1; expires=1800000300"},
		{"an anonymous registry token's document", "registry-anon", "/api/mk/web/mk-local/d/left-pad", iface.ReadPartial, iface.ClassPrivate, "anonymous"},
		{"a Quader token presented directly", "token", "/api/mk/web/mk-local/d/left-pad", iface.ReadFull, iface.ClassShared, "token=t-1; principal=p-1; expires=1900000000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			r.shapes.read = iface.ReadFull // what the decision point answers for the token behind it
			rec := r.do("GET", c.path, hRecognize, "forward", hCred, c.cred)
			mk := markingOf(t, rec)
			if mk.Read != c.read || mk.Class != c.class || rec.Header().Get(iface.HeaderSubject) != c.subject {
				t.Fatalf("read %s class %s subject %q", mk.Read, mk.Class, rec.Header().Get(iface.HeaderSubject))
			}
		})
	}
	t.Run("partial and none are not raised", func(t *testing.T) {
		for _, read := range []iface.Read{iface.ReadPartial, iface.ReadNone} {
			if got := bastionmain.CapRegistryShape(read, true); got != read {
				t.Errorf("%s capped to %s", read, got)
			}
		}
	})
}

// TestR_BAS_080_TheDecisionPointDoesNotSeeTheGrant establishes what the cap rests on: the subject the decision point and the
// shape resolver get (edge.SubjectOf, the dispatcher's conversion) is the same for a registry token and for the token it was
// issued for. authz.Subject has no member for the grant (a scan token's one repository is a system subject's and not a
// client's), so no shape the resolver computes can depend on it, and an uncapped full would hand a token granted one image
// every cached image of the repository. The rig shows the same at the marking: both callers reach the resolver as one subject.
func TestR_BAS_080_TheDecisionPointDoesNotSeeTheGrant(t *testing.T) {
	exp := registryExpiry
	direct := httpserver.Principal{
		Kind: httpserver.KindUser, ID: "p-1", Name: "alice", Credential: httpserver.Credential{Method: httpserver.MethodBearer, TokenID: "t-1", ExpiresAt: &exp},
	}
	granted := direct
	granted.Credential.Registry = registryGrant()
	a := edge.SubjectOf(httpserver.WithPrincipal(context.Background(), direct))
	b := edge.SubjectOf(httpserver.WithPrincipal(context.Background(), granted))
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("the grant reaches the decision point: %+v != %+v", a, b)
	}
	r := newRig(t)
	r.do("GET", "/api/mk/web/mk-local/d/left-pad", hRecognize, "forward", hCred, "token-no-expiry")
	r.do("GET", "/api/mk/web/mk-local/d/left-pad", hRecognize, "forward", hCred, "registry")
	r.do("GET", "/api/mk/web/mk-local/d/left-pad", hRecognize, "forward", hCred, "token")
	if len(r.shapes.subs) != 3 || !reflect.DeepEqual(r.shapes.subs[1], r.shapes.subs[2]) {
		t.Fatalf("the resolver was asked %+v", r.shapes.subs)
	}
}

// TestR_BAS_052_InsufficientScopeIsNoStatementAboutTheCredential: a 401 whose challenge says insufficient_scope - the
// enforcement point's answer to a registry token whose grant lacks the route's action - is marked with the subject the
// authentication stage accepted, so a Bastion neither counts it as a failed authentication nor keeps the token as invalid;
// every other 401 stays invalid (R-BAS-052, H-442).
func TestR_BAS_052_InsufficientScopeIsNoStatementAboutTheCredential(t *testing.T) {
	for name, c := range map[string]struct{ challenge, want string }{
		"insufficient_scope": {`Bearer realm="https://packages.example.com/v2/_token",service="packages.example.com",scope="repository:web/mk-local/x:pull",error="insufficient_scope"`, "token=t-1; principal=p-1; expires=1800000300"},
		"invalid_token":      {`Bearer realm="https://packages.example.com/v2/_token",service="packages.example.com",error="invalid_token"`, "invalid"},
		"a bare challenge":   {`Bearer realm="quader"`, "invalid"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.status = http.StatusUnauthorized
			r.respond = func(w http.ResponseWriter) { w.Header().Set("WWW-Authenticate", c.challenge) }
			rec := r.do("GET", "/api/mk/web/mk-local/d/left-pad", hRecognize, "forward", hCred, "registry")
			if rec.Code != http.StatusUnauthorized || rec.Header().Get(iface.HeaderSubject) != c.want || markingOf(t, rec).Class != iface.ClassPass {
				t.Fatalf("%d subject %q %v", rec.Code, rec.Header().Get(iface.HeaderSubject), rec.Header())
			}
		})
	}
}

// TestR_BAS_059_ADownloadTheCoreCountedIsMarkedCounted: a 200 whose serve the core's download recording noted carries
// Quader-Bastion-Counted: download, a document's included (the manifest of an OCI tag is one, Q-OCI-222); a 304, a 404, a
// response nothing noted and an ordinary caller's response carry none (R-BAS-059).
func TestR_BAS_059_ADownloadTheCoreCountedIsMarkedCounted(t *testing.T) {
	counted := func(ctx context.Context) { bastionmain.NoteCounted(ctx) }
	for _, c := range []struct {
		name, path string
		status     int
		notes      func(context.Context)
		want       bool
	}{
		{"a download counted", "/api/mk/web/mk-local/f/x.bin", 200, counted, true},
		{"a document counted", "/api/mk/web/mk-local/d/left-pad", 200, counted, true},
		{"a download nothing noted", "/api/mk/web/mk-local/f/x.bin", 200, nil, false},
		{"a 304", "/api/mk/web/mk-local/f/x.bin", 304, counted, false},
		{"a 404", "/api/mk/web/mk-local/f/x.bin", 404, counted, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			r.status, r.notes = c.status, c.notes
			rec := r.do("GET", c.path, hRecognize, "forward", hCred, "token")
			if got := markingOf(t, rec).Counted; got != c.want {
				t.Fatalf("counted %v, want %v: %v", got, c.want, rec.Header())
			}
		})
	}
	t.Run("an ordinary caller learns nothing", func(t *testing.T) {
		r := newRig(t)
		r.notes = counted
		if rec := r.do("GET", "/api/mk/web/mk-local/f/x.bin", hCred, "token"); rec.Header().Get(iface.HeaderCounted) != "" {
			t.Fatalf("%v", rec.Header())
		}
	})
}

// TestR_BAS_051_AWriteBelowTheRegistryRootNamesTheImagePrefix is the main installation's side of R-BAS-051's write
// refusal: the probe of a push through a Bastion reaches the dispatcher's handler of the registry root, which tells the
// marking its route, and is refused 403 DENIED in the distribution's shape before any decision, naming the image prefix
// a client pushes to on the main installation. No handler runs and no body is read.
func TestR_BAS_051_AWriteBelowTheRegistryRootNamesTheImagePrefix(t *testing.T) {
	r := newRig(t)
	d, err := dispatch.New(dispatch.Options{
		Host: fakeHost{}, Decider: r.decider, Formats: func() []formats.Format { return []formats.Format{oci.New()} },
		Repos: &fakeRepos{byKey: map[string]repository.Repository{
			"web/docker-local": {ID: "5", Name: qr("docker-local"), Class: repository.Local, Format: "oci"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	core := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("a core route ran for a repository route")
		w.WriteHeader(http.StatusTeapot)
	})
	h, err := d.RegistryHandler(dispatch.CoreRoutes{Version: core, Catalog: core, Token: core})
	if err != nil {
		t.Fatal(err)
	}
	router := httpserver.NewClientRouter()
	router.Handle("/v2/{rest...}", h)
	marker := New(Options{Shapes: r.shapes, MainURL: mainURL, RegistryRoot: "oci", Metrics: r.metrics})
	chain := httpserver.Chain{
		Router:       router,
		Authenticate: func(*http.Request) (httpserver.Principal, error) { return httpserver.AnonymousPrincipal(), nil },
		Bastion: func(next http.Handler) http.Handler {
			next = marker.Stage()(next)
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				rec := bastionmain.Recognition{Bastion: "edge-1", Interface: iface.InterfaceVersion, Probe: true}
				next.ServeHTTP(w, req.WithContext(bastionmain.WithRecognition(req.Context(), rec)))
			})
		},
	}.Handler()
	r.chain = chain
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/v2/web/docker-local/app/blobs/uploads/"},
		{http.MethodPut, "/v2/web/docker-local/app/manifests/1.0"},
		{http.MethodDelete, "/v2/web/docker-local/app/manifests/sha256:" + strings.Repeat("a", 64)},
	} {
		rec := r.doBody(c.method, c.path, failingReader{t})
		body := rec.Body.String()
		if rec.Code != http.StatusForbidden || !strings.Contains(body, `"code":"DENIED"`) ||
			!strings.Contains(body, "This is a read-only Bastion of "+mainURL+"; publish to quader.example.com/web/docker-local") ||
			rec.Header().Get("Docker-Distribution-Api-Version") != "registry/2.0" {
			t.Errorf("%s %s: %d %v\n%s", c.method, c.path, rec.Code, rec.Header(), body)
		}
		if mk := markingOf(t, rec); mk.Class != iface.ClassPass || mk.Repository != "web/docker-local" {
			t.Errorf("%s %s: marking %+v", c.method, c.path, mk)
		}
	}
	if len(r.shapes.asked) != 0 {
		t.Fatalf("a decision was observed for a probe: %v", r.shapes.asked)
	}
	t.Run("another format names its protocol base path", func(t *testing.T) {
		err := marker.readOnly(bastionmain.RouteMatch{Format: "npm", Repo: qname.Repo{Project: "web", Key: "npm-all"}})
		if !strings.Contains(err.Error(), "publish to "+mainURL+"/api/npm/web/npm-all/") {
			t.Fatalf("%v", err)
		}
	})
}
