// Package httplimit is the HTTP side of the download limits (usage-and-limits.md): the caller of a request as the limits
// read it, the protocol paths, the counting and pacing writer around a response, and the fields of the request log. It is an
// edge package: it reads the principal of internal/httpserver, which the domain package internal/limits cannot import.
package httplimit

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/observability"
	"github.com/guided-traffic/quader/internal/qname"
)

// ProtocolPath reports whether a path is a protocol path (R-LIM-005): every path below /api/ that is not /api/v1/, /files/
// and everything below /v2/ (the version check /v2/ and /v2 themselves included).
func ProtocolPath(p string) bool {
	switch {
	case p == "/v2" || strings.HasPrefix(p, "/v2/"), strings.HasPrefix(p, "/files/"):
		return true
	case p == "/api/v1" || strings.HasPrefix(p, "/api/v1/"):
		return false
	}
	return strings.HasPrefix(p, "/api/")
}

// TokenPath is the registry's token service, which authenticates its own credential (R-LIM-003).
const TokenPath = "/v2/_token" //nolint:gosec // G101: a path, not a credential

// Repo is the repository a protocol request names (R-LIM-005); zero for a path that names none (/v2/, /v2/_catalog,
// /v2/_token, a key that does not parse).
func Repo(r *http.Request) qname.Repo {
	a, ok := qname.ParseProtocolAddress(r.URL.EscapedPath())
	if !ok {
		return qname.Repo{}
	}
	return a.Repo
}

// CallerOf converts the principal of a request to the caller the limits derive the subject from (R-LIM-001). admin is
// whether the principal holds the role admin now, whatever the token's scope (Q-LIM-18).
func CallerOf(r *http.Request, p httpserver.Principal, admin bool) limits.Caller {
	c := limits.Caller{
		Kind: limits.CallerKind(p.Kind), Anonymous: p.Anonymous, PrincipalID: p.ID, SessionID: p.Credential.SessionID,
		TokenID: p.Credential.TokenID, TechnicalTokenID: p.Credential.TechnicalTokenID, Admin: admin,
		Client: httpserver.InfoFrom(r.Context()).Client,
	}
	if c.Kind == "" && !p.Anonymous {
		c.Kind = limits.CallerUser
	}
	if p.Anonymous {
		c.Kind = limits.CallerAnonymous
	}
	if p.CI != nil {
		c.CIIssuerID, c.CIProjectID = p.CI.IssuerID, p.CI.ProjectID
	}
	if len(p.Groups) > 0 {
		c.Groups = make([]string, len(p.Groups))
		for i, g := range p.Groups {
			c.Groups[i] = g.ID
		}
	}
	return c
}

// Writer counts the bytes of a response body as cached bytes of the request's charge (R-LIM-006) and paces the body of an
// admitted download at the budget key's cachedBytesPerSecond (R-LIM-023). The writes of the leader's stream of a remote fill
// are counted where they were read and pass uncounted and unpaced. HEAD and 304 write no body and count nothing.
type Writer struct {
	http.ResponseWriter
	ctx    context.Context
	e      *limits.Engine
	c      *limits.Charge
	pace   *limits.Pace
	paced  bool
	status int
}

// Wrap puts the writer around w.
func Wrap(ctx context.Context, w http.ResponseWriter, e *limits.Engine, c *limits.Charge) *Writer {
	return &Writer{ResponseWriter: w, ctx: ctx, e: e, c: c}
}

// WriteHeader records the status for the request log.
func (w *Writer) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write counts and paces.
func (w *Writer) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.c.Streaming() {
		return w.ResponseWriter.Write(p)
	}
	if w.c.Download() && !w.paced {
		w.paced = true
		w.pace = w.e.Pacer(w.c, false)
	}
	if w.pace == nil {
		n, err := w.ResponseWriter.Write(p)
		w.c.AddCached(n)
		return n, err
	}
	written := 0
	for len(p) > 0 {
		chunk := p
		if len(chunk) > limits.PaceChunk {
			chunk = chunk[:limits.PaceChunk]
		}
		if err := w.pace.Wait(w.ctx, len(chunk)); err != nil {
			return written, err
		}
		n, err := w.ResponseWriter.Write(chunk)
		w.c.AddCached(n)
		written += n
		if err != nil {
			return written, err
		}
		p = p[len(chunk):]
	}
	return written, nil
}

// Flush passes a flush on.
func (w *Writer) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap gives http.ResponseController the writer below.
func (w *Writer) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Close ends the pace of the body.
func (w *Writer) Close() {
	if w.pace != nil {
		w.pace.Close()
		w.pace = nil
	}
}

// Log writes the fields of R-LIM-030 into the request log: limits_subject, limits_place, limits_rule, limits_outcome,
// bytes_cached, bytes_uncached, and for a refusal of the limits the limiter and the mode.
func Log(ctx context.Context, c *limits.Charge) {
	ri := observability.RequestInfoFrom(ctx)
	if ri == nil || c == nil {
		return
	}
	ri.SetField("limits_subject", c.Subject.String())
	ri.SetField("limits_place", c.Resolution.Place.LogName())
	ri.SetField("limits_rule", c.Resolution.Rule.LogID())
	ri.SetField("limits_outcome", c.Outcome())
	cached, uncached := c.Totals()
	ri.SetField("bytes_cached", strconv.FormatInt(cached, 10))
	ri.SetField("bytes_uncached", strconv.FormatInt(uncached, 10))
	if lim := c.Limiter(); lim != "" {
		ri.SetLimit(lim, string(c.Mode))
	}
}

// SetPolicies sets the RateLimit fields of the request figures (R-LIM-020): added, not set, beside the endpoint's class and
// the class of R-API-016.
func SetPolicies(h http.Header, ps []limits.Policy) {
	for _, p := range ps {
		w := int64(p.Window / time.Second)
		t := int64((p.Reset + time.Second - 1) / time.Second)
		h.Add("RateLimit-Policy", `"`+p.Name+`";q=`+strconv.FormatInt(p.Quota, 10)+";w="+strconv.FormatInt(w, 10))
		h.Add("RateLimit", `"`+p.Name+`";r=`+strconv.FormatInt(p.Remaining, 10)+";t="+strconv.FormatInt(t, 10))
	}
}
