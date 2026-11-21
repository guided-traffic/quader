package anchors

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
)

// The bounds of the material (R-SIG-015).
const (
	maxCABytes          = 64 << 10
	maxCACertificates   = 16
	maxOpenPGPBytes     = 256 << 10
	maxOpenPGPCerts     = 32
	maxPublicKeys       = 16
	maxPublicKeyPEM     = 16 << 10
	maxTrustedRootBytes = 1 << 20
	maxPatternLength    = 512
	maxIdentityLength   = 1024
	maxSANPatterns      = 16
	maxSubjectValue     = 256
)

// memberOf is the member of [Material] of a kind, as its JSON names it: the first segment of every pointer of a refusal.
func memberOf(kind string) string {
	switch kind {
	case KindCA:
		return "ca"
	case KindOpenPGP:
		return "openpgp"
	case KindPublicKey:
		return "publicKey"
	}
	return "sigstore"
}

// checking is one check of material: at save (now set, the checks of the time of saving applied) or of stored material (now zero).
type checking struct {
	now    time.Time
	atSave bool
}

func validate(kind string, m Material, now time.Time) (*Parsed, error) {
	return check(kind, m, checking{now: now, atSave: true})
}

func parse(kind string, m Material) (*Parsed, error) {
	return check(kind, m, checking{})
}

// check applies the checks of R-SIG-015 in their order: the kind, no secret anywhere in the material, exactly the kind's member, then the
// kind's own checks.
func check(kind string, m Material, c checking) (*Parsed, error) {
	if !KindValid(kind) {
		return nil, refuse("/kind", "the kind is none of ca, openpgp, public-key and sigstore")
	}
	if err := scanSecrets(m); err != nil {
		return nil, err
	}
	if err := onlyMember(kind, m); err != nil {
		return nil, err
	}
	switch kind {
	case KindCA:
		return checkCA(m.CA, c)
	case KindOpenPGP:
		return checkOpenPGP(m.OpenPGP, c)
	case KindPublicKey:
		return checkPublicKeys(m.PublicKey)
	default:
		return checkSigstore(m.Sigstore, c)
	}
}

// onlyMember refuses material whose kind's member is missing or that sets the member of another kind.
func onlyMember(kind string, m Material) error {
	set := map[string]bool{
		KindCA:        m.CA != nil,
		KindOpenPGP:   m.OpenPGP != nil,
		KindPublicKey: m.PublicKey != nil,
		KindSigstore:  m.Sigstore != nil,
	}
	for _, k := range []string{KindCA, KindOpenPGP, KindPublicKey, KindSigstore} {
		if k != kind && set[k] {
			return refuse("/"+memberOf(k), "only the member of the kind "+kind+" may be set")
		}
	}
	if !set[kind] {
		return refuse("/"+memberOf(kind), "the material of the kind "+kind+" is missing")
	}
	return nil
}

// privateBlock matches the first line of a PEM block of a private key of any type (PKCS#1, PKCS#8, SEC 1, OpenSSH, encrypted) and of an
// armored OpenPGP secret key.
var privateBlock = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY[A-Z ]*-----`)

// field is one text of the material with its JSON pointer.
type field struct {
	pointer string
	text    string
}

// fields lists every text the material carries, of every member, so that a secret is found wherever it was put.
func fields(m Material) []field {
	var out []field
	if ca := m.CA; ca != nil {
		out = append(out, field{"/ca/certificates", ca.Certificates}, field{"/ca/use", ca.Use})
		for k, v := range ca.Subject {
			out = append(out, field{"/ca/subject/" + k, k + v})
		}
		for i, p := range ca.SubjectAltNames {
			out = append(out, field{fmt.Sprintf("/ca/subjectAltNames/%d", i), p})
		}
	}
	if pgp := m.OpenPGP; pgp != nil {
		out = append(out, field{"/openpgp/keys", pgp.Keys})
	}
	if pk := m.PublicKey; pk != nil {
		for i, k := range pk.Keys {
			out = append(out, field{fmt.Sprintf("/publicKey/keys/%d/pem", i), k.PEM}, field{fmt.Sprintf("/publicKey/keys/%d/keyid", i), k.KeyID})
		}
	}
	if s := m.Sigstore; s != nil {
		out = append(out, field{"/sigstore/issuer", s.Issuer}, field{"/sigstore/identity", s.Identity},
			field{"/sigstore/identityPattern", s.IdentityPattern}, field{"/sigstore/trustRoot", s.TrustRoot},
			field{"/sigstore/trustedRoot", string(s.TrustedRoot)})
	}
	return out
}

// scanSecrets refuses material that holds a private key or an OpenPGP secret key in any of its texts (R-SIG-015: an anchor's material
// is public data).
func scanSecrets(m Material) error {
	for _, f := range fields(m) {
		if privateBlock.MatchString(f.text) {
			return &Invalid{
				Pointer: f.pointer, Code: CodeSecretMaterial,
				Reason: "the material holds a private key; an anchor holds public material only",
			}
		}
	}
	return nil
}

// refuse is a refusal of the code invalid_value.
func refuse(pointer, reason string) *Invalid {
	return &Invalid{Pointer: pointer, Code: CodeInvalidValue, Reason: reason}
}

// anchored compiles an RE2 pattern so that it matches the whole value.
func anchored(pattern string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(`^(?:` + pattern + `)$`)
	if err != nil {
		return nil, fmt.Errorf("compile pattern: %w", err)
	}
	return re, nil
}

// sha256Hex is the lower-case hex SHA-256 of b.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
