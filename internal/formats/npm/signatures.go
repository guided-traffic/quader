package npm

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/npm/name"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// The signatures of npm (signatures.md R-SIG-002, R-SIG-028): the registry signature of a version in the dist.signatures of a remote's
// document, the provenance bundle a local publish stores beside the tarball, and the attestations a remote stores for a version.

// AttestationsPrefix is the canonical path below which a remote keeps the attestations document of a version, as the attestations
// endpoint names it: "-/npm/v1/attestations/<name>@<version>" (R-SIG-028). The detector reads it; the endpoint stores and serves it.
const AttestationsPrefix = "-/npm/v1/attestations/"

// The predicate types of npm's attestations (read in npm's lib/utils/verify-signatures.js on 2026-11-21).
const (
	predicateSLSAPrefix = "https://slsa.dev/provenance/"
	predicatePublish    = "https://github.com/npm/attestation/tree/main/specs/publish/"
	// maxRegistrySignatures bounds the registry signatures of one version a detector names (R-SIG-013).
	maxRegistrySignatures = 8
	// maxAttestations bounds the attestations of one version a detector names.
	maxAttestations = 32
)

type signatureAdapter struct{ f *Format }

// Signatures implements formats.Format.
func (f *Format) Signatures() formats.SignatureAdapter { return signatureAdapter{f} }

// Declaration implements formats.SignatureAdapter: the row of npm in R-SIG-002.
func (signatureAdapter) Declaration() formats.SignatureDeclaration {
	return formats.SignatureDeclaration{Kinds: []formats.SignatureKind{
		{
			Name: "registry signature", Schemes: []string{formats.SignatureSchemeNPMRegistry}, Role: formats.SignatureRoleRegistry,
			Location: "dist.signatures of the version in the package document", Covers: "<name>@<version>:<integrity>, and so the tarball",
			Client: "dist.signatures of the document; the keys at -/npm/v1/keys",
		},
		{
			Name: "provenance attestation", Schemes: []string{formats.SignatureSchemeSigstoreBundle}, Role: formats.SignatureRoleProvenance,
			Location: "the bundle <name>/-/<basename>-<version>.sigstore of a publish; the attestations of a remote's version",
			Covers:   "the tarball", Client: "dist.attestations and -/npm/v1/attestations/<name>@<version>",
		},
		{
			Name: "publish attestation", Schemes: []string{formats.SignatureSchemeSigstoreBundle}, Role: formats.SignatureRoleRegistry,
			Location: "the attestations of a remote's version", Covers: "the tarball",
			Client: "-/npm/v1/attestations/<name>@<version>",
		},
	}}
}

// Detect implements formats.SignatureAdapter.
func (a signatureAdapter) Detect(_ context.Context, ev formats.SignatureEvent) ([]formats.SignatureCandidate, error) {
	p := ev.Path.Path
	switch {
	case strings.HasPrefix(p, AttestationsPrefix):
		return detectAttestations(ev)
	case strings.HasSuffix(p, sigstoreSuffix):
		return a.detectBundle(ev), nil
	case ev.Item.ID == 0 && ev.Repo.Class == formats.Remote:
		info := a.f.Classify(ev.Path)
		if info.Package == "" || info.Kind == formats.Content {
			return nil, nil
		}
		if doc, ok := a.f.PackageDocumentPath(info.Package); !ok || doc.Path != p {
			return nil, nil
		}
		return detectRegistrySignatures(ev, info.Package)
	}
	return nil, nil
}

// detectBundle names the provenance bundle of a publish (R-NPM-006, R-SIG-028): it covers the tarball it was published with.
func (a signatureAdapter) detectBundle(ev formats.SignatureEvent) []formats.SignatureCandidate {
	info := a.f.Classify(ev.Path)
	n, err := name.Parse(info.Package)
	if err != nil || info.Version == "" {
		return nil
	}
	return []formats.SignatureCandidate{{
		Object:      formats.SignatureLocator{Blob: sum32(ev.Digests.SHA256), MediaType: ev.MediaType},
		SubjectPath: TarballPath(n, info.Version), Role: formats.SignatureRoleProvenance,
		SchemeHint: formats.SignatureSchemeSigstoreBundle, Source: formats.SignatureSourceUpload, Location: ev.Path.Path,
	}}
}

