//go:build integration

package ansibleremote

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/ansiblestub"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// installRole makes the requests of `ansible-galaxy role install geerlingguy.pip,2.0.0`: the lookup, the versions, the archive.
func (e *aenv) installRole(r *testenv.Replica, repo string) {
	e.t.Helper()
	for _, p := range []string{"v1/roles/?owner__username=geerlingguy&name=pip", "v1/roles/16/versions/?page_size=50", "v1/roles/archives/16/2.0.0.tar.gz"} {
		if a := e.get(r, repo, p); a.Status != http.StatusOK {
			e.t.Fatalf("%s: %d %.300s", p, a.Status, a.Body)
		}
	}
}

// TestR_ANS_056_ARoleInstalledThroughARemoteReadsLikeARole: a role installed through a remote is listed by its owner and name with the
// upstream's description, its page shows its tags, its GitHub repository, the upstream's download count and the name rules see, and its
// README is the upstream's HTML, fetched once by the job and served sanitized. Data: the stub's role geerlingguy.pip, an archive of a
// few hundred bytes and a README of about 600 bytes.
func TestR_ANS_056_ARoleInstalledThroughARemoteReadsLikeARole(t *testing.T) {
	e := newEnv(t, options{})
	r := e.jobsReplica()
	repo := e.fresh()
	e.installRole(r, repo)
	eventually(t, 30*time.Second, "the README job", func() bool {
		return e.st.Text(`SELECT readme_state FROM ansible_roles WHERE repo_id = $1 AND role_id = 16`, e.repoID(repo)) == "available"
	})

	list := e.st.Do(r, http.MethodGet, "/api/v1/packages?format=ansible", e.admin, nil)
	items := listOf(list.JSON(t)["items"])
	if list.Status != http.StatusOK || len(items) != 1 || obj(items[0])["name"] != "geerlingguy.pip" ||
		obj(items[0])["description"] != "Pip (Python package manager) for Linux." {
		t.Fatalf("list: %d %s", list.Status, list.Body)
	}
	// The search finds the role by its owner, and the summary names the key its page is addressed by.
	found := listOf(e.st.Do(r, http.MethodGet, "/api/v1/packages?format=ansible&q=geerlingguy", e.admin, nil).JSON(t)["items"])
	if len(found) != 1 || obj(found[0])["key"] != "role/16" {
		t.Errorf("search by the owner: %v", found)
	}
	// The find of the format matches the role's words as a collection's (R-ANS-057): a word that begins a word of its description and a
	// tag, by relevance; the key has no namespace, so the namespace of its display name lists nothing.
	byWords := listOf(e.st.Do(r, http.MethodGet, "/api/v1/packages?format=ansible&q=manag+devel&sort=relevance", e.admin, nil).JSON(t)["items"])
	if len(byWords) != 1 || obj(byWords[0])["key"] != "role/16" {
		t.Errorf("search by the words of the description and a tag: %v", byWords)
	}
	if ns := listOf(e.st.Do(r, http.MethodGet, "/api/v1/packages?format=ansible&namespace=geerlingguy", e.admin, nil).JSON(t)["items"]); len(ns) != 0 {
		t.Errorf("the namespace of the display name: %v", ns)
	}
	// The search field's suggestion carries the key as well, so its link leads to the page (Q-UI-1261).
	suggested := obj(e.st.Do(r, http.MethodGet, "/api/v1/search/suggestions?q=geerlingguy&kind=package", e.admin, nil).JSON(t)["packages"])
	if s := listOf(suggested["items"]); len(s) != 1 || obj(s[0])["name"] != "geerlingguy.pip" || obj(s[0])["key"] != "role/16" {
		t.Errorf("suggestion of the owner: %v", suggested)
	}
	lookup := e.st.Do(r, http.MethodGet, "/api/v1/packages/lookup?format=ansible&name=role/16", e.admin, nil)
	id, _ := lookup.JSON(t)["id"].(string)
	if lookup.Status != http.StatusOK || id == "" {
		t.Fatalf("lookup: %d %s", lookup.Status, lookup.Body)
	}
	d := e.st.Do(r, http.MethodGet, "/api/v1/packages/"+id+"/versions/2.0.0", e.admin, nil)
	detail := d.JSON(t)
	md := obj(detail["metadata"])
	role := obj(md["role"])
	if d.Status != http.StatusOK || role["owner"] != "geerlingguy" || role["name"] != "pip" || role["packageName"] != "role/16" ||
		role["upstreamDownloads"] != float64(1234) || role["readme"] != "available" ||
		md["repositoryUrl"] != "https://github.com/geerlingguy/ansible-role-pip" || detail["hasReadme"] != true {
		t.Fatalf("details: %d %s", d.Status, d.Body)
	}
	var keywords []string
	for _, k := range listOf(obj(items[0])["keywords"]) {
		keywords = append(keywords, k.(string))
	}
	if !slices.Equal(keywords, []string{"python", "pip", "development"}) {
		t.Errorf("keywords = %v", keywords)
	}
	rd := e.st.Do(r, http.MethodGet, "/api/v1/packages/"+id+"/versions/2.0.0/readme", e.admin, nil)
	html, _ := rd.JSON(t)["html"].(string)
	if rd.Status != http.StatusOK || rd.JSON(t)["contentType"] != "text/html" || !strings.Contains(html, "Ansible Role: Pip") ||
		!strings.Contains(html, "<table>") || !strings.Contains(html, "pip_package") {
		t.Fatalf("readme: %d %s", rd.Status, rd.Body)
	}
	for _, bad := range []string{"<script", "onerror", "alert("} {
		if strings.Contains(html, bad) {
			t.Errorf("the README holds %q: %s", bad, html)
		}
	}
	if n := e.stub.Counts().OfKind(ansiblestub.ClassRoleContent, http.MethodGet); n != 1 {
		t.Errorf("the stub saw %d requests for the role's content, want 1", n)
	}

	// A second install of another version fetches no README again: the role's commit is the same.
	if a := e.get(r, repo, "v1/roles/archives/16/1.1.0.tar.gz"); a.Status != http.StatusOK {
		t.Fatalf("1.1.0: %d", a.Status)
	}
	if v := e.st.Number(`SELECT count(*) FROM package_versions pv JOIN packages p ON p.id = pv.package_id
		WHERE p.repo_id = $1 AND p.name = 'role/16' AND pv.trashed_at IS NULL`, e.repoID(repo)); v != 2 {
		t.Errorf("%d versions of the role, want 2", v)
	}
	time.Sleep(500 * time.Millisecond)
	if n := e.stub.Counts().OfKind(ansiblestub.ClassRoleContent, http.MethodGet); n != 1 {
		t.Errorf("the stub saw %d requests for the role's content after a second version, want 1", n)
	}

	// A rule that blocks the role by the name the rules see makes its cached version undelivered, with the reason, on its page
	// (R-CUR-014, Q-UI-1260): the details decide by role/16, not by the display name.
	e.st.MustPutDeliveryRule(r, e.admin, repo, testenv.PackageRule("role/16", "block"))
	eventually(t, 10*time.Second, "the rule on the version's page", func() bool {
		held := listOf(e.st.Do(r, http.MethodGet, "/api/v1/packages/"+id+"/versions/2.0.0", e.admin, nil).JSON(t)["repositories"])
		return len(held) == 1 && obj(held[0])["available"] == false && obj(held[0])["unavailableReason"] == "blocked"
	})
}

