package bastionmain

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/errs"
)

// RecognizeOptions configure [Recognize].
type RecognizeOptions struct {
	// Tokens validates a Bastion token (work package 1). Nil makes every Bastion token invalid: a main
	// installation that cannot validate one recognizes no Bastion.
	Tokens TokenAuthenticator
	// Metrics counts the interface requests; nil counts nothing.
	Metrics *Metrics
	// Throttle returns the failed-credential throttle of the request's client address (R-AUTH-038), nil for
	// none. The authenticator asks it when the token is not in the replica's cache and refuses the lookup
	// while the address is in throttled mode (429), and every token the authenticator refuses counts as a
	// failure of the address: the stage runs before the authentication stage, so without it a flood of
	// well-formed tokens would reach the token store unthrottled.
	Throttle func(r *http.Request) Throttle
	// Source returns the client address and the user agent of the request as the chain resolved them before
	// a Bastion's forwarded headers count (server.trusted_proxies only): the address the token came from,
	// which token.rejected names and the token's last use records (R-BAS-017). Nil names none.
	Source func(r *http.Request) (clientAddr, userAgent string)
	// Egress returns the networks the Bastion of a registration connects from (R-BAS-017, Q-BAS-334), from the replica's
	// snapshot of the registrations; the stage sets Recognition.FromEgress when the address Source names lies in one of them.
	// Nil, or no network, is a Bastion whose reports about its callers never count (Q-BAS-430).
	Egress func(bastionID string) []netip.Prefix
	// Forwarded re-derives, for a recognized request, what the chain resolved from the forwarded headers -
	// the client address, the effective host and the response base URL - with the networks of the request's own
	// registration (Recognition.FromEgress) trusted besides server.trusted_proxies, and returns the context the request
	// is served with (R-BAS-017, Q-BAS-430). It runs after the token check, so the headers of a request
	// without a valid Bastion token never count through it. Nil keeps what the chain resolved.
	Forwarded func(r *http.Request) context.Context
	// Disabled returns the way a registration is disabled (R-BAS-133), from the replica's snapshot of the registrations; empty while it
	// works. Nil disables none.
	Disabled func(bastionID string) iface.DisableMode
	// InFlight holds the forwards in flight per registration, which a hard disable cancels (R-BAS-133). Nil tracks none.
	InFlight *InFlight
	// Problem renders an error as the problem details of the management API, whatever the path: the
	// answers of this stage concern the Bastion, never the caller, so a package route does not render
	// them in its format's shape (R-BAS-008). The composition root passes errs.Write with the request's ID
	// and logger.
	Problem func(w http.ResponseWriter, r *http.Request, err error)
}

// Throttle is the failed-credential throttle of one client address (R-AUTH-038): token.Throttle, which the
// authentication stage uses, has the same methods.
type Throttle interface {
	// Exceeded reports whether the address is in throttled mode.
	Exceeded(ctx context.Context) bool
	// Count records one rejected credential from the address.
	Count(ctx context.Context)
}

// ThrottleScope prefixes the bucket of the throttle of rejected Bastion tokens: they count against a budget of
// their own, so the rejected credentials of the authentication stage never refuse a Bastion token and the other
// way round.
const ThrottleScope = "bastion:"

// UnknownBastion is the bastion label of an interface request whose token named no registration.
const UnknownBastion = "unknown"

// maxReplicaName bounds the Quader-Bastion-Replica value taken into a Recognition: a pod name is at most 253
// characters.
const maxReplicaName = 253

