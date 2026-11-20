//go:build integration

package vulnimage

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/ocistub"
)

// The flows of the role scanner against the stack (R-VULN-041, R-VULN-042): the worker's call to a scanner replica, what the call is put
// off for, the handshake both links require, and the scanner's pull as the subject system:image-scanner through the scan listener.

// R-VULN-041: with one scanner of one slot whose analysis waits, the job's second call finds the scanner busy and the image is put off
// with scanner_busy, its attempts untouched; it is scanned once the slot is free.
func TestR_VULN_041_ABusyScannerPutsTheScanOffWithoutAnAttempt(t *testing.T) {
	f := newFlowWith(t, options{laterWorker: true})
	release := f.an.holdAnalyses(t)
	f.push("one", "1.0", newImage(t))
	f.push("two", "1.0", variant(t, 1))
	f.startWorker() // the job finds both rows at once and calls for both (vulnerabilities.images.concurrency 2)
	f.runScan()
	testenv.WaitFor(t, "the second scan put off as busy", 30*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM vuln_image_scan WHERE error_code = 'scanner_busy' AND state = 'pending' AND attempts = 0`) == 1
	})
	if n := f.an.started.Load(); n != 1 {
		t.Fatalf("%d analyses started on a scanner of one slot", n)
	}
	release()
	testenv.WaitFor(t, "the held analysis to finish", 30*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM vuln_image_scan WHERE state = 'scanned'`) == 1
	})
	if n := f.st.Number(`SELECT count(*) FROM vuln_image_scan WHERE attempts > 0`); n != 0 {
		t.Fatalf("%d rows spent an attempt", n)
	}
}

