package iface

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sort"
)

// Configuration is the handshake's member configuration (R-BAS-093, Q-BAS-320): what the main installation decides for
// a Bastion, delivered whole with the registration's revision. Every member is always present; a figure of [Figures],
// [Throughput] and [Ceiling] is a positive integer or null, which is unlimited.
type Configuration struct {
	// Revision is the registration's revision (its ETag), raised by every change of a member the registration holds and by a
	// change of a rule of the Bastion's place or of a profile such a rule names. [Display] is the installation's and changes
	// without it (Q-BAS-402): a replica applies a configuration that differs in any member from the one it applies.
	Revision int64    `json:"revision"`
	Switches Switches `json:"switches"`
	Limits   Limits   `json:"limits"`
	Display  Display  `json:"display"`
	// Disabled is the registration's disable (R-BAS-133): null while the Bastion works, [DisableSoft] or [DisableHard] while an
	// administrator has disabled it. Always present on the wire.
	Disabled *DisableMode `json:"disabled"`
}

// DisableMode is how a Bastion is disabled (R-BAS-133): soft admits no new request and lets every request in flight finish; hard
// also cuts every request in flight.
type DisableMode string

// The ways of a disable.
const (
	DisableSoft DisableMode = "soft"
	DisableHard DisableMode = "hard"
)

// Valid reports whether m is one of the two ways.
func (m DisableMode) Valid() bool { return m == DisableSoft || m == DisableHard }

// Display is how the Bastion's web interface writes what the main installation's screens write by the installation's settings
// (R-BAS-128): the installation's default form of a time (api-and-ui.md R-UI-251), so that a time reads the same at both.
type Display struct {
	// TimeFormat is one of [TimeFormats].
	TimeFormat string `json:"timeFormat"`
}

// FactoryTimeFormat is the form of a time while an administrator set none (api-and-ui.md R-UI-251): the form of a replica's
// built-in configuration.
const FactoryTimeFormat = "dmy-weekday"

// TimeFormats are the forms an installation's default of a time can take (api-and-ui.md R-UI-250), the factory default first. The
// main installation's list (sysstate.TimeFormats) is the same; a test of the main installation's configuration holds them equal.
func TimeFormats() []string {
	return []string{FactoryTimeFormat, "dmy", "iso", "dmy-slash", "mdy", "locale"}
}

// Switches are the registration's switches (R-BAS-093, R-BAS-102).
type Switches struct {
	// Handover is the handover per blob (R-BAS-071): while it is true the Bastion offers to take a handover on a miss
	// ([HandoverOffer], R-BAS-072).
	Handover bool `json:"handover"`
	// GUI is the read-only web interface (R-BAS-102): while it is false the catalogue routes answer 404 not_found and the
	// Bastion serves no page.
	GUI bool `json:"gui"`
	// Docs is the documentation of the web interface (R-BAS-102), effective only with GUI: while it is false no
	// documentation path of the catalogue ([CataloguePath.Documentation]) is served.
	Docs bool `json:"docs"`
	// HideRepositories is the web interface's switch Hide repositories (R-BAS-131), effective only with GUI: while it is true the
	// pages name no repository a visitor reads, the catalogue holds no repository's index and no remote's upstream, and a visitor
	// sees the projects and their packages alone. Off, the zero value, shows the repositories.
	HideRepositories bool `json:"hideRepositories"`
}

// LimitsMode is Audit or Enforce at a Bastion (usage-and-limits.md R-LIM-044).
type LimitsMode string

// The modes.
const (
	ModeAudit   LimitsMode = "audit"
	ModeEnforce LimitsMode = "enforce"
)

// LimitsPreset is the preset a Bastion's limits were filled from (usage-and-limits.md R-LIM-046), or custom once a kind
// rule or a flood figure differs from the preset's.
type LimitsPreset string

// The presets.
const (
	PresetInternet       LimitsPreset = "internet"
	PresetPartnerNetwork LimitsPreset = "partnerNetwork"
	PresetInternal       LimitsPreset = "internal"
	PresetCustom         LimitsPreset = "custom"
)

// Limits are a Bastion's limits (R-BAS-093 to R-BAS-097).
type Limits struct {
	Mode   LimitsMode   `json:"mode"`
	Preset LimitsPreset `json:"preset"`
	// Place is the table of the Bastion's place: the rules with their effective figures, the address rules and the kind
	// rules (R-BAS-096, Q-LIM-302).
	Place PlaceTable `json:"place"`
	// Flood is the flood protection per address (R-BAS-035 to R-BAS-037).
	Flood Flood `json:"flood"`
	// Slots are the requests in flight to the main installation (R-BAS-025).
	Slots Slots `json:"slots"`
	// Throughput is the byte rate the Bastion reads at (R-BAS-097).
	Throughput Throughput `json:"throughput"`
	// AnonymousCeiling is the ceiling of all anonymous callers' requests to the main installation (R-BAS-094).
	AnonymousCeiling Ceiling `json:"anonymousCeiling"`
}

