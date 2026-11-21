package nugetcms

import (
	"bytes"
	"crypto"
	"crypto/subtle"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"time"

	"github.com/digitorus/pkcs7"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
)

// The object identifiers of a NuGet signature (RFC 5652, RFC 5126, RFC 5035, RFC 3161 and NuGet's package signature technical details,
// https://github.com/NuGet/Home/wiki/Package-Signatures-Technical-Details).
func oidContentType() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
}

func oidMessageDigest() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
}

func oidSigningTime() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 5}
}

func oidCountersignature() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 6}
}

func oidTimestampToken() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 14}
}

func oidCommitmentType() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 16}
}

func oidSigningCertV2() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47}
}

func oidProofOfOrigin() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 6, 1}
}

func oidProofOfReceipt() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 6, 2}
}

func oidServiceIndex() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 84, 2, 1, 1, 1}
}

func oidPackageOwners() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 84, 2, 1, 1, 2}
}

func oidSHA1() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
}

func oidSHA256() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
}

func oidSHA384() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
}

func oidSHA512() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
}

func oidRSA() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
}

func oidRSASHA256() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
}

func oidRSASHA384() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 12}
}

func oidRSASHA512() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 13}
}

func oidECPublicKey() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 10045, 2, 1}
}

func oidECDSASHA256() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
}

func oidECDSASHA384() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
}

func oidECDSASHA512() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 4}
}

func oidEd25519() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 3, 101, 112}
}

func oidRSASHA1() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 5}
}

func oidECDSASHA1() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 1}
}

func oidRSAPSS() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 10}
}

func oidDSASignature() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 10040, 4, 3}
}

// maxCountersignatures bounds the countersignatures a check reads (R-SIG-013): nuget.org adds one.
const maxCountersignatures = 16

// attribute is a CMS attribute: its type and its SET OF values.
type attribute struct {
	Type  asn1.ObjectIdentifier
	Value asn1.RawValue `asn1:"set"`
}

// issuerAndSerial names a signer's certificate.
type issuerAndSerial struct {
	IssuerName   asn1.RawValue
	SerialNumber *big.Int
}

// signerInfo is a CMS SignerInfo (RFC 5652, section 5.3) of version 1, identified by issuer and serial, as a countersignature carries it.
type signerInfo struct {
	Version                   int `asn1:"default:1"`
	IssuerAndSerialNumber     issuerAndSerial
	DigestAlgorithm           pkix.AlgorithmIdentifier
	AuthenticatedAttributes   []attribute `asn1:"optional,omitempty,tag:0"`
	DigestEncryptionAlgorithm pkix.AlgorithmIdentifier
	EncryptedDigest           []byte
	UnauthenticatedAttributes []attribute `asn1:"optional,omitempty,tag:1"`
}

// parsed is the CMS SignedData of a .signature.p7s: its content (the signature content), its certificates, its primary signer and the
// countersignatures of the primary.
type parsed struct {
	content  []byte
	certs    []*x509.Certificate
	primary  *signerInfo
	counters []*signerInfo
}

// parse parses a .signature.p7s with digitorus/pkcs7, after the bounds of R-SIG-013.
func parse(p7s []byte, maxDepth, maxCerts, maxCertSize int) (*parsed, error) {
	if err := pki.CheckBER(p7s, maxDepth); err != nil {
		return nil, err
	}
	p7, err := pkcs7.Parse(p7s)
	if err != nil {
		return nil, pki.Malformed("the .signature.p7s is no CMS SignedData")
	}
	if len(p7.Signers) != 1 {
		return nil, pki.Malformed("the .signature.p7s has %d signers, a NuGet signature one", len(p7.Signers))
	}
	if len(p7.Certificates) > maxCerts {
		return nil, pki.TooLarge("the .signature.p7s carries %d certificates, more than %d", len(p7.Certificates), maxCerts)
	}
	for _, c := range p7.Certificates {
		if len(c.Raw) > maxCertSize {
			return nil, pki.TooLarge("a certificate of the .signature.p7s is larger than %d bytes", maxCertSize)
		}
	}
	s := p7.Signers[0]
	primary := &signerInfo{
		Version: s.Version, DigestAlgorithm: s.DigestAlgorithm, DigestEncryptionAlgorithm: s.DigestEncryptionAlgorithm,
		EncryptedDigest:       s.EncryptedDigest,
		IssuerAndSerialNumber: issuerAndSerial{IssuerName: s.IssuerAndSerialNumber.IssuerName, SerialNumber: s.IssuerAndSerialNumber.SerialNumber},
	}
	for _, a := range s.AuthenticatedAttributes {
		primary.AuthenticatedAttributes = append(primary.AuthenticatedAttributes, attribute{Type: a.Type, Value: a.Value})
	}
	for _, a := range s.UnauthenticatedAttributes {
		primary.UnauthenticatedAttributes = append(primary.UnauthenticatedAttributes, attribute{Type: a.Type, Value: a.Value})
	}
	counters, err := countersignatures(primary)
	if err != nil {
		return nil, err
	}
	return &parsed{content: p7.Content, certs: p7.Certificates, primary: primary, counters: counters}, nil
}

