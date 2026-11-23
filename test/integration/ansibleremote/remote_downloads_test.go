//go:build integration

package ansibleremote

import (
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// R-CUR-010, R-CUR-011, R-ANS-047: a download ahead of one version of a collection stores its archive, the gate reading the version
// document for its digest. Integration tier, about 2 s, the stub's acme.tools 2.0.0.
func TestR_CUR_010_ADownloadAheadOfAVersionStoresItsArchive(t *testing.T) {
	e := newEnv(t, options{env: []string{"QUADER_JOBS_POLL_INTERVAL=200ms"}})
	repo := e.fresh()
	rep := e.st.Start(t, testenv.ReplicaOptions{Name: "all-1", Role: "all", DialOverride: e.dial})
	path := "/api/v1/projects/" + project + "/repositories/" + repo
	a := e.st.Do(rep, http.MethodPost, path+"/downloads", e.admin, map[string]any{"level": "version", "name": "acme.tools", "version": "2.0.0"})
	if a.Status != http.StatusAccepted {
		t.Fatalf("the download: %d %s", a.Status, a.Body)
	}
	id := a.JSON(t)["download"].(map[string]any)["id"].(string)
	var row map[string]any
	testenv.WaitFor(t, "the row done", 60*time.Second, func() bool {
		row = e.st.Do(rep, http.MethodGet, path+"/downloads/"+id, e.admin, nil).JSON(t)
		return row["state"] == "done" || row["state"] == "failed"
	})
	if row["state"] != "done" || row["outcome"] != "completed" || row["stored"] != float64(1) {
		t.Fatalf("the row: %v", row)
	}
	if n := e.st.Number(`SELECT count(*) FROM items WHERE repo_id = $1 AND path = 'collections/acme/tools/acme-tools-2.0.0.tar.gz' AND trashed_at IS NULL`,
		e.repoID(repo)); n != 1 {
		t.Errorf("the archive is stored %d times", n)
	}
}
