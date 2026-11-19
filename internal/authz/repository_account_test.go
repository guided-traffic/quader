package authz

import (
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The actions a repository's service account holds on its repository (R-AUTHZ-039, Q-AUTHZ-296): a grant source beside the
// policies and the project roles, named account:sa:<project>/<repository>/<name> in a decision.

const idRepoBot = "sa-web-npm-ci"

var repoBotSub = Subject{Kind: principal.KindServiceAccount, ID: idRepoBot, Name: "web/web-npm/ci"}

// accountGrant gives the account the actions on the repository, as the snapshot reads service_account_repository_grant.
func (f *fixture) accountGrant(repo, principalID, name string, actions ...string) *fixture {
	f.s.AccountGrants = append(f.s.AccountGrants, q.AuthzBuildAccountGrantsRow{
		RepositoryID: f.repoID[repo], PrincipalID: principalID, PrincipalName: name, Actions: actions,
	})
	return f
}

func repositoryAccountFixture() *fixture {
	return projectFixture().
		repo("web-all", "virtual", "npm", `{"virtual":{"members":["web-npm"],"deployTarget":"web-npm"}}`).
		assign("web-all", pWeb).
		accountGrant("web-npm", idRepoBot, "web/web-npm/ci", "read", "deploy")
}

func TestR_AUTHZ_039_ARepositoryAccountHoldsItsActionsOnItsRepositoryAndNothingElse(t *testing.T) {
	e := newTestEngine(t, repositoryAccountFixture(), nil)
	t.Run("read and deploy on its repository, named by the account", func(t *testing.T) {
		d := e.Decide(t.Context(), repoBotSub, write("web-npm", "left-pad", "", Deploy))
		if d.Outcome != Allow || !slices.Contains(d.Sources, "account:sa:web/web-npm/ci") {
			t.Fatalf("deploy: %+v", d)
		}
		if d := e.Decide(t.Context(), repoBotSub, read("web-npm", "left-pad", "")); d.Outcome != Allow {
			t.Fatalf("read: %+v", d)
		}
	})
	t.Run("an action the set does not hold is refused", func(t *testing.T) {
		if d := e.Decide(t.Context(), repoBotSub, write("web-npm", "left-pad", "", Delete)); d.Outcome == Allow {
			t.Fatalf("delete was allowed: %+v", d)
		}
		if d := e.Decide(t.Context(), repoBotSub, write("web-npm", "left-pad", "", Manage)); d.Outcome == Allow {
			t.Fatalf("manage was allowed: %+v", d)
		}
	})
	t.Run("another repository of the same project, another project and the instance are refused", func(t *testing.T) {
		for _, repo := range []string{"web-files", "shop-npm", "npm-internal"} {
			if d := e.Decide(t.Context(), repoBotSub, read(repo, "left-pad", "")); d.Outcome == Allow {
				t.Fatalf("%s: read was allowed: %+v", repo, d)
			}
		}
	})
	t.Run("a virtual repository over its repository is not its repository: no read through it", func(t *testing.T) {
		if d := e.Decide(t.Context(), repoBotSub, metadata("web-all")); d.Outcome == Allow {
			t.Fatalf("the virtual repository was readable: %+v", d)
		}
	})
	t.Run("the token scope narrows the account's actions", func(t *testing.T) {
		narrowed := tokenScop(repoBotSub, scope.Scope{Repositories: []scope.Grant{{Repositories: []string{"web/web-npm"}, Actions: []string{"read"}}}})
		if d := e.Decide(t.Context(), narrowed, write("web-npm", "left-pad", "", Deploy)); d.Outcome == Allow {
			t.Fatalf("a read-only scope deployed: %+v", d)
		}
		if d := e.Decide(t.Context(), narrowed, read("web-npm", "left-pad", "")); d.Outcome != Allow {
			t.Fatalf("a read-only scope does not read: %+v", d)
		}
		elsewhere := tokenScop(repoBotSub, scope.Scope{Repositories: []scope.Grant{{Repositories: []string{"web/web-files"}, Actions: []string{"read"}}}})
		if d := e.Decide(t.Context(), elsewhere, read("web-npm", "left-pad", "")); d.Outcome == Allow {
			t.Fatalf("a scope of another repository reads: %+v", d)
		}
	})
	t.Run("only the account itself holds the actions: a user or another account with its ID's neighbour holds none", func(t *testing.T) {
		other := Subject{Kind: principal.KindServiceAccount, ID: idBot, Name: "ci-bot"}
		if d := e.Decide(t.Context(), other, read("web-npm", "left-pad", "")); d.Outcome == Allow {
			t.Fatalf("another account read: %+v", d)
		}
		user := Subject{Kind: principal.KindUser, ID: idRepoBot, Name: "web/web-npm/ci"}
		if d := e.Decide(t.Context(), user, read("web-npm", "left-pad", "")); d.Outcome == Allow {
			t.Fatalf("a user with the account's ID read: %+v", d)
		}
		if d := e.Decide(t.Context(), anonSub, write("web-npm", "left-pad", "", Deploy)); d.Outcome == Allow {
			t.Fatalf("the anonymous caller deployed: %+v", d)
		}
	})
	t.Run("the listing shape reads every package of its repository", func(t *testing.T) {
		readable, err := e.Readable(t.Context(), repoBotSub, qrs([]string{"web-npm", "web-files", "shop-npm"}))
		if err != nil {
			t.Fatal(err)
		}
		if got := qkeys(readable); !slices.Equal(got, []string{"web-npm"}) {
			t.Fatalf("readable %v", got)
		}
	})
}

func TestR_AUTHZ_039_TheAccountsActionsFollowTheIndex(t *testing.T) {
	t.Run("a narrowed set refuses what it no longer holds after the rebuild", func(t *testing.T) {
		f := projectFixture().accountGrant("web-npm", idRepoBot, "web/web-npm/ci", "read")
		e := newTestEngine(t, f, nil)
		if d := e.Decide(t.Context(), repoBotSub, write("web-npm", "left-pad", "", Deploy)); d.Outcome == Allow {
			t.Fatalf("deploy without deploy: %+v", d)
		}
	})
	t.Run("a row of a repository the snapshot does not hold grants nothing", func(t *testing.T) {
		f := projectFixture()
		f.s.AccountGrants = append(f.s.AccountGrants, q.AuthzBuildAccountGrantsRow{RepositoryID: 9999, PrincipalID: idRepoBot, PrincipalName: "web/gone/ci", Actions: []string{"read"}})
		e := newTestEngine(t, f, nil)
		for _, repo := range []string{"web-npm", "web-files"} {
			if d := e.Decide(t.Context(), repoBotSub, read(repo, "left-pad", "")); d.Outcome == Allow {
				t.Fatalf("%s: %+v", repo, d)
			}
		}
	})
	t.Run("a row whose actions do not parse grants nothing", func(t *testing.T) {
		f := projectFixture().accountGrant("web-npm", idRepoBot, "web/web-npm/ci", "read", "fly")
		e := newTestEngine(t, f, nil)
		if d := e.Decide(t.Context(), repoBotSub, read("web-npm", "left-pad", "")); d.Outcome == Allow {
			t.Fatalf("a corrupt row granted: %+v", d)
		}
	})
	t.Run("on a virtual repository of its own only read and manage take effect", func(t *testing.T) {
		f := projectFixture().repo("web-all", "virtual", "npm", `{"virtual":{"members":["web-npm"],"deployTarget":"web-npm"}}`).assign("web-all", pWeb).
			accountGrant("web-all", idRepoBot, "web/web-all/ci", "read", "deploy")
		e := newTestEngine(t, f, nil)
		idx, err := e.Begin(t.Context(), repoBotSub, qr("web-all"))
		if err != nil {
			t.Fatal(err)
		}
		g, ok := idx.accountGrantOf(repoBotSub, f.repoID["web-all"])
		if !ok || g.actions.Has(Deploy) || !g.actions.Has(Read) {
			t.Fatalf("grant on the virtual repository: %+v %v", g, ok)
		}
	})
}
