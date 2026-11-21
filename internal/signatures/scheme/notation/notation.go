// Package notation verifies notation-jws and notation-cose (signatures.md R-SIG-005, R-SIG-006, R-SIG-007, R-SIG-013, R-SIG-023,
// R-SIG-032): a Notation envelope with notation-core-go v1.3.0, the chain it carries, its payload's target descriptor and its RFC 3161
// timestamp countersignature (tspclient-go v1.0.0). No content is read: the descriptor names the subject by its digest and size.
//
// The object is the OCI manifest that carries the signature, the envelope its layer Part (Q-SIG-48), or, for a generic sibling file
// (<file>.jws.sig, <file>.cose.sig), the envelope itself. The check runs in this order, and the first step that fails decides: the
// envelope is found and bounded (malformed); its chain is read within signatures.max_chain and uses no weak algorithm (malformed,
// unsupported); every certificate of the chain is issued by the next and the chain is one Notation accepts for code signing
// (chain_broken); the library verifies the envelope with the leaf (signature_mismatch, attribute_missing); the payload's descriptor names
// the subject's digest and size (subject_mismatch); a timestamp countersignature stamps the signature value and verifies with its
// authority's certificate (timestamp_invalid). Whether the chain ends in a trusted certificate, and whether the timestamp's authority is
// trusted, is the trust step's (R-SIG-008, Q-SIG-41).
package notation

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/notaryproject/notation-core-go/signature"
	_ "github.com/notaryproject/notation-core-go/signature/cose" // registers the COSE envelope
	_ "github.com/notaryproject/notation-core-go/signature/jws"  // registers the JWS envelope
	nx509 "github.com/notaryproject/notation-core-go/x509"
	"github.com/notaryproject/tspclient-go"

	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
)

// payloadType is the content type of a Notation payload, a descriptor of the signed artifact.
const payloadType = "application/vnd.cncf.notary.payload.v1+json"

// Check reads and checks one signature of the schemes notation-jws and notation-cose. An error is a check that could not run (it wraps
// [scheme.ErrTransient]); every other outcome is the Output's Result.
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
	env, err := load(ctx, in)
	if err != nil {
		return scheme.Output{}, err
	}
	rd.Parts = env.parts
	chain, err := pki.ParseChain(env.chain, in.Limits)
	if err != nil {
		return scheme.Output{}, err
	}
	pki.ChainSigner(chain, &rd.Signer)
	rd.KeyIDs = pki.CertificateKeyIDs(chain)
	if err := pki.ChainSigns(chain); err != nil {
		return scheme.Output{}, err
	}
	if err := nx509.ValidateCodeSigningCertChain(chain, nil); err != nil {
		return scheme.Output{}, pki.Invalid(signatures.ReasonChainBroken, "the chain is not one Notation accepts for code signing: %v", err)
	}
	e, err := signature.ParseEnvelope(env.mediaType, env.raw)
	if err != nil {
		return scheme.Output{}, pki.Malformed("the envelope does not parse")
	}
	content, err := e.Verify()
	if err != nil {
		return scheme.Output{}, verifyError(err)
	}
	readSignerInfo(&content.SignerInfo, rd)
	if err := checkPayload(content.Payload, in); err != nil {
		return scheme.Output{}, err
	}
	stamps, err := checkTimestamp(ctx, &content.SignerInfo, in.Limits, rd)
	if err != nil {
		return scheme.Output{}, err
	}
	return pki.Answer(rd, signatures.StateValid, &scheme.Evidence{Chain: pki.DERs(chain), Timestamps: stamps}), nil
}

// verifyError is the refusal of an envelope the library does not verify.
func verifyError(err error) error {
	var integrity *signature.SignatureIntegrityError
	if errors.As(err, &integrity) {
		return pki.Invalid(signatures.ReasonSignatureMismatch, "the envelope's signature does not verify with its leaf certificate")
	}
	var algorithm *signature.UnsupportedSignatureAlgoError
	if errors.As(err, &algorithm) {
		return pki.Unsupported(signatures.ReasonVersionUnsupported, "the envelope's signature algorithm is not one Notation reads")
	}
	var inv *signature.InvalidSignatureError
	if errors.As(err, &inv) && strings.Contains(inv.Msg, "not present") {
		return pki.Invalid(signatures.ReasonAttributeMissing, "the envelope lacks a signed attribute Notation requires")
	}
	return pki.Malformed("the envelope is not one Notation reads")
}

