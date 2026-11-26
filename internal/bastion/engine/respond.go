package engine

import (
	"context"
	"errors"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
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
	return errs.E(errs.Unavailable, errs.CodeMainUnreachable, "The main installation is not reachable; retry later.", errs.WithRetryAfter(retryUnreachable))
}

func errBusy() *errs.Error {
	return errs.E(errs.Unavailable, errs.CodeBastionBusy, "The Bastion is at its limit of requests to the main installation; retry.", errs.WithRetryAfter(retryBusy))
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

// problem writes one of the Bastion's own answers in the shape of the request's family: below the registry root the
// distribution's (Q-BAS-169), everywhere else the problem details of R-BAS-029. Its Retry-After is at most the longest wait
// the format of the request tells its clients (R-BAS-058); the detail keeps the true wait.
func (e *engine) problem(q *rq, err *errs.Error) {
	if facts := e.factsOf(q); err.RetryAfter > 0 && facts.CapRetryAfter(err.RetryAfter) != err.RetryAfter {
		capped := *err
		capped.RetryAfter = facts.CapRetryAfter(err.RetryAfter)
		err = &capped
	}
	if q.fam == familyRegistry {
		writeRegistryError(q.w, q.r, err)
		return
	}
	httpserver.Problem(q.w, q.r, err)
}

// fail answers 502 main_failed or 504 main_timeout: the request to the main installation did not produce an
// answer to pass on (R-BAS-021). Nothing is kept.
func (e *engine) fail(q *rq, err error) {
	q.finish(metrics.ClassPass, metrics.ResultError)
	q.w.Header().Set("Cache-Control", "no-store")
	var timeout net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()):
		e.problem(q, errs.E(errs.UpstreamTimeout, errs.CodeMainTimeout, "The main installation did not answer in time."))
	default:
		e.problem(q, errs.E(errs.UpstreamFailed, errs.CodeMainFailed, "The main installation did not answer."))
	}
}

// mainError maps the error of a request to the main installation to the Bastion's answer (R-BAS-021,
// R-BAS-025, R-BAS-027, R-BAS-028).
func (e *engine) mainError(q *rq, err error) {
	switch {
	case q.ctx().Err() != nil:
		// The caller went away: there is nobody to answer.
		q.finish(metrics.ClassPass, metrics.ResultError)
	case errors.Is(err, mainclient.ErrBusy):
		e.refuse(q, errBusy(), metrics.RefusedBusy)
	case errors.Is(err, mainclient.ErrTokenRefused), errors.Is(err, mainclient.ErrInterfaceMismatch):
		// The client Deps.Main is wrapped by Link.Guard, which has made the replica unreachable already.
		e.refuse(q, errUnreachable(), metrics.RefusedUnreachable)
	default:
		e.fail(q, err)
	}
}

// unreachable answers the request that needs the main installation while the replica treats it as unreachable
// (R-BAS-027): 503 main_unreachable with Retry-After: 30.
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

// replayHeader copies the headers of the main installation's response that the caller receives.
func replayHeader(src http.Header) http.Header {
	drop := map[string]bool{}
	for _, v := range src.Values("Connection") {
		for _, tok := range strings.Split(v, ",") {
			drop[http.CanonicalHeaderKey(strings.TrimSpace(tok))] = true
		}
	}
	out := http.Header{}
	for k, vs := range src {
		ck := http.CanonicalHeaderKey(k)
		if hopByHop(ck) || neverReplayed(ck) || drop[ck] || strings.HasPrefix(ck, iface.HeaderPrefix) {
			continue
		}
		out[ck] = append([]string(nil), vs...)
	}
	return out
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
