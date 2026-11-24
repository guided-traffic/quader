//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// What a format adds to the tests of the curation of a remote repository (remote-curation.md R-CUR-022,
// docs/developer/e2e.md#what-a-new-format-adds-to-the-tests): the rules' decision, the documents' filter, the queue and the requests are
// the unit tests', the kit's and the integration areas' (one case per format there); a format's own set holds one case of what only its
// real client shows: the refusal of a remote in mode onlyAllowed as the client prints it, the client after an allow, and a version
// downloaded ahead through the queue that a later client fetch takes from the cache. The helpers here are what those cases share.

// onlyAllowed is the member remote.delivery of a remote that delivers only what a rule allows (R-CUR-001), merged into the remote
// section a format's fixture creates.
func onlyAllowed() map[string]any {
	return map[string]any{"delivery": map[string]any{"mode": "onlyAllowed"}}
}

// allowRule writes a rule that allows a package (version "") or one version of it in a remote, through the management API as the
// administrator (R-CUR-006). The caller waits for the replicas (awaitOnEveryPackagesReplica): a rule reaches a replica's snapshot
// through the change area of the repository within its bound (H-132).
func allowRule(t testing.TB, adm *api, key, name, version string) {
	t.Helper()
	rule := map[string]any{"kind": "package", "name": name, "effect": "allow", "note": "end-to-end: the client after an allow"}
	if version != "" {
		rule["kind"], rule["version"] = "version", version
	}
	adm.do(http.MethodPost, repoAPI(key)+"/delivery-rules", rule).want(t, fmt.Sprintf("allow %s %s in %s", name, version, key), 200, 201)
}

// downloadAhead queues a download ahead of one version through the repository's queue (R-CUR-010) as the administrator and waits until
// its row is done. A version the rules do not deliver is stored for examination and not served (R-CUR-014), so the case allows it
// afterwards and the client's fetch is the cache's. The run is a one-attempt job of the queue downloads on a worker (R-CUR-011); the
// bound is a failure's, not a wait.
func downloadAhead(t testing.TB, adm *api, key, name, version string) {
	t.Helper()
	var started struct {
		Download struct {
			ID string `json:"id"`
		} `json:"download"`
	}
	adm.do(http.MethodPost, repoAPI(key)+"/downloads", map[string]any{"level": "version", "name": name, "version": version}).
		want(t, fmt.Sprintf("the download ahead of %s %s in %s", name, version, key), 202).JSON(t, &started)
	if started.Download.ID == "" {
		t.Fatalf("the download ahead of %s %s answered no row", name, version)
	}
	var row struct {
		State   string          `json:"state"`
		Outcome string          `json:"outcome"`
		Stored  int             `json:"stored"`
		Failure json.RawMessage `json:"failure"`
	}
	took := waitFor(t, "the download ahead of "+name+" "+version+" to end", 2*time.Minute, 250*time.Millisecond, func() bool {
		adm.get(repoAPI(key)+"/downloads/"+started.Download.ID).want(t, "the row of the download ahead", 200).JSON(t, &row)
		return row.State == "done" || row.State == "failed" || row.State == "cancelled"
	})
	if row.State != "done" || row.Outcome != "completed" || row.Stored < 1 {
		t.Fatalf("the download ahead of %s %s ended %s, outcome %q, %d files stored, failure %s", name, version, row.State, row.Outcome, row.Stored, row.Failure)
	}
	t.Logf("curation: the download ahead of %s %s stored %d files after %s", name, version, row.Stored, took.Round(time.Millisecond))
}

// awaitOnEveryPackagesReplica waits until every packages replica answers the path with the token as ok says: a rule written through the
// management API is in a replica's snapshot of the repository when the replica serves by it (R-CUR-006 "Propagation"). A request that
// failed on the transport of the pod's forward goes to a new forward (getOverForward).
func awaitOnEveryPackagesReplica(t testing.TB, token, what, path string, ok func(reply) bool, h ...string) {
	t.Helper()
	addrs, stop := packagesForwards(t)
	defer stop()
	start := time.Now()
	for i, addr := range addrs {
		a := apiAtPod(t, addr, token)
		var last reply
		waitFor(t, fmt.Sprintf("packages replica %d: %s", i, what), 30*time.Second, 200*time.Millisecond, func() bool {
			last = a.getOverForward(path, h...)
			return ok(last)
		})
	}
	t.Logf("curation: %s on all %d packages replicas after %s", what, len(addrs), time.Since(start).Round(time.Millisecond))
}

// clientCuration is what a client printed and how it exited when a remote's rules refused it (R-CUR-004): one row of the manual's table
// of the clients (docs/manual/user/remote-curation.md), written to .out/e2e/curation-clients.json.
type clientCuration struct {
	Client  string  `json:"client"`
	Printed string  `json:"printed"`
	Exit    int     `json:"exit"`
	Seconds float64 `json:"seconds"`
	Tail    string  `json:"tail"` // the last lines of the output
}

var curationClients sync.Mutex

// noteCuration logs the row with the tail of the client's output and adds it to the artifact.
func noteCuration(t testing.TB, out string, c clientCuration) {
	t.Helper()
	c.Tail = tailLines(out, 8)
	t.Logf("curation: %s printed %q at the rules' refusal, exit %d after %.1f s; the output ends:\n%s", c.Client, c.Printed, c.Exit, c.Seconds, c.Tail)
	curationClients.Lock()
	defer curationClients.Unlock()
	var all []clientCuration
	if b, err := os.ReadFile(filepath.Join(cfg.out, "curation-clients.json")); err == nil {
		_ = json.Unmarshal(b, &all)
	}
	noteResult(t, "curation-clients", append(all, c))
}

// caseClock starts the clock of a format's case after the fixture its set builds anyway, and returns the function that logs what the
// case itself took: the seconds its leg gains (docs/developer/e2e.md#what-a-new-format-adds-to-the-tests).
func caseClock(t testing.TB) func() {
	start := time.Now()
	return func() {
		t.Logf("curation: the case took %s beyond the fixture of its set", time.Since(start).Round(100*time.Millisecond))
	}
}
