package errs

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ProblemContentType is the media type of RFC 9457 problem details.
const ProblemContentType = "application/problem+json"

// ProblemTypePrefix prefixes the code in the "type" member. The reference is
// relative to the origin, so it holds for every base URL.
const ProblemTypePrefix = "/api/v1/problems/"

// AboutBlank is the type of a problem that says no more than its status (RFC 9457 section 4.2.1). Every problem a Bastion answers
// and every problem the main installation renders for a forward of a Bastion has it: the relative type of [ProblemTypePrefix] is a
// path of the management API, which a Bastion does not serve and which names the installation behind it (bastion.md R-BAS-099,
// Q-BAS-321).
const AboutBlank = "about:blank"

// reader is who reads the problems of a request, when that is not a caller of this installation itself.
type reader uint8

const (
	// readerBastion: the request is answered by a Bastion.
	readerBastion reader = iota + 1
	// readerForward: the request is a forward of a Bastion that the main installation recognized; its answer reaches a caller of
	// the Bastion, who learns nothing of the installation behind it.
	readerForward
)

type readerKey struct{}

// WithBastionAnswer marks the context of a request a Bastion answers: [ProblemFor] gives every problem of it the type
// [AboutBlank] and the status phrase as its title (bastion.md R-BAS-029, Q-BAS-321).
func WithBastionAnswer(ctx context.Context) context.Context {
	return context.WithValue(ctx, readerKey{}, readerBastion)
}

// WithBastionForward marks the context of a forward of a Bastion that the main installation recognized (bastion.md R-BAS-099):
// [ProblemFor] gives every problem of it the type [AboutBlank] and the status phrase as its title, leaves out the main
// installation's requestId, and gives a failure of the remote engine a detail that names no repository and no host
// ([neutralDetail]).
func WithBastionForward(ctx context.Context) context.Context {
	return context.WithValue(ctx, readerKey{}, readerForward)
}

// BastionForward reports whether ctx is the context of a recognized forward of a Bastion ([WithBastionForward]). A component that
// writes a member or a sentence about this installation into a problem leaves it out then: the place and the link to the usage
// overview of a limit's refusal (usage-and-limits.md R-LIM-020, bastion.md R-BAS-099 row 7).
func BastionForward(ctx context.Context) bool {
	return readerOf(ctx) == readerForward
}

func readerOf(ctx context.Context) reader {
	if ctx == nil {
		return 0
	}
	r, _ := ctx.Value(readerKey{}).(reader)
	return r
}

// neutralDetail is the detail a forward of a Bastion reads for a failure of the remote engine and its upstream clients
// (R-REPO-029; bastion.md R-BAS-099 row 15, Q-BAS-426). Their own details name the remote repository that failed, which for a
// caller of a virtual repository is a member it did not address, and some name an upstream's host. The code stays, and so does
// every other member and header of the answer.
func neutralDetail(code string) (string, bool) {
	switch code {
	case CodeUpstreamFailed, CodeUpstreamTimeout, CodeEgressDenied, CodeUpstreamSecretUnavailable, CodeSourceNetworkUnavailable,
		CodeForeignHostDenied, CodeNotCachedOffline, CodeUpstreamAssumedOffline, CodeUpstreamRateLimited, CodeUpstreamAuthFailed,
		CodeUpstreamError, CodeUpstreamUnreachable, CodeUpstreamUnexpectedStatus, CodeUpstreamItemTooLarge,
		CodeUpstreamIntegrityMismatch, CodeUpstreamDigestMissing, CodeUpstreamContentTypeBlocked, CodeTLSUntrusted,
		CodeTooManyRedirects, CodeInsecureRedirect, CodeContentUnavailable:
		return "The package could not be fetched right now.", true
	case CodePopulateNotPermitted:
		return "The caller may not bring new content into the cache of this repository.", true
	}
	return "", false
}

