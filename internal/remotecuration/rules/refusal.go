package rules

import (
	"errors"
	"net/url"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
)

// RefusalKind says what a refusal refused (R-CUR-004, the event's member kind).
type RefusalKind string

// The kinds of a refusal.
const (
	// KindDocument: a package document, a version list or tag list, a version document.
	KindDocument RefusalKind = "document"
	// KindArtifact: a file of a version.
	KindArtifact RefusalKind = "artifact"
)

// Refusal is a refusal by the delivery rules of one remote repository (R-CUR-004): what the client's answer says and what the record
// of the refusal counts (the event remote.delivery.refused, the metric and the request list's entry). It travels as the cause of the
// client's error ([Refusal.Err]), so that whoever answers the client finds it ([RefusalOf]) and records it once, whatever the place
// that refused: before a fill, at the serve point or at a document.
type Refusal struct {
	RepoID int64
	Repo   qname.Repo
	// Name is the normalized package name; Version the version in the format's canon, "" for a refusal of the package whole.
	Name, Version string
	Reason        rc.Reason
	Kind          RefusalKind
	// Path is the repository path the client asked for.
	Path string
}

// Error renders the refusal for logs.
func (r *Refusal) Error() string {
	v := r.Version
	if v == "" {
		v = "any version"
	}
	return "rules: " + r.Name + " " + v + " is not delivered by " + r.Repo.String() + " (" + string(r.Reason) + ")"
}

// Text is the answer's text (R-CUR-004): the package, the version or "any version", the repository, the reason and where to ask. It
// names no rule's note and no other repository. base is the response base URL without a trailing slash.
func (r *Refusal) Text(base string) string {
	v := r.Version
	if v == "" {
		v = "any version"
	}
	why := "blocked by a rule of the repository."
	if r.Reason == rc.ReasonNotAllowed {
		why = "this repository delivers only what is allowed."
	}
	ask := base + "/ui/projects/" + url.PathEscape(r.Repo.Project) + "/repositories/" + url.PathEscape(r.Repo.Key) +
		"/upstream?name=" + url.QueryEscape(r.Name)
	return r.Name + " " + v + " is not allowed in " + r.Repo.String() + ": " + why + " Ask for it at " + ask + "."
}

// Err is the client's error of the refusal: Forbidden with the code not_allowed_in_repository and Cache-Control: no-store, the text of
// [Refusal.Text], the refusal its cause. Each format's WriteError renders it in its client's shape.
func (r *Refusal) Err(base string) error {
	return errs.E(errs.Forbidden, errs.CodeNotAllowedInRepository, r.Text(base),
		errs.WithHeader("Cache-Control", "no-store"), errs.WithCause(r))
}

// RefusalOf returns the refusal an error carries, if it is one of the rules.
func RefusalOf(err error) (*Refusal, bool) {
	var r *Refusal
	if errors.As(err, &r) {
		return r, true
	}
	return nil, false
}

// EventReason is the event's spelling of a reason: blocked or not_allowed (the schema of remote.delivery.refused).
func EventReason(r rc.Reason) string {
	if r == rc.ReasonNotAllowed {
		return "not_allowed"
	}
	return "blocked"
}
