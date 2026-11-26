package engine

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/errs"
)

// countingReader counts the bytes read of a body.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// TestR_BAS_095_AWriteIsRefusedAtTheBastionInTheFormatsShape: PUT, POST, DELETE and PATCH on the paths of npm, NuGet, OCI, Ansible
// Galaxy, Debian and /files/ answer 403 read_only in the shape each format declares, with the detail that names no address and
// no-store, with a credential, without one and with one the Bastion cannot read, while the main installation is reachable and
// while it is not. Nothing reaches the main installation, and none of a 1 MiB body is read. Data: one body of 1 MiB, read by no one.
func TestR_BAS_095_AWriteIsRefusedAtTheBastionInTheFormatsShape(t *testing.T) {
	s := newSiteHarness(t, options{})
	paths := []struct {
		path  string
		shape iface.ErrorShape
	}{
		{"/api/npm/web/npm-all/acme-sdk", iface.ErrorShapeNPM},
		{"/api/npm/web/npm-all/no/route/serves/this", iface.ErrorShapeNPM},
		{"/api/nuget/web/nuget-all/v3/package", iface.ErrorShapeText},
		{"/v2/web/docker-local/app/blobs/uploads/", iface.ErrorShapeDistribution},
		{"/api/ansible/web/galaxy/api/v3/artifacts/collections/", iface.ErrorShapeGalaxy},
		{"/api/deb/web/apt/pool/main/a.deb", iface.ErrorShapeProblem},
		{"/files/web/files/a/b.bin", iface.ErrorShapeProblem},
	}
	credentials := map[string][]string{
		"with a credential":                     {"Authorization", bearer(s.full)},
		"without a credential":                  nil,
		"with a credential the Bastion refuses": {"Authorization", "Basic !!!"},
	}
	refused := 0
	for _, unreachable := range []bool{false, true} {
		s.setUnreachable(unreachable)
		for name, cred := range credentials {
			for _, p := range paths {
				for _, method := range []string{http.MethodPut, http.MethodPost, http.MethodDelete, http.MethodPatch} {
					body := &countingReader{r: strings.NewReader(strings.Repeat("u", 1<<20))}
					w := s.do(method, p.path, body, cred...)
					refused++
					got, err := errs.ReadShape(p.shape, w.Header().Get("Content-Type"), w.Body.Bytes())
					if w.Code != http.StatusForbidden || err != nil || got.Code != iface.CodeReadOnly || got.Detail != errs.ReadOnlyDetail {
						t.Fatalf("%s %s %s (unreachable %v): %d %v %s", method, p.path, name, unreachable, w.Code, err, w.Body)
					}
					if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("WWW-Authenticate") != "" || w.Header().Get("Retry-After") != "" {
						t.Fatalf("%s %s %s: %v", method, p.path, name, w.Header())
					}
					if body.n != 0 {
						t.Fatalf("%s %s %s: %d bytes of the body were read", method, p.path, name, body.n)
					}
				}
			}
		}
	}
	if n := s.main.count(); n != 0 {
		t.Fatalf("the main installation received %d requests: %+v", n, s.main.requests())
	}
	if got := counterValue(t, s.harness, "refused", metrics.RefusedReadOnly); got != float64(refused) {
		t.Fatalf("refused read_only %v, want %d", got, refused)
	}
	if n := s.guard.failedCount(); n != 0 {
		t.Fatalf("%d failed authentications counted for writes the Bastion refused unread", n)
	}
}

