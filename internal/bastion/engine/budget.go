package engine

import (
	"net/http"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/budget"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/errs"
)

// The budgets of the Bastion's place in the request flow (R-BAS-096, R-LIM-044): a request is charged to its subject once the
// credential is decided (step 4 of R-BAS-018), a request to the main installation as an upstream request, a download before it
// starts; the bytes it writes are counted and, for a download in Enforce, paced. A caller with a token the Bastion holds no
// answer for is learned from the main installation's answer: that request is counted then and refused by nothing of the
// budgets (Q-BAS-471).

// statusWriter keeps the status the request was answered with, for the count of a refused request (R-LIM-008).
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(p []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(p)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Flush implements http.Flusher.
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// stepBudget charges the request to its subject once the credential is decided: an anonymous caller by its address, a caller
// with a token by the answer the Bastion holds. A caller whose answer the main installation still has to give is charged when it
// arrives ([engine.learnSubject]).
func (e *engine) stepBudget(q *rq) bool {
	if e.d.Budgets == nil {
		return true
	}
	switch {
	case q.cred.anonymous || !q.hasAuth:
		e.startCharge(q, iface.Subject{Kind: iface.SubjectAnonymous}, true)
	case q.credAns != nil && q.credAns.Subject.Kind == iface.SubjectToken:
		e.startCharge(q, q.credAns.Subject, false)
	default:
		return true
	}
	if r := q.charge.Request(q.ctx()); r != nil {
		e.refuseBudget(q, r)
		return false
	}
	return true
}

func (e *engine) startCharge(q *rq, sub iface.Subject, anonymous bool) {
	a := e.d.Link.Configuration().For(q.ctx())
	s := budget.SubjectOf(a.Place, sub, anonymous, q.client, q.addr)
	q.charge = e.d.Budgets.Charge(a, s, q.repo)
}

// learnSubject charges a request whose caller the main installation's answer named (Q-BAS-471): the request and the upstream
// requests it sent meanwhile are counted, nothing is refused.
func (e *engine) learnSubject(q *rq, sub iface.Subject) {
	if e.d.Budgets == nil || q.charge != nil || sub.Kind != iface.SubjectToken {
		return
	}
	e.startCharge(q, sub, false)
	q.charge.Count(q.ctx(), q.pendingUpstream)
	q.pendingUpstream = 0
}

// chargeUpstream charges a request to the main installation for the caller (upstreamRequestsPerHour); false when it refused.
func (e *engine) chargeUpstream(q *rq) bool {
	if e.d.Budgets == nil {
		return true
	}
	if q.charge == nil {
		q.pendingUpstream++
		return true
	}
	if r := q.charge.Upstream(q.ctx()); r != nil {
		e.refuseBudget(q, r)
		return false
	}
	return true
}

// admitTransfer is the check before a download starts (R-BAS-096): an answer of the class artifact, or a file below /files/, that
// writes its bytes on a GET. It reports false when it refused the request.
func (e *engine) admitTransfer(q *rq, cached bool) bool {
	if q.charge == nil || q.r.Method != http.MethodGet {
		return true
	}
	if r := q.charge.Transfer(q.ctx(), cached); r != nil {
		e.refuseBudget(q, r)
		return false
	}
	return true
}

// endCharge ends the request's charge: a refusal of the decision point (a 403) or a presented credential that failed (a 401) is a
// refused request of the subject (R-LIM-008); the challenge of a request without a credential is not. The bytes are counted and the
// lease given back.
func (e *engine) endCharge(q *rq) {
	if q.charge == nil {
		return
	}
	credential := q.hasAuth && !q.cred.anonymous
	if !q.budgetRefused && (q.sw.status == http.StatusForbidden || (q.sw.status == http.StatusUnauthorized && credential)) {
		q.charge.Refused(q.ctx())
	}
	q.charge.End(q.ctx())
}

// policyName is the RateLimit policy of R-LIM-020 of a figure.
func policyName(figure string) string {
	switch figure {
	case budget.FigureRequestsPerHour:
		return "requests-hour"
	case budget.FigureCachedBytesPerHour:
		return "cached-hour"
	case budget.FigureUncachedBytesPerHour:
		return "uncached-hour"
	case budget.FigureUpstreamRequestsPerHour:
		return "upstream-hour"
	case budget.FigureBytesPerDay:
		return "day"
	case budget.FigureParallelTransfers:
		return "parallel"
	case budget.FigureRefusedPerHour:
		return "refused-hour"
	}
	return "requests"
}

// refuseBudget answers a budget's refusal (R-BAS-096, R-LIM-020): the code and the members figure, limit, windowSeconds and
// retryAfterSeconds, the figure's RateLimit policy, one wait rounded once, in the format's error shape and status (R-BAS-095,
// Q-BAS-451), and nothing that names a place of the main installation (R-BAS-099).
func (e *engine) refuseBudget(q *rq, r *budget.Refusal) {
	q.budgetRefused = true
	facts := e.factsOf(q)
	kind := errs.RateLimited
	if facts.RateLimitedStatus == http.StatusServiceUnavailable {
		kind = errs.Unavailable
	}
	wait := oneWait(r.RetryAfter)
	if r.Window > 0 && (r.Code == budget.CodeDownloadLimitReached || r.Code == budget.CodeUpstreamLimitReached) {
		wait = min(wait, r.Window)
	}
	told := facts.CapRetryAfter(wait)
	name := policyName(r.Figure)
	q.limiter = "limits." + r.Figure
	opts := []errs.Option{
		errs.WithRetryAfter(told),
		errs.WithExtension("figure", r.Figure),
		errs.WithExtension("retryAfterSeconds", int64(wait/time.Second)),
		errs.WithHeader("RateLimit", `"`+name+`";r=0;t=`+strconv.FormatInt(int64(told/time.Second), 10)),
	}
	if r.Limit > 0 {
		opts = append(opts, errs.WithExtension("limit", r.Limit))
		policy := `"` + name + `";q=` + strconv.FormatInt(r.Limit, 10)
		if r.Window > 0 {
			policy += ";w=" + strconv.FormatInt(int64(r.Window/time.Second), 10)
		}
		switch r.Figure {
		case budget.FigureCachedBytesPerHour, budget.FigureUncachedBytesPerHour, budget.FigureBytesPerDay:
			policy += `;qu="content-bytes"`
		case budget.FigureParallelTransfers:
			policy += `;qu="concurrent-requests"`
		}
		opts = append(opts, errs.WithHeader("RateLimit-Policy", policy))
	}
	if r.Window > 0 {
		opts = append(opts, errs.WithExtension("windowSeconds", int64(r.Window/time.Second)))
	}
	e.refuse(q, errs.E(kind, r.Code, budgetDetail(r, wait), opts...), metrics.RefusedBudget)
}

// budgetDetail names the figure, its limit and the true wait in plain words, as the main installation's refusals do (R-LIM-020).
func budgetDetail(r *budget.Refusal, wait time.Duration) string {
	again := "Try again in " + waitWords(wait) + "."
	switch r.Code {
	case budget.CodeDownloadLimitReached:
		return "The download limit of " + amountWords(r) + " is used up. " + again
	case budget.CodeUpstreamLimitReached:
		return "The upstream limit of " + amountWords(r) + " is used up; what is cached is still served. " + again
	case budget.CodeTooManyParallelTransfers:
		return "The limit of " + strconv.FormatInt(r.Limit, 10) + " parallel transfers is reached: each layer of an image and each package of an install is one transfer. " + again
	case budget.CodeCallerBlocked:
		return "Too many refused requests: requests of this caller are refused for a while. " + again
	}
	return "The request limit of " + amountWords(r) + " is used up. " + again
}

// amountWords names a limit: "30 GB per hour", "20000 requests per hour", "1250 requests per 25 seconds".
func amountWords(r *budget.Refusal) string {
	per := "per hour"
	switch {
	case r.Window >= 24*time.Hour:
		per = "per day"
	case r.Window < time.Hour:
		per = "per " + strconv.FormatInt(int64(r.Window/time.Second), 10) + " seconds"
	}
	switch r.Figure {
	case budget.FigureCachedBytesPerHour, budget.FigureUncachedBytesPerHour, budget.FigureBytesPerDay:
		return bytesWords(r.Limit) + " " + per
	}
	return strconv.FormatInt(r.Limit, 10) + " requests " + per
}

func bytesWords(n int64) string {
	const mb, gb = 1_000_000, 1_000_000_000
	switch {
	case n >= gb:
		return trimDecimal(float64(n)/gb) + " GB"
	case n >= mb:
		return trimDecimal(float64(n)/mb) + " MB"
	}
	return strconv.FormatInt(n, 10) + " bytes"
}

func trimDecimal(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}
