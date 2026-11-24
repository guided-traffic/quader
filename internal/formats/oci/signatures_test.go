package oci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// sigDesc is a descriptor of a manifest the tests build.
type sigDesc struct {
	MediaType    string            `json:"mediaType"`
	Digest       string            `json:"digest"`
	Size         int               `json:"size"`
	ArtifactType string            `json:"artifactType,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
}

func descOf(mediaType string, body []byte) sigDesc {
	return sigDesc{MediaType: mediaType, Digest: sha(body), Size: len(body)}
}

// signatureManifest builds an image manifest with an artifact type, a config, layers and a subject, as a signing client pushes it.
func signatureManifest(artifactType string, config sigDesc, subject *sigDesc, annotations map[string]string, layers ...sigDesc) []byte {
	m := map[string]any{"schemaVersion": 2, "mediaType": ctImage, "config": config, "layers": layers}
	if artifactType != "" {
		m["artifactType"] = artifactType
	}
	if subject != nil {
		m["subject"] = subject
	}
	if annotations != nil {
		m["annotations"] = annotations
	}
	b, _ := json.Marshal(m)
	return b
}

// The bytes the samples carry: a Notation envelope, a Sigstore bundle, cosign's simple-signing payloads, a DSSE envelope and an SPDX
// document. The detector reads none of them; the read of R-SIG-005 does.
var (
	sigEnvelope = []byte(`{"payload":"e30","protected":"e30","signature":"AA"}`)
	sigBundle   = []byte(`{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json"}`)
	sigPayload1 = []byte(`{"critical":{"type":"cosign container image signature"},"optional":null}`)
	sigPayload2 = []byte(`{"critical":{"type":"cosign container image signature"},"optional":{"n":2}}`)
	sigDSSE     = []byte(`{"payloadType":"application/vnd.in-toto+json","payload":"e30","signatures":[]}`)
	sigSPDX     = []byte(`{"spdxVersion":"SPDX-2.3"}`)
	emptyConfig = descOf("application/vnd.oci.empty.v1+json", seedConfig)
)

func seedSubject() *sigDesc {
	d := descOf(ctImage, seedManifest)
	return &d
}

func hexDigits(digest string) string { return strings.TrimPrefix(digest, "sha256:") }

func blobOf(b []byte) [32]byte { return sha256.Sum256(b) }

// signatureCases are check 18's samples of OCI, each pushed by the test's client through the registry protocol after the image app:1.
func signatureCases() []formattest.SignatureCase {
	subject := blobOf(seedManifest)
	push := func(ref string, body []byte, blobs ...[]byte) func(h *formattest.Host) error {
		return func(h *formattest.Host) error {
			if err := seed(h); err != nil {
				return err
			}
			for _, b := range blobs {
				h.PutItem(BlobPath("app", sha(b)), "application/octet-stream", b)
			}
			return pushManifest(h, "app/manifests/"+ref, body)
		}
	}
	// A signature follows its image (R-SIG-025): the DELETE of the image's manifest takes its referrers and its attachment tags.
	removeImage := func(h *formattest.Host) error { return deleteManifest(h, "app/manifests/"+seedDigest) }
	referrer := func(kind, artifact, layerType, scheme, role string, layer []byte, annotations map[string]string) formattest.SignatureCase {
		m := signatureManifest(artifact, emptyConfig, seedSubject(), annotations, descOf(layerType, layer))
		return formattest.SignatureCase{
			Kind: kind, Path: ManifestPath("app", sha(m)), Commit: push(sha(m), m, layer), RemoveSubject: removeImage,
			Want: []formats.SignatureCandidate{{
				Object: formats.SignatureLocator{Blob: blobOf(m)}, Subject: subject, Role: role, SchemeHint: scheme,
				Source: formats.SignatureSourceReferrer, Location: ManifestPath("app", sha(m)),
			}},
		}
	}
	sigTag := "sha256-" + hexDigits(seedDigest) + ".sig"
	sigManifest := signatureManifest("", descOf("application/vnd.oci.image.config.v1+json", seedConfig), nil, nil,
		descOf(layerSimpleSigning, sigPayload1), descOf(layerSimpleSigning, sigPayload2))
	attTag := "sha256-" + hexDigits(seedDigest) + ".att"
	dsse := descOf(layerDSSE, sigDSSE)
	dsse.Annotations = map[string]string{annotationPredicate: "https://slsa.dev/provenance/v1"}
	attManifest := signatureManifest("", descOf("application/vnd.oci.image.config.v1+json", seedConfig), nil, nil, dsse)
	return []formattest.SignatureCase{
		referrer("Notation", artifactNotation, layerJWS, formats.SignatureSchemeNotationJWS, formats.SignatureRolePublisher, sigEnvelope, nil),
		referrer("Sigstore bundle", "application/vnd.dev.sigstore.bundle.v0.3+json", "application/vnd.dev.sigstore.bundle.v0.3+json",
			formats.SignatureSchemeSigstoreBundle, formats.SignatureRolePublisher, sigBundle, map[string]string{annotationBundlePred: predicateCosignSign}),
		referrer("attestation or SBOM", "application/spdx+json", "application/spdx+json", formats.SignatureSchemeUnknown,
			formats.SignatureRoleAttestation, sigSPDX, nil),
		{
			Kind: "cosign signature", Path: TagPath("app", sigTag), Commit: push(sigTag, sigManifest, sigPayload1, sigPayload2), RemoveSubject: removeImage,
			Want: []formats.SignatureCandidate{
				{
					Object: formats.SignatureLocator{Blob: blobOf(sigManifest)}, Subject: subject, Role: formats.SignatureRolePublisher,
					SchemeHint: formats.SignatureSchemeCosignLegacy, Source: formats.SignatureSourceCosignTag, Location: TagPath("app", sigTag),
				},
				{
					Object: formats.SignatureLocator{Blob: blobOf(sigManifest)}, Part: 1, Subject: subject, Role: formats.SignatureRolePublisher,
					SchemeHint: formats.SignatureSchemeCosignLegacy, Source: formats.SignatureSourceCosignTag, Location: TagPath("app", sigTag),
				},
			},
		},
		{
			Kind: "provenance attestation", Path: TagPath("app", attTag), Commit: push(attTag, attManifest, sigDSSE), RemoveSubject: removeImage,
			Want: []formats.SignatureCandidate{{
				Object: formats.SignatureLocator{Blob: blobOf(attManifest)}, Subject: subject, Role: formats.SignatureRoleProvenance,
				SchemeHint: formats.SignatureSchemeCosignLegacy, Source: formats.SignatureSourceCosignTag, Location: TagPath("app", attTag),
			}},
		},
	}
}

// detectOCI runs the detector over bytes stored at p.
func detectOCI(t *testing.T, p string, body []byte) []formats.SignatureCandidate {
	t.Helper()
	sum := sha256.Sum256(body)
	got, err := detectSignatures(formats.SignatureEvent{
		Item: formats.ItemRef{ID: 1, Path: p}, Path: formats.Path{Path: p}, Digests: formats.Digests{SHA256: sum[:]},
		Content: formattest.ContentOf(body),
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestR_SIG_023_DetectNamesEveryKindOfOCI: a Notation referrer, a cosign 3 bundle referrer, an in-toto attestation referrer, an SPDX
// referrer, a referrers tag, a .sig with two layers (two candidates), an .att and BuildKit's attestation entry, with their roles,
// subjects and sources. Unit, milliseconds, manifests built by the test.
func TestR_SIG_023_DetectNamesEveryKindOfOCI(t *testing.T) {
	subject := blobOf(seedManifest)
	type want struct {
		part                 int
		scheme, role, source string
	}
	check := func(t *testing.T, got []formats.SignatureCandidate, s [32]byte, ws ...want) {
		t.Helper()
		if len(got) != len(ws) {
			t.Fatalf("%d candidates, want %d: %+v", len(got), len(ws), got)
		}
		for i, w := range ws {
			g := got[i]
			if g.Part != w.part || g.SchemeHint != w.scheme || g.Role != w.role || g.Source != w.source || g.Subject != s {
				t.Errorf("candidate %d = %+v, want %+v of %x", i, g, w, s[:4])
			}
		}
	}
	referrer := func(artifact, layerType string, layer []byte, annotations map[string]string, layerAnn map[string]string) []byte {
		l := descOf(layerType, layer)
		l.Annotations = layerAnn
		return signatureManifest(artifact, emptyConfig, seedSubject(), annotations, l)
	}
	t.Run("Notation referrer", func(t *testing.T) {
		m := referrer(artifactNotation, layerCOSE, []byte{0xd2}, nil, nil)
		check(t, detectOCI(t, ManifestPath("app", sha(m)), m), subject, want{0, formats.SignatureSchemeNotationCOSE, formats.SignatureRolePublisher, formats.SignatureSourceReferrer})
	})
	t.Run("cosign 3 bundle referrer: a signature and SLSA provenance", func(t *testing.T) {
		const bt = "application/vnd.dev.sigstore.bundle.v0.3+json"
		m := referrer(bt, bt, sigBundle, map[string]string{annotationBundlePred: predicateCosignSign}, nil)
		check(t, detectOCI(t, ManifestPath("app", sha(m)), m), subject, want{0, formats.SignatureSchemeSigstoreBundle, formats.SignatureRolePublisher, formats.SignatureSourceReferrer})
		p := referrer(bt, bt, sigBundle, map[string]string{annotationBundlePred: "https://slsa.dev/provenance/v1"}, nil)
		check(t, detectOCI(t, ManifestPath("app", sha(p)), p), subject, want{0, formats.SignatureSchemeSigstoreBundle, formats.SignatureRoleProvenance, formats.SignatureSourceReferrer})
	})
	t.Run("in-toto attestation referrer", func(t *testing.T) {
		m := referrer("application/vnd.dev.cosign.artifact.att.v1+json", layerDSSE, sigDSSE, nil, map[string]string{annotationPredicate: "https://cyclonedx.org/vex"})
		check(t, detectOCI(t, ManifestPath("app", sha(m)), m), subject, want{0, formats.SignatureSchemeCosignLegacy, formats.SignatureRoleAttestation, formats.SignatureSourceReferrer})
	})
	t.Run("SPDX referrer", func(t *testing.T) {
		m := referrer("application/spdx+json", "application/spdx+json", sigSPDX, nil, nil)
		check(t, detectOCI(t, ManifestPath("app", sha(m)), m), subject, want{0, formats.SignatureSchemeUnknown, formats.SignatureRoleAttestation, formats.SignatureSourceReferrer})
	})
	t.Run("referrers tag: each listed referrer, attached to the tag's digest", func(t *testing.T) {
		n := referrer(artifactNotation, layerJWS, sigEnvelope, nil, nil)
		s := referrer("application/spdx+json", "application/spdx+json", sigSPDX, nil, nil)
		nd, sd := descOf(ctImage, n), descOf(ctImage, s)
		nd.ArtifactType, sd.ArtifactType = artifactNotation, "application/spdx+json"
		idx, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": ctIndex, "manifests": []sigDesc{nd, sd}})
		got := detectOCI(t, TagPath("app", "sha256-"+hexDigits(seedDigest)), idx)
		check(t, got, subject, want{0, "", formats.SignatureRolePublisher, formats.SignatureSourceReferrersTag},
			want{0, formats.SignatureSchemeUnknown, formats.SignatureRoleAttestation, formats.SignatureSourceReferrersTag})
		if got[0].Object.Blob != blobOf(n) || got[1].Object.Blob != blobOf(s) {
			t.Errorf("the objects are not the listed manifests: %+v", got)
		}
	})
	t.Run(".sig with two layers names two signatures of one object", func(t *testing.T) {
		m := signatureManifest("", descOf("application/vnd.oci.image.config.v1+json", seedConfig), nil, nil,
			descOf(layerSimpleSigning, sigPayload1), descOf(layerSimpleSigning, sigPayload2))
		got := detectOCI(t, TagPath("app", "sha256-"+hexDigits(seedDigest)+".sig"), m)
		check(t, got, subject, want{0, formats.SignatureSchemeCosignLegacy, formats.SignatureRolePublisher, formats.SignatureSourceCosignTag},
			want{1, formats.SignatureSchemeCosignLegacy, formats.SignatureRolePublisher, formats.SignatureSourceCosignTag})
	})
	t.Run(".att with SLSA provenance and .sbom", func(t *testing.T) {
		l := descOf(layerDSSE, sigDSSE)
		l.Annotations = map[string]string{annotationPredicate: "https://slsa.dev/provenance/v0.2"}
		m := signatureManifest("", descOf("application/vnd.oci.image.config.v1+json", seedConfig), nil, nil, l)
		check(t, detectOCI(t, TagPath("app", "sha256-"+hexDigits(seedDigest)+".att"), m), subject,
			want{0, formats.SignatureSchemeCosignLegacy, formats.SignatureRoleProvenance, formats.SignatureSourceCosignTag})
		check(t, detectOCI(t, TagPath("app", "sha256-"+hexDigits(seedDigest)+".sbom"), m), subject,
			want{0, formats.SignatureSchemeUnknown, formats.SignatureRoleAttestation, formats.SignatureSourceCosignTag})
	})
	t.Run("BuildKit's attestation entry of an index", func(t *testing.T) {
		att := []byte(`{"schemaVersion":2}`)
		entry := descOf(ctImage, att)
		entry.Annotations = map[string]string{annotationRefType: refTypeAttestation, annotationRefDigest: seedDigest}
		idx, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": ctIndex, "manifests": []sigDesc{descOf(ctImage, seedManifest), entry}})
		p := ManifestPath("app", sha(idx))
		got := detectOCI(t, p, idx)
		check(t, got, subject, want{0, formats.SignatureSchemeUnknown, formats.SignatureRoleAttestation, formats.SignatureSourceIndexEntry})
		if got[0].Location != p+"#/manifests/1" || got[0].Object.Blob != blobOf(att) {
			t.Errorf("the entry is %+v", got[0])
		}
	})
	t.Run("an image, a tag of a release and a blob name nothing", func(t *testing.T) {
		for _, p := range []string{ManifestPath("app", seedDigest), TagPath("app", "1.0"), BlobPath("app", seedDigest)} {
			if got := detectOCI(t, p, seedManifest); len(got) != 0 {
				t.Errorf("%s names %+v", p, got)
			}
		}
	})
	t.Run("an attachment tag is recognised by its name (R-SIG-024)", func(t *testing.T) {
		h := hex.EncodeToString(subject[:])
		for tag, ok := range map[string]bool{
			"sha256-" + h: true, "sha256-" + h + ".sig": true, "sha256-" + h + ".att": true,
			"sha256-" + h + ".sbom": true, "sha256-" + h[:10]: false, "sha256-" + h + ".txt": false, "1.0": false,
		} {
			if IsAttachmentTag(tag) != ok {
				t.Errorf("IsAttachmentTag(%q) = %v", tag, !ok)
			}
		}
	})
}

// deleteManifest is a client's DELETE of a manifest; the registry answers 202.
func deleteManifest(h *formattest.Host, path string) error {
	resp, err := h.Serve(context.Background(), http.MethodDelete, path, nil, false)
	if err != nil {
		return err
	}
	if resp.Code != http.StatusAccepted {
		return fmt.Errorf("DELETE %s: %d %s", path, resp.Code, resp.Body)
	}
	return nil
}
