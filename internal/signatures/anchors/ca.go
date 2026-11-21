package anchors

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"regexp"
	"strings"
)

// minRSABits is the least RSA modulus of a certificate or a key of an anchor (R-SIG-015).
const minRSABits = 2048

// subjectAttribute reports whether k is an attribute a subject restriction may name (R-SIG-015, Q-SIG-30): O, OU, CN or C.
func subjectAttribute(k string) bool {
	switch k {
	case "O", "OU", "CN", "C":
		return true
	}
	return false
}

// checkCA checks a CA anchor's material (R-SIG-015): 1 to 16 PEM certificates in at most 64 KiB, each a CA certificate or a pinned
// end-entity certificate with an allowed key, none expired at save, the use, and the subject restriction.
func checkCA(m *CAMaterial, c checking) (*Parsed, error) {
	use, err := caUse(m.Use)
	if err != nil {
		return nil, err
	}
	certs, err := caCertificates(m.Certificates)
	if err != nil {
		return nil, err
	}
	out := &Parsed{CA: &ParsedCA{Certificates: certs, Use: use}}
	for i, cert := range certs {
		if err := checkCertificate(cert, i, use, c); err != nil {
			return nil, err
		}
		out.Describes = append(out.Describes, Fingerprint{
			Kind: "sha256", Value: sha256Hex(cert.Raw), Subject: cert.Subject.String(),
			NotAfter: cert.NotAfter.UTC(),
		})
	}
	if out.CA.Subject, err = caSubject(m.Subject); err != nil {
		return nil, err
	}
	if out.CA.SANPatterns, err = sanPatterns(m.SubjectAltNames); err != nil {
		return nil, err
	}
	return out, nil
}

// caUse is the use of a CA anchor: code signing when unset.
func caUse(use string) (string, error) {
	switch use {
	case "", UseCodeSigning:
		return UseCodeSigning, nil
	case UseTimestamping:
		return UseTimestamping, nil
	}
	return "", refuse("/ca/use", "the use is codeSigning or timestamping")
}

// caCertificates reads the PEM certificates: only CERTIFICATE blocks, nothing else but white space around them, 1 to 16 of them.
func caCertificates(text string) ([]*x509.Certificate, error) {
	const ptr = "/ca/certificates"
	if len(text) > maxCABytes {
		return nil, refuse(ptr, fmt.Sprintf("the certificates are at most %d KiB", maxCABytes>>10))
	}
	var certs []*x509.Certificate
	rest := []byte(text)
	for {
		block, next := pem.Decode(rest)
		if block == nil {
			break
		}
		if len(bytes.TrimSpace(beforeBlock(rest))) != 0 {
			return nil, refuse(ptr, "the certificates hold text that is no PEM block")
		}
		if block.Type != "CERTIFICATE" {
			return nil, refuse(ptr, fmt.Sprintf("a block of type %q is no certificate", block.Type))
		}
		if len(certs) == maxCACertificates {
			return nil, refuse(ptr, fmt.Sprintf("an anchor holds at most %d certificates", maxCACertificates))
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, refuse(ptr, fmt.Sprintf("certificate %d does not parse: %v", len(certs)+1, err))
		}
		certs = append(certs, cert)
		rest = next
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return nil, refuse(ptr, "the certificates hold text that is no PEM block")
	}
	if len(certs) == 0 {
		return nil, refuse(ptr, "at least one PEM certificate is needed")
	}
	return certs, nil
}

// beforeBlock is the text before the first PEM block of b.
func beforeBlock(b []byte) []byte {
	i := bytes.Index(b, []byte("-----BEGIN "))
	if i < 0 {
		return b
	}
	return b[:i]
}

