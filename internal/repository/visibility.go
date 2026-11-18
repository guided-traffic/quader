package repository

import (
	"context"
	"encoding/json"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/visibility"
)

// The levels of a repository (R-AUTHZ-033, R-REPO-004). They are columns of the repository row; the document carries them as the
// member `visibility`, and a write names them through it. This file decides what a create or an update stores.

// Field codes of the level rules, in errors[] of a 422 validation_failed answer (R-AUTHZ-033 rule 2).
const (
	codeVisibilityAboveProject       = "visibility_above_project"
	codeVisibilityWikiAboveArtifacts = "visibility_wiki_above_artifacts"
	// WarningPublicPopulatesRemote is the warning of a write that opens a remote repository with cacheFillAction read to the
	// public: a request without a credential then fills the cache (R-AUTHZ-008).
	WarningPublicPopulatesRemote = "public_populates_remote"
)

// levelChoice is what a write of a repository's levels is decided on: the installation's rules, whether the caller is an
// administrator in the sense of Q-AUTHZ-283, the project's level, the stored levels of an update (nil for a create) and the
// levels the client named, where an empty one is absent.
type levelChoice struct {
	rules        visibility.Rules
	admin        bool
	projectLevel string
	cur          *Visibility
	named        Visibility
	publicOK     bool
}

// levelOutcome is the decision: the levels the write stores, the levels it is measured against (the stored ones, or on a create
// what absent members take without a restriction), and the field errors of the container rules.
type levelOutcome struct {
	out, baseline Visibility
	fields        []errs.FieldError
}

// decide applies the rules of R-AUTHZ-033 and R-AUTHZ-037 in the order of R-AUTHZ-038: a value that is no level and the rules of
// the container are field errors (422); a level that becomes public while public is unavailable is 409 public_not_allowed; a
// restricted level that the caller chooses and may not is 403 visibility_level_restricted.
//
// For a create an absent artifacts level is the lower of the installation's default and the project's level, and an absent wiki
// level the lower of that and the artifacts level; each is lowered further to the highest level the caller may choose
// (Q-AUTHZ-286), and refused when there is none. For an update an absent level stays as stored, so a PUT without the member
// changes no level, and a named level equal to the stored one is no choice (Q-AUTHZ-285).
func (lc levelChoice) decide() (levelOutcome, error) {
	baseline := Visibility{Artifacts: visibility.Lower(lc.rules.Default, lc.projectLevel)}
	baseline.Wiki = baseline.Artifacts
	if lc.cur != nil {
		baseline = *lc.cur
	}
	got := lc.named
	o := levelOutcome{out: baseline, baseline: baseline}
	if got.Artifacts != "" {
		o.out.Artifacts = got.Artifacts
	}
	switch {
	case got.Wiki != "":
		o.out.Wiki = got.Wiki
	case lc.cur == nil:
		o.out.Wiki = visibility.Lower(baseline.Wiki, o.out.Artifacts)
	}
	if !visibility.Valid(o.out.Artifacts) {
		o.fields = append(o.fields, visibility.InvalidLevel("/visibility/artifacts"))
		return o, nil
	}
	if !visibility.Valid(o.out.Wiki) {
		o.fields = append(o.fields, visibility.InvalidLevel("/visibility/wiki"))
		return o, nil
	}
	if lc.cur == nil {
		if err := lc.fallBack(&o, got); err != nil {
			return o, err
		}
	}
	if visibility.Above(o.out.Artifacts, lc.projectLevel) {
		o.fields = append(o.fields, errs.FieldError{
			Pointer: "/visibility/artifacts", Code: codeVisibilityAboveProject,
			Detail: "The artifacts level is at most the project's level, " + lc.projectLevel + ".",
		})
	}
	if visibility.Above(o.out.Wiki, o.out.Artifacts) {
		o.fields = append(o.fields, errs.FieldError{
			Pointer: "/visibility/wiki", Code: codeVisibilityWikiAboveArtifacts,
			Detail: "The wiki level is at most the artifacts level, " + o.out.Artifacts + ".",
		})
	}
	if len(o.fields) > 0 {
		return o, nil
	}
	opensPublic := o.out.Artifacts == visibility.Public && baseline.Artifacts != visibility.Public ||
		o.out.Wiki == visibility.Public && baseline.Wiki != visibility.Public
	if opensPublic && !lc.publicOK {
		return o, visibility.PublicNotAllowed()
	}
	if err := lc.chosen(got.Artifacts, "/visibility/artifacts"); err != nil {
		return o, err
	}
	return o, lc.chosen(got.Wiki, "/visibility/wiki")
}

