// Package metrics defines every metric of the quader Bastion in one place (R-BAS-040, INV-16): the names, the
// label values and the collectors. The packages of the Bastion reference the fields of [Metrics] and add no
// metric of their own; a new one is a new line here and a new row in R-BAS-040. No label carries an address, a
// token, a path or a repository key (R-BAS-040).
//
// The request metrics of R-OPS-008 (duration, in flight, bytes by handler) and quader_valkey_available and
// quader_ratelimit_fallback are the shared ones of internal/observability and internal/cluster, not defined here.
package metrics

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// The names of R-BAS-040.
const (
	RequestsTotal            = "quader_bastion_requests_total"
	BytesServedTotal         = "quader_bastion_bytes_served_total"
	MainRequestsTotal        = "quader_bastion_main_requests_total"
	MainRequestDuration      = "quader_bastion_main_request_duration_seconds"
	MainInFlight             = "quader_bastion_main_in_flight"
	MainBudget               = "quader_bastion_main_budget"
	MainBudgetExhaustedTotal = "quader_bastion_main_budget_exhausted_total"
	MainReachable            = "quader_bastion_main_reachable"
	MainUnreachableSince     = "quader_bastion_main_unreachable_since_timestamp_seconds"
	BaseURLMismatch          = "quader_bastion_base_url_mismatch"
	InterfaceMismatch        = "quader_bastion_interface_mismatch"
	InterfaceVersion         = "quader_bastion_interface_version"
	GracePeriodSeconds       = "quader_bastion_grace_period_seconds"
	AnswerPeriodSeconds      = "quader_bastion_answer_period_seconds"
	Answers                  = "quader_bastion_answers"
	AnswersTotal             = "quader_bastion_answers_total"
	FeedLagSeconds           = "quader_bastion_feed_lag_seconds"
	FeedEntriesAppliedTotal  = "quader_bastion_feed_entries_applied_total"
	FeedResetsTotal          = "quader_bastion_feed_resets_total"
	TokenRemainingSeconds    = "quader_bastion_token_remaining_seconds"   //nolint:gosec // G101: a metric name, not a credential
	TokenWarnBeforeSeconds   = "quader_bastion_token_warn_before_seconds" //nolint:gosec // G101: a metric name, not a credential
	MainTokenRefused         = "quader_bastion_main_token_refused"        //nolint:gosec // G101: a metric name, not a credential
	TokenReloadsTotal        = "quader_bastion_token_reloads_total"       //nolint:gosec // G101: a metric name, not a credential
	CacheBytes               = "quader_bastion_cache_bytes"
	CacheCapacityBytes       = "quader_bastion_cache_capacity_bytes"
	CacheEvictionsTotal      = "quader_bastion_cache_evictions_total"
	CacheCorruptTotal        = "quader_bastion_cache_corrupt_total"
	CacheErrorsTotal         = "quader_bastion_cache_errors_total"
	CacheSweepsTotal         = "quader_bastion_cache_sweeps_total"
	CacheNoRoomTotal         = "quader_bastion_cache_no_room_total"
	CacheProtectedBytes      = "quader_bastion_cache_protected_bytes"
	RefusedTotal             = "quader_bastion_refused_total"
	BlocksTotal              = "quader_bastion_blocks_total"
	BlockedAddresses         = "quader_bastion_blocked_addresses"
	AuditEventsTotal         = "quader_bastion_audit_events_total"
	AuditBufferEvents        = "quader_bastion_audit_buffer_events"
	AuditEventsDroppedTotal  = "quader_bastion_audit_events_dropped_total"
	HandoversTotal           = "quader_bastion_handovers_total"
	FillBytesTotal           = "quader_bastion_fill_bytes_total"
	HandoverEnabled          = "quader_bastion_handover_enabled"
	HandoverCooldown         = "quader_bastion_handover_cooldown"
	ConfigurationRevision    = "quader_bastion_configuration_revision"
	ConfigurationSource      = "quader_bastion_configuration_source"
	ThroughputLimit          = "quader_bastion_throughput_limit_bytes_per_second"
	ThroughputWaitTotal      = "quader_bastion_throughput_wait_seconds_total"
	ThroughputRefusedTotal   = "quader_bastion_throughput_refused_total"
	// The public catalogue of the web interface (W-35, R-BAS-109 to R-BAS-111).
	GUIEnabled                 = "quader_bastion_gui_enabled"
	CatalogueConfirmed         = "quader_bastion_catalogue_confirmed_timestamp_seconds"
	CatalogueStaleAfterSeconds = "quader_bastion_catalogue_stale_after_seconds"
	CatalogueBytes             = "quader_bastion_catalogue_bytes"
	CatalogueMaxBytes          = "quader_bastion_catalogue_max_bytes"
	CatalogueDocuments         = "quader_bastion_catalogue_documents"
	CatalogueSyncTotal         = "quader_bastion_catalogue_sync_total"
)

