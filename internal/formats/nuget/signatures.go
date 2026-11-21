package nuget

import (
	"context"
	"crypto/sha256"
	"encoding/asn1"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// The signature of NuGet (signatures.md R-SIG-002, R-SIG-029): the entry .signature.p7s of a package, a CMS SignedData whose signer is
// the author (proof of origin) or a repository (proof of receipt), and whose unsigned attributes may hold repository countersignatures.
// The entry is part of the package's bytes: every repository that commits the package gets an attachment with the source embedded.

// signatureEntry is the name of the signature entry of a signed package (NuGet's package signature technical details).
const signatureEntry = ".signature.p7s"

// maxCountersignatures bounds the countersignatures a detector names (R-SIG-013).
const maxCountersignatures = 8

// The object identifiers the detector reads (RFC 5652, RFC 5126 and NuGet's technical details).
func oidSignedData() asn1.ObjectIdentifier { return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2} }

func oidCommitmentType() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 16}
}

func oidProofOfReceipt() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 6, 2}
}

func oidCountersignatures() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 6}
}

type signatureAdapter struct{}

// Signatures implements formats.Format.
func (f *Format) Signatures() formats.SignatureAdapter { return signatureAdapter{} }

// Declaration implements formats.SignatureAdapter: the row of NuGet in R-SIG-002.
func (signatureAdapter) Declaration() formats.SignatureDeclaration {
	const where = "the entry .signature.p7s inside the .nupkg"
	return formats.SignatureDeclaration{Attests: attests, Kinds: []formats.SignatureKind{
		{
			Name: "author signature", Schemes: []string{formats.SignatureSchemeNuGetCMS}, Role: formats.SignatureRolePublisher,
			Location: where + ", the primary signature with the commitment type proof of origin",
			Covers:   "the package (a hash of its unsigned bytes)", Client: ".signature.p7s inside the .nupkg",
		},
		{
			Name: "repository signature", Schemes: []string{formats.SignatureSchemeNuGetCMS}, Role: formats.SignatureRoleRegistry,
			Location: where + ", the primary signature with proof of receipt or a repository countersignature",
			Covers:   "the package (a hash of its unsigned bytes)", Client: ".signature.p7s inside the .nupkg",
		},
	}, ArtifactName: artifactName}
}

// Gate implements formats.SignatureAdapter (R-SIG-063): a version's .nupkg is the artifact; its .snupkg is one too and passes on the
// .nupkg of the same ID and version in the repository, which carries the author's signature (Q-SIG-81); what Quader derives from a
// package (the .nuspec, the embedded files, the symbol files) is no artifact of its own.
func (signatureAdapter) Gate(ctx context.Context, h formats.Host, repo formats.RepoRef, items []formats.GateItem) ([]formats.GateSubject, error) {
	out := make([]formats.GateSubject, len(items))
	for i, it := range items {
		segs := strings.Split(it.Path, "/")
		if len(segs) != 3 {
			continue
		}
		pkg, ver, file := segs[0], segs[1], segs[2]
		switch file {
		case NupkgFile(pkg, ver):
			out[i].Kind = formats.GateArtifact
		case SnupkgFile(pkg, ver):
			out[i].Kind = formats.GateArtifact
			lead, found, err := h.Items().Stat(ctx, repo, formats.Path{Path: NupkgPath(pkg, ver)})
			if err != nil {
				return nil, err
			}
			if found && len(lead.Digests.SHA256) == 32 {
				out[i].Covering = [][32]byte{[32]byte(lead.Digests.SHA256)}
			}
		}
	}
	return out, nil
}

// Detect implements formats.SignatureAdapter: a committed .nupkg (a push, a fill) is read once for its central directory; a package
// with a signature entry names the primary signature as part 0 and each countersignature as part 1, 2, … (R-SIG-029). The read
// confirms the roles and the parts it finds (R-SIG-005).
func (signatureAdapter) Detect(ctx context.Context, ev formats.SignatureEvent) ([]formats.SignatureCandidate, error) {
	if ev.Item.ID == 0 || !strings.HasSuffix(ev.Path.Path, nupkgSuffix) {
		return nil, nil
	}
	ra, err := ev.Content.ReaderAt()
	if err != nil {
		return nil, err
	}
	z, err := untrusted.NewZip(ctx, ra, ev.Content.Size(), untrusted.Limits{ManifestEntryBytes: formats.MaxSignatureRaw})
	if err != nil {
		return nil, err
	}
	var entry *untrusted.ZipEntry
	for _, e := range z.Entries() {
		if e.Name == signatureEntry {
			entry = &e
			break
		}
	}
	if entry == nil {
		return nil, nil
	}
	data, err := z.ReadEntry(*entry, formats.MaxSignatureRaw)
	if err != nil {
		return nil, err
	}
	pkg := sum32(ev.Digests.SHA256)
	obj := formats.SignatureLocator{
		Blob: pkg, Locator: formats.SignatureLocatorEntryPrefix + signatureEntry, SHA256: sha256.Sum256(data), Size: int64(len(data)),
		MediaType: "application/pkcs7-signature",
	}
	content := formats.SignatureLocator{Blob: pkg, Locator: formats.SignatureLocatorUnsigned}
	primaryRole, counters := readCMS(data)
	one := func(part int, role string) formats.SignatureCandidate {
		return formats.SignatureCandidate{
			Object: obj, Part: part, Subject: pkg, Role: role, SchemeHint: formats.SignatureSchemeNuGetCMS,
			Source: formats.SignatureSourceEmbedded, Location: ev.Path.Path, SignedContent: content,
		}
	}
	out := []formats.SignatureCandidate{one(0, primaryRole)}
	for i := range min(counters, maxCountersignatures) {
		out = append(out, one(i+1, formats.SignatureRoleRegistry))
	}
	return out, nil
}

