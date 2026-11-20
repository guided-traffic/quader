package authz

import (
	"testing"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// The worked rows of R-AUTHZ-041 in the engine: a member of the built-in project upstream through a team's virtual repository is
// decided on its own level and the grants that name it, as every member is (R-AUTHZ-009 rule 2); nothing of the virtual repository's
// grants or roles reaches it, and the level internal admits every signed-in subject but a user flagged external (R-AUTHZ-030). The
// team's project is web: npm-all (virtual) over npm-local and upstream/npmjs; the CI mapping web-build grants read on npm-all only;
// Erin is a user flagged external with the role reader in web; the project service account web/deployer holds read on npm-all. The
// grant index names the member upstream/npmjs with its own project (AuthzBuildVirtualMembers), which the scope lift of Q-AUTHZ-163,
// the anonymous rule 6 of R-AUTHZ-009 and the reach of a Bastion follow.

const pUpstream = "project-upstream"

var (
	webNpmAll   = qname.Repo{Project: "web", Key: "npm-all"}
	webNpmLocal = qname.Repo{Project: "web", Key: "npm-local"}
	upNpmjs     = qname.Repo{Project: wellknown.ProjectKey, Key: "npmjs"}
)

func upstreamRowsEngine(t *testing.T, level string) *Engine {
	t.Helper()
	return newTestEngine(t, upstreamRowsFixture(level), nil)
}

// upstreamRowsFixture is the fixture of the rows with upstream/npmjs at a level; npm-local and npm-all are private.
func upstreamRowsFixture(level string) *fixture {
	f := newFixture().
		repo("npm-local", "local", "npm", "").
		repo("npmjs", "remote", "npm", "").
		repo("npm-all", "virtual", "npm", `{"virtual":{"members":["npm-local","upstream/npmjs"]}}`)
	f.project(pWeb, "web").project(pUpstream, wellknown.ProjectKey)
	f.assign("npm-local", pWeb).assign("npm-all", pWeb).assign("npmjs", pUpstream)
	f.levels("npmjs", level, level)
	f.member(pWeb, "user-erin", "reader")
	f.member(pWeb, "user-ann", "reader")
	f.policy(policyDef{name: "deployer-reads", selectors: []string{"web/npm-all"}, entries: map[string][]string{idBot: {"read"}}})
	return f
}

// scopedToNpmAll narrows a subject's token to read on the team's virtual repository, by its exact name: what a CI job that requested
// the scope read:web/npm-all, or a personal token made for the virtual repository's setup, carries (R-AUTHZ-013, Q-AUTHZ-163).
func scopedToNpmAll(s Subject) Subject {
	return tokenScop(s, scope.Scope{Repositories: []scope.Grant{{Repositories: []string{webNpmAll.String()}, Actions: []string{"read"}}}})
}

func TestR_AUTHZ_041_AMemberOfUpstreamIsDecidedOnItsLevelThroughATeamsVirtualRepository(t *testing.T) {
	ci := Subject{Kind: principal.KindCI, Name: "ci", Mappings: []string{"ci:gitlab/web-build"}, TokenID: "cit-1", AuthMethod: "bearer"}
	erin := bearer(external(userSub("user-erin", "erin")))
	// A request a Bastion forwards is decided on the caller's own subject, which carries no mark of the channel (Q-AUTHZ-321): row 5
	// is the service account's token as it is at the main installation's door.
	deployer := bearer(Subject{Kind: principal.KindServiceAccount, ID: idBot, Name: "web/deployer"})
	through := func(member qname.Repo) Coordinate {
		return Coordinate{Repository: member, Addressed: webNpmAll, Package: "lodash", Kind: KindArtifact, Actions: []Action{Read}}
	}
	for _, c := range []struct {
		row    string
		sub    Subject
		level  string
		member qname.Repo
		read   bool
		reason string
	}{
		{"1 the CI token reads an internal member by its level", ci, "internal", upNpmjs, true, ReasonAllowVisibility},
		{"2 the CI token does not read a private member: its grant on npm-all does not reach it", ci, "private", upNpmjs, false, ReasonNoGrant},
		{"3 Erin, external, does not read an internal member", erin, "internal", upNpmjs, false, ReasonNoGrant},
		{"4 Erin reads the local member by her role in web", erin, "internal", webNpmLocal, true, ReasonAllowGrant},
		{"5 the service account's token reads an internal member by its level, through a Bastion as without it", deployer, "internal", upNpmjs, true, ReasonAllowVisibility},
		{"6 a placeholder is read by the CI token as by its level: the 404 then carries the sentence", ci, "internal", upNpmjs, true, ReasonAllowVisibility},
		{"7 Erin would not read the placeholder once it is on: her 404 carries no sentence", erin, "internal", upNpmjs, false, ReasonNoGrant},
		{"8 a CI token whose scope names npm-all reads an internal member through it: the scope's lift reaches the member", scopedToNpmAll(ci), "internal", upNpmjs, true, ReasonAllowVisibility},
		{"8 a CI token whose scope names npm-all does not read a private member through it", scopedToNpmAll(ci), "private", upNpmjs, false, ReasonNoGrant},
		{"8 Erin's personal token scoped to npm-all does not read an internal member: the scope lifts no refusal of the level", scopedToNpmAll(erin), "internal", upNpmjs, false, ReasonNoGrant},
	} {
		t.Run("R-AUTHZ-041 row "+c.row, func(t *testing.T) {
			e := upstreamRowsEngine(t, c.level)
			idx, err := e.Begin(t.Context(), c.sub, webNpmAll)
			if err != nil {
				t.Fatal(err)
			}
			coord := through(c.member)
			if read, _ := e.MemberUsableOn(t.Context(), idx, c.sub, c.member, coord); read != c.read {
				t.Fatalf("MemberUsableOn(%s) = %v, want %v", c.member, read, c.read)
			}
			if d := e.DecideOn(t.Context(), idx, c.sub, coord); d.Allowed() != c.read || d.Reason != c.reason {
				t.Fatalf("decision %+v, want allowed %v reason %q", d, c.read, c.reason)
			}
		})
	}
}

// TestR_AUTHZ_009_TheGrantIndexFollowsAMemberOfUpstream: the questions of the index that walk a virtual repository's members meet the
// member of upstream under its own name. An anonymous caller reads through npm-all only while every member, upstream/npmjs included, is
// public (R-AUTHZ-009 rule 6); a listing through npm-all decides the member's rows as reads through it (Q-AUTHZ-168); and a Bastion's
// reach of npm-all is full for a caller who reads every member in full, partial for one who reads only the local member (R-BAS-012,
// Q-BAS-148).
func TestR_AUTHZ_009_TheGrantIndexFollowsAMemberOfUpstream(t *testing.T) {
	t.Run("R-AUTHZ-009 rule 6 an anonymous caller is asked to sign in while the member of upstream is not public", func(t *testing.T) {
		for level, want := range map[string]Outcome{"internal": DenyUnauthenticated, "private": DenyUnauthenticated, "public": Allow} {
			f := upstreamRowsFixture(level)
			f.levels("npm-local", "public", "public").levels("npm-all", "public", "public")
			e := newTestEngine(t, f, nil)
			d := e.Decide(t.Context(), anonSub, Coordinate{Repository: webNpmAll, Package: "lodash", Kind: KindMetadata, Actions: []Action{Read}})
			if d.Outcome != want || (want == DenyUnauthenticated && d.Reason != ReasonAnonymousPartialVirtual) {
				t.Errorf("upstream/npmjs %s: %+v, want outcome %d", level, d, want)
			}
		}
	})
	t.Run("Q-AUTHZ-168 a listing through npm-all reads the member's rows as reads through it", func(t *testing.T) {
		e := upstreamRowsEngine(t, "internal")
		sub := scopedToNpmAll(Subject{Kind: principal.KindCI, Name: "ci", Mappings: []string{"ci:gitlab/web-build"}, TokenID: "cit-1", AuthMethod: "bearer"})
		idx, err := e.Begin(t.Context(), sub, webNpmAll)
		if err != nil {
			t.Fatal(err)
		}
		rs, err := e.ReadScopeOn(t.Context(), idx, sub, webNpmAll, []qname.Repo{upNpmjs})
		if err != nil {
			t.Fatal(err)
		}
		if rs.Shape(upNpmjs) == ReadNone || !rs.MayRead(upNpmjs, "lodash", "") {
			t.Fatalf("shape %v, MayRead %v: the member's rows are not read through npm-all", rs.Shape(upNpmjs), rs.MayRead(upNpmjs, "lodash", ""))
		}
	})
	t.Run("R-BAS-012 Q-BAS-148 a Bastion's reach of npm-all follows the member of upstream", func(t *testing.T) {
		e := upstreamRowsEngine(t, "internal")
		for who, c := range map[string]struct {
			sub  Subject
			want Reach
		}{
			"Ann, internal, reads every member in full": {bearer(userSub("user-ann", "ann")), ReachFull},
			"Erin, external, reads only npm-local":      {bearer(external(userSub("user-erin", "erin"))), ReachPartial},
		} {
			idx, err := e.Begin(t.Context(), c.sub, webNpmAll)
			if err != nil {
				t.Fatal(err)
			}
			got, err := e.ReachOn(t.Context(), idx, c.sub, []qname.Repo{webNpmAll})
			if err != nil {
				t.Fatal(err)
			}
			if got[webNpmAll].Reach != c.want {
				t.Errorf("%s: reach %s, want %s", who, got[webNpmAll].Reach, c.want)
			}
		}
	})
}
