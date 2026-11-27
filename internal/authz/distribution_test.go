package authz

import (
	"fmt"
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/qname"
)

// The Bastion registrations of the distribution's unit fixtures: principal IDs, opaque to the index.
const (
	bastionPartners = "bastion-partners"
	bastionPublic   = "bastion-public"
)

// distribute adds the rows of repository_bastion: the Bastion distributes the repositories (R-BAS-134).
func (f *fixture) distribute(bastion string, keys ...string) *fixture {
	for _, k := range keys {
		id, ok := f.repoID[k]
		if !ok {
			panic("unknown repository " + k)
		}
		f.s.Distribution = append(f.s.Distribution, q.AuthzBuildDistributionRow{RepositoryID: id, BastionID: bastion})
	}
	return f
}

// through is the subject of a request that came through the Bastion (edge.SubjectOf sets it from the recognition).
func through(s Subject, bastion string) Subject { s.Bastion = bastion; return s }

// distributionFixture is the reach fixture with what the Bastion partners distributes: the locals npm-a, npm-strict, npm-pub and
// npm-open, the remote npm-frozen, and the virtual npm-v (over npm-a and the undistributed npmjs), npm-v2 (over npm-v and npm-frozen),
// npm-vpub and npm-vmix (over npm-pub and the undistributed internal npm-int). npm-b, npmjs, npm-int, npm-vb, npm-v3 and npm-empty are
// distributed nowhere. The Bastion public distributes npm-pub alone.
func distributionFixture() *fixture {
	return reachFixture().
		distribute(bastionPartners, "npm-a", "npm-strict", "npm-pub", "npm-open", "npm-frozen", "npm-v", "npm-v2", "npm-vpub", "npm-vmix").
		distribute(bastionPublic, "npm-pub")
}

// TestR_AUTHZ_043_ABastionReadsOnlyWhatItDistributes: through a Bastion a repository it does not distribute is refused to every
// subject - an administrator with content:bypass, a service account with a grant, the anonymous caller on a public repository - with
// the answer of a repository that does not exist (404 for a credential, 401 without one), byte for byte; a distributed one is decided
// as at the main installation's own door; no action but read passes; a subject without a Bastion is not bound.
func TestR_AUTHZ_043_ABastionReadsOnlyWhatItDistributes(t *testing.T) {
	e := newTestEngine(t, distributionFixture(), nil)
	ctx := t.Context()
	art := func(key string) Coordinate {
		return Coordinate{Repository: qr(key), Package: "x", Kind: KindArtifact, Actions: []Action{Read}}
	}
	cases := []struct {
		name   string
		sub    Subject
		c      Coordinate
		want   Outcome
		reason string
	}{
		{"an administrator reads a distributed repository", through(adminSub, bastionPartners), art("npm-a"), Allow, ReasonAllowRole},
		{"an administrator does not read an undistributed one", through(adminSub, bastionPartners), art("npm-b"), DenyHidden, ReasonDistribution},
		{"a grant reads a distributed repository", through(botSub, bastionPartners), art("npm-a"), Allow, ReasonAllowGrant},
		{"a grant does not read an undistributed one", through(botSub, bastionPartners), art("npmjs"), DenyHidden, ReasonDistribution},
		{"the anonymous caller reads a distributed public repository", through(anonSub, bastionPartners), art("npm-open"), Allow, ReasonAllowVisibility},
		{"the anonymous caller does not read a public repository the Bastion does not distribute", through(anonSub, bastionPublic), art("npm-open"), DenyUnauthenticated, ReasonDistribution},
		{"an internal repository leaves through no Bastion that does not distribute it", through(carolSub, bastionPartners), art("npm-int"), DenyHidden, ReasonDistribution},
		{"the Bastion's ceiling is its own: another Bastion's distribution does not count", through(botSub, bastionPublic), art("npm-a"), DenyHidden, ReasonDistribution},
		{"a write is refused on a distributed repository as on a missing one", through(adminSub, bastionPartners), write("npm-a", "x", "x/-/x-1.tgz", Deploy), DenyVisible, ReasonDistribution},
		{"the same subject at the main installation's door is not bound", botSub, art("npmjs"), Allow, ReasonAllowGrant},
		{
			"a member read through a distributed virtual repository is refused when the member is not distributed",
			through(botSub, bastionPartners),
			Coordinate{Repository: qr("npmjs"), Addressed: qr("npm-v"), Package: "x", Kind: KindArtifact, Actions: []Action{Read}},
			DenyHidden, ReasonDistribution,
		},
		{
			"a distributed member read through an undistributed virtual repository is refused",
			through(botSub, bastionPartners),
			Coordinate{Repository: qr("npm-a"), Addressed: qr("npm-v3"), Package: "x", Kind: KindArtifact, Actions: []Action{Read}},
			DenyHidden, ReasonDistribution,
		},
	}
	for _, tc := range cases {
		t.Run("R-AUTHZ-043 "+tc.name, func(t *testing.T) {
			d := e.Decide(ctx, tc.sub, tc.c)
			if d.Outcome != tc.want || (tc.reason != "" && d.Reason != tc.reason) {
				t.Fatalf("%+v, want outcome %d reason %s", d, tc.want, tc.reason)
			}
		})
	}
	t.Run("R-AUTHZ-043 a refused repository and a missing one answer the same error", func(t *testing.T) {
		for _, sub := range []Subject{through(adminSub, bastionPartners), through(anonSub, bastionPartners)} {
			refused, _ := errs.ProblemOf(e.Decide(ctx, sub, art("npm-b")).Err(), "/x", "r")
			missing, _ := errs.ProblemOf(e.Decide(ctx, sub, art("ghost")).Err(), "/x", "r")
			if fmt.Sprint(refused) != fmt.Sprint(missing) {
				t.Fatalf("%s: refused %+v, missing %+v", sub.Name, refused, missing)
			}
		}
	})
}

