//go:build integration

package search

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// pageFixture is an installation of 3,067 packages for the reads of a page: pkg-1 … pkg-2000 in npm-a, every third name again in
// npm-b with a second version for every fourth package there, every fifth name in nuget-a (a name of two formats), every seventh
// package of npm-a with its one version in the trash, all written by SQL. It returns the stack and the ids of the three repositories.
func pageFixture(t *testing.T) (*testenv.Stack, string, string, string) {
	t.Helper()
	st := testenv.NewStack(t, testenv.StackOptions{Name: "searchpage"})
	st.Repository("npm-a", "local", "npm", `{}`)
	st.Repository("npm-b", "local", "npm", `{}`)
	st.Repository("nuget-a", "local", "nuget", `{}`)
	st.Exec(`INSERT INTO packages (repo_id, name, display_name)
	         SELECT r.id, 'pkg-' || n, 'pkg-' || n FROM repositories r, generate_series(1, 2000) n
	          WHERE r.key = 'npm-a' OR (r.key = 'npm-b' AND n % 3 = 0) OR (r.key = 'nuget-a' AND n % 5 = 0)`)
	key, _ := testenv.VersionKey(t, "npm", "1.0.0")
	st.Exec(`INSERT INTO package_versions (package_id, version, display_version, sort_key, prerelease, created_at)
	         SELECT id, '1.0.0', '1.0.0', $1, false, now() - (id || ' seconds')::interval FROM packages`, key)
	key2, _ := testenv.VersionKey(t, "npm", "2.0.0")
	st.Exec(`INSERT INTO package_versions (package_id, version, display_version, sort_key, prerelease, created_at)
	         SELECT p.id, '2.0.0', '2.0.0', $1, false, now() FROM packages p JOIN repositories r ON r.id = p.repo_id
	          WHERE r.key = 'npm-b' AND p.id % 4 = 0`, key2)
	st.Exec(`UPDATE package_versions SET trashed_at = now()
	          WHERE package_id IN (SELECT p.id FROM packages p JOIN repositories r ON r.id = p.repo_id
	                                WHERE r.key = 'npm-a' AND substr(p.name, 5)::int % 7 = 0)`)
	st.Exec(`ANALYZE packages, package_versions`)
	id := func(key string) string { return st.Text(`SELECT id::text FROM repositories WHERE key = $1`, key) }
	return st, id("npm-a"), id("npm-b"), id("nuget-a")
}

