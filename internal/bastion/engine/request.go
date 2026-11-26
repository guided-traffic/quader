package engine

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/observability"
	"github.com/guided-traffic/quader/internal/qname"
)

// rq is one request on its way through the flow.
type rq struct {
	w    http.ResponseWriter
	out  *countingWriter
	r    *http.Request
	e    *engine
	span trace.Span

	start      time.Time
	peer       netip.Addr
	client     netip.Addr
	clientText string
	addr       string // the guard's key of the client: an IPv6 address as its /64

	hasAuth bool
	cred    credential
	// credOK is false for a credential the Bastion refuses itself (parseCredential), read once the family is known.
	credOK  bool
	credAns *answerVal
	subject iface.Subject
	// form is the body of POST /v2/_token, read before the limits: its password is the credential (R-BAS-051).
	form []byte

	format, repo string
	fam          family
	core         string
	facts        iface.HandshakeFormat
	classified   bool
	target       string
	baseKey      string
	cacheKey     string

	class, result, reason string
	counted               bool
	collapsed             bool
	mainRequestID         string
	limiter               string

	// flight is the flight this request leads, nil if it leads none; hold is the context of the request to the
	// main installation, which a fill takes over from the request (Q-BAS-123). call is the flight's call, through
	// which the waiters of the flight follow a fill (R-BAS-054).
	flight *handle
	hold   *mainHold
	call   *call
	// passOnly is set for a core route of the registry root: its answer is passed on and nothing of it is kept or
	// collapsed (R-BAS-051).
	passOnly bool
	// offer is the handover offer the request's last forward carried (R-BAS-072); noOffer keeps every further forward of
	// the request without one (the forward that replaces a failed handover). handoverOutcome gives the request log's
	// bastion_handover once the request received a handover (R-BAS-075).
	offer           iface.HandoverOffer
	noOffer         bool
	handoverOutcome func() string
}