// TestR_AUTHZ_043_AnUndistributedMemberTakesNoPart: through a Bastion a virtual repository resolves only through the members the
// Bastion distributes. The resolver skips the other one (MemberUsable), the shape is that of the distributed members alone - neither
// narrowed nor widened by it - and the anonymous caller reads through a public virtual repository whose undistributed member is internal.
func TestR_AUTHZ_043_AnUndistributedMemberTakesNoPart(t *testing.T) {
	e := newTestEngine(t, distributionFixture(), nil)
	ctx := t.Context()
	bot := through(botSub, bastionPartners)
	idx, err := e.Begin(ctx, bot, qr("npm-v"))
	if err != nil {
		t.Fatal(err)
	}
	c := Coordinate{Repository: qr("npm-v"), Package: "x", Kind: KindArtifact, Actions: []Action{Read}}
	if read, _ := e.MemberUsableOn(ctx, idx, bot, qr("npm-a"), c); !read {
		t.Error("the distributed member npm-a is not used")
	}
	if read, fill := e.MemberUsableOn(ctx, idx, bot, qr("npmjs"), c); read || fill {
		t.Errorf("the undistributed member npmjs is used: read %v fill %v", read, fill)
	}
	if read, _ := e.MemberUsableOn(ctx, idx, botSub, qr("npmjs"), c); !read {
		t.Error("at the main installation's door the member npmjs is not used")
	}
	got, err := e.ReachOn(ctx, idx, bot, qrs([]string{"npm-v", "npm-vb"}))
	if err != nil {
		t.Fatal(err)
	}
	if got[qr("npm-v")].Reach != ReachFull {
		t.Errorf("npm-v through the Bastion: %s, want full (npmjs takes no part)", got[qr("npm-v")].Reach)
	}
	if r := got[qr("npm-vb")]; r.Reach != ReachNone || r.Denial != DenyHidden {
		t.Errorf("the undistributed npm-vb: %+v, want none and 404", r)
	}
	anon := through(anonSub, bastionPartners)
	if d := e.Decide(ctx, anon, Coordinate{Repository: qr("npm-vmix"), Package: "x", Kind: KindArtifact, Actions: []Action{Read}}); d.Outcome != Allow {
		t.Errorf("the anonymous caller through npm-vmix, whose internal member is not distributed: %+v", d)
	}
	if d := e.Decide(ctx, anonSub, Coordinate{Repository: qr("npm-vmix"), Package: "x", Kind: KindArtifact, Actions: []Action{Read}}); d.Outcome != DenyUnauthenticated {
		t.Errorf("the anonymous caller through npm-vmix at the main installation's door: %+v, want 401 (R-AUTHZ-009 rule 6)", d)
	}
}