// fallBack lowers the absent members of a create to the highest level the caller may choose (Q-AUTHZ-286). A create whose absent
// member has no such level at or below the one it would take is refused with that level.
func (lc levelChoice) fallBack(o *levelOutcome, got Visibility) error {
	if got.Artifacts == "" {
		level := lc.rules.Highest(o.out.Artifacts, lc.admin)
		if level == "" {
			return visibility.LevelRestricted(o.out.Artifacts, "/visibility/artifacts")
		}
		o.out.Artifacts = level
		if got.Wiki == "" {
			o.out.Wiki = visibility.Lower(o.out.Wiki, level)
		}
	}
	if got.Wiki == "" {
		level := lc.rules.Highest(o.out.Wiki, lc.admin)
		if level == "" {
			return visibility.LevelRestricted(o.out.Wiki, "/visibility/wiki")
		}
		o.out.Wiki = level
	}
	return nil
}

// chosen refuses a level the client named when it is restricted and the caller may not choose it. On an update the level the
// part already has is no choice: it may be kept (Q-AUTHZ-285).
func (lc levelChoice) chosen(level, pointer string) error {
	if level == "" || lc.rules.Allows(level, lc.admin) {
		return nil
	}
	if lc.cur != nil {
		stored := lc.cur.Artifacts
		if pointer == "/visibility/wiki" {
			stored = lc.cur.Wiki
		}
		if level == stored {
			return nil
		}
	}
	return visibility.LevelRestricted(level, pointer)
}

// resolveLevels returns the levels a write stores and checks them against the rules of R-AUTHZ-033 and R-AUTHZ-037 (decide). cur
// is the stored levels of an update and nil for a create; d holds the levels the client named. Violations of the container rules
// are added to v as field errors. A level that rises, relative to the stored one or, on a create, to what an absent member would
// store, asks the caller's RaiseGate (recent authentication) after every other rule passed.
func (s *Service) resolveLevels(ctx context.Context, qs *q.Queries, v *validation, c Caller, projectLevel string, cur *Visibility, d *Document) (Visibility, error) {
	rules, err := visibility.ReadRules(ctx, qs)
	if err != nil {
		return Visibility{}, err
	}
	lc := levelChoice{rules: rules, admin: c.MayChooseRestricted, projectLevel: projectLevel, cur: cur, named: d.Visibility, publicOK: s.publicAvailable()}
	o, err := lc.decide()
	// A document with field errors answers its 422 first, with the container rules beside them.
	if len(v.fields) > 0 {
		v.fields = append(v.fields, o.fields...)
		return o.out, nil
	}
	if err != nil {
		return Visibility{}, err
	}
	if len(o.fields) > 0 {
		v.fields = append(v.fields, o.fields...)
		return o.out, nil
	}
	rises := visibility.Above(o.out.Artifacts, o.baseline.Artifacts) || visibility.Above(o.out.Wiki, o.baseline.Wiki)
	if rises && c.RaiseGate != nil {
		if err := c.RaiseGate(ctx); err != nil {
			return Visibility{}, err
		}
	}
	if o.out.Artifacts == visibility.Public && o.baseline.Artifacts != visibility.Public && fillsOnRead(d) {
		v.warnings = append(v.warnings, Warning{
			Code: WarningPublicPopulatesRemote, Pointer: "/visibility/artifacts",
			Detail: "A request without a credential fills the cache of this repository: its cacheFillAction is read. Set it to deploy to let only a deploying caller fill it.",
		})
	}
	return o.out, nil
}

// fillsOnRead reports whether the document is a remote repository whose reads fill the cache (cacheFillAction read, the
// default of R-AUTHZ-008).
func fillsOnRead(d *Document) bool {
	if d.Class != Remote {
		return false
	}
	var r struct {
		CacheFillAction string `json:"cacheFillAction"`
	}
	if len(d.Remote) > 0 && json.Unmarshal(d.Remote, &r) != nil {
		return false
	}
	return r.CacheFillAction == "" || r.CacheFillAction == FillRead
}

func (s *Service) publicAvailable() bool { return s.publicOK != nil && s.publicOK() }
