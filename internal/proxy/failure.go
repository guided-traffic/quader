package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/internal/proxy/upstream"
	"github.com/guided-traffic/quader/internal/remotecuration"
)

// The failure of an exchange with an upstream in the words of R-CUR-011 (Q-CUR-20): where the attempt failed (the step), the address
// asked without its query, the upstream's status, the first bytes of its answer and its Retry-After. The engine attaches them to the
// error of a failed exchange as the error's cause ([Exchange]); [Describe] turns any error of a fill into a [remotecuration.Failure]
// with its class and code. A client never sees them: the cause of an *errs.Error stays in the process (R-ARCH-023).

// excerptBytes is the length of an answer's excerpt (R-CUR-011).
const excerptBytes = 512

// Exchange is what a failed exchange with the upstream told: the cause of the engine's error.
type Exchange struct {
	Step remotecuration.FailureStep
	// Address is scheme, host, port and path of the request; the query is dropped (a signed address of a CDN carries a credential).
	Address string
	// Status is the upstream's status, 0 when no answer came.
	Status int
	// Excerpt is the first 512 bytes of a non-success answer's body, cleaned (R-CUR-011).
	Excerpt string
	// RetryAfter is the upstream's Retry-After, 0 when it named none.
	RetryAfter time.Duration
	cause      error
}

func (x *Exchange) Error() string {
	s := "proxy: the exchange with " + x.Address + " failed at " + string(x.Step)
	if x.Status != 0 {
		s += " with status " + strconv.Itoa(x.Status)
	}
	if x.cause != nil {
		s += ": " + x.cause.Error()
	}
	return s
}

// Unwrap returns the error the engine classified.
func (x *Exchange) Unwrap() error { return x.cause }

// addressOf is u without its query, fragment and user information.
func addressOf(u *url.URL) string {
	if u == nil {
		return ""
	}
	c := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawPath: u.RawPath}
	return c.String()
}

// binaryMedia reports whether a declared media type is no text: an excerpt of it is not taken.
func binaryMedia(contentType string) bool {
	if contentType == "" {
		return false
	}
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch {
	case strings.HasPrefix(mt, "text/"), strings.HasSuffix(mt, "+json"), strings.HasSuffix(mt, "+xml"):
		return false
	case mt == "application/json", mt == "application/xml", mt == "application/problem+json", mt == "application/javascript":
		return false
	}
	return true
}

// cleanExcerpt cuts b at 512 bytes and replaces every byte outside printable ASCII and line ends by '?' (R-CUR-011).
func cleanExcerpt(b []byte) string {
	if len(b) > excerptBytes {
		b = b[:excerptBytes]
	}
	out := make([]byte, len(b))
	for i, c := range b {
		switch {
		case c == '\n' || c == '\r' || c == '\t' || (c >= 0x20 && c < 0x7f):
			out[i] = c
		default:
			out[i] = '?'
		}
	}
	return string(out)
}

// excerptOf reads the excerpt of a non-success answer: sniffed are bytes of the body read before (the background throttle's sniff).
// An answer the upstream declared as binary has none.
func excerptOf(resp *http.Response, sniffed []byte) string {
	if resp == nil || binaryMedia(resp.Header.Get("Content-Type")) {
		return ""
	}
	b := sniffed
	if len(b) < excerptBytes && resp.Body != nil {
		more, _ := io.ReadAll(io.LimitReader(resp.Body, int64(excerptBytes-len(b))))
		b = append(append([]byte(nil), b...), more...)
	}
	return cleanExcerpt(b)
}

