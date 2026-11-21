package testformat

import (
	"context"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
)

// SignatureSuffix names the signature file of a tarball: "<tarball>.sig" beside it, the one kind of signature the test format knows.
const SignatureSuffix = ".tgz.sig"

// SignatureSample is the bytes of the test format's signature file: a binary OpenPGP signature packet header and a body of zeros, which
// the detector names and nothing checks.
func SignatureSample() []byte { return []byte{0xc2, 0x04, 0, 0, 0, 0} }

// Signatures implements formats.Format; NoSignatureDeclaration leaves it nil.
func (f Format) Signatures() formats.SignatureAdapter {
	if f.has(string(NoSignatureDeclaration)) {
		return nil
	}
	return signatureAdapter{}
}

type signatureAdapter struct{}

// Declaration implements formats.SignatureAdapter.
func (signatureAdapter) Declaration() formats.SignatureDeclaration {
	return formats.SignatureDeclaration{Kinds: []formats.SignatureKind{{
		Name: "detached signature", Schemes: []string{formats.SignatureSchemeOpenPGP}, Role: formats.SignatureRolePublisher,
		Location: "<tarball>.sig beside the tarball", Covers: "the tarball", Client: "the file beside the tarball",
	}}}
}

// Detect implements formats.SignatureAdapter: a signature file names the tarball beside it.
func (signatureAdapter) Detect(_ context.Context, ev formats.SignatureEvent) ([]formats.SignatureCandidate, error) {
	if !strings.HasSuffix(ev.Path.Path, SignatureSuffix) {
		return nil, nil
	}
	var self [32]byte
	copy(self[:], ev.Digests.SHA256)
	return []formats.SignatureCandidate{{
		Object: formats.SignatureLocator{Blob: self}, SubjectPath: strings.TrimSuffix(ev.Path.Path, ".sig"), Role: formats.SignatureRolePublisher,
		SchemeHint: formats.SignatureSchemeOpenPGP, Source: formats.SignatureSourceSibling, Location: ev.Path.Path,
	}}, nil
}
