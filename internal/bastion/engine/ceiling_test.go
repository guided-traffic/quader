package engine

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/bastion/settings"
)

// The anonymous ceiling at the engine (bastion.md R-BAS-094, Q-BAS-445): the real guard.Ceiling over the harness's shared
// state and clock, taken in the slot budget of the double of the main installation as the real client takes it. Payloads are
// bytes.

// countingCeiling counts the requests that asked the ceiling.
type countingCeiling struct {
	mainclient.Ceiling
	asked atomic.Int64
}

func (c *countingCeiling) Take(ctx context.Context) (bool, time.Duration) {
	c.asked.Add(1)
	return c.Ceiling.Take(ctx)
}

// withCeiling gives the double of the main installation a budget whose anonymous class takes from a ceiling of perSecond with
// burst, no hour, over the harness's shared state.
func withCeiling(t *testing.T, s *siteHarness, perSecond, burst int64) *countingCeiling {
	t.Helper()
	c := settings.Builtin()
	c.Limits.AnonymousCeiling.RequestsPerSecond, c.Limits.AnonymousCeiling.Burst, c.Limits.AnonymousCeiling.RequestsPerHour = &perSecond, &burst, nil
	a, err := settings.FromConfiguration(c, settings.SourceMain)
	if err != nil {
		t.Fatal(err)
	}
	cc := &countingCeiling{Ceiling: guard.NewCeiling(s.state, func(context.Context) *settings.Applied { return a })}
	s.main.budget = mainclient.NewBudget(mainclient.BudgetOptions{
		MaxConcurrent: 64, Replicas: 1, AnonymousShare: 0.25, QueueTimeout: 20 * time.Millisecond, Ceiling: cc, Metrics: s.m,
	})
	return cc
}

// TestR_BAS_094_AnAnonymousMissAboveTheCeilingIsBusyWhileHitsAndSignedInMissesAreServed: at a ceiling of 10 a second with a
// burst of 20, the anonymous callers' 21st request to the main installation within the burst (the forward of a 21st miss; the
// marking of each forward answered their shape, so no check was asked) answers 503 busy with Retry-After, while a hit and a
// signed-in miss are served; a refused miss takes nothing, so 100 ms later exactly one more anonymous miss passes.
func TestR_BAS_094_AnAnonymousMissAboveTheCeilingIsBusyWhileHitsAndSignedInMissesAreServed(t *testing.T) {
	s := newSiteHarness(t, options{})
	withCeiling(t, s, 10, 20)
	for i := range 40 {
		s.site.artifacts[fmt.Sprintf("/api/npm/web/npm-all/p%d.tgz", i)] = payload(64)
	}
	codes := map[int]int{}
	var busy *http.Response
	for i := range 25 {
		w := s.get(fmt.Sprintf("/api/npm/web/npm-all/p%d.tgz", i))
		codes[w.Code]++
		if w.Code == http.StatusServiceUnavailable && busy == nil {
			busy = w.Result()
			if problemCode(w) != "busy" {
				t.Fatalf("a refusal of the ceiling: %d %s", w.Code, w.Body)
			}
		}
	}
	anonymous := 0
	for _, r := range s.main.requests() {
		if r.Class == mainclient.Anonymous {
			anonymous++
		}
	}
	if codes[http.StatusOK] != 20 || codes[http.StatusServiceUnavailable] != 5 || anonymous != 20 {
		t.Fatalf("answers %v and %d anonymous requests at the main installation, want 20 served, 5 refused and 20", codes, anonymous)
	}
	if busy.Header.Get("Retry-After") == "" || busy.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("the refusal's headers: %v", busy.Header)
	}
	if got := counterValue(t, s.harness, "refused", metrics.RefusedCeiling); got != 5 {
		t.Fatalf("quader_bastion_refused_total{reason=anonymous_ceiling} = %v, want 5", got)
	}
	before := len(s.main.requests())
	if w := s.get("/api/npm/web/npm-all/p0.tgz"); w.Code != http.StatusOK || len(s.main.requests()) != before {
		t.Fatalf("a hit above the ceiling: %d, %d requests at the main installation", w.Code, len(s.main.requests())-before)
	}
	if w := s.get("/api/npm/web/npm-all/p30.tgz", "Authorization", bearer(s.full)); w.Code != http.StatusOK {
		t.Fatalf("a signed-in miss above the anonymous ceiling: %d %s", w.Code, w.Body)
	}
	s.clock.advance(100 * time.Millisecond)
	if w := s.get("/api/npm/web/npm-all/p31.tgz"); w.Code != http.StatusOK {
		t.Fatalf("the anonymous miss the ceiling refilled 100 ms later: %d %s", w.Code, w.Body)
	}
	if w := s.get("/api/npm/web/npm-all/p32.tgz"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("a second anonymous miss in the same 100 ms: %d", w.Code)
	}
}

// TestR_BAS_094_TheBackgroundRefreshOfAnAnonymousAnswerTakesFromTheCeiling: the refresh of the anonymous callers' answer in
// the last fifth of its period (R-BAS-020) is a request to the main installation on their behalf, which no guard counted
// before: it asks the ceiling, in the anonymous class.
func TestR_BAS_094_TheBackgroundRefreshOfAnAnonymousAnswerTakesFromTheCeiling(t *testing.T) {
	s := newSiteHarness(t, options{cfg: []func(*configT){func(c *configT) { c.Bastion.Cache.ArtifactRevalidateAfter = time.Hour }}})
	cc := withCeiling(t, s, 1000, 1000)
	s.site.artifacts[tgz] = payload(64)
	if w := s.get(tgz); w.Code != http.StatusOK {
		t.Fatalf("the anonymous fill: %d", w.Code)
	}
	asked := cc.asked.Load()
	s.clock.advance(s.cfg.Bastion.Answers.Period/5*4 + time.Second)
	before := len(s.main.requests())
	if w := s.get(tgz); w.Code != http.StatusOK {
		t.Fatalf("the anonymous hit: %d", w.Code)
	}
	waitFor(t, "the refresh", func() bool { return len(s.main.requests()) > before })
	s.e.wg.Wait()
	refresh := s.main.requests()[before]
	if refresh.Kind != mainclient.KindCheck || refresh.Class != mainclient.Anonymous {
		t.Fatalf("the refresh: %+v", refresh)
	}
	if got := cc.asked.Load() - asked; got != 1 {
		t.Fatalf("the refresh asked the ceiling %d times, want 1", got)
	}
}