// TestR_BAS_095_ARouteThatTakesABodyIsForwardedWithTheCallersCredential: a request that matches a read route with a body of the
// format table (npm's bulk audit) is forwarded at once with its body and the caller's credential; its answer is passed on and never
// kept. A chunked body is held up to bastion.main.max_forwarded_body and refused 413 above it.
func TestR_BAS_095_ARouteThatTakesABodyIsForwardedWithTheCallersCredential(t *testing.T) {
	s := newSiteHarness(t, options{})
	inner := s.main.doFn
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		if f.Request.Method == http.MethodPost {
			return resp(200, marking(iface.ClassPass, iface.ReadFull, "web/npm-all", tokenSubject("full", nil)), http.Header{"Content-Type": {"application/json"}}, `{"audit":"ok"}`), nil
		}
		return inner(f)
	}
	w := s.do(http.MethodPost, "/api/npm/web/npm-all/-/npm/v1/security/advisories/bulk", strings.NewReader(`{"lodash":["1.0.0"]}`), "Authorization", bearer(s.full))
	if w.Code != 200 || w.Body.String() != `{"audit":"ok"}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	reqs := s.main.requests()
	if len(reqs) != 1 || reqs[0].Kind != mainclient.KindForward {
		t.Fatalf("%+v", reqs)
	}
	if reqs[0].Header.Get("Authorization") != bearer(s.full) || string(reqs[0].Body) != `{"lodash":["1.0.0"]}` || reqs[0].Method != http.MethodPost {
		t.Fatalf("the request: %+v", reqs[0])
	}
	if s.cache.entryCount() != 0 {
		t.Fatal("the answer to a body request was kept")
	}
	s.cfg.Bastion.Main.MaxForwardedBody = 10
	r := httptest.NewRequest(http.MethodPost, "https://packages.example.com/api/npm/web/npm-all/-/npm/v1/security/advisories/bulk", io.NopCloser(strings.NewReader(strings.Repeat("z", 100))))
	r.ContentLength = -1
	r.RemoteAddr = "203.0.113.7:1"
	r.Header.Set("Authorization", bearer(s.full))
	rec := httptest.NewRecorder()
	s.e.ServeHTTP(rec, r)
	if rec.Code != 413 || problemCode(rec) != "payload_too_large" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

// TestR_BAS_095_EachShapeRenders: the one renderer of the Bastion's own answers renders each of the five shapes so that the
// shape's reader takes the code and the detail from it, with the challenge of a 401, the methods of a 405, the wait of a 429 and
// a 503, and no body for a HEAD. The galaxy shape's 429 is byte for byte the answer ansible-galaxy 2.17.14 was run against
// (bastion.md R-BAS-095's table, measured 2026-11-26).
func TestR_BAS_095_EachShapeRenders(t *testing.T) {
	answers := []*errs.Error{
		errReadOnly(),
		errs.E(errs.RateLimited, iface.CodeRateLimited, "Too many requests from this address. Try again in 14 minutes.", errs.WithRetryAfter(30*time.Second)),
		errBusy(),
		errs.E(errs.Unauthenticated, errs.CodeCredentialsRequired, "The request carries no credential and anonymous access is off.", errs.WithChallenge(`Basic realm="quader"`)),
		errs.E(errs.MethodNotAllowed, errs.CodeMethodNotAllowed, "This address serves GET and HEAD.", errs.WithAllow(http.MethodGet, http.MethodHead)),
	}
	for _, shape := range []iface.ErrorShape{iface.ErrorShapeProblem, iface.ErrorShapeNPM, iface.ErrorShapeText, iface.ErrorShapeDistribution, iface.ErrorShapeGalaxy} {
		for _, e := range answers {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				r := httptest.NewRequestWithContext(errs.WithBastionAnswer(context.Background()), method, "https://packages.example.com/api/x/web/r/p", nil)
				w := httptest.NewRecorder()
				writeShaped(w, r, shape, e)
				what := string(shape) + " " + e.Code + " " + method
				if w.Code != e.Status() {
					t.Fatalf("%s: status %d", what, w.Code)
				}
				switch e.Kind {
				case errs.Unauthenticated:
					if w.Header().Get("WWW-Authenticate") != e.Challenge {
						t.Errorf("%s: WWW-Authenticate %q", what, w.Header().Get("WWW-Authenticate"))
					}
				case errs.MethodNotAllowed:
					if w.Header().Get("Allow") != "GET, HEAD" {
						t.Errorf("%s: Allow %q", what, w.Header().Get("Allow"))
					}
				case errs.RateLimited, errs.Unavailable:
					if w.Header().Get("Retry-After") != retrySeconds(e.RetryAfter) {
						t.Errorf("%s: Retry-After %q", what, w.Header().Get("Retry-After"))
					}
				}
				if method == http.MethodHead {
					if w.Body.Len() != 0 && shape != iface.ErrorShapeProblem { // problem details leave a HEAD's body to net/http, which drops it
						t.Errorf("%s: a body of %d bytes", what, w.Body.Len())
					}
					continue
				}
				got, err := errs.ReadShape(shape, w.Header().Get("Content-Type"), w.Body.Bytes())
				if err != nil || got.Code != e.Code || got.Detail != e.Detail {
					t.Errorf("%s: %+v %v\n%s", what, got, err, w.Body)
				}
				for _, other := range []iface.ErrorShape{iface.ErrorShapeProblem, iface.ErrorShapeNPM, iface.ErrorShapeText, iface.ErrorShapeDistribution, iface.ErrorShapeGalaxy} {
					if _, err := errs.ReadShape(other, w.Header().Get("Content-Type"), w.Body.Bytes()); other != shape && err == nil {
						t.Errorf("%s reads as %s as well: %s", what, other, w.Body)
					}
				}
				if shape == iface.ErrorShapeGalaxy && e.Kind == errs.RateLimited {
					const measured = `{"errors":[{"status":"429","code":"rate_limited","title":"Too Many Requests.","detail":"Too many requests from this address. Try again in 14 minutes."}]}`
					if w.Body.String() != measured {
						t.Errorf("the galaxy shape's 429 is not the answer ansible-galaxy was run against:\n%s\n%s", w.Body, measured)
					}
				}
			}
		}
	}
}
