package engine

import (
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
)

// TestQ_BAS_203_TheGalaxyTokenSchemeIsACredentialOfTheProtocolFamily: "Authorization: Token <Quader token>", which ansible-galaxy sends
// and the main installation reads on the routes that declare it, is forwarded unchanged on /api/<format>/…; on another family it is an
// unknown scheme, refused without a request. Its answers are the token's under that scheme only: the main installation's refusal of
// the scheme on a route that does not declare it never refuses the same token sent as Bearer.
func TestQ_BAS_203_TheGalaxyTokenSchemeIsACredentialOfTheProtocolFamily(t *testing.T) {
	s := newSiteHarness(t, options{})
	tok := s.full
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		if strings.HasPrefix(f.Request.Header.Get("Authorization"), "Token ") && !strings.HasPrefix(f.Request.URL.Path, "/api/ansible/") {
			return resp(http.StatusUnauthorized, marking(iface.ClassPass, "", "", iface.Subject{Kind: iface.SubjectInvalid}), nil, ""), nil
		}
		return resp(http.StatusOK, marking(iface.ClassPass, iface.ReadFull, "web/npm-all", tokenSubject("full", nil)), nil, `{}`), nil
	}
	galaxy := "/api/ansible/web/galaxy/api/v3/collections/acme/tools/"
	before := len(s.main.requests())
	if w := s.get(galaxy, "Authorization", "Token "+tok); w.Code != http.StatusOK {
		t.Fatalf("the Galaxy route with the scheme Token: %d %s", w.Code, w.Body)
	}
	if reqs := s.main.requests()[before:]; len(reqs) != 1 || reqs[0].Header.Get("Authorization") != "Token "+tok {
		t.Fatalf("forwarded as %+v", reqs)
	}
	before = s.main.count()
	if w := s.get("/files/web/npm-all/a.txt", "Authorization", "Token "+tok); w.Code != http.StatusUnauthorized || s.main.count() != before {
		t.Fatalf("the scheme Token on /files/: %d, %d requests", w.Code, s.main.count()-before)
	}
	if w := s.get("/api/npm/web/npm-all/lodash", "Authorization", "Token "+tok); w.Code != http.StatusUnauthorized {
		t.Fatalf("the scheme Token on npm, which the main installation refuses: %d", w.Code)
	}
	if w := s.get("/api/npm/web/npm-all/lodash", "Authorization", bearer(tok)); w.Code != http.StatusOK {
		t.Fatalf("the same token as Bearer after the scheme's refusal: %d %s", w.Code, w.Body)
	}
}
