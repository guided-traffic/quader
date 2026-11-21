package untrusted_test

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/openpgp"
)

// signDetached signs data with a key made by the test (Ed25519, milliseconds) and returns the armored signature and the key's
// fingerprint.
func signDetached(t *testing.T, data []byte) ([]byte, string) {
	t.Helper()
	secret, _, fp, err := openpgp.Generate("Release key", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	key, err := openpgp.ParseSecret(secret, nil, time.Now(), openpgp.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	sig, err := openpgp.DetachSign(data, []*openpgp.Key{key}, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return sig, fp
}

// TestR_SIG_031_APostedSignatureIsReadWithoutAKey: the facts of a detached signature (its issuer fingerprint, the prefix of the hash it
// signs) are read from the packet alone, and the prefix tells a signature of MANIFEST.json from one of other content. Unit,
// milliseconds, a key made by the test.
func TestR_SIG_031_APostedSignatureIsReadWithoutAKey(t *testing.T) {
	manifest := []byte(`{"collection_info":{"namespace":"acme","name":"tools","version":"1.0.0"}}`)
	sig, fp := signDetached(t, manifest)
	facts, err := untrusted.OpenPGPSignatureFacts(sig)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("%d signatures, want 1", len(facts))
	}
	f := facts[0]
	if f.Version != 4 || f.SigType != 0 {
		t.Errorf("version %d type %d, want a version 4 signature of a binary document", f.Version, f.SigType)
	}
	if got := hex.EncodeToString(f.Fingerprint); !strings.EqualFold(got, strings.ReplaceAll(fp, " ", "")) {
		t.Errorf("fingerprint %s, want %s", got, fp)
	}
	if checked, ok := f.MatchesHashPrefix(manifest); !checked || !ok {
		t.Errorf("the prefix of the signed content: checked %t, matches %t", checked, ok)
	}
	if checked, ok := f.MatchesHashPrefix([]byte(`{"collection_info":{"namespace":"evil"}}`)); checked && ok {
		t.Error("the prefix matches other content")
	}
	for _, bad := range [][]byte{nil, []byte("not a signature"), []byte("-----BEGIN PGP SIGNATURE-----\n\nAAAA\n-----END PGP SIGNATURE-----\n")} {
		if _, err := untrusted.OpenPGPSignatureFacts(bad); err == nil {
			t.Errorf("%q is read as a signature", bad)
		}
	}
}
