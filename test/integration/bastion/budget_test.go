//go:build integration

package bastion

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_BAS_096_ASubjectOverItsBudgetIsRefusedInItsFormatsShapeAndItsUsageAndStateReachTheMainInstallation: at a place whose rules
// hold two requests an hour, an anonymous caller's third request of an npm document is refused 429 rate_limited in npm's shape by
// the Bastion while another address is served; the usage the Bastion counted reaches the main installation's usage API at the
// Bastion's place, and the replica's state its registration (R-BAS-096, R-LIM-045). One npm document of bytes; the main
// installation writes its usage buffer every second.
func TestR_BAS_096_ASubjectOverItsBudgetIsRefusedInItsFormatsShapeAndItsUsageAndStateReachTheMainInstallation(t *testing.T) {
	w := newWorld(t, worldOptions{Env: []string{"QUADER_LIMITS_USAGE_FLUSH_INTERVAL=1s"}})
	w.content()
	w.setAnonymous(true)
	two := int64(2)
	b := w.newBastion(t, bastionOptions{Configure: then(relaxed, func(c *iface.Configuration) {
		for id, f := range c.Limits.Place.Rules {
			f.RequestsPerHour = &two
			c.Limits.Place.Rules[id] = f
		}
	})}).wait()
	r := b.reps[0]
	doc := "/api/npm/" + npmAll + "/plain-lib"
	var codes []int
	var refused reply
	for range 3 {
		got := r.do(t, "GET", doc, callOpts{addr: "198.51.100.161"})
		codes = append(codes, got.Status)
		if got.Status != http.StatusOK {
			refused = got
		}
	}
	if codes[0] != 200 || codes[1] != 200 || codes[2] != http.StatusTooManyRequests {
		t.Fatalf("answers %v, want 200, 200, 429", codes)
	}
	if refused.Code() != "rate_limited" || !strings.Contains(string(refused.Body), "2 requests per hour") || refused.Header.Get("Retry-After") == "" {
		t.Fatalf("the refusal: %s %v", refused.Body, refused.Header)
	}
	if got := r.do(t, "GET", doc, callOpts{addr: "198.51.100.162"}); got.Status != http.StatusOK {
		t.Fatalf("another address: %d %s", got.Status, got.Body)
	}

	testenv.WaitFor(t, "the Bastion's usage at its place", 30*time.Second, func() bool {
		a := w.mustAPI("GET", "/api/v1/usage?place=bastion:partners", http.StatusOK, nil)
		var u struct {
			Items []struct {
				Subject  map[string]any `json:"subject"`
				Requests int64          `json:"requests"`
			} `json:"items"`
		}
		if json.Unmarshal(a.Body, &u) != nil {
			return false
		}
		for _, it := range u.Items {
			if it.Subject["id"] == "198.51.100.161" && it.Requests == 3 {
				return true
			}
		}
		return false
	})
	a := w.mustAPI("GET", "/api/v1/bastions/partners", http.StatusOK, nil)
	var reg struct {
		Status []struct {
			Replica string `json:"replica"`
		} `json:"status"`
	}
	if err := json.Unmarshal(a.Body, &reg); err != nil || len(reg.Status) == 0 {
		t.Fatalf("the registration's status: %s (%v)", a.Body, err)
	}
}
