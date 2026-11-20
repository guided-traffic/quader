package authz

import "github.com/guided-traffic/quader/internal/auth/principal"

// Level is a visibility level (R-AUTHZ-033), ordered from the most restrictive. A level names who reads a part of a repository
// besides the members of its project and the principals a grant names; it never grants a write, a role, a capability or an
// owner record (R-AUTHZ-033 rule 6).
type Level uint8

// The levels, in the order of the database function visibility_rank.
const (
	// LevelPrivate admits nobody besides members and grants.
	LevelPrivate Level = iota
	// LevelInternal admits every internal subject: every authenticated subject but an external user (R-AUTHZ-030).
	LevelInternal
	// LevelPublic admits every request, also one without a credential (R-AUTHZ-003).
	LevelPublic
)

// ParseLevel reads the name of a level as the API and the database spell it.
func ParseLevel(s string) (Level, bool) {
	switch s {
	case "private":
		return LevelPrivate, true
	case "internal":
		return LevelInternal, true
	case "public":
		return LevelPublic, true
	}
	return LevelPrivate, false
}

// String renders the level as the API and the database spell it; a value outside the order renders as private, the most
// restrictive level.
func (l Level) String() string {
	switch l {
	case LevelInternal:
		return "internal"
	case LevelPublic:
		return "public"
	}
	return "private"
}

// Part is the part of a repository a coordinate reads (R-AUTHZ-034). The zero value is the artifacts, so every existing
// coordinate keeps its meaning.
type Part uint8

// The parts.
const (
	// PartArtifacts is what package clients resolve, install and download, and the documents and searches that describe it.
	PartArtifacts Part = iota
	// PartWiki is the documentation of a repository's packages that quader serves or shows apart from their files.
	PartWiki
)

// String renders the part.
func (p Part) String() string {
	if p == PartWiki {
		return "wiki"
	}
	return "artifacts"
}

// Internal reports R-AUTHZ-030: every authenticated subject - a user (or the break-glass administrator) through a session or
// a personal token, a service account's technical token (of the instance, a project or a repository), a CI token - except a
// user flagged external, whose tokens carry the flag with it. The anonymous subject is not internal, and neither is a Bastion
// registration, which reads nothing on its own credential (Q-AUTHZ-320). The channel plays no part: a request through a
// Bastion is decided as any other.
func (s Subject) Internal() bool {
	if s.Anonymous || s.Kind == principal.KindAnonymous || s.External || isSystem(s) {
		return false // a system subject is never internal or external (R-AUTHZ-002)
	}
	return kindOf(s) != kindBastion
}
