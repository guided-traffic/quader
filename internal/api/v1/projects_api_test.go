package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/project"
)

// fakeProjects is a project.Service that serves a fixed list; the methods it does
// not define answer through the nil embedded interface and are not reached.
type fakeProjects struct {
	project.Service
	all []project.Project
}

func (f fakeProjects) List(context.Context) ([]project.Project, error) { return f.all, nil }

func (f fakeProjects) Get(_ context.Context, key string) (project.Project, error) {
	for _, p := range f.all {
		if p.Key == key {
			return p, nil
		}
	}
	return project.Project{}, errs.E(errs.NotFound, errs.CodeNotFound, "no such project")
}

// LookupPrincipals answers a user with a display name and an avatar, a group and, when the lookup asks for the instance's
// accounts, an instance-wide service account; every kind asked, in that order.
func (f fakeProjects) LookupPrincipals(_ context.Context, query project.Lookup) ([]project.Found, error) {
	all := []project.Found{
		{Kind: project.KindUser, Name: query.Prefix + "-x", ID: "u-1", DisplayName: "Al X", AvatarSHA256: "ab12"},
		{Kind: project.KindGroup, Name: query.Prefix + "-ops"},
		{Kind: project.KindServiceAccount, Name: query.Project + "/" + query.Prefix + "-bot", Project: query.Project},
	}
	if query.InstanceAccounts {
		all = append(all, project.Found{Kind: project.KindServiceAccount, Name: query.Prefix + "-ci"})
	}
	var out []project.Found
	for _, f := range all {
		if len(query.Kinds) == 0 || slices.Contains(query.Kinds, f.Kind) {
			out = append(out, f)
		}
	}
	return out, nil
}

func sampleProjects() fakeProjects {
	mk := func(key, name string) project.Project {
		return project.Project{
			ID: "id-" + key, Key: key, DisplayName: name, Version: 3, LogicalBytes: int64(len(key)) * 1000,
			Members: []project.Member{{Project: key, Principal: project.PrincipalRef{Kind: project.KindGroup, Name: key + "-leads"}, Role: authz.ProjectRoleOwner}},
		}
	}
	return fakeProjects{all: []project.Project{mk("docs", "Documentation"), mk("web", "Web platform"), mk("web2", "Second web")}}
}

func projectsSetup(t *testing.T, c projectCaller) setup {
	t.Helper()
	return newSetup(t, func(o *Options) {
		o.Access = c
		o.Authz.Projects = sampleProjects()
		o.API.Config = apiConfigForTest()
	}, authed)
}

