//go:build integration

package ociremote

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/ocistub"
)

// A probe starts the download (repositories-and-proxy.md R-REPO-065, W-25): a client that asks a remote repository for the manifest of a
// tag, or a HEAD of it, and fetches nothing else, makes the repository download the image of the platforms of oci.platforms in the
// background, as a job of a worker, charged to the client (usage-and-limits.md R-LIM-037).

// The repositories of these tests, besides newEnv's: the stub's registry, the platforms linux/amd64 only.
const (
	rProbe    = "oci-probe"     // the switch on (the default)
	rProbeOff = "oci-probe-off" // downloadOnProbe false
	rProbeX   = "oci-probe-x"   // the exclude pattern library/nginx
	// Virtual repositories over each of them (Q-REPO-367).
	rProbeV    = "oci-probe-v"
	rProbeVOff = "oci-probe-voff"
	rProbeVX   = "oci-probe-vx"
)

// probeEnv is newEnv with the three repositories and one replica of the role all, which runs the jobs of the background downloads.
func probeEnv(t *testing.T) (*renv, *testenv.Replica) {
	t.Helper()
	e := newEnv(t, options{env: []string{
		"QUADER_LIMITS_FLUSH_INTERVAL=200ms", "QUADER_LIMITS_USAGE_FLUSH_INTERVAL=1s", "QUADER_JOBS_POLL_INTERVAL=200ms",
	}})
	for key, extra := range map[string]map[string]any{
		rProbe:    {},
		rProbeOff: {"downloadOnProbe": false},
		rProbeX:   {},
	} {
		e.probeRepo(key, extra)
	}
	for key, member := range map[string]string{rProbeV: rProbe, rProbeVOff: rProbeOff, rProbeVX: rProbeX} {
		e.st.Repository(key, "virtual", "oci", "{}")
		e.st.Exec(`INSERT INTO virtual_members (virtual_id, position, member_id)
			SELECT v.id, 0, m.id FROM repositories v, repositories m WHERE v.key = $1 AND m.key = $2`, key, member)
	}
	reader := testenv.Person{ID: e.st.Text(`SELECT id::text FROM principal WHERE kind = 'user' AND name = 'reader'`), Name: "reader", Kind: "user"}
	e.st.Policy("probe-readers", []string{"read"}, []string{rProbe, rProbeOff, rProbeX, rProbeV, rProbeVOff, rProbeVX}, reader)
	rep := e.st.Start(t, testenv.ReplicaOptions{
		Name: "all-1", Role: "all", DialOverride: e.dial, Env: []string{"QUADER_STORAGE_STATS_FLUSH_INTERVAL=1s"},
	})
	return e, rep
}

// probeRepo creates a remote of the stub's registry whose oci.platforms is linux/amd64, in the canonical form e.repo writes (the row
// is written as stored), with members of the remote section replaced.
func (e *renv) probeRepo(key string, remoteExtra map[string]any) {
	e.t.Helper()
	remote := map[string]any{
		"url": e.run.URL, "authMode": "none", "username": "", "password": nil, "clientCertificate": nil, "headers": []any{},
		"forwardAuthOnRedirect": false, "outboundProxy": map[string]any{"mode": "direct"}, "connectTimeoutMs": 5000, "readTimeoutMs": 15000,
		"sourceNetwork": nil, "cacheFillAction": "read", "downloadOnProbe": true,
		"extraHosts": []any{
			map[string]any{"pattern": ocistub.LoopbackRealmHost, "sendCredentials": false},
			map[string]any{"pattern": ocistub.LoopbackCDNHost, "sendCredentials": false},
		},
		"cache": map[string]any{
			"storeArtifacts": true, "metadataTtlSeconds": 300, "metadataRefreshTimeoutSeconds": 10, "notFoundTtlSeconds": 60,
			"maxStaleSeconds": 0, "unusedRetentionHours": 0,
		},
		"integrity":    map[string]any{"checksumPolicy": "verify-if-available", "blockUnexpectedContentTypes": true, "allowedContentTypes": []any{}},
		"availability": map[string]any{"mode": "online", "failureThreshold": 3, "circuitOpenSeconds": 30},
		"limits":       map[string]any{"maxArtifactBytes": nil, "maxMetadataBytes": nil, "maxConcurrentFetches": 16},
	}
	for k, v := range remoteExtra {
		remote[k] = v
	}
	body := map[string]any{"remote": remote, "oci": map[string]any{"platforms": []string{"linux/amd64"}}}
	if key == rProbeX {
		body["excludePatterns"] = []string{"library/nginx"}
	}
	doc, err := json.Marshal(body)
	if err != nil {
		e.t.Fatal(err)
	}
	e.st.Repository(key, "remote", "oci", string(doc))
}

