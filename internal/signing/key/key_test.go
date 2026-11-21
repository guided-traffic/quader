package key_test

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/secrets"
	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/key"
	"github.com/guided-traffic/quader/internal/signing/signingtest"
)

func ring(t *testing.T) *secrets.KeyRing {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "k1.key")
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(b)), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := secrets.Load(config.Secrets{EncryptionKeyFiles: []string{path}, ActiveKeyID: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestR_SIG_047_ASealedKeyOpensUnderItsOwnRowAlone: a key sealed for the row of id 3 opens for id 3 with the same public key, and not
// for id 4 (the AAD binds it to its row) nor with another ring; the sealed text holds no PEM.
func TestR_SIG_047_ASealedKeyOpensUnderItsOwnRowAlone(t *testing.T) {
	r := ring(t)
	k := signingtest.Key(t)
	sealed, err := key.Seal(r, 3, k)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "PRIVATE") || !secrets.IsSealed(sealed) {
		t.Fatalf("the sealed value %q", sealed[:16])
	}
	p, err := key.Open(r, 3, sealed)
	if err != nil || !p.Public().Equal(&k.PublicKey) {
		t.Fatalf("open: %v", err)
	}
	if _, err := key.Open(r, 4, sealed); !errors.Is(err, secrets.ErrAuthentication) {
		t.Errorf("another row's key opened it: %v", err)
	}
	if _, err := key.Open(ring(t), 3, sealed); err == nil {
		t.Error("another ring opened it")
	}
	if key.Column().String() != "signing_identities.sealed_key" {
		t.Errorf("the column is %s", key.Column())
	}
}

// TestR_SIG_046_TheKeyFileIsReadAsAKeyOnP256: an unencrypted PKCS#8 key file is read with the digest of its bytes; a missing file names
// the path and an RSA key the check, never the key.
func TestR_SIG_046_TheKeyFileIsReadAsAKeyOnP256(t *testing.T) {
	dir := t.TempDir()
	k := signingtest.Key(t)
	path := filepath.Join(dir, "tls.key")
	if err := os.WriteFile(path, signingtest.KeyPEM(t, k), 0o600); err != nil {
		t.Fatal(err)
	}
	p, d, err := key.ReadFile(path)
	if err != nil || !p.Public().Equal(&k.PublicKey) || d == ([32]byte{}) {
		t.Fatalf("read: %v", err)
	}
	if _, _, err := key.ReadFile(filepath.Join(dir, "none")); err == nil || !strings.Contains(err.Error(), "none") {
		t.Errorf("a missing file: %v", err)
	}
	if err := os.WriteFile(path, []byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := key.ReadFile(path); signing.RefusalOf(err) == nil || strings.Contains(err.Error(), "AAAA") {
		t.Errorf("a broken key: %v", err)
	}
}
