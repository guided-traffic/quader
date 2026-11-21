package sigstore

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"errors"
	"regexp"

	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// payloadInToto is the DSSE payload type of an in-toto statement.
const payloadInToto = "application/vnd.in-toto+json"

// bundleVersions are the bundle media types sigstore-go v1.4.0 reads: versions 0.1 to 0.3 (R-SIG-005).
var bundleVersions = regexp.MustCompile(`^application/vnd\.dev\.sigstore\.bundle(\+json;version=0\.[123]|\.v0\.[123](\.[0-9]+)?\+json)$`)

// object is one signature as a Sigstore bundle, parsed: what the read and the integrity check work on.
type object struct {
	// b is the bundle; json its JSON as the evidence keeps it (cosign's older layout converted, R-SIG-005).
	b    *bundle.Bundle
	json json.RawMessage
	// certs is the chain the signature carries, leaf first; hint the key hint of a bundle signed with a key.
	certs []*x509.Certificate
	hint  string
	// sig is the signature value; digest and digestAlg the message digest of a message signature (the protobuf's name, "SHA2_256").
	sig       []byte
	digest    []byte
	digestAlg protocommon.HashAlgorithm
	// envelope is a DSSE envelope's payload type and payload; stmt its in-toto statement.
	payloadType string
	payload     []byte
	stmt        *statement
	// legacy is the simple-signing payload of cosign's older layout.
	legacy *simpleSigning
	// artifactAlg and artifactDigest are what the bundle was checked against: "sha256" or "sha512" and the digest.
	artifactAlg    string
	artifactDigest []byte
	// parts is the number of signature layers of the manifest the signature came from, 0 for a bundle that is the object itself.
	parts int
	// notes are sentences about what the read left out (a log entry of a kind sigstore-go does not read).
	notes []string
}

// leaf is the signer's certificate, nil for a bundle signed with a key.
func (o *object) leaf() *x509.Certificate {
	if len(o.certs) == 0 {
		return nil
	}
	return o.certs[0]
}

// statement is what the check reads of an in-toto statement: its predicate type and its subjects, not the predicate.
type statement struct {
	Type          string             `json:"_type"`
	PredicateType string             `json:"predicateType"`
	Subject       []statementSubject `json:"subject"`
}

// statementSubject is one subject of a statement: a name and its digests by algorithm ("sha256", "sha512"), lower-case hex.
type statementSubject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

// fromBundle parses a Sigstore bundle's JSON within the bounds of R-SIG-013. A media type or version sigstore-go does not read is
// unsupported (version_unsupported); anything else that does not parse is malformed.
func fromBundle(data []byte, l scheme.Limits) (*object, error) {
	if err := checkJSON(data, l); err != nil {
		return nil, err
	}
	var p probe
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, malformed("the Sigstore bundle does not parse")
	}
	if !bundleVersions.MatchString(p.MediaType) {
		return nil, unsupported(signatures.ReasonVersionUnsupported, "the bundle's media type %q is not one Quader reads", p.MediaType)
	}
	pb := new(protobundle.Bundle)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(data, pb); err != nil {
		return nil, malformed("the Sigstore bundle does not parse")
	}
	return fromProto(pb, data, l)
}

// fromProto makes an object of a bundle: the chain bounded before it is parsed, the bundle validated by sigstore-go, the signature and the
// statement read.
func fromProto(pb *protobundle.Bundle, data []byte, l scheme.Limits) (*object, error) {
	ders := chainDER(pb.GetVerificationMaterial())
	if err := boundChain(len(ders), ders, l); err != nil {
		return nil, err
	}
	b, err := bundle.NewBundle(pb)
	if errors.Is(err, bundle.ErrUnsupportedMediaType) {
		return nil, unsupported(signatures.ReasonVersionUnsupported, "the bundle's media type %q is not one Quader reads", pb.GetMediaType())
	}
	if err != nil {
		return nil, malformed("the Sigstore bundle is not valid: %s", err.Error())
	}
	if data == nil {
		if data, err = protojson.Marshal(pb); err != nil {
			return nil, malformed("the converted bundle does not encode")
		}
	}
	o := &object{b: b, json: data, hint: pb.GetVerificationMaterial().GetPublicKey().GetHint()}
	for _, der := range ders {
		c, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, malformed("a certificate of the signature does not parse")
		}
		o.certs = append(o.certs, c)
	}
	if o.leaf() == nil && pb.GetVerificationMaterial().GetPublicKey() == nil {
		return nil, malformed("the bundle carries neither a certificate nor a key")
	}
	if err := o.content(pb, l); err != nil {
		return nil, err
	}
	return o, nil
}

// content reads the bundle's signature: a message signature's digest and value, or a DSSE envelope's one signature and its statement.
func (o *object) content(pb *protobundle.Bundle, l scheme.Limits) error {
	if ms := pb.GetMessageSignature(); ms != nil {
		o.sig, o.digest, o.digestAlg = ms.GetSignature(), ms.GetMessageDigest().GetDigest(), ms.GetMessageDigest().GetAlgorithm()
		if len(o.sig) == 0 || len(o.digest) == 0 {
			return malformed("the message signature lacks its signature or its digest")
		}
		return nil
	}
	env := pb.GetDsseEnvelope()
	if env == nil || len(env.GetSignatures()) != 1 {
		return malformed("the DSSE envelope does not hold exactly one signature")
	}
	o.sig, o.payloadType, o.payload = env.GetSignatures()[0].GetSig(), env.GetPayloadType(), env.GetPayload()
	if o.payloadType != payloadInToto {
		return unsupported(signatures.ReasonVersionUnsupported, "the DSSE payload type %q is not an in-toto statement", o.payloadType)
	}
	if err := checkJSON(o.payload, l); err != nil {
		return err
	}
	var st statement
	if err := json.Unmarshal(o.payload, &st); err != nil {
		return malformed("the in-toto statement does not parse")
	}
	o.stmt = &st
	return nil
}

// chainDER is the certificates a bundle's verification material carries, leaf first.
func chainDER(vm *protobundle.VerificationMaterial) [][]byte {
	if c := vm.GetCertificate(); c != nil {
		return [][]byte{c.GetRawBytes()}
	}
	var out [][]byte
	for _, c := range vm.GetX509CertificateChain().GetCertificates() {
		out = append(out, c.GetRawBytes())
	}
	return out
}

// boundChain refuses a chain longer than signatures.max_chain or a certificate larger than its size bound (R-SIG-013).
func boundChain(n int, ders [][]byte, l scheme.Limits) error {
	if n > l.MaxChain {
		return tooLarge("the signature carries %d certificates, more than signatures.max_chain (%d)", n, l.MaxChain)
	}
	for _, d := range ders {
		if len(d) > l.MaxCertificateSize {
			return tooLarge("a certificate of the signature is larger than %d bytes", l.MaxCertificateSize)
		}
	}
	return nil
}

// sha256Of is the SHA-256 of b as a slice.
func sha256Of(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}
