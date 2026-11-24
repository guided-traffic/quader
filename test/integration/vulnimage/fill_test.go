//go:build integration

package vulnimage

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/app"
	"github.com/guided-traffic/quader/internal/malware/clamd/clamdtest"
	"github.com/guided-traffic/quader/internal/malware/scanner/scannertest"
	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/ocistub"
)

// hubKey is the remote repository of the stub's registry.
const hubKey = "hub"

// hub is the stub's registry over TLS and the remote repository of it, which a test seeds before the replicas start.
type hub struct {
	stub *ocistub.Server
	run  *ocistub.Running
	ca   string
	// oci is the repository's member oci, nil for none.
	oci map[string]any
}

// newHub starts the stub's registry with its own CA. oci is the remote repository's member oci (its platforms), nil for none.
func newHub(t *testing.T, oci map[string]any) *hub {
	t.Helper()
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
	return &hub{stub: stub, run: run, ca: testenv.WriteFile(t, t.TempDir(), "stub-ca.pem", string(ss.CAPEM)), oci: oci}
}

// options adds the remote repository hub of the stub to o: the seed, the web replica's dial to the stub's names, and the outbound settings
// that trust the stub's CA and its loopback address. The background fetch of an image's signatures (signatures.md R-SIG-026) is off, so
// that what the upstream sees is the test's.
func (h *hub) options(o options) options {
	host := func(p string) map[string]any { return map[string]any{"pattern": p, "sendCredentials": false} }
	body := map[string]any{"remote": map[string]any{
		"url": h.run.URL, "extraHosts": []any{host(ocistub.LoopbackRealmHost), host(ocistub.LoopbackCDNHost)}, "authMode": "none", "username": "",
		"password": nil, "clientCertificate": nil, "headers": []any{}, "forwardAuthOnRedirect": false, "outboundProxy": map[string]any{"mode": "direct"},
		"connectTimeoutMs": 5000, "readTimeoutMs": 15000, "sourceNetwork": nil, "cacheFillAction": "read",
		"cache": map[string]any{
			"storeArtifacts": true, "metadataTtlSeconds": 300, "metadataRefreshTimeoutSeconds": 10, "notFoundTtlSeconds": 60,
			"maxStaleSeconds": 0, "unusedRetentionHours": 0,
		},
		"integrity":    map[string]any{"checksumPolicy": "verify-if-available", "blockUnexpectedContentTypes": true, "allowedContentTypes": []any{}},
		"availability": map[string]any{"mode": "online", "failureThreshold": 3, "circuitOpenSeconds": 30},
		"limits":       map[string]any{"maxArtifactBytes": nil, "maxMetadataBytes": nil, "maxConcurrentFetches": 16},
	}, "signatures": map[string]any{"fetch": false}, "vulnerabilities": map[string]any{"onMissingData": "allow"}}
	if h.oci != nil {
		body["oci"] = h.oci
	}
	doc, err := json.Marshal(body)
	if err != nil {
		panic(err) // a map of plain values
	}
	seed := o.seed
	o.seed = func(st *testenv.Stack) {
		st.Repository(hubKey, "remote", "oci", string(doc))
		if seed != nil {
			seed(st)
		}
	}
	o.dial, o.workerDial = h.run.DialOverride(), h.run.DialOverride()
	o.stackEnv = append(o.stackEnv, "QUADER_PROXY_OUTBOUND_TLS_CA_FILES=["+strconv.Quote(filepath.ToSlash(h.ca))+"]",
		`QUADER_PROXY_OUTBOUND_ADDRESS_POLICY_ALLOW_CIDRS=["127.0.0.1/32"]`)
	return o
}

