package sigstore

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/secure-systems-lab/go-securesystemslib/dsse"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/sigstore/sigstore/pkg/signature/options"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// The integrity check (R-SIG-006): the algorithms, the subject, the chain, the signature, then the proofs of time.

// checkIntegrity runs the integrity check of o against the input; rd gains the key, the proofs and the evidence's parts.
func checkIntegrity(in scheme.Input, o *object, rd *reading) error {
	if err := checkAlgorithms(o, in.Limits); err != nil {
		return err
	}
	if err := checkSubject(o, in); err != nil {
		return err
	}
	if err := checkChain(o); err != nil {
		return err
	}
	keys, err := checkSignature(o, in, rd)
	if err != nil {
		return err
	}
	return prove(o, rd, roots(in.Roots), keys)
}

// checkAlgorithms refuses a weak algorithm (unsupported, weak_algorithm: SHA-1, RSA below signatures' minimum) and an RSA key above the
// maximum (malformed, limits_exceeded), in every certificate the signature carries (R-SIG-013).
func checkAlgorithms(o *object, l scheme.Limits) error {
	for _, c := range o.certs {
		switch c.SignatureAlgorithm {
		case x509.SHA1WithRSA, x509.ECDSAWithSHA1, x509.DSAWithSHA1, x509.MD5WithRSA, x509.MD2WithRSA:
			return unsupported(signatures.ReasonWeakAlgorithm, "a certificate of the signature is signed with %s", c.SignatureAlgorithm)
		}
		if err := checkKey(c.PublicKey, l); err != nil {
			return err
		}
	}
	return nil
}

// checkKey bounds an RSA key: below MinRSABits is weak, above MaxRSABits is refused.
func checkKey(pub crypto.PublicKey, l scheme.Limits) error {
	k, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil
	}
	switch bits := k.N.BitLen(); {
	case bits < l.MinRSABits:
		return unsupported(signatures.ReasonWeakAlgorithm, "an RSA key of %d bits is below %d bits", bits, l.MinRSABits)
	case bits > l.MaxRSABits:
		return tooLarge("an RSA key of %d bits is above %d bits", bits, l.MaxRSABits)
	}
	return nil
}

// checkSubject checks that the signature names the subject and sets the artifact digest it is checked against: a message signature's
// digest is the subject's SHA-256 (or SHA-512) or, for cosign's simple signing, the payload's, whose docker-manifest-digest must be the
// subject; a statement must name the subject's SHA-256 or SHA-512 among its subjects (npm's provenance names the tarball by SHA-512).
func checkSubject(o *object, in scheme.Input) error {
	if o.legacy != nil {
		want := "sha256:" + hex.EncodeToString(in.Subject[:])
		if !strings.EqualFold(o.legacy.Critical.Image.DockerManifestDigest, want) {
			return invalid(signatures.ReasonSubjectMismatch, "the payload names the image %s, not %s", o.legacy.Critical.Image.DockerManifestDigest, want)
		}
		o.artifactAlg, o.artifactDigest = "sha256", o.digest
		return nil
	}
	if o.stmt != nil {
		return checkStatement(o, in)
	}
	var want []byte
	switch o.digestAlg {
	case protocommon.HashAlgorithm_SHA2_256:
		o.artifactAlg, want = "sha256", in.Subject[:]
	case protocommon.HashAlgorithm_SHA2_512:
		o.artifactAlg, want = "sha512", in.SubjectSHA512
	default:
		return unsupported(signatures.ReasonVersionUnsupported, "the message digest algorithm %s is not one Quader keeps of a subject", o.digestAlg)
	}
	if len(want) == 0 || !bytes.Equal(o.digest, want) {
		return invalid(signatures.ReasonDigestMismatch, "the signature's digest is not the digest of the content")
	}
	o.artifactDigest = want
	return nil
}

// checkStatement looks for the subject among a statement's subjects, by SHA-256 first, then by SHA-512.
func checkStatement(o *object, in scheme.Input) error {
	want := map[string][]byte{"sha256": in.Subject[:]}
	if len(in.SubjectSHA512) > 0 {
		want["sha512"] = in.SubjectSHA512
	}
	for _, alg := range []string{"sha256", "sha512"} {
		w, ok := want[alg]
		if !ok {
			continue
		}
		for _, s := range o.stmt.Subject {
			if d, err := hex.DecodeString(s.Digest[alg]); err == nil && bytes.Equal(d, w) {
				o.artifactAlg, o.artifactDigest = alg, w
				return nil
			}
		}
	}
	return invalid(signatures.ReasonSubjectMismatch, "no subject of the statement is the content")
}

// checkChain checks that each certificate the signature carries is signed by the next: the chain signs the leaf (chain_broken). Whether
// the chain ends in a trusted root is the trust step's question (R-SIG-008).
func checkChain(o *object) error {
	for i := 0; i+1 < len(o.certs); i++ {
		if err := o.certs[i].CheckSignatureFrom(o.certs[i+1]); err != nil {
			return invalid(signatures.ReasonChainBroken, "the certificate %q is not signed by %q", o.certs[i].Subject.String(), o.certs[i+1].Subject.String())
		}
	}
	return nil
}

