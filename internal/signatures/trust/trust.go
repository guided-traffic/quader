// Package trust is the trust step of signatures on artifacts (signatures.md R-SIG-007, R-SIG-008, R-SIG-012, R-SIG-016, R-SIG-020): it
// judges a signature whose integrity check found it valid (or no_key) against the anchors of one scope and the blocks of the installation,
// from the stored fields alone (the evidence, the key IDs, the committed digest, the signer, the times), and reads no content.
//
// The judgement runs in this order:
//
//  1. The time of the signature in the scope (R-SIG-007, Q-SIG-41): a timestamp of the evidence whose authority's chain builds to a CA
//     anchor of the use timestamping proves its time; else the signature's receipt counts. A Sigstore anchor judges a bundle at the time a
//     log entry or a timestamp of its own trust root proves (sigstore.Judge). A proof no anchor of the scope knows is no proof.
//  2. Every anchor of the scope, in ID order, says whether it knows the signer (R-SIG-008): a CA anchor of the use codeSigning when the
//     evidence's chain builds to one of its certificates at that time with the leaf's extended key use codeSigning (or the leaf is one of
//     its pinned certificates and valid then), and the subject restriction matches; an OpenPGP or public-key anchor when one of its keys
//     verifies the evidence's key signature against the stored digest and was valid then; a Sigstore anchor when the bundle verifies
//     against its trust root and the leaf's issuer and identity are its own.
//  3. A no_key signature is decided by the anchors' keys (R-SIG-012): one the issuer names that verifies makes it valid, one that fails
//     makes it invalid, none leaves it untrusted.
//  4. A block that matches one of the key IDs (or the key of an anchor that verified it) or the Sigstore identity makes it blocked whatever
//     anchors know it (R-SIG-020); else internal when an anchor of the Helm values, the administrator or the project knows it (internal
//     wins, Q-SIG-25), public when only a shipped one does, untrusted otherwise.
package trust

import (
	"bytes"
	"crypto/x509"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/openpgp"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey"
	"github.com/guided-traffic/quader/internal/signatures/scheme/sigstore"
)

// The states of a trust row (R-SIG-008).
const (
	StatePublic    = "public"
	StateInternal  = "internal"
	StateUntrusted = "untrusted"
	StateBlocked   = "blocked"
)

// The kinds of a block (R-SIG-020).
const (
	BlockFingerprint = "fingerprint"
	BlockSerial      = "serial"
	BlockKeyID       = "keyid"
	BlockIdentity    = "identity"
)

// The time proofs a verdict names (signatures.time_proof).
const (
	ProofTimestamp = "timestamp"
	ProofLog       = "log"
)

// maxChain bounds a stored chain the judgement parses: the evidence was bounded by signatures.max_chain when it was stored, and a bound
// lowered since does not make a stored chain unreadable.
const maxChain = 32

// Block is a blocked signer: a certificate's or an OpenPGP key's fingerprint, an issuer and a serial ("<issuer DN>|<serial>"), npm's or a
// raw key's keyid (or the SHA-256 of its PKIX DER), or a Sigstore identity (Issuer and Value, an anchored RE2 pattern; an empty Issuer
// matches any issuer).
type Block struct {
	ID     int64
	Kind   string
	Value  string
	Issuer string
}

// Scope is the anchors that judge a signature (R-SIG-008, R-SIG-016): the installation's (the shipped anchors that are switched on, the
// anchors of the Helm values and the administrator's), or a project's (those and the project's own, while the switch allows them).
type Scope struct {
	// ProjectID is the project's uuid, "" for the installation's scope.
	ProjectID string
	// Anchors are the enabled anchors of the scope, parsed (Anchor.Parsed nil: skipped).
	Anchors []anchors.Anchor
	// PublicRoot is the public Sigstore root in use (the release's snapshot or the refreshed one, R-SIG-017); Roots the own roots of the
	// scope's Sigstore identity anchors by anchor ID. Both parsed once per run by the caller.
	PublicRoot *sigstore.Root
	Roots      map[int64]*sigstore.Root
}

// Signature is what the trust step judges: the stored fields of a signature row whose state is valid or no_key.
type Signature struct {
	ID           int64
	Scheme       string
	State        signatures.State
	KeyIDs       []string
	SignedDigest []byte
	DigestAlg    string
	Evidence     *scheme.Evidence
	Signer       *signatures.Signer
	ReceivedAt   time.Time
	ProvenAt     time.Time
	TimeProof    string
}

