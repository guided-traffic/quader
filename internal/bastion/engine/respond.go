package engine

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/errs"
)

// The retry hints of the Bastion's own answers (R-BAS-029).
const (
	retryUnreachable = 30 * time.Second
	retryBusy        = time.Second
)

func (e *engine) errInvalidToken(q *rq) *errs.Error {
	return errs.E(errs.Unauthenticated, errs.CodeInvalidToken, "The token is not valid.", errs.WithChallenge(e.challenge(q, true)))
}

func (e *engine) errCredentialsRequired(q *rq) *errs.Error {
	return errs.E(errs.Unauthenticated, errs.CodeCredentialsRequired, "The request carries no credential and anonymous access is off.", errs.WithChallenge(e.challenge(q, false)))
}

func errUnreachable() *errs.Error {
	return errs.E(errs.Unavailable, errs.CodeUnavailable, "This service is not available right now; try again later.", errs.WithRetryAfter(retryUnreachable))
}

// errReadOnly is the refusal of a write (R-BAS-095, R-BAS-099 row 1): it names no address.
func errReadOnly() *errs.Error {
	return errs.E(errs.Forbidden, errs.CodeReadOnly, errs.ReadOnlyDetail)
}

// errTooLarge is the refusal of a read route's body above bastion.main.max_forwarded_body (R-BAS-095).
func errTooLarge() *errs.Error {
	return errs.E(errs.TooLarge, errs.CodeTooLarge, "The body is too large.")
}

func errBusy() *errs.Error {
	return errs.E(errs.Unavailable, errs.CodeBusy, "Too many requests are waiting; try again in a moment.", errs.WithRetryAfter(retryBusy))
}

// errShapeNone is the refusal of a read-shape answer none: the status the main installation said the caller
// would receive (R-BAS-011, R-BAS-022).
func (e *engine) errShapeNone(q *rq, status int) *errs.Error {
	switch status {
	case http.StatusUnauthorized:
		return e.errCredentialsRequired(q)
	case http.StatusForbidden:
		return errs.E(errs.Forbidden, errs.CodeForbidden, "The credential is valid but the operation is not permitted.")
	}
	return errs.E(errs.NotFound, errs.CodeNotFound, "The resource does not exist or is not visible to the caller.")
}

// refuse answers with one of the Bastion's own problem details (R-BAS-029): Cache-Control: no-store, the
// challenge on a 401, and the counters of a refusal. reason is the label of quader_bastion_refused_total, or
// empty for a refusal that has none (a read-shape answer none).
func (e *engine) refuse(q *rq, err *errs.Error, reason string) {
	q.finish(metrics.ClassRefused, metrics.ResultRefused)
	if reason != "" {
		q.reason = reason
		e.d.Metrics.Refused.WithLabelValues(reason).Inc()
	}
	q.w.Header().Set("Cache-Control", "no-store")
	e.problem(q, err)
}

// problem writes one of the Bastion's own answers in the error shape the format of the request declares (R-BAS-095, Q-BAS-337):
// below the registry root the distribution's (Q-BAS-169) whatever the table says, on /files/ and for a format the table does not
// name the problem details of R-BAS-029. Its Retry-After is at most the longest wait the format of the request tells its clients
// (R-BAS-058); the detail keeps the true wait.
func (e *engine) problem(q *rq, err *errs.Error) {
	facts := e.factsOf(q)
	if err.RetryAfter > 0 && facts.CapRetryAfter(err.RetryAfter) != err.RetryAfter {
		capped := *err
		capped.RetryAfter = facts.CapRetryAfter(err.RetryAfter)
		err = &capped
	}
	shape := facts.ErrorShape
	if q.fam == familyRegistry {
		shape = iface.ErrorShapeDistribution
	}
	writeShaped(q.w, q.r, shape, err)
}

// fail answers 502 bad_gateway or 504 gateway_timeout: the request to the main installation did not produce an
// answer to pass on (R-BAS-021). Nothing is kept.
func (e *engine) fail(q *rq, err error) {
	q.finish(metrics.ClassPass, metrics.ResultError)
	q.w.Header().Set("Cache-Control", "no-store")
	var timeout net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()):
		e.problem(q, errs.E(errs.UpstreamTimeout, errs.CodeGatewayTimeout, "This service did not answer in time; try again later."))
	default:
		e.problem(q, errBadGateway())
	}
}

// errBadGateway is the 502 of an answer the Bastion could not get or does not pass on (R-BAS-021, R-BAS-099 row 6).
func errBadGateway() *errs.Error {
	return errs.E(errs.UpstreamFailed, errs.CodeBadGateway, "This service could not answer; try again later.")
}

