package limits

import (
	"net/netip"
	"testing"

	"github.com/guided-traffic/quader/internal/qname"
)

func builtinSnapshot() Snapshot {
	var s Snapshot
	for key, f := range BuiltinProfiles() {
		s.Profiles = append(s.Profiles, Profile{ID: "p-" + key, Key: key, Name: key, Builtin: true, Figures: f})
	}
	for _, e := range []struct{ who, profile string }{
		{EveryAdministrator, ProfileUnlimited},
		{EveryUser, ProfileStandard},
		{EveryTechnicalToken, ProfileTechnical},
		{EveryCI, ProfileTechnical},
		{EveryAnonymous, ProfileAnonymous},
	} {
		s.Rules = append(s.Rules, Rule{ID: "builtin-" + e.who, Place: InstancePlace(), WhoKind: WhoEvery, WhoRef: e.who, Profile: e.profile, Builtin: true})
	}
	s.Settings = DefaultSettings()
	return s
}

var (
	webNpmRemote = qname.Repo{Project: "web", Key: "npm-remote"}
	webNpmLocal  = qname.Repo{Project: "web", Key: "npm-local"}
	dockerhub    = qname.Repo{Project: "platform", Key: "dockerhub"}
	platformNpm  = qname.Repo{Project: "platform", Key: "npm"}
)

func repoPlace(r qname.Repo, id int64) Place {
	return Place{Kind: PlaceRepository, Repo: r, RepoID: id, ProjectKey: r.Project, ProjectID: "proj-" + r.Project}
}

func override(f Figure, v int64) Overrides {
	var o Overrides
	o.Set[f], o.Values[f] = true, v
	return o
}

// The worked examples of R-LIM-013: instance rules group web-devs → Standard, group contractors → Restricted, the built-ins;
// project web has no rules.
func workedIndex() *Index {
	s := builtinSnapshot()
	s.Rules = append(s.Rules,
		Rule{ID: "r-webdevs", Place: InstancePlace(), WhoKind: WhoGroup, WhoRef: "g-webdevs", Profile: ProfileStandard},
		Rule{ID: "r-contractors", Place: InstancePlace(), WhoKind: WhoGroup, WhoRef: "g-contractors", Profile: ProfileRestricted},
		Rule{
			ID: "r-repo-contractors", Place: repoPlace(webNpmRemote, 11), WhoKind: WhoGroup, WhoRef: "g-contractors",
			Profile: ProfileRestricted, Overrides: override(UncachedBytesPerHour, 20_000_000_000),
		},
		Rule{
			ID: "r-release", Place: repoPlace(webNpmLocal, 12), WhoKind: WhoTechnicalToken, WhoRef: "tt-release",
			Profile: ProfileTechnical, Overrides: override(ParallelDownloads, 100),
		},
		Rule{ID: "r-inst-24", Place: InstancePlace(), WhoKind: WhoAddress, WhoRef: "198.51.100.0/24", Profile: ProfileRestricted},
		Rule{
			ID: "r-hub-22", Place: repoPlace(dockerhub, 21), WhoKind: WhoAddress, WhoRef: "198.51.100.0/22",
			Profile: ProfileAnonymous, Overrides: override(UpstreamRequestsPerHour, 5),
		},
		Rule{
			ID: "r-ci-4711", Place: InstancePlace(), WhoKind: WhoCIProject, WhoRef: "gitlab-internal:4711",
			Profile: ProfileTechnical, Overrides: override(BytesPerDay, 50_000_000_000),
		},
		Rule{ID: "r-npm-remote-users", Place: repoPlace(webNpmRemote, 11), WhoKind: WhoEvery, WhoRef: EveryUser, Profile: ProfileRestricted},
	)
	return NewIndex(s)
}

