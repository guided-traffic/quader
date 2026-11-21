package signing_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/guided-traffic/quader/internal/signing"
)

// TestR_SIG_050_ASignatureOfTheSignerVerifiesWithPacotesCheck: the signature SignNPM makes over <name>@<version>:<integrity> verifies
// with pacote's check written in Go (the base64 DER key in a PUBLIC KEY block, SHA-256, the base64 DER signature), carries the
// identity's keyid, and is the entry {"keyid", "sig"} of dist.signatures; another message, another key and a signature cut short do not
// verify. Unit, milliseconds, a P-256 key made by the test.
func TestR_SIG_050_ASignatureOfTheSignerVerifiesWithPacotesCheck(t *testing.T) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyid, err := signing.NPMKeyID(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(der)
	msg := signing.NPMMessage("@acme/ui", "1.2.0", "sha512-q8L7Yw==")
	if string(msg) != "@acme/ui@1.2.0:sha512-q8L7Yw==" {
		t.Fatalf("the message %q", msg)
	}
	sig, err := signing.SignNPM(k, keyid, msg)
	if err != nil {
		t.Fatal(err)
	}
	if sig.KeyID != keyid {
		t.Errorf("keyid %q, want %q", sig.KeyID, keyid)
	}
	if err := signing.VerifyNPM(key, msg, sig.Sig); err != nil {
		t.Errorf("pacote's check: %v", err)
	}
	var entry map[string]string
	if err := json.Unmarshal(sig.Entry(), &entry); err != nil || !reflect.DeepEqual(entry, map[string]string{"keyid": keyid, "sig": sig.Sig}) {
		t.Errorf("the entry %s", sig.Entry())
	}
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	otherDER, _ := x509.MarshalPKIXPublicKey(&other.PublicKey)
	for name, check := range map[string]func() error{
		"another message": func() error {
			return signing.VerifyNPM(key, signing.NPMMessage("@acme/ui", "1.2.1", "sha512-q8L7Yw=="), sig.Sig)
		},
		"another key":          func() error { return signing.VerifyNPM(base64.StdEncoding.EncodeToString(otherDER), msg, sig.Sig) },
		"a cut signature":      func() error { return signing.VerifyNPM(key, msg, sig.Sig[:len(sig.Sig)/2]) },
		"a key that is no key": func() error { return signing.VerifyNPM("bm8ta2V5", msg, sig.Sig) },
	} {
		if check() == nil {
			t.Errorf("%s verifies", name)
		}
	}
	if _, err := signing.SignNPM(nil, keyid, msg); err == nil {
		t.Error("a signature without a key")
	}
}

// TestR_SIG_048_TheDocumentsServeTheSignaturesOfTheListedIdentities: the identities whose npm signatures the documents serve are the
// current and every previous one while one is current, and none otherwise; pending, announced and retired never (R-SIG-045). Unit,
// milliseconds.
func TestR_SIG_048_TheDocumentsServeTheSignaturesOfTheListedIdentities(t *testing.T) {
	id := func(n int64, state string) signing.Identity { return signing.Identity{ID: n, State: state} }
	for name, tc := range map[string]struct {
		ids  []signing.Identity
		want []int64
	}{
		"none":                     {nil, []int64{}},
		"a current one":            {[]signing.Identity{id(3, signing.StateCurrent)}, []int64{3}},
		"rotated and announced":    {[]signing.Identity{id(5, signing.StateAnnounced), id(4, signing.StateCurrent), id(2, signing.StatePrevious), id(1, signing.StateRetired), id(6, signing.StatePending)}, []int64{2, 4}},
		"removed: previous alone":  {[]signing.Identity{id(2, signing.StatePrevious), id(4, signing.StatePrevious), id(5, signing.StateAnnounced)}, []int64{}},
		"retired beside a current": {[]signing.Identity{id(1, signing.StateRetired), id(7, signing.StateCurrent)}, []int64{7}},
	} {
		if got := signing.NPMServed(tc.ids); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}
