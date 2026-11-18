// Package visibility holds the installation's visibility settings (R-API-056): the default level and the levels restricted to
// administrators (R-AUTHZ-037), and the rules about levels that the project and the repository services share (R-AUTHZ-033): the
// three levels and their order, the validation of a level named by a client, the refusal of public while the default endpoint
// admits no request without a credential, and the refusal of a restricted level to a caller who is not an administrator. It
// imports neither the authorization engine nor the repository package, so both can use it.
package visibility

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The three levels as the API and the database spell them, from the most restrictive.
const (
	Private  = "private"
	Internal = "internal"
	Public   = "public"
)

// levels are the three levels in their order.
var levels = [...]string{Private, Internal, Public}

// MaxPublicObjects is the most projects, and the most repositories, that publicObjects lists (R-API-056).
const MaxPublicObjects = 100

// CodeDefaultRestricted is the field code of a settings write whose default level would be one of its restricted levels
// (Q-AUTHZ-284).
const CodeDefaultRestricted = "default_level_restricted"

// Rank orders the levels: private 0, internal 1, public 2; -1 for a string that is no level.
func Rank(level string) int {
	switch level {
	case Private:
		return 0
	case Internal:
		return 1
	case Public:
		return 2
	}
	return -1
}

// Valid reports whether level is one of the three levels.
func Valid(level string) bool { return Rank(level) >= 0 }

// Lower returns the lower of two valid levels.
func Lower(a, b string) string {
	if Rank(b) < Rank(a) {
		return b
	}
	return a
}

// Above reports whether level a is more open than level b.
func Above(a, b string) bool { return Rank(a) > Rank(b) }

// Normalize returns the levels of a set in the order private, internal, public, each once; a string that is no level is dropped.
func Normalize(set []string) []string {
	out := []string{}
	for _, level := range levels {
		if slices.Contains(set, level) {
			out = append(out, level)
		}
	}
	return out
}

// PublicNotAllowed is the 409 of a write that sets public while the default endpoint admits no request without a credential
// (R-AUTHZ-033 rule 5).
func PublicNotAllowed() error {
	return errs.E(errs.Conflict, errs.CodePublicNotAllowed,
		"The level public needs the default endpoint to admit requests without a credential (endpoints.default.auth.anonymous).")
}

// LevelRestricted is the 403 of a caller who is not an administrator and chooses a restricted level (R-AUTHZ-037), with the level
// at the member that named it, or at the member whose level a create could not take.
func LevelRestricted(level, pointer string) error {
	return errs.E(errs.Forbidden, errs.CodeVisibilityLevelRestricted,
		"The installation restricts the level "+level+" to administrators. An object that has it keeps it and may be lowered; choosing it anew needs security-settings:write.",
		errs.WithFields(errs.FieldError{Pointer: pointer, Code: errs.CodeVisibilityLevelRestricted, Detail: "Only an administrator chooses the level " + level + "."}))
}

// InvalidLevel is the 422 field error of a member that names no level.
func InvalidLevel(pointer string) errs.FieldError {
	return errs.FieldError{Pointer: pointer, Code: "invalid_value", Detail: "The level is one of private, internal and public."}
}

// Queries binds the generated queries to the PostgreSQL transaction behind tx.
func Queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errors.New("visibility: the transaction does not come from the PostgreSQL store")
	}
	return q.New(ptx), nil
}

// Rules are what a write of a level reads from the installation's settings in its own transaction (R-AUTHZ-033 rule 1,
// R-AUTHZ-037): the default level and the levels restricted to administrators.
type Rules struct {
	Default    string
	Restricted []string
}

// ReadRules reads the rules in the transaction that creates or changes a project or a repository; they are cached nowhere.
func ReadRules(ctx context.Context, qs *q.Queries) (Rules, error) {
	rows, err := qs.VisibilityDefaultGet(ctx)
	if err != nil {
		return Rules{}, err
	}
	if len(rows) != 1 || !Valid(rows[0].DefaultLevel) {
		return Rules{}, errors.New("visibility: the settings row is missing")
	}
	return Rules{Default: rows[0].DefaultLevel, Restricted: Normalize(rows[0].RestrictedLevels)}, nil
}

// IsRestricted reports whether the installation restricts level to administrators.
func (r Rules) IsRestricted(level string) bool { return slices.Contains(r.Restricted, level) }

// Allows reports whether a caller may choose level (Q-AUTHZ-283): an administrator every level, anyone else every level that is
// not restricted.
func (r Rules) Allows(level string, admin bool) bool { return admin || !r.IsRestricted(level) }

// Highest returns the highest level at or below ceiling that the caller may choose, "" when there is none (Q-AUTHZ-286): the
// level a create takes for a member it does not name.
func (r Rules) Highest(ceiling string, admin bool) string {
	for i := Rank(ceiling); i >= 0; i-- {
		if r.Allows(levels[i], admin) {
			return levels[i]
		}
	}
	return ""
}

