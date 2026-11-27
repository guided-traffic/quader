package gui

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/bastion/shared"
)

// firstLoad is what a browser's cold load of a package's page asked of the profile's bundle on 2026-11-27 (H-2448, measured with
// headless Chromium against the built bundle and this package's test reader): the page, its stylesheet, five scripts the document
// names, two fonts, 15 lazy chunks and five data calls, 28 requests; a start page took 17, a project's page 22. A headed browser adds
// its favicon. The addresses here are the same kinds in the test bundle; what counts is the number and the class.
func firstLoad() []string {
	out := []string{
		"/ui/en/projects/acme/packages/npm/@acme/ui", "/ui/en/styles-ABCD1234.css", "/ui/en/polyfills-ABCD1234.js",
		"/ui/en/main-ABCD1234.js", "/ui/en/media/inter-latin-wght-normal-ABCD1234.woff2", "/ui/en/media/material-icons-ABCD1234.woff2",
		"/ui/en/favicon.svg", "/ui/data/v1/legal?locale=en", "/ui/data/v1/state", "/ui/data/v1/projects",
		"/ui/data/v1/packages/lookup?format=npm&name=@acme/ui", "/ui/data/v1/packages/" + pkgID + "/versions?limit=10",
	}
	for i := len(out); i < 29; i++ {
		out = append(out, "/ui/en/chunk-"+string(rune('A'+i))+"BCD1234.js")
	}
	return out
}

// TestR_BAS_035_APageFloodTakesThePageLimitAndABrowsersFirstLoadStaysInsideIt: with the real guard at the built-in class gui (20 a
// second, a burst of 100) and a small anonymous class (10 a second, a burst of 40), on a clock that does not move: a browser's first
// load of 29 requests is admitted whole; a page flood is refused at the 101st request with 429 rate_limited and the page of R-BAS-123,
// and leaves the address's anonymous request class untouched; robots.txt takes the anonymous class and leaves the pages alone
// (Q-BAS-525, Q-BAS-526). Microseconds per request, a few hundred requests of bytes.
func TestR_BAS_035_APageFloodTakesThePageLimitAndABrowsersFirstLoadStaysInsideIt(t *testing.T) {
	at := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return at }
	c := settings.Builtin()
	c.Limits.Flood.Anonymous = iface.FloodClass{RequestsPerSecond: 10, Burst: 40, MissesPerMinute: 30}
	a, err := settings.FromConfiguration(c, settings.SourceMain)
	if err != nil {
		t.Fatal(err)
	}
	m, err := metrics.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	g, err := guard.New(guard.Options{Figures: func(context.Context) *settings.Applied { return a }, State: shared.NewLocal(1, now), Metrics: m, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	h := newGUI(t, newReader(), func(o *Options) {
		o.Guard, o.Flood = g, func(context.Context) settings.Flood { return a.Flood }
	})
	const visitor = "192.0.2.1" // httptest.NewRequest's address

	for _, p := range firstLoad() {
		if rec := do(h, http.MethodGet, p); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("the first load's %s was refused", p)
		}
	}
	// The same visitor goes on reading: 71 more requests are the burst's rest, the 101st is refused as a page.
	for i := range 71 {
		if rec := do(h, http.MethodGet, "/ui/data/v1/state"); rec.Code != http.StatusOK {
			t.Fatalf("page request %d = %d", 30+i, rec.Code)
		}
	}
	rec := do(h, http.MethodGet, "/ui/en/projects/acme")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("RateLimit-Policy") != `"pages";q=100;w=5` ||
		rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("the 101st page request = %d %v", rec.Code, rec.Header())
	}
	// The pages took nothing of the anonymous class: its whole burst of 40 is left, and robots.txt takes from it, not from the pages.
	for i := range 39 {
		if v := g.Request(context.Background(), visitor, false); !v.Allowed {
			t.Fatalf("anonymous request %d after the page flood was refused", i+1)
		}
	}
	if rec := do(h, http.MethodGet, "/robots.txt"); rec.Code != http.StatusOK {
		t.Fatalf("robots.txt as the 40th anonymous request = %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/robots.txt"); rec.Code != http.StatusTooManyRequests || rec.Header().Get("RateLimit-Policy") != `"requests";q=40;w=4` {
		t.Fatalf("robots.txt as the 41st anonymous request = %d %v", rec.Code, rec.Header())
	}
}
