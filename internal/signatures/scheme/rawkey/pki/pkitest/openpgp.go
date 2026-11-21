package pkitest

import (
	"bytes"
	"crypto"
	"testing"
	"time"

	pgp "github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"

	qopenpgp "github.com/guided-traffic/quader/internal/openpgp"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
)

// ReleaseKey makes the release key with internal/openpgp.Generate (an EdDSA key of go-crypto) created at created: the secret key opened
// for signing, and its certificate as an anchor holds it.
func ReleaseKey(t testing.TB, uid string, created time.Time) (*qopenpgp.Key, anchors.OpenPGPCertificate) {
	t.Helper()
	secret, public, fp, err := qopenpgp.Generate(uid, created)
	if err != nil {
		t.Fatal(err)
	}
	key, err := qopenpgp.ParseSecret(secret, nil, time.Now(), qopenpgp.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return key, anchors.OpenPGPCertificate{Armored: public, Fingerprint: fp, UserID: uid, Created: key.Created}
}

// ExpiringKey makes an OpenPGP key of go-crypto created at created that expires lifetime later.
func ExpiringKey(t testing.TB, uid string, created time.Time, lifetime time.Duration) (*pgp.Entity, anchors.OpenPGPCertificate) {
	t.Helper()
	created = created.UTC().Truncate(time.Second)
	cfg := &packet.Config{
		Algorithm: packet.PubKeyAlgoEdDSA, Curve: packet.Curve25519, DefaultHash: crypto.SHA256,
		KeyLifetimeSecs: uint32(lifetime / time.Second), //nolint:gosec // a test's lifetime of hours to days
		Time:            func() time.Time { return created },
	}
	e, err := pgp.NewEntity(uid, "", "", cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.Subkeys = nil
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, "PGP PUBLIC KEY BLOCK", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Serialize(w); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return e, anchors.OpenPGPCertificate{Armored: buf.Bytes(), UserID: uid, Created: created, Expires: created.Add(lifetime)}
}

// DetachSign signs data with an entity at at (signature type binary, SHA-256) and answers the armored signature.
func DetachSign(t testing.TB, e *pgp.Entity, data []byte, at time.Time) []byte {
	t.Helper()
	var buf bytes.Buffer
	cfg := &packet.Config{DefaultHash: crypto.SHA256, Time: func() time.Time { return at }}
	if err := pgp.ArmoredDetachSign(&buf, e, bytes.NewReader(data), cfg); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