// TestR_API_029_ThePackageListInTheOrderOfTheNameReadsItsPageAlone: the package list in the order of the name without a filter that
// needs every package's versions reads the page's packages first (the walk of PackageViewList, H-1683) and answers exactly what the
// statement answers without the walk: the same packages in the same order with the same counts, times and downloads, lowest and highest
// first, on the first page and after a cursor, for a caller who reads every repository and for one who reads a repository in part and
// does not get some withheld versions. With the walk it reads a bounded number of package and version rows, a few page lengths, where
// the statement without it reads every one of the 3,067 packages and their versions. The statement is the one the server runs, with its
// values written in (a custom plan, as the service plans it while the walk is asked). Integration, area search; 3,067 tiny rows written
// by SQL, about two seconds.
func TestR_API_029_ThePackageListInTheOrderOfTheNameReadsItsPageAlone(t *testing.T) {
	st, a, b, n := pageFixture(t)
	rows := st.Text(`SELECT array_agg(concat_ws(chr(31), p.repo_id::text, p.name, pv.version))::text
	                   FROM packages p JOIN package_versions pv ON pv.package_id = p.id WHERE p.repo_id = $1 AND p.id % 2 = 0`, b)
	withheld := st.Text(`SELECT array_agg(pv.id)::text FROM packages p JOIN package_versions pv ON pv.package_id = p.id
	                      WHERE p.repo_id = $1 AND substr(p.name, 5)::int % 11 = 0`, a)
	callers := []struct{ name, whole, rows, withheld string }{
		{"a reader of every repository", "{" + a + "," + b + "," + n + "}", "{}", "{}"},
		{"a reader of npm-b in part, without withheld versions", "{" + a + "," + n + "}", rows, withheld},
	}
	pages := []struct {
		name                   string
		desc, after            bool
		afterName, afterFormat string
	}{
		{"the first page, lowest first", false, false, "", ""},
		{"the first page, highest first", true, false, "", ""},
		{"a page after a cursor, lowest first", false, true, "pkg-1500", "npm"},
		{"a page after a cursor, highest first", true, true, "pkg-1500", "nuget"},
	}
	all := "{" + a + "," + b + "," + n + "}"
	for _, c := range callers {
		for _, p := range pages {
			stmt := func(walk bool) string {
				return testenv.QueryText(t, "package_view.sql", "PackageViewList", map[string]string{
					"any_file": "'{deb}'", "document": "'{oci}'", "withheld": "'" + c.withheld + "'::bigint[]", "formats": "'{npm,nuget}'",
					"has_repos": "false", "repos": "'{}'", "name_prefix": "''", "exact_name": "''", "exact_format": "''",
					"whole": "'" + c.whole + "'::bigint[]", "rows": "'" + c.rows + "'::text[]", "q": "''", "q_ids": "'{}'", "q_ranks": "'{}'",
					"signed": "false", "after_relevance": "0", "keyword": "''", "keyword_ids": "'{}'", "has_from": "false", "created_from": "''",
					"has_to": "false", "created_to": "''", "deprecated": "false", "deprecated_ids": "'{}'", "depends": "false", "depends_ids": "'{}'",
					"has_after": fmt.Sprint(p.after), "sort": "'name'", "descending": fmt.Sprint(p.desc), "after_time": "''",
					"after_name": "'" + p.afterName + "'", "after_format": "'" + p.afterFormat + "'", "max_rows": "51",
					"walk": fmt.Sprint(walk), "walk_repos": "'" + all + "'::bigint[]",
				})
			}
			t.Run("R-API-029 "+c.name+", "+p.name, func(t *testing.T) {
				want, got := answer(st, stmt(false)), answer(st, stmt(true))
				if len(want) != 51 {
					t.Fatalf("the statement without the walk answered %d rows, want 51: %v", len(want), want)
				}
				if strings.Join(got, "\n") != strings.Join(want, "\n") {
					t.Fatalf("the walk answered\n%s\nwithout it\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
				}
				whole := st.RowsRead(stmt(false))
				read := st.RowsRead(stmt(true))
				if whole["packages"] < 3000 || read["packages"] > 600 || read["package_versions"] > 600 {
					t.Fatalf("rows read with the walk: %v packages, %v versions; without it %v, %v",
						read["packages"], read["package_versions"], whole["packages"], whole["package_versions"])
				}
			})
		}
	}
}

// TestR_API_064_APackageStatementChecksAServedVersionPerCandidate: statements A and B of the suggestions check a candidate's served
// version per candidate (the EXISTS with OFFSET 0 in search.sql, a subplan in the plan): a text that few names start with or that no
// name's word begins with reads a version row per candidate and no others, where the check flattened into a join read every version of
// the installation and every package by its id first (about 100 ms with 50,000 packages, whatever the text). The statements are the ones the server runs
// with their values written in, a custom plan as SearchCustomPlans makes it. Integration, area search; the 3,067 tiny rows of
// pageFixture, about a second.
func TestR_API_064_APackageStatementChecksAServedVersionPerCandidate(t *testing.T) {
	st, a, b, n := pageFixture(t)
	repos := "'{" + a + "," + b + "," + n + "}'::bigint[]"
	prefix := testenv.QueryText(t, "search.sql", "SearchPackagesByPrefix", map[string]string{
		"repos": repos, "low": "'pkg-1995'", "high": "'pkg-1996'", "withheld": "'{}'::bigint[]", "max_rows": "200",
	})
	words := testenv.QueryText(t, "search.sql", "SearchPackagesByWords", map[string]string{
		"repos": repos, "query": `'''zzz'':*'`, "skip": "'{}'::bigint[]", "withheld": "'{}'::bigint[]", "max_rows": "200",
	})
	for _, c := range []struct {
		name, sql string
		found     string
	}{
		{"statement A, a name of three repositories, one of them with its version in the trash", prefix, "2"},
		{"statement B, a word no name holds", words, "0"},
	} {
		t.Run("R-API-064 "+c.name, func(t *testing.T) {
			if got := st.Text("SELECT count(*)::text FROM (" + c.sql + ") x"); got != c.found {
				t.Fatalf("found %s packages, want %s", got, c.found)
			}
			if read := st.RowsRead(c.sql); read["package_versions"] > 10 || read["packages"] > 10 {
				t.Fatalf("read %v version rows and %v package rows", read["package_versions"], read["packages"])
			}
			// The check stays a check per candidate whatever the planner estimates: on this small table a flattened join reads few rows
			// as well, at 50,000 packages it read all of them.
			var plan []string
			st.Query("EXPLAIN (COSTS OFF) "+c.sql, func(r pgx.Rows) error {
				var line string
				if err := r.Scan(&line); err != nil {
					return err
				}
				plan = append(plan, line)
				return nil
			})
			if text := strings.Join(plan, "\n"); !strings.Contains(text, "EXISTS(SubPlan") || strings.Contains(text, "Semi Join") {
				t.Fatalf("the served version is not checked per candidate:\n%s", text)
			}
		})
	}
}

// answer runs a statement and returns its rows, each as the text of its values, in the statement's order.
func answer(st *testenv.Stack, sql string) []string {
	var out []string
	st.Query(sql, func(r pgx.Rows) error {
		v, err := r.Values()
		if err != nil {
			return err
		}
		out = append(out, fmt.Sprint(v...))
		return nil
	})
	return out
}
