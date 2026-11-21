// Package rawkey verifies npm-registry and public-key (signatures.md R-SIG-005, R-SIG-006, R-SIG-007): an ECDSA, RSA or Ed25519 signature
// over a digest, with the standard library. Neither scheme carries a key: the check verifies with the known keys it is handed (the keys of
// the anchors and the keys an upstream announces), and a signature no known key verifies is no_key with the digest it commits to kept
// (Q-SIG-23), so that the trust step verifies it with a key added later by [Verify] without reading anything again.
//
//	npm-registry  the object is npm's {"keyid", "sig"}; the digest is the SHA-256 of "<name>@<version>:<integrity>" the row keeps; the
//	              key is the one whose keyid the signature names, and a key whose expires lies before the signature's receipt does not
//	              count (pacote compares it with the publish time; Quader with the time of R-SIG-007)
//	public-key    the object is a file of cosign sign-blob, a base64 raw signature; the digest is the SHA-256 of the signed file, the
//	              subject; the signature names no key, so every known key is tried
package rawkey

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"time"

	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
)

// The kinds of a key signature as the evidence names them (scheme.KeyEvidence.Kind).
const (
	KindECDSA      = "ecdsa"
	KindRSAPKCS1v5 = "rsa-pkcs1v15"
	KindRSAPSS     = "rsa-pss"
	KindEd25519    = "ed25519"
)

// The bounds of a raw signature of cosign sign-blob: an Ed25519 or ECDSA signature is 64 bytes or more, an RSA one of 8192 bits 1024.
const (
	minRawSignature = 64
	maxRawSignature = 1024
)

// Check reads and checks one signature of the schemes npm-registry and public-key. An error is a check that could not run (it wraps
// [scheme.ErrTransient]); every other outcome is the Output's Result.
func Check(ctx context.Context, in scheme.Input) (scheme.Output, error) {
	in.Limits = pki.Limits(in.Limits)
	rd := pki.NewReading(in)
	var (
		out scheme.Output
		err error
	)
	switch in.Scheme {
	case signatures.SchemeNPMRegistry:
		out, err = checkNPM(ctx, in, rd)
	case signatures.SchemePublicKey:
		out, err = checkPublicKey(ctx, in, rd)
	default:
		err = pki.Unsupported(signatures.ReasonSchemeUnknown, "the scheme %q is not one of this verifier", in.Scheme)
	}
	if err != nil {
		return pki.Refused(in, rd, err)
	}
	return out, nil
}

// npmSignature is one entry of dist.signatures of an npm version.
type npmSignature struct {
	KeyID string `json:"keyid"`
	Sig   string `json:"sig"`
}

// checkNPM checks an npm registry signature with the key its keyid names (R-SIG-028).
func checkNPM(ctx context.Context, in scheme.Input, rd *pki.Reading) (scheme.Output, error) {
	obj, err := pki.Object(ctx, in)
	if err != nil {
		return scheme.Output{}, err
	}
	if err := untrusted.CheckJSON(obj, untrusted.Limits{JSONDepth: in.Limits.JSONDepth, JSONMembers: in.Limits.JSONValues}); err != nil {
		return scheme.Output{}, pki.TooLarge("the registry signature is out of the JSON bounds")
	}
	var e npmSignature
	if err := json.Unmarshal(obj, &e); err != nil || e.KeyID == "" || e.Sig == "" {
		return scheme.Output{}, pki.Malformed("the registry signature is not npm's {keyid, sig}")
	}
	sig, err := base64.StdEncoding.DecodeString(e.Sig)
	if err != nil || len(sig) == 0 || len(sig) > maxRawSignature {
		return scheme.Output{}, pki.Malformed("the registry signature's sig is not a base64 signature")
	}
	if len(in.SignedDigest) != sha256.Size {
		return scheme.Output{}, pki.Malformed("the digest of the registry signature's message is not kept")
	}
	rd.SignedDigest, rd.DigestAlg = append([]byte(nil), in.SignedDigest...), "sha256"
	rd.Signer.KeyID, rd.Signer.HashAlgorithm = e.KeyID, pki.HashName(crypto.SHA256)
	rd.AddKeyID(e.KeyID)
	ev := &scheme.Evidence{Key: &scheme.KeyEvidence{Kind: KindECDSA, Signature: sig, KeyID: e.KeyID, Hash: "sha256"}}
	var named, expired []anchors.ParsedKey
	for _, k := range in.Keys.Public {
		switch {
		case k.KeyID != e.KeyID:
		case Expired(k, in.ReceivedAt):
			expired = append(expired, k)
		default:
			named = append(named, k)
		}
	}
	for _, k := range named {
		if err := pki.CheckKey(k.Key, in.Limits); err != nil {
			return scheme.Output{}, err
		}
		if _, ok := Verify(KindECDSA, k.Key, "sha256", rd.SignedDigest, sig); ok {
			keyFound(rd, k)
			return pki.Answer(rd, signatures.StateValid, ev), nil
		}
	}
	if len(named) > 0 {
		return scheme.Output{}, pki.Invalid(signatures.ReasonSignatureMismatch, "the signature does not verify with the key %s", e.KeyID)
	}
	if len(expired) > 0 {
		rd.Detail("the key " + e.KeyID + " expired before the signature was received")
	}
	rd.Detail("no known key verifies this signature yet")
	return pki.Answer(rd, signatures.StateNoKey, ev), nil
}

