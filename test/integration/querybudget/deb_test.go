//go:build integration

package querybudget

import (
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/debstub/debgen"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// R-ARCH-027, R-DEB-035: a download from a local Debian repository costs one PostgreSQL statement on every route that serves a stored
// file: a package file, a plain index path (a generated item) and a by-hash path. The item lookup carries the tombstone the serve
// check decides on (the serve-facts kind deb_local), so no second statement of the request is left.
func TestR_ARCH_027_ADebianDownloadCostsOneStatement(t *testing.T) {
	e := newEnv(t)
	e.st.LogStatements(t)
	r := e.st.Start(t, testenv.ReplicaOptions{Name: "web-1", Role: "all", Env: []string{"QUADER_DEB_INDEX_DELAY=1s"}})

	keys := "/api/v1/projects/it/repositories/" + rDeb + "/deb/signing-keys"
	if a := e.st.Do(r, "POST", keys, e.tokAdmin, map[string]any{"generate": true}); a.Status/100 != 2 {
		t.Fatalf("first key: %d %s", a.Status, a.Body)
	}
	if a := e.st.Do(r, "POST", keys+"/promote", e.tokAdmin, nil); a.Status/100 != 2 {
		t.Fatalf("promote: %d %s", a.Status, a.Body)
	}
	d, err := debgen.Build(debgen.Hello("budget", "1.0-1", "amd64"))
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/deb/it/" + rDeb + "/"
	up := e.st.Do(r, "PUT", base+"upload/budget_1.0-1_amd64.deb?distribution=stable", e.tokAdmin, d.Bytes, "Content-Type", "application/vnd.debian.binary-package")
	if up.Status != 201 {
		t.Fatalf("upload: %d %s", up.Status, up.Body)
	}
	deadline := time.Now().Add(60 * time.Second)
	for e.st.Do(r, "GET", base+"dists/stable/InRelease", e.tokReader, nil).Status != 200 {
		if time.Now().After(deadline) {
			t.Fatal("the index was not published")
		}
		time.Sleep(300 * time.Millisecond)
	}
	var byHash string
	for _, l := range strings.Split(string(e.st.Do(r, "GET", base+"dists/stable/Release", e.tokReader, nil).Body), "\n") {
		if f := strings.Fields(l); len(f) == 3 && f[2] == "main/binary-amd64/Packages.gz" {
			byHash = base + "dists/stable/main/binary-amd64/by-hash/SHA256/" + f[0]
		}
	}
	if byHash == "" {
		t.Fatal("the Release lists no Packages.gz")
	}
	// The replica of the role all enqueues the latest slot of every task at its first tick, the weekly orphan sweep among them, which
	// walks the 256 prefixes of the connection with a pause check, a finish of the settled deletes and a checkpoint each (storage.md
	// R-STOR-026): within the window of the downloads that rhythm passes for statements of every download. The window starts once
	// that run has ended.
	awaitFirstRun(t, e, "orphan-sweep")

	for _, rt := range []struct {
		name, path string
		budget     int
	}{
		{"a package file of a local Debian repository", base + "pool/b/budget/budget_1.0-1_amd64.deb", 1},
		{"InRelease of a local Debian repository", base + "dists/stable/InRelease", 1},
		{"Packages.gz at its plain path", base + "dists/stable/main/binary-amd64/Packages.gz", 1},
		{"Packages.gz by its hash", byHash, 1},
	} {
		t.Run(rt.name, func(t *testing.T) {
			got := perDownload(t, e, r, rt.path)
			total := 0
			for _, v := range got {
				total += v
			}
			t.Logf("statements per download: %d (%s)", total, describe(got))
			if total > rt.budget {
				t.Errorf("a download runs %d statements (%s), the budget of R-ARCH-027 is %d", total, describe(got), rt.budget)
			}
		})
	}
}

// awaitFirstRun waits until the run of a task that the scheduler's first tick enqueued has ended, at most two minutes.
func awaitFirstRun(t *testing.T, e *env, task string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for e.st.Number(`SELECT count(*) FROM job WHERE task = $1 AND state IN ('succeeded', 'failed', 'cancelled')`, task) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the first run of the task %s did not end within two minutes", task)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
