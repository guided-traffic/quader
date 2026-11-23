//go:build integration

package vulnimage

import (
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The detection in the scanner (R-VULN-048, R-VULN-049, ADR-229): the worker sends the stored analysis to the scanner replica, which
// detects with the database its loader read through the scan listener; while the loader does not hold the database in use, the run ends
// and changes no row.

// R-VULN-048, R-VULN-049, Q-VULN-201: an archive of bytes in a local repository is loaded by the loader of the kind trivy-db through the
// scan listener with the scanner's pair, and a pushed image is analysed and detected with it; a second archive in use re-detects without
// an analysis; with the loader stopped before the next archive, the run ends database_loading, enqueues the next run, and the image keeps
// its detection. About 6 s.
func TestR_VULN_048_TheScannerDetectsWithTheDatabaseItsLoaderReadAndARunWaitsWhileItLoads(t *testing.T) {
	const dbRepo = "scanner-db"
	f := newFlowWith(t, options{
		realDatabase: true,
		seed: func(st *testenv.Stack) {
			st.Repository(dbRepo, "local", "oci", `{"vulnerabilities":{"enabled":false}}`)
		},
		stackEnv: []string{"QUADER_VULNERABILITIES_IMAGES_DATABASE_REPOSITORY=" + testenv.DefaultProject + "/" + dbRepo},
	})
	first := f.pushDatabase(dbRepo, dbArchiveOf(t, "the first build"))
	f.push("app", "1.0", newImage(t))
	f.runDatabase()
	f.waitLoaded(first)
	f.runScan()
	f.waitEvaluation("app", "1.0", "evaluated")
	if got := f.st.Text(`SELECT db_digest FROM vuln_image_scan`); got != first {
		t.Fatalf("the detection is recorded under %q, want the database the loader read %s", got, first)
	}
	if d := f.an.database.Load(); d == nil || !strings.HasSuffix(*d, strings.TrimPrefix(first, "sha256:")) {
		t.Fatalf("the analyser detected with %v", d)
	}

	// A second build: the loader reads it, the image is detected again, and no layer is read again.
	second := f.pushDatabase(dbRepo, dbArchiveOf(t, "the second build"))
	f.runDatabase()
	f.waitLoaded(second)
	f.runScan()
	testenv.WaitFor(t, "the re-detection under the second database", 30*time.Second, func() bool {
		return f.st.Text(`SELECT db_digest FROM vuln_image_scan`) == second
	})
	if n := f.an.analyses.Load(); n != 1 {
		t.Fatalf("a new database read the layers again: %d analyses", n)
	}

	// The loader stops; a third build is in use that no scanner pod holds: the run ends database_loading and no row changes.
	if err := f.loader.Stop(); err != nil {
		t.Fatalf("stop the loader: %v", err)
	}
	third := f.pushDatabase(dbRepo, dbArchiveOf(t, "the third build"))
	f.runDatabase()
	before := f.st.Text(`SELECT concat_ws(' ', state, db_digest, attempts, error_code, scanned_at) FROM vuln_image_scan`)
	detections := f.an.detections.Load()
	f.runScan()
	testenv.WaitFor(t, "a run that waits for the scanners to load the database", 30*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM job WHERE kind = 'vuln-image-scan' AND state = 'succeeded' AND result->>'waiting' LIKE 'database_loading%'`) >= 1
	})
	if got := f.st.Text(`SELECT concat_ws(' ', state, db_digest, attempts, error_code, scanned_at) FROM vuln_image_scan`); got != before {
		t.Fatalf("the row changed while the scanners load %s: %s, then %s", third, before, got)
	}
	if f.an.detections.Load() != detections {
		t.Fatal("the analyser ran a detection of a database the pod does not hold")
	}
	if ev := f.versionReport("app", "1.0").Repositories[0].Evaluation; ev != "evaluated" {
		t.Fatalf("the image reads %q while the scanners load the new database, want its detection kept", ev)
	}
	// The run that waits enqueues the next one 30 s after its end; a run started by hand meanwhile moves it earlier and waits again.
	testenv.WaitFor(t, "the next run queued about 30 s later", 20*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM job WHERE kind = 'vuln-image-scan' AND state = 'queued' AND run_at > now() + interval '5 seconds'`) == 1
	})
}

// Q-VULN-200: after a new database the images that were never detected go first: with one call at a time, an image pushed after five
// stored ones were detected under the first database is detected before their re-detections under the second. About 4 s.
func TestQ_VULN_200_APushedImageIsDetectedBeforeTheRedetections(t *testing.T) {
	f := newFlowWith(t, options{workerEnv: []string{"QUADER_VULNERABILITIES_IMAGES_CONCURRENCY=1"}})
	for i := range 5 {
		f.push("stored-"+string(rune('a'+i)), "1.0", variant(t, i+1))
	}
	f.runScan()
	testenv.WaitFor(t, "the five stored images detected", 60*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM vuln_image_scan WHERE state = 'scanned'`) == 5
	})
	f.setDB(dbDigest(2))
	f.push("pushed", "1.0", variant(t, 9))
	f.runFreshScan()
	testenv.WaitFor(t, "every image detected under the second database", 60*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM vuln_image_scan WHERE state = 'scanned' AND db_digest = $1`, dbDigest(2)) == 6
	})
	if n := f.st.Number(`SELECT count(*) FROM vuln_image_scan s WHERE s.name <> 'pushed'
		AND s.scanned_at <= (SELECT p.scanned_at FROM vuln_image_scan p WHERE p.name = 'pushed')`); n != 0 {
		t.Fatalf("%d re-detections ran before the pushed image's first detection", n)
	}
}
