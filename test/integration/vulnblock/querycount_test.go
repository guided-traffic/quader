//go:build integration

package vulnblock

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/app"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// countedReplica starts a replica whose statements rt counts by request (testenv.RequestTransactions, Q-ARCH-700).
func (e *env) countedReplica(rt *testenv.RequestTransactions, name string, env ...string) *testenv.Replica {
	e.t.Helper()
	return e.st.Start(e.t, testenv.ReplicaOptions{
		Name: name, Env: env, Vuln: &app.VulnOverride{States: e.states, Evaluator: e.eval}, Database: []postgres.Option{rt.Option()},
	})
}

// downloadStatements runs n downloads of a path against a replica and returns the statements they ran, by name over all of them,
// and per download: a name that ran in nine downloads of ten, by its runs per download (RequestTransactions.PerRequest). Only the
// statements that ran with a download's request ID count, so the replica's own loops and a run of the job scheduler are never
// taken for a download's (Q-ARCH-700, Q-MAL-92), and no window of the database's log is read.
func downloadStatements(t *testing.T, e *env, rt *testenv.RequestTransactions, r *testenv.Replica, path string, n int) (counts, perDownload map[string]int) {
	t.Helper()
	if a := e.do(r, "GET", path, e.tokReader); a.Status != 200 {
		t.Fatalf("warm-up %s: %d %s", path, a.Status, a.Body)
	}
	ids := make([]string, 0, n)
	rt.Count(true)
	for range n {
		a := e.do(r, "GET", path, e.tokReader)
		if a.Status != 200 || a.Header.Get("X-Request-Id") == "" {
			rt.Count(false)
			t.Fatalf("%s: %d, X-Request-Id %q", path, a.Status, a.Header.Get("X-Request-Id"))
		}
		ids = append(ids, a.Header.Get("X-Request-Id"))
	}
	rt.Count(false)
	counts = map[string]int{}
	for _, id := range ids {
		for name, k := range rt.Of(id).Statements() {
			counts[name] += k
		}
	}
	for name := range counts {
		if strings.Contains(rt.SQL(name), "vuln_") && name != "GetLiveItem" {
			t.Errorf("a statement of the scanning's tables runs on a download: %s", name)
		}
	}
	perDownload = rt.PerRequest(ids)
	if len(perDownload) == 0 {
		t.Fatalf("%s: no statement ran in nine downloads of ten; the counter is not in the replica: %v", path, counts)
	}
	return counts, perDownload
}

func describe(c map[string]int) string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "  %-50s x%d\n", k, c[k])
	}
	return b.String()
}

// statementsPerDownload checks that a path costs the same statements with scanning (the replica on) and without (the replica
// off), that the item lookup runs once per download, and that no statement of the scanning's tables runs but the item lookup's join. A statement of the request runs once
// per download; the batched and the cached ones (download statistics, token and principal reads) run less often.
func statementsPerDownload(t *testing.T, e *env, rt *testenv.RequestTransactions, on, off *testenv.Replica, path string) {
	t.Helper()
	const n = 30
	withScan, pw := downloadStatements(t, e, rt, on, path, n)
	without, po := downloadStatements(t, e, rt, off, path, n)
	if fmt.Sprint(pw) != fmt.Sprint(po) || pw["GetLiveItem"] != 1 {
		t.Errorf("per download with scanning %v, without %v; want the same statements, one GetLiveItem per download\nwith:\n%swithout:\n%s",
			pw, po, describe(withScan), describe(without))
	}
	sum := func(c map[string]int) (total int) {
		for _, v := range c {
			total += v
		}
		return total
	}
	t.Logf("statements per download: %.2f with scanning, %.2f without\n%s", float64(sum(withScan))/n, float64(sum(without))/n, describe(withScan))
}

// R-VULN-015 (R-ARCH-027): scanning adds no query to a download. One replica scans (a dismissed and a resolved finding exist,
// so the lateral join runs and skips them) and one does not (vulnerabilities.enabled false); both serve the same stored
// versions of one database, and the statements each download ran are the same: the verdict is read by the item
// lookup, not by a query of its own.
func TestR_VULN_015_ScanningAddsNoQueryToADownload(t *testing.T) {
	e := newEnv(t, options{})
	rt := testenv.NewRequestTransactions()
	on := e.countedReplica(rt, "scan-on")
	off := e.countedReplica(rt, "scan-off", "QUADER_VULNERABILITIES_ENABLED=false")
	e.publish(on, rNPM, mal, "1.0.0")
	e.publish(on, rNPM, mal, "1.0.1")
	e.finding(rNPM, "npm", mal, "1.0.0", malA, "malicious", "dismissed")
	e.finding(rNPM, "npm", mal, "1.0.1", malA, "malicious", "resolved")
	for name, path := range map[string]string{
		"a local npm tarball":     tgzPath(rNPM, mal, "1.0.0"),
		"a local npm tarball (2)": tgzPath(rNPM, mal, "1.0.1"),
		"the raw view":            "/files/it/" + rNPM + "/" + e.itemPath(rNPM, "-1.0.0.tgz"),
	} {
		t.Run(name, func(t *testing.T) { statementsPerDownload(t, e, rt, on, off, path) })
	}
}

// The same for the remote hit, which reads the cache entry, the item and the format's facts: the verdict joins the item lookup.
func TestR_VULN_015_ScanningAddsNoQueryToARemoteHit(t *testing.T) {
	e := newEnv(t, options{})
	rt := testenv.NewRequestTransactions()
	on := e.countedReplica(rt, "scan-on")
	off := e.countedReplica(rt, "scan-off", "QUADER_VULNERABILITIES_ENABLED=false")
	if a := e.do(on, "GET", "/api/npm/it/"+rRemote+"/"+e.pkg, e.tokReader); a.Status != 200 {
		t.Fatalf("document: %d %s", a.Status, a.Body)
	}
	if a := e.do(on, "GET", tgzPath(rRemote, e.pkg, e.ver), e.tokReader); a.Status != 200 {
		t.Fatalf("fill: %d %s", a.Status, a.Body)
	}
	e.requireLinked(rRemote, e.pkg, e.ver)
	e.finding(rRemote, "npm", e.pkg, e.ver, malA, "malicious", "dismissed")
	statementsPerDownload(t, e, rt, on, off, tgzPath(rRemote, e.pkg, e.ver))
}
