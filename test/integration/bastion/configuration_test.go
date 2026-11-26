//go:build integration

package bastion

import (
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_BAS_093_AChangeOfTheRegistrationIsAppliedByTheBastion: a change of the registration made through the API raises its
// revision, which the next handshake brings in the member configuration, and the in-process Bastion applies it without a
// restart (R-BAS-093): quader_bastion_configuration_revision follows and the source is the main installation. The change is the
// switch handover and an anonymous flood figure of the registration's configuration (E8); it travels with the feed's settings entry or
// at the latest with the regular handshake (5 s in this harness). About 3 to 6 s, no content.
func TestR_BAS_093_AChangeOfTheRegistrationIsAppliedByTheBastion(t *testing.T) {
	w := newWorld(t, worldOptions{})
	b := w.newBastion(t, bastionOptions{}).wait()
	r := b.reps[0]
	text := r.metrics(t)
	if metricValue(t, text, `quader_bastion_configuration_source{source="main"}`) != 1 {
		t.Fatalf("a connected Bastion does not apply the main installation's configuration:\n%s", grepLines(text, "quader_bastion_configuration"))
	}
	before := metricValue(t, text, "quader_bastion_configuration_revision")
	w.mustAPI("PATCH", "/api/v1/bastions/partners", http.StatusOK, map[string]any{
		"switches": map[string]any{"handover": true}, "limits": map[string]any{"flood": map[string]any{"anonymous": map[string]any{"requestsPerSecond": 51}}},
	},
		"Content-Type", "application/merge-patch+json", "If-Match", "*")
	start := time.Now()
	testenv.WaitFor(t, "the new revision applied", 15*time.Second, func() bool {
		return metricValue(t, r.metrics(t), "quader_bastion_configuration_revision") > before
	})
	t.Logf("the change was applied %s after the API's answer", time.Since(start).Round(100*time.Millisecond))
	if metricValue(t, r.metrics(t), "quader_bastion_handover_enabled") != 1 {
		t.Fatal("the switch of the new configuration is not applied")
	}
}

// TestR_BAS_094_TheCeilingRefusesAboveItsFigureWhileHitsAreServed: a Bastion whose configuration holds an anonymous ceiling of
// two requests an hour sends the anonymous callers' first two misses to the main installation and answers the next ones
// 503 busy with Retry-After without a request there, while it serves the anonymous hit it holds and a signed-in miss
// (R-BAS-094). About 3 s, three npm tarballs and a document of bytes.
func TestR_BAS_094_TheCeilingRefusesAboveItsFigureWhileHitsAreServed(t *testing.T) {
	w := newWorld(t, worldOptions{})
	w.content()
	w.setAnonymous(true)
	perHour := int64(2)
	b := w.newBastion(t, bastionOptions{Configure: then(relaxed, func(c *iface.Configuration) {
		c.Limits.AnonymousCeiling = iface.Ceiling{RequestsPerHour: &perHour}
	})}).wait()
	r := b.reps[0]
	first := tarballPath(npmAll, "@acme/sdk", "1.0.0")
	m := w.front.Mark()
	served, busy := 0, 0
	for _, p := range []string{first, tarballPath(npmAll, "@acme/sdk", "2.0.0"), tarballPath(npmAll, "plain-lib", "1.0.0"), "/api/npm/" + npmAll + "/plain-lib"} {
		got := r.do(t, "GET", p, callOpts{addr: "198.51.100.150"})
		switch {
		case got.Status == http.StatusOK:
			served++
		case got.Status == http.StatusServiceUnavailable && got.Code() == "busy" && got.Header.Get("Retry-After") != "":
			busy++
		default:
			t.Fatalf("%s: %d %s %v", p, got.Status, got.Body, got.Header)
		}
	}
	if cost := w.front.Cost(m); cost > 2 || busy == 0 || served+busy != 4 {
		t.Fatalf("%d served, %d refused busy, %d requests at the main installation; want at most 2 there and the rest refused", served, busy, cost)
	}
	m = w.front.Mark()
	if got := r.do(t, "GET", first, callOpts{addr: "198.51.100.151"}); got.Status != http.StatusOK || w.front.Cost(m) != 0 {
		t.Fatalf("an anonymous hit above the ceiling: %d, %d requests at the main installation", got.Status, w.front.Cost(m))
	}
	if got := r.do(t, "GET", "/api/npm/"+npmAll+"/"+escapeNPM("@acme/sdk"), callOpts{token: w.full.token, addr: "198.51.100.152"}); got.Status != http.StatusOK {
		t.Fatalf("a signed-in miss above the anonymous ceiling: %d %s", got.Status, got.Body)
	}
	if sumMetric(r.metrics(t), "quader_bastion_refused_total", `reason="anonymous_ceiling"`) != float64(busy) {
		t.Fatal("quader_bastion_refused_total{reason=\"anonymous_ceiling\"} does not count the refusals")
	}
}
