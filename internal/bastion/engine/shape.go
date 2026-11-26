package engine

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// writeShaped renders one of the Bastion's own answers in an error shape of the format table (R-BAS-095, Q-BAS-337): the refusal
// of a write, the guard's limits, the answers of an unreachable or busy main installation and the rest of R-BAS-029's table. Every
// shape carries the status of the error, its code and its detail, and the headers its kind needs (the challenge of a 401, the
// methods of a 405, Retry-After of a 429 or 503); a shape the table does not name renders as problem details. It is the one
// renderer of the Bastion's own answers on a format's family.
func writeShaped(w http.ResponseWriter, r *http.Request, shape iface.ErrorShape, err *errs.Error) {
	switch shape {
	case iface.ErrorShapeDistribution:
		writeRegistryError(w, r, err)
		return
	case iface.ErrorShapeNPM, iface.ErrorShapeText, iface.ErrorShapeGalaxy:
	default:
		httpserver.Problem(w, r, err)
		return
	}
	p, e := errs.ProblemFor(r, err, "")
	h := w.Header()
	errorHeaders(h, e)
	text := p.Detail
	if text == "" {
		text = p.Title
	}
	status := p.Status
	var body []byte
	switch shape {
	case iface.ErrorShapeNPM:
		// npm prints the member error (measured 2026-11-26); the code serves curl and the logs.
		body, _ = json.Marshal(struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}{text, p.Code})
		h.Set("Content-Type", "application/json")
	case iface.ErrorShapeText:
		// dotnet prints the status alone; the line serves curl and the logs.
		body = []byte(oneLine(p.Code + ": " + text))
		h.Set("Content-Type", "text/plain; charset=utf-8")
	case iface.ErrorShapeGalaxy:
		// ansible-galaxy prints the detail and the code of the list's entries, never problem details (measured 2026-11-26). A
		// gateway failure answers 503: the client retries a 502 of the version list into the empty entry of its own response cache
		// and ends with exit 250 (H-2358, measured 2026-11-26), as Ansible's WriteError answers at the main installation.
		status = galaxyStatus(status)
		body, _ = json.Marshal(struct {
			Errors []galaxyError `json:"errors"`
		}{[]galaxyError{{Status: strconv.Itoa(status), Code: p.Code, Title: http.StatusText(status) + ".", Detail: text}}})
		h.Set("Content-Type", "application/json")
	}
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// galaxyStatus is the status of the galaxy shape: a gateway failure (502, 504) is 503, which ansible-galaxy prints and does not
// retry (R-BAS-095); every other status is the error's.
func galaxyStatus(status int) int {
	if status == http.StatusBadGateway || status == http.StatusGatewayTimeout {
		return http.StatusServiceUnavailable
	}
	return status
}

// galaxyError is one entry of the error list of a Galaxy's API (the shape galaxy).
type galaxyError struct {
	Status string `json:"status"`
	Code   string `json:"code"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// errorHeaders sets the headers the kind of an error needs and the ones it carries, as errs.Write does for problem details: the
// challenge of a 401, the methods of a 405, Retry-After of a 429 or 503 and every header of errs.WithHeader (the RateLimit
// fields of a limit's refusal).
func errorHeaders(h http.Header, e *errs.Error) {
	switch e.Kind {
	case errs.Unauthenticated:
		if e.Challenge != "" {
			h.Set("WWW-Authenticate", e.Challenge)
		}
	case errs.MethodNotAllowed:
		if len(e.Allow) > 0 {
			h.Set("Allow", strings.Join(e.Allow, ", "))
		}
	case errs.RateLimited, errs.Unavailable:
		h.Set("Retry-After", retrySeconds(e.RetryAfter))
	}
	for k, v := range e.Headers {
		switch http.CanonicalHeaderKey(k) {
		case "Content-Type", "X-Content-Type-Options":
		default:
			h.Set(k, v)
		}
	}
}

// oneLine keeps the text shape's body to one line.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, s)
}

// writeRegistryError renders one of the Bastion's own answers below the registry root in the distribution's shape
// (R-BAS-029, R-BAS-051, Q-BAS-169): the status of the error, the API version header, the challenge, Retry-After and the
// other headers the error carries, and {"errors":[{"code":…,"message":…,"detail":{"code":"<Quader code>"}}]}.
func writeRegistryError(w http.ResponseWriter, r *http.Request, err *errs.Error) {
	p, e := errs.ProblemFor(r, err, "")
	h := w.Header()
	errorHeaders(h, e)
	text := p.Detail
	if text == "" {
		text = p.Title
	}
	body, _ := json.Marshal(struct {
		Errors []registryErrorBody `json:"errors"`
	}{[]registryErrorBody{{Code: registryCode(e), Message: text, Detail: quaderProblem{Code: p.Code}}}})
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set(distributionVersionHeader, distributionVersion)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(e.Status())
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// retrySeconds renders a Retry-After value in whole seconds, at least 1.
func retrySeconds(d time.Duration) string {
	s := int64((d + time.Second - 1) / time.Second)
	return strconv.FormatInt(max(s, 1), 10)
}