// R-VULN-041, R-HA-065: a scanner replica stopped during an analysis drains: the analysis is ended and answered 503, the worker puts the
// image off without an attempt, and the image never reads as clean. A replica that dies without its shutdown (an OOM kill) leaves
// scanner_lost and one attempt instead; the in-process stack cannot kill a replica that way, so the client's unit test shows that path
// (internal/vuln/imagescan, Q-VULN-181).
func TestR_VULN_041_AScannerStoppedDuringAnAnalysisPutsTheScanOff(t *testing.T) {
	f := newFlow(t)
	f.an.holdAnalyses(t)
	f.push("app", "1.0", newImage(t))
	f.runScan()
	testenv.WaitFor(t, "the analysis to start", 30*time.Second, func() bool { return f.an.started.Load() == 1 })
	stopped := make(chan error, 1)
	go func() { stopped <- f.scanner.Stop() }()
	testenv.WaitFor(t, "the scan put off", 30*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM vuln_image_scan WHERE error_code IN ('scanner_busy', 'scanner_unavailable') AND state = 'pending' AND attempts = 0`) == 1
	})
	if err := <-stopped; err != nil {
		t.Fatalf("the scanner's shutdown: %v", err)
	}
	if n := f.an.analyses.Load(); n != 0 {
		t.Fatalf("%d analyses finished", n)
	}
	if ev := f.versionReport("app", "1.0").Repositories[0].Evaluation; ev != "pending_scan" {
		t.Fatalf("an image whose analysis was stopped reads as %q", ev)
	}
}

// R-VULN-041, R-ARCH-037, R-ARCH-038, Q-VULN-185: the scanner's listener and the scan listener refuse a client without a certificate of
// the internal CA, and a certificate of that CA of another kind, in the handshake; the scan listener refuses a request with the
// scanner's pair but without a scan token, and a user's token, with 401 and no challenge.
func TestR_VULN_041_TheLinksRefuseAClientWithoutTheCertificateOfTheirKind(t *testing.T) {
	f := newFlow(t)
	img := newImage(t)
	f.push("app", "1.0", img)
	manifest := "/v2/" + testenv.DefaultProject + "/" + repoKey + "/app/manifests/" + digestOf(img.manifest)
	get := func(cfg *tls.Config, addr, path, token string) (*http.Response, error) {
		c := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: cfg, DisableKeepAlives: true}}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://"+addr+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return c.Do(req)
	}
	for name, addr := range map[string]string{"the scanner's listener": f.scanner.ScannerAddr(), "the scan listener": f.api.Addr("scan")} {
		res, err := get(testenv.TLSConfig(t), addr, "/", "")
		if err == nil {
			_ = res.Body.Close()
			t.Errorf("%s answered %d to a client without a certificate", name, res.StatusCode)
		}
	}
	// R-ARCH-038, Q-VULN-185: a certificate of the CA admits only the one kind each listener serves. The stack's own server pair is a
	// certificate of the CA with neither kind's name; the pair of web carries the names of api and packages.
	shared, err := tls.LoadX509KeyPair(filepath.Join(testenv.CertDir(t), "server.crt"), filepath.Join(testenv.CertDir(t), "server.key"))
	if err != nil {
		t.Fatal(err)
	}
	with := func(c tls.Certificate) *tls.Config {
		cfg := testenv.TLSConfig(t)
		cfg.Certificates = []tls.Certificate{c}
		return cfg
	}
	for _, c := range []struct {
		name, addr string
		pair       tls.Certificate
	}{
		{"the scanner's listener with the pair of packages", f.scanner.ScannerAddr(), f.st.RolePair(t, "web")},
		{"the scanner's listener with the scanner's own pair", f.scanner.ScannerAddr(), f.st.RolePair(t, "scanner")},
		{"the scanner's listener with a certificate of the CA for another name", f.scanner.ScannerAddr(), shared},
		{"the scan listener with the worker's pair", f.api.Addr("scan"), f.st.RolePair(t, "worker")},
		{"the scan listener with a certificate of the CA for another name", f.api.Addr("scan"), shared},
	} {
		res, err := get(with(c.pair), c.addr, "/", "")
		if err == nil {
			_ = res.Body.Close()
			t.Errorf("%s answered %d", c.name, res.StatusCode)
		}
	}
	withPair := with(f.st.RolePair(t, "scanner"))
	for what, token := range map[string]string{"no token": "", "a user's token": f.adm} {
		res, err := get(withPair, f.api.Addr("scan"), manifest, token)
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized || res.Header.Get("WWW-Authenticate") != "" {
			t.Errorf("%s: %d, challenge %q; want 401 without a challenge", what, res.StatusCode, res.Header.Get("WWW-Authenticate"))
		}
	}
}

// R-VULN-042, Q-VULN-166, Q-LIM-66: an image in a private repository, blocked in triage, is analysed again after its analysis was removed and
// a new database is in use. The scanner's pull is no download: the block stays, no vulnerability.download_blocked is recorded, the version's
// download count and lastDownloadedAt are unchanged, and the usage store shows the bytes under the subject system.
func TestR_VULN_042_ABlockedPrivateImageIsAnalysedAgainWithoutADownload(t *testing.T) {
	flush := []string{"QUADER_STORAGE_STATS_FLUSH_INTERVAL=1s", "QUADER_LIMITS_FLUSH_INTERVAL=200ms", "QUADER_LIMITS_USAGE_FLUSH_INTERVAL=1s"}
	f := newFlowWith(t, options{webEnv: flush})
	img := newImage(t)
	f.push("app", "1.0", img)
	f.runScan()
	id := f.waitEvaluation("app", "1.0", "evaluated").Repositories[0].Findings[0].ID
	// A client's pull before the block counts one download.
	if a := f.do("GET", v2("app")+"/manifests/1.0", nil, "Accept", manifestType); a.Status != http.StatusOK {
		t.Fatalf("a pull: %d %s", a.Status, a.Body)
	}
	downloads := `SELECT COALESCE(string_agg(item_id::text || ':' || downloads || ':' || COALESCE(last_download_at::text, ''), ',' ORDER BY item_id), '')
		FROM item_download_stats WHERE downloads > 0`
	testenv.WaitFor(t, "the client's download counted", 30*time.Second, func() bool { return f.st.Text(downloads) != "" })
	before := f.st.Text(downloads)
	for _, status := range []string{"confirmed", "blocked"} {
		body, _ := json.Marshal(map[string]string{"status": status})
		if a := f.st.Do(f.api, "PATCH", "/api/v1/vulnerabilities/findings/"+id, f.adm, body, "If-Match", etagOf(t, f, id), "Content-Type", "application/merge-patch+json"); a.Status != http.StatusOK {
			t.Fatalf("set %s: %d %s", status, a.Status, a.Body)
		}
	}

	f.st.Exec(`DELETE FROM vuln_image_sbom`)
	f.setDB("sha256:db2")
	f.runScan()
	testenv.WaitFor(t, "the image analysed again", 60*time.Second, func() bool { return f.an.analyses.Load() == 2 })
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("usage: %s", f.st.Text(`SELECT COALESCE(string_agg(concat_ws(' ', granularity, subject_kind, subject_ref, credential_ref, repository_id, requests, cached_bytes), '; '), 'none') FROM usage_bucket`))
		}
	})
	system := `SELECT COALESCE(sum(requests), 0) FROM usage_bucket WHERE granularity = '5m' AND subject_kind = 'system' AND credential_ref = 'image-scanner'`
	testenv.WaitFor(t, "the pull in the usage store under system", 30*time.Second, func() bool { return f.st.Number(system) >= 3 })
	if n := f.st.Number(`SELECT COALESCE(sum(cached_bytes), 0) FROM usage_bucket WHERE subject_kind = 'system'`); n < int64(len(img.manifest)+len(img.config)+len(img.layer)) {
		t.Errorf("the usage store holds %d bytes under system, want at least the image's", n)
	}
	time.Sleep(2 * time.Second) // two flushes of the download statistics
	if got := f.st.Text(downloads); got != before {
		t.Errorf("the scanner's pull moved the downloads: %s, then %s", before, got)
	}
	if n := f.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'vulnerability.download_blocked'`); n != 0 {
		t.Errorf("%d vulnerability.download_blocked events for the scanner's pull", n)
	}
	if got := f.st.Text(`SELECT status FROM vuln_finding WHERE ecosystem = 'OCI'`); got != "blocked" {
		t.Errorf("the finding is %q after the analysis, want it still blocked", got)
	}
	if a := f.do("GET", v2("app")+"/manifests/1.0", nil, "Accept", manifestType); a.Status != http.StatusForbidden {
		t.Errorf("a client's pull of the blocked image: %d", a.Status)
	}
}

