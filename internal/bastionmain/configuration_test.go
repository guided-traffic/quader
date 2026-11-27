package bastionmain

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/sysstate"
)

// presetRules are the five kind rules of a preset, each rule named after its kind.
func presetRules(p iface.LimitsPreset) []PlaceRule {
	var out []PlaceRule
	for _, kind := range kindOrder() {
		profile := Presets()[p].Kinds[kind]
		out = append(out, PlaceRule{
			ID: "kind-" + kind, WhoKind: limits.WhoEvery, WhoRef: kind, Profile: profile, Builtin: true, Figures: limits.BuiltinProfiles()[profile],
		})
	}
	return out
}

// TestR_LIM_046_EachPresetWritesTheTablesKindRulesAndFloodFigures: the three presets give the kinds the profiles of R-LIM-046's
// table, the flood figures of the table and Enforce; a registration reads custom once a kind rule names another profile or carries
// an override, or a flood figure differs from its preset's. Milliseconds, no data.
func TestR_LIM_046_EachPresetWritesTheTablesKindRulesAndFloodFigures(t *testing.T) {
	want := map[iface.LimitsPreset][5]string{ // anonymous, user, technical token, CI, administrator
		iface.PresetInternet: {
			limits.ProfileBastionAnonymous, limits.ProfileBastionSignedIn, limits.ProfileBastionSignedIn,
			limits.ProfileBastionSignedIn, limits.ProfileBastionSignedIn,
		},
		iface.PresetPartnerNetwork: {
			limits.ProfileAnonymous, limits.ProfileStandard, limits.ProfileTechnical, limits.ProfileTechnical,
			limits.ProfileUnlimited,
		},
		iface.PresetInternal: {
			limits.ProfileUnlimited, limits.ProfileUnlimited, limits.ProfileUnlimited, limits.ProfileUnlimited,
			limits.ProfileUnlimited,
		},
	}
	for name, kinds := range want {
		p := Presets()[name]
		for i, kind := range kindOrder() {
			if p.Kinds[kind] != kinds[i] {
				t.Errorf("%s: %s gets %s, want %s", name, kind, p.Kinds[kind], kinds[i])
			}
		}
		f := p.Flood
		if p.Mode != iface.ModeEnforce || f.Anonymous != (iface.FloodClass{RequestsPerSecond: 50, Burst: 1000, MissesPerMinute: 1000}) ||
			f.Authenticated != (iface.FloodClass{RequestsPerSecond: 200, Burst: 4000, MissesPerMinute: 4000}) || f.GUI != (iface.FloodRate{RequestsPerSecond: 20, Burst: 100}) {
			t.Errorf("%s: mode %s, flood %+v", name, p.Mode, f)
		}
		l := DefaultLimits()
		l.Preset = name
		if got := EffectivePreset(l, presetRules(name)); got != name {
			t.Errorf("%s reads %s", name, got)
		}
	}
	l := DefaultLimits()
	rules := presetRules(iface.PresetInternet)
	changed := slices.Clone(rules)
	changed[1].Profile = limits.ProfileStandard
	overridden := slices.Clone(rules)
	overridden[0].Overrides.Set[limits.RequestsPerHour], overridden[0].Overrides.Values[limits.RequestsPerHour] = true, 10
	flood := l
	flood.Flood.Anonymous.RequestsPerSecond = 51
	for what, got := range map[string]iface.LimitsPreset{
		"a kind rule with another profile": EffectivePreset(l, changed),
		"a kind rule with an override":     EffectivePreset(l, overridden),
		"another flood figure":             EffectivePreset(flood, rules),
		"a place without its kind rules":   EffectivePreset(l, rules[:4]),
	} {
		if got != iface.PresetCustom {
			t.Errorf("%s reads %s, want custom", what, got)
		}
	}
}

