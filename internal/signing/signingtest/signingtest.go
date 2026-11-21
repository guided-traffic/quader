// Package signingtest makes the company CA of the tests of Quader's signing identity in process (signatures.md R-SIG-060): a root, an
// intermediate and code-signing leaves for ECDSA P-256 keys with crypto/x509, as R-SIG-042 makes the company CA of stage 1, and the
// failing samples of R-SIG-045's table. One of the CAs a test makes stands for a signing service's own CA (the owner's three ways).
package signingtest

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

var serial atomic.Int64

// CA is a root and an intermediate that issue code-signing leaves.
type CA struct {
	Root, Intermediate       *x509.Certificate
	rootKey, intermediateKey *ecdsa.PrivateKey
}

// NewCA makes a root of the organization and an intermediate under it, valid from an hour ago for ten years.
func NewCA(t testing.TB, org string) *CA {
	t.Helper()
	rk, ik := Key(t), Key(t)
	now := time.Now()
	root := issue(t, &x509.Certificate{
		Subject: pkix.Name{CommonName: org + " Root", Organization: []string{org}}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(10, 0, 0),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}, nil, &rk.PublicKey, rk)
	inter := issue(t, &x509.Certificate{
		Subject: pkix.Name{CommonName: org + " Code Signing CA", Organization: []string{org}}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(5, 0, 0),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, MaxPathLenZero: true,
	}, root, &ik.PublicKey, rk)
	return &CA{Root: root, Intermediate: inter, rootKey: rk, intermediateKey: ik}
}

// Key makes an ECDSA P-256 key.
func Key(t testing.TB) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// Leaf is what a leaf the CA issues looks like; the zero value of a member is the passing default: a code-signing end-entity certificate
// for the URI https://quader.example.com, valid from an hour ago for 90 days.
type Leaf struct {
	// Public is the leaf's key; nil makes an ECDSA P-256 key, answered by Issue.
	Public crypto.PublicKey
	// NoCodeSigning leaves codeSigning out of the extended key usage; NoDigitalSignature leaves digitalSignature out of the key usage.
	NoCodeSigning, NoDigitalSignature bool
	// NoNames leaves the alternative names out; URIs and Emails replace the default URI.
	NoNames bool
	URIs    []string
	Emails  []string
	// IsCA makes the leaf a CA certificate.
	IsCA bool
	// NotBefore and NotAfter replace the validity.
	NotBefore, NotAfter time.Time
	// Subject replaces the common name Quader signing.
	CN string
}

// Issue issues a leaf from the intermediate; key is the leaf's private key when Public was nil.
func (ca *CA) Issue(t testing.TB, l Leaf) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	var key *ecdsa.PrivateKey
	pub := l.Public
	if pub == nil {
		key = Key(t)
		pub = &key.PublicKey
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		Subject:   pkix.Name{CommonName: orDefault(l.CN, "Quader signing"), Organization: []string{"Example GmbH"}},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(90 * 24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning}, BasicConstraintsValid: true,
		EmailAddresses: l.Emails,
	}
	if !l.NotBefore.IsZero() {
		tmpl.NotBefore = l.NotBefore
	}
	if !l.NotAfter.IsZero() {
		tmpl.NotAfter = l.NotAfter
	}
	if l.NoCodeSigning {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	}
	if l.NoDigitalSignature {
		tmpl.KeyUsage = x509.KeyUsageKeyEncipherment
	}
	if l.IsCA {
		tmpl.IsCA, tmpl.KeyUsage = true, tmpl.KeyUsage|x509.KeyUsageCertSign
	}
	uris := l.URIs
	if len(uris) == 0 && len(l.Emails) == 0 {
		uris = []string{"https://quader.example.com"}
	}
	if !l.NoNames {
		for _, u := range uris {
			p, err := url.Parse(u)
			if err != nil {
				t.Fatal(err)
			}
			tmpl.URIs = append(tmpl.URIs, p)
		}
	} else {
		tmpl.EmailAddresses = nil
	}
	return issue(t, tmpl, ca.Intermediate, pub, ca.intermediateKey), key
}

// IssueRequest issues a leaf for a PKCS#10 request (PEM), as the company's CA does for Quader's key request: the request's subject,
// alternative names and key, codeSigning and digitalSignature, 90 days.
func (ca *CA) IssueRequest(t testing.TB, csrPEM string) *x509.Certificate {
	t.Helper()
	b, _ := pem.Decode([]byte(csrPEM))
	if b == nil || b.Type != "CERTIFICATE REQUEST" {
		t.Fatalf("no certificate request: %q", csrPEM)
	}
	csr, err := x509.ParseCertificateRequest(b.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Fatalf("the request's signature: %v", err)
	}
	now := time.Now()
	return issue(t, &x509.Certificate{
		Subject: csr.Subject, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(90 * 24 * time.Hour), URIs: csr.URIs,
		EmailAddresses: csr.EmailAddresses, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		BasicConstraintsValid: true,
	}, ca.Intermediate, csr.PublicKey, ca.intermediateKey)
}

// ChainPEM is the leaf and the intermediate as PEM, as tls.crt holds them; RootPEM the root, as ca.crt holds it.
func (ca *CA) ChainPEM(leaf *x509.Certificate) []byte { return PEM(leaf, ca.Intermediate) }

// RootPEM is the root as PEM.
func (ca *CA) RootPEM() []byte { return PEM(ca.Root) }

// PEM encodes certificates.
func PEM(certs ...*x509.Certificate) []byte {
	var b bytes.Buffer
	for _, c := range certs {
		_ = pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
	}
	return b.Bytes()
}

// KeyPEM encodes a key as unencrypted PKCS#8.
func KeyPEM(t testing.TB, k crypto.Signer) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func issue(t testing.TB, tmpl, parent *x509.Certificate, pub crypto.PublicKey, signer crypto.Signer) *x509.Certificate {
	t.Helper()
	tmpl.SerialNumber = big.NewInt(1000 + serial.Add(1))
	if parent == nil {
		parent = tmpl
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, signer)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
