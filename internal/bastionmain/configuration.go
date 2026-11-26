package bastionmain

import "github.com/guided-traffic/quader/internal/bastion/iface"

// The configuration of a Bastion (R-BAS-093). Until the registration holds one (package E8 of
// docs/developer/bastion-limits.md, migration 00148), the handshake sends the interim configuration below for every
// registration: the registration's revision and its switch of the handover, the web interface and its documentation off,
// and limits that equal what a Bastion does today, so that a Bastion that applies them changes nothing (Q-BAS-322):
//
//   - no budget per subject at the Bastion: one rule, InterimRule, every figure unlimited, for every kind of caller;
//   - the flood figures, the failed-authentication timeout and the blocking the Bastion chart held before they became members
//     of this configuration (R-BAS-093), no exempt network;
//   - the slots the chart held then (max_concurrent 64, anonymous_share 0.25, queue_timeout 2 s);
//   - no throughput cap and no anonymous ceiling (both unlimited);
//   - the mode enforce and the preset custom, since no preset's figures are what it holds.
//
// A Bastion applies the whole configuration (E9a): its flood figures and slots are these; the place and the throughput are read by
// nothing yet (E9b, E10).

// InterimRule is the ID of the one rule of the interim configuration.
const InterimRule = "interim-unlimited"

// The figures of the interim configuration's flood protection: the Bastion chart's defaults of today (R-BAS-035 to
// R-BAS-037, R-BAS-092), and the gui class of W-35 (R-BAS-096; no page is served yet).
var interimFlood = iface.Flood{
	Anonymous:      iface.FloodClass{RequestsPerSecond: 50, Burst: 1000, MissesPerMinute: 1000},
	Authenticated:  iface.FloodClass{RequestsPerSecond: 200, Burst: 4000, MissesPerMinute: 4000},
	GUI:            iface.FloodRate{RequestsPerSecond: 20, Burst: 100},
	FailedAuth:     iface.FailedAuth{MaxFailures: 20, WindowSeconds: 600, TimeoutSeconds: 900},
	Blocking:       iface.Blocking{Threshold: 3000, WindowSeconds: 60, DurationSeconds: 900, MaxDurationSeconds: 86400},
	ExemptNetworks: []string{},
}

// InterimConfiguration is the configuration the handshake sends until a registration holds its own (E8): revision is the
// registration's revision and handover its switch of the handover per blob (R-BAS-071).
func InterimConfiguration(revision int64, handover bool) iface.Configuration {
	flood := interimFlood
	flood.ExemptNetworks = []string{}
	return iface.Configuration{
		Revision: revision,
		Switches: iface.Switches{Handover: handover},
		Limits: iface.Limits{
			Mode:   iface.ModeEnforce,
			Preset: iface.PresetCustom,
			Place: iface.PlaceTable{
				Rules:        map[string]iface.Figures{InterimRule: {}},
				AddressRules: []iface.AddressRule{},
				KindRules: iface.KindRules{
					Anonymous: InterimRule, User: InterimRule, TechnicalToken: InterimRule, CI: InterimRule, Administrator: InterimRule,
				},
			},
			Flood:      flood,
			Slots:      iface.Slots{MaxConcurrent: 64, AnonymousShare: 0.25, QueueTimeoutMillis: 2000},
			Throughput: iface.Throughput{AnonymousShare: 0.25},
		},
	}
}
