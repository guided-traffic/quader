package nugetcms

import (
	"bytes"
	"crypto"
	"crypto/sha1" //nolint:gosec // RFC 5280's key identifier of a certificate without the extension
	"crypto/subtle"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"math/big"
	"time"

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

func oidSignedData() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
}

// maxCountersignatures bounds the countersignatures a check reads (R-SIG-013): nuget.org adds one.
const maxCountersignatures = 16

// attribute is a CMS attribute: its type and its SET OF values.
type attribute struct {
	Type  asn1.ObjectIdentifier
	Value asn1.RawValue `asn1:"set"`
}

// The CMS structures as they are encoded (RFC 5652, sections 3, 5.1 to 5.3). The reader is code of this package on encoding/asn1
// (Q-SIG-216): a SignerInfo names its signer by issuer and serial number (version 1) or by subject key identifier (version 3), and
// dotnet nuget sign and nuget.org write the second, which digitorus/pkcs7 does not read.
type (
	contentInfo struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,optional,tag:0"`
	}
	encapsulatedContentInfo struct {
		EContentType asn1.ObjectIdentifier
		EContent     asn1.RawValue `asn1:"explicit,optional,tag:0"`
	}
	signedData struct {
		Version          int
		DigestAlgorithms asn1.RawValue
		EncapContentInfo encapsulatedContentInfo
		Certificates     asn1.RawValue `asn1:"optional,tag:0"`
		CRLs             asn1.RawValue `asn1:"optional,tag:1"`
		SignerInfos      asn1.RawValue
	}
	signerInfoASN1 struct {
		Version            int
		SID                asn1.RawValue
		DigestAlgorithm    pkix.AlgorithmIdentifier
		SignedAttrs        asn1.RawValue `asn1:"optional,tag:0"`
		SignatureAlgorithm pkix.AlgorithmIdentifier
		Signature          []byte
		UnsignedAttrs      asn1.RawValue `asn1:"optional,tag:1"`
	}
	issuerAndSerial struct {
		IssuerName   asn1.RawValue
		SerialNumber *big.Int
	}
)

// signerInfo is a CMS SignerInfo as the checks read it: who signed (sid, one of the two forms of RFC 5652, section 5.3), its algorithms,
// its signed attributes with the DER the signature covers (an explicit SET OF, section 5.4), its signature and its unsigned attributes.
type signerInfo struct {
	sid                       asn1.RawValue
	DigestAlgorithm           pkix.AlgorithmIdentifier
	AuthenticatedAttributes   []attribute
	signedDER                 []byte
	DigestEncryptionAlgorithm pkix.AlgorithmIdentifier
	EncryptedDigest           []byte
	UnauthenticatedAttributes []attribute
}

// parsed is the CMS SignedData of a .signature.p7s: its content (the signature content), its certificates, its primary signer and the
// countersignatures of the primary.
type parsed struct {
	content  []byte
	certs    []*x509.Certificate
	primary  *signerInfo
	counters []*signerInfo
}

// parse parses a .signature.p7s after the bounds of R-SIG-013.
func parse(p7s []byte, maxDepth, maxCerts, maxCertSize int) (*parsed, error) {
	sd, err := parseSignedData(p7s, maxDepth, maxCerts, maxCertSize)
	if err != nil {
		return nil, err
	}
	if len(sd.signers) != 1 {
		return nil, pki.Malformed("the .signature.p7s has %d signers, a NuGet signature one", len(sd.signers))
	}
	primary := sd.signers[0]
	counters, err := countersignatures(primary)
	if err != nil {
		return nil, err
	}
	return &parsed{content: sd.content, certs: sd.certs, primary: primary, counters: counters}, nil
}

// cms is a SignedData as read: the encapsulated content's type and bytes, the certificates and the signers.
type cms struct {
	contentType asn1.ObjectIdentifier
	content     []byte
	certs       []*x509.Certificate
	signers     []*signerInfo
}

