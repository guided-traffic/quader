package config

import (
	"time"
)

// Bastions configures the main installation's side of the quader Bastion (R-BAS-004, R-BAS-011, R-BAS-015,
// R-BAS-016): how many registrations exist, what one check may ask, how the revocation feed reads the audit trail and
// how download events are accepted. The networks a Bastion connects from, whose forwarded headers count for its
// requests, are a member of its registration (R-BAS-017, Q-BAS-334), not of this section. The Bastion's own
// configuration is BastionConfig; a Bastion reads nothing of this section.
type Bastions struct {
	MaxRegistrations int               `yaml:"max_registrations" default:"16" validate:"min=1,max=256" doc:"Bastion registrations that may exist; also bounds the bastion label of the interface metrics (R-BAS-004)."`
	Check            BastionsCheck     `yaml:"check"`
	Feed             BastionsFeed      `yaml:"feed"`
	Downloads        BastionsDownloads `yaml:"downloads"`
}

// BastionsCheck bounds the credential and read check (R-BAS-011).
type BastionsCheck struct {
	MaxRepositories int `yaml:"max_repositories" default:"16" validate:"min=1,max=256" doc:"Repository keys one check may ask about (R-BAS-011)."`
}

// BastionsFeed configures the revocation feed (R-BAS-015).
type BastionsFeed struct {
	Lookback time.Duration `yaml:"lookback" default:"5m" validate:"min=30s,max=1h" doc:"A feed read returns every entry newer than its cursor minus this period, so a transaction that committed late is not missed; longer than any transaction (the replay margin of R-HA-054)."`
	MaxPage  int           `yaml:"max_page" default:"1000" validate:"min=10,max=10000" doc:"Entries one feed read returns at most (R-BAS-015)."`
}

// BastionsDownloads configures the intake of download events (R-BAS-016).
type BastionsDownloads struct {
	MaxBatch int           `yaml:"max_batch" default:"1000" validate:"min=1,max=10000" doc:"Events one request of the intake accepts (R-BAS-016)."`
	MaxAge   time.Duration `yaml:"max_age" default:"720h" validate:"min=1h" doc:"An event older than this is not recorded and counts as dropped (R-BAS-016)."`
	// EventsPerSecond and EventsBurst are the limiter of R-ARCH-034 that every registration's events are
	// charged to, in units of DownloadEventsPerUnit events (Q-BAS-117).
	EventsPerSecond float64 `yaml:"events_per_second" default:"200" validate:"min=1" doc:"Download events one registration may report per second across the replicas (R-BAS-016); a batch beyond the budget is answered 429 with Retry-After and sent again by the Bastion."`
	EventsBurst     int     `yaml:"events_burst" default:"2000" validate:"min=100,max=100000" doc:"Download events one window of the limiter admits; at least max_batch, so that every batch can fit (R-BAS-016). Charged in units of 100 events."`
}

// DownloadEventsPerUnit is the size of the unit a batch of download events is charged in (Q-BAS-117): the shared
// counter takes one INCR per unit, and a unit of 100 events keeps the largest batch (10,000 events) within
// cluster.MaxTakeUnits.
const DownloadEventsPerUnit = 100

// Units returns the units a batch of events is charged: the events rounded up to whole units.
func (d BastionsDownloads) Units(events int) int {
	return (max(0, events) + DownloadEventsPerUnit - 1) / DownloadEventsPerUnit
}

// LimiterRate and LimiterBurst are events_per_second and events_burst in units.
func (d BastionsDownloads) LimiterRate() float64 { return d.EventsPerSecond / DownloadEventsPerUnit }

// LimiterBurst is events_burst in whole units, rounded down.
func (d BastionsDownloads) LimiterBurst() int { return d.EventsBurst / DownloadEventsPerUnit }

// maxEventsWindow is the longest window of the events limiter: the longest a cluster limiter has (R-ARCH-034).
const maxEventsWindow = 12 * time.Hour

// Validate checks the cross-field rules of the section.
func (b *Bastions) Validate(r *Report) {
	d := b.Downloads
	if d.LimiterBurst() < d.Units(d.MaxBatch) {
		r.Add("downloads.events_burst", "(%d) must hold downloads.max_batch (%d) in units of %d events: a full batch would never fit", d.EventsBurst, d.MaxBatch, DownloadEventsPerUnit)
	}
	if d.EventsPerSecond > 0 && time.Duration(float64(d.EventsBurst)/d.EventsPerSecond*float64(time.Second)) > maxEventsWindow {
		r.Add("downloads.events_burst", "divided by downloads.events_per_second is a window of more than %s", maxEventsWindow)
	}
}
