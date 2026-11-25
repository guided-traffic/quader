package oci

import (
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
	"github.com/guided-traffic/quader/internal/formats/oci/rows"
)

// An image's facts on the package screens (R-OCI-039 to R-OCI-041, Q-OCI-271 to Q-OCI-274).
//
// The description, the licence and the links of an image are the keys org.opencontainers.image.* of the image specification, read from
// the annotations of the tag's manifest, then of the first platform's manifest (an index is often annotated per platform only), then
// from the labels of the first platform's configuration: an annotation wins over a label. The annotations are part of the manifest, which
// every caller who reads the tag reads; the labels and the rest of the configuration are the config blob's content and reach only a
// caller who may read that blob (ViewData.ContentMetadata), so a list and a search show the annotations' description alone.

// imageKey is a key of the image specification the screens show and the member of the metadata it becomes.
type imageKey struct{ key, member string }

// imageKeys are the keys of the image specification the screens show, the licences last: the names of the links are those every format's
// Links block reads (homepage, documentationUrl, repositoryUrl).
func imageKeys() []imageKey {
	return []imageKey{
		{"org.opencontainers.image.description", "description"},
		{"org.opencontainers.image.title", "title"},
		{"org.opencontainers.image.url", "homepage"},
		{"org.opencontainers.image.documentation", "documentationUrl"},
		{"org.opencontainers.image.source", "repositoryUrl"},
		{"org.opencontainers.image.revision", "revision"},
		{"org.opencontainers.image.created", "created"},
		{"org.opencontainers.image.version", "imageVersion"},
		{"org.opencontainers.image.vendor", "vendor"},
		{"org.opencontainers.image.authors", "authors"},
		{"org.opencontainers.image.base.name", "baseName"},
		{"org.opencontainers.image.base.digest", "baseDigest"},
		{"org.opencontainers.image.licenses", "licenses"},
	}
}

// maxFactBytes bounds a fact taken from an annotation or a label: a description longer than a screen shows is cut there.
const maxFactBytes = 4 << 10

// The members of a version's metadata that are read from the config blob's content (ViewData.ContentMetadata).
const (
	memberConfigs = "configs"
	memberLabeled = "labeled"
)

// configNotRead is the reason of a configuration whose blob no fetch read yet (a remote's manifest is fetched before its config).
const configNotRead = "not-read"

// annotationsOf reads the stored annotations of a manifest row; none for a row without.
func annotationsOf(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]string
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

// firstPlatform is the child of an index whose facts stand for the image: the first in platform order that is an image of a platform
// (an attestation is none). nil for an image manifest and for an index that holds none of its platforms.
func firstPlatform(tf rows.TagFacts) *rows.ManifestFacts {
	for i := range tf.Children {
		if tf.Children[i].Platform != rows.AttestationPlatform {
			return &tf.Children[i]
		}
	}
	return nil
}

// clipFact bounds a fact at maxFactBytes, at a character boundary.
func clipFact(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxFactBytes {
		return s
	}
	end := maxFactBytes
	for end > 0 && s[end]&0xC0 == 0x80 {
		end--
	}
	return s[:end]
}

// annotationFacts are the facts of the image from the annotations: the tag's manifest's first, then the first platform's.
func annotationFacts(tf rows.TagFacts) map[string]string {
	sources := []map[string]string{annotationsOf(tf.Image.Annotations)}
	if p := firstPlatform(tf); p != nil {
		sources = append(sources, annotationsOf(p.Annotations))
	}
	out := map[string]string{}
	for _, k := range imageKeys() {
		for _, src := range sources {
			if v := clipFact(src[k.key]); v != "" {
				out[k.member] = v
				break
			}
		}
	}
	return out
}

// imageConfigOf decodes a stored configuration; nil for none or one that is not an image's.
func imageConfigOf(raw json.RawMessage) *rows.ConfigFacts {
	if len(raw) == 0 {
		return nil
	}
	var cf rows.ConfigFacts
	if json.Unmarshal(raw, &cf) != nil {
		return nil
	}
	return &cf
}

