package bastionmain

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/limits"
)

// The configuration of a Bastion (R-BAS-093, Q-BAS-320): what the main installation decides for a Bastion, kept on its registration -
// the switches, the mode and the preset of its download limits, its flood protection per address, its slots, its throughput and its
// anonymous ceiling - and the rules of its place (R-LIM-043), which live in limit_assignment. The handshake sends all of it as one
// iface.Configuration with the registration's revision, built here from the registration's row and its place's rules.

// Limits are the members of a registration's limits it holds itself (R-BAS-093); the rules of its place are rows of the limits.
type Limits struct {
	Mode iface.LimitsMode
	// Preset is the preset an administrator last applied (internet, partnerNetwork or internal); the registration reads custom once
	// a kind rule or a flood figure differs from it ([EffectivePreset]).
	Preset           iface.LimitsPreset
	Flood            iface.Flood
	Slots            iface.Slots
	Throughput       iface.Throughput
	AnonymousCeiling iface.Ceiling
}

// PlaceRule is a rule of a Bastion's place as the configuration reads it: its "who", its profile with the profile's figures and its
// overrides.
type PlaceRule struct {
	ID        string
	WhoKind   limits.WhoKind
	WhoRef    string
	Profile   string
	Overrides limits.Overrides
	Builtin   bool
	// Figures are the profile's figures, before the overrides.
	Figures limits.Figures
}

// Effective is the rule's effective figures: the profile's with the overrides. A rule of a Bastion's place is never capped
// (R-LIM-043: R-LIM-016's caps do not apply).
func (r PlaceRule) Effective() limits.Figures { return r.Overrides.Apply(r.Figures) }

// Preset is what a preset writes (R-LIM-046): the profile of each kind rule of the place, the flood figures and the mode.
type Preset struct {
	// Kinds maps the store's every-kind (limits.EveryUser, …) to a profile key.
	Kinds map[string]string
	Flood iface.Flood
	Mode  iface.LimitsMode
}

// internetFlood are the flood figures of every preset (R-LIM-046, R-BAS-092's figures, unchanged by the measurement of 2026-11-26):
// per address anonymous 50 a second with a burst of 1,000 and 1,000 misses a minute, signed-in 200, 4,000 and 4,000, the web
// interface's pages 20 a second with a burst of 100 (Q-LIM-307), R-BAS-036's failed-authentication timeout and R-BAS-037's blocking.
func internetFlood() iface.Flood {
	return iface.Flood{
		Anonymous:      iface.FloodClass{RequestsPerSecond: 50, Burst: 1000, MissesPerMinute: 1000},
		Authenticated:  iface.FloodClass{RequestsPerSecond: 200, Burst: 4000, MissesPerMinute: 4000},
		GUI:            iface.FloodRate{RequestsPerSecond: 20, Burst: 100},
		FailedAuth:     iface.FailedAuth{MaxFailures: 20, WindowSeconds: 600, TimeoutSeconds: 900},
		Blocking:       iface.Blocking{Threshold: 3000, WindowSeconds: 60, DurationSeconds: 900, MaxDurationSeconds: 86400},
		ExemptNetworks: []string{},
	}
}

// Presets are the three presets of R-LIM-046. Internet gives every kind the two profiles of a Bastion, administrators included (a
// leaked administrator token on the internet is the worst case, Q-LIM-300); Partner network the main installation's profiles;
// Internal Unlimited, which still counts. All three keep the flood figures and Enforce.
func Presets() map[iface.LimitsPreset]Preset {
	kinds := func(anonymous, user, machine, admin string) map[string]string {
		return map[string]string{
			limits.EveryAnonymous: anonymous, limits.EveryUser: user, limits.EveryTechnicalToken: machine, limits.EveryCI: machine,
			limits.EveryAdministrator: admin,
		}
	}
	signed := limits.ProfileBastionSignedIn
	return map[iface.LimitsPreset]Preset{
		iface.PresetInternet: {Kinds: kinds(limits.ProfileBastionAnonymous, signed, signed, signed), Flood: internetFlood(), Mode: iface.ModeEnforce},
		iface.PresetPartnerNetwork: {
			Kinds: kinds(limits.ProfileAnonymous, limits.ProfileStandard, limits.ProfileTechnical, limits.ProfileUnlimited),
			Flood: internetFlood(), Mode: iface.ModeEnforce,
		},
		iface.PresetInternal: {
			Kinds: kinds(limits.ProfileUnlimited, limits.ProfileUnlimited, limits.ProfileUnlimited, limits.ProfileUnlimited),
			Flood: internetFlood(), Mode: iface.ModeEnforce,
		},
	}
}