// PlaceTable is the rule table of a Bastion's place (R-BAS-096): the main installation resolves a signed-in caller's rule
// and names it ([SubjectLimits]); the Bastion resolves the address rules, the longest prefix first, and the kind rules.
type PlaceTable struct {
	// Rules maps a rule ID ([ValidRuleID]) to its effective figures.
	Rules map[string]Figures `json:"rules"`
	// AddressRules are canonical prefixes (an address is a /32 or /128), each naming a rule of Rules, no prefix twice.
	AddressRules []AddressRule `json:"addressRules"`
	// KindRules name a rule of Rules for each kind of caller of R-LIM-011.
	KindRules KindRules `json:"kindRules"`
}

// AddressRule is one address rule of a [PlaceTable].
type AddressRule struct {
	Prefix string `json:"prefix"`
	Rule   string `json:"rule"`
}

// KindRules name the rule of each kind of caller (usage-and-limits.md R-LIM-011); a certificate takes the technical
// tokens' rule (Q-LIM-96).
type KindRules struct {
	Anonymous      string `json:"anonymous"`
	User           string `json:"user"`
	TechnicalToken string `json:"technicalToken"`
	CI             string `json:"ci"`
	Administrator  string `json:"administrator"`
}

// Figures are the eleven figures of a rule (usage-and-limits.md R-LIM-004), as read at a Bastion's place (R-BAS-096): a
// positive integer, or null for unlimited.
type Figures struct {
	RequestsPerSecond       *int64 `json:"requestsPerSecond"`
	RequestsBurst           *int64 `json:"requestsBurst"`
	RequestsPerHour         *int64 `json:"requestsPerHour"`
	CachedBytesPerSecond    *int64 `json:"cachedBytesPerSecond"`
	CachedBytesPerHour      *int64 `json:"cachedBytesPerHour"`
	UncachedBytesPerSecond  *int64 `json:"uncachedBytesPerSecond"`
	UncachedBytesPerHour    *int64 `json:"uncachedBytesPerHour"`
	UpstreamRequestsPerHour *int64 `json:"upstreamRequestsPerHour"`
	ParallelTransfers       *int64 `json:"parallelTransfers"`
	BytesPerDay             *int64 `json:"bytesPerDay"`
	RefusedPerHour          *int64 `json:"refusedPerHour"`
}

// FigureNames are the JSON names of the eleven figures, in the order of [Figures]: the keys of a usage row's limited and
// wouldLimit ([UsageRow]).
func FigureNames() []string {
	return []string{
		"requestsPerSecond", "requestsBurst", "requestsPerHour", "cachedBytesPerSecond", "cachedBytesPerHour",
		"uncachedBytesPerSecond", "uncachedBytesPerHour", "upstreamRequestsPerHour", "parallelTransfers", "bytesPerDay",
		"refusedPerHour",
	}
}

func (f Figures) all() []*int64 {
	return []*int64{
		f.RequestsPerSecond, f.RequestsBurst, f.RequestsPerHour, f.CachedBytesPerSecond, f.CachedBytesPerHour,
		f.UncachedBytesPerSecond, f.UncachedBytesPerHour, f.UpstreamRequestsPerHour, f.ParallelTransfers, f.BytesPerDay,
		f.RefusedPerHour,
	}
}

// Flood is the flood protection per client address (R-BAS-035 to R-BAS-037, Q-LIM-307). Its figures are always set:
// flood protection is never unlimited; an address that is to meet none is an exempt network.
type Flood struct {
	Anonymous     FloodClass `json:"anonymous"`
	Authenticated FloodClass `json:"authenticated"`
	// GUI holds the pages of the web interface per address; a page view is never a miss.
	GUI        FloodRate  `json:"gui"`
	FailedAuth FailedAuth `json:"failedAuth"`
	Blocking   Blocking   `json:"blocking"`
	// ExemptNetworks are canonical prefixes no flood figure, timeout or block applies to.
	ExemptNetworks []string `json:"exemptNetworks"`
}

// FloodClass is the request and miss figures of a class of callers per address.
type FloodClass struct {
	RequestsPerSecond int `json:"requestsPerSecond"`
	Burst             int `json:"burst"`
	MissesPerMinute   int `json:"missesPerMinute"`
}

