package shipped

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/signatures/anchors"
)

// TestR_SIG_017_NPMKeysConvertsBothShapes converts npm's keys documents to a public-key anchor's material (R-SIG-017): the TUF target's
// shape, where npm names each key twice (npm:signatures and npm:attestations), gives one entry per key with the later expiry, and none
// when one use has none; a registry's -/npm/v1/keys shape gives the same; the result passes the checks of a public-key anchor; an empty,
// malformed or keyless document is refused.
func TestR_SIG_017_NPMKeysConvertsBothShapes(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	raw := base64.StdEncoding.EncodeToString(der)
	sum := sha256.Sum256(der)
	id := "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	tufShape := fmt.Sprintf(`{"keys":[
	  {"keyId":%[2]q,"keyUsage":"npm:signatures","publicKey":{"rawBytes":%[1]q,"validFor":{"start":"1999-01-01T00:00:00Z","end":"2025-01-29T00:00:00Z"}}},
	  {"keyId":%[2]q,"keyUsage":"npm:attestations","publicKey":{"rawBytes":%[1]q,"validFor":{"start":"2022-12-01T00:00:00Z","end":"2025-02-01T00:00:00Z"}}}]}`, raw, id)
	registryShape := fmt.Sprintf(`{"keys":[{"expires":"2025-02-01T00:00:00Z","keyid":%[2]q,"keytype":"ed25519","key":%[1]q}]}`, raw, id)
	want := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	for name, doc := range map[string]string{"TUF": tufShape, "registry": registryShape} {
		m, err := NPMKeys([]byte(doc))
		if err != nil {
			t.Fatalf("%s shape: %v", name, err)
		}
		if len(m.Keys) != 1 || m.Keys[0].KeyID != id || m.Keys[0].Expires == nil || !m.Keys[0].Expires.Equal(want) {
			t.Fatalf("%s shape: %+v", name, m.Keys)
		}
		if _, err := anchors.Parse(anchors.KindPublicKey, anchors.Material{PublicKey: m}); err != nil {
			t.Fatalf("%s shape does not parse as an anchor: %v", name, err)
		}
	}
	open := fmt.Sprintf(`{"keys":[{"keyId":%[2]q,"publicKey":{"rawBytes":%[1]q,"validFor":{"end":"2025-01-29T00:00:00Z"}}},
	  {"keyId":%[2]q,"publicKey":{"rawBytes":%[1]q,"validFor":{}}}]}`, raw, id)
	if m, err := NPMKeys([]byte(open)); err != nil || m.Keys[0].Expires != nil {
		t.Fatalf("a key one use names without an end: %v, %+v", err, m)
	}
	for _, bad := range []string{``, `{`, `{"keys":[]}`, `{"keys":[{"keyid":"x","key":"%%%"}]}`} {
		if _, err := NPMKeys([]byte(bad)); err == nil {
			t.Fatalf("NPMKeys(%q) is accepted", bad)
		}
	}
}
