package scanner

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/vuln/imagescan"
)

// The detection in the scanner (R-VULN-048, Q-VULN-197, ADR-229): the worker sends the stored analysis and names the database in use and
// the analyser version that made the analysis; the scanner detects with the copy of the database its pod holds, which its container
// mounts read-only (R-VULN-049), and answers the analyser's report. Detections and analyses share the slots (Q-VULN-204).

const (
	// detectionMargin is held of the spool for a detection: the analysis it was sent, the report and the analyser's own files
	// (R-VULN-036).
	detectionMargin = int64(64) << 20
	// DetectionDirPrefix names the working directory of a detection below scan_dir; a process that starts removes what one that stopped
	// mid-detection left.
	DetectionDirPrefix = "detection-"
)

// DetectHandler serves POST /scan/v1/detections (R-VULN-048).
func (s *Service) DetectHandler() http.Handler { return http.HandlerFunc(s.serveDetect) }

// detection is a decoded detection call.
type detection struct {
	database, analyser, digest string
}

// decodeDetection reads the query strictly: exactly database, analyser and digest, each once and each of its grammar.
func decodeDetection(raw string) (detection, error) {
	q, err := url.ParseQuery(raw)
	if err != nil {
		return detection{}, fmt.Errorf("the query does not parse: %w", err)
	}
	for k, v := range q {
		switch k {
		case imagescan.DetectQueryDatabase, imagescan.DetectQueryAnalyser, imagescan.DetectQueryDigest:
			if len(v) != 1 {
				return detection{}, fmt.Errorf("the query names %s %d times", k, len(v))
			}
		default:
			return detection{}, fmt.Errorf("the query names %q, which a detection does not take", cut(k, 32))
		}
	}
	d := detection{database: q.Get(imagescan.DetectQueryDatabase), analyser: q.Get(imagescan.DetectQueryAnalyser), digest: q.Get(imagescan.DetectQueryDigest)}
	switch {
	case !imagescan.ValidDigest(d.database):
		return detection{}, errors.New("database is no sha256:<64 hex>")
	case !imagescan.ValidAnalyserVersion(d.analyser):
		return detection{}, errors.New("analyser is no version of at most 32 letters, digits and . + _ ~ -")
	case !registrytoken.ValidDigest(d.digest):
		return detection{}, errors.New("digest is no sha256:<64 hex>")
	}
	return d, nil
}

func cut(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (s *Service) serveDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.answerDetect(w, r, &callError{status: http.StatusMethodNotAllowed, code: imagescan.CodeMalformedRequest, detail: "the call is POST"}, nil)
		return
	}
	d, err := decodeDetection(r.URL.RawQuery)
	switch {
	case err != nil:
		s.answerDetect(w, r, &callError{status: http.StatusBadRequest, code: imagescan.CodeMalformedRequest, detail: err.Error()}, nil)
		return
	case !strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip"):
		s.answerDetect(w, r, &callError{status: http.StatusBadRequest, code: imagescan.CodeMalformedRequest, detail: "the body is the stored analysis, gzip"}, &d)
		return
	case r.ContentLength > imagescan.MaxStoredAnalysisBytes:
		s.answerDetect(w, r, &callError{
			status: http.StatusBadRequest, code: imagescan.CodeMalformedRequest,
			detail: fmt.Sprintf("the body exceeds %d bytes", imagescan.MaxStoredAnalysisBytes),
		}, &d)
		return
	case s.draining.Load():
		s.answerDetect(w, r, &callError{status: http.StatusServiceUnavailable, code: imagescan.CodeScannerDraining, detail: "the scanner is draining"}, &d)
		return
	case d.analyser != s.o.Analyser.Version():
		// The analysis is of another analyser version: the worker has the image analysed again (Q-VULN-199). Answered before the body is read.
		w.Header().Set(imagescan.AnalyserVersionHeader, s.o.Analyser.Version())
		s.answerDetect(w, r, &callError{
			status: http.StatusConflict, code: imagescan.CodeAnalysisOutdated,
			detail: fmt.Sprintf("the analysis was made by %s, this scanner's analyser is %s", d.analyser, s.o.Analyser.Version()),
		}, &d)
		return
	}
	dbDir, ok := s.databaseDir(d.database)
	if !ok {
		s.answerDetect(w, r, &callError{
			status: http.StatusServiceUnavailable, code: imagescan.CodeDatabaseLoading,
			detail: "this scanner's pod holds no complete copy of the database " + d.database + " yet",
		}, &d)
		return
	}
	if !s.take() {
		s.answerDetect(w, r, &callError{status: http.StatusServiceUnavailable, code: imagescan.CodeScannerBusy, detail: "every slot of the scanner holds a call"}, &d)
		return
	}
	defer s.give()
	report, cleanup, err := s.detect(r.Context(), r.Body, dbDir)
	defer cleanup()
	if err != nil {
		if _, held := imagescan.DatabaseIn(dbDir); !held && r.Context().Err() == nil {
			// The loader removed the database while the call ran (R-VULN-048).
			err = &callError{status: http.StatusServiceUnavailable, code: imagescan.CodeDatabaseLoading, detail: "the database " + d.database + " was removed during the detection"}
		}
		s.answerDetect(w, r, s.callErrorOf(r.Context(), err), &d)
		return
	}
	s.sendReport(w, r, d, report)
}