// FloodRate is the request figures of the class gui per address.
type FloodRate struct {
	RequestsPerSecond int `json:"requestsPerSecond"`
	Burst             int `json:"burst"`
}

// FailedAuth is the failed-authentication timeout (R-BAS-036).
type FailedAuth struct {
	MaxFailures    int `json:"maxFailures"`
	WindowSeconds  int `json:"windowSeconds"`
	TimeoutSeconds int `json:"timeoutSeconds"`
}

// Blocking is the blocking of flooding addresses (R-BAS-037).
type Blocking struct {
	Threshold          int `json:"threshold"`
	WindowSeconds      int `json:"windowSeconds"`
	DurationSeconds    int `json:"durationSeconds"`
	MaxDurationSeconds int `json:"maxDurationSeconds"`
}

// Slots are the requests in flight to the main installation across a Bastion's replicas (R-BAS-025).
type Slots struct {
	MaxConcurrent      int     `json:"maxConcurrent"`
	AnonymousShare     float64 `json:"anonymousShare"`
	QueueTimeoutMillis int     `json:"queueTimeoutMillis"`
}

// Throughput is the byte rate a Bastion reads at (R-BAS-097, Q-BAS-332): the total, the anonymous callers' share of it,
// and on demand an hourly volume per class.
type Throughput struct {
	BytesPerSecond *int64          `json:"bytesPerSecond"`
	AnonymousShare float64         `json:"anonymousShare"`
	BytesPerHour   ThroughputHours `json:"bytesPerHour"`
}

// ThroughputHours are the hourly volumes per class (R-BAS-097).
type ThroughputHours struct {
	Anonymous *int64 `json:"anonymous"`
	SignedIn  *int64 `json:"signedIn"`
	Catalogue *int64 `json:"catalogue"`
}

// Ceiling is the anonymous ceiling of a Bastion (R-BAS-094): requests to the main installation on behalf of all anonymous
// callers together, a second with a burst and an hour. A burst is set only with a rate.
type Ceiling struct {
	RequestsPerSecond *int64 `json:"requestsPerSecond"`
	Burst             *int64 `json:"burst"`
	RequestsPerHour   *int64 `json:"requestsPerHour"`
}

// errConfiguration starts every refusal of [Configuration.Validate].
var errConfiguration = errors.New("bastion configuration")

func refuse(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{errConfiguration}, a...)...)
}

// Validate checks a configuration as a Bastion applies it (R-BAS-093): a revision not below 0; a known form of a time; a disable
// that is null, soft or hard (R-BAS-133); a known mode and preset;
// rules with valid IDs and figures that are positive or null; address rules and exempt networks as canonical prefixes, no
// prefix twice, each address rule and kind rule naming a rule of the table; flood figures that are positive with a block's
// duration not above its longest; slots of at least one with a share in 0 to 1 and a wait not below 0; a throughput and a
// ceiling whose figures are positive or null with a share in 0 to 1, a ceiling's burst only with its rate. A refused
// configuration fails the handshake.
func (c Configuration) Validate() error {
	if c.Revision < 0 {
		return refuse("the revision %d is negative", c.Revision)
	}
	if !slices.Contains(TimeFormats(), c.Display.TimeFormat) {
		return refuse("display.timeFormat %q", c.Display.TimeFormat)
	}
	if c.Disabled != nil && !c.Disabled.Valid() {
		return refuse("disabled %q", *c.Disabled)
	}
	l := c.Limits
	switch {
	case l.Mode != ModeAudit && l.Mode != ModeEnforce:
		return refuse("the mode %q", l.Mode)
	case l.Preset != PresetInternet && l.Preset != PresetPartnerNetwork && l.Preset != PresetInternal && l.Preset != PresetCustom:
		return refuse("the preset %q", l.Preset)
	}
	if err := l.Place.validate(); err != nil {
		return err
	}
	if err := l.Flood.validate(); err != nil {
		return err
	}
	switch s := l.Slots; {
	case s.MaxConcurrent < 1:
		return refuse("slots.maxConcurrent %d is below 1", s.MaxConcurrent)
	case !share(s.AnonymousShare):
		return refuse("slots.anonymousShare %v lies outside 0 to 1", s.AnonymousShare)
	case s.QueueTimeoutMillis < 0:
		return refuse("slots.queueTimeoutMillis %d is negative", s.QueueTimeoutMillis)
	}
	t := l.Throughput
	if !share(t.AnonymousShare) {
		return refuse("throughput.anonymousShare %v lies outside 0 to 1", t.AnonymousShare)
	}
	for _, v := range []*int64{t.BytesPerSecond, t.BytesPerHour.Anonymous, t.BytesPerHour.SignedIn, t.BytesPerHour.Catalogue} {
		if !figure(v) {
			return refuse("a figure of throughput is %d", *v)
		}
	}
	a := l.AnonymousCeiling
	for _, v := range []*int64{a.RequestsPerSecond, a.Burst, a.RequestsPerHour} {
		if !figure(v) {
			return refuse("a figure of anonymousCeiling is %d", *v)
		}
	}
	if a.Burst != nil && a.RequestsPerSecond == nil {
		return refuse("anonymousCeiling has a burst without a rate")
	}
	return nil
}

