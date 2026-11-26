package bastionmain

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_BAS_014_AForwardReachesOnlyThePackageFamilies is the route-family backstop (Q-BAS-115): a request that
// carries Quader-Bastion-Authorization with a valid Bastion token is a forward whatever its path, and a forward
// whose path, decoded segment by segment as the router decodes it, lies outside /api/<format>/ and /files/
// answers 404 not_found before any stage behind runs - never bastion_token_invalid, which a Bastion reads as the
// refusal of its own token. The paths are the management API, the interface itself, the login tree and the UI,
// spelled plainly and with escapes the router decodes.
func TestR_BAS_014_AForwardReachesOnlyThePackageFamilies(t *testing.T) {
	for _, path := range []string{
		"/api/v1/tokens", "/api/%76%31/tokens", "/api/v%31/tokens", "/%61pi/v1/tokens",
		iface.PathHandshake, "/api/%761/bastion/handshake", "/api/v1/bastion/check",
		"/auth/oidc/start", "/ui/", "/ui/bastions", "/", "/api", "/api/", "/api/v1", "//api/npm/x", "/api%2Fv1/tokens",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			r := httptest.NewRequest(method, "https://quader.example.com"+path, nil)
			r.Header.Set(iface.HeaderAuthorization, "Bearer "+tokValid)
			r.Header.Set(iface.HeaderInterface, version())
			r.Header.Set("Authorization", "Bearer "+pat)
			w, seen := run(t, RecognizeOptions{Tokens: &fakeTokens{}}, r)
			if seen != nil {
				t.Errorf("%s %s: a forward reached the stages behind the recognition", method, path)
				continue
			}
			if w.Code != http.StatusNotFound || problem(t, w)["code"] != errs.CodeNotFound {
				t.Errorf("%s %s: %d %s, want 404 not_found", method, path, w.Code, w.Body)
			}
		}
	}
	for _, path := range []string{"/api/npm/web/npm-all/lodash", "/api/%6Epm/npm-all/lodash", "/api/nuget/web/nuget-v3/v3/index.json", "/files/web/files-local/a.zip", "/files/web/files-local/"} {
		r := forwarded(path, iface.HeaderAuthorization, "Bearer "+tokValid, iface.HeaderInterface, version())
		w, seen := run(t, RecognizeOptions{Tokens: &fakeTokens{}}, r)
		if seen == nil {
			t.Errorf("%s: a forward of a package family was refused: %d %s", path, w.Code, w.Body)
			continue
		}
		if rec, _ := RecognitionFrom(seen.Context()); rec.Route || rec.Bastion != "partners" {
			t.Errorf("%s: recognition %+v", path, rec)
		}
	}
}

// TestR_BAS_051_TheMainInstallationForwardsTheRegistryRoot is the main installation's side of R-BAS-051: the backstop
// admits the registry root as a family a Bastion forwards - a repository route, the three core routes, the root without
// its slash and a segment the router decodes to v2 - and still refuses the management API however it is spelled, and a
// /v2 whose encoded slash the router keeps in one segment. Named by the decision until the Bastion's side (package C of
// Every format) proves the requirement.
func TestR_BAS_051_TheMainInstallationForwardsTheRegistryRoot(t *testing.T) {
	admitted := []string{
		"/v2/web/docker/app/manifests/1", "/v2/web/docker-local/app/blobs/uploads/", "/v2/_token", "/v2/_catalog", "/v2/", "/v2",
		"/%76%32/_token", "/v2/library/alpine/manifests/3",
	}
	for _, path := range admitted {
		if !forwardablePath(routingSegments(path)) {
			t.Errorf("%s: not admitted", path)
		}
		r := forwarded(path, iface.HeaderAuthorization, "Bearer "+tokValid, iface.HeaderInterface, version())
		w, seen := run(t, RecognizeOptions{Tokens: &fakeTokens{}}, r)
		if seen == nil {
			t.Errorf("%s: a forward of the registry root was refused: %d %s", path, w.Code, w.Body)
			continue
		}
		if rec, _ := RecognitionFrom(seen.Context()); rec.Route || rec.Bastion != "partners" {
			t.Errorf("%s: recognition %+v", path, rec)
		}
	}
	for _, path := range []string{"/api/%76%31/tokens", "/api/v1/tokens", "/v2%2Fweb/docker/app/manifests/1", "/v3/x", "/v"} {
		if forwardablePath(routingSegments(path)) {
			t.Errorf("%s: admitted", path)
		}
	}
	t.Run("a forwarded registry write is recognized as a forward, for the marking's backstop to refuse", func(t *testing.T) {
		// A Bastion refuses every write itself (R-BAS-095); one that reaches the main installation anyway is a defect, which the
		// recognition passes on like any forward, with or without a body or a credential, so that the marking refuses it.
		for name, edit := range map[string]func(*http.Request){
			"no body":        func(*http.Request) {},
			"a body":         func(r *http.Request) { r.ContentLength = 2 },
			"a chunked body": func(r *http.Request) { r.TransferEncoding = []string{"chunked"} },
			"a credential":   func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+pat) },
		} {
			w := forwarded("/v2/web/docker-local/app/blobs/uploads/", iface.HeaderAuthorization, "Bearer "+tokValid, iface.HeaderInterface, version())
			w.Method = http.MethodPost
			edit(w)
			_, seen := run(t, RecognizeOptions{Tokens: &fakeTokens{}}, w)
			if seen == nil {
				t.Fatalf("a write with %s was refused by the recognition", name)
			}
			if rec, ok := RecognitionFrom(seen.Context()); !ok || rec.Route {
				t.Errorf("a write with %s: recognition %+v", name, rec)
			}
		}
	})
}

