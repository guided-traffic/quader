//go:build integration

package upstream

import (
	"fmt"
	"strings"
	"testing"
)

// TestR_REPO_069_TheProductKeepsTheProjectUpstream holds the refusals of R-REPO-069 and R-AUTHZ-041 over HTTP (409 managed_by_product,
// the 422 of a merge patch at each managed member), the database's guards that hold whatever the API checked (AI010, the admission of
// AI006), and who sees the project (Q-AUTHZ-301, Q-UI-837).
func TestR_REPO_069_TheProductKeepsTheProjectUpstream(t *testing.T) {
	f := newFixture(t)
	f.e.Project("web")
	f.start()
	repo := "/api/v1/projects/upstream/repositories/npmjs"

	t.Run("R-REPO-069 a merge patch changes the narrowing settings and nothing else", func(t *testing.T) {
		cur := f.do(f.admin, "GET", repo, nil)
		wantStatus(t, cur, 200, "")
		ok := f.do(f.admin, "PATCH", repo, map[string]any{"excludePatterns": []string{"@evil/*"}}, "If-Match", cur.Header.Get("ETag"),
			"Content-Type", "application/merge-patch+json")
		wantStatus(t, ok, 200, "")
		if f.events("repository.narrowed", "npmjs") != 1 {
			t.Error("the narrowing is not recorded")
		}
		url := f.do(f.admin, "PATCH", repo, map[string]any{"remote": map[string]any{"url": "https://registry.example.com/"}}, "If-Match", "*",
			"Content-Type", "application/merge-patch+json")
		wantStatus(t, url, 422, "managed_by_product")
		if !hasField(t, url, "/remote/url", "managed_by_product") {
			t.Errorf("%s", url.Body)
		}
		mode := f.do(f.admin, "PATCH", repo, map[string]any{"remote": map[string]any{"mode": "mirror"}}, "If-Match", "*",
			"Content-Type", "application/merge-patch+json")
		if !hasField(t, mode, "/remote/mode", "managed_by_product") {
			t.Errorf("%s", mode.Body)
		}
		back := f.do(f.admin, "GET", repo, nil)
		wantStatus(t, f.do(f.admin, "PUT", repo, back.JSON(t), "If-Match", back.Header.Get("ETag")), 409, "managed_by_product")
	})
	t.Run("R-REPO-069 nothing is created, deleted or moved in the project upstream", func(t *testing.T) {
		wantStatus(t, f.do(f.admin, "DELETE", repo, nil), 409, "managed_by_product")
		wantStatus(t, f.do(f.admin, "POST", repo+"/move", map[string]any{"project": "web"}), 409, "managed_by_product")
		wantStatus(t, f.do(f.admin, "POST", "/api/v1/projects/upstream/repositories",
			map[string]any{"key": "mine", "class": "remote", "format": "npm"}, "Idempotency-Key", "c1"), 409, "managed_by_product")
		if f.id("npmjs") == 0 || f.e.Number("SELECT count(*) FROM repositories r JOIN project p ON p.id = r.project_id WHERE p.key = 'upstream'") != 13 {
			t.Fatal("a refused write changed the project")
		}
		f.e.RepoIn("web", "npm-local", "local", "npm")
		wantStatus(t, f.do(f.admin, "POST", "/api/v1/projects/web/repositories/npm-local/move", map[string]any{"project": "upstream"}), 409, "managed_by_product")
	})
	t.Run("R-AUTHZ-041 the project has no members, policies or accounts, and its name, level and existence are fixed", func(t *testing.T) {
		p := "/api/v1/projects/upstream"
		wantStatus(t, f.do(f.admin, "POST", "/api/v1/projects", map[string]any{"key": "upstream", "displayName": "Mine"}, "Idempotency-Key", "p1"), 409, "already_exists")
		wantStatus(t, f.do(f.admin, "PUT", p+"/members/user:alice", map[string]any{"role": "reader"}), 409, "managed_by_product")
		wantStatus(t, f.do(f.admin, "POST", p+"/policies", map[string]any{
			"name": "readers", "resources": map[string]any{"repositories": []string{"npmjs"}},
			"entries": []any{map[string]any{"principal": "user:alice", "actions": []string{"read"}}},
		}, "Idempotency-Key", "pol1"), 409, "managed_by_product")
		// The service accounts of the project and of its repositories are refused by the identity manager (its unit test): the
		// replicas of this area serve no identity API.
		wantStatus(t, f.do(f.admin, "PUT", p+"/visibility", map[string]any{"level": "private"}, "If-Match", "*"), 409, "managed_by_product")
		wantStatus(t, f.do(f.admin, "PUT", p, map[string]any{"displayName": "Mine"}, "If-Match", "*"), 409, "managed_by_product")
		wantStatus(t, f.do(f.admin, "DELETE", p, nil), 409, "managed_by_product")
	})
	t.Run("R-REPO-066 the database refuses what the API refuses (AI010)", func(t *testing.T) {
		for _, stmt := range []string{
			"DELETE FROM project WHERE key = 'upstream'",
			"UPDATE project SET display_name = 'Mine' WHERE key = 'upstream'",
			fmt.Sprintf("INSERT INTO project_member (project_id, principal_id, role) VALUES ('00000000-0000-0000-0000-000000000100', '%s', 'reader')", f.alice.ID),
			"UPDATE repositories SET key = 'npm-registry' WHERE key = 'npmjs' AND project_id = '00000000-0000-0000-0000-000000000100'",
			"INSERT INTO repositories (key, class, format, config, project_id) VALUES ('stray', 'remote', 'npm', '{}', '00000000-0000-0000-0000-000000000100')",
			`WITH p AS (INSERT INTO principal (id, kind, name) VALUES (gen_random_uuid(), 'service_account', 'upstream/bot') RETURNING id)
			 INSERT INTO service_account (principal_id, project_id) SELECT id, '00000000-0000-0000-0000-000000000100' FROM p`,
		} {
			if err := f.e.TryExec(stmt); err == nil || !strings.Contains(err.Error(), "AI010") {
				t.Errorf("%s: %v", stmt, err)
			}
		}
	})
	t.Run("R-REPO-071 the member tables admit a repository of upstream; a deploy target stays the own project's (AI006)", func(t *testing.T) {
		f.e.RepoIn("web", "npm-all", "virtual", "npm")
		f.e.RepoIn("mobile", "npm-mobile", "local", "npm")
		if err := f.e.TryExec(`INSERT INTO virtual_members (virtual_id, position, member_id) SELECT v.id, 0, $1 FROM repositories v JOIN project p ON p.id = v.project_id
		                       WHERE p.key = 'web' AND v.key = 'npm-all'`, f.id("npmjs")); err != nil {
			t.Fatalf("a member of upstream: %v", err)
		}
		if err := f.e.TryExec(`INSERT INTO virtual_deploy_targets (virtual_id, target_id) SELECT v.id, $1 FROM repositories v JOIN project p ON p.id = v.project_id
		                       WHERE p.key = 'web' AND v.key = 'npm-all'`, f.id("npmjs")); err == nil || !strings.Contains(err.Error(), "AI006") {
			t.Errorf("a deploy target of upstream: %v", err)
		}
		if err := f.e.TryExec(`INSERT INTO virtual_members (virtual_id, position, member_id)
		                       SELECT v.id, 1, m.id FROM repositories v, repositories m WHERE v.key = 'npm-all' AND m.key = 'npm-mobile'`); err == nil ||
			!strings.Contains(err.Error(), "AI006") {
			t.Errorf("a member of another project: %v", err)
		}
	})
	t.Run("R-REPO-071 Q-REPO-379 a Debian virtual repository takes upstream/debian: the preset carries the keys the member check reads", func(t *testing.T) {
		f.e.RepoIn("web", "deb-all", "virtual", "deb")
		r := f.do(f.admin, "PATCH", "/api/v1/projects/web/repositories/deb-all", map[string]any{
			"deb":     map[string]any{"suites": []string{"stable"}, "components": []string{"main"}, "architectures": []string{"amd64"}},
			"virtual": map[string]any{"members": []string{"upstream/debian"}},
		}, "If-Match", "*", "Content-Type", "application/merge-patch+json")
		wantStatus(t, r, 200, "")
		if !strings.Contains(string(r.Body), `"member_inactive"`) || strings.Contains(string(r.Body), "deb_member_unverified") {
			t.Errorf("want the member taken with member_inactive (not switched on), not refused: %s", r.Body)
		}
	})
	t.Run("Q-AUTHZ-301 a caller sees the project through a repository it reads; holders of projects:read see it first (Q-UI-837)", func(t *testing.T) {
		keys := func(person string) string {
			r := f.cl.Do(person, "GET", "/api/v1/projects", nil)
			wantStatus(t, r, 200, "")
			var out []string
			for _, it := range r.Items(t) {
				out = append(out, it["key"].(string))
			}
			return strings.Join(out, ",")
		}
		// The repositories of upstream bob reads in the list of repositories and in the search's suggestions for "npm".
		upstreamRepos := func() string {
			r := f.cl.Do(f.bob.Name, "GET", "/api/v1/repositories?limit=200", nil)
			wantStatus(t, r, 200, "")
			var out []string
			for _, it := range r.Items(t) {
				if it["project"] == "upstream" {
					out = append(out, it["key"].(string))
				}
			}
			s := f.cl.Do(f.bob.Name, "GET", "/api/v1/search/suggestions?q=npm&kind=repository", nil)
			wantStatus(t, s, 200, "")
			repos, _ := s.JSON(t)["repositories"].(map[string]any)
			items, _ := repos["items"].([]any)
			for _, it := range items {
				if m := it.(map[string]any); m["project"] == "upstream" {
					out = append(out, "suggested "+m["key"].(string))
				}
			}
			return strings.Join(out, ",")
		}
		f.e.Project("alpha")
		if got := keys(f.admin.Name); !strings.HasPrefix(got, "upstream,alpha,") {
			t.Errorf("the administrator lists %s", got)
		}
		// Every preset is internal and off (Q-REPO-388): bob, who reads internal, sees neither the project nor a repository of it.
		if got := keys(f.bob.Name); strings.Contains(got, "upstream") {
			t.Errorf("bob lists %s", got)
		}
		if got := upstreamRepos(); got != "" {
			t.Errorf("Q-AUTHZ-306 bob lists the switched-off repositories %s", got)
		}
		wantStatus(t, f.do(f.bob, "GET", "/api/v1/projects/upstream", nil), 404, "")
		// One switch makes it readable for him, and the project with it.
		wantStatus(t, f.do(f.admin, "PUT", items+"/npmjs/state", map[string]any{"state": "on"}), 200, "")
		within(t, func() bool { return strings.Contains(keys(f.bob.Name), "upstream") })
		wantStatus(t, f.do(f.bob, "GET", "/api/v1/projects/upstream", nil), 200, "")
		if got := upstreamRepos(); got != "npmjs,suggested npmjs" {
			t.Errorf("Q-AUTHZ-306 bob lists %q of upstream, want the one that serves, listed and suggested", got)
		}
		vis := f.do(f.admin, "GET", "/api/v1/settings/visibility", nil)
		if objects := fmt.Sprint(vis.JSON(t)["publicObjects"]); strings.Contains(objects, "upstream") {
			t.Errorf("publicObjects %s", objects)
		}
	})
}