// Label values. The sets are closed: a value outside them is a defect, never a new series.
const (
	// class of quader_bastion_requests_total: the classes of the marking (iface.Class) and refused.
	ClassArtifact = "artifact"
	ClassShared   = "shared"
	ClassPrivate  = "private"
	ClassAbsent   = "absent"
	ClassPass     = "pass"
	ClassRefused  = "refused"

	// result of quader_bastion_requests_total.
	ResultHit     = "hit"
	ResultChecked = "checked"
	ResultMiss    = "miss"
	ResultRefused = "refused"
	ResultError   = "error"

	// source of quader_bastion_bytes_served_total; main and store are the sources of quader_bastion_fill_bytes_total.
	SourceCache = "cache"
	SourceMain  = "main"
	SourceStore = "store"

	// outcome of quader_bastion_handovers_total (R-BAS-075, Q-BAS-290): one per handover, by the path it took.
	HandoverRead      = "read"
	HandoverFallback  = "fallback"
	HandoverContinued = "continued"
	HandoverRefused   = "refused"

	// kind of quader_bastion_main_requests_total and of the main request duration.
	KindForward     = "forward"
	KindConditional = "conditional"
	KindCheck       = "check"
	KindFeed        = "feed"
	KindHandshake   = "handshake"
	KindDownloads   = "downloads"
	// KindCatalogue is the public catalogue's reads of the web interface (R-BAS-109).
	KindCatalogue = "catalogue"

	// outcome of quader_bastion_main_requests_total: the status class of the answer, or how the request failed.
	Outcome2xx     = "2xx"
	Outcome3xx     = "3xx"
	Outcome4xx     = "4xx"
	Outcome5xx     = "5xx"
	OutcomeError   = "error"
	OutcomeTimeout = "timeout"
	OutcomeBusy    = "busy"

	// class of the budget gauges and of quader_bastion_fill_bytes_total: on whose behalf a request to the main installation is
	// made (mainclient.Class).
	BudgetAuthenticated = "authenticated"
	BudgetAnonymous     = "anonymous"

	// side of quader_bastion_interface_version.
	SideBastion = "bastion"
	SideMain    = "main"

	// kind of quader_bastion_answers and quader_bastion_answers_total.
	AnswerCredential = "credential"
	AnswerShape      = "shape"
	AnswerURL        = "url"
	// AnswerPackage is a credential's shape on one package (R-BAS-084); no label carries the package's name.
	AnswerPackage = "package"

	// result of quader_bastion_answers_total.
	AnswerHit          = "hit"
	AnswerMiss         = "miss"
	AnswerExpired      = "expired"
	AnswerDropped      = "dropped"
	AnswerBadSignature = "bad_signature"

	// outcome of quader_bastion_token_reloads_total.
	ReloadLoaded  = "loaded"
	ReloadRefused = "refused"

	// area of quader_bastion_cache_bytes and quader_bastion_cache_capacity_bytes.
	AreaArtifacts = "artifacts"
	AreaDocuments = "documents"

	// reason of quader_bastion_cache_evictions_total.
	EvictSize    = "size"
	EvictStale   = "stale"
	EvictMissing = "missing"

	// trigger of quader_bastion_cache_sweeps_total (R-BAS-089): the timer of sweep_interval, or the size of an area, which a
	// commit took over its high mark or found without room, a fill announced above it, or the listing at start found above it.
	SweepInterval = "interval"
	SweepSize     = "size"

	// reason of quader_bastion_refused_total.
	RefusedBlocked     = "blocked"
	RefusedRateLimited = "rate_limited"
	RefusedMissLimited = "miss_limited"
	RefusedAuthTimeout = "auth_timeout"
	RefusedInvalid     = "invalid_token"
	RefusedBusy        = "busy"
	RefusedUnreachable = "unreachable"
	RefusedNotServed   = "not_served"
	RefusedReadOnly    = "read_only"
	// RefusedForeignLocation: an answer of the main installation whose Location names another host than the Bastion's,
	// answered 502 instead (R-BAS-099 row 6).
	RefusedForeignLocation = "foreign_location"
	// RefusedCeiling: an anonymous caller's request to the main installation above the Bastion's anonymous ceiling, answered
	// 503 busy (R-BAS-094).
	RefusedCeiling = "anonymous_ceiling"
	// RefusedThroughput: a miss the throughput did not admit, answered 503 busy at the cap or 429 when its class's hourly volume
	// is used up (R-BAS-097).
	RefusedThroughput = "throughput"

	// bucket of quader_bastion_throughput_limit_bytes_per_second (R-BAS-097, Q-BAS-332).
	BucketTotal     = "total"
	BucketAnonymous = "anonymous"

	// class of quader_bastion_throughput_wait_seconds_total and quader_bastion_throughput_refused_total: on whose behalf the
	// Bastion reads (R-BAS-097).
	ThroughputAnonymous = "anonymous"
	ThroughputSignedIn  = "signed_in"
	ThroughputCatalogue = "catalogue"

	// reason of quader_bastion_throughput_refused_total: at the cap, or the class's hourly volume used up.
	ThroughputBusy = "busy"
	ThroughputHour = "hour"

	// source of quader_bastion_configuration_source (R-BAS-093): where the configuration the replica applies came from.
	ConfigurationFromMain    = "main"
	ConfigurationFromStored  = "stored"
	ConfigurationFromBuiltin = "builtin"

	// outcome of quader_bastion_catalogue_sync_total (R-BAS-109): a round of this replica's own that read the public set and the
	// change list to its end and kept every document it named; a round that failed; a round that took the state another replica
	// of a shared bucket synced (the lease's follower).
	SyncConfirmed = "confirmed"
	SyncFailed    = "failed"
	SyncFollowed  = "followed"

	// reason of quader_bastion_blocks_total.
	BlockRefusals = "refusals"

	// outcome of quader_bastion_audit_events_total.
	AuditSent    = "sent"
	AuditSpooled = "spooled"
	AuditDropped = "dropped"
)

