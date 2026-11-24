//go:build integration

package packages

import (
	"fmt"
	"slices"
	"strconv"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_API_010_APageOfVersionsReadsNoVersionPastItsLimit: a package that one repository holds is listed a page at a time by a keyset
// query, in the order of publication (Q-API-417) and in the order of the versions on the stored sort key (Q-API-429): each page's
// statement reads no version row past its limit, on the first page and on one deep in the list, and the pages the API answers follow
// one another, a pre-release filter included. The package list's latest version reads one version row per kind, and its downloads run
// no subplan per version (Q-API-429). 2,000 tiny version rows are written by SQL into left-pad, which npm-local alone holds; about 2 s
// on the stack.
func TestR_API_010_APageOfVersionsReadsNoVersionPastItsLimit(t *testing.T) {
	e := newEnv(t)
	e.seed()
	pkg := e.st.Text(`SELECT p.id::text FROM packages p JOIN repositories r ON r.id = p.repo_id WHERE r.key = 'npm-local' AND p.name = 'left-pad'`)
	// 0.0.1 … 0.0.2000, after the seeded 1.0.0 and the newest the highest; every tenth a pre-release.
	e.versions(pkg, 2000, func(g int) string {
		if g%10 == 0 {
			return "0.0." + strconv.Itoa(g) + "-rc.1"
		}
		return "0.0." + strconv.Itoa(g)
	})
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

	keyArgs := map[string]string{
		"package_id": pkg, "after_key": `'\x02'::bytea`, "withheld": "'{}'", "whole": "true", "allowed": "'{}'", "max_rows": "51",
	}
	kFirst := testenv.QueryText(t, "package_view.sql", "PackageViewVersionKeyDesc", keyArgs)
	mid, _ := e.st.VersionKey("npm", "0.0.1000-rc.1")
	keyArgs["after_key"] = fmt.Sprintf(`'\x%x'::bytea`, mid)
	kDeep := testenv.QueryText(t, "package_view.sql", "PackageViewVersionKeyDesc", keyArgs)
	keyArgs["after_key"] = `''::bytea`
	kAsc := testenv.QueryText(t, "package_view.sql", "PackageViewVersionKeyAsc", keyArgs)
	for name, q := range map[string]string{"the first page by version": kFirst, "a page by version deep in the list": kDeep, "the first page by version, lowest first": kAsc} {
		if n := e.st.Text("SELECT count(*)::text FROM (" + q + ") x"); n != "51" {
			t.Errorf("%s holds %s versions, want 51", name, n)
		}
		if read := e.st.RowsRead(q)["package_versions"]; read > 51 {
			t.Errorf("%s read %v version rows for 51", name, read)
		}
	}
	repo := e.st.Text(`SELECT id::text FROM repositories WHERE key = 'npm-local'`)
	latest := testenv.QueryText(t, "package_view.sql", "PackageViewLatest", map[string]string{
		"withheld": "'{}'", "whole": "'{" + repo + "}'", "rows": "'{}'", "packages": "ARRAY['npm' || chr(31) || 'left-pad']",
		"scope": "'{" + repo + "}'", "has_repos": "false", "repos": "'{}'",
	})
	if read := e.st.RowsRead(latest)["package_versions"]; read > 2 {
		t.Errorf("the latest version read %v version rows, want one release and one pre-release", read)
	}
	list := testenv.QueryText(t, "package_view.sql", "PackageViewList", map[string]string{
		"any_file": "'{deb}'", "document": "'{oci}'", "withheld": "'{}'", "formats": "'{npm}'", "has_repos": "true", "repos": "'{" + repo + "}'",
		"name_prefix": "''", "exact_name": "''", "exact_format": "''", "whole": "'{" + repo + "}'", "rows": "'{}'", "q": "''",
		"q_ids": "'{}'", "keyword": "''", "keyword_ids": "'{}'", "has_from": "false", "created_from": "''", "has_to": "false",
		"created_to": "''", "has_after": "false", "sort": "'name'", "descending": "false", "after_time": "''", "after_name": "''",
		"after_format": "''", "max_rows": "51",
	})
	if loops := e.st.SubPlanLoops(list); loops > 51 {
		t.Errorf("the package list ran a subplan %v times over 2,001 versions", loops)
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
	// In the order of the versions: the seeded 1.0.0 is the highest, then 0.0.2000-rc.1, 0.0.1999 and down.
	v1 := e.page(e.tokE, base+"&sort=-version")
	if got := versionsOf(v1.Items); len(got) != 50 || got[0] != "1.0.0" || got[1] != "0.0.2000-rc.1" || got[49] != "0.0.1952" || v1.NextCursor == nil {
		t.Fatalf("the first page by version: %v", got)
	}
	v2 := e.page(e.tokE, base+"&sort=-version&cursor="+*v1.NextCursor)
	if got := versionsOf(v2.Items); len(got) != 50 || got[0] != "0.0.1951" || got[49] != "0.0.1902" {
		t.Fatalf("the second page by version: %v", got)
	}
	if got := versionsOf(e.page(e.tokE, base+"&sort=version").Items); len(got) != 50 || got[0] != "0.0.1" || got[49] != "0.0.50-rc.1" {
		t.Fatalf("the first page by version, lowest first: %v", got)
	}
	if lp := pkgByName(e.page(e.tokE, "/api/v1/packages?repository=it/npm-local").Items, "left-pad"); lp == nil || lp["latestVersion"] != "1.0.0" || num(lp["versionCount"]) != 2001 {
		t.Fatalf("the package list: %v", lp)
	}
}
