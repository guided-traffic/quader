//go:build integration

package projects

import (
	"fmt"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/authz"
	_ "github.com/guided-traffic/quader/internal/formats/all"
	"github.com/guided-traffic/quader/test/integration/stack"
)

const settingsPath = "/api/v1/settings/visibility"

// level is the stored level of a project.
func (f *fixture) level(project string) string {
	return f.e.Text("SELECT visibility FROM project WHERE key = $1", project)
}

// levels are the stored artifacts and wiki levels of a repository, "artifacts/wiki".
func (f *fixture) levels(project, key string) string {
	return f.e.Text(`SELECT r.visibility_artifacts || '/' || r.visibility_wiki FROM repositories r JOIN project p ON p.id = r.project_id
	                 WHERE p.key = $1 AND r.key = $2`, project, key)
}

// repo creates a local npm repository as the admin with the levels in the document ("" members are left out).
func (f *fixture) repo(project, key, artifacts, wiki string) stack.Response {
	f.t.Helper()
	doc := map[string]any{"key": key, "class": "local", "format": "npm"}
	v := map[string]any{}
	if artifacts != "" {
		v["artifacts"] = artifacts
	}
	if wiki != "" {
		v["wiki"] = wiki
	}
	if len(v) > 0 {
		doc["visibility"] = v
	}
	return f.do(f.admin, "POST", base+"/"+project+"/repositories", doc)
}