// The schedules of both scans are off: what a fill stores reaches each scan through the wake-up of its commit alone.
const scansOff = `{"vuln-sync-osv":{"enabled":false,"schedule":"*/15 * * * *"},"vuln-sync-osv-full":{"enabled":false,"schedule":"17 3 * * 0"},` +
	`"vuln-sync-kev":{"enabled":false,"schedule":"41 */6 * * *"},"vuln-sync-trivy-db":{"enabled":false,"schedule":"23 */6 * * *"},` +
	`"vuln-image-scan":{"enabled":false,"schedule":"*/5 * * * *"},"malware-scan":{"enabled":false,"schedule":"*/5 * * * *"}}`

// R-VULN-050, R-MAL-035: an image that a download ahead stores (R-CUR-010, the queue's run on the worker, the path of a mirror pass and of
// an examination as well) and an image that a client's pull fills (the remote engine on the web replica) are each scanned for
// vulnerabilities and checked for malware without a schedule and without a start of a task: the schedules of both scans are off, and the
// commit of what the fill stores wakes each job. Both tags are indexes of the stub with attestation manifests: the download ahead stores
// the platform of oci.platforms with its attestation, which is checked for malware and not scanned for vulnerabilities (R-VULN-033); the
// client pulls the other platform by its digest, and its GET of the tag is a probe that stores the platform of oci.platforms beside it in
// the background (R-REPO-065), as a docker pull through Quader does. Integration, about 8 s, the stub's images of kilobytes.
func TestR_VULN_050_R_MAL_035_ADownloadAheadAndAClientsFillAreScannedAtOnceWithoutASchedule(t *testing.T) {
	hub := newHub(t, map[string]any{"platforms": []string{"linux/amd64"}})
	clamd := clamdtest.Start(t, clamdtest.Options{StreamMaxLength: 64 << 10})
	const malwareDB = "main-62.daily-27841.bytecode-339"
	// A scan delay as in production, shorter: a fill commits a manifest before its layers, and a scan at once ends layer_missing.
	stackEnv := []string{"QUADER_MAINTENANCE_TASKS=" + scansOff, "QUADER_VULNERABILITIES_IMAGES_SCAN_DELAY=2s"}
	f := newFlowWith(t, hub.options(options{laterWorker: true, stackEnv: stackEnv, seed: func(st *testenv.Stack) {
		st.Exec(`UPDATE malware_database SET version = $1, files = '[]', built_at = now() - interval '1 hour', imported_at = now(),
			repository = 'upstream/clamav'`, malwareDB)
	}}))
	malware := f.st.Start(t, testenv.ReplicaOptions{
		Name: "malware-scanner-clamav", Role: "scanner", Pair: "malware-scanner-clamav",
		Malware: &app.MalwareOverride{Database: scannertest.NewDatabase(malwareDB)}, Env: []string{
			"QUADER_PROCESS_SCANNER_KIND=clamav", "QUADER_MALWARE_SCANNER_SOCKET=" + clamd.Path,
			"QUADER_MALWARE_SCAN_LISTENER_URL=" + f.st.Origin(f.api.Addr("scan")), "QUADER_STORAGE_SPOOL_DIR=" + filepath.Join(t.TempDir(), "spool"),
		},
	})
	f.o.workerEnv = append(f.o.workerEnv, "QUADER_MALWARE_SCANNER_URL="+f.st.Origin(malware.ScannerAddr()))
	f.startWorker()
	hubV2 := "/v2/" + testenv.DefaultProject + "/" + hubKey + "/"

	// The download ahead of a tag through the API, which the queue runs on the worker.
	path := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + hubKey
	a := f.do("POST", path+"/downloads", mustJSON(t, map[string]any{"level": "version", "name": "library/alpine", "version": "3.20"}), "Content-Type", "application/json")
	if a.Status != http.StatusAccepted {
		t.Fatalf("the download ahead: %d %s", a.Status, a.Body)
	}
	id := a.JSON(t)["download"].(map[string]any)["id"].(string)
	var row map[string]any
	testenv.WaitFor(t, "the download ahead done", 60*time.Second, func() bool {
		row = f.do("GET", path+"/downloads/"+id, nil).JSON(t)
		return row["state"] == "done" || row["state"] == "failed"
	})
	if row["state"] != "done" || row["outcome"] != "completed" {
		t.Fatalf("the download ahead's row: %v", row)
	}
	alpine, err := hub.stub.Image("library/alpine", "3.20")
	if err != nil {
		t.Fatal(err)
	}
	amd64, attestation := childrenOf(t, alpine, "linux/amd64")

	// A client's pull of another image: the index by its tag, one platform's manifest, its config and its layers.
	b, err := hub.stub.Image("acme/b", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	pulled, _ := childrenOf(t, b, "linux/arm64/v8")
	_, probed := childrenOf(t, b, "linux/amd64")
	accept := "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json"
	for _, p := range append([]string{"acme/b/manifests/1.0", "acme/b/manifests/" + pulled.Digest, "acme/b/blobs/" + pulled.Config.Digest}, layerPaths("acme/b", pulled)...) {
		if a := f.do("GET", hubV2+p, nil, "Accept", accept); a.Status != http.StatusOK {
			t.Fatalf("the client's GET %s: %d %s", p, a.Status, a.Body)
		}
	}

	// Each tag is evaluated from the scans of its stored platform images, without a start of the task.
	for _, tag := range []struct{ name, version, image string }{{"library/alpine", "3.20", amd64.Digest}, {"acme/b", "1.0", pulled.Digest}} {
		r := f.waitEvaluation(tag.name, tag.version, "evaluated")
		found := false
		for _, m := range r.Repositories[0].Images {
			found = found || m.Digest == tag.image
			if m.State != "scanned" {
				t.Errorf("%s:%s reads the image %s %s", tag.name, tag.version, m.Digest, m.State)
			}
		}
		if !found {
			t.Fatalf("%s:%s reads the images %+v, without %s", tag.name, tag.version, r.Repositories[0].Images, tag.image)
		}
	}
	for _, att := range []string{attestation.Digest, probed.Digest} {
		if n := f.st.Number(`SELECT count(*) FROM vuln_image_scan WHERE digest = decode($1, 'hex')`, strings.TrimPrefix(att, "sha256:")); n != 0 {
			t.Errorf("the attestation manifest %s has %d scan rows", att, n)
		}
	}

	// Every file the two fills stored, the attestation's manifest and its in-toto layer included, is checked for malware.
	files := `SELECT count(*) FROM items i JOIN repositories r ON r.id = i.repo_id
		WHERE r.key = $1 AND i.kind = 'file' AND NOT i.generated AND i.trashed_at IS NULL AND (i.path LIKE 'library/alpine/%' OR i.path LIKE 'acme/b/%')`
	unchecked := files + ` AND NOT EXISTS (SELECT 1 FROM malware_scan m WHERE m.sha256 = i.blob_sha256 AND m.state = 'clean')`
	testenv.WaitFor(t, "every stored file of both fills checked clean", 30*time.Second, func() bool { return f.st.Number(unchecked, hubKey) == 0 })
	want := int64(2 + len(amd64.Layers) + 2 + len(attestation.Layers) + 2 + len(pulled.Layers)) // each manifest with its config and layers
	if n := f.st.Number(files, hubKey); n < want {
		t.Errorf("%d files of the two fills are stored, want at least %d", n, want)
	}
}

// childrenOf finds the child of an index of the stub by its platform, and the attestation manifest of that child.
func childrenOf(t *testing.T, idx ocistub.ImageInfo, platform string) (img, attestation ocistub.ImageInfo) {
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
	if img.Digest == "" || img.Config == nil {
		t.Fatalf("%s has no image of %s: %+v", idx.Name, platform, idx.Manifests)
	}
	return img, attestation
}

func layerPaths(name string, img ocistub.ImageInfo) []string {
	out := make([]string, 0, len(img.Layers))
	for _, l := range img.Layers {
		out = append(out, name+"/blobs/"+l.Digest)
	}
	return out
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
