package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/session"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/errs"
)

// refusingSessions refuses a cookie the way the store does (Store.Validate): a cookie of the wrong form without a lookup, a
// well-formed one that names no live session after it; both are session.ErrInvalid. looked counts the lookups.
type refusingSessions struct{ looked *int }

func (f refusingSessions) Validate(_ context.Context, cookie string) (session.Info, error) {
	if !session.WellFormed(cookie) {
		return session.Info{}, session.ErrInvalid
	}
	*f.looked++
	return session.Info{}, session.ErrInvalid
}

func (refusingSessions) CheckCSRF(*http.Request, session.Info) error { return nil }

// written is what the chain writes for the stage's verdict on a request: the problem of a rejection (httpserver.Problem
// renders it with errs.Write), or nothing for a request that passes.
type written struct {
	StatusCode int
	Header     http.Header
	Body       string
}

func answer(t *testing.T, s *Stage, r *http.Request, ep EndpointRules) (written, Result) {
	t.Helper()
	res, err := s.Authenticate(r, ep)
	rec := httptest.NewRecorder()
	if err != nil {
		errs.Write(rec, r, err, "req-1", nil)
	}
	return written{StatusCode: rec.Code, Header: rec.Header(), Body: rec.Body.String()}, res
}

func withCookie(r *http.Request, value string) *http.Request {
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: value})
	return r
}

// endedCookie is a cookie of the session form whose session has ended.
var endedCookie = session.NewSecret()

