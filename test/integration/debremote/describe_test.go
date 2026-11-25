//go:build integration

package debremote

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

// TestR_DEB_037_ACachedPackageFileIsDescribedByItsControlParagraph: a package file a remote repository cached has the row of its
// control paragraph a local one has (R-DEB-037), so the package API answers its description, its control fields and the suite and
// component of the index that listed it; the removal of the cached file takes the row with it.
func TestR_DEB_037_ACachedPackageFileIsDescribedByItsControlParagraph(t *testing.T) {
	e := newEnv(t)
	repo := e.fresh(true)
	e.fetchIndexes(repo)
	eventually(t, 10*time.Second, "the rows", func() bool { return e.rows("deb_remote_entries", repo) > 0 })
	path := e.st.Text(`SELECT path FROM deb_remote_entries WHERE repo_id = $1 AND package = 'quader-hello' AND architecture = 'amd64'`, e.repoID(repo))
	if path == "" {
		t.Fatal("the stable index lists no quader-hello for amd64")
	}
	if a := e.get(repo, path); a.Status != http.StatusOK {
		t.Fatalf("the package file: %d", a.Status)
	}
	a := e.st.Do(e.r, "GET", "/api/v1/packages?format=deb&repository="+url.QueryEscape(project+"/"+repo), e.admin, nil)
	items, _ := a.JSON(t)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("the remote lists %v", items)
	}
	pkg, _ := items[0].(map[string]any)
	id, _ := pkg["id"].(string)
	version, _ := pkg["latestVersion"].(string)
	var md map[string]any
	eventually(t, 30*time.Second, "the control paragraph", func() bool {
		a := e.st.Do(e.r, "GET", "/api/v1/packages/"+id+"/versions/"+url.PathEscape(version), e.admin, nil)
		if a.Status != http.StatusOK {
			t.Fatalf("the version: %d %s", a.Status, a.Body)
		}
		md, _ = a.JSON(t)["metadata"].(map[string]any)
		return md["maintainer"] != nil
	})
	// The package's description is its newest version's: the first line of the cached file's Description.
	if p := e.st.Do(e.r, "GET", "/api/v1/packages/"+id, e.admin, nil).JSON(t); p["description"] != "prints a fixed line" {
		t.Errorf("description %v, want the paragraph's first line", p["description"])
	}
	for key, want := range map[string]string{
		"depends": "libquader1 (>= 1.2)", "maintainer": "quader test archive <debstub@quader.invalid>",
	} {
		if md[key] != want {
			t.Errorf("metadata.%s %v, want %q", key, md[key], want)
		}
	}
	if arch, _ := md["architectures"].([]any); len(arch) != 1 || arch[0] != "amd64" {
		t.Errorf("architectures %v, want [amd64]", md["architectures"])
	}
	if c, _ := md["coordinates"].([]any); len(c) != 1 || c[0] != "stable/main" {
		t.Errorf("coordinates %v, want [stable/main]: the index that listed the file", md["coordinates"])
	}

	// The removal of the cached file takes its row.
	if a := e.st.Do(e.r, "DELETE", "/api/v1/projects/"+project+"/repositories/"+repo+"/items?path="+url.QueryEscape(path), e.admin, nil); a.Status >= 300 {
		t.Fatalf("delete the cached file: %d %s", a.Status, a.Body)
	}
	eventually(t, 20*time.Second, "the row to go", func() bool { return e.rows("deb_binaries", repo) == 0 })
}