// countersignatures reads the SignerInfos of the primary's countersignature attributes, in order.
func countersignatures(primary *signerInfo) ([]*signerInfo, error) {
	var out []*signerInfo
	for _, a := range primary.UnauthenticatedAttributes {
		if !a.Type.Equal(oidCountersignature()) {
			continue
		}
		for rest := a.Value.Bytes; len(rest) > 0; {
			if len(out) == maxCountersignatures {
				return nil, pki.TooLarge("the signature has more than %d countersignatures", maxCountersignatures)
			}
			si := new(signerInfo)
			r, err := asn1.Unmarshal(rest, si)
			if err != nil {
				return nil, pki.Malformed("a countersignature does not parse")
			}
			out, rest = append(out, si), r
		}
	}
	return out, nil
}

// find is the first attribute of a type, nil when there is none.
func find(attrs []attribute, oid asn1.ObjectIdentifier) *attribute {
	for i := range attrs {
		if attrs[i].Type.Equal(oid) {
			return &attrs[i]
		}
	}
	return nil
}

// value unmarshals the first value of an attribute.
func value(a *attribute, out any) bool {
	if a == nil {
		return false
	}
	_, err := asn1.Unmarshal(a.Value.Bytes, out)
	return err == nil
}

// certificateOf finds a signer's certificate among the certificates of the SignedData by issuer and serial.
func certificateOf(si *signerInfo, certs []*x509.Certificate) *x509.Certificate {
	for _, c := range certs {
		if bytes.Equal(c.RawIssuer, si.IssuerAndSerialNumber.IssuerName.FullBytes) && si.IssuerAndSerialNumber.SerialNumber != nil &&
			c.SerialNumber.Cmp(si.IssuerAndSerialNumber.SerialNumber) == 0 {
			return c
		}
	}
	return nil
}

// digestHash is the hash a digest algorithm names: SHA-256, SHA-384 or SHA-512; SHA-1 is weak.
func digestHash(oid asn1.ObjectIdentifier) (crypto.Hash, error) {
	switch {
	case oid.Equal(oidSHA256()):
		return crypto.SHA256, nil
	case oid.Equal(oidSHA384()):
		return crypto.SHA384, nil
	case oid.Equal(oidSHA512()):
		return crypto.SHA512, nil
	case oid.Equal(oidSHA1()):
		return 0, pki.Unsupported(signatures.ReasonWeakAlgorithm, "the signature uses SHA-1")
	}
	return 0, pki.Unsupported(signatures.ReasonVersionUnsupported, "the digest algorithm %s is not one NuGet signs with", oid)
}

// signatureAlgorithm is the x509 algorithm of a signer's digest and signature algorithms.
func signatureAlgorithm(enc asn1.ObjectIdentifier, h crypto.Hash) (x509.SignatureAlgorithm, error) {
	byHash := func(sha256, sha384, sha512 x509.SignatureAlgorithm) x509.SignatureAlgorithm {
		return map[crypto.Hash]x509.SignatureAlgorithm{crypto.SHA256: sha256, crypto.SHA384: sha384, crypto.SHA512: sha512}[h]
	}
	switch {
	case enc.Equal(oidRSA()), enc.Equal(oidRSASHA256()), enc.Equal(oidRSASHA384()), enc.Equal(oidRSASHA512()):
		return byHash(x509.SHA256WithRSA, x509.SHA384WithRSA, x509.SHA512WithRSA), nil
	case enc.Equal(oidECPublicKey()), enc.Equal(oidECDSASHA256()), enc.Equal(oidECDSASHA384()), enc.Equal(oidECDSASHA512()):
		return byHash(x509.ECDSAWithSHA256, x509.ECDSAWithSHA384, x509.ECDSAWithSHA512), nil
	case enc.Equal(oidEd25519()):
		return x509.PureEd25519, nil
	case enc.Equal(oidRSASHA1()), enc.Equal(oidECDSASHA1()), enc.Equal(oidDSASignature()):
		return 0, pki.Unsupported(signatures.ReasonWeakAlgorithm, "the signature uses a weak algorithm")
	case enc.Equal(oidRSAPSS()):
		return 0, pki.Unsupported(signatures.ReasonVersionUnsupported, "an RSASSA-PSS signature is not one Quader reads in NuGet")
	}
	return 0, pki.Unsupported(signatures.ReasonVersionUnsupported, "the signature algorithm %s is not one NuGet signs with", enc)
}

