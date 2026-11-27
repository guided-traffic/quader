package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/bastion/shared"
)

// The engine with the real guard at small figures (the figures of R-BAS-035's criteria: anonymous 10 a second with a burst of 40,
// a block after 300 refusals a minute) and the harness's clock, which does not move: no token comes back while a test runs, and
// nothing waits. Microseconds per request, a few hundred requests without a body.

// countingHarness is a harness whose engine asks a guard.New over the harness's shared state and clock.
func countingHarness(t *testing.T) *harness {
	t.Helper()
	clk := &testClock{t: epoch}
	m, err := metrics.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	st := shared.NewLocal(1, clk.now)
	c := settings.Builtin()
	c.Limits.Flood = iface.Flood{
		Anonymous:      iface.FloodClass{RequestsPerSecond: 10, Burst: 40, MissesPerMinute: 30},
		Authenticated:  iface.FloodClass{RequestsPerSecond: 50, Burst: 200, MissesPerMinute: 300},
		GUI:            iface.FloodRate{RequestsPerSecond: 5, Burst: 20},
		FailedAuth:     iface.FailedAuth{MaxFailures: 20, WindowSeconds: 600, TimeoutSeconds: 900},
		Blocking:       iface.Blocking{Threshold: 300, WindowSeconds: 60, DurationSeconds: 900, MaxDurationSeconds: 86400},
		ExemptNetworks: []string{},
	}
	a, err := settings.FromConfiguration(c, settings.SourceMain)
	if err != nil {
		t.Fatal(err)
	}
	g, err := guard.New(guard.Options{
		Figures: func(context.Context) *settings.Applied { return a }, State: st, Metrics: m, Now: clk.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return newHarness(t, options{clock: clk, m: m, state: st, realGuard: g})
}

// from sends a request through the engine from the client address addr.
func (h *harness) from(addr, method, path string) *httptest.ResponseRecorder {
	h.t.Helper()
	r := httptest.NewRequest(method, "https://packages.example.com"+path, nil)
	r.RemoteAddr = addr + ":4711"
	w := httptest.NewRecorder()
	h.e.ServeHTTP(w, r)
	return w
}

// TestR_BAS_035_R_BAS_037_APathThatIsNoAddressCountsAgainstItsAddress: the paths of the scan of 2026-11-27 (no address of the
// Bastion, a method the engine refuses, a token form that is too large) take a token of their address's request limit like every
// other request (Q-BAS-525): the burst of 40 is answered as before, the 41st is 429 rate_limited with Retry-After whatever the path,
// and the refusals past the threshold of 300 block the address, which then answers 429 address_blocked first. Another address is
// not touched.
func TestR_BAS_035_R_BAS_037_APathThatIsNoAddressCountsAgainstItsAddress(t *testing.T) {
	h := countingHarness(t)
	const scanner, neighbour = "198.51.100.9", "198.51.100.10"
	scan := []struct{ method, path string }{
		{http.MethodGet, "/wp-admin/admin-ajax.php"},
		{http.MethodGet, "/350.htm"},
		{http.MethodHead, "/.env"},
		{http.MethodPost, "/xmlrpc.php"},
		{http.MethodGet, "/api/v1/system/ping"},
	}
	for i := range 40 {
		s := scan[i%len(scan)]
		if rec := h.from(scanner, s.method, s.path); rec.Code != http.StatusNotFound {
			t.Fatalf("request %d %s %s within the burst = %d %s", i+1, s.method, s.path, rec.Code, rec.Body.String())
		}
	}
	for i := range 301 {
		s := scan[i%len(scan)]
		rec := h.from(scanner, s.method, s.path)
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" ||
			(s.method != http.MethodHead && !strings.Contains(rec.Body.String(), iface.CodeRateLimited)) {
			t.Fatalf("refusal %d %s %s = %d %v %s", i+1, s.method, s.path, rec.Code, rec.Header(), rec.Body.String())
		}
	}
	// The 301st refusal crossed the threshold of 300: the address is blocked, for every path.
	for _, p := range []string{"/.git/config", "/api/npm/web/npm-all/lodash"} {
		if rec := h.from(scanner, http.MethodGet, p); rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), iface.CodeAddressBlocked) {
			t.Fatalf("blocked %s = %d %s", p, rec.Code, rec.Body.String())
		}
	}
	if rec := h.from(neighbour, http.MethodGet, "/wp-admin/admin-ajax.php"); rec.Code != http.StatusNotFound {
		t.Errorf("another address = %d %s", rec.Code, rec.Body.String())
	}
	// A request with a token form that is too large is counted too: the neighbour's burst ends 39 requests later.
	for range 38 {
		h.from(neighbour, http.MethodGet, "/robots.php")
	}
	r := httptest.NewRequest(http.MethodPost, "https://packages.example.com/v2/_token", strings.NewReader("x"))
	r.RemoteAddr, r.ContentLength = neighbour+":4711", 1<<20
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.e.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("the 40th request, a token form too large = %d %s", w.Code, w.Body.String())
	}
	if rec := h.from(neighbour, http.MethodGet, "/api/npm/web/npm-all/lodash"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("the neighbour's 41st request = %d %s", rec.Code, rec.Body.String())
	}
	if n := counterValue(t, h, "refused", metrics.RefusedNotServed); n != 40+39 {
		t.Errorf("refused not_served = %v, want the scanner's 40 and the neighbour's 39", n)
	}
	if n := h.main.count(); n != 0 {
		t.Errorf("the main installation received %d requests", n)
	}
}
