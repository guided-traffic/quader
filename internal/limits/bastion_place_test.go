package limits

import (
	"testing"
)

// bastionIndex is the instance's built-ins and the place of the registration b-partners under the preset Internet, with a rule of
// its own for the technical token tt-partner and an anonymous /24.
func bastionIndex() *Index {
	s := builtinSnapshot()
	partners := BastionPlace("b-partners", "partners")
	for _, e := range []struct{ who, profile string }{
		{EveryAnonymous, ProfileBastionAnonymous},
		{EveryUser, ProfileBastionSignedIn},
		{EveryTechnicalToken, ProfileBastionSignedIn},
		{EveryCI, ProfileBastionSignedIn},
		{EveryAdministrator, ProfileBastionSignedIn},
	} {
		s.Rules = append(s.Rules, Rule{ID: "kind-" + e.who, Place: partners, WhoKind: WhoEvery, WhoRef: e.who, Profile: e.profile, Builtin: true})
	}
	s.Rules = append(s.Rules,
		Rule{ID: "r-partner-token", Place: partners, WhoKind: WhoTechnicalToken, WhoRef: "tt-partner", Profile: ProfileTechnical},
		Rule{ID: "r-partner-24", Place: partners, WhoKind: WhoAddress, WhoRef: "198.51.100.0/24", Profile: ProfileRestricted},
	)
	return NewIndex(s)
}

// TestR_LIM_043_ABastionsPlaceResolvesAlone: a rule for a technical token at the place of partners applies to that token through
// partners and not at the main installation; an administrator through partners under the preset Internet gets "Bastion: signed-in",
// not the instance's Unlimited; an anonymous /24 rule at the place wins over its kind rule for an address inside it; a request of the
// main installation never resolves at a Bastion's place, and the budget key names the place. Milliseconds, no data.
func TestR_LIM_043_ABastionsPlaceResolvesAlone(t *testing.T) {
	x := bastionIndex()
	token := Subject{Kind: KindTechnicalToken, Ref: "tt-partner"}
	if res := x.ResolveBastion(token, "b-partners"); res.Rule == nil || res.Rule.ID != "r-partner-token" || res.Place.Kind != PlaceBastion {
		t.Fatalf("the token through partners: %+v", res.Rule)
	}
	if res := x.Resolve(token, webNpmLocal, false); res.Rule == nil || res.Rule.ID != "builtin-"+EveryTechnicalToken {
		t.Fatalf("the token at the main installation: %+v", res.Rule)
	}
	admin := Subject{Kind: KindUser, Ref: "u-admin", Admin: true}
	if res := x.ResolveBastion(admin, "b-partners"); res.Rule == nil || res.Rule.Profile != ProfileBastionSignedIn ||
		res.Figures != BuiltinProfiles()[ProfileBastionSignedIn] {
		t.Fatalf("an administrator through partners: %+v", res.Rule)
	}
	if res := x.ResolveAt(admin, InstancePlace()); res.Rule == nil || res.Rule.Profile != ProfileUnlimited {
		t.Fatalf("an administrator at the instance: %+v", res.Rule)
	}
	in, out := Subject{Kind: KindAnonymous, Ref: "198.51.100.7"}, Subject{Kind: KindAnonymous, Ref: "203.0.113.7"}
	if res := x.ResolveBastion(in, "b-partners"); res.Rule == nil || res.Rule.ID != "r-partner-24" {
		t.Fatalf("an address inside the /24: %+v", res.Rule)
	}
	if res := x.ResolveBastion(out, "b-partners"); res.Rule == nil || res.Rule.ID != "kind-"+EveryAnonymous {
		t.Fatalf("an address outside the /24: %+v", res.Rule)
	}
	if res := x.ResolveBastion(token, "b-unknown"); res.Rule != nil {
		t.Fatalf("a place the index does not hold: %+v", res.Rule)
	}
	if k := BudgetKey(token, BastionPlace("b-partners", "partners")); k != "technical_token:tt-partner@bastion:b-partners" {
		t.Fatalf("the budget key %q", k)
	}
	if n := BastionPlace("b-partners", "partners").LogName(); n != "bastion:partners" {
		t.Fatalf("the log name %q", n)
	}
}

