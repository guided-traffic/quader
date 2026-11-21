package pki

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// ParseChain parses a chain of DER certificates as a signature carries it, leaf first, within signatures.max_chain and the size of one
// certificate (R-SIG-013), and refuses a weak algorithm or an RSA key outside the bounds in any of them.
func ParseChain(ders [][]byte, l scheme.Limits) ([]*x509.Certificate, error) {
	if len(ders) == 0 {
		return nil, Malformed("the signature carries no certificate")
	}
	if len(ders) > l.MaxChain {
		return nil, TooLarge("the chain holds %d certificates, more than signatures.max_chain (%d)", len(ders), l.MaxChain)
	}
	out := make([]*x509.Certificate, 0, len(ders))
	for i, der := range ders {
		if len(der) > l.MaxCertificateSize {
			return nil, TooLarge("certificate %d of the chain is %d bytes, more than %d", i, len(der), l.MaxCertificateSize)
		}
		c, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, Malformed("certificate %d of the chain does not parse", i)
		}
		if err := CheckCertificate(c, l); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// CheckCertificate refuses a certificate signed with a weak algorithm (SHA-1, MD5) or holding a key CheckKey refuses. The signature of a
// self-signed certificate is not judged: nothing verifies it, because a root is trusted as an anchor and not by its own signature (as
// crypto/x509 never checks it), so a root signed with SHA-1 in 2006 does not make a chain weak; its key is judged (Q-SIG-218).
func CheckCertificate(c *x509.Certificate, l scheme.Limits) error {
	if !bytes.Equal(c.RawIssuer, c.RawSubject) {
		switch c.SignatureAlgorithm {
		case x509.SHA1WithRSA, x509.ECDSAWithSHA1, x509.DSAWithSHA1, x509.DSAWithSHA256, x509.MD5WithRSA, x509.MD2WithRSA:
			return Unsupported(signatures.ReasonWeakAlgorithm, "the certificate %q is signed with %s", c.Subject.String(), c.SignatureAlgorithm)
		case x509.UnknownSignatureAlgorithm:
			return Unsupported(signatures.ReasonVersionUnsupported, "the certificate %q is signed with an algorithm Quader does not read", c.Subject.String())
		}
	}
	return CheckKey(c.PublicKey, l)
}

// CheckKey refuses an RSA key below signatures' least modulus (weak_algorithm) or above its largest (limits_exceeded), a curve other than
// P-256, P-384 and P-521, and any other kind of key than RSA, ECDSA and Ed25519.
func CheckKey(pub crypto.PublicKey, l scheme.Limits) error {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		bits := k.N.BitLen()
		if bits < l.MinRSABits {
			return Unsupported(signatures.ReasonWeakAlgorithm, "an RSA key of %d bits is below %d", bits, l.MinRSABits)
		}
		if bits > l.MaxRSABits {
			return TooLarge("an RSA key of %d bits is above %d", bits, l.MaxRSABits)
		}
		return nil
	case *ecdsa.PublicKey:
		switch k.Curve {
		case elliptic.P256(), elliptic.P384(), elliptic.P521():
			return nil
		}
		return Unsupported(signatures.ReasonWeakAlgorithm, "an ECDSA key on a curve Quader does not accept")
	case ed25519.PublicKey:
		return nil
	}
	return Unsupported(signatures.ReasonWeakAlgorithm, "a key of a kind Quader does not accept (%T)", pub)
}

// ChainSigns checks that every certificate of a chain (leaf first) is issued by the next: its issuer is the next one's subject and the
// next one's key verifies its signature. Whether the chain ends in a trusted certificate is the trust step's (R-SIG-008).
func ChainSigns(chain []*x509.Certificate) error {
	for i := 0; i+1 < len(chain); i++ {
		c, parent := chain[i], chain[i+1]
		if !bytes.Equal(c.RawIssuer, parent.RawSubject) {
			return Invalid(signatures.ReasonChainBroken, "certificate %q is not issued by %q", c.Subject.String(), parent.Subject.String())
		}
		if err := parent.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature); err != nil {
			return Invalid(signatures.ReasonChainBroken, "the signature of %q by %q does not verify", c.Subject.String(), parent.Subject.String())
		}
	}
	return nil
}

// Order puts the certificates a CMS structure carries into a chain from leaf: each next certificate is the issuer of the one before. The
// certificates that are no link of it are left out.
func Order(leaf *x509.Certificate, certs []*x509.Certificate, max int) []*x509.Certificate {
	chain := []*x509.Certificate{leaf}
	for len(chain) < max {
		last := chain[len(chain)-1]
		if bytes.Equal(last.RawIssuer, last.RawSubject) {
			break
		}
		next := issuerOf(last, certs, chain)
		if next == nil {
			break
		}
		chain = append(chain, next)
	}
	return chain
}

