package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
)

// A Bastion names nothing of the main installation (bastion.md R-BAS-099, Q-BAS-338): the tests below hold rows 2 to 6 and 9 of
// its table at the engine, against the fake main installation, whose host is quader.example.com.

// noMainInstallation fails the test when an answer of the Bastion names the main installation's host, the party "main
// installation", a path of the management API or a link into the main installation's web interface.
func noMainInstallation(t *testing.T, what string, w *httptest.ResponseRecorder) {
	t.Helper()
	var all strings.Builder
	for k, vs := range w.Header() {
		all.WriteString(k + ": " + strings.Join(vs, ", ") + "\n")
	}
	all.Write(w.Body.Bytes())
	for _, bad := range []string{"quader.example.com", "main installation", "/api/v1/", "/ui/"} {
		if strings.Contains(all.String(), bad) {
			t.Errorf("%s: the answer names %q:\n%s", what, bad, all.String())
		}
	}
}

// problemOf reads a problem details body.
func problemOf(t *testing.T, w *httptest.ResponseRecorder) errs.Problem {
	t.Helper()
	var p errs.Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("%d %s: %v", w.Code, w.Body, err)
	}
	return p
}

// ownAnswer reads one of the Bastion's own answers in the shape the format of its path declares (R-BAS-095): npm's on npm's paths,
// problem details on a path that is not served, whose type is about:blank and whose title is the status phrase.
func ownAnswer(t *testing.T, w *httptest.ResponseRecorder, path string) errs.Shaped {
	t.Helper()
	shape := iface.ErrorShapeProblem
	if strings.HasPrefix(path, "/api/npm/") {
		shape = iface.ErrorShapeNPM
	}
	got, err := errs.ReadShape(shape, w.Header().Get("Content-Type"), w.Body.Bytes())
	if err != nil {
		t.Fatalf("%d %s: not the shape %s: %v", w.Code, w.Body, shape, err)
	}
	if p := problemOf(t, w); shape == iface.ErrorShapeProblem && (p.Type != errs.AboutBlank || p.Title != http.StatusText(w.Code)) {
		t.Errorf("type %q title %q", p.Type, p.Title)
	}
	return got
}

// TestR_BAS_099_TheBastionsOwnAnswersNameNoParty: every answer of the Bastion's own has the text of Q-BAS-321, which names
// neither the Bastion nor the main installation, in the format's error shape; as problem details its type is about:blank and its
// title the status phrase (rows 2 and 3).
// The codes are E7's to rename; the RateLimit policy names carry no prefix (row 4).
func TestR_BAS_099_TheBastionsOwnAnswersNameNoParty(t *testing.T) {
	small := func(c *config.BastionConfig) { c.Bastion.Main.MaxForwardedBody = 4 }
	for _, c := range []struct {
		name   string
		o      options
		doErr  error
		method string
		path   string
		host   string
		status int
		code   string
		detail string
		// problem: the answer is problem details whatever the path (before the first handshake no format's shape is known, H-2359).
		problem bool
	}{
		{
			name: "unreachable", o: options{down: true}, method: http.MethodGet, path: "/api/npm/web/npm-all/lodash", status: 503,
			code: iface.CodeUnavailable, detail: "This service is not available right now; try again later.", problem: true,
		},
		{
			name: "busy", doErr: mainclient.ErrBusy, method: http.MethodGet, path: "/api/npm/web/npm-all/lodash", status: 503,
			code: iface.CodeBusy, detail: "Too many requests are waiting; try again in a moment.",
		},
		{
			name: "bad gateway", doErr: errors.New("connection reset"), method: http.MethodGet, path: "/api/npm/web/npm-all/lodash", status: 502,
			code: iface.CodeBadGateway, detail: "This service could not answer; try again later.",
		},
		{
			name: "gateway timeout", doErr: context.DeadlineExceeded, method: http.MethodGet, path: "/api/npm/web/npm-all/lodash", status: 504,
			code: iface.CodeGatewayTimeout, detail: "This service did not answer in time; try again later.",
		},
		{name: "not served", method: http.MethodGet, path: "/login", status: 404, code: iface.CodeNotFound, detail: "Nothing is served at this address."},
		{
			name: "method", method: http.MethodOptions, path: "/api/npm/web/npm-all/lodash", status: 405, code: iface.CodeMethodNotAllowed,
			detail: "This address serves GET and HEAD.",
		},
		{
			name: "misdirected", method: http.MethodGet, path: "/api/npm/web/npm-all/lodash", host: "quader.internal", status: 421,
			code: iface.CodeMisdirected, detail: "This address does not answer for the host of the request.",
		},
		{
			name: "too large", o: options{cfg: []func(*config.BastionConfig){small}}, method: http.MethodPost,
			path: "/api/npm/web/npm-all/-/npm/v1/security/advisories/bulk", status: 413, code: iface.CodePayloadTooLarge, detail: "The body is too large.",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, c.o)
			if c.doErr != nil {
				h.main.doFn = func(mainclient.Forward) (*mainclient.Response, error) { return nil, c.doErr }
			}
			r := httptest.NewRequest(c.method, "https://packages.example.com"+c.path, strings.NewReader("12345678"))
			r.RemoteAddr = "203.0.113.7:4711"
			r.Header.Set("Authorization", bearer(newToken(t, "pat")))
			if c.host != "" {
				r.Host = c.host
			}
			w := httptest.NewRecorder()
			h.e.ServeHTTP(w, r)
			path := c.path
			if c.problem {
				path = "/"
			}
			p := ownAnswer(t, w, path)
			if w.Code != c.status || p.Code != c.code || p.Detail != c.detail || strings.Contains(p.Detail, "Bastion") {
				t.Fatalf("%d %s, want %d %s %q", w.Code, w.Body, c.status, c.code, c.detail)
			}
			noMainInstallation(t, c.name, w)
		})
	}

	t.Run("the guard's refusals", func(t *testing.T) {
		h := newHarness(t, options{})
		for reason, want := range map[string]string{
			metrics.RefusedRateLimited: `"requests"`, metrics.RefusedMissLimited: `"misses"`, metrics.RefusedAuthTimeout: `"failed-auth"`,
			metrics.RefusedBlocked: `"blocking"`,
		} {
			code := iface.CodeRateLimited
			switch reason {
			case metrics.RefusedAuthTimeout:
				code = iface.CodeAuthTimeout
			case metrics.RefusedBlocked:
				code = iface.CodeAddressBlocked
			}
			h.guard.request = guard.Verdict{Status: 429, Code: code, RetryAfter: 3 * time.Second, Reason: reason}
			w := h.get("/api/npm/web/npm-all/lodash")
			if p := ownAnswer(t, w, "/api/npm/"); w.Code != 429 || p.Code != code || strings.Contains(p.Detail, "Bastion") {
				t.Errorf("%s: %d %s", reason, w.Code, w.Body)
			}
			if pol := w.Header().Get("RateLimit-Policy"); !strings.HasPrefix(pol, want+";") || strings.Contains(pol, "bastion") {
				t.Errorf("%s: RateLimit-Policy %q, want the name %s", reason, pol, want)
			}
			noMainInstallation(t, reason, w)
		}
	})
}

