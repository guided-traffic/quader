//go:build integration

package querybudget

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestMeasureTheServerCostOfALiveRefresh is a measurement, not a test (CLAUDE.md, "Tests stay runnable"; api-and-ui.md R-UI-213,
// H-1847): what one refresh of a live view costs a replica, by the calls the views send when they read again (the paths the browser
// measurement of web/e2e/renders/live-cost.spec.ts recorded). It runs only with QUADER_TEST_MEASURE_LIVE set to the repetitions per
// call and writes its numbers to the log, which docs/developer/web-ui.md keeps:
//
//	QUADER_TEST_MEASURE_LIVE=50 test/integration/run.sh -run TestMeasureTheServerCostOfALiveRefresh -v ./test/integration/querybudget/
//
// One replica of the role web with the request tracer (statements by request ID), the area's installation with 20 npm packages in its
// local repository, a token of a reader and of an administrator. Each call is sent the given number of times, one at a time; the
// statements are the median per call, the time the median at the client, and the CPU the process's user and system time over the
// calls (the replica and the plain HTTP client of this test, nothing else runs) divided by their number.
func TestMeasureTheServerCostOfALiveRefresh(t *testing.T) {
	reps, _ := strconv.Atoi(os.Getenv("QUADER_TEST_MEASURE_LIVE"))
	if reps < 1 {
		t.Skip("a measurement: set QUADER_TEST_MEASURE_LIVE to the repetitions per call, such as 50")
	}
	e := newEnv(t)
	r := e.start(t, testenv.ReplicaOptions{Name: "web-1"})
	for i := range 20 {
		e.publish(r, rNPM, fmt.Sprintf("live-%02d", i), "1.0.0")
	}
	e.publish(r, rNPM, "live-00", "1.1.0")
	look := e.st.Do(r, "GET", "/api/v1/packages/lookup?format=npm&name=live-00", e.tokReader, nil)
	if look.Status != http.StatusOK {
		t.Fatalf("lookup: %d %s", look.Status, look.Body)
	}
	var pkg struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(look.Body, &pkg); err != nil || pkg.ID == "" {
		t.Fatalf("lookup answer %s: %v", look.Body, err)
	}
	repo := url.QueryEscape("it/" + rNPM)
	calls := []struct{ who, what, path string }{
		{"reader", "top bar", "/api/v1/system/banner"},
		{"reader", "top bar", "/api/v1/system/maintenance"},
		{"reader", "a repository's content (a page of 50)", "/api/v1/packages?repository=" + repo + "&format=npm&sort=name&include=vulnerabilities&limit=50"},
		{"reader", "a package's page: its summary", "/api/v1/packages/lookup?format=npm&name=live-00"},
		{"reader", "a package's page: its findings' summary", "/api/v1/packages/" + pkg.ID + "/vulnerabilities"},
		{"reader", "a version's page: the version", "/api/v1/packages/" + pkg.ID + "/versions/1.1.0"},
		{"reader", "a version's page: its report", "/api/v1/packages/" + pkg.ID + "/versions/1.1.0/vulnerabilities"},
		{"reader", "the tab Upstream: its state", "/api/v1/projects/it/repositories/" + rNPMRem + "/upstream"},
		{"reader", "the tab Upstream: its levels", "/api/v1/projects/it/repositories/" + rNPMRem + "/upstream/levels?limit=200"},
		{"reader", "Vulnerabilities: the summary", "/api/v1/vulnerabilities/summary?project=it"},
		{"reader", "Vulnerabilities: the trend", "/api/v1/vulnerabilities/trend?project=it"},
		{"reader", "Vulnerabilities: the findings", "/api/v1/vulnerabilities/findings?project=it&sort=-severity&limit=50"},
		{"admin", "top bar: Downloads", "/api/v1/system/upstream-downloads/summary"},
		{"admin", "System status, dashboard: info", "/api/v1/system/info"},
		{"admin", "System status, dashboard: replicas", "/api/v1/system/replicas"},
		{"admin", "dashboard: overview", "/api/v1/system/overview"},
		{"admin", "System status, dashboard, Settings › Storage: encryption", "/api/v1/system/encryption"},
		{"admin", "dashboard, Settings › Storage: overview", "/api/v1/storage/overview"},
		{"admin", "Settings › Storage: the cells of 30 days", "/api/v1/storage/history?period=30d"},
		{"admin", "Settings › Storage: problems", "/api/v1/storage/problems?period=30d&limit=10"},
		{"admin", "Settings › Storage: findings", "/api/v1/storage/findings?period=30d&state=all&limit=10"},
		{"admin", "Settings › Storage: connections", "/api/v1/system/storage-connections"},
		{"admin", "dashboard: backups", "/api/v1/system/backups/summary"},
		{"admin", "System status: scanners", "/api/v1/system/scanners"},
		{"admin", "System status: recent jobs", "/api/v1/system/jobs?limit=10"},
		{"admin", "Jobs: a page", "/api/v1/system/jobs?limit=50"},
		{"admin", "System status: tasks", "/api/v1/system/tasks?limit=500"},
		{"admin", "Settings › Storage: damaged blobs", "/api/v1/storage/damaged-blobs?scope=blobs&limit=20"},
		{"admin", "Security: sources", "/api/v1/vulnerabilities/sources"},
		{"admin", "Audit log: a page", "/api/v1/audit/rows?limit=100"},
		{"reader", "Downloads: the queue", "/api/v1/projects/it/repositories/" + rNPMRem + "/downloads?limit=50"},
		{"reader", "Downloads: the bulks", "/api/v1/projects/it/repositories/" + rNPMRem + "/download-bulks?limit=5"},
	}
	token := map[string]string{"reader": e.tokReader, "admin": e.tokAdmin}
	send := func(path, tok string) (int, string, int, time.Duration) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, e.st.Origin(r.Addr("default"))+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = testenv.DefaultHost
		req.Header.Set("Authorization", "Bearer "+tok)
		t0 := time.Now()
		res, err := testenv.Client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		return res.StatusCode, res.Header.Get("X-Request-Id"), len(b), time.Since(t0)
	}
	cpu := func() time.Duration {
		var u syscall.Rusage
		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &u); err != nil {
			t.Fatal(err)
		}
		return time.Duration(u.Utime.Nano() + u.Stime.Nano())
	}
	for _, c := range calls {
		send(c.path, token[c.who]) // the first answer fills what the replica caches
		e.rt.Count(true)
		var stmts []int
		var lat []time.Duration
		status, size := 0, 0
		c0 := cpu()
		for range reps {
			s, id, n, d := send(c.path, token[c.who])
			status, size = s, n
			total := 0
			for _, k := range e.rt.Of(id).Statements() {
				total += k
			}
			stmts, lat = append(stmts, total), append(lat, d)
		}
		perCall := (cpu() - c0) / time.Duration(reps)
		e.rt.Count(false)
		slices.Sort(stmts)
		slices.Sort(lat)
		t.Logf("MEASURE %-6s %-45s %d, %6d B, statements median %d (min %d, max %d), time median %s, CPU %s per call — %s",
			c.who, c.what, status, size, stmts[len(stmts)/2], stmts[0], stmts[len(stmts)-1], lat[len(lat)/2].Round(10*time.Microsecond),
			perCall.Round(10*time.Microsecond), c.path)
	}
}
