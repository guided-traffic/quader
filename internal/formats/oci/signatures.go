package oci

import (
	"context"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// The signatures of OCI (signatures.md R-SIG-002, R-SIG-023): referrers recognised by their artifact type and layers, the referrers
// tag of the distribution specification's fallback, cosign's older tags, and BuildKit's attestation entries of an index. A signature
// object of OCI is the manifest that carries it (Q-SIG-48): its digest is the object's, and its part is the index of the layer that
// holds the signature (cosign's older layout holds one signature per layer; Notation, a Sigstore bundle and a DSSE envelope one, in
// layer 0). A detector reads the committed manifest only, never a layer: the read follows the layer (package of R-SIG-005).

// The media and artifact types the detector recognises (R-SIG-023; cosign's names verified in its pkg/types/media.go).
const (
	artifactNotation       = "application/vnd.cncf.notary.signature"
	layerJWS               = "application/jose+json"
	layerCOSE              = "application/cose"
	sigstoreBundlePrefix   = "application/vnd.dev.sigstore.bundle"
	artifactCosignSig      = "application/vnd.dev.cosign.artifact.sig.v1+json"
	layerSimpleSigning     = "application/vnd.dev.cosign.simplesigning.v1+json"
	layerDSSE              = "application/vnd.dsse.envelope.v1+json"
	annotationBundlePred   = "dev.sigstore.bundle.predicateType"
	annotationPredicate    = "predicateType"
	annotationRefType      = "vnd.docker.reference.type"
	annotationRefDigest    = "vnd.docker.reference.digest"
	refTypeAttestation     = "attestation-manifest"
	predicateSLSAPrefix    = "https://slsa.dev/provenance/"
	predicateCosignSign    = "https://sigstore.dev/cosign/sign/v1"
	attachmentSuffixSig    = ".sig"
	attachmentSuffixAtt    = ".att"
	attachmentSuffixSBOM   = ".sbom"
	attachmentTagPrefix    = "sha256-"
	maxSignatureCandidates = 32 // signatures.max_per_subject's default: a detector names no more per event (R-SIG-013)
)

type signatureAdapter struct{}

// Signatures implements formats.Format.
func (f *Format) Signatures() formats.SignatureAdapter { return signatureAdapter{} }

// Declaration implements formats.SignatureAdapter: the row of OCI in R-SIG-002.
func (signatureAdapter) Declaration() formats.SignatureDeclaration {
	const client = "the referrers API; the referrers tag sha256-<hex>; the tags sha256-<hex>.sig, .att and .sbom"
	return formats.SignatureDeclaration{Kinds: []formats.SignatureKind{
		{
			Name: "Notation", Schemes: []string{formats.SignatureSchemeNotationJWS, formats.SignatureSchemeNotationCOSE},
			Role: formats.SignatureRolePublisher, Location: "a referrer of the image (artifact type application/vnd.cncf.notary.signature)",
			Covers: "the manifest or index digest", Client: client,
		},
		{
			Name: "Sigstore bundle", Schemes: []string{formats.SignatureSchemeSigstoreBundle}, Role: formats.SignatureRolePublisher,
			Location: "a referrer of the image (cosign 3, keyless, with a key or its own PKI)", Covers: "the manifest or index digest", Client: client,
		},
		{
			Name: "cosign signature", Schemes: []string{formats.SignatureSchemeCosignLegacy}, Role: formats.SignatureRolePublisher,
			Location: "the tag sha256-<hex>.sig, one signature per simple-signing layer", Covers: "the manifest or index digest", Client: client,
		},
		{
			Name: "provenance attestation", Schemes: []string{formats.SignatureSchemeSigstoreBundle, formats.SignatureSchemeCosignLegacy},
			Role: formats.SignatureRoleProvenance, Location: "a referrer or the tag sha256-<hex>.att holding a DSSE envelope with SLSA provenance",
			Covers: "the manifest or index digest", Client: client,
		},
		{
			Name: "attestation or SBOM", Schemes: []string{formats.SignatureSchemeUnknown, formats.SignatureSchemeCosignLegacy},
			Role: formats.SignatureRoleAttestation, Location: "a referrer of another artifact type, the tag sha256-<hex>.sbom, BuildKit's attestation entries of an index",
			Covers: "the manifest or index digest", Client: client,
		},
	}, ReadToSign: true, ArtifactName: artifactName} // cosign and notation read the manifest before they push its signature (Q-SIG-211)
}

// sigManifest is what the detector reads of a manifest or an index: more than manifest.Parse keeps (the descriptors' annotations).
type sigManifest struct {
	MediaType    string            `json:"mediaType"`
	ArtifactType string            `json:"artifactType"`
	Config       *sigDescriptor    `json:"config"`
	Layers       []sigDescriptor   `json:"layers"`
	Manifests    []sigDescriptor   `json:"manifests"`
	Subject      *sigDescriptor    `json:"subject"`
	Annotations  map[string]string `json:"annotations"`
}

type sigDescriptor struct {
	MediaType    string            `json:"mediaType"`
	Digest       string            `json:"digest"`
	Size         int64             `json:"size"`
	ArtifactType string            `json:"artifactType"`
	Annotations  map[string]string `json:"annotations"`
}

// detectSignatures is the detector of OCI. A manifest item names its referrer's signatures and BuildKit's entries; a tag item or a
// remote's tag document names the attachments its name makes it (R-SIG-023). Nothing else of the layout carries a signature.
func detectSignatures(ev formats.SignatureEvent) ([]formats.SignatureCandidate, error) {
	_, kind, tail, ok := splitLayout(ev.Path.Path)
	if !ok || (kind != dirManifests && kind != dirTags) {
		return nil, nil
	}
	var subject [32]byte
	var suffix string
	if kind == dirTags {
		if subject, suffix, ok = attachmentTag(tail); !ok {
			return nil, nil
		}
	}
	data, err := ev.Content.Bytes(MaxDocumentBytes)
	if err != nil {
		return nil, err
	}
	var m sigManifest
	if err := untrusted.UnmarshalJSON(data, untrusted.Limits{JSONDepth: 32, JSONMembers: 100_000}, &m); err != nil {
		return nil, err
	}
	self := formats.SignatureLocator{Blob: digest32(ev.Digests.SHA256), MediaType: ev.MediaType}
	var out []formats.SignatureCandidate
	switch {
	case kind == dirManifests:
		if m.Subject != nil {
			if s, ok := manifest.ParseDigest(m.Subject.Digest); ok {
				out = append(out, referrerCandidates(m, self, s, ev.Path.Path)...)
			}
		}
		out = append(out, buildkitCandidates(m, ev.Path.Path)...)
	case suffix == "":
		out = referrersTagCandidates(m, subject, ev.Path.Path)
	default:
		out = cosignTagCandidates(m, self, subject, suffix, ev.Path.Path)
	}
	return capCandidates(out), nil
}

// Detect implements formats.SignatureAdapter.
func (signatureAdapter) Detect(_ context.Context, ev formats.SignatureEvent) ([]formats.SignatureCandidate, error) {
	return detectSignatures(ev)
}

// attachmentTag splits an attachment tag (Q-SIG-32): the referrers tag sha256-<hex>, or cosign's sha256-<hex>.sig, .att and .sbom
// (the tag form <algorithm>-<hex>.<suffix> of cosign's normalize). ok is false for every other tag.
func attachmentTag(tag string) (subject [32]byte, suffix string, ok bool) {
	base := tag
	for _, s := range []string{attachmentSuffixSig, attachmentSuffixAtt, attachmentSuffixSBOM} {
		if b, cut := strings.CutSuffix(tag, s); cut {
			base, suffix = b, s
			break
		}
	}
	hexPart, cut := strings.CutPrefix(base, attachmentTagPrefix)
	if !cut {
		return subject, "", false
	}
	subject, ok = manifest.ParseDigest("sha256:" + hexPart)
	return subject, suffix, ok
}

// IsAttachmentTag reports whether a tag is an attachment tag of R-SIG-024: it gets no version row (Q-SIG-32).
func IsAttachmentTag(tag string) bool {
	_, _, ok := attachmentTag(tag)
	return ok
}

// referrerCandidates are the signatures a manifest with a subject carries, by its artifact type and its layers (R-SIG-023).
func referrerCandidates(m sigManifest, self formats.SignatureLocator, subject [32]byte, location string) []formats.SignatureCandidate {
	art := m.ArtifactType
	if art == "" && m.Config != nil {
		art = m.Config.MediaType
	}
	one := func(part int, scheme, role string) formats.SignatureCandidate {
		return formats.SignatureCandidate{
			Object: self, Part: part, Subject: subject, Role: role, SchemeHint: scheme,
			Source: formats.SignatureSourceReferrer, Location: location,
		}
	}
	var out []formats.SignatureCandidate
	switch art {
	case artifactNotation:
		for i, l := range m.Layers {
			switch l.MediaType {
			case layerJWS:
				out = append(out, one(i, formats.SignatureSchemeNotationJWS, formats.SignatureRolePublisher))
			case layerCOSE:
				out = append(out, one(i, formats.SignatureSchemeNotationCOSE, formats.SignatureRolePublisher))
			}
		}
	case artifactCosignSig:
		for i, l := range m.Layers {
			if l.MediaType == layerSimpleSigning {
				out = append(out, one(i, formats.SignatureSchemeCosignLegacy, formats.SignatureRolePublisher))
			}
		}
	default:
		for i, l := range m.Layers {
			switch {
			case strings.HasPrefix(art, sigstoreBundlePrefix) || strings.HasPrefix(l.MediaType, sigstoreBundlePrefix):
				out = append(out, one(i, formats.SignatureSchemeSigstoreBundle, bundleRole(m.Annotations[annotationBundlePred])))
			case l.MediaType == layerDSSE:
				out = append(out, one(i, formats.SignatureSchemeCosignLegacy, predicateRole(l.Annotations[annotationPredicate])))
			}
			if len(out) > 0 {
				break // a referrer is one signature: the first layer that holds it
			}
		}
	}
	if len(out) == 0 {
		// An SBOM, a scan result, any other artifact: an attachment of the role attestation without a signature.
		out = append(out, one(0, formats.SignatureSchemeUnknown, formats.SignatureRoleAttestation))
	}
	return out
}

// bundleRole is the role of a Sigstore bundle by the predicate type cosign annotates its referrer with: a signature of the image is
// the publisher's, SLSA provenance is provenance, any other statement an attestation.
func bundleRole(predicate string) string {
	if predicate == "" || predicate == predicateCosignSign {
		return formats.SignatureRolePublisher
	}
	return predicateRole(predicate)
}

// predicateRole is the role of an attestation by its predicate type.
func predicateRole(predicate string) string {
	if strings.HasPrefix(predicate, predicateSLSAPrefix) {
		return formats.SignatureRoleProvenance
	}
	return formats.SignatureRoleAttestation
}

// buildkitCandidates are BuildKit's attestation entries of an index (R-OCI-026): unsigned attachments of the manifest the entry names.
func buildkitCandidates(m sigManifest, location string) []formats.SignatureCandidate {
	var out []formats.SignatureCandidate
	for i, d := range m.Manifests {
		if d.Annotations[annotationRefType] != refTypeAttestation {
			continue
		}
		subject, ok := manifest.ParseDigest(d.Annotations[annotationRefDigest])
		obj, okObj := manifest.ParseDigest(d.Digest)
		if !ok || !okObj {
			continue
		}
		out = append(out, formats.SignatureCandidate{
			Object: formats.SignatureLocator{Blob: obj, Size: d.Size, MediaType: d.MediaType}, Subject: subject,
			Role: formats.SignatureRoleAttestation, SchemeHint: formats.SignatureSchemeUnknown, Source: formats.SignatureSourceIndexEntry,
			Location: location + "#/manifests/" + strconv.Itoa(i),
		})
	}
	return out
}

// referrersTagCandidates are the referrers the index of a referrers tag lists, each attached to the tag's subject (R-SIG-023). The
// detector reads no listed manifest: each is named as the object with part 0, and the read of the manifest's own commit names its
// further layers.
func referrersTagCandidates(m sigManifest, subject [32]byte, location string) []formats.SignatureCandidate {
	var out []formats.SignatureCandidate
	for _, d := range m.Manifests {
		obj, ok := manifest.ParseDigest(d.Digest)
		if !ok {
			continue
		}
		scheme, role := descriptorKind(d)
		out = append(out, formats.SignatureCandidate{
			Object: formats.SignatureLocator{Blob: obj, Size: d.Size, MediaType: d.MediaType}, Subject: subject, Role: role,
			SchemeHint: scheme, Source: formats.SignatureSourceReferrersTag, Location: location,
		})
	}
	return out
}

// descriptorKind recognises a referrer by the artifact type its descriptor in a referrers index names.
func descriptorKind(d sigDescriptor) (scheme, role string) {
	switch art := d.ArtifactType; {
	case art == artifactNotation:
		return "", formats.SignatureRolePublisher // JWS or COSE: the read tells
	case strings.HasPrefix(art, sigstoreBundlePrefix):
		return formats.SignatureSchemeSigstoreBundle, bundleRole(d.Annotations[annotationBundlePred])
	case art == artifactCosignSig:
		return formats.SignatureSchemeCosignLegacy, formats.SignatureRolePublisher
	}
	return formats.SignatureSchemeUnknown, formats.SignatureRoleAttestation
}

// cosignTagCandidates are the signatures of cosign's older tags (R-SIG-023): every layer of a .sig manifest is one signature whose
// payload must name the tag's digest, every layer of an .att manifest a DSSE attestation, and an .sbom an attachment.
func cosignTagCandidates(m sigManifest, self formats.SignatureLocator, subject [32]byte, suffix, location string) []formats.SignatureCandidate {
	one := func(part int, scheme, role string) formats.SignatureCandidate {
		return formats.SignatureCandidate{
			Object: self, Part: part, Subject: subject, Role: role, SchemeHint: scheme,
			Source: formats.SignatureSourceCosignTag, Location: location,
		}
	}
	var out []formats.SignatureCandidate
	switch suffix {
	case attachmentSuffixSig:
		for i := range m.Layers {
			out = append(out, one(i, formats.SignatureSchemeCosignLegacy, formats.SignatureRolePublisher))
		}
	case attachmentSuffixAtt:
		for i, l := range m.Layers {
			out = append(out, one(i, formats.SignatureSchemeCosignLegacy, predicateRole(l.Annotations[annotationPredicate])))
		}
	default:
		out = append(out, one(0, formats.SignatureSchemeUnknown, formats.SignatureRoleAttestation))
	}
	return out
}

// capCandidates keeps at most maxSignatureCandidates: an index or a manifest of thousands of entries names no more (R-SIG-013).
func capCandidates(c []formats.SignatureCandidate) []formats.SignatureCandidate {
	if len(c) > maxSignatureCandidates {
		return c[:maxSignatureCandidates]
	}
	return c
}

func digest32(b []byte) [32]byte {
	var d [32]byte
	copy(d[:], b)
	return d
}

// artifactName names a manifest as a client does (Q-SIG-213): "<name>:<tag>" for a tag, "<name>@<digest>" for a manifest by digest.
func artifactName(path string, _ formats.PathInfo) string {
	name, kind, tail, ok := splitLayout(path)
	switch {
	case !ok:
		return ""
	case kind == dirTags:
		return name + ":" + tail
	case kind == dirManifests:
		return name + "@" + tail
	}
	return ""
}
