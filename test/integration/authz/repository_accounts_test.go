//go:build integration

package authz

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_AUTHZ_039_ARepositoryAccountHoldsItsActionsAndIsNamedByNoPolicy: a repository's service account holds the actions
// stored with it on its own repository and nothing else, on every replica; no policy, global or of its project, and no
// system role names it (R-AUTHZ-039, R-AUTH-046).
func TestR_AUTHZ_039_ARepositoryAccountHoldsItsActionsAndIsNamedByNoPolicy(t *testing.T) {
	p := newProjectEnv(t)
	ctx := context.Background()
	acct := person{ID: p.principalRow("service_account", "web/web-npm/release"), Name: "web/web-npm/release", Kind: principal.KindServiceAccount}
	p.exec(`INSERT INTO service_account (principal_id, project_id, repository_id)
	        SELECT $1::uuid, r.project_id, r.id FROM repositories r JOIN project pr ON pr.id = r.project_id
	         WHERE pr.key = 'web' AND r.key = 'web-npm'`, acct.ID)
	p.exec(`INSERT INTO service_account_repository_grant (service_account_id, actions) VALUES ($1::uuid, ARRAY['read','deploy'])`, acct.ID)
	p.changed()
	release := sub(acct)
	web := "web"

	t.Run("its actions on its repository, on every replica", func(t *testing.T) {
		for _, r := range []*replica{p.a, p.b} {
			waitFor(t, "the account grant is built", 5*time.Second, func() bool { return p.may(r, release, "web/web-npm", authz.Read) })
			if !p.may(r, release, "web/web-npm", authz.Deploy) || p.may(r, release, "web/web-npm", authz.Delete) {
				t.Error("the actions are not the stored ones")
			}
		}
	})
	t.Run("nothing on another repository of its project, of another project or of the instance's project", func(t *testing.T) {
		for _, repo := range []string{"web/web-files", "web/npm-web", "shop/shop-npm", "npm-internal"} {
			if p.may(p.a, release, repo, authz.Read) {
				t.Errorf("reads %s", repo)
			}
		}
	})
	t.Run("a global policy naming it is refused", func(t *testing.T) {
		_, _, err := p.a.svc.Create(ctx, p.root, authz.PolicyInput{
			Name: "g-release", Resources: authz.Resources{Repositories: []string{"web/web-files"}},
			Entries: []authz.Entry{{Principal: "sa:web/web-npm/release", Actions: []string{"read"}}},
		})
		problem(t, err, 422, errs.CodePrincipalOutsideRepository)
	})
	t.Run("its own project's policy naming it is refused", func(t *testing.T) {
		_, _, err := p.a.svc.Create(ctx, p.root, authz.PolicyInput{
			Name: "web/release", Project: &web, Resources: authz.Resources{Repositories: []string{"web-files"}},
			Entries: []authz.Entry{{Principal: "sa:web/web-npm/release", Actions: []string{"read"}}},
		})
		problem(t, err, 422, errs.CodePrincipalOutsideRepository)
	})
	t.Run("a system role is not bound to it", func(t *testing.T) {
		_, _, err := p.a.svc.BindRole(ctx, p.root, authz.RoleAuditor, "sa:web/web-npm/release")
		problem(t, err, 422, errs.CodeRoleNotAllowed)
	})
	t.Run("the database refuses it as the principal of a role binding", func(t *testing.T) {
		p.mu.Lock()
		_, err := p.admin.Exec(ctx, `INSERT INTO authz_role_binding (role, principal_id) VALUES ('auditor', $1::uuid)`, acct.ID)
		p.mu.Unlock()
		if err == nil || !strings.Contains(err.Error(), "AI005") {
			t.Fatalf("the role binding: %v", err)
		}
	})
	t.Run("the actions follow the index when they are narrowed", func(t *testing.T) {
		p.exec(`UPDATE service_account_repository_grant SET actions = ARRAY['read'] WHERE service_account_id = $1::uuid`, acct.ID)
		p.changed()
		waitFor(t, "the narrowing is built", 5*time.Second, func() bool { return !p.may(p.b, release, "web/web-npm", authz.Deploy) })
		if !p.may(p.b, release, "web/web-npm", authz.Read) {
			t.Error("the narrowing took read")
		}
	})
}
