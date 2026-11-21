// Package pki is what the verifiers of notation, nugetcms, openpgp and rawkey share (signatures.md R-SIG-005, R-SIG-006, R-SIG-013): the
// outcome of a check (a refusal with its state and closed reason, the result of a read, the transient error of the object store), the
// bounded reads of an object and of a blob, the bounds on ASN.1 and CBOR before any library parses them, the checks of a certificate
// chain and of a key, and the key IDs and signer fields a chain gives.
//
// It holds no state and reads nothing but what a check hands it.
package pki

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// maxSigner is the bound of signatures.signer (R-SIG-003): a signer whose JSON is longer loses its lists and says so in its cut.
const maxSigner = 64 << 10

// Refusal is a check that ended in a state other than valid or no_key: the state, its closed reason (R-SIG-006) and a sentence in plain
// words for signer.detail.
type Refusal struct {
	State  signatures.State
	Reason string
	Detail string
}

// Error renders the refusal as its state, reason and detail.
func (r *Refusal) Error() string { return fmt.Sprintf("%s (%s): %s", r.State, r.Reason, r.Detail) }

// Malformed is an object that does not parse (malformed, content_unreadable).
func Malformed(detail string, args ...any) error {
	return &Refusal{signatures.StateMalformed, signatures.ReasonContentUnreadable, fmt.Sprintf(detail, args...)}
}

// TooLarge is an object beyond a bound of R-SIG-013 (malformed, limits_exceeded).
func TooLarge(detail string, args ...any) error {
	return &Refusal{signatures.StateMalformed, signatures.ReasonLimitsExceeded, fmt.Sprintf(detail, args...)}
}

// Invalid is a signature that does not match its subject, with the reason of R-SIG-006.
func Invalid(reason, detail string, args ...any) error {
	return &Refusal{signatures.StateInvalid, reason, fmt.Sprintf(detail, args...)}
}

// Unsupported is a scheme, an algorithm or a version Quader does not read, or a weak algorithm.
func Unsupported(reason, detail string, args ...any) error {
	return &Refusal{signatures.StateUnsupported, reason, fmt.Sprintf(detail, args...)}
}

// Transient wraps an error of the object store: the job tries again (R-SIG-011).
func Transient(err error) error { return fmt.Errorf("%w: %w", scheme.ErrTransient, err) }

// Limits fills the zero fields of l with the defaults of R-SIG-013.
func Limits(l scheme.Limits) scheme.Limits {
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

// Reading is what a read found of one signature (R-SIG-005): what the Result carries besides its state.
type Reading struct {
	Role          string
	PredicateType string
	Signer        signatures.Signer
	KeyIDs        []string
	SignedDigest  []byte
	DigestAlg     string
	ClaimedAt     time.Time
	ProvenAt      time.Time
	TimeProof     string
	// Parts is the number of signatures the object holds, 0 when the read could not count them.
	Parts int
}

// NewReading is the reading of a signature before anything was read: the detector's role.
func NewReading(in scheme.Input) *Reading { return &Reading{Role: in.Role} }

// AddKeyID adds an identifier an anchor or a block can match, once.
func (rd *Reading) AddKeyID(id string) {
	if id == "" {
		return
	}
	for _, k := range rd.KeyIDs {
		if k == id {
			return
		}
	}
	rd.KeyIDs = append(rd.KeyIDs, id)
}

// Detail adds a sentence to signer.detail.
func (rd *Reading) Detail(s string) {
	if rd.Signer.Detail == "" {
		rd.Signer.Detail = s
		return
	}
	rd.Signer.Detail += "; " + s
}

// Result is the reading as a Result of state and reason.
func (rd *Reading) Result(state signatures.State, reason string) signatures.Result {
	res := signatures.Result{
		State: state, Reason: reason, Role: rd.Role, PredicateType: rd.PredicateType, KeyIDs: rd.KeyIDs,
		SignedDigest: rd.SignedDigest, ClaimedAt: rd.ClaimedAt, ProvenAt: rd.ProvenAt, TimeProof: rd.TimeProof,
		Signer: SignerJSON(rd.Signer),
	}
	if len(rd.SignedDigest) > 0 {
		res.DigestAlg = rd.DigestAlg
	}
	return res
}

// SignerJSON encodes a signer within the bound of signatures.signer: lists are dropped from one that is longer, and the cut says so.
func SignerJSON(s signatures.Signer) json.RawMessage {
	b, err := json.Marshal(s)
	if err == nil && len(b) <= maxSigner {
		return b
	}
	s.AlternativeNames, s.Chain, s.Owners, s.StatementSubjects, s.Log, s.Timestamps = nil, nil, nil, nil, nil, nil
	s.Cut = "the signer's lists are longer than signatures.signer holds"
	if len(s.Detail) > 1024 {
		s.Detail = s.Detail[:1024]
	}
	b, err = json.Marshal(s)
	if err != nil || len(b) > maxSigner {
		return json.RawMessage(`{"cut":"the signer is longer than signatures.signer holds"}`)
	}
	return b
}

// Refused turns an error of a check into its Output: a transient error is returned, a refusal is its state with what the read found, and
// any other error is a signature that could not be read. Key IDs and a signed digest are kept for an invalid signature only: a signature
// that could not be read or checked matches no anchor and no block by an identifier it may have misread.
func Refused(in scheme.Input, rd *Reading, err error) (scheme.Output, error) {
	if errors.Is(err, scheme.ErrTransient) {
		return scheme.Output{}, err
	}
	var r *Refusal
	if !errors.As(err, &r) {
		r = &Refusal{signatures.StateMalformed, signatures.ReasonContentUnreadable, "the signature could not be read"}
	}
	if rd == nil {
		rd = NewReading(in)
	}
	rd.Detail(r.Detail)
	res := rd.Result(r.State, r.Reason)
	if r.State != signatures.StateInvalid {
		res.KeyIDs, res.SignedDigest, res.DigestAlg = nil, nil, ""
	}
	return scheme.Output{Result: res, Parts: rd.Parts}, nil
}

// Answer is the Output of a signature that passed the integrity check (valid) or that no known key could check yet (no_key), with the
// evidence the trust step judges it by (Q-SIG-109), also as JSON in the Result.
func Answer(rd *Reading, state signatures.State, ev *scheme.Evidence) scheme.Output {
	res := rd.Result(state, "")
	if ev != nil {
		if b, err := json.Marshal(ev); err == nil {
			res.Evidence = b
		}
	}
	return scheme.Output{Result: res, Evidence: ev, Parts: rd.Parts}
}
