//go:build integration

package ociremote

import (
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// R-CUR-010, R-CUR-011, R-OCI-038: a download ahead of one tag (the version of OCI) stores the image of the platforms of oci.platforms
// with their layers, as a probe of the tag does, without a client's request of the manifest. Integration tier, about 2 s, the stub's
// image acme/a:1.0.
func TestR_CUR_010_ADownloadAheadOfATagStoresTheImageOfTheListedPlatforms(t *testing.T) {
	e, rep := probeEnv(t)
	img, err := e.stub.Image("acme/a", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + rProbe
	a := e.st.Do(rep, http.MethodPost, path+"/downloads", e.admin, map[string]any{"level": "version", "name": "acme/a", "version": "1.0"})
	if a.Status != http.StatusAccepted {
		t.Fatalf("the download: %d %s", a.Status, a.Body)
	}
	id := a.JSON(t)["download"].(map[string]any)["id"].(string)
	var row map[string]any
	testenv.WaitFor(t, "the row done", 60*time.Second, func() bool {
		row = e.st.Do(rep, http.MethodGet, path+"/downloads/"+id, e.admin, nil).JSON(t)
		return row["state"] == "done" || row["state"] == "failed"
	})
	if row["state"] != "done" || row["outcome"] != "completed" {
		t.Fatalf("the row: %v", row)
	}
	want := []string{"acme/a/_manifests/" + img.Digest}
	for _, b := range blobsOf(img) {
		want = append(want, "acme/a/_blobs/"+b)
	}
	if n := e.stored(rProbe, want...); n != int64(len(want)) {
		t.Errorf("%d of the %d files of the image are stored", n, len(want))
	}
	if n := e.st.Number(`SELECT count(*) FROM remote_download WHERE origin = 'probe'`); n != 0 {
		t.Errorf("the download ahead queued %d probe rows", n)
	}
}

// R-CUR-010, R-CUR-008: a bulk of every version of an image of a Docker Hub remote is expanded through the catalogue, which reads the
// tags on Docker Hub's API host (hub.docker.com, the preset's catalogue host), not on the registry: the bulk reaches that host as the
// tab Upstream does and queues its rows, although the host is no extraHosts entry of the repository. The rows are not awaited; what
// they fetch is R-CUR-011's. Integration tier, about 2 s, the API stub's tags of acme/a.
func TestR_CUR_010_ABulkOfADockerHubRemoteReadsTheTagsWhereTheCatalogueReadsThem(t *testing.T) {
	e := newEnv(t, options{env: []string{"QUADER_JOBS_POLL_INTERVAL=200ms"}})
	rep := e.st.Start(t, testenv.ReplicaOptions{Name: "all-1", Role: "all", DialOverride: e.dial})
	path := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + rHub
	a := e.st.Do(rep, http.MethodPost, path+"/downloads", e.admin, map[string]any{"level": "package", "name": "acme/a", "maxVersions": 2})
	if a.Status != http.StatusAccepted {
		t.Fatalf("the bulk: %d %s", a.Status, a.Body)
	}
	id := a.JSON(t)["bulk"].(map[string]any)["id"].(string)
	var bulk map[string]any
	testenv.WaitFor(t, "the bulk expanded", 30*time.Second, func() bool {
		_ = e.st.Do(rep, http.MethodPost, "/api/v1/system/tasks/remote-downloads-dispatch/run", e.admin, nil)
		bulk = e.st.Do(rep, http.MethodGet, path+"/download-bulks/"+id, e.admin, nil).JSON(t)
		return bulk["state"] != "expanding"
	})
	if bulk["state"] == "failed" || bulk["versionsQueued"] != float64(2) {
		t.Fatalf("the bulk of acme/a: %v", bulk)
	}
	if n := e.st.Number(`SELECT count(*) FROM remote_download WHERE bulk_id = $1::uuid AND version IN ('2.0', '1.2')`, id); n != 2 {
		t.Errorf("the bulk queued %d rows of the two newest tags, want 2", n)
	}
	if e.hubAPI.requests.Load() == 0 {
		t.Error("the catalogue host of Docker Hub was not asked")
	}
	// The repository's bulks list it (Q-CUR-168), as the person's, with its counters.
	l := e.st.Do(rep, http.MethodGet, path+"/download-bulks?limit=1", e.admin, nil)
	items, _ := l.JSON(t)["items"].([]any)
	if l.Status != http.StatusOK || len(items) != 1 {
		t.Fatalf("the list of bulks: %d %s", l.Status, l.Body)
	}
	if got := items[0].(map[string]any); got["id"] != id || got["origin"] != "person" || got["versionsQueued"] != float64(2) {
		t.Errorf("the listed bulk: %v", got)
	}
}
