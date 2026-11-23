//go:build integration

package debremote

import (
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// R-CUR-010, R-CUR-011, R-DEB-036: a download ahead of one version of a package stores the files the remote's parsed indexes list for
// it, whatever its mirror scope says (rows.MirrorScope.AnyIndex), verified against the index's digest. Integration tier, about 3 s, the
// stub's stable suite.
func TestR_CUR_010_ADownloadAheadOfAVersionStoresTheFilesTheParsedIndexesList(t *testing.T) {
	e := newEnv(t)
	repo := e.fresh(true)
	path, digest := e.poolFile()
	e.fetchIndexes(repo)
	id := e.repoID(repo)
	eventually(t, 10*time.Second, "the rows", func() bool { return e.rows("deb_remote_entries", repo) > 0 })
	pkg := e.st.Text(`SELECT package FROM deb_remote_entries WHERE repo_id = $1 AND path = $2 LIMIT 1`, id, path)
	version := e.st.Text(`SELECT version FROM deb_remote_entries WHERE repo_id = $1 AND path = $2 LIMIT 1`, id, path)

	api := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + repo
	a := e.st.Do(e.r, http.MethodPost, api+"/downloads", e.admin, map[string]any{"level": "version", "name": pkg, "version": version})
	if a.Status != http.StatusAccepted {
		t.Fatalf("the download: %d %s", a.Status, a.Body)
	}
	rowID := a.JSON(t)["download"].(map[string]any)["id"].(string)
	var row map[string]any
	eventually(t, 60*time.Second, "the row done", func() bool {
		row = e.st.Do(e.r, http.MethodGet, api+"/downloads/"+rowID, e.admin, nil).JSON(t)
		return row["state"] == "done" || row["state"] == "failed"
	})
	if row["state"] != "done" || row["outcome"] != "completed" || row["stored"].(float64) < 1 {
		t.Fatalf("the row: %v", row)
	}
	if got := e.st.Text(`SELECT COALESCE((SELECT encode(blob_sha256, 'hex') FROM items
		WHERE repo_id = $1 AND path = $2 AND trashed_at IS NULL), '')`, id, path); got != digest {
		t.Errorf("the stored file %s has the digest %q, want the index's %s", path, got, digest)
	}
}
