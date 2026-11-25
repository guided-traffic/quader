package oci

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
	"github.com/guided-traffic/quader/internal/formats/oci/rows"
)

func configJSON(t *testing.T, img rows.ImageConfig) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(rows.ConfigFacts{Kind: rows.ConfigImage, Image: &img})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// R-OCI-039: an annotation of the index wins over one of the first platform, which wins over a label of the first platform's
// configuration; the labels' facts and the configurations are content of the config blobs, named with their paths; a list's summary
// reads the annotations alone.
func TestR_OCI_039_AnnotationsWinOverLabelsAndLabelsAreContent(t *testing.T) {
	amd, arm, att, cfg := [32]byte{1}, [32]byte{2}, [32]byte{3}, [32]byte{4}
	tf := rows.TagFacts{
		VersionID: 7,
		Image:     rows.ManifestFacts{Annotations: json.RawMessage(`{"org.opencontainers.image.description":"From the index"}`)},
		Children: []rows.ManifestFacts{
			{
				Platform: "linux/amd64", Digest: amd, ConfigDigest: &cfg,
				Annotations: json.RawMessage(`{"org.opencontainers.image.description":"From the platform","org.opencontainers.image.source":"https://example.com/src"}`),
				Blobs:       json.RawMessage(`{"layers":[]}`),
				ConfigFacts: configJSON(t, rows.ImageConfig{Labels: []rows.Var{
					{Name: "org.opencontainers.image.licenses", Value: "MIT"},
					{Name: "org.opencontainers.image.url", Value: "javascript:alert(1)"},
					{Name: "org.opencontainers.image.source", Value: "https://example.com/label"},
				}, Env: []rows.Var{{Name: "TOKEN", Value: "secret"}}}),
			},
			{Platform: "linux/arm64", Digest: arm, ConfigDigest: &cfg},
			{Platform: rows.AttestationPlatform, Digest: att},
		},
	}
	var d formats.ViewData
	describeFacts(&d, "acme/app", tf, formats.ViewSummary|formats.ViewDetails|formats.ViewFacts)
	if d.Description != "From the index" || d.Metadata["repositoryUrl"] != "https://example.com/src" {
		t.Errorf("annotations: %q %v", d.Description, d.Metadata)
	}
	if d.Licenses != nil {
		t.Errorf("a licence of a label is no licence of the annotations: %v", d.Licenses)
	}
	labeled, _ := d.Metadata[memberLabeled].(map[string]string)
	if labeled["licenses"] != "MIT" || labeled["homepage"] != "javascript:alert(1)" || labeled["repositoryUrl"] != "" {
		t.Errorf("labels fill what the annotations left: %v", labeled)
	}
	configs, _ := d.Metadata[memberConfigs].(map[string]any)
	if len(configs) != 2 {
		t.Fatalf("configs of the two platforms: %v", configs)
	}
	if c, _ := configs[digestText(arm)].(*rows.ConfigFacts); c == nil || c.Unreadable != configNotStored {
		t.Errorf("a configuration the repository does not hold says so: %+v", c)
	}
	for i := range tf.Children {
		tf.Children[i].ConfigStored = tf.Children[i].Digest == arm
	}
	var stored formats.ViewData
	describeFacts(&stored, "acme/app", tf, formats.ViewSummary|formats.ViewDetails|formats.ViewFacts)
	if c, _ := stored.Metadata[memberConfigs].(map[string]any)[digestText(arm)].(*rows.ConfigFacts); c == nil || c.Unreadable != configNotRead {
		t.Errorf("a stored configuration the job did not read yet says so: %+v", c)
	}
	if !slices.Equal(d.ContentMetadata, []string{memberConfigs, memberLabeled}) || len(d.ContentPaths) != 2 ||
		d.ContentPaths[0] != BlobPath("acme/app", digestText(cfg)) {
		t.Errorf("content: %v %v", d.ContentMetadata, d.ContentPaths)
	}

	var summary formats.ViewData
	describeFacts(&summary, "acme/app", tf, formats.ViewSummary)
	if summary.Description != "From the index" || summary.Metadata != nil || summary.ContentPaths != nil {
		t.Errorf("a summary is the annotations' description alone: %+v", summary)
	}
	// R-OCI-041: a summary names the kind of the tag's manifest, so that a list offers a chart's commands for a chart.
	tf.Image.MediaType, tf.Image.ArtifactType = manifest.MediaTypeImageManifest, manifest.MediaTypeHelmConfig
	var chartSummary formats.ViewData
	describeFacts(&chartSummary, "acme/app", tf, formats.ViewSummary)
	if chartSummary.Metadata["kind"] != "helm-chart" || len(chartSummary.Metadata) != 1 || chartSummary.ContentPaths != nil {
		t.Errorf("a chart's summary: %+v", chartSummary)
	}
}

// R-OCI-041: the word for what a manifest holds.
func TestR_OCI_041_KindOfArtifact(t *testing.T) {
	for _, tc := range []struct{ mediaType, artifactType, want string }{
		{"application/vnd.oci.image.index.v1+json", "", "image"},
		{"application/vnd.oci.image.manifest.v1+json", "application/vnd.oci.image.config.v1+json", "image"},
		{"application/vnd.docker.distribution.manifest.v2+json", "application/vnd.docker.container.image.v1+json", "image"},
		{"application/vnd.oci.image.manifest.v1+json", "application/vnd.cncf.helm.config.v1+json", "helm-chart"},
		{"application/vnd.oci.image.manifest.v1+json", "application/vnd.dev.cosign.simplesigning.v1+json", "signature"},
		{"application/vnd.oci.image.manifest.v1+json", "application/spdx+json", "sbom"},
		{"application/vnd.oci.image.manifest.v1+json", "application/vnd.dev.sigstore.bundle.v0.3+json", "attestation"},
		{"application/vnd.oci.image.manifest.v1+json", "application/vnd.example.thing", "other"},
	} {
		if got := artifactKind(tc.mediaType, tc.artifactType); got != tc.want {
			t.Errorf("%s %s: %s, want %s", tc.mediaType, tc.artifactType, got, tc.want)
		}
	}
}
