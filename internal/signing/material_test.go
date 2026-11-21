package signing_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/signingtest"
)

// TestR_SIG_045_EachRowOfTheTableHasAPassingAndAFailingSample holds the material of an identity to R-SIG-045's table (Q-SIG-53): a
// code-signing leaf for a P-256 key with a URI, its intermediate and its root pass, with the root last in the chain or in the CA input;
// each sample below fails exactly its row, named by the refusal's check. Unit, milliseconds, certificates made by the test.
func TestR_SIG_045_EachRowOfTheTableHasAPassingAndAFailingSample(t *testing.T) {
	ca := signingtest.NewCA(t, "Example")
	other := signingtest.NewCA(t, "Elsewhere")
	now := time.Now()
	leaf, key := ca.Issue(t, signingtest.Leaf{})

	t.Run("the passing pair: the root in the CA input, or last in the chain", func(t *testing.T) {
		for name, in := range map[string][2][]byte{
			"root in ca":       {ca.ChainPEM(leaf), ca.RootPEM()},
			"root last":        {signingtest.PEM(leaf, ca.Intermediate, ca.Root), nil},
			"intermediate too": {signingtest.PEM(leaf), signingtest.PEM(ca.Intermediate, ca.Root)},
		} {
			c, err := signing.ParseChain(in[0], in[1])
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			checked, err := c.Check(&key.PublicKey, now)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if len(checked.Chain) != 2 || !checked.Chain[1].Equal(ca.Root) || !checked.Chain[0].Equal(ca.Intermediate) {
				t.Errorf("%s: the chain served is %d certificates", name, len(checked.Chain))
			}
		}
	})

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rsaLeaf, _ := ca.Issue(t, signingtest.Leaf{Public: &rsaKey.PublicKey})
	p384Leaf, _ := ca.Issue(t, signingtest.Leaf{Public: &p384.PublicKey})
	noCode, noCodeKey := ca.Issue(t, signingtest.Leaf{NoCodeSigning: true})
	noDigital, noDigitalKey := ca.Issue(t, signingtest.Leaf{NoDigitalSignature: true})
	noNames, noNamesKey := ca.Issue(t, signingtest.Leaf{NoNames: true})
	expired, expiredKey := ca.Issue(t, signingtest.Leaf{NotBefore: now.Add(-48 * time.Hour), NotAfter: now.Add(-time.Hour)})
	short, shortKey := ca.Issue(t, signingtest.Leaf{NotAfter: now.Add(23 * time.Hour)})
	caLeaf, caLeafKey := ca.Issue(t, signingtest.Leaf{IsCA: true})
	emailOnly, emailKey := ca.Issue(t, signingtest.Leaf{Emails: []string{"signing@example.com"}})
	stranger := signingtest.Key(t)

	for _, c := range []struct {
		name  string
		chain []byte
		ca    []byte
		pub   any
		check string
	}{
		{"an RSA key", ca.ChainPEM(rsaLeaf), ca.RootPEM(), &rsaKey.PublicKey, signing.CheckKeyType},
		{"a P-384 key", ca.ChainPEM(p384Leaf), ca.RootPEM(), &p384.PublicKey, signing.CheckKeyType},
		{"a leaf without codeSigning", ca.ChainPEM(noCode), ca.RootPEM(), &noCodeKey.PublicKey, signing.CheckCodeSigning},
		{"a leaf without digitalSignature", ca.ChainPEM(noDigital), ca.RootPEM(), &noDigitalKey.PublicKey, signing.CheckKeyUsage},
		{"a leaf without an alternative name", ca.ChainPEM(noNames), ca.RootPEM(), &noNamesKey.PublicKey, signing.CheckAlternativeName},
		{"an expired leaf", ca.ChainPEM(expired), ca.RootPEM(), &expiredKey.PublicKey, signing.CheckValidity},
		{"a leaf with 23 hours left", ca.ChainPEM(short), ca.RootPEM(), &shortKey.PublicKey, signing.CheckRemaining},
		{"a CA certificate as the leaf", ca.ChainPEM(caLeaf), ca.RootPEM(), &caLeafKey.PublicKey, signing.CheckLeafCA},
		{"a chain without its root", ca.ChainPEM(leaf), nil, &key.PublicKey, signing.CheckRoot},
		{"a chain that does not build", ca.ChainPEM(leaf), other.RootPEM(), &key.PublicKey, signing.CheckChainBuild},
		{"a key that is not the leaf's", ca.ChainPEM(leaf), ca.RootPEM(), &stranger.PublicKey, signing.CheckPair},
	} {
		t.Run(c.name, func(t *testing.T) {
			chain, err := signing.ParseChain(c.chain, c.ca)
			if err == nil {
				_, err = chain.Check(c.pub, now)
			}
			r := signing.RefusalOf(err)
			if r == nil || r.Check != c.check {
				t.Fatalf("refusal %v, want %s", err, c.check)
			}
			if strings.Contains(r.Detail, "PRIVATE KEY") {
				t.Errorf("the refusal carries key material: %s", r.Detail)
			}
		})
	}

	t.Run("an e-mail address alone is an alternative name", func(t *testing.T) {
		chain, err := signing.ParseChain(ca.ChainPEM(emailOnly), ca.RootPEM())
		if err == nil {
			_, err = chain.Check(&emailKey.PublicKey, now)
		}
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("the bounds: nine certificates, a key in the chain, no PEM", func(t *testing.T) {
		nine := signingtest.PEM(leaf, ca.Intermediate, ca.Root)
		for range 6 {
			x, _ := other.Issue(t, signingtest.Leaf{})
			nine = append(nine, signingtest.PEM(x)...)
		}
		for name, c := range map[string]struct {
			chain []byte
			check string
		}{
			"nine certificates": {nine, signing.CheckBounds},
			"a private key":     {append(ca.ChainPEM(leaf), signingtest.KeyPEM(t, key)...), signing.CheckMalformed},
			"no PEM":            {[]byte("not a certificate"), signing.CheckMalformed},
			"nothing":           {nil, signing.CheckMalformed},
		} {
			_, err := signing.ParseChain(c.chain, nil)
			if r := signing.RefusalOf(err); r == nil || r.Check != c.check {
				t.Errorf("%s: %v, want %s", name, err, c.check)
			} else if strings.Contains(err.Error(), "MII") {
				t.Errorf("%s: the refusal repeats the input: %v", name, err)
			}
		}
	})
}

// TestR_SIG_047_AnImportedKeyIsReadInEveryFormItTakes: PKCS#8, SEC 1 and an encrypted PKCS#8 with its passphrase are read; a wrong
// passphrase, an RSA key, two blocks, OpenSSL's legacy encryption and PKCS#12-like garbage are refused, never with the key in the text.
func TestR_SIG_047_AnImportedKeyIsReadInEveryFormItTakes(t *testing.T) {
	k := signingtest.Key(t)
	pkcs8 := signingtest.KeyPEM(t, k)
	sec1DER, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	sec1 := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: sec1DER})
	encrypted := encryptedPKCS8(t, k, "correct horse")
	for name, in := range map[string][2]string{
		"PKCS#8":           {string(pkcs8), ""},
		"SEC 1":            {string(sec1), ""},
		"encrypted PKCS#8": {string(encrypted), "correct horse"},
	} {
		got, err := signing.ParseKey([]byte(in[0]), []byte(in[1]))
		if err != nil || !got.PublicKey.Equal(&k.PublicKey) {
			t.Errorf("%s: %v", name, err)
		}
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	legacy := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Headers: map[string]string{"Proc-Type": "4,ENCRYPTED"}, Bytes: []byte{1}})
	for name, c := range map[string]struct {
		text, pass string
		check      string
	}{
		"a wrong passphrase":  {string(encrypted), "wrong", signing.CheckMalformed},
		"no passphrase":       {string(encrypted), "", signing.CheckMalformed},
		"an RSA key":          {string(signingtest.KeyPEM(t, rsaKey)), "", signing.CheckKeyType},
		"two blocks":          {string(pkcs8) + string(pkcs8), "", signing.CheckMalformed},
		"legacy encryption":   {string(legacy), "", signing.CheckMalformed},
		"a PKCS#12 container": {"0\x82\x05\x00", "", signing.CheckMalformed},
	} {
		_, err := signing.ParseKey([]byte(c.text), []byte(c.pass))
		r := signing.RefusalOf(err)
		if r == nil || r.Check != c.check {
			t.Errorf("%s: %v, want %s", name, err, c.check)
			continue
		}
		if strings.Contains(err.Error(), "MIG") || strings.Contains(err.Error(), "PRIVATE KEY-") {
			t.Errorf("%s: the refusal repeats the key: %v", name, err)
		}
	}
}

