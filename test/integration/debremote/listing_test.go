//go:build integration

package debremote

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

// TestR_API_081_ARemoteListsThePackageFilesClientsFetchedThroughIt: a Debian remote lists what was fetched through it, as every
// format's remote does (Q-REPO-330): the package file a client fetched is a version of its package, named by the parsed index row of
// its path, with its size, one download and the time of it (R-API-079, R-API-080). The indexes are no version and list nothing; the
// eviction of the cached file takes the version with it.
func TestR_API_081_ARemoteListsThePackageFilesClientsFetchedThroughIt(t *testing.T) {
	e := newEnv(t)
	path, _ := e.poolFile()
	repo := e.fresh(true)
	e.fetchIndexes(repo)
	eventually(t, 10*time.Second, "the rows", func() bool { return e.rows("deb_remote_entries", repo) > 0 })
	list := func() []any {
		t.Helper()
		a := e.st.Do(e.r, "GET", "/api/v1/packages?format=deb&repository="+url.QueryEscape(project+"/"+repo), e.admin, nil)
		if a.Status != http.StatusOK {
			t.Fatalf("list: %d %s", a.Status, a.Body)
		}
		items, _ := a.JSON(t)["items"].([]any)
		return items
	}
	if got := list(); len(got) != 0 {
		t.Fatalf("a remote that holds only indexes lists %v", got)
	}

	id := e.repoID(repo)
	name := e.st.Text(`SELECT package FROM deb_remote_entries WHERE repo_id = $1 AND path = $2 LIMIT 1`, id, path)
	version := e.st.Text(`SELECT version FROM deb_remote_entries WHERE repo_id = $1 AND path = $2 LIMIT 1`, id, path)
	size := e.st.Number(`SELECT size FROM deb_remote_entries WHERE repo_id = $1 AND path = $2 LIMIT 1`, id, path)
	if a := e.get(repo, path); a.Status != http.StatusOK {
		t.Fatalf("the package file: %d", a.Status)
	}
	got := list()
	if len(got) != 1 || got[0].(map[string]any)["name"] != name || got[0].(map[string]any)["latestVersion"] != version {
		t.Fatalf("the remote lists %v, want %s %s", got, name, version)
	}
	pkg := got[0].(map[string]any)["id"].(string)
	var v map[string]any
	eventually(t, 20*time.Second, "the download to be counted", func() bool {
		a := e.st.Do(e.r, "GET", "/api/v1/packages/"+pkg+"/versions?repository="+url.QueryEscape(project+"/"+repo), e.admin, nil)
		items, _ := a.JSON(t)["items"].([]any)
		if len(items) != 1 {
			return false
		}
		v = items[0].(map[string]any)
		return v["downloadCount"] == float64(1)
	})
	if v["lastDownloadedAt"] == nil || v["sizeBytes"] != float64(size) || v["sizePartial"] != false {
		t.Errorf("the version: lastDownloadedAt %v, sizeBytes %v (want %d), sizePartial %v", v["lastDownloadedAt"], v["sizeBytes"], size, v["sizePartial"])
	}

	linked := e.st.Number(`SELECT count(*) FROM items i JOIN package_versions v ON v.id = i.version_id AND v.lead_item_id = i.id
		WHERE i.repo_id = $1 AND i.path = $2`, id, path)
	if linked != 1 {
		t.Fatalf("the cached file is the lead of %d versions, want 1", linked)
	}

	// The cached file is removed (housekeeping, a purge): the version goes with it.
	a := e.st.Do(e.r, "DELETE", "/api/v1/projects/"+project+"/repositories/"+repo+"/items?path="+url.QueryEscape(path), e.admin, nil)
	if a.Status >= 300 {
		t.Fatalf("delete the cached file: %d %s", a.Status, a.Body)
	}
	eventually(t, 20*time.Second, "the file to be removed", func() bool {
		return e.st.Number(`SELECT count(*) FROM items WHERE repo_id = $1 AND path = $2`, id, path) == 0
	})
	if n := e.st.Number(`SELECT count(*) FROM package_versions v JOIN packages p ON p.id = v.package_id WHERE p.repo_id = $1`, id); n != 0 {
		t.Fatalf("after the removal (answer %d %s) the repository holds %d versions", a.Status, a.Body, n)
	}
	if got := list(); len(got) != 0 {
		t.Errorf("after the removal the remote lists %v", got)
	}
}
