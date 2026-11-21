package oci

import (
	"context"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
	"github.com/guided-traffic/quader/internal/formats/oci/rows"
)

// What the signature gate of a repository judges of an image (signatures.md R-SIG-063, Q-SIG-82): a manifest or an index by tag and by
// digest is an artifact, and a platform manifest passes on an index of the name in the repository that lists it, because `cosign sign`
// of a tag signs the index; a manifest the format recognised by its content as a carrier of signatures, attestations or SBOMs is an
// attachment the gate passes, so that a client can reach and verify an image's signatures; a blob (a layer, a config) is no artifact of
// its own. The recognition looks at the content, never at a tag's name: an image pushed under `sha256-<hex>.sig` is judged.

// imageLayerTypes are the media types of a runnable image's layers: a manifest with one of them is never an attachment.
func imageLayerTypes() []string {
	return []string{
		"application/vnd.oci.image.layer.v1.tar",
		"application/vnd.oci.image.layer.v1.tar+gzip",
		"application/vnd.oci.image.layer.v1.tar+zstd",
		"application/vnd.oci.image.layer.nondistributable.v1.tar",
		"application/vnd.oci.image.layer.nondistributable.v1.tar+gzip",
		"application/vnd.oci.image.layer.nondistributable.v1.tar+zstd",
		"application/vnd.docker.image.rootfs.diff.tar.gzip",
		"application/vnd.docker.image.rootfs.foreign.diff.tar.gzip",
	}
}

// attachmentTypes are the artifact, config and layer media types R-SIG-023 recognises as a signature, an attestation or an SBOM. The row
// "every other artifact type" of R-SIG-023 is not among them: a Helm chart pushed with a subject is an artifact.
func attachmentTypes() []string {
	return []string{
		artifactNotation, layerJWS, layerCOSE, artifactCosignSig, layerSimpleSigning, layerDSSE,
		"application/vnd.in-toto+json",
		"application/spdx+json", "text/spdx", "application/vnd.cyclonedx+json", "application/vnd.cyclonedx+xml", "application/vnd.syft+json",
	}
}

func isAttachmentType(t string) bool {
	if t == "" {
		return false
	}
	base, _, _ := strings.Cut(t, ";")
	base = strings.TrimSpace(base)
	if strings.HasPrefix(base, sigstoreBundlePrefix) {
		return true
	}
	for _, a := range attachmentTypes() {
		if base == a {
			return true
		}
	}
	return false
}

func isImageLayer(t string) bool {
	for _, l := range imageLayerTypes() {
		if t == l {
			return true
		}
	}
	return false
}

// IsAttachment reports whether a parsed manifest is a carrier of signatures, attestations or SBOMs by its content (R-SIG-063): an image
// manifest without an image layer whose artifact type, config media type or a layer media type is one R-SIG-023 recognises, or an index
// that lists at least one manifest and whose every descriptor carries such an artifact type (a referrers tag's index).
func IsAttachment(m *manifest.Manifest) bool {
	if m == nil {
		return false
	}
	if m.Config == nil && len(m.Layers) == 0 {
		if len(m.Manifests) == 0 {
			return false
		}
		for _, d := range m.Manifests {
			if !isAttachmentType(d.ArtifactType) {
				return false
			}
		}
		return true
	}
	for _, l := range m.Layers {
		if isImageLayer(l.MediaType) {
			return false
		}
	}
	if isAttachmentType(m.ArtifactType) || (m.Config != nil && isAttachmentType(m.Config.MediaType)) {
		return true
	}
	for _, l := range m.Layers {
		if isAttachmentType(l.MediaType) {
			return true
		}
	}
	return false
}

// Gate implements formats.SignatureAdapter (R-SIG-063). The facts of the manifests come from the format's data port in one read; a
// manifest that is not recorded is an artifact with nothing that covers it (fail closed).
func (signatureAdapter) Gate(ctx context.Context, h formats.Host, repo formats.RepoRef, items []formats.GateItem) ([]formats.GateSubject, error) {
	out := make([]formats.GateSubject, len(items))
	keys := make([]rows.ManifestKey, 0, len(items))
	at := make([]int, 0, len(items))
	for i, it := range items {
		name, kind, _, ok := splitLayout(it.Path)
		if !ok || (kind != dirManifests && kind != dirTags) {
			continue // a blob, the tags list, a path outside the layout: no artifact of its own
		}
		out[i].Kind = formats.GateArtifact
		keys = append(keys, rows.ManifestKey{Name: name, Digest: it.Blob})
		at = append(at, i)
	}
	if len(keys) == 0 {
		return out, nil
	}
	st, err := storeOf(h)
	if err != nil {
		return nil, err
	}
	facts, err := st.GateFacts(ctx, repo.ID, keys)
	if err != nil {
		return nil, err
	}
	for n, k := range keys {
		f, ok := facts[k]
		if !ok {
			continue
		}
		i := at[n]
		if f.Attachment {
			out[i].Kind = formats.GateAttachment
			continue
		}
		out[i].Covering = f.Indexes
	}
	return out, nil
}
