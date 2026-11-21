package signing

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

// The bounds of an identity's chain (R-SIG-045, R-SIG-013's bound): at most 8 certificates with the leaf, each of at most 16 KiB.
const (
	MaxChainCertificates = 8
	MaxCertificateSize   = 16 << 10
	// maxPEM bounds what is parsed of a PEM input before the certificates are counted: 8 certificates of 16 KiB in base64 with headers.
	maxPEM = 192 << 10
)

// The checks of R-SIG-045, as a refusal names them (the API's errors[].reason, the event signing.identity.refused's reason).
const (
	CheckMalformed       = "malformed"
	CheckBounds          = "chain_bounds"
	CheckKeyType         = "key_type"
	CheckPair            = "key_mismatch"
	CheckLeafCA          = "leaf_is_ca"
	CheckKeyUsage        = "key_usage"
	CheckCodeSigning     = "code_signing"
	CheckAlternativeName = "alternative_name"
	CheckValidity        = "validity"
	CheckRemaining       = "remaining_validity"
	CheckRoot            = "chain_root"
	CheckChainBuild      = "chain_build"
)

// Refusal is material that fails a check of R-SIG-045: the check and a sentence that says what is wrong without key material.
type Refusal struct {
	Check  string
	Detail string
}

func (r *Refusal) Error() string { return r.Detail }

func refuse(check, format string, args ...any) error {
	return &Refusal{Check: check, Detail: fmt.Sprintf(format, args...)}
}

// RefusalOf answers the refusal in err, or nil.
func RefusalOf(err error) *Refusal {
	var r *Refusal
	if errors.As(err, &r) {
		return r
	}
	return nil
}

// Chain is an identity's certificates as they were handed in: the leaf first, then every other certificate of the chain and of the
// separate CA input.
type Chain struct {
	Leaf   *x509.Certificate
	Others []*x509.Certificate
}

// ParseChain reads the PEM chain (the leaf first, then intermediates, the root optionally last) and the optional PEM of the CA (the
// root, or a bundle that holds it, as cert-manager's ca.crt and an administrator's separate field give it). Blocks other than
// certificates are refused, so that a private key pasted into the chain never travels further.
func ParseChain(chainPEM, caPEM []byte) (Chain, error) {
	if len(chainPEM)+len(caPEM) > maxPEM {
		return Chain{}, refuse(CheckBounds, "the chain is larger than %d KiB: an identity's chain holds at most %d certificates of 16 KiB", maxPEM>>10, MaxChainCertificates)
	}
	leafs, err := certificates(chainPEM, "chain")
	if err != nil {
		return Chain{}, err
	}
	if len(leafs) == 0 {
		return Chain{}, refuse(CheckMalformed, "the chain holds no certificate: give the leaf first, then its intermediates")
	}
	cas, err := certificates(caPEM, "ca")
	if err != nil {
		return Chain{}, err
	}
	c := Chain{Leaf: leafs[0]}
	seen := map[string]bool{string(leafs[0].Raw): true}
	for _, x := range append(leafs[1:], cas...) {
		if !seen[string(x.Raw)] {
			seen[string(x.Raw)] = true
			c.Others = append(c.Others, x)
		}
	}
	if n := 1 + len(c.Others); n > MaxChainCertificates {
		return Chain{}, refuse(CheckBounds, "the chain holds %d certificates; at most %d are taken with the leaf", n, MaxChainCertificates)
	}
	return c, nil
}

func certificates(in []byte, what string) ([]*x509.Certificate, error) {
	var out []*x509.Certificate
	rest := bytes.TrimSpace(in)
	for len(rest) > 0 {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			return nil, refuse(CheckMalformed, "the %s is not PEM: give certificates as -----BEGIN CERTIFICATE----- blocks", what)
		}
		if b.Type != "CERTIFICATE" {
			return nil, refuse(CheckMalformed, "the %s holds a %s block; only certificates belong there", what, b.Type)
		}
		if len(b.Bytes) > MaxCertificateSize {
			return nil, refuse(CheckBounds, "a certificate of the %s is larger than 16 KiB", what)
		}
		x, err := x509.ParseCertificate(b.Bytes)
		if err != nil {
			return nil, refuse(CheckMalformed, "a certificate of the %s does not parse: %v", what, err)
		}
		out = append(out, x)
		rest = bytes.TrimSpace(rest)
	}
	return out, nil
}

// Checked is material that passed every check of R-SIG-045: the leaf, the chain Quader serves (the intermediates of the built path and
// then its self-signed root) and the key.
type Checked struct {
	Leaf  *x509.Certificate
	Chain []*x509.Certificate
	Key   *ecdsa.PublicKey
}