// TestR_AUTHZ_033_TheLevelOfAProject: the project's level over HTTP (R-API-057): set by an administrator with the version of
// the project, lowering takes the repositories above it down in the same transaction, raising raises nothing, and the change
// is audited, journaled and reaches both change areas (R-AUTHZ-036).
func TestR_AUTHZ_033_TheLevelOfAProject(t *testing.T) {
	f := newFixture(t)
	created := f.create("web", map[string]any{"visibility": "public"})
	if got := created.JSON(t)["visibility"]; got != "public" {
		t.Fatalf("a created project reads back %v, want public", got)
	}
	wantStatus(t, f.repo("web", "npm-open", "public", "internal"), 201, "")
	wantStatus(t, f.repo("web", "npm-team", "internal", ""), 201, "")
	wantStatus(t, f.repo("web", "npm-closed", "private", ""), 201, "")
	if got := f.levels("web", "npm-open"); got != "public/internal" {
		t.Fatalf("npm-open stores %s", got)
	}
	if got := f.levels("web", "npm-team"); got != "internal/private" {
		t.Fatalf("npm-team stores %s: an absent wiki takes the lower of the baseline (the default private) and the artifacts level", got)
	}
	etag := func() string {
		r := f.do(f.admin, "GET", base+"/web", nil)
		wantStatus(t, r, 200, "")
		return r.Header.Get("ETag")
	}
	put := func(who stack.Person, key, level string, hdr ...string) stack.Response {
		return f.do(who, "PUT", base+"/"+key+"/visibility", map[string]any{"level": level}, hdr...)
	}

	t.Run("R-API-057 lowering to internal lowers the public repository, with its levels before, and nothing else", func(t *testing.T) {
		versions := f.configVersion(f.repoID("web", "npm-open"))
		team := f.configVersion(f.repoID("web", "npm-team"))
		epoch, repoEpoch := f.epoch(), f.repoEpoch()
		r := put(f.admin, "web", "internal", "If-Match", etag())
		wantStatus(t, r, 200, "")
		body := r.JSON(t)
		if body["project"].(map[string]any)["visibility"] != "internal" {
			t.Errorf("the answer shows %v", body["project"])
		}
		if fmt.Sprint(body["lowered"]) != "[map[before:map[artifacts:public wiki:internal] repository:web/npm-open]]" {
			t.Errorf("lowered = %v", body["lowered"])
		}
		if f.level("web") != "internal" || f.levels("web", "npm-open") != "internal/internal" {
			t.Errorf("stored: project %s, npm-open %s", f.level("web"), f.levels("web", "npm-open"))
		}
		if f.configVersion(f.repoID("web", "npm-open")) != versions+1 || f.configVersion(f.repoID("web", "npm-team")) != team {
			t.Error("only the lowered repository takes a new configuration version")
		}
		if r.Epoch() != epoch+1 || f.epoch() != r.Epoch() {
			t.Errorf("Quader-Authz-Epoch %d, database %d, before %d: one write advances the epoch once (R-AUTHZ-028)", r.Epoch(), f.epoch(), epoch)
		}
		if f.repoEpoch() <= repoEpoch {
			t.Error("the lowered repository did not notify repo_config")
		}
		if f.events("authz.project.visibility_changed") != 1 || f.detail("authz.project.visibility_changed", "before") != "public" ||
			f.detail("authz.project.visibility_changed", "after") != "internal" {
			t.Error("authz.project.visibility_changed is not recorded with the levels before and after")
		}
		if f.events("authz.settings.changed") != 1 || f.detail("authz.settings.changed", "cause") != "project" {
			t.Error("the lowered repository has no authz.settings.changed with the cause project")
		}
		if f.journal("authz.project.visibility_changed") != 1 || f.journal("authz.settings.changed") != 1 {
			t.Error("the change is not in the revocation journal")
		}
		if fmt.Sprint(f.do(f.admin, "GET", base+"/web", nil).JSON(t)["version"]) != fmt.Sprint(created.JSON(t)["version"]) {
			t.Error("the level moved the version of the project: it moves with displayName and description only")
		}
	})
	t.Run("R-API-057 the level the project has changes and records nothing", func(t *testing.T) {
		events, epoch := f.events("authz.project.visibility_changed"), f.epoch()
		r := put(f.admin, "web", "internal", "If-Match", "*")
		wantStatus(t, r, 200, "")
		if fmt.Sprint(r.JSON(t)["lowered"]) != "[]" || f.events("authz.project.visibility_changed") != events || f.epoch() != epoch {
			t.Errorf("a no-op PUT wrote: %s", r.Body)
		}
	})
	t.Run("R-API-057 raising a project raises none of its repositories", func(t *testing.T) {
		wantStatus(t, put(f.admin, "web", "public", "If-Match", "*"), 200, "")
		if f.level("web") != "public" || f.levels("web", "npm-open") != "internal/internal" {
			t.Errorf("project %s, npm-open %s", f.level("web"), f.levels("web", "npm-open"))
		}
	})
	t.Run("R-API-057 a stale If-Match is 412 and a missing one 428", func(t *testing.T) {
		wantStatus(t, put(f.admin, "web", "private", "If-Match", `"999"`), 412, "precondition_failed")
		wantStatus(t, f.do(f.admin, "PUT", base+"/web/visibility", map[string]any{"level": "private"}), 428, "")
		if f.level("web") != "public" {
			t.Error("a refused PUT changed the level")
		}
	})
	t.Run("R-API-057 a level that is none is 422 and an unknown project 404", func(t *testing.T) {
		wantStatus(t, put(f.admin, "web", "secret", "If-Match", "*"), 422, "")
		wantStatus(t, put(f.admin, "nothing", "private", "If-Match", "*"), 404, "")
	})
	t.Run("R-API-057 an Owner without projects:write is 403, and the level is the administrator's decision", func(t *testing.T) {
		wantStatus(t, f.member("web", "user:alice", "owner"), 201, "")
		wantStatus(t, put(f.alice, "web", "private", "If-Match", "*"), 403, "")
		if f.level("web") != "public" {
			t.Error("an Owner changed the project's level")
		}
	})
	t.Run("R-AUTHZ-033 the database refuses a repository above its project at commit (AI008)", func(t *testing.T) {
		if err := f.e.TryExec("UPDATE project SET visibility = 'private' WHERE key = 'web'"); err == nil {
			t.Error("a project was lowered below its repositories without the cascade")
		}
	})
}