// checkCertificate checks one certificate of a CA anchor; i is its index, for the reason.
func checkCertificate(cert *x509.Certificate, i int, use string, c checking) error {
	const ptr = "/ca/certificates"
	name := fmt.Sprintf("certificate %d (%s)", i+1, cert.Subject.CommonName)
	if reason := keyReason(cert.PublicKey); reason != "" {
		return refuse(ptr, name+" "+reason)
	}
	if cert.BasicConstraintsValid && cert.IsCA {
		if cert.KeyUsage != 0 && cert.KeyUsage&x509.KeyUsageCertSign == 0 {
			return refuse(ptr, name+" is a CA certificate that may not sign certificates")
		}
	} else if cert.KeyUsage != 0 && cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		return refuse(ptr, name+" is an end-entity certificate that may not make signatures")
	}
	if !allowsUse(cert, use) {
		return refuse(ptr, fmt.Sprintf("%s does not allow the use %s", name, use))
	}
	if c.atSave && c.now.After(cert.NotAfter) {
		return refuse(ptr, fmt.Sprintf("%s expired on %s", name, cert.NotAfter.UTC().Format("2006-01-02")))
	}
	return nil
}

// allowsUse reports whether a certificate's extended key usage allows the anchor's use: a certificate without the extension allows every
// use, one with it must name the use or any extended key usage. A certificate whose extension leaves the use out can never be part of a
// chain the use accepts, so the anchor would know nothing.
func allowsUse(cert *x509.Certificate, use string) bool {
	if len(cert.ExtKeyUsage) == 0 && len(cert.UnknownExtKeyUsage) == 0 {
		return true
	}
	want := x509.ExtKeyUsageCodeSigning
	if use == UseTimestamping {
		want = x509.ExtKeyUsageTimeStamping
	}
	for _, u := range cert.ExtKeyUsage {
		if u == want || u == x509.ExtKeyUsageAny {
			return true
		}
	}
	return false
}

// keyReason is why a public key is not allowed in an anchor, "" for an allowed one: RSA of at least 2048 bits, ECDSA over P-256, P-384
// or P-521, or Ed25519.
func keyReason(pub any) string {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		if k.N.BitLen() < minRSABits {
			return fmt.Sprintf("has an RSA key of %d bits, below %d", k.N.BitLen(), minRSABits)
		}
		return ""
	case *ecdsa.PublicKey:
		switch k.Curve {
		case elliptic.P256(), elliptic.P384(), elliptic.P521():
			return ""
		}
		return "has an ECDSA key on a curve other than P-256, P-384 and P-521"
	case ed25519.PublicKey:
		return ""
	}
	return "has a key of an algorithm other than RSA, ECDSA and Ed25519"
}

// caSubject checks a subject restriction: only the attributes O, OU, CN and C, each with a value.
func caSubject(subject map[string]string) (map[string]string, error) {
	if len(subject) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(subject))
	for k, v := range subject {
		if !subjectAttribute(k) {
			return nil, refuse("/ca/subject", fmt.Sprintf("the attribute %q cannot restrict the subject; O, OU, CN and C can", k))
		}
		if strings.TrimSpace(v) == "" || len(v) > maxSubjectValue {
			return nil, refuse("/ca/subject/"+k, fmt.Sprintf("the value is 1 to %d characters", maxSubjectValue))
		}
		out[k] = v
	}
	return out, nil
}

// sanPatterns compiles the patterns of the alternative names, anchored, at most 16 of at most 512 characters each.
func sanPatterns(patterns []string) ([]*regexp.Regexp, error) {
	if len(patterns) > maxSANPatterns {
		return nil, refuse("/ca/subjectAltNames", fmt.Sprintf("at most %d patterns", maxSANPatterns))
	}
	var out []*regexp.Regexp
	for i, p := range patterns {
		ptr := fmt.Sprintf("/ca/subjectAltNames/%d", i)
		if p == "" || len(p) > maxPatternLength {
			return nil, refuse(ptr, fmt.Sprintf("a pattern is 1 to %d characters", maxPatternLength))
		}
		re, err := anchored(p)
		if err != nil {
			return nil, refuse(ptr, "the pattern is no RE2 pattern: "+err.Error())
		}
		out = append(out, re)
	}
	return out, nil
}