// platformOf finds the child of an index of the stub by its platform, and the attestation of that child.
func platformOf(t *testing.T, idx ocistub.ImageInfo, platform string) (img, attestation ocistub.ImageInfo) {
	t.Helper()
	for _, m := range idx.Manifests {
		if m.Platform == platform {
			img = m
		}
	}
	for _, m := range idx.Manifests {
		if m.AttestationFor != "" && m.AttestationFor == img.Digest {
			attestation = m
		}
	}
	if img.Digest == "" {
		t.Fatalf("the stub's index has no %s: %+v", platform, idx)
	}
	return img, attestation
}

func blobsOf(m ocistub.ImageInfo) []string {
	var out []string
	if m.Config != nil {
		out = append(out, m.Config.Digest)
	}
	for _, l := range m.Layers {
		out = append(out, l.Digest)
	}
	return out
}

// storedPaths counts the items of a repository whose path is one of paths.
func (e *renv) stored(repo string, paths ...string) int64 {
	return e.st.Number(`SELECT count(*) FROM items i JOIN repositories r ON r.id = i.repo_id WHERE r.key = $1 AND i.path = ANY($2) AND i.trashed_at IS NULL`, repo, paths)
}

func TestR_REPO_065_AProbeOfATagDownloadsTheImageOfTheListedPlatformsInTheBackground(t *testing.T) {
	e, rep := probeEnv(t)
	idx, err := e.stub.Image("library/alpine", "3.20")
	if err != nil {
		t.Fatal(err)
	}
	amd, amdAtt := platformOf(t, idx, "linux/amd64")
	arm, _ := platformOf(t, idx, "linux/arm64/v8")

	if a := e.get(rep, rProbe, "library/alpine/manifests/3.20"); a.Status != http.StatusOK {
		t.Fatalf("the probe: %d %s", a.Status, a.Body)
	}
	want := []string{"library/alpine/_manifests/" + amd.Digest}
	for _, b := range blobsOf(amd) {
		want = append(want, "library/alpine/_blobs/"+b)
	}
	if amdAtt.Digest != "" {
		want = append(want, "library/alpine/_manifests/"+amdAtt.Digest)
		for _, b := range blobsOf(amdAtt) {
			want = append(want, "library/alpine/_blobs/"+b)
		}
	}
	testenv.WaitFor(t, "the background download of the linux/amd64 image", 60*time.Second, func() bool {
		return e.stored(rProbe, want...) == int64(len(want))
	})
	c := e.stub.Counts()
	if n := c.ManifestGets("library/alpine", arm.Digest); n != 0 {
		t.Errorf("the platform the list leaves out was asked %d times", n)
	}
	for _, b := range blobsOf(arm) {
		if c.BlobFetches(b) != 0 {
			t.Errorf("a blob of linux/arm64 was fetched: %s", b)
		}
	}

	t.Run("R-API-080 R-API-081 the tag is listed with the size of the platform held, partial, and the client's download", func(t *testing.T) {
		var vs []map[string]any
		testenv.WaitFor(t, "the tag listed with its download", 30*time.Second, func() bool {
			got := e.listing(rep, e.reader, rProbe)["library/alpine"]
			if got == nil {
				return false
			}
			vs = e.tags(rep, e.reader, got["id"].(string), rProbe)
			return len(vs) == 1 && vs[0]["downloadCount"] == float64(1)
		})
		if size, _ := vs[0]["sizeBytes"].(float64); size <= 0 || vs[0]["sizePartial"] != true {
			t.Errorf("the tag lists the size %v (partial %v), want the size of linux/amd64, partial", vs[0]["sizeBytes"], vs[0]["sizePartial"])
		}
	})

	t.Run("R-LIM-037 the probe's caller is charged one background download, its upstream requests and the uncached bytes", func(t *testing.T) {
		reader := e.st.Text(`SELECT id::text FROM principal WHERE kind = 'user' AND name = 'reader'`)
		sum := func(column string) int64 {
			return e.st.Number(`SELECT COALESCE(sum(`+column+`), 0) FROM usage_bucket WHERE granularity = '5m' AND subject_kind = 'user' AND subject_ref = $1`, reader)
		}
		// The decision's row arrives with the probe; the download's exchanges and bytes with the flushes after the worker read them.
		// The probe's own fetch of the tag, the decision's upstream request (the download's first exchange) and one exchange for every
		// further file: at least as many as the files.
		testenv.WaitFor(t, "the usage of the background download", 30*time.Second, func() bool {
			return sum("background_fetches") == 1 && sum("upstream_requests") >= int64(len(want)) && sum("uncached_bytes") > 0
		})
		if n := sum("background_fetches"); n != 1 {
			t.Errorf("%d background downloads charged to the reader, want 1", n)
		}
	})

	t.Run("a second probe of the stored image starts nothing", func(t *testing.T) {
		jobs := e.st.Number(`SELECT count(*) FROM job WHERE kind = 'remote-background-download'`)
		if a := e.get(rep, rProbe, "library/alpine/manifests/3.20"); a.Status != http.StatusOK {
			t.Fatalf("the second probe: %d", a.Status)
		}
		time.Sleep(2 * time.Second)
		if n := e.st.Number(`SELECT count(*) FROM job WHERE kind = 'remote-background-download'`); n != jobs {
			t.Errorf("%d jobs after the second probe, %d before", n, jobs)
		}
	})
}