// TestR_SIG_047_TheKeyRequestAsksForCodeSigningWithTheDefaultsOfTheForm: the request holds the key, the subject, the base URL as its
// URI without names of the caller, the key usage digitalSignature and codeSigning; members out of their bounds are refused at their
// pointer.
func TestR_SIG_047_TheKeyRequestAsksForCodeSigningWithTheDefaultsOfTheForm(t *testing.T) {
	req, err := signing.KeyRequest{Subject: signing.Subject{O: "Example GmbH", C: "DE"}}.Normalized("https://quader.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if req.Subject.CN != signing.DefaultCommonName || len(req.URIs) != 1 || req.URIs[0] != "https://quader.example.com" {
		t.Fatalf("defaults %+v", req)
	}
	k, err := signing.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	csrPEM, err := signing.CertificateRequest(k, req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := pem.Decode([]byte(csrPEM))
	csr, err := x509.ParseCertificateRequest(b.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		t.Fatalf("the request does not parse or verify: %v", err)
	}
	if pub, ok := csr.PublicKey.(*ecdsa.PublicKey); !ok || pub.Curve != elliptic.P256() || !pub.Equal(&k.PublicKey) {
		t.Errorf("the request's key is %T", csr.PublicKey)
	}
	if csr.Subject.CommonName != "Quader signing" || csr.Subject.Organization[0] != "Example GmbH" || csr.URIs[0].String() != "https://quader.example.com" {
		t.Errorf("subject %v, URIs %v", csr.Subject, csr.URIs)
	}
	ca := signingtest.NewCA(t, "Example")
	leaf := ca.IssueRequest(t, csrPEM)
	chain, err := signing.ParseChain(ca.ChainPEM(leaf), ca.RootPEM())
	if err == nil {
		_, err = chain.Check(&k.PublicKey, time.Now())
	}
	if err != nil {
		t.Fatalf("the CA's certificate for the request: %v", err)
	}
	var sawKU, sawEKU bool
	for _, e := range csr.Extensions {
		switch e.Id.String() {
		case "2.5.29.15":
			sawKU = e.Critical
		case "2.5.29.37":
			sawEKU = strings.Contains(string(e.Value), "\x2b\x06\x01\x05\x05\x07\x03\x03")
		}
	}
	if !sawKU || !sawEKU {
		t.Errorf("the request asks for digitalSignature %v, codeSigning %v", sawKU, sawEKU)
	}
	for name, bad := range map[string]signing.KeyRequest{
		"/subject/C":  {Subject: signing.Subject{C: "Germany"}},
		"/subject/CN": {Subject: signing.Subject{CN: strings.Repeat("x", 65)}},
		"/uris/0":     {URIs: []string{"not a uri"}},
		"/emails/0":   {Emails: []string{"nobody"}},
		"/uris":       {URIs: make([]string, 0)},
	} {
		base := "https://quader.example.com"
		if name == "/uris" {
			base = ""
		}
		_, err := bad.Normalized(base)
		if err == nil || !strings.Contains(fieldsOf(err), name) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