// verify checks one SignerInfo over message with its certificate (RFC 5652, section 5.4): its message-digest attribute is the hash of the
// message, and the certificate's key verifies the signature over the DER of the signed attributes.
func verify(si *signerInfo, cert *x509.Certificate, message []byte) error {
	h, err := digestHash(si.DigestAlgorithm.Algorithm)
	if err != nil {
		return err
	}
	alg, err := signatureAlgorithm(si.DigestEncryptionAlgorithm.Algorithm, h)
	if err != nil {
		return err
	}
	var md []byte
	if !value(find(si.AuthenticatedAttributes, oidMessageDigest()), &md) {
		return pki.Invalid(signatures.ReasonAttributeMissing, "the signer has no message-digest attribute")
	}
	sum := h.New()
	sum.Write(message)
	if subtle.ConstantTimeCompare(md, sum.Sum(nil)) != 1 {
		return pki.Invalid(signatures.ReasonSignatureMismatch, "the signer's message digest is not the hash of what it signs")
	}
	signed, err := asn1.Marshal(struct {
		A []attribute `asn1:"set"`
	}{A: si.AuthenticatedAttributes})
	if err != nil {
		return pki.Malformed("the signed attributes do not encode")
	}
	var set asn1.RawValue
	if _, err := asn1.Unmarshal(signed, &set); err != nil {
		return pki.Malformed("the signed attributes do not encode")
	}
	if err := cert.CheckSignature(alg, set.Bytes, si.EncryptedDigest); err != nil {
		return pki.Invalid(signatures.ReasonSignatureMismatch, "the signature does not verify with the signer's certificate")
	}
	return nil
}

// essCertIDv2 and signingCertificateV2 are RFC 5035's signing-certificate-v2 attribute.
type essCertIDv2 struct {
	HashAlgorithm pkix.AlgorithmIdentifier `asn1:"optional"`
	CertHash      []byte
	IssuerSerial  asn1.RawValue `asn1:"optional"`
}

type signingCertificateV2 struct {
	Certs    []essCertIDv2
	Policies asn1.RawValue `asn1:"optional"`
}

// checkSigningCertificate checks the signing-certificate-v2 attribute NuGet requires: its first entry is the hash of the signer's
// certificate (SHA-256 when it names no algorithm).
func checkSigningCertificate(si *signerInfo, cert *x509.Certificate) error {
	var sc signingCertificateV2
	if !value(find(si.AuthenticatedAttributes, oidSigningCertV2()), &sc) || len(sc.Certs) == 0 {
		return pki.Invalid(signatures.ReasonAttributeMissing, "the signer has no signing-certificate-v2 attribute")
	}
	h := crypto.SHA256
	if alg := sc.Certs[0].HashAlgorithm.Algorithm; len(alg) > 0 {
		var err error
		if h, err = digestHash(alg); err != nil {
			return err
		}
	}
	sum := h.New()
	sum.Write(cert.Raw)
	if !bytes.Equal(sum.Sum(nil), sc.Certs[0].CertHash) {
		return pki.Invalid(signatures.ReasonChainBroken, "the signing-certificate-v2 attribute names another certificate")
	}
	return nil
}

// commitment reads a signer's commitment type: "author" (proof of origin, also when the attribute is absent from a primary signature) or
// "repository" (proof of receipt).
func commitment(si *signerInfo) string {
	var ct struct {
		ID         asn1.ObjectIdentifier
		Qualifiers asn1.RawValue `asn1:"optional"`
	}
	if !value(find(si.AuthenticatedAttributes, oidCommitmentType()), &ct) {
		return ""
	}
	switch {
	case ct.ID.Equal(oidProofOfOrigin()):
		return commitmentAuthor
	case ct.ID.Equal(oidProofOfReceipt()):
		return commitmentRepository
	}
	return "other"
}

// repositoryAttributes reads a repository signature's service index URL and package owners.
func repositoryAttributes(si *signerInfo) (index string, owners []string) {
	var ia5 string
	if a := find(si.AuthenticatedAttributes, oidServiceIndex()); a != nil {
		if _, err := asn1.Unmarshal(a.Value.Bytes, &ia5); err == nil {
			index = ia5
		}
	}
	var list []string
	if a := find(si.AuthenticatedAttributes, oidPackageOwners()); a != nil {
		if _, err := asn1.Unmarshal(a.Value.Bytes, &list); err == nil {
			owners = list
		}
	}
	return index, owners
}

// signingTime reads the signing-time attribute, the signer's claim.
func signingTime(si *signerInfo) time.Time {
	var t time.Time
	if value(find(si.AuthenticatedAttributes, oidSigningTime()), &t) {
		return t.UTC()
	}
	return time.Time{}
}
