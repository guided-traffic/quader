//go:build integration

package identity

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/test/integration/idapi"
)

// The service accounts of a repository and their technical tokens over HTTP, with the real authentication stage and the real
// decision point (R-AUTH-045, R-AUTH-046, R-AUTHZ-039).

// repository inserts an active npm repository into the project, which must exist; it runs before the replica serves, so the
// decision point's first build holds it.
func (e *env) repository(project, key string) int64 {
	e.t.Helper()
	e.exec(`INSERT INTO repositories (key, class, format, config, project_id)
	        SELECT $1, 'local', 'npm', '{}'::jsonb, id FROM project WHERE key = $2`, key, project)
	return e.scalar(`SELECT r.id FROM repositories r JOIN project p ON p.id = r.project_id WHERE p.key = $1 AND r.key = $2`, project, key)
}

// serveWithChecks is serve with the caller's checks (POST /api/v1/authz/check) answered by the replica's decision point.
func (r *replica) serveWithChecks(e *env) *served {
	r.t.Helper()
	h := idapi.Handler(r.t, idapi.Components{
		Name: r.name, DB: r.db, Changes: r.changes, Stage: r.stage, Manager: r.manager, Tokens: r.tokens, Settings: r.settings,
		Recorder: r.recorder, Auth: idapi.AuthConfig(r.tokenCfg), BaseURL: baseURL,
		Options: func(engine *authz.Engine, o *v1.Options) { o.Authz.Engine = engine },
	})
	s := &served{replica: r, srv: httptest.NewServer(h)}
	r.t.Cleanup(s.srv.Close)
	return s
}

// allowed asks the decision point through POST /api/v1/authz/check whether the bearer may take the action on each repository.
func (s *served) allowed(t *testing.T, bearer, action string, repos ...string) string {
	t.Helper()
	checks := make([]any, 0, len(repos))
	for _, r := range repos {
		checks = append(checks, map[string]any{"repository": r, "action": action})
	}
	rp := s.call(post, "/api/v1/authz/check", bearer, map[string]any{"checks": checks})
	want(t, rp, 200, "")
	var out []bool
	for _, r := range asList(rp.json(t)["results"]) {
		m, _ := r.(map[string]any)
		ok, _ := m["allowed"].(bool)
		out = append(out, ok)
	}
	return fmt.Sprint(out)
}