// Settings is the installation's visibility settings as the API shows them.
type Settings struct {
	DefaultLevel string
	// RestrictedLevels are the levels only an administrator may choose (R-AUTHZ-037), in the order of the levels.
	RestrictedLevels []string
	PublicAvailable  bool
	PublicObjects    PublicObjects
	// Version is the ETag source; it moves with the default level and the restricted levels.
	Version int64
}

// PublicObjects are the projects and the repositories whose artifacts are public, at most MaxPublicObjects of each in key order.
type PublicObjects struct {
	Projects     []string
	Repositories []string
	Truncated    bool
}

// Patch is a merge patch of the settings: a nil member is absent and keeps the stored value (R-API-056). RestrictedLevels
// replaces the whole set, as a merge patch replaces an array.
type Patch struct {
	DefaultLevel     *string
	RestrictedLevels *[]string
}

// Choices are the levels a caller may choose, as the forms of a project and a repository read them (R-API-076): the default
// level, the restricted levels, whether the caller is exempt from them, and whether public can be set at all.
type Choices struct {
	DefaultLevel        string
	RestrictedLevels    []string
	MayChooseRestricted bool
	PublicAvailable     bool
}

// Caller is who writes the settings and from where; it goes into the audit event.
type Caller struct {
	Actor  audit.Actor
	Source audit.Source
}

// Options configure [New].
type Options struct {
	// DB runs the writes in transactions.
	DB meta.Runner
	// Recorder records settings.visibility.changed in the transaction of the write.
	Recorder audit.Recorder
	// PublicAvailable reports whether the default endpoint admits requests without a credential
	// (endpoints.default.auth.anonymous, R-EP-010); nil means it does not.
	PublicAvailable func() bool
}

// Service reads and writes the installation's visibility settings.
type Service struct {
	o Options
}

// New creates the service.
func New(o Options) *Service {
	if o.Recorder == nil {
		o.Recorder = audit.Nop{}
	}
	return &Service{o: o}
}

func (s *Service) publicAvailable() bool { return s.o.PublicAvailable != nil && s.o.PublicAvailable() }

// Get returns the settings, read from PostgreSQL and not from a cache.
func (s *Service) Get(ctx context.Context) (Settings, error) {
	var out Settings
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := Queries(tx)
		if err != nil {
			return err
		}
		out, err = s.read(ctx, qs)
		return err
	})
	return out, err
}

func (s *Service) read(ctx context.Context, qs *q.Queries) (Settings, error) {
	rows, err := qs.VisibilityDefaultGet(ctx)
	if err != nil {
		return Settings{}, err
	}
	if len(rows) != 1 {
		return Settings{}, errors.New("visibility: the settings row is missing")
	}
	out := Settings{
		DefaultLevel: rows[0].DefaultLevel, RestrictedLevels: Normalize(rows[0].RestrictedLevels), Version: rows[0].Version,
		PublicAvailable: s.publicAvailable(),
	}
	projects, err := qs.VisibilityPublicProjects(ctx, MaxPublicObjects+1)
	if err != nil {
		return Settings{}, err
	}
	repos, err := qs.VisibilityPublicRepositories(ctx, MaxPublicObjects+1)
	if err != nil {
		return Settings{}, err
	}
	out.PublicObjects = PublicObjects{Projects: []string{}, Repositories: []string{}}
	if len(projects) > MaxPublicObjects {
		projects, out.PublicObjects.Truncated = projects[:MaxPublicObjects], true
	}
	if len(repos) > MaxPublicObjects {
		repos, out.PublicObjects.Truncated = repos[:MaxPublicObjects], true
	}
	out.PublicObjects.Projects = append(out.PublicObjects.Projects, projects...)
	out.PublicObjects.Repositories = append(out.PublicObjects.Repositories, repos...)
	return out, nil
}

// Choices returns the levels the caller may choose (R-API-076); admin says whether the caller is an administrator in the sense of
// Q-AUTHZ-283. It reads PostgreSQL, so a change another replica committed shows at once.
func (s *Service) Choices(ctx context.Context, admin bool) (Choices, error) {
	var out Choices
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := Queries(tx)
		if err != nil {
			return err
		}
		r, err := ReadRules(ctx, qs)
		if err != nil {
			return err
		}
		out = Choices{DefaultLevel: r.Default, RestrictedLevels: r.Restricted, MayChooseRestricted: admin, PublicAvailable: s.publicAvailable()}
		return nil
	})
	return out, err
}