// TestR_BAS_093_TheConfigurationCarriesEachRulesEffectiveFigures: the handshake's member of a registration names every rule of its
// place with the profile's figures and its overrides, its address rules canonical and in address order, its five kind rules, the
// preset the registration reads, and holds the Bastion's rules; a place without a kind rule is an error, never a guess. Milliseconds.
func TestR_BAS_093_TheConfigurationCarriesEachRulesEffectiveFigures(t *testing.T) {
	rules := presetRules(iface.PresetInternet)
	rules = append(rules,
		PlaceRule{ID: "r-24", WhoKind: limits.WhoAddress, WhoRef: "198.51.100.0/24", Profile: limits.ProfileRestricted, Figures: limits.BuiltinProfiles()[limits.ProfileRestricted]},
		PlaceRule{ID: "r-a", WhoKind: limits.WhoAddress, WhoRef: "192.0.2.7", Profile: limits.ProfileUnlimited},
		PlaceRule{
			ID: "r-token", WhoKind: limits.WhoTechnicalToken, WhoRef: "tt-1", Profile: limits.ProfileTechnical,
			Figures: limits.BuiltinProfiles()[limits.ProfileTechnical], Overrides: func() limits.Overrides {
				var o limits.Overrides
				o.Set[limits.ParallelTransfers], o.Values[limits.ParallelTransfers] = true, 12
				return o
			}(),
		},
	)
	c, err := Configuration(17, iface.Switches{Handover: true}, DefaultLimits(), rules)
	if err != nil {
		t.Fatal(err)
	}
	p := c.Limits.Place
	if c.Revision != 17 || !c.Switches.Handover || c.Limits.Preset != iface.PresetInternet || p.KindRules.Administrator != "kind-"+limits.EveryAdministrator {
		t.Fatalf("%+v", c)
	}
	if len(p.AddressRules) != 2 || p.AddressRules[0] != (iface.AddressRule{Prefix: "192.0.2.7/32", Rule: "r-a"}) || p.AddressRules[1].Prefix != "198.51.100.0/24" {
		t.Fatalf("address rules %+v", p.AddressRules)
	}
	if f := p.Rules["r-token"]; f.ParallelTransfers == nil || *f.ParallelTransfers != 12 || f.RequestsPerSecond == nil || *f.RequestsPerSecond != 1000 {
		t.Fatalf("the token's figures %+v", f)
	}
	if f := p.Rules["r-a"]; f.RequestsPerSecond != nil || f.BytesPerDay != nil {
		t.Fatalf("an Unlimited rule carries a figure: %+v", f)
	}
	if _, err := Configuration(1, iface.Switches{}, DefaultLimits(), rules[1:]); err == nil {
		t.Fatal("a place without the anonymous kind rule was sent")
	}
	if err := ValidateLimits(DefaultLimits()); err != nil {
		t.Fatalf("the default limits are refused: %v", err)
	}
	bad := DefaultLimits()
	bad.Slots.AnonymousShare = 2
	if ValidateLimits(bad) == nil {
		t.Fatal("a share of 2 passed")
	}
	long := DefaultLimits()
	long.Flood.FailedAuth.WindowSeconds = MaxFailedAuthWindowSeconds + 1
	if ValidateLimits(long) == nil {
		t.Fatal("a failed-authentication window above 12 hours passed")
	}
}

// TestR_BAS_096_TheSubjectsLimitsNameItsKeyAndItsRuleAtThePlace: a signed-in subject's key and the rule the index resolves at the
// registration's place, its own rule before the kind rule; nothing for an anonymous caller or a place the index does not hold.
func TestR_BAS_096_TheSubjectsLimitsNameItsKeyAndItsRuleAtThePlace(t *testing.T) {
	var s limits.Snapshot
	for key, f := range limits.BuiltinProfiles() {
		s.Profiles = append(s.Profiles, limits.Profile{Key: key, Figures: f})
	}
	place := limits.BastionPlace("0192f1c4-0000-7000-8000-0000000000b1", "partners")
	for _, r := range presetRules(iface.PresetInternet) {
		s.Rules = append(s.Rules, limits.Rule{ID: "0193a0c4-0000-7000-8000-0000000000" + r.WhoRef[:2], Place: place, WhoKind: r.WhoKind, WhoRef: r.WhoRef, Profile: r.Profile})
	}
	s.Rules = append(s.Rules, limits.Rule{ID: "0193a0c4-0000-7000-8000-00000000aaaa", Place: place, WhoKind: limits.WhoUser, WhoRef: "u-1", Profile: limits.ProfileStandard})
	x := limits.NewIndex(s)
	if got := SubjectLimits(x, limits.Subject{Kind: limits.KindUser, Ref: "u-1"}, place.BastionID); got == nil || got.Key != "user:u-1" || got.Rule != "0193a0c4-0000-7000-8000-00000000aaaa" {
		t.Fatalf("a user with a rule of its own: %+v", got)
	}
	if got := SubjectLimits(x, limits.Subject{Kind: limits.KindUser, Ref: "u-2"}, place.BastionID); got == nil || got.Rule != "0193a0c4-0000-7000-8000-0000000000us" {
		t.Fatalf("a user by the kind rule: %+v", got)
	}
	if got := SubjectLimits(x, limits.Subject{Kind: limits.KindAnonymous, Ref: "198.51.100.7"}, place.BastionID); got != nil {
		t.Fatalf("an anonymous caller: %+v", got)
	}
	if got := SubjectLimits(x, limits.Subject{Kind: limits.KindUser, Ref: "u-1"}, "another"); got != nil {
		t.Fatalf("a place the index does not hold: %+v", got)
	}
}

type deltas []limits.UsageDelta

func (d *deltas) Add(x limits.UsageDelta) { *d = append(*d, x) }