func issuerOf(c *x509.Certificate, certs, used []*x509.Certificate) *x509.Certificate {
	for _, cand := range certs {
		if !bytes.Equal(c.RawIssuer, cand.RawSubject) || contains(used, cand) {
			continue
		}
		return cand
	}
	return nil
}

func contains(list []*x509.Certificate, c *x509.Certificate) bool {
	for _, x := range list {
		if bytes.Equal(x.Raw, c.Raw) {
			return true
		}
	}
	return false
}

// DERs is the DER of each certificate of a chain.
func DERs(chain []*x509.Certificate) [][]byte {
	out := make([][]byte, 0, len(chain))
	for _, c := range chain {
		out = append(out, c.Raw)
	}
	return out
}

// CertificateKeyIDs are the key IDs of a chain (R-SIG-005): the lower-case hex SHA-256 of every certificate's DER, and the leaf's
// "<issuer DN>|<serial decimal>".
func CertificateKeyIDs(chain []*x509.Certificate) []string {
	out := make([]string, 0, len(chain)+1)
	for _, c := range chain {
		s := sha256.Sum256(c.Raw)
		out = append(out, hex.EncodeToString(s[:]))
	}
	leaf := chain[0]
	return append(out, leaf.Issuer.String()+"|"+leaf.SerialNumber.String())
}

// ChainSigner fills the signer's fields a chain gives (R-SIG-005): the leaf's subject, issuer, alternative names, serial and validity, the
// subjects of the chain and the leaf's key algorithm.
func ChainSigner(chain []*x509.Certificate, s *signatures.Signer) {
	leaf := chain[0]
	s.Subject, s.Issuer = leaf.Subject.String(), leaf.Issuer.String()
	s.AlternativeNames = AlternativeNames(leaf)
	if len(s.AlternativeNames) > 0 {
		s.Identity = s.AlternativeNames[0]
	}
	s.Serial = leaf.SerialNumber.String()
	s.CertificateNotBefore, s.CertificateNotAfter = leaf.NotBefore.UTC(), leaf.NotAfter.UTC()
	s.Chain = nil
	for _, c := range chain {
		s.Chain = append(s.Chain, c.Subject.String())
	}
	s.Algorithm = AlgorithmName(leaf.PublicKey)
}

// AlternativeNames are every subject alternative name of a certificate: URIs, e-mail addresses, DNS names and IP addresses.
func AlternativeNames(c *x509.Certificate) []string {
	var out []string
	for _, u := range c.URIs {
		out = append(out, u.String())
	}
	out = append(out, c.EmailAddresses...)
	out = append(out, c.DNSNames...)
	for _, ip := range c.IPAddresses {
		out = append(out, ip.String())
	}
	return out
}

// KeyID is the lower-case hex SHA-256 of a public key's PKIX DER: the key ID of a raw key (R-SIG-005).
func KeyID(pub crypto.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return ""
	}
	s := sha256.Sum256(der)
	return hex.EncodeToString(s[:])
}

// AlgorithmName names a public key's algorithm as the signer shows it ("ECDSA P-256", "RSA 4096", "Ed25519").
func AlgorithmName(pub crypto.PublicKey) string {
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		return "ECDSA " + k.Curve.Params().Name
	case *rsa.PublicKey:
		return fmt.Sprintf("RSA %d", k.N.BitLen())
	case ed25519.PublicKey:
		return "Ed25519"
	}
	return fmt.Sprintf("%T", pub)
}

// HashName names a hash as the signer shows it ("SHA-256").
func HashName(h crypto.Hash) string {
	switch h {
	case crypto.SHA256:
		return "SHA-256"
	case crypto.SHA384:
		return "SHA-384"
	case crypto.SHA512:
		return "SHA-512"
	case crypto.SHA1:
		return "SHA-1"
	}
	return h.String()
}

// HashKey is a hash's name as the evidence and signatures.digest_alg spell it ("sha256").
func HashKey(h crypto.Hash) string {
	switch h {
	case crypto.SHA256:
		return "sha256"
	case crypto.SHA384:
		return "sha384"
	case crypto.SHA512:
		return "sha512"
	case crypto.SHA1:
		return "sha1"
	}
	return ""
}

// HashOf is the hash a name of HashKey stands for, 0 for none.
func HashOf(name string) crypto.Hash {
	switch name {
	case "sha256":
		return crypto.SHA256
	case "sha384":
		return crypto.SHA384
	case "sha512":
		return crypto.SHA512
	}
	return 0
}