// stepOf is the step of a transport error (R-CUR-011): a name that does not resolve, a connection that is not made, a TLS handshake
// or certificate, a body that stopped arriving (idle), else the request.
func stepOf(err error, idle bool) remotecuration.FailureStep {
	var (
		dns    *net.DNSError
		op     *net.OpError
		unk    x509.UnknownAuthorityError
		host   x509.HostnameError
		inval  x509.CertificateInvalidError
		verify *tls.CertificateVerificationError
		rec    tls.RecordHeaderError
		alert  tls.AlertError
	)
	switch {
	case idle:
		return remotecuration.StepBody
	case errors.As(err, &dns):
		return remotecuration.StepResolve
	case errors.As(err, &verify), errors.As(err, &unk), errors.As(err, &host), errors.As(err, &inval), errors.As(err, &rec),
		errors.As(err, &alert):
		return remotecuration.StepTLS
	case errors.Is(err, outbound.ErrEgressDenied), errors.Is(err, outbound.ErrSourceNetworkUnavailable), errors.Is(err, syscall.ECONNREFUSED):
		return remotecuration.StepConnect
	case errors.As(err, &op) && op.Op == "dial":
		return remotecuration.StepConnect
	}
	return remotecuration.StepRequest
}

// withExchange returns a copy of the engine's error whose cause carries what the exchange told.
func withExchange(e *errs.Error, x Exchange) *errs.Error {
	if e == nil {
		return nil
	}
	c := *e
	x.cause = e.Err
	c.Err = &x
	return &c
}

// Describe is the failure of R-CUR-011 for an error of a background fill or of a client's fetch (Q-CUR-20): its class, its step, its
// code and Quader's English sentence for a script and an administrator (a screen writes its own from the code, Q-CUR-169), where an
// exchange failed the address without its query, the status, the excerpt and the Retry-After, and where Quader refused an address the
// upstream named, that address as the target. The caller sets the file, the attempt and the time.
func Describe(err error) remotecuration.Failure {
	f := remotecuration.Failure{Class: remotecuration.ClassInternal, Step: remotecuration.StepPlan, Code: errs.CodeInternal}
	if err == nil {
		return f
	}
	var x *Exchange
	if errors.As(err, &x) {
		f.Step, f.Address, f.Status, f.Excerpt = x.Step, x.Address, x.Status, x.Excerpt
		f.RetryAfter = int(x.RetryAfter / time.Second)
	}
	var target *upstream.RefusedTarget
	if errors.As(err, &target) {
		f.Target = target.Address
	}
	var wait *BackgroundWait
	if errors.As(err, &wait) {
		f.Class, f.Step, f.Code, f.RetryAfter = remotecuration.ClassCircuitOpen, remotecuration.StepRequest, "circuit_open", int(wait.After/time.Second)
		if wait.Reason == WaitSpoolBusy {
			f.Class, f.Step, f.Code = remotecuration.ClassStorage, remotecuration.StepStore, "spool_busy"
		}
		f.Detail = describeDetail(f, "")
		return f
	}
	if rl, ok := discover.AsRateLimited(err); ok {
		f.Class, f.Code = remotecuration.ClassUpstreamRateLimited, errs.CodeUpstreamRateLimited
		if f.Step == remotecuration.StepPlan {
			f.Step = remotecuration.StepStatus
		}
		if d, ok := rl.RetryAfter(); ok && d > 0 {
			f.RetryAfter = int(d / time.Second)
		}
		var answer *upstream.RateLimitError
		if errors.As(err, &answer) {
			limitAnswer(&f, answer.RateLimit)
		}
		f.Detail = describeDetail(f, "")
		return f
	}
	if errors.Is(err, ErrPreempted) {
		f.Class, f.Step, f.Code = remotecuration.ClassUpstreamRateLimited, remotecuration.StepRequest, "preempted"
		f.Detail = "A client's fetch took the repository's background slot; the next attempt goes on."
		return f
	}
	ee, ok := errs.As(err)
	if !ok {
		f.Detail = "The download failed inside Quader: " + truncate(err.Error(), 300)
		return f
	}
	f.Code = ee.Code
	f.Class, f.Step = classOf(ee, f.Step)
	if ee.RetryAfter > 0 && f.RetryAfter == 0 && f.Class == remotecuration.ClassUpstreamRateLimited {
		f.RetryAfter = int(ee.RetryAfter / time.Second)
	}
	f.Detail = describeDetail(f, ee.Detail)
	return f
}