// cmsSignerInfo is what the detector reads of a SignerInfo: its signed attributes [0] and its unsigned attributes [1].
type cmsSignerInfo struct {
	SignedAttrs, UnsignedAttrs asn1.RawValue
}

type cmsAttribute struct {
	Type   asn1.ObjectIdentifier
	Values asn1.RawValue
}

// readCMS reads the role of the primary signature from its commitment type and counts the countersignatures of its unsigned attributes.
// It checks nothing: a structure it cannot read gives the publisher's role and no countersignature, and the read judges the object.
func readCMS(data []byte) (role string, counters int) {
	role = formats.SignatureRolePublisher
	si, ok := firstSignerInfo(data)
	if !ok {
		return role, 0
	}
	if commitment(si.SignedAttrs) {
		role = formats.SignatureRoleRegistry
	}
	return role, countersignatures(si.UnsignedAttrs)
}

// firstSignerInfo walks ContentInfo and SignedData to the first SignerInfo (RFC 5652, section 5).
func firstSignerInfo(data []byte) (cmsSignerInfo, bool) {
	var ci struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,tag:0"`
	}
	if _, err := asn1.Unmarshal(data, &ci); err != nil || !ci.ContentType.Equal(oidSignedData()) {
		return cmsSignerInfo{}, false
	}
	var sd asn1.RawValue
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return cmsSignerInfo{}, false
	}
	// SignedData's last element is the SET OF SignerInfo; certificates [0] and crls [1] before it are optional.
	var last asn1.RawValue
	for rest := sd.Bytes; len(rest) > 0; {
		var el asn1.RawValue
		r, err := asn1.Unmarshal(rest, &el)
		if err != nil {
			return cmsSignerInfo{}, false
		}
		last, rest = el, r
	}
	if last.Tag != asn1.TagSet || len(last.Bytes) == 0 {
		return cmsSignerInfo{}, false
	}
	var first asn1.RawValue
	if _, err := asn1.Unmarshal(last.Bytes, &first); err != nil || first.Tag != asn1.TagSequence {
		return cmsSignerInfo{}, false
	}
	return signerInfoOf(first.Bytes)
}

// signerInfoOf walks the elements of a SignerInfo and keeps its attribute sets (RFC 5652, section 5.3).
func signerInfoOf(b []byte) (cmsSignerInfo, bool) {
	var si cmsSignerInfo
	for rest := b; len(rest) > 0; {
		var el asn1.RawValue
		r, err := asn1.Unmarshal(rest, &el)
		if err != nil {
			return cmsSignerInfo{}, false
		}
		if el.Class == asn1.ClassContextSpecific {
			switch el.Tag {
			case 0:
				si.SignedAttrs = el
			case 1:
				si.UnsignedAttrs = el
			}
		}
		rest = r
	}
	return si, true
}

// attributes reads the attributes of an implicitly tagged SET OF Attribute.
func attributes(raw asn1.RawValue) []cmsAttribute {
	var out []cmsAttribute
	for rest := raw.Bytes; len(rest) > 0; {
		var a cmsAttribute
		r, err := asn1.Unmarshal(rest, &a)
		if err != nil {
			return out
		}
		out, rest = append(out, a), r
	}
	return out
}

// commitment reports whether the signed attributes name the commitment type proof of receipt: a repository signature.
func commitment(signed asn1.RawValue) bool {
	for _, a := range attributes(signed) {
		if !a.Type.Equal(oidCommitmentType()) {
			continue
		}
		var inner asn1.RawValue
		if _, err := asn1.Unmarshal(a.Values.Bytes, &inner); err != nil {
			return false
		}
		var id asn1.ObjectIdentifier
		if _, err := asn1.Unmarshal(inner.Bytes, &id); err == nil && id.Equal(oidProofOfReceipt()) {
			return true
		}
	}
	return false
}

// countersignatures counts the SignerInfos of the countersignature attributes.
func countersignatures(unsigned asn1.RawValue) int {
	n := 0
	for _, a := range attributes(unsigned) {
		if !a.Type.Equal(oidCountersignatures()) {
			continue
		}
		for rest := a.Values.Bytes; len(rest) > 0; {
			var el asn1.RawValue
			r, err := asn1.Unmarshal(rest, &el)
			if err != nil {
				break
			}
			n, rest = n+1, r
		}
	}
	return n
}

func sum32(b []byte) [32]byte {
	var d [32]byte
	copy(d[:], b)
	return d
}

// artifactName names a package as NuGet's client does (Q-SIG-213): "<id> <version>".
func artifactName(_ string, info formats.PathInfo) string {
	if info.Package == "" || info.Version == "" {
		return ""
	}
	return info.Package + " " + info.Version
}

// attests is what Quader's receipt attestation is about in a NuGet repository (signatures.md R-SIG-044).
const attests = "every .nupkg and every .snupkg"

// AttestationSubjects implements formats.SignatureAdapter (R-SIG-044): a version's .nupkg and its .snupkg are attested, one statement
// each (R-SIG-051), under the package ID and the version of the path; what Quader derives from a package is not.
func (signatureAdapter) AttestationSubjects(_ context.Context, _ formats.Host, _ formats.RepoRef, items []formats.AttestationItem) ([]formats.AttestationSubject, error) {
	out := make([]formats.AttestationSubject, len(items))
	for i, it := range items {
		segs := strings.Split(it.Path, "/")
		if len(segs) != 3 {
			continue
		}
		pkg, ver, file := segs[0], segs[1], segs[2]
		if file == NupkgFile(pkg, ver) || file == SnupkgFile(pkg, ver) {
			out[i] = formats.AttestationSubject{Attested: true, Name: pkg, Version: ver}
		}
	}
	return out, nil
}
