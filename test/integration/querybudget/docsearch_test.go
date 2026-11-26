//go:build integration

package querybudget

import (
	"maps"
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_ANS_058_TheDocumentationSearchCostsAFixedNumberOfStatements: a search of the documentation across collections runs the same
// statements, each as often, with 10 and with 10,000 entries in 20 collections (R-ANS-058, Q-ANS-127): the newest versions are one
// statement, the entries one, and nothing runs per collection or per entry. The rows are written directly: 20 collections of one version
// each, every one with 500 modules whose names and short descriptions hold the text, in the end.
func TestR_ANS_058_TheDocumentationSearchCostsAFixedNumberOfStatements(t *testing.T) {
	st := testenv.NewStack(t, testenv.StackOptions{Name: "querybudget"})
	st.Repository("ansible-local", "local", "ansible", `{}`)
	ada := st.User("ada", "budget-leads")
	st.Bind("admin", ada)
	tok := st.Token(ada, scope.Scope{Inherit: true, Admin: true})
	key, pre := st.VersionKey("ansible", "1.0.0")
	st.Exec(`INSERT INTO packages (repo_id, name, display_name)
	         SELECT r.id, 'acme.c' || n, 'acme.c' || n FROM repositories r, generate_series(1, 20) n WHERE r.key = 'ansible-local'`)
	st.Exec(`INSERT INTO package_versions (package_id, version, display_version, sort_key, prerelease)
	         SELECT p.id, '1.0.0', '1.0.0', $1, $2 FROM packages p`, key, pre)
	st.Exec(`INSERT INTO ansible_version_docs (repo_id, name, version, source, generator, entry_count, total_bytes)
	         SELECT p.repo_id, p.name, '1.0.0', 'generated', 1, 500, 0 FROM packages p`)
	seed := func(from, to int) {
		st.Exec(`INSERT INTO ansible_doc_entries (repo_id, name, version, content_type, content_name, short_description, has_docs, search_text)
		         SELECT p.repo_id, p.name, '1.0.0', 'module', 'budget_' || n, 'Budget module ' || n, false, 'budget_' || n || ' budget module'
		           FROM packages p, generate_series($1::int, $2::int) n
		          WHERE ($2::int - $1::int) >= 499 OR p.name = 'acme.c1'`, from, to)
	}
	seed(1, 10)
	rt := testenv.NewRequestTransactions()
	r := st.Start(t, testenv.ReplicaOptions{Name: "web-docs", Database: []postgres.Option{rt.Option()}})
	const path = "/api/v1/search/documentation?q=budget&limit=25"

	measure := func(t *testing.T) (map[string]int, int, time.Duration) {
		t.Helper()
		if a := st.Do(r, "GET", path, tok, nil); a.Status != http.StatusOK {
			t.Fatalf("warm-up: %d %s", a.Status, a.Body)
		}
		var rounds []map[string]int
		var took time.Duration
		for range 3 {
			rt.Count(true)
			t0 := time.Now()
			a := st.Do(r, "GET", path, tok, nil)
			took = time.Since(t0)
			rt.Count(false)
			if a.Status != http.StatusOK || len(a.JSON(t)["items"].([]any)) == 0 {
				t.Fatalf("%d %s", a.Status, a.Body)
			}
			rounds = append(rounds, rt.Of(a.Header.Get("X-Request-Id")).Statements())
		}
		few, total := leastOf(rounds)
		return few, total, took
	}
	few, fewTotal, fewTook := measure(t)
	st.Exec(`DELETE FROM ansible_doc_entries`)
	seed(1, 500)
	st.Exec(`ANALYZE ansible_doc_entries`)
	if n := st.Number(`SELECT count(*) FROM ansible_doc_entries`); n != 10_000 {
		t.Fatalf("seeded %d entries", n)
	}
	many, manyTotal, manyTook := measure(t)
	t.Logf("10 entries: %d statements (%s), %s; 10,000 entries: %d statements (%s), %s", fewTotal, describe(few), fewTook, manyTotal, describe(many), manyTook)
	if !maps.Equal(few, many) {
		t.Errorf("R-ANS-058 the statements differ: with 10 entries %s, with 10,000 %s", describe(few), describe(many))
	}
	for _, name := range []string{"AnsibleDocsCandidates", "PackageViewLatestOfPackages", "AnsibleDocsSearch"} {
		if many[name] != 1 {
			t.Errorf("R-ANS-058 %s ran %d times, want once", name, many[name])
		}
	}
}
