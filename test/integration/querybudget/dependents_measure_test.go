//go:build integration

package querybudget

import (
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/packages/view"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestMeasureUsedBy is a measurement, not a test (CLAUDE.md, "Tests stay runnable"; api-and-ui.md R-API-086, Q-API-458): the read of
// "Used by" and the package list filtered by dependsOn with 50,000 npm packages of which 5,000 depend on one name, for a caller who reads
// the repository whole, and for a reader of that repository beside a second repository she may not read whose 5,000 further packages
// depend on the same name, and the list without the filter beside them. It runs only with QUADER_TEST_MEASURE_DEPENDENTS set and writes
// the medians to the log, which docs/developer/packages.md keeps:
//
//	QUADER_TEST_MEASURE_DEPENDENTS=1 test/integration/run.sh -run TestMeasureUsedBy -v ./test/integration/querybudget/
//
// The rows are tiny and written directly: one version per package, one dependency row each (one in ten on "lib", the rest spread over 50
// other names).
func TestMeasureUsedBy(t *testing.T) {
	if os.Getenv("QUADER_TEST_MEASURE_DEPENDENTS") == "" {
		t.Skip("a measurement: set QUADER_TEST_MEASURE_DEPENDENTS=1")
	}
	st := testenv.NewStack(t, testenv.StackOptions{Name: "querybudget"})
	st.Repository("npm-open", "local", "npm", `{}`)
	st.Repository("npm-closed", "local", "npm", `{}`)
	ada := st.User("ada", "budget-leads")
	st.Bind("admin", ada)
	rita := st.User("rita")
	st.Policy("open-readers", []string{"read"}, []string{"npm-open"}, rita)
	admin := st.Token(ada, scope.Scope{Inherit: true, Admin: true})
	reader := st.Token(rita, scope.Default())
	key, pre := st.VersionKey("npm", "1.0.0")
	seed := func(repo string, n int) {
		st.Exec(`INSERT INTO packages (repo_id, name, display_name)
		         SELECT r.id, 'p' || r.id || '-' || g, 'p' || r.id || '-' || g FROM repositories r, generate_series(1, $2::int) g WHERE r.key = $1`, repo, n)
		st.Exec(`INSERT INTO package_versions (package_id, version, display_version, sort_key, prerelease)
		         SELECT p.id, '1.0.0', '1.0.0', $1, $2 FROM packages p
		          WHERE NOT EXISTS (SELECT 1 FROM package_versions v WHERE v.package_id = p.id)`, key, pre)
	}
	seed("npm-open", 50_000)
	seed("npm-closed", 5_000)
	st.Exec(`INSERT INTO package_dependencies (version_id, repo_id, name, kind, range)
	         SELECT pv.id, p.repo_id,
	                CASE WHEN r.key = 'npm-closed' OR p.id % 10 = 0 THEN 'lib' ELSE 'other-' || (p.id % 50) END, 'dependencies', '^1.0.0'
	           FROM package_versions pv JOIN packages p ON p.id = pv.package_id JOIN repositories r ON r.id = p.repo_id`)
	st.Exec(`INSERT INTO package_dependency_reads (version_id, reader) SELECT id, 1 FROM package_versions`)
	st.Exec(`ANALYZE packages, package_versions, package_dependencies, package_dependency_reads`)
	r := st.Start(t, testenv.ReplicaOptions{Name: "web-measure"})

	// median reads a path 12 times after a first read, so that the statements run past the fifth execution of a prepared statement,
	// where PostgreSQL may plan them generically, and returns the median and the slowest.
	median := func(token, path string) (time.Duration, time.Duration, int) {
		t.Helper()
		var d []time.Duration
		status := 0
		st.Do(r, "GET", path, token, nil)
		for range 12 {
			t0 := time.Now()
			a := st.Do(r, "GET", path, token, nil)
			d = append(d, time.Since(t0))
			status = a.Status
		}
		slices.Sort(d)
		return d[len(d)/2], d[len(d)-1], status
	}
	// The list statement with the filter, as the server sends it (the ids a literal array), for each caller's repositories.
	for _, c := range []struct{ who, repos string }{{"admin", "('npm-open','npm-closed')"}, {"reader", "('npm-open')"}} {
		whole := st.Text(`SELECT array_agg(id ORDER BY id)::text FROM repositories WHERE key IN ` + c.repos)
		ids := st.Text(`SELECT array_agg(DISTINCT d.version_id)::text FROM package_dependencies d WHERE d.name = 'lib' AND d.repo_id = ANY($1::bigint[])`, whole)
		sql := testenv.QueryText(t, "package_view.sql", "PackageViewList", map[string]string{
			"sort": "'name'", "descending": "false", "max_rows": "26", "withheld": "'{}'::bigint[]", "formats": "'{npm}'::text[]",
			"has_repos": "false", "repos": "'{}'::bigint[]", "name_prefix": "''", "exact_name": "''", "exact_format": "''",
			"whole": "'" + whole + "'::bigint[]", "rows": "'{}'::text[]", "q": "''", "q_ids": "'{}'::bigint[]", "q_ranks": "'{}'::integer[]",
			"keyword": "''", "keyword_ids": "'{}'::bigint[]", "signed": "false", "deprecated": "false", "deprecated_ids": "'{}'::bigint[]",
			"depends": "true", "depends_ids": "'" + ids + "'::bigint[]", "has_from": "false", "created_from": "''", "has_to": "false",
			"created_to": "''", "has_after": "false", "after_name": "''", "after_format": "''", "after_time": "''", "after_relevance": "0",
			"any_file": "'{}'::text[]", "document": "'{oci}'::text[]", "walk": "false", "walk_repos": "'{}'::bigint[]",
		})
		versions := testenv.QueryText(t, "package_view.sql", "PackageViewDependentVersions", map[string]string{
			"repos": "'" + whole + "'::bigint[]", "names": "'{lib}'::text[]", "self": "'lib'", "max_rows": "100001",
		})
		users := testenv.QueryText(t, "package_view.sql", "PackageViewDependents", map[string]string{
			"repos": "'" + whole + "'::bigint[]", "names": "'{lib}'::text[]", "self": "'lib'", "withheld": "'{}'::bigint[]",
			"whole": "'" + whole + "'::bigint[]", "rows": "'{}'::text[]", "max_rows": "50",
		})
		for _, s := range []struct{ what, sql string }{{"the list statement with dependsOn", sql}, {"the versions of dependsOn", versions}, {"used by", users}} {
			var lines []string
			st.Query("EXPLAIN (ANALYZE, BUFFERS) "+s.sql, func(rows pgx.Rows) error {
				for rows.Next() {
					var l string
					if err := rows.Scan(&l); err != nil {
						return err
					}
					lines = append(lines, l)
				}
				return rows.Err()
			})
			t.Logf("MEASURE %s, %s:\n%s", s.what, c.who, strings.Join(lines, "\n"))
		}
	}
	id := view.ID("npm", "lib")
	for _, c := range []struct{ who, token string }{{"admin, both repositories", admin}, {"reader of npm-open", reader}} {
		ms := func(d time.Duration) string { return d.Round(100 * time.Microsecond).String() }
		used, usedMax, s1 := median(c.token, "/api/v1/packages/"+id+"/dependents")
		filtered, filteredMax, s2 := median(c.token, "/api/v1/packages?format=npm&dependsOn="+url.QueryEscape("lib")+"&limit=25")
		bare, bareMax, s3 := median(c.token, "/api/v1/packages?format=npm&limit=25")
		t.Logf("MEASURE %s (median, slowest of 12): used by %s, %s (%d); list dependsOn=lib %s, %s (%d); list without the filter %s, %s (%d)",
			c.who, ms(used), ms(usedMax), s1, ms(filtered), ms(filteredMax), s2, ms(bare), ms(bareMax), s3)
	}
}