// R-LIM-034, Q-LIM-52: the rule of a budget key the usage rows recorded is the one that applies at that place now.
func TestR_LIM_034_TheRuleOfABudgetKeyIsTheOneThatAppliesThereNow(t *testing.T) {
	x := workedIndex()
	release := Subject{Kind: KindTechnicalToken, Ref: "tt-release"}
	if res := x.ResolveAt(release, repoPlace(webNpmLocal, 12)); res.Rule == nil || res.Rule.ID != "r-release" || res.Figures[ParallelDownloads] != 100 {
		t.Fatalf("the repository's rule: %+v", res.Rule)
	}
	if res := x.ResolveAt(release, InstancePlace()); res.Rule == nil || res.Rule.ID != "builtin-"+EveryTechnicalToken {
		t.Fatalf("the instance's rule: %+v", res.Rule)
	}
	// A project without rules of its own falls through to the instance, as resolution does.
	if res := x.ResolveAt(release, Place{Kind: PlaceProject, ProjectKey: "web"}); res.Place.Kind != PlaceInstance {
		t.Fatalf("the project's place: %+v", res.Place)
	}
	admin := Subject{Kind: KindUser, Ref: "u-ada", Admin: true}
	if res := x.ResolveAt(admin, repoPlace(webNpmRemote, 11)); res.Rule == nil || res.Rule.ID != "builtin-"+EveryAdministrator {
		t.Fatalf("an administrator is decided at the instance: %+v", res.Rule)
	}
}

