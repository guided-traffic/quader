//go:build integration

package ociremote

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// A probe through a virtual repository starts the download at the member that serves it (repositories-and-proxy.md Q-REPO-367): the
// member's switch, patterns and platforms apply, the content is stored at the member, and the charge is the probe's: the caller's budget
// key at the repository the client addressed, the member's ceiling, the usage recorded at the member (usage-and-limits.md Q-LIM-2,
// R-LIM-037).

func (e *renv) jobsOf(member string) int64 {
	return e.st.Number(`SELECT count(*) FROM remote_download d JOIN repositories r ON r.id = d.repo_id
		WHERE d.origin = 'probe' AND r.key = $1`, member)
}

func TestR_REPO_065_AProbeThroughAVirtualRepositoryDownloadsAtTheMemberThatServesIt(t *testing.T) {
	e, rep := probeEnv(t)
	put := func(method, path string, body any, h ...string) {
		t.Helper()
		raw, _ := json.Marshal(body)
		if a := e.st.Do(rep, method, path, e.admin, raw, h...); a.Status != http.StatusOK && a.Status != http.StatusCreated {
			t.Fatalf("%s %s: %d %s", method, path, a.Status, a.Body)
		}
	}
	// The reader's figures at the virtual repository: the budget key a request addressed to it is decided with (Q-LIM-2).
	put(http.MethodPost, "/api/v1/projects/"+testenv.DefaultProject+"/repositories/"+rProbeV+"/limits/assignments", map[string]any{
		"who": map[string]any{"kind": "user", "user": "reader"}, "profile": "standard", "overrides": map[string]any{}, "note": "",
	}, "Idempotency-Key", "reader-at-the-virtual-repository")
	// One upstream request an hour for every signed-in caller of the member behind oci-probe-vx: the probe's own fetch takes it.
	put(http.MethodPut, "/api/v1/projects/"+testenv.DefaultProject+"/repositories/"+rProbeX+"/limits/ceilings", map[string]any{
		"anonymous": map[string]any{"uncachedBytesPerHour": nil, "upstreamRequestsPerHour": nil},
		"signedIn":  map[string]any{"uncachedBytesPerHour": nil, "upstreamRequestsPerHour": 1},
	}, "If-Match", "*")
	time.Sleep(1500 * time.Millisecond) // the change areas reach the replica's index
	reader := e.st.Text(`SELECT id::text FROM principal WHERE kind = 'user' AND name = 'reader'`)
	id := func(key string) string { return e.st.Text(`SELECT id::text FROM repositories WHERE key = $1`, key) }

	img, err := e.stub.Image("acme/a", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("a HEAD of a tag through the virtual repository stores the image at the remote member", func(t *testing.T) {
		if a := e.do(rep, http.MethodHead, rProbeV, "acme/a/manifests/1.0"); a.Status != http.StatusOK {
			t.Fatalf("HEAD of the tag: %d %s", a.Status, a.Body)
		}
		want := []string{"acme/a/_manifests/" + img.Digest}
		for _, b := range blobsOf(img) {
			want = append(want, "acme/a/_blobs/"+b)
		}
		testenv.WaitFor(t, "the background download at the member", 60*time.Second, func() bool {
			return e.stored(rProbe, want...) == int64(len(want))
		})
		if n := e.jobsOf(rProbe); n != 1 {
			t.Errorf("%d background downloads of the member, want 1", n)
		}
	})

	t.Run("the charge is the reader's budget key at the virtual repository, the usage recorded at the member", func(t *testing.T) {
		row := func(column string) int64 {
			return e.st.Number(`SELECT COALESCE(sum(`+column+`), 0) FROM usage_bucket WHERE granularity = '5m' AND subject_kind = 'user'
				AND subject_ref = $1 AND place = 'repository:' || $2 AND repository_id = $3::bigint`, reader, id(rProbeV), id(rProbe))
		}
		testenv.WaitFor(t, "the usage of the background download", 30*time.Second, func() bool {
			return row("background_fetches") == 1 && row("uncached_bytes") > 0
		})
		if n := e.st.Number(`SELECT COALESCE(sum(background_fetches), 0) FROM usage_bucket WHERE granularity = '5m' AND subject_ref = $1`, reader); n != 1 {
			t.Errorf("%d background downloads charged to the reader in all, want the one at the virtual repository", n)
		}
	})

	t.Run("the member's switch off starts nothing", func(t *testing.T) {
		if a := e.do(rep, http.MethodHead, rProbeVOff, "acme/a/manifests/1.0"); a.Status != http.StatusOK {
			t.Fatalf("HEAD of the tag: %d %s", a.Status, a.Body)
		}
		time.Sleep(3 * time.Second)
		if n := e.jobsOf(rProbeOff); n != 0 {
			t.Errorf("%d background downloads of the member whose switch is off", n)
		}
	})

	t.Run("the member's ceiling is counted: used up by the probe, it starts nothing", func(t *testing.T) {
		other, err := e.stub.Image("other/c", "1.0")
		if err != nil {
			t.Fatal(err)
		}
		if a := e.do(rep, http.MethodHead, rProbeVX, "other/c/manifests/1.0"); a.Status != http.StatusOK {
			t.Fatalf("HEAD of the tag: %d %s", a.Status, a.Body)
		}
		testenv.WaitFor(t, "the ceiling the decision met", 30*time.Second, func() bool {
			return e.st.Number(`SELECT COALESCE(sum(count), 0) FROM usage_limit_event WHERE granularity = '5m' AND subject_ref = $1
				AND figure = 'ceiling.signedIn' AND mode = 'enforce'`, reader) >= 1
		})
		time.Sleep(time.Second)
		if n := e.jobsOf(rProbeX); n != 0 {
			t.Errorf("%d background downloads past the member's ceiling", n)
		}
		shared := map[string]bool{}
		for _, b := range blobsOf(img) {
			shared[b] = true // fetched by the first test
		}
		for _, b := range blobsOf(other) {
			if !shared[b] && e.stub.Counts().BlobFetches(b) != 0 {
				t.Errorf("a blob was fetched past the member's ceiling: %s", b)
			}
		}
	})
}
