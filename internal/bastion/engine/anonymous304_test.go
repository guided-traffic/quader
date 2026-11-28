package engine

import (
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// TestR_BAS_094_A304ForTheAnonymousCallerServesTheSharedEntryWhileItIsFresh: the anonymous caller is a partial reader of a public
// remote repository and of a virtual repository over one, because it never fills it (Q-BAS-531). Where its document is the shared
// entry's bytes, the main installation answers the Bastion's conditional request 304; that answer is kept for every anonymous
// caller, so ten anonymous reads cost the first one's check and conditional request, as a kept anonymous copy does, and the entry
// is not renewed on their behalf. A caller with a token whose document is the same bytes is still asked about on every use
// (R-BAS-023). Milliseconds, bodies of a few bytes.
func TestR_BAS_094_A304ForTheAnonymousCallerServesTheSharedEntryWhileItIsFresh(t *testing.T) {
	s := newSiteHarness(t, options{})
	s.site.addReader("", &reader{id: "anon", read: iface.ReadPartial, sameDoc: true})
	same := newToken(t, "pat")
	s.site.addReader(same, &reader{id: "same", read: iface.ReadPartial, sameDoc: true})
	s.site.docs[doc] = "all packages"
	s.get(doc, "Authorization", bearer(s.full))
	rec := s.cache.entryRecords()
	cost := func(what string, want int, f func()) {
		t.Helper()
		before := s.main.count()
		f()
		if got := s.main.requests()[before:]; len(got) != want {
			t.Fatalf("%s: %d requests to the main installation, want %d: %+v", what, len(got), want, got)
		}
	}
	cost("ten anonymous reads of the shared entry's bytes: the check of the first and its conditional request", 2, func() {
		for range 10 {
			if w := s.get(doc); w.Code != http.StatusOK || w.Body.String() != "all packages" {
				t.Fatalf("%d %q", w.Code, w.Body)
			}
		}
	})
	for k, v := range s.cache.entryRecords() {
		if string(rec[k]) != string(v) {
			t.Fatal("the shared entry was renewed on behalf of the anonymous caller")
		}
	}
	cost("three reads of a partial reader with a token: one check, then a conditional request each", 4, func() {
		for range 3 {
			s.get(doc, "Authorization", bearer(same))
		}
	})
}