// TestR_AUTHZ_033_TheLevelsOfARepository: the member visibility of the repository document (R-REPO-004): the defaults of a
// create, the container rules as field errors, a merge patch of one part, the move into a project of a lower level.
func TestR_AUTHZ_033_TheLevelsOfARepository(t *testing.T) {
	f := newFixture(t)
	f.create("web", map[string]any{"visibility": "internal"})
	f.create("mobile", map[string]any{"visibility": "private"})

	t.Run("R-AUTHZ-033 an absent member takes the lower of the installation's default and the project's level", func(t *testing.T) {
		wantStatus(t, f.repo("web", "npm-a", "", ""), 201, "")
		if got := f.levels("web", "npm-a"); got != "private/private" {
			t.Errorf("default private: %s", got)
		}
		wantStatus(t, f.do(f.admin, "PATCH", settingsPath, map[string]any{"defaultLevel": "public"}, "If-Match", "*", "Content-Type", "application/merge-patch+json"), 200, "")
		wantStatus(t, f.repo("web", "npm-b", "", ""), 201, "")
		if got := f.levels("web", "npm-b"); got != "internal/internal" {
			t.Errorf("default public in an internal project: %s", got)
		}
		if got := f.levels("web", "npm-a"); got != "private/private" {
			t.Errorf("a repository that existed changed with the default: %s", got)
		}
	})
	t.Run("R-AUTHZ-033 the artifacts level above the project and the wiki above the artifacts are 422 at their members", func(t *testing.T) {
		wantStatus(t, f.repo("mobile", "npm-c", "internal", ""), 422, "visibility_above_project")
		wantStatus(t, f.repo("web", "npm-d", "internal", ""), 201, "")
		wantStatus(t, f.repo("web", "npm-e", "private", "internal"), 422, "visibility_wiki_above_artifacts")
		wantStatus(t, f.repo("web", "npm-e", "secret", ""), 422, "")
	})
	t.Run("R-API-057 a merge patch of one part leaves the other", func(t *testing.T) {
		wantStatus(t, f.repo("web", "npm-f", "internal", "private"), 201, "")
		r := f.do(f.admin, "GET", base+"/web/repositories/npm-f", nil)
		wantStatus(t, r, 200, "")
		version := r.Header.Get("ETag")
		upd := f.do(f.admin, "PATCH", base+"/web/repositories/npm-f", map[string]any{"visibility": map[string]any{"wiki": "internal"}}, "If-Match", version, "Content-Type", "application/merge-patch+json")
		wantStatus(t, upd, 200, "")
		if got := f.levels("web", "npm-f"); got != "internal/internal" {
			t.Errorf("stored %s", got)
		}
		if f.events("authz.settings.changed") == 0 || f.detail("authz.settings.changed", "cause") != "request" {
			t.Error("the repository update did not record authz.settings.changed with the cause request")
		}
		// the artifacts level lowered below the wiki is 422 until the wiki goes down with it
		r = f.do(f.admin, "GET", base+"/web/repositories/npm-f", nil)
		bad := f.do(f.admin, "PATCH", base+"/web/repositories/npm-f", map[string]any{"visibility": map[string]any{"artifacts": "private"}}, "If-Match", r.Header.Get("ETag"), "Content-Type", "application/merge-patch+json")
		wantStatus(t, bad, 422, "visibility_wiki_above_artifacts")
		ok := f.do(f.admin, "PATCH", base+"/web/repositories/npm-f", map[string]any{"visibility": map[string]any{"artifacts": "private", "wiki": "private"}}, "If-Match", r.Header.Get("ETag"), "Content-Type", "application/merge-patch+json")
		wantStatus(t, ok, 200, "")
		if got := f.levels("web", "npm-f"); got != "private/private" {
			t.Errorf("stored %s", got)
		}
	})
	t.Run("R-API-057 a PUT without the member keeps the stored levels", func(t *testing.T) {
		r := f.do(f.admin, "GET", base+"/web/repositories/npm-d", nil)
		doc := r.JSON(t)
		delete(doc, "visibility")
		put := f.do(f.admin, "PUT", base+"/web/repositories/npm-d", doc, "If-Match", r.Header.Get("ETag"))
		wantStatus(t, put, 200, "")
		if got := f.levels("web", "npm-d"); got != "internal/internal" {
			t.Errorf("stored %s", got)
		}
	})
	t.Run("R-AUTHZ-033 a move into a project of a lower level lowers the repository, cause move", func(t *testing.T) {
		wantStatus(t, f.repo("web", "npm-g", "internal", "internal"), 201, "")
		before := f.events("authz.settings.changed")
		r := f.do(f.admin, "POST", base+"/web/repositories/npm-g/move", map[string]any{"project": "mobile"})
		wantStatus(t, r, 200, "")
		if got := f.levels("mobile", "npm-g"); got != "private/private" {
			t.Errorf("stored %s", got)
		}
		if f.events("authz.settings.changed") != before+1 || f.detail("authz.settings.changed", "cause") != "move" {
			t.Error("the move did not record authz.settings.changed with the cause move")
		}
	})
}

