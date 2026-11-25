package oci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// Quader's receipt attestation of an image as a referrer (signatures.md R-SIG-054, Q-SIG-66, Q-SIG-183): a manifest of the form cosign 3
// writes and reads (artifactType and the single layer of a Sigstore bundle, the empty config, the subject the attested manifest, and the
// annotations dev.sigstore.bundle.content, dev.sigstore.bundle.predicateType and org.opencontainers.image.created). It stands only where it
// hides nothing: in a local repository, whose referrers answer is the API's own (R-OCI-016), and only while no tag sha256-<hex>.sig or
// .att stands for the digest, because cosign 3.1.4 reads those older tags only when it finds no bundle among the referrers. A remote's and
// a virtual's images get their attestation through the API (Q-SIG-183). The job signing commits the files through the item store as its
// own system write; the manifest's commit carries attestationCommit, which OnCommit admits like a push of the manifest by digest without
// a claim of the name, a version or a revision: a referrer is in no list and no count (R-SIG-024).

// The media types of the referrer (R-SIG-054).
const (
	mediaTypeImageManifest = "application/vnd.oci.image.manifest.v1+json"
	mediaTypeEmpty         = "application/vnd.oci.empty.v1+json"
	mediaTypeBundle        = "application/vnd.dev.sigstore.bundle.v0.3+json"
)

// emptyConfigJSON is the empty JSON object of the OCI image specification, the config of an artifact without one.
const emptyConfigJSON = "{}"

// attestationCommit is the payload of the manifest of an attestation's referrer: the parsed manifest and its image name.
type attestationCommit struct {
	name string
	m    *manifest.Manifest
}

var _ formats.AttestationReferrer = (*Format)(nil)

// ReferrerPlacement implements formats.AttestationReferrer (R-SIG-054, Q-SIG-66): a referrer stands in a local repository while no older
// cosign signature or attestation tag stands for the digest.
func (f *Format) ReferrerPlacement(ctx context.Context, h formats.Host, repo formats.RepoRef, item formats.AttestationItem) (bool, string, error) {
	if repo.Class != formats.Local {
		return false, formats.ReferrerNoAPI, nil
	}
	name, kind, _, ok := splitLayout(item.Path)
	if !ok || kind != dirManifests {
		return false, formats.ReferrerNoAPI, nil
	}
	base := "sha256-" + hex.EncodeToString(item.Blob[:])
	for _, suffix := range []string{".sig", ".att"} {
		_, found, err := h.Items().Stat(ctx, repo, formats.Path{Path: TagPath(name, base+suffix)})
		if err != nil {
			return false, "", err
		}
		if found {
			return false, formats.ReferrerHidesSignature, nil
		}
	}
	return true, "", nil
}

// AttestationReferrer implements formats.AttestationReferrer: the empty config, the bundle as the one layer, and the manifest that names
// them with the attested manifest as its subject. The same bundle and time always make the same manifest, so a referrer that was deleted
// is made again with its digest.
func (f *Format) AttestationReferrer(subject formats.AttestationSubject, item formats.AttestationItem, bundle []byte, predicateType string, created time.Time) ([]formats.ReferrerFile, [32]byte, error) {
	name, kind, _, ok := splitLayout(item.Path)
	if !ok || kind != dirManifests || len(bundle) == 0 {
		return nil, [32]byte{}, errs.E(errs.Internal, errs.CodeInternal, "An attestation's referrer is made for a manifest only.")
	}
	subjectType := subject.MediaType
	if subjectType == "" {
		subjectType = mediaTypeImageManifest
	}
	bundleSum, configSum := sha256.Sum256(bundle), sha256.Sum256([]byte(emptyConfigJSON))
	doc := map[string]any{
		"schemaVersion": 2,
		"mediaType":     mediaTypeImageManifest,
		"artifactType":  mediaTypeBundle,
		"config":        map[string]any{"mediaType": mediaTypeEmpty, "digest": digestText(configSum), "size": len(emptyConfigJSON)},
		"layers":        []any{map[string]any{"mediaType": mediaTypeBundle, "digest": digestText(bundleSum), "size": len(bundle)}},
		"subject":       map[string]any{"mediaType": subjectType, "digest": digestText(item.Blob), "size": item.Size},
		"annotations": map[string]string{
			"dev.sigstore.bundle.content":       "dsse-envelope",
			"dev.sigstore.bundle.predicateType": predicateType,
			"org.opencontainers.image.created":  created.UTC().Format(time.RFC3339),
		},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, [32]byte{}, err
	}
	m, err := manifest.Parse(body, mediaTypeImageManifest, untrusted.Limits{DocumentBytes: MaxDocumentBytes})
	if err != nil {
		return nil, [32]byte{}, err
	}
	return []formats.ReferrerFile{
		{Path: BlobPath(name, digestText(configSum)), Body: []byte(emptyConfigJSON), MediaType: mediaTypeEmpty},
		{Path: BlobPath(name, digestText(bundleSum)), Body: bundle, MediaType: mediaTypeBundle},
		{Path: ManifestPath(name, digestText(m.Sum)), Body: body, MediaType: mediaTypeImageManifest, Payload: &attestationCommit{name: name, m: m}},
	}, m.Sum, nil
}

// ReferrerPaths implements formats.AttestationReferrer: the manifest and the bundle's layer; the empty config is shared by every artifact
// without a config, and the format's sweep of unreferenced blobs takes it when nothing names it any more.
func (f *Format) ReferrerPaths(item formats.AttestationItem, manifestSum, bundle [32]byte) []string {
	name, _, _, ok := splitLayout(item.Path)
	if !ok {
		return nil
	}
	return []string{ManifestPath(name, digestText(manifestSum)), BlobPath(name, digestText(bundle))}
}

// commitAttestationReferrer records the manifest of an attestation's referrer in the transaction that stores its item (R-SIG-054): the
// references are held as for a push, and no name is claimed, no version made, no revision raised.
func (f *Format) commitAttestationReferrer(ctx context.Context, tx formats.Tx, c formats.CommitEvent, p *attestationCommit) error {
	if c.Repo.Class != formats.Local || !strings.HasPrefix(c.Path.Path, p.name+"/"+dirManifests+"/") {
		return errs.E(errs.Conflict, errs.CodeConflict, "An attestation's referrer is stored in a local repository under its image only.")
	}
	st, err := portOf(c.Data)
	if err != nil {
		return err
	}
	if err := checkReferences(ctx, st, tx, c.Repo.ID, p.name, p.m); err != nil {
		return err
	}
	return st.PutManifest(ctx, tx, manifestInput(c.Repo.ID, p.name, p.m, nil))
}
