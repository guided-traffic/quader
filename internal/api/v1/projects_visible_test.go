package v1

import (
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/project"
)

// TestR_AUTHZ_035_TheProjectsACallerSees: a holder of projects:read sees every project, a member its own, any caller the projects its
// level admits (internal for every authenticated subject but an external user, public for every request); membership keeps the caller's own (R-AUTHZ-035 rule 8,
// R-API-057).
func TestR_AUTHZ_035_TheProjectsACallerSees(t *testing.T) {
	user := authz.Subject{Kind: "user", ID: "u1", Name: "alice"}
	external := user
	external.External = true
	service := authz.Subject{Kind: "service_account", ID: "s1", Name: "bot"}
	ci := authz.Subject{Kind: "ci", Name: "ci", Mappings: []string{"ci:gitlab/acme"}}
	anonymous := authz.Subject{Anonymous: true}

	priv := project.Project{Key: "shut", Visibility: "private"}
	inner := project.Project{Key: "inner", Visibility: "internal"}
	open := project.Project{Key: "open", Visibility: "public"}
	member := map[string]authz.ProjectRole{"shut": authz.ProjectRoleReader}

	cases := []struct {
		name       string
		sub        authz.Subject
		p          project.Project
		roles      map[string]authz.ProjectRole
		all        bool
		membership bool
		want       bool
	}{
		{"an internal user sees an internal project", user, inner, nil, false, false, true},
		{"an internal user sees a public project", user, open, nil, false, false, true},
		{"an internal user does not see a private project", user, priv, nil, false, false, false},
		{"a member sees its private project", user, priv, member, false, false, true},
		{"a holder of projects:read sees every project", user, priv, nil, true, false, true},
		{"an external user is not admitted by internal", external, inner, nil, false, false, false},
		{"an external user sees a public project", external, open, nil, false, false, true},
		{"a service account is internal and sees an internal project", service, inner, nil, false, false, true},
		{"a CI token is internal and sees an internal project", ci, inner, nil, false, false, true},
		{"a service account does not see a private project", service, priv, nil, false, false, false},
		{"an anonymous caller sees a public project", anonymous, open, nil, false, false, true},
		{"an anonymous caller does not see an internal one", anonymous, inner, nil, false, false, false},
		{"membership keeps the memberships", user, priv, member, false, true, true},
		{"membership drops an internal project", user, inner, nil, false, true, false},
		{"membership drops every project for a holder of projects:read", user, open, nil, true, true, false},
	}
	for _, c := range cases {
		t.Run("R-AUTHZ-035 "+c.name, func(t *testing.T) {
			if got := (projectSight{roles: c.roles, all: c.all}).listed(c.sub, c.p, c.membership); got != c.want {
				t.Errorf("listed = %v, want %v", got, c.want)
			}
		})
	}
}

// TestR_AUTHZ_041_TheProjectUpstreamIsSeenThroughItsRepositories: the built-in project upstream is stored public and its level admits
// nobody; a holder of projects:read sees it, and so does a caller that reads one of its repositories (Q-AUTHZ-301).
func TestR_AUTHZ_041_TheProjectUpstreamIsSeenThroughItsRepositories(t *testing.T) {
	user := authz.Subject{Kind: "user", ID: "u1", Name: "alice"}
	anonymous := authz.Subject{Anonymous: true}
	upstream := project.Project{Key: "upstream", Visibility: "public"}
	for _, c := range []struct {
		name  string
		sub   authz.Subject
		sight projectSight
		want  bool
	}{
		{"an internal user who reads none of its repositories does not see it", user, projectSight{}, false},
		{"an anonymous caller who reads none of its repositories does not see it", anonymous, projectSight{}, false},
		{"a caller who reads one of its repositories sees it", user, projectSight{upstream: true}, true},
		{"an anonymous caller who reads a public one of its repositories sees it", anonymous, projectSight{upstream: true}, true},
		{"a holder of projects:read sees it", user, projectSight{all: true}, true},
	} {
		t.Run("R-AUTHZ-041 "+c.name, func(t *testing.T) {
			if got := c.sight.listed(c.sub, upstream, false); got != c.want {
				t.Errorf("listed = %v, want %v", got, c.want)
			}
		})
	}
	t.Run("R-UI-190 the project upstream stands first, before a project with the key alpha, whatever the sort", func(t *testing.T) {
		list := []project.Project{{Key: "alpha"}, {Key: "upstream"}, {Key: "web"}}
		keysOf := func(ps []project.Project) string {
			var out []string
			for _, p := range ps {
				out = append(out, p.Key)
			}
			return strings.Join(out, ",")
		}
		if got := keysOf(projectOrder(list, false)); got != "upstream,alpha,web" {
			t.Errorf("ascending %s", got)
		}
		if got := keysOf(projectOrder(list, true)); got != "upstream,web,alpha" {
			t.Errorf("descending %s", got)
		}
		for _, c := range []struct {
			key, cursor string
			desc, want  bool
		}{
			{"alpha", "upstream", false, true},
			{"upstream", "alpha", false, false},
			{"web", "alpha", false, true},
			{"alpha", "web", true, true},
			{"web", "upstream", true, true},
			{"alpha", "web", false, false},
		} {
			if got := projectAfter(c.key, c.cursor, c.desc); got != c.want {
				t.Errorf("projectAfter(%s, %s, desc %v) = %v", c.key, c.cursor, c.desc, got)
			}
		}
	})
	t.Run("R-AUTHZ-041 reading one of its repositories shows no other public project", func(t *testing.T) {
		if (projectSight{upstream: true}).listed(user, project.Project{Key: "shut", Visibility: "private"}, false) {
			t.Error("a private project is listed")
		}
	})
}
