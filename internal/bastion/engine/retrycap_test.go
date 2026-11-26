package engine

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// TestR_BAS_058_TheBastionsOwnRefusalsKeepTheFormatsLongestWait: a Bastion's own refusals (its block list, its limits, its
// failed-authentication timeout) tell a client of NuGet or Ansible Galaxy to wait at most the 30 s the format declares, in
// Retry-After and in the RateLimit field's t, and name the true wait in the detail; npm and OCI, which declare no cap, are told
// the true wait (R-BAS-029, R-BAS-058, Q-BAS-310).
func TestR_BAS_058_TheBastionsOwnRefusalsKeepTheFormatsLongestWait(t *testing.T) {
	s := newSiteHarness(t, options{})
	paths := map[string]struct {
		path string
		want string
	}{
		"nuget":   {"/api/nuget/web/nuget-all/v3/index.json", "30"},
		"ansible": {"/api/ansible/web/galaxy/api/v3/collections/acme/tools/", "30"},
		"npm":     {"/api/npm/web/npm-all/lodash", "900"},
		"oci":     {"/v2/web/docker-hub/library/alpine/manifests/3", "900"},
	}
	for _, c := range []struct {
		name string
		set  func(guard.Verdict)
		v    guard.Verdict
	}{
		{"block list", func(v guard.Verdict) { s.guard.blocked = v }, guard.Verdict{Status: 429, Code: iface.CodeAddressBlocked, RetryAfter: 15 * time.Minute, Reason: metrics.RefusedBlocked}},
		{"request limit", func(v guard.Verdict) { s.guard.request = v }, guard.Verdict{Status: 429, Code: iface.CodeRateLimited, RetryAfter: 15 * time.Minute, Reason: metrics.RefusedRateLimited}},
		{"failed-authentication timeout", func(v guard.Verdict) { s.guard.timeout = v }, guard.Verdict{Status: 429, Code: iface.CodeAuthTimeout, RetryAfter: 15 * time.Minute, Reason: metrics.RefusedAuthTimeout}},
	} {
		c.set(c.v)
		for format, p := range paths {
			w := s.get(p.path)
			if c.v.Code == iface.CodeAuthTimeout {
				w = s.get(p.path, "Authorization", bearer(s.part))
			}
			if w.Code != http.StatusTooManyRequests {
				t.Errorf("%s on %s: %d %s", c.name, format, w.Code, w.Body)
				continue
			}
			if got := w.Header().Get("Retry-After"); got != p.want {
				t.Errorf("%s on %s: Retry-After %q, want %q", c.name, format, got, p.want)
			}
			if got := w.Header().Get("RateLimit"); !strings.HasSuffix(got, ";t="+p.want) {
				t.Errorf("%s on %s: RateLimit %q, want t=%s", c.name, format, got, p.want)
			}
			if !strings.Contains(w.Body.String(), "15 minutes") {
				t.Errorf("%s on %s: the detail does not name the true wait: %s", c.name, format, w.Body)
			}
		}
		c.set(guard.Allow())
	}
}

// TestR_BAS_029_ARefusalNamesOneWait: the wait of a refusal of the guard is rounded up once, to seconds below two minutes and to
// minutes from there, and the detail names that wait (usage-and-limits.md Q-LIM-291's rule at the Bastion).
func TestR_BAS_029_ARefusalNamesOneWait(t *testing.T) {
	for _, c := range []struct {
		in    time.Duration
		wait  time.Duration
		words string
	}{
		{0, time.Second, "1 second"},
		{1500 * time.Millisecond, 2 * time.Second, "2 seconds"},
		{119 * time.Second, 119 * time.Second, "119 seconds"},
		{2*time.Minute + time.Second, 3 * time.Minute, "3 minutes"},
		{15*time.Minute - 100*time.Millisecond, 15 * time.Minute, "15 minutes"},
		{2 * time.Hour, 2 * time.Hour, "2 hours"},
		{2*time.Hour + time.Second, 2*time.Hour + time.Minute, "2 hours 1 minute"},
		{24 * time.Hour, 24 * time.Hour, "24 hours"},
	} {
		if got := oneWait(c.in); got != c.wait || waitWords(got) != c.words {
			t.Errorf("%s: %s %q, want %s %q", c.in, got, waitWords(got), c.wait, c.words)
		}
	}
}