// kindOrder is the order the kind rules are written and named in.
func kindOrder() []string {
	return []string{limits.EveryAnonymous, limits.EveryUser, limits.EveryTechnicalToken, limits.EveryCI, limits.EveryAdministrator}
}

// kindNote is the note of a kind rule a new registration's place gets.
func kindNote(kind string) string {
	return map[string]string{
		limits.EveryAnonymous: "Callers without a credential.", limits.EveryUser: "Signed-in users.",
		limits.EveryTechnicalToken: "Technical tokens.", limits.EveryCI: "CI projects.", limits.EveryAdministrator: "Administrators.",
	}[kind]
}

// DefaultLimits are the limits of a new registration (R-LIM-046, R-BAS-093): the preset Internet in Enforce, the slots of R-BAS-025
// (64 requests in flight, a quarter for anonymous callers, a wait of 5 s, the figure the Bastion's built-in configuration holds,
// Q-BAS-448), the throughput of 200 MB/s with a quarter for anonymous callers and no hourly volume (Q-BAS-455), and the anonymous ceiling measured on 2026-11-26 (developer/bastion.md): 100 requests a second to the main installation with a
// burst of 2,000 and 36,000 an hour, for all anonymous callers together. Migration 00147 writes the same values as the columns'
// defaults; an integration test compares them.
func DefaultLimits() Limits {
	return Limits{
		Mode: iface.ModeEnforce, Preset: iface.PresetInternet, Flood: internetFlood(),
		Slots:            iface.Slots{MaxConcurrent: 64, AnonymousShare: 0.25, QueueTimeoutMillis: 5000},
		Throughput:       iface.Throughput{BytesPerSecond: ptr(200_000_000), AnonymousShare: 0.25},
		AnonymousCeiling: iface.Ceiling{RequestsPerSecond: ptr(100), Burst: ptr(2000), RequestsPerHour: ptr(36000)},
	}
}

func ptr(v int64) *int64 { return &v }

// EffectivePreset is the preset the registration reads (R-LIM-046): the stored one while every kind rule names the preset's profile
// without an override and the flood figures are the preset's, custom otherwise.
func EffectivePreset(l Limits, rules []PlaceRule) iface.LimitsPreset {
	p, ok := Presets()[l.Preset]
	if !ok || !sameFlood(p.Flood, l.Flood) {
		return iface.PresetCustom
	}
	seen := 0
	for _, r := range rules {
		if r.WhoKind != limits.WhoEvery {
			continue
		}
		seen++
		if p.Kinds[r.WhoRef] != r.Profile || !r.Overrides.Empty() {
			return iface.PresetCustom
		}
	}
	if seen != len(p.Kinds) {
		return iface.PresetCustom
	}
	return l.Preset
}

// sameFlood compares two sets of flood figures, an empty list of exempt networks equal to none.
func sameFlood(a, b iface.Flood) bool {
	return a.Anonymous == b.Anonymous && a.Authenticated == b.Authenticated && a.GUI == b.GUI && a.FailedAuth == b.FailedAuth &&
		a.Blocking == b.Blocking && slices.Equal(a.ExemptNetworks, b.ExemptNetworks)
}

