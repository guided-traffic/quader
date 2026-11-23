//go:build integration

package packages

import (
	"slices"
	"strconv"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_API_010_APageOfVersionsReadsNoVersionPastItsLimit: a package that one repository holds is listed a page at a time by a keyset
// query (Q-API-417): the page's statement reads no version row past its limit, on the first page and on one deep in the list, and the
// pages the API answers follow one another in the order of publication, a pre-release filter included. 2,000 tiny version rows are
// written by SQL into left-pad, which npm-local alone holds; about 2 s on the stack.
func TestR_API_010_APageOfVersionsReadsNoVersionPastItsLimit(t *testing.T) {
	e := newEnv(t)
	e.seed()
	pkg := e.st.Text(`SELECT p.id::text FROM packages p JOIN repositories r ON r.id = p.repo_id WHERE r.key = 'npm-local' AND p.name = 'left-pad'`)
	// 0.0.1 … 0.0.2000, after the seeded 1.0.0 and the newest the highest; every tenth a pre-release.
	e.st.Exec(`INSERT INTO package_versions (package_id, version, display_version, created_at)
		SELECT $1::bigint, v, v, now() + make_interval(secs => g)
		  FROM generate_series(1, 2000) g, LATERAL (SELECT '0.0.' || g || CASE WHEN g % 10 = 0 THEN '-rc.1' ELSE '' END AS v) x`, pkg)
	e.st.Exec("ANALYZE package_versions")

	args := map[string]string{
		"package_id": pkg, "after_at": "'infinity'", "after_version": "''", "withheld": "'{}'", "whole": "true",
		"allowed": "'{}'", "max_rows": "51",
	}
	first := testenv.QueryText(t, "package_view.sql", "PackageViewVersionPageDesc", args)
	args["after_at"], args["after_version"] = "(now() + interval '1000 seconds')", "'0.0.1000'"
	deep := testenv.QueryText(t, "package_view.sql", "PackageViewVersionPageDesc", args)
	args["after_at"], args["after_version"] = "'-infinity'", "''"
	asc := testenv.QueryText(t, "package_view.sql", "PackageViewVersionPageAsc", args)
	for name, q := range map[string]string{"the first page": first, "a page deep in the list": deep, "the first page oldest first": asc} {
		if n := e.st.Text("SELECT count(*)::text FROM (" + q + ") x"); n != "51" {
			t.Errorf("%s holds %s versions, want 51", name, n)
		}
		if read := e.st.RowsRead(q)["package_versions"]; read > 51 {
			t.Errorf("%s read %v version rows for 51", name, read)
		}
	}

	id := e.idOf(e.tokE, "left-pad")
	base := "/api/v1/packages/" + id + "/versions?limit=50"
	p1 := e.page(e.tokE, base)
	if got := versionsOf(p1.Items); len(got) != 50 || got[0] != "0.0.2000-rc.1" || got[49] != "0.0.1951" || p1.NextCursor == nil {
		t.Fatalf("the first page: %v", got)
	}
	p2 := e.page(e.tokE, base+"&cursor="+*p1.NextCursor)
	if got := versionsOf(p2.Items); len(got) != 50 || got[0] != "0.0.1950-rc.1" || got[49] != "0.0.1901" {
		t.Fatalf("the second page: %v", got)
	}
	pre := e.page(e.tokE, base+"&prerelease=true")
	want := make([]string, 0, 50)
	for g := 2000; len(want) < 50; g -= 10 {
		want = append(want, "0.0."+strconv.Itoa(g)+"-rc.1")
	}
	if got := versionsOf(pre.Items); !slices.Equal(got, want) {
		t.Fatalf("the pre-releases: %v", got)
	}
}
