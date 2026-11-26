package config

import (
	"fmt"
	"net"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"
)

// This file is the configuration schema of the role bastion (R-BAS-001, R-BAS-046, Q-BAS-50): a tree of its
// own, loaded by LoadBastion, with its own JSON Schema (bastion.schema.json, written by go generate and
// included by the chart quader-bastion as the schema of its `config` value, R-HA-029's mechanism). It shares the
// machinery of the main tree (the loader, the tags, the units, the secrets) and none of its sections: a key of
// another section stops startup with an error that names the key and the role.
//
// The keys marked `chart:"managed"` are the ones the chart renders from its own values (a mounted file, the
// replica count): the schema has them so that the binary can read them, and the chart refuses a value for them
// under `config`. Their schema nodes carry x-quader-chart-managed.

// BastionRole is the value of --role that selects the Bastion (R-BAS-001).
const BastionRole = "bastion"

// BastionConfig is the root of the Bastion's configuration (R-BAS-001).
type BastionConfig struct {
	Server  BastionServer   `yaml:"server"`
	Bastion BastionSettings `yaml:"bastion"`
	Valkey  BastionValkey   `yaml:"valkey"`
	Log     Log             `yaml:"log"`
	Tracing BastionTracing  `yaml:"tracing"`
	Runtime Runtime         `yaml:"runtime"`
	UI      BastionUI       `yaml:"ui"`
}

// BastionUI is the ui section of the role: the imprint and the privacy notice and nothing else (R-BAS-100). They are the
// Bastion's own texts, set in its own chart by its operator, never read from the main installation.
type BastionUI struct {
	Legal UILegal `yaml:"legal"`
}

// BastionServer is the server section of the role: the two listeners, the base URL the Bastion answers for,
// the trusted proxies of its own ingress and the in-flight bound (R-BAS-001). Every other timeout of the
// listeners is the default of the main installation's server section (BastionConfig.ServerSection).
type BastionServer struct {
	Listen          string       `yaml:"listen" default:":8443" doc:"Client listener: the Bastion's read flow (TLS)."`
	OpsListen       string       `yaml:"ops_listen" default:":8081" doc:"Ops listener: probes and metrics."`
	BaseURL         string       `yaml:"base_url" default:"" validate:"required" doc:"The origin this Bastion answers for, for example https://packages.example.com; the Bastion sends it to the main installation as the forwarded host and requires the handshake to honour it (R-BAS-010, R-BAS-017)."`
	TrustedProxies  []string     `yaml:"trusted_proxies" default:"[]" doc:"Addresses or CIDRs of the Bastion's own ingress that set the forwarding header (R-BAS-019). A /0 prefix is refused."`
	ForwardedHeader string       `yaml:"forwarded_header" default:"x-forwarded-for" validate:"oneof=x-forwarded-for|forwarded" doc:"The one header family the ingress rewrites; the other is never read (R-ARCH-006)."`
	MaxInFlight     int          `yaml:"max_in_flight" default:"1024" validate:"min=1" doc:"Concurrent requests per replica."`
	TLS             ServerTLS    `yaml:"tls" chart:"managed" doc:"TLS of the client listener (R-BAS-044); rendered by the chart from tls.existingSecret or the cert-manager Certificate."`
	OpsTLS          ServerOpsTLS `yaml:"ops_tls" chart:"managed" doc:"TLS of the ops listener (R-BAS-044); inherits server.tls by default."`
}

// BastionSettings is the section bastion: what the installation of the pods sizes, which the engine, the cache and the audit
// buffer read (R-BAS-006, R-BAS-020 to R-BAS-040). What an administrator of the main installation decides (the flood
// protection, the slots, the anonymous ceiling, the switches) is no key of this file: it comes from the Bastion's registration
// with every handshake (R-BAS-093), and a file that sets one of the keys it replaced stops startup naming the key.
type BastionSettings struct {
	Main    BastionMain    `yaml:"main"`
	Answers BastionAnswers `yaml:"answers"`
	Feed    BastionFeed    `yaml:"feed"`
	Cache   BastionCache   `yaml:"cache"`
	Audit   BastionAudit   `yaml:"audit"`
	Token   BastionToken   `yaml:"token"`
	// GUI is the public catalogue the replica mirrors for its web interface (W-35, R-BAS-109 to R-BAS-111): how often it reads
	// the change list, the share of the cache its documents take, and the age at which its pages say they are old. Whether the
	// web interface is on is no key of this file: it is the registration's switch gui (R-BAS-102).
	GUI BastionGUI `yaml:"gui"`
	// EdgeLimit is the edge figure of the ingress in front of the replicas, which the chart renders from its edgeLimit
	// values and every handshake reports to the main installation (R-BAS-093).
	EdgeLimit BastionEdgeLimit `yaml:"edge_limit" chart:"managed" doc:"Rendered by the chart from edgeLimit: the per-address figures of the ingress in front of the Bastion, reported to the main installation with every handshake (R-BAS-093)."`
	// Replicas is the number of replicas the chart runs at most (replicaCount, or autoscaling.maxReplicas): the
	// divisor of the budgets and limits without Valkey (R-BAS-025, Q-BAS-34).
	Replicas int `yaml:"replicas" default:"1" validate:"min=1,max=1000" chart:"managed" doc:"Rendered by the chart: replicaCount, or autoscaling.maxReplicas with autoscaling (R-BAS-025)."`
	// SigningKeyFile is the HMAC key of the signed entries and answers (R-BAS-032).
	SigningKeyFile string `yaml:"signing_key_file" default:"" secret:"file" chart:"managed" doc:"File with the HMAC-SHA-256 key (32 random bytes) that signs cache entries, negative entries and answers in Valkey; required with the bucket backend or Valkey (R-BAS-032)."`
}

