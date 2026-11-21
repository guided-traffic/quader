package generic

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// The signatures of a generic repository (signatures.md R-SIG-002, R-SIG-032, Q-SIG-8): a file beside the file of the same directory,
// recognised by its name, its scheme read from its content (Q-SIG-38). Either order works: a signature file names the file it signs
// when that is stored, and a file names the signature files stored beside it; what is not stored yet is named when it is.

// The names of a signature file, longest first: a.jws.sig is Notation's of a, not a .sig of a.jws (R-SIG-032).
func signatureSuffixes() []string {
	return []string{".sigstore.json", ".jws.sig", ".cose.sig", ".asc", ".sig"}
}

// maxSiblingSignatures bounds the OpenPGP signatures of one signature file a detector names (R-SIG-013).
const maxSiblingSignatures = 16

// Signatures implements formats.Format.
func (Format) Signatures() formats.SignatureAdapter { return signatureAdapter{} }

type signatureAdapter struct{}

// Declaration implements formats.SignatureAdapter: the row of generic in R-SIG-002.
func (signatureAdapter) Declaration() formats.SignatureDeclaration {
	return formats.SignatureDeclaration{Kinds: []formats.SignatureKind{
		{
			Name: "signature file", Schemes: []string{
				formats.SignatureSchemeOpenPGP, formats.SignatureSchemePublicKey, formats.SignatureSchemeSigstoreBundle,
				formats.SignatureSchemeNotationJWS, formats.SignatureSchemeNotationCOSE,
			}, Role: formats.SignatureRolePublisher,
			Location: "a file beside the file: <file>.asc, <file>.sig, <file>.sigstore.json, <file>.jws.sig, <file>.cose.sig",
			Covers:   "the file of the same directory", Client: "the sibling file, downloaded as a file",
		},
	}}
}

// SignedFile returns the file a signature file signs by its name, and whether p is a signature file at all.
func SignedFile(p string) (file, suffix string, ok bool) {
	for _, s := range signatureSuffixes() {
		if f, cut := strings.CutSuffix(p, s); cut && f != "" && !strings.HasSuffix(f, "/") {
			return f, s, true
		}
	}
	return "", "", false
}

// Gate implements formats.SignatureAdapter (R-SIG-063): every file is an artifact, and a file named like a signature of the file beside
// it is a signature file, which the core passes while its scheme was read.
func (signatureAdapter) Gate(_ context.Context, _ formats.Host, _ formats.RepoRef, items []formats.GateItem) ([]formats.GateSubject, error) {
	out := make([]formats.GateSubject, len(items))
	for i, it := range items {
		out[i].Kind = formats.GateArtifact
		if _, _, ok := SignedFile(it.Path); ok {
			out[i].Kind = formats.GateSignatureFile
		}
	}
	return out, nil
}

// Detect implements formats.SignatureAdapter.
func (signatureAdapter) Detect(_ context.Context, ev formats.SignatureEvent) ([]formats.SignatureCandidate, error) {
	if ev.Item.ID == 0 {
		return nil, nil
	}
	p := ev.Path.Path
	self := sum32(ev.Digests.SHA256)
	var out []formats.SignatureCandidate
	if file, suffix, ok := SignedFile(p); ok {
		scheme, parts := sniff(ev, suffix)
		for i := range parts {
			out = append(out, formats.SignatureCandidate{
				Object: formats.SignatureLocator{Blob: self, MediaType: ev.MediaType}, Part: i, SubjectPath: file,
				Role: formats.SignatureRolePublisher, SchemeHint: scheme, Source: formats.SignatureSourceSibling, Location: p,
			})
		}
	}
	// The signature files that may stand beside this file: each is named by its path, and the core keeps those that are stored.
	for _, s := range signatureSuffixes() {
		out = append(out, formats.SignatureCandidate{
			Object: formats.SignatureLocator{Path: p + s}, Subject: self, Role: formats.SignatureRolePublisher,
			Source: formats.SignatureSourceSibling, Location: p + s,
		})
	}
	return out, nil
}

// sniff reads the scheme of a signature file from its content (Q-SIG-38) and the number of signatures it holds: an armored or a binary
// OpenPGP signature (one per packet), a Sigstore bundle, a Notation JWS or COSE envelope, for .sig also the base64 raw signature of
// cosign sign-blob with a key; anything else is unknown. A file larger than a signature object is unknown: the store records it so.
func sniff(ev formats.SignatureEvent, suffix string) (scheme string, parts int) {
	b, err := ev.Content.Bytes(formats.MaxSignatureRaw)
	if err != nil {
		return formats.SignatureSchemeUnknown, 1
	}
	trimmed := bytes.TrimSpace(b)
	switch {
	case untrusted.OpenPGPArmored(b) || untrusted.OpenPGPBinary(b):
		n, err := untrusted.OpenPGPSignatures(b)
		if err != nil || n == 0 {
			return formats.SignatureSchemeOpenPGP, 1
		}
		return formats.SignatureSchemeOpenPGP, min(n, maxSiblingSignatures)
	case suffix == ".cose.sig" || (len(b) > 0 && b[0] == 0xd2): // CBOR tag 18, COSE_Sign1
		return formats.SignatureSchemeNotationCOSE, 1
	case len(trimmed) > 0 && trimmed[0] == '{':
		return jsonScheme(trimmed), 1
	case suffix == ".jws.sig":
		return formats.SignatureSchemeNotationJWS, 1
	case suffix == ".sig" && rawSignature(trimmed):
		return formats.SignatureSchemePublicKey, 1
	}
	return formats.SignatureSchemeUnknown, 1
}

// jsonScheme tells a Sigstore bundle from a Notation JWS envelope in its JSON serialization.
func jsonScheme(b []byte) string {
	if untrusted.CheckJSON(b, untrusted.Limits{}) != nil {
		return formats.SignatureSchemeUnknown
	}
	var probe struct {
		MediaType string          `json:"mediaType"`
		Payload   json.RawMessage `json:"payload"`
		Protected json.RawMessage `json:"protected"`
	}
	if json.Unmarshal(b, &probe) != nil {
		return formats.SignatureSchemeUnknown
	}
	switch {
	case strings.HasPrefix(probe.MediaType, "application/vnd.dev.sigstore.bundle"):
		return formats.SignatureSchemeSigstoreBundle
	case len(probe.Payload) > 0 && len(probe.Protected) > 0:
		return formats.SignatureSchemeNotationJWS
	}
	return formats.SignatureSchemeUnknown
}

// rawSignature reports whether b is one line of base64 that decodes to the length of an ECDSA, Ed25519 or RSA signature, as cosign
// sign-blob writes it.
func rawSignature(b []byte) bool {
	if len(b) == 0 || bytes.ContainsAny(b, " \n\r\t") {
		return false
	}
	d, err := base64.StdEncoding.DecodeString(string(b))
	return err == nil && len(d) >= 64 && len(d) <= 1024
}

func sum32(b []byte) [32]byte {
	var d [32]byte
	copy(d[:], b)
	return d
}