// checkSignature verifies the signature with the leaf's key, or, for a bundle signed with a key, with a key of the input (the hint's key
// first). No known key: the signature is no_key and keeps what a key added later verifies it against (Q-SIG-23). It answers the trust
// material of the key the log entries are compared with.
func checkSignature(o *object, in scheme.Input, rd *reading) (root.TrustedMaterial, error) {
	sc, err := o.b.SignatureContent()
	if err != nil {
		return nil, malformed("the bundle's signature does not parse")
	}
	vc, err := o.b.VerificationContent()
	if err != nil {
		return nil, malformed("the bundle's certificate or key does not parse")
	}
	digests := []verify.ArtifactDigest{{Algorithm: o.artifactAlg, Digest: o.artifactDigest}}
	run := func(tm root.TrustedMaterial) error {
		if o.stmt != nil {
			return verify.VerifySignature(sc, vc, tm)
		}
		return verify.VerifySignatureWithArtifactDigests(sc, vc, tm, digests)
	}
	if o.leaf() != nil {
		if err := run(&root.BaseTrustedMaterial{}); err != nil {
			return nil, invalid(signatures.ReasonSignatureMismatch, "the signature does not verify with the certificate's key")
		}
		return nil, nil
	}
	rd.signedDigest = signedDigest(o)
	if o.stmt == nil {
		rd.digestAlg = o.artifactAlg
	}
	return checkKeySignature(o, in, rd, run)
}

// checkKeySignature verifies a key-signed bundle with the known keys: the keys its hint names first, every key when the hint names none
// (or there is no hint). A named key that does not verify it is signature_mismatch; no key that verifies it is no_key, whose log entries
// are compared with the key the log recorded.
func checkKeySignature(o *object, in scheme.Input, rd *reading, run func(root.TrustedMaterial) error) (root.TrustedMaterial, error) {
	var named, others []anchors.ParsedKey
	for _, k := range in.Keys.Public {
		if hintMatches(o.hint, k.Key, k.SHA256, k.KeyID) {
			named = append(named, k)
		} else {
			others = append(others, k)
		}
	}
	try := others
	if len(named) > 0 {
		try = named
	}
	for _, k := range try {
		tm, err := keyMaterial(o, k.Key)
		if err != nil {
			continue
		}
		if run(tm) == nil {
			return tm, keyFound(in, rd, k)
		}
	}
	if len(named) > 0 {
		return nil, invalid(signatures.ReasonSignatureMismatch, "the signature does not verify with the key its hint names")
	}
	rd.noKey = true
	rd.signer.Detail = joinDetail(rd.signer.Detail, "no known key verifies this signature yet")
	return loggedKeyMaterial(o), nil
}

// keyFound records the key that verified a key-signed bundle.
func keyFound(in scheme.Input, rd *reading, k anchors.ParsedKey) error {
	if err := checkKey(k.Key, in.Limits); err != nil {
		return err
	}
	id := k.SHA256
	if id == "" {
		id = keyID(k.Key)
	}
	rd.key = k.Key
	rd.signer.KeyID, rd.signer.Algorithm = id, algorithmName(k.Key)
	rd.keyIDs = appendUnique(rd.keyIDs, id)
	if k.KeyID != "" {
		rd.keyIDs = appendUnique(rd.keyIDs, k.KeyID)
	}
	return nil
}

// keyMaterial is trust material that holds one key under the bundle's hint, as sigstore-go asks for it.
func keyMaterial(o *object, pub crypto.PublicKey) (root.TrustedMaterial, error) {
	var opts []signature.LoadOption
	if o.stmt == nil {
		opts = append(opts, options.WithED25519ph())
	}
	v, err := signature.LoadDefaultVerifier(pub, opts...)
	if err != nil {
		return nil, fmt.Errorf("sigstore: the key cannot verify: %w", err)
	}
	ek := root.NewExpiringKey(v, time.Time{}, time.Time{})
	return root.NewTrustedPublicKeyMaterialFromMapping(map[string]*root.ExpiringKey{o.hint: ek}), nil
}

// loggedKeyMaterial is the key a log entry of a no_key bundle recorded: the entry is checked for its own consistency with the bundle
// (its signature and digest), which proves when it was logged and nothing about who signed.
func loggedKeyMaterial(o *object) root.TrustedMaterial {
	entries, err := o.b.TlogEntries()
	if err != nil || len(entries) == 0 {
		return nil
	}
	pub, ok := entries[0].PublicKey().(crypto.PublicKey)
	if !ok || pub == nil {
		return nil
	}
	if _, isCert := pub.(*x509.Certificate); isCert {
		return nil
	}
	tm, err := keyMaterial(o, pub)
	if err != nil {
		return nil
	}
	return tm
}

// signedDigest is what a key signature commits to (Q-SIG-23): a message signature's digest, or the SHA-256 of a DSSE envelope's
// pre-authentication encoding.
func signedDigest(o *object) []byte {
	if o.stmt == nil {
		return o.digest
	}
	s := sha256.Sum256(dsse.PAE(o.payloadType, o.payload))
	return s[:]
}

// keyKind is the kind of a key signature for the evidence: by the key when one verified it, else by the signature's encoding (an ASN.1
// pair of integers is ECDSA, 64 bytes Ed25519, anything else RSA PKCS #1 v1.5, cosign's default).
func keyKind(sig []byte, pub crypto.PublicKey) string {
	switch pub.(type) {
	case *ecdsa.PublicKey:
		return "ecdsa"
	case *rsa.PublicKey:
		return "rsa-pkcs1v15"
	case ed25519.PublicKey:
		return "ed25519"
	}
	var rs struct{ R, S *big.Int }
	if rest, err := asn1.Unmarshal(sig, &rs); err == nil && len(rest) == 0 {
		return "ecdsa"
	}
	if len(sig) == ed25519.SignatureSize {
		return "ed25519"
	}
	return "rsa-pkcs1v15"
}

// roots parses the trust roots of the check; a root that does not parse knows nothing and is skipped (the anchor's save validated it).
func roots(in []scheme.SigstoreRoot) []*root.TrustedRoot {
	var out []*root.TrustedRoot
	for _, r := range in {
		if tr, err := parseRoot(r.TrustedRoot); err == nil {
			out = append(out, tr)
		}
	}
	return out
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func joinDetail(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}
