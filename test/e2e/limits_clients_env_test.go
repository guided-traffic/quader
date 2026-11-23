//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// What a format adds to the tests of the download limits (R-LIM-039, docs/developer/e2e.md#what-a-new-format-adds-to-the-tests): the
// limits themselves are the integration set's (test/integration/limits), without a client; a format's own set holds one case of how
// its client reacts to a refusal, in Enforce, with payloads of bytes. The helpers here are what those cases share.

// limitRule puts a rule at a repository of the suite's project: the profile Unlimited with the figures given, for every caller of a
// kind (R-LIM-011, R-LIM-032), and waits until every replica's index has it (awaitLimitsIndex). A repository of the case's own
// carries it, so nothing else of the suite meets the figure.
func limitRule(t testing.TB, adm *api, key, callerKind string, figures map[string]any) {
	t.Helper()
	adm.do(http.MethodPost, repoAPI(key)+"/limits/assignments", map[string]any{
		"who": map[string]any{"kind": "every", "callerKind": callerKind}, "profile": "unlimited", "overrides": figures,
		"note": "end-to-end: what the client shows at a refusal",
	}).want(t, "the rule "+callerKind+" at "+key, 201)
	awaitLimitsIndex(t)
}

// awaitLimitsIndex waits until every serving replica decides with every change of the download limits made so far: its gauge
// quader_limits_index_epoch reaches the epoch of the change area limits in the database (Q-LIM-111). A fixed three seconds stood
// here before, and a replica that took longer counted the first document against the instance's figures where the case expected
// the rule's 429. The bound is a failure's, not a wait: the area's changes reach a replica within seconds (R-ARCH-018).
func awaitLimitsIndex(t testing.TB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := bakSQL(ctx, "SELECT epoch FROM cluster_epoch WHERE area = 'limits'")
	if err != nil {
		t.Fatalf("the epoch of the area limits: %v", err)
	}
	// psql prints the tag of the SET of bakSQL before the row
	fields := strings.Fields(out)
	epoch := ""
	if len(fields) > 0 {
		epoch = fields[len(fields)-1]
	}
	want, err := strconv.ParseFloat(epoch, 64)
	if err != nil {
		t.Fatalf("the epoch of the area limits: %q", out)
	}
	start := time.Now()
	pods := readyServingPods(t)
	for _, pod := range pods {
		waitFor(t, pod+" to decide with the limits of epoch "+epoch, 30*time.Second, 100*time.Millisecond, func() bool {
			return metricsOf(t, pod)["quader_limits_index_epoch"] >= want
		})
	}
	t.Logf("limits: epoch %s on all %d serving replicas after %s", epoch, len(pods), time.Since(start).Round(time.Millisecond))
}

// awaitPathOnEveryReplica waits until every packages replica answers a read of the path with the token with 200: the repository and the
// grant are known there (R-AUTHZ-028).
func awaitPathOnEveryReplica(t testing.TB, token, path string) {
	t.Helper()
	addrs, stop := packagesForwards(t)
	defer stop()
	for i, addr := range addrs {
		a := apiAtPod(t, addr, token)
		start := time.Now()
		for a.getOverForward(path).Status != 200 {
			if time.Since(start) > 10*time.Second {
				t.Fatalf("replica %d: %s did not answer 200 within 10 s", i, path)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// clientRefusal is what a client printed and how it exited at a refusal of the limits: one row of the manual's table of the clients
// (docs/manual/download-limits.md), written to .out/e2e/limits-clients.json.
type clientRefusal struct {
	Client  string  `json:"client"`
	Figure  string  `json:"figure"`
	Printed string  `json:"printed"`
	Exit    int     `json:"exit"`
	Seconds float64 `json:"seconds"`
	Retried bool    `json:"retried"`
	Tail    string  `json:"tail"` // the last lines of the output
}

var clientRefusals sync.Mutex

// noteRefusal logs the row with the tail of the client's output and adds it to the artifact.
func noteRefusal(t testing.TB, out string, c clientRefusal) {
	t.Helper()
	c.Tail = tailLines(out, 8)
	t.Logf("limits: %s at %s printed %q, exit %d after %.1f s, retried %v; the output ends:\n%s", c.Client, c.Figure, c.Printed, c.Exit, c.Seconds, c.Retried, c.Tail)
	clientRefusals.Lock()
	defer clientRefusals.Unlock()
	path := filepath.Join(cfg.out, "limits-clients.json")
	var all []clientRefusal
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &all)
	}
	all = append(all, c)
	noteResult(t, "limits-clients", all)
}

// firstLineWith is the first line of the output that contains the needle (the client's own message), trimmed.
func firstLineWith(out, needle string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, needle) {
			return strings.TrimSpace(l)
		}
	}
	return ""
}

// exitOf is the exit status of a command run through kubectl exec: kubectl exits with the remote command's status.
func exitOf(err error) int {
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return ee.ExitCode()
	default:
		return -1
	}
}