// Cache operations of the op label of quader_bastion_cache_errors_total.
const (
	OpGet    = "get"
	OpPut    = "put"
	OpDelete = "delete"
	OpList   = "list"
	OpSpool  = "spool"
)

// Metrics holds the collectors. Every package of the Bastion that counts takes the *Metrics and uses a field;
// a nil *Metrics is not valid, tests pass one made by [New] with a nil registerer.
type Metrics struct {
	// Requests counts the requests the read flow ended by class and result; the hit rate of the cache is
	// result="hit" over all (R-BAS-040).
	Requests *prometheus.CounterVec
	// BytesServed counts the response bytes by source (cache, main).
	BytesServed *prometheus.CounterVec
	// MainRequests counts the requests to the main installation by kind and outcome (the HTTP status class, or
	// error, timeout, refused); MainDuration times them by kind.
	MainRequests *prometheus.CounterVec
	MainDuration *prometheus.HistogramVec
	// MainInFlight and MainBudget are the slots in use and the slots the replica holds, by class (R-BAS-025);
	// BudgetExhausted counts the requests that got no slot within the queue timeout.
	MainInFlight    *prometheus.GaugeVec
	MainBudget      *prometheus.GaugeVec
	BudgetExhausted *prometheus.CounterVec
	// MainReachable is 1 or 0; MainUnreachableSince the Unix time unreachability began, 0 while reachable
	// (R-BAS-027). BaseURLMismatch and InterfaceMismatch are 1 while the condition holds (R-BAS-010, R-BAS-028).
	MainReachable        prometheus.Gauge
	MainUnreachableSince prometheus.Gauge
	BaseURLMismatch      prometheus.Gauge
	InterfaceMismatch    prometheus.Gauge
	// InterfaceVersion is the two versions by side (R-BAS-028).
	InterfaceVersion *prometheus.GaugeVec
	// GracePeriod and AnswerPeriod export the configured seconds, which the rule QuaderBastionGraceExhausted
	// reads (R-BAS-041).
	GracePeriod  prometheus.Gauge
	AnswerPeriod prometheus.Gauge
	// Answers is the number of answers held by kind; AnswersTotal counts lookups by kind and result (R-BAS-020).
	Answers      *prometheus.GaugeVec
	AnswersTotal *prometheus.CounterVec
	// FeedLag is the age of the last successful poll; FeedApplied counts entries by feed kind; FeedResets
	// counts resets (R-BAS-026).
	FeedLag     prometheus.Gauge
	FeedApplied *prometheus.CounterVec
	FeedResets  prometheus.Counter
	// TokenRemaining is the remaining lifetime of the token in use, TokenWarnBefore the configured warning
	// threshold, MainTokenRefused is 1 while the main installation refuses the token (R-BAS-006).
	TokenRemaining   prometheus.Gauge
	TokenWarnBefore  prometheus.Gauge
	MainTokenRefused prometheus.Gauge
	TokenReloads     *prometheus.CounterVec
	// CacheBytes and CacheCapacity are the bytes held and the limit by area; CacheEvictions counts evictions by
	// reason; CacheCorrupt counts verifications that failed; CacheErrors counts backend errors by op (R-BAS-030
	// to R-BAS-032).
	CacheBytes     *prometheus.GaugeVec
	CacheCapacity  *prometheus.GaugeVec
	CacheEvictions *prometheus.CounterVec
	CacheCorrupt   prometheus.Counter
	CacheErrors    *prometheus.CounterVec
	// CacheSweeps counts the sweeps this replica ran by what started them; CacheNoRoom counts the fills passed through unkept by
	// area because their bytes would have taken the area past its limit (R-BAS-089).
	CacheSweeps *prometheus.CounterVec
	CacheNoRoom *prometheus.CounterVec
	// CacheProtected is the bytes of the objects of an area that were served twice and are evicted last, as the last sweep of
	// this replica found them (R-BAS-098).
	CacheProtected *prometheus.GaugeVec
	// Refused counts the Bastion's own refusals by reason (R-BAS-029); Blocks counts blocks set; Blocked is the
	// number of addresses blocked (R-BAS-037).
	Refused *prometheus.CounterVec
	Blocks  *prometheus.CounterVec
	Blocked prometheus.Gauge
	// AuditEvents counts download events by outcome, AuditBuffer is the buffer's fill, AuditDropped counts the
	// events a full buffer lost (R-BAS-039).
	AuditEvents  *prometheus.CounterVec
	AuditBuffer  prometheus.Gauge
	AuditDropped prometheus.Counter
	// Handovers counts the handovers by outcome (read, fallback, continued, refused); FillBytes counts the bytes written into
	// fills by their source (main, store) and the budget class of the caller whose request started the fill (authenticated,
	// anonymous; R-BAS-090); HandoverEnabled is the handshake's member handover and HandoverCooldown 1 while the
	// replica sends no offer after a failure of the store's path (R-BAS-075).
	Handovers        *prometheus.CounterVec
	FillBytes        *prometheus.CounterVec
	HandoverEnabled  prometheus.Gauge
	HandoverCooldown prometheus.Gauge
	// ConfigRevision is the revision of the configuration the replica applies; ConfigSource is 1 for where that configuration
	// came from (main, stored, builtin) and 0 for the others (R-BAS-093).
	ConfigRevision prometheus.Gauge
	ConfigSource   *prometheus.GaugeVec
	// ThroughputLimit is the configured byte rate of the whole Bastion by bucket (total, anonymous), 0 while unlimited;
	// ThroughputWait counts the seconds this replica's reads waited for the cap, by class; ThroughputRefused counts the misses
	// the throughput did not admit, by class and reason (busy, hour) (R-BAS-097).
	ThroughputLimit   *prometheus.GaugeVec
	ThroughputWait    *prometheus.CounterVec
	ThroughputRefused *prometheus.CounterVec
	// GUIEnabled is 1 while the web interface is on for this replica (the switch gui, R-BAS-102). CatalogueConfirmed is the Unix
	// time of the replica's last confirmed state of the public catalogue, 0 while it holds none; CatalogueStaleAfter exports
	// bastion.gui.stale_after (R-BAS-111). CatalogueBytes is what the catalogue's documents hold in the cache backend, as stored,
	// and CatalogueMaxBytes its share, bastion.gui.max_bytes (R-BAS-110); CatalogueDocuments is the number of documents the
	// replica serves; CatalogueSync counts the rounds by outcome (R-BAS-109).
	GUIEnabled          prometheus.Gauge
	CatalogueConfirmed  prometheus.Gauge
	CatalogueStaleAfter prometheus.Gauge
	CatalogueBytes      prometheus.Gauge
	CatalogueMaxBytes   prometheus.Gauge
	CatalogueDocuments  prometheus.Gauge
	CatalogueSync       *prometheus.CounterVec
}

