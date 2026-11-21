//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestAnsibleLimits_R_LIM_020_WhatAnsibleGalaxyShowsAtARefusal is Ansible Galaxy's one case of the download limits (R-LIM-039): how
// ansible-galaxy reacts to a refusal. The figures themselves are the integration set's. Each step has a repository of its own with a
// rule for every technical token (the publisher's token is one): a request figure of one a second, whose 429 carries Retry-After 1, and
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

	// ansible-galaxy retries a 429 of the version list, and its response cache then holds the entry of the first attempt without its
	// results: it fails with "Missing expected 'results' in ansible-galaxy cache" (ansible-core 2.21, measured 2026-11-20), a message
	// of the client that does not name the limit
	t.Run("R-LIM-020 requestsPerSecond: ansible-galaxy retries the 429 of the version list and fails on its own cache", func(t *testing.T) {
		key, c := repo(t, "rps", map[string]any{"requestsPerSecond": 1, "requestsBurst": 1})
		out, code, d := install(c, key)
		noteRefusal(t, out, clientRefusal{
			Client: "ansible-galaxy collection install (2.21)", Figure: "requestsPerSecond", Printed: firstLineWith(out, "[ERROR]"),
			Exit: code, Seconds: d.Seconds(), Retried: true,
		})
		if code != 1 || (!strings.Contains(out, "Missing expected 'results'") && !strings.Contains(out, "429")) {
			t.Fatalf("ansible-galaxy at one request a second exited %d:\n%s", code, tailLines(out, 20))
		}
	})

	t.Run("R-LIM-021 cachedBytesPerHour: after a document the install's archive is refused with 429", func(t *testing.T) {
		key, c := repo(t, "bytes", map[string]any{"cachedBytesPerHour": 1})
		// the version list counts its bytes but is never refused by a byte figure (R-LIM-007): after it the hour is used up. An
		// install's own documents cannot stand in for it: the archive follows them within tens of milliseconds, and whether a
		// replica flushed their bytes in between (R-LIM-018) decides whether the archive is admitted
		fx.registry(t).get(ansibleRepoPath(key)+"/v3/collections/"+col.ns+"/"+col.name+"/versions/").want(t, "the version list", 200)
		time.Sleep(2 * time.Second) // the bytes reach the window at the next flush
		out, code, d := install(c, key)
		noteRefusal(t, out, clientRefusal{
			Client: "ansible-galaxy collection install (2.21)", Figure: "cachedBytesPerHour", Printed: firstLineWith(out, "[ERROR]"),
			Exit: code, Seconds: d.Seconds(), Retried: strings.Count(out, "429") > 1,
		})
		if code != 1 || !strings.Contains(out, "Failed to download collection tar from 'quader': HTTP Error 429: Too Many Requests") {
			t.Fatalf("ansible-galaxy with the hour's bytes used up exited %d:\n%s", code, tailLines(out, 20))
		}
	})
}