// TestR_API_056_TheInstallationsVisibilitySettings: the default level over HTTP (R-API-056).
func TestR_API_056_TheInstallationsVisibilitySettings(t *testing.T) {
	f := newFixture(t)
	patch := func(who stack.Person, body map[string]any, hdr ...string) stack.Response {
		return f.do(who, "PATCH", settingsPath, body, append(hdr, "Content-Type", "application/merge-patch+json")...)
	}
	get := f.do(f.admin, "GET", settingsPath, nil)
	wantStatus(t, get, 200, "")
	body := get.JSON(t)
	if body["defaultLevel"] != "private" || body["publicAvailable"] != true {
		t.Fatalf("a fresh installation: %s", get.Body)
	}
	etag := get.Header.Get("ETag")
	t.Run("R-API-056 a PATCH with the tag of the GET changes the default, and a project created afterwards takes it", func(t *testing.T) {
		f.create("old")
		r := patch(f.admin, map[string]any{"defaultLevel": "internal"}, "If-Match", etag)
		wantStatus(t, r, 200, "")
		if r.JSON(t)["defaultLevel"] != "internal" || r.Header.Get("ETag") == etag {
			t.Errorf("the answer: %s", r.Body)
		}
		f.create("new")
		if f.level("old") != "private" || f.level("new") != "internal" {
			t.Errorf("old %s, new %s", f.level("old"), f.level("new"))
		}
		if f.events("settings.visibility.changed") != 1 || f.journal("settings.visibility.changed") != 0 {
			t.Error("the change is recorded once and not journaled")
		}
	})
	t.Run("R-API-056 the stale tag is 412 and the read-only members of a GET body are ignored", func(t *testing.T) {
		wantStatus(t, patch(f.admin, map[string]any{"defaultLevel": "private"}, "If-Match", etag), 412, "precondition_failed")
		cur := f.do(f.admin, "GET", settingsPath, nil)
		sent := cur.JSON(t)
		sent["defaultLevel"] = "private"
		wantStatus(t, patch(f.admin, sent, "If-Match", cur.Header.Get("ETag")), 200, "")
	})
	t.Run("R-API-056 publicObjects lists the public projects and repositories", func(t *testing.T) {
		wantStatus(t, f.do(f.admin, "PUT", base+"/new/visibility", map[string]any{"level": "public"}, "If-Match", "*"), 200, "")
		wantStatus(t, f.repo("new", "npm-pub", "public", "public"), 201, "")
		r := f.do(f.admin, "GET", settingsPath, nil)
		objects := r.JSON(t)["publicObjects"].(map[string]any)
		if fmt.Sprint(objects) != "map[projects:[new] repositories:[new/npm-pub] truncated:false]" {
			t.Errorf("publicObjects = %v", objects)
		}
	})
	t.Run("R-API-056 the auditor reads the settings and gets 403 on the PATCH", func(t *testing.T) {
		wantStatus(t, f.do(f.auditor, "GET", settingsPath, nil), 200, "")
		wantStatus(t, patch(f.auditor, map[string]any{"defaultLevel": "internal"}, "If-Match", "*"), 403, "")
	})
	t.Run("R-API-056 a user without the permission gets 403", func(t *testing.T) {
		wantStatus(t, f.do(f.bob, "GET", settingsPath, nil), 403, "")
	})
}

