package config

import "time"

// The installation's keys of the curation of a remote repository (remote-curation.md R-CUR-020): proxy.delivery (the rules and the
// requests), proxy.catalogue (the upstream catalogue's budget, cache and bounds) and proxy.downloads (the download queue). A
// repository's own switches (remote.delivery.*) are members of its document, not keys of this file. The queue's workers are
// jobs.workers.downloads; the two tasks are maintenance.tasks.remote-downloads-dispatch and remote-examination-cleanup.

// ProxyDelivery bounds the delivery rules and the requests for an allow of a remote repository (R-CUR-006, R-CUR-015).
type ProxyDelivery struct {
	MaxRules                  int           `yaml:"max_rules" default:"10000" validate:"min=100,max=1000000" doc:"Delivery rules per remote repository, of which at most 100 are sections; one more answers 422 too_many_rules (R-CUR-006). About 150 bytes per rule in each replica's snapshot."`
	OpenRequestsPerPerson     int           `yaml:"open_requests_per_person" default:"20" validate:"min=1,max=1000" doc:"Open requests for an allow one person may hold in one repository; one more answers 429 too_many_requests (R-CUR-015)."`
	ExamineVersionsPerRequest int           `yaml:"examine_versions_per_request" default:"1" validate:"min=1,max=100" doc:"Versions a new request for a package downloads for examination: the newest that are no prereleases (R-CUR-015)."`
	RequestRetention          time.Duration `yaml:"request_retention" default:"720h" validate:"min=1h" doc:"How long an entry with no asker and no client refusal stays before the retention task removes it (R-CUR-015)."`
	MaxClientEntries          int           `yaml:"max_client_entries" default:"10000" validate:"min=1,max=1000000" doc:"Request entries without an asker per repository; beyond it the entry with the oldest last refusal goes first (R-CUR-015)."`
}

// ProxyCatalogue is the budget, the cache and the bounds of the upstream catalogue of remote repositories (R-CUR-008).
type ProxyCatalogue struct {
	RequestsPerMinute          int           `yaml:"requests_per_minute" default:"120" validate:"min=1,max=100000" doc:"Upstream requests of the catalogue per repository and minute across replicas, on the cluster limiter; one more answers 429 catalogue_budget_exhausted (R-CUR-008)."`
	RequestsPerMinutePerPerson int           `yaml:"requests_per_minute_per_person" default:"30" validate:"min=1,max=100000" doc:"Upstream requests of the catalogue per person and minute across repositories and replicas (R-CUR-008)."`
	CacheBytes                 ByteSize      `yaml:"cache_bytes" default:"32MiB" validate:"min=0" doc:"Per replica: the parsed pages of the catalogue's answers (names and facts, never a document's bytes); 0 turns the cache off (R-CUR-008)."`
	CacheTTL                   time.Duration `yaml:"cache_ttl" default:"300s" validate:"min=0s,max=1h" doc:"How long a parsed answer of the catalogue is kept; 0s keeps none (R-CUR-008)."`
	MaxDocumentBytes           ByteSize      `yaml:"max_document_bytes" default:"64MiB" validate:"min=1KiB" doc:"The largest upstream document the catalogue reads; a larger one answers 502 catalogue_answer_too_large (R-CUR-008)."`
	Timeout                    time.Duration `yaml:"timeout" default:"20s" validate:"min=1s,max=5m" doc:"How long one catalogue call may take before it answers 502 catalogue_answer_too_large (R-CUR-008)."`
}

// ProxyDownloads bounds the download queue of a remote repository (R-CUR-010, R-CUR-011).
type ProxyDownloads struct {
	PerRepository               int           `yaml:"per_repository" default:"4" validate:"min=1,max=64" doc:"Rows of one repository's queue that run at once; the rest wait (R-CUR-011). Each running row takes one of the repository's background slots (R-REPO-055)."`
	MaxWaitingPerRepository     int           `yaml:"max_waiting_per_repository" default:"10000" validate:"min=1,max=1000000" doc:"Waiting rows of one repository; a person's download beyond it answers 429 download_queue_full, a bulk's expansion pauses (R-CUR-011)."`
	MaxVersionsPerBulk          int           `yaml:"max_versions_per_bulk" default:"10000" validate:"min=1,max=1000000" doc:"The default and the highest value of a bulk's maxVersions, which the person may lower (R-CUR-010)."`
	MaxBytesPerBulk             ByteSize      `yaml:"max_bytes_per_bulk" default:"100GiB" validate:"min=1MiB" doc:"The default and the highest value of a bulk's maxBytes, which the person may lower (R-CUR-010)."`
	MaxAttempts                 int           `yaml:"max_attempts" default:"5" validate:"min=1,max=20" doc:"Attempts of a row whose failure a later attempt can overcome (R-CUR-011)."`
	DoneRetention               time.Duration `yaml:"done_retention" default:"168h" validate:"min=1h" doc:"How long a done or cancelled row stays after it finished (R-CUR-011)."`
	FailedRetention             time.Duration `yaml:"failed_retention" default:"720h" validate:"min=1h" doc:"How long a failed row stays after it finished (R-CUR-011)."`
	ClientFailuresPerRepository int           `yaml:"client_failures_per_repository" default:"1000" validate:"min=1,max=100000" doc:"Rows of clients' failed fetches per repository; beyond it the oldest is removed (R-CUR-011, Q-CUR-19)."`
	MaxSyncsPerRepository       int           `yaml:"max_syncs_per_repository" default:"100" validate:"min=1,max=10000" doc:"Downloads kept in sync per repository; one more answers 422 too_many_syncs (R-CUR-027). Each runs every remote.syncs.intervalSeconds of its repository."`
}