func TestR_AUTH_046_RepositoryAccountsAndTheirTokens(t *testing.T) {
	e := newEnv(t)
	e.project("web")
	e.project("shop")
	repoA := e.repository("web", "npm-a")
	e.repository("web", "npm-b")
	e.repository("shop", "npm-c")
	a := e.newReplica("a", replicaOpts{})
	fx := e.newFixture(a)
	wanda, devon := a.user("wanda"), a.user("devon")
	e.member("web", wanda.ID, "owner")
	e.member("web", devon.ID, "maintainer")
	wandaTok, devonTok := a.ownerToken(wanda), a.ownerToken(devon)
	s := a.serveWithChecks(e)
	ctx := context.Background()

	const accounts = "/api/v1/projects/web/repositories/npm-a/service-accounts"
	const release = accounts + "/release"
	var secret, secretID, technicalID string

	t.Run("R-AUTH-046 an Owner creates an account of a repository of its project and only of it", func(t *testing.T) {
		rp := s.call(post, accounts, wandaTok, map[string]any{"name": "release", "description": "the release job", "actions": []string{"deploy", "read"}})
		want(t, rp, 201, "")
		m := rp.json(t)
		if str(m, "reference") != "sa:web/npm-a/release" || str(m, "level") != "repository" || str(m, "repository") != "npm-a" || str(m, "project") != "web" ||
			fmt.Sprint(m["actions"]) != "[read deploy]" {
			t.Fatalf("%s", rp.Body)
		}
		want(t, s.call(post, accounts, wandaTok, map[string]any{"name": "release"}), 409, "already_exists")
		want(t, s.call(post, accounts, wandaTok, map[string]any{"name": "fly", "actions": []string{"fly"}}), 422, "")
		want(t, s.call(post, accounts, devonTok, map[string]any{"name": "dev-x"}), 403, "forbidden")
		want(t, s.call(post, "/api/v1/projects/shop/repositories/npm-c/service-accounts", wandaTok, map[string]any{"name": "shop-x"}), 404, "")
		want(t, s.call(post, "/api/v1/projects/web/repositories/nothing/service-accounts", wandaTok, map[string]any{"name": "xx"}), 404, "")
		// No actions named: read alone.
		rp = s.call(post, accounts, wandaTok, map[string]any{"name": "reader"})
		want(t, rp, 201, "")
		if fmt.Sprint(rp.json(t)["actions"]) != "[read]" {
			t.Fatalf("%s", rp.Body)
		}
	})

	t.Run("R-AUTH-046 the account is addressed under its own repository only", func(t *testing.T) {
		want(t, s.call(get, release, wandaTok, nil), 200, "")
		want(t, s.call(get, "/api/v1/projects/web/repositories/npm-b/service-accounts/release", wandaTok, nil), 404, "")
		want(t, s.call(get, "/api/v1/projects/web/service-accounts/release", wandaTok, nil), 404, "")
		if got := list(t, s.call(get, "/api/v1/projects/web/repositories/npm-b/service-accounts", wandaTok, nil)); len(got) != 0 {
			t.Fatalf("npm-b lists %v", got)
		}
		// The project's list holds the project's own accounts, not the repositories'.
		if got := list(t, s.call(get, "/api/v1/projects/web/service-accounts", wandaTok, nil)); len(got) != 0 {
			t.Fatalf("the project lists %v", got)
		}
		// The instance list of an administrator holds it, with its level.
		found := false
		for _, sa := range list(t, s.call(get, "/api/v1/service-accounts", fx.adminToken, nil)) {
			found = found || (str(sa, "reference") == "sa:web/npm-a/release" && str(sa, "level") == "repository")
		}
		if !found {
			t.Fatal("the administrator's list does not hold the repository's account")
		}
	})

	t.Run("R-AUTH-045 a technical token of the account, and its rights on its repository alone", func(t *testing.T) {
		rp := s.call(post, release+"/technical-tokens", wandaTok, map[string]any{"name": "ci", "ttlSeconds": 3600})
		want(t, rp, 201, "")
		m := rp.json(t)
		secret, secretID, technicalID = str(m, "token"), str(m, "secretId"), str(m, "id")
		if secret == "" || secretID == "" || technicalID == "" {
			t.Fatalf("%s", rp.Body)
		}
		if acct, _ := m["account"].(map[string]any); acct["reference"] != "sa:web/npm-a/release" || acct["level"] != "repository" {
			t.Fatalf("%s", rp.Body)
		}
		res, err := a.bearer(secret, "/api/v1/auth/whoami")
		if err != nil || res.Credential.TechnicalTokenID != technicalID {
			t.Fatalf("the stage names the technical token: %+v %v", res.Credential, err)
		}
		// R-AUTHZ-039: read and deploy on npm-a; nothing on the other repository of the project or on another project's.
		waitFor(t, "the account grant reaches the index", 15*time.Second, func() bool {
			return s.allowed(t, secret, "read", "web/npm-a") == "[true]"
		})
		if got := s.allowed(t, secret, "read", "web/npm-a", "web/npm-b", "shop/npm-c"); got != "[true false false]" {
			t.Fatalf("read: %s", got)
		}
		if got := s.allowed(t, secret, "deploy", "web/npm-a", "web/npm-b"); got != "[true false]" {
			t.Fatalf("deploy: %s", got)
		}
		if got := s.allowed(t, secret, "delete", "web/npm-a"); got != "[false]" {
			t.Fatalf("delete: %s", got)
		}
	})

	t.Run("R-AUTHZ-039 a narrowing of the actions reaches the decision", func(t *testing.T) {
		tag := s.call(get, release, wandaTok, nil).Header.Get("ETag")
		want(t, s.call(patch, release, wandaTok, map[string]any{"actions": []string{"read"}}, "If-Match", tag), 200, "")
		waitFor(t, "the narrowing reaches the index", 15*time.Second, func() bool {
			return s.allowed(t, secret, "deploy", "web/npm-a") == "[false]"
		})
		if got := s.allowed(t, secret, "read", "web/npm-a"); got != "[true]" {
			t.Fatalf("read after the narrowing: %s", got)
		}
	})

	t.Run("R-AUTHZ-039 a repository's account joins no group", func(t *testing.T) {
		if _, err := a.manager.CreateGroup(ctx, admin, principal.NewGroup{Name: "team"}); err != nil {
			t.Fatal(err)
		}
		want(t, s.call(patch, "/api/v1/groups/team/members", fx.adminToken, map[string]any{"add": []string{"sa:web/npm-a/release"}}), 422, "project_account_not_groupable")
	})

	t.Run("R-AUTH-046 a disabled account's token is refused, and works again once it is enabled", func(t *testing.T) {
		want(t, s.call(post, release+"/disable", wandaTok, nil), 204, "")
		waitFor(t, "the disable reaches the stage", 15*time.Second, func() bool {
			_, err := a.bearer(secret, "/api/v1/auth/whoami")
			return err != nil
		})
		_, err := a.bearer(secret, "/api/v1/auth/whoami")
		wantCode(t, err, 401, "principal_disabled")
		want(t, s.call(post, release+"/enable", wandaTok, nil), 204, "")
		waitFor(t, "the enable reaches the stage", 15*time.Second, func() bool {
			_, err := a.bearer(secret, "/api/v1/auth/whoami")
			return err == nil
		})
	})

	t.Run("R-AUTH-045 a rotation keeps the technical token and ends the old secret", func(t *testing.T) {
		// An Owner does not rotate (Q-AUTH-419; the rotation answers a token it does not rotate as one it does not see, 404); an
		// administrator does.
		want(t, s.call(post, "/api/v1/tokens/"+secretID+"/rotate", wandaTok, map[string]any{"overlapSeconds": 0}), 404, "")
		rp := s.call(post, "/api/v1/tokens/"+secretID+"/rotate", fx.adminToken, map[string]any{"overlapSeconds": 0})
		want(t, rp, 201, "")
		next, nextID := str(rp.json(t), "token"), str(rp.json(t), "id")
		rec := s.call(get, "/api/v1/tokens/"+nextID, fx.adminToken, nil)
		want(t, rec, 200, "")
		if tt, _ := rec.json(t)["technicalToken"].(map[string]any); str(tt, "id") != technicalID || str(tt, "name") != "ci" {
			t.Fatalf("the successor names another technical token: %s", rec.Body)
		}
		waitFor(t, "the old secret is refused", 15*time.Second, func() bool {
			_, err := a.bearer(secret, "/api/v1/auth/whoami")
			return err != nil
		})
		res, err := a.bearer(next, "/api/v1/auth/whoami")
		if err != nil || res.Credential.TechnicalTokenID != technicalID {
			t.Fatalf("the new secret: %+v %v", res.Credential, err)
		}
		secret = next
		// A second live secret is no new secret: 409 technical_token_active.
		want(t, s.call(post, release+"/technical-tokens/ci/secrets", wandaTok, map[string]any{"ttlSeconds": 3600}), 409, "technical_token_active")
	})

	t.Run("R-AUTH-045 deleting a technical token revokes its secret", func(t *testing.T) {
		rp := s.call(post, release+"/technical-tokens", wandaTok, map[string]any{"name": "gone", "ttlSeconds": 3600})
		want(t, rp, 201, "")
		gone := str(rp.json(t), "token")
		want(t, s.call(del, release+"/technical-tokens/gone", wandaTok, nil), 204, "")
		waitFor(t, "the deleted token's secret is refused", 15*time.Second, func() bool {
			_, err := a.bearer(gone, "/api/v1/auth/whoami")
			return err != nil
		})
		want(t, s.call(get, release+"/technical-tokens/gone", wandaTok, nil), 404, "")
		if _, err := a.bearer(secret, "/api/v1/auth/whoami"); err != nil {
			t.Fatalf("the other token of the account: %v", err)
		}
	})

	t.Run("R-AUTH-046 the accounts end with their repository", func(t *testing.T) {
		// The repository hook's call (internal/app repositoryAccountsHook), then the removal of the row.
		err := a.db.InTx(ctx, func(tx meta.Tx) error {
			return a.manager.EndRepositoryAccountsTx(ctx, tx, admin, repoA)
		})
		if err != nil {
			t.Fatal(err)
		}
		e.exec(`DELETE FROM repositories WHERE id = $1`, repoA)
		waitFor(t, "the ended account's token is refused", 15*time.Second, func() bool {
			_, err := a.bearer(secret, "/api/v1/auth/whoami")
			return err != nil
		})
		if n := e.scalar(`SELECT count(*) FROM principal WHERE name LIKE 'web/npm-a/%'`); n != 0 {
			t.Fatalf("%d principals of the repository are left", n)
		}
		want(t, s.call(get, release, fx.adminToken, nil), 404, "")
	})
}