// Configuration is the handshake's member of a registration (R-BAS-093, Q-BAS-320): its revision, its switches, and its limits
// with the table of its place - every rule's effective figures, the address rules and the five kind rules. Its rules hold whatever
// the Bastion's own rules accept (iface.Configuration.Validate); a place without its five kind rules is an error, never a guess. Its
// display is the factory form of a time: the installation's own is no member of the registration, and the composition root sets it
// from the system state where it answers a handshake (R-BAS-128, Q-BAS-402).
func Configuration(revision int64, sw iface.Switches, l Limits, rules []PlaceRule) (iface.Configuration, error) {
	place := iface.PlaceTable{Rules: map[string]iface.Figures{}, AddressRules: []iface.AddressRule{}}
	kinds := map[string]string{}
	for _, r := range rules {
		place.Rules[r.ID] = wireFigures(r.Effective())
		switch r.WhoKind {
		case limits.WhoAddress:
			if p, ok := limits.CanonicalPrefix(r.WhoRef); ok {
				place.AddressRules = append(place.AddressRules, iface.AddressRule{Prefix: p.String(), Rule: r.ID})
			}
		case limits.WhoEvery:
			kinds[r.WhoRef] = r.ID
		}
	}
	slices.SortFunc(place.AddressRules, func(a, b iface.AddressRule) int { return comparePrefixes(a.Prefix, b.Prefix) })
	place.KindRules = iface.KindRules{
		Anonymous: kinds[limits.EveryAnonymous], User: kinds[limits.EveryUser], TechnicalToken: kinds[limits.EveryTechnicalToken],
		CI: kinds[limits.EveryCI], Administrator: kinds[limits.EveryAdministrator],
	}
	flood := l.Flood
	if flood.ExemptNetworks == nil {
		flood.ExemptNetworks = []string{}
	}
	c := iface.Configuration{
		Revision: revision, Switches: sw,
		Limits: iface.Limits{
			Mode: l.Mode, Preset: EffectivePreset(l, rules), Place: place, Flood: flood, Slots: l.Slots, Throughput: l.Throughput,
			AnonymousCeiling: l.AnonymousCeiling,
		},
		Display: iface.Display{TimeFormat: iface.FactoryTimeFormat},
	}
	if err := c.Validate(); err != nil {
		return iface.Configuration{}, fmt.Errorf("the configuration of a Bastion registration: %w", err)
	}
	return c, nil
}

// comparePrefixes orders prefixes by their address and then their length, so the table's order does not depend on the rules' IDs.
func comparePrefixes(a, b string) int {
	pa, ea := netip.ParsePrefix(a)
	pb, eb := netip.ParsePrefix(b)
	if ea != nil || eb != nil {
		return cmp.Compare(a, b)
	}
	if c := pa.Addr().Compare(pb.Addr()); c != 0 {
		return c
	}
	return pa.Bits() - pb.Bits()
}

// wireFigures are a rule's figures on the wire: a positive figure, or null for unlimited.
func wireFigures(f limits.Figures) iface.Figures {
	v := func(fig limits.Figure) *int64 {
		if f[fig] <= 0 {
			return nil
		}
		x := f[fig]
		return &x
	}
	return iface.Figures{
		RequestsPerSecond: v(limits.RequestsPerSecond), RequestsBurst: v(limits.RequestsBurst), RequestsPerHour: v(limits.RequestsPerHour),
		CachedBytesPerSecond: v(limits.CachedBytesPerSecond), CachedBytesPerHour: v(limits.CachedBytesPerHour),
		UncachedBytesPerSecond: v(limits.UncachedBytesPerSecond), UncachedBytesPerHour: v(limits.UncachedBytesPerHour),
		UpstreamRequestsPerHour: v(limits.UpstreamRequestsPerHour), ParallelTransfers: v(limits.ParallelTransfers),
		BytesPerDay: v(limits.BytesPerDay), RefusedPerHour: v(limits.RefusedPerHour),
	}
}

// MaxFailedAuthWindowSeconds is the longest window of the failed-authentication timeout a Bastion counts in (R-BAS-036): a longer
// one the Bastion would clamp to it (Q-BAS-128), so the main installation refuses it instead of storing a figure that does not hold.
const MaxFailedAuthWindowSeconds = 12 * 60 * 60

