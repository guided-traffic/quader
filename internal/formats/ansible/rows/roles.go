package rows

import "time"

// The states of a role's README (R-ANS-056): never asked, a fetch queued, the upstream's README held, the upstream has none, or the
// fetch failed (RoleReadme.Reason says why, one of the DocReason* words).
const (
	RoleReadmeNone        = "none"
	RoleReadmeQueued      = "queued"
	RoleReadmeAvailable   = "available"
	RoleReadmeAbsent      = "absent"
	RoleReadmeUnavailable = "unavailable"
)

// MaxRoleReadmeBytes bounds the README HTML of a role that is kept (R-ANS-056): a larger one is unavailable with the reason too_large.
const MaxRoleReadmeBytes = 512 << 10

// Role is what a remote keeps of a role of its upstream's v1 API (R-ANS-056, the table ansible_roles): the facts of the role document
// of a lookup, as the upstream sent them, and the state of its README. Nothing of it decides a request: the package of a role stays
// "role/<id>" (Q-ANS-36).
type Role struct {
	// ID is the upstream's numeric id.
	ID                                   int64
	Owner, Name, Description             string
	GitHubUser, GitHubRepo, GitHubBranch string
	Tags, Dependencies                   []string
	// Downloads is the upstream's download count, -1 when the document names none.
	Downloads int64
	// Commit is the commit the upstream imported the role from; a lookup that names another one asks for the README again.
	Commit   string
	Imported time.Time
	Readme   RoleReadme
	// Packaged says that the repository has the role's package: a version of the role was cached, so the role has a page whose README
	// is wanted. Read by RoleStore.Role only.
	Packaged bool
}

// RoleReadme is the state of a role's README and, when asked for, its HTML (the upstream's readme_html, untrusted).
type RoleReadme struct {
	State, Reason, File string
	// Commit is the commit the README was fetched for.
	Commit string
	HTML   string
}

// RoleView is the facts of a role as the screens read them for one generic version of the role's package.
type RoleView struct {
	VersionID int64
	Package   string
	Role
}
