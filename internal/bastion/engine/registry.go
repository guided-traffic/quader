package engine

import (
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/errs"
)

// maxTokenForm bounds the form of POST /v2/_token the Bastion reads, as the token service does (R-BAS-051).
const maxTokenForm = 16 << 10

// stepTokenForm reads the form of POST /v2/_token before the limits (R-BAS-051): its password is the one credential
// the token service reads, so it decides the request's slot class and limits and is checked for its syntax before
// anything is sent. The Bastion keeps nothing of the form beyond the request and logs none of it. Every other request
// passes unchanged.
func (e *engine) stepTokenForm(q *rq) bool {
	if q.core != coreToken || q.r.Method != http.MethodPost {
		return true
	}
	limit := min(int64(maxTokenForm), int64(e.cfg.Main.MaxForwardedBody))
	if q.r.ContentLength > limit {
		e.refuse(q, errs.E(errs.TooLarge, errs.CodeTooLarge, "The token request form is larger than a token service reads."), metrics.RefusedReadOnly)
		return false
	}
	body, err := io.ReadAll(io.LimitReader(q.r.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		e.refuse(q, errs.E(errs.TooLarge, errs.CodeTooLarge, "The token request form is larger than a token service reads."), metrics.RefusedReadOnly)
		return false
	}
	q.form = body
	form, _ := url.ParseQuery(string(body)) // a form the token service cannot read is its 400 to give
	pw := form.Get("password")
	if pw == "" {
		return true
	}
	// A form with a password takes an authenticated slot and the authenticated limits; a malformed token in it is
	// refused before anything is sent (stepCredentials, R-BAS-036).
	q.hasAuth = true
	q.cred, q.credOK = parseToken(pw, "form")
	return true
}

// versionChallenge answers the version check without a credential itself (R-BAS-051): 401 with the registry's
// challenge on the Bastion's base URL and the API version header, because the main installation answers every version
// check without a credential so, whatever is public (Q-OCI-20). Nothing is sent.
func (e *engine) versionChallenge(q *rq) {
	e.refuse(q, errs.E(errs.Unauthenticated, errs.CodeCredentialsRequired, "The registry needs a token; ask the token service of the challenge.",
		errs.WithChallenge(e.registryChallenge(false))), "")
}

// passCore forwards a core route of the registry root with the caller's credential (R-BAS-051): the version check with
// a credential, GET /v2/_token and the catalog. The answer is passed on as the main installation gave it; it is never
// kept, never collapsed with another caller's, and the token service's body is never logged.
func (e *engine) passCore(q *rq) {
	q.passOnly = true
	e.forward(q, mainclient.KindForward, "", nil)
}

// onPassOnly passes the answer of a core route on and keeps nothing of it, whatever its marking says (R-BAS-051).
func (e *engine) onPassOnly(q *rq, resp *mainclient.Response) {
	e.passThrough(q, resp, nil, metrics.ClassPass, metrics.ResultMiss)
}

// registryTokenExpired reports whether the main installation reported an expiry for the registry token of the key and
// it has passed (R-BAS-052).
func (e *engine) registryTokenExpired(key string) bool {
	exp, ok := e.expiries.get(key)
	return ok && !e.now().Before(exp)
}

// noteRegistryToken keeps the expiry the main installation reported for a registry token (R-BAS-052): the token's own,
// which the subject of the marking and the check carries.
func (e *engine) noteRegistryToken(c credential, sub iface.Subject) {
	if c.registry && !c.anonymous && sub.Kind == iface.SubjectToken && sub.ExpiresAt != nil {
		e.expiries.put(c.key, *sub.ExpiresAt, 0)
	}
}

// capRead caps the read shape of a registry token at partial (R-BAS-052, Q-BAS-167): a grant names image names and never a
// whole repository, and the decision point that computes a shape does not see the grant, so a full answer would let a token
// granted one image read every cached image of the repository. The main installation caps it too; the Bastion does not
// rely on it.
func capRead(c credential, read iface.Read) iface.Read {
	if c.registry && !c.anonymous && read == iface.ReadFull {
		return iface.ReadPartial
	}
	return read
}

// insufficientScope reports whether a 401 is the registry's insufficient_scope (R-BAS-052): no statement about the
// credential, which the client answers with a token request for the scope the challenge names.
func insufficientScope(status int, h http.Header) bool {
	if status != http.StatusUnauthorized {
		return false
	}
	for _, v := range h.Values("Www-Authenticate") {
		if strings.Contains(v, `error="insufficient_scope"`) {
			return true
		}
	}
	return false
}
