package engine

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// The class of the request limit after the credential's syntax (R-BAS-035, Q-BAS-529) and the answer to a malformed credential during
// the failed-authentication timeout (R-BAS-036, Q-BAS-530), with the real guard at the small figures of countingHarness (anonymous
// burst 40, authenticated burst 200, the timeout after 20 failures) and a clock that does not move. Microseconds per request, about a
// hundred requests without a body; nothing reaches the main installation.

// fromWith sends a request from addr with the header pairs given.
func (h *harness) fromWith(addr, method, path string, body string, hdr ...string) *httptest.ResponseRecorder {
	h.t.Helper()
	r := httptest.NewRequest(method, "https://packages.example.com"+path, strings.NewReader(body))
	r.RemoteAddr = addr + ":4711"
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.e.ServeHTTP(w, r)
	return w
}

// TestR_BAS_035_R_BAS_036_AMalformedCredentialCountsInTheAnonymousRequestClass: an address that sends credentials the Bastion refuses
// itself (no scheme it reads, an unreadable Basic, Basic without a colon, an unknown prefix, an empty Bearer, a token form's junk
// password) is answered 401 invalid_token for the anonymous burst of 40, the 20th failure starting the timeout, and then 429
// rate_limited under the anonymous policy: the junk header no longer buys the authenticated figures (H-2447 of the attack of
// 2026-11-27). During the timeout a malformed credential stays 401 invalid_token (Q-BAS-530), while a well-formed unknown token of the
// same address is 429 auth_timeout without a request. A well-formed token on a path that is no address counts in the authenticated
// class: 41 such requests of another address are all 404.
func TestR_BAS_035_R_BAS_036_AMalformedCredentialCountsInTheAnonymousRequestClass(t *testing.T) {
	h := countingHarness(t)
	const junk, partner = "198.51.100.20", "198.51.100.21"
	const read = "/api/npm/web/npm-all/lodash"
	headers := []string{
		"Digest abc",
		"Basic !!!",
		"Basic " + base64.StdEncoding.EncodeToString([]byte("justaname")),
		bearer("qdr_xyz_" + strings.Repeat("a", 49)),
		"Bearer ",
		"Bearer hello",
		"Token junk",
	}
	for i := range 40 {
		var rec *httptest.ResponseRecorder
		if i%8 == 7 {
			rec = h.fromWith(junk, http.MethodPost, "/v2/_token", "grant_type=password&username=x&password=junk",
				"Content-Type", "application/x-www-form-urlencoded")
		} else {
			rec = h.fromWith(junk, http.MethodGet, read, "", "Authorization", headers[i%8])
		}
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "invalid_token") {
			t.Fatalf("malformed credential %d = %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	// The 20th failure started the timeout: a well-formed token without an answer is refused by it, in the authenticated class.
	if rec := h.fromWith(junk, http.MethodGet, read, "", "Authorization", bearer(newToken(t, token.TypePAT))); rec.Code != http.StatusTooManyRequests ||
		problemCode(rec) != iface.CodeAuthTimeout {
		t.Fatalf("a well-formed token during the timeout = %d %s", rec.Code, rec.Body.String())
	}
	// The anonymous burst of 40 is spent: the 41st malformed credential is refused by the anonymous request limit, and its policy
	// names the anonymous figures of the configuration the engine reads (the built-in one here: 1,000 at 50 a second; the
	// authenticated class would be 4,000).
	rec := h.fromWith(junk, http.MethodGet, read, "", "Authorization", "Basic !!!")
	if rec.Code != http.StatusTooManyRequests || problemCode(rec) != iface.CodeRateLimited ||
		rec.Header().Get("RateLimit-Policy") != `"requests";q=1000;w=20` {
		t.Fatalf("the 41st malformed credential = %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
	// A well-formed token counts in the authenticated class whatever its path: 41 requests of a path that is no address are 404.
	tok := bearer(newToken(t, token.TypePAT))
	for i := range 41 {
		if rec := h.fromWith(partner, http.MethodGet, "/wp-admin/admin-ajax.php", "", "Authorization", tok); rec.Code != http.StatusNotFound {
			t.Fatalf("well-formed token on a path that is no address, request %d = %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if n := h.main.count(); n != 0 {
		t.Errorf("the main installation received %d requests", n)
	}
}