// readSignerInfo fills what the signed and unsigned attributes say (R-SIG-005).
func readSignerInfo(si *signature.SignerInfo, rd *pki.Reading) {
	rd.Signer.SigningScheme = string(si.SignedAttributes.SigningScheme)
	rd.Signer.SigningAgent = si.UnsignedAttributes.SigningAgent
	if !si.SignedAttributes.Expiry.IsZero() {
		rd.Signer.Expiry = si.SignedAttributes.Expiry.UTC()
	}
	rd.ClaimedAt = si.SignedAttributes.SigningTime.UTC()
	rd.Signer.HashAlgorithm = pki.HashName(si.SignatureAlgorithm.Hash())
}

// notaryPayload is a Notation payload: the descriptor of the signed artifact.
type notaryPayload struct {
	TargetArtifact struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
		Size      int64  `json:"size"`
	} `json:"targetArtifact"`
}

// checkPayload checks that the payload's descriptor names the subject: its SHA-256 (or SHA-512) and its size.
func checkPayload(p signature.Payload, in scheme.Input) error {
	if p.ContentType != payloadType {
		return pki.Unsupported(signatures.ReasonVersionUnsupported, "the payload's content type %q is not Notation's", p.ContentType)
	}
	if err := untrusted.CheckJSON(p.Content, jsonLimits(in.Limits)); err != nil {
		return pki.TooLarge("the payload is out of the JSON bounds")
	}
	var np notaryPayload
	if err := json.Unmarshal(p.Content, &np); err != nil {
		return pki.Malformed("the payload does not parse")
	}
	d := np.TargetArtifact.Digest
	named := d == "sha256:"+hex.EncodeToString(in.Subject[:]) ||
		(len(in.SubjectSHA512) > 0 && d == "sha512:"+hex.EncodeToString(in.SubjectSHA512))
	if !named {
		return pki.Invalid(signatures.ReasonSubjectMismatch, "the payload names %s, not the subject", d)
	}
	if in.SubjectSize > 0 && np.TargetArtifact.Size != in.SubjectSize {
		return pki.Invalid(signatures.ReasonSubjectMismatch, "the payload names a size of %d, the subject has %d", np.TargetArtifact.Size, in.SubjectSize)
	}
	return nil
}

// checkTimestamp verifies the RFC 3161 timestamp countersignature of the envelope, when it carries one: the token's message imprint is
// the hash of the signature value, and its signer's certificate (one with the use timeStamping only, as RFC 3161 asks) verifies it at the
// time it states. Which authority is trusted is the trust step's (Q-SIG-41); the token and its chain are the evidence it judges.
func checkTimestamp(ctx context.Context, si *signature.SignerInfo, l scheme.Limits, rd *pki.Reading) ([]scheme.Timestamp, error) {
	tok := si.UnsignedAttributes.TimestampSignature
	if len(tok) == 0 {
		return nil, nil
	}
	if err := pki.CheckBER(tok, l.ASN1Depth); err != nil {
		return nil, err
	}
	st, err := tspclient.ParseSignedToken(tok)
	if err != nil {
		return nil, pki.Invalid(signatures.ReasonTimestampInvalid, "the timestamp does not parse")
	}
	if len(st.Certificates) > l.MaxChain {
		return nil, pki.TooLarge("the timestamp carries %d certificates, more than signatures.max_chain", len(st.Certificates))
	}
	info, err := st.Info()
	if err != nil {
		return nil, pki.Invalid(signatures.ReasonTimestampInvalid, "the timestamp's information does not parse")
	}
	if _, err := info.Validate(si.Signature); err != nil {
		return nil, pki.Invalid(signatures.ReasonTimestampInvalid, "the timestamp stamps another signature value")
	}
	own := x509.NewCertPool()
	for _, c := range st.Certificates {
		own.AddCert(c)
	}
	chain, err := st.Verify(ctx, x509.VerifyOptions{Roots: own, CurrentTime: info.GenTime})
	if err != nil || len(chain) == 0 {
		return nil, pki.Invalid(signatures.ReasonTimestampInvalid, "the timestamp does not verify with its authority's certificate")
	}
	ordered := pki.Order(chain[0], st.Certificates, l.MaxChain)
	rd.Signer.Timestamps = append(rd.Signer.Timestamps, signatures.TimestampInfo{Time: info.GenTime.UTC(), Authority: chain[0].Subject.String()})
	return []scheme.Timestamp{{Token: tok, Time: info.GenTime.UTC(), Chain: pki.DERs(ordered)}}, nil
}