// TestR_LIM_046_TheBastionProfilesHoldOneCommandOfEveryClient: the figures of "Bastion: anonymous" (per address) and "Bastion:
// signed-in" (per subject) hold, with a third of room, each command measured through a cold Bastion on 2026-11-26
// (developer/bastion.md) and the hour's uses R-LIM-046's table promises each figure; a rate per second is the pace the table names, a
// first pull of 1 GB in so many seconds, without room. A figure lowered below one fails here. Milliseconds, no data.
func TestR_LIM_046_TheBastionProfilesHoldOneCommandOfEveryClient(t *testing.T) {
	type use struct {
		what string
		fig  Figure
		need int64
	}
	room := func(n int64) int64 { return n + n/3 } // a third of room
	paced := map[Figure]bool{CachedBytesPerSecond: true, UncachedBytesPerSecond: true}
	for profile, uses := range map[string][]use{
		ProfileBastionAnonymous: {
			// one command at once (measured): requests within the burst, transfers at once within the parallel figure
			{"a cold anonymous dotnet restore of 200 packages: 441 requests", RequestsBurst, 441},
			{"a cold anonymous dotnet restore of 200 packages: 200 at once", ParallelTransfers, 200},
			{"a cold npm ci of 475 packages: 475 requests", RequestsBurst, 475},
			{"a docker pull of 40 layers: 43 requests", RequestsBurst, 43},
			{"podman pull of 40 layers: 44 requests, 6 at once", ParallelTransfers, 6},
			{"a cold npm ci and a cold restore at once (475 + 441)", RequestsBurst, 916},
			// the hour of one address (derived from the measured counts and R-LIM-042's sizes)
			{"thirty npm ci of 475 packages", RequestsPerHour, 30 * 475},
			{"four cold npm ci, two cold restores, five first pulls of 1 GB, five apt-get of 106 packages, ten Galaxy installs", UpstreamRequestsPerHour, 3857},
			{"the same uses' bytes from the main installation (6.6 GB)", UncachedBytesPerHour, 6600 * mb},
			{"a first pull of 1 GB in 20 s", UncachedBytesPerSecond, 1 * gb / 20},
			{"twenty pulls of 1 GB from the Bastion's cache", CachedBytesPerHour, 20 * gb},
			{"one pull of 1 GB from the cache in 10 s", CachedBytesPerSecond, 1 * gb / 10},
			{"twenty cached pulls of 1 GB an hour for eight hours", BytesPerDay, 160 * gb},
			{"fifteen npm ci on an expired token, 15 refusals each", RefusedPerHour, 225},
		},
		ProfileBastionSignedIn: {
			{"a cold dotnet restore of 200 packages with a credential: 802 requests", RequestsBurst, 802},
			{"a docker pull of 40 layers with its login: 48 requests", RequestsBurst, 48},
			{"an install of 4,000 packages at once", RequestsBurst, 4000},
			{"a restore of 200, an npm ci, five podman pulls and a docker pull at once (200 + 15 + 30 + 3)", ParallelTransfers, 248},
			{"twenty installs of 3,000 packages, or fifty restores of 300 with their challenges", RequestsPerHour, 60100},
			{"two cold installs of 4,000, a cold restore of 300, the first pulls of twelve 1 GB images", UpstreamRequestsPerHour, 8781},
			{"1.5 GB of npm, 0.45 GB of NuGet, 12 GB of images from the main installation", UncachedBytesPerHour, 13900 * mb},
			{"a first pull of 1 GB in 8 s", UncachedBytesPerSecond, 1 * gb / 8},
			{"a compose file of twelve 1 GB images pulled ten times from the cache", CachedBytesPerHour, 120 * gb},
			{"one pull of 1 GB from the cache in 4 s", CachedBytesPerSecond, 1 * gb / 4},
			{"the compose file's 120 GB an hour for eight hours", BytesPerDay, 960 * gb},
			{"forty installs that fail on a token without read", RefusedPerHour, 40 * 15},
		},
	} {
		f := BuiltinProfiles()[profile]
		for _, u := range uses {
			if f[u.fig] == 0 {
				t.Errorf("%s: %s is unlimited; no figure of a Bastion profile is", profile, u.fig)
				continue
			}
			need := room(u.need)
			if paced[u.fig] {
				need = u.need
			}
			if need > f[u.fig] {
				t.Errorf("%s: %s needs %d, the figure is %d (%s)", profile, u.fig, need, f[u.fig], u.what)
			}
		}
		for i, v := range f {
			if v <= 0 {
				t.Errorf("%s: %s is unlimited", profile, Figure(i))
			}
		}
	}
}
