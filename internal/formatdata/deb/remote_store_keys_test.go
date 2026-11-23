package deb

import (
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/openpgp"
)

func publicKey(t *testing.T) (armored, fingerprint string) {
	t.Helper()
	_, public, _, err := openpgp.Generate("test", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	certs, err := openpgp.ParseStoredCertificates(public, openpgp.Limits{})
	if err != nil || len(certs) != 1 {
		t.Fatalf("the public certificate: %v", err)
	}
	return string(public), certs[0].Fingerprint
}

// Q-DEB-133: a verification recorded after it was made (the remote job's re-verification) holds only while its signer is a key of the
// set the recording transaction reads.
func TestQ_DEB_133_ASignerHoldsWhileItsKeyIsInTheSet(t *testing.T) {
	a, fa := publicKey(t)
	b, fb := publicKey(t)
	for _, c := range []struct {
		keys, signer string
		want         bool
	}{
		{a, fa, true}, {a + "\n" + b, fb, true}, {b, fa, false}, {"", fa, false}, {"", "", true}, {b, "", true}, {"not a key", fa, false},
	} {
		if got := signerIsCurrent(c.keys, c.signer); got != c.want {
			t.Errorf("signer %q under %d bytes of keys = %v, want %v", c.signer, len(c.keys), got, c.want)
		}
	}
}
