package sigstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// The judgement of a bundle against a Sigstore identity anchor (R-SIG-008, R-SIG-012): the bundle the integrity check kept as evidence is
// verified against the anchor's trust root (its certificate chain to the root's certificate authorities at the time a log entry or a
// timestamp of that root proves, its log entries and timestamps against that root, its signature over the artifact digest it was
// checked against) and its leaf's OIDC issuer and identity are matched with the anchor's. It reads no content.

// Root is a parsed Sigstore trust root (trusted_root.json): what [Judge] verifies a bundle's certificate, log entries and timestamps
// against. The trust step parses each root once per run.
type Root struct {
	tr *root.TrustedRoot
}

// LoadRoot parses a trusted_root.json (sigstore-go's root.NewTrustedRootFromJSON), bounded as every JSON document of R-SIG-013.
func LoadRoot(doc json.RawMessage) (*Root, error) {
	tr, err := parseRoot(doc)
	if err != nil {
		return nil, err
	}
	return &Root{tr: tr}, nil
}

// rootCache keeps the trust roots the checks parsed, by the SHA-256 of their document: the integrity check of every bundle meets the
// same few roots (the public one and the anchors' own). It is emptied when it holds rootCacheSize roots.
var rootCache = struct {
	sync.Mutex
	m map[[32]byte]*root.TrustedRoot
}{m: map[[32]byte]*root.TrustedRoot{}}

const rootCacheSize = 64

// parseRoot parses a trust root document once.
func parseRoot(doc json.RawMessage) (*root.TrustedRoot, error) {
	if len(doc) == 0 {
		return nil, errors.New("sigstore: an empty trust root")
	}
	key := sha256.Sum256(doc)
	rootCache.Lock()
	tr, ok := rootCache.m[key]
	rootCache.Unlock()
	if ok {
		return tr, nil
	}
	if err := untrusted.CheckJSON(doc, untrusted.Limits{}); err != nil {
		return nil, fmt.Errorf("sigstore: the trust root is out of bounds: %w", err)
	}
	tr, err := root.NewTrustedRootFromJSON(doc)
	if err != nil {
		return nil, fmt.Errorf("sigstore: the trust root does not parse: %w", err)
	}
	rootCache.Lock()
	if len(rootCache.m) >= rootCacheSize {
		rootCache.m = map[[32]byte]*root.TrustedRoot{}
	}
	rootCache.m[key] = tr
	rootCache.Unlock()
	return tr, nil
}

// Identity is what a Sigstore identity anchor requires of a bundle's leaf: the OIDC issuer exactly ("" any) and the identity exactly or
// by an anchored pattern.
type Identity struct {
	Issuer   string
	Identity string
	Pattern  *regexp.Regexp
}

// matches reports whether a leaf's OIDC issuer and identity are the anchor's. A pattern must match the whole identity, whether or not it
// was compiled anchored.
func (id Identity) matches(issuer, identity string) bool {
	if id.Issuer != "" && id.Issuer != issuer {
		return false
	}
	if id.Pattern != nil {
		loc := id.Pattern.FindStringIndex(identity)
		return loc != nil && loc[0] == 0 && loc[1] == len(identity)
	}
	return id.Identity != "" && id.Identity == identity
}

// Judgement is what [Judge] found: whether the anchor knows the signer, and the time a log entry or a timestamp of the root proves
// (Proof "log" or "timestamp"), zero when none of the root's verified.
type Judgement struct {
	Known    bool
	ProvenAt time.Time
	Proof    string
}

// Judge verifies the bundle of ev against r (its certificate chain at the time a log entry or a timestamp of r proves, its log entries and
// timestamps) with the artifact digest of ev, and matches the leaf's issuer and identity with id. It reads no content. An error is a
// bundle that cannot be judged at all (it does not parse); a bundle r does not verify is a Judgement with Known false. A bundle signed
// with a key, and one whose leaf no certificate authority of r issued (cosign with the company's own PKI, judged by CA anchors through
// the evidence's chain), is not known.
func Judge(ev *scheme.SigstoreEvidence, r *Root, id Identity) (Judgement, error) {
	if ev == nil || r == nil || r.tr == nil {
		return Judgement{}, errors.New("sigstore: nothing to judge")
	}
	b, err := evidenceBundle(ev.Bundle)
	if err != nil {
		return Judgement{}, err
	}
	vc, err := b.VerificationContent()
	if err != nil {
		return Judgement{}, fmt.Errorf("sigstore: the bundle's certificate does not parse: %w", err)
	}
	if vc.Certificate() == nil {
		return Judgement{}, nil
	}
	opts := []verify.VerifierOption{verify.WithObserverTimestamps(1)}
	if knowsLog(b, r.tr) {
		opts = append(opts, verify.WithTransparencyLog(1))
	}
	v, err := verify.NewVerifier(r.tr, opts...)
	if err != nil {
		return Judgement{}, fmt.Errorf("sigstore: %w", err)
	}
	res, err := v.Verify(b, verify.NewPolicy(verify.WithArtifactDigest(ev.DigestAlg, ev.Digest), verify.WithoutIdentitiesUnsafe()))
	if err != nil || res.Signature == nil || res.Signature.Certificate == nil {
		return Judgement{}, nil //nolint:nilerr // a bundle the root does not verify is not known; that is the answer, not an error
	}
	if !id.matches(res.Signature.Certificate.Issuer, res.Signature.Certificate.SubjectAlternativeName) {
		return Judgement{}, nil
	}
	j := Judgement{Known: true}
	for _, t := range res.VerifiedTimestamps {
		proof := map[string]string{"Tlog": "log", "TimestampAuthority": "timestamp"}[t.Type]
		if proof != "" && (j.ProvenAt.IsZero() || t.Timestamp.Before(j.ProvenAt)) {
			j.ProvenAt, j.Proof = t.Timestamp.UTC(), proof
		}
	}
	return j, nil
}

// evidenceBundle parses the bundle of the evidence, bounded.
func evidenceBundle(data json.RawMessage) (*bundle.Bundle, error) {
	if err := untrusted.CheckJSON(data, untrusted.Limits{JSONDepth: 32, JSONMembers: 100_000}); err != nil {
		return nil, fmt.Errorf("sigstore: the evidence's bundle is out of bounds: %w", err)
	}
	pb := new(protobundle.Bundle)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(data, pb); err != nil {
		return nil, fmt.Errorf("sigstore: the evidence's bundle does not parse: %w", err)
	}
	b, err := bundle.NewBundle(pb)
	if err != nil {
		return nil, fmt.Errorf("sigstore: the evidence's bundle is not valid: %w", err)
	}
	return b, nil
}

// knowsLog reports whether r knows the log of a log entry of b: then the entry must verify and its integrated time proves the time.
func knowsLog(b *bundle.Bundle, tr *root.TrustedRoot) bool {
	entries, err := b.TlogEntries()
	if err != nil {
		return false
	}
	for _, e := range entries {
		if _, ok := tr.RekorLogs()[hex.EncodeToString([]byte(e.LogKeyID()))]; ok {
			return true
		}
	}
	return false
}
