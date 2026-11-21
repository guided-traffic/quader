package oci

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// The fetch of a remote image's signatures (signatures.md R-SIG-025, R-SIG-026, Q-SIG-33, Q-SIG-99): what Quader asks an upstream
// for a top-level manifest that a fill or a mirror's pass stored, so that the badge of an image is right even if no client asked for
// its signatures. A job of the worker runs it for a client's pull (FetchSignatures), a mirror's plan for each kept tag; both store
// what they find through the upstream's Document and Artifact, so the commit hook detects it as it detects a client's fill.
//
//  1. GET referrers/<digest>: the referrers API.
//  2. HEAD of the tags sha256-<hex>.sig and sha256-<hex>.att (cosign's older layout): a HEAD costs no pull on Docker Hub.
//  3. Only when the API answered 404 (or 405): HEAD of the referrers tag sha256-<hex>, and GET of its index when it is there.
//  4. GET of at most signatures.remote.max_manifests_per_subject (4) signature or attestation manifests, signatures first: a referrer
//     by digest, an attachment tag as the tag's document.
//  5. GET of their layers up to signatures.max_object_size, at most maxFetchLayers per manifest; never an SBOM's.

// The bounds of a fetch (R-SIG-040's defaults; the configuration that changes them belongs to the package of R-SIG-040).
const (
	// maxFetchManifests is signatures.remote.max_manifests_per_subject.
	maxFetchManifests = 4
	// maxFetchObject is signatures.max_object_size: a layer above it is not fetched (the read would refuse it).
	maxFetchObject = 256 << 10
	// maxFetchLayers bounds the layers fetched of one signature manifest: cosign's older layout holds one signature per layer.
	maxFetchLayers = 8
)

var _ formats.SignatureFetcher = remoteAdapter{}

// FetchSignatures implements [formats.SignatureFetcher]: the target is "<image name>@sha256:<hex>", queued by the fill of a top-level
// manifest or by the refresh of its tag (R-SIG-026). A target of another form names nothing to fetch.
func (a remoteAdapter) FetchSignatures(ctx context.Context, u discover.Upstream, target string) error {
	name, dgst, ok := strings.Cut(target, "@")
	if !ok || !ValidName(name) || !ValidDigest(dgst) {
		return nil
	}
	return fetchImageSignatures(ctx, u, name, dgst)
}

// SignatureFetchTarget is the target of the background fetch of a top-level manifest (R-SIG-026).
func SignatureFetchTarget(name, digest string) string { return name + "@" + digest }

// fetchRef is one signature or attestation manifest a fetch found: a referrer by digest, or an attachment tag.
type fetchRef struct {
	digest string // a referrer's manifest digest; "" for a tag
	tag    string // an attachment tag; "" for a referrer
	rank   int    // 0 a publisher's signature, 1 provenance, 2 another attestation, 3 an SBOM
	sbom   bool
}

// fetchImageSignatures asks the upstream for the signatures of the manifest dgst of the image name, and stores what it finds.
func fetchImageSignatures(ctx context.Context, u discover.Upstream, name, dgst string) error {
	hexPart := strings.TrimPrefix(dgst, "sha256:")
	base := u.Base()
	var refs []fetchRef
	res, err := u.Get(ctx, registryURL(base, name, "/referrers/"+dgst), manifest.MediaTypeImageIndex, MaxDocumentBytes)
	if err != nil {
		return err
	}
	noAPI := res.Status == http.StatusNotFound || res.Status == http.StatusMethodNotAllowed
	if res.Status == http.StatusOK {
		refs = append(refs, referrerRefs(res.Body)...)
	}
	for _, suffix := range []string{attachmentSuffixSig, attachmentSuffixAtt} {
		tag := attachmentTagPrefix + hexPart + suffix
		there, err := tagThere(ctx, u, name, tag)
		if err != nil {
			return err
		}
		if there {
			rank := 0
			if suffix == attachmentSuffixAtt {
				rank = 1
			}
			refs = append(refs, fetchRef{tag: tag, rank: rank})
		}
	}
	if noAPI {
		// The fallback of the distribution specification: the referrers tag holds the index the API would have answered.
		tag := attachmentTagPrefix + hexPart
		there, err := tagThere(ctx, u, name, tag)
		if err != nil {
			return err
		}
		if there {
			doc, found, err := u.Document(ctx, TagPath(name, tag), "")
			if err != nil {
				return err
			}
			if found {
				refs = append(refs, referrerRefs(doc)...)
			}
		}
	}
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].rank < refs[j].rank })
	if len(refs) > maxFetchManifests {
		refs = refs[:maxFetchManifests]
	}
	for _, r := range refs {
		if err := fetchRefAndLayers(ctx, u, name, r); err != nil {
			return err
		}
	}
	return nil
}