// countingWriter counts the body bytes served by their source (quader_bastion_bytes_served_total).
type countingWriter struct {
	http.ResponseWriter
	q      *rq
	source string
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.ResponseWriter.Write(p)
	if n > 0 {
		c.q.e.d.Metrics.BytesServed.WithLabelValues(c.source).Add(float64(n))
	}
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (c *countingWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

// Flush implements http.Flusher.
func (c *countingWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (q *rq) ctx() context.Context { return q.r.Context() }

// finish records the class and result of the request once; the first call wins.
func (q *rq) finish(class, result string) {
	if q.counted {
		return
	}
	q.counted, q.class, q.result = true, class, result
	q.e.d.Metrics.Requests.WithLabelValues(class, result).Inc()
}

// ServeHTTP implements [http.Handler]: the steps of R-BAS-018 in their order, the first that answers ends it.
func (e *engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := e.newRq(w, r)
	defer q.end()
	if !e.stepHost(q) || !e.stepBlocked(q) || !e.stepPath(q) || !e.stepTokenForm(q) || !e.stepLimits(q) {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if q.core == coreVersion && !q.hasAuth {
			e.versionChallenge(q)
			return
		}
		if !e.stepCredentials(q) {
			return
		}
		if q.core != "" {
			e.core(q)
			return
		}
		e.read(q)
	case http.MethodOptions:
		e.refuse(q, errs.E(errs.MethodNotAllowed, errs.CodeMethodNotAllowed, "This address serves GET and HEAD.", errs.WithAllow(http.MethodGet, http.MethodHead)), metrics.RefusedNotServed)
	default:
		if anonymousTokenRequest(q) {
			if e.stepCredentials(q) {
				e.answerAnonymousToken(q)
			}
			return
		}
		e.write(q)
	}
}

// core answers a read of a core route of the registry root (R-BAS-051): an anonymous token request at the Bastion itself
// (R-BAS-094), an anonymous HEAD of the token service with the token service's own 405, every other one forwarded.
func (e *engine) core(q *rq) {
	switch {
	case anonymousTokenRequest(q):
		e.answerAnonymousToken(q)
	case q.core == coreToken && !q.hasAuth:
		e.refuse(q, errs.E(errs.MethodNotAllowed, errs.CodeMethodNotAllowed, "The token service answers GET and POST.", errs.WithAllow(http.MethodGet, http.MethodPost)), metrics.RefusedNotServed)
	default:
		e.passCore(q)
	}
}

func (e *engine) newRq(w http.ResponseWriter, r *http.Request) *rq {
	// Every problem the Bastion answers has the type about:blank and names no party (R-BAS-029, R-BAS-099, Q-BAS-321); the
	// client listener's chain marks its requests so too, and the engine marks them for a handler that runs without it.
	r = r.WithContext(errs.WithBastionAnswer(r.Context()))
	// The request reads the configuration once, at its start: every decision it makes is made with the same figures, the old
	// ones or the new ones of a handshake that swaps them meanwhile (R-BAS-093).
	r = r.WithContext(settings.With(r.Context(), e.d.Link.Configuration().Current()))
	q := &rq{w: w, r: r, e: e, start: e.now(), span: trace.SpanFromContext(r.Context())}
	q.out = &countingWriter{ResponseWriter: w, q: q, source: metrics.SourceMain}
	q.peer, q.client = e.addresses(r)
	if q.client.IsValid() {
		q.clientText = q.client.Unmap().String()
		q.addr = guard.AddressKey(q.client)
	} else {
		q.addr = "invalid"
	}
	q.hasAuth = strings.TrimSpace(r.Header.Get("Authorization")) != ""
	q.target = r.RequestURI
	if !strings.HasPrefix(q.target, "/") {
		q.target = r.URL.EscapedPath()
		if r.URL.RawQuery != "" {
			q.target += "?" + r.URL.RawQuery
		}
	}
	return q
}

// addresses returns the TCP peer and the client address as the chain resolved them (R-ARCH-006 with the
// Bastion's own trusted proxies); without a chain the peer is the client.
func (e *engine) addresses(r *http.Request) (peer, client netip.Addr) {
	info := httpserver.InfoFrom(r.Context())
	peer, client = info.Peer, info.Client
	if !peer.IsValid() {
		if ap, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
			peer = ap.Addr()
		} else if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			peer, _ = netip.ParseAddr(host)
		}
	}
	if !client.IsValid() {
		client = peer
	}
	return peer.Unmap(), client
}

// end finishes the request: the counter, the request log fields and the span of R-BAS-042.
func (q *rq) end() {
	if !q.counted {
		q.finish(metrics.ClassPass, metrics.ResultError)
	}
	if ri := observability.RequestInfoFrom(q.ctx()); ri != nil {
		ri.SetRemoteAddr(q.clientText)
		if q.repo != "" {
			ri.SetRepo(q.repo, q.format)
		}
		ri.SetCache(q.result)
		if q.subject.TokenID != "" {
			ri.SetToken(q.subject.TokenID, q.cred.method)
		}
		if q.reason != "" {
			ri.SetDenyReason(q.reason)
		}
		if q.limiter != "" {
			ri.SetLimit(q.limiter, "bastion")
		}
		ri.SetField("bastion_class", q.class)
		ri.SetField("bastion_result", q.result)
		if q.cred.hint != "" {
			ri.SetField("token_hint", q.cred.hint)
		}
		if q.mainRequestID != "" {
			ri.SetField("main_request_id", q.mainRequestID)
		}
		if q.handoverOutcome != nil {
			ri.SetField("bastion_handover", q.handoverOutcome())
		}
	}
	q.span.SetAttributes(attribute.String("quader.bastion.class", q.class), attribute.String("quader.bastion.result", q.result))
	if q.reason != "" {
		q.span.SetAttributes(attribute.String("quader.bastion.refusal", q.reason))
	}
}

// stepHost is step 1: the effective host must be the host of server.base_url (421 misdirected_request).
func (e *engine) stepHost(q *rq) bool {
	if e.effectiveHost(q) == e.baseHost {
		return true
	}
	e.refuse(q, errs.E(errs.Misdirected, errs.CodeMisdirectedRequest, "This address does not answer for the host of the request."), metrics.RefusedNotServed)
	return false
}

// effectiveHost is the request's host, or the host a trusted proxy forwarded (R-ARCH-006, R-EP-003). An IP
// literal and a missing host are never the Bastion's host.
func (e *engine) effectiveHost(q *rq) string {
	h := q.r.Host
	if e.trustedPeer(q.peer) {
		if fh := e.forwardedHost(q.r.Header); fh != "" {
			h = fh
		}
	}
	h = strings.ToLower(strings.TrimSuffix(hostOnly(h), "."))
	if h == "" {
		return ""
	}
	if _, err := netip.ParseAddr(strings.Trim(h, "[]")); err == nil {
		return ""
	}
	return h
}

func (e *engine) trustedPeer(a netip.Addr) bool {
	for _, p := range e.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func hostOnly(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}

// forwardedHost reads the host the right-most hop reports, from the family server.forwarded_header selects.
func (e *engine) forwardedHost(h http.Header) string {
	if !e.forwarded {
		vals := h.Values("X-Forwarded-Host")
		if len(vals) == 0 {
			return ""
		}
		parts := strings.Split(vals[len(vals)-1], ",")
		return strings.TrimSpace(parts[len(parts)-1])
	}
	vals := h.Values("Forwarded")
	if len(vals) == 0 {
		return ""
	}
	elems := strings.Split(vals[len(vals)-1], ",")
	for _, pair := range strings.Split(elems[len(elems)-1], ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && strings.EqualFold(k, "host") {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// stepBlocked is step 2: a blocked address answers 429 address_blocked.
func (e *engine) stepBlocked(q *rq) bool {
	return e.admit(q, e.d.Guard.Blocked(q.ctx(), q.addr))
}

// stepPath is step 3: only /api/<format>/<projectKey>/<repoKey>/…, /files/<projectKey>/<repoKey>/…, the registry root
// /v2/… and a bound host's prefixes are served (R-BAS-003, R-BAS-050, R-BAS-051, R-BAS-057). Below /v2/ a path that is
// not served answers 404 NAME_UNKNOWN in the distribution's shape. The credential of the request is read once the
// family is known, because a registry token is a credential below /v2/ only (R-BAS-052).
func (e *engine) stepPath(q *rq) bool {
	c, ok := e.classifyRq(q)
	if !ok {
		q.fam = c.fam
		e.refuse(q, errs.E(errs.NotFound, errs.CodeNotFound, "Nothing is served at this address."), metrics.RefusedNotServed)
		return false
	}
	q.cred, q.credOK = parseCredential(q.r.Header, q.fam)
	// An anonymous registry token is no credential: the request takes the anonymous limits and slots (Q-BAS-168).
	q.hasAuth = q.hasAuth && !q.cred.dropped
	return true
}

// classifyRq classifies the request's path once and keeps the result on the request.
func (e *engine) classifyRq(q *rq) (pathClass, bool) {
	c, ok := e.classify(q.r.URL.EscapedPath())
	if ok {
		q.classified = true
		q.fam, q.core, q.format, q.repo, q.facts = c.fam, c.core, c.format, c.repo, c.facts
	}
	return c, ok
}

// factsOf returns the format facts of the request, classifying its path when an earlier step refuses before step 3:
// the block list's refusal takes the status of the format's rate limit too (R-BAS-056).
func (e *engine) factsOf(q *rq) iface.HandshakeFormat {
	if q.classified {
		return q.facts
	}
	c, ok := e.classify(q.r.URL.EscapedPath())
	q.fam = c.fam
	if !ok {
		return iface.HandshakeFormats(nil).Lookup(c.format)
	}
	return c.facts
}

// classifyPath reads the family, the format and the qualified repository name "<projectKey>/<repoKey>" from the
// path as received, through qname.ParseAddress, whose key rules are INV-5's; the Bastion reads no other part of a
// path (R-BAS-018). The main installation's router decodes every segment before it compares it (net/http.ServeMux),
// so a segment that names the family, the format, the project or the key in an encoded spelling
// (/api/%761/users is /api/v1/users there) is not one the Bastion can classify by what it received: such a path is
// not served, and so is a path with a dot segment, which the router answers with a redirect to the cleaned path
// (R-BAS-003, Q-BAS-121). A path with one segment after the format (/api/npm/web) is no address, and a key with the
// cache suffix is no repository key: the cache view is not served.
func classifyPath(p string) (format, repo string, ok bool) {
	a, ok := qname.ParseAddress(p)
	if !ok || a.CacheView || a.DotSegments() {
		return "", "", false
	}
	format = string(a.Family)
	if a.Family == qname.FamilyAPI {
		format = a.Format
	}
	return format, a.Repo.String(), true
}

// stepLimits is step 4, first half: the per-address request limit of the class (R-BAS-035).
func (e *engine) stepLimits(q *rq) bool {
	return e.admit(q, e.d.Guard.Request(q.ctx(), q.addr, guard.Credentialed(q.hasAuth)))
}

// stepCredentials is step 4, second half (R-BAS-019, R-BAS-036): the credential of a read, refused before any
// request when it is malformed, a Bastion token or of an unknown type; anonymous only while the main
// installation admits anonymous requests; the failed-authentication timeout for a credential the Bastion has
// no usable answer for.
func (e *engine) stepCredentials(q *rq) bool {
	c, ok := q.cred, q.credOK
	if !ok {
		e.d.Guard.FailedAuth(q.ctx(), q.addr)
		e.refuse(q, e.errInvalidToken(q), metrics.RefusedInvalid)
		return false
	}
	if c.registry && !c.anonymous && e.registryTokenExpired(c) {
		// The end of a registry token's life, which a client answers with a new token request: no request, and no
		// failed authentication (R-BAS-052).
		e.refuse(q, e.errInvalidToken(q), metrics.RefusedInvalid)
		return false
	}
	if c.anonymous {
		st := e.d.Link.State()
		switch {
		case !st.Handshaken:
			// The main installation has not said yet whether it admits anonymous requests (Q-BAS-99).
			e.unreachable(q)
			return false
		case !st.AnonymousReads:
			e.refuse(q, e.errCredentialsRequired(q), metrics.RefusedInvalid)
			return false
		}
		return true
	}
	if q.credAns = e.answers.credential(q.ctx(), c.key); q.credAns != nil {
		q.subject = q.credAns.Subject
		return true
	}
	return e.admit(q, e.d.Guard.AuthTimeout(q.ctx(), q.addr))
}

// admit renders the guard's verdict: nil for an admitted request.
func (e *engine) admit(q *rq, v guard.Verdict) bool {
	if v.Allowed {
		return true
	}
	e.refuseVerdict(q, v)
	return false
}

// refuseVerdict renders a refusal of the guard as the problem details of R-BAS-029 with Retry-After and the
// RateLimit-Policy and RateLimit fields of R-API-016. The wait is rounded once ([oneWait]); Retry-After and the RateLimit
// field's t tell at most the longest wait the format of the request declares (R-BAS-058), and the detail names the true
// wait, so a client of NuGet or Ansible Galaxy that is blocked for an hour comes back within 30 s and its user reads the hour.
func (e *engine) refuseVerdict(q *rq, v guard.Verdict) {
	if v.Quiet {
		markQuiet(q.ctx())
	}
	// Every refusal of the guard is a 429 (guard.Verdict.Status says so), or the status the format declares for a rate
	// limit on its family: 503 for a format whose clients take a 429 badly (R-BAS-056, R-BAS-058).
	code := v.Code
	if code == "" {
		code = iface.CodeRateLimited
	}
	facts := e.factsOf(q)
	kind := errs.RateLimited
	if facts.RateLimitedStatus == http.StatusServiceUnavailable {
		kind = errs.Unavailable
	}
	name, quota, window := e.policyOf(q, v.Reason, q.cred.anonymous || !q.hasAuth)
	wait := oneWait(v.RetryAfter)
	told := facts.CapRetryAfter(wait)
	q.limiter = name
	err := errs.E(kind, code, refusalDetail(code, wait),
		errs.WithRetryAfter(told),
		errs.WithHeader("RateLimit-Policy", `"`+name+`";q=`+strconv.Itoa(quota)+";w="+strconv.Itoa(window)),
		errs.WithHeader("RateLimit", `"`+name+`";r=0;t=`+strconv.FormatInt(int64(told/time.Second), 10)))
	e.refuse(q, err, v.Reason)
}

// oneWait rounds the wait of a refusal up once, as the main installation's refusals do (usage-and-limits.md Q-LIM-291): to
// whole seconds below two minutes, at least one, and to whole minutes from there. The rounded wait is the one Retry-After, the
// RateLimit field's t and the detail name, so a client that waits it is never early.
func oneWait(d time.Duration) time.Duration {
	s := max((d+time.Second-1)/time.Second, 1) * time.Second
	if s < 2*time.Minute {
		return s
	}
	return (s + time.Minute - 1) / time.Minute * time.Minute
}

// refusalDetail is the detail of a refusal of the guard: what refused, and the true wait in words (R-BAS-029), which a
// format's cap of Retry-After does not shorten (R-BAS-058).
func refusalDetail(code string, wait time.Duration) string {
	again := "Try again in " + waitWords(wait) + "."
	switch code {
	case iface.CodeAddressBlocked:
		return "Requests from this address are refused for a while after too many refused requests. " + again
	case iface.CodeAuthTimeout:
		return "Too many failed authentications from this address: a credential that cannot be checked now is refused. " + again
	}
	return "Too many requests from this address. " + again
}

// waitWords names a wait that oneWait rounded: "45 seconds", "14 minutes", "3 hours", "2 hours 1 minute".
func waitWords(d time.Duration) string {
	plural := func(n int64, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return strconv.FormatInt(n, 10) + " " + unit + "s"
	}
	if d < 2*time.Minute {
		return plural(int64(d/time.Second), "second")
	}
	m := int64(d / time.Minute)
	if m < 120 {
		return plural(m, "minute")
	}
	if m%60 == 0 {
		return plural(m/60, "hour")
	}
	return plural(m/60, "hour") + " " + plural(m%60, "minute")
}

// policyOf names the limit that refused and its quota and window in seconds, for the RateLimit-Policy field: the figures of
// the configuration the request read (R-BAS-093).
func (e *engine) policyOf(q *rq, reason string, anonymous bool) (name string, quota, window int) {
	f := e.d.Link.Configuration().For(q.ctx()).Flood
	switch reason {
	case metrics.RefusedMissLimited:
		quota = f.Authenticated.MissesPerMinute
		if anonymous {
			quota = f.Anonymous.MissesPerMinute
		}
		return "misses", quota, 60
	case metrics.RefusedAuthTimeout:
		return "failed-auth", f.FailedAuth.MaxFailures, int(f.FailedAuth.Window / time.Second)
	case metrics.RefusedBlocked:
		return "blocking", f.Blocking.Threshold, int(f.Blocking.Window / time.Second)
	}
	burst, rps := f.Authenticated.Burst, f.Authenticated.RequestsPerSecond
	if anonymous {
		burst, rps = f.Anonymous.Burst, f.Anonymous.RequestsPerSecond
	}
	return "requests", burst, max(1, (burst+rps-1)/rps)
}