// Recognize is the Recognize stage of the Bastion interface (R-BAS-008, R-BAS-009, Q-BAS-52): it runs before the
// authentication and decides, for every request of the default endpoint, whether it is a request of a Bastion.
//
//   - A request that carries Quader-Bastion-Authorization is a forward, whatever its path; the token is the
//     Bearer credential of that header. A request without it is an interface call when its path, decoded
//     segment by segment as the router decodes it, lies below [iface.PathPrefix]; the token is then the
//     Bearer credential of Authorization (Q-BAS-115). Only a value that has the prefix of a Bastion token is
//     looked at: any other credential leaves the request ordinary, with no Recognition and no marking, and
//     every Quader-Bastion-* header is ignored.
//   - A Bastion token the authenticator refuses answers 401 bastion_token_invalid; one it does not look up
//     because the sender's address is throttled answers 429 rate_limited. A failure of the store is a 503, not
//     a verdict on the token.
//   - A request with a valid token whose Quader-Bastion-Interface is missing or not [iface.InterfaceVersion]
//     answers 400 bastion_interface_mismatch with the member interfaceVersion, and is served in no other way.
//   - A forward whose path is not one of the package clients' families (/api/<format>/, /files/ and the registry
//     root /v2/, decoded as the router decodes it) answers 404 not_found: a Bastion forwards nothing else, and no
//     other route runs for it, with the caller's credential or without one (R-BAS-014, R-BAS-051, Q-BAS-115).
//   - Otherwise the request carries a [Recognition]; every answer to it has Quader-Bastion-Interface. On an
//     interface route the validated token is taken out of Authorization, so no later stage sees it; on a
//     forwarded request Quader-Bastion-Authorization is taken out of the request the stages behind receive
//     (nothing downstream needs it, and a remote repository must never be able to forward it). The forwarded
//     headers are re-derived with the networks of the Bastion's own registration trusted, when the request came from one
//     ([Recognition.FromEgress], [RecognizeOptions.Forwarded]).
//   - A forward, the check and a catalogue route of a registration an administrator has disabled answer 503 bastion_disabled
//     (R-BAS-133): the Bastion gets nothing new while it is disabled, and learns the disable from its handshake, which, like the feed
//     and the download intake, answers as before. A forward is tracked while it runs, and a hard disable cancels it.
//   - The offer of a handover per blob, Quader-Bastion-Handover, is read into the Recognition of a forwarded GET
//     ([Recognition.Offer], R-BAS-072) and taken out of every request, recognized or not, before the stages behind: the
//     serve point decides a handover on the Recognition alone, so an ordinary caller that sends the header gets the file
//     (R-BAS-073, storage.md R-STOR-095 step 1).
//
// The 401, 404, 429 and 400 answers are written by o.Problem as the management API's problem details, also on
// a package route. Interface requests are counted by registration, operation and outcome.
func Recognize(o RecognizeOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			segs := routingSegments(r.URL.EscapedPath())
			forward := len(r.Header.Values(iface.HeaderAuthorization)) > 0
			route := !forward && interfacePath(segs)
			raw := bastionCredential(r, route)
			if !strings.HasPrefix(raw, iface.TokenPrefix) {
				// An ordinary request that sends the offer or the statement of a HEAD gets what any request gets: neither header
				// reaches a stage behind this one (R-BAS-008, R-BAS-073, Q-BAS-479).
				next.ServeHTTP(w, withoutOffer(r))
				return
			}
			op := operationOf(r, route)
			req := AuthRequest{}
			if o.Throttle != nil {
				req.Throttle = o.Throttle(r)
			}
			if o.Source != nil {
				req.ClientAddr, req.UserAgent = o.Source(r)
			}
			tok, err := authenticate(r.Context(), o.Tokens, raw, req)
			if err != nil {
				o.refuse(w, r, op, req.Throttle, err)
				return
			}
			if v := r.Header.Values(iface.HeaderInterface); len(v) != 1 || strings.TrimSpace(v[0]) != strconv.Itoa(iface.InterfaceVersion) {
				o.count(tok.Name, op, OutcomeMismatch)
				w.Header().Set(iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion))
				o.problem(w, r, errs.E(errs.Invalid, errs.CodeBastionInterfaceMismatch,
					"The Bastion speaks another version of the Bastion interface than this installation ("+strconv.Itoa(iface.InterfaceVersion)+").",
					errs.WithBadRequest(), errs.WithExtension(iface.ProblemMemberInterfaceVersion, iface.InterfaceVersion)))
				return
			}
			w.Header().Set(iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion))
			if refusedWhileDisabled(route, r.URL.Path) && o.disabled(tok.BastionID) != "" {
				o.count(tok.Name, op, OutcomeRefused)
				o.problem(w, r, disabledError())
				return
			}
			if !route {
				// Every answer to a forward reaches a caller of the Bastion: its problems name nothing of this installation
				// (R-BAS-099, Q-BAS-321).
				r = r.WithContext(errs.WithBastionForward(r.Context()))
			}
			if !route && !forwardablePath(segs) {
				// The route-family backstop: whatever the Bastion sent, a forward reaches no route of the management
				// API, the login tree or the UI. The answer is the one every path without a route gets.
				o.problem(w, r, errs.E(errs.NotFound, errs.CodeNotFound, "No route matches the request path."))
				return
			}
			rec := Recognition{
				Bastion: tok.Name, BastionID: tok.BastionID, TokenID: tok.TokenID, TokenExpiresAt: tok.ExpiresAt,
				Interface: iface.InterfaceVersion, Replica: clip(r.Header.Get(iface.HeaderReplica), maxReplicaName), Route: route,
				FromEgress: o.fromEgress(tok.BastionID, req.ClientAddr),
			}
			if !route && r.Method == http.MethodGet {
				// The offer of a handover per blob (R-BAS-072): a value the interface refuses, or more than one, is no offer.
				rec.Offer, rec.Offered, _ = iface.HandoverOfferFrom(r.Header)
				// The statement that the GET answers a HEAD (Q-BAS-479): a value other than the one the interface names is none.
				rec.ForHead = iface.ForHead(r.Header)
			}
			// The stages behind this one get a request of their own: the credential that proved the
			// Bastion is not in it, and the forwarded headers count with its registration's networks trusted.
			ctx := WithRecognition(r.Context(), rec)
			if o.Forwarded != nil {
				ctx = o.Forwarded(r.WithContext(ctx))
			}
			r2 := r.Clone(ctx)
			if route {
				r2.Header.Del("Authorization")
			} else {
				r2.Header.Del(iface.HeaderAuthorization)
			}
			// The offer and the statement of a HEAD are the Recognition's; no handler reads their headers.
			r2.Header.Del(iface.HeaderHandover)
			r2.Header.Del(iface.HeaderForHead)
			if !route {
				o.forward(w, r2, next, tok.BastionID)
				return
			}
			if op == "" {
				next.ServeHTTP(w, r2)
				return
			}
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r2)
			o.count(tok.Name, op, outcomeOf(sw.status))
		})
	}
}