// labelFacts are the facts of the image from the labels of the first platform's configuration (or the image's own), for the keys the
// annotations did not give.
func labelFacts(tf rows.TagFacts, given map[string]string) map[string]string {
	src := &tf.Image
	if p := firstPlatform(tf); p != nil {
		src = p
	}
	cf := imageConfigOf(src.ConfigFacts)
	if cf == nil || cf.Image == nil {
		return nil
	}
	labels := make(map[string]string, len(cf.Image.Labels))
	for _, l := range cf.Image.Labels {
		labels[l.Name] = l.Value
	}
	out := map[string]string{}
	for _, k := range imageKeys() {
		if _, ok := given[k.member]; ok {
			continue
		}
		if v := clipFact(labels[k.key]); v != "" {
			out[k.member] = v
		}
	}
	return out
}

// artifactKind is the word for what a manifest holds (R-OCI-041): an image, a Helm chart, a signature, an SBOM, an attestation, or
// another artifact. It reads the media type and the artifact type (the manifest's own, else its config's).
func artifactKind(mediaType, artifactType string) string {
	switch mediaType {
	case manifest.MediaTypeImageIndex, manifest.MediaTypeDockerList:
		if artifactType == "" {
			return "image"
		}
	}
	switch {
	case artifactType == "" || manifest.ConfigKind(artifactType) == rows.ConfigImage:
		return "image"
	case artifactType == "application/vnd.cncf.helm.config.v1+json":
		return "helm-chart"
	case artifactType == "application/vnd.dev.cosign.simplesigning.v1+json", artifactType == "application/vnd.cncf.notary.signature",
		artifactType == "application/vnd.dev.cosign.artifact.sig.v1+json":
		return "signature"
	case artifactType == "application/spdx+json", artifactType == "text/spdx", artifactType == "application/vnd.cyclonedx+json",
		artifactType == "application/vnd.cyclonedx+xml", artifactType == "application/vnd.syft+json":
		return "sbom"
	case artifactType == "application/vnd.in-toto+json", artifactType == "application/vnd.dsse.envelope.v1+json",
		strings.HasPrefix(artifactType, "application/vnd.dev.sigstore.bundle"):
		return "attestation"
	}
	return "other"
}

// describeFacts adds the facts of a tag to its view data (R-OCI-039, R-OCI-040): with summary the description of the annotations; with
// details and facts the annotations' facts, the licences, every held manifest's blobs and, as content of the config blobs, the
// configurations and the labels' facts with the paths of the blobs they were read from.
func describeFacts(d *formats.ViewData, name string, tf rows.TagFacts, want formats.ViewFields) {
	given := annotationFacts(tf)
	if want.Has(formats.ViewSummary) {
		d.Description = given["description"]
	}
	if !want.Has(formats.ViewDetails | formats.ViewFacts) {
		return
	}
	if d.Metadata == nil {
		d.Metadata = map[string]any{}
	}
	if lic := given["licenses"]; lic != "" {
		d.Licenses = []string{lic}
	}
	for k, v := range given {
		if k != "licenses" {
			d.Metadata[k] = v
		}
	}
	blobs, configs := map[string]json.RawMessage{}, map[string]any{}
	var paths []string
	manifests := append([]rows.ManifestFacts{tf.Image}, tf.Children...)
	for _, m := range manifests {
		digest := "sha256:" + hex.EncodeToString(m.Digest[:])
		if len(m.Blobs) > 0 {
			blobs[digest] = m.Blobs
		}
		if m.ConfigDigest == nil {
			continue
		}
		cf := imageConfigOf(m.ConfigFacts)
		if cf == nil {
			cf = &rows.ConfigFacts{Kind: rows.ConfigImage, Unreadable: configNotRead}
		}
		configs[digest] = cf
		paths = append(paths, BlobPath(name, "sha256:"+hex.EncodeToString(m.ConfigDigest[:])))
	}
	if len(blobs) > 0 {
		d.Metadata["blobs"] = blobs
	}
	if len(configs) == 0 {
		return
	}
	d.Metadata[memberConfigs] = configs
	if labeled := labelFacts(tf, given); len(labeled) > 0 {
		d.Metadata[memberLabeled] = labeled
	}
	d.ContentMetadata = []string{memberConfigs, memberLabeled}
	d.ContentPaths = paths
}