// R-VULN-042, Q-VULN-165: the scanner reads a remote repository from what it holds. A manifest whose layers no client brought ends the
// analysis with layer_missing and one attempt, and the upstream sees no request of the scan.
func TestR_VULN_042_ARemoteImageWithoutItsLayersEndsLayerMissing(t *testing.T) {
	ss, err := ocistub.NewSelfSigned(ocistub.LoopbackHosts...)
	if err != nil {
		t.Fatal(err)
	}
	stub, err := ocistub.New(ocistub.Config{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := stub.Start("127.0.0.1:0", ss.ServerConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = run.Close() })
	ca := testenv.WriteFile(t, t.TempDir(), "stub-ca.pem", string(ss.CAPEM))
	host := func(h string) map[string]any { return map[string]any{"pattern": h, "sendCredentials": false} }
	doc, err := json.Marshal(map[string]any{"remote": map[string]any{
		"url": run.URL, "extraHosts": []any{host(ocistub.LoopbackRealmHost), host(ocistub.LoopbackCDNHost)}, "authMode": "none", "username": "",
		"password": nil, "clientCertificate": nil, "headers": []any{}, "forwardAuthOnRedirect": false, "outboundProxy": map[string]any{"mode": "direct"},
		"connectTimeoutMs": 5000, "readTimeoutMs": 15000, "sourceNetwork": nil, "cacheFillAction": "read",
		"cache": map[string]any{
			"storeArtifacts": true, "metadataTtlSeconds": 300, "metadataRefreshTimeoutSeconds": 10, "notFoundTtlSeconds": 60,
			"maxStaleSeconds": 0, "unusedRetentionHours": 0,
		},
		"integrity":    map[string]any{"checksumPolicy": "verify-if-available", "blockUnexpectedContentTypes": true, "allowedContentTypes": []any{}},
		"availability": map[string]any{"mode": "online", "failureThreshold": 3, "circuitOpenSeconds": 30},
		"limits":       map[string]any{"maxArtifactBytes": nil, "maxMetadataBytes": nil, "maxConcurrentFetches": 16},
	}})
	if err != nil {
		t.Fatal(err)
	}
	f := newFlowWith(t, options{dial: run.DialOverride(), seed: func(st *testenv.Stack) { st.Repository("hub", "remote", "oci", string(doc)) }, stackEnv: []string{
		"QUADER_PROXY_OUTBOUND_TLS_CA_FILES=[" + strconv.Quote(filepath.ToSlash(ca)) + "]", `QUADER_PROXY_OUTBOUND_ADDRESS_POLICY_ALLOW_CIDRS=["127.0.0.1/32"]`,
	}})
	digest, ok := ocistub.Default().Digest("other/c", "1.0")
	if !ok {
		t.Fatal("the stub has no other/c:1.0")
	}
	// A pull of the manifest by its digest brings the manifest alone into the cache: a GET by digest is no probe (R-REPO-065).
	if a := f.st.Do(f.api, "GET", "/v2/"+testenv.DefaultProject+"/hub/other/c/manifests/"+digest, f.adm, nil, "Accept", "application/vnd.oci.image.manifest.v1+json"); a.Status != http.StatusOK {
		t.Fatalf("the manifest through the remote: %d %s", a.Status, a.Body)
	}
	before := stub.Counts().Total()
	f.runScan()
	testenv.WaitFor(t, "the analysis to end layer_missing", 60*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM vuln_image_scan WHERE error_code = 'layer_missing' AND attempts = 1`) == 1
	})
	if after := stub.Counts().Total(); after != before {
		t.Errorf("the scan made %d requests upstream: %s", after-before, stub.Counts().Summary())
	}
	if n := f.an.analyses.Load(); n != 0 {
		t.Errorf("%d analyses of an image the scanner could not pull", n)
	}
}

// R-VULN-042: the rows of a disabled repository are not due, so its images are not scanned while the job scans the images of another
// repository; enabling it makes them due again.
func TestR_VULN_042_TheImagesOfADisabledRepositoryAreNotScanned(t *testing.T) {
	f := newFlowWith(t, options{laterWorker: true, seed: func(st *testenv.Stack) {
		st.Repository("docker-other", "local", "oci", `{"vulnerabilities":{"onMissingData":"allow"}}`)
	}})
	f.push("app", "1.0", newImage(t))
	path := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + repoKey
	patch := func(body string) {
		t.Helper()
		got := f.st.Do(f.api, "GET", path, f.adm, nil)
		if got.Status != http.StatusOK {
			t.Fatalf("read the repository: %d %s", got.Status, got.Body)
		}
		if a := f.st.Do(f.api, "PATCH", path, f.adm, []byte(body), "If-Match", got.Header.Get("ETag"), "Content-Type", "application/merge-patch+json"); a.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", body, a.Status, a.Body)
		}
	}
	patch(`{"disabled":true}`)
	other := variant(t, 2)
	for _, b := range [][]byte{other.config, other.layer} {
		if a := f.do("POST", "/v2/"+testenv.DefaultProject+"/docker-other/other/blobs/uploads/?digest="+digestOf(b), b, "Content-Type", "application/octet-stream"); a.Status != http.StatusCreated {
			t.Fatalf("blob upload: %d %s", a.Status, a.Body)
		}
	}
	if a := f.do("PUT", "/v2/"+testenv.DefaultProject+"/docker-other/other/manifests/1.0", other.manifest, "Content-Type", manifestType); a.Status != http.StatusCreated {
		t.Fatalf("manifest push: %d %s", a.Status, a.Body)
	}
	f.startWorker()
	f.runScan()
	testenv.WaitFor(t, "the image of the enabled repository scanned", 60*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM vuln_image_scan s JOIN repositories r ON r.id = s.repo_id WHERE r.key = 'docker-other' AND s.state = 'scanned'`) == 1
	})
	if n := f.st.Number(`SELECT count(*) FROM vuln_image_scan s JOIN repositories r ON r.id = s.repo_id WHERE r.key = $1 AND (s.state <> 'pending' OR s.attempts > 0)`, repoKey); n != 0 {
		t.Fatalf("the disabled repository's image was taken up")
	}
	if n := f.an.analyses.Load(); n != 1 {
		t.Fatalf("%d analyses, want the enabled repository's one", n)
	}
	patch(`{"disabled":false}`)
	// The scan listener decides by the replica's grant index, which follows the change at its next epoch: the scan is started once a
	// client's pull through the same replica sees the repository again, so that the pull is not refused as one of a disabled one.
	testenv.WaitFor(t, "the repository readable again", 30*time.Second, func() bool {
		return f.do("GET", v2("app")+"/manifests/1.0", nil, "Accept", manifestType).Status == http.StatusOK
	})
	f.runScan()
	testenv.WaitFor(t, "the image scanned after the repository was enabled", 60*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM vuln_image_scan s JOIN repositories r ON r.id = s.repo_id WHERE r.key = $1 AND s.state = 'scanned'`, repoKey) == 1
	})
}

// R-VULN-041, Q-VULN-168: before any database exists the job still has the scanner analyse what has no analysis; the row stays pending,
// the evaluation never reads as clean, and the first database detects without reading a layer again.
func TestR_VULN_041_AnImageIsAnalysedBeforeAnyDatabaseExists(t *testing.T) {
	f := newFlowWith(t, options{noDatabase: true})
	img := newImage(t)
	f.push("app", "1.0", img)
	f.runScan()
	testenv.WaitFor(t, "the analysis stored without a database", 30*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM vuln_image_sbom`) == 1
	})
	if n := f.an.detections.Load(); n != 0 {
		t.Fatalf("%d detections without a database", n)
	}
	if got := f.st.Text(`SELECT state FROM vuln_image_scan`); got != "pending" {
		t.Fatalf("the row is %q without a database", got)
	}
	if ev := f.versionReport("app", "1.0").Repositories[0].Evaluation; ev != "pending_data" && ev != "pending_scan" {
		t.Fatalf("an image analysed without a database reads as %q", ev)
	}
	f.setDB("sha256:db1")
	f.runScan()
	f.waitEvaluation("app", "1.0", "evaluated")
	if n := f.an.analyses.Load(); n != 1 {
		t.Fatalf("the first database read the layers again: %d analyses", n)
	}
}

