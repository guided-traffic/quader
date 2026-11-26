//go:build integration

package ansibleremote

import (
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_CUR_028_ACollectionsReadmeIsReadThroughItsDocsBlobAndSanitized: the tab Upstream's lookup of a collection no client fetched
// names its highest version's facts from the version document, and its README call reads the docs-blob of a version through the lent
// upstream at the address the remote derives, once, answers its README sanitized (the stub's holds script) and stores nothing: no
// cache entry and no documentation row (R-CUR-028, R-CUR-008). About 1 s, the stub's corpus.
func TestR_CUR_028_ACollectionsReadmeIsReadThroughItsDocsBlobAndSanitized(t *testing.T) {
	e := newEnv(t, options{})
	repo := e.fresh()
	rep := e.st.Start(t, testenv.ReplicaOptions{Name: "all-1", Role: "all", DialOverride: e.dial})
	path := "/api/v1/projects/" + project + "/repositories/" + repo + "/upstream"

	a := e.st.Do(rep, http.MethodGet, path+"/package?name=acme.tools", e.admin, nil)
	m := a.JSON(t)
	about, _ := m["about"].(map[string]any)
	if a.Status != http.StatusOK || about == nil || about["version"] != "2.0.0" || m["readme"] != true {
		t.Fatalf("R-CUR-028 the lookup of acme.tools: %d %.500s", a.Status, a.Body)
	}
	deps, _ := about["dependencies"].([]any)
	if len(deps) != 1 || deps[0].(map[string]any)["name"] != "acme.base" {
		t.Errorf("R-CUR-028 the facts' dependencies: %v", about["dependencies"])
	}

	before := e.docsRequests()
	for range 2 {
		a = e.st.Do(rep, http.MethodGet, path+"/readme?name=acme.tools&version=1.0.0", e.admin, nil)
		rd := a.JSON(t)
		body, _ := rd["readme"].(map[string]any)
		if a.Status != http.StatusOK || rd["state"] != "available" || body == nil || body["contentType"] != "text/html" {
			t.Fatalf("R-CUR-028 the README of acme.tools 1.0.0: %d %.500s", a.Status, a.Body)
		}
		html, _ := body["html"].(string)
		if !strings.Contains(html, ">acme.tools</h1>") {
			t.Errorf("R-CUR-028 the README lacks its heading: %.300s", html)
		}
		for _, bad := range []string{"<script", "onerror", "javascript:"} {
			if strings.Contains(html, bad) {
				t.Errorf("R-CUR-028 the README holds %q: %.300s", bad, html)
			}
		}
	}
	if n := e.docsRequests() - before; n != 1 {
		t.Errorf("R-CUR-028 two README calls reached the stub's docs-blob %d times, want 1 (the catalogue's cache)", n)
	}
	id := e.repoID(repo)
	if n := e.st.Number(`SELECT count(*) FROM remote_cache_entries WHERE repo_id = $1`, id); n != 0 {
		t.Errorf("R-CUR-028 the lookup and the README stored %d cache entries", n)
	}
	if n := e.st.Number(`SELECT count(*) FROM ansible_version_docs WHERE repo_id = $1`, id); n != 0 {
		t.Errorf("R-CUR-028 the README wrote %d documentation rows", n)
	}
}
