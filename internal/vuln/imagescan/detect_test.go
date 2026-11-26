package imagescan

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/vuln/store"
)

const (
	testDB       = "sha256:" + "db0000000000000000000000000000000000000000000000000000000000beef"
	testAnalyser = "0.76.2"
)

func gzipOf(t *testing.T, b []byte) []byte {
	t.Helper()
	gz, err := gzipBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	return gz
}

func gunzip(t *testing.T, gz []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// R-VULN-043, Q-VULN-198: the worker checks the scanner's analysis in Go and keeps it as answered: a document nested deeper than 32
// levels, of another bomFormat, with data after it, without a specVersion or above the bound is refused; a valid one counts its library
// and application components at any depth.
func TestR_VULN_043_TheAnalysisIsCheckedUnderLimitsAndItsPackagesCounted(t *testing.T) {
	doc := `{"bomFormat":"CycloneDX","specVersion":"1.6","unknown":{"x":[1,2,3]},
	 "metadata":{"component":{"type":"container","name":"app","components":[{"type":"library","name":"not a package of the image's list"}]}},
	 "components":[{"type":"operating-system","name":"alpine"},{"type":"library","name":"xz","components":[{"type":"application","name":"nested"}]},
	  {"type":"library","name":"zlib"}],"dependencies":[{"ref":"a"}]}`
	n, err := CheckAnalysis([]byte(doc))
	if err != nil || n != 3 {
		t.Fatalf("%d packages, %v", n, err)
	}
	deep := strings.Repeat(`{"components":[`, 17) + strings.Repeat(`]}`, 17)
	for name, bad := range map[string]string{
		"a nesting of depth 33":       `{"bomFormat":"CycloneDX","specVersion":"1.6","metadata":{"component":` + deep + `}}`,
		"a bomFormat of another kind": `{"bomFormat":"SPDX","specVersion":"1.6"}`,
		"no specVersion":              `{"bomFormat":"CycloneDX"}`,
		"no JSON":                     `<bom/>`,
		"an array":                    `[{"bomFormat":"CycloneDX","specVersion":"1.6"}]`,
		"data after the document":     `{"bomFormat":"CycloneDX","specVersion":"1.6"} {}`,
		"a truncated document":        `{"bomFormat":"CycloneDX","specVersion":"1.6","components":[{"type":"library"`,
	} {
		_, err := CheckAnalysis([]byte(bad))
		var se *ScanError
		if !errors.As(err, &se) || se.Code != CodeAnalyserFailed {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := checkAnalysis([]byte(`{"bomFormat":"CycloneDX","specVersion":"1.6"}`), 16); !isCode(err, CodeReportTooLarge) {
		t.Errorf("above the bound: %v", err)
	}
	for v, ok := range map[string]bool{"0.76.2": true, "0.76.2+dev_1~rc-2": true, strings.Repeat("1", 32): true, "": false, strings.Repeat("1", 33): false, "0.76 2": false, "<b>": false} {
		if ValidAnalyserVersion(v) != ok {
			t.Errorf("analyser version %q: %v", v, !ok)
		}
	}
}

func isCode(err error, code string) bool {
	var se *ScanError
	return errors.As(err, &se) && se.Code == code
}

func testDetectCall(t *testing.T) DetectCall {
	t.Helper()
	return DetectCall{
		Repository: qname.Repo{Project: "web", Key: "docker-local"}, Name: "app", Digest: "sha256:" + strings.Repeat("ab", 32),
		Database: testDB, Analyser: testAnalyser, Analysis: gzipOf(t, []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6"}`)),
	}
}

// report answers a detection as the scanner does, with the headers given.
func reportAnswer(db, analyser, body string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", ReportMediaType)
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set(DatabaseHeader, db)
		w.Header().Set(AnalyserVersionHeader, analyser)
		zw := gzip.NewWriter(w)
		_, _ = zw.Write([]byte(body))
		_ = zw.Close()
	}
}

const goodReport = `{"SchemaVersion":2,"Metadata":{"OS":{"Family":"alpine","Name":"3.12.0"}},"Results":[{"Class":"os-pkgs","Type":"alpine",
 "Vulnerabilities":[{"VulnerabilityID":"CVE-2021-1","PkgName":"xz","InstalledVersion":"5.2.5-r0","FixedVersion":"5.2.5-r1","Severity":"HIGH"}]}]}`

// R-VULN-048: each answer of a detection maps to its code, attempt and wait; the body is the stored gzip byte for byte; an answer whose
// database or analyser version differs from the call's is refused before its body is read.
func TestR_VULN_048_EveryAnswerOfADetectionMapsToItsCodeAttemptAndWait(t *testing.T) {
	cases := []struct {
		name    string
		answer  func(w http.ResponseWriter, r *http.Request)
		code    string
		spent   bool
		wait    time.Duration
		refused bool
	}{
		{"409 analysis_outdated", problem(409, CodeAnalysisOutdated, false), CodeAnalysisOutdated, true, 0, false},
		{"503 database_loading", problem(503, CodeDatabaseLoading, false), CodeDatabaseLoading, false, DatabaseLoadingWait, false},
		{"503 busy", problem(503, CodeScannerBusy, false), CodeScannerBusy, false, time.Minute, false},
		{"503 draining", problem(503, CodeScannerDraining, false), CodeScannerBusy, false, time.Minute, false},
		{"422 analyser_failed drops the analysis", problem(422, CodeAnalyserFailed, false), CodeAnalyserFailed, true, 0, true},
		{"422 timeout", problem(422, CodeTimeout, false), CodeTimeout, true, 0, false},
		{"422 report_too_large", problem(422, CodeReportTooLarge, true), CodeReportTooLarge, true, 0, false},
		{"422 spool_too_small", problem(422, CodeSpoolTooSmall, false), CodeSpoolTooSmall, false, time.Hour, false},
		{"an unknown status", problem(418, "teapot", false), CodeAnalyserFailed, true, 0, false},
		{"another database", reportAnswer("sha256:"+strings.Repeat("0", 64), testAnalyser, "not a report"), CodeAnalyserFailed, true, 0, false},
		{"another analyser version", reportAnswer(testDB, "0.77.0", "not a report"), CodeAnalyserFailed, true, 0, false},
		{"a report of another schema", reportAnswer(testDB, testAnalyser, `{"SchemaVersion":3}`), CodeAnalyserFailed, true, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newScannerStub(t)
			s.answer = tc.answer
			_, err := s.client(t).Detect(t.Context(), testDetectCall(t))
			var se *ScanError
			if !errors.As(err, &se) || se.Code != tc.code || se.Defer != tc.wait || (se.Defer == 0) != tc.spent || se.RefusedAnalysis != tc.refused || se.Terminal {
				t.Fatalf("%v (%+v)", err, se)
			}
		})
	}
	t.Run("a report that is no JSON media type is refused before its body is read", func(t *testing.T) {
		s := newScannerStub(t)
		s.answer = func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set(DatabaseHeader, testDB)
			w.Header().Set(AnalyserVersionHeader, testAnalyser)
			_, _ = w.Write([]byte(goodReport))
		}
		if _, err := s.client(t).Detect(t.Context(), testDetectCall(t)); !isCode(err, CodeAnalyserFailed) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("200 is the report: the call carries the three values and the stored gzip byte for byte, and no token", func(t *testing.T) {
		s := newScannerStub(t)
		call := testDetectCall(t)
		var mu sync.Mutex
		var got *http.Request
		var body []byte
		s.answer = func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			got, body = r, nil
			body, _ = io.ReadAll(r.Body)
			mu.Unlock()
			reportAnswer(testDB, testAnalyser, goodReport)(w, r)
		}
		det, err := s.client(t).Detect(t.Context(), call)
		if err != nil || det.OSFamily != "alpine" || len(det.Advisories) != 1 || det.Advisories[0].ID != "CVE-2021-1" {
			t.Fatalf("%+v %v", det, err)
		}
		mu.Lock()
		defer mu.Unlock()
		q := got.URL.Query()
		if got.Method != http.MethodPost || got.URL.Path != DetectionsPath || len(q) != 3 || q.Get(DetectQueryDatabase) != testDB ||
			q.Get(DetectQueryAnalyser) != testAnalyser || q.Get(DetectQueryDigest) != call.Digest {
			t.Fatalf("%s %s", got.Method, got.URL)
		}
		if !bytes.Equal(body, call.Analysis) || got.Header.Get("Content-Encoding") != "gzip" || got.Header.Get("Authorization") != "" {
			t.Fatalf("the body is not the stored gzip, or the call carries a credential: %v", got.Header)
		}
	})
	t.Run("database_loading is tried three times, each on a new connection", func(t *testing.T) {
		s := newScannerStub(t)
		s.answer = problem(503, CodeDatabaseLoading, false)
		_, _ = s.client(t).Detect(t.Context(), testDetectCall(t))
		if s.calls.Load() != 3 || s.conns.Load() != 3 {
			t.Fatalf("%d calls on %d connections", s.calls.Load(), s.conns.Load())
		}
	})
	t.Run("the detection's deadline is its own", func(t *testing.T) {
		s := newScannerStub(t)
		s.answer = func(_ http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-r.Context().Done() }
		c := s.client(t)
		c.o.DetectDeadline = 50 * time.Millisecond
		if _, err := c.Detect(t.Context(), testDetectCall(t)); !isCode(err, CodeTimeout) {
			t.Fatalf("%v", err)
		}
	})
}

// fakeRows are the statements of the job in memory: one row due, its stored analysis, and what the job recorded.
type fakeRows struct {
	mu       sync.Mutex
	due      []store.ImageDue
	sbom     map[[32]byte]store.StoredAnalysis
	failed   []string
	failures []store.ImageFailure
	deferred []string
	// started are the rows the job marked as taken for a call before it (Q-VULN-276).
	started []string
	deletes int
	// nextNew is what NextNew answers once due is empty: the wait of a discovered manifest, hasNew whether one waits.
	nextNew time.Duration
	hasNew  bool
	// missing and age are what BlobsMissing answers: the blobs of a manifest its repository does not hold yet, and since when it is
	// stored (Q-VULN-282).
	missing int
	age     time.Duration
}

func (f *fakeRows) BlobsMissing(context.Context, int64, string, [32]byte) (int, time.Duration, error) {
	return f.missing, f.age, nil
}

func (f *fakeRows) NextNew(context.Context) (time.Duration, bool, error) {
	return f.nextNew, f.hasNew, nil
}

func (f *fakeRows) Discover(context.Context, time.Duration, int) (int64, error) { return 0, nil }
func (f *fakeRows) Orphans(context.Context, int) ([]string, error)              { return nil, nil }
func (f *fakeRows) DueCount(context.Context, string) (int64, error)             { return int64(len(f.due)), nil }
func (f *fakeRows) PruneSBOMs(context.Context) (int64, error)                   { return 0, nil }
func (f *fakeRows) Rearm(context.Context, int64) (int64, error)                 { return 0, nil }

func (f *fakeRows) Started(_ context.Context, due []store.ImageDue) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range due {
		f.started = append(f.started, d.Name)
	}
	return nil
}

func (f *fakeRows) Unanalysed(context.Context, store.Retry, int) ([]store.ImageDue, error) {
	return nil, nil
}

func (f *fakeRows) Due(context.Context, string, store.Retry, int) ([]store.ImageDue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.due
	f.due = nil
	return out, nil
}

func (f *fakeRows) SBOM(_ context.Context, d [32]byte) (store.StoredAnalysis, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.sbom[d]
	return a, ok, nil
}

func (f *fakeRows) PutSBOM(_ context.Context, d [32]byte, a store.StoredAnalysis) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sbom[d] = a
	return nil
}

