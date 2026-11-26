//go:build integration

package debremote

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_DEB_036_TheCatalogueSearchesTheIndexesTheRemoteParsed: after the remote parsed its indexes (R-DEB-023), the catalogue's search
// finds a package by its name and, unless names only are asked, by a word of its short description, which the parse keeps with Provides
// (Q-CUR-16); the root level lists the suite and a lookup lists the package's version from the rows. Nothing is read from the upstream.
// The lookup and the search made before the parse find nothing, and the catalogue does not keep a page that found nothing without asking
// the upstream (R-CUR-008, Q-CUR-164): the case waits on the catalogue's lookup, not on the rows. About 3 s, the stub archive.
func TestR_DEB_036_TheCatalogueSearchesTheIndexesTheRemoteParsed(t *testing.T) {
	e := newEnv(t)
	repo := e.fresh(true)
	path, _ := e.poolFile()
	pkg, _, _ := strings.Cut(path[strings.LastIndex(path, "/")+1:], "_")
	api := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + repo + "/upstream"
	lookup := func() testenv.Answer {
		t.Helper()
		return e.st.Do(e.r, http.MethodGet, api+"/package?name="+url.QueryEscape(pkg), e.admin, nil)
	}
	search := func(q string, namesOnly bool) []any {
		t.Helper()
		u := api + "/search?q=" + url.QueryEscape(q)
		if namesOnly {
			u += "&namesOnly=true"
		}
		a := e.st.Do(e.r, http.MethodGet, u, e.admin, nil)
		if a.Status != http.StatusOK {
			t.Fatalf("search %q: %d %s", q, a.Status, a.Body)
		}
		hits, _ := a.JSON(t)["hits"].([]any)
		return hits
	}
	has := func(hits []any) bool {
		for _, h := range hits {
			if h.(map[string]any)["name"] == pkg {
				return true
			}
		}
		return false
	}
	if a := lookup(); a.Status != http.StatusNotFound {
		t.Fatalf("the lookup of %s before the parse: %d %s, want 404", pkg, a.Status, a.Body)
	}
	if has(search(strings.ToUpper(pkg), false)) {
		t.Fatalf("the search finds %s before the parse", pkg)
	}
	e.fetchIndexes(repo)
	eventually(t, 10*time.Second, "the catalogue's lookup of "+pkg+" after the parse", func() bool { return lookup().Status == http.StatusOK })
	id := e.repoID(repo)
	version := e.st.Text(`SELECT version FROM deb_remote_entries WHERE repo_id = $1 AND path = $2 LIMIT 1`, id, path)
	desc := e.st.Text(`SELECT description FROM deb_remote_entries WHERE repo_id = $1 AND path = $2 LIMIT 1`, id, path)
	if desc == "" {
		t.Fatalf("the parse kept no short description of %s", pkg)
	}
	// The lookup names the short description the rows keep, no facts and no README (R-CUR-028, Q-CUR-262).
	if m := lookup().JSON(t); m["description"] == "" || m["about"] != nil || m["readme"] != false {
		t.Errorf("R-CUR-028 the lookup of %s: description %q, about %v, readme %v", pkg, m["description"], m["about"], m["readme"])
	}
	if !has(search(strings.ToUpper(pkg), false)) {
		t.Errorf("the search by the name %s does not find it", pkg)
	}
	word := ""
	for _, w := range strings.Fields(strings.ToLower(desc)) {
		if len(w) > 3 && !strings.Contains(pkg, w) && strings.Trim(w, "abcdefghijklmnopqrstuvwxyz") == "" {
			word = w
			break
		}
	}
	if word != "" {
		if !has(search(word+" "+pkg, false)) {
			t.Errorf("the words %q and %q do not find %s", word, pkg, pkg)
		}
		if has(search(word+" "+pkg, true)) {
			t.Errorf("names only finds %s by a word of its description, %q", pkg, word)
		}
	}
	root := e.st.Do(e.r, http.MethodGet, api+"/levels", e.admin, nil)
	if levels, _ := root.JSON(t)["levels"].([]any); root.Status != http.StatusOK || len(levels) == 0 {
		t.Fatalf("the root level: %d %s", root.Status, root.Body)
	}
	a := lookup()
	if a.Status != http.StatusOK || !strings.Contains(string(a.Body), `"version":"`+version+`"`) {
		t.Fatalf("the lookup of %s: %d %s", pkg, a.Status, a.Body)
	}
}