// TestR_LIM_045_ABatchsUsageGoesToTheBuffer: three usage rows and an overflow row of a batch go to the usage buffer at the place of
// the registration, the subject from its key and the limit events in their mode; a row whose subject does not read, or that is older
// than max_age, is dropped and counted; a batch refused for its events stores no usage, so the batch sent again is not counted twice.
// Milliseconds, no database.
func TestR_LIM_045_ABatchsUsageGoesToTheBuffer(t *testing.T) {
	var sink deltas
	now := time.Date(2026, 11, 26, 12, 7, 0, 0, time.UTC)
	refuse := false
	in := NewIntake(IntakeOptions{
		MaxBatch: 10, MaxAge: time.Hour, Record: true, Now: func() time.Time { return now },
		Events: func(context.Context, string, int) (bool, time.Duration) { return !refuse, time.Second },
		Usage:  UsageOptions{Sink: &sink, RuleFigures: func(string, string) (limits.Figures, bool) { return limits.Figures{limits.RequestsPerHour: 9}, true }},
	})
	rec := Recognition{Bastion: "partners", BastionID: "b-1", Replica: "r-1"}
	bucket := now.Truncate(5 * time.Minute)
	req := iface.DownloadsRequest{Usage: iface.UsageReport{BucketSeconds: 300, Rows: []iface.UsageRow{
		{BucketStart: bucket, Subject: "user:u-1", Credential: "tok-1", Repository: "web/npm", Rule: "r-1", Requests: 5, Limited: map[string]int64{"cachedBytesPerHour": 1}},
		{BucketStart: bucket, Subject: "technical_token:tt-1", Credential: "secret-1", Repository: "web/npm", Requests: 2, WouldLimit: map[string]int64{"requestsPerHour": 3}},
		{BucketStart: bucket, Subject: "anonymous:198.51.100.7", Repository: "web/npm", Requests: 1},
		{BucketStart: bucket, Subject: "system", Repository: "web/npm", Requests: 1},
		{BucketStart: now.Add(-2 * time.Hour), Subject: "user:u-1", Repository: "web/npm", Requests: 1},
	}, Overflow: []iface.UsageOverflow{{BucketStart: bucket, SubjectKind: "anonymous", Repository: "web/npm", Subjects: 40, Requests: 400}}}}
	req.Events = []iface.DownloadEvent{{At: now, Repository: "web/npm", Path: "/x", SHA256: "0000000000000000000000000000000000000000000000000000000000000000"}}

	refuse = true
	if _, err := in.Accept(context.Background(), rec, req); errs.KindOf(err) != errs.RateLimited || len(sink) != 0 {
		t.Fatalf("a refused batch: %v, %d usage rows stored", err, len(sink))
	}
	refuse = false
	out, err := in.Accept(context.Background(), rec, req)
	if err != nil || out.UsageDropped != 2 || len(sink) != 4 {
		t.Fatalf("%+v %v: %d rows", out, err, len(sink))
	}
	for _, d := range sink {
		if d.Place.Key() != "bastion:b-1" || d.RepoID != 0 {
			t.Fatalf("a row at %s, repository %d", d.Place.Key(), d.RepoID)
		}
	}
	if u := sink[0]; u.Subject.Kind != limits.KindUser || u.Subject.Credential != "tok-1" || u.Limits[limits.RequestsPerHour] != 9 ||
		len(u.Events) != 1 || u.Events[0] != (limits.EventDelta{Figure: "cachedBytesPerHour", Mode: limits.ModeEnforce, Count: 1}) {
		t.Fatalf("the user's row %+v", u)
	}
	if tt := sink[1]; tt.Subject.Kind != limits.KindTechnicalToken || tt.Subject.Credential != "" || tt.Events[0].Mode != limits.ModeAudit {
		t.Fatalf("the token's row %+v", tt)
	}
	if ov := sink[3]; ov.Subject.Ref != "overflow" || ov.Subject.Kind != limits.KindAnonymous || ov.Requests != 400 {
		t.Fatalf("the overflow row %+v", ov)
	}
}

// TestR_BAS_128_TheConfigurationNamesTheFormsOfTheMainInstallation: the forms of a time the interface admits are the main
// installation's, in its order, the factory default first, and a registration's configuration carries the factory form until the
// composition root sets the installation's.
func TestR_BAS_128_TheConfigurationNamesTheFormsOfTheMainInstallation(t *testing.T) {
	if !slices.Equal(iface.TimeFormats(), sysstate.TimeFormats()) || iface.FactoryTimeFormat != sysstate.FactoryTimeFormat {
		t.Fatalf("the interface's forms %v (%s) are not the main installation's %v (%s)", iface.TimeFormats(), iface.FactoryTimeFormat,
			sysstate.TimeFormats(), sysstate.FactoryTimeFormat)
	}
	c, err := Configuration(1, iface.Switches{}, DefaultLimits(), presetRules(iface.PresetInternet))
	if err != nil || c.Display.TimeFormat != iface.FactoryTimeFormat {
		t.Fatalf("the configuration's display: %+v %v", c.Display, err)
	}
}
