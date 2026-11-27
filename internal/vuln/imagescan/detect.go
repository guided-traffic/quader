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
	"mime"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/qname"
)

// The detection in the scanner (R-VULN-048, Q-VULN-197, ADR-229): the worker sends the stored analysis, gzip as stored, and names the
// database in use and the analyser version that made the analysis; the scanner detects with the copy of the database its pod holds and
// answers the analyser's report, which the worker reads in Go under limits. No token: a detection reads nothing of Quader, and the mutual
// TLS that admits the worker's name alone decides who may call (Q-VULN-185).

// DetectionsPath is the route of the detection on the scanner's listener.
const DetectionsPath = "/scan/v1/detections"

// DatabaseHeader names the database a detection ran with, on its answer: "sha256:<64 hex>".
const DatabaseHeader = "Quader-Database"

// ReportMediaType is the media type of a detection's answer: the analyser's JSON report.
const ReportMediaType = "application/json"

// The query of a detection: the database in use, the analyser version of the analysis and the manifest's digest (for the logs only).
const (
	DetectQueryDatabase = "database"
	DetectQueryAnalyser = "analyser"
	DetectQueryDigest   = "digest"
)

// The codes of a detection that are no code of an analysis (R-VULN-034, R-VULN-048).
const (
	// CodeAnalysisOutdated is the scanner's 409: the analysis was made by another analyser version than its own (Q-VULN-199).
	CodeAnalysisOutdated = "analysis_outdated"
	// CodeDatabaseLoading is the scanner's 503: its pod holds no complete copy of the database the call names yet (Q-VULN-201). No row
	// records it: the run ends and runs again after DatabaseLoadingWait.
	CodeDatabaseLoading = "database_loading"
	// CodeScannerVersionsDiffer is the second analysis_outdated of one cycle: scanner pods of two analyser versions answer, which a
	// rollout of a new pin causes. The row is put off a minute without an attempt.
	CodeScannerVersionsDiffer = "scanner_versions_differ"
)

// DatabaseLoadingWait is how long a run that met database_loading waits before the next one (Q-VULN-201).
const DatabaseLoadingWait = 30 * time.Second

// MaxStoredAnalysisBytes bounds the stored analysis, gzip, and so the body of a detection on the wire (R-VULN-043, R-VULN-048): 64 MiB.
const MaxStoredAnalysisBytes = int64(64) << 20

// MaxReportBytes bounds the report of a detection after decoding (R-VULN-048): 256 MiB, checked by the scanner before it answers and by
// the worker again before it decodes.
const MaxReportBytes = int64(256) << 20

// DetectCall is one detection: the stored analysis exactly as vuln_image_sbom holds it, the analyser version that made it and the
// database in use, and the image it describes for the logs.
type DetectCall struct {
	Repository qname.Repo
	Name       string
	// Digest is the manifest's, sha256:<64 hex>; it decides nothing.
	Digest string
	// Database is the digest of the database in use as the worker read it from the source state.
	Database string
	// Analyser is the version that made the analysis.
	Analyser string
	// Analysis is the gzip of the CycloneDX document as stored.
	Analysis []byte
}

// LogValue renders the call without its analysis.
func (c DetectCall) LogValue() slog.Value {
	return slog.GroupValue(slog.String("repository", c.Repository.String()), slog.String("name", c.Name),
		slog.String("digest", c.Digest), slog.String("database", c.Database), slog.String("analyser", c.Analyser))
}

// Detect implements [Scanner]: it sends the detection and maps every answer to the report or a [ScanError] with the code, the attempt
// and the wait of R-VULN-048's table. A busy, draining or loading answer is tried three times within three seconds, each on a new
// connection. An error that is not a ScanError is the worker's own (the job's context ended).
func (c *Client) Detect(ctx context.Context, call DetectCall) (Detection, error) {
	q := url.Values{}
	q.Set(DetectQueryDatabase, call.Database)
	q.Set(DetectQueryAnalyser, call.Analyser)
	q.Set(DetectQueryDigest, call.Digest)
	start := time.Now()
	for try := 1; ; try++ {
		det, err := c.detectOnce(ctx, call, q.Encode())
		var se *ScanError
		retry := errors.As(err, &se) && (se.Code == CodeScannerBusy || se.Code == CodeDatabaseLoading || se.Code == CodeScannerDiskFailed)
		if !retry || try >= busyTries || time.Since(start) >= busyWindow {
			return det, err
		}
		if err := c.o.Sleep(ctx, busyWindow/busyTries); err != nil {
			return Detection{}, context.Cause(ctx)
		}
	}
}