// TestR_BAS_099_AForwardedAnswerCarriesOnlyTheAllowedHeaders: the headers of an answer of the main installation pass an
// allowlist (row 5): the core's representation headers, the format's declared replay headers and the fixed list of the framing,
// the wait, the challenge, the methods, the links and the distribution's version. The member repository that served the answer,
// every other Quader- header and the main installation's RateLimit fields, request ID and unknown headers stay at the Bastion.
func TestR_BAS_099_AForwardedAnswerCarriesOnlyTheAllowedHeaders(t *testing.T) {
	h := newHarness(t, options{})
	tok := newToken(t, "pat")
	sub := tokenSubject("t-1", nil)
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusTooManyRequests} {
		h.main.doFn = func(mainclient.Forward) (*mainclient.Response, error) {
			hdr := http.Header{
				"Content-Type": {"application/json"}, "Etag": {`"x"`}, "Cache-Control": {"no-store"}, "Link": {`</next>; rel="next"`},
				"Www-Authenticate": {`Basic realm="quader"`}, "Retry-After": {"3"}, "Allow": {"GET, HEAD"},
				"Docker-Distribution-Api-Version": {"registry/2.0"},
				"Quader-Resolved-From":            {"web/npm-remote"}, "Quader-Annotations-Cut": {"true"},
				"Ratelimit": {`"anonymous-place";r=0;t=3`}, "Ratelimit-Policy": {`"anonymous-place";q=100;w=3600`},
				"X-Request-Id": {"main-req-1"}, "X-Quader-Replica": {"web-1"}, "Cache-Status": {"quader; hit"},
				"Set-Cookie": {"a=b"}, "X-Internal-Host": {"quader.example.com"}, "Server": {"quader/1.2.3"},
			}
			return resp(status, marking(iface.ClassPass, iface.ReadFull, "web/npm-all", sub), hdr, `{}`), nil
		}
		w := h.get("/api/npm/web/npm-all/-/v1/search?text=x", "Authorization", bearer(tok))
		if w.Code != status {
			t.Fatalf("%d: %d %s", status, w.Code, w.Body)
		}
		for _, k := range []string{"Content-Type", "Etag", "Cache-Control", "Link", "Www-Authenticate", "Retry-After", "Allow", "Docker-Distribution-Api-Version", "Content-Length"} {
			if w.Header().Get(k) == "" {
				t.Errorf("%d: %s did not reach the caller: %v", status, k, w.Header())
			}
		}
		for _, k := range []string{"Quader-Resolved-From", "Quader-Annotations-Cut", "Ratelimit", "Ratelimit-Policy", "X-Request-Id", "X-Quader-Replica", "Cache-Status", "Set-Cookie", "X-Internal-Host", "Server"} {
			if v := w.Header().Get(k); v != "" {
				t.Errorf("%d: %s reached the caller: %q", status, k, v)
			}
		}
		noMainInstallation(t, http.StatusText(status), w)
	}

	t.Run("a format's declared header passes", func(t *testing.T) {
		got := replayHeader(http.Header{"Docker-Content-Digest": {"sha256:1"}, "Npm-Notice": {"n"}}, []string{"Docker-Content-Digest"})
		if got.Get("Docker-Content-Digest") == "" || got.Get("Npm-Notice") != "" {
			t.Fatalf("%v", got)
		}
	})
	t.Run("a header the answer's Connection names is dropped", func(t *testing.T) {
		got := replayHeader(http.Header{"Connection": {"Link"}, "Link": {"</x>"}, "Etag": {`"e"`}}, nil)
		if got.Get("Link") != "" || got.Get("Etag") == "" {
			t.Fatalf("%v", got)
		}
	})
}