// BastionMain is the connection to the main installation (R-BAS-006, R-BAS-021, R-BAS-025, R-BAS-027).
type BastionMain struct {
	URL                   string        `yaml:"url" default:"" validate:"required" doc:"The main endpoint of the main installation, for example https://quader.example.com; https only (R-BAS-044)."`
	PackagesURL           string        `yaml:"packages_url" default:"" doc:"The Service of the main installation's role packages, for example https://quader-packages.quader.svc:8080; the forwarded package and raw-file requests and the probes go there, everything else to url. Empty means url for both; https only (R-BAS-044)."`
	TokenFile             string        `yaml:"token_file" default:"" secret:"file" chart:"managed" doc:"File with the qdr_bst_ token, read again every token_reload_interval (R-BAS-006); required to serve."`
	CAFile                string        `yaml:"ca_file" default:"" chart:"managed" doc:"CA bundle that verifies the main installation, added to the system roots (trustedCA of the chart)."`
	ConnectTimeout        time.Duration `yaml:"connect_timeout" default:"5s" validate:"min=100ms,max=2m"`
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout" default:"30s" validate:"min=1s,max=10m" doc:"How long the main installation may take to answer a request before the Bastion answers 504 gateway_timeout (R-BAS-021)."`
	MaxForwardedBody      ByteSize      `yaml:"max_forwarded_body" default:"8MiB" validate:"min=0" doc:"Largest body the Bastion forwards on a read route that takes one, npm's bulk audit (R-BAS-014)."`
	HandshakeInterval     time.Duration `yaml:"handshake_interval" default:"1m" validate:"min=5s,max=1h" doc:"How often the handshake is repeated (R-BAS-010)."`
	UnreachableAfter      time.Duration `yaml:"unreachable_after" default:"10s" validate:"min=3s" doc:"No successful feed poll for this long makes the replica treat the main installation as unreachable; at least 3 x bastion.feed.poll_interval (R-BAS-027)."`
	TokenReloadInterval   time.Duration `yaml:"token_reload_interval" default:"10s" validate:"min=1s,max=10m" doc:"How often the token file is read again (R-BAS-006)."`
}

// BastionAnswers bounds the answers of the main installation the Bastion keeps (R-BAS-020).
type BastionAnswers struct {
	Period      time.Duration `yaml:"period" default:"5m" validate:"min=30s,max=1h" doc:"How long an answer of the main installation is usable (R-BAS-020)."`
	GracePeriod time.Duration `yaml:"grace_period" default:"15m" validate:"min=0s,max=24h" doc:"Extension of an answer's period while the main installation is unreachable (R-BAS-027)."`
	MaxEntries  int           `yaml:"max_entries" default:"200000" validate:"min=1000" doc:"Answers kept per replica, least recently used first."`
}

// BastionFeed configures how the Bastion follows the revocation feed (R-BAS-026).
type BastionFeed struct {
	PollInterval time.Duration `yaml:"poll_interval" default:"2s" validate:"min=1s,max=10s" doc:"How often every replica reads the feed (R-BAS-026)."`
}