// parseSignedData reads a ContentInfo of signedData within the bounds: at most maxDepth levels of nesting (BER is brought to definite
// lengths first, so that a writer's BER reads as its DER would), at most maxCerts certificates of at most maxCertSize bytes each.
func parseSignedData(der []byte, maxDepth, maxCerts, maxCertSize int) (*cms, error) {
	der, err := pki.Definite(der, maxDepth)
	if err != nil {
		return nil, err
	}
	// An explicitly tagged RawValue holds the tag's element; its Bytes are the encoding of what it wraps.
	var ci contentInfo
	if rest, err := asn1.Unmarshal(der, &ci); err != nil || len(rest) > 0 || !ci.ContentType.Equal(oidSignedData()) || len(ci.Content.Bytes) == 0 {
		return nil, pki.Malformed("the structure is no CMS SignedData")
	}
	var sd signedData
	if rest, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil || len(rest) > 0 {
		return nil, pki.Malformed("the CMS SignedData does not parse")
	}
	out := &cms{contentType: sd.EncapContentInfo.EContentType}
	if wrapped := sd.EncapContentInfo.EContent.Bytes; len(wrapped) > 0 {
		var os asn1.RawValue
		if rest, err := asn1.Unmarshal(wrapped, &os); err != nil || len(rest) > 0 {
			return nil, pki.Malformed("the encapsulated content does not parse")
		}
		if out.content, err = octets(os); err != nil {
			return nil, err
		}
	}
	if out.certs, err = certificates(sd.Certificates, maxCerts, maxCertSize); err != nil {
		return nil, err
	}
	if out.signers, err = signerInfos(sd.SignerInfos.Bytes); err != nil {
		return nil, err
	}
	return out, nil
}

// octets reads an OCTET STRING, a constructed one (BER) as its segments in order.
func octets(v asn1.RawValue) ([]byte, error) {
	if v.Class != asn1.ClassUniversal || v.Tag != asn1.TagOctetString {
		return nil, pki.Malformed("the encapsulated content is no OCTET STRING")
	}
	if !v.IsCompound {
		return v.Bytes, nil
	}
	var out []byte
	for rest := v.Bytes; len(rest) > 0; {
		var seg asn1.RawValue
		r, err := asn1.Unmarshal(rest, &seg)
		if err != nil {
			return nil, pki.Malformed("a segment of the encapsulated content does not parse")
		}
		b, err := octets(seg)
		if err != nil {
			return nil, err
		}
		out, rest = append(out, b...), r
	}
	return out, nil
}

// certificates reads the CertificateSet [0]: the X.509 certificates in it; an attribute certificate or another format is left out.
func certificates(set asn1.RawValue, maxCerts, maxCertSize int) ([]*x509.Certificate, error) {
	var out []*x509.Certificate
	for rest := set.Bytes; len(rest) > 0; {
		var el asn1.RawValue
		r, err := asn1.Unmarshal(rest, &el)
		if err != nil {
			return nil, pki.Malformed("the certificates of the SignedData do not parse")
		}
		rest = r
		if el.Class != asn1.ClassUniversal || el.Tag != asn1.TagSequence {
			continue
		}
		if len(out) == maxCerts {
			return nil, pki.TooLarge("the SignedData carries more than %d certificates", maxCerts)
		}
		if len(el.FullBytes) > maxCertSize {
			return nil, pki.TooLarge("a certificate of the SignedData is larger than %d bytes", maxCertSize)
		}
		c, err := x509.ParseCertificate(el.FullBytes)
		if err != nil {
			return nil, pki.Malformed("a certificate of the SignedData does not parse")
		}
		out = append(out, c)
	}
	return out, nil
}

// signerInfos reads the SignerInfos of a SET OF SignerInfo's content (the SignedData's, or a countersignature attribute's values).
func signerInfos(b []byte) ([]*signerInfo, error) {
	var out []*signerInfo
	for rest := b; len(rest) > 0; {
		if len(out) == maxCountersignatures {
			return nil, pki.TooLarge("the structure holds more than %d signers", maxCountersignatures)
		}
		var w signerInfoASN1
		r, err := asn1.Unmarshal(rest, &w)
		if err != nil {
			return nil, pki.Malformed("a SignerInfo does not parse")
		}
		si, err := signerOf(w)
		if err != nil {
			return nil, err
		}
		out, rest = append(out, si), r
	}
	return out, nil
}

// signerOf reads the attributes of an encoded SignerInfo and keeps the DER its signature covers: the signed attributes with the
// explicit SET OF tag in place of the implicit [0] (RFC 5652, section 5.4).
func signerOf(w signerInfoASN1) (*signerInfo, error) {
	si := &signerInfo{
		sid: w.SID, DigestAlgorithm: w.DigestAlgorithm, DigestEncryptionAlgorithm: w.SignatureAlgorithm, EncryptedDigest: w.Signature,
	}
	if !sidReadable(w.SID) {
		return nil, pki.Malformed("a SignerInfo names its signer neither by issuer and serial number nor by subject key identifier")
	}
	var err error
	if len(w.SignedAttrs.FullBytes) > 0 {
		if !w.SignedAttrs.IsCompound {
			return nil, pki.Malformed("the signed attributes of a SignerInfo are not a SET")
		}
		si.signedDER = append([]byte{0x31}, w.SignedAttrs.FullBytes[1:]...)
		if si.AuthenticatedAttributes, err = attributes(w.SignedAttrs.Bytes); err != nil {
			return nil, err
		}
	}
	if len(w.UnsignedAttrs.FullBytes) > 0 {
		if si.UnauthenticatedAttributes, err = attributes(w.UnsignedAttrs.Bytes); err != nil {
			return nil, err
		}
	}
	return si, nil
}

