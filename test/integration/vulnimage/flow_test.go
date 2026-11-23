//go:build integration

// Package vulnimage is the integration tier's flow of the scanning of container images (R-VULN-033 to R-VULN-037, R-VULN-041, R-VULN-042,
// R-VULN-048, R-VULN-049): a push through the registry protocol, the job on a worker, its two calls over mutual TLS to a replica of the role
// scanner (the analysis, with the scanner's pull through the scan listener of a web replica, and the detection of the stored analysis), the
// findings through the package API, and the block of a pull in the registry's own error shape. Everything runs against the integration
// stack, with the stack's pair and CA on every listener and link, except the analyser, which is replaced at the seam in the scanner so that
// the tier needs no binary. The database is a directory of the test that the flow writes as the loader would, or, with realDatabase, an
// archive of bytes in a repository of the stack that a replica of the kind trivy-db loads through the scan listener.
package vulnimage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/app"
	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/packages/view"
	"github.com/guided-traffic/quader/internal/vuln/imagescan"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

func TestMain(m *testing.M) { os.Exit(testenv.Main(m)) }

const (
	manifestType = "application/vnd.oci.image.manifest.v1+json"
	repoKey      = "docker-local"
	cve          = "CVE-2024-3094"
	taskOff      = `{"vuln-sync-osv":{"enabled":false,"schedule":"*/15 * * * *"},"vuln-sync-osv-full":{"enabled":false,"schedule":"17 3 * * 0"},` +
		`"vuln-sync-kev":{"enabled":false,"schedule":"41 */6 * * *"},"vuln-sync-trivy-db":{"enabled":false,"schedule":"23 */6 * * *"},` +
		`"vuln-image-scan":{"enabled":false,"schedule":"*/5 * * * *"}}`
)

// fakeAnalyser is the analyser behind the seam in the scanner replica, for both steps: it counts how often the layers are read and how
// often a stored analysis is detected, and answers one advisory for the package xz.
type fakeAnalyser struct {
	// started counts the analyses that began; analyses those that read the layout and wrote their list.
	started, analyses, detections atomic.Int64
	// report is the JSON report the detection leaves.
	report atomic.Pointer[string]
	fail   atomic.Pointer[imagescan.ScanError]
	// hold, while set, keeps every analysis waiting until it is closed or the analysis's context ends.
	hold atomic.Pointer[chan struct{}]
	// database is the directory of the last detection's database.
	database atomic.Pointer[string]
}

func (a *fakeAnalyser) Version() string { return "0.0.0-test" }