// New creates the collectors and registers them on reg; a nil reg creates them unregistered. The series with
// a closed label set are created at zero, so every metric of the table is scrapeable from the start.
func New(reg prometheus.Registerer) (*Metrics, error) {
	counter := func(name, help string, labels ...string) *prometheus.CounterVec {
		return prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels)
	}
	gauge := func(name, help string, labels ...string) *prometheus.GaugeVec {
		return prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, labels)
	}
	m := &Metrics{
		Requests:     counter(RequestsTotal, "Requests of the read flow by class (artifact, shared, private, absent, pass, refused) and result (hit, checked, miss, refused, error).", "class", "result"),
		BytesServed:  counter(BytesServedTotal, "Response bytes by source (cache, main).", "source"),
		MainRequests: counter(MainRequestsTotal, "Requests to the main installation by kind and outcome.", "kind", "outcome"),
		MainDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: MainRequestDuration, Help: "Duration of requests to the main installation by kind.",
			Buckets: []float64{0.005, 0.025, 0.1, 0.25, 0.5, 1, 2.5, 10, 30},
		}, []string{"kind"}),
		MainInFlight:         gauge(MainInFlight, "Requests to the main installation in flight by class (authenticated, anonymous).", "class"),
		MainBudget:           gauge(MainBudget, "Slots of the concurrency budget towards the main installation held by this replica, by class.", "class"),
		BudgetExhausted:      counter(MainBudgetExhaustedTotal, "Requests that got no slot of the budget within the queue timeout, by class.", "class"),
		MainReachable:        prometheus.NewGauge(prometheus.GaugeOpts{Name: MainReachable, Help: "1 while this replica treats the main installation as reachable (R-BAS-027)."}),
		MainUnreachableSince: prometheus.NewGauge(prometheus.GaugeOpts{Name: MainUnreachableSince, Help: "Unix time at which the main installation became unreachable for this replica; 0 while reachable."}),
		BaseURLMismatch:      prometheus.NewGauge(prometheus.GaugeOpts{Name: BaseURLMismatch, Help: "1 while the main installation does not honour the Bastion's base URL (R-BAS-010)."}),
		InterfaceMismatch:    prometheus.NewGauge(prometheus.GaugeOpts{Name: InterfaceMismatch, Help: "1 while the interface versions of the Bastion and the main installation differ (R-BAS-028)."}),
		InterfaceVersion:     gauge(InterfaceVersion, "The interface version by side (bastion, main).", "side"),
		GracePeriod:          prometheus.NewGauge(prometheus.GaugeOpts{Name: GracePeriodSeconds, Help: "bastion.answers.grace_period in seconds."}),
		AnswerPeriod:         prometheus.NewGauge(prometheus.GaugeOpts{Name: AnswerPeriodSeconds, Help: "bastion.answers.period in seconds."}),
		Answers:              gauge(Answers, "Answers of the main installation held by this replica, by kind (credential, shape, url, package).", "kind"),
		AnswersTotal:         counter(AnswersTotal, "Answer lookups by kind and result (hit, miss, expired, dropped, bad_signature).", "kind", "result"),
		FeedLag:              prometheus.NewGauge(prometheus.GaugeOpts{Name: FeedLagSeconds, Help: "Age of the last successful feed poll in seconds (R-BAS-026)."}),
		FeedApplied:          counter(FeedEntriesAppliedTotal, "Feed entries applied by kind (token, principal, grants, settings, content, catalogue).", "kind"),
		FeedResets:           prometheus.NewCounter(prometheus.CounterOpts{Name: FeedResetsTotal, Help: "Feed resets (R-BAS-026)."}),
		TokenRemaining:       prometheus.NewGauge(prometheus.GaugeOpts{Name: TokenRemainingSeconds, Help: "Remaining lifetime of the Bastion token in use in seconds (R-BAS-006)."}),
		TokenWarnBefore:      prometheus.NewGauge(prometheus.GaugeOpts{Name: TokenWarnBeforeSeconds, Help: "bastion.token.warn_before in seconds."}),
		MainTokenRefused:     prometheus.NewGauge(prometheus.GaugeOpts{Name: MainTokenRefused, Help: "1 while the main installation refuses the Bastion token (R-BAS-006)."}),
		TokenReloads:         counter(TokenReloadsTotal, "Reads of the token file by outcome (loaded, refused).", "outcome"),
		CacheBytes:           gauge(CacheBytes, "Bytes held by the cache by area (artifacts, documents).", "area"),
		CacheCapacity:        gauge(CacheCapacityBytes, "Configured size limit of the cache by area.", "area"),
		CacheEvictions:       counter(CacheEvictionsTotal, "Evictions by reason (size, stale, missing).", "reason"),
		CacheCorrupt:         prometheus.NewCounter(prometheus.CounterOpts{Name: CacheCorruptTotal, Help: "Cached objects whose verification failed (R-BAS-032)."}),
		CacheErrors:          counter(CacheErrorsTotal, "Errors of the cache backend by operation.", "op"),
		CacheSweeps:          counter(CacheSweepsTotal, "Sweeps of the cache this replica ran, by what started them (interval, size) (R-BAS-089).", "trigger"),
		CacheNoRoom:          counter(CacheNoRoomTotal, "Fills passed through unkept because their area had no room for them, by area (R-BAS-089).", "area"),
		CacheProtected:       gauge(CacheProtectedBytes, "Bytes of the objects served twice that the last sweep of this replica protected, by area (R-BAS-098).", "area"),
		Refused:              counter(RefusedTotal, "Requests the Bastion refused itself, by reason (R-BAS-029).", "reason"),
		Blocks:               counter(BlocksTotal, "Blocks set on addresses, by reason.", "reason"),
		Blocked:              prometheus.NewGauge(prometheus.GaugeOpts{Name: BlockedAddresses, Help: "Addresses blocked now."}),
		AuditEvents:          counter(AuditEventsTotal, "Download events by outcome (sent, spooled, dropped) (R-BAS-039).", "outcome"),
		AuditBuffer:          prometheus.NewGauge(prometheus.GaugeOpts{Name: AuditBufferEvents, Help: "Download events buffered in memory."}),
		AuditDropped:         prometheus.NewCounter(prometheus.CounterOpts{Name: AuditEventsDroppedTotal, Help: "Download events lost to a full buffer."}),
		Handovers:            counter(HandoversTotal, "Handovers by outcome (read, fallback, continued, refused) (R-BAS-075).", "outcome"),
		FillBytes:            counter(FillBytesTotal, "Bytes written into fills by source (main, store) and the class of the caller that started the fill (authenticated, anonymous) (R-BAS-075, R-BAS-090).", "source", "class"),
		HandoverEnabled:      prometheus.NewGauge(prometheus.GaugeOpts{Name: HandoverEnabled, Help: "1 while the last handshake said the registration's handover is on (R-BAS-071)."}),
		HandoverCooldown:     prometheus.NewGauge(prometheus.GaugeOpts{Name: HandoverCooldown, Help: "1 while this replica sends no offer after a failure of the object store's path (R-BAS-075)."}),
		ConfigRevision:       prometheus.NewGauge(prometheus.GaugeOpts{Name: ConfigurationRevision, Help: "The revision of the configuration this replica applies (R-BAS-093)."}),
		ConfigSource:         gauge(ConfigurationSource, "1 for where the configuration this replica applies came from (main, stored, builtin), 0 for the others (R-BAS-093).", "source"),
		ThroughputLimit:      gauge(ThroughputLimit, "The configured byte rate between the main installation and the whole Bastion, by bucket (total, anonymous); 0 while unlimited (R-BAS-097).", "bucket"),
		ThroughputWait:       counter(ThroughputWaitTotal, "Seconds this replica's reads from the main installation and the object store waited for the throughput cap, by class (anonymous, signed_in, catalogue) (R-BAS-097).", "class"),
		ThroughputRefused:    counter(ThroughputRefusedTotal, "Misses the throughput did not admit, by class and reason (busy, hour) (R-BAS-097).", "class", "reason"),
		GUIEnabled:           prometheus.NewGauge(prometheus.GaugeOpts{Name: GUIEnabled, Help: "1 while the web interface is on for this replica (R-BAS-102)."}),
		CatalogueConfirmed:   prometheus.NewGauge(prometheus.GaugeOpts{Name: CatalogueConfirmed, Help: "Unix time of this replica's last confirmed state of the public catalogue; 0 while it holds none (R-BAS-111)."}),
		CatalogueStaleAfter:  prometheus.NewGauge(prometheus.GaugeOpts{Name: CatalogueStaleAfterSeconds, Help: "bastion.gui.stale_after in seconds (R-BAS-111)."}),
		CatalogueBytes:       prometheus.NewGauge(prometheus.GaugeOpts{Name: CatalogueBytes, Help: "Bytes the public catalogue's documents hold in the cache backend, as stored (R-BAS-110)."}),
		CatalogueMaxBytes:    prometheus.NewGauge(prometheus.GaugeOpts{Name: CatalogueMaxBytes, Help: "bastion.gui.max_bytes: the public catalogue's share of the cache (R-BAS-110)."}),
		CatalogueDocuments:   prometheus.NewGauge(prometheus.GaugeOpts{Name: CatalogueDocuments, Help: "Documents of the public catalogue this replica serves (R-BAS-109)."}),
		CatalogueSync:        counter(CatalogueSyncTotal, "Rounds of the public catalogue's sync by outcome (confirmed, failed, followed) (R-BAS-109).", "outcome"),
	}
	m.seed()
	if reg == nil {
		return m, nil
	}
	for _, c := range m.collectors() {
		if err := reg.Register(c); err != nil {
			var are prometheus.AlreadyRegisteredError
			if errors.As(err, &are) {
				return nil, errors.New("bastion metrics are already registered on this registerer")
			}
			return nil, err
		}
	}
	return m, nil
}

