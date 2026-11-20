package auth

import (
	"net/http"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/errs"
)

// scanRoute is the kind of route a request of the scan listener addresses, read from its escaped and its unescaped path alike, so that
// an escape never makes a route of another kind: a path whose two readings differ is no route.
type scanRoute int

const (
	scanRouteNone scanRoute = iota
	// scanRouteRegistry is below /v2/, the token service excepted: a scan token of an image (R-OCI-037).
	scanRouteRegistry
	// scanRouteBlob is the blob route of the malware scanning: a blob token (R-MAL-006).
	scanRouteBlob
	// scanRouteDatabase is the database routes of the malware scanning: the malware scanner's certificate (Q-MAL-27).
	scanRouteDatabase
)

func scanRouteOf(p, raw string) scanRoute {
	switch {
	case isRegistryPath(p) && isRegistryPath(raw) && raw != registrytoken.TokenPath:
		return scanRouteRegistry
	case p != raw:
		return scanRouteNone
	case registrytoken.IsDatabasePath(raw):
		return scanRouteDatabase
	}
	if _, ok := registrytoken.BlobOfPath(raw); ok {
		return scanRouteBlob
	}
	return scanRouteNone
}

// byScanToken is the stage of the scan listener of packages (R-ARCH-037, R-EP-010). It accepts one credential per kind of route and
// refuses every other one - a session cookie, a quader token of any type, a client's registry token, the token service's credential, a
// token of the other kind of scan - and a request without one, with 401 and no challenge: the scanners are its only clients and ask for
// no token. Below /v2/ the credential is a scan token of an image (R-OCI-037); on the blob route a blob token for the digest of the path
// (R-MAL-006); on the database routes no token at all, but the client certificate of the malware scanner (Q-MAL-27). A token is
// verified by its MAC with no query, so a refusal costs one HMAC; none counts towards the failed-credential throttle (R-AUTH-038), and
// a refused one still counts in quader_auth_attempts_total.
func (s *Stage) byScanToken(r *http.Request, ep EndpointRules) (Result, error) {
	route := scanRouteOf(r.URL.Path, r.URL.EscapedPath())
	secret, method, _, present := credentialHeader(r, &routeAnswer{})
	if route == scanRouteDatabase {
		return s.byMalwareScannerCertificate(r, ep, method, present)
	}
	if !present {
		ep.refused(r, Refusal{Reason: RefusedAnonymous})
		return s.fail(MethodNone, "no_credential", errs.E(errs.Unauthenticated, errs.CodeCredentialsRequired, "The scan listener accepts a scan token only."))
	}
	reject := func(reason string) (Result, error) {
		return s.fail(method, reason, errs.E(errs.Unauthenticated, errs.CodeInvalidToken, "The token is not valid."))
	}
	if method != MethodBearer || !registrytoken.Is(secret) || route == scanRouteNone {
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
	expires := c.ExpiresAt
	switch route {
	case scanRouteRegistry:
		if c.System != registrytoken.ScanSubject {
			// A client's registry token is issued on a client endpoint, so its endpoint claim refuses it above; a blob token reaches
			// no route of the registry.
			return reject("type_not_accepted")
		}
		grant := c.Grant
		grant.Digest = c.Digest
		s.o.Metrics.Attempt(string(MethodBearer), "success", "")
		return Result{
			Principal:  principal.Principal{Kind: principal.KindSystem, Name: registrytoken.ScanSubject, Status: principal.StatusActive},
			Credential: Credential{Method: MethodBearer, ExpiresAt: &expires, Registry: &grant, ScanID: c.ScanID},
		}, nil
	default: // scanRouteBlob
		if !c.IsBlob() {
			return reject("type_not_accepted") // an image's scan token reads no blob by its digest alone
		}
		if d, _ := registrytoken.BlobOfPath(r.URL.EscapedPath()); d != c.Digest {
			return reject("other_digest")
		}
		if ep.MalwareScanner != nil && !ep.MalwareScanner(r) {
			return reject("certificate_not_admitted")
		}
		s.o.Metrics.Attempt(string(MethodBearer), "success", "")
		return Result{
			Principal: principal.Principal{Kind: principal.KindSystem, Name: registrytoken.BlobSubject, Status: principal.StatusActive},
			Credential: Credential{
				Method: MethodBearer, ExpiresAt: &expires, ScanID: c.ScanID,
				Blob: &registrytoken.BlobRef{Repo: c.Repo, Digest: c.Digest},
			},
		}, nil
	}
}

// byMalwareScannerCertificate is the credential of the database routes (Q-MAL-27): the client certificate the listener's handshake
// verified, whose name must be the malware scanner's, and no token. The database is the public signature set, and a scanner needs it
// before its first call, when it holds no token yet. A request that presents any credential is refused: no token has a meaning here.
// Without TLS there is no certificate, and the NetworkPolicy is what remains (H-1108).
func (s *Stage) byMalwareScannerCertificate(r *http.Request, ep EndpointRules, method Method, present bool) (Result, error) {
	if present {
		ep.refused(r, Refusal{Reason: RefusedTokenType})
		return s.fail(method, "type_not_accepted", errs.E(errs.Unauthenticated, errs.CodeInvalidToken, "The signature database is read with the malware scanner's certificate and no token."))
	}
	if ep.MalwareScanner != nil && !ep.MalwareScanner(r) {
		ep.refused(r, Refusal{Reason: RefusedAnonymous})
		return s.fail(MethodClientCertificate, "certificate_not_admitted",
			errs.E(errs.Unauthenticated, errs.CodeCredentialsRequired, "The signature database is read by the malware scanner only."))
	}
	s.o.Metrics.Attempt(string(MethodClientCertificate), "success", "")
	return Result{
		Principal:  principal.Principal{Kind: principal.KindSystem, Name: registrytoken.BlobSubject, Status: principal.StatusActive},
		Credential: Credential{Method: MethodClientCertificate},
	}, nil
}