// databaseDir is the directory of the database digest names below vulnerabilities.images.database.dir, and whether the pod holds a
// complete copy of it: the loader renames a directory into place only when it is complete (R-VULN-049).
func (s *Service) databaseDir(digest string) (string, bool) {
	if s.o.DatabaseDir == "" || !imagescan.ValidDigest(digest) {
		return "", false
	}
	dir := filepath.Join(s.o.DatabaseDir, strings.TrimPrefix(digest, "sha256:"))
	_, ok := imagescan.DatabaseIn(dir)
	return dir, ok
}

// detect reads the analysis from the body within its bounds into a working directory of its own, whose room is held in the spool before
// its first file is written, and runs the analyser's detection with the database's directory as its cache. It returns the path of the
// report and the cleanup every exit runs: the directory first, then the room.
func (s *Service) detect(rctx context.Context, body io.Reader, dbDir string) (string, func(), error) {
	ctx, cancel := context.WithCancelCause(rctx)
	stop := context.AfterFunc(s.stop, func() { cancel(errStopped) })
	var held func()
	dir := ""
	cleanup := func() {
		stop()
		cancel(nil)
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
		if held != nil {
			held()
		}
	}
	if budget := s.o.Space.Budget(); detectionMargin > budget {
		return "", cleanup, &callError{
			status: http.StatusUnprocessableEntity, code: imagescan.CodeSpoolTooSmall,
			detail: fmt.Sprintf("a detection needs %d bytes of the scanner's spool and its budget is %d; raise vulnScannerTrivy.spool.sizeLimit", detectionMargin, budget),
		}
	}
	release, err := s.o.Space.Hold(detectionMargin)
	if errors.Is(err, imagescan.ErrNoSpace) {
		return "", cleanup, &callError{status: http.StatusServiceUnavailable, code: imagescan.CodeScannerBusy, detail: "the scanner's spool has no room for a detection now"}
	}
	if err != nil {
		return "", cleanup, err
	}
	held = release
	if err := os.MkdirAll(s.o.ScanDir, 0o700); err != nil {
		return "", cleanup, err
	}
	d, err := os.MkdirTemp(s.o.ScanDir, DetectionDirPrefix)
	if err != nil {
		return "", cleanup, err
	}
	dir = d
	sbom := filepath.Join(dir, "analysis.json")
	if err := writeAnalysis(body, sbom); err != nil {
		return "", cleanup, err
	}
	report := filepath.Join(dir, "report.json")
	started := s.o.Now()
	err = s.o.Analyser.Detect(ctx, sbom, report, dir, dbDir, s.o.Run)
	s.o.Metrics.step(StepDetect, s.o.Now().Sub(started).Seconds())
	if err != nil {
		return "", cleanup, err
	}
	return report, cleanup, nil
}

// writeAnalysis decodes the body, the stored gzip, into path: at most MaxStoredAnalysisBytes on the wire and MaxAnalysisBytes decoded.
// A body over either bound, or one that is no gzip, is malformed_request.
func writeAnalysis(body io.Reader, path string) error {
	malformed := func(detail string) error {
		return &callError{status: http.StatusBadRequest, code: imagescan.CodeMalformedRequest, detail: detail}
	}
	wire := &boundedReader{r: body, left: imagescan.MaxStoredAnalysisBytes}
	zr, err := gzip.NewReader(wire)
	if err != nil {
		if wire.over {
			return malformed(fmt.Sprintf("the body exceeds %d bytes", imagescan.MaxStoredAnalysisBytes))
		}
		return malformed("the body is not gzip: " + err.Error())
	}
	defer func() { _ = zr.Close() }()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // a path below the detection's own working directory
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	n, err := io.Copy(f, io.LimitReader(zr, imagescan.MaxAnalysisBytes+1))
	switch {
	case wire.over:
		return malformed(fmt.Sprintf("the body exceeds %d bytes", imagescan.MaxStoredAnalysisBytes))
	case err != nil:
		return malformed("the body is not gzip: " + err.Error())
	case n > imagescan.MaxAnalysisBytes:
		return malformed(fmt.Sprintf("the analysis exceeds %d bytes decoded", imagescan.MaxAnalysisBytes))
	}
	return f.Close()
}

