package settings

import "github.com/guided-traffic/quader/internal/bastion/iface"

// The rules of the built-in configuration's place: the two Bastion profiles of usage-and-limits.md R-LIM-046, with the figures
// the measurement of 2026-11-26 proposes for them (docs/developer/bastion.md, "The proposal for the figures").
const (
	BuiltinRuleAnonymous = "builtin-bastion-anonymous"
	BuiltinRuleSignedIn  = "builtin-bastion-signed-in"
)

// The built-in figures of a Bastion (R-BAS-094, R-BAS-096, Q-BAS-448): the defaults of a new registration, which the
// built-in configuration holds as well.
const (
	// DefaultCeilingPerSecond, DefaultCeilingBurst and DefaultCeilingPerHour are the anonymous ceiling: the burst holds three
	// cold anonymous npm ci of 475 packages at once with a third of room, the hour 75 of them (derived from the measurement).
	DefaultCeilingPerSecond = 100
	DefaultCeilingBurst     = 2000
	DefaultCeilingPerHour   = 36000
	// DefaultMaxConcurrent, DefaultAnonymousShare and DefaultQueueTimeoutMillis are the slots: 16 anonymous slots of a whole
	// Bastion, and a wait of 5 s, in which the 200 requests at once of one anonymous dotnet restore are served instead of
	// 31 to 40 of them being answered 503 busy after the 2 s measured (derived).
	DefaultMaxConcurrent      = 64
	DefaultAnonymousShare     = 0.25
	DefaultQueueTimeoutMillis = 5000
)

func figure(v int64) *int64 { return &v }

// Builtin is the configuration a replica applies before its first handshake when it holds no stored one whose signature holds
// (Q-BAS-327): it fails closed, as strict as a new registration. Every switch is off; the limits are the Internet preset
// (usage-and-limits.md R-LIM-046: the two Bastion profiles as its kind rules, its flood figures, Enforce), the default slots
// and anonymous ceiling, and the throughput unlimited (R-BAS-097). The revision is 0, which no registration has, so the first
// handshake always replaces it.
func Builtin() iface.Configuration {
	anonymous := iface.Figures{
		RequestsPerSecond: figure(50), RequestsBurst: figure(1250), RequestsPerHour: figure(20000),
		CachedBytesPerSecond: figure(100_000_000), CachedBytesPerHour: figure(30_000_000_000),
		UncachedBytesPerSecond: figure(50_000_000), UncachedBytesPerHour: figure(9_000_000_000),
		UpstreamRequestsPerHour: figure(5200), ParallelTransfers: figure(80), BytesPerDay: figure(220_000_000_000),
		RefusedPerHour: figure(300),
	}
	signedIn := iface.Figures{
		RequestsPerSecond: figure(200), RequestsBurst: figure(5400), RequestsPerHour: figure(100000),
		CachedBytesPerSecond: figure(250_000_000), CachedBytesPerHour: figure(160_000_000_000),
		UncachedBytesPerSecond: figure(125_000_000), UncachedBytesPerHour: figure(20_000_000_000),
		UpstreamRequestsPerHour: figure(12000), ParallelTransfers: figure(340), BytesPerDay: figure(1_300_000_000_000),
		RefusedPerHour: figure(1000),
	}
	return iface.Configuration{
		Revision: 0,
		Switches: iface.Switches{},
		Limits: iface.Limits{
			Mode:   iface.ModeEnforce,
			Preset: iface.PresetInternet,
			Place: iface.PlaceTable{
				Rules:        map[string]iface.Figures{BuiltinRuleAnonymous: anonymous, BuiltinRuleSignedIn: signedIn},
				AddressRules: []iface.AddressRule{},
				KindRules: iface.KindRules{
					Anonymous: BuiltinRuleAnonymous, User: BuiltinRuleSignedIn, TechnicalToken: BuiltinRuleSignedIn,
					CI: BuiltinRuleSignedIn, Administrator: BuiltinRuleSignedIn,
				},
			},
			Flood: iface.Flood{
				Anonymous:      iface.FloodClass{RequestsPerSecond: 50, Burst: 1000, MissesPerMinute: 1000},
				Authenticated:  iface.FloodClass{RequestsPerSecond: 200, Burst: 4000, MissesPerMinute: 4000},
				GUI:            iface.FloodRate{RequestsPerSecond: 20, Burst: 100},
				FailedAuth:     iface.FailedAuth{MaxFailures: 20, WindowSeconds: 600, TimeoutSeconds: 900},
				Blocking:       iface.Blocking{Threshold: 3000, WindowSeconds: 60, DurationSeconds: 900, MaxDurationSeconds: 86400},
				ExemptNetworks: []string{},
			},
			Slots:      iface.Slots{MaxConcurrent: DefaultMaxConcurrent, AnonymousShare: DefaultAnonymousShare, QueueTimeoutMillis: DefaultQueueTimeoutMillis},
			Throughput: iface.Throughput{AnonymousShare: 0.25},
			AnonymousCeiling: iface.Ceiling{
				RequestsPerSecond: figure(DefaultCeilingPerSecond), Burst: figure(DefaultCeilingBurst),
				RequestsPerHour: figure(DefaultCeilingPerHour),
			},
		},
	}
}
