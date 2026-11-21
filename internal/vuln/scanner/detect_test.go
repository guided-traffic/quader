package scanner

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/vuln/imagescan"
)

const (
	testDBHex = "db0000000000000000000000000000000000000000000000000000000000beef"
	testDB    = "sha256:" + testDBHex
	testImage = "sha256:abababababababababababababababababababababababababababababababab"
)

// load writes a complete database below the rig's database directory, as the loader renames it into place.
func (r *rig) load(hex, built string) {
	r.t.Helper()
	dir := filepath.Join(r.db, hex, imagescan.DatabaseDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"Version":2,"UpdatedAt":"`+built+`"}`), 0o600); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "trivy.db"), []byte("database"), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func detectQuery(database, analyser, digest string) string {
	q := url.Values{}
	q.Set(imagescan.DetectQueryDatabase, database)
	q.Set(imagescan.DetectQueryAnalyser, analyser)
	q.Set(imagescan.DetectQueryDigest, digest)
	return q.Encode()
}

func gz(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	_, _ = zw.Write([]byte(s))
	_ = zw.Close()
	return b.Bytes()
}

// readCounter records whether the scanner read the body.
type readCounter struct {
	r    io.Reader
	read atomic.Int64
}

func (c *readCounter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read.Add(int64(n))
	return n, err
}

// detectAnswer is the status and the headers of a detection's answer; detect has read and closed its body.
type detectAnswer struct {
	StatusCode int
	Header     http.Header
}

// detect sends a detection and returns the status, the problem and the answer.
func (r *rig) detect(ctx context.Context, query string, body io.Reader, size int64, hdr ...string) (*detectAnswer, imagescan.CallProblem, []byte) {
	r.t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.srv.URL+imagescan.DetectionsPath+"?"+query, body)
	if err != nil {
		r.t.Fatal(err)
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", imagescan.AnalysisMediaType)
	req.Header.Set("Content-Encoding", "gzip")
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}}).Do(req)
	if err != nil {
		return nil, imagescan.CallProblem{}, nil
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var p imagescan.CallProblem
	_ = json.Unmarshal(raw, &p)
	return &detectAnswer{StatusCode: res.StatusCode, Header: res.Header}, p, raw
}

const testAnalysis = `{"bomFormat":"CycloneDX","specVersion":"1.6","components":[{"type":"library","name":"xz"}]}`

// R-VULN-048: the scanner detects a stored analysis with the database its pod holds and answers the report with the analyser version and
// the database; the working directory goes and the spool's room comes back.
func TestR_VULN_048_TheScannerAnswersTheReportOfADetection(t *testing.T) {
	r := newRig(t, 1)
	r.load(testDBHex, "2026-11-21T07:02:51Z")
	body := gz(t, testAnalysis)
	res, p, raw := r.detect(t.Context(), detectQuery(testDB, "0.76.2", testImage), bytes.NewReader(body), int64(len(body)))
	if res == nil || res.StatusCode != http.StatusOK {
		t.Fatalf("%v %+v", res, p)
	}
	if res.Header.Get("Content-Type") != imagescan.ReportMediaType || res.Header.Get("Content-Encoding") != "gzip" ||
		res.Header.Get(imagescan.AnalyserVersionHeader) != "0.76.2" || res.Header.Get(imagescan.DatabaseHeader) != testDB {
		t.Fatalf("%v", res.Header)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if rep, _ := io.ReadAll(zr); !strings.Contains(string(rep), `"SchemaVersion":2`) {
		t.Fatalf("%s", rep)
	}
	r.an.mu.Lock()
	got := r.an.detected
	r.an.mu.Unlock()
	if got.analysis != testAnalysis || got.database != filepath.Join(r.db, testDBHex) || !strings.HasPrefix(filepath.Base(got.scratch), DetectionDirPrefix) {
		t.Fatalf("the analyser was given %+v", got)
	}
	if r.sp.Held() != 0 || r.svc.Busy() != 0 {
		t.Fatalf("held %d, busy %d after the call", r.sp.Held(), r.svc.Busy())
	}
	if ents, _ := os.ReadDir(r.dir); len(ents) != 0 {
		t.Fatalf("%d working directories left", len(ents))
	}
	// The report of the scanner names the newest database its directory holds (Q-OPS-225): the one the loader wrote last, also when it
	// was built before the one it follows, as after a repository's tag moved back (Q-VULN-213).
	hour := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(r.db, testDBHex, imagescan.DatabaseDir, "metadata.json"), hour, hour); err != nil {
		t.Fatal(err)
	}
	if hex, built := r.svc.Database(); hex != testDBHex || built.Format(time.RFC3339) != "2026-11-21T07:02:51Z" {
		t.Fatalf("the only database %s %s", hex, built)
	}
	older := strings.Repeat("e", 64)
	r.load(older, "2026-11-20T07:02:51Z")
	if hex, built := r.svc.Database(); hex != older || built.Format(time.RFC3339) != "2026-11-20T07:02:51Z" {
		t.Fatalf("the database loaded last %s %s, want %s of 2026-11-20", hex, built, older)
	}
}

// R-VULN-048: what the scanner refuses, and when: a malformed call is 400; an analysis of another analyser version is 409 before the
// body is read; a database the pod does not hold is 503 database_loading; a detection while the slot holds an analysis is busy.
func TestR_VULN_048_TheScannerRefusesADetectionBeforeItReadsTheBody(t *testing.T) {
	r := newRig(t, 1)
	r.load(testDBHex, "2026-11-21T07:02:51Z")
	good := gz(t, testAnalysis)
	for name, tc := range map[string]struct {
		query string
		body  []byte
		size  int64
		hdr   []string
	}{
		"an unknown member":         {detectQuery(testDB, "0.76.2", testImage) + "&registry=evil", good, int64(len(good)), nil},
		"a database of no digest":   {detectQuery("sha256:latest", "0.76.2", testImage), good, int64(len(good)), nil},
		"an analyser of no version": {detectQuery(testDB, "0.76 2", testImage), good, int64(len(good)), nil},
		"a member twice":            {detectQuery(testDB, "0.76.2", testImage) + "&database=" + url.QueryEscape(testDB), good, int64(len(good)), nil},
		"a body without gzip":       {detectQuery(testDB, "0.76.2", testImage), []byte(testAnalysis), int64(len(testAnalysis)), []string{"Content-Encoding", "identity"}},
		"a body that is no gzip":    {detectQuery(testDB, "0.76.2", testImage), []byte(testAnalysis), int64(len(testAnalysis)), nil},
	} {
		res, p, _ := r.detect(t.Context(), tc.query, bytes.NewReader(tc.body), tc.size, tc.hdr...)
		if res == nil || res.StatusCode != http.StatusBadRequest || p.Code != imagescan.CodeMalformedRequest {
			t.Errorf("%s: %v %+v", name, res, p)
		}
	}
	t.Run("a body above 64 MiB", func(t *testing.T) {
		big := &readCounter{r: io.LimitReader(zeros{}, imagescan.MaxStoredAnalysisBytes+1)}
		res, p, _ := r.detect(t.Context(), detectQuery(testDB, "0.76.2", testImage), big, imagescan.MaxStoredAnalysisBytes+1)
		if res == nil || res.StatusCode != http.StatusBadRequest || p.Code != imagescan.CodeMalformedRequest {
			t.Fatalf("%v %+v", res, p)
		}
	})
	t.Run("an analysis of another analyser version is 409 before the body is read", func(t *testing.T) {
		body := &readCounter{r: bytes.NewReader(good)}
		res, p, _ := r.detect(t.Context(), detectQuery(testDB, "0.75.0", testImage), body, -1)
		if res == nil || res.StatusCode != http.StatusConflict || p.Code != imagescan.CodeAnalysisOutdated || res.Header.Get(imagescan.AnalyserVersionHeader) != "0.76.2" {
			t.Fatalf("%v %+v", res, p)
		}
		if r.an.runs.Load() != 0 {
			t.Fatal("the analyser ran")
		}
	})
	t.Run("a database the pod does not hold is 503 database_loading", func(t *testing.T) {
		res, p, _ := r.detect(t.Context(), detectQuery("sha256:"+strings.Repeat("c", 64), "0.76.2", testImage), bytes.NewReader(good), int64(len(good)))
		if res == nil || res.StatusCode != http.StatusServiceUnavailable || p.Code != imagescan.CodeDatabaseLoading {
			t.Fatalf("%v %+v", res, p)
		}
	})
	t.Run("Q-VULN-204 with the slot taken by an analysis a detection is 503 scanner_busy", func(t *testing.T) {
		r.an.block, r.an.started = make(chan struct{}), make(chan struct{}, 1)
		d := r.reg.add(imageOf(t, tarOf(t, map[string]string{"a": "1"})))
		first := make(chan int, 1)
		go func() {
			code, _, _ := r.post(context.Background(), callBody("web/docker-local", "app", d, testToken))
			first <- code
		}()
		<-r.an.started
		res, p, _ := r.detect(t.Context(), detectQuery(testDB, "0.76.2", testImage), bytes.NewReader(good), int64(len(good)))
		if res == nil || res.StatusCode != http.StatusServiceUnavailable || p.Code != imagescan.CodeScannerBusy {
			t.Fatalf("%v %+v", res, p)
		}
		close(r.an.block)
		if code := <-first; code != http.StatusOK {
			t.Fatalf("the analysis: %d", code)
		}
		r.an.block, r.an.started = nil, nil
	})
}

// zeros is an endless reader of zero bytes.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// R-VULN-048: the analyser of a detection is started with the database's directory as its cache and the working directory as its HOME
// and TMPDIR, the quiet flags and an empty environment; a call whose client goes away frees its slot.
func TestR_VULN_048_TheDetectionRunsTheAnalyserOnTheNamedDatabase(t *testing.T) {
	dir := t.TempDir()
	argsFile, envFile := filepath.Join(dir, "args"), filepath.Join(dir, "env")
	script := filepath.Join(dir, "analyser.sh")
	body := "#!/bin/sh\necho \"$@\" > " + argsFile + "\n/usr/bin/env > " + envFile + "\n" +
		"while [ $# -gt 0 ]; do if [ \"$1\" = \"--output\" ]; then out=\"$2\"; fi; shift; done\n" +
		"echo '{\"SchemaVersion\":2,\"Results\":[]}' > \"$out\"\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, 1)
	r.svc.o.Analyser = &imagescan.Trivy{Binary: script, AnalyserVersion: "0.76.2"}
	r.load(testDBHex, "2026-11-21T07:02:51Z")
	good := gz(t, testAnalysis)
	if res, p, _ := r.detect(t.Context(), detectQuery(testDB, "0.76.2", testImage), bytes.NewReader(good), int64(len(good))); res == nil || res.StatusCode != http.StatusOK {
		t.Fatalf("%v %+v", res, p)
	}
	args, _ := os.ReadFile(argsFile) //nolint:gosec // the test's own directory
	env, _ := os.ReadFile(envFile)   //nolint:gosec // the test's own directory
	for _, want := range []string{"sbom ", "--format json", "--scanners vuln", "--cache-dir " + filepath.Join(r.db, testDBHex), "--offline-scan", "--skip-db-update", "--disable-telemetry"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("the analyser's arguments %q lack %q", args, want)
		}
	}
	var home, tmp string
	for _, line := range strings.Split(strings.TrimSpace(string(env)), "\n") {
		k, v, _ := strings.Cut(line, "=")
		switch k {
		case "HOME":
			home = v
		case "TMPDIR":
			tmp = v
		}
	}
	if home == "" || home != tmp || !strings.HasPrefix(filepath.Base(home), DetectionDirPrefix) || filepath.Dir(home) != r.dir {
		t.Fatalf("HOME %q and TMPDIR %q are not the detection's working directory below %s", home, tmp, r.dir)
	}

	t.Run("a call whose client goes away frees its slot", func(t *testing.T) {
		r := newRig(t, 1)
		r.load(testDBHex, "2026-11-21T07:02:51Z")
		r.an.block, r.an.started = make(chan struct{}), make(chan struct{}, 1)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _, _ = r.detect(ctx, detectQuery(testDB, "0.76.2", testImage), bytes.NewReader(good), int64(len(good)))
		}()
		<-r.an.started
		cancel()
		<-done
		deadline := time.Now().Add(5 * time.Second)
		for r.svc.Busy() != 0 || r.sp.Held() != 0 {
			if time.Now().After(deadline) {
				t.Fatalf("busy %d, held %d after the client went away", r.svc.Busy(), r.sp.Held())
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
}
