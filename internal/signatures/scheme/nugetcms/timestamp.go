package nugetcms

import (
	"bytes"
	"crypto"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"math/big"
	"time"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
)

// maxTimestamps bounds the timestamp tokens of one signer (R-SIG-013): NuGet adds one.
const maxTimestamps = 4

func oidTSTInfo() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4}
}

// tstInfo is RFC 3161's TSTInfo as far as the check reads it: the message imprint and the time (encoding/asn1 leaves the optional
// members after genTime unread).
type tstInfo struct {
	Version        int
	Policy         asn1.ObjectIdentifier
	MessageImprint struct {
		HashAlgorithm pkix.AlgorithmIdentifier
		HashedMessage []byte
	}
	SerialNumber *big.Int
	GenTime      time.Time `asn1:"generalized"`
}

// timestamps verifies the RFC 3161 timestamp tokens of a signer's unsigned attributes, read by the CMS reader of this package
// (Q-SIG-216): each must stamp the signer's signature value and verify with the certificate of its authority it carries, named by issuer
// and serial number or by subject key identifier. Which authority is trusted is the trust step's (R-SIG-007, Q-SIG-41): the token, its
// time and its chain are the evidence. A token over a SHA-1 imprint, signed with SHA-1 or an algorithm Quader does not read, or without
// its authority's certificate proves nothing and is left out with a detail; one that does not match or verify is timestamp_invalid.
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

// imprintHash is the hash of a message imprint, ok false for SHA-1 and an algorithm NuGet does not use: such a token proves nothing.
func imprintHash(oid asn1.ObjectIdentifier) (crypto.Hash, bool) {
	h, err := digestHash(oid)
	return h, err == nil
}

// oneTimestamp verifies one token against the signature value it stamps.
func oneTimestamp(tok, signatureValue []byte, l scheme.Limits, rd *pki.Reading) (*scheme.Timestamp, error) {
	if err := pki.CheckBER(tok, l.ASN1Depth); err != nil {
		return nil, err
	}
	sd, err := parseSignedData(tok, l.ASN1Depth, l.MaxChain, l.MaxCertificateSize)
	var refusal *pki.Refusal
	switch {
	case errors.As(err, &refusal) && refusal.Reason == signatures.ReasonLimitsExceeded:
		return nil, err
	case err != nil:
		return nil, pki.Invalid(signatures.ReasonTimestampInvalid, "a timestamp token does not parse")
	case len(sd.signers) != 1:
		return nil, pki.Invalid(signatures.ReasonTimestampInvalid, "a timestamp has no single signer")
	case len(sd.certs) == 0:
		rd.Detail("a timestamp without its authority's certificate proves nothing")
		return nil, nil
	}
	var info tstInfo
	if rest, err := asn1.Unmarshal(sd.content, &info); err != nil || len(rest) > 0 || !sd.contentType.Equal(oidTSTInfo()) {
		return nil, pki.Invalid(signatures.ReasonTimestampInvalid, "a timestamp token holds no TSTInfo")
	}
	h, ok := imprintHash(info.MessageImprint.HashAlgorithm.Algorithm)
	if !ok {
		rd.Detail("a timestamp over a SHA-1 imprint proves nothing")
		return nil, nil
	}
	sum := h.New()
	sum.Write(signatureValue)
	if !bytes.Equal(sum.Sum(nil), info.MessageImprint.HashedMessage) {
		return nil, pki.Invalid(signatures.ReasonTimestampInvalid, "a timestamp stamps another signature value")
	}
	ts := sd.signers[0]
	leaf := certificateOf(ts, sd.certs)
	if leaf == nil {
		return nil, pki.Invalid(signatures.ReasonTimestampInvalid, "a timestamp does not carry the certificate of %s", signerName(ts.sid))
	}
	if err := verify(ts, leaf, sd.content); err != nil {
		if errors.As(err, &refusal) && refusal.State == signatures.StateUnsupported {
			rd.Detail("a timestamp signed with SHA-1 or an algorithm Quader does not read proves nothing")
			return nil, nil
		}
		return nil, pki.Invalid(signatures.ReasonTimestampInvalid, "a timestamp does not verify with its authority's certificate")
	}
	at := info.GenTime.UTC()
	chain := pki.Order(leaf, sd.certs, l.MaxChain)
	rd.Signer.Timestamps = append(rd.Signer.Timestamps, signatures.TimestampInfo{Time: at, Authority: leaf.Subject.String()})
	return &scheme.Timestamp{Token: tok, Time: at, Chain: pki.DERs(chain)}, nil
}