// R-HA-067, Q-ARCH-551: a replica of the role all builds its own scanner. Without the analyser binary it stays ready, and reports the
// scanner as degraded: the scanner is one part of a process that serves every route (the UI development profile runs this role).
func TestR_HA_067_TheRoleAllStaysReadyWithoutTheAnalyser(t *testing.T) {
	st := testenv.NewStack(t, testenv.StackOptions{Name: "vulnall", TLS: true, Env: []string{
		"QUADER_VULNERABILITIES_SOURCES_OSV_MODE=off", "QUADER_VULNERABILITIES_SOURCES_KEV_MODE=off",
		"QUADER_VULNERABILITIES_SOURCES_TRIVY_MODE=off", "QUADER_MAINTENANCE_TASKS=" + taskOff,
		"QUADER_VULNERABILITIES_IMAGES_BINARY=" + filepath.Join(t.TempDir(), "no-analyser"),
		"QUADER_VULNERABILITIES_IMAGES_SCAN_DIR=" + t.TempDir(),
	}})
	all := st.Start(t, testenv.ReplicaOptions{Name: "all", Role: "all"})
	if all.ScannerAddr() == "" || all.Addr("scan") == "" {
		t.Fatalf("the role all opened no scanner's listener (%q) or scan listener (%q)", all.ScannerAddr(), all.Addr("scan"))
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("health: %+v", all.App.Health().Snapshot())
		}
	})
	testenv.WaitFor(t, "the replica ready, the scanner reported", 30*time.Second, func() bool {
		snap := all.App.Health().Snapshot()
		return len(snap.Failing) == 0 && slices.Contains(snap.Degraded, "scanner")
	})
}

// variant is the test image with another layer, so another digest; n tells the variants apart.
func variant(t *testing.T, n int) image {
	t.Helper()
	img := newImage(t)
	img.layer = append(img.layer, make([]byte, 512*n)...)
	m, err := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": manifestType,
		"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": digestOf(img.config), "size": len(img.config)},
		"layers": []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar", "digest": digestOf(img.layer), "size": len(img.layer)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	img.manifest = m
	return img
}
