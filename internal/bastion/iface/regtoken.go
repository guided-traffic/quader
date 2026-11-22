package iface

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
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
	payload, ok := registryTokenPayload(s)
	if !ok {
		return false
	}
	version, rest, ok := bytes.Cut(payload, []byte(":"))
	if !ok || string(version) != "1" {
		return false
	}
	_, claims, ok := bytes.Cut(rest, []byte(":"))
	if !ok {
		return false
	}
	var c struct {
		Anonymous bool `json:"a"`
	}
	if json.Unmarshal(claims, &c) != nil {
		return false
	}
	return c.Anonymous
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
