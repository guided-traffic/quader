//go:build integration

package upstream

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/wellknown"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// virtMembers is the member list of a repository document.
func virtMembers(t *testing.T, a testenv.Answer) []string {
	t.Helper()
	v, _ := a.JSON(t)["virtual"].(map[string]any)
	list, _ := v["members"].([]any)
	out := make([]string, 0, len(list))
	for _, m := range list {
		s, _ := m.(string)
		out = append(out, s)
	}
	return out
}

// virtFields lists "pointer code" of the errors[] of a problem body.
func virtFields(t *testing.T, a testenv.Answer) map[string]string {
	t.Helper()
	out := map[string]string{}
	list, _ := a.JSON(t)["errors"].([]any)
	for _, e := range list {
		m, _ := e.(map[string]any)
		p, _ := m["pointer"].(string)
		c, _ := m["code"].(string)
		out[p] = c
	}
	return out
}

// TestR_REPO_071_R_REPO_072_ATeamResolvesThroughAPlaceholderAndThroughTheMemberOnceItIsOn: the team's virtual repository web/npm-all
// lists npm-local and upstream/npmjs, which an administrator has not switched on. The member is accepted with member_inactive, the 404 of
// a name only it would have had says why to whom would read it, and the switch-on makes the virtual repository resolve through it
// without a change to the virtual repository; the readers of R-AUTHZ-041 read it by its level, the journal replays its removal by ID,
// and a virtual repository moves with it.
func TestR_REPO_071_R_REPO_072_ATeamResolvesThroughAPlaceholderAndThroughTheMemberOnceItIsOn(t *testing.T) {
	e := virtNewEnv(t)
	doc := e.pkg.Name
	sentence := "upstream/npmjs is a member of this virtual repository, but an administrator of Quader has not switched it on."

	t.Run("R-REPO-071 a create lists upstream/npmjs with member_inactive and reads it back", func(t *testing.T) {
		a := e.do("POST", "/api/v1/projects/web/repositories", e.admin, map[string]any{
			"key": "npm-new", "class": "virtual", "format": "npm", "virtual": map[string]any{"members": []string{"npm-local", "upstream/npmjs"}},
		})
		if a.Status != 201 {
			t.Fatalf("create: %d %s", a.Status, a.Body)
		}
		var warned bool
		ws, _ := a.JSON(t)["warnings"].([]any)
		for _, w := range ws {
			m, _ := w.(map[string]any)
			d, _ := m["detail"].(string)
			warned = warned || (m["code"] == "member_inactive" && m["pointer"] == "/virtual/members/1" &&
				strings.Contains(d, "upstream/npmjs") && strings.Contains(d, string(wellknown.StateNotSwitchedOn)))
		}
		if !warned {
			t.Fatalf("no member_inactive naming upstream/npmjs and its state: %s", a.Body)
		}
		if got := virtMembers(t, e.do("GET", "/api/v1/projects/web/repositories/npm-new", e.admin, nil)); !slices.Equal(got, []string{"npm-local", "upstream/npmjs"}) {
			t.Fatalf("members read back %v", got)
		}
	})

	t.Run("R-REPO-071 another format, no such repository, the system repository and every other qualified name are invalid_member", func(t *testing.T) {
		a := e.do("POST", "/api/v1/projects/web/repositories", e.admin, map[string]any{
			"key": "npm-bad", "class": "virtual", "format": "npm", "virtual": map[string]any{
				"members": []string{"upstream/docker-hub", "upstream/nope", "upstream/" + wellknown.ScannerTrivyKey, "mobile/npm-local", "web/npm-local"},
			},
		})
		f := virtFields(t, a)
		if a.Status != 422 || len(f) != 5 {
			t.Fatalf("%d %v", a.Status, f)
		}
		for p, code := range f {
			if code != "invalid_member" {
				t.Errorf("%s = %s", p, code)
			}
		}
	})

	t.Run("R-REPO-071 the member tables admit a repository of upstream and no other project's; a deploy target stays the own project's", func(t *testing.T) {
		e.st.RepositoryIn("web", "npm-guard", "virtual", "npm", `{}`)
		guard, npmjs := e.id("web", "npm-guard"), e.id(wellknown.ProjectKey, "npmjs")
		state := func(sql string, args ...any) string {
			_, err := e.st.TryText(sql+" RETURNING 'ok'", args...)
			if err == nil {
				return "ok"
			}
			if strings.Contains(err.Error(), "AI006") {
				return "AI006"
			}
			return err.Error()
		}
		if got := state(`INSERT INTO virtual_members (virtual_id, position, member_id) VALUES ($1, 0, $2)`, guard, e.id("mobile", "npm-local")); got != "AI006" {
			t.Errorf("a member of project mobile: %s", got)
		}
		if got := state(`INSERT INTO virtual_members (virtual_id, position, member_id) VALUES ($1, 0, $2)`, guard, npmjs); got != "ok" {
			t.Errorf("a member of upstream: %s", got)
		}
		if got := state(`INSERT INTO virtual_deploy_targets (virtual_id, target_id) VALUES ($1, $2)`, guard, npmjs); got != "AI006" {
			t.Errorf("a deploy target of upstream: %s", got)
		}
	})

	t.Run("R-REPO-072 a name only the placeholder would have had: 404 with the sentence, and the upstream is not asked", func(t *testing.T) {
		a := e.npm("npm-all", doc, e.ann)
		if code, msg := virtNpmError(t, a); a.Status != 404 || code != "not_found" || msg != sentence {
			t.Fatalf("%d %s %q", a.Status, code, msg)
		}
		if n := e.stub.Counts().Total(); n != 0 {
			t.Fatalf("the test upstream was asked %d times", n)
		}
		ex := e.do("GET", "/api/v1/projects/web/repositories/npm-all/resolution?path="+doc, e.ann, nil).JSON(t)
		members, _ := ex["members"].([]any)
		var line map[string]any
		for _, m := range members {
			if mm, _ := m.(map[string]any); mm["key"] == "upstream/npmjs" {
				line = mm
			}
		}
		if line["decision"] != "disabled" || line["upstreamState"] != string(wellknown.StateNotSwitchedOn) {
			t.Fatalf("explanation %v", ex)
		}
	})

	t.Run("R-REPO-072 R-AUTHZ-041 row 7 a user flagged external is not told about an internal placeholder", func(t *testing.T) {
		a := e.npm("npm-all", doc, e.erin)
		if _, msg := virtNpmError(t, a); a.Status != 404 || strings.Contains(msg, "upstream/npmjs") {
			t.Fatalf("%d %q", a.Status, msg)
		}
	})

	allVersion := e.st.Number(`SELECT config_version FROM repositories WHERE id = $1`, e.id("web", "npm-all"))
	e.virtSwitchOn("npmjs")

	t.Run("R-REPO-072 after the switch-on the same request resolves through the member, without a change to the virtual repository", func(t *testing.T) {
		took := testenv.WaitFor(t, "the virtual repository resolving through upstream/npmjs", 5*time.Second, func() bool {
			return e.npm("npm-all", doc, e.ann).Status == 200
		})
		t.Logf("resolved through the member %s after the switch-on (bound 2 s, R-REPO-049)", took)
		if got := e.st.Number(`SELECT config_version FROM repositories WHERE id = $1`, e.id("web", "npm-all")); got != allVersion {
			t.Fatalf("npm-all changed: version %d, was %d", got, allVersion)
		}
	})

	t.Run("R-AUTHZ-041 rows 1, 3, 5 and 8: CI tokens, with and without a scope that names npm-all, and a service account's token read the internal member by its level, a user flagged external does not", func(t *testing.T) {
		tarball := e.tarball()
		inherited, scoped := e.virtCITokens()
		for who, token := range map[string]string{
			"the CI token of web-build": inherited, "the CI token scoped to read:web/npm-all": scoped, "the service account's token": e.deployer,
		} {
			a := e.npm("npm-all", tarball, token)
			if a.Status != 200 || a.Header.Get("Quader-Resolved-From") != "upstream/npmjs" {
				t.Errorf("%s: %d from %q %.200s", who, a.Status, a.Header.Get("Quader-Resolved-From"), a.Body)
			}
		}
		a := e.npm("npm-all", tarball, e.erin)
		if _, msg := virtNpmError(t, a); a.Status != 404 || strings.Contains(msg, "upstream") {
			t.Fatalf("Erin: %d %q", a.Status, msg)
		}
	})

	t.Run("R-REPO-071 R-REPO-061 a removed member of upstream is journaled by its ID and the restore replays its removal", func(t *testing.T) {
		before := time.Now().Add(-time.Second)
		a := e.do("PATCH", "/api/v1/projects/web/repositories/npm-all", e.admin, []byte(`{"virtual":{"members":["npm-local"]}}`),
			"Content-Type", "application/merge-patch+json", "If-Match", "*")
		if a.Status != 200 {
			t.Fatalf("patch: %d %s", a.Status, a.Body)
		}
		all, npmjs := e.id("web", "npm-all"), e.id(wellknown.ProjectKey, "npmjs")
		// The restored database holds the member list of the restore point.
		e.st.Exec(`INSERT INTO virtual_members (virtual_id, position, member_id) VALUES ($1, 1, $2)`, all, npmjs)
		e.virtReplay(before)
		if n := e.st.Number(`SELECT count(*) FROM virtual_members WHERE virtual_id = $1 AND member_id = $2`, all, npmjs); n != 0 {
			t.Fatal("the replay left upstream/npmjs in npm-all")
		}
	})

	t.Run("R-REPO-071 Q-REPO-389 a virtual repository moves with its member of upstream and reads it back there", func(t *testing.T) {
		a := e.do("PATCH", "/api/v1/projects/web/repositories/npm-new", e.admin, []byte(`{"virtual":{"members":["upstream/npmjs"]}}`),
			"Content-Type", "application/merge-patch+json", "If-Match", "*")
		if a.Status != 200 {
			t.Fatalf("patch: %d %s", a.Status, a.Body)
		}
		if a := e.do("POST", "/api/v1/projects/web/repositories/npm-new/move", e.admin, map[string]any{"project": "mobile"}); a.Status != 200 {
			t.Fatalf("move: %d %s", a.Status, a.Body)
		}
		if got := virtMembers(t, e.do("GET", "/api/v1/projects/mobile/repositories/npm-new", e.admin, nil)); !slices.Equal(got, []string{"upstream/npmjs"}) {
			t.Fatalf("members in mobile %v", got)
		}
	})
}
