// Package gateset is the setting of the gate per repository of the signatures (signatures.md R-SIG-061): the member
// signatures.onlyVerified and signatures.anchors of a repository, the order of a change of it (what narrows, what weakens) and the replay
// of a journaled narrowing. It imports nothing of the module, so that the repository model, which validates and journals the member, and
// the gate (internal/signatures/gate), which applies it, read one rule.
package gateset

import (
	"regexp"
	"slices"
	"strings"
)

// Setting is a repository's gate as its document's member signatures holds it (R-SIG-061): OnlyVerified switches it on, and Anchors,
// empty for any anchor of the repository's scope, names the anchors it accepts as "<scope>/<key>".
type Setting struct {
	OnlyVerified bool
	Anchors      []string
}

// MaxAnchors bounds the references of a repository's member (R-SIG-061).
const MaxAnchors = 32

// The scopes a reference names (R-SIG-016).
const (
	ScopeShipped       = "shipped"
	ScopeConfiguration = "configuration"
	ScopeInstallation  = "installation"
	ScopeProject       = "project"
)

// keyPattern is the rule of signature_anchors.key (R-SIG-015).
var keyPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ParseRef splits a reference "<scope>/<key>" (Q-SIG-77); ok is false for one whose scope is none of the four or whose key breaks the
// rule of an anchor's key.
func ParseRef(ref string) (scope, key string, ok bool) {
	scope, key, cut := strings.Cut(ref, "/")
	if !cut || len(key) > 64 || !keyPattern.MatchString(key) {
		return "", "", false
	}
	switch scope {
	case ScopeShipped, ScopeConfiguration, ScopeInstallation, ScopeProject:
		return scope, key, true
	}
	return "", "", false
}

// Change is what a write of the member does to the gate (the table of R-SIG-061).
type Change int

const (
	// Same changes nothing the gate does: equal members, or anchors changed while the gate is off.
	Same Change = iota
	// Narrows serves less: the gate switched on, any anchor replaced by a list, a reference removed from a list that stays non-empty.
	Narrows
	// Weakens serves more: the gate switched off, a list replaced by any anchor, a reference added; a write that does both (one
	// reference swapped for another) weakens.
	Weakens
)

// Order compares the stored member with the new one (R-SIG-061). Anchors stored while the gate is off do nothing, so a change of them
// alone is Same; a create is compared with the zero Setting, so a create never weakens.
func Order(cur, next Setting) Change {
	switch {
	case !cur.OnlyVerified && !next.OnlyVerified:
		return Same
	case !cur.OnlyVerified:
		return Narrows
	case !next.OnlyVerified:
		return Weakens
	}
	before, after := refSet(cur.Anchors), refSet(next.Anchors)
	switch {
	case len(before) == 0 && len(after) == 0:
		return Same
	case len(before) == 0:
		return Narrows
	case len(after) == 0:
		return Weakens
	}
	for r := range after {
		if !before[r] {
			return Weakens
		}
	}
	if len(after) < len(before) {
		return Narrows
	}
	return Same
}

// Narrower is the replay of a journaled narrowing of the member (R-SIG-061): the gate on, and of the restored anchors and the
// journaled ones the narrower: a list before any anchor, and of two lists the references in both. Two lists without a common
// reference would mean any anchor, which widens, so disable is true and the repository is disabled instead (R-REPO-061). Anchors the
// restored member stores while its gate is off do nothing and count as any anchor.
func Narrower(restored, journaled Setting) (out Setting, disable bool) {
	have := restored.Anchors
	if !restored.OnlyVerified {
		have = nil
	}
	want := journaled.Anchors
	if !journaled.OnlyVerified {
		want = nil
	}
	out.OnlyVerified = true
	switch {
	case len(have) == 0:
		out.Anchors = slices.Clone(want)
	case len(want) == 0:
		out.Anchors = slices.Clone(have)
	default:
		set := refSet(want)
		for _, r := range have {
			if set[r] && !slices.Contains(out.Anchors, r) {
				out.Anchors = append(out.Anchors, r)
			}
		}
		if len(out.Anchors) == 0 {
			return Setting{}, true
		}
	}
	return out, false
}

// Equal reports whether two members say the same: the switch and the same set of references.
func Equal(a, b Setting) bool {
	if a.OnlyVerified != b.OnlyVerified {
		return false
	}
	x, y := refSet(a.Anchors), refSet(b.Anchors)
	if len(x) != len(y) {
		return false
	}
	for r := range x {
		if !y[r] {
			return false
		}
	}
	return true
}

func refSet(refs []string) map[string]bool {
	out := make(map[string]bool, len(refs))
	for _, r := range refs {
		out[r] = true
	}
	return out
}
