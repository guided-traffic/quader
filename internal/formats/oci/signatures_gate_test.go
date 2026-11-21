package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
	"github.com/guided-traffic/quader/internal/formats/oci/rows"
)

// GateFacts implements the data port's read of the signature gate (R-SIG-063) over the twin's rows.
func (t *twin) GateFacts(_ context.Context, repoID int64, keys []rows.ManifestKey) (map[rows.ManifestKey]rows.GateFacts, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := map[rows.ManifestKey]rows.GateFacts{}
	for _, k := range keys {
		m, ok := t.manifests[mkey{repoID, k.Name, k.Digest}]
		if !ok {
			continue
		}
		f := rows.GateFacts{Attachment: m.Attachment}
		for mk, refs := range t.refs {
			if mk.repo != repoID || mk.name != k.Name {
				continue
			}
			for _, r := range refs {
				if r.Kind == rows.KindManifest && r.Target == k.Digest {
					f.Indexes = append(f.Indexes, mk.digest)
				}
			}
		}
		out[k] = f
	}
	return out, nil
}

// referrersTagIndex builds an index whose descriptors carry an artifact type, as a referrers tag holds one.
func referrersTagIndex(artifactType string, manifests ...[]byte) []byte {
	descs := make([]sigDesc, len(manifests))
	for i, m := range manifests {
		descs[i] = descOf(ctImage, m)
		descs[i].ArtifactType = artifactType
	}
	b, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": ctIndex, "manifests": descs})
	return b
}

func parsed(t *testing.T, body []byte, contentType string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse(body, contentType, New().parseLimits())
	if err != nil {
		t.Fatalf("%s: %v", body, err)
	}
	return m
}

// TestR_SIG_063_AnAttachmentIsRecognisedByItsContent: a Notation referrer, a cosign 3 bundle, an older .sig manifest with an image config,
// an SPDX referrer and a referrers tag's index are attachments; an image with a subject, a Helm chart with a subject and an index under a
// sha256-<hex> tag that lists images are not (Q-SIG-82). Unit, milliseconds, manifests built by the test.
func TestR_SIG_063_AnAttachmentIsRecognisedByItsContent(t *testing.T) {
	imageConfig := descOf("application/vnd.oci.image.config.v1+json", seedConfig)
	layer := descOf("application/vnd.oci.image.layer.v1.tar+gzip", seedLayer)
	notation := signatureManifest(artifactNotation, emptyConfig, seedSubject(), nil, descOf(layerJWS, sigEnvelope))
	bundle := signatureManifest("application/vnd.dev.sigstore.bundle.v0.3+json", emptyConfig, seedSubject(), nil,
		descOf("application/vnd.dev.sigstore.bundle.v0.3+json", sigBundle))
	oldSig := signatureManifest("", imageConfig, nil, nil, descOf(layerSimpleSigning, sigPayload1))
	spdx := signatureManifest("application/spdx+json", emptyConfig, seedSubject(), nil, descOf("application/spdx+json", sigSPDX))
	for name, body := range map[string][]byte{
		"a Notation referrer": notation, "a cosign 3 bundle": bundle, "an older .sig manifest": oldSig, "an SPDX referrer": spdx,
	} {
		if !IsAttachment(parsed(t, body, ctImage)) {
			t.Errorf("%s is not recognised as an attachment", name)
		}
	}
	if !IsAttachment(parsed(t, referrersTagIndex(artifactNotation, notation), ctIndex)) {
		t.Error("a referrers tag's index is not recognised as an attachment")
	}
	image := signatureManifest("", imageConfig, seedSubject(), nil, layer)
	helm := signatureManifest("", descOf("application/vnd.cncf.helm.config.v1+json", seedConfig), seedSubject(), nil,
		descOf("application/vnd.cncf.helm.chart.content.v1.tar+gzip", seedLayer))
	for name, body := range map[string][]byte{"an image with a subject": image, "a Helm chart with a subject": helm} {
		if IsAttachment(parsed(t, body, ctImage)) {
			t.Errorf("%s is recognised as an attachment", name)
		}
	}
	if IsAttachment(parsed(t, indexOf(seedDigest+" linux/amd64"), ctIndex)) {
		t.Error("an index that lists images is recognised as an attachment")
	}
}

// pushIndex pushes an index the way a client does.
func pushIndex(h *formattest.Host, path string, body []byte) error {
	resp, err := h.Serve(context.Background(), http.MethodPut, path, bytes.NewReader(body), false, func(r *http.Request) {
		r.Header.Set("Content-Type", ctIndex)
		r.ContentLength = int64(len(body))
	})
	if err != nil {
		return err
	}
	if resp.Code != http.StatusCreated {
		return fmt.Errorf("PUT %s: %d %s", path, resp.Code, resp.Body)
	}
	return nil
}

// gateCases is check 18's classification of OCI (R-SIG-063): a manifest by digest and by tag is an artifact, a platform manifest is
// covered by the index of the name that lists it, an attachment recognised by its content passes, a blob is none. Milliseconds.
func gateCases() []formattest.GateCase {
	idx := indexOf(seedDigest + " linux/amd64")
	notation := signatureManifest(artifactNotation, emptyConfig, seedSubject(), nil, descOf(layerJWS, sigEnvelope))
	withIndex := func(h *formattest.Host) error {
		if err := seed(h); err != nil {
			return err
		}
		return pushIndex(h, "app/manifests/multi", idx)
	}
	withReferrer := func(h *formattest.Host) error {
		if err := seed(h); err != nil {
			return err
		}
		h.PutItem(BlobPath("app", sha(sigEnvelope)), "application/octet-stream", sigEnvelope)
		return pushManifest(h, "app/manifests/"+sha(notation), notation)
	}
	return []formattest.GateCase{
		{Name: "a manifest by tag is an artifact", Seed: seed, Path: TagPath("app", "1"), Want: formats.GateArtifact},
		{Name: "a manifest by digest is an artifact", Seed: seed, Path: ManifestPath("app", seedDigest), Want: formats.GateArtifact, Attested: true},
		{
			Name: "a platform manifest is covered by its index", Seed: withIndex, Path: ManifestPath("app", seedDigest), Want: formats.GateArtifact,
			Covering: []string{ManifestPath("app", sha(idx))},
		},
		{Name: "a Notation referrer is an attachment", Seed: withReferrer, Path: ManifestPath("app", sha(notation)), Want: formats.GateAttachment},
		{Name: "a layer is none", Seed: seed, Path: BlobPath("app", sha(seedLayer)), Want: formats.GateNone},
	}
}
