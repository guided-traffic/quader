//go:build integration

package bastion

import (
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_BAS_133_ADisableReachesTheBastionWithinSeconds: a hard disable through the API reaches the in-process Bastion over its
// connection: within seconds it answers a package request 503 with Retry-After, its status confirms the disable, and the
// registration reads disabled; null enables it, and it serves again (R-BAS-133). About 6 s, one npm tarball of bytes.
func TestR_BAS_133_ADisableReachesTheBastionWithinSeconds(t *testing.T) {
	w := newWorld(t, worldOptions{})
	w.content()
	b := w.newBastion(t, bastionOptions{}).wait()
	r := b.reps[0]
	path := tarballPath(npmAll, "@acme/sdk", "1.0.0")
	if got := r.do(t, "GET", path, callOpts{token: w.full.token}); got.Status != http.StatusOK {
		t.Fatalf("before the disable: %d %s", got.Status, got.Body)
	}
	patch := func(v any) {
		w.mustAPI("PATCH", "/api/v1/bastions/partners", http.StatusOK, map[string]any{"disabled": v},
			"Content-Type", "application/merge-patch+json", "If-Match", "*")
	}
	patch("hard")
	start := time.Now()
	testenv.WaitFor(t, "the Bastion answering 503", 15*time.Second, func() bool {
		got := r.do(t, "GET", path, callOpts{token: w.full.token})
		return got.Status == http.StatusServiceUnavailable && got.Header.Get("Retry-After") != ""
	})
	t.Logf("the disable reached the Bastion %s after the API's answer", time.Since(start).Round(100*time.Millisecond))
	if metricValue(t, r.metrics(t), `quader_bastion_disabled{mode="hard"}`) != 1 {
		t.Fatal("quader_bastion_disabled{mode=\"hard\"} is not 1")
	}
	testenv.WaitFor(t, "the registration's state disabled", 15*time.Second, func() bool {
		return w.api("GET", "/api/v1/bastions/partners", w.admin, nil).JSON(t)["state"] == "disabled"
	})
	patch(nil)
	testenv.WaitFor(t, "the Bastion serving again", 15*time.Second, func() bool {
		return r.do(t, "GET", path, callOpts{token: w.full.token}).Status == http.StatusOK
	})
}
