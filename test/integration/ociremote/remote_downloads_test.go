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
