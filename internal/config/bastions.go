package config

import (
	"fmt"
	"net/netip"
	"path"
	"regexp"
	"time"
	"unicode/utf8"
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
	// Declared is keyed by the registration's name, the way repositories.upstream_accounts is keyed by the repository's key: a map of
	// structs may nest, a list element may not (R-ARCH-020).
	Declared map[string]DeclaredBastion `yaml:"declared" default:"{}" doc:"Bastion registrations the configuration declares, keyed by the registration's name (R-BAS-127). Each entry names description, egress (the networks the Bastion connects from, CIDR or single addresses) and token_file, the absolute path of a file that holds the Bastion's token (qdr_bst_, 57 characters, the value the Bastion's chart reads from mainToken.existingSecret), and secret_name, the Kubernetes Secret the admin page names. quader migrate applies the map on every run, also on a fresh database: it creates a missing registration, makes its description, networks and token follow the entry, replaces the token when the file holds another value, and of a registration no longer declared revokes the token it had declared and keeps the rest. Only migrate reads the file; a value that is not a Bastion token stops migrate with the name. Limits, presets, switches and tokens issued on the screen stay an administrator's. In the chart, bastions.declared renders this map. As an environment variable the map is JSON, for example QUADER_BASTIONS_DECLARED='{\"partners\":{\"egress\":[\"198.51.100.0/28\"],\"token_file\":\"/run/secrets/partners/token\"}}'."`
}

// DeclaredBastion is one entry of bastions.declared (R-BAS-127): a Bastion registration and its token as the installation's
// configuration declares them. The token file is read by quader migrate only, never at load and never by a serving replica, so a
// Secret that does not exist yet stops no role; it stops migrate, which names the entry. An element of a map is decoded as it is
// written, so a member left out is the zero value: an empty description and no networks.
type DeclaredBastion struct {
	Description string   `yaml:"description" json:"description,omitempty"`
	Egress      []string `yaml:"egress" json:"egress,omitempty"`
	TokenFile   string   `yaml:"token_file" json:"token_file"`
	// SecretName is the Kubernetes Secret the chart mounts the token file from; the admin page names it. Empty outside the chart.
	SecretName string `yaml:"secret_name" json:"secret_name,omitempty"`
}

// declaredBastionName is the name rule of a registration (R-BAS-004, bastionmain.ValidName and the check principal_bastion_name).
var declaredBastionName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,19}$`)

// maxDeclaredDescription is the longest description of a registration, in characters (bastionmain's maxDescription).
const maxDeclaredDescription = 1024

// maxDeclaredEgress is the most networks one registration lists (bastionmain.MaxEgress, Q-BAS-431).
const maxDeclaredEgress = 32

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
	if len(b.Declared) > b.MaxRegistrations {
		r.Add("declared", "declares %d registrations, more than max_registrations (%d) allows", len(b.Declared), b.MaxRegistrations)
	}
	for _, name := range sortedMapKeys(b.Declared) {
		b.Declared[name].validate(r, name)
	}
}

// validate checks the form of one declared registration (R-BAS-127): the name, the description's length, each network's form, a clean
// absolute token file. It never reads the file; quader migrate does, and checks the value. The networks' finer rules (no /0, no bits
// beyond the prefix, none twice) are bastionmain.ParseEgress's, which migrate applies with the entry named.
func (d DeclaredBastion) validate(r *Report, name string) {
	at := "declared." + name
	if !declaredBastionName.MatchString(name) {
		r.Add(at, "the name must match ^[a-z][a-z0-9-]{0,19}$: it is the name of a Bastion registration (R-BAS-004)")
	}
	if utf8.RuneCountInString(d.Description) > maxDeclaredDescription {
		r.Add(at+".description", "has more than %d characters", maxDeclaredDescription)
	}
	if len(d.Egress) > maxDeclaredEgress {
		r.Add(at+".egress", "lists %d networks; a Bastion lists at most %d", len(d.Egress), maxDeclaredEgress)
	}
	for i, e := range d.Egress {
		if _, err := netip.ParsePrefix(e); err == nil {
			continue
		}
		if a, err := netip.ParseAddr(e); err != nil || a.Zone() != "" {
			r.Add(fmt.Sprintf("%s.egress[%d]", at, i), "%q is neither an IP address nor a network in CIDR form (198.51.100.20, 198.51.100.0/28, 2001:db8::/64)", e)
		}
	}
	switch {
	case d.TokenFile == "":
		r.Add(at+".token_file", "is required: the file that holds the Bastion's token")
	case !path.IsAbs(d.TokenFile) || path.Clean(d.TokenFile) != d.TokenFile:
		r.Add(at+".token_file", "%q must be a clean absolute path", d.TokenFile)
	}
}