// seed creates the series of the closed label sets at zero.
func (m *Metrics) seed() {
	for _, c := range []string{ClassArtifact, ClassShared, ClassPrivate, ClassAbsent, ClassPass, ClassRefused} {
		for _, r := range []string{ResultHit, ResultChecked, ResultMiss, ResultRefused, ResultError} {
			m.Requests.WithLabelValues(c, r)
		}
	}
	for _, s := range []string{SourceCache, SourceMain} {
		m.BytesServed.WithLabelValues(s)
	}
	for _, k := range []string{KindForward, KindConditional, KindCheck, KindFeed, KindHandshake, KindDownloads, KindCatalogue} {
		m.MainDuration.WithLabelValues(k)
		for _, o := range []string{Outcome2xx, Outcome3xx, Outcome4xx, Outcome5xx, OutcomeError, OutcomeTimeout, OutcomeBusy} {
			m.MainRequests.WithLabelValues(k, o)
		}
	}
	for _, c := range []string{BudgetAuthenticated, BudgetAnonymous} {
		m.MainInFlight.WithLabelValues(c)
		m.MainBudget.WithLabelValues(c)
		m.BudgetExhausted.WithLabelValues(c)
	}
	for _, s := range []string{SideBastion, SideMain} {
		m.InterfaceVersion.WithLabelValues(s)
	}
	for _, k := range []string{AnswerCredential, AnswerShape, AnswerURL, AnswerPackage} {
		m.Answers.WithLabelValues(k)
		for _, r := range []string{AnswerHit, AnswerMiss, AnswerExpired, AnswerDropped, AnswerBadSignature} {
			m.AnswersTotal.WithLabelValues(k, r)
		}
	}
	for _, k := range []string{"token", "principal", "grants", "settings", "content", "catalogue"} {
		m.FeedApplied.WithLabelValues(k)
	}
	for _, o := range []string{ReloadLoaded, ReloadRefused} {
		m.TokenReloads.WithLabelValues(o)
	}
	for _, a := range []string{AreaArtifacts, AreaDocuments} {
		m.CacheBytes.WithLabelValues(a)
		m.CacheCapacity.WithLabelValues(a)
		m.CacheNoRoom.WithLabelValues(a)
		m.CacheProtected.WithLabelValues(a)
	}
	for _, tr := range []string{SweepInterval, SweepSize} {
		m.CacheSweeps.WithLabelValues(tr)
	}
	for _, r := range []string{EvictSize, EvictStale, EvictMissing} {
		m.CacheEvictions.WithLabelValues(r)
	}
	for _, o := range []string{OpGet, OpPut, OpDelete, OpList, OpSpool} {
		m.CacheErrors.WithLabelValues(o)
	}
	for _, r := range []string{RefusedBlocked, RefusedRateLimited, RefusedMissLimited, RefusedAuthTimeout, RefusedInvalid, RefusedBusy, RefusedUnreachable, RefusedNotServed, RefusedReadOnly, RefusedForeignLocation, RefusedCeiling, RefusedThroughput} {
		m.Refused.WithLabelValues(r)
	}
	m.Blocks.WithLabelValues(BlockRefusals)
	for _, o := range []string{AuditSent, AuditSpooled, AuditDropped} {
		m.AuditEvents.WithLabelValues(o)
	}
	for _, o := range []string{HandoverRead, HandoverFallback, HandoverContinued, HandoverRefused} {
		m.Handovers.WithLabelValues(o)
	}
	for _, s := range []string{SourceMain, SourceStore} {
		for _, c := range []string{BudgetAuthenticated, BudgetAnonymous} {
			m.FillBytes.WithLabelValues(s, c)
		}
	}
	for _, s := range []string{ConfigurationFromMain, ConfigurationFromStored, ConfigurationFromBuiltin} {
		m.ConfigSource.WithLabelValues(s)
	}
	for _, b := range []string{BucketTotal, BucketAnonymous} {
		m.ThroughputLimit.WithLabelValues(b)
	}
	for _, c := range []string{ThroughputAnonymous, ThroughputSignedIn, ThroughputCatalogue} {
		m.ThroughputWait.WithLabelValues(c)
		for _, r := range []string{ThroughputBusy, ThroughputHour} {
			m.ThroughputRefused.WithLabelValues(c, r)
		}
	}
	for _, o := range []string{SyncConfirmed, SyncFailed, SyncFollowed} {
		m.CatalogueSync.WithLabelValues(o)
	}
}

