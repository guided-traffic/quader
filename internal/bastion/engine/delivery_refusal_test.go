package engine

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
)

// TestR_CUR_004_ARuleRefusalReachesTheClientThroughABastionAndIsNotKept: the main installation refuses an artifact its delivery
// rules do not deliver with 403 not_allowed_in_repository and Cache-Control: no-store (remote-curation.md R-CUR-004). A Bastion relays
// that answer unchanged, and it keeps no such refusal (Q-CUR-44): once a rule allows the version, the next request reaches the main
// installation and is served, inside the answer period that a kept refusal would have been replayed for. Data: one tarball of 1 kB.
func TestR_CUR_004_ARuleRefusalReachesTheClientThroughABastionAndIsNotKept(t *testing.T) {
	s := newSiteHarness(t, options{})
	s.site.artifacts[tgz] = payload(1000)
	var refusing atomic.Bool
	refusing.Store(true)
	serve := s.main.doFn
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		if refusing.Load() && !f.Probe && f.Request.URL.Path == tgz {
			sub, r, _ := s.site.subject(tokenOf(f.Request.Header))
			return resp(http.StatusForbidden, marking(iface.ClassPass, r.read, s.site.repo, sub),
				http.Header{"Content-Type": {"application/problem+json"}, "Cache-Control": {"no-store"}},
				`{"code":"not_allowed_in_repository","detail":"@acme/sdk 1.0.0 is not allowed in web/npm-all."}`), nil
		}
		return serve(f)
	}

	for i := range 2 {
		w := s.get(tgz, "Authorization", bearer(s.full))
		if w.Code != http.StatusForbidden || problemCode(w) != "not_allowed_in_repository" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("request %d: %d %q %s, want the main installation's 403 not_allowed_in_repository with no-store", i, w.Code, w.Header().Get("Cache-Control"), w.Body)
		}
	}
	if n := s.main.count(); n != 2 {
		t.Fatalf("%d requests reached the main installation for two refused reads, want 2: a refusal that says no-store is not kept", n)
	}

	// A Maintainer allows the version: no feed entry says so, and the next read is served at once.
	refusing.Store(false)
	if w := s.get(tgz, "Authorization", bearer(s.full)); w.Code != http.StatusOK || w.Body.Len() != 1000 {
		t.Fatalf("after the allow: %d, want the artifact", w.Code)
	}
}
