package engine

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/errs"
)

// maxTokenForm bounds the form of POST /v2/_token the Bastion reads, as the token service does (R-BAS-051).
const maxTokenForm = 16 << 10

// readTokenForm reads the form of POST /v2/_token before the limits (R-BAS-051): its password is the one credential
// the token service reads, so it decides the request's slot class and limits and is checked for its syntax before
// anything is sent. A form that is too large is refused by stepRefusal, after the limit has counted the request
// (Q-BAS-525). The Bastion keeps nothing of the form beyond the request and logs none of it. Every other request passes
// unchanged.
func (e *engine) readTokenForm(q *rq) {
	if q.refusal != nil || q.core != coreToken || q.r.Method != http.MethodPost {
		return
	}
	limit := min(int64(maxTokenForm), int64(e.cfg.Main.MaxForwardedBody))
	if q.r.ContentLength > limit {
		q.refusal, q.refusalReason = errTokenFormTooLarge(), metrics.RefusedReadOnly
		return
	}
	body, err := io.ReadAll(io.LimitReader(q.r.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		q.refusal, q.refusalReason = errTokenFormTooLarge(), metrics.RefusedReadOnly
		return
	}
	q.form = body
	form, _ := url.ParseQuery(string(body)) // a form the token service cannot read is its 400 to give
	pw := form.Get("password")
	if pw == "" {
		return
	}
	// A form with a password takes an authenticated slot and the authenticated limits; a malformed token in it is
	// refused before anything is sent (stepCredentials, R-BAS-036).
	q.hasAuth = true
	q.cred, q.credOK = parseToken(pw, "form")
}

func errTokenFormTooLarge() *errs.Error {
	return errs.E(errs.TooLarge, errs.CodeTooLarge, "The body is too large.")
}

// versionChallenge answers the version check without a credential itself (R-BAS-051): 401 with the registry's
// challenge on the Bastion's base URL and the API version header, because the main installation answers every version
// check without a credential so, whatever is public (Q-OCI-20). Nothing is sent.
func (e *engine) versionChallenge(q *rq) {
	e.refuse(q, errs.E(errs.Unauthenticated, errs.CodeCredentialsRequired, "The registry needs a token; ask the token service of the challenge.",
		errs.WithChallenge(e.registryChallenge(false))), "")
}

// The Bastion's anonymous registry token (R-BAS-094, Q-BAS-335): payload version 1 with the key ID "bastion" and only the claims
// "a" and "exp", which iface.RegistryTokenAnonymous and iface.RegistryTokenExpiry read, and a MAC of random bytes that nobody
// verifies. Wherever it is presented a Bastion drops it and serves the anonymous caller (Q-BAS-168), so it is worth exactly a
// request without a credential, at every replica and every Bastion alike (Q-BAS-435); the main installation has no key of this
// ID and refuses it.
const (
	anonymousTokenKeyID    = "bastion"
	anonymousTokenLifetime = 300 * time.Second // the main installation's default oci.token_lifetime
	anonymousTokenMACBytes = 32                // the length of an HMAC-SHA-256, so the token has the form of the main installation's
)

// anonymousToken is the token of an anonymous token request, expiring at exp.
func anonymousToken(exp time.Time) string {
	payload := "1:" + anonymousTokenKeyID + `:{"a":true,"exp":` + strconv.FormatInt(exp.Unix(), 10) + "}"
	mac := make([]byte, anonymousTokenMACBytes)
	_, _ = rand.Read(mac) // crypto/rand.Read never returns an error
	return iface.RegistryTokenPrefix + base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac)
}

// tokenAnswer is the body of the token service's 200 (formats/oci.md R-OCI-004 step 4): token and access_token carry the same
// value, so a client of the Docker form and one of the OAuth 2.0 form both find it.
type tokenAnswer struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	IssuedAt    string `json:"issued_at"`
}

// anonymousTokenRequest reports whether a request to the token service is anonymous (Q-BAS-436): a GET without a credential, or a
// POST whose form holds no password and that carries no Authorization header. An anonymous registry token presented with it is
// no credential (Q-BAS-168). Neither scope nor service is read: the token grants nothing.
func anonymousTokenRequest(q *rq) bool {
	return q.core == coreToken && !q.hasAuth && (q.r.Method == http.MethodGet || q.r.Method == http.MethodPost)
}

// answerAnonymousToken answers an anonymous token request itself, without a request to the main installation (R-BAS-094,
// Q-BAS-335), once stepCredentials has admitted the anonymous caller: while the handshake says anonymousReads, as for every
// anonymous request (R-BAS-019). The answer is never kept and names nothing of the main installation (R-BAS-099).
func (e *engine) answerAnonymousToken(q *rq) {
	iat := e.now().Truncate(time.Second)
	exp := iat.Add(anonymousTokenLifetime)
	tok := anonymousToken(exp)
	body, err := json.Marshal(tokenAnswer{ //nolint:gosec // G117: the answer of a token service is the token (R-OCI-004 step 4)
		Token: tok, AccessToken: tok, ExpiresIn: int64(anonymousTokenLifetime / time.Second), IssuedAt: iat.UTC().Format(time.RFC3339),
	})
	if err != nil {
		e.fail(q, err)
		return
	}
	q.finish(metrics.ClassPass, metrics.ResultHit)
	h := q.w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	// A token answer is never stored by a cache on the way (RFC 6749 section 5.1).
	h.Set("Cache-Control", "no-store")
	h.Set("Pragma", "no-cache")
	h.Set(distributionVersionHeader, distributionVersion)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	q.w.WriteHeader(http.StatusOK)
	_, _ = q.w.Write(body)
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

// registryTokenExpired reports whether the expiry a registry token's claims name, read without verifying them, has passed
// (R-BAS-052, Q-BAS-252): every replica knows it without having seen the token, and trusting it can only refuse, because a
// token whose claims were changed fails the main installation's MAC anyway. It is the expiry the main installation reports
// for the token (the authentication stage takes the credential's expiry from the same claims).
func (e *engine) registryTokenExpired(c credential) bool {
	exp, ok := iface.RegistryTokenExpiry(c.token)
	return ok && !e.now().Before(exp)
}

// capPackage limits a package answer of a registry token to its grant (R-BAS-084, Q-BAS-246): full only when the token's
// grant, read without verifying it, names the repository and the package with pull; partial otherwise, whatever the main
// installation marked, so a defect there cannot open another image to the token. The comparison is made when the answer
// is recorded, and nothing of the grant is kept. Every other read than full is partial: a package answer has two values
// (Q-BAS-243).
func capPackage(c credential, repo, pkg string, read iface.Read) iface.Read {
	if read != iface.ReadFull {
		return iface.ReadPartial
	}
	if c.registry && !c.anonymous {
		if g, ok := iface.RegistryTokenGrant(c.token); !ok || !g.Pulls(repo, pkg) {
			return iface.ReadPartial
		}
	}
	return iface.ReadFull
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