func (m *Metrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.Requests, m.BytesServed, m.MainRequests, m.MainDuration, m.MainInFlight, m.MainBudget, m.BudgetExhausted,
		m.MainReachable, m.MainUnreachableSince, m.BaseURLMismatch, m.InterfaceMismatch, m.InterfaceVersion,
		m.GracePeriod, m.AnswerPeriod, m.Answers, m.AnswersTotal, m.FeedLag, m.FeedApplied, m.FeedResets,
		m.TokenRemaining, m.TokenWarnBefore, m.MainTokenRefused, m.TokenReloads,
		m.CacheBytes, m.CacheCapacity, m.CacheEvictions, m.CacheCorrupt, m.CacheErrors, m.CacheSweeps, m.CacheNoRoom,
		m.CacheProtected, m.Refused, m.Blocks, m.Blocked, m.AuditEvents, m.AuditBuffer, m.AuditDropped,
		m.Handovers, m.FillBytes, m.HandoverEnabled, m.HandoverCooldown, m.ConfigRevision, m.ConfigSource,
		m.ThroughputLimit, m.ThroughputWait, m.ThroughputRefused,
		m.GUIEnabled, m.CatalogueConfirmed, m.CatalogueStaleAfter, m.CatalogueBytes, m.CatalogueMaxBytes, m.CatalogueDocuments, m.CatalogueSync,
	}
}