// TestR_BAS_008_AnInterfaceCallCarriesNoQuaderBastionAuthorization: the interface accepts the Bastion token in
// Authorization only on a request without Quader-Bastion-Authorization (Q-BAS-115). With that header the request is
// a forward, also when its path decodes to an interface route, and a forward never reaches an interface handler;
// without it a path the router decodes to an interface route is one.
func TestR_BAS_008_AnInterfaceCallCarriesNoQuaderBastionAuthorization(t *testing.T) {
	t.Run("the Bastion token in both headers on the handshake is a forward, refused", func(t *testing.T) {
		r := forwarded(iface.PathHandshake, "Authorization", "Bearer "+tokValid, iface.HeaderAuthorization, "Bearer "+tokValid, iface.HeaderInterface, version())
		w, seen := run(t, RecognizeOptions{Tokens: &fakeTokens{}}, r)
		if seen != nil || w.Code != http.StatusNotFound {
			t.Fatalf("status %d, reached %v", w.Code, seen != nil)
		}
	})
	t.Run("another credential in Quader-Bastion-Authorization leaves an interface path an ordinary request", func(t *testing.T) {
		r := forwarded(iface.PathHandshake, "Authorization", "Bearer "+tokValid, iface.HeaderAuthorization, "Bearer "+pat, iface.HeaderInterface, version())
		tokens := &fakeTokens{}
		w, seen := run(t, RecognizeOptions{Tokens: tokens}, r)
		if seen == nil || tokens.calls != 0 {
			t.Fatalf("status %d, lookups %d", w.Code, tokens.calls)
		}
		if _, ok := RecognitionFrom(seen.Context()); ok {
			t.Fatal("the request was recognized")
		}
	})
	t.Run("a path the router decodes to the handshake is the handshake", func(t *testing.T) {
		r := forwarded("/api/%761/bastion/handshake", "Authorization", "Bearer "+tokValid, iface.HeaderInterface, version())
		w, seen := run(t, RecognizeOptions{Tokens: &fakeTokens{}}, r)
		if seen == nil {
			t.Fatalf("status %d %s", w.Code, w.Body)
		}
		if rec, _ := RecognitionFrom(seen.Context()); !rec.Route || seen.Header.Get("Authorization") != "" {
			t.Fatalf("recognition %+v, Authorization %q", rec, seen.Header.Get("Authorization"))
		}
	})
	t.Run("a path whose encoded slash the router keeps in one segment is no interface route", func(t *testing.T) {
		r := forwarded("/api%2Fv1/bastion/handshake", "Authorization", "Bearer "+tokValid, iface.HeaderInterface, version())
		tokens := &fakeTokens{}
		if _, seen := run(t, RecognizeOptions{Tokens: tokens}, r); seen == nil || tokens.calls != 0 {
			t.Fatalf("lookups %d", tokens.calls)
		}
	})
}

// TestR_BAS_017_TheForwardedHeadersAreReDerivedOnlyAfterTheTokenCheck: the re-derivation of the client address
// and the host runs for a recognized request only, and the stages behind see its context; a request with a token
// the authenticator refuses, or without a Bastion token, is never re-derived (Q-BAS-116).
func TestR_BAS_017_TheForwardedHeadersAreReDerivedOnlyAfterTheTokenCheck(t *testing.T) {
	type key struct{}
	calls := 0
	o := RecognizeOptions{Tokens: &fakeTokens{}, Forwarded: func(r *http.Request) context.Context {
		calls++
		if _, ok := RecognitionFrom(r.Context()); !ok {
			t.Error("the re-derivation ran before the request was recognized")
		}
		return context.WithValue(r.Context(), key{}, "re-derived")
	}}
	_, seen := run(t, o, forwarded("/api/npm/web/npm-all/x", iface.HeaderAuthorization, "Bearer "+tokValid, iface.HeaderInterface, version()))
	if seen == nil || seen.Context().Value(key{}) != "re-derived" || calls != 1 {
		t.Fatalf("calls %d", calls)
	}
	run(t, o, forwarded("/api/npm/web/npm-all/x", iface.HeaderAuthorization, "Bearer "+tokRevoked, iface.HeaderInterface, version()))
	run(t, o, forwarded("/api/npm/web/npm-all/x", iface.HeaderAuthorization, "Bearer "+pat, "X-Forwarded-For", "192.0.2.1"))
	run(t, o, forwarded("/api/v1/tokens", iface.HeaderAuthorization, "Bearer "+tokValid, iface.HeaderInterface, version()))
	if calls != 1 {
		t.Fatalf("re-derived %d times; only the recognized forward of a package family is", calls)
	}
}