// limitAnswer adds what a rate-limit answer told to its failure (R-CUR-011, Q-CUR-174): its status, the address asked without its
// query, the excerpt of its body (none for a body the upstream declared binary) and the headers of the limit, where the exchange did
// not name them already.
func limitAnswer(f *remotecuration.Failure, rl upstream.RateLimit) {
	if f.Status == 0 {
		f.Status = rl.Status
	}
	if f.Address == "" {
		f.Address = rl.Address
	}
	if f.Excerpt == "" && len(rl.Body) > 0 && !binaryMedia(rl.ContentType) {
		f.Excerpt = cleanExcerpt(rl.Body)
	}
	f.Headers = rl.Headers
}

// classOf maps the engine's code to the class of R-CUR-011 and, where no exchange named it, the step.
func classOf(ee *errs.Error, step remotecuration.FailureStep) (remotecuration.FailureClass, remotecuration.FailureStep) {
	at := func(s remotecuration.FailureStep) remotecuration.FailureStep {
		if step == remotecuration.StepPlan {
			return s
		}
		return step
	}
	switch ee.Code {
	case errs.CodeNotFound:
		return remotecuration.ClassUpstreamNotFound, at(remotecuration.StepStatus)
	case errs.CodeUpstreamAuthFailed:
		return remotecuration.ClassUpstreamAuth, at(remotecuration.StepStatus)
	case errs.CodeUpstreamRateLimited:
		return remotecuration.ClassUpstreamRateLimited, at(remotecuration.StepStatus)
	case errs.CodeUpstreamError:
		return remotecuration.ClassUpstreamError, at(remotecuration.StepStatus)
	case errs.CodeUpstreamUnexpectedStatus, errs.CodeTooManyRedirects, errs.CodeInsecureRedirect, errs.CodeForeignHostDenied,
		errs.CodeUpstreamPackageMismatch, errs.CodeNPMInvalidUpstreamDocument, errs.CodeNuGetInvalidUpstreamDocument:
		return remotecuration.ClassUpstreamUnexpected, at(remotecuration.StepStatus)
	case errs.CodeUpstreamUnreachable, errs.CodeUpstreamTimeout, errs.CodeSourceNetworkUnavailable, errs.CodeUpstreamSecretUnavailable:
		return remotecuration.ClassNetwork, at(remotecuration.StepConnect)
	case errs.CodeTLSUntrusted:
		return remotecuration.ClassTLS, at(remotecuration.StepTLS)
	case errs.CodeEgressDenied:
		return remotecuration.ClassEgressDenied, at(remotecuration.StepConnect)
	case errs.CodeUpstreamIntegrityMismatch, errs.CodeUpstreamDigestMissing, errs.CodeOCIDigestInvalid, errs.CodeOCISizeInvalid:
		return remotecuration.ClassIntegrity, at(remotecuration.StepVerify)
	case errs.CodeUpstreamContentTypeBlocked, errs.CodeContentRefused:
		return remotecuration.ClassContentRefused, at(remotecuration.StepVerify)
	case errs.CodeVulnerabilityBlocked, errs.CodeVulnerabilityDataMissing, errs.CodePopulateNotPermitted:
		return remotecuration.ClassGateRefused, remotecuration.StepGate
	case errs.CodeUpstreamItemTooLarge, errs.CodeTooLarge, errs.CodeNPMTooLarge:
		return remotecuration.ClassBound, at(remotecuration.StepBody)
	case errs.CodeObjectStoreUnavail, errs.CodeDatabaseTimeout:
		return remotecuration.ClassStorage, remotecuration.StepStore
	case errs.CodeNotCachedOffline, errs.CodeUpstreamAssumedOffline:
		return remotecuration.ClassCircuitOpen, at(remotecuration.StepRequest)
	}
	switch ee.Kind {
	case errs.UpstreamFailed:
		return remotecuration.ClassUpstreamError, at(remotecuration.StepRequest)
	case errs.UpstreamTimeout:
		return remotecuration.ClassNetwork, at(remotecuration.StepRequest)
	case errs.InsufficientStorage:
		return remotecuration.ClassStorage, remotecuration.StepStore
	}
	return remotecuration.ClassInternal, step
}

