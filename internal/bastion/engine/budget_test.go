package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/budget"
	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/bastion/shared"
	"github.com/guided-traffic/quader/internal/bastion/usage"
)

// The budgets of the Bastion's place in the request flow (R-BAS-096, R-LIM-044): the site double, the local shared state and the
// harness's clock; an artifact of 64 bytes and a document of a few bytes.

// budgetSite is a site harness whose requests are charged at a place whose every rule has the figures f, in the mode.
func budgetSite(t *testing.T, f iface.Figures, mode iface.LimitsMode) (*siteHarness, *usage.Buffer) {
	t.Helper()
	clk := &testClock{t: epoch}
	st := shared.NewLocal(1, clk.now)
	m, err := metrics.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	use := usage.New(0)
	b := budget.New(budget.Options{Limiter: st.Limiter, Counters: st.Budget, Usage: use, Metrics: m, MaxInFlight: 1024, Now: clk.now})
	s := newSiteHarness(t, options{clock: clk, state: st, m: m, budgets: b})
	c := settings.Builtin()
	c.Revision = 2
	c.Limits.Mode = mode
	for id := range c.Limits.Place.Rules {
		c.Limits.Place.Rules[id] = f
	}
	if _, err := s.link.Configuration().Apply(context.Background(), c, nil); err != nil {
		t.Fatal(err)
	}
	s.site.artifacts["/api/npm/web/npm-all/p.tgz"] = payload(64)
	s.site.docs["/api/npm/web/npm-all/p"] = `{"name":"p"}`
	return s, use
}

func limitOf(n int64) *int64 { return &n }

// TestR_BAS_096_ASubjectOverItsCachedBytesIsRefusedAHitBeforeItStartsAndServedADocument: at 100 cached bytes an hour an anonymous
// caller's fill and two hits of 64 bytes are served, the third hit is refused 429 download_limit_reached before its first byte with
// the figure, the limit and the wait, and a document is still served.
func TestR_BAS_096_ASubjectOverItsCachedBytesIsRefusedAHitBeforeItStartsAndServedADocument(t *testing.T) {
	s, use := budgetSite(t, iface.Figures{CachedBytesPerHour: limitOf(100)}, iface.ModeEnforce)
	var codes []int
	var refused *http.Response
	var body []byte
	for range 4 {
		w := s.get("/api/npm/web/npm-all/p.tgz")
		codes = append(codes, w.Code)
		if w.Code != http.StatusOK {
			refused, body = w.Result(), w.Body.Bytes()
		}
	}
	if want := []int{200, 200, 200, 429}; len(codes) != 4 || codes[0] != want[0] || codes[1] != want[1] || codes[2] != want[2] || codes[3] != want[3] {
		t.Fatalf("answers %v, want %v", codes, want)
	}
	// npm's shape carries the code and the detail, which names the figure's limit and the true wait (R-BAS-095, R-LIM-020).
	var p struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &p); err != nil || p.Code != budget.CodeDownloadLimitReached || !strings.Contains(p.Error, "100 bytes per hour") {
		t.Fatalf("the refusal %s (%v)", body, err)
	}
	if refused.Header.Get("Retry-After") == "" || refused.Header.Get("RateLimit-Policy") == "" {
		t.Fatalf("the refusal's headers %v", refused.Header)
	}
	if w := s.get("/api/npm/web/npm-all/p"); w.Code != http.StatusOK {
		t.Fatalf("a document past the budget: %d %s", w.Code, w.Body)
	}
	rep := use.Drain()
	var row iface.UsageRow
	for _, r := range rep.Rows {
		if r.Subject == "anonymous:203.0.113.7" {
			row = r
		}
	}
	if row.Requests != 5 || row.CachedBytes != 128 || row.UncachedBytes < 64 || row.UpstreamRequests < 1 || row.Limited["cachedBytesPerHour"] != 1 || row.Repository != "web/npm-all" {
		t.Fatalf("the usage %+v", rep.Rows)
	}
}

// TestR_LIM_044_ABastionInAuditServesPastTheBudgetAndStillHoldsItsFloodProtection: in Audit the same hits are all served and one
// would_limit is reported, while an address the flood protection refuses is refused in either mode.
func TestR_LIM_044_ABastionInAuditServesPastTheBudgetAndStillHoldsItsFloodProtection(t *testing.T) {
	s, use := budgetSite(t, iface.Figures{CachedBytesPerHour: limitOf(100)}, iface.ModeAudit)
	for i := range 4 {
		if w := s.get("/api/npm/web/npm-all/p.tgz"); w.Code != http.StatusOK {
			t.Fatalf("request %d in Audit: %d %s", i, w.Code, w.Body)
		}
	}
	rep := use.Drain()
	if len(rep.Rows) != 1 || rep.Rows[0].WouldLimit["cachedBytesPerHour"] != 1 || len(rep.Rows[0].Limited) != 0 {
		t.Fatalf("the usage in Audit %+v", rep.Rows)
	}
	s.guard.mu.Lock()
	s.guard.request = guard.Verdict{Status: 429, Code: iface.CodeRateLimited, RetryAfter: 1e9, Reason: metrics.RefusedRateLimited}
	s.guard.mu.Unlock()
	if w := s.get("/api/npm/web/npm-all/p.tgz"); w.Code != http.StatusTooManyRequests || problemCode(w) != iface.CodeRateLimited {
		t.Fatalf("an address past the flood figure in Audit: %d %s", w.Code, w.Body)
	}
}

// TestR_BAS_096_ATokenWhoseCallerIsLearnedFromTheAnswerIsCountedThenAndChargedByItsKey: a caller with a token the Bastion holds no
// answer for is counted once the main installation's answer names it, under the users' kind rule and its principal (Q-BAS-470,
// Q-BAS-471), and its next request is charged before it is served.
func TestR_BAS_096_ATokenWhoseCallerIsLearnedFromTheAnswerIsCountedThenAndChargedByItsKey(t *testing.T) {
	s, use := budgetSite(t, iface.Figures{RequestsPerSecond: limitOf(1), RequestsBurst: limitOf(1)}, iface.ModeEnforce)
	first := s.get("/api/npm/web/npm-all/p", "Authorization", bearer(s.full))
	second := s.get("/api/npm/web/npm-all/p", "Authorization", bearer(s.full))
	if first.Code != http.StatusOK || second.Code != http.StatusTooManyRequests {
		t.Fatalf("answers %d and %d, want 200 and 429", first.Code, second.Code)
	}
	rep := use.Drain()
	if len(rep.Rows) != 1 || rep.Rows[0].Subject != "user:p-full" || rep.Rows[0].Credential == "" || rep.Rows[0].Requests != 2 {
		t.Fatalf("the usage %+v", rep.Rows)
	}
}
