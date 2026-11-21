package anchors

import (
	"fmt"
	"net/url"
	"regexp"

	"github.com/sigstore/sigstore-go/pkg/root"
)

// checkSigstore checks a Sigstore identity anchor's material (R-SIG-015, Q-SIG-27): the issuer an HTTPS URL (any issuer, "", only in
// stored material: the shipped sigstore-public is the one anchor that uses it), exactly one of the identity and its pattern, and the
// trust root public or an own trusted_root.json that parses and holds a certificate authority and a log or a timestamp authority.
func checkSigstore(m *SigstoreMaterial, c checking) (*Parsed, error) {
	if err := sigstoreIssuer(m.Issuer, c); err != nil {
		return nil, err
	}
	out := &ParsedSigstore{Issuer: m.Issuer, Identity: m.Identity}
	var err error
	if out.Pattern, err = sigstoreIdentity(m); err != nil {
		return nil, err
	}
	if err := sigstoreTrustRoot(m); err != nil {
		return nil, err
	}
	issuer := m.Issuer
	if issuer == "" {
		issuer = "any issuer"
	}
	identity := m.Identity
	if identity == "" {
		identity = m.IdentityPattern
	}
	describes := []Fingerprint{{Kind: "sigstore", Value: m.TrustRoot, Subject: issuer + " " + identity}}
	if m.TrustRoot == TrustRootOwn {
		out.TrustedRoot = append([]byte(nil), m.TrustedRoot...)
		describes = append(describes, Fingerprint{Kind: "sha256", Value: sha256Hex(m.TrustedRoot), Subject: "trusted_root.json"})
	}
	return &Parsed{Sigstore: out, Describes: describes}, nil
}

// sigstoreIssuer checks the issuer: an absolute HTTPS URL with a host; "" (any issuer) is refused on save.
func sigstoreIssuer(issuer string, c checking) error {
	const ptr = "/sigstore/issuer"
	if issuer == "" {
		if c.atSave {
			return refuse(ptr, "the issuer is needed: an HTTPS URL")
		}
		return nil
	}
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(issuer) > maxIdentityLength {
		return refuse(ptr, "the issuer is an HTTPS URL")
	}
	return nil
}

// sigstoreIdentity checks that exactly one of the identity and its pattern is set and compiles the pattern, anchored; nil for an exact
// identity.
func sigstoreIdentity(m *SigstoreMaterial) (*regexp.Regexp, error) {
	switch {
	case m.Identity != "" && m.IdentityPattern != "":
		return nil, refuse("/sigstore/identityPattern", "the identity is an exact value or a pattern, not both")
	case m.Identity == "" && m.IdentityPattern == "":
		return nil, refuse("/sigstore/identity", "the identity is needed: an exact value or a pattern")
	case m.Identity != "":
		if len(m.Identity) > maxIdentityLength {
			return nil, refuse("/sigstore/identity", fmt.Sprintf("the identity is at most %d characters", maxIdentityLength))
		}
		return nil, nil
	}
	if len(m.IdentityPattern) > maxPatternLength {
		return nil, refuse("/sigstore/identityPattern", fmt.Sprintf("the pattern is at most %d characters", maxPatternLength))
	}
	re, err := anchored(m.IdentityPattern)
	if err != nil {
		return nil, refuse("/sigstore/identityPattern", "the pattern is no RE2 pattern: "+err.Error())
	}
	return re, nil
}

// sigstoreTrustRoot checks the trust root: public without a document, or own with a trusted_root.json of at most 1 MiB that sigstore-go
// reads and that holds at least one certificate authority and at least one transparency log or timestamp authority.
func sigstoreTrustRoot(m *SigstoreMaterial) error {
	const ptr = "/sigstore/trustedRoot"
	switch m.TrustRoot {
	case TrustRootPublic:
		if len(m.TrustedRoot) != 0 {
			return refuse(ptr, "a trusted root is given only with the trust root own")
		}
		return nil
	case TrustRootOwn:
	default:
		return refuse("/sigstore/trustRoot", "the trust root is public or own")
	}
	if len(m.TrustedRoot) == 0 {
		return refuse(ptr, "the trust root own needs the company's trusted_root.json")
	}
	if len(m.TrustedRoot) > maxTrustedRootBytes {
		return refuse(ptr, fmt.Sprintf("the trusted root is at most %d MiB", maxTrustedRootBytes>>20))
	}
	tr, err := root.NewTrustedRootFromJSON(m.TrustedRoot)
	if err != nil {
		return refuse(ptr, "the trusted root does not parse as Sigstore's trusted_root.json: "+err.Error())
	}
	if len(tr.FulcioCertificateAuthorities()) == 0 {
		return refuse(ptr, "the trusted root holds no certificate authority")
	}
	if len(tr.RekorLogs()) == 0 && len(tr.TimestampingAuthorities()) == 0 {
		return refuse(ptr, "the trusted root holds neither a transparency log nor a timestamp authority")
	}
	return nil
}
