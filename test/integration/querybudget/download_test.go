//go:build integration

package querybudget

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/nugetstub"
)

// requests is how many downloads of one path a window holds: a statement of the request runs once per download, the cached ones
// (token and principal reads whose entry expired) in a few of them.
const requests = 30

// perDownload runs a path against a replica and returns the statements of the downloads by name, per download: the statements
// that ran in nearly every request, by the number of times per request.
func perDownload(t *testing.T, e *env, r *testenv.Replica, path string) map[string]int {
	t.Helper()
	return perRequestAs(t, e, r, path, e.tokReader)
}

// perRequestAs is perDownload for the caller of a token. The budget is the default installation's: the malware scanning is on
// (malware.enabled, its default) and nothing is strict, which the window checks before it starts (R-MAL-013, Q-MAL-92). hdr are
// header pairs every download sends (a Bastion's forward, handover_test.go). The replica is one of e.start's.
//
// A statement is a download's when it ran with the download's request ID (testenv.RequestTransactions, Q-ARCH-700): what the
// replica's own loops, its scheduler, the batched writers or another process send meanwhile has none and never counts,
// however long the downloads take. The windows of the database's log before counted a GetSystemState of the readiness check
// as a download's when a loaded host stretched the window over enough of its rounds (2026-11-23).
func perRequestAs(t *testing.T, e *env, r *testenv.Replica, path, token string, hdr ...string) map[string]int {
	t.Helper()
	if a := e.st.Do(r, "GET", "/api/v1/malware/switch", token, nil); a.Status != 200 ||
		!strings.Contains(string(a.Body), `"enabled":true`) || !strings.Contains(string(a.Body), `"onlyChecked":false`) {
		t.Fatalf("the malware scanning is not on and lax as in a default installation: %d %s", a.Status, a.Body)
	}
	if a := e.st.Do(r, "GET", path, token, nil, hdr...); a.Status != 200 {
		t.Fatalf("warm-up %s: %d %.200s", path, a.Status, a.Body)
	}
	return e.perRequest(t, path, func() (int, string) {
		a := e.st.Do(r, "GET", path, token, nil, hdr...)
		return a.Status, a.Header.Get("X-Request-Id")
	}, 200)
}

// perRequest sends requests requests by send, each answered want, and returns the statements of the requests by name, per
// request: a name that ran in at least nine requests in ten, by the number of its runs per request, rounded. A name that ran in
// fewer is a cache entry that expired in one of them, which is no statement of every request.
func (e *env) perRequest(t *testing.T, what string, send func() (status int, id string), want int) map[string]int {
	t.Helper()
	ids := make([]string, 0, requests)
	e.rt.Count(true)
	for range requests {
		status, id := send()
		if status != want {
			e.rt.Count(false)
			t.Fatalf("%s: %d, want %d", what, status, want)
		}
		if id == "" {
			e.rt.Count(false)
			t.Fatalf("%s: the answer carries no X-Request-Id", what)
		}
		ids = append(ids, id)
	}
	e.rt.Count(false)
	runs, in := map[string]int{}, map[string]int{}
	for _, id := range ids {
		for name, n := range e.rt.Of(id).Statements() {
			runs[name] += n
			in[name]++
		}
	}
	out := map[string]int{}
	for name, n := range runs {
		if in[name]*10 >= len(ids)*9 {
			out[name] = (n + len(ids)/2) / len(ids)
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s: no statement ran in nine requests in ten; the counter is not in the replica (e.start): %v", what, runs)
	}
	return out
}

func describe(c map[string]int) string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s x%d", k, c[k])
	}
	return strings.Join(parts, ", ")
}

// R-ARCH-027: a download of a stored file costs one PostgreSQL statement on every route and every format that serves one: a local
// file, a remote hit, and the tiers of a virtual repository over both. The item lookup carries what the serve check decides on
// (the tombstone of npm and NuGet, the facts of an npm remote version) and, for a remote hit, the cache entry, so that no
// second statement of the request is left.
func TestR_ARCH_027_ADownloadCostsOneStatement(t *testing.T) {
	e := newEnv(t)
	r := e.start(t, testenv.ReplicaOptions{Name: "web-1"})

	const local = "budget-local"
	e.publish(r, rNPM, local, "1.0.0")
	e.push(r, rNuGet, "Budget.Local", "1.0.0")

	// The remote hits are filled first, through the remote repository itself.
	if a := e.do(r, "/api/npm/it/"+rNPMRem+"/"+e.pkg); a.Status != 200 {
		t.Fatalf("npm document: %d %s", a.Status, a.Body)
	}
	npmRemote := npmTarballPath(rNPMRem, e.pkg, e.ver)
	if a := e.do(r, npmRemote); a.Status != 200 {
		t.Fatalf("npm fill: %d %.200s", a.Status, a.Body)
	}
	pkg, _ := nugetstub.Default().Get(nugetstub.NewtonsoftID)
	nugetVer := pkg.Versions[1].Version
	nugetRemote := nupkgPath(rNuGetRem, "Newtonsoft.Json", nugetVer)
	if a := e.do(r, nugetRemote); a.Status != 200 {
		t.Fatalf("nuget fill: %d %.200s", a.Status, a.Body)
	}

	// budget is one statement per member the resolution of a virtual repository visits (Q-ARCH-290): the local member is
	// asked first, so a file of the remote member costs the miss at the local one and the hit. The walk is the same for every
	// format (each tier is its member's one lookup with the facts of the member's format), so npm's two virtual routes hold it;
	// NuGet keeps its local route and its remote hit, where its own serve facts are read.
	routes := []struct {
		name, path string
		budget     int
	}{
		{"an npm tarball of a local repository", npmTarballPath(rNPM, local, "1.0.0"), 1},
		{"the raw view of an npm tarball", "/files/it/" + rNPM + "/" + e.itemPath(rNPM, "-1.0.0.tgz"), 1},
		{"an npm tarball of a remote repository (hit)", npmRemote, 1},
		{"an npm tarball of the local member of a virtual repository", npmTarballPath(rNPMVirt, local, "1.0.0"), 1},
		{"an npm tarball of the remote member of a virtual repository", npmTarballPath(rNPMVirt, e.pkg, e.ver), 2},
		{"a .nupkg of a local NuGet repository", nupkgPath(rNuGet, "Budget.Local", "1.0.0"), 1},
		{"a .nupkg of a remote NuGet repository (hit)", nugetRemote, 1},
	}
	for _, rt := range routes {
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
	// A download handed to a signed link of the object store costs what a streamed one does (link_test.go).
	t.Run("an OCI blob of a local repository handed to a signed link", func(t *testing.T) { linkBudget(t, e) })
	// And a download handed over to a Bastion (handover_test.go).
	t.Run("an OCI blob of a local repository handed over to a Bastion", func(t *testing.T) { handoverBudget(t, e) })
}