// BastionCache configures the cache (R-BAS-023, R-BAS-024, R-BAS-030, R-BAS-031).
type BastionCache struct {
	Backend                  string             `yaml:"backend" default:"volume" validate:"oneof=volume|bucket" doc:"Where the cache lives: a local volume per replica, or a bucket of the Bastion's own shared by its replicas (R-BAS-030)."`
	MaxBytes                 ByteSize           `yaml:"max_bytes" default:"20GiB" validate:"min=1MiB" doc:"Artifacts, documents and the public catalogue stay below this; a sweep evicts by last access (R-BAS-031). Less documents_max_bytes and bastion.gui.max_bytes it holds at least one object of max_object_bytes: the artifacts' room, or the role does not start."`
	MaxObjectBytes           ByteSize           `yaml:"max_object_bytes" default:"2GiB" validate:"min=1KiB" doc:"Larger answers are passed through and not kept (R-BAS-021). At most max_bytes less documents_max_bytes less bastion.gui.max_bytes."`
	SpoolMaxBytes            ByteSize           `yaml:"spool_max_bytes" default:"2GiB" validate:"min=1MiB" doc:"The bytes all fills in progress of a replica may hold in temporary storage together; a fill that would pass it is passed through and not kept (Q-BAS-131). At least max_object_bytes. With the bucket backend the chart sizes the pod's tmp volume from it."`
	DocumentsMaxBytes        ByteSize           `yaml:"documents_max_bytes" default:"2GiB" validate:"min=1MiB" doc:"A share of max_bytes for shared documents, so that documents never evict artifacts (R-BAS-023); the artifacts get the rest less bastion.gui.max_bytes, which must hold one object of max_object_bytes (R-BAS-031)."`
	PrivateDocumentsMaxBytes ByteSize           `yaml:"private_documents_max_bytes" default:"256MiB" validate:"min=0" doc:"Memory per replica for the callers' private document copies (R-BAS-023)."`
	MetadataRevalidateAfter  time.Duration      `yaml:"metadata_revalidate_after" default:"1m" validate:"min=1s,max=24h" doc:"A shared document is asked about again after this period (R-BAS-023)."`
	ArtifactRevalidateAfter  time.Duration      `yaml:"artifact_revalidate_after" default:"10m" validate:"min=1s,max=24h" doc:"A cached artifact is checked again after this period (R-BAS-022)."`
	NegativePeriod           time.Duration      `yaml:"negative_period" default:"1m" validate:"min=1s,max=24h" doc:"How long a 404 marked absent is replayed (R-BAS-024)."`
	NegativeMaxEntries       int                `yaml:"negative_max_entries" default:"100000" validate:"min=0" doc:"Negative entries per replica (R-BAS-024)."`
	SweepInterval            time.Duration      `yaml:"sweep_interval" default:"5m" validate:"min=10s,max=24h" doc:"How often the eviction sweep runs (R-BAS-031)."`
	AccessResolution         time.Duration      `yaml:"access_resolution" default:"1h" validate:"min=1s,max=24h" doc:"A served entry's last access is written at most this often (R-BAS-031)."`
	FillWaitMax              time.Duration      `yaml:"fill_wait_max" default:"10m" validate:"min=30s,max=1h" doc:"How long a replica waits for another replica's fill of the same cache key while that replica keeps extending its fetch lock, sending nothing, before it fetches itself (R-BAS-054); the bucket backend with Valkey only."`
	EncryptionKeyFile        string             `yaml:"encryption_key_file" default:"" secret:"file" chart:"managed" doc:"File with the cache key (32 random bytes, raw or base64): with it every object of the cache store is encrypted or sealed below a tree named by the key's identifier, and a new key is an empty cache; empty keeps the cache plain (R-BAS-061). Rendered by the chart from secrets.cacheKey; read at start."`
	Volume                   BastionCacheVolume `yaml:"volume"`
	Bucket                   BastionCacheBucket `yaml:"bucket"`
}

// BastionCacheVolume is the local volume of the volume backend.
type BastionCacheVolume struct {
	Dir string `yaml:"dir" default:"/var/cache/quader-bastion" chart:"managed" doc:"Mount point of the cache volume; rendered by the chart."`
	// Persistent says whether the volume outlives its pod (the chart's cacheVolume.type persistent): the replica reports from it
	// whether its copy of the public catalogue survives a restart (Quader-Bastion-Catalogue-Kept, R-BAS-111).
	Persistent bool `yaml:"persistent" default:"false" chart:"managed" doc:"Rendered by the chart: true for cacheVolume.type persistent. The replica reports from it whether its copy of the public catalogue survives a restart (R-BAS-111)."`
}

// BastionGUI is the public catalogue of the web interface at the replica (W-35, R-BAS-109 to R-BAS-111).
type BastionGUI struct {
	PollInterval time.Duration `yaml:"poll_interval" default:"10s" validate:"min=1s,max=10m" doc:"How often the replica reads the change list of the public catalogue while the web interface is on (R-BAS-109); a feed entry of the kinds grants, settings, content and catalogue makes it read at once."`
	MaxBytes     ByteSize      `yaml:"max_bytes" default:"4GiB" validate:"min=1MiB" doc:"A share of cache.max_bytes for the public catalogue's documents as stored (compressed), reserved whether or not the web interface is on; a catalogue above it is kept and raises QuaderBastionCatalogueOverBound (R-BAS-110). The artifacts get max_bytes less documents_max_bytes less this, which must hold one object of max_object_bytes."`
	StaleAfter   time.Duration `yaml:"stale_after" default:"1h" validate:"min=1m,max=720h" doc:"The age of the catalogue's last confirmation after which the web interface says its information is old and QuaderBastionCatalogueStale fires (R-BAS-111); the pages are served whatever the age."`
}