func TestR_REPO_065_AHeadOfATagListsItOnceTheBackgroundDownloadStoredTheImage(t *testing.T) {
	e, rep := probeEnv(t)
	if a := e.do(rep, http.MethodHead, rProbe, "acme/a/manifests/1.0"); a.Status != http.StatusOK {
		t.Fatalf("HEAD of the tag: %d %s", a.Status, a.Body)
	}
	var vs []map[string]any
	testenv.WaitFor(t, "the tag listed after its background download", 60*time.Second, func() bool {
		got := e.listing(rep, e.reader, rProbe)["acme/a"]
		if got == nil {
			return false
		}
		vs = e.tags(rep, e.reader, got["id"].(string), rProbe)
		return len(vs) == 1
	})
	if vs[0]["version"] != "1.0" || vs[0]["downloadCount"] != float64(0) || vs[0]["lastDownloadedAt"] != nil {
		t.Errorf("the tag a client only asked about: %v, want 1.0 with no download", vs[0])
	}
	if size, _ := vs[0]["sizeBytes"].(float64); size <= 0 || vs[0]["sizePartial"] == true {
		t.Errorf("an image of one platform lists the size %v (partial %v), want its whole size", vs[0]["sizeBytes"], vs[0]["sizePartial"])
	}
}

func TestR_REPO_065_TheSwitchAndThePatternsStopABackgroundDownload(t *testing.T) {
	e, rep := probeEnv(t)
	busy, err := e.stub.Image("library/busybox", "latest")
	if err != nil {
		t.Fatal(err)
	}
	amd, _ := platformOf(t, busy, "linux/amd64")
	if a := e.get(rep, rProbeOff, "library/busybox/manifests/latest"); a.Status != http.StatusOK {
		t.Fatalf("the probe of the remote without the switch: %d", a.Status)
	}
	if a := e.get(rep, rProbeX, "library/nginx/manifests/latest"); a.Status != http.StatusNotFound {
		t.Fatalf("the excluded image answers %d, want 404", a.Status)
	}
	time.Sleep(3 * time.Second)
	if n := e.st.Number(`SELECT count(*) FROM job WHERE kind = 'remote-background-download'`); n != 0 {
		t.Errorf("%d background downloads were queued", n)
	}
	for _, b := range blobsOf(amd) {
		if e.stub.Counts().BlobFetches(b) != 0 {
			t.Errorf("a blob of busybox was fetched: %s", b)
		}
	}
	nginx, err := e.stub.Image("library/nginx", "latest")
	if err != nil {
		t.Fatal(err)
	}
	if n := e.stub.Counts().ManifestGets("library/nginx", ""); n != 0 || len(nginx.Manifests) == 0 {
		t.Errorf("the excluded image was asked %d times", n)
	}
}