type projectPage struct {
	Items []struct {
		Key     string   `json:"key"`
		Admins  []string `json:"admins"`
		Version int64    `json:"version"`
	} `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

func decodePage(t *testing.T, body string) projectPage {
	t.Helper()
	var p projectPage
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return p
}

func keysOf(p projectPage) string {
	var ks []string
	for _, it := range p.Items {
		ks = append(ks, it.Key)
	}
	return strings.Join(ks, ",")
}

func TestR_API_047_ListProjectsNarrowsToTheCaller(t *testing.T) {
	auditor := projectCaller{caps: map[string]bool{"projects:read": true}}
	web := projectCaller{members: map[string]authz.ProjectRole{"web": authz.ProjectRoleOwner, "docs": authz.ProjectRoleReader}}

	t.Run("R-API-047 a holder of projects:read sees every project", func(t *testing.T) {
		w := projectsSetup(t, auditor).get("/api/v1/projects")
		if w.Code != 200 || keysOf(decodePage(t, w.Body.String())) != "docs,web,web2" {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	})
	t.Run("R-API-047 a member without the capability sees its own projects only", func(t *testing.T) {
		w := projectsSetup(t, web).get("/api/v1/projects")
		if w.Code != 200 || keysOf(decodePage(t, w.Body.String())) != "docs,web" {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	})
	t.Run("R-API-047 q, sort and the cursor page by key", func(t *testing.T) {
		s := projectsSetup(t, auditor)
		if w := s.get("/api/v1/projects?q=SECOND"); keysOf(decodePage(t, w.Body.String())) != "web2" {
			t.Errorf("q on the display name: %s", w.Body)
		}
		if w := s.get("/api/v1/projects?sort=-key"); keysOf(decodePage(t, w.Body.String())) != "web2,web,docs" {
			t.Errorf("descending: %s", w.Body)
		}
		first := decodePage(t, s.get("/api/v1/projects?limit=2").Body.String())
		if keysOf(first) != "docs,web" || first.NextCursor == nil {
			t.Fatalf("first page: %+v", first)
		}
		second := decodePage(t, s.get("/api/v1/projects?limit=2&cursor="+*first.NextCursor).Body.String())
		if keysOf(second) != "web2" || second.NextCursor != nil {
			t.Errorf("second page: %+v", second)
		}
	})
}

func TestR_AUTHZ_026_GetProjectIsNotFoundForAnotherProject(t *testing.T) {
	web := projectCaller{members: map[string]authz.ProjectRole{"web": authz.ProjectRoleOwner}, owns: map[string]bool{"web": true}}
	s := projectsSetup(t, web)
	t.Run("R-AUTHZ-026 an administrator reads its project with an ETag, and a conditional read still answers the body", func(t *testing.T) {
		w := s.get("/api/v1/projects/web")
		if w.Code != 200 || w.Header().Get("ETag") != `"3"` {
			t.Fatalf("%d %v", w.Code, w.Header())
		}
		w = s.do("GET", "/api/v1/projects/web", "", map[string]string{"If-None-Match": `"3"`})
		if w.Code != http.StatusOK || w.Body.Len() == 0 {
			t.Errorf("If-None-Match: %d, want the body", w.Code)
		}
	})
	t.Run("R-AUTHZ-026 a project the caller does not administer answers 404, existing or not", func(t *testing.T) {
		for _, key := range []string{"docs", "nothing"} {
			if w := s.get("/api/v1/projects/" + key); w.Code != 404 {
				t.Errorf("%s: %d", key, w.Code)
			}
		}
	})
}

func TestR_AUTHZ_026_TheLookupBoundsItsInput(t *testing.T) {
	web := projectCaller{members: map[string]authz.ProjectRole{"web": authz.ProjectRoleOwner}, owns: map[string]bool{"web": true}}
	s := projectsSetup(t, web)
	if w := s.get("/api/v1/projects/web/principals?q=al"); w.Code != 200 || !strings.Contains(w.Body.String(), `"principal":"user:al-x"`) {
		t.Errorf("a valid lookup: %d %s", w.Code, w.Body)
	}
	if w := s.get("/api/v1/projects/web/principals?q=al&kind=bot"); w.Code != 422 {
		t.Errorf("an unknown kind: %d", w.Code)
	}
	if w := s.get("/api/v1/projects/web/principals?q=al&limit=21"); w.Code != 422 {
		t.Errorf("limit above the maximum: %d", w.Code)
	}
	if w := s.get("/api/v1/projects/web/principals?q=" + strings.Repeat("a", 129)); w.Code != 422 {
		t.Errorf("a prefix of 129 characters: %d", w.Code)
	}
	if w := s.get("/api/v1/projects/docs/principals?q=al"); w.Code != 404 {
		t.Errorf("another project: %d", w.Code)
	}
}

// The figure a project is charged is part of its representation and follows the read rules of the project
// itself: the member reads its own project's figure, a holder of projects:read every project's (Q-STOR-155).
func TestR_STOR_049_TheProjectCarriesItsLogicalSize(t *testing.T) {
	type sized struct {
		Key          string `json:"key"`
		LogicalBytes *int64 `json:"logicalBytes"`
	}
	auditor := projectCaller{caps: map[string]bool{"projects:read": true}}
	member := projectCaller{members: map[string]authz.ProjectRole{"web": authz.ProjectRoleReader}}

	t.Run("R-STOR-049 the list carries the figure of every project for projects:read", func(t *testing.T) {
		w := projectsSetup(t, auditor).get("/api/v1/projects")
		var page struct {
			Items []sized `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 3 {
			t.Fatalf("%v %d %s", err, w.Code, w.Body)
		}
		want := map[string]int64{"docs": 4000, "web": 3000, "web2": 4000}
		for _, it := range page.Items {
			if it.LogicalBytes == nil || *it.LogicalBytes != want[it.Key] {
				t.Errorf("%s: logicalBytes %v, want %d", it.Key, it.LogicalBytes, want[it.Key])
			}
		}
	})
	t.Run("R-STOR-049 a member reads the figure of its own project, a zero figure is present, not absent", func(t *testing.T) {
		w := projectsSetup(t, member).get("/api/v1/projects/web")
		var got sized
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.LogicalBytes == nil || *got.LogicalBytes != 3000 {
			t.Fatalf("%v %d %s", err, w.Code, w.Body)
		}
		s := newSetup(t, func(o *Options) {
			o.Access = member
			o.Authz.Projects = fakeProjects{all: []project.Project{{ID: "id-web", Key: "web", DisplayName: "Web", Version: 1}}}
			o.API.Config = apiConfigForTest()
		}, authed)
		if w := s.get("/api/v1/projects/web"); !strings.Contains(w.Body.String(), `"logicalBytes":0`) {
			t.Errorf("an empty project: %s", w.Body)
		}
	})
	t.Run("R-STOR-049 a project the caller is no member of stays hidden with its figure", func(t *testing.T) {
		if w := projectsSetup(t, member).get("/api/v1/projects/docs"); w.Code != 404 {
			t.Errorf("%d %s", w.Code, w.Body)
		}
	})
}