// TestR_ANS_056_ARoleCachedBeforeItsFactsIsNamedAtItsNextLookup: a role whose archive was cached when the remote held no facts of it is
// listed as role/<id>, and the next lookup gives it its owner and name and fetches its README (the state of a role cached before the
// facts were kept). Data: as above.
func TestR_ANS_056_ARoleCachedBeforeItsFactsIsNamedAtItsNextLookup(t *testing.T) {
	e := newEnv(t, options{})
	r := e.jobsReplica()
	repo := e.fresh()
	e.installRole(r, repo)
	e.st.Exec(`DELETE FROM ansible_roles WHERE repo_id = $1`, e.repoID(repo))
	e.st.Exec(`UPDATE packages SET display_name = name WHERE repo_id = $1`, e.repoID(repo))
	if a := e.get(r, repo, "v1/roles/archives/16/1.0.0.tar.gz"); a.Status != http.StatusOK {
		t.Fatalf("1.0.0: %d", a.Status)
	}
	if n := e.st.Text(`SELECT display_name FROM packages WHERE repo_id = $1 AND name = 'role/16'`, e.repoID(repo)); n != "role/16" {
		t.Fatalf("display name without facts = %q", n)
	}
	before := e.stub.Counts().OfKind(ansiblestub.ClassRoleContent, http.MethodGet)
	e.expire(repo)
	if a := e.get(r, repo, "v1/roles/?owner__username=geerlingguy&name=pip"); a.Status != http.StatusOK {
		t.Fatalf("lookup: %d", a.Status)
	}
	if n := e.st.Text(`SELECT display_name FROM packages WHERE repo_id = $1 AND name = 'role/16'`, e.repoID(repo)); n != "geerlingguy.pip" {
		t.Errorf("display name after the lookup = %q", n)
	}
	eventually(t, 30*time.Second, "the README job", func() bool {
		return e.stub.Counts().OfKind(ansiblestub.ClassRoleContent, http.MethodGet) == before+1
	})
}

