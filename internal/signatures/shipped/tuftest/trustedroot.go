package tuftest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"testing"
	"time"
)

// TrustedRoot makes a Sigstore trusted_root.json (media type version 0.1) of keys made in the test: one certificate authority with a
// self-signed ECDSA P-256 certificate named by name, and one transparency log with an ECDSA P-256 key, both valid from an hour ago. It
// is what an own trust root of a Sigstore anchor holds (R-SIG-015), and a newer trusted_root.json target of a test's repository.
func TrustedRoot(t testing.TB, name string) []byte {
	t.Helper()
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	caKey := newP256(t)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{name}, CommonName: name + " CA"},
		NotBefore:             start,
		NotAfter:              start.Add(lifetime),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, caKey.Public(), caKey)
	if err != nil {
		t.Fatalf("tuftest: CA certificate: %v", err)
	}
	logDER, err := x509.MarshalPKIXPublicKey(newP256(t).Public())
	if err != nil {
		t.Fatalf("tuftest: log key: %v", err)
	}
	logID := sha256.Sum256(logDER)
	validFor := map[string]any{"start": start.Format(time.RFC3339)}
	doc := map[string]any{
		"mediaType": "application/vnd.dev.sigstore.trustedroot+json;version=0.1",
		"tlogs": []any{map[string]any{
			"baseUrl":       "https://log.example.test",
			"hashAlgorithm": "SHA2_256",
			"publicKey":     map[string]any{"rawBytes": logDER, "keyDetails": "PKIX_ECDSA_P256_SHA_256", "validFor": validFor},
			"logId":         map[string]any{"keyId": logID[:]},
		}},
		"certificateAuthorities": []any{map[string]any{
			"subject":   map[string]any{"organization": name, "commonName": name + " CA"},
			"uri":       "https://ca.example.test",
			"certChain": map[string]any{"certificates": []any{map[string]any{"rawBytes": caDER}}},
			"validFor":  validFor,
		}},
		"ctlogs":               []any{},
		"timestampAuthorities": []any{},
	}
	b, err := json.MarshalIndent(doc, "", "  ") // []byte marshals as base64, the protobuf JSON of bytes
	if err != nil {
		t.Fatalf("tuftest: trusted root: %v", err)
	}
	return b
}

func newP256(t testing.TB) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("tuftest: key: %v", err)
	}
	return k
}
