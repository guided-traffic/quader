package engine

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
)

// A HEAD of a document is answered from what the Bastion keeps (R-BAS-021, R-BAS-023, Q-BAS-475 to Q-BAS-478): the site double,
// a document of a few bytes, the harness's clock.

// costOf runs f and returns the requests it sent to the main installation, failing unless there are want of them.
func costOf(t *testing.T, h *harness, what string, want int, f func()) []recorded {
	t.Helper()
	before := h.main.count()
	f()
	got := h.main.requests()[before:]
	if len(got) != want {
		t.Fatalf("%s: %d requests to the main installation, want %d: %+v", what, len(got), want, got)
	}
	return got
}

// headOK sends a HEAD and fails unless it is answered 200 without a body, with the document's validator and length.
func headOK(t *testing.T, h *harness, path, etag, body string, hdr ...string) {
	t.Helper()
	w := h.do(http.MethodHead, path, nil, hdr...)
	if w.Code != http.StatusOK || w.Body.Len() != 0 || w.Header().Get("Etag") != etag || w.Header().Get("Content-Length") != strconv.Itoa(len(body)) {
		t.Fatalf("HEAD %s: %d, %d body bytes, %v", path, w.Code, w.Body.Len(), w.Header())
	}
}

// TestR_BAS_021_AHeadMissOfADocumentIsFilledAsAGetAndAnsweredFromIt: a full reader's HEAD of a document is forwarded as a HEAD,
// and the answer's marking (shared) makes the Bastion send the GET that keeps it; the HEAD is answered from the entry with the
// GET's headers and no body. Further HEADs and a GET within metadata_revalidate_after cost nothing; after it a HEAD sends one
// conditional GET, a 304 renews the entry, and a 200 of a moved document replaces it.
func TestR_BAS_021_AHeadMissOfADocumentIsFilledAsAGetAndAnsweredFromIt(t *testing.T) {
	s := newSiteHarness(t, options{})
	s.site.docs[doc] = "version 1"
	auth := []string{"Authorization", bearer(s.full)}

	reqs := costOf(t, s.harness, "a cold HEAD", 2, func() { headOK(t, s.harness, doc, `"d0"`, "version 1", auth...) })
	if reqs[0].Method != http.MethodHead || reqs[1].Method != http.MethodGet || reqs[1].Kind != mainclient.KindForward || reqs[1].Header.Get("Authorization") != bearer(s.full) {
		t.Fatalf("the cold HEAD sent %+v, want the HEAD, then the GET with the caller's credential", reqs)
	}
	if s.cache.entryCount() == 0 || s.cache.objectCount() != 1 {
		t.Fatalf("the document was not kept: %d entries, %d objects", s.cache.entryCount(), s.cache.objectCount())
	}
	costOf(t, s.harness, "three HEADs and a GET within the period", 0, func() {
		for range 3 {
			headOK(t, s.harness, doc, `"d0"`, "version 1", auth...)
		}
		if w := s.get(doc, auth...); w.Code != http.StatusOK || w.Body.String() != "version 1" {
			t.Fatalf("GET: %d %q", w.Code, w.Body)
		}
	})

	s.clock.advance(s.cfg.Bastion.Cache.MetadataRevalidateAfter + time.Second)
	reqs = costOf(t, s.harness, "a HEAD after the period", 1, func() { headOK(t, s.harness, doc, `"d0"`, "version 1", auth...) })
	if reqs[0].Kind != mainclient.KindConditional || reqs[0].Method != http.MethodGet || reqs[0].INM != `"d0"` {
		t.Fatalf("the revalidation %+v, want a conditional GET naming the entry's validator", reqs[0])
	}
	costOf(t, s.harness, "a HEAD after the 304", 0, func() { headOK(t, s.harness, doc, `"d0"`, "version 1", auth...) })

	s.site.mu.Lock()
	s.site.docs[doc] = "version 2"
	s.site.mu.Unlock()
	s.site.bump()
	s.clock.advance(s.cfg.Bastion.Cache.MetadataRevalidateAfter + time.Second)
	costOf(t, s.harness, "a HEAD of the moved document", 1, func() { headOK(t, s.harness, doc, `"d1"`, "version 2", auth...) })
	costOf(t, s.harness, "a GET after it", 0, func() {
		if w := s.get(doc, auth...); w.Body.String() != "version 2" {
			t.Fatalf("GET: %q", w.Body)
		}
	})
}