// BastionCacheBucket is the Bastion's own bucket of the bucket backend (R-BAS-030).
type BastionCacheBucket struct {
	Endpoint       string                   `yaml:"endpoint" default:"" doc:"Empty means AWS endpoint resolution for the region; https only (R-BAS-044)."`
	Region         string                   `yaml:"region" default:""`
	Bucket         string                   `yaml:"bucket" default:"" doc:"The Bastion's own bucket; required with the bucket backend."`
	Prefix         string                   `yaml:"prefix" default:"quader-bastion"`
	ForcePathStyle bool                     `yaml:"force_path_style" default:"false" doc:"True for SeaweedFS, MinIO and Ceph RGW."`
	CAFile         string                   `yaml:"ca_file" default:"" chart:"managed" doc:"CA bundle that verifies the bucket endpoint, added to the system roots (trustedCA of the chart)."`
	Credentials    BastionBucketCredentials `yaml:"credentials"`
}

// BastionBucketCredentials selects where the bucket credential comes from (R-BAS-030). The static files are
// rendered by the chart from secrets.cacheBucket; the token file of the workload identity is the operator's.
type BastionBucketCredentials struct {
	Source               string `yaml:"source" default:"static" validate:"oneof=static|webIdentity|default"`
	RoleARN              string `yaml:"role_arn" default:""`
	AccessKeyIDFile      string `yaml:"access_key_id_file" default:"" secret:"file" chart:"managed"`
	SecretAccessKeyFile  string `yaml:"secret_access_key_file" default:"" secret:"file" chart:"managed"`
	WebIdentityTokenFile string `yaml:"web_identity_token_file" default:"" secret:"file" doc:"The projected service account token of the workload identity; set by the operator (the chart renders no volume for it)."`
}

// BastionEdgeLimit is the edge figure of the ingress in front of the replicas (R-BAS-038, R-BAS-093): what HAProxy Ingress
// admits per client address before a request reaches the Bastion. The chart renders it from edgeLimit; the replica only reports
// it, in Quader-Bastion-Edge-Limit of every handshake, and the main installation's card warns where a per-address figure of the
// configuration lies above it. Off (the default, and with edgeLimit.enabled false) reports "off".
type BastionEdgeLimit struct {
	Enabled           bool `yaml:"enabled" default:"false" doc:"Whether the ingress limits per address (edgeLimit.enabled)."`
	RequestsPerSecond int  `yaml:"requests_per_second" default:"0" validate:"min=0" doc:"The requests a second per address the ingress admits (edgeLimit.requestsPerSecond); 0 sets none."`
	Connections       int  `yaml:"connections" default:"0" validate:"min=0" doc:"The requests in flight per address the ingress admits (edgeLimit.connections); 0 sets none."`
}

// BastionAudit is the buffer of download events (R-BAS-039).
type BastionAudit struct {
	BufferEvents   int           `yaml:"buffer_events" default:"100000" validate:"min=1" doc:"Events held in memory per replica; one that finds the buffer full is dropped and counted."`
	BatchSize      int           `yaml:"batch_size" default:"500" validate:"min=1" doc:"Events per request to the main installation; at most bastions.downloads.max_batch there."`
	FlushInterval  time.Duration `yaml:"flush_interval" default:"1s" validate:"min=100ms,max=1h"`
	SpoolMaxEvents int           `yaml:"spool_max_events" default:"1000000" validate:"min=0" doc:"Events the drain writes to the cache backend's audit-spool at most; 0 spools nothing."`
}

// BastionToken is the warning about the Bastion token's expiry (R-BAS-006).
type BastionToken struct {
	WarnBefore time.Duration `yaml:"warn_before" default:"720h" validate:"min=1h" doc:"The log warns and QuaderBastionTokenExpiresSoon fires below this remaining lifetime of the token (R-BAS-006)."`
}

// BastionValkey is the optional Valkey of the Bastion (R-BAS-034): the keys of the chart quader (R-HA-056) with
// the mode none as the default. The credentials are chart-managed files.
type BastionValkey struct {
	Mode                 string           `yaml:"mode" default:"none" validate:"oneof=none|sentinel|standalone" doc:"none: no Valkey (the default); sentinel; standalone is for development only."`
	Sentinel             BastionSentinel  `yaml:"sentinel"`
	Address              string           `yaml:"address" default:"" doc:"Standalone mode only."`
	Database             int              `yaml:"database" default:"0" validate:"min=0"`
	KeyPrefix            string           `yaml:"key_prefix" default:"" doc:"Part of every key, quader-bastion:<key_prefix>:…; empty means the host of server.base_url (R-BAS-034)."`
	UsernameFile         string           `yaml:"username_file" default:"" chart:"managed" doc:"Empty means the default user; rendered by the chart from secrets.valkey."`
	PasswordFile         string           `yaml:"password_file" default:"" secret:"file" chart:"managed"`
	TLS                  BastionValkeyTLS `yaml:"tls"`
	AllowInsecure        bool             `yaml:"allow_insecure" default:"false" doc:"Required for tls.mode disable."`
	ConnectTimeout       time.Duration    `yaml:"connect_timeout" default:"2s" validate:"min=10ms"`
	CommandTimeout       time.Duration    `yaml:"command_timeout" default:"500ms" validate:"min=10ms"`
	SentinelQueryTimeout time.Duration    `yaml:"sentinel_query_timeout" default:"300ms" validate:"min=10ms" doc:"Per Sentinel tried."`
	MaxConnLifetime      time.Duration    `yaml:"max_conn_lifetime" default:"30m" validate:"min=1s"`
	HealthCheckInterval  time.Duration    `yaml:"health_check_interval" default:"5s" validate:"min=100ms"`
	Reconnect            ValkeyReconnect  `yaml:"reconnect"`
	CircuitBreaker       ValkeyBreaker    `yaml:"circuit_breaker"`
}

