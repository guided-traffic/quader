package nugetcms

import (
	"bytes"
	"crypto"
	"encoding/asn1"

	"github.com/digitorus/pkcs7"
	"github.com/digitorus/timestamp"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
)

// maxTimestamps bounds the timestamp tokens of one signer (R-SIG-013): NuGet adds one.
const maxTimestamps = 4

// timestamps verifies the RFC 3161 timestamp tokens of a signer's unsigned attributes (digitorus/timestamp): each must stamp the signer's
// signature value and verify with the certificate of its authority it carries. Which authority is trusted is the trust step's
// (R-SIG-007, Q-SIG-41): the token, its time and its chain are the evidence. A token over a SHA-1 imprint or without its authority's
// certificate proves nothing and is left out with a detail; one that does not match or verify is timestamp_invalid.
func timestamps(si *signerInfo, l scheme.Limits, rd *pki.Reading) ([]scheme.Timestamp, error) {
	var out []scheme.Timestamp
	for _, a := range si.UnauthenticatedAttributes {
		if !a.Type.Equal(oidTimestampToken()) {
			continue
		}
		for rest := a.Value.Bytes; len(rest) > 0; {
			if len(out) == maxTimestamps {
				return nil, pki.TooLarge("the signer carries more than %d timestamps", maxTimestamps)
			}
			var tok asn1.RawValue
			r, err := asn1.Unmarshal(rest, &tok)
			if err != nil {
				return nil, pki.Malformed("a timestamp token does not parse")
			}
			rest = r
			ts, err := oneTimestamp(tok.FullBytes, si.EncryptedDigest, l, rd)
			if err != nil {
				return nil, err
			}
			if ts != nil {
				out = append(out, *ts)
			}
		}
	}
	return out, nil
}

// oneTimestamp verifies one token against the signature value it stamps.
func oneTimestamp(tok, signatureValue []byte, l scheme.Limits, rd *pki.Reading) (*scheme.Timestamp, error) {
	if err := pki.CheckBER(tok, l.ASN1Depth); err != nil {
		return nil, err
	}
	p7, err := pkcs7.Parse(tok)
	if err != nil {
		return nil, pki.Invalid(signatures.ReasonTimestampInvalid, "a timestamp token does not parse")
	}
	if len(p7.Certificates) > l.MaxChain {
		return nil, pki.TooLarge("a timestamp carries more certificates than signatures.max_chain")
	}
	if len(p7.Certificates) == 0 {
		rd.Detail("a timestamp without its authority's certificate proves nothing")
		return nil, nil
	}
	ts, err := timestamp.Parse(tok)
	if err != nil {
		return nil, pki.Invalid(signatures.ReasonTimestampInvalid, "a timestamp does not verify with its authority's certificate")
	}
	if ts.HashAlgorithm == crypto.SHA1 || !ts.HashAlgorithm.Available() {
		rd.Detail("a timestamp over a SHA-1 imprint proves nothing")
		return nil, nil
	}
	h := ts.HashAlgorithm.New()
	h.Write(signatureValue)
	if !bytes.Equal(h.Sum(nil), ts.HashedMessage) {
		return nil, pki.Invalid(signatures.ReasonTimestampInvalid, "a timestamp stamps another signature value")
	}
	leaf := p7.GetOnlySigner()
	if leaf == nil {
		return nil, pki.Invalid(signatures.ReasonTimestampInvalid, "a timestamp has no single signer")
	}
	chain := pki.Order(leaf, p7.Certificates, l.MaxChain)
	rd.Signer.Timestamps = append(rd.Signer.Timestamps, signatures.TimestampInfo{Time: ts.Time.UTC(), Authority: leaf.Subject.String()})
	return &scheme.Timestamp{Token: tok, Time: ts.Time.UTC(), Chain: pki.DERs(chain)}, nil
}