func TestR_LIM_013_WorkedExamples(t *testing.T) {
	x := workedIndex()
	std, restr, tech, anon := BuiltinProfiles()[ProfileStandard], BuiltinProfiles()[ProfileRestricted], BuiltinProfiles()[ProfileTechnical], BuiltinProfiles()[ProfileAnonymous]
	alice := Subject{Kind: KindUser, Ref: "u-alice", Groups: []string{"g-webdevs", "g-contractors"}}

	t.Run("R-LIM-013 a user in two groups and a group rule at a repository", func(t *testing.T) {
		res := x.Resolve(alice, webNpmRemote, false)
		want := restr
		want[UncachedBytesPerHour] = 20_000_000_000
		if res.Place.Kind != PlaceRepository || res.Rule.ID != "r-repo-contractors" || res.Figures != want {
			t.Fatalf("place %v rule %s figures %v, want the repository's contractors rule with 20 GB", res.Place, res.Rule.ID, res.Figures)
		}
		if got := BudgetKey(alice, res.Place); got != "user:u-alice@repository:11" {
			t.Fatalf("budget key %s", got)
		}
		res = x.Resolve(alice, webNpmLocal, false)
		if res.Place.Kind != PlaceInstance || res.Figures != std || len(res.GroupRules) != 2 {
			t.Fatalf("npm-local: place %v figures %v groups %d, want the instance with Standard from two group rules", res.Place, res.Figures, len(res.GroupRules))
		}
	})

	t.Run("R-LIM-013 a technical token of a repository", func(t *testing.T) {
		tok := Subject{Kind: KindTechnicalToken, Ref: "tt-release"}
		res := x.Resolve(tok, webNpmLocal, false)
		want := tech
		want[ParallelDownloads] = 100
		if res.Rule.ID != "r-release" || res.Figures != want {
			t.Fatalf("rule %s figures %v", res.Rule.ID, res.Figures)
		}
		res = x.Resolve(tok, webNpmRemote, false)
		if res.Place.Kind != PlaceInstance || res.Rule.WhoRef != EveryTechnicalToken || res.Figures != tech {
			t.Fatalf("npm-remote: place %v rule %v", res.Place, res.Rule)
		}
	})

	t.Run("R-LIM-013 an anonymous address inside a range", func(t *testing.T) {
		in := Subject{Kind: KindAnonymous, Ref: "198.51.100.23"}
		res := x.Resolve(in, dockerhub, false)
		want := anon
		want[UpstreamRequestsPerHour] = 5
		if res.Rule.ID != "r-hub-22" || res.Figures != want {
			t.Fatalf("dockerhub: rule %v figures %v", res.Rule, res.Figures)
		}
		if res = x.Resolve(in, platformNpm, false); res.Rule.ID != "r-inst-24" || res.Figures != restr {
			t.Fatalf("platform/npm: rule %v", res.Rule)
		}
		out := Subject{Kind: KindAnonymous, Ref: "203.0.113.9"}
		if res = x.Resolve(out, platformNpm, false); res.Rule.WhoRef != EveryAnonymous || res.Figures != anon {
			t.Fatalf("203.0.113.9: rule %v", res.Rule)
		}
	})

	t.Run("R-LIM-013 a CI identity", func(t *testing.T) {
		job := Subject{Kind: KindCI, Ref: "gitlab-internal:4711"}
		want := tech
		want[BytesPerDay] = 50_000_000_000
		if res := x.Resolve(job, platformNpm, false); res.Rule.ID != "r-ci-4711" || res.Figures != want {
			t.Fatalf("4711: rule %v", res.Rule)
		}
		other := Subject{Kind: KindCI, Ref: "gitlab-internal:4712"}
		if res := x.Resolve(other, platformNpm, false); res.Rule.WhoRef != EveryCI || res.Figures != tech {
			t.Fatalf("4712: rule %v", res.Rule)
		}
	})

	t.Run("R-LIM-013 an administrator is decided at the instance", func(t *testing.T) {
		ada := Subject{Kind: KindUser, Ref: "u-ada", Admin: true}
		res := x.Resolve(ada, webNpmRemote, false)
		if res.Place.Kind != PlaceInstance || res.Rule.WhoRef != EveryAdministrator || !res.Figures.AllUnlimited() {
			t.Fatalf("ada: place %v rule %v figures %v", res.Place, res.Rule, res.Figures)
		}
		bob := Subject{Kind: KindUser, Ref: "u-bob"}
		if res := x.Resolve(bob, webNpmRemote, false); res.Rule.ID != "r-npm-remote-users" {
			t.Fatalf("bob: rule %v", res.Rule)
		}
	})

	t.Run("R-LIM-013 a user removed from a group is decided by the new group set", func(t *testing.T) {
		after := alice
		after.Groups = []string{"g-webdevs"}
		if res := x.Resolve(after, webNpmRemote, false); res.Rule.ID != "r-npm-remote-users" || res.Figures != restr {
			t.Fatalf("rule %v", res.Rule)
		}
	})

	t.Run("R-LIM-013 a request that names no repository is decided at the instance", func(t *testing.T) {
		if res := x.Resolve(Subject{Kind: KindUser, Ref: "u-bob"}, qname.Repo{}, true); res.Place.Kind != PlaceInstance || len(res.Steps) != 3 {
			t.Fatalf("place %v steps %d", res.Place, len(res.Steps))
		}
	})
}