// BastionSentinel is ValkeySentinel with the Sentinels' credential files rendered by the chart (R-BAS-046): from
// secrets.valkeySentinel. The Sentinels of a production Valkey require a password, so the files are keys of the
// section; the fields equal ValkeySentinel, so the Valkey client reads the section by conversion.
type BastionSentinel struct {
	Addresses       []string      `yaml:"addresses" default:"[]" doc:"Sentinel host:port list; required in sentinel mode."`
	PrimaryName     string        `yaml:"primary_name" default:"" doc:"Name in the Sentinels' monitor line; required in sentinel mode."`
	UsernameFile    string        `yaml:"username_file" default:"" chart:"managed" doc:"Empty means the default user; rendered by the chart from secrets.valkeySentinel."`
	PasswordFile    string        `yaml:"password_file" default:"" secret:"file" chart:"managed"`
	TLS             bool          `yaml:"tls" default:"true" doc:"Use valkey.tls for Sentinel connections too."`
	RefreshInterval time.Duration `yaml:"refresh_interval" default:"5s" validate:"min=1s" doc:"Periodic reconciliation with the Sentinels."`
}

// BastionValkeyTLS is ValkeyTLS with ca_file rendered by the chart (R-BAS-046): from secrets.valkeyCA or, without
// it, from the trust bundle trustedCA. The other keys are the operator's. The fields equal ValkeyTLS, so the
// Valkey client reads the section by conversion.
type BastionValkeyTLS struct {
	Mode       string `yaml:"mode" default:"verify-full" validate:"oneof=verify-full|verify-ca|disable"`
	CAFile     string `yaml:"ca_file" default:"" chart:"managed" doc:"CA bundle that verifies Valkey and the Sentinels, added to the system roots; rendered by the chart from secrets.valkeyCA or trustedCA."`
	CertFile   string `yaml:"cert_file" default:""`
	KeyFile    string `yaml:"key_file" default:"" secret:"file"`
	ServerName string `yaml:"server_name" default:"" doc:"Empty means the host name of each address."`
}

// BastionTracing is the tracing section of the role (R-BAS-042): the keys of the main installation's section
// except the ones about incoming context. The Bastion never continues a trace context a caller sends
// (incoming_context is none, fixed) and always propagates its own to the main installation.
type BastionTracing struct {
	Enabled       bool    `yaml:"enabled" default:"false"`
	Endpoint      string  `yaml:"endpoint" default:"" doc:"Collector endpoint, for example https://otel-collector.observability:4318; https unless allow_insecure is true."`
	Protocol      string  `yaml:"protocol" default:"http/protobuf" validate:"oneof=grpc|http/protobuf"`
	CAFile        string  `yaml:"ca_file" default:"" chart:"managed" doc:"CA bundle that verifies the collector, added to the system roots (trustedCA of the chart)."`
	AllowInsecure bool    `yaml:"allow_insecure" default:"false" doc:"Lets spans go to the collector in plaintext; for a collector on loopback or behind a sidecar only (INV-20)."`
	HeadersFile   string  `yaml:"headers_file" default:"" secret:"file" doc:"One \"Name: value\" per line."`
	SampleRatio   float64 `yaml:"sample_ratio" default:"0.1" validate:"min=0,max=1"`
	QueueSize     int     `yaml:"queue_size" default:"2048" validate:"min=1"`
}

// Tracing returns the equivalent section of the main tree, the form the tracing setup reads: incoming context
// is never continued and the Bastion's context is propagated (R-BAS-042).
func (t BastionTracing) Tracing() Tracing {
	return Tracing{
		Enabled: t.Enabled, Endpoint: t.Endpoint, Protocol: t.Protocol, CAFile: t.CAFile, AllowInsecure: t.AllowInsecure,
		HeadersFile: t.HeadersFile, SampleRatio: t.SampleRatio, IncomingContext: "none", TrustedPeers: nil,
		PropagateToUpstreams: true, QueueSize: t.QueueSize,
	}
}

// Validate checks the section with the rules of the main tree's tracing section.
func (t *BastionTracing) Validate(r *Report) {
	tr := t.Tracing()
	tr.Validate(r)
}