// registryDocument is what the detector reads of a stored package document.
type registryDocument struct {
	Versions map[string]struct {
		Dist struct {
			Integrity  string            `json:"integrity"`
			Signatures []json.RawMessage `json:"signatures"`
		} `json:"dist"`
	} `json:"versions"`
}

// detectRegistrySignatures names every registry signature of a remote's package document: the signed message is
// "<name>@<version>:<integrity>" (pacote), which is the subject; the entry {keyid, sig} is kept as bytes (Q-SIG-22, Q-SIG-50).
func detectRegistrySignatures(ev formats.SignatureEvent, pkg string) ([]formats.SignatureCandidate, error) {
	data, err := ev.Content.Bytes(untrusted.DefaultLimits().Bound())
	if err != nil {
		return nil, err
	}
	var doc registryDocument
	if err := untrusted.UnmarshalJSON(data, untrusted.Limits{}, &doc); err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(doc.Versions))
	for v := range doc.Versions {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	var out []formats.SignatureCandidate
	for _, v := range versions {
		dist := doc.Versions[v].Dist
		if dist.Integrity == "" {
			continue
		}
		subjectName := pkg + "@" + v
		msg := []byte(subjectName + ":" + dist.Integrity)
		for i, sig := range dist.Signatures {
			if i >= maxRegistrySignatures || len(sig) > formats.MaxSignatureRaw {
				break
			}
			out = append(out, formats.SignatureCandidate{
				Object: formats.SignatureLocator{Raw: []byte(sig), MediaType: "application/json"}, Subject: sha256.Sum256(msg),
				SubjectName: subjectName, Role: formats.SignatureRoleRegistry, SchemeHint: formats.SignatureSchemeNPMRegistry,
				Source: formats.SignatureSourceDocument, Location: ev.Path.Path + "#/versions/" + pointerToken(v) + "/dist/signatures/" + strconv.Itoa(i),
				SignedContent: formats.SignatureLocator{Raw: msg},
			})
		}
	}
	return out, nil
}

// attestationsDocument is npm's answer of -/npm/v1/attestations/<name>@<version>.
type attestationsDocument struct {
	Attestations []struct {
		PredicateType string          `json:"predicateType"`
		Bundle        json.RawMessage `json:"bundle"`
	} `json:"attestations"`
}

// detectAttestations names the bundles of a remote's attestations document; each covers the version's tarball.
func detectAttestations(ev formats.SignatureEvent) ([]formats.SignatureCandidate, error) {
	spec := strings.TrimPrefix(ev.Path.Path, AttestationsPrefix)
	at := strings.LastIndex(spec, "@")
	if at <= 0 {
		return nil, nil
	}
	n, nameErr := name.Parse(spec[:at])
	version, ok := canonicalVersion(spec[at+1:])
	if nameErr != nil || !ok {
		return nil, nil //nolint:nilerr // a path that names no version of a package carries no attestations of one
	}
	data, err := ev.Content.Bytes(untrusted.DefaultLimits().Bound())
	if err != nil {
		return nil, err
	}
	var doc attestationsDocument
	if err := untrusted.UnmarshalJSON(data, untrusted.Limits{}, &doc); err != nil {
		return nil, err
	}
	var out []formats.SignatureCandidate
	for i, a := range doc.Attestations {
		if i >= maxAttestations {
			break
		}
		if len(a.Bundle) == 0 || len(a.Bundle) > formats.MaxSignatureRaw {
			continue
		}
		role := formats.SignatureRoleAttestation
		switch {
		case strings.HasPrefix(a.PredicateType, predicateSLSAPrefix):
			role = formats.SignatureRoleProvenance
		case strings.HasPrefix(a.PredicateType, predicatePublish):
			role = formats.SignatureRoleRegistry
		}
		out = append(out, formats.SignatureCandidate{
			Object:      formats.SignatureLocator{Raw: []byte(a.Bundle), MediaType: "application/json"},
			SubjectPath: TarballPath(n, version), Role: role, SchemeHint: formats.SignatureSchemeSigstoreBundle,
			Source: formats.SignatureSourceDocument, Location: ev.Path.Path + "#/attestations/" + strconv.Itoa(i),
		})
	}
	return out, nil
}

// pointerToken escapes a key for a JSON pointer (RFC 6901).
func pointerToken(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

func sum32(b []byte) [32]byte {
	var d [32]byte
	copy(d[:], b)
	return d
}