func (f *fakeRows) DeleteSBOM(_ context.Context, d [32]byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sbom, d)
	f.deletes++
	return nil
}

func (f *fakeRows) Failed(_ context.Context, _ int64, _ string, _ [32]byte, fl store.ImageFailure) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = append(f.failed, fl.Code)
	f.failures = append(f.failures, fl)
	return nil
}

func (f *fakeRows) Deferred(_ context.Context, _ int64, _ string, _ [32]byte, code, _ string, wait time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deferred = append(f.deferred, code+" "+wait.String())
	return nil
}

// fakeScanner answers the job's calls with what the test sets.
type fakeScanner struct {
	mu       sync.Mutex
	analyses []string // the versions it answered
	detects  []string // the analyser version each detection named
	analyse  func() (Analysis, error)
	detect   func(DetectCall) (Detection, error)
}

func (f *fakeScanner) Analyse(context.Context, Call) (Analysis, error) {
	a, err := f.analyse()
	f.mu.Lock()
	f.analyses = append(f.analyses, a.AnalyserVersion)
	f.mu.Unlock()
	return a, err
}

func (f *fakeScanner) Detect(_ context.Context, c DetectCall) (Detection, error) {
	f.mu.Lock()
	f.detects = append(f.detects, c.Analyser)
	f.mu.Unlock()
	return f.detect(c)
}

