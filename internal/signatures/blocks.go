package signatures

import (
	"regexp"
	"strings"
)

// The kinds of a block of a signer (R-SIG-020), as signature_blocks.kind spells them.
const (
	BlockFingerprint = "fingerprint"
	BlockSerial      = "serial"
	BlockKeyID       = "keyid"
	BlockIdentity    = "identity"
)

// BlockMatches reports whether a block of a signer (R-SIG-020, Q-SIG-119) matches a signature by its identifiers (the stored key IDs, and
// for the trust step the keys of the anchors that verified it) or by its Sigstore identity: a fingerprint or a keyid in hex matches
// regardless of case (certificates' are spelled lower-case, OpenPGP's upper-case); an issuer and serial and an npm keyid match exactly;
// an identity pattern matches the whole identity, under the block's OIDC issuer when it names one. The trust step judges a row by it
// (internal/signatures/trust), and the gate of a repository subtracts a block newer than a row by it (R-SIG-066).
func BlockMatches(kind, value, blockIssuer string, ids map[string]bool, oidcIssuer, identity string) bool {
	switch kind {
	case BlockFingerprint, BlockKeyID:
		if ids[value] {
			return true
		}
		if !isHex(value) {
			return false
		}
		for id := range ids {
			if strings.EqualFold(id, value) {
				return true
			}
		}
	case BlockSerial:
		return ids[value]
	case BlockIdentity:
		if identity == "" || (blockIssuer != "" && blockIssuer != oidcIssuer) {
			return false
		}
		p, err := regexp.Compile(`^(?:` + value + `)$`)
		return err == nil && p.MatchString(identity)
	}
	return false
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}
