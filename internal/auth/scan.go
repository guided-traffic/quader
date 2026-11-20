package auth

import (
	"net/http"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/errs"
)

// byScanToken is the stage of the scan listener of packages (R-ARCH-037, R-EP-010): it accepts exactly one credential, a scan
// token below /v2/ (R-OCI-037), and refuses every other one - a session cookie, a quader token of any type, a client's registry
// token, the token service's credential - and a request without one, with 401 and no challenge: the scanner is its one client and
// asks for no token. A scan token is verified by its MAC with no query, so a refusal costs one HMAC; none counts towards the
// failed-credential throttle (R-AUTH-038), and a refused one still counts in quader_auth_attempts_total.
func (s *Stage) byScanToken(r *http.Request, ep EndpointRules) (Result, error) {
	secret, method, _, present := credentialHeader(r, &routeAnswer{})
	if !present {
		ep.refused(r, Refusal{Reason: RefusedAnonymous})
		return s.fail(MethodNone, "no_credential", errs.E(errs.Unauthenticated, errs.CodeCredentialsRequired, "The scan listener accepts a scan token only."))
	}
	reject := func(reason string) (Result, error) {
		return s.fail(method, reason, errs.E(errs.Unauthenticated, errs.CodeInvalidToken, "The token is not valid."))
	}
	p, raw := r.URL.Path, r.URL.EscapedPath()
	if method != MethodBearer || !registrytoken.Is(secret) || !isRegistryPath(p) || !isRegistryPath(raw) || raw == registrytoken.TokenPath {
		// A quader token is refused by its type before any lookup, as on every endpoint that does not accept it.
		typ, _ := token.Check(secret)
		ep.refused(r, Refusal{Reason: RefusedTokenType, TokenType: typ})
		return reject("type_not_accepted")
	}
	if s.o.Registry == nil {
		return reject("malformed")
	}
	c, err := s.o.Registry.Verify(secret, ep.Name, s.o.Now())
	if err != nil {
		return reject(registryRejection(err))
	}
	if c.System != registrytoken.ScanSubject {
		// A client's registry token is issued on a client endpoint, so its endpoint claim refuses it above; this is defence only.
		return reject("type_not_accepted")
	}
	grant, expires := c.Grant, c.ExpiresAt
	grant.Digest = c.Digest
	s.o.Metrics.Attempt(string(MethodBearer), "success", "")
	return Result{
		Principal:  principal.Principal{Kind: principal.KindSystem, Name: registrytoken.ScanSubject, Status: principal.StatusActive},
		Credential: Credential{Method: MethodBearer, ExpiresAt: &expires, Registry: &grant, ScanID: c.ScanID},
	}, nil
}
