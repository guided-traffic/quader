package bastionmark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/api/dispatch"
	"github.com/guided-traffic/quader/internal/api/rawfiles"
	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/auth"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/npm"
	"github.com/guided-traffic/quader/internal/formats/nuget"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/repository"
)

// noTokens is a token store that knows no token: the requests carry none, and nothing here may look one up.
type noTokens struct{ t *testing.T }

func (n noTokens) Validate(context.Context, string) (token.Info, error) {
	n.t.Error("a request without a credential made the authentication stage look up a token")
	return token.Info{}, token.ErrUnknown
}

// realStageChain is the chain of the default endpoint with anonymous access off (the default), the real
// authentication stage of internal/auth behind the Bastion's wrapper of the composition root
// (v1.BastionRouteAuth), the Mark stage, and the real format dispatcher and raw file handler. The recognition is
// a stand-in: the request is recognized as a forward of the Bastion edge-1.
func realStageChain(t *testing.T) (http.Handler, *rig) {
	t.Helper()
	r := &rig{t: t, shapes: &shapes{read: iface.ReadFull}, decider: &fakeDecider{}}
	fs := []formats.Format{npm.New(), nuget.New()}
	d, err := dispatch.New(dispatch.Options{
		Host: fakeHost{}, Decider: r.decider, Formats: func() []formats.Format { return fs },
		Repos: &fakeRepos{byKey: map[string]repository.Repository{
			"web/npm-all":  {ID: "2", Name: qr("npm-all"), Class: repository.Virtual, Format: "npm"},
			"web/nuget-v3": {ID: "3", Name: qr("nuget-v3"), Class: repository.Local, Format: "nuget"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	router := httpserver.NewClientRouter()
	d.Register(router)
	rawfiles.New(rawfiles.Options{}).Register(router)
	stage := auth.New(auth.Options{Tokens: noTokens{t}})
	rules := auth.EndpointRules{Name: "default", Anonymous: false, TokenTypes: []token.Type{token.TypePAT, token.TypeSAT}}
	credential := func(req *http.Request) (httpserver.Principal, error) {
		res, err := stage.Authenticate(req, rules)
		if err != nil {
			return httpserver.Principal{}, err
		}
		if res.Anonymous {
			return httpserver.AnonymousPrincipal(), nil
		}
		return httpserver.Principal{Kind: httpserver.PrincipalKind(res.Principal.Kind), ID: res.Principal.ID, Name: res.Principal.Name}, nil
	}
	marker := New(Options{Shapes: r.shapes})
	recognize := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			rec := bastionmain.Recognition{Bastion: "edge-1", Interface: iface.InterfaceVersion}
			next.ServeHTTP(w, req.WithContext(bastionmain.WithRecognition(req.Context(), rec)))
		})
	}
	return httpserver.Chain{
		Router: router, Authenticate: v1.BastionRouteAuth(credential, nil),
		Bastion: func(next http.Handler) http.Handler { return recognize(marker.Stage()(next)) },
	}.Handler(), r
}

// TestR_BAS_095_AForwardedWriteIsTheAuthenticationStagesAndThenTheBackstops: a Bastion refuses every write itself, so the
// main installation admits no forward for routing alone (Q-BAS-118 is superseded by Q-BAS-336). With anonymous access off, a
// forwarded write without a credential is refused 401 by the authentication stage like a read, whatever its body (one with a
// credential reaches the route match and the backstop's 403 read_only, TestR_BAS_095_TheBackstopRefusesAForwardedWrite). No
// handler runs and no decision is asked.
func TestR_BAS_095_AForwardedWriteIsTheAuthenticationStagesAndThenTheBackstops(t *testing.T) {
	h, r := realStageChain(t)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPut, "/api/npm/web/npm-all/lodash", ""},
		{http.MethodPut, "/api/npm/web/npm-all/lodash", "{}"},
		{http.MethodPut, "/api/nuget/web/nuget-v3/v3/package", ""},
		{http.MethodPut, "/files/web/files-local/a/b.zip", ""},
		{http.MethodGet, "/api/npm/web/npm-all/lodash", ""},
	} {
		if rec := do(c.method, c.path, c.body); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a credential, body %q: %d %s", c.method, c.path, c.body, rec.Code, rec.Body.String())
		}
	}
	if len(r.reached) != 0 {
		t.Fatalf("a handler ran: %v", r.reached)
	}
	if len(r.shapes.asked) != 0 {
		t.Fatalf("a decision was observed for a refused forward: %v", r.shapes.asked)
	}
}