// Verdict is the trust row of one signature in one scope.
type Verdict struct {
	// State is public, internal, untrusted or blocked.
	State string
	// AnchorIDs are every anchor of the scope that knows the signer, in ID order (a gate of stage 3 asks for a set, R-SIG-043).
	AnchorIDs []int64
	// BlockID is the block that matched, 0 for none.
	BlockID int64
	// Integrity is set when the judgement decided the integrity of a no_key signature by an anchor's key (R-SIG-012): valid, or invalid
	// with Reason. The integrity verdict is a fact of the bytes: the caller writes it to the signature row for every scope. An invalid one
	// has no trust row (R-SIG-008, "anchors never repair the bytes"): the caller writes none, and State is untrusted with no anchor.
	Integrity signatures.State
	Reason    string
	// ProvenAt and TimeProof are the time a log entry or a timestamp proves in this scope (Q-SIG-41); zero when none, and the time of
	// the signature is then its ReceivedAt (R-SIG-007).
	ProvenAt  time.Time
	TimeProof string
}

// Judge judges sig in sc under blocks (R-SIG-008): a block matching one of its key IDs or its identity makes it blocked whatever anchor
// knows it (R-SIG-020); else internal when an anchor of the Helm values, the administrator or the project knows it (internal wins,
// Q-SIG-25), public when only a shipped one does, untrusted otherwise and for a no_key signature no anchor's key verifies. It reads no
// content, and a signature in any other state than valid or no_key is untrusted with no anchor.
func Judge(sig Signature, sc *Scope, blocks []Block) Verdict {
	if sc == nil || (sig.State != signatures.StateValid && sig.State != signatures.StateNoKey) {
		return Verdict{State: StateUntrusted}
	}
	j := newJudgement(sig, sc)
	for _, a := range j.anchors {
		j.judge(a)
	}
	v := Verdict{ProvenAt: j.proven, TimeProof: j.proof}
	if sig.State == signatures.StateNoKey {
		switch {
		case j.keyVerified:
			v.Integrity = signatures.StateValid
		case j.keyFailed:
			v.Integrity, v.Reason, v.State = signatures.StateInvalid, signatures.ReasonSignatureMismatch, StateUntrusted
			return v
		}
	}
	v.AnchorIDs = j.known
	if id := matchBlock(blocks, j.ids, sig.Signer); id != 0 {
		v.State, v.BlockID = StateBlocked, id
		return v
	}
	switch {
	case j.internal:
		v.State = StateInternal
	case j.public:
		v.State = StatePublic
	default:
		v.State = StateUntrusted
	}
	return v
}

// judgement is the state of one Judge call.
type judgement struct {
	sig     Signature
	sc      *Scope
	anchors []anchors.Anchor
	// at is the time of the signature in the scope; proven and proof what proves a time (a timestamp or a Sigstore judgement).
	at     time.Time
	proven time.Time
	proof  string
	chain  []*x509.Certificate
	// ids are the identifiers a block can match: the stored key IDs and the keys of anchors that verified the signature.
	ids map[string]bool
	// known are the anchors that know the signer; internal and public whether one of an own or a shipped scope is among them.
	known            []int64
	internal, public bool
	// keyVerified and keyFailed are what the anchors' keys said of a keyless signature.
	keyVerified, keyFailed bool
}

func newJudgement(sig Signature, sc *Scope) *judgement {
	j := &judgement{sig: sig, sc: sc, ids: map[string]bool{}}
	for _, a := range sc.Anchors {
		if a.Parsed != nil {
			j.anchors = append(j.anchors, a)
		}
	}
	sort.SliceStable(j.anchors, func(a, b int) bool { return j.anchors[a].ID < j.anchors[b].ID })
	for _, id := range sig.KeyIDs {
		j.ids[id] = true
	}
	if ev := sig.Evidence; ev != nil && len(ev.Chain) > 0 {
		if chain, err := scheme.ParseChain(ev.Chain, maxChain); err == nil && len(chain) > 0 {
			j.chain = chain
		}
	}
	j.at = sig.ReceivedAt
	j.timestampTime()
	return j
}

