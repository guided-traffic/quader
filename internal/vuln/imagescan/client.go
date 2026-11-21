package imagescan

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/internal/qname"
)

// The call from the worker to the scanner (R-VULN-041, Q-VULN-161, Q-VULN-162): one request per manifest, answered with the
// analysis when it is done. The worker keeps the job; the scanner keeps nothing between calls.

// AnalysesPath is the route of the call on the scanner's listener.
const AnalysesPath = "/scan/v1/analyses"

// AnalyserVersionHeader names the analyser version an analysis ran with, on the answer of a call.
const AnalyserVersionHeader = "Quader-Analyser-Version"

// AnalysisMediaType is the media type of the answer: the CycloneDX document of the analysis.
const AnalysisMediaType = "application/vnd.cyclonedx+json"

// The codes of a call that did not end in an analysis and that the scanner or the call itself names (R-VULN-034, R-VULN-041).
const (
	CodeScannerBusy         = "scanner_busy"
	CodeScannerDraining     = "scanner_draining"
	CodeScannerUnavailable  = "scanner_unavailable"
	CodeRegistryUnavailable = "registry_unavailable"
	CodePullRefused         = "pull_refused"
	CodeScannerLost         = "scanner_lost"
	CodeMalformedRequest    = "malformed_request"
)

// The waits of a scan put off for a reason of the installation (R-VULN-034): no attempt is spent.
const (
	scannerRetryAfter = time.Minute
	pullRefusedWait   = 5 * time.Minute
	// busyTries is how often a busy answer is tried on a new connection within busyWindow before the scan is put off (Q-VULN-162).
	busyTries  = 3
	busyWindow = 3 * time.Second
)

// MaxAnalysisBytes bounds the answer of a call after decoding (R-VULN-043): 256 MiB.
const MaxAnalysisBytes = int64(256) << 20

// MaxCallBytes bounds the body of a call (R-VULN-041): 16 KiB.
const MaxCallBytes = 16 << 10

// Call is one call: the repository whose row is due, the image name, the manifest's digest and the scan token for exactly that
// manifest. The token renders nothing of itself; ScanID is what the log lines of the worker, the scanner and the scan listener
// share.
type Call struct {
	Repository qname.Repo
	Name       string
	Digest     string
	Token      registrytoken.ScanToken
	ScanID     string
}

// LogValue renders the call without its token.
func (c Call) LogValue() slog.Value {
	return slog.GroupValue(slog.String("repository", c.Repository.String()), slog.String("name", c.Name),
		slog.String("digest", c.Digest), slog.String("scan_id", c.ScanID))
}

// Analysis is the answer of a call: the analyser's document as the scanner answered it, decoded and bounded, and the analyser
// version it ran with. The worker checks it ([CheckAnalysis]) and keeps it as answered (R-VULN-043).
type Analysis struct {
	Document        []byte
	AnalyserVersion string
}

// Scanner is the two calls as the scan job sees them: the client of the role scanner, or a stand-in of a test. Analyse pulls and
// analyses an image (R-VULN-041); Detect compares a stored analysis with the database the scanner pod holds (R-VULN-048).
type Scanner interface {
	Analyse(ctx context.Context, c Call) (Analysis, error)
	Detect(ctx context.Context, c DetectCall) (Detection, error)
}

// CallRequest is the body of a call (R-VULN-041), decoded strictly by the scanner.
type CallRequest struct {
	Repository string `json:"repository"`
	Name       string `json:"name"`
	Digest     string `json:"digest"`
	Token      string `json:"token"`
}

// CallProblem is the problem a call that did not end in an analysis answers (R-ARCH-023): its code and whether the same bytes can
// never pass.
type CallProblem struct {
	Code     string `json:"code"`
	Detail   string `json:"detail"`
	Terminal bool   `json:"terminal"`
}