// TestR_AUTHZ_043_ListsAndCountsAreFiltered: a listing through a Bastion holds only what the Bastion distributes - the readable
// keys, the keys a holder of repositories:read-config lists, the shapes a count is filtered by - while the same subject at the main
// installation's door lists every key it reads.
func TestR_AUTHZ_043_ListsAndCountsAreFiltered(t *testing.T) {
	e := newTestEngine(t, distributionFixture(), nil)
	ctx := t.Context()
	keys := qrs([]string{"npm-a", "npm-b", "npmjs", "npm-v", "npm-v3", "npm-pub"})
	want := qrs([]string{"npm-a", "npm-v", "npm-pub"})
	sub := through(adminSub, bastionPartners)
	idx, err := e.Begin(ctx, sub, keys...)
	if err != nil {
		t.Fatal(err)
	}
	readable, err := e.ReadableOn(ctx, idx, sub, keys)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(readable, want) {
		t.Errorf("readable through the Bastion: %v, want %v", readable, want)
	}
	if got := e.ExposedOn(idx, sub, keys); !slices.Equal(got, want) {
		t.Errorf("listed through the Bastion: %v, want %v", got, want)
	}
	if got, _ := e.ReadableOn(ctx, idx, adminSub, keys); !slices.Equal(got, keys) {
		t.Errorf("readable at the door: %v, want %v", got, keys)
	}
	rs, err := e.ReadScopeOn(ctx, idx, sub, qname.Repo{}, keys)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if got, distributed := rs.Shape(k), slices.Contains(want, k) && k.Key != "npm-v"; (got != ReadNone) != distributed {
			t.Errorf("shape of %s: %s, distributed %v", k, got, distributed)
		}
	}
	if rs.MayRead(qr("npm-b"), "x", "1.0.0") {
		t.Error("a row of the undistributed npm-b is readable through the Bastion")
	}
	through3, err := e.ReadScopeOn(ctx, idx, sub, qr("npm-v3"), qrs([]string{"npm-a", "npm-b"}))
	if err != nil {
		t.Fatal(err)
	}
	if through3.Shape(qr("npm-a")) != ReadNone {
		t.Error("a member is read through the undistributed virtual npm-v3")
	}
}

// TestR_AUTHZ_043_ShapeAgreesWithTheDecisionPointThroughABastion: the sweep of R-BAS-012 once more, every subject through the Bastion
// partners: a full reach never meets a refused read and a none reach never an allowed one, so the Bastion's check and its marking keep
// to the ceiling.
func TestR_AUTHZ_043_ShapeAgreesWithTheDecisionPointThroughABastion(t *testing.T) {
	e := newTestEngine(t, distributionFixture(), func(o *Options) { o.Ceiling = reachCeiling() })
	shapeAgrees(t, e, reachSubjects(), func(s Subject) Subject { return through(s, bastionPartners) })
}

// TestR_BAS_010_AnonymousReadsIsTheRegistrations: the fact behind a handshake's anonymousReads is per registration (Q-BAS-522): a
// Bastion that distributes a public repository has it, one that distributes only repositories that are not public, or nothing, does
// not.
func TestR_BAS_010_AnonymousReadsIsTheRegistrations(t *testing.T) {
	f := reachFixture().distribute(bastionPartners, "npm-a", "npm-int").distribute(bastionPublic, "npm-open")
	idx := BuildIndex(&f.s, testRegistry(), nil)
	if !idx.PublicContent() {
		t.Fatal("the fixture holds no public repository")
	}
	for bastion, want := range map[string]bool{bastionPublic: true, bastionPartners: false, "bastion-none": false} {
		if got := idx.PublicDistributed(bastion); got != want {
			t.Errorf("%s: %v, want %v", bastion, got, want)
		}
	}
}