// boundedReader ends with an error once more than left bytes would be read, and says so.
type boundedReader struct {
	r    io.Reader
	left int64
	over bool
}

func (b *boundedReader) Read(p []byte) (int, error) {
	if b.left < 0 {
		b.over = true
		return 0, errors.New("the body exceeds its bound")
	}
	if int64(len(p)) > b.left+1 {
		p = p[:b.left+1]
	}
	n, err := b.r.Read(p)
	b.left -= int64(n)
	if b.left < 0 {
		b.over = true
		return n, errors.New("the body exceeds its bound")
	}
	return n, err
}

// answerDetect writes the problem of a detection that did not end in a report, and counts and logs it with the database; never the
// analysis or a report (R-VULN-048).
func (s *Service) answerDetect(w http.ResponseWriter, r *http.Request, ce *callError, d *detection) {
	s.o.Metrics.detection(ce.code)
	attrs := []any{"code", ce.code}
	if d != nil {
		attrs = append(attrs, "digest", d.digest, "database", d.database)
	}
	if r.Context().Err() != nil {
		s.o.Log.Info("the worker ended a detection", attrs...)
		return
	}
	s.o.Log.Info("a detection ended without a report", attrs...)
	s.writeProblem(w, ce)
}

// sendReport answers the report (R-VULN-048): the analyser's JSON, gzip on the wire, with the analyser version and the database it was
// detected with. A report above MaxReportBytes is report_too_large.
func (s *Service) sendReport(w http.ResponseWriter, r *http.Request, d detection, report string) {
	st, err := os.Stat(report)
	if err != nil {
		s.answerDetect(w, r, s.callErrorOf(r.Context(), err), &d)
		return
	}
	if st.Size() > imagescan.MaxReportBytes {
		s.answerDetect(w, r, &callError{
			status: http.StatusUnprocessableEntity, code: imagescan.CodeReportTooLarge,
			detail: fmt.Sprintf("the report is %d bytes, the limit of the answer is %d", st.Size(), imagescan.MaxReportBytes),
		}, &d)
		return
	}
	f, err := os.Open(report) //nolint:gosec // a path below the detection's own working directory
	if err != nil {
		s.answerDetect(w, r, s.callErrorOf(r.Context(), err), &d)
		return
	}
	defer func() { _ = f.Close() }()
	h := w.Header()
	h.Set("Content-Type", imagescan.ReportMediaType)
	h.Set("Content-Encoding", "gzip")
	h.Set(imagescan.AnalyserVersionHeader, s.o.Analyser.Version())
	h.Set(imagescan.DatabaseHeader, d.database)
	w.WriteHeader(http.StatusOK)
	zw := gzip.NewWriter(w)
	_, err = io.Copy(zw, f)
	if err == nil {
		err = zw.Close()
	}
	s.o.Metrics.detection(ResultDetected)
	s.o.Log.Info("an image was detected", "digest", d.digest, "database", d.database, "bytes", st.Size(), "error", err)
}

// Database is the newest database the pod's directory holds, the one the scanner reports of itself (R-OPS-048, Q-OPS-225): the 64
// hexadecimal digits of its layer's digest and the build time of its metadata.json; "" before the loader completed the first. The newest
// is the one the loader wrote last (imagescan.LoadedAt, Q-VULN-213), which is the database in use once the pod has loaded it.
func (s *Service) Database() (string, time.Time) {
	if s.o.DatabaseDir == "" {
		return "", time.Time{}
	}
	entries, err := os.ReadDir(s.o.DatabaseDir)
	if err != nil {
		return "", time.Time{}
	}
	var newest string
	var built, loaded time.Time
	for _, e := range entries {
		if !e.IsDir() || !imagescan.ValidDigest("sha256:"+e.Name()) {
			continue
		}
		dir := filepath.Join(s.o.DatabaseDir, e.Name())
		meta, ok := imagescan.DatabaseIn(dir)
		at, _ := imagescan.LoadedAt(dir)
		if ok && (newest == "" || at.After(loaded) || (at.Equal(loaded) && e.Name() < newest)) {
			newest, built, loaded = e.Name(), meta.UpdatedAt.UTC(), at
		}
	}
	return newest, built
}