func (a *fakeAnalyser) Analyse(ctx context.Context, layout, sbom, _ string, _ imagescan.Limits) error {
	a.started.Add(1)
	if h := a.hold.Load(); h != nil {
		select {
		case <-*h:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
	if e := a.fail.Load(); e != nil {
		return e
	}
	a.analyses.Add(1)
	if _, err := os.Stat(layout + "/index.json"); err != nil {
		return errors.New("the analyser got no OCI layout")
	}
	return os.WriteFile(sbom, []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","components":[{"type":"library","name":"xz"},{"type":"operating-system","name":"alpine"}]}`), 0o600)
}

// holdAnalyses makes every analysis wait until the returned function is called; the test's cleanup calls it too.
func (a *fakeAnalyser) holdAnalyses(t *testing.T) func() {
	t.Helper()
	ch := make(chan struct{})
	a.hold.Store(&ch)
	var once atomic.Bool
	release := func() {
		if once.CompareAndSwap(false, true) {
			a.hold.Store(nil)
			close(ch)
		}
	}
	t.Cleanup(release)
	return release
}

func (a *fakeAnalyser) Detect(_ context.Context, sbom, out, _, database string, _ imagescan.Limits) error {
	a.detections.Add(1)
	if _, ok := imagescan.DatabaseIn(database); !ok {
		return errors.New("the detection got no complete database at " + database)
	}
	if doc, err := os.ReadFile(sbom); err != nil || !strings.Contains(string(doc), `"bomFormat":"CycloneDX"`) { //nolint:gosec // the scanner's working directory
		return errors.New("the detection got no stored analysis")
	}
	a.database.Store(&database)
	return os.WriteFile(out, []byte(*a.report.Load()), 0o600)
}

// fakeDatabase is the worker's look-up of the database in use (R-VULN-044); its digest changes when the test publishes a new one.
type fakeDatabase struct{ digest atomic.Pointer[string] }

func (d *fakeDatabase) InUse(context.Context) (string, error) {
	v := d.digest.Load()
	if v == nil {
		return "", imagescan.ErrNoDatabase
	}
	return *v, nil
}

// dbDigest is the digest of the n-th database the flow publishes.
func dbDigest(n int) string { return "sha256:" + strings.Repeat("0", 63) + strconv.Itoa(n%10) }

func reportWith(fixed string) string {
	b, _ := json.Marshal(map[string]any{
		"SchemaVersion": 2,
		"Metadata":      map[string]any{"OS": map[string]any{"Family": "alpine", "Name": "3.19.1", "EOSL": false}},
		"Results": []map[string]any{{
			"Class": "os-pkgs", "Type": "alpine",
			"Vulnerabilities": []map[string]any{{
				"VulnerabilityID": cve, "PkgName": "xz", "InstalledVersion": "5.6.0-r0", "FixedVersion": fixed, "Severity": "CRITICAL", "SeveritySource": "nvd",
				"Title": "xz backdoor", "Description": "Malicious code in the upstream tarballs.", "References": []string{"https://example.org/a"},
				"CweIDs": []string{"CWE-506"}, "CVSS": map[string]any{"nvd": map[string]any{"V3Vector": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:C/C:H/I:H/A:H", "V3Score": 10.0}},
				"PublishedDate": "2024-03-29T17:15:21Z", "LastModifiedDate": "2024-04-01T10:00:00Z",
			}},
		}},
	})
	return string(b)
}

func digestOf(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func layerTar(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := []byte("hello")
	if err := tw.WriteHeader(&tar.Header{Name: "etc/motd", Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type image struct{ config, layer, manifest []byte }

func newImage(t *testing.T) image {
	t.Helper()
	cfg := []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}`)
	l := layerTar(t)
	m, err := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": manifestType,
		"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": digestOf(cfg), "size": len(cfg)},
		"layers": []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar", "digest": digestOf(l), "size": len(l)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return image{config: cfg, layer: l, manifest: m}
}

type flow struct {
	t       *testing.T
	st      *testenv.Stack
	api     *testenv.Replica
	scanner *testenv.Replica
	// loader is the replica of the kind trivy-db beside the scanner (realDatabase only), and dbDir their shared database directory.
	loader *testenv.Replica
	dbDir  string
	adm    string
	an     *fakeAnalyser
	worker *testenv.Replica
	db     *fakeDatabase
	o      options
	ov     *app.VulnOverride
}

func v2(name string) string { return "/v2/" + testenv.DefaultProject + "/" + repoKey + "/" + name }

// options are what a test changes of the installation.
type options struct {
	// realDatabase leaves the worker's look-up of the database to the source state and the repository (R-VULN-044), and starts the loader
	// of the kind trivy-db beside the scanner, which reads the layer through the scan listener (R-VULN-049).
	realDatabase bool
	// noDatabase keeps the database seam but publishes no database: Acquire answers ErrNoDatabase.
	noDatabase bool
	// workerEnv is added to the environment of the worker, scannerEnv to the scanner's.
	workerEnv, scannerEnv []string
	// stackEnv is added to the environment of every replica, webEnv to the web replica's.
	stackEnv, webEnv []string
	// dial is the web replica's dial override: the names of an upstream stub; workerDial is the worker's, whose database task reads a
	// remote repository in process (R-VULN-044).
	dial, workerDial map[string]string
	// laterWorker leaves the start of the worker to the test (startWorker), so that what a test sets up before is not scanned yet.
	laterWorker bool
	// seed writes rows into the stack before any replica starts: a repository a replica must see from its start.
	seed func(st *testenv.Stack)
}

func newFlow(t *testing.T) *flow { return newFlowWith(t, options{}) }

func newFlowWith(t *testing.T, o options) *flow {
	t.Helper()
	dir := t.TempDir()
	// The stack serves every listener with its pair and makes its CA the internal CA: the worker's call to the scanner and the scanner's
	// pull from the scan listener are mutual TLS, as in the chart (R-HA-066).
	st := testenv.NewStack(t, testenv.StackOptions{Name: "vulnimage", TLS: true, Env: append([]string{
		"QUADER_VULNERABILITIES_SOURCES_OSV_MODE=off", "QUADER_VULNERABILITIES_SOURCES_KEV_MODE=off",
		"QUADER_VULNERABILITIES_IMAGES_SCAN_DIR=" + dir, "QUADER_VULNERABILITIES_IMAGES_SCAN_DELAY=0s",
		"QUADER_MAINTENANCE_TASKS=" + taskOff, "QUADER_CLUSTER_EPOCH_POLL_INTERVAL=500ms", "QUADER_JOBS_POLL_INTERVAL=200ms",
	}, o.stackEnv...)})
	f := &flow{t: t, st: st, an: &fakeAnalyser{}, db: &fakeDatabase{}, o: o}
	rita := st.User("rita")
	st.Bind("admin", rita)
	f.adm = st.Token(rita, scope.Scope{Inherit: true, Admin: true})
	st.Repository(repoKey, "local", "oci", `{"vulnerabilities":{"onMissingData":"allow"}}`)
	if o.seed != nil {
		o.seed(st)
	}
	f.setReport(reportWith("5.6.1-r2"))
	f.dbDir = t.TempDir()
	ov := &app.VulnOverride{}
	if !o.realDatabase {
		if !o.noDatabase {
			f.setDB(dbDigest(1))
		}
		ov.ImageDatabase = f.db
	}
	f.ov = ov
	// The web replica opens the scan listener beside its client listener (R-ARCH-037); the scanner pulls from it, with a spool and a
	// working directory of its own, as its pod has (R-HA-065), and detects with the database directory its loader writes (R-VULN-049).
	f.api = st.Start(t, testenv.ReplicaOptions{Name: "web", Role: "web", DialOverride: o.dial, Env: append([]string{"QUADER_SERVER_SCAN_LISTEN=127.0.0.1:0"}, o.webEnv...)})
	own := t.TempDir()
	f.scanner = st.Start(t, testenv.ReplicaOptions{Name: "vuln-scanner-trivy", Role: "scanner", Pair: "vuln-scanner-trivy", Vuln: &app.VulnOverride{ImageAnalyser: f.an}, Env: append([]string{
		"QUADER_VULNERABILITIES_IMAGES_REGISTRY_URL=" + st.Origin(f.api.Addr("scan")), "QUADER_VULNERABILITIES_IMAGES_DATABASE_DIR=" + f.dbDir,
		"QUADER_VULNERABILITIES_IMAGES_SCAN_DIR=" + filepath.Join(own, "scan"), "QUADER_STORAGE_SPOOL_DIR=" + filepath.Join(own, "spool"),
	}, o.scannerEnv...)})
	if o.realDatabase {
		f.startLoader()
	}
	if !o.laterWorker {
		f.startWorker()
	}
	return f
}

// startLoader starts the loader of the kind trivy-db beside the scanner, with the scanner's pair and database directory, as the second
// container of its pod (R-HA-070); it asks the listing every 200 ms instead of every 5 s or 30 s.
func (f *flow) startLoader() {
	f.t.Helper()
	f.loader = f.st.Start(f.t, testenv.ReplicaOptions{
		Name: "vuln-scanner-trivy-db", Role: "scanner", Pair: "vuln-scanner-trivy",
		Vuln: &app.VulnOverride{LoaderPoll: 200 * time.Millisecond}, Env: []string{
			"QUADER_PROCESS_SCANNER_KIND=trivy-db", "QUADER_VULNERABILITIES_IMAGES_REGISTRY_URL=" + f.st.Origin(f.api.Addr("scan")),
			"QUADER_VULNERABILITIES_IMAGES_DATABASE_DIR=" + f.dbDir,
		},
	})
}

// waitLoaded waits until the scanner's database directory holds the database digest completely.
func (f *flow) waitLoaded(digest string) {
	f.t.Helper()
	testenv.WaitFor(f.t, "the loader to hold "+digest, 30*time.Second, func() bool {
		_, ok := imagescan.DatabaseIn(filepath.Join(f.dbDir, strings.TrimPrefix(digest, "sha256:")))
		return ok
	})
}

// startWorker starts the worker, which calls the stack's scanner.
func (f *flow) startWorker() {
	f.t.Helper()
	env := append([]string{"QUADER_VULNERABILITIES_IMAGES_SCANNER_URL=" + f.st.Origin(f.scanner.ScannerAddr())}, f.o.workerEnv...)
	f.worker = f.st.Start(f.t, testenv.ReplicaOptions{Name: "worker", Role: "worker", Vuln: f.ov, Env: env, DialOverride: f.o.workerDial})
}

// setDB publishes the database d: the worker's look-up names it, the state row says it as the database's sync writes it, and the
// scanner's database directory holds it as the loader renames it into place (R-VULN-049).
func (f *flow) setDB(d string) {
	f.t.Helper()
	dir := filepath.Join(f.dbDir, strings.TrimPrefix(d, "sha256:"), imagescan.DatabaseDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		f.t.Fatal(err)
	}
	for name, body := range map[string]string{"metadata.json": `{"Version":2,"UpdatedAt":"2026-11-21T07:02:51Z"}`, "trivy.db": "database bytes"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			f.t.Fatal(err)
		}
	}
	f.db.digest.Store(&d)
	f.st.Exec(`INSERT INTO vuln_source_state (source, ecosystem, state, etag, synced_at, full_synced_at) VALUES ('trivy', 'OCI', 'complete', $1, now(), now())
		ON CONFLICT (source, ecosystem) DO UPDATE SET state = 'complete', etag = $1, synced_at = now()`, d)
}

func (f *flow) setReport(r string) { f.an.report.Store(&r) }

func (f *flow) do(method, path string, body []byte, hdr ...string) testenv.Answer {
	f.t.Helper()
	var b any
	if body != nil {
		b = body
	}
	return f.st.Do(f.api, method, path, f.adm, b, hdr...)
}

func (f *flow) push(name, tag string, img image) {
	f.t.Helper()
	for _, b := range [][]byte{img.config, img.layer} {
		if a := f.do("POST", v2(name)+"/blobs/uploads/?digest="+digestOf(b), b, "Content-Type", "application/octet-stream"); a.Status != http.StatusCreated {
			f.t.Fatalf("blob upload: %d %s", a.Status, a.Body)
		}
	}
	if a := f.do("PUT", v2(name)+"/manifests/"+tag, img.manifest, "Content-Type", manifestType); a.Status != http.StatusCreated {
		f.t.Fatalf("manifest push: %d %s", a.Status, a.Body)
	}
}

func (f *flow) runScan() {
	f.t.Helper()
	if a := f.st.Do(f.api, "POST", "/api/v1/system/tasks/vuln-image-scan/run", f.adm, nil); a.Status != http.StatusAccepted && a.Status != http.StatusConflict {
		f.t.Fatalf("start the scan: %d %s", a.Status, a.Body)
	}
}

// runFreshScan starts a run of the scan task that begins after the call: a run still in progress answers 409 and would not see what the
// test changed since it began (a new image, a new database), so the start is repeated until the task accepts a new run.
func (f *flow) runFreshScan() {
	f.t.Helper()
	testenv.WaitFor(f.t, "a new run of the scan task accepted", 30*time.Second, func() bool {
		a := f.st.Do(f.api, "POST", "/api/v1/system/tasks/vuln-image-scan/run", f.adm, nil)
		if a.Status != http.StatusAccepted && a.Status != http.StatusConflict {
			f.t.Fatalf("start the scan: %d %s", a.Status, a.Body)
		}
		return a.Status == http.StatusAccepted
	})
}

type versionReport struct {
	Repositories []struct {
		Evaluation string `json:"evaluation"`
		Blocked    bool   `json:"blocked"`
		Findings   []struct {
			ID            string   `json:"id"`
			Advisory      string   `json:"advisory"`
			Version       string   `json:"version"`
			Severity      string   `json:"severity"`
			Status        string   `json:"status"`
			FixedVersions []string `json:"fixedVersions"`
			Components    []struct {
				Name, Version, FixedVersion string
				Type, Class                 string
			} `json:"components"`
		} `json:"findings"`
		Images []struct {
			Digest, State string
			OsFamily      string `json:"osFamily"`
			Packages      int    `json:"packages"`
		} `json:"images"`
		Grade imageGrade `json:"grade"`
	} `json:"repositories"`
}

// imageGrade is the grade of a tag in the answers (R-VULN-039).
type imageGrade struct {
	Grade      string  `json:"grade"`
	Eosl       bool    `json:"eosl"`
	Os         *string `json:"os"`
	CheckedAt  *string `json:"checkedAt"`
	Evaluation string  `json:"evaluation"`
	Fixable    int64   `json:"fixable"`
	Open       struct {
		Critical int64 `json:"critical"`
	} `json:"open"`
}

func (f *flow) versionReport(name, tag string) versionReport {
	f.t.Helper()
	a := f.do("GET", "/api/v1/packages/"+view.ID("oci", name)+"/versions/"+tag+"/vulnerabilities", nil)
	if a.Status != http.StatusOK {
		f.t.Fatalf("version report: %d %s", a.Status, a.Body)
	}
	var r versionReport
	if err := json.Unmarshal(a.Body, &r); err != nil {
		f.t.Fatal(err)
	}
	if len(r.Repositories) != 1 {
		f.t.Fatalf("%d repositories in %s", len(r.Repositories), a.Body)
	}
	return r
}

func (f *flow) waitEvaluation(name, tag, want string) versionReport {
	f.t.Helper()
	var r versionReport
	testenv.WaitFor(f.t, "the evaluation "+want, 60*time.Second, func() bool {
		r = f.versionReport(name, tag)
		return r.Repositories[0].Evaluation == want
	})
	return r
}

func TestR_VULN_034_AnImageIsScannedAfterItsPushAndItsFindingsReachThePackageAPI(t *testing.T) {
	f := newFlow(t)
	img := newImage(t)
	f.push("app", "1.0", img)

	// The commit woke the job, or the task does: both end in the same row.
	r := f.versionReport("app", "1.0")
	if ev := r.Repositories[0].Evaluation; ev != "pending_scan" && ev != "evaluated" {
		t.Fatalf("before the scan finished the evaluation is %q; no finding must not read as clean", ev)
	}
	f.runScan()
	r = f.waitEvaluation("app", "1.0", "evaluated")
	if len(r.Repositories[0].Findings) != 1 {
		t.Fatalf("findings: %+v", r.Repositories[0].Findings)
	}
	got := r.Repositories[0].Findings[0]
	if got.Advisory != cve || got.Severity != "critical" || got.Status != "needs_triage" || got.Version != digestOf(img.manifest) {
		t.Fatalf("finding: %+v", got)
	}
	if len(got.Components) != 1 || got.Components[0].Name != "xz" || got.Components[0].Version != "5.6.0-r0" || got.Components[0].FixedVersion != "5.6.1-r2" {
		t.Fatalf("components: %+v", got.Components)
	}
	if len(got.FixedVersions) != 1 || got.FixedVersions[0] != "5.6.1-r2" {
		t.Fatalf("fixed versions: %+v", got.FixedVersions)
	}
	if len(r.Repositories[0].Images) != 1 || r.Repositories[0].Images[0].OsFamily != "alpine" || r.Repositories[0].Images[0].Packages != 1 {
		t.Fatalf("images: %+v", r.Repositories[0].Images)
	}

	// R-VULN-038: one Critical grades D; the OS and the scan time come from the manifest's scan. R-VULN-040: its fixed version
	// makes it the tag's one fixable finding.
	if g := r.Repositories[0].Grade; g.Grade != "D" || g.Open.Critical != 1 || g.Fixable != 1 || g.Eosl || g.Os == nil || *g.Os != "alpine 3.19.1" || g.CheckedAt == nil {
		t.Fatalf("grade: %+v", g)
	}
	// The grade per tag of GET /packages/{id}/vulnerabilities, which the repository page shows (R-VULN-039).
	a := f.do("GET", "/api/v1/packages/"+view.ID("oci", "app")+"/vulnerabilities", nil)
	if a.Status != http.StatusOK || !strings.Contains(string(a.Body), `"critical":1`) || !strings.Contains(string(a.Body), `"grade":"D"`) || !strings.Contains(string(a.Body), `"fixable":1`) {
		t.Fatalf("package grades: %d %s", a.Status, a.Body)
	}
}

// R-VULN-038: an image whose OS is at its end of life grades C at best, without a finding; the list grades the image by that tag.
func TestR_VULN_038_AnImageAtTheEndOfLifeOfItsOSGradesCAtBest(t *testing.T) {
	f := newFlow(t)
	f.setReport(`{"SchemaVersion":2,"Metadata":{"OS":{"Family":"alpine","Name":"3.12.0","EOSL":true}},"Results":[]}`)
	f.push("app", "1.0", newImage(t))
	f.runScan()
	r := f.waitEvaluation("app", "1.0", "evaluated")
	if g := r.Repositories[0].Grade; g.Grade != "C" || !g.Eosl || g.Os == nil || *g.Os != "alpine 3.12.0" || g.Open.Critical != 0 {
		t.Fatalf("grade: %+v", g)
	}
	a := f.do("GET", "/api/v1/packages?format=oci&include=vulnerabilities", nil)
	var page struct {
		Items []struct {
			Name               string      `json:"name"`
			VulnerabilityGrade *imageGrade `json:"vulnerabilityGrade"`
		} `json:"items"`
	}
	if err := json.Unmarshal(a.Body, &page); err != nil || a.Status != http.StatusOK {
		t.Fatalf("list: %d %s", a.Status, a.Body)
	}
	if len(page.Items) != 1 || page.Items[0].VulnerabilityGrade == nil || page.Items[0].VulnerabilityGrade.Grade != "C" || !page.Items[0].VulnerabilityGrade.Eosl {
		t.Fatalf("the list grades the image by its latest tag: %s", a.Body)
	}
}

func TestR_VULN_035_ANewDatabaseIsReadWithoutTheLayersAndAFixedReportResolves(t *testing.T) {
	f := newFlow(t)
	f.push("app", "1.0", newImage(t))
	f.runScan()
	f.waitEvaluation("app", "1.0", "evaluated")
	if n := f.an.analyses.Load(); n != 1 {
		t.Fatalf("%d analyses of the first scan", n)
	}

	// A new database that no longer lists the advisory: the detection runs again, the layers are not read, and the finding resolves.
	f.setReport(`{"SchemaVersion":2,"Results":[]}`)
	f.setDB(dbDigest(2))
	before := f.an.detections.Load()
	f.runScan()
	testenv.WaitFor(t, "the finding resolved", 60*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM vuln_finding WHERE ecosystem = 'OCI' AND status = 'resolved'`) == 1
	})
	if f.an.detections.Load() <= before {
		t.Fatal("the new database was not used")
	}
	if n := f.an.analyses.Load(); n != 1 {
		t.Fatalf("the new database read the layers again: %d analyses", n)
	}
}

func TestR_VULN_015_ABlockedImageIsRefusedInTheRegistrysShape(t *testing.T) {
	f := newFlow(t)
	img := newImage(t)
	f.push("app", "1.0", img)
	f.runScan()
	r := f.waitEvaluation("app", "1.0", "evaluated")
	id := r.Repositories[0].Findings[0].ID

	if a := f.do("GET", v2("app")+"/manifests/1.0", nil, "Accept", manifestType); a.Status != http.StatusOK {
		t.Fatalf("a pull before the block: %d %s", a.Status, a.Body)
	}
	// R-VULN-012: a block is set on a confirmed finding.
	for _, status := range []string{"confirmed", "blocked"} {
		body, _ := json.Marshal(map[string]string{"status": status})
		a := f.st.Do(f.api, "PATCH", "/api/v1/vulnerabilities/findings/"+id, f.adm, body, "If-Match", etagOf(t, f, id), "Content-Type", "application/merge-patch+json")
		if a.Status != http.StatusOK {
			t.Fatalf("set %s: %d %s", status, a.Status, a.Body)
		}
	}
	for _, ref := range []string{"1.0", digestOf(img.manifest)} {
		a := f.do("GET", v2("app")+"/manifests/"+ref, nil, "Accept", manifestType)
		if a.Status != http.StatusForbidden || !strings.Contains(string(a.Body), `"code":"DENIED"`) || !strings.Contains(string(a.Body), cve) {
			t.Fatalf("a blocked pull of %s: %d %s", ref, a.Status, a.Body)
		}
		if got := a.Header.Get("Cache-Control"); got != "no-store" {
			t.Fatalf("Cache-Control of the refusal: %q", got)
		}
	}
	// A blob of the blocked image is not an image: a layer is shared and is not refused (H-811 records it).
	if a := f.do("GET", v2("app")+"/blobs/"+digestOf(img.layer), nil); a.Status != http.StatusOK {
		t.Fatalf("a layer of the blocked image: %d", a.Status)
	}
}

// R-VULN-002, R-VULN-033: a repository of images has the member `vulnerabilities`. Switching it off resolves the images' findings through
// the rescan, and switching it on again re-detects them with their status.
func TestR_VULN_002_TheScanningOfARepositoryOfImagesIsSwitchedByItsMember(t *testing.T) {
	f := newFlow(t)
	f.push("app", "1.0", newImage(t))
	f.runScan()
	f.waitEvaluation("app", "1.0", "evaluated")
	path := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + repoKey
	patch := func(body string) testenv.Answer {
		got := f.st.Do(f.api, "GET", path, f.adm, nil)
		if got.Status != http.StatusOK {
			t.Fatalf("read the repository: %d %s", got.Status, got.Body)
		}
		return f.st.Do(f.api, "PATCH", path, f.adm, []byte(body), "If-Match", got.Header.Get("ETag"), "Content-Type", "application/merge-patch+json")
	}
	if a := patch(`{"vulnerabilities":{"enabled":false}}`); a.Status != http.StatusOK {
		t.Fatalf("switch the scanning off: %d %s", a.Status, a.Body)
	}
	testenv.WaitFor(t, "the findings to resolve with scanning_disabled", 60*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM vuln_finding WHERE ecosystem = 'OCI' AND status = 'resolved' AND resolved_reason = 'scanning_disabled'`) == 1
	})
	if a := patch(`{"vulnerabilities":{"enabled":true}}`); a.Status != http.StatusOK {
		t.Fatalf("switch the scanning on: %d %s", a.Status, a.Body)
	}
	f.runScan()
	testenv.WaitFor(t, "the finding to be detected again", 60*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM vuln_finding WHERE ecosystem = 'OCI' AND status = 'needs_triage'`) == 1
	})
	if n := f.an.analyses.Load(); n != 1 {
		t.Fatalf("switching the scanning read the layers again: %d analyses", n)
	}
}

func TestR_VULN_034_AScanThatDoesNotFinishIsNeverReportedAsClean(t *testing.T) {
	f := newFlow(t)
	f.an.fail.Store(&imagescan.ScanError{Code: imagescan.CodeImageTooLarge, Detail: "too large", Terminal: true})
	f.push("big", "1.0", newImage(t))
	f.runScan()
	r := f.waitEvaluation("big", "1.0", "scan_failed")
	if len(r.Repositories[0].Findings) != 0 {
		t.Fatalf("findings of an image that was not scanned: %+v", r.Repositories[0].Findings)
	}
	if got := r.Repositories[0].Images[0].State; got != "failed" {
		t.Fatalf("image state %q", got)
	}
}

// dbArchive is a database archive as the publisher builds it, with a database file that is not a real one: the analyser is replaced here.
func dbArchive(t *testing.T) []byte { return dbArchiveOf(t, "database bytes") }

// dbArchiveOf is a database archive whose database file holds the text: another text is another layer, as each build of the publisher is.
func dbArchiveOf(t *testing.T, db string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ name, body string }{
		{"metadata.json", `{"Version":2,"UpdatedAt":"2026-11-08T12:51:20Z","NextUpdate":"2026-11-09T12:51:20Z"}`},
		{"trivy.db", db},
	} {
		name, body := f.name, f.body
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func etagOf(t *testing.T, f *flow, id string) string {
	t.Helper()
	a := f.st.Do(f.api, "GET", "/api/v1/vulnerabilities/findings/"+id, f.adm, nil)
	if a.Status != http.StatusOK {
		t.Fatalf("finding %s: %d %s", id, a.Status, a.Body)
	}
	return a.Header.Get("ETag")
}