// ValidateLimits checks a registration's limits as a Bastion checks the configuration that carries them (iface.Configuration.Validate,
// R-BAS-093): the figures of the flood protection, the slots, the throughput and the ceiling, the mode and the stored preset, and a
// failed-authentication window of at most MaxFailedAuthWindowSeconds.
func ValidateLimits(l Limits) error {
	if _, ok := Presets()[l.Preset]; !ok {
		return fmt.Errorf("bastion configuration: the preset %q", l.Preset)
	}
	if w := l.Flood.FailedAuth.WindowSeconds; w > MaxFailedAuthWindowSeconds {
		return fmt.Errorf("bastion configuration: flood.failedAuth.windowSeconds %d exceeds %d, the longest window a Bastion counts in", w, MaxFailedAuthWindowSeconds)
	}
	probe := iface.Configuration{Limits: iface.Limits{
		Mode: l.Mode, Preset: l.Preset, Flood: l.Flood, Slots: l.Slots, Throughput: l.Throughput, AnonymousCeiling: l.AnonymousCeiling,
		Place: iface.PlaceTable{Rules: map[string]iface.Figures{"probe": {}}, KindRules: iface.KindRules{
			Anonymous: "probe", User: "probe", TechnicalToken: "probe", CI: "probe", Administrator: "probe",
		}},
	}, Display: iface.Display{TimeFormat: iface.FactoryTimeFormat}}
	return probe.Validate()
}

// limitsColumns are a registration's limits as the columns of 00147 hold them.
type limitsColumns struct {
	mode, preset                      string
	flood, slots, throughput, ceiling json.RawMessage
}

// limitsOf reads the columns back; a member that does not read leaves its zero value, which no Bastion accepts, so a damaged row fails
// the handshake rather than loosening a figure.
func limitsOf(c limitsColumns) Limits {
	l := Limits{Mode: iface.LimitsMode(c.mode), Preset: iface.LimitsPreset(c.preset)}
	_ = json.Unmarshal(c.flood, &l.Flood)
	_ = json.Unmarshal(c.slots, &l.Slots)
	_ = json.Unmarshal(c.throughput, &l.Throughput)
	_ = json.Unmarshal(c.ceiling, &l.AnonymousCeiling)
	if l.Flood.ExemptNetworks == nil {
		l.Flood.ExemptNetworks = []string{}
	}
	return l
}

// columnsOf writes a registration's limits as the columns hold them.
func columnsOf(l Limits) (flood, slots, throughput, ceiling string) {
	if l.Flood.ExemptNetworks == nil {
		l.Flood.ExemptNetworks = []string{}
	}
	return string(mustJSON(l.Flood)), string(mustJSON(l.Slots)), string(mustJSON(l.Throughput)), string(mustJSON(l.AnonymousCeiling))
}

// placeRuleDoc is a rule of the place as the statements BastionSnapshot and BastionPlaceRules write it.
type placeRuleDoc struct {
	ID        string           `json:"id"`
	WhoKind   string           `json:"whoKind"`
	WhoRef    string           `json:"whoRef"`
	Profile   string           `json:"profile"`
	Overrides limits.Overrides `json:"overrides"`
	Builtin   bool             `json:"builtin"`
	Figures   limits.Figures   `json:"figures"`
}

func (d placeRuleDoc) rule() PlaceRule {
	return PlaceRule{
		ID: d.ID, WhoKind: limits.WhoKind(d.WhoKind), WhoRef: d.WhoRef, Profile: d.Profile, Overrides: d.Overrides, Builtin: d.Builtin,
		Figures: d.Figures,
	}
}

// placeRulesOf reads the rules a snapshot row aggregated.
func placeRulesOf(raw json.RawMessage) ([]PlaceRule, error) {
	var docs []placeRuleDoc
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil, fmt.Errorf("the rules of a Bastion's place: %w", err)
	}
	out := make([]PlaceRule, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.rule())
	}
	return out, nil
}

// SubjectLimits names what a Bastion charges a signed-in caller against (R-BAS-096, Q-BAS-328, Q-LIM-302): the subject's key of the
// download limits and the rule the index resolves for it at the place of the registration bastionID, a single caller's or a group's
// rule, else the kind rule. nil when the subject is no signed-in subject of R-LIM-001 or the index holds no rule of the place yet (a
// registration created within the index's propagation bound): the member is optional on the wire, and the Bastion then charges the
// caller by its kind rule.
func SubjectLimits(x *limits.Index, s limits.Subject, bastionID string) *iface.SubjectLimits {
	switch s.Kind {
	case limits.KindUser, limits.KindTechnicalToken, limits.KindCI, limits.KindCertificate:
	default:
		return nil
	}
	res := x.ResolveBastion(s, bastionID)
	if res.Rule == nil {
		return nil
	}
	out := iface.SubjectLimits{Key: s.String(), Rule: res.Rule.ID}
	if !out.Valid() {
		return nil
	}
	return &out
}
