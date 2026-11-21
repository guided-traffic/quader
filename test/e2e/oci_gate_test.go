//go:build e2e

package e2e

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The one end-to-end case of signing in a gated OCI repository (signatures.md R-SIG-064, Q-SIG-211): the real cosign reads the image it
// signs before it pushes the signature, and the gate lets the image's pusher read it there. The integration tier shows the HTTP reads
// (test/integration/signatures, TestR_SIG_070_…); only the tool shows that it signs. What the gate decides and why is tested at the unit and
// the integration tier.

const (
	ociGatedKey   = "oci-e2e-gated"       // local, signatures.onlyVerified true
	ociGateReader = "e2e-oci-gate-reader" // reads oci-e2e-gated and nothing else
)

// TestOCIGate_R_SIG_064_ThePusherSignsWithCosignAndAReaderPullsOnlyThen: Docker pushes an unsigned image into a local repository whose
// gate is on; a reader's docker pull is refused with the gate's text; the pusher, with the credentials of the push, runs cosign sign
// --key by digest without a transparency log, and once Quader checked the signature against an anchor of the key pair the reader's pull
// is served. E2E, a few seconds on the set's fixture and an image of a few hundred bytes; the set oci, no leg of its own.
func TestOCIGate_R_SIG_064_ThePusherSignsWithCosignAndAReaderPullsOnlyThen(t *testing.T) {
	fx := ociFx(t)
	host := ociRegistryHost(t)
	adm := fx.adm

	adm.do(http.MethodPost, reposRoute, map[string]any{"key": ociGatedKey, "class": "local", "format": "oci"}).
		want(t, "create "+ociGatedKey, http.StatusCreated, http.StatusConflict)
	cur := adm.get(repoAPI(ociGatedKey)).want(t, "read "+ociGatedKey, http.StatusOK)
	adm.do(http.MethodPatch, repoAPI(ociGatedKey), map[string]any{"signatures": map[string]any{"onlyVerified": true, "anchors": []string{}}},
		"Content-Type", "application/merge-patch+json", "If-Match", cur.Header.Get("ETag")).want(t, "switch the gate of "+ociGatedKey+" on", http.StatusOK)
	ensureSA(t, adm, ociGateReader)
	ensurePolicy(t, adm, "e2e-oci-gated", []string{ociGatedKey},
		[2]any{"sa:" + ociPublisher, []string{"read", "deploy"}}, [2]any{"sa:" + ociGateReader, []string{"read"}})
	readerToken := freshSAToken(t, adm, ociGateReader, 3600, map[string]any{"inherit": true}).Token
	ociReadableOnEveryReplica(t, fx.token, ociGatedKey)
	ociReadableOnEveryReplica(t, readerToken, ociGatedKey)

	pusher := fx.client(t, 0, ociDocker, "gated")
	pusher.login(host)
	name := fx.name("gated")
	ref := fx.image(t, ociGatedKey, name, "1")
	pusher.build(ref, "gated image "+fx.run)
	pusher.push(ref)
	digest := fx.digestOf(t, ociGatedKey, name, "1") // the pusher's read through the gate (Q-SIG-211)
	image := fx.image(t, ociGatedKey, name, "") + "@" + digest

	reader := fx.client(t, 1, ociDocker, "gated-reader")
	reader.token = readerToken
	reader.login(host)
	t.Run("R-SIG-064 a reader is refused the unsigned image with the gate's text", func(t *testing.T) {
		out, err := reader.try("pull", ref)
		if err == nil {
			t.Fatalf("the reader pulled an unsigned image from a gated repository:\n%s", tailLines(out, 20))
		}
		if !strings.Contains(out, "Quader refused") || !strings.Contains(out, "no signature") {
			t.Errorf("the refusal does not carry the gate's text:\n%s", tailLines(out, 10))
		}
		t.Logf("docker pull as the reader printed:\n%s", tailLines(out, 4))
	})

	// The key pair and its anchor: cosign makes the pair in the pod, Quader trusts its public key in the installation's scope for this run.
	const cosignEnv = "COSIGN_PASSWORD= SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt "
	if out, err := pusher.shell(cosignEnv + "cosign generate-key-pair && cosign signing-config create --out nolog.json"); err != nil {
		t.Fatalf("cosign generate-key-pair, signing-config create: %v\n%s", err, tailLines(out, 20))
	}
	pub, err := pusher.shell("cat cosign.pub")
	if err != nil || !strings.Contains(pub, "BEGIN PUBLIC KEY") {
		t.Fatalf("cosign.pub: %v\n%s", err, pub)
	}
	var anchor struct {
		ID int64 `json:"id"`
	}
	adm.do(http.MethodPost, "/api/v1/signatures/anchors", map[string]any{
		"key": "e2e-cosign-" + fx.run, "title": "cosign key of the run " + fx.run, "kind": "public-key",
		"publicKey": map[string]any{"keys": []map[string]string{{"pem": pub[strings.Index(pub, "-----BEGIN"):]}}},
	}).want(t, "the anchor of the key pair", http.StatusCreated).JSON(t, &anchor)
	t.Cleanup(func() { adm.do(http.MethodDelete, "/api/v1/signatures/anchors/"+strconv.FormatInt(anchor.ID, 10), nil) })

	t.Run("R-SIG-064 the pusher signs the image with cosign through the gate", func(t *testing.T) {
		start := time.Now()
		out, err := pusher.shell(cosignEnv + "cosign sign --key cosign.key --signing-config nolog.json --yes " + image)
		if err != nil {
			t.Fatalf("cosign sign in a gated repository (Q-SIG-211): %v\n%s", err, tailLines(out, 30))
		}
		t.Logf("cosign sign (%s):\n%s", time.Since(start).Round(time.Millisecond), tailLines(out, 6))
	})
	t.Run("R-SIG-064 the reader pulls the image once Quader checked its signature", func(t *testing.T) {
		start := time.Now()
		deadline := start.Add(90 * time.Second)
		for {
			out, err := reader.try("pull", ref)
			if err == nil {
				t.Logf("served %s after the signature", time.Since(start).Round(time.Millisecond))
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("the signed image is still refused to the reader:\n%s", tailLines(out, 10))
			}
			time.Sleep(2 * time.Second)
		}
	})
}
