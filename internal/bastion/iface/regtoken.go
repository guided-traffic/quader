package iface

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// The registry token below the registry root /v2/ (R-BAS-052, formats/oci.md R-OCI-005). A Bastion holds no key ring, so it
// checks a registry token by its form only and hands it to the main installation, which verifies the MAC, the expiry, the
// endpoint and the token behind it (Q-BAS-167). Its form is
//
//	qdr_rgt_ + base64url(payload) + "." + base64url(MAC)    payload = "1:<key id>:<claims as JSON>"
//
// at most [MaxRegistryTokenBytes], base64url as RFC 4648 section 5 without padding.
const (
	// RegistryTokenPrefix starts every registry token. Below /v2/ a Bastion accepts it as Authorization: Bearer; on every
	// other family it is an unknown prefix.
	RegistryTokenPrefix = "qdr_rgt_" //nolint:gosec // G101: the prefix of a token type, not a credential
	// MaxRegistryTokenBytes bounds a registry token; a longer value is refused before anything is decoded.
	MaxRegistryTokenBytes = 4096
)

// RegistryTokenWellFormed reports whether s has the form of a registry token (R-BAS-052): the prefix, a base64url payload, one
// ".", a base64url MAC, at most [MaxRegistryTokenBytes]. Both parts decode strictly, as the main installation decodes them, so a
// value the main installation would refuse as malformed for its encoding is refused here without a request (Q-BAS-185). It
// says nothing about the MAC, the expiry or whom the token stands for.
func RegistryTokenWellFormed(s string) bool {
	_, ok := registryTokenPayload(s)
	return ok
}

// RegistryTokenAnonymous reads the claims of a well-formed registry token without verifying them and reports whether they name
// the anonymous principal (R-BAS-052, Q-BAS-168): payload version 1 whose claims hold the member "a" true, which the main
// installation's token service writes into the token it issues to a request without a credential. The Bastion then drops the
// token and serves the request as the anonymous caller. Trusting the unverified claim can only lower what is read, because a
// client can always send no credential. A payload that does not decode, another version and claims that are no JSON object
// read as not anonymous: the token is then a credential like any other.
func RegistryTokenAnonymous(s string) bool {
	var c struct {
		Anonymous bool `json:"a"`
	}
	return registryTokenClaims(s, &c) && c.Anonymous
}

// RegistryGrantPull is the bit of pull in the actions of a registry grant entry: the distribution token protocol's pull, push
// and delete are the bits 1, 2 and 4 of the member "a" (formats/oci.md R-OCI-006).
const RegistryGrantPull = 1

// RegistryGrantEntry is one entry of the grant a registry token's claims carry, as the main installation's token service writes
// it into the claim "g" (R-BAS-052): a repository, an image name and the actions on it.
type RegistryGrantEntry struct {
	// Repository is the qualified name "<projectKey>/<repoKey>".
	Repository string `json:"r"`
	// Name is the image name: the package of the marking and of the check for the registry root (R-BAS-081).
	Name string `json:"n"`
	// Actions is a set of bits; [RegistryGrantPull] is pull.
	Actions int `json:"a"`
}

// RegistryGrant is the grant of a registry token read without verifying it ([RegistryTokenGrant]).
type RegistryGrant []RegistryGrantEntry

// Pulls reports whether the grant allows pull on the package of the repository: an entry names both, compared byte for byte
// as opaque strings, with the bit [RegistryGrantPull]. The Bastion records a package answer full for a registry token only
// when it does (R-BAS-084, Q-BAS-246). An empty package is never pulled.
func (g RegistryGrant) Pulls(repository, pkg string) bool {
	if pkg == "" {
		return false
	}
	for _, e := range g {
		if e.Repository == repository && e.Name == pkg && e.Actions&RegistryGrantPull != 0 {
			return true
		}
	}
	return false
}

// RegistryTokenGrant reads the grant of a well-formed registry token without verifying it (R-BAS-052, Q-BAS-246): the entries
// of the claim "g" of payload version 1. The Bastion compares a package answer the main installation marks full with it, so a
// defect at the main installation cannot open another image to the token; trusting the unverified grant can only lower what is
// read. ok is false, and the grant empty, for a value outside the form, another payload version, claims that do not decode, and
// claims that name a system subject (the member "s" of a scan or blob token, which reaches one manifest or blob, never a whole
// image). A member the interface does not know is ignored.
func RegistryTokenGrant(s string) (RegistryGrant, bool) {
	var c struct {
		Grant  RegistryGrant `json:"g"`
		System string        `json:"s"`
	}
	if !registryTokenClaims(s, &c) || c.System != "" {
		return nil, false
	}
	return c.Grant, true
}

// RegistryTokenExpiry reads the expiry of a well-formed registry token without verifying it (Q-BAS-252, Q-BAS-202 option (b)):
// the member "exp" of payload version 1, in Unix seconds. A Bastion refuses a registry token past this expiry without a request
// and without counting a failed authentication; trusting the unverified expiry can only refuse, because a token whose claims
// were changed fails the main installation's MAC anyway. ok is false for a value outside the form, another payload version,
// claims that do not decode and an expiry that is missing or not positive.
func RegistryTokenExpiry(s string) (time.Time, bool) {
	var c struct {
		ExpiresAt int64 `json:"exp"`
	}
	if !registryTokenClaims(s, &c) || c.ExpiresAt <= 0 {
		return time.Time{}, false
	}
	return time.Unix(c.ExpiresAt, 0).UTC(), true
}

// registryTokenClaims decodes the claims of a well-formed registry token of payload version 1 ("1:<key id>:<claims>") into c
// without verifying them; it reports false when any step fails. Each reader decodes only the members it reads, so a member it
// does not read never changes its answer. The member names are the token service's, held equal to its encoder by the tests.
func registryTokenClaims(s string, c any) bool {
	payload, ok := registryTokenPayload(s)
	if !ok {
		return false
	}
	version, rest, ok := bytes.Cut(payload, []byte(":"))
	if !ok || string(version) != "1" {
		return false
	}
	_, raw, ok := bytes.Cut(rest, []byte(":"))
	return ok && json.Unmarshal(raw, c) == nil
}

// registryTokenPayload checks the form of a registry token and returns its decoded payload.
func registryTokenPayload(s string) ([]byte, bool) {
	if len(s) > MaxRegistryTokenBytes || !strings.HasPrefix(s, RegistryTokenPrefix) {
		return nil, false
	}
	encoded, mac, ok := strings.Cut(s[len(RegistryTokenPrefix):], ".")
	if !ok || encoded == "" || mac == "" {
		return nil, false
	}
	if _, err := base64.RawURLEncoding.Strict().DecodeString(mac); err != nil {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, false
	}
	return payload, true
}