// Names lists the metric names the table of R-BAS-040 holds, for the test that the table and the code agree.
func Names() []string {
	return []string{
		RequestsTotal, BytesServedTotal, MainRequestsTotal, MainRequestDuration, MainInFlight, MainBudget,
		MainBudgetExhaustedTotal, MainReachable, MainUnreachableSince, BaseURLMismatch, InterfaceMismatch,
		InterfaceVersion, GracePeriodSeconds, AnswerPeriodSeconds, Answers, AnswersTotal, FeedLagSeconds,
		FeedEntriesAppliedTotal, FeedResetsTotal, TokenRemainingSeconds, TokenWarnBeforeSeconds, MainTokenRefused,
		TokenReloadsTotal, CacheBytes, CacheCapacityBytes, CacheEvictionsTotal, CacheCorruptTotal, CacheErrorsTotal,
		CacheSweepsTotal, CacheNoRoomTotal, CacheProtectedBytes,
		RefusedTotal, BlocksTotal, BlockedAddresses, AuditEventsTotal, AuditBufferEvents, AuditEventsDroppedTotal,
		HandoversTotal, FillBytesTotal, HandoverEnabled, HandoverCooldown, ConfigurationRevision, ConfigurationSource,
		ThroughputLimit, ThroughputWaitTotal, ThroughputRefusedTotal,
		GUIEnabled, CatalogueConfirmed, CatalogueStaleAfterSeconds, CatalogueBytes, CatalogueMaxBytes, CatalogueDocuments, CatalogueSyncTotal,
	}
}
