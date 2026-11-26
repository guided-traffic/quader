//go:build integration

package search

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// viewStatement reads the text of a statement of the package view from the generated code, with its $n parameters.
func viewStatement(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile("../../../internal/meta/postgres/q/package_view.sql.go")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?s)-- name: " + name + " :many\n(.*?)`").FindSubmatch(src)
	if m == nil {
		t.Fatalf("no statement %s in package_view.sql.go", name)
	}
	return string(m[1])
}

// TestR_API_086_TheReadOfDependentsWalksTheIndexOfTheRepositoriesTheCallerMayRead: the statements of "Used by" and of the list's filter
// dependsOn reach the rows of package_dependencies through package_dependencies_by_name, an index on (repo_id, name), with the
// repositories the caller may read as its first condition (Q-API-458): a repository the caller may not read is not walked. The plans are
// those the server runs (a prepared statement under force_custom_plan, enable_seqscan off as the other plan assertions of the area), on
// 4,000 packages of two repositories of which 400 depend on one name. Integration, area search, rows written by SQL; about a second.
func TestR_API_086_TheReadOfDependentsWalksTheIndexOfTheRepositoriesTheCallerMayRead(t *testing.T) {
	st := testenv.NewStack(t, testenv.StackOptions{Name: "searchdeps"})
	st.Repository("npm-a", "local", "npm", `{}`)
	st.Repository("npm-b", "local", "npm", `{}`)
	st.Exec(`INSERT INTO packages (repo_id, name, display_name)
	         SELECT r.id, 'pkg-' || n, 'pkg-' || n FROM repositories r, generate_series(1, 2000) n WHERE r.key IN ('npm-a', 'npm-b')`)
	key, _ := testenv.VersionKey(t, "npm", "1.0.0")
	st.Exec(`INSERT INTO package_versions (package_id, version, display_version, sort_key, prerelease) SELECT id, '1.0.0', '1.0.0', $1, false FROM packages`, key)
	st.Exec(`INSERT INTO package_dependencies (version_id, repo_id, name, kind, range)
	         SELECT pv.id, p.repo_id, CASE WHEN p.id % 10 = 0 THEN 'lib' ELSE 'other-' || (p.id % 50) END, 'dependencies', '^1.0.0'
	           FROM package_versions pv JOIN packages p ON p.id = pv.package_id`)
	st.Exec(`ANALYZE packages, package_versions, package_dependencies`)
	one := st.Text(`SELECT '{' || id || '}' FROM repositories WHERE key = 'npm-a'`)

	explain := func(t *testing.T, statement, values string) string {
		t.Helper()
		st.Exec(`SET enable_seqscan = off`)
		st.Exec(`SET plan_cache_mode = force_custom_plan`)
		st.Exec(`PREPARE deps_explain AS ` + statement)
		defer func() {
			st.Exec(`DEALLOCATE deps_explain`)
			st.Exec(`RESET plan_cache_mode`)
			st.Exec(`RESET enable_seqscan`)
		}()
		var plan []string
		st.Query(`EXPLAIN (COSTS OFF) EXECUTE deps_explain(`+values+`)`, func(r pgx.Rows) error {
			var line string
			if err := r.Scan(&line); err != nil {
				return err
			}
			plan = append(plan, line)
			return nil
		})
		return strings.Join(plan, "\n")
	}
	for _, c := range []struct{ name, values string }{
		// repos, names, self, withheld, whole, rows, max_rows
		{"PackageViewDependents", `'` + one + `', '{lib}', 'lib', '{}', '` + one + `', '{}', 51`},
		// repos, names, self, max_rows
		{"PackageViewDependentVersions", `'` + one + `', '{lib}', 'lib', 100001`},
	} {
		t.Run("R-API-086 "+c.name+" reads package_dependencies_by_name for the repositories it is given", func(t *testing.T) {
			plan := explain(t, viewStatement(t, c.name), c.values)
			if !strings.Contains(plan, "package_dependencies_by_name") || !regexp.MustCompile(`Index Cond: \(\(.*repo_id = ANY`).MatchString(plan) {
				t.Fatalf("the plan does not walk package_dependencies_by_name by the repositories:\n%s", plan)
			}
		})
	}
}