// attributes reads the content of a SET OF Attribute.
func attributes(b []byte) ([]attribute, error) {
	var out []attribute
	for rest := b; len(rest) > 0; {
		var a attribute
		r, err := asn1.Unmarshal(rest, &a)
		if err != nil {
			return nil, pki.Malformed("an attribute of a SignerInfo does not parse")
		}
		out, rest = append(out, a), r
	}
	return out, nil
}

// sidReadable reports whether a SignerIdentifier is one of its two forms: issuerAndSerialNumber (a SEQUENCE) or subjectKeyIdentifier
// [0] IMPLICIT OCTET STRING.
func sidReadable(sid asn1.RawValue) bool {
	switch {
	case sid.Class == asn1.ClassUniversal && sid.Tag == asn1.TagSequence:
		var ias issuerAndSerial
		rest, err := asn1.Unmarshal(sid.FullBytes, &ias)
		return err == nil && len(rest) == 0 && ias.SerialNumber != nil
	case sid.Class == asn1.ClassContextSpecific && sid.Tag == 0 && !sid.IsCompound:
		return len(sid.Bytes) > 0
	}
	return false
}

// countersignatures reads the SignerInfos of the primary's countersignature attributes, in order.
func countersignatures(primary *signerInfo) ([]*signerInfo, error) {
	var out []*signerInfo
	for _, a := range primary.UnauthenticatedAttributes {
		if !a.Type.Equal(oidCountersignature()) {
			continue
		}
		got, err := signerInfos(a.Value.Bytes)
		if err != nil {
			return nil, err
		}
		if len(out)+len(got) > maxCountersignatures {
			return nil, pki.TooLarge("the signature has more than %d countersignatures", maxCountersignatures)
		}
		out = append(out, got...)
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

// certificateOf finds a signer's certificate among the certificates of the SignedData by the form its SignerInfo names it in: issuer and
// serial number, or subject key identifier (RFC 5652, section 5.3).
func certificateOf(si *signerInfo, certs []*x509.Certificate) *x509.Certificate {
	for _, c := range certs {
		if names(si.sid, c) {
			return c
		}
	}
	return nil
}

// names reports whether a SignerIdentifier names c: by its issuer and serial number, or by its subject key identifier, which is the
// certificate's extension or, for a certificate without one, the SHA-1 of its public key (RFC 5280, section 4.2.1.2, method 1).
func names(sid asn1.RawValue, c *x509.Certificate) bool {
	if sid.Class == asn1.ClassContextSpecific {
		ski := c.SubjectKeyId
		if len(ski) == 0 {
			ski = publicKeyID(c)
		}
		return len(sid.Bytes) > 0 && bytes.Equal(sid.Bytes, ski)
	}
	var ias issuerAndSerial
	if _, err := asn1.Unmarshal(sid.FullBytes, &ias); err != nil || ias.SerialNumber == nil {
		return false
	}
	return bytes.Equal(c.RawIssuer, ias.IssuerName.FullBytes) && c.SerialNumber.Cmp(ias.SerialNumber) == 0
}

// publicKeyID is the SHA-1 of a certificate's subjectPublicKey bits: the key identifier of a certificate without the extension.
func publicKeyID(c *x509.Certificate) []byte {
	var spki struct {
		Algorithm pkix.AlgorithmIdentifier
		PublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(c.RawSubjectPublicKeyInfo, &spki); err != nil {
		return nil
	}
	sum := sha1.Sum(spki.PublicKey.Bytes) //nolint:gosec // RFC 5280's key identifier, no signature
	return sum[:]
}

// signerName describes who a SignerInfo names, for a refusal's detail.
func signerName(sid asn1.RawValue) string {
	if sid.Class == asn1.ClassContextSpecific {
		return "the key " + hex.EncodeToString(sid.Bytes)
	}
	var ias issuerAndSerial
	if _, err := asn1.Unmarshal(sid.FullBytes, &ias); err != nil || ias.SerialNumber == nil {
		return "a signer"
	}
	var rdn pkix.RDNSequence
	if _, err := asn1.Unmarshal(ias.IssuerName.FullBytes, &rdn); err != nil {
		return "a signer"
	}
	var n pkix.Name
	n.FillFromRDNSequence(&rdn)
	return "the serial " + ias.SerialNumber.Text(16) + " of " + n.String()
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
	if err := cert.CheckSignature(alg, si.signedDER, si.EncryptedDigest); err != nil {
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
