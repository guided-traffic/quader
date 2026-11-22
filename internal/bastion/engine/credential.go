package engine

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
)

// anonymousKey is the credential key of every anonymous caller (R-BAS-019): the main installation's decision
// does not depend on the client address, so the answers are shared.
const anonymousKey = "anonymous"

// credential is what a request presents (R-BAS-019). The token string is held for the duration of the request
// only; every answer is keyed by [credential.key], the SHA-256 of the token string.
type credential struct {
	anonymous bool
	token     string
	key       string
	// hint is the public prefix of the token (prefix and four characters), for logs.
	hint string
	// method is "bearer" or "basic", for the request log.
	method string
	// registry is a registry token below the registry root (R-BAS-052): checked by its form only, checked by the main
	// installation on every use, its read shape at most partial. An anonymous registry token is no credential: it is
	// the anonymous caller (Q-BAS-168), and dropped is true.
	registry bool
	dropped  bool
}

// hintLength is the length of the public prefix of a token in logs: the prefix and four characters.
const hintLength = 12

// credentialKey is the SHA-256 of the token string in hex.
func credentialKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// authorization reads the Authorization header the way the main installation does (internal/auth stage.go):
// Bearer, Basic with the token as the password, or the scheme Token of the Galaxy clients (method "token", which
// parseCredential admits on the protocol family only). present is true when the header carries any value, so a
// header that cannot be read is a refused credential and never a fall-through to anonymous. Cookies and
// X-NuGet-ApiKey are never read (R-BAS-019).
func authorization(h http.Header) (token, method string, present bool) {
	v := strings.TrimSpace(h.Get("Authorization"))
	if v == "" {
		return "", "", false
	}
	scheme, rest, _ := strings.Cut(v, " ")
	rest = strings.TrimSpace(rest)
	switch {
	case strings.EqualFold(scheme, "Bearer"):
		return rest, "bearer", true
	case strings.EqualFold(scheme, "Token"):
		return rest, "token", true
	case strings.EqualFold(scheme, "Basic"):
		dec, err := base64.StdEncoding.DecodeString(rest)
		if err != nil {
			return "", "basic", true
		}
		_, pw, ok := strings.Cut(string(dec), ":")
		if !ok {
			return "", "basic", true
		}
		return pw, "basic", true
	}
	return "", "", true
}

// parseCredential classifies the credential of a request. ok is false for a credential the Bastion refuses
// itself: an unreadable header, a token that fails the syntax or the checksum, a token of the type bst or of an
// unknown prefix (R-BAS-019). Such a request is never sent to the main installation. Below the registry root a
// registry token as Bearer is a credential of its own (R-BAS-052): it has no checksum, so its form is checked
// (iface.RegistryTokenWellFormed), and one whose unverified claims name the anonymous principal is the anonymous
// caller (iface.RegistryTokenAnonymous, Q-BAS-168). On every other family its prefix is unknown. The scheme Token,
// which the main installation reads as a Quader token on the routes that declare it (every route of Ansible Galaxy,
// R-ANS-006), is read on /api/<format>/… only, checked like any token and forwarded unchanged; the main installation
// decides whether the scheme is a credential on the route. Its answers are keyed apart from the same token's under
// another scheme, so a refusal of the scheme on one route is never the token's answer on another (Q-BAS-203).
func parseCredential(h http.Header, fam family) (c credential, ok bool) {
	token, method, present := authorization(h)
	if !present {
		return credential{anonymous: true, key: anonymousKey}, true
	}
	if method == "token" {
		if fam != familyAPI {
			return credential{method: method}, false
		}
		c, ok := parseToken(token, method)
		if ok {
			c.key = credentialKey("Token " + token)
		}
		return c, ok
	}
	if fam == familyRegistry && method == "bearer" && strings.HasPrefix(token, iface.RegistryTokenPrefix) {
		switch {
		case !iface.RegistryTokenWellFormed(token):
			return credential{method: method, registry: true}, false
		case iface.RegistryTokenAnonymous(token):
			return credential{anonymous: true, key: anonymousKey, registry: true, dropped: true}, true
		}
		return credential{token: token, key: credentialKey(token), hint: token[:hintLength], method: method, registry: true}, true
	}
	return parseToken(token, method)
}

// parseToken classifies a Quader token: the syntax and the checksum, never a Bastion token (R-BAS-019).
func parseToken(token, method string) (credential, bool) {
	typ, valid := mainclient.TokenType(token)
	if !valid || typ == "bst" {
		return credential{method: method}, false
	}
	return credential{token: token, key: credentialKey(token), hint: token[:hintLength], method: method}, true
}

// challenge is the WWW-Authenticate value of a 401 for the path family (R-AUTH-002): below the registry root the
// registry's challenge with the token service's realm, error="invalid_token" for a credential the Bastion refused
// (R-OCI-004, R-BAS-051); on the token service and every other family Basic for the package clients, Bearer for a
// browser, which Fetch Metadata headers reveal (the choice of internal/auth, Q-AUTH-173).
func (e *engine) challenge(q *rq, rejected bool) string {
	if q.fam == familyRegistry && q.core != coreToken {
		return e.registryChallenge(rejected)
	}
	if q.r.Header.Get("Sec-Fetch-Mode") != "" || q.r.Header.Get("Sec-Fetch-Dest") != "" {
		return `Bearer realm="quader"`
	}
	return `Basic realm="quader"`
}
