// Package sigstore verifies the Sigstore family of signatures (signatures.md R-SIG-005 to R-SIG-007, R-SIG-013, R-SIG-023, R-SIG-028,
// R-SIG-032): a Sigstore bundle
// (sigstore-bundle: a message signature or a DSSE envelope, keyless, with a key, or with the company's own PKI) and cosign's older layout
// (cosign-legacy: the simple-signing layers of a sha256-<hex>.sig manifest and the DSSE layers of an .att manifest, converted to a bundle),
// with sigstore-go v1.4.0. It also judges a bundle against a Sigstore identity anchor for the trust step (R-SIG-008): [LoadRoot] and
// [Judge].
//
// The check runs in this order, and the first step that fails decides the state: the object is found and parsed within the bounds of
// R-SIG-013 (malformed, or unsupported for a bundle version Quader does not read); the read fills the signer (R-SIG-005); a weak algorithm
// is unsupported; the bundle's digest or the statement's subject must name the subject (digest_mismatch, subject_mismatch); the chain the
// signature carries must sign its leaf (chain_broken); the signature must verify with the leaf's key or a known key (signature_mismatch,
// or no_key when no key is known, Q-SIG-23); every log entry and timestamp is verified against every trust root that knows its log or
// authority (log_entry_invalid, timestamp_invalid; one no root knows leaves time_proof unset, R-SIG-006, R-SIG-007, Q-SIG-41).
package sigstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// Check reads and checks one signature of the schemes sigstore-bundle and cosign-legacy. An error is a check that could not run (it wraps
// [scheme.ErrTransient]); every other outcome is the Output's Result.
func Check(ctx context.Context, in scheme.Input) (scheme.Output, error) {
	in.Limits = limitsOf(in.Limits)
	o, err := load(ctx, in)
	if err != nil {
		return refused(in, nil, err)
	}
	rd := read(o, in)
	if err := checkIntegrity(in, o, rd); err != nil {
		return refused(in, rd, err)
	}
	return answer(o, rd), nil
}

// limitsOf fills the zero fields of l with the defaults of R-SIG-013.
func limitsOf(l scheme.Limits) scheme.Limits {
	d := scheme.DefaultLimits()
	pick64 := func(v, def int64) int64 {
		if v > 0 {
			return v
		}
		return def
	}
	pick := func(v, def int) int {
		if v > 0 {
			return v
		}
		return def
	}
	return scheme.Limits{
		MaxObjectSize: pick64(l.MaxObjectSize, d.MaxObjectSize), MaxChain: pick(l.MaxChain, d.MaxChain),
		MaxCertificateSize: pick(l.MaxCertificateSize, d.MaxCertificateSize), MaxContentSize: pick64(l.MaxContentSize, d.MaxContentSize),
		MinRSABits: pick(l.MinRSABits, d.MinRSABits), MaxRSABits: pick(l.MaxRSABits, d.MaxRSABits),
		JSONDepth: pick(l.JSONDepth, d.JSONDepth), JSONValues: pick(l.JSONValues, d.JSONValues), ASN1Depth: pick(l.ASN1Depth, d.ASN1Depth),
	}
}

// refusal is a check that ended in a state other than valid or no_key: the state, its closed reason (R-SIG-006) and a sentence in plain
// words for signer.detail.
type refusal struct {
	state  signatures.State
	reason string
	detail string
}

func (r *refusal) Error() string { return fmt.Sprintf("%s (%s): %s", r.state, r.reason, r.detail) }

func malformed(detail string, args ...any) error {
	return &refusal{signatures.StateMalformed, signatures.ReasonContentUnreadable, fmt.Sprintf(detail, args...)}
}

func tooLarge(detail string, args ...any) error {
	return &refusal{signatures.StateMalformed, signatures.ReasonLimitsExceeded, fmt.Sprintf(detail, args...)}
}

func invalid(reason, detail string, args ...any) error {
	return &refusal{signatures.StateInvalid, reason, fmt.Sprintf(detail, args...)}
}

func unsupported(reason, detail string, args ...any) error {
	return &refusal{signatures.StateUnsupported, reason, fmt.Sprintf(detail, args...)}
}

// transient wraps an error of the object store: the job tries again (R-SIG-011).
func transient(err error) error { return fmt.Errorf("%w: %w", scheme.ErrTransient, err) }

// refused turns an error of the check into its Output: a transient error is returned, a refusal is its state with what the read found,
// and any other error is a signature that could not be read.
func refused(in scheme.Input, rd *reading, err error) (scheme.Output, error) {
	if errors.Is(err, scheme.ErrTransient) {
		return scheme.Output{}, err
	}
	var r *refusal
	if !errors.As(err, &r) {
		r = &refusal{signatures.StateMalformed, signatures.ReasonContentUnreadable, "the signature could not be read"}
	}
	if rd == nil {
		rd = &reading{role: in.Role}
	}
	rd.signer.Detail = r.detail
	res := rd.result()
	res.State, res.Reason = r.state, r.reason
	if r.state != signatures.StateInvalid {
		// Key IDs and a signed digest are kept for an invalid signature only: a signature that could not be read or checked matches no
		// anchor and no block by an identifier it may have misread. The signer stays: it says what the read found.
		res.KeyIDs, res.SignedDigest, res.DigestAlg = nil, nil, ""
	}
	return scheme.Output{Result: res, Parts: rd.parts}, nil
}

// answer is the Output of a signature that passed the integrity check (valid) or that no known key could check yet (no_key), with the
// evidence the trust step judges it by (Q-SIG-109).
func answer(o *object, rd *reading) scheme.Output {
	res := rd.result()
	res.State = signatures.StateValid
	if rd.noKey {
		res.State = signatures.StateNoKey
	}
	ev := evidenceOf(o, rd)
	if b, err := json.Marshal(ev); err == nil {
		res.Evidence = b
	}
	return scheme.Output{Result: res, Evidence: ev, Parts: rd.parts}
}

// evidenceOf is what the trust step needs: the bundle with its artifact digest, the chain as the signature carries it, the timestamps
// that matched the signature, and the key signature of a bundle signed with a key.
func evidenceOf(o *object, rd *reading) *scheme.Evidence {
	ev := &scheme.Evidence{
		Sigstore:   &scheme.SigstoreEvidence{Bundle: o.json, DigestAlg: o.artifactAlg, Digest: o.artifactDigest},
		Timestamps: rd.stamps,
	}
	for _, c := range o.certs {
		ev.Chain = append(ev.Chain, c.Raw)
	}
	if o.leaf() == nil {
		ev.Key = &scheme.KeyEvidence{Kind: keyKind(o.sig, rd.key), Signature: o.sig, KeyID: o.hint, Hash: rd.digestAlg}
	}
	return ev
}
