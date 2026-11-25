package rules

import (
	rc "github.com/guided-traffic/quader/internal/remotecuration"
)

// Narrowing is the part rules of an entry of repository.narrowed (remote-curation.md R-CUR-006 "Journal", repositories-and-proxy.md
// R-REPO-061): the subjects a write of the rules blocked (a block added, an allow turned into a block) and the subjects whose allow it
// removed. The replay of a restore adds the blocks and removes the allows on the restored rule set, so a restore keeps the repository
// narrowed; a widening is never journaled. RangesNarrowed are the ranges of versions a write set on a package's allow (R-CUR-026,
// Q-CUR-225): the replay keeps the narrower of the restored range and the journaled one, and gives a package the restored set delivers
// without an allow of its own the journaled range.
type Narrowing struct {
	BlocksAdded    []Subject        `json:"blocksAdded,omitempty"`
	AllowsRemoved  []Subject        `json:"allowsRemoved,omitempty"`
	RangesNarrowed []RangeNarrowing `json:"rangesNarrowed,omitempty"`
}

// RangeNarrowing is a range of versions a write set on a package's allow, the package in its canonical name and the bounds in the
// format's version canon; a bound "" is no bound on that side.
type RangeNarrowing struct {
	Name        string `json:"name"`
	FromVersion string `json:"fromVersion,omitempty"`
	ToVersion   string `json:"toVersion,omitempty"`
	ToInclusive bool   `json:"toInclusive"`
}

// Range is the narrowing's range.
func (r RangeNarrowing) Range() rc.Range {
	return rc.Range{From: r.FromVersion, To: r.ToVersion, ToInclusive: r.ToInclusive}.Normal()
}

// Valid reports whether the narrowing names a package and bounds something, each member within the table's bounds.
func (r RangeNarrowing) Valid() bool {
	return r.Name != "" && len(r.Name) <= MaxNameLength && (r.FromVersion != "" || r.ToVersion != "") &&
		len(r.FromVersion) <= MaxVersionLength && len(r.ToVersion) <= MaxVersionLength
}

// Empty reports whether the narrowing carries nothing.
func (n *Narrowing) Empty() bool {
	return n == nil || len(n.BlocksAdded)+len(n.AllowsRemoved)+len(n.RangesNarrowed) == 0
}

// Subject is the subject of a rule as an entry carries it: the kind and the members of that kind, in their canonical forms.
type Subject struct {
	Kind    rc.RuleKind `json:"kind"`
	Pattern string      `json:"pattern,omitempty"`
	Name    string      `json:"name,omitempty"`
	Version string      `json:"version,omitempty"`
}

// SubjectOfRule is the subject of a rule.
func SubjectOfRule(r Rule) Subject {
	return Subject{Kind: r.Kind, Pattern: r.Pattern, Name: r.Name, Version: r.Version}
}

// Valid reports whether the subject has the members of its kind and no other, as the table's CHECK holds a row (R-CUR-006).
func (s Subject) Valid() bool {
	switch s.Kind {
	case rc.KindSection:
		return s.Pattern != "" && s.Name == "" && s.Version == "" && len(s.Pattern) <= MaxPatternLength
	case rc.KindPackage:
		return s.Pattern == "" && s.Name != "" && s.Version == "" && len(s.Name) <= MaxNameLength
	case rc.KindVersion:
		return s.Pattern == "" && s.Name != "" && s.Version != "" && len(s.Name) <= MaxNameLength && len(s.Version) <= MaxVersionLength
	}
	return false
}

// The bounds of a rule's members (the table's CHECKs, R-CUR-006) and of a repository's section rules.
const (
	MaxPatternLength = 1024
	MaxNameLength    = 1024
	MaxVersionLength = 256
	MaxNoteLength    = 1000
	// MaxSections is the most section rules a repository holds: the bound of a pattern list (R-REPO-005).
	MaxSections = 100
	// DefaultMaxRules is proxy.delivery.max_rules's default.
	DefaultMaxRules = 50000
)
