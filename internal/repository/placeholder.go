package repository

import (
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// Members of the built-in project upstream in a virtual repository (R-REPO-071, Q-REPO-381) and the placeholders among them
// (R-REPO-072, Q-REPO-382). A virtual repository lists repositories of its own project by their bare keys and repositories of
// the project upstream by their qualified names; nothing else is a member. A member of upstream that is not switched on stays in
// the list as a placeholder: routing skips it as a disabled member, and a 404 of a name it would have been asked for says why.

// MemberRef is how a virtual repository of the project names a member in its member list and its pin rules (R-REPO-017,
// R-REPO-071, Q-REPO-381): the bare key for a repository of the same project, the qualified name for any other, which can only
// be upstream/<key> (the guard AI006). The document, the explanation and the header Quader-Resolved-From write it so.
func MemberRef(project string, member qname.Repo) string {
	if member.Project == project {
		return member.Key
	}
	return member.String()
}

// upstreamMemberKey returns the key of a member written upstream/<key>; ok is false for any other spelling, a bare key
// included. Whether the key is a repository key of R-REPO-002 is the lookup's to decide: a key that is none names no
// repository.
func upstreamMemberKey(member string) (key string, ok bool) {
	key, ok = strings.CutPrefix(member, wellknown.ProjectKey+"/")
	if !ok || key == "" || strings.Contains(key, "/") {
		return "", false
	}
	return key, true
}

// Placeholder reports whether the repository is a preset repository of the project upstream that is not active: a member of a
// virtual repository that takes no part until an administrator switches it on, and that says so (R-REPO-068, R-REPO-072).
func (r Repository) Placeholder() bool {
	return r.Upstream != nil && r.Upstream.Kind == wellknown.KindPreset && r.Upstream.State != wellknown.StateActive
}

// takesNoPart reports whether a member is dropped as disabled at step 2 of R-REPO-019: disabled or being deleted, a placeholder
// (which is disabled as well), and a system repository of the project upstream, which is never a member (R-REPO-070,
// Q-REPO-380) and is dropped here should a row name it anyway (fail closed).
func (r Repository) takesNoPart() bool {
	return r.Disabled || r.Lifecycle != Active || r.Placeholder() || r.Upstream.System()
}

// placeholderTail is the sentence of a placeholder's 404 (R-REPO-072) after the name of the member, one per state; "" for a state
// that has none.
func placeholderTail(state wellknown.State) string {
	switch state {
	case wellknown.StateNotSwitchedOn:
		return " is a member of this virtual repository, but an administrator of Quader has not switched it on."
	case wellknown.StateSwitchedOff:
		return " is a member of this virtual repository, but an administrator of Quader switched it off."
	case wellknown.StateRetired:
		return " is a member of this virtual repository, but Quader no longer offers it; remove it from the members."
	}
	return ""
}

// PlaceholderSentence is the sentence of R-REPO-072 for a placeholder in a state, with the number of the further placeholders
// that would have been asked as well (Q-REPO-400); "" for a state that has none (active, or a system repository's).
func PlaceholderSentence(member qname.Repo, state wellknown.State, others int) string {
	tail := placeholderTail(state)
	if tail == "" {
		return ""
	}
	s := member.String() + tail
	switch {
	case others == 1:
		s += " One more member of the project upstream is not in use either."
	case others > 1:
		s += " " + strconv.Itoa(others) + " more members of the project upstream are not in use either."
	}
	return s
}

// PlaceholderNotFound is the 404 of a request to a virtual repository that a placeholder would have been asked for (R-REPO-072,
// Q-REPO-382): the kind and the code of every other not-found answer, so each format keeps its own not-found code and status,
// and the sentence as the message the format renders in its error shape. nil for a state without a sentence.
func PlaceholderNotFound(member qname.Repo, state wellknown.State, others int) *errs.Error {
	s := PlaceholderSentence(member, state, others)
	if s == "" {
		return nil
	}
	return errs.E(errs.NotFound, errs.CodeNotFound, s)
}
