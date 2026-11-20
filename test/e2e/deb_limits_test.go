//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestDebLimits_R_LIM_020_WhatAptShowsAtARefusal is APT's one case of the download limits (R-LIM-039): how apt-get reacts to a refusal.
// The figures themselves are the integration set's. Each step has a repository of its own with a rule for every technical token (the
// publisher's token is one): a request figure of one a second, whose 429 carries Retry-After 1, and a byte figure of one byte an hour,
// which refuses the package after the indexes. One package of about a kilobyte, on the Debian 13 client (APT 3.0).
//
// APT retries a 429 three times (an "Ign" line each) before it fails, waiting longer each time: with its defaults the refused package
// failed after 90 s (measured 2026-11-20). The byte step caps APT's wait at one second (Acquire::Retries::Delay::Maximum), so it shows the
// same attempts in seconds; what the defaults cost is in the manual's table. The request step keeps the defaults: retries a second
// apart are requests of the window too (R-ARCH-034), so at one request a second each would be refused again (seen once).
func TestDebLimits_R_LIM_020_WhatAptShowsAtARefusal(t *testing.T) {
	fx := debFx(t)
	name := fx.name("limits")
	repo := func(t *testing.T, purpose string, figures map[string]any) (string, *aptClient) {
		key := fx.localRepo(t, "limits-"+purpose, nil)
		fx.awaitPublished(t, key, debSuite, fx.upload(t, key, newDebPackage(t, name, "1.0"), debSuite))
		c := fx.client(t, aptDebian13)
		c.configure(key)
		limitRule(t, fx.adm, key, "technicalToken", figures)
		return key, c
	}
	run := func(c *aptClient, script string) (string, int, time.Duration) {
		start := time.Now()
		out, err := c.sh(script)
		return out, exitOf(err), time.Since(start)
	}

	t.Run("R-LIM-020 requestsPerSecond: apt-get update at one request a second", func(t *testing.T) {
		_, c := repo(t, "rps", map[string]any{"requestsPerSecond": 1, "requestsBurst": 1})
		out, code, d := run(c, "apt-get update --error-on=any")
		noteRefusal(t, out, clientRefusal{
			Client: "apt-get update (APT 3.0)", Figure: "requestsPerSecond", Printed: firstLineWith(out, "429"),
			Exit: code, Seconds: d.Seconds(), Retried: strings.Contains(out, "Ign:"),
		})
		if code != 0 || !strings.Contains(out, "Ign:") {
			t.Fatalf("apt-get update at one request a second exited %d, want a retry of the refused index and success:\n%s", code, tailLines(out, 20))
		}
	})

	t.Run("R-LIM-021 cachedBytesPerHour: apt-get download is refused with 429 and exits 100", func(t *testing.T) {
		_, c := repo(t, "bytes", map[string]any{"cachedBytesPerHour": 1})
		// the indexes count their bytes but are never refused by a byte figure (R-LIM-007): after them the hour is used up
		c.mustUpdate()
		time.Sleep(2 * time.Second) // the bytes reach the window at the next flush
		out, code, d := run(c, "cd /tmp && apt-get -o Acquire::Retries::Delay::Maximum=1 download "+name)
		noteRefusal(t, out, clientRefusal{
			Client: "apt-get download (APT 3.0)", Figure: "cachedBytesPerHour", Printed: firstLineWith(out, "E: Failed to fetch"),
			Exit: code, Seconds: d.Seconds(), Retried: strings.Count(out, "Ign:1") == 3,
		})
		if code != 100 || strings.Count(out, "Ign:1") != 3 || !strings.Contains(out, "429  Too Many Requests") {
			t.Fatalf("apt-get download with the hour's bytes used up exited %d:\n%s", code, tailLines(out, 20))
		}
	})
}