// mainError maps the error of a request to the main installation to the Bastion's answer (R-BAS-021,
// R-BAS-025, R-BAS-027, R-BAS-028).
func (e *engine) mainError(q *rq, err error) {
	var ceiling *mainclient.CeilingError
	switch {
	case q.ctx().Err() != nil:
		// The caller went away: there is nobody to answer.
		q.finish(metrics.ClassPass, metrics.ResultError)
	case errors.As(err, &ceiling):
		// Above the anonymous ceiling: 503 busy with the wait the ceiling names, which the format caps (R-BAS-094, R-BAS-091).
		e.refuse(q, errs.E(errs.Unavailable, errs.CodeBusy, "Too many requests are waiting; try again in a moment.",
			errs.WithRetryAfter(max(ceiling.RetryAfter, retryBusy))), metrics.RefusedCeiling)
	case errors.Is(err, mainclient.ErrBusy):
		e.refuse(q, errBusy(), metrics.RefusedBusy)
	case errors.Is(err, mainclient.ErrTokenRefused), errors.Is(err, mainclient.ErrInterfaceMismatch):
		// The client Deps.Main is wrapped by Link.Guard, which has made the replica unreachable already.
		e.refuse(q, errUnreachable(), metrics.RefusedUnreachable)
	case errors.Is(err, errForeignLocation):
		e.refuse(q, errBadGateway(), metrics.RefusedForeignLocation)
	default:
		e.fail(q, err)
	}
}

// unreachable answers the request that needs the main installation while the replica treats it as unreachable
// (R-BAS-027): 503 unavailable with Retry-After: 30.
func (e *engine) unreachable(q *rq) {
	e.refuse(q, errUnreachable(), metrics.RefusedUnreachable)
}

// hopByHop reports a hop-by-hop header name in canonical form (RFC 9110 section 7.6.1).
func hopByHop(name string) bool {
	switch name {
	case "Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	}
	return false
}

// neverReplayed reports a response header that is dropped whatever the main installation sent, also from an answer
// passed through: the replica header, the cache state (Q-BAS-40), cookies (R-BAS-003) and the main installation's
// request ID. The headers of the interface (Quader-Bastion-*) are dropped by their prefix. Each is a per-request
// header of the core (catalog.PerRequestHeaders) or Set-Cookie; the other per-request headers are the framing of the
// one answer passed through (Content-Length, Content-Range, Accept-Ranges, Date, Age), its Retry-After and the member
// of a virtual repository that answered it (Quader-Resolved-From), which a cached answer never replays because
// [keepHeader] keeps the representation headers only (TestR_BAS_053_TheEnginesHeaderListsAreTheCatalogs).
func neverReplayed(name string) bool {
	switch name {
	case "X-Quader-Replica", "Cache-Status", "Set-Cookie", "X-Request-Id":
		return true
	}
	return false
}

// passedHeaders are the headers of an answer of the main installation that a caller receives besides the core's representation
// headers and the replay headers of the request's format (R-BAS-099 row 5, Q-BAS-425): the framing of the one answer passed on, the
// wait of a refusal, the challenge of a 401, the methods of a 405, the links of a page and the version of the distribution's API
// that the registry root's core routes answer with, and a Location the Bastion checked (row 6). Every other header stays at the
// Bastion: the member repository of a virtual repository that served the answer (Quader-Resolved-From), every other Quader-
// header, the main installation's RateLimit fields, its request ID and replica, its cache state and its cookies.
func passedHeaders() []string {
	return []string{
		"Content-Length", "Content-Range", "Accept-Ranges", "Retry-After", "Www-Authenticate", "Allow", "Link",
		"Docker-Distribution-Api-Version", "Location",
	}
}

// replayHeader copies the headers of the main installation's response that the caller receives: an allowlist of the core's
// representation headers, the headers the format of the request declares (iface.HandshakeFormat.ReplayHeaders) and
// [passedHeaders] (R-BAS-099 row 5). A header the answer's Connection names is hop-by-hop for that answer and is dropped.
func replayHeader(src http.Header, declared []string) http.Header {
	drop := map[string]bool{}
	for _, v := range src.Values("Connection") {
		for _, tok := range strings.Split(v, ",") {
			drop[http.CanonicalHeaderKey(strings.TrimSpace(tok))] = true
		}
	}
	out := http.Header{}
	for _, list := range [][]string{representationHeaders(), declared, passedHeaders()} {
		for _, k := range list {
			ck := http.CanonicalHeaderKey(k)
			if hopByHop(ck) || neverReplayed(ck) || drop[ck] || strings.HasPrefix(ck, "Quader-") {
				continue
			}
			if vs := src.Values(ck); len(vs) > 0 {
				out[ck] = append([]string(nil), vs...)
			}
		}
	}
	return out
}

// errForeignLocation is the answer of the main installation whose Location names another host than the Bastion's (R-BAS-099
// row 6): it is not passed on.
var errForeignLocation = errors.New("the answer of the main installation names a location on another host than the Bastion's")

// locationError returns errForeignLocation when an answer of the main installation carries a Location that is neither a
// relative reference nor a URL on the Bastion's own scheme, host and port (R-BAS-099 row 6, Q-BAS-427), and logs the location's
// host for the operator. Such an answer would send the caller to the main installation, an upstream or any other address the
// main installation renders. The main installation renders the URLs of a forward on the Bastion's base URL (R-BAS-017), and a
// signed link to an object store is never the answer to a forward (storage.md R-STOR-070 step 2), so the check holds what both
// promise instead of trusting them; mainError answers it 502.
func (e *engine) locationError(h http.Header) error {
	for _, v := range h.Values("Location") {
		u, err := url.Parse(strings.TrimSpace(v))
		switch {
		case strings.ContainsRune(v, '\\'):
			// A client that resolves the reference as a browser does reads "/\host/x" as a URL on host: no Location of the main
			// installation has a backslash, so one with it is not passed on.
		case err == nil && u.Scheme == "" && u.Host == "" && u.Opaque == "":
			continue // a relative reference: on the host the caller asked
		case err == nil && strings.EqualFold(u.Scheme, e.base.Scheme) && strings.EqualFold(u.Hostname(), e.base.Hostname()) &&
			effectivePort(u) == effectivePort(e.base):
			continue
		}
		host := ""
		if err == nil {
			host = u.Host
		}
		e.log.Warn("an answer of the main installation names a location on another host and is not passed on", "location_host", host)
		return errForeignLocation
	}
	return nil
}

// effectivePort is the port of an http or https URL, the scheme's default when it names none.
func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}
	return "443"
}