// share reports whether s lies in 0 to 1 (and is a number).
func share(s float64) bool { return s >= 0 && s <= 1 }

// figure reports whether v is null or positive.
func figure(v *int64) bool { return v == nil || *v > 0 }

func (p PlaceTable) validate() error {
	if len(p.Rules) == 0 {
		return refuse("the place has no rule")
	}
	ids := make([]string, 0, len(p.Rules))
	for id := range p.Rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if !ValidRuleID(id) {
			return refuse("the rule ID %q", id)
		}
		for i, v := range p.Rules[id].all() {
			if !figure(v) {
				return refuse("rule %s has %s %d", id, FigureNames()[i], *v)
			}
		}
	}
	seen := map[string]bool{}
	for _, a := range p.AddressRules {
		switch {
		case !canonicalPrefix(a.Prefix) || seen[a.Prefix]:
			return refuse("the address rule's prefix %q is no canonical prefix or comes twice", a.Prefix)
		case !hasRule(p.Rules, a.Rule):
			return refuse("the address rule of %s names the rule %q, which the place does not hold", a.Prefix, a.Rule)
		}
		seen[a.Prefix] = true
	}
	k := p.KindRules
	for kind, id := range map[string]string{"anonymous": k.Anonymous, "user": k.User, "technicalToken": k.TechnicalToken, "ci": k.CI, "administrator": k.Administrator} {
		if !hasRule(p.Rules, id) {
			return refuse("the kind rule %s names the rule %q, which the place does not hold", kind, id)
		}
	}
	return nil
}

func hasRule(rules map[string]Figures, id string) bool {
	_, ok := rules[id]
	return ok
}

func (f Flood) validate() error {
	for name, v := range map[string]int{
		"anonymous.requestsPerSecond": f.Anonymous.RequestsPerSecond, "anonymous.burst": f.Anonymous.Burst,
		"anonymous.missesPerMinute": f.Anonymous.MissesPerMinute, "authenticated.requestsPerSecond": f.Authenticated.RequestsPerSecond,
		"authenticated.burst": f.Authenticated.Burst, "authenticated.missesPerMinute": f.Authenticated.MissesPerMinute,
		"gui.requestsPerSecond": f.GUI.RequestsPerSecond, "gui.burst": f.GUI.Burst,
		"failedAuth.maxFailures": f.FailedAuth.MaxFailures, "failedAuth.windowSeconds": f.FailedAuth.WindowSeconds,
		"failedAuth.timeoutSeconds": f.FailedAuth.TimeoutSeconds, "blocking.threshold": f.Blocking.Threshold,
		"blocking.windowSeconds": f.Blocking.WindowSeconds, "blocking.durationSeconds": f.Blocking.DurationSeconds,
		"blocking.maxDurationSeconds": f.Blocking.MaxDurationSeconds,
	} {
		if v < 1 {
			return refuse("flood.%s %d is below 1", name, v)
		}
	}
	if f.Blocking.DurationSeconds > f.Blocking.MaxDurationSeconds {
		return refuse("flood.blocking.durationSeconds %d exceeds maxDurationSeconds %d", f.Blocking.DurationSeconds, f.Blocking.MaxDurationSeconds)
	}
	seen := map[string]bool{}
	for _, n := range f.ExemptNetworks {
		if !canonicalPrefix(n) || seen[n] {
			return refuse("the exempt network %q is no canonical prefix or comes twice", n)
		}
		seen[n] = true
	}
	return nil
}

// canonicalPrefix reports whether s is a prefix in the form netip writes it with its host bits cleared: "198.51.100.0/24",
// "2001:db8::/32", an address as "/32" or "/128".
func canonicalPrefix(s string) bool {
	p, err := netip.ParsePrefix(s)
	return err == nil && p == p.Masked() && p.String() == s
}
