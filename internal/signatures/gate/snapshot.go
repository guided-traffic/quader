package gate

import (
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/gateset"
)

// Setting is a repository's member signatures as the gate applies it (gateset.Setting).
type Setting = gateset.Setting

// The scopes of an anchor (R-SIG-016).
const (
	ScopeShipped = gateset.ScopeShipped
	ScopeProject = gateset.ScopeProject
)

// Anchor is a trust anchor as the replica's snapshot holds it (R-SIG-066): enough to resolve a repository's references and to subtract
// a narrowing, no material.
type Anchor struct {
	ID int64
	// Scope is shipped, configuration, installation or project; ProjectID the project's uuid for a project anchor.
	Scope     string
	ProjectID string
	Key       string
	Enabled   bool
	// NarrowedEpoch is the anchor epoch of the write that last narrowed the anchor (switched off, its material changed); 0 for never.
	NarrowedEpoch int64
}

// Block is a blocked signer as the snapshot holds it, with the epoch its creation raised.
type Block struct {
	ID     int64
	Kind   string
	Value  string
	Issuer string
	Epoch  int64
}

// Snapshot is what a replica holds of anchors, blocks and the switch for project Owners (R-SIG-041, R-SIG-066), refreshed through the
// change area signature_anchors within the propagation bound of R-ARCH-018. The gate subtracts from a trust row what narrowed since
// the row was computed, without cryptography.
type Snapshot struct {
	Anchors        map[int64]Anchor
	Blocks         []Block
	ProjectAnchors bool
	Epoch          int64
}

// inForce reports whether an anchor counts at all now: it exists, it is switched on, and a project anchor only while the switch allows
// project anchors (R-SIG-019).
func (s *Snapshot) inForce(a Anchor) bool {
	return a.Enabled && (a.Scope != ScopeProject || s.ProjectAnchors)
}

// Chosen resolves a repository's references in its scope (R-SIG-061, Q-SIG-77): the IDs of the anchors in force that they name. A
// reference to an anchor that was removed, that is switched off, or a project anchor while the switch is off resolves to nothing (fail
// closed); a reference to a project anchor names one of the repository's own project.
func (s *Snapshot) Chosen(refs []string, projectID string) map[int64]bool {
	out := map[int64]bool{}
	if s == nil {
		return out
	}
	want := make(map[string]bool, len(refs))
	for _, r := range refs {
		want[r] = true
	}
	for id, a := range s.Anchors {
		if !s.inForce(a) || !want[a.Scope+"/"+a.Key] {
			continue
		}
		if a.Scope == ScopeProject && (projectID == "" || a.ProjectID != projectID) {
			continue
		}
		out[id] = true
	}
	return out
}

// The trust states of a row (R-SIG-008).
const (
	TrustPublic    = "public"
	TrustInternal  = "internal"
	TrustUntrusted = "untrusted"
	TrustBlocked   = "blocked"
)

// Subtract applies to a trust row what narrowed since it was computed (R-SIG-066): of its anchors only those in force whose narrowing
// is not above the row's epoch count, and a block created after the row that matches the signature's key IDs or its issuer and identity
// makes it blocked. What remains decides: no anchor, untrusted; shipped anchors only, public; else internal. A widening is not applied
// here: it reaches the gate with the trust job's new row. A stored blocked row stays blocked.
func (s *Snapshot) Subtract(r Row) (state string, anchors []int64) {
	if r.TrustState == TrustBlocked {
		return TrustBlocked, nil
	}
	if s == nil {
		return r.TrustState, r.TrustAnchors
	}
	ids := make(map[string]bool, len(r.KeyIDs))
	for _, k := range r.KeyIDs {
		ids[k] = true
	}
	for _, b := range s.Blocks {
		if b.Epoch > r.TrustEpoch && signatures.BlockMatches(b.Kind, b.Value, b.Issuer, ids, r.OIDCIssuer, r.Identity) {
			return TrustBlocked, nil
		}
	}
	internal := false
	for _, id := range r.TrustAnchors {
		a, ok := s.Anchors[id]
		if !ok || !s.inForce(a) || a.NarrowedEpoch > r.TrustEpoch {
			continue
		}
		anchors = append(anchors, id)
		if a.Scope != ScopeShipped {
			internal = true
		}
	}
	switch {
	case len(anchors) == 0:
		return TrustUntrusted, nil
	case internal:
		return TrustInternal, anchors
	}
	return TrustPublic, anchors
}