// tagThere asks by HEAD whether the upstream has a tag of the image (a manifest the tag names).
func tagThere(ctx context.Context, u discover.Upstream, name, tag string) (bool, error) {
	res, err := discover.Head(ctx, u, registryURL(u.Base(), name, "/manifests/"+tag), acceptManifests)
	if err != nil {
		return false, err
	}
	return res.Status == http.StatusOK, nil
}

// referrerRefs reads the manifests a referrers index lists, each with its rank by its artifact type.
func referrerRefs(body []byte) []fetchRef {
	var idx sigManifest
	if err := untrusted.UnmarshalJSON(body, untrusted.Limits{JSONDepth: 32, JSONMembers: 100_000}, &idx); err != nil {
		return nil
	}
	var out []fetchRef
	for _, d := range idx.Manifests {
		if _, ok := manifest.ParseDigest(d.Digest); !ok {
			continue
		}
		_, role := descriptorKind(d)
		r := fetchRef{digest: d.Digest, rank: rankOf(role)}
		if sbomType(d.ArtifactType) {
			r.rank, r.sbom = 3, true
		}
		out = append(out, r)
	}
	return out
}

func rankOf(role string) int {
	switch role {
	case formats.SignatureRolePublisher:
		return 0
	case formats.SignatureRoleProvenance:
		return 1
	}
	return 2
}

// sbomType reports whether an artifact type is one of an SBOM (R-SIG-023): its layers are never fetched.
func sbomType(t string) bool {
	for _, p := range []string{"application/spdx", "text/spdx", "application/vnd.cyclonedx", "application/vnd.syft"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// fetchRefAndLayers stores one signature or attestation manifest and the layers that hold its signature.
func fetchRefAndLayers(ctx context.Context, u discover.Upstream, name string, r fetchRef) error {
	var (
		body  []byte
		found bool
		err   error
	)
	if r.tag != "" {
		body, found, err = u.Document(ctx, TagPath(name, r.tag), "")
	} else {
		body, found, err = u.Artifact(ctx, ManifestPath(name, r.digest), MaxDocumentBytes)
	}
	if err != nil || !found || body == nil || r.sbom {
		return err
	}
	var m sigManifest
	if err := untrusted.UnmarshalJSON(body, untrusted.Limits{JSONDepth: 32, JSONMembers: 100_000}, &m); err != nil {
		return nil //nolint:nilerr // a manifest that does not parse carries no layer to fetch; the detector judges it
	}
	art := m.ArtifactType
	if art == "" && m.Config != nil {
		art = m.Config.MediaType
	}
	if sbomType(art) {
		return nil
	}
	n := 0
	for _, l := range m.Layers {
		if n >= maxFetchLayers {
			break
		}
		if l.Size <= 0 || l.Size > maxFetchObject || sbomType(l.MediaType) || !ValidDigest(l.Digest) {
			continue
		}
		n++
		if _, _, err := u.Artifact(ctx, BlobPath(name, l.Digest), maxFetchObject); err != nil {
			return err
		}
	}
	return nil
}

// attachmentManifest reports whether a manifest is a signature or an attestation by R-SIG-023 (a referrer, an artifact of a signature
// type, cosign's older layout): it is no image whose own signatures are fetched.
func attachmentManifest(data []byte) bool {
	var m sigManifest
	if err := untrusted.UnmarshalJSON(data, untrusted.Limits{JSONDepth: 32, JSONMembers: 100_000}, &m); err != nil {
		return false
	}
	if m.Subject != nil {
		return true
	}
	art := m.ArtifactType
	if art == "" && m.Config != nil {
		art = m.Config.MediaType
	}
	switch {
	case art == artifactNotation, art == artifactCosignSig, strings.HasPrefix(art, sigstoreBundlePrefix), sbomType(art):
		return true
	}
	for _, l := range m.Layers {
		switch {
		case l.MediaType == layerSimpleSigning, l.MediaType == layerDSSE, strings.HasPrefix(l.MediaType, sigstoreBundlePrefix):
			return true
		}
	}
	return false
}