// forward serves a recognized forward while it is tracked as in flight for its registration, so that a hard disable cancels it
// (R-BAS-133). A hard disable the snapshot took between the stage's check and the tracking is caught by the check after it.
func (o RecognizeOptions) forward(w http.ResponseWriter, r *http.Request, next http.Handler, bastionID string) {
	if o.InFlight == nil {
		next.ServeHTTP(w, r)
		return
	}
	ctx, done := o.InFlight.Track(r.Context(), bastionID)
	defer done()
	if o.disabled(bastionID) == iface.DisableHard {
		o.problem(w, r, disabledError())
		return
	}
	next.ServeHTTP(w, r.WithContext(ctx))
}

// disabled is the way the registration bastionID is disabled, empty without a source.
func (o RecognizeOptions) disabled(bastionID string) iface.DisableMode {
	if o.Disabled == nil {
		return ""
	}
	return o.Disabled(bastionID)
}

// refusedWhileDisabled reports whether a disabled registration's request is refused (R-BAS-133): a forward, the check and the
// catalogue routes; the handshake, the feed and the download intake are answered, so the Bastion learns the disable, confirms it and
// reports the downloads a soft disable lets finish.
func refusedWhileDisabled(route bool, path string) bool {
	return !route || path == iface.PathCheck || path == iface.PathCatalogue || strings.HasPrefix(path, iface.PathCatalogue+"/")
}

// disabledError is the 503 of a disabled registration's refused request (R-BAS-133): a problem of the interface, which the Bastion
// never passes on to its caller.
func disabledError() error {
	return errs.E(errs.Unavailable, errs.CodeBastionDisabled, "An administrator has disabled this Bastion: its requests are not answered until it is enabled again.")
}

// refuse answers a Bastion token the authenticator did not accept: 429 for a throttled address, 401 for a
// token it refused (counted as a failure of the address), 503 for a store that did not answer.
func (o RecognizeOptions) refuse(w http.ResponseWriter, r *http.Request, op string, th Throttle, err error) {
	switch {
	case errors.Is(err, ErrThrottled):
		o.count(UnknownBastion, op, OutcomeRefused)
		o.problem(w, r, errs.E(errs.RateLimited, errs.CodeRateLimited, "Too many rejected credentials from this address.", errs.WithRetryAfter(throttleWindow)))
	case errors.Is(err, ErrTokenInvalid):
		if th != nil {
			th.Count(r.Context())
		}
		o.count(UnknownBastion, op, OutcomeInvalid)
		o.problem(w, r, errs.E(errs.Unauthenticated, errs.CodeBastionTokenInvalid,
			"The Bastion token is invalid, revoked or expired.",
			errs.WithChallenge(`Bearer realm="quader", error="invalid_token"`)))
	default:
		o.count(UnknownBastion, op, OutcomeError)
		o.problem(w, r, storeError(err))
	}
}

// routingSegments splits an escaped request path into its segments and decodes each one on its own, which is
// how the router (net/http.ServeMux) matches a request against its patterns: "/api/%76%31/tokens" is api, v1,
// tokens - a route of the management API - and "/api%2Fv1/tokens" is api/v1, tokens, which no pattern holds. A
// segment that does not decode stays as it is, as the router keeps it. The decoded r.URL.Path is not that path.
func routingSegments(escaped string) []string {
	parts := strings.Split(strings.TrimPrefix(escaped, "/"), "/")
	for i, p := range parts {
		if d, err := url.PathUnescape(p); err == nil {
			parts[i] = d
		}
	}
	return parts
}

// interfacePath reports whether the routing segments lie below iface.PathPrefix (/api/v1/bastion/).
func interfacePath(segs []string) bool {
	return len(segs) >= 4 && segs[0] == "api" && segs[1] == "v1" && segs[2] == "bastion"
}

