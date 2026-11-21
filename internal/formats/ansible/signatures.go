package ansible

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection"
	"github.com/guided-traffic/quader/internal/formats/ansible/galaxydoc"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// The signatures of Ansible Galaxy (signatures.md R-SIG-002, R-SIG-031): detached OpenPGP signatures of a collection's MANIFEST.json,
// which names FILES.json, which names every file, listed in signatures[] of a version document. A remote's fill of a version document
// is where they are detected; a local version takes them through the management API (package of R-SIG-031), which hands them to the
// store itself.

// maxVersionSignatures bounds the signatures of one version document a detector names (R-SIG-013).
const maxVersionSignatures = 16

// manifestEntry is the entry of a collection archive its signatures sign.
const manifestEntry = "MANIFEST.json"

type signatureAdapter struct{ f *Format }

// Signatures implements formats.Format.
func (f *Format) Signatures() formats.SignatureAdapter { return signatureAdapter{f} }

// Declaration implements formats.SignatureAdapter: the row of Ansible Galaxy in R-SIG-002.
func (signatureAdapter) Declaration() formats.SignatureDeclaration {
	const (
		covers = "the collection archive: MANIFEST.json, which names FILES.json, which names every file"
		client = "signatures[] of the version document"
	)
	return formats.SignatureDeclaration{Kinds: []formats.SignatureKind{
		{
			Name: "signing service signature", Schemes: []string{formats.SignatureSchemeOpenPGP}, Role: formats.SignatureRoleRegistry,
			Location: "an entry of signatures[] that names a signing_service", Covers: covers, Client: client,
		},
		{
			Name: "uploaded signature", Schemes: []string{formats.SignatureSchemeOpenPGP}, Role: formats.SignatureRolePublisher,
			Location: "an entry of signatures[] without a signing_service", Covers: covers, Client: client,
		},
	}}
}

// signatureEntry is one entry of signatures[] of a version document.
type signatureEntry struct {
	Signature      string          `json:"signature"`
	SigningService json.RawMessage `json:"signing_service"`
}

// Gate implements formats.SignatureAdapter (R-SIG-063): a collection archive and a role archive are the artifacts; the documents a
// remote stores are none of their own.
func (a signatureAdapter) Gate(_ context.Context, _ formats.Host, _ formats.RepoRef, items []formats.GateItem) ([]formats.GateSubject, error) {
	out := make([]formats.GateSubject, len(items))
	for i, it := range items {
		if strings.HasSuffix(it.Path, collection.ArchiveSuffix) && a.f.Classify(formats.Path{Path: it.Path}).Kind == formats.Content {
			out[i].Kind = formats.GateArtifact
		}
	}
	return out, nil
}

// Detect implements formats.SignatureAdapter: a remote's version document names each of its signatures with the archive its
// artifact.sha256 names as the subject (Q-SIG-37).
func (a signatureAdapter) Detect(_ context.Context, ev formats.SignatureEvent) ([]formats.SignatureCandidate, error) {
	if ev.Item.ID != 0 || ev.Repo.Class != formats.Remote || !strings.HasSuffix(ev.Path.Path, jsonSuffix) {
		return nil, nil
	}
	info := a.f.Classify(ev.Path)
	if info.Kind != formats.PathMetadata || info.Version == "" || strings.Contains(ev.Path.Path, "/"+dirDocs+"/") {
		return nil, nil
	}
	data, err := ev.Content.Bytes(galaxydoc.MaxDocumentBytes)
	if err != nil {
		return nil, err
	}
	if err := untrusted.CheckJSON(data, untrusted.Limits{}); err != nil {
		return nil, err
	}
	v, _, err := galaxydoc.ParseUpstreamVersion(data)
	if err != nil {
		return nil, err
	}
	var out []formats.SignatureCandidate
	for i, raw := range v.Signatures {
		if i >= maxVersionSignatures {
			break
		}
		var e signatureEntry
		if json.Unmarshal(raw, &e) != nil || e.Signature == "" || len(e.Signature) > formats.MaxSignatureRaw {
			continue
		}
		role := formats.SignatureRolePublisher
		if s := strings.TrimSpace(string(e.SigningService)); s != "" && s != "null" && s != `""` {
			role = formats.SignatureRoleRegistry
		}
		obj := formats.SignatureLocator{Raw: []byte(e.Signature), MediaType: "application/pgp-signature"}
		parts, err := untrusted.OpenPGPSignatures(obj.Raw)
		if err != nil {
			parts = 1 // the read judges what does not parse
		}
		for p := range min(parts, maxVersionSignatures) {
			out = append(out, formats.SignatureCandidate{
				Object: obj, Part: p, Subject: v.SHA256, Role: role, SchemeHint: formats.SignatureSchemeOpenPGP,
				Source: formats.SignatureSourceDocument, Location: ev.Path.Path + "#/signatures/" + strconv.Itoa(i),
				SignedContent: formats.SignatureLocator{Blob: v.SHA256, Locator: formats.SignatureLocatorEntryPrefix + manifestEntry},
			})
		}
	}
	return out, nil
}