// representationHeaders are the core's representation headers (catalog.RepresentationHeaders, Q-BAS-181): the
// representation metadata and validators of RFC 9110 and the content headers of R-ARCH-007 that every content answer
// of the main installation carries. The engine holds the list itself because the Bastion's code imports no format
// package (R-BAS-001); a test holds it equal to the catalog's.
func representationHeaders() []string {
	return []string{
		"Content-Type", "Content-Encoding", "Content-Language", "Content-Disposition", "Etag", "Last-Modified", "Cache-Control",
		"Expires", "Vary", "Repr-Digest",
		"X-Content-Type-Options", "Content-Security-Policy", "Cross-Origin-Resource-Policy",
	}
}

// perRequestHeaders are the core's per-request headers (catalog.PerRequestHeaders, Q-BAS-181): they belong to one answer
// and a cached answer never replays them, whatever a format declares. A test holds the list equal to the catalog's.
func perRequestHeaders() []string {
	return []string{
		"Content-Length", "Content-Range", "Accept-Ranges", "Date", "Age",
		"X-Request-Id", "X-Quader-Replica", "Cache-Status", "Retry-After", "Quader-Resolved-From",
	}
}

// keepHeader selects the response headers a cache entry replays (R-BAS-022, R-BAS-053): the core's representation
// headers and the headers the format of the request declares in the handshake's table (Q-BAS-171), never a header of
// the interface, a per-request header of the core, a cookie or a hop-by-hop header, which the main installation's
// catalog refuses as a declaration and the Bastion drops all the same.
func keepHeader(src http.Header, declared []string) http.Header {
	out := http.Header{}
	perRequest := perRequestHeaders()
	for _, list := range [][]string{representationHeaders(), declared} {
		for _, k := range list {
			if hopByHop(k) || neverReplayed(k) || strings.HasPrefix(k, "Quader-") || slices.Contains(perRequest, k) {
				continue
			}
			if vs := src.Values(k); len(vs) > 0 {
				out[k] = append([]string(nil), vs...)
			}
		}
	}
	return out
}

// writeRefusal replays a kept response: status, headers and body.
func (e *engine) writeRefusal(q *rq, ref *refusal, source string) {
	h := q.w.Header()
	for k, vs := range ref.Header {
		h[k] = append([]string(nil), vs...)
	}
	h.Set("Content-Length", strconv.Itoa(len(ref.Body)))
	q.out.source = source
	q.w.WriteHeader(ref.Status)
	if q.r.Method != http.MethodHead {
		_, _ = q.out.Write(ref.Body)
	}
}