// TestR_BAS_023_AHeadOfADocumentKeepsTheCopyOfACallerWithoutFull: the GET a HEAD sends for a document the main installation
// filters for the caller keeps the caller's copy (private); a caller with a token validates it with one conditional GET on every
// later HEAD, the anonymous caller's copy answers every anonymous HEAD of the metadata period without a request (R-BAS-094).
func TestR_BAS_023_AHeadOfADocumentKeepsTheCopyOfACallerWithoutFull(t *testing.T) {
	s := newSiteHarness(t, options{})
	s.site.addReader("", &reader{id: "anon", read: iface.ReadPartial})
	s.site.docs[doc] = "all packages"
	auth := []string{"Authorization", bearer(s.part)}

	reqs := costOf(t, s.harness, "a partial reader's cold HEAD", 2, func() { headOK(t, s.harness, doc, `"p0-part"`, "filtered:all packages", auth...) })
	if reqs[0].Method != http.MethodHead || reqs[1].Method != http.MethodGet {
		t.Fatalf("%+v", reqs)
	}
	if s.cache.entryCount() != 0 {
		t.Fatal("a private document reached the cache backend")
	}
	reqs = costOf(t, s.harness, "its next HEAD", 1, func() { headOK(t, s.harness, doc, `"p0-part"`, "filtered:all packages", auth...) })
	if reqs[0].Kind != mainclient.KindConditional || reqs[0].Method != http.MethodGet || reqs[0].INM != `"p0-part"` {
		t.Fatalf("the validation of the copy %+v", reqs[0])
	}

	costOf(t, s.harness, "the anonymous caller's cold HEAD", 2, func() { headOK(t, s.harness, doc, `"p0-anon"`, "filtered:all packages") })
	costOf(t, s.harness, "five anonymous HEADs and a GET within the period", 0, func() {
		for range 5 {
			headOK(t, s.harness, doc, `"p0-anon"`, "filtered:all packages")
		}
		if w := s.get(doc); w.Body.String() != "filtered:all packages" {
			t.Fatalf("GET: %q", w.Body)
		}
	})
}

// TestR_BAS_021_AHeadOfADocumentAboveTheMemoryBoundStaysAHead: a document whose HEAD answer declares a length above the memory
// bound of its class is not fetched for a HEAD (Q-BAS-477): the HEAD's answer is passed on and nothing is kept.
func TestR_BAS_021_AHeadOfADocumentAboveTheMemoryBoundStaysAHead(t *testing.T) {
	s := newSiteHarness(t, options{})
	s.site.docs[doc] = "a document of 28 bytes, long"
	s.e.docMemory = 16
	auth := []string{"Authorization", bearer(s.full)}
	for range 2 {
		reqs := costOf(t, s.harness, "a HEAD", 1, func() { headOK(t, s.harness, doc, `"d0"`, s.site.docs[doc], auth...) })
		if reqs[0].Method != http.MethodHead {
			t.Fatalf("%+v", reqs)
		}
	}
	if s.cache.entryCount() != 0 {
		t.Fatal("a document above the bound was kept for a HEAD")
	}
}

// TestR_BAS_035_TheGetOfAHeadIsAMissAndARefusedOnePassesTheHeadOn: the GET a HEAD sends is a miss of the caller's address; when the
// miss figure refuses it, the caller receives the HEAD's answer the Bastion holds, and nothing is kept (Q-BAS-478).
func TestR_BAS_035_TheGetOfAHeadIsAMissAndARefusedOnePassesTheHeadOn(t *testing.T) {
	g := newGuard()
	g.missLimit = 1
	s := newSiteHarness(t, options{guard: g})
	s.site.docs[doc] = "version 1"
	costOf(t, s.harness, "a HEAD whose GET the miss figure refuses", 1, func() {
		headOK(t, s.harness, doc, `"d0"`, "version 1", "Authorization", bearer(s.full))
	})
	if g.misses != 2 || s.cache.entryCount() != 0 {
		t.Fatalf("%d misses counted, %d entries; want the HEAD's and the GET's miss and nothing kept", g.misses, s.cache.entryCount())
	}
}

// TestR_BAS_096_AHeadIsChargedAsARequestAndItsGetAsAnUpstreamRequest: at the Bastion's place a HEAD answered from the cache is
// charged as one request and nothing upstream, as a cached GET of a document is; the GET of a cold HEAD is a second upstream
// request. With one upstream request an hour the cold HEAD is answered from the HEAD's own answer and its GET is not sent.
func TestR_BAS_096_AHeadIsChargedAsARequestAndItsGetAsAnUpstreamRequest(t *testing.T) {
	const path = "/api/npm/web/npm-all/p"
	t.Run("charged", func(t *testing.T) {
		s, use := budgetSite(t, iface.Figures{}, iface.ModeEnforce)
		costOf(t, s.harness, "a cold HEAD and three warm ones", 2, func() {
			for range 4 {
				headOK(t, s.harness, path, `"d0"`, `{"name":"p"}`)
			}
		})
		row := anonymousRow(t, use)
		if row.Requests != 4 || row.UpstreamRequests != 2 || row.CachedBytes != 0 || row.UncachedBytes != 0 {
			t.Fatalf("the usage %+v", row)
		}
	})
	t.Run("one upstream request an hour", func(t *testing.T) {
		s, _ := budgetSite(t, iface.Figures{UpstreamRequestsPerHour: limitOf(1)}, iface.ModeEnforce)
		costOf(t, s.harness, "a cold HEAD", 1, func() { headOK(t, s.harness, path, `"d0"`, `{"name":"p"}`) })
		if _, ok := s.e.loadEntry(t.Context(), path); ok || s.cache.entryCount() != 0 {
			t.Fatal("the GET the budget refused kept the document")
		}
	})
}

func anonymousRow(t *testing.T, use interface{ Drain() iface.UsageReport }) iface.UsageRow {
	t.Helper()
	for _, r := range use.Drain().Rows {
		if r.Subject == "anonymous:203.0.113.7" {
			return r
		}
	}
	t.Fatal("no usage row of the anonymous caller")
	return iface.UsageRow{}
}