// TestR_ANS_056_RoleSearchPassesThroughWithinItsBoundsAndKeepsWhatTheRepositoryDelivers: `ansible-galaxy role search` is answered from
// the upstream's search with the hits' addresses on the repository, once per search and replica within its period; a query outside its
// bounds asks the upstream nothing; a role the repository's rules block is no hit, and its install is refused with the rules' reason.
// `role info` is the lookup, which names the description. Data: the stub's three roles.
func TestR_ANS_056_RoleSearchPassesThroughWithinItsBoundsAndKeepsWhatTheRepositoryDelivers(t *testing.T) {
	e := newEnv(t, options{})
	const blocking = "ans-blockrole"
	e.repo(blocking, "verify-if-available", ansiblestub.DefaultGitHubHost, ansiblestub.DefaultCodeloadHost)
	e.st.DeliveryRule(blocking, testenv.PackageRule("role/16", "block"))
	r := e.start(1)[0]
	repo := e.fresh()

	names := func(a testenv.Answer) []string {
		var out []string
		for _, h := range list(a.JSON(t)["results"]) {
			out = append(out, obj(h)["username"].(string)+"."+obj(h)["name"].(string))
		}
		return out
	}
	const search = "v1/search/roles/?&autocomplete=pip&page_size=1000"
	for range 2 {
		a := e.get(r, repo, search)
		if a.Status != http.StatusOK || !slices.Equal(names(a), []string{"geerlingguy.pip"}) || a.JSON(t)["count"] != float64(1) {
			t.Fatalf("search: %d %s", a.Status, a.Body)
		}
		if u := obj(list(a.JSON(t)["results"])[0])["url"]; u != baseURL+base(repo)+"v1/roles/16/" {
			t.Errorf("hit url = %v", u)
		}
	}
	if n := e.stub.Counts().OfKind(ansiblestub.ClassRoleSearch, http.MethodGet); n != 1 {
		t.Errorf("two searches made %d requests to the stub, want 1", n)
	}
	if a := e.get(r, repo, "v1/search/roles/?autocomplete=x&page_size=1001"); a.Status != http.StatusBadRequest {
		t.Errorf("a page size above the bound: %d %s", a.Status, a.Body)
	}
	if n := e.stub.Counts().OfKind(ansiblestub.ClassRoleSearch, http.MethodGet); n != 1 {
		t.Errorf("a refused search reached the stub: %d requests", n)
	}

	info := e.get(r, repo, "v1/roles/?owner__username=geerlingguy&name=pip")
	if d := obj(list(info.JSON(t)["results"])[0])["description"]; info.Status != http.StatusOK || d != "Pip (Python package manager) for Linux." {
		t.Errorf("role info: %d %v", info.Status, d)
	}

	a := e.get(r, blocking, "v1/search/roles/?autocomplete=geerlingguy")
	if a.Status != http.StatusOK || slices.Contains(names(a), "geerlingguy.pip") {
		t.Errorf("a blocked role is a hit: %d %s", a.Status, a.Body)
	}
	v := e.get(r, blocking, "v1/roles/16/versions/?page_size=50")
	if v.Status != http.StatusForbidden || !strings.Contains(string(v.Body), "role/16 any version is not allowed") || !strings.Contains(string(v.Body), "blocked by a rule") {
		t.Errorf("the install of a blocked role: %d %s", v.Status, v.Body)
	}
}