// timestampTime finds the earliest time a timestamp of the evidence proves under a timestamping anchor of the scope (R-SIG-007, Q-SIG-41).
func (j *judgement) timestampTime() {
	if j.sig.Evidence == nil {
		return
	}
	for _, ts := range j.sig.Evidence.Timestamps {
		chain, err := scheme.ParseChain(ts.Chain, maxChain)
		if err != nil || len(chain) == 0 {
			continue
		}
		for _, a := range j.anchors {
			ca := a.Parsed.CA
			if a.Kind != anchors.KindCA || ca == nil || ca.Use != anchors.UseTimestamping {
				continue
			}
			if builds(chain, ca.Certificates, ts.Time, x509.ExtKeyUsageTimeStamping) {
				j.prove(ts.Time, ProofTimestamp)
				j.at = j.proven
			}
		}
	}
}

// prove keeps the earliest proven time.
func (j *judgement) prove(t time.Time, proof string) {
	if t.IsZero() {
		return
	}
	if j.proven.IsZero() || t.Before(j.proven) {
		j.proven, j.proof = t.UTC(), proof
	}
}

// judge asks one anchor whether it knows the signer.
func (j *judgement) judge(a anchors.Anchor) {
	var knows bool
	switch a.Kind {
	case anchors.KindCA:
		knows = j.caKnows(a.Parsed.CA)
	case anchors.KindOpenPGP:
		knows = j.openPGPKnows(a.Parsed.OpenPGP)
	case anchors.KindPublicKey:
		knows = j.publicKeyKnows(a.Parsed.Public)
	case anchors.KindSigstore:
		knows = j.sigstoreKnows(a)
	}
	if !knows {
		return
	}
	j.known = append(j.known, a.ID)
	if a.Scope == anchors.ScopeShipped {
		j.public = true
	} else {
		j.internal = true
	}
}

// caKnows reports whether a CA anchor of the use codeSigning knows the evidence's chain at the signature's time (R-SIG-008, Q-SIG-30).
// A timestamping anchor never knows a signer.
func (j *judgement) caKnows(ca *anchors.ParsedCA) bool {
	if ca == nil || ca.Use == anchors.UseTimestamping || len(j.chain) == 0 {
		return false
	}
	leaf := j.chain[0]
	if !slices.Contains(leaf.ExtKeyUsage, x509.ExtKeyUsageCodeSigning) || !subjectMatches(ca, leaf) {
		return false
	}
	for _, c := range ca.Certificates {
		if bytes.Equal(c.Raw, leaf.Raw) {
			return !j.at.Before(leaf.NotBefore) && !j.at.After(leaf.NotAfter)
		}
	}
	return builds(j.chain, ca.Certificates, j.at, x509.ExtKeyUsageCodeSigning)
}

// builds reports whether chain's leaf builds to one of roots at t with the extended key use, the rest of chain as intermediates.
func builds(chain, roots []*x509.Certificate, t time.Time, use x509.ExtKeyUsage) bool {
	if len(roots) == 0 {
		return false
	}
	pool := x509.NewCertPool()
	for _, r := range roots {
		pool.AddCert(r)
	}
	inter := x509.NewCertPool()
	for _, c := range chain[1:] {
		inter.AddCert(c)
	}
	_, err := chain[0].Verify(x509.VerifyOptions{Roots: pool, Intermediates: inter, CurrentTime: t, KeyUsages: []x509.ExtKeyUsage{use}})
	return err == nil
}

// subjectMatches applies a CA anchor's subject restriction (attributes O, OU, CN, C, each equal to one of the leaf's values) and its
// patterns of alternative names (one name matches one pattern whole).
func subjectMatches(ca *anchors.ParsedCA, leaf *x509.Certificate) bool {
	for attr, want := range ca.Subject {
		var have []string
		switch attr {
		case "O":
			have = leaf.Subject.Organization
		case "OU":
			have = leaf.Subject.OrganizationalUnit
		case "CN":
			have = []string{leaf.Subject.CommonName}
		case "C":
			have = leaf.Subject.Country
		}
		if !slices.Contains(have, want) {
			return false
		}
	}
	if len(ca.SANPatterns) == 0 {
		return true
	}
	for _, name := range alternativeNames(leaf) {
		for _, p := range ca.SANPatterns {
			if whole(p, name) {
				return true
			}
		}
	}
	return false
}

func alternativeNames(c *x509.Certificate) []string {
	var out []string
	for _, u := range c.URIs {
		out = append(out, u.String())
	}
	out = append(out, c.EmailAddresses...)
	out = append(out, c.DNSNames...)
	for _, ip := range c.IPAddresses {
		out = append(out, ip.String())
	}
	return out
}

