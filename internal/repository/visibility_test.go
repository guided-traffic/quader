package repository

import (
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/visibility"
)

// TestR_AUTHZ_037_TheLevelsAWriteStores: the decision of a repository write's levels against the restricted levels of the
// installation (R-AUTHZ-037, R-AUTHZ-038): an Owner is refused a restricted level it names, keeps and lowers one the repository
// has, and an absent member of a create takes the highest level it may choose (Q-AUTHZ-285, Q-AUTHZ-286); an administrator is
// held by none of it. The rules of the container and public come first.
func TestR_AUTHZ_037_TheLevelsAWriteStores(t *testing.T) {
	restricted := func(levels ...string) visibility.Rules {
		return visibility.Rules{Default: visibility.Public, Restricted: levels}
	}
	stored := func(a, w string) *Visibility { return &Visibility{Artifacts: a, Wiki: w} }
	for _, c := range []struct {
		name    string
		lc      levelChoice
		want    string // the stored levels "artifacts/wiki", or the code of the refusal
		pointer string
	}{
		{
			"an Owner names a restricted level at create",
			levelChoice{rules: restricted("public"), projectLevel: "public", named: Visibility{Artifacts: "public"}},
			errs.CodeVisibilityLevelRestricted, "/visibility/artifacts",
		},
		{
			"an administrator names it",
			levelChoice{rules: restricted("public"), admin: true, projectLevel: "public", named: Visibility{Artifacts: "public"}},
			"public/public", "",
		},
		{
			"an absent member below the restricted project level",
			levelChoice{rules: restricted("internal"), projectLevel: "internal"},
			"private/private", "",
		},
		{
			"the administrator's absent member takes the project's level",
			levelChoice{rules: restricted("internal"), admin: true, projectLevel: "internal"},
			"internal/internal", "",
		},
		{
			"an absent wiki follows named artifacts",
			levelChoice{rules: restricted("internal"), projectLevel: "public", named: Visibility{Artifacts: "public"}},
			"public/public", "",
		},
		{
			"no level left to take",
			levelChoice{rules: restricted("private", "internal"), projectLevel: "internal"},
			errs.CodeVisibilityLevelRestricted, "/visibility/artifacts",
		},
		{
			"an update keeps a restricted level",
			levelChoice{rules: restricted("public"), projectLevel: "public", cur: stored("public", "public"), named: Visibility{Artifacts: "public", Wiki: "public"}},
			"public/public", "",
		},
		{
			"an update lowers the wiki under restricted artifacts",
			levelChoice{rules: restricted("public"), projectLevel: "public", cur: stored("public", "public"), named: Visibility{Wiki: "private"}},
			"public/private", "",
		},
		{
			"an update raises the wiki back to the restricted level",
			levelChoice{rules: restricted("public"), projectLevel: "public", cur: stored("public", "private"), named: Visibility{Wiki: "public"}},
			errs.CodeVisibilityLevelRestricted, "/visibility/wiki",
		},
		{
			"lowering to a restricted level is choosing it",
			levelChoice{rules: restricted("internal"), projectLevel: "public", cur: stored("public", "public"), named: Visibility{Artifacts: "internal", Wiki: "internal"}},
			errs.CodeVisibilityLevelRestricted, "/visibility/artifacts",
		},
		{
			"the container rule comes first",
			levelChoice{rules: restricted("public"), projectLevel: "internal", named: Visibility{Artifacts: "public"}},
			"visibility_above_project", "/visibility/artifacts",
		},
		{
			"R-REPO-068 the reconcile creates a preset internal, whatever the installation's default",
			levelChoice{rules: restricted(), projectLevel: "public", productLevel: "internal"},
			"internal/internal", "",
		},
		{
			"R-REPO-066 where internal is restricted the preset is created private",
			levelChoice{rules: restricted("internal"), projectLevel: "public", productLevel: "internal"},
			"private/private", "",
		},
		{
			"R-REPO-066 where nothing at or below internal is open, private, and the reconcile goes on",
			levelChoice{rules: restricted("private", "internal"), projectLevel: "public", productLevel: "internal"},
			"private/private", "",
		},
		{
			"R-REPO-066 scanner-trivy's private is the declaration's, not a choice",
			levelChoice{rules: restricted("private"), projectLevel: "public", productLevel: "private", named: Visibility{Artifacts: "private", Wiki: "private"}},
			"private/private", "",
		},
		{
			"public unavailable comes before the restriction",
			levelChoice{rules: visibility.Rules{Default: visibility.Private, Restricted: []string{"public"}}, projectLevel: "public", named: Visibility{Artifacts: "public"}},
			errs.CodePublicNotAllowed, "",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			lc := c.lc
			if c.want != errs.CodePublicNotAllowed {
				lc.publicOK = true
			}
			o, err := lc.decide()
			switch {
			case err != nil:
				e, ok := errs.As(err)
				if !ok || e.Code != c.want || c.pointer != "" && (len(e.Fields) != 1 || e.Fields[0].Pointer != c.pointer) {
					t.Errorf("refused with %v, want %s at %s", err, c.want, c.pointer)
				}
			case len(o.fields) > 0:
				if o.fields[0].Code != c.want || o.fields[0].Pointer != c.pointer {
					t.Errorf("field errors %+v, want %s", o.fields, c.want)
				}
			default:
				if got := o.out.Artifacts + "/" + o.out.Wiki; got != c.want {
					t.Errorf("stores %s, want %s", got, c.want)
				}
			}
		})
	}
}
