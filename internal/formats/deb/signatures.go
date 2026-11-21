package deb

import (
	"context"
	"path"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// The signatures of Debian (signatures.md R-SIG-002, R-SIG-030): the OpenPGP signatures of a remote's release document, InRelease
// (signed inline) and Release.gpg (detached, of Release). apt checks no signature of a package; a package is covered by a release
// whose index names its SHA-256, which the format answers through its data port. A local or virtual repository signs its indexes with
// its own key, and Quader checks no signature it made itself (Q-SIG-31): only a remote's release documents are detected.

// maxReleaseSignatures bounds the signature packets of one release document a detector names (R-SIG-013).
const maxReleaseSignatures = 16

type signatureAdapter struct{}

// Signatures implements formats.Format.
func (f *Format) Signatures() formats.SignatureAdapter { return signatureAdapter{} }

// Declaration implements formats.SignatureAdapter: the row of Debian in R-SIG-002.
func (signatureAdapter) Declaration() formats.SignatureDeclaration {
	return formats.SignatureDeclaration{Attests: attests, Kinds: []formats.SignatureKind{
		{
			Name: "release signature", Schemes: []string{formats.SignatureSchemeOpenPGP}, Role: formats.SignatureRoleRegistry,
			Location: "the OpenPGP signatures of dists/<suite>/InRelease and of Release.gpg",
			Covers:   "the release document, which names the index files, which name each package's SHA-256",
			Client:   "dists/<suite>/InRelease, Release, Release.gpg",
		},
	}}
}

// Gate implements formats.SignatureAdapter (R-SIG-063): no package of Debian carries a signature of its publisher, so a Debian repository
// is not offered the gate (Q-SIG-80) and nothing it stores is judged.
func (signatureAdapter) Gate(_ context.Context, _ formats.Host, _ formats.RepoRef, items []formats.GateItem) ([]formats.GateSubject, error) {
	return formats.GateNoneFor(items), nil
}

// Detect implements formats.SignatureAdapter.
func (signatureAdapter) Detect(_ context.Context, ev formats.SignatureEvent) ([]formats.SignatureCandidate, error) {
	if ev.Repo.Class != formats.Remote || !strings.HasPrefix(ev.Path.Path, dirDists+"/") {
		return nil, nil
	}
	dir, file := path.Split(ev.Path.Path)
	self := formats.SignatureLocator{Blob: sum32(ev.Digests.SHA256), MediaType: ev.MediaType}
	one := func(part int) formats.SignatureCandidate {
		return formats.SignatureCandidate{
			Object: self, Part: part, Role: formats.SignatureRoleRegistry,
			SchemeHint: formats.SignatureSchemeOpenPGP, Source: formats.SignatureSourceRelease, Location: ev.Path.Path,
		}
	}
	switch file {
	case FileInRelease:
		n, err := packets(ev)
		if err != nil {
			return nil, err
		}
		out := make([]formats.SignatureCandidate, 0, n)
		for i := range n {
			c := one(i)
			c.Subject, c.SignedContent = self.Blob, formats.SignatureLocator{Blob: self.Blob, Locator: formats.SignatureLocatorCleartext}
			out = append(out, c)
		}
		return out, nil
	case FileReleaseGPG:
		n, err := packets(ev)
		if err != nil {
			return nil, err
		}
		out := make([]formats.SignatureCandidate, 0, n)
		for i := range n {
			c := one(i)
			c.SubjectPath = dir + FileRelease
			out = append(out, c)
		}
		return out, nil
	case FileRelease:
		// Release.gpg stored before its Release: named from this side, its first signature; the read names the others (R-SIG-005).
		return []formats.SignatureCandidate{{
			Object: formats.SignatureLocator{Path: dir + FileReleaseGPG}, Subject: self.Blob, Role: formats.SignatureRoleRegistry,
			SchemeHint: formats.SignatureSchemeOpenPGP, Source: formats.SignatureSourceRelease, Location: dir + FileReleaseGPG,
		}}, nil
	}
	return nil, nil
}

// packets counts the OpenPGP signature packets of a release document, bounded.
func packets(ev formats.SignatureEvent) (int, error) {
	data, err := ev.Content.Bytes(untrusted.MaxOpenPGPInput)
	if err != nil {
		// an InRelease larger than a signature object: the signature block is at its end
		return tailPackets(ev)
	}
	n, err := untrusted.OpenPGPSignatures(data)
	return min(n, maxReleaseSignatures), err
}

// tailPackets reads the last MaxOpenPGPInput bytes of a large InRelease, where its signature block is.
func tailPackets(ev formats.SignatureEvent) (int, error) {
	ra, err := ev.Content.ReaderAt()
	if err != nil {
		return 0, err
	}
	size := ev.Content.Size()
	off := max(size-untrusted.MaxOpenPGPInput, 0)
	buf := make([]byte, size-off)
	if _, err := ra.ReadAt(buf, off); err != nil {
		return 0, err
	}
	n, err := untrusted.OpenPGPSignatures(buf)
	return min(n, maxReleaseSignatures), err
}

func sum32(b []byte) [32]byte {
	var d [32]byte
	copy(d[:], b)
	return d
}

// attests is what Quader's receipt attestation is about in a Debian repository (signatures.md R-SIG-044, Q-SIG-69): the binary
// packages; never an index or a release, which the repository's own key signs.
const attests = "every binary package (.deb, .udeb); never an index"

// AttestationSubjects implements formats.SignatureAdapter (R-SIG-044): a binary package of the pool is attested under its package name
// and version; the releases, the indexes and the by-hash copies are not.
func (signatureAdapter) AttestationSubjects(_ context.Context, _ formats.Host, _ formats.RepoRef, items []formats.AttestationItem) ([]formats.AttestationSubject, error) {
	out := make([]formats.AttestationSubject, len(items))
	for i, it := range items {
		segs := strings.Split(it.Path, "/")
		if segs[0] == dirDists || it.Path == SigningKeyFile {
			continue
		}
		if segs[0] == dirMembers {
			if len(segs) < 3 {
				continue
			}
			segs = segs[2:]
		}
		info := classifyFile(segs)
		if info.Kind == formats.Content && info.Package != "" && info.Version != "" {
			out[i] = formats.AttestationSubject{Attested: true, Name: info.Package, Version: info.Version}
		}
	}
	return out, nil
}