// R-API-077: the lookup answers what a member's row shows: a user with its display name, the ID that seeds its avatar and the
// path of its uploaded avatar, never an e-mail address; a service account with its project. The project's own accounts are found
// by its Owner; the instance-wide ones only by a holder of identities:read. kind narrows, repeated or comma-separated.
func TestR_API_077_TheLookupAnswersWhatARowShows(t *testing.T) {
	owner := projectCaller{members: map[string]authz.ProjectRole{"web": authz.ProjectRoleOwner}, owns: map[string]bool{"web": true}}
	read := func(t *testing.T, c projectCaller, query string) []map[string]any {
		t.Helper()
		w := projectsSetup(t, c).get("/api/v1/projects/web/principals?" + query)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		var page struct{ Items []map[string]any }
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page.Items
	}
	t.Run("R-API-077 a user carries its display name and avatar, a service account its project", func(t *testing.T) {
		items := read(t, owner, "q=al")
		if len(items) != 3 {
			t.Fatalf("%v", items)
		}
		u := items[0]
		if u["principal"] != "user:al-x" || u["displayName"] != "Al X" || u["id"] != "u-1" || u["avatarUrl"] != "/api/v1/avatars/ab12" {
			t.Errorf("user: %v", u)
		}
		for _, key := range []string{"email", "status", "origin", "lastLoginAt"} {
			if _, has := u[key]; has {
				t.Errorf("the user carries %s: %v", key, u)
			}
		}
		if sa := items[2]; sa["principal"] != "sa:web/al-bot" || sa["project"] != "web" {
			t.Errorf("service account: %v", sa)
		}
	})
	t.Run("R-API-077 the instance-wide accounts are found by a holder of identities:read only", func(t *testing.T) {
		for _, it := range read(t, owner, "q=al&kind=service_account") {
			if it["principal"] == "sa:al-ci" {
				t.Errorf("the Owner found an instance-wide account: %v", it)
			}
		}
		admin := projectCaller{caps: map[string]bool{"identities:read": true}}
		items := read(t, admin, "q=al&kind=service_account")
		if len(items) != 2 || items[1]["principal"] != "sa:al-ci" {
			t.Errorf("identities:read: %v", items)
		}
	})
	t.Run("R-API-077 kind narrows, comma-separated", func(t *testing.T) {
		items := read(t, owner, "q=al&kind=user,group")
		if len(items) != 2 || items[0]["kind"] != "user" || items[1]["kind"] != "group" {
			t.Errorf("%v", items)
		}
	})
}