// CheckPatch validates the members a patch names: each a level. The restricted levels come back without duplicates, in the order
// of the levels.
func CheckPatch(p Patch) (Patch, error) {
	var fields []errs.FieldError
	if p.DefaultLevel != nil && !Valid(*p.DefaultLevel) {
		fields = append(fields, InvalidLevel("/defaultLevel"))
	}
	if p.RestrictedLevels != nil {
		for i, level := range *p.RestrictedLevels {
			if !Valid(level) {
				fields = append(fields, InvalidLevel(fmt.Sprintf("/restrictedLevels/%d", i)))
			}
		}
		set := Normalize(*p.RestrictedLevels)
		p.RestrictedLevels = &set
	}
	if len(fields) > 0 {
		return Patch{}, errs.E(errs.Invalid, errs.CodeValidation, "A level is one of private, internal and public.", errs.WithFields(fields...))
	}
	return p, nil
}

// Apply returns the rules after a checked patch, or the 422 of a default that would be restricted (Q-AUTHZ-284), at the member
// that makes it so: the restricted levels when the patch names a set that holds the default, the default level otherwise.
func Apply(before Rules, p Patch) (Rules, error) {
	after := before
	if p.DefaultLevel != nil {
		after.Default = *p.DefaultLevel
	}
	if p.RestrictedLevels != nil {
		after.Restricted = *p.RestrictedLevels
	}
	if !after.IsRestricted(after.Default) {
		return after, nil
	}
	pointer := "/defaultLevel"
	if p.RestrictedLevels != nil {
		pointer = "/restrictedLevels"
	}
	return Rules{}, errs.E(errs.Invalid, errs.CodeValidation, "The default level cannot be restricted.", errs.WithFields(errs.FieldError{
		Pointer: pointer, Code: CodeDefaultRestricted,
		Detail: "The level " + after.Default + " is the default; a restricted level must differ from the default.",
	}))
}

// Update applies a merge patch of the settings (R-API-056, R-API-075) under the version check: expected is the version the
// client saw (errs.AnyRevision is the deliberate blind write). 422 validation_failed for a value that is no level, and with the
// field code default_level_restricted when the default would be one of the restricted levels (Q-AUTHZ-284); 409
// public_not_allowed for a default that becomes public while public is unavailable; 412 for a stale version. It records
// settings.visibility.changed with the default and the restricted levels before and after, in the same transaction; a patch that
// changes nothing records nothing. The change is not journaled and reaches no cache: the values are read in the transaction that
// creates or changes a project or a repository.
func (s *Service) Update(ctx context.Context, c Caller, expected int64, p Patch) (Settings, error) {
	p, err := CheckPatch(p)
	if err != nil {
		return Settings{}, err
	}
	var out Settings
	err = s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := Queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.VisibilityDefaultLock(ctx)
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			return errors.New("visibility: the settings row is missing")
		}
		cur := rows[0]
		if expected != errs.AnyRevision && expected != cur.Version {
			return errs.Stale("The visibility settings", cur.Version)
		}
		before := Rules{Default: cur.DefaultLevel, Restricted: Normalize(cur.RestrictedLevels)}
		after, err := Apply(before, p)
		if err != nil {
			return err
		}
		if after.Default != before.Default || !slices.Equal(after.Restricted, before.Restricted) {
			if after.Default != before.Default && after.Default == Public && !s.publicAvailable() {
				return PublicNotAllowed()
			}
			if _, err := qs.VisibilityDefaultSet(ctx, q.VisibilityDefaultSetParams{
				Level: after.Default, Restricted: after.Restricted, Actor: principal.IDOrEmpty(c.Actor.ID),
			}); err != nil {
				return fmt.Errorf("visibility: set the settings: %w", err)
			}
			if err := s.o.Recorder.RecordTx(ctx, tx, ChangedEvent(c, before, after)); err != nil {
				return err
			}
		}
		out, err = s.read(ctx, qs)
		return err
	})
	return out, err
}

// ChangedEvent is settings.visibility.changed (R-AUTHZ-036, Q-API-332) with the default and the restricted levels before and
// after.
func ChangedEvent(c Caller, before, after Rules) audit.Event {
	type state struct {
		DefaultLevel     string   `json:"defaultLevel"`
		RestrictedLevels []string `json:"restrictedLevels"`
	}
	nonNil := func(set []string) []string {
		if set == nil {
			return []string{}
		}
		return set
	}
	details, err := json.Marshal(map[string]state{
		"before": {DefaultLevel: before.Default, RestrictedLevels: nonNil(before.Restricted)},
		"after":  {DefaultLevel: after.Default, RestrictedLevels: nonNil(after.Restricted)},
	})
	if err != nil {
		panic(err) // strings and slices of strings always marshal
	}
	return audit.Event{
		Type: "settings.visibility.changed", Actor: c.Actor, Source: c.Source, Outcome: audit.Success,
		Subject: audit.Subject{Kind: "settings", Name: "visibility"}, Details: details,
	}
}
