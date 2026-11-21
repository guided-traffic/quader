// Package anchors is the trust anchors of signatures on artifacts (signatures.md R-SIG-015, R-SIG-016): the four kinds, their material as
// the API, the Helm values and signature_anchors.material carry it, the checks on save, the parsed forms the trust step judges with, and
// the fingerprints and expiries an anchor shows.
package anchors

import (
	"crypto"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/guided-traffic/quader/internal/signatures"
)

// The kinds of trust anchor (R-SIG-015, Q-SIG-2).
const (
	KindCA        = "ca"
	KindOpenPGP   = "openpgp"
	KindPublicKey = "public-key"
	KindSigstore  = "sigstore"
)

// The scopes of an anchor (R-SIG-016).
const (
	ScopeShipped       = "shipped"
	ScopeConfiguration = "configuration"
	ScopeInstallation  = "installation"
	ScopeProject       = "project"
)

// The uses of a CA anchor (R-SIG-015, Q-SIG-30): a code-signing anchor knows signers, a timestamping anchor proves a time and never
// trusts a signer.
const (
	UseCodeSigning  = "codeSigning"
	UseTimestamping = "timestamping"
)

// The trust roots a Sigstore identity anchor names (Q-SIG-27).
const (
	TrustRootPublic = "public"
	TrustRootOwn    = "own"
)

// KindValid reports whether kind is one of the four kinds.
func KindValid(kind string) bool {
	switch kind {
	case KindCA, KindOpenPGP, KindPublicKey, KindSigstore:
		return true
	}
	return false
}

// Material is an anchor's material: exactly the member of its kind is set. Its JSON is signature_anchors.material, the kind's member of
// the API's anchor (R-SIG-034) and the kind's block of an anchor of the Helm values (R-SIG-018).
type Material struct {
	CA        *CAMaterial        `json:"ca,omitempty"`
	OpenPGP   *OpenPGPMaterial   `json:"openpgp,omitempty"`
	PublicKey *PublicKeyMaterial `json:"publicKey,omitempty"`
	Sigstore  *SigstoreMaterial  `json:"sigstore,omitempty"`
}

// CAMaterial is a CA anchor's material.
type CAMaterial struct {
	// Certificates are 1 to 16 PEM certificates: CA certificates, or a pinned end-entity certificate that is the signer itself.
	Certificates string `json:"certificates"`
	// Use is UseCodeSigning (the default, "") or UseTimestamping.
	Use string `json:"use,omitempty"`
	// Subject restricts the leaf's subject: attributes O, OU, CN, C that must match exactly.
	Subject map[string]string `json:"subject,omitempty"`
	// SubjectAltNames restricts the leaf's alternative names: RE2 patterns, anchored, at least one of which an alternative name matches.
	SubjectAltNames []string `json:"subjectAltNames,omitempty"`
}

// OpenPGPMaterial is an OpenPGP anchor's material.
type OpenPGPMaterial struct {
	// Keys are 1 to 32 armored public certificates.
	Keys string `json:"keys"`
}

// PublicKeyMaterial is a public-key anchor's material.
type PublicKeyMaterial struct {
	// Keys are 1 to 16 public keys.
	Keys []PublicKeyEntry `json:"keys"`
}

// PublicKeyEntry is one public key.
type PublicKeyEntry struct {
	// PEM is the PKIX public key, PEM.
	PEM string `json:"pem"`
	// KeyID is npm's keyid ("SHA256:<base64>"), optional.
	KeyID string `json:"keyid,omitempty"`
	// Expires is when the key stops signing, optional (npm's keys.json "validFor.end").
	Expires *time.Time `json:"expires,omitempty"`
}

