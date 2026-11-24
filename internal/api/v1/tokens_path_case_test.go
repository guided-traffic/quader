package v1

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// pathCaseSource is the decision point's snapshot of two repositories of the project it: one of a format that stores lower-case
// paths, as NuGet's layout does, and one of a format whose paths keep their case.
type pathCaseSource struct{}

func (pathCaseSource) Snapshot(context.Context) (*authz.Snapshot, error) {
	repo := func(id int64, key, format string) q.AuthzBuildRepositoriesRow {
		return q.AuthzBuildRepositoriesRow{
			ID: id, Key: key, Class: "local", Format: format, ProjectID: "p1", ProjectKey: "it",
			VisibilityArtifacts: "private", VisibilityWiki: "private",
		}
	}
	return &authz.Snapshot{Started: time.Now(), Repositories: []q.AuthzBuildRepositoriesRow{
		repo(1, "lower-local", "lowfmt"), repo(2, "cased-local", "casedfmt"),
	}}, nil
}

func (pathCaseSource) Epochs(context.Context) (authz.Label, error) { return authz.Label{}, nil }

// noChanges is a change log that never changes: the test installs the index once with Refresh.
type noChanges struct{}

func (noChanges) AddListener(cluster.Listener)           {}
func (noChanges) AreaState(string) (int64, time.Time)    { return 0, time.Time{} }
func (noChanges) FreshWithin(time.Duration) bool         { return true }
func (noChanges) Notify(meta.Tx, string, int, any) error { return nil }

// TestR_AUTHZ_005_ATokenScopesUpperCasePathPatternOverALowerCaseFormatIsRefused: the token API refuses a scope with 400
// invalid_scope when a path pattern of an element, include or exclude, has an upper-case letter and the element's selectors cover a
// repository of a format that stores lower-case paths (R-AUTH-027); a lower-case pattern, a format that keeps its case and a scope
// without path patterns pass. It runs the API's step over a real decision point whose index holds two repositories, so the wiring
// (the generated scope to its stored form, every element, include and exclude together, the selectors as the scope writes them) is
// held here and not only by a format's integration case.
func TestR_AUTHZ_005_ATokenScopesUpperCasePathPatternOverALowerCaseFormatIsRefused(t *testing.T) {
	reg := catalog.NewRegistry()
	for _, f := range []catalog.Facts{
		{ID: "lowfmt", Classes: []catalog.Class{catalog.Local}, Layout: "lowfmt", PatternSubject: catalog.SubjectPath, LowerCasePaths: true},
		{ID: "casedfmt", Classes: []catalog.Class{catalog.Local}, Layout: "casedfmt", PatternSubject: catalog.SubjectPath},
	} {
		reg.Register(catalog.Entry{Facts: f})
	}
	e, err := authz.New(authz.Options{
		Source: pathCaseSource{}, Changes: noChanges{}, Formats: reg,
		Config: config.Authz{Cache: config.AuthzCache{MaxStaleness: 10 * time.Second}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Authz: AuthzServices{Engine: e}})
	check := func(t *testing.T, scope string) error {
		t.Helper()
		var sc gen.TokenScope
		if err := json.Unmarshal([]byte(scope), &sc); err != nil {
			t.Fatal(err)
		}
		raw, err := scopeBytes(&sc)
		if err != nil {
			t.Fatal(err)
		}
		return s.refuseUpperCasePaths(raw)
	}
	for _, tc := range []struct {
		name, scope string
		refused     bool
	}{
		{"an upper-case exclude over the lower-case repository", `{"repositories":[{"repositories":["it/lower-local"],"paths":{"exclude":["Acme.Secret/**"]},"actions":["read"]}]}`, true},
		{"an upper-case include over the lower-case repository", `{"repositories":[{"repositories":["it/lower-local"],"paths":{"include":["Acme.Open/**"]},"actions":["read"]}]}`, true},
		{"an upper-case pattern in the second element, whose selector covers the repository by its format", `{"repositories":[{"repositories":["it/cased-local"],"actions":["read"]},{"repositories":["@any-local:lowfmt"],"paths":{"exclude":["acme.open/**","Acme.Secret/**"]},"actions":["read"]}]}`, true},
		{"a lower-case exclude over the lower-case repository", `{"repositories":[{"repositories":["it/lower-local"],"paths":{"exclude":["acme.secret/**"]},"actions":["read"]}]}`, false},
		{"an upper-case exclude over a repository whose paths keep their case", `{"repositories":[{"repositories":["it/cased-local"],"paths":{"exclude":["Releases/**"]},"actions":["read"]}]}`, false},
		{"a scope that inherits the owner's rights", `{"inherit":true}`, false},
	} {
		t.Run("R-AUTHZ-005 "+tc.name, func(t *testing.T) {
			err := check(t, tc.scope)
			if !tc.refused {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				return
			}
			if e, ok := errs.As(err); !ok || e.Code != errs.CodeInvalidScope || e.Status() != 400 {
				t.Fatalf("got %v, want 400 %s", err, errs.CodeInvalidScope)
			}
		})
	}
}
