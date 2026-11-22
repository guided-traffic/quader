//go:build e2e

package e2e

import (
	"net/http"
	"strings"
	"testing"
)

// TestOCI_R_STOR_071_ClientsCheckALink: with the switch on and minSize 0, Docker and Podman pull a fresh one-file image whose blobs
// come through 307s to links of the object store; after the layer's object was overwritten in the bucket with other bytes of the
// same length, both refuse it (the digest in the path, R-STOR-071's rule of evidence for oci.blob.get). The links issued count each
// attempt, so the refusal is the client's.
func TestOCI_R_STOR_071_ClientsCheckALink(t *testing.T) {
	fx := ociFx(t)
	adm := fx.adm.withToken(adminToken(t))
	name := fx.name("links")
	ref := fx.image(t, ociLocalKey, name, "1")
	pub := fx.client(t, 0, ociDocker, "links-push")
	pub.login(ociRegistryHost(t))
	pub.build(ref, "signed links "+fx.run)
	pub.push(ref)

	var m struct {
		Config    struct{ Digest string }   `json:"config"`
		Layers    []struct{ Digest string } `json:"layers"`
		Manifests []struct {
			Digest   string
			Platform struct{ OS string } `json:"platform"`
		} `json:"manifests"`
	}
	manifest := func(reference string) {
		fx.registry(t).do(http.MethodGet, "/v2/"+repoRef(ociLocalKey)+"/"+name+"/manifests/"+reference, nil, "Accept", ociManifestAccept).want(t, "the manifest", 200).JSON(t, &m)
	}
	manifest("1")
	// Buildx pushes an index with the image and its attestation (platform unknown/unknown); the layer is the image's.
	for _, d := range m.Manifests {
		if d.Platform.OS != "unknown" {
			manifest(d.Digest)
			break
		}
	}
	if len(m.Layers) != 1 || !strings.HasPrefix(m.Layers[0].Digest, "sha256:") {
		t.Fatalf("the image has layers %+v", m.Layers)
	}
	layer := strings.TrimPrefix(m.Layers[0].Digest, "sha256:")
	markClean(t, layer)
	markClean(t, strings.TrimPrefix(m.Config.Digest, "sha256:"))
	linksOn(t)
	if link := waitForLink(t, adm, "/v2/"+repoRef(ociLocalKey)+"/"+name+"/blobs/"+m.Layers[0].Digest); !strings.Contains(link, "X-Amz-Signature=") {
		t.Fatalf("the 307 names no signed link: %q", link)
	}

	pull := func(t *testing.T, tool string, pod int, scenario string) (string, error) {
		t.Helper()
		c := fx.client(t, pod, tool, scenario)
		c.token = adm.token
		c.login(ociRegistryHost(t))
		_, _ = c.try("image", "rm", "--force", ref)
		return c.try("pull", ref)
	}
	tools := []string{ociDocker, ociPodman}
	for i, tool := range tools {
		t.Run(tool+" pulls the image through links", func(t *testing.T) {
			before := issuedLinks(t, "oci")
			if out, err := pull(t, tool, i+1, "links-good"); err != nil {
				t.Fatalf("%s pull: %v\n%s", tool, err, tailLines(out, 30))
			}
			if after := issuedLinks(t, "oci"); after <= before {
				t.Fatalf("%s pulled without a link (issued %v, before %v)", tool, after, before)
			}
		})
	}
	driver(t, "overwrite", layer)
	for i, tool := range tools {
		t.Run(tool+" refuses the layer's altered bytes", func(t *testing.T) {
			before := issuedLinks(t, "oci")
			out, err := pull(t, tool, i+1, "links-altered")
			if after := issuedLinks(t, "oci"); after <= before {
				t.Fatalf("%s asked for the layer without getting a link (issued %v, before %v):\n%s", tool, after, before, tailLines(out, 30))
			}
			if err == nil {
				t.Fatalf("%s pulled the altered layer: R-STOR-071's declaration of oci.blob.get does not hold for it\n%s", tool, tailLines(out, 30))
			}
			if low := strings.ToLower(out); !strings.Contains(low, "digest") && !strings.Contains(low, "checksum") {
				t.Errorf("%s failed, but not on the digest:\n%s", tool, tailLines(out, 30))
			}
		})
	}
}