// R-LIM-022, R-LIM-026, R-LIM-037: with the caller's upstream budget used up, enforce answers the probe from what it can and starts nothing;
// audit starts the download and records that it would have been limited.
func TestR_LIM_037_TheUpstreamBudgetOfTheProbesCallerDecides(t *testing.T) {
	e, rep := probeEnv(t)
	put := func(path string, body any, mt string) {
		t.Helper()
		raw, _ := json.Marshal(body)
		h := []string{"If-Match", "*"}
		if mt != "" {
			h = append(h, "Content-Type", mt)
		}
		method := http.MethodPut
		if mt != "" {
			method = http.MethodPatch
		}
		if a := e.st.Do(rep, method, path, e.admin, raw, h...); a.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", path, a.Status, a.Body)
		}
	}
	// One upstream request an hour: the probe's own fetch of the tag takes it, the decision on the background download finds none.
	put("/api/v1/limits/profiles/standard", map[string]any{"name": "Standard", "description": "test", "figures": map[string]any{"upstreamRequestsPerHour": 1}}, "")
	time.Sleep(1500 * time.Millisecond) // the change area reaches the replica's index
	other, err := e.stub.Image("other/c", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	a1, err := e.stub.Image("acme/a", "1.1")
	if err != nil {
		t.Fatal(err)
	}
	reader := e.st.Text(`SELECT id::text FROM principal WHERE kind = 'user' AND name = 'reader'`)
	events := func(mode string) int64 {
		return e.st.Number(`SELECT COALESCE(sum(count), 0) FROM usage_limit_event WHERE granularity = '5m' AND subject_ref = $1 AND figure = 'upstreamRequestsPerHour' AND mode = $2`, reader, mode)
	}

	t.Run("enforce: the probe is answered and nothing is downloaded", func(t *testing.T) {
		if a := e.get(rep, rProbe, "other/c/manifests/1.0"); a.Status != http.StatusOK {
			t.Fatalf("the probe: %d %s", a.Status, a.Body)
		}
		testenv.WaitFor(t, "the limit the decision met", 30*time.Second, func() bool { return events("enforce") >= 1 })
		time.Sleep(time.Second)
		for _, b := range blobsOf(other) {
			if e.stub.Counts().BlobFetches(b) != 0 {
				t.Errorf("a blob was fetched for a caller without upstream budget: %s", b)
			}
		}
		if n := e.st.Number(`SELECT count(*) FROM job WHERE kind = 'remote-background-download'`); n != 0 {
			t.Errorf("%d background downloads were queued", n)
		}
	})

	t.Run("audit: the download starts and would have been limited", func(t *testing.T) {
		put("/api/v1/limits/settings", map[string]any{"mode": "audit"}, "application/merge-patch+json")
		time.Sleep(1500 * time.Millisecond)
		if a := e.get(rep, rProbe, "acme/a/manifests/1.1"); a.Status != http.StatusOK {
			t.Fatalf("the probe in audit: %d %s", a.Status, a.Body)
		}
		var want []string
		for _, b := range blobsOf(a1) {
			want = append(want, "acme/a/_blobs/"+b)
		}
		testenv.WaitFor(t, "the background download in audit", 60*time.Second, func() bool { return e.stored(rProbe, want...) == int64(len(want)) })
		testenv.WaitFor(t, "the would_limit of the decision", 30*time.Second, func() bool { return events("audit") >= 1 })
	})
}