func TestR_LIM_016_TheClamp(t *testing.T) {
	std, tech := BuiltinProfiles()[ProfileStandard], BuiltinProfiles()[ProfileTechnical]
	base := builtinSnapshot()
	base.Rules = append(base.Rules, Rule{ID: "r-webdevs", Place: InstancePlace(), WhoKind: WhoGroup, WhoRef: "g-webdevs", Profile: ProfileStandard})
	projWeb := Place{Kind: PlaceProject, ProjectKey: "web", ProjectID: "proj-web"}
	ownerRule := Rule{ID: "r-owner", Place: projWeb, WhoKind: WhoGroup, WhoRef: "g-webdevs", Profile: ProfileUnlimited, Capped: true}

	t.Run("R-LIM-016 without a maximum an Owner is capped at the instance's value", func(t *testing.T) {
		x := NewIndex(base)
		if cap := x.CapFor(ownerRule, nil); cap != std {
			t.Fatalf("cap %v, want Standard", cap)
		}
	})
	t.Run("R-LIM-016 with the maximum Technical the cap is Technical", func(t *testing.T) {
		s := base
		s.Maxima = []Maximum{{ProjectID: "proj-web", ProjectKey: "web", Profile: ProfileTechnical}}
		x := NewIndex(s)
		if cap := x.CapFor(ownerRule, nil); cap != tech {
			t.Fatalf("cap %v, want Technical", cap)
		}
	})
	t.Run("R-LIM-016 a lowered maximum clamps a stored capped rule at the next request", func(t *testing.T) {
		s := base
		stored := ownerRule
		stored.Profile = ProfileTechnical
		s.Rules = append(append([]Rule(nil), s.Rules...), stored)
		s.Maxima = []Maximum{{ProjectID: "proj-web", ProjectKey: "web", Profile: ProfileStandard}}
		x := NewIndex(s)
		res := x.Resolve(Subject{Kind: KindUser, Ref: "u-x", Groups: []string{"g-webdevs"}}, webNpmLocal, false)
		if res.Rule.ID != "r-owner" || res.Figures != std {
			t.Fatalf("rule %v figures %v, want r-owner with Standard", res.Rule, res.Figures)
		}
		clampedAny := false
		for _, c := range res.Clamped {
			clampedAny = clampedAny || c
		}
		if !clampedAny {
			t.Fatal("no figure is marked clamped")
		}
		eff, clamped := x.EffectiveOf(stored)
		if eff != std || !clamped[RequestsPerSecond] {
			t.Fatalf("EffectiveOf %v %v", eff, clamped)
		}
	})
	t.Run("R-LIM-016 a rule of limits:write is not clamped", func(t *testing.T) {
		s := base
		uncapped := ownerRule
		uncapped.Capped = false
		s.Rules = append(append([]Rule(nil), s.Rules...), uncapped)
		x := NewIndex(s)
		res := x.Resolve(Subject{Kind: KindUser, Ref: "u-x", Groups: []string{"g-webdevs"}}, webNpmLocal, false)
		if !res.Figures.AllUnlimited() {
			t.Fatalf("figures %v, want unlimited", res.Figures)
		}
	})
}

func TestR_LIM_002_AddressPrefix(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"198.51.100.7", "198.51.100.7"},
		{"::ffff:198.51.100.7", "198.51.100.7"},
		{"2001:db8:1:2::10", "2001:db8:1:2::/64"},
		{"2001:db8:1:2:ffff::1", "2001:db8:1:2::/64"},
		{"2001:db8:1:3::1", "2001:db8:1:3::/64"},
		{"fe80::1%eth0", "fe80::/64"},
	} {
		if got := AddressPrefix(netip.MustParseAddr(c.in)); got != c.want {
			t.Errorf("%s: %s, want %s", c.in, got, c.want)
		}
	}
	if got := AddressPrefix(netip.Addr{}); got != UnknownAddress {
		t.Errorf("no address: %s", got)
	}
	for _, c := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"198.51.100.77/24", "198.51.100.0/24", true},
		{"10.0.0.0/7", "", false},
		{"2001:db8::/48", "2001:db8::/48", true},
		{"2001:db8::/65", "", false},
		{"2001:db8::1", "", false},
		{"198.51.100.7", "198.51.100.7/32", true},
	} {
		p, ok := CanonicalPrefix(c.in)
		if ok != c.ok || (ok && p.String() != c.want) {
			t.Errorf("%s: %v %v, want %s %v", c.in, p, ok, c.want, c.ok)
		}
	}
}