// forwardablePath reports whether the routing segments lie in a family a Bastion forwards: /files/…,
// /api/<format>/…, where <format> is any segment but v1, and the registry root /v2 and /v2/… (R-BAS-003, R-BAS-014,
// R-BAS-051). Every route of these families tells the marking stage its kind before it decides or reads a body (the
// format dispatcher, below /v2/ through its registry handler, the raw file handler, the registry's token service and
// catalog); the registry's version check answers GET and HEAD only and decides nothing. No route of any other family
// does.
func forwardablePath(segs []string) bool {
	switch {
	case len(segs) >= 2 && segs[0] == "files":
		return true
	case len(segs) >= 2 && segs[0] == "api":
		return segs[1] != "" && segs[1] != "v1"
	case len(segs) >= 1 && segs[0] == "v2":
		return true
	}
	return false
}

// fromEgress reports whether the address the request came from, as Source named it, lies in the networks of the registration
// bastionID (Q-BAS-430).
func (o RecognizeOptions) fromEgress(bastionID, source string) bool {
	if o.Egress == nil {
		return false
	}
	a, err := netip.ParseAddr(source)
	return err == nil && InEgress(a, o.Egress(bastionID))
}

func (o RecognizeOptions) problem(w http.ResponseWriter, r *http.Request, err error) {
	if o.Problem != nil {
		o.Problem(w, r, err)
		return
	}
	errs.Write(w, r, err, "", nil)
}

func (o RecognizeOptions) count(bastion, op, outcome string) {
	if o.Metrics != nil && op != "" {
		o.Metrics.Interface.WithLabelValues(bastion, op, outcome).Inc()
	}
}

// authenticate asks the authenticator, treating a missing one as a refusal of every token.
func authenticate(ctx context.Context, a TokenAuthenticator, raw string, req AuthRequest) (BastionToken, error) {
	if a == nil {
		return BastionToken{}, ErrTokenInvalid
	}
	return a.Authenticate(ctx, raw, req)
}

// storeError is the answer to a failure of the token store: an error already in the registry passes, any other
// is a 503 like the authentication stage's (R-AUTH-023), never a 401 that would make the Bastion discard a
// working token.
func storeError(err error) error {
	if _, ok := errs.As(err); ok {
		return err
	}
	return errs.E(errs.Unavailable, errs.CodeDatabaseUnavailable, "The Bastion token cannot be checked right now.", errs.WithCause(err))
}

// withoutOffer returns r, or a copy of it without Quader-Bastion-Handover and Quader-Bastion-For-Head when it carries one of
// them: on a request that is no recognized forward they are ordinary headers that no handler may read (R-BAS-073, Q-BAS-479). A
// request without them pays nothing.
func withoutOffer(r *http.Request) *http.Request {
	if len(r.Header.Values(iface.HeaderHandover)) == 0 && len(r.Header.Values(iface.HeaderForHead)) == 0 {
		return r
	}
	r2 := r.Clone(r.Context())
	r2.Header.Del(iface.HeaderHandover)
	r2.Header.Del(iface.HeaderForHead)
	return r2
}

// bastionCredential returns the raw credential of the request that may be a Bastion token: the Bearer value
// of Authorization on an interface call, of Quader-Bastion-Authorization on any other request. Only the first
// header line counts, as for Authorization everywhere else; any other scheme is no credential of a Bastion.
func bastionCredential(r *http.Request, route bool) string {
	name := iface.HeaderAuthorization
	if route {
		name = "Authorization"
	}
	v := r.Header.Values(name)
	if len(v) == 0 {
		return ""
	}
	scheme, rest, ok := strings.Cut(strings.TrimSpace(v[0]), " ")
	if !ok || !strings.EqualFold(scheme, iface.BearerScheme) {
		return ""
	}
	return strings.TrimSpace(rest)
}

// operationOf names the interface operation of a route request, "" for a path below the prefix that no
// operation serves and for a forwarded request.
func operationOf(r *http.Request, route bool) string {
	if !route {
		return ""
	}
	switch r.URL.Path {
	case iface.PathHandshake:
		return iface.OperationHandshake
	case iface.PathCheck:
		return iface.OperationCheck
	case iface.PathFeed:
		return iface.OperationFeed
	case iface.PathDownloads:
		return iface.OperationDownloads
	}
	return ""
}

// outcomeOf is the outcome label of an answered interface request.
func outcomeOf(status int) string {
	switch {
	case status == http.StatusNotImplemented:
		return OutcomeNotServed
	case status >= 500:
		return OutcomeError
	case status >= 400:
		return OutcomeRefused
	}
	return OutcomeOK
}

// clip cuts s to at most n bytes at a character boundary, and drops control characters: the value is
// attacker-influenced text that reaches the database and the audit trail.
func clip(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) { // the cut fell inside a multi-byte character
		s = s[:len(s)-1]
	}
	return s
}

// statusWriter records the status of the answer for the outcome label.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
