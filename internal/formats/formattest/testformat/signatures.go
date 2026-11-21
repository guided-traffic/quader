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
	return signatureAdapter{f}
}

type signatureAdapter struct{ f Format }

// Declaration implements formats.SignatureAdapter.
func (signatureAdapter) Declaration() formats.SignatureDeclaration {
	return formats.SignatureDeclaration{Attests: "every tarball", Kinds: []formats.SignatureKind{{
		Name: "detached signature", Schemes: []string{formats.SignatureSchemeOpenPGP}, Role: formats.SignatureRolePublisher,
		Location: "<tarball>.sig beside the tarball", Covers: "the tarball", Client: "the file beside the tarball",
	}}}
}

// Gate implements formats.SignatureAdapter (R-SIG-063): a tarball is an artifact and its signature file a signature file; the defects
// GatesAttachment and PassesArtifact break one each.
func (a signatureAdapter) Gate(_ context.Context, _ formats.Host, _ formats.RepoRef, items []formats.GateItem) ([]formats.GateSubject, error) {
	out := make([]formats.GateSubject, len(items))
	for i, it := range items {
		switch {
		case strings.HasSuffix(it.Path, SignatureSuffix):
			out[i].Kind = formats.GateSignatureFile
			if a.f.has(string(GatesAttachment)) {
				out[i].Kind = formats.GateArtifact
			}
		case strings.HasSuffix(it.Path, ".tgz") && !a.f.has(string(PassesArtifact)):
			out[i].Kind = formats.GateArtifact
		}
	}
	return out, nil
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

// AttestationSubjects implements formats.SignatureAdapter (R-SIG-044): a tarball is attested under its package and version, its signature
// file is not; the defect AttestsSignature attests the signature file too.
func (a signatureAdapter) AttestationSubjects(_ context.Context, _ formats.Host, _ formats.RepoRef, items []formats.AttestationItem) ([]formats.AttestationSubject, error) {
	out := make([]formats.AttestationSubject, len(items))
	for i, it := range items {
		switch {
		case strings.HasSuffix(it.Path, SignatureSuffix):
			if a.f.has(string(AttestsSignature)) {
				out[i] = formats.AttestationSubject{Attested: true, Name: it.Path}
			}
		case strings.HasSuffix(it.Path, ".tgz"):
			info := a.f.Classify(formats.Path{Path: it.Path})
			out[i] = formats.AttestationSubject{Attested: true, Name: info.Package, Version: info.Version}
		}
	}
	return out, nil
}
