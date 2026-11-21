// Package nugetcms verifies nuget-cms (signatures.md R-SIG-005, R-SIG-006, R-SIG-007, R-SIG-013, R-SIG-029): the CMS SignedData of a
// NuGet package's .signature.p7s, its repository countersignatures, their RFC 3161 timestamps, and the hash of the package's unsigned
// bytes. The CMS reader (both forms of a SignerInfo's signer, issuer and serial number or subject key identifier: dotnet nuget sign and
// nuget.org write the second), the countersignature, the timestamps, the attribute checks and the unsigned view are code of this package
// on encoding/asn1 (R-SIG-014, Q-SIG-216).
//
// The object is the package blob with the locator "entry:.signature.p7s"; part 0 is the primary signature (an author's, role publisher, or
// a repository's, role registry, by its commitment type), part n the n-th repository countersignature (role registry). The check runs in
// this order, and the first step that fails decides: the entry is read and parsed within the bounds (malformed); the signer's
// certificate is there and its chain links (chain_broken); the digest and signature algorithms are not weak (unsupported); the signed
// attributes NuGet requires are there (attribute_missing); the primary signature verifies over the signature content, and a
// countersignature over the primary signature's value (signature_mismatch); the signature content's hash equals the hash of the
// package's unsigned bytes (digest_mismatch); a timestamp stamps its signer's signature value and verifies with its authority's
// certificate (timestamp_invalid). Every part checks the primary signature and the package too: a countersignature vouches for a primary
// signature that covers these bytes.
package nugetcms

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"strings"

	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
)

// signatureEntry is the entry of a signed package that holds its signature, and the locator that names it.
const (
	signatureEntry   = ".signature.p7s"
	signatureLocator = "entry:" + signatureEntry
	unsignedLocator  = "unsigned"
)

// The commitment types as the signer shows them.
const (
	commitmentAuthor     = "author"
	commitmentRepository = "repository"
)

// Check reads and checks one signature of the scheme nuget-cms. An error is a check that could not run (it wraps [scheme.ErrTransient]);
// every other outcome is the Output's Result.
func Check(ctx context.Context, in scheme.Input) (scheme.Output, error) {
	in.Limits = pki.Limits(in.Limits)
	rd := pki.NewReading(in)
	out, err := check(ctx, in, rd)
	if err != nil {
		return pki.Refused(in, rd, err)
	}
	return out, nil
}

func check(ctx context.Context, in scheme.Input, rd *pki.Reading) (scheme.Output, error) {
	p7s, err := signatureBytes(ctx, in)
	if err != nil {
		return scheme.Output{}, err
	}
	p, err := parse(p7s, in.Limits.ASN1Depth, 2*in.Limits.MaxChain, in.Limits.MaxCertificateSize)
	if err != nil {
		return scheme.Output{}, err
	}
	rd.Parts = 1 + len(p.counters)
	if in.Part < 0 || in.Part >= rd.Parts {
		return scheme.Output{}, pki.Malformed("the signature has %d parts, no part %d", rd.Parts, in.Part)
	}
	si, message := p.primary, p.content
	if in.Part > 0 {
		si, message = p.counters[in.Part-1], p.primary.EncryptedDigest
	}
	chain, err := signerChain(si, p.certs, in.Limits)
	if err != nil {
		return scheme.Output{}, err
	}
	pki.ChainSigner(chain, &rd.Signer)
	rd.KeyIDs = pki.CertificateKeyIDs(chain)
	if err := readAttributes(si, in.Part, rd); err != nil {
		return scheme.Output{}, err
	}
	if err := checkPrimary(p, si, chain[0], message, in.Limits); err != nil {
		return scheme.Output{}, err
	}
	if err := checkContent(ctx, in, p.content); err != nil {
		return scheme.Output{}, err
	}
	stamps, err := timestamps(si, in.Limits, rd)
	if err != nil {
		return scheme.Output{}, err
	}
	return pki.Answer(rd, signatures.StateValid, &scheme.Evidence{Chain: pki.DERs(chain), Timestamps: stamps}), nil
}

// checkPrimary verifies the primary signature (also for a countersignature's part, whose voucher it is) and, for a countersignature, the
// countersignature over the primary signature's value.
func checkPrimary(p *parsed, si *signerInfo, cert *x509.Certificate, message []byte, l scheme.Limits) error {
	if si != p.primary {
		primaryChain, err := signerChain(p.primary, p.certs, l)
		if err != nil {
			return err
		}
		if err := checkSigner(p.primary, primaryChain[0], p.content); err != nil {
			return err
		}
	}
	return checkSigner(si, cert, message)
}

// checkSigner checks the signing-certificate-v2 attribute and the signature of one signer.
func checkSigner(si *signerInfo, cert *x509.Certificate, message []byte) error {
	if err := checkSigningCertificate(si, cert); err != nil {
		return err
	}
	return verify(si, cert, message)
}

// signatureBytes reads the .signature.p7s: the object's bytes when the job hands them, else the entry of the package blob through the
// bounded zip reader.
func signatureBytes(ctx context.Context, in scheme.Input) ([]byte, error) {
	if len(in.Raw) > 0 {
		return pki.Object(ctx, in)
	}
	if in.Locator != signatureLocator {
		return nil, pki.Malformed("the object's locator %q is not the signature entry", in.Locator)
	}
	c, err := pki.Open(ctx, in, in.ItemBlob)
	if err != nil {
		return nil, err
	}
	ra, err := c.ReaderAt()
	if err != nil {
		return nil, pki.Transient(err)
	}
	z, err := untrusted.NewZip(ctx, ra, c.Size(), untrusted.Limits{ManifestEntryBytes: in.Limits.MaxObjectSize})
	if err != nil {
		return nil, zipError(ctx, err)
	}
	for _, e := range z.Entries() {
		if e.Name != signatureEntry {
			continue
		}
		data, err := z.ReadEntry(e, in.Limits.MaxObjectSize)
		if err != nil {
			return nil, zipError(ctx, err)
		}
		return data, nil
	}
	return nil, pki.Malformed("the package holds no %s", signatureEntry)
}