// TestR_AUTHZ_033_PublicNeedsAnAnonymousDoor: while the default endpoint admits no request without a credential, every write
// that sets a level to public is 409 public_not_allowed (R-AUTHZ-033 rule 5), and GET reports publicAvailable false.
func TestR_AUTHZ_033_PublicNeedsAnAnonymousDoor(t *testing.T) {
	f := newFixtureWith(t, stack.Options{NoPublic: true})
	f.create("web")
	wantStatus(t, f.do(f.admin, "POST", base, map[string]any{"key": "pub", "displayName": "P", "visibility": "public"}), 409, "public_not_allowed")
	wantStatus(t, f.do(f.admin, "PUT", base+"/web/visibility", map[string]any{"level": "public"}, "If-Match", "*"), 409, "public_not_allowed")
	wantStatus(t, f.do(f.admin, "PATCH", settingsPath, map[string]any{"defaultLevel": "public"}, "If-Match", "*", "Content-Type", "application/merge-patch+json"), 409, "public_not_allowed")
	if got := f.do(f.admin, "GET", settingsPath, nil).JSON(t)["publicAvailable"]; got != false {
		t.Errorf("publicAvailable = %v", got)
	}
	// A project that was public before the door closed keeps its level; a repository in it still cannot be opened to public.
	f.e.Exec("UPDATE project SET visibility = 'public' WHERE key = 'web'")
	wantStatus(t, f.repo("web", "npm-x", "public", ""), 409, "public_not_allowed")
	wantStatus(t, f.repo("web", "npm-x", "internal", ""), 201, "")
	if f.e.Number("SELECT count(*) FROM project WHERE key = 'pub'") != 0 {
		t.Error("a refused create left a project")
	}
}

// TestR_AUTHZ_035_TheProjectsAnInternalUserSees: the project list and the project read decide by the level (R-AUTHZ-035 rule 8).
func TestR_AUTHZ_035_TheProjectsAnInternalUserSees(t *testing.T) {
	f := newFixture(t)
	f.create("shut")
	f.create("inner", map[string]any{"visibility": "internal"})
	f.create("open", map[string]any{"visibility": "public"})
	member := f.member("shut", "user:bob", "reader")
	wantStatus(t, member, 201, "")
	min := authz.HeaderMinEpoch
	epoch := fmt.Sprint(member.Epoch())
	keys := func(r stack.Response) string {
		var out []string
		for _, it := range r.Items(t) {
			out = append(out, fmt.Sprint(it["key"], ":", it["role"]))
		}
		return fmt.Sprint(out)
	}
	t.Run("R-AUTHZ-035 an internal user lists its memberships with a role and the internal and public projects without", func(t *testing.T) {
		if got := keys(f.do(f.bob, "GET", base, nil, min, epoch)); got != "[inner:<nil> open:<nil> shut:reader]" {
			t.Errorf("bob sees %s", got)
		}
		if got := keys(f.do(f.bob, "GET", base+"?membership=true", nil, min, epoch)); got != "[shut:reader]" {
			t.Errorf("bob's memberships %s", got)
		}
	})
	t.Run("R-AUTHZ-035 an anonymous caller lists the public projects", func(t *testing.T) {
		r := f.cl.Do("", "GET", base, nil)
		wantStatus(t, r, 200, "")
		if got := keys(r); got != "[open:<nil>]" {
			t.Errorf("anonymous sees %s", got)
		}
		wantStatus(t, f.cl.Do("", "GET", base+"/inner", nil), 404, "")
		wantStatus(t, f.cl.Do("", "GET", base+"/open", nil), 200, "")
	})
	t.Run("R-AUTHZ-035 the auditor lists every project, and the members of a visible project stay hidden", func(t *testing.T) {
		if got := keys(f.do(f.auditor, "GET", base, nil)); got != "[upstream:<nil> inner:<nil> open:<nil> shut:<nil>]" { // R-AUTHZ-041, first in the list (Q-UI-837)
			t.Errorf("auditor sees %s", got)
		}
		wantStatus(t, f.do(f.bob, "GET", base+"/inner", nil), 200, "")
		wantStatus(t, f.do(f.bob, "GET", base+"/inner/members", nil), 404, "")
		wantStatus(t, f.do(f.bob, "GET", base+"/shut", nil, min, epoch), 200, "")
	})
	t.Run("R-AUTHZ-035 an external user is not admitted by internal", func(t *testing.T) {
		f.e.Exec("UPDATE app_user SET external = true WHERE principal_id = $1::uuid", f.bob.ID)
		wantStatus(t, f.do(f.bob, "GET", base+"/inner", nil), 404, "")
		wantStatus(t, f.do(f.bob, "GET", base+"/open", nil), 200, "")
	})
}