func TestR_LIM_001_TheSubjectOfEveryCaller(t *testing.T) {
	client := netip.MustParseAddr("198.51.100.7")
	cases := []struct {
		name string
		c    Caller
		want string
		cred string
	}{
		{"session", Caller{Kind: CallerUser, PrincipalID: "u-alice", SessionID: "s1", Client: client}, "user:u-alice", SessionCredential},
		{"personal token", Caller{Kind: CallerUser, PrincipalID: "u-alice", TokenID: "tok-a", Client: client}, "user:u-alice", "tok-a"},
		{"technical token", Caller{Kind: CallerServiceAccount, PrincipalID: "sa-1", TokenID: "tok-x", TechnicalTokenID: "tt-1", Client: client}, "technical_token:tt-1", ""},
		{"ci", Caller{Kind: CallerCI, PrincipalID: "ci:x", CIIssuerID: "iss-1", CIProjectID: "4711", Client: client}, "ci:iss-1:4711", ""},
		{"anonymous", Caller{Kind: CallerAnonymous, Anonymous: true, Client: client}, "anonymous:198.51.100.7", ""},
		{"break-glass", Caller{Kind: CallerBreakGlass, PrincipalID: "bg", SessionID: "s", Client: client}, "user:bg", SessionCredential},
		{"an unknown kind is the address", Caller{Kind: "future", PrincipalID: "x", Client: client}, "anonymous:198.51.100.7", ""},
		{"a service account without a technical token is the address", Caller{Kind: CallerServiceAccount, PrincipalID: "sa", TokenID: "t", Client: client}, "anonymous:198.51.100.7", ""},
	}
	for _, c := range cases {
		s, _ := SubjectOf(c.c)
		if s.String() != c.want || s.Credential != c.cred {
			t.Errorf("%s: %s (%q), want %s (%q)", c.name, s, s.Credential, c.want, c.cred)
		}
	}
	if s, _ := SubjectOf(Caller{Kind: CallerBreakGlass, PrincipalID: "bg"}); !s.Admin {
		t.Error("the break-glass administrator is an administrator")
	}
}

// TestR_CLAMAV_010_TheCertificateSubjectsBudget: a certificate subject is charged to the certificate's subject with its serial as the
// credential, and a certificate without a subject to the address (Q-CLAMAV-12); no rule names it, so the kind default of the technical
// tokens applies (Q-LIM-96).
func TestR_CLAMAV_010_TheCertificateSubjectsBudget(t *testing.T) {
	client := netip.MustParseAddr("198.51.100.7")
	s, ok := SubjectOf(Caller{Kind: CallerCertificate, CertSubject: "CN=scan-01.example.com", CertSerial: "a1b2c", Client: client})
	if !ok || s.Kind != KindCertificate || s.String() != "certificate:CN=scan-01.example.com" || s.Credential != "a1b2c" || s.Kind.APIName() != "certificate" {
		t.Fatalf("subject %+v", s)
	}
	if s, ok := SubjectOf(Caller{Kind: CallerCertificate, Client: client}); ok || s.Kind != KindAnonymous {
		t.Fatalf("a certificate without a subject: %+v", s)
	}
	if k, ok := ParseSubjectKind("certificate"); !ok || k != KindCertificate {
		t.Fatalf("parse: %v %v", k, ok)
	}
	if res := workedIndex().Resolve(s, qname.Repo{Project: "upstream", Key: "clamav"}, false); res.Rule.WhoRef != EveryTechnicalToken {
		t.Fatalf("rule %+v", res.Rule)
	}
}

func TestR_LIM_004_Figures(t *testing.T) {
	var f Figures
	f[RequestsBurst] = 10
	if _, _, ok := f.Validate(); ok {
		t.Error("a burst without a rate is accepted")
	}
	f[RequestsPerSecond] = 20
	if _, _, ok := f.Validate(); ok {
		t.Error("a burst below the rate is accepted")
	}
	f = Figures{RequestsPerSecond: 20}
	if f.Burst() != 20 {
		t.Errorf("burst %d, want the rate", f.Burst())
	}
	a, b := Figures{5, 0, 7}, Figures{9, 3, 2}
	if g := MostGenerous(a, b); g[0] != 9 || g[1] != 0 || g[2] != 7 {
		t.Errorf("most generous %v", g)
	}
	raw, _ := BuiltinProfiles()[ProfileAnonymous].MarshalJSON()
	var back Figures
	if err := back.UnmarshalJSON(raw); err != nil || back != BuiltinProfiles()[ProfileAnonymous] {
		t.Errorf("round trip %v %v", back, err)
	}
}