// Validate checks the cross-field rules of the server section (R-BAS-001, R-BAS-044).
func (s *BastionServer) Validate(r *Report) {
	srv := Server{TLS: s.TLS, OpsTLS: s.OpsTLS}
	srv.validateTLS(r)
	for _, h := range []string{s.Listen, s.OpsListen} {
		if _, _, err := net.SplitHostPort(h); err != nil {
			r.Add("listen", "%q is not a host:port address", h)
		}
	}
	if s.Listen == s.OpsListen && !kernelPort(s.Listen) {
		r.Add("ops_listen", "must differ from listen")
	}
	if s.BaseURL != "" {
		checkOrigin(r, "base_url", s.BaseURL)
	}
	checkTrustedProxies(r, "trusted_proxies", s.TrustedProxies)
}

// Validate checks the cross-field rules of the section bastion.
func (b *BastionSettings) Validate(r *Report) {
	m := b.Main
	if m.URL != "" {
		checkMainURL(r, "main.url", m.URL)
	}
	if m.PackagesURL != "" {
		checkMainURL(r, "main.packages_url", m.PackagesURL)
	}
	if min := 3 * b.Feed.PollInterval; m.UnreachableAfter < min {
		r.Add("main.unreachable_after", "(%s) must be at least 3 x feed.poll_interval (%s): a feed poll that is merely late must not make the main installation unreachable (R-BAS-027)", m.UnreachableAfter, b.Feed.PollInterval)
	}
	c := b.Cache
	// The artifacts' room is what the two shares leave (R-BAS-031, R-BAS-110); below one largest object, or below nothing, the
	// cache would refuse the fills it is there for.
	if room := c.MaxBytes - c.DocumentsMaxBytes - b.GUI.MaxBytes; room < c.MaxObjectBytes {
		left := "leaves the artifacts " + room.String()
		if room < 0 {
			left = "leaves the artifacts nothing"
		}
		r.Add("cache.max_bytes", "(%s) less cache.documents_max_bytes (%s) and gui.max_bytes (%s) %s, less than one object of cache.max_object_bytes (%s): the cache would refuse the fills it is there for (R-BAS-031, R-BAS-110); raise max_bytes, or lower documents_max_bytes, gui.max_bytes or max_object_bytes",
			c.MaxBytes, c.DocumentsMaxBytes, b.GUI.MaxBytes, left, c.MaxObjectBytes)
	}
	if c.SpoolMaxBytes < c.MaxObjectBytes {
		r.Add("cache.spool_max_bytes", "(%s) must be at least cache.max_object_bytes (%s): an object up to that size is spooled whole before it is kept, so a smaller spool would never keep one of the sizes the limit allows (Q-BAS-131); lower max_object_bytes or raise spool_max_bytes", c.SpoolMaxBytes, c.MaxObjectBytes)
	}
	if c.Backend == "bucket" {
		c.validateBucket(r)
	}
	if b.Audit.BatchSize > b.Audit.BufferEvents {
		r.Add("audit.batch_size", "(%d) must not exceed audit.buffer_events (%d)", b.Audit.BatchSize, b.Audit.BufferEvents)
	}
}

// validateBucket checks the bucket backend's keys.
func (c BastionCache) validateBucket(r *Report) {
	b := c.Bucket
	if b.Bucket == "" {
		r.Add("cache.bucket.bucket", "is required with cache.backend bucket (R-BAS-030)")
	}
	if b.Endpoint != "" {
		u, err := url.Parse(NormalizeEndpoint(b.Endpoint))
		if err != nil || u.Host == "" || u.Scheme != "https" {
			r.Add("cache.bucket.endpoint", "must be an https URL (is %q): the cache bucket is reached over TLS (R-BAS-044)", redactUserinfo(b.Endpoint))
		} else if u.User != nil {
			r.Add("cache.bucket.endpoint", "must not carry user information: the credentials come from cache.bucket.credentials")
		}
	}
	switch p := b.Prefix; {
	case strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/"):
		r.Add("cache.bucket.prefix", "must not start or end with /")
	case strings.Contains(p, "//"):
		r.Add("cache.bucket.prefix", "must not contain //")
	}
}

// checkMainURL requires an https origin (R-BAS-044): a plain http URL stops startup and names the key.
func checkMainURL(r *Report, key, raw string) {
	u, err := url.Parse(raw)
	if err == nil && u.Scheme == "http" {
		r.Add(key, "%q is an http URL: the connection to the main installation carries the Bastion token and the callers' credentials and runs over https only (R-BAS-044)", redactUserinfo(raw))
		return
	}
	checkOrigin(r, key, raw)
	if err == nil && u.Scheme != "https" && u.Scheme != "http" {
		return // checkOrigin has named the scheme
	}
}

// Validate checks the cross-field rules of the Valkey section (R-BAS-034, R-ARCH-031).
func (v *BastionValkey) Validate(r *Report) {
	if v.Mode == "none" {
		return
	}
	asMain := v.Valkey()
	asMain.Validate(r)
	if v.KeyPrefix != "" && strings.ContainsAny(v.KeyPrefix, " \t\r\n") {
		r.Add("key_prefix", "must not contain white space")
	}
}

