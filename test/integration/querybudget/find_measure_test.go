//go:build integration

package querybudget

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestMeasureTheSearchOfCollections is a measurement, not a test (CLAUDE.md, "Tests stay runnable"; formats/ansible.md R-ANS-057,
// R-ANS-058, H-2154): the find of the package list's text over Ansible Galaxy collections (AnsibleViewFind), the list and the
// suggestions that ask it, with 1,000, 10,000 and 50,000 collections, and the documentation searched across collections with 200,000
// entries (2,000 collections of 100 entries). It runs only with QUADER_TEST_MEASURE_FIND set and writes the statements' times and plans
// to the log, which formats/ansible.md and docs/developer/packages.md keep:
//
//	QUADER_TEST_MEASURE_FIND=1 test/integration/run.sh -run TestMeasureTheSearchOfCollections -v ./test/integration/querybudget/
//
// The rows are tiny and written directly: one version per collection, a description of a few words, two tags; one collection in a
// hundred holds the word the measurement searches ("docker"), and one entry of one collection in ten of the documentation (200 of
// 200,000 entries); "widget" and "manage" are words of nearly every row.
func TestMeasureTheSearchOfCollections(t *testing.T) {
	if os.Getenv("QUADER_TEST_MEASURE_FIND") == "" {
		t.Skip("a measurement: set QUADER_TEST_MEASURE_FIND=1")
	}
	st := testenv.NewStack(t, testenv.StackOptions{Name: "querybudget"})
	st.Repository("ansible-local", "local", "ansible", `{}`)
	ada := st.User("ada", "budget-leads")
	st.Bind("admin", ada)
	tok := st.Token(ada, scope.Scope{Inherit: true, Admin: true})
	key, pre := st.VersionKey("ansible", "1.0.0")
	r := st.Start(t, testenv.ReplicaOptions{Name: "web-measure"})

	seed := func(from, to int) {
		st.Exec(`INSERT INTO items (repo_id, path, parent_path, kind, explicit_folder)
		         SELECT r.id, 'c' || n, '', 'folder', true FROM repositories r, generate_series($1::int, $2::int) n WHERE r.key = 'ansible-local'`, from, to)
		st.Exec(`INSERT INTO packages (repo_id, name, display_name)
		         SELECT r.id, 'ns' || (n % 500) || '.coll' || n, 'ns' || (n % 500) || '.coll' || n
		           FROM repositories r, generate_series($1::int, $2::int) n WHERE r.key = 'ansible-local'`, from, to)
		st.Exec(`INSERT INTO package_versions (package_id, version, display_version, sort_key, prerelease)
		         SELECT p.id, '1.0.0', '1.0.0', $1, $2 FROM packages p
		          WHERE NOT EXISTS (SELECT 1 FROM package_versions v WHERE v.package_id = p.id)`, key, pre)
		st.Exec(`INSERT INTO ansible_collection_versions (repo_id, name, version, sort_key, item_id, sha256, size, collection_info, published_by)
		         SELECT r.id, 'ns' || (n % 500) || '.coll' || n, '1.0.0', $3, i.id, sha256(n::text::bytea), 1,
		                jsonb_build_object('description', CASE WHEN n % 100 = 7 THEN 'Run docker containers on host ' || n ELSE 'Manage widget ' || n || ' of the hosts' END,
		                                   'tags', jsonb_build_array('linux', CASE WHEN n % 100 = 7 THEN 'containers' ELSE 'acme' END)),
		                'measure'
		           FROM repositories r CROSS JOIN generate_series($1::int, $2::int) n JOIN items i ON i.repo_id = r.id AND i.path = 'c' || n
		          WHERE r.key = 'ansible-local'`, from, to, key)
		st.Exec(`ANALYZE items, packages, package_versions, ansible_collection_versions`)
	}
	plan := func(sql string) (string, float64) {
		t.Helper()
		var lines []string
		st.Query("EXPLAIN (ANALYZE, BUFFERS) "+sql, func(rows pgx.Rows) error {
			for rows.Next() {
				var l string
				if err := rows.Scan(&l); err != nil {
					return err
				}
				lines = append(lines, l)
			}
			return rows.Err()
		})
		ms := 0.0
		for _, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "Execution Time:") {
				ms = parseMs(l)
			}
		}
		return strings.Join(lines, "\n"), ms
	}
	median := func(path string) (time.Duration, int) {
		t.Helper()
		var d []time.Duration
		status := 0
		st.Do(r, "GET", path, tok, nil)
		for range 7 {
			t0 := time.Now()
			a := st.Do(r, "GET", path, tok, nil)
			d = append(d, time.Since(t0))
			status = a.Status
		}
		slices.Sort(d)
		return d[len(d)/2], status
	}
	find := func(query string) string {
		return testenv.QueryText(t, "ansible_local.sql", "AnsibleViewFind", map[string]string{
			"repos": "ARRAY['" + st.Text(`SELECT p.key || '/' || r.key FROM repositories r JOIN project p ON p.id = r.project_id WHERE r.key = 'ansible-local'`) + "']",
			"query": "'" + strings.ReplaceAll(query, "'", "''") + "'", "keyword": "''", "row_limit": "10001",
		})
	}

	done := 0
	for _, n := range []int{1_000, 10_000, 50_000} {
		seed(done+1, n)
		done = n
		rare, rareMs := plan(find(`'docker':*`))
		_, commonMs := plan(find(`'widget':*`))
		list, listStatus := median("/api/v1/packages?format=ansible&q=docker&sort=relevance&limit=25")
		common, commonStatus := median("/api/v1/packages?format=ansible&q=widget&sort=relevance&limit=25")
		bare, _ := median("/api/v1/packages?format=ansible&limit=25")
		sug, sugStatus := median("/api/v1/search/suggestions?q=docker&kind=repository,package,project,documentation")
		t.Logf("MEASURE %d collections: find 'docker' %.1f ms, find 'widget' %.1f ms; list without q %s, q=docker %s (%d), q=widget %s (%d); suggestions %s (%d)\n%s",
			n, rareMs, commonMs, bare.Round(100*time.Microsecond), list.Round(100*time.Microsecond), listStatus, common.Round(100*time.Microsecond), commonStatus,
			sug.Round(100*time.Microsecond), sugStatus, rare)
	}

	// The documentation of 2,000 collections, 100 entries each.
	st.Exec(`INSERT INTO ansible_version_docs (repo_id, name, version, source, generator, entry_count, total_bytes)
	         SELECT p.repo_id, p.name, '1.0.0', 'generated', 1, 100, 0 FROM packages p
	          WHERE p.name IN (SELECT name FROM packages ORDER BY id LIMIT 2000)`)
	st.Exec(`INSERT INTO ansible_doc_entries (repo_id, name, version, content_type, content_name, short_description, has_docs, search_text)
	         SELECT d.repo_id, d.name, '1.0.0', 'module', CASE WHEN k = 7 AND d.name LIKE '%7' THEN 'docker_' || k ELSE 'mod_' || k END,
	                'Manage thing ' || k, false,
	                CASE WHEN k = 7 AND d.name LIKE '%7' THEN 'docker_' || k || ' docker container' ELSE 'mod_' || k || ' manage thing' END
	           FROM ansible_version_docs d, generate_series(1, 100) k`)
	st.Exec(`ANALYZE ansible_version_docs, ansible_doc_entries`)
	if n := st.Number(`SELECT count(*) FROM ansible_doc_entries`); n != 200_000 {
		t.Fatalf("seeded %d entries", n)
	}
	arr := func(col, cast string) string {
		return "(SELECT array_agg(" + col + " ORDER BY name)::" + cast + " FROM ansible_version_docs)"
	}
	docs := func(text string) string {
		return testenv.QueryText(t, "ansible_docs.sql", "AnsibleDocsSearch", map[string]string{
			"repo_ids": arr("repo_id", "bigint[]"), "names": arr("name", "text[]"), "versions": arr("version", "text[]"),
			"text": "'" + text + "'", "query": tsq(text), "has_after": "false", "after_tier": "0", "after_entry": "''",
			"after_name": "''", "after_repo": "0", "after_type": "''", "max_rows": "26",
		})
	}
	cands := func(text string) string {
		return testenv.QueryText(t, "ansible_docs.sql", "AnsibleDocsCandidates", map[string]string{
			"query": tsq(text), "repo_ids": "ARRAY[(SELECT id FROM repositories WHERE key = 'ansible-local')]", "max_rows": "20001",
		})
	}
	cp, c1 := plan(cands("docker"))
	_, c2 := plan(cands("manage"))
	dp, dms := plan(docs("docker"))
	_, cms := plan(docs("manage"))
	dl, dlStatus := median("/api/v1/search/documentation?q=docker&limit=25")
	ds, dsStatus := median("/api/v1/search/suggestions?q=docker&kind=documentation")
	dc, dcStatus := median("/api/v1/search/suggestions?q=do&kind=repository,package,project,documentation")
	for _, kind := range []string{"package", "documentation", "repository,project"} {
		for _, q := range []string{"do", "doc", "docker", "ma", "manage"} {
			d, status := median("/api/v1/search/suggestions?q=" + q + "&kind=" + kind)
			t.Logf("MEASURE suggestions kind=%s q=%s: %s (%d)", kind, q, d.Round(100*time.Microsecond), status)
		}
	}
	t.Logf("MEASURE documentation, 200,000 entries of 2,000 collections in 50,000: the candidates 'docker' %.1f ms, 'manage' %.1f ms; "+
		"the entries of 2,000 versions 'docker' %.1f ms, 'manage' %.1f ms; list %s (%d); suggestions 'docker' %s (%d), 'do' %s (%d)\n%s\n%s",
		c1, c2, dms, cms, dl.Round(100*time.Microsecond), dlStatus, ds.Round(100*time.Microsecond), dsStatus,
		dc.Round(100*time.Microsecond), dcStatus, cp, dp)
}

// tsq is the to_tsquery text of one word as an SQL literal: 'w':*.
func tsq(word string) string { return `'''` + word + `'':*'` }

func parseMs(line string) float64 {
	f := strings.Fields(line)
	for i, w := range f {
		if w == "Time:" && i+1 < len(f) {
			v, _ := strconv.ParseFloat(f[i+1], 64)
			return v
		}
	}
	return 0
}
