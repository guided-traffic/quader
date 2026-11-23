//go:build integration

package nuget

import (
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// R-CUR-010, R-CUR-011, R-NUGET-035: a download ahead of one version stores its package with the rows of origin cache, as a client's
// fetch does. Integration tier, about 2 s, the stub's Newtonsoft.Json 13.0.3.
func TestR_CUR_010_ADownloadAheadOfAVersionStoresItsPackage(t *testing.T) {
	e := newRemoteEnv(t, remoteOptions{Valkey: true, Env: []string{"QUADER_JOBS_POLL_INTERVAL=200ms"}})
	rep := e.st.Start(t, testenv.ReplicaOptions{Name: "all-1", Role: "all"})
	path := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + rRemote
	a := e.st.Do(rep, http.MethodPost, path+"/downloads", e.tokAdmin, map[string]any{"level": "version", "name": "Newtonsoft.Json", "version": "13.0.3"})
	if a.Status != http.StatusAccepted {
		t.Fatalf("the download: %d %s", a.Status, a.Body)
	}
	id := a.JSON(t)["download"].(map[string]any)["id"].(string)
	var row map[string]any
	testenv.WaitFor(t, "the row done", 60*time.Second, func() bool {
		row = e.st.Do(rep, http.MethodGet, path+"/downloads/"+id, e.tokAdmin, nil).JSON(t)
		return row["state"] == "done" || row["state"] == "failed"
	})
	if row["state"] != "done" || row["outcome"] != "completed" || row["stored"] != float64(1) {
		t.Fatalf("the row: %v", row)
	}
	if got := e.itemCount(rRemote, newtonsoft+"/13.0.3/"); got != 1 {
		t.Errorf("%d items of the version, want 1", got)
	}
	if got := e.cacheRows(rRemote, newtonsoft); got != 1 {
		t.Errorf("%d NuGet rows of origin cache, want 1", got)
	}
}