func zipError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return pki.Transient(ctx.Err())
	}
	if untrusted.IsTooLarge(err) {
		return pki.TooLarge("the package or its signature entry is beyond the bounds of the zip reader")
	}
	return pki.Malformed("the package cannot be read as a zip archive")
}

// signerChain finds a signer's certificate and the chain the SignedData carries for it, leaf first, and checks that it links and uses
// no weak algorithm.
func signerChain(si *signerInfo, certs []*x509.Certificate, l scheme.Limits) ([]*x509.Certificate, error) {
	leaf := certificateOf(si, certs)
	if leaf == nil {
		return nil, pki.Invalid(signatures.ReasonChainBroken, "the certificate of the signer (%s) is not in the signature", signerName(si.sid))
	}
	chain := pki.Order(leaf, certs, l.MaxChain+1)
	if len(chain) > l.MaxChain {
		return nil, pki.TooLarge("the chain holds more than signatures.max_chain (%d) certificates", l.MaxChain)
	}
	for _, c := range chain {
		if err := pki.CheckCertificate(c, l); err != nil {
			return nil, err
		}
	}
	if err := pki.ChainSigns(chain); err != nil {
		return nil, err
	}
	return chain, nil
}

// readAttributes reads the commitment type, a repository signature's service index and owners, and the signing time into the signer,
// sets the role by the commitment type, and refuses a signature without the attributes its commitment type requires.
func readAttributes(si *signerInfo, part int, rd *pki.Reading) error {
	if part == 0 && find(si.AuthenticatedAttributes, oidContentType()) == nil {
		return pki.Invalid(signatures.ReasonAttributeMissing, "the primary signature has no content-type attribute")
	}
	ct := commitment(si)
	index, owners := repositoryAttributes(si)
	rd.ClaimedAt = signingTime(si)
	switch {
	case part > 0 && ct != commitmentRepository:
		return pki.Invalid(signatures.ReasonAttributeMissing, "a countersignature is not a repository signature")
	case ct == "other":
		return pki.Unsupported(signatures.ReasonVersionUnsupported, "the commitment type is neither proof of origin nor proof of receipt")
	case ct == commitmentRepository && index == "":
		return pki.Invalid(signatures.ReasonAttributeMissing, "a repository signature has no service index URL")
	case ct == "":
		ct = commitmentAuthor
	}
	rd.Signer.CommitmentType, rd.Signer.ServiceIndex, rd.Signer.Owners = ct, index, owners
	rd.Role = signatures.RolePublisher
	if ct == commitmentRepository {
		rd.Role = signatures.RoleRegistry
	}
	return nil
}

// checkContent checks that the signature content names the hash of the package's unsigned bytes (the content view "unsigned" of the
// content blob, normally the subject itself).
func checkContent(ctx context.Context, in scheme.Input, content []byte) error {
	h, want, err := signatureContent(content)
	if err != nil {
		return err
	}
	if in.ContentLocator != "" && in.ContentLocator != unsignedLocator {
		return pki.Malformed("the content locator %q is not the unsigned package", in.ContentLocator)
	}
	blob := in.ContentBlob
	if blob == ([32]byte{}) {
		blob = in.Subject
	}
	sum := h.New()
	if err := hashUnsigned(ctx, in, blob, sum); err != nil {
		return err
	}
	if !bytes.Equal(sum.Sum(nil), want) {
		return pki.Invalid(signatures.ReasonDigestMismatch, "the package's unsigned bytes are not what the signature names")
	}
	return nil
}

// contentHash is the hash algorithm a signature content names by its OID's text, 0 for one NuGet does not use.
func contentHash(oid string) crypto.Hash {
	switch oid {
	case oidSHA256().String():
		return crypto.SHA256
	case oidSHA384().String():
		return crypto.SHA384
	case oidSHA512().String():
		return crypto.SHA512
	}
	return 0
}

// signatureContent reads NuGet's signature content: "Version:1", a blank line, "<hash OID>-Hash:<base64 hash>", each line ended by CRLF
// (a bare LF is accepted too).
func signatureContent(content []byte) (crypto.Hash, []byte, error) {
	var lines []string
	for _, l := range strings.Split(string(content), "\n") {
		if l = strings.TrimSuffix(l, "\r"); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) < 2 || lines[0] != "Version:1" {
		return 0, nil, pki.Unsupported(signatures.ReasonVersionUnsupported, "the signature content is not of version 1")
	}
	oid, b64, ok := strings.Cut(lines[1], "-Hash:")
	if !ok {
		return 0, nil, pki.Malformed("the signature content names no package hash")
	}
	h := contentHash(oid)
	if h == 0 {
		if oid == oidSHA1().String() {
			return 0, nil, pki.Unsupported(signatures.ReasonWeakAlgorithm, "the signature content's hash is SHA-1")
		}
		return 0, nil, pki.Unsupported(signatures.ReasonVersionUnsupported, "the signature content's hash %s is not one NuGet uses", oid)
	}
	want, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(want) != h.Size() {
		return 0, nil, pki.Malformed("the signature content's package hash does not decode")
	}
	return h, want, nil
}
