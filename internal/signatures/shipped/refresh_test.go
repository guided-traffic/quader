package shipped

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/signatures/shipped/tuftest"
)

// npmKeysDoc is a keys.json of npm's TUF shape with one ECDSA P-256 key made in the test.
func npmKeysDoc(t *testing.T) []byte {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(k.Public())
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	doc, err := json.Marshal(map[string]any{"keys": []any{map[string]any{
		"keyId": "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), "keyUsage": "npm:signatures",
		"publicKey": map[string]any{
			"rawBytes": base64.StdEncoding.EncodeToString(der), "keyDetails": "PKIX_ECDSA_P256_SHA_256",
			"validFor": map[string]any{"start": "2025-01-01T00:00:00Z"},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func refreshFrom(r *tuftest.Repo) (Refreshed, error) {
	return Refresh(context.Background(), RefreshOptions{URL: r.URL, Client: r.Client(), Root: r.Root()})
}

// TestR_SIG_017_RefreshReturnsANewerTrustedRoot refreshes from a TUF repository of the test (R-SIG-017): the first refresh returns the
// targets of version 1, and after the repository published a newer trusted root the next returns it with the higher version.
func TestR_SIG_017_RefreshReturnsANewerTrustedRoot(t *testing.T) {
	keys := npmKeysDoc(t)
	older, newer := tuftest.TrustedRoot(t, "older"), tuftest.TrustedRoot(t, "newer")
	repo := tuftest.New(t, map[string][]byte{TargetTrustedRoot: older, TargetNPMKeys: keys})
	got, err := refreshFrom(repo)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !bytes.Equal(got.TrustedRoot, older) || !bytes.Equal(got.NPMKeys, keys) || got.TUFVersion != 1 {
		t.Fatalf("first refresh: version %d, the older root %v", got.TUFVersion, bytes.Equal(got.TrustedRoot, older))
	}
	repo.Publish(map[string][]byte{TargetTrustedRoot: newer, TargetNPMKeys: keys})
	if got, err = refreshFrom(repo); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !bytes.Equal(got.TrustedRoot, newer) || got.TUFVersion != 2 {
		t.Fatalf("second refresh: version %d, the newer root %v", got.TUFVersion, bytes.Equal(got.TrustedRoot, newer))
	}
}

// TestR_SIG_017_RefreshRefusesWhatTheChainDoesNotSign holds the refusals of a refresh (R-SIG-017): a target file the targets metadata
// does not sign, a repository whose root is not the one the client starts from, and a signed trusted root sigstore-go cannot read.
func TestR_SIG_017_RefreshRefusesWhatTheChainDoesNotSign(t *testing.T) {
	keys := npmKeysDoc(t)
	for _, tc := range []struct {
		name  string
		setup func(r *tuftest.Repo) RefreshOptions
		want  string
	}{
		{"a target not signed by the chain", func(r *tuftest.Repo) RefreshOptions {
			r.Serve(TargetTrustedRoot, tuftest.TrustedRoot(t, "forged"))
			return RefreshOptions{URL: r.URL, Client: r.Client(), Root: r.Root()}
		}, "trusted_root.json"},
		{"metadata of another root", func(r *tuftest.Repo) RefreshOptions {
			other := tuftest.New(t, map[string][]byte{TargetTrustedRoot: tuftest.TrustedRoot(t, "other"), TargetNPMKeys: keys})
			return RefreshOptions{URL: r.URL, Client: r.Client(), Root: other.Root()}
		}, "TUF metadata"},
		{"a signed trusted root that does not parse", func(r *tuftest.Repo) RefreshOptions {
			r.Publish(map[string][]byte{TargetTrustedRoot: []byte(`{"mediaType":"nonsense"}`), TargetNPMKeys: keys})
			return RefreshOptions{URL: r.URL, Client: r.Client(), Root: r.Root()}
		}, "does not parse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := tuftest.New(t, map[string][]byte{TargetTrustedRoot: tuftest.TrustedRoot(t, "good"), TargetNPMKeys: keys})
			_, err := Refresh(context.Background(), tc.setup(repo))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refresh: %v, want an error naming %q", err, tc.want)
			}
		})
	}
}
