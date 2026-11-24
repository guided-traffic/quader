package oci

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
)

// readAttestationFixture reads a file Docker Hub served on 2026-11-24 (testdata/attestation/README.md).
func readAttestationFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "attestation", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// R-VULN-033, R-SIG-063, Q-VULN-269: Docker's attestation manifest, as Docker Hub served it for library/ubuntu:26.10, is recorded as an
// attachment by its content, so the image scanning's discovery leaves it out, while the platform image beside it is an image. The index
// names each attestation with Docker's annotation and the platform unknown/unknown; the attestation has no subject and an image's config
// media type, which a selection by media types alone took for an image, and an in-toto layer that is no tar stream. Unit, milliseconds,
// the recorded bytes.
func TestR_VULN_033_DockersAttestationManifestIsRecordedAsAnAttachment(t *testing.T) {
	lim := New().parseLimits()
	indexBytes := readAttestationFixture(t, "ubuntu-26.10-index.json")
	idx, err := manifest.Parse(indexBytes, ctIndex, lim)
	if err != nil {
		t.Fatal(err)
	}
	if idx.Digest != "sha256:29ac5e68db830ab61b7d77c1ffdb78c73f0631de88a5d77bf4399868f64f38dd" {
		t.Fatalf("the recorded index is %s", idx.Digest)
	}
	// The parser keeps no descriptor annotations; the mirror's reader of Docker's annotation reads them from the recorded index.
	attests, err := attestationRefs(indexBytes) // attestation digest -> the image it attests
	if err != nil {
		t.Fatal(err)
	}
	if len(attests) != 6 {
		t.Fatalf("%d attestation entries in the index, want one per platform of the six", len(attests))
	}
	for _, r := range idx.References() {
		if _, ok := attests[r.Digest]; ok && r.Platform != "unknown/unknown" {
			t.Errorf("the attestation %s has the platform %q", r.Digest, r.Platform)
		}
	}

	attBytes := readAttestationFixture(t, "ubuntu-26.10-amd64-attestation.json")
	att, err := manifest.Parse(attBytes, ctImage, lim)
	if err != nil {
		t.Fatal(err)
	}
	image, ok := attests[att.Digest]
	if !ok {
		t.Fatalf("the recorded attestation %s is not an attestation entry of the index", att.Digest)
	}
	if att.Subject != nil || att.ArtifactType != "" || att.EffectiveArtifactType() != "application/vnd.oci.image.config.v1+json" ||
		len(att.Layers) != 1 || att.Layers[0].MediaType != "application/vnd.in-toto+json" {
		t.Fatalf("the recorded attestation is not the shape the selection by media types took for an image: %s", attBytes)
	}
	if row := manifestInput(1, "library/ubuntu", att); !row.Attachment || row.Subject != nil {
		t.Errorf("the attestation's row: attachment %v, subject %v; want an attachment without a subject", row.Attachment, row.Subject)
	}
	cfg := readAttestationFixture(t, "ubuntu-26.10-amd64-attestation-config.json")
	if got := digestOf(sha256Sum(cfg)); got != att.Config.Digest {
		t.Errorf("the recorded config is %s, the attestation names %s", got, att.Config.Digest)
	}

	imgBytes := readAttestationFixture(t, "ubuntu-26.10-amd64.json")
	img, err := manifest.Parse(imgBytes, ctImage, lim)
	if err != nil {
		t.Fatal(err)
	}
	if img.Digest != image {
		t.Fatalf("the recorded image %s is not the one the attestation attests (%s)", img.Digest, image)
	}
	if row := manifestInput(1, "library/ubuntu", img); row.Attachment {
		t.Error("the platform image is recorded as an attachment")
	}
}

func sha256Sum(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}