// ValidateFor requires the connection settings of the chosen mode where a replica connects to Valkey.
func (v *BastionValkey) ValidateFor(cmd Command, r *Report) {
	if v.Mode == "none" {
		return
	}
	asMain := v.Valkey()
	asMain.ValidateFor(cmd, r)
}

// Valkey returns the section in the form of the main tree, which the Valkey client reads. The key prefix is the
// Bastion's keyspace quader-bastion:<key_prefix>: (R-BAS-034).
func (v BastionValkey) Valkey() Valkey {
	return Valkey{
		Mode: v.Mode, Sentinel: ValkeySentinel(v.Sentinel), Address: v.Address, Database: v.Database,
		KeyPrefix:    BastionKeyspace + strings.TrimSuffix(v.KeyPrefix, ":") + ":",
		UsernameFile: v.UsernameFile, PasswordFile: v.PasswordFile, TLS: ValkeyTLS(v.TLS), AllowInsecure: v.AllowInsecure,
		ConnectTimeout: v.ConnectTimeout, CommandTimeout: v.CommandTimeout, SentinelQueryTimeout: v.SentinelQueryTimeout,
		MaxConnLifetime: v.MaxConnLifetime, HealthCheckInterval: v.HealthCheckInterval,
		Reconnect: v.Reconnect, CircuitBreaker: v.CircuitBreaker,
	}
}

// BastionKeyspace starts every Valkey key of a Bastion (R-BAS-034).
const BastionKeyspace = "quader-bastion:"

// ValidateFor requires what a serving Bastion needs (R-BAS-001, R-BAS-006, R-BAS-030, R-BAS-032, R-BAS-034).
func (c *BastionConfig) ValidateFor(cmd Command, r *Report) {
	if !cmd.runsReplica() {
		return
	}
	b := c.Bastion
	if b.Main.TokenFile == "" {
		r.Add("bastion.main.token_file", "is required to serve: the Bastion token is read from this file (R-BAS-006); the chart renders it from secrets.mainToken")
	}
	if (b.Cache.Backend == "bucket" || c.Valkey.Mode != "none") && b.SigningKeyFile == "" {
		r.Add("bastion.signing_key_file", "is required with the bucket backend or Valkey: the entries and answers they hold are signed (R-BAS-032)")
	}
	if b.Cache.Backend == "volume" && b.Cache.Volume.Dir == "" {
		r.Add("bastion.cache.volume.dir", "is required with the volume backend")
	}
	if b.Cache.Backend == "bucket" && b.Cache.Bucket.Credentials.Source == "static" &&
		(b.Cache.Bucket.Credentials.AccessKeyIDFile == "" || b.Cache.Bucket.Credentials.SecretAccessKeyFile == "") {
		r.Add("bastion.cache.bucket.credentials", "source static needs access_key_id_file and secret_access_key_file (secrets.cacheBucket of the chart)")
	}
	if b.Cache.Backend == "bucket" && b.Cache.Bucket.Credentials.Source == "webIdentity" && b.Cache.Bucket.Credentials.WebIdentityTokenFile == "" {
		r.Add("bastion.cache.bucket.credentials.web_identity_token_file", "is required with source webIdentity")
	}
}

// Validate checks the rules that span sections (R-BAS-038 is the chart's; the rest are the binary's).
func (c *BastionConfig) Validate(r *Report) {
	if g := c.Runtime.TerminationGracePeriod; g > 0 {
		fixed := BastionShutdownDrainDelay + ShutdownMargin
		if g-fixed < minShutdownTimeout {
			r.Add("runtime.termination_grace_period", "%s leaves no shutdown time after the drain delay (%s) and the %s margin", g, BastionShutdownDrainDelay, ShutdownMargin)
		}
	}
}

// BastionShutdownDrainDelay is the time between the readiness flip and the stop of the listeners at a drain
// (R-BAS-043, R-ARCH-024): the default of health.shutdown_drain_delay of the main installation. The role has
// no health section, so the value is fixed.
const BastionShutdownDrainDelay = 5 * time.Second

// Finalize derives the key prefix of Valkey from the host of server.base_url when it is empty (R-BAS-034).
func (c *BastionConfig) Finalize(sources map[string]Source) {
	if c.Valkey.KeyPrefix != "" {
		return
	}
	if u, err := url.Parse(c.Server.BaseURL); err == nil && u.Host != "" {
		c.Valkey.KeyPrefix = u.Host
		sources["valkey.key_prefix"] = Source{Kind: "derived", Detail: "server.base_url"}
	}
}

// ServerSection returns the server section in the form of the main tree, which the listeners read: the keys of
// the role, every other one at its default, and the shutdown timeout derived from the grace period like
// Config.Finalize does (R-ARCH-024, R-HA-011).
func (c *BastionConfig) ServerSection() Server {
	s := mustDefaults[Server]()
	b := c.Server
	s.Listen, s.OpsListen, s.BaseURL = b.Listen, b.OpsListen, b.BaseURL
	s.TrustedProxies, s.ForwardedHeader, s.MaxInFlight = b.TrustedProxies, b.ForwardedHeader, b.MaxInFlight
	s.TLS, s.OpsTLS = b.TLS, b.OpsTLS
	if grace := c.Runtime.TerminationGracePeriod; grace > 0 {
		s.ShutdownTimeout = grace - BastionShutdownDrainDelay - ShutdownMargin
	} else {
		s.ShutdownTimeout = DefaultShutdownTimeout
	}
	return s
}

