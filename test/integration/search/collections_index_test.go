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

// generatedStatement returns the text of a sqlc statement as the server prepares it, from the generated file of its query file.
func generatedStatement(t *testing.T, file, name string) string {
	t.Helper()
	src, err := os.ReadFile("../../../internal/meta/postgres/q/" + file + ".go")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?s)-- name: " + name + " :many\n(.*?)`").FindSubmatch(src)
	if m == nil {
		t.Fatalf("no statement %s in %s.go", name, file)
	}
	return string(m[1])
}

// TestR_ANS_057_R_ANS_058_TheSearchOfCollectionsAndOfTheirDocumentationUsesItsIndexes: the find of collections reads
// ansible_collection_versions_search (migration 00138, whose expression the statement repeats word for word) and the first step of the
// documentation searched across collections reads ansible_doc_entries_search, in a custom and in a generic plan, as the server's prepared
// statements run them, with enable_seqscan off as the search's plan tests state it, on 3,000 collections of which few match. The
// measurement of H-2154 gives the times with 50,000 (formats/ansible.md R-ANS-057).
func TestR_ANS_057_R_ANS_058_TheSearchOfCollectionsAndOfTheirDocumentationUsesItsIndexes(t *testing.T) {
	st := testenv.NewStack(t, testenv.StackOptions{Name: "searchindex"})
	st.Repository("ansible-local", "local", "ansible", `{}`)
	key, pre := testenv.VersionKey(t, "ansible", "1.0.0")
	st.Exec(`INSERT INTO items (repo_id, path, parent_path, kind, explicit_folder)
	         SELECT r.id, 'c' || n, '', 'folder', true FROM repositories r, generate_series(1, 3000) n WHERE r.key = 'ansible-local'`)
	st.Exec(`INSERT INTO packages (repo_id, name, display_name)
	         SELECT r.id, 'ns' || (n % 50) || '.coll' || n, 'ns' || (n % 50) || '.coll' || n
	           FROM repositories r, generate_series(1, 3000) n WHERE r.key = 'ansible-local'`)
	st.Exec(`INSERT INTO package_versions (package_id, version, display_version, sort_key, prerelease) SELECT id, '1.0.0', '1.0.0', $1, $2 FROM packages`, key, pre)
	st.Exec(`INSERT INTO ansible_collection_versions (repo_id, name, version, sort_key, item_id, sha256, size, collection_info, published_by)
	         SELECT r.id, 'ns' || (n % 50) || '.coll' || n, '1.0.0', $1, i.id, sha256(n::text::bytea), 1,
	                jsonb_build_object('description', CASE WHEN n % 300 = 7 THEN 'Run docker containers' ELSE 'Manage widget ' || n END,
	                                   'tags', jsonb_build_array('linux')), 'index-test'
	           FROM repositories r CROSS JOIN generate_series(1, 3000) n JOIN items i ON i.repo_id = r.id AND i.path = 'c' || n
	          WHERE r.key = 'ansible-local'`, key)
	st.Exec(`INSERT INTO ansible_version_docs (repo_id, name, version, source, generator, entry_count, total_bytes)
	         SELECT repo_id, name, '1.0.0', 'generated', 1, 20, 0 FROM packages`)
	st.Exec(`INSERT INTO ansible_doc_entries (repo_id, name, version, content_type, content_name, has_docs, search_text)
	         SELECT d.repo_id, d.name, '1.0.0', 'module', 'mod_' || k, false,
	                CASE WHEN k = 7 AND d.name LIKE '%7' THEN 'docker_run docker' ELSE 'mod_' || k || ' manage thing' END
	           FROM ansible_version_docs d, generate_series(1, 20) k`)
	st.Exec(`ANALYZE`)

	explain := func(t *testing.T, mode, statement, values string) string {
		t.Helper()
		st.Exec(`SET enable_seqscan = off`)
		st.Exec(`SET plan_cache_mode = ` + mode)
		st.Exec(`PREPARE collections_explain AS ` + statement)
		defer func() {
			st.Exec(`DEALLOCATE collections_explain`)
			st.Exec(`RESET plan_cache_mode`)
			st.Exec(`RESET enable_seqscan`)
		}()
		var plan []string
		st.Query(`EXPLAIN (COSTS OFF) EXECUTE collections_explain(`+values+`)`, func(r pgx.Rows) error {
			var line string
			if err := r.Scan(&line); err != nil {
				return err
			}
			plan = append(plan, line)
			return nil
		})
		return strings.Join(plan, "\n")
	}
	repoKey := st.Text(`SELECT p.key || '/' || r.key FROM repositories r JOIN project p ON p.id = r.project_id WHERE r.key = 'ansible-local'`)
	repoID := st.Text(`SELECT id::text FROM repositories WHERE key = 'ansible-local'`)
	for _, c := range []struct {
		name, file, statement, index, values string
	}{
		{"the find of collections", "ansible_local.sql", "AnsibleViewFind", "ansible_collection_versions_search", `10001, '''docker'':*', '{` + repoKey + `}', ''`},
		{"the documentation's candidates", "ansible_docs.sql", "AnsibleDocsCandidates", "ansible_doc_entries_search", `'''docker'':*', '{` + repoID + `}', 20001`},
	} {
		text := generatedStatement(t, c.file, c.statement)
		for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
			t.Run(c.name+" reads "+c.index+" in a "+strings.TrimPrefix(mode, "force_"), func(t *testing.T) {
				if plan := explain(t, mode, text, c.values); !strings.Contains(plan, c.index) {
					t.Fatalf("the plan does not use %s:\n%s", c.index, plan)
				}
			})
		}
	}
}
