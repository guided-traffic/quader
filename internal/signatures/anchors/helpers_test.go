package anchors

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"
)

// now is the time of saving in every test: a fixed instant, so that no test depends on the date; certificates are made around it.
var now = time.Date(2030, 6, 1, 12, 0, 0, 0, time.UTC)

// rsaKeys holds the two RSA keys the tests need, made once: generating RSA is the slow part of these tests.
var rsaKeys = struct {
	once       sync.Once
	big, small *rsa.PrivateKey
}{}

func rsaKey(t *testing.T, bits int) *rsa.PrivateKey {
	t.Helper()
	rsaKeys.once.Do(func() {
		var err error
		if rsaKeys.big, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			t.Fatal(err)
		}
		if rsaKeys.small, err = rsa.GenerateKey(rand.Reader, 1024); err != nil {
			t.Fatal(err)
		}
	})
	if bits == 1024 {
		return rsaKeys.small
	}
	return rsaKeys.big
}

func p256(t *testing.T) *ecdsa.PrivateKey { return ecKey(t, elliptic.P256()) }

func ecKey(t *testing.T, c elliptic.Curve) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(c, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func edKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// certOpts describe a self-signed certificate of a test.
type certOpts struct {
	key      crypto.Signer // default a P-256 key
	ca       bool
	usage    x509.KeyUsage // default certSign for a CA, digitalSignature otherwise
	eku      []x509.ExtKeyUsage
	notAfter time.Time // default a year after now
	cn       string
}

// cert makes a self-signed certificate and returns it as PEM.
func cert(t *testing.T, o certOpts) string {
	t.Helper()
	if o.key == nil {
		o.key = p256(t)
	}
	if o.usage == 0 {
		o.usage = x509.KeyUsageDigitalSignature
		if o.ca {
			o.usage = x509.KeyUsageCertSign
		}
	}
	if o.notAfter.IsZero() {
		o.notAfter = now.AddDate(1, 0, 0)
	}
	if o.cn == "" {
		o.cn = "Test signer"
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: o.cn, Organization: []string{"Example GmbH"}},
		NotBefore: now.AddDate(-1, 0, 0), NotAfter: o.notAfter, KeyUsage: o.usage, ExtKeyUsage: o.eku,
		BasicConstraintsValid: true, IsCA: o.ca,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, o.key.Public(), o.key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// pubPEM is a public key as a PKIX PEM block.
func pubPEM(t *testing.T, k crypto.Signer) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(k.Public())
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// privPEM is a private key as a PKCS#8 PEM block: what an anchor must never hold.
func privPEM(t *testing.T, k crypto.Signer) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// wantInvalid checks that err is an *Invalid with the code and pointer.
func wantInvalid(t *testing.T, err error, code, pointer string) {
	t.Helper()
	inv, ok := AsInvalid(err)
	if !ok {
		t.Fatalf("err = %v, want a refusal %s at %s", err, code, pointer)
	}
	if inv.Code != code || inv.Pointer != pointer || strings.TrimSpace(inv.Reason) == "" {
		t.Fatalf("refusal %s at %s (%q), want %s at %s", inv.Code, inv.Pointer, inv.Reason, code, pointer)
	}
}
