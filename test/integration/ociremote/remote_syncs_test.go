//go:build integration

package ociremote

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/ocistub"
)

// R-CUR-027, Q-CUR-244, Q-CUR-245: an OCI tag is a cache entry and no version row, so a sync's run queues it again; its row revalidates
// the tag with a HEAD, and a tag the upstream moved is fetched with its new image while the earlier one stays. A run whose tag did not
// move costs one HEAD and stores nothing. Integration tier, about 3 s, the stub's image acme/a:1.0 (manifests of a few hundred bytes).
func TestR_CUR_027_ASyncFetchesATagThatMovedAgain(t *testing.T) {
	e, rep := probeEnv(t)
	path := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + rProbe
	dispatch := func() {
		_ = e.st.Do(rep, http.MethodPost, "/api/v1/system/tasks/remote-downloads-dispatch/run", e.admin, nil)
	}
	// lastRun waits until the sync's newest run is another than not and every row of it ended.
	lastRun := func(t *testing.T, syncID, not string) map[string]any {
		t.Helper()
		var run map[string]any
		testenv.WaitFor(t, "the sync's run ended", 60*time.Second, func() bool {
			items, _ := e.st.Do(rep, http.MethodGet, path+"/download-syncs", e.admin, nil).JSON(t)["items"].([]any)
			run = nil
			for _, it := range items {
				if sy := it.(map[string]any); sy["id"] == syncID {
					run, _ = sy["lastBulk"].(map[string]any)
				}
			}
			if run == nil || run["id"] == not {
				dispatch()
				return false
			}
			rows, _ := run["rows"].(map[string]any)
			if run["state"] == "expanding" || run["state"] == "queued" || rows["waiting"] != nil || rows["running"] != nil {
				dispatch()
				return false
			}
			return true
		})
		return run
	}
	tagTarget := func() string {
		return e.st.Text(`SELECT 'sha256:' || encode(e.blob_sha256, 'hex') FROM remote_cache_entries e JOIN repositories r ON r.id = e.repo_id
			WHERE r.key = $1 AND e.tag_name = 'acme/a' AND e.tag = '1.0' AND e.kind = 'metadata' AND e.variant = ''`, rProbe)
	}

	// The range of the one tag 1.0 keeps the test to one image.
	a := e.st.Do(rep, http.MethodPost, path+"/download-syncs", e.admin,
		map[string]any{"level": "package", "name": "acme/a", "fromVersion": "1.0", "toVersion": "1.0"})
	if a.Status != http.StatusCreated {
		t.Fatalf("the sync: %d %s", a.Status, a.Body)
	}
	syncID := a.JSON(t)["sync"].(map[string]any)["id"].(string)
	first := lastRun(t, syncID, "")
	before, err := e.stub.Image("acme/a", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	if first["versionsQueued"] != float64(1) || e.stored(rProbe, "acme/a/_manifests/"+before.Digest) != 1 {
		t.Fatalf("the first run: %v; the image %s stored %d times", first, before.Digest, e.stored(rProbe, "acme/a/_manifests/"+before.Digest))
	}

	t.Run("an unchanged tag costs a HEAD and stores nothing", func(t *testing.T) {
		c0 := e.stub.Counts()
		e.st.Exec(`UPDATE remote_download_syncs SET next_run_at = now()`)
		run := lastRun(t, syncID, first["id"].(string))
		c1 := e.stub.Counts()
		heads := c1.ManifestHeads("acme/a", "1.0") - c0.ManifestHeads("acme/a", "1.0")
		gets := c1.ManifestGets("acme/a", "1.0") - c0.ManifestGets("acme/a", "1.0")
		blobs := c1.OfKind(ocistub.KindBlob, "GET") - c0.OfKind(ocistub.KindBlob, "GET")
		t.Logf("a run of an unchanged tag asked the upstream: %d request(s) in all, %d HEAD and %d GET of the tag's manifest, %d blob GETs",
			c1.Total()-c0.Total(), heads, gets, blobs)
		if run["versionsQueued"] != float64(1) || heads != 1 || gets != 0 || blobs != 0 {
			t.Fatalf("the run of an unchanged tag: %v; %d HEAD, %d GET, %d blobs; want the tag queued, one HEAD and nothing fetched", run, heads, gets, blobs)
		}
		if got := tagTarget(); got != before.Digest {
			t.Errorf("the tag's entry names %s, want %s", got, before.Digest)
		}
	})

	t.Run("a moved tag is fetched with its new image, the earlier one stays", func(t *testing.T) {
		prev := lastRun(t, syncID, first["id"].(string))
		moved, err := e.stub.MoveTag("acme/a", "1.0")
		if err != nil {
			t.Fatal(err)
		}
		after, err := e.stub.Image("acme/a", "1.0")
		if err != nil {
			t.Fatal(err)
		}
		e.st.Exec(`UPDATE remote_download_syncs SET next_run_at = now()`)
		run := lastRun(t, syncID, prev["id"].(string))
		if run["versionsQueued"] != float64(1) || !strings.HasPrefix(moved, "sha256:") {
			t.Fatalf("the run after the move: %v", run)
		}
		want := []string{"acme/a/_manifests/" + after.Digest}
		for _, b := range blobsOf(after) {
			want = append(want, "acme/a/_blobs/"+b)
		}
		if n := e.stored(rProbe, want...); n != int64(len(want)) {
			t.Errorf("%d of the %d files of the moved tag's image are stored", n, len(want))
		}
		if n := e.stored(rProbe, "acme/a/_manifests/"+before.Digest); n != 1 {
			t.Errorf("the earlier image is stored %d times, want it kept", n)
		}
		if got := tagTarget(); got != after.Digest {
			t.Errorf("the tag's entry names %s, want the moved image %s", got, after.Digest)
		}
	})
}