// TestR_AUTHZ_037_RestrictedLevels: the restricted levels of the installation over HTTP (R-AUTHZ-037, R-API-075, R-API-076): an
// Owner of a project is refused a restricted level for a repository and an administrator is not; a repository that has the level
// keeps it, and its Owner lowers it but cannot choose it again; an absent level of an Owner's create takes the highest level it
// may choose, and none is refused; the default is never restricted; the change is audited with both sets.
func TestR_AUTHZ_037_RestrictedLevels(t *testing.T) {
	f := newFixture(t)
	f.create("web", map[string]any{"visibility": "public"})
	wantStatus(t, f.member("web", "user:alice", "owner"), 201, "")
	// The role reaches both replicas through the change log: wait until each one answers the Owner.
	deadline := time.Now().Add(15 * time.Second)
	for streak := 0; streak < 6; {
		if got := f.do(f.alice, "GET", base+"/web", nil); got.Status == 200 && got.JSON(t)["role"] == "owner" {
			streak++
		} else if streak = 0; time.Now().After(deadline) {
			t.Fatal("the Owner is not known to both replicas")
		}
	}
	patchSettings := func(body map[string]any) stack.Response {
		return f.do(f.admin, "PATCH", settingsPath, body, "If-Match", "*", "Content-Type", "application/merge-patch+json")
	}
	repoPatch := func(who stack.Person, key string, v map[string]any) stack.Response {
		return f.do(who, "PATCH", base+"/web/repositories/"+key, map[string]any{"visibility": v}, "If-Match", "*", "Content-Type", "application/merge-patch+json")
	}
	create := func(who stack.Person, key, artifacts string) stack.Response {
		doc := map[string]any{"key": key, "class": "local", "format": "npm"}
		if artifacts != "" {
			doc["visibility"] = map[string]any{"artifacts": artifacts}
		}
		return f.do(who, "POST", base+"/web/repositories", doc)
	}
	wantStatus(t, f.repo("web", "npm-open", "public", "public"), 201, "")

	t.Run("R-API-075 the restricted levels are set beside the default, audited with both sets", func(t *testing.T) {
		r := patchSettings(map[string]any{"restrictedLevels": []string{"public", "public"}})
		wantStatus(t, r, 200, "")
		if got := fmt.Sprint(r.JSON(t)["restrictedLevels"]); got != "[public]" {
			t.Errorf("restrictedLevels = %s", got)
		}
		if f.events("settings.visibility.changed") != 1 ||
			f.e.Text("SELECT details->'after'->>'restrictedLevels' FROM audit_event WHERE action = 'settings.visibility.changed'") != `["public"]` ||
			f.e.Text("SELECT details->'before'->>'restrictedLevels' FROM audit_event WHERE action = 'settings.visibility.changed'") != `[]` {
			t.Error("settings.visibility.changed does not carry the set before and after")
		}
	})
	t.Run("the default is never restricted, and a restricted level never becomes the default", func(t *testing.T) {
		wantStatus(t, patchSettings(map[string]any{"restrictedLevels": []string{"private"}}), 422, "default_level_restricted")
		wantStatus(t, patchSettings(map[string]any{"defaultLevel": "public"}), 422, "default_level_restricted")
		if err := f.e.TryExec("UPDATE visibility_settings SET restricted_levels = '{private}'"); err == nil {
			t.Error("the database stored a restricted default")
		}
	})
	t.Run("R-API-076 every signed-in caller reads its choices", func(t *testing.T) {
		mine := f.do(f.alice, "GET", "/api/v1/visibility-choices", nil)
		wantStatus(t, mine, 200, "")
		if got := fmt.Sprint(mine.JSON(t)); got != "map[defaultLevel:private mayChooseRestricted:false publicAvailable:true restrictedLevels:[public]]" {
			t.Errorf("alice reads %s", got)
		}
		if got := f.do(f.admin, "GET", "/api/v1/visibility-choices", nil).JSON(t)["mayChooseRestricted"]; got != true {
			t.Errorf("the administrator reads mayChooseRestricted %v", got)
		}
		wantStatus(t, f.cl.Do("", "GET", "/api/v1/visibility-choices", nil), 401, "")
	})
	t.Run("R-AUTHZ-037 an Owner is refused a restricted level, an administrator is not", func(t *testing.T) {
		wantProblem(t, create(f.alice, "npm-owner", "public"), 403, "visibility_level_restricted")
		wantStatus(t, create(f.alice, "npm-owner", "internal"), 201, "")
		wantProblem(t, repoPatch(f.alice, "npm-owner", map[string]any{"artifacts": "public"}), 403, "visibility_level_restricted")
		wantStatus(t, create(f.admin, "npm-admin", "public"), 201, "")
		wantStatus(t, repoPatch(f.admin, "npm-owner", map[string]any{"artifacts": "public"}), 200, "")
	})
	t.Run("an existing object keeps a restricted level and is lowered, but not raised to it again", func(t *testing.T) {
		wantStatus(t, f.do(f.alice, "PATCH", base+"/web/repositories/npm-open", map[string]any{"description": "kept public"}, "If-Match", "*", "Content-Type", "application/merge-patch+json"), 200, "")
		wantStatus(t, repoPatch(f.alice, "npm-open", map[string]any{"artifacts": "public", "wiki": "internal"}), 200, "")
		if got := f.levels("web", "npm-open"); got != "public/internal" {
			t.Errorf("stored %s", got)
		}
		wantProblem(t, repoPatch(f.alice, "npm-open", map[string]any{"wiki": "public"}), 403, "visibility_level_restricted")
		wantStatus(t, repoPatch(f.alice, "npm-open", map[string]any{"artifacts": "internal", "wiki": "internal"}), 200, "")
		wantProblem(t, repoPatch(f.alice, "npm-open", map[string]any{"artifacts": "public"}), 403, "visibility_level_restricted")
	})
	t.Run("an absent level takes the highest level the Owner may choose, and none left is refused", func(t *testing.T) {
		wantStatus(t, patchSettings(map[string]any{"defaultLevel": "public", "restrictedLevels": []string{"internal"}}), 200, "")
		wantStatus(t, f.do(f.admin, "PUT", base+"/web/visibility", map[string]any{"level": "internal"}, "If-Match", "*"), 200, "")
		wantStatus(t, create(f.alice, "npm-absent", ""), 201, "")
		if got := f.levels("web", "npm-absent"); got != "private/private" {
			t.Errorf("an Owner's absent level in an internal project with internal restricted: %s", got)
		}
		wantStatus(t, create(f.admin, "npm-absent-admin", ""), 201, "")
		if got := f.levels("web", "npm-absent-admin"); got != "internal/internal" {
			t.Errorf("an administrator's absent level: %s", got)
		}
		wantStatus(t, patchSettings(map[string]any{"restrictedLevels": []string{"private", "internal"}}), 200, "")
		wantProblem(t, create(f.alice, "npm-none", ""), 403, "visibility_level_restricted")
	})
	t.Run("R-AUTHZ-037 an administrator sets a restricted level of a project", func(t *testing.T) {
		// The default is public since the subtest before: both move in one patch, the result is checked (Q-AUTHZ-284).
		wantStatus(t, patchSettings(map[string]any{"defaultLevel": "private", "restrictedLevels": []string{"public"}}), 200, "")
		wantStatus(t, f.do(f.admin, "PUT", base+"/web/visibility", map[string]any{"level": "public"}, "If-Match", "*"), 200, "")
		if f.level("web") != "public" {
			t.Errorf("the administrator's project level: %s", f.level("web"))
		}
	})
}
