package registry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/api/bastionmark"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/oci"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/qname"
)

// The main installation's side of the registry root through a Bastion (bastion.md R-BAS-051): the tests carry the decisions'
// IDs, since the requirement is proven once the Bastion's side forwards /v2/.

// noBody fails the test when the request's body is read.
type noBody struct{ t *testing.T }

func (b noBody) Read([]byte) (int, error) {
	b.t.Error("the body of a probe was read")
	return 0, io.EOF
}

// markedChain serves the token service and the catalog behind the Mark stage and a stand-in for the recognition: a request
// with Quader-Bastion-Probe is a probe of the Bastion edge-1, one with X-Test-Forward a forward, any other an ordinary
// request. The chain's authentication sees every request as anonymous, as the stage sees /v2/_token (the token service
// presents the credential itself).
func markedChain(t *testing.T, tok *Token, cat *Catalog) http.Handler {
	t.Helper()
	router := httpserver.NewClientRouter()
	router.Handle("/v2/_token", tok)
	router.Handle("/v2/_catalog", cat)
	marker := bastionmark.New(bastionmark.Options{MainURL: "https://quader.example.com", RegistryRoot: "oci"})
	return httpserver.Chain{
		Router:       router,
		Authenticate: func(*http.Request) (httpserver.Principal, error) { return httpserver.AnonymousPrincipal(), nil },
		Bastion: func(next http.Handler) http.Handler {
			next = marker.Stage()(next)
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec := bastionmain.Recognition{Bastion: "edge-1", Interface: iface.InterfaceVersion}
				switch {
				case r.Header.Get(iface.HeaderProbe) == iface.ProbeValue:
					rec.Probe = r.Method != http.MethodGet && r.Method != http.MethodHead
				case r.Header.Get("X-Test-Forward") == "":
					next.ServeHTTP(w, r)
					return
				}
				next.ServeHTTP(w, r.WithContext(bastionmain.WithRecognition(r.Context(), rec)))
			})
		},
	}.Handler()
}

// noNames is a data port that holds no image name.
type noNames struct{}

func (noNames) CatalogNames(context.Context, int64, string, int) ([]string, error) { return nil, nil }