// detectOnce is one try on a new connection.
func (c *Client) detectOnce(ctx context.Context, call DetectCall, query string) (Detection, error) {
	cctx, cancel := context.WithTimeout(ctx, c.o.DetectDeadline)
	defer cancel()
	var wrote atomic.Bool
	cctx = httptrace.WithClientTrace(cctx, &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { wrote.Store(true) }})
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, *c.url.Load()+DetectionsPath+"?"+query, bytes.NewReader(call.Analysis))
	if err != nil {
		return Detection{}, err
	}
	req.Header.Set("Content-Type", AnalysisMediaType)
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Accept-Encoding", "gzip")
	res, err := c.http.Do(req)
	if err != nil {
		return Detection{}, c.broken(ctx, cctx, wrote.Load(), err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return Detection{}, detectProblemOf(res)
	}
	det, err := readReport(res, call)
	switch {
	case err != nil && ctx.Err() != nil:
		return Detection{}, context.Cause(ctx)
	case err != nil && cctx.Err() != nil:
		return Detection{}, &ScanError{Code: CodeTimeout, Detail: "the detection did not end within its deadline"}
	}
	return det, err
}

// readReport checks the answer's headers against the call before it reads a byte of the body (R-VULN-048): the report must be JSON, name
// the database and the analyser version of the call, and decode within MaxReportBytes as the analyser's report (ParseReport: one document,
// schema version 2, the bounds of an advisory, every text cleaned). An answer that differs spends an attempt as analyser_failed.
func readReport(res *http.Response, call DetectCall) (Detection, error) {
	mt, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	switch {
	case mt != ReportMediaType:
		return Detection{}, &ScanError{Code: CodeAnalyserFailed, Detail: fmt.Sprintf("the scanner answered the detection as %q, not %s", cut(mt, 64), ReportMediaType)}
	case res.Header.Get(DatabaseHeader) != call.Database:
		return Detection{}, &ScanError{Code: CodeAnalyserFailed, Detail: fmt.Sprintf("the scanner's report names the database %q, the call %s", cut(res.Header.Get(DatabaseHeader), 80), call.Database)}
	case res.Header.Get(AnalyserVersionHeader) != call.Analyser:
		return Detection{}, &ScanError{Code: CodeAnalyserFailed, Detail: fmt.Sprintf("the scanner's report names the analyser %q, the call %s", cut(res.Header.Get(AnalyserVersionHeader), 40), call.Analyser)}
	}
	var r io.Reader = res.Body
	if strings.EqualFold(res.Header.Get("Content-Encoding"), "gzip") {
		zr, err := gzip.NewReader(res.Body)
		if err != nil {
			return Detection{}, &ScanError{Code: CodeAnalyserFailed, Detail: "the scanner's report is not gzip"}
		}
		defer func() { _ = zr.Close() }()
		r = zr
	}
	counted := &byteCounter{r: r}
	det, err := ParseReport(counted, MaxReportBytes)
	switch {
	case err != nil && counted.n > MaxReportBytes:
		return Detection{}, &ScanError{Code: CodeReportTooLarge, Detail: fmt.Sprintf("the scanner's report exceeds %d bytes", MaxReportBytes)}
	case err != nil:
		return Detection{}, &ScanError{Code: CodeAnalyserFailed, Detail: err.Error()}
	}
	det.Analyser = res.Header.Get(AnalyserVersionHeader)
	return det, nil
}

// byteCounter counts what is read through it.
type byteCounter struct {
	r io.Reader
	n int64
}

func (b *byteCounter) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	b.n += int64(n)
	return n, err
}

// detectProblemOf maps an answer that is no report to the code, the attempt and the wait of R-VULN-048.
func detectProblemOf(res *http.Response) error {
	var p CallProblem
	_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&p)
	detail := cut(p.Detail, 1024)
	switch res.StatusCode {
	case http.StatusConflict:
		if p.Code == CodeAnalysisOutdated {
			return &ScanError{Code: CodeAnalysisOutdated, Detail: fmt.Sprintf("the scanner's analyser is %q: %s", cut(res.Header.Get(AnalyserVersionHeader), 40), detail)}
		}
	case http.StatusServiceUnavailable:
		if p.Code == CodeDatabaseLoading {
			return &ScanError{Code: CodeDatabaseLoading, Defer: DatabaseLoadingWait, Detail: "the scanner holds no copy of the database in use yet: " + detail}
		}
		if p.Code == CodeScannerDiskFailed {
			return &ScanError{Code: CodeScannerDiskFailed, Defer: scannerRetryAfter, Detail: "the scanner could not write to its spool: " + detail}
		}
		return &ScanError{Code: CodeScannerBusy, Defer: scannerRetryAfter, Detail: "every scanner the detection reached was busy or draining: " + detail}
	case http.StatusUnprocessableEntity:
		switch p.Code {
		case CodeSpoolTooSmall:
			return &ScanError{Code: CodeSpoolTooSmall, Defer: refusedBackoff, Detail: detail}
		case CodeAnalyserFailed:
			// The analyser refused the analysis or failed on it: the analysis is the suspect (Q-VULN-202).
			return &ScanError{Code: CodeAnalyserFailed, Detail: "the analyser refused the stored analysis: " + detail, RefusedAnalysis: true}
		case CodeTimeout, CodeReportTooLarge:
			return &ScanError{Code: p.Code, Detail: detail}
		}
	}
	// A status or a code the table does not name is the scanner's defect or a hostile answer: one attempt.
	return &ScanError{Code: CodeAnalyserFailed, Detail: fmt.Sprintf("the scanner answered the detection %d %s: %s", res.StatusCode, cut(p.Code, 64), detail)}
}
