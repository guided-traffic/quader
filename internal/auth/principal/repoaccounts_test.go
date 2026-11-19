package principal

import (
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
)

// The address and the rights of a repository's service account (R-AUTH-046, R-AUTHZ-039).

func TestR_AUTH_046_ARepositoryAccountIsAddressedByProjectRepositoryAndName(t *testing.T) {
	for _, tc := range []struct {
		ref  string
		ok   bool
		want string
	}{
		{"web/npm-local/ci", true, "web/npm-local/ci"},
		{"web/npm.local_x/release-bot", true, "web/npm.local_x/release-bot"},
		{"web/ci", false, ""}, // a project's account
		{"ci", false, ""},     // an instance-wide account
		{"web/npm-local/ci/x", false, ""},
		{"Web/npm-local/ci", false, ""},  // no project key
		{"web/-npm/ci", false, ""},       // no repository key
		{"web/npm-local/-ci", false, ""}, // no account name
		{"web/npm-local/c", false, ""},   // a name of one character
	} {
		a, ok := ParseRepositoryAccount(tc.ref)
		if ok != tc.ok || ok && a.String() != tc.want {
			t.Errorf("ParseRepositoryAccount(%q) = %+v, %v; want %q, %v", tc.ref, a, ok, tc.want, tc.ok)
		}
	}
}

func TestR_AUTHZ_039_TheActionsOfARepositoryAccountAreASetInTheCanonicalOrder(t *testing.T) {
	t.Run("absent is read", func(t *testing.T) {
		got, err := NormalizeActions(nil)
		if err != nil || !slices.Equal(got, []string{"read"}) {
			t.Fatalf("%v %v", got, err)
		}
	})
	t.Run("the order of R-AUTHZ-006, each once", func(t *testing.T) {
		got, err := NormalizeActions([]string{"manage", "deploy", "read", "deploy"})
		if err != nil || !slices.Equal(got, []string{"read", "deploy", "manage"}) {
			t.Fatalf("%v %v", got, err)
		}
	})
	for _, bad := range [][]string{{}, {"fly"}, {"read", "admin"}} {
		_, err := NormalizeActions(bad)
		e, ok := errs.As(err)
		if !ok || e.Kind != errs.Unprocessable || len(e.Fields) != 1 || e.Fields[0].Pointer != "/actions" {
			t.Errorf("%v: %v", bad, err)
		}
	}
}

func TestR_AUTH_046_TheLevelAndTheNamesOfAnAccount(t *testing.T) {
	for _, tc := range []struct {
		sa              ServiceAccount
		level, local, r string
	}{
		{ServiceAccount{Name: "ci-bot"}, LevelInstance, "ci-bot", "sa:ci-bot"},
		{ServiceAccount{Name: "web/partner", Project: "web"}, LevelProject, "partner", "sa:web/partner"},
		{ServiceAccount{Name: "web/npm-local/ci", Project: "web", Repository: "npm-local"}, LevelRepository, "ci", "sa:web/npm-local/ci"},
	} {
		if tc.sa.Level() != tc.level || tc.sa.LocalName() != tc.local || tc.sa.Reference() != tc.r {
			t.Errorf("%+v: %s %s %s", tc.sa, tc.sa.Level(), tc.sa.LocalName(), tc.sa.Reference())
		}
	}
}

func TestR_AUTHZ_031_AReplayedChangeOfTheActionsNarrowsOnly(t *testing.T) {
	var cmp actionSet
	for _, tc := range []struct {
		current, after []string
		want           bool
	}{
		{[]string{"read", "deploy"}, []string{"read"}, true},
		{[]string{"read", "deploy"}, []string{"read", "deploy"}, true},
		{[]string{"read"}, []string{"read", "deploy"}, false},
		{[]string{"read", "deploy"}, []string{"manage"}, false},
	} {
		if got := cmp.Narrows(tc.current, tc.after); got != tc.want {
			t.Errorf("Narrows(%v, %v) = %v", tc.current, tc.after, got)
		}
	}
}