// ClientOptions configure [NewClient].
type ClientOptions struct {
	// URL is the origin of the scanner's listener (vulnerabilities.images.scanner.url).
	URL string
	// TLS returns the configuration of one connection: the pair the worker presents and the internal CA (httpserver.LinkTLS).
	// Nil: the link is plain HTTP (an installation without TLS, Q-HA-322).
	TLS func() *tls.Config
	// Deadline is the bound of one analysis: pull_timeout + timeout + 1m. DetectDeadline is the bound of one detection: timeout + 1m
	// (R-VULN-048); zero is Deadline.
	Deadline, DetectDeadline time.Duration
	// Sleep waits between the tries of a busy answer; nil sleeps.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Client calls the scanner. Every call opens a connection of its own and keeps none, so the Service of the scanners spreads the
// calls over its replicas (Q-VULN-170).
type Client struct {
	o    ClientOptions
	http *http.Client
	// url is the origin of the scanner's listener: o.URL, or what SetURL set.
	url atomic.Pointer[string]
	// conns counts the connections opened, for the tests of one connection per call.
	conns atomic.Int64
}

var _ Scanner = (*Client)(nil)

// NewClient returns the client.
func NewClient(o ClientOptions) (*Client, error) {
	if o.URL == "" {
		return nil, errors.New("imagescan: the scanner's address is required (vulnerabilities.images.scanner.url)")
	}
	if o.Sleep == nil {
		o.Sleep = sleepCtx
	}
	if o.DetectDeadline <= 0 {
		o.DetectDeadline = o.Deadline
	}
	c := &Client{o: o}
	// One connection per call (keep-alives off), the link's TLS built for each connection (R-ARCH-038), no redirect.
	c.http = outbound.LinkClient(outbound.LinkOptions{TLS: o.TLS, OnDial: func() { c.conns.Add(1) }})
	c.SetURL(o.URL)
	return c, nil
}

// SetURL points the client at a scanner's listener.
func (c *Client) SetURL(u string) {
	u = strings.TrimSuffix(u, "/")
	c.url.Store(&u)
}

// Connections is the number of connections the client opened.
func (c *Client) Connections() int64 { return c.conns.Load() }

// Analyse implements [Scanner]: it sends the call and maps every answer to the analysis or a [ScanError] with the code, the
// attempt and the wait of R-VULN-041's table. An error that is not a ScanError is the worker's own (the job's context ended).
func (c *Client) Analyse(ctx context.Context, call Call) (Analysis, error) {
	body, err := json.Marshal(CallRequest{Repository: call.Repository.String(), Name: call.Name, Digest: call.Digest, Token: call.Token.Reveal()})
	if err != nil {
		return Analysis{}, err
	}
	start := time.Now()
	for try := 1; ; try++ {
		a, err := c.once(ctx, body)
		var se *ScanError
		if !errors.As(err, &se) || se.Code != CodeScannerBusy || try >= busyTries || time.Since(start) >= busyWindow {
			return a, err
		}
		// The next connection may reach a free replica (Q-VULN-162).
		if err := c.o.Sleep(ctx, busyWindow/busyTries); err != nil {
			return Analysis{}, context.Cause(ctx)
		}
	}
}

// once is one try on a new connection.
func (c *Client) once(ctx context.Context, body []byte) (Analysis, error) {
	cctx, cancel := context.WithTimeout(ctx, c.o.Deadline)
	defer cancel()
	var wrote atomic.Bool
	cctx = httptrace.WithClientTrace(cctx, &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { wrote.Store(true) }})
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, *c.url.Load()+AnalysesPath, bytes.NewReader(body))
	if err != nil {
		return Analysis{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	res, err := c.http.Do(req)
	if err != nil {
		return Analysis{}, c.broken(ctx, cctx, wrote.Load(), err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusOK {
		a, err := readAnalysis(res)
		if err != nil && ctx.Err() == nil && cctx.Err() != nil {
			return Analysis{}, &ScanError{Code: CodeTimeout, Detail: "the call to the scanner did not end within its deadline"}
		}
		if err != nil && ctx.Err() != nil {
			return Analysis{}, context.Cause(ctx)
		}
		return a, err
	}
	return Analysis{}, problemOf(res)
}

// broken maps a call that ended without an answer: the job's own context, the call's deadline, a connection that broke after the
// request was written (the scanner took the call and was lost: an image may have ended its pod) and one that never opened.
func (c *Client) broken(ctx, cctx context.Context, wrote bool, err error) error {
	switch {
	case ctx.Err() != nil:
		return context.Cause(ctx)
	case cctx.Err() != nil:
		return &ScanError{Code: CodeTimeout, Detail: "the call to the scanner did not end within its deadline"}
	case refusedHandshake(err):
		// Under TLS 1.3 the scanner checks the worker's certificate after the worker's side of the handshake is done: its refusal
		// arrives as an alert when the request is written already. The call was never taken, so nothing about the image failed.
		return &ScanError{Code: CodeScannerUnavailable, Defer: scannerRetryAfter, Detail: "the scanner refused the handshake: " + err.Error()}
	case wrote:
		return &ScanError{Code: CodeScannerLost, Detail: "the connection to the scanner broke after it took the call: " + err.Error()}
	}
	return &ScanError{Code: CodeScannerUnavailable, Defer: scannerRetryAfter, Detail: "no scanner answered: " + err.Error()}
}

// readAnalysis reads the answer of an analysis within MaxAnalysisBytes after decoding (R-VULN-043).
func readAnalysis(res *http.Response) (Analysis, error) {
	version := res.Header.Get(AnalyserVersionHeader)
	if !ValidAnalyserVersion(version) {
		return Analysis{}, &ScanError{Code: CodeAnalyserFailed, Detail: "the scanner's answer names no analyser version of at most 32 letters, digits and . + _ ~ -"}
	}
	var r io.Reader = res.Body
	if strings.EqualFold(res.Header.Get("Content-Encoding"), "gzip") {
		zr, err := gzip.NewReader(res.Body)
		if err != nil {
			return Analysis{}, &ScanError{Code: CodeAnalyserFailed, Detail: "the scanner's answer is not gzip"}
		}
		defer func() { _ = zr.Close() }()
		r = zr
	}
	doc, err := io.ReadAll(io.LimitReader(r, MaxAnalysisBytes+1))
	if err != nil {
		return Analysis{}, err
	}
	if int64(len(doc)) > MaxAnalysisBytes {
		return Analysis{}, &ScanError{Code: CodeReportTooLarge, Detail: fmt.Sprintf("the scanner's answer exceeds %d bytes", MaxAnalysisBytes)}
	}
	return Analysis{Document: doc, AnalyserVersion: version}, nil
}

// problemOf maps an answer that is no analysis to the code, the attempt and the wait of R-VULN-041.
func problemOf(res *http.Response) error {
	var p CallProblem
	_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&p)
	detail := cut(p.Detail, 1024)
	switch res.StatusCode {
	case http.StatusServiceUnavailable:
		return &ScanError{Code: CodeScannerBusy, Defer: scannerRetryAfter, Detail: "every scanner the call reached was busy or draining: " + detail}
	case http.StatusBadGateway:
		return &ScanError{Code: CodeRegistryUnavailable, Defer: scannerRetryAfter, Detail: "the scanner could not reach the scan listener of packages: " + detail}
	case http.StatusForbidden:
		return &ScanError{Code: CodePullRefused, Defer: pullRefusedWait, Detail: "the scan listener refused the scan token: " + detail}
	case http.StatusUnprocessableEntity:
		switch {
		case p.Code == CodeSpoolTooSmall:
			return &ScanError{Code: CodeSpoolTooSmall, Defer: refusedBackoff, Detail: detail}
		case imageCode(p.Code):
			return &ScanError{Code: p.Code, Detail: detail, Terminal: p.Terminal}
		}
	}
	// A status or a code the table does not name is the scanner's defect or a hostile answer: one attempt, as for a broken call.
	return &ScanError{Code: CodeAnalyserFailed, Detail: fmt.Sprintf("the scanner answered %d %s: %s", res.StatusCode, cut(p.Code, 64), detail)}
}

// imageCode reports whether a code is one of the image's (R-VULN-034): the scanner names them on a 422.
func imageCode(code string) bool {
	switch code {
	case CodeImageTooLarge, CodeTooManyLayers, CodeDecompressedSize, CodeTooManyEntries, CodePathTraversal, CodeDigestMismatch,
		CodeManifestInvalid, CodeLayerFormat, CodeReportTooLarge, CodeLayerMissing, CodeTimeout, CodeAnalyserFailed:
		return true
	}
	return false
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// refusedHandshake reports whether a call ended with a TLS alert of the far end: the scanner refused the worker's certificate (or the
// worker's link refused the scanner's). crypto/tls reports a received alert as a net.OpError of the operation "remote error"; the
// scanner sends none on a call it took, so such an alert never stands for a lost analysis.
func refusedHandshake(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "remote error"
}