type fakeTokens struct{}

func (fakeTokens) IssueScan(qname.Repo, string, string, time.Time) (registrytoken.ScanToken, string, error) {
	return registrytoken.ScanToken{}, "sid", nil
}

type fakeDB struct{ digest string }

func (f fakeDB) InUse(context.Context) (string, error) { return f.digest, nil }

type noTx struct{}

func (noTx) InTx(_ context.Context, fn func(meta.Tx) error) error { return fn(nil) }

type fakeRun struct{ result any }

func (r *fakeRun) SaveProgress(context.Context, int64, int64, string) error { return nil }
func (r *fakeRun) SetResult(v any) error                                    { r.result = v; return nil }

// rig is the job over the fakes, with one manifest due whose analysis of version stored is kept, and the applied detections counted.
type rig struct {
	svc      *Service
	rows     *fakeRows
	scanner  *fakeScanner
	enqueued []time.Duration
	applied  []Detection
	packages []int
}

func newJobRig(t *testing.T, stored string) *rig {
	t.Helper()
	d := store.ImageDue{RepoID: 1, Name: "app", Repo: qname.Repo{Project: "web", Key: "docker-local"}}
	d.Digest[0] = 0xab
	rows := &fakeRows{due: []store.ImageDue{d}, sbom: map[[32]byte]store.StoredAnalysis{}}
	if stored != "" {
		rows.sbom[d.Digest] = store.StoredAnalysis{SBOM: gzipOf(t, []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6"}`)), Analyser: stored, Packages: 7}
	}
	r := &rig{rows: rows, scanner: &fakeScanner{}}
	r.svc = &Service{log: slog.Default(), o: Options{
		DB: noTx{}, Images: rows, Scanner: r.scanner, Tokens: fakeTokens{}, Database: fakeDB{testDB}, Concurrency: 1, MaxAttempts: 5, Now: time.Now,
		Enqueue: func(_ context.Context, _ meta.Tx, delay time.Duration) error {
			r.enqueued = append(r.enqueued, delay)
			return nil
		},
	}}
	r.svc.scope = func(context.Context, ImageRef) (bool, error) { return true, nil }
	r.svc.apply = func(_ context.Context, _ ImageRef, det Detection, packages int, digest string, _ bool) error {
		if digest != testDB {
			t.Errorf("the detection was recorded under %q", digest)
		}
		r.applied, r.packages = append(r.applied, det), append(r.packages, packages)
		return nil
	}
	return r
}

func analysisOf(version, doc string) func() (Analysis, error) {
	return func() (Analysis, error) { return Analysis{Document: []byte(doc), AnalyserVersion: version}, nil }
}

// R-VULN-048, Q-VULN-201: a detection the scanners answer database_loading ends the run, which runs again after 30 s; no row changes.
func TestR_VULN_048_WhileTheScannersLoadTheDatabaseTheRunEndsAndNoRowChanges(t *testing.T) {
	r := newJobRig(t, testAnalyser)
	r.scanner.detect = func(DetectCall) (Detection, error) {
		return Detection{}, &ScanError{Code: CodeDatabaseLoading, Defer: DatabaseLoadingWait}
	}
	run := &fakeRun{}
	if err := r.svc.RunScan(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	res, _ := json.Marshal(run.result)
	if !strings.Contains(string(res), CodeDatabaseLoading) || len(r.enqueued) != 1 || r.enqueued[0] != DatabaseLoadingWait {
		t.Fatalf("result %s, enqueued %v", res, r.enqueued)
	}
	if len(r.rows.failed)+len(r.rows.deferred)+len(r.applied) != 0 || len(r.scanner.analyses) != 0 {
		t.Fatalf("a row changed: failed %v, deferred %v, applied %d, analyses %v", r.rows.failed, r.rows.deferred, len(r.applied), r.scanner.analyses)
	}
}

// R-VULN-050, Q-VULN-282: a manifest whose blobs are still on their way into the repository is not analysed: the row is put off by the
// scan delay without a call and without an attempt. A manifest stored longer ago than the wait is analysed, and the scanner's answer counts
// as before; a stored analysis needs no blob and is detected at once.
func TestR_VULN_050_AnImageWhoseBlobsAreOnTheirWayIsScannedAfterThem(t *testing.T) {
	const delay = 30 * time.Second
	for _, c := range []struct {
		name     string
		stored   string
		age      time.Duration
		deferred bool
	}{
		{"blobs on their way", "", time.Minute, true},
		{"stored longer ago than the wait", "", blobsWait + time.Second, false},
		{"an analysis is stored already", testAnalyser, time.Minute, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newJobRig(t, c.stored)
			r.svc.o.Delay = delay
			r.rows.missing, r.rows.age = 2, c.age
			r.scanner.analyse = analysisOf(testAnalyser, `{"bomFormat":"CycloneDX","specVersion":"1.6"}`)
			r.scanner.detect = func(DetectCall) (Detection, error) { return Detection{}, nil }
			if err := r.svc.RunScan(t.Context(), &fakeRun{}); err != nil {
				t.Fatal(err)
			}
			called := len(r.scanner.analyses) > 0 || len(r.applied) > 0
			if c.deferred && (called || len(r.rows.failed) != 0 || fmt.Sprint(r.rows.deferred) != "[blobs_pending 30s]") {
				t.Fatalf("called %v, failed %v, deferred %v; want only [blobs_pending 30s]", called, r.rows.failed, r.rows.deferred)
			}
			if !c.deferred && (!called || len(r.rows.deferred) != 0) {
				t.Fatalf("called %v, deferred %v; want the scanner called", called, r.rows.deferred)
			}
		})
	}
}

// R-VULN-039, Q-VULN-276: the job marks the rows of a batch as taken before it calls the scanner, so that a list says "Scanning" of them
// while the call runs; the record of the result clears the mark (the statements of the store).
func TestR_VULN_039_ABatchIsMarkedAsBeingScannedBeforeItsCalls(t *testing.T) {
	r := newJobRig(t, testAnalyser)
	var marked []string
	r.scanner.detect = func(DetectCall) (Detection, error) {
		r.rows.mu.Lock()
		marked = append(marked, r.rows.started...)
		r.rows.mu.Unlock()
		return Detection{}, nil
	}
	if err := r.svc.RunScan(t.Context(), &fakeRun{}); err != nil {
		t.Fatal(err)
	}
	if len(marked) != 1 || marked[0] != "app" {
		t.Fatalf("the rows marked when the scanner was called: %v, want [app]", marked)
	}
}

// R-VULN-034, Q-VULN-268: a run that finds nothing due while a discovered manifest waits out its scan delay hands it to a job at the
// time it becomes due; with none waiting it enqueues nothing and the task's schedule stays the only next run.
func TestR_VULN_034_ARunWithNothingDueRunsAgainWhenADiscoveredManifestBecomesDue(t *testing.T) {
	t.Run("a manifest waits out its delay", func(t *testing.T) {
		r := newJobRig(t, testAnalyser)
		r.rows.due, r.rows.nextNew, r.rows.hasNew = nil, 20*time.Second, true
		run := &fakeRun{}
		if err := r.svc.RunScan(t.Context(), run); err != nil {
			t.Fatal(err)
		}
		res, _ := json.Marshal(run.result)
		if len(r.enqueued) != 1 || r.enqueued[0] != 20*time.Second || !strings.Contains(string(res), `"next":"20s"`) {
			t.Fatalf("result %s, enqueued %v, want one job in 20s", res, r.enqueued)
		}
	})
	t.Run("nothing waits", func(t *testing.T) {
		r := newJobRig(t, testAnalyser)
		r.rows.due = nil
		run := &fakeRun{}
		if err := r.svc.RunScan(t.Context(), run); err != nil {
			t.Fatal(err)
		}
		if len(r.enqueued) != 0 || run.result == nil {
			t.Fatalf("result %v, enqueued %v, want a result and no job", run.result, r.enqueued)
		}
	})
}

// R-VULN-048, Q-VULN-199: a scanner of another version refuses the stored analysis: the image is analysed again, the new analysis
// replaces the stored one, and the detection runs once more with it; a second refusal of the cycle puts the row off a minute.
func TestR_VULN_048_AnOutdatedAnalysisIsAnalysedAgainOnceAndASecondRefusalPutsTheRowOff(t *testing.T) {
	t.Run("the new analysis is kept as answered and detected", func(t *testing.T) {
		r := newJobRig(t, "0.75.0")
		doc := `{"bomFormat":"CycloneDX","specVersion":"1.6","components":[{"type":"library","name":"xz"},{"type":"library","name":"zlib"}]}`
		r.scanner.analyse = analysisOf(testAnalyser, doc)
		r.scanner.detect = func(c DetectCall) (Detection, error) {
			if c.Analyser != testAnalyser {
				return Detection{}, &ScanError{Code: CodeAnalysisOutdated}
			}
			return Detection{OSFamily: "alpine"}, nil
		}
		if err := r.svc.RunScan(t.Context(), &fakeRun{}); err != nil {
			t.Fatal(err)
		}
		if len(r.scanner.analyses) != 1 || strings.Join(r.scanner.detects, ",") != "0.75.0,"+testAnalyser || len(r.applied) != 1 || r.packages[0] != 2 {
			t.Fatalf("analyses %v, detections %v, applied %d, packages %v", r.scanner.analyses, r.scanner.detects, len(r.applied), r.packages)
		}
		for _, a := range r.rows.sbom {
			if a.Analyser != testAnalyser || a.Packages != 2 || string(gunzip(t, a.SBOM)) != doc {
				t.Fatalf("stored %s %d %s, want the answer byte for byte", a.Analyser, a.Packages, gunzip(t, a.SBOM))
			}
		}
	})
	t.Run("a second analysis_outdated of the cycle puts the row off a minute without an attempt", func(t *testing.T) {
		r := newJobRig(t, "0.75.0")
		r.scanner.analyse = analysisOf("0.75.1", `{"bomFormat":"CycloneDX","specVersion":"1.6"}`)
		r.scanner.detect = func(DetectCall) (Detection, error) { return Detection{}, &ScanError{Code: CodeAnalysisOutdated} }
		if err := r.svc.RunScan(t.Context(), &fakeRun{}); err != nil {
			t.Fatal(err)
		}
		if len(r.scanner.detects) != 2 || len(r.rows.deferred) != 1 || r.rows.deferred[0] != CodeScannerVersionsDiffer+" 1m0s" || len(r.rows.failed) != 0 {
			t.Fatalf("detections %v, deferred %v, failed %v", r.scanner.detects, r.rows.deferred, r.rows.failed)
		}
	})
}

// Q-VULN-202: a detection the analyser refused spends an attempt and deletes the stored analysis, so that the next attempt analyses the
// image again; an analysis the worker's check refuses is not stored.
func TestR_VULN_048_ARefusedDetectionDropsTheStoredAnalysis(t *testing.T) {
	r := newJobRig(t, testAnalyser)
	r.scanner.detect = func(DetectCall) (Detection, error) {
		return Detection{}, &ScanError{Code: CodeAnalyserFailed, RefusedAnalysis: true}
	}
	if err := r.svc.RunScan(t.Context(), &fakeRun{}); err != nil {
		t.Fatal(err)
	}
	if r.rows.deletes != 1 || len(r.rows.sbom) != 0 || len(r.rows.failed) != 1 || r.rows.failed[0] != CodeAnalyserFailed {
		t.Fatalf("deletes %d, stored %d, failed %v", r.rows.deletes, len(r.rows.sbom), r.rows.failed)
	}

	r = newJobRig(t, "")
	r.scanner.analyse = analysisOf(testAnalyser, `{"bomFormat":"SPDX"}`)
	r.scanner.detect = func(DetectCall) (Detection, error) {
		t.Fatal("a refused analysis was detected")
		return Detection{}, nil
	}
	if err := r.svc.RunScan(t.Context(), &fakeRun{}); err != nil {
		t.Fatal(err)
	}
	if len(r.rows.sbom) != 0 || len(r.rows.failed) != 1 || r.rows.failed[0] != CodeAnalyserFailed {
		t.Fatalf("stored %d, failed %v", len(r.rows.sbom), r.rows.failed)
	}
}