// describeDetail is Quader's sentence for a person (R-CUR-011): what failed, where, and what helps.
func describeDetail(f remotecuration.Failure, engine string) string {
	where := f.Address
	if u, err := url.Parse(f.Address); err == nil && u.Host != "" {
		where = u.Host
	}
	var s string
	switch f.Class {
	case remotecuration.ClassTLS:
		s = "The TLS certificate of " + orUpstream(where) + " is not trusted by this installation; add its CA to proxy.outbound.tls.ca_files."
	case remotecuration.ClassUpstreamAuth:
		s = orUpstream(where) + " refused the repository's credentials (status " + strconv.Itoa(f.Status) + "); check the remote's credentials."
	case remotecuration.ClassUpstreamRateLimited:
		s = orUpstream(where) + " is rate limiting Quader"
		if f.Status != 0 {
			s += " (status " + strconv.Itoa(f.Status) + ")"
		}
		if f.RetryAfter > 0 {
			s += " and asked to wait " + strconv.Itoa(f.RetryAfter) + " s"
		}
		s += "; Quader asks again after the wait, and the wait is no failed attempt of the file."
	case remotecuration.ClassUpstreamError:
		s = orUpstream(where) + " answered with an error"
		if f.Status != 0 {
			s += " (status " + strconv.Itoa(f.Status) + ")"
		}
		s += "; a later attempt may succeed."
	case remotecuration.ClassNetwork:
		s = orUpstream(where) + " could not be reached at the step " + string(f.Step) + "; check the address, the network and the proxy settings."
	case remotecuration.ClassEgressDenied:
		s = "The address of " + orUpstream(where) + " is refused by the egress address policy (proxy.outbound.address_policy)."
	case remotecuration.ClassCircuitOpen:
		s = "The upstream is marked unavailable for now (its circuit is open or the repository is offline); the next attempt waits for it."
	case remotecuration.ClassUpstreamNotFound:
		s = orUpstream(where) + " does not have the file."
	case remotecuration.ClassGateRefused:
		s = "A gate refused the file before it was fetched."
	case remotecuration.ClassStorage:
		s = "The file could not be stored; a later attempt may succeed."
	}
	if s == "" {
		s = "The download failed at the step " + string(f.Step) + "."
	}
	if engine != "" {
		s += " " + engine
	}
	if f.Address != "" {
		s += " The address asked was " + f.Address + " (a query is never shown)."
	}
	return s
}

func orUpstream(host string) string {
	if host == "" {
		return "The upstream"
	}
	return host
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ClientFailures is the recorder of clients' failed fetches (R-CUR-011, Q-CUR-19): internal/remotecuration/queue implements it, at
// most one write per repository and file per replica and minute.
type ClientFailures interface {
	ClientFailure(ctx context.Context, repoID int64, name, version, file string, f remotecuration.Failure)
}

// clientFailure records the failure of a client's fetch; a background request's failure is its run's, and a 404 is the negative
// cache's (R-REPO-028).
func (f *fetcher) clientFailure(ctx context.Context, err error) {
	rec := f.e.o.ClientFailures
	if rec == nil || err == nil || f.rq.background() || f.rq.Remote.ID() == 0 {
		return
	}
	if ee, ok := errs.As(err); ok && (ee.Code == errs.CodeShuttingDown || ee.Kind == errs.NotFound) {
		return
	}
	rec.ClientFailure(context.WithoutCancel(ctx), f.rq.Remote.ID(), f.rq.Package, f.rq.Version, f.rq.Path, Describe(err))
}