// Problem is the wire form: the RFC 9457 members plus the quader extensions
// code, requestId and errors.
type Problem struct {
	Type      string       `json:"type"`
	Title     string       `json:"title"`
	Status    int          `json:"status"`
	Detail    string       `json:"detail,omitempty"`
	Instance  string       `json:"instance,omitempty"`
	Code      string       `json:"code"`
	RequestID string       `json:"requestId,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
	// Extensions are the further members of the body (Error.Extensions).
	Extensions map[string]any `json:"-"`
}

// MarshalJSON writes the standard members and then the extensions, which never
// replace a standard member.
func (p Problem) MarshalJSON() ([]byte, error) {
	type plain Problem
	base, err := json.Marshal(plain(p))
	if err != nil || len(p.Extensions) == 0 {
		return base, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(base, &m); err != nil {
		return nil, err
	}
	for k, v := range p.Extensions {
		if _, standard := m[k]; standard || reservedMember(k) {
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		m[k] = raw
	}
	return json.Marshal(m)
}

func reservedMember(name string) bool {
	switch name {
	case "type", "title", "status", "detail", "instance", "code", "requestId", "errors":
		return true
	}
	return false
}

// Problem converts err into its wire form. An error that is not an *Error
// becomes Internal with the generic detail; its cause is not part of the result.
func ProblemOf(err error, requestID, instance string) (Problem, *Error) {
	e, ok := As(err)
	if !ok {
		e = E(Internal, CodeInternal, "", WithCause(err))
	}
	title := http.StatusText(e.Status())
	if info, found := Lookup(e.Code); found {
		title = info.Title
	}
	detail := e.Detail
	if e.Kind == Internal {
		detail = "The server failed to process the request."
	}
	return Problem{
		Type:       ProblemTypePrefix + e.Code,
		Title:      title,
		Status:     e.Status(),
		Detail:     detail,
		Instance:   instance,
		Code:       e.Code,
		RequestID:  requestID,
		Errors:     e.Fields,
		Extensions: e.Extensions,
	}, e
}

// ProblemFor is [ProblemOf] for the answer to r: the instance is the request's path, and the reader the request's context names
// applies ([WithBastionAnswer], [WithBastionForward]). Every writer of an error answer to a request renders through it, a format's
// own error shape included (kit check 19).
func ProblemFor(r *http.Request, err error, requestID string) (Problem, *Error) {
	instance := ""
	var ctx context.Context
	if r != nil {
		ctx = r.Context()
		if r.URL != nil {
			instance = r.URL.Path
		}
	}
	p, e := ProblemOf(err, requestID, instance)
	switch readerOf(ctx) {
	case readerForward:
		p.RequestID = ""
		if d, ok := neutralDetail(e.Code); ok && e.Kind != Internal {
			p.Detail = d
		}
		p.Type, p.Title = AboutBlank, http.StatusText(p.Status)
	case readerBastion:
		p.Type, p.Title = AboutBlank, http.StatusText(p.Status)
	}
	return p, e
}

// retryAfterSeconds rounds a duration up to whole seconds, at least one.
func retryAfterSeconds(d time.Duration) string {
	s := int64((d + time.Second - 1) / time.Second)
	if s < 1 {
		s = 1
	}
	return strconv.FormatInt(s, 10)
}

// Write renders err as problem+json ([ProblemFor]). It sets WWW-Authenticate, Allow and
// Retry-After where the kind requires them, and logs a 5xx at error level with
// the cause and a 4xx at debug level (R-ARCH-023). instance is the request path.
func Write(w http.ResponseWriter, r *http.Request, err error, requestID string, log *slog.Logger) {
	p, e := ProblemFor(r, err, requestID)
	h := w.Header()
	h.Set("Content-Type", ProblemContentType)
	// A problem body echoes request data (the path in instance, field names);
	// nosniff keeps a browser from reading it as anything but JSON. The chain
	// sets it for every answer; a problem written outside the chain has it too.
	h.Set("X-Content-Type-Options", "nosniff")
	switch e.Kind {
	case Unauthenticated:
		if e.Challenge != "" {
			h.Set("WWW-Authenticate", e.Challenge)
		}
	case MethodNotAllowed:
		if len(e.Allow) > 0 {
			h.Set("Allow", strings.Join(e.Allow, ", "))
		}
	case RateLimited, Unavailable:
		h.Set("Retry-After", retryAfterSeconds(e.RetryAfter))
	default:
		// A conflict that tells the client when to try again, such as an
		// idempotent request still in progress (R-API-014).
		if e.RetryAfter > 0 {
			h.Set("Retry-After", retryAfterSeconds(e.RetryAfter))
		}
	}
	for k, v := range e.Headers {
		switch http.CanonicalHeaderKey(k) {
		case "Content-Type", "X-Content-Type-Options":
		default:
			h.Set(k, v)
		}
	}
	for _, c := range e.Cookies {
		http.SetCookie(w, c)
	}
	body, mErr := json.Marshal(p)
	if mErr != nil { // cannot happen: Problem holds only strings and ints
		body = []byte(`{"type":"` + ProblemTypePrefix + CodeInternal + `","title":"Internal error","status":500,"code":"internal"}`)
		p.Status = http.StatusInternalServerError
	}
	w.WriteHeader(p.Status)
	_, _ = w.Write(body)

	if log == nil {
		return
	}
	attrs := []any{"request_id", requestID, "status", p.Status, "code", p.Code, "kind", e.Kind.String()}
	if e.Op != "" {
		attrs = append(attrs, "op", e.Op)
	}
	// An internal error's own sentence is never answered (the client reads the generic one), so the log is the only place that holds
	// it: an errs.E(Internal, …) built without a cause says there what failed.
	if e.Kind == Internal && e.Detail != "" {
		attrs = append(attrs, "detail", e.Detail)
	}
	if e.Err != nil {
		attrs = append(attrs, "cause", e.Err.Error())
	}
	if p.Status >= 500 {
		log.Error("request failed", attrs...)
	} else {
		log.Debug("request rejected", attrs...)
	}
}