// Check holds the chain and the identity's public key to every row of R-SIG-045's table at now: the key is ECDSA on P-256 and the
// leaf's; the leaf is no CA, carries digitalSignature and codeSigning and at least one URI or e-mail address; it is valid at now with at
// least 24 hours left; and a path to a self-signed root of the chain builds with crypto/x509 for codeSigning. The first check that
// fails is the refusal.
func (c Chain) Check(pub crypto.PublicKey, now time.Time) (Checked, error) {
	key, err := P256(pub)
	if err != nil {
		return Checked{}, err
	}
	leaf := c.Leaf
	if leaf == nil {
		return Checked{}, refuse(CheckMalformed, "the chain holds no certificate")
	}
	lk, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || !lk.Equal(key) {
		return Checked{}, refuse(CheckPair, "the certificate is not for this key: its public key differs from the identity's")
	}
	if err := checkLeaf(leaf, now); err != nil {
		return Checked{}, err
	}
	root, inter := splitRoot(c.Others)
	if root == nil {
		return Checked{}, refuse(CheckRoot, "the chain does not end in a self-signed root: add the root certificate last, or in the CA field")
	}
	path, err := buildPath(leaf, inter, root, now)
	if err != nil {
		return Checked{}, err
	}
	return Checked{Leaf: leaf, Chain: path, Key: key}, nil
}

// P256 answers the key as an ECDSA public key on P-256, or the refusal of R-SIG-045's first row.
func P256(pub crypto.PublicKey) (*ecdsa.PublicKey, error) {
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		if k.Curve != elliptic.P256() {
			return nil, refuse(CheckKeyType, "the key is ECDSA on %s; an identity's key is ECDSA on P-256, the one type npm's client accepts", k.Curve.Params().Name)
		}
		return k, nil
	case nil:
		return nil, refuse(CheckKeyType, "no key")
	default:
		return nil, refuse(CheckKeyType, "the key is %s; an identity's key is ECDSA on P-256, the one type npm's client accepts", keyName(pub))
	}
}

func keyName(pub crypto.PublicKey) string {
	switch pub.(type) {
	case *rsa.PublicKey:
		return "RSA"
	case ed25519.PublicKey:
		return "Ed25519"
	}
	return fmt.Sprintf("%T", pub)
}

func checkLeaf(leaf *x509.Certificate, now time.Time) error {
	if leaf.BasicConstraintsValid && leaf.IsCA {
		return refuse(CheckLeafCA, "the leaf is a CA certificate; the identity needs an end-entity certificate")
	}
	if leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		return refuse(CheckKeyUsage, "the leaf's key usage lacks digitalSignature")
	}
	if !hasCodeSigning(leaf) {
		return refuse(CheckCodeSigning, "the leaf's extended key usage lacks codeSigning, which every verifier of an attestation requires")
	}
	if len(leaf.URIs) == 0 && len(leaf.EmailAddresses) == 0 {
		return refuse(CheckAlternativeName, "the leaf has no URI or e-mail address among its alternative names; cosign matches the identity against them")
	}
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return refuse(CheckValidity, "the leaf is valid from %s to %s, not now", leaf.NotBefore.UTC().Format(time.RFC3339), leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	if leaf.NotAfter.Sub(now) < MinRemaining {
		return refuse(CheckRemaining, "the leaf ends at %s, less than 24 hours from now", leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	return nil
}

func hasCodeSigning(c *x509.Certificate) bool {
	for _, u := range c.ExtKeyUsage {
		if u == x509.ExtKeyUsageCodeSigning {
			return true
		}
	}
	return false
}

// splitRoot answers the last self-signed certificate as the root and the others as intermediates.
func splitRoot(certs []*x509.Certificate) (*x509.Certificate, []*x509.Certificate) {
	ri := -1
	for i, c := range certs {
		if selfSigned(c) {
			ri = i
		}
	}
	if ri < 0 {
		return nil, certs
	}
	inter := make([]*x509.Certificate, 0, len(certs)-1)
	inter = append(inter, certs[:ri]...)
	inter = append(inter, certs[ri+1:]...)
	return certs[ri], inter
}

func selfSigned(c *x509.Certificate) bool {
	return bytes.Equal(c.RawSubject, c.RawIssuer) && c.CheckSignatureFrom(c) == nil
}

// buildPath verifies the leaf to the root for codeSigning and answers the path without the leaf: the intermediates it used, then the
// root.
func buildPath(leaf *x509.Certificate, inter []*x509.Certificate, root *x509.Certificate, now time.Time) ([]*x509.Certificate, error) {
	roots, pool := x509.NewCertPool(), x509.NewCertPool()
	roots.AddCert(root)
	for _, c := range inter {
		pool.AddCert(c)
	}
	paths, err := leaf.Verify(x509.VerifyOptions{
		Roots: roots, Intermediates: pool, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
	})
	if err != nil || len(paths) == 0 {
		return nil, refuse(CheckChainBuild, "the chain does not build from the leaf to its root for code signing: %v", err)
	}
	best := paths[0]
	for _, p := range paths[1:] {
		if len(p) < len(best) {
			best = p
		}
	}
	return best[1:], nil
}
