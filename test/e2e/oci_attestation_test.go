//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/signing/signingtest"
)

// The one end-to-end case of Quader's receipt attestations (signatures.md R-SIG-052, R-SIG-060): the real cosign verifies the attestation
// of an image that the real Docker pushed, offline, against the trusted root the installation serves, with the command the client setup's
// tab verify shows. Everything the case does not show only a client can (the statement, the bundle against sigstore-go's verifier, when
// an attestation is made, its referrer and where it stands) is tested at the unit and the integration tier.

// attestationIdentity is the URI of the case's signing identity, what cosign names as --certificate-identity.
const attestationIdentity = "https://quader.e2e.test/attestations"

// TestOCIAttestation_R_SIG_052_CosignVerifiesQuadersAttestationOfAPushedImage: an identity of a CA of the test is imported in the admin
// area; an image docker pushes to the local repository gets Quader's attestation as a referrer, and cosign verify-attestation with the
// trusted root of /api/v1/signing/trusted-root.json, the identity, the issuer regexp '.*', the two --insecure-ignore flags and the
// predicate type exits 0 for it, and non-zero for another identity. The identity is removed again at the end, so that what the set pushes
// afterwards gets no new attestation. E2E, about 4 s and an image of a few hundred bytes; the set oci, no leg of its own.
func TestOCIAttestation_R_SIG_052_CosignVerifiesQuadersAttestationOfAPushedImage(t *testing.T) {
	fx := ociFx(t)
	host := ociRegistryHost(t)
	adm := fx.adm

	c := fx.client(t, 0, ociDocker, "attest")
	t.Run("R-SIG-052 cosign runs at the pinned version", func(t *testing.T) {
		image := nugetPinned(t, "E2E_OCICLIENTS_COSIGN_IMAGE")
		m := regexp.MustCompile(`:v([0-9][^@]*)@sha256:`).FindStringSubmatch(image)
		if m == nil {
			t.Fatalf("E2E_OCICLIENTS_COSIGN_IMAGE %q has no version tag", image)
		}
		if out := ociClientExec(t, 0, ociDocker, "cosign", "version"); !strings.Contains(out, "v"+m[1]) {
			t.Fatalf("cosign version printed %q, tools/versions.env pins %s", out, m[1])
		}
	})

	// The installation's identity: a rerun on the same cluster finds the one of the run before removed (previous), so the import is current
	// at once; one that was left current is removed first.
	if r := adm.do(http.MethodDelete, "/api/v1/signing/identity", nil); r.Status != http.StatusNoContent && r.Status != http.StatusNotFound && r.Status != http.StatusConflict {
		t.Fatalf("removing a current identity: %s", r)
	}
	ca := signingtest.NewCA(t, "E2E Corp")
	leaf, key := ca.Issue(t, signingtest.Leaf{URIs: []string{attestationIdentity}})
	imported := adm.do(http.MethodPost, "/api/v1/signing/identity/import", map[string]any{
		"key": string(signingtest.KeyPEM(t, key)), "chain": string(ca.ChainPEM(leaf)), "ca": string(ca.RootPEM()),
	}).want(t, "the import of the identity", http.StatusCreated)
	var id struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(imported.Body, &id); err != nil || id.State != "current" {
		t.Fatalf("the imported identity is not current: %s", imported)
	}
	t.Cleanup(func() { adm.do(http.MethodDelete, "/api/v1/signing/identity", nil) })

	name := fx.name("attested")
	ref := fx.image(t, ociLocalKey, name, "1")
	c.login(host)
	c.build(ref, "attested image "+fx.run)
	c.push(ref)
	digest := fx.digestOf(t, ociLocalKey, name, "1")
	image := fx.image(t, ociLocalKey, name, "") + "@" + digest

	root := newAPI(t).do(http.MethodGet, "/api/v1/signing/trusted-root.json", nil).want(t, "the trusted root", http.StatusOK)
	c.write("quader-trusted-root.json", root.Body)
	verify := func(identity string) (string, error) {
		return c.shell("SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt cosign verify-attestation" +
			" --trusted-root quader-trusted-root.json --certificate-identity " + identity +
			" --certificate-oidc-issuer-regexp '.*' --insecure-ignore-tlog --insecure-ignore-sct" +
			" --type https://github.com/guided-traffic/quader/blob/main/docs/spec/signatures.md#receipt-attestation-v1 " + image)
	}
	t.Run("R-SIG-052 cosign verify-attestation verifies the image's attestation against the trusted root Quader serves", func(t *testing.T) {
		// The job signing attests the image once its checks answered; the dev profile scans neither malware nor images, so at once.
		deadline := time.Now().Add(90 * time.Second)
		for {
			start := time.Now()
			out, err := verify(attestationIdentity)
			if err == nil {
				t.Logf("cosign verify-attestation (%s):\n%s", time.Since(start).Round(time.Millisecond), tailLines(out, 12))
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("cosign verify-attestation: %v\n%s", err, tailLines(out, 30))
			}
			time.Sleep(2 * time.Second)
		}
	})
	t.Run("R-SIG-052 another identity is refused", func(t *testing.T) {
		out, err := verify("https://other.example.com")
		if err == nil {
			t.Fatalf("cosign verified the attestation for another identity:\n%s", tailLines(out, 20))
		}
		t.Logf("for another identity cosign printed:\n%s", tailLines(out, 6))
	})
}