// SigstoreMaterial is a Sigstore identity anchor's material.
type SigstoreMaterial struct {
	// Issuer is the OIDC issuer, exact, an HTTPS URL; "" matches any issuer (only the shipped sigstore-public uses it).
	Issuer string `json:"issuer"`
	// Identity is the identity as an exact value; IdentityPattern as an RE2 pattern, anchored, at most 512 characters. One of the two.
	Identity        string `json:"identity,omitempty"`
	IdentityPattern string `json:"identityPattern,omitempty"`
	// TrustRoot is TrustRootPublic (the public root in use, R-SIG-017) or TrustRootOwn (TrustedRoot holds the company's root).
	TrustRoot string `json:"trustRoot"`
	// TrustedRoot is a trusted_root.json of the company's Sigstore, at most 1 MiB, when TrustRoot is TrustRootOwn.
	TrustedRoot json.RawMessage `json:"trustedRoot,omitempty"`
}

// Anchor is one stored anchor with its parsed material: what the trust step judges with (R-SIG-008).
type Anchor struct {
	ID        int64
	Scope     string
	ProjectID string
	Key       string
	Title     string
	Kind      string
	Enabled   bool
	Material  Material
	// Parsed is the material parsed; nil when it does not parse any more (the trust step then skips the anchor and logs it).
	Parsed *Parsed
}

// Parsed is an anchor's material in the forms the verifiers and the trust step use.
type Parsed struct {
	CA        *ParsedCA
	OpenPGP   []OpenPGPCertificate
	Public    []ParsedKey
	Sigstore  *ParsedSigstore
	Describes []Fingerprint
}

// ParsedCA is a CA anchor parsed.
type ParsedCA struct {
	Certificates []*x509.Certificate
	Use          string
	Subject      map[string]string
	SANPatterns  []*regexp.Regexp
}

// OpenPGPCertificate is one armored OpenPGP public certificate with its primary key's fingerprint (upper-case hex) and its key IDs
// (the primary's and every subkey's, upper-case hex of 16 digits), as internal/openpgp reads it.
type OpenPGPCertificate struct {
	Armored     []byte
	Fingerprint string
	KeyIDs      []string
	UserID      string
	Created     time.Time
	Expires     time.Time
}

// ParsedKey is one raw public key: the key, npm's keyid where named, its expiry, and the SHA-256 of its PKIX DER (hex) by which a
// signature without a keyid is matched and a block names it.
type ParsedKey struct {
	Key     crypto.PublicKey
	KeyID   string
	Expires time.Time
	SHA256  string
}

// ParsedSigstore is a Sigstore identity anchor parsed. TrustedRoot is the document of an own root; nil names the public root in use,
// which the trust step resolves (the release's snapshot or the refreshed one).
type ParsedSigstore struct {
	Issuer      string
	Identity    string
	Pattern     *regexp.Regexp
	TrustedRoot json.RawMessage
}

// Fingerprint is what an anchor shows of one piece of its material (R-SIG-015).
type Fingerprint = signatures.Fingerprint

// Invalid is a refusal of an anchor's material on save: the JSON pointer of the member, the code ("invalid_value" or "secret_material")
// and the reason in plain words (R-SIG-015, answered 422).
type Invalid struct {
	Pointer string
	Code    string
	Reason  string
}

func (e *Invalid) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Pointer, e.Reason, e.Code) }

// The codes of [Invalid].
const (
	CodeInvalidValue   = "invalid_value"
	CodeSecretMaterial = "secret_material"
)

// AsInvalid reports whether err is an [Invalid].
func AsInvalid(err error) (*Invalid, bool) {
	var inv *Invalid
	ok := errors.As(err, &inv)
	return inv, ok
}

// Validate applies the checks on save of R-SIG-015 to the material of an anchor of kind at now and answers it parsed, or an [*Invalid].
// The member of the kind must be the one set.
func Validate(kind string, m Material, now time.Time) (*Parsed, error) {
	return validate(kind, m, now)
}

// Parse parses stored material without the checks that depend on the time of saving (an expired certificate still judges a signature
// made while it was valid, R-SIG-007).
func Parse(kind string, m Material) (*Parsed, error) {
	return parse(kind, m)
}