// TestR_BAS_099_ALocationOnAnotherHostIsAnsweredBadGateway: a Location of the main installation's answer is passed on only as a
// relative reference or on the Bastion's own scheme, host and port; any other answers 502 and is counted (row 6).
func TestR_BAS_099_ALocationOnAnotherHostIsAnsweredBadGateway(t *testing.T) {
	h := newHarness(t, options{})
	tok := newToken(t, "pat")
	sub := tokenSubject("t-1", nil)
	refused := 0.0
	for _, c := range []struct {
		location string
		passed   bool
	}{
		{"/files/web/raw-local/folder/", true},
		{"folder/", true},
		{"https://packages.example.com/files/web/raw-local/folder/", true},
		{"https://PACKAGES.example.com:443/files/web/raw-local/folder/", true},
		{"https://quader.example.com/files/web/raw-local/folder/", false},
		{"https://objects.example.net/bucket/sha256/ab?X-Amz-Signature=1", false},
		{"//quader.example.com/files/", false},
		{`/\quader.example.com/files/`, false},
		{"http://packages.example.com/files/", false},
		{"https://packages.example.com:8443/files/", false},
		{"https://packages.example.com.evil/files/", false},
		{"::not a url", false},
	} {
		h.main.doFn = func(mainclient.Forward) (*mainclient.Response, error) {
			return resp(http.StatusMovedPermanently, marking(iface.ClassPass, iface.ReadFull, "web/raw-local", sub), http.Header{"Location": {c.location}}, ""), nil
		}
		w := h.get("/files/web/raw-local/folder", "Authorization", bearer(tok))
		switch {
		case c.passed && (w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != c.location):
			t.Errorf("%s: %d %v", c.location, w.Code, w.Header())
		case !c.passed:
			refused++
			if p := problemOf(t, w); w.Code != http.StatusBadGateway || w.Header().Get("Location") != "" || p.Type != errs.AboutBlank {
				t.Errorf("%s: %d %v %s", c.location, w.Code, w.Header(), w.Body)
			}
			noMainInstallation(t, c.location, w)
		}
	}
	if got := counterValue(t, h, "refused", metrics.RefusedForeignLocation); got != refused {
		t.Fatalf("quader_bastion_refused_total{reason=foreign_location} = %v, want %v", got, refused)
	}
}

// TestR_BAS_099_TheBastionsOwnChallengesNameItsHost: the challenge of the Bastion's own 401 names the Bastion's host only, below
// the registry root and on a package path (row 9); the main installation's challenge of a forwarded 401 is its own on the
// response base URL, which is the Bastion's (R-BAS-017), and passes as it came (TestR_BAS_099_AForwardedAnswerCarriesOnlyThe
// AllowedHeaders, and the integration replays' scan).
func TestR_BAS_099_TheBastionsOwnChallengesNameItsHost(t *testing.T) {
	h := newHarness(t, options{})
	h.setAnonymous(false)
	for path, want := range map[string]string{
		"/v2/":                        `Bearer realm="https://packages.example.com/v2/_token",service="packages.example.com"`,
		"/api/npm/web/npm-all/lodash": `Basic realm="quader"`,
	} {
		w := h.get(path)
		if w.Code != http.StatusUnauthorized || w.Header().Get("Www-Authenticate") != want {
			t.Errorf("%s: %d %q", path, w.Code, w.Header().Get("Www-Authenticate"))
		}
		noMainInstallation(t, path, w)
	}
}
