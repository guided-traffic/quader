package oci

import (
	"context"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci/rows"
)

// What Quader's receipt attestation of an image is about (signatures.md R-SIG-044, R-SIG-051, Q-SIG-181): every top-level manifest or
// index, one a tag names or one pulled by digest that no stored index of the name lists, stored by a push or a remote's fill. A platform
// manifest an index lists is not attested of its own (the index's attestation commits to its digest), and neither is a carrier of
// signatures, attestations or SBOMs (oci_manifests.attachment, R-SIG-063), a layer, a config, a tag item or the tags list. The judgement
// is made when the job signs, so that an index pushed after its children takes them out (Q-SIG-181). The statement names the image, the
// manifest's media type, and the verdicts of the blobs the image consists of.

// attests is what Quader's receipt attestation is about in an OCI repository (signatures.md R-SIG-044).
const attests = "every top-level manifest or index (one a tag names, or one pulled by digest that no stored index lists)"

// maxScannedBlobs bounds the blobs whose malware verdicts one statement names: an index's children's layers included.
const maxScannedBlobs = 256

// AttestationSubjects implements formats.SignatureAdapter (R-SIG-044). The facts of the manifests come from the format's data port; a
// manifest that is not recorded is not attested (its rows are written in the transaction that stores its item, so a manifest without
// rows is no manifest of the layout).
func (signatureAdapter) AttestationSubjects(ctx context.Context, h formats.Host, repo formats.RepoRef, items []formats.AttestationItem) ([]formats.AttestationSubject, error) {
	out := make([]formats.AttestationSubject, len(items))
	keys := make([]rows.ManifestKey, 0, len(items))
	at := make([]int, 0, len(items))
	for i, it := range items {
		name, kind, _, ok := splitLayout(it.Path)
		if !ok || kind != dirManifests || !ValidName(name) {
			continue // a tag (its manifest item is the subject), a blob, the tags list: nothing of its own
		}
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
		if !ok || f.Attachment || len(f.Indexes) > 0 {
			continue
		}
		m, found, err := st.Manifest(ctx, repo.ID, k.Name, k.Digest)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		scanned, err := scannedBlobs(ctx, st, repo.ID, k.Name, k.Digest)
		if err != nil {
			return nil, err
		}
		out[at[n]] = formats.AttestationSubject{Attested: true, Name: k.Name, MediaType: m.MediaType, Scanned: scanned}
	}
	return out, nil
}

// scannedBlobs are the blobs of an image whose malware verdicts its statement names: the config and the layers of a manifest, and of
// each manifest an index lists, at most maxScannedBlobs.
func scannedBlobs(ctx context.Context, st Store, repoID int64, name string, digest [32]byte) ([][32]byte, error) {
	refs, err := st.References(ctx, repoID, name, digest)
	if err != nil {
		return nil, err
	}
	seen := map[[32]byte]bool{}
	var out [][32]byte
	add := func(d [32]byte) {
		if !seen[d] && len(out) < maxScannedBlobs {
			seen[d] = true
			out = append(out, d)
		}
	}
	for _, r := range refs {
		switch r.Kind {
		case rows.KindBlob:
			add(r.Target)
		case rows.KindManifest:
			children, err := st.References(ctx, repoID, name, r.Target)
			if err != nil {
				return nil, err
			}
			for _, c := range children {
				if c.Kind == rows.KindBlob {
					add(c.Target)
				}
			}
		}
	}
	return out, nil
}