func catalogOf(t *testing.T) *Catalog {
	t.Helper()
	c, err := NewCatalog(CatalogOptions{
		Repos: repos{}, Decider: decider{}, Store: noNames{}, Format: "oci", Documents: NewDocuments(oci.New()), WriteError: writeError,
		Bound: func(*http.Request) (qname.Repo, bool, error) { return qname.Repo{}, false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestQ_BAS_166_TheTokenServiceAndTheCatalogTellTheMarkingTheirKind: below /v2/ the token service and the catalog tell the
// marking stage their route before they authenticate or read a body. The probe of a POST to the token service is answered
// 204 read-with-body without its form being read, so the Bastion forwards the real request with the form; the probe of a
// method neither route answers gets the route's own 405, not a 204. A token answer is pass, never kept, and names the
// caller the service accepted (Q-BAS-187); a refused credential is invalid; the catalog is pass.
func TestQ_BAS_166_TheTokenServiceAndTheCatalogTellTheMarkingTheirKind(t *testing.T) {
	f := newFixture(t, false)
	f.allow[aliceID+" web/docker-local app read"] = true
	h := markedChain(t, f.h, catalogOf(t))
	do := func(method, target string, body io.Reader, hdr ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, body)
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	marking := func(t *testing.T, w *httptest.ResponseRecorder) iface.Marking {
		t.Helper()
		m, ok, err := iface.ParseMarking(w.Header())
		if !ok || err != nil {
			t.Fatalf("no marking (%v): %v", err, w.Header())
		}
		return m
	}

	t.Run("the probe of a token request with a form is a read route that takes a body", func(t *testing.T) {
		w := do(http.MethodPost, "/v2/_token", noBody{t}, iface.HeaderProbe, iface.ProbeValue, "Content-Type", "application/x-www-form-urlencoded")
		if w.Code != http.StatusNoContent || w.Header().Get(iface.HeaderRoute) != iface.RouteReadWithBody || marking(t, w).Class != iface.ClassPass {
			t.Fatalf("%d %v", w.Code, w.Header())
		}
		if f.count("issued") > 0 {
			t.Fatal("the probe issued a token")
		}
	})
	t.Run("the real request with the form is served and names the caller", func(t *testing.T) {
		form := url.Values{"grant_type": {"password"}, "username": {"ci"}, "password": {f.pat}, "scope": {"repository:web/docker-local/app:pull"}}
		w := do(http.MethodPost, "/v2/_token", strings.NewReader(form.Encode()), "X-Test-Forward", "1", "Content-Type", "application/x-www-form-urlencoded")
		mk := marking(t, w)
		want := "token=t1; principal=" + aliceID + "; expires=" + strconv.FormatInt(f.expires.Unix(), 10)
		if w.Code != http.StatusOK || mk.Class != iface.ClassPass || mk.Repository != "" || w.Header().Get(iface.HeaderSubject) != want {
			t.Fatalf("%d %v", w.Code, w.Header())
		}
	})
	t.Run("a GET with Basic credentials names the caller, a refused one is invalid", func(t *testing.T) {
		w := do(http.MethodGet, "/v2/_token?scope="+url.QueryEscape("repository:web/docker-local/app:pull"), nil, "X-Test-Forward", "1", "Authorization", basic(f.pat))
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get(iface.HeaderSubject), "token=t1; principal="+aliceID) {
			t.Fatalf("%d %v", w.Code, w.Header())
		}
		w = do(http.MethodGet, "/v2/_token", nil, "X-Test-Forward", "1", "Authorization", basic("qdr_pat_nottheone"))
		if w.Code != http.StatusUnauthorized || w.Header().Get(iface.HeaderSubject) != "invalid" {
			t.Fatalf("%d %v", w.Code, w.Header())
		}
	})
	t.Run("a probe of another method gets the route's 405", func(t *testing.T) {
		for _, target := range []string{"/v2/_token", "/v2/_catalog"} {
			w := do(http.MethodPut, target, noBody{t}, iface.HeaderProbe, iface.ProbeValue)
			if w.Code != http.StatusMethodNotAllowed || w.Header().Get(iface.HeaderRoute) != "" {
				t.Errorf("%s: %d %v", target, w.Code, w.Header())
			}
		}
		if w := do(http.MethodPost, "/v2/_catalog", noBody{t}, iface.HeaderProbe, iface.ProbeValue); w.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST /v2/_catalog: %d", w.Code)
		}
	})
	t.Run("the catalog is pass", func(t *testing.T) {
		w := do(http.MethodGet, "/v2/_catalog", nil, "X-Test-Forward", "1")
		if mk := marking(t, w); w.Code != http.StatusOK || mk.Class != iface.ClassPass || mk.Repository != "" || mk.Subject.Kind != iface.SubjectAnonymous {
			t.Fatalf("%d %v", w.Code, w.Header())
		}
	})
	t.Run("an ordinary request is served as before and carries no marking", func(t *testing.T) {
		w := do(http.MethodGet, "/v2/_token", nil, "Authorization", basic(f.pat))
		if w.Code != http.StatusOK || w.Header().Get(iface.HeaderClass) != "" {
			t.Fatalf("%d %v", w.Code, w.Header())
		}
	})
}

// oneBastion knows one Bastion token.
type oneBastion struct{}

const theBST = "qdr_bst_registryregistryregistryregistry"

func (oneBastion) Authenticate(_ context.Context, raw string, _ bastionmain.AuthRequest) (bastionmain.BastionToken, error) {
	if raw != theBST {
		return bastionmain.BastionToken{}, bastionmain.ErrTokenInvalid
	}
	return bastionmain.BastionToken{TokenID: "t-bst", BastionID: "b-1", Name: "partners", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// TestQ_BAS_166_ABastionsHostReachesTheTokenServicesServiceCheck establishes that the token service, reached by a forward,
// checks service against the Bastion's host (R-BAS-051): the recognition re-derives the forwarded headers of a recognized
// request with the Bastion's egress trusted (R-BAS-017, the composition root's bastionForwarded), and the token service reads
// the response base URL the chain hands on. The Bastion's host is accepted and the main installation's own refused; the same
// headers without a Bastion token count for nothing, and the main installation's host stays the service.
func TestQ_BAS_166_ABastionsHostReachesTheTokenServicesServiceCheck(t *testing.T) {
	const egress, caller = "198.51.100.20", "203.0.113.7"
	f := newFixture(t, false)
	main, err := httpserver.NewResolver(nil, "https://quader.example.com", []string{"https://packages.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	withEgress, err := httpserver.NewResolver([]string{egress + "/32"}, "https://quader.example.com", []string{"https://packages.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	router := httpserver.NewClientRouter()
	router.Handle("/v2/_token", f.h)
	h := httpserver.Chain{
		Router: router, Resolver: main,
		Authenticate: func(*http.Request) (httpserver.Principal, error) { return httpserver.AnonymousPrincipal(), nil },
		Bastion: bastionmain.Stages{
			Recognize: bastionmain.Recognize(bastionmain.RecognizeOptions{
				Tokens: oneBastion{},
				Forwarded: func(r *http.Request) context.Context {
					return httpserver.WithResolved(r.Context(), withEgress.Resolve(r))
				},
				Problem: func(w http.ResponseWriter, r *http.Request, err error) { errs.Write(w, r, err, "", nil) },
			}),
			Mark: bastionmark.New(bastionmark.Options{}).Stage(),
		}.Chain(),
	}.Handler()
	token := func(service string, bastion bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "http://quader.example.com/v2/_token?service="+service, nil)
		r.RemoteAddr = egress + ":40000"
		r.Header.Set("Authorization", basic(f.pat))
		r.Header.Set("X-Forwarded-For", caller)
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "packages.example.com")
		if bastion {
			r.Header.Set(iface.HeaderAuthorization, "Bearer "+theBST)
			r.Header.Set(iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := token("packages.example.com", true); w.Code != http.StatusOK {
		t.Fatalf("the Bastion's host through a recognized forward: %d %s", w.Code, w.Body)
	}
	if w := token("quader.example.com", true); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "This token service serves packages.example.com") {
		t.Fatalf("the main installation's host through a Bastion: %d %s", w.Code, w.Body)
	}
	if w := token("packages.example.com", false); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "serves quader.example.com") {
		t.Fatalf("the forwarded host without a Bastion token: %d %s", w.Code, w.Body)
	}
}