// MetricsSection returns the metrics section of the main tree at its defaults: the ops listener serves
// /metrics without authentication, as the Bastion's chart scrapes it through the ops port (R-BAS-040). The
// role has no metrics keys of its own.
func (c *BastionConfig) MetricsSection() Metrics { return mustDefaults[Metrics]() }

// HealthSection returns the health section of the main tree at its defaults, with the drain delay of the role
// (BastionShutdownDrainDelay). The role has no health keys of its own.
func (c *BastionConfig) HealthSection() Health {
	h := mustDefaults[Health]()
	h.ShutdownDrainDelay = BastionShutdownDrainDelay
	return h
}

// mustDefaults returns a T with every key at its default, read from the tags like the loader reads them.
func mustDefaults[T any]() T {
	var v T
	rv := reflect.ValueOf(&v).Elem()
	schema, errs := Describe(rv.Type())
	if len(errs) > 0 {
		panic(fmt.Sprintf("config tree %T is invalid: %v", v, errs))
	}
	for _, k := range schema.Keys {
		if !k.HasDefault {
			continue
		}
		d, err := parseText(k.Type, k.Default)
		if err != nil {
			panic(fmt.Sprintf("%s: default %q: %v", k.Path, k.Default, err))
		}
		rv.FieldByIndex(k.Index).Set(d)
	}
	return v
}

// BastionDescribe flattens the tree of the role.
func BastionDescribe() *Schema {
	s, errs := Describe(reflect.TypeOf(BastionConfig{}))
	if len(errs) > 0 {
		panic(fmt.Sprintf("bastion config tree is invalid: %v", errs))
	}
	return s
}

// BastionLoaded is the result of a successful LoadBastion.
type BastionLoaded struct {
	Config  *BastionConfig
	Schema  *Schema
	Sources map[string]Source
	File    string
}

// LoadBastion builds the Bastion's configuration: defaults, the file, the environment (R-OPS-028). A key that
// belongs to no section of the role stops it with an error that names the key and the role (R-BAS-001). The
// role is chosen by --role, which this function does not see; the one flag it accepts is --log-level, and every
// other setting is a value of the chart (R-BAS-002).
func LoadBastion(opts Options) (*BastionLoaded, error) {
	cfg := &BastionConfig{}
	opts.Role = BastionRole
	var refused []string
	for k := range opts.Flags {
		if k != "log.level" {
			refused = append(refused, k)
		}
	}
	if len(refused) > 0 {
		sort.Strings(refused)
		return nil, fmt.Errorf("the role %s takes no configuration flags but --log-level (%s): every setting is a value of its chart (R-BAS-002)", BastionRole, strings.Join(refused, ", "))
	}
	loaded, err := loadInto(cfg, opts)
	if err != nil {
		return nil, err
	}
	return &BastionLoaded{Config: cfg, Schema: loaded.Schema, Sources: loaded.Sources, File: loaded.File}, nil
}

// StorageS3 returns the object-store client configuration of the bucket backend (R-BAS-030): the keys of the
// Bastion's bucket on the defaults of an object-store client (timeouts, retries, checksums), the endpoint reached over TLS
// only and the credentials of cache.bucket.credentials. The cache bucket uses no server-side encryption setting
// of its own, so the bucket's default applies.
func (b BastionCacheBucket) StorageS3() StorageS3 {
	s := mustDefaults[StorageS3]()
	s.Names = StorageS3Names{Settings: "bastion.cache.bucket"}
	s.Endpoint, s.Region, s.Bucket, s.Prefix = b.Endpoint, b.Region, b.Bucket, b.Prefix
	s.ForcePathStyle, s.AllowInsecureEndpoint, s.CAFile = b.ForcePathStyle, false, b.CAFile
	s.Credentials = StorageS3Credentials{
		Source: b.Credentials.Source, RoleARN: b.Credentials.RoleARN, WebIdentityTokenFile: b.Credentials.WebIdentityTokenFile,
		AccessKeyIDFile: b.Credentials.AccessKeyIDFile, SecretAccessKeyFile: b.Credentials.SecretAccessKeyFile,
	}
	s.StartupCheck = false // the cache has its own first-readiness check (cache.Backend.Check)
	// A cache that is slow to fail is worse than one that misses: a request must not wait out the SDK's default
	// five attempts and 20 s of backoff before it is passed through (R-BAS-030, Q-BAS-89).
	s.Retry = StorageS3Retry{MaxAttempts: 3, MaxBackoff: 2 * time.Second}
	return s
}
