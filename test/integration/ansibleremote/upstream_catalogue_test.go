//go:build integration

package ansibleremote

import (
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_CUR_010_ASubSectionIsPreviewedAndDownloadedThroughTheCatalogue: the namespace acme of the stub Galaxy is a level of the upstream
// catalogue (R-ANS-047): its preview counts its collections from the level's count, and its bulk is expanded through the catalogue's
// level and lookup, bounded by maxVersions, into rows that end done (R-CUR-010). About 4 s, two archives of the stub's corpus.
func TestR_CUR_010_ASubSectionIsPreviewedAndDownloadedThroughTheCatalogue(t *testing.T) {
	e := newEnv(t, options{env: []string{"QUADER_JOBS_POLL_INTERVAL=200ms"}})
	repo := e.fresh()
	rep := e.st.Start(t, testenv.ReplicaOptions{Name: "all-1", Role: "all", DialOverride: e.dial})
	path := "/api/v1/projects/" + project + "/repositories/" + repo
	body := map[string]any{"level": "section", "section": "acme", "maxVersions": 2}

	pv := e.st.Do(rep, http.MethodPost, path+"/downloads/preview", e.admin, body)
	if m := pv.JSON(t); pv.Status != http.StatusOK || m["packages"] != float64(3) || m["versions"] != nil {
		t.Fatalf("the preview of the namespace: %d %s", pv.Status, pv.Body)
	}
	a := e.st.Do(rep, http.MethodPost, path+"/downloads", e.admin, body)
	if a.Status != http.StatusAccepted {
		t.Fatalf("the bulk: %d %s", a.Status, a.Body)
	}
	bulk := a.JSON(t)["bulk"].(map[string]any)["id"].(string)
	run := func() {
		e.st.Exec(`UPDATE remote_download SET not_before = now() WHERE state = 'waiting'`)
		_ = e.st.Do(rep, http.MethodPost, "/api/v1/system/tasks/remote-downloads-dispatch/run", e.admin, nil)
	}
	run()
	var got map[string]any
	testenv.WaitFor(t, "the bulk's two rows done", 60*time.Second, func() bool {
		got = e.st.Do(rep, http.MethodGet, path+"/download-bulks/"+bulk, e.admin, nil).JSON(t)
		rows, _ := got["rows"].(map[string]any)
		if got["state"] == "expanding" || got["state"] == "queued" {
			run()
		}
		return got["versionsQueued"] == float64(2) && rows["done"] == float64(2)
	})
	if n := e.st.Number(`SELECT count(*) FROM remote_download WHERE bulk_id = $1::uuid AND name LIKE 'acme.%'`, bulk); n != 2 {
		t.Errorf("the bulk queued %d rows of acme, want 2: %v", n, got)
	}
}
