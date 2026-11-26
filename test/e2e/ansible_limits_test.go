//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestAnsibleLimits_R_LIM_020_WhatAnsibleGalaxyShowsAtARefusal is Ansible Galaxy's one case of the download limits (R-LIM-039): how
// ansible-galaxy reacts to a refusal. The figures themselves are the integration set's. Each step has a repository of its own with a
// rule for every technical token (the publisher's token is one): a request figure of one a second, whose refusal carries Retry-After 1, and
// a byte figure of one byte an hour, which refuses the archive of an install once a document has used the hour up. One collection of a
// few kilobytes, on ansible-core 2.21.
func TestAnsibleLimits_R_LIM_020_WhatAnsibleGalaxyShowsAtARefusal(t *testing.T) {
	fx := ansibleFx(t)
	env := tokenEnvVar + "=" + fx.token
	col := collection{ns: "e2e" + fx.run, name: "limits", version: "1.0.0"}
	repo := func(t *testing.T, purpose string, figures map[string]any) (string, *ansibleClient) {
		key := fx.localRepo(t, "limits-"+purpose)
		c := fx.client(t, ansibleCore221)
		c.must(withCfg(ansibleCfg(ansibleRepoURL(t, key)), col.publishScript()), env)
		limitRule(t, fx.adm, key, "technicalToken", figures)
		return key, c
	}
	install := func(c *ansibleClient, key string) (string, int, time.Duration) {
		start := time.Now()
		out, err := c.sh(withCfg(ansibleCfg(ansibleRepoURL(t, key)), "ansible-galaxy collection install "+col.full()+":1.0.0 -p coll"), env)
		return out, exitOf(err), time.Since(start)
	}

	// A Galaxy endpoint answers a rate limit 503 (bastion.md Q-BAS-451): ansible-galaxy retries a 429 of the version list, and its
	// response cache then holds the entry of the first attempt without its results ("Missing expected 'results' in ansible-galaxy
	// cache" with 2.21, measured 2026-11-20; exit 250 with 2.17, measured 2026-11-26). A 503 it does not retry: it exits 1 and prints
	// the refusal's status, detail and code
	t.Run("R-LIM-020 requestsPerSecond: ansible-galaxy prints the 503 of the version list", func(t *testing.T) {
		key, c := repo(t, "rps", map[string]any{"requestsPerSecond": 1, "requestsBurst": 1})
		out, code, d := install(c, key)
		noteRefusal(t, out, clientRefusal{
			Client: "ansible-galaxy collection install (2.21)", Figure: "requestsPerSecond", Printed: firstLineWith(out, "[ERROR]"),
			Exit: code, Seconds: d.Seconds(), Retried: false,
		})
		if code != 1 || !strings.Contains(out, "HTTP Code: 503") || strings.Contains(out, "Missing expected 'results'") {
			t.Fatalf("ansible-galaxy at one request a second exited %d:\n%s", code, tailLines(out, 20))
		}
	})

	t.Run("R-LIM-021 cachedBytesPerHour: after a document the install's archive is refused with 503", func(t *testing.T) {
		key, c := repo(t, "bytes", map[string]any{"cachedBytesPerHour": 1})
		// the version list counts its bytes but is never refused by a byte figure (R-LIM-007): after it the hour is used up. An
		// install's own documents cannot stand in for it: the archive follows them within tens of milliseconds, and whether a
		// replica flushed their bytes in between (R-LIM-018) decides whether the archive is admitted
		fx.registry(t).get(ansibleRepoPath(key)+"/v3/collections/"+col.ns+"/"+col.name+"/versions/").want(t, "the version list", 200)
		awaitFlush(t) // the bytes reach the window at the next round of the charge buffer
		out, code, d := install(c, key)
		noteRefusal(t, out, clientRefusal{
			Client: "ansible-galaxy collection install (2.21)", Figure: "cachedBytesPerHour", Printed: firstLineWith(out, "[ERROR]"),
			Exit: code, Seconds: d.Seconds(), Retried: strings.Count(out, "503") > 1,
		})
		if code != 1 || !strings.Contains(out, "Failed to download collection tar from 'quader': HTTP Error 503: Service Unavailable") {
			t.Fatalf("ansible-galaxy with the hour's bytes used up exited %d:\n%s", code, tailLines(out, 20))
		}
	})
}
