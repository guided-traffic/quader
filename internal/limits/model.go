package limits

import (
	"net/netip"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/qname"
)

// Mode is the instance's switch of R-LIM-026.
type Mode string

// The modes.
const (
	ModeAudit   Mode = "audit"
	ModeEnforce Mode = "enforce"
)

// PlaceKind is where a rule is set and where its budget counts (R-LIM-011).
type PlaceKind uint8

// The places.
const (
	PlaceInstance PlaceKind = iota
	PlaceProject
	PlaceRepository
)

// String is the place kind as the API and the store spell it.
func (k PlaceKind) String() string {
	switch k {
	case PlaceProject:
		return "project"
	case PlaceRepository:
		return "repository"
	}
	return "instance"
}

// Place is the instance, a project or a repository. The index keys places by name (what a request carries) and keeps the
// IDs, which the budget key and the usage store use, so a renamed project or repository keeps its budget.
type Place struct {
	Kind       PlaceKind
	ProjectID  string
	ProjectKey string
	RepoID     int64
	Repo       qname.Repo
}

// InstancePlace is the instance.
func InstancePlace() Place { return Place{Kind: PlaceInstance} }

// Key is the place as the budget key and the usage store's place column write it: instance, project:<uuid>,
// repository:<id> (R-LIM-014).
func (p Place) Key() string {
	switch p.Kind {
	case PlaceProject:
		return "project:" + p.ProjectID
	case PlaceRepository:
		return "repository:" + strconv.FormatInt(p.RepoID, 10)
	}
	return "instance"
}

// LogName is the place as the request log names it (R-LIM-030): instance, project:<key>, repository:<project>/<key>.
func (p Place) LogName() string {
	switch p.Kind {
	case PlaceProject:
		return "project:" + p.ProjectKey
	case PlaceRepository:
		return "repository:" + p.Repo.String()
	}
	return "instance"
}

// WhoKind is the kind of a rule's "who" (R-LIM-011), as the store spells it.
type WhoKind string

// The kinds of "who".
const (
	WhoUser           WhoKind = "user"
	WhoTechnicalToken WhoKind = "technical_token"
	WhoCIProject      WhoKind = "ci_project"
	WhoAddress        WhoKind = "address"
	WhoGroup          WhoKind = "group"
	WhoEvery          WhoKind = "every"
)

// The caller kinds of an `every` rule, as the store spells them.
const (
	EveryUser           = "user"
	EveryTechnicalToken = "technical_token"
	EveryCI             = "ci"
	EveryAnonymous      = "anonymous"
	EveryAdministrator  = "administrator"
)

// everyOf is the `every` kind of a subject.
func everyOf(s Subject) string {
	if s.Admin {
		return EveryAdministrator
	}
	switch s.Kind {
	case KindTechnicalToken, KindCertificate:
		// A certificate subject is a machine client with no rule of its own to name it: the kind default of the technical tokens
		// applies (Q-LIM-96).
		return EveryTechnicalToken
	case KindCI:
		return EveryCI
	case KindAnonymous:
		return EveryAnonymous
	}
	return EveryUser
}

// Profile is a named set of every figure (R-LIM-010).
type Profile struct {
	ID          string
	Key         string
	Name        string
	Description string
	Builtin     bool
	Figures     Figures
	Version     int64
	CreatedAt   time.Time
	CreatedBy   string
	UpdatedAt   time.Time
	UpdatedBy   string
}

// The keys of the built-in profiles.
const (
	ProfileUnlimited  = "unlimited"
	ProfileStandard   = "standard"
	ProfileTechnical  = "technical"
	ProfileRestricted = "restricted"
	ProfileAnonymous  = "anonymous"
)

const (
	mb = 1_000_000
	gb = 1_000_000_000
)

// BuiltinProfiles are the five built-in profiles with their values (R-LIM-010): the request figures set from a measurement
// (Q-LIM-59, migration 00085), the others the development agent's estimates of the first migration. The migrations write the
// same numbers; a test compares them.
func BuiltinProfiles() map[string]Figures {
	return map[string]Figures{
		ProfileUnlimited:  {},
		ProfileStandard:   {200, 4000, 100000, 200 * mb, 300 * gb, 50 * mb, 30 * gb, 2000, 20, 1000 * gb, 500},
		ProfileTechnical:  {1000, 20000, 1000000, 500 * mb, 1000 * gb, 100 * mb, 100 * gb, 10000, 50, 5000 * gb, 2000},
		ProfileRestricted: {100, 1000, 25000, 50 * mb, 50 * gb, 10 * mb, 5 * gb, 200, 6, 100 * gb, 100},
		ProfileAnonymous:  {50, 1000, 20000, 20 * mb, 10 * gb, 5 * mb, 2 * gb, 20, 4, 30 * gb, 60},
	}
}

// Rule is an assignment (R-LIM-011): a profile with overrides for a "who" at a place.
type Rule struct {
	ID        string
	Place     Place
	WhoKind   WhoKind
	WhoRef    string
	Profile   string
	Overrides Overrides
	// Capped is a project or repository rule last written by a caller without limits:write (R-LIM-016).
	Capped  bool
	Builtin bool
	Note    string
	Version int64

	CreatedAt time.Time
	CreatedBy string
	UpdatedAt time.Time
	UpdatedBy string

	prefix netip.Prefix
}

// LogID is the rule as the request log names it: its ID, or builtin:<kind> for a built-in rule (R-LIM-030).
func (r *Rule) LogID() string {
	if r == nil {
		return ""
	}
	if r.Builtin && r.WhoKind == WhoEvery {
		return "builtin:" + r.WhoRef
	}
	return r.ID
}

// CeilingClass is a class of callers of a remote's ceiling (R-LIM-015).
type CeilingClass string

// The classes.
const (
	CeilingAnonymous CeilingClass = "anonymous"
	CeilingSignedIn  CeilingClass = "signed_in"
)

// Ceiling is a remote repository's limit for a class of callers together.
type Ceiling struct {
	RepoID                  int64
	Class                   CeilingClass
	UncachedBytesPerHour    int64
	UpstreamRequestsPerHour int64
	Version                 int64
	UpdatedAt               time.Time
	UpdatedBy               string
}

// Key is the ceiling's counter key: ceiling:<repository ID>:<class>.
func (c Ceiling) Key() string { return CeilingKey(c.RepoID, c.Class) }

// CeilingKey is the counter key of a ceiling.
func CeilingKey(repoID int64, class CeilingClass) string {
	return "ceiling:" + strconv.FormatInt(repoID, 10) + ":" + string(class)
}

// Maximum is a project's maximum (R-LIM-016).
type Maximum struct {
	ProjectID  string
	ProjectKey string
	Profile    string
	Overrides  Overrides
	Version    int64
	UpdatedAt  time.Time
	UpdatedBy  string
}

// Settings are the instance's mode and block time (R-LIM-026, R-LIM-025).
type Settings struct {
	Mode         Mode
	BlockSeconds int
	Version      int64
	UpdatedAt    time.Time
	UpdatedBy    string
}

// DefaultSettings are the settings of a fresh installation: Enforce, 900 s (decision 17 of usage-and-limits.md).
func DefaultSettings() Settings { return Settings{Mode: ModeEnforce, BlockSeconds: 900, Version: 1} }

// Snapshot is what the index is compiled from: everything the store holds of the limits.
type Snapshot struct {
	Profiles []Profile
	Rules    []Rule
	Ceilings []Ceiling
	Maxima   []Maximum
	Settings Settings
}