// whole reports whether p matches all of s, whether or not it was compiled anchored.
func whole(p *regexp.Regexp, s string) bool {
	loc := p.FindStringIndex(s)
	return loc != nil && loc[0] == 0 && loc[1] == len(s)
}

// openPGPKnows reports whether a key of an OpenPGP anchor the issuer names verifies the stored digest and was valid at the time
// (R-SIG-007, R-SIG-008).
func (j *judgement) openPGPKnows(certs []anchors.OpenPGPCertificate) bool {
	ev := j.sig.Evidence
	if ev == nil || ev.Key == nil || ev.Key.Kind != openpgp.KindOpenPGP || len(j.sig.SignedDigest) == 0 {
		return false
	}
	knows := false
	for _, cert := range certs {
		m, err := openpgp.VerifyDigest(ev.Key.Signature, j.sig.SignedDigest, cert, j.at)
		if err != nil || !m.Named {
			continue
		}
		if !m.Verified {
			j.keyFailed = true
			continue
		}
		j.keyVerified = true
		j.ids[m.Fingerprint], j.ids[m.KeyFingerprint] = true, true
		knows = knows || m.ValidAt
	}
	return knows
}

// publicKeyKnows reports whether a key of a public-key anchor verifies the stored digest and had not expired at the time: the keys the
// evidence names by keyid (or SHA-256) when it names one of them, else every key, whose failures then say nothing.
func (j *judgement) publicKeyKnows(keys []anchors.ParsedKey) bool {
	ev := j.sig.Evidence
	if ev == nil || ev.Key == nil || ev.Key.Kind == openpgp.KindOpenPGP || len(j.sig.SignedDigest) == 0 {
		return false
	}
	var named []anchors.ParsedKey
	for _, k := range keys {
		if ev.Key.KeyID != "" && (k.KeyID == ev.Key.KeyID || strings.EqualFold(k.SHA256, ev.Key.KeyID)) {
			named = append(named, k)
		}
	}
	try, counts := named, true
	if len(named) == 0 {
		try, counts = keys, false
	}
	knows := false
	for _, k := range try {
		if _, ok := rawkey.Verify(ev.Key.Kind, k.Key, ev.Key.Hash, j.sig.SignedDigest, ev.Key.Signature); !ok {
			j.keyFailed = j.keyFailed || counts
			continue
		}
		j.keyVerified = true
		j.ids[k.SHA256] = true
		if k.KeyID != "" {
			j.ids[k.KeyID] = true
		}
		knows = knows || !rawkey.Expired(k, j.at)
	}
	return knows
}

// sigstoreKnows judges the evidence's bundle against a Sigstore identity anchor: its own trust root, or the public root in use when it
// names the public one (R-SIG-008, Q-SIG-27). A time the root proves is the scope's proof.
func (j *judgement) sigstoreKnows(a anchors.Anchor) bool {
	ev, id := j.sig.Evidence, a.Parsed.Sigstore
	if ev == nil || ev.Sigstore == nil || id == nil {
		return false
	}
	root := j.sc.PublicRoot
	if len(id.TrustedRoot) > 0 {
		root = j.sc.Roots[a.ID]
	}
	if root == nil {
		return false
	}
	jm, err := sigstore.Judge(ev.Sigstore, root, sigstore.Identity{Issuer: id.Issuer, Identity: id.Identity, Pattern: id.Pattern})
	if err != nil || !jm.Known {
		return false
	}
	j.prove(jm.ProvenAt, jm.Proof)
	return true
}

// matchBlock is the block of lowest ID that matches the identifiers or the identity (R-SIG-020), 0 for none. A fingerprint or a keyid in
// hex matches regardless of case (certificates' are spelled lower-case, OpenPGP's upper-case); an issuer and serial and an npm keyid
// match exactly.
func matchBlock(blocks []Block, ids map[string]bool, signer *signatures.Signer) int64 {
	sorted := slices.Clone(blocks)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].ID < sorted[b].ID })
	for _, b := range sorted {
		if blockMatches(b, ids, signer) {
			return b.ID
		}
	}
	return 0
}

func blockMatches(b Block, ids map[string]bool, signer *signatures.Signer) bool {
	var issuer, identity string
	if signer != nil {
		issuer, identity = signer.OIDCIssuer, signer.Identity
	}
	return signatures.BlockMatches(b.Kind, b.Value, b.Issuer, ids, issuer, identity)
}
