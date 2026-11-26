//go:build integration

package ociremote

import (
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// R-OPS-049, R-OPS-052: a download from an upstream is the image a person asked for, not its files. A client's pull of the stub's image
// acme/b:1.0, an OCI index of two platforms with an attestation manifest each (its platform and attestation manifests, their configs
// and layers, one request each), is one finished download of the image, named by its tag although its blobs name none, with every file counted and no total; a download ahead of the same tag on
// a remote of one platform is one download whose total is what the run named and is reached. The list is read on a replica of the role
// api, the one the chart serves the management API on, while a replica of the role all serves the pull and runs the row. Both remotes
// start no download on a probe: a probe's background download is a download of its own (origin queue), and would fetch what the
// client's pull then waits for. Integration tier, about 10 s (the linger of a pull, 5 s, and a publication interval of 1 s), the stub's
// image of a few kilobytes.
func TestR_OPS_049_AnImageIsOneDownload(t *testing.T) {
	e := newEnv(t, options{env: []string{"QUADER_JOBS_POLL_INTERVAL=200ms", "QUADER_PROXY_UPSTREAM_DOWNLOADS_PUBLISH_INTERVAL=1s"}})
	const rUnits = "oci-units"
	e.probeRepo(rUnits, map[string]any{"downloadOnProbe": false})
	e.probeRepo(rProbe, map[string]any{"downloadOnProbe": false})
	reader := testenv.Person{ID: e.st.Text(`SELECT id::text FROM principal WHERE kind = 'user' AND name = 'reader'`), Name: "reader", Kind: "user"}
	e.st.Policy("units-readers", []string{"read"}, []string{rUnits}, reader)
	rep := e.st.Start(t, testenv.ReplicaOptions{Name: "all-1", Role: "all", DialOverride: e.dial})
	api := e.st.Start(t, testenv.ReplicaOptions{Name: "api-a", Role: "api", DialOverride: e.dial})
	idx, err := e.stub.Image("acme/b", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	// The files a pull fetches: an image manifest's config and layers, or an index's platform and attestation manifests with theirs;
	// the manifest the tag names is a document, no file.
	files := map[string]bool{}
	for _, b := range blobsOf(idx) {
		files[b] = true
	}
	for _, m := range idx.Manifests {
		files[m.Digest] = true
		for _, b := range blobsOf(m) {
			files[b] = true
		}
	}

	// finished waits until the list read on the api replica holds a finished download of acme/b in the repository and nothing of it runs.
	finished := func(repo string) map[string]any {
		t.Helper()
		var got map[string]any
		testenv.WaitFor(t, "the download of acme/b in "+repo+" finished", 30*time.Second, func() bool {
			a := e.st.Do(api, http.MethodGet, "/api/v1/system/upstream-downloads", e.admin, nil)
			if a.Status != http.StatusOK {
				t.Fatalf("the list on the role api: %d %s", a.Status, a.Body)
			}
			list := a.JSON(t)
			for _, r := range list["running"].([]any) {
				if r.(map[string]any)["repository"].(map[string]any)["key"] == repo {
					return false
				}
			}
			var mine []map[string]any
			for _, f := range list["finished"].([]any) {
				if d := f.(map[string]any); d["repository"].(map[string]any)["key"] == repo && d["name"] == "acme/b" {
					mine = append(mine, d)
				}
			}
			if len(mine) > 1 {
				t.Fatalf("the image is %d downloads in %s: %v", len(mine), repo, mine)
			}
			if len(mine) == 1 {
				got = mine[0]
			}
			return got != nil
		})
		return got
	}

	t.Run("R-OPS-049 a client's pull of an image is one download named by its tag", func(t *testing.T) {
		e.pull(rep, rUnits, "acme/b", "1.0")
		d := finished(rUnits)
		if d["version"] != "1.0" || d["origin"] != "client" || d["outcome"] != "completed" || d["total"] != nil ||
			d["parts"] != float64(len(files)) || d["bytes"].(float64) <= 0 {
			t.Errorf("the pull of %d files: %v", len(files), d)
		}
	})

	t.Run("R-OPS-049 a download ahead of a tag is one download whose named total is reached", func(t *testing.T) {
		path := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + rProbe
		a := e.st.Do(rep, http.MethodPost, path+"/downloads", e.admin, map[string]any{"level": "version", "name": "acme/b", "version": "1.0"})
		if a.Status != http.StatusAccepted {
			t.Fatalf("the download: %d %s", a.Status, a.Body)
		}
		d := finished(rProbe)
		if d["version"] != "1.0" || d["origin"] != "queue" || d["queueOrigin"] != "person" || d["outcome"] != "completed" ||
			d["total"] == nil || d["total"] != d["bytes"] || d["parts"].(float64) < 3 || d["queueRow"] == nil {
			t.Errorf("the download ahead: %v", d)
		}
	})
}