func TestR_AUTH_002_WhoamiClearsAnEndedSessionsCookie(t *testing.T) {
	looked := 0
	s := newStage(&fakeTokens{}, Options{Sessions: refusingSessions{looked: &looked}})

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run("R-AUTH-001 R-AUTH-013 "+method+" whoami with an ended session's cookie answers session_expired and clears both cookies as the sign-out does", func(t *testing.T) {
			resp, _ := answer(t, s, withCookie(req(method, whoamiPath), endedCookie), defaultRules())
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status %d", resp.StatusCode)
			}
			if !strings.Contains(resp.Body, `"code":"session_expired"`) {
				t.Fatalf("body %s", resp.Body)
			}
			if got := resp.Header.Get("WWW-Authenticate"); got != `Bearer realm="quader"` {
				t.Fatalf("challenge %q", got)
			}
			if got := resp.Header.Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control %q, want no-store: the answer carries Set-Cookie", got)
			}
			assertCleared(t, resp.Header.Values("Set-Cookie"))
			// The lines of the acceptance criterion of R-AUTH-001, as the sign-out writes them.
			want := []string{
				"__Host-qdr_session=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Lax",
				"__Host-qdr_csrf=; Path=/; Max-Age=0; Secure; SameSite=Strict",
			}
			if got := resp.Header.Values("Set-Cookie"); !slices.Equal(got, want) {
				t.Fatalf("Set-Cookie %q, want %q", got, want)
			}
		})
	}

	t.Run("R-AUTH-002 a malformed cookie and an ended session's cookie get the same answer, so it tells nothing about which sessions exist", func(t *testing.T) {
		before := looked
		malformed, _ := answer(t, s, withCookie(req("GET", whoamiPath), "not-a-session"), defaultRules())
		if looked != before {
			t.Fatal("a malformed cookie was looked up")
		}
		ended, _ := answer(t, s, withCookie(req("GET", whoamiPath), endedCookie), defaultRules())
		if looked != before+1 {
			t.Fatal("a well-formed cookie was not looked up")
		}
		if malformed.StatusCode != ended.StatusCode || !headersEqual(malformed.Header, ended.Header) {
			t.Fatalf("malformed %d %v\nended %d %v", malformed.StatusCode, malformed.Header, ended.StatusCode, ended.Header)
		}
		if malformed.Body != ended.Body {
			t.Fatalf("bodies differ:\n%s\n%s", malformed.Body, ended.Body)
		}
	})

	t.Run("R-AUTH-002 every other route answers session_expired and leaves the cookies alone", func(t *testing.T) {
		for _, c := range []struct{ method, path string }{
			{"GET", "/api/v1/formats"},
			{"GET", "/api/v1/projects"},
			{"GET", "/api/v1/system/banner"},
			{"GET", "/api/v1/auth/whoami/x"},
			{"POST", whoamiPath},
			{"GET", "/files/r/x"},
		} {
			resp, _ := answer(t, s, withCookie(req(c.method, c.path), endedCookie), defaultRules())
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("%s %s: status %d", c.method, c.path, resp.StatusCode)
			}
			if got := resp.Header.Values("Set-Cookie"); len(got) != 0 {
				t.Fatalf("%s %s cleared a cookie: %q", c.method, c.path, got)
			}
		}
	})

	t.Run("R-UI-004 without a cookie whoami is anonymous and nothing is cleared", func(t *testing.T) {
		resp, res := answer(t, s, req("GET", whoamiPath), defaultRules())
		if !res.Anonymous || len(resp.Header.Values("Set-Cookie")) != 0 {
			t.Fatalf("%+v %v", res, resp.Header)
		}
	})

	t.Run("R-AUTH-010 a live session passes and nothing is cleared", func(t *testing.T) {
		live := newStage(&fakeTokens{}, Options{Sessions: fakeSessions{info: session.Info{ID: "s1", Principal: alice}}})
		resp, res := answer(t, live, withCookie(req("GET", whoamiPath), endedCookie), defaultRules())
		if res.Credential.Method != MethodSession || len(resp.Header.Values("Set-Cookie")) != 0 {
			t.Fatalf("%+v %v", res, resp.Header)
		}
	})

	t.Run("R-EP-010 an endpoint that does not accept the session cookie ignores it and clears nothing", func(t *testing.T) {
		ep := defaultRules()
		ep.AcceptsSession = false
		resp, res := answer(t, s, withCookie(req("GET", whoamiPath), endedCookie), ep)
		if !res.Anonymous || len(resp.Header.Values("Set-Cookie")) != 0 {
			t.Fatalf("%+v %v", res, resp.Header)
		}
		closed := closedRules()
		closed.AcceptsSession = false
		resp, _ = answer(t, s, withCookie(req("GET", whoamiPath), endedCookie), closed)
		if resp.StatusCode != http.StatusUnauthorized || len(resp.Header.Values("Set-Cookie")) != 0 {
			t.Fatalf("%d %v", resp.StatusCode, resp.Header)
		}
	})

	t.Run("R-AUTH-001 a replica without sessions ignores the cookie and clears nothing", func(t *testing.T) {
		resp, res := answer(t, newStage(&fakeTokens{}, Options{}), withCookie(req("GET", whoamiPath), endedCookie), defaultRules())
		if !res.Anonymous || len(resp.Header.Values("Set-Cookie")) != 0 {
			t.Fatalf("%+v %v", res, resp.Header)
		}
	})

	t.Run("R-AUTH-001 a token beside the cookie is the credential; the cookie is neither read nor cleared", func(t *testing.T) {
		pat := newToken(t, token.TypePAT)
		ft := &fakeTokens{known: map[string]token.Info{pat: {ID: "t1", Type: token.TypePAT, Owner: alice}}}
		ts := newStage(ft, Options{Sessions: refusingSessions{looked: &looked}})
		resp, res := answer(t, ts, withCookie(req("GET", whoamiPath, "Authorization", "Bearer "+pat), endedCookie), defaultRules())
		if res.Credential.Method != MethodBearer || len(resp.Header.Values("Set-Cookie")) != 0 {
			t.Fatalf("%+v %v", res, resp.Header)
		}
	})

	t.Run("R-AUTH-001 a session that cannot be checked is 503 and keeps its cookie", func(t *testing.T) {
		down := newStage(&fakeTokens{}, Options{Sessions: fakeSessions{err: errors.New("database down")}})
		resp, _ := answer(t, down, withCookie(req("GET", whoamiPath), endedCookie), defaultRules())
		if resp.StatusCode != http.StatusServiceUnavailable || len(resp.Header.Values("Set-Cookie")) != 0 {
			t.Fatalf("%d %v", resp.StatusCode, resp.Header)
		}
	})
}

// assertCleared checks the two Set-Cookie lines of R-AUTH-013: both cookies, Max-Age=0, Secure and Path=/, no Domain; the
// session cookie HttpOnly. Without Secure and Path=/ a browser ignores the line for a __Host- cookie and keeps the cookie.
func assertCleared(t *testing.T, lines []string) {
	t.Helper()
	var names []string
	for _, l := range lines {
		name, _, _ := strings.Cut(l, "=")
		names = append(names, name)
		for _, want := range []string{"Max-Age=0", "Secure", "Path=/"} {
			if !strings.Contains(l, want) {
				t.Errorf("%q lacks %s", l, want)
			}
		}
		if strings.Contains(l, "Domain") {
			t.Errorf("%q carries a Domain", l)
		}
		if name == session.CookieSession && !strings.Contains(l, "HttpOnly") {
			t.Errorf("%q is not HttpOnly", l)
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{session.CookieCSRF, session.CookieSession}) {
		t.Fatalf("cleared %q, want both cookies", names)
	}
}

func headersEqual(a, b http.Header) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !slices.Equal(v, b[k]) {
			return false
		}
	}
	return true
}