// checkPublicKey checks a raw signature of cosign sign-blob over the SHA-256 of the file beside it with every known key (R-SIG-032).
func checkPublicKey(ctx context.Context, in scheme.Input, rd *pki.Reading) (scheme.Output, error) {
	obj, err := pki.Object(ctx, in)
	if err != nil {
		return scheme.Output{}, err
	}
	sig, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(obj)))
	if err != nil || len(sig) < minRawSignature || len(sig) > maxRawSignature {
		return scheme.Output{}, pki.Malformed("the file is not the base64 raw signature of cosign sign-blob")
	}
	rd.SignedDigest, rd.DigestAlg = append([]byte(nil), in.Subject[:]...), "sha256"
	rd.Signer.HashAlgorithm = pki.HashName(crypto.SHA256)
	ev := &scheme.Evidence{Key: &scheme.KeyEvidence{Kind: KindOfSignature(sig), Signature: sig, Hash: "sha256"}}
	for _, k := range in.Keys.Public {
		if Expired(k, in.ReceivedAt) || pki.CheckKey(k.Key, in.Limits) != nil {
			continue
		}
		if kind, ok := Verify("", k.Key, "sha256", rd.SignedDigest, sig); ok {
			keyFound(rd, k)
			ev.Key.Kind, ev.Key.KeyID = kind, rd.Signer.KeyID
			return pki.Answer(rd, signatures.StateValid, ev), nil
		}
	}
	rd.Detail("no known key verifies this signature yet")
	return pki.Answer(rd, signatures.StateNoKey, ev), nil
}

// keyFound records the key that verified a signature: its SHA-256, npm's keyid where it has one, its algorithm.
func keyFound(rd *pki.Reading, k anchors.ParsedKey) {
	id := k.SHA256
	if id == "" {
		id = pki.KeyID(k.Key)
	}
	if rd.Signer.KeyID == "" {
		rd.Signer.KeyID = id
	}
	rd.Signer.Algorithm = pki.AlgorithmName(k.Key)
	rd.AddKeyID(id)
	rd.AddKeyID(k.KeyID)
}

// Expired reports whether a key's expiry lies before at, the time of a signature (R-SIG-007): such a key does not count for it.
func Expired(k anchors.ParsedKey, at time.Time) bool {
	return !k.Expires.IsZero() && !at.IsZero() && k.Expires.Before(at)
}

// Verify reports whether pub verifies sig over digest, a digest of the hash named ("sha256", "sha384", "sha512"), as a signature of kind
// ("" tries every kind the key allows), and answers the kind that verified. It reads no content: the trust step verifies a stored digest
// with it (Q-SIG-23). An Ed25519 signature is verified over the digest as its message, and as Ed25519ph over a SHA-512 digest.
func Verify(kind string, pub crypto.PublicKey, hashName string, digest, sig []byte) (string, bool) {
	h := pki.HashOf(hashName)
	if h == 0 || len(digest) != h.Size() {
		return "", false
	}
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		if (kind == "" || kind == KindECDSA) && ecdsa.VerifyASN1(k, digest, sig) {
			return KindECDSA, true
		}
		return "", false
	case *rsa.PublicKey:
		if (kind == "" || kind == KindRSAPKCS1v5) && rsa.VerifyPKCS1v15(k, h, digest, sig) == nil {
			return KindRSAPKCS1v5, true
		}
		pss := &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthAuto}
		if (kind == "" || kind == KindRSAPSS) && rsa.VerifyPSS(k, h, digest, sig, pss) == nil {
			return KindRSAPSS, true
		}
		return "", false
	case ed25519.PublicKey:
		if kind != "" && kind != KindEd25519 {
			return "", false
		}
		if ed25519.Verify(k, digest, sig) {
			return KindEd25519, true
		}
		if h == crypto.SHA512 && ed25519.VerifyWithOptions(k, digest, sig, &ed25519.Options{Hash: crypto.SHA512}) == nil {
			return KindEd25519, true
		}
	}
	return "", false
}

// KindOfSignature guesses a signature's kind from its encoding when no key verified it: an ASN.1 pair of integers is ECDSA, 64 bytes
// Ed25519, anything else RSA PKCS #1 v1.5 (cosign's default).
func KindOfSignature(sig []byte) string {
	var rs struct{ R, S *big.Int }
	if rest, err := asn1.Unmarshal(sig, &rs); err == nil && len(rest) == 0 {
		return KindECDSA
	}
	if len(sig) == ed25519.SignatureSize {
		return KindEd25519
	}
	return KindRSAPKCS1v5
}
