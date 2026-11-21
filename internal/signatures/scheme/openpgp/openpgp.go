// Package openpgp verifies openpgp (signatures.md R-SIG-005, R-SIG-006, R-SIG-030 to R-SIG-032): a signature packet over a content and
// the packet's trailer, with go-crypto. The scheme carries no key: the check computes the digest the packet commits to once (the hash of
// the content followed by the packet's trailer, with a version 6 packet's salt first) and keeps it as signatures.signed_digest
// (Q-SIG-23), then verifies it with every known key the packet's issuer names. A key added later verifies the stored digest through
// [VerifyDigest] without reading the content again.
//
// The object is an armored or binary signature (a Release.gpg, a generic .asc or .sig, an Ansible signature handed as bytes) or a
// cleartext message (an InRelease); Part is the index of the signature packet. The content is the subject's bytes, the cleartext of an
// inline signature (content locator "cleartext"), or the MANIFEST.json of an Ansible collection (content locator "entry:MANIFEST.json"),
// whose FILES.json and every file are checked against the archive as ansible-galaxy collection verify does.
//
// The check runs in this order, and the first step that fails decides: the object is split into its signature packets within the bounds
// (malformed); the packet parses and uses no weak algorithm (unsupported); the content is read and, for an Ansible collection, every file
// matches (digest_mismatch); the digest's first two octets equal the packet's hash tag (digest_mismatch, decided without any key); a known
// key the issuer names verifies it (valid), fails (signature_mismatch), or none is known (no_key).
package openpgp

import (
	"context"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/packet"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
)

// The content locators an OpenPGP signature names (Q-SIG-48).
const (
	locatorCleartext = "cleartext"
	locatorEntry     = "entry:"
	manifestEntry    = "MANIFEST.json"
)

// KindOpenPGP is the kind of the evidence's key signature of this scheme: the signature packet itself.
const KindOpenPGP = "openpgp"

// Check reads and checks one signature of the scheme openpgp. An error is a check that could not run (it wraps [scheme.ErrTransient]);
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
	data, err := pki.Object(ctx, in)
	if err != nil {
		return scheme.Output{}, err
	}
	obj, err := split(data, in.Limits.MaxObjectSize)
	if err != nil {
		return scheme.Output{}, err
	}
	rd.Parts = len(obj.packets)
	if in.Part < 0 || in.Part >= len(obj.packets) {
		return scheme.Output{}, pki.Malformed("the object holds %d signatures, no part %d", len(obj.packets), in.Part)
	}
	raw := obj.packets[in.Part]
	sig, err := parseSignature(raw)
	if err != nil {
		return scheme.Output{}, err
	}
	read(sig, rd)
	if err := weak(sig); err != nil {
		return scheme.Output{}, err
	}
	digest, err := digestOf(ctx, in, obj, sig)
	if err != nil {
		return scheme.Output{}, err
	}
	rd.SignedDigest, rd.DigestAlg = digest, hashKey(sig)
	if digest[0] != sig.HashTag[0] || digest[1] != sig.HashTag[1] {
		return scheme.Output{}, pki.Invalid(signatures.ReasonDigestMismatch, "the content is not what the signature was made over")
	}
	ev := &scheme.Evidence{Key: &scheme.KeyEvidence{Kind: KindOpenPGP, Signature: raw, KeyID: rd.Signer.KeyID, Hash: hashKey(sig)}}
	return verifyKnown(in, rd, sig, raw, digest, ev)
}

// read fills what a packet says of its signer (R-SIG-005): the issuer's fingerprint and key ID, the creation time, the algorithms.
func read(sig *packet.Signature, rd *pki.Reading) {
	fp, keyID := issuer(sig)
	rd.Signer.KeyID = fp
	if fp == "" {
		rd.Signer.KeyID = keyID
	}
	rd.AddKeyID(fp)
	rd.AddKeyID(keyID)
	rd.Signer.Algorithm = algorithmName(sig.PubKeyAlgo, 0)
	rd.Signer.HashAlgorithm = pki.HashName(sig.Hash)
	rd.ClaimedAt = sig.CreationTime.UTC()
}

// verifyKnown verifies the digest with every known key the issuer names: one that verifies makes the signature valid, one that fails
// without any that verifies makes it invalid, none makes it no_key.
func verifyKnown(in scheme.Input, rd *pki.Reading, sig *packet.Signature, raw, digest []byte, ev *scheme.Evidence) (scheme.Output, error) {
	mismatch := false
	for _, cert := range in.Keys.OpenPGP {
		m, err := VerifyDigest(raw, digest, cert, time.Time{})
		if err != nil || !m.Named {
			continue
		}
		if !m.Verified {
			mismatch = true
			continue
		}
		if m.Bits > 0 && m.Bits < in.Limits.MinRSABits {
			return scheme.Output{}, pki.Unsupported(signatures.ReasonWeakAlgorithm, "an RSA key of %d bits is below %d", m.Bits, in.Limits.MinRSABits)
		}
		rd.Signer.Fingerprint, rd.Signer.Subject = m.Fingerprint, m.UserID
		rd.Signer.Algorithm = algorithmName(sig.PubKeyAlgo, m.Bits)
		rd.AddKeyID(m.Fingerprint)
		rd.AddKeyID(m.KeyFingerprint)
		return pki.Answer(rd, signatures.StateValid, ev), nil
	}
	if mismatch {
		return scheme.Output{}, pki.Invalid(signatures.ReasonSignatureMismatch, "the signature does not verify with the key %s", rd.Signer.KeyID)
	}
	rd.Detail("no known key of " + rd.Signer.KeyID + " verifies this signature yet")
	return pki.Answer(rd, signatures.StateNoKey, ev), nil
}

// hashKey names the packet's hash as signatures.digest_alg and the evidence spell it.
func hashKey(sig *packet.Signature) string {
	if k := pki.HashKey(sig.Hash); k != "" {
		return k
	}
	return strings.ToLower(strings.ReplaceAll(sig.Hash.String(), "-", ""))
}
