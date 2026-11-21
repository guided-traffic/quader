//go:build integration

package signing

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"testing"

	"github.com/youmark/pkcs8"

	"github.com/guided-traffic/quader/internal/signing/signingtest"
)

// fingerprint is the identity's fingerprint of a leaf: the lower-case hex SHA-256 of its DER.
func fingerprint(c *x509.Certificate) string { return fingerprintDER(c.Raw) }

func fingerprintDER(der []byte) string {
	s := sha256.Sum256(der)
	return hex.EncodeToString(s[:])
}

// rsaPair is a code-signing leaf of the CA for an RSA key and the key's PEM: a pair R-SIG-045 refuses.
func rsaPair(t *testing.T, ca *signingtest.CA) (*x509.Certificate, []byte) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := ca.Issue(t, signingtest.Leaf{Public: &k.PublicKey})
	return leaf, signingtest.KeyPEM(t, k)
}

// encryptedKey is a key as openssl pkcs8 -topk8 -v2 aes-256-cbc writes it.
func encryptedKey(t *testing.T, k *ecdsa.PrivateKey, pass string) []byte {
	t.Helper()
	der, err := pkcs8.MarshalPrivateKey(k, []byte(pass), nil)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: der})
}
