//go:build integration

package packages

import (
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

const sortKeyKind = "data_migration_version_sort_key"

// TestR_API_029_VersionsWithoutAStoredKeyKeepTheirPlaceBeforeAndAfterTheFill: version rows without a sort key, as the release before
// migration 00116 wrote them and as a replica of it writes them during a rolling upgrade (R-HA-020), are listed in the order of the
// versions among the rows that carry one, on every page in both directions and under the pre-release filter, and the latest version of
// the package list counts them (Q-API-430). A worker that starts fills their keys (the background migration version_sort_key, R-HA-022,
// Q-HA-492) in batches with the key the host's writer stores, records the completion, and the pages stay the same; a worker after it
// queues nothing, and one that starts after a row without a key was written again reopens the completion and fills it. left-pad of
// npm-local holds the seeded 1.0.0, two keyed rows and 31 rows without a key, all tiny; about 6 s on the stack.
func TestR_API_029_VersionsWithoutAStoredKeyKeepTheirPlaceBeforeAndAfterTheFill(t *testing.T) {
	e := newEnv(t)
	e.seed()
	pkg := e.st.Text(`SELECT p.id::text FROM packages p JOIN repositories r ON r.id = p.repo_id WHERE r.key = 'npm-local' AND p.name = 'left-pad'`)
	keyed := func(v string) {
		t.Helper()
		key, pre := e.st.VersionKey("npm", v)
		e.st.Exec(`INSERT INTO package_versions (package_id, version, display_version, sort_key, prerelease) VALUES ($1::bigint, $2, $2, $3, $4)`, pkg, v, key, pre)
	}
	unkeyed := func(v string, g int) {
		t.Helper()
		e.st.Exec(`INSERT INTO package_versions (package_id, version, display_version, created_at) VALUES ($1::bigint, $2, $2, now() + make_interval(secs => $3::int))`, pkg, v, g)
	}
	keyed("0.5.0")
	keyed("2.0.0-rc.1")
	// The highest release has no key, so the latest version is one the stored keys do not know.
	unkeyed("1.5.0", 0)
	// 0.0.1 … 0.0.30, every tenth a pre-release.
	var low []string
	for g := 1; g <= 30; g++ {
		v := "0.0." + strconv.Itoa(g)
		if g%10 == 0 {
			v += "-rc.1"
		}
		low = append(low, v)
		unkeyed(v, g)
	}
	want := []string{"2.0.0-rc.1", "1.5.0", "1.0.0", "0.5.0"}
	for i := len(low) - 1; i >= 0; i-- {
		want = append(want, low[i])
	}
	wantAsc := slices.Clone(want)
	slices.Reverse(wantAsc)
	wantPre := []string{"2.0.0-rc.1", "0.0.30-rc.1", "0.0.20-rc.1", "0.0.10-rc.1"}

	id := e.idOf(e.tokE, "left-pad")
	base := "/api/v1/packages/" + id + "/versions?limit=4"
	check := func(when string) {
		t.Helper()
		if got := versionsOf(e.all(e.tokE, base+"&sort=-version")); !slices.Equal(got, want) {
			t.Errorf("%s: highest first, pages of 4: %v\nwant %v", when, got, want)
		}
		if got := versionsOf(e.all(e.tokE, base+"&sort=version")); !slices.Equal(got, wantAsc) {
			t.Errorf("%s: lowest first, pages of 4: %v\nwant %v", when, got, wantAsc)
		}
		if got := versionsOf(e.all(e.tokE, base+"&sort=-version&prerelease=true")); !slices.Equal(got, wantPre) {
			t.Errorf("%s: the pre-releases: %v, want %v", when, got, wantPre)
		}
		lp := pkgByName(e.page(e.tokE, "/api/v1/packages?repository=it/npm-local").Items, "left-pad")
		if lp == nil || lp["latestVersion"] != "1.5.0" || num(lp["versionCount"]) != int64(len(want)) {
			t.Errorf("%s: the package list: %v", when, lp)
		}
	}
	if n := e.st.Number(`SELECT count(*) FROM package_versions WHERE sort_key IS NULL`); n != 31 {
		t.Fatalf("%d rows without a key before the fill, want 31", n)
	}
	check("before the fill")

	// A batch of 7 rows: 31 rows are five batches, each with its checkpoint.
	e.st.Start(t, testenv.ReplicaOptions{Name: "worker-a", Role: "worker", Env: []string{"QUADER_JOBS_BACKGROUND_MIGRATIONS_BATCH_SIZE=7"}})
	e.awaitSortKeyRuns(1)
	if n := e.st.Number(`SELECT count(*) FROM package_versions WHERE sort_key IS NULL OR prerelease IS NULL`); n != 0 {
		t.Fatalf("%d rows without a key after the fill", n)
	}
	for _, v := range append([]string{"1.5.0"}, low...) {
		key, pre := e.st.VersionKey("npm", v)
		stored := e.st.Text(`SELECT encode(sort_key, 'hex') || ' ' || prerelease::text FROM package_versions WHERE package_id = $1::bigint AND version = $2`, pkg, v)
		if writer := fmt.Sprintf("%x %v", key, pre); stored != writer {
			t.Errorf("%s: stored key and pre-release %s; the writer's are %s", v, stored, writer)
		}
	}
	if got := e.st.Text(`SELECT value->>'filled' FROM system_state WHERE key = 'version_sort_key_filled'`); got != "31" {
		t.Errorf("the recorded completion says filled %q, want 31", got)
	}
	check("after the fill")

	t.Run("a worker that starts after the completion queues nothing", func(t *testing.T) {
		e.st.Start(t, testenv.ReplicaOptions{Name: "worker-b", Role: "worker"})
		if n := e.st.Number(`SELECT count(*) FROM job WHERE kind = $1`, sortKeyKind); n != 1 {
			t.Errorf("%d runs after a second worker started, want the first alone", n)
		}
	})

	t.Run("a row without a key written after the completion reopens it", func(t *testing.T) {
		// A rollback to the release before 00116 wrote 0.0.31 and the upgrade came again.
		unkeyed("0.0.31", 31)
		e.st.Start(t, testenv.ReplicaOptions{Name: "worker-c", Role: "worker"})
		e.awaitSortKeyRuns(2)
		if n := e.st.Number(`SELECT count(*) FROM package_versions WHERE sort_key IS NULL`); n != 0 {
			t.Fatalf("%d rows without a key after the second fill", n)
		}
		if got := e.st.Text(`SELECT value->>'filled' FROM system_state WHERE key = 'version_sort_key_filled'`); got != "1" {
			t.Errorf("the recorded completion says filled %q, want 1", got)
		}
	})
}

// awaitSortKeyRuns waits until n runs of the fill succeeded and recorded the completion, within two minutes (a worker that starts,
// queues and runs one job).
func (e *env) awaitSortKeyRuns(n int64) {
	e.t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for e.st.Number(`SELECT count(*) FROM job WHERE kind = $1 AND state = 'succeeded' AND result->>'state' = 'filled'`, sortKeyKind) < n {
		if time.Now().After(deadline) {
			e.t.Fatalf("%d runs of the fill did not succeed within 2 min; the runs: %s", n,
				e.st.Text(`SELECT coalesce(string_agg(state::text || ' ' || coalesce(result::text, '') || ' after ' || attempt || ' attempts', ', ' ORDER BY created_at), 'none') FROM job WHERE kind = $1`, sortKeyKind))
		}
		time.Sleep(50 * time.Millisecond)
	}
}
