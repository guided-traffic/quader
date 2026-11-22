package config

import (
	"net/url"
	"strings"
	"time"
)

// Storage configures the content-addressed blob store, its object-store connections and its spool
// (R-STOR-001 to R-STOR-063). The credential files of the connections are file-only secrets.
type Storage struct {
	Connections      []StorageConnection     `yaml:"connections" default:"[]" doc:"The object-store connections, one to eight (R-STOR-050). The list order is the write order and the read order; every connection that is not draining holds every blob. An element without id is default, and at most one element may leave it out. In the environment the whole list is one JSON value (QUADER_STORAGE_CONNECTIONS)."`
	ObjectStore      StorageObjectStore      `yaml:"object_store" doc:"The client of every connection (R-STOR-033 to R-STOR-035)."`
	Replication      StorageReplication      `yaml:"replication"`
	Reconcile        StorageReconcile        `yaml:"reconcile" doc:"The reconciliation between the connections (R-STOR-056)."`
	Spool            StorageSpool            `yaml:"spool"`
	Upload           StorageUpload           `yaml:"upload"`
	Uploads          StorageUploads          `yaml:"uploads" doc:"Multi-request upload sessions (P1)."`
	DeployByChecksum StorageDeployByChecksum `yaml:"deploy_by_checksum"`
	Sidecars         StorageSidecars         `yaml:"sidecars"`
	Download         StorageDownload         `yaml:"download"`
	Stats            StorageStats            `yaml:"stats"`
	Delete           StorageDelete           `yaml:"delete"`
	Properties       StorageProperties       `yaml:"properties"`
	Usage            StorageUsage            `yaml:"usage"`
	GC               StorageGC               `yaml:"gc"`
	Orphans          StorageOrphans          `yaml:"orphans"`
	Scrub            StorageScrub            `yaml:"scrub"`
}

// StorageS3 is the configuration of one S3 client: the client of an object-store connection (a connection's element with the
// shared settings of storage.object_store, [ClientConfig]), of the backup target and of the Bastion's cache bucket. It is no
// key of the configuration file: storage.s3 is gone since several connections (R-STOR-050); the defaults of its tags are the
// shared defaults those clients start from.
type StorageS3 struct {
	Endpoint              string               `yaml:"endpoint" default:"" doc:"Empty means AWS endpoint resolution for the region. A host without a scheme means https://; plain HTTP needs http:// and allow_insecure_endpoint."`
	Region                string               `yaml:"region" default:""`
	Bucket                string               `yaml:"bucket" default:""`
	Prefix                string               `yaml:"prefix" default:"quader"`
	ForcePathStyle        bool                 `yaml:"force_path_style" default:"false" doc:"True for SeaweedFS, MinIO and Ceph RGW."`
	AllowInsecureEndpoint bool                 `yaml:"allow_insecure_endpoint" default:"false"`
	CAFile                string               `yaml:"ca_file" default:"" doc:"Empty means the system trust store."`
	StorageClass          string               `yaml:"storage_class" default:""`
	PayloadChecksum       string               `yaml:"payload_checksum" default:"sha256" validate:"oneof=sha256|md5|none"`
	Checksums             StorageS3Checksums   `yaml:"checksums"`
	Credentials           StorageS3Credentials `yaml:"credentials"`
	SSE                   StorageS3SSE         `yaml:"sse"`
	HTTP                  StorageS3HTTP        `yaml:"http"`
	Retry                 StorageS3Retry       `yaml:"retry"`
	CircuitBreaker        StorageS3Breaker     `yaml:"circuit_breaker"`
	Multipart             StorageS3Multipart   `yaml:"multipart"`
	StartupCheck          bool                 `yaml:"startup_check" default:"true" doc:"Object-store conformance check before the first readiness (R-STOR-035)."`
	Names                 StorageS3Names       `yaml:"names" doc:"Where the client's settings are written in the configuration, for the errors that name one; set by whoever builds the client."`
}

// StorageS3Names says where the settings of one S3 client are written in the configuration, so that an error names the key
// an operator sets: [ClientConfig] fills it for a connection, the backup target and the Bastion's cache bucket for theirs.
type StorageS3Names struct {
	Settings string `yaml:"settings" default:"" doc:"The client's own settings: storage.connections[<n>] (<id>): for a connection, backup.target, bastion.cache.bucket."`
	HTTP     string `yaml:"http" default:"" doc:"Its HTTP settings: storage.object_store.http for a connection."`
}

// Setting names one of the client's own settings in an error, as the validation of the configuration names it:
// "storage.connections[0] (default): ca_file", "backup.target.ca_file".
func (s StorageS3) Setting(name string) string { return joinSetting(s.Names.Settings, name) }

// HTTPSetting names one of the client's HTTP settings in an error: "storage.object_store.http.proxy_url" for a connection.
func (s StorageS3) HTTPSetting(name string) string {
	if s.Names.HTTP == "" {
		return joinSetting(s.Names.Settings, "http."+name)
	}
	return joinSetting(s.Names.HTTP, name)
}

func joinSetting(base, name string) string {
	switch {
	case base == "":
		return "object store " + name
	case strings.HasSuffix(base, ":"):
		return base + " " + name
	}
	return base + "." + name
}

// StorageS3Checksums are the SDK's checksum behaviours.
type StorageS3Checksums struct {
	RequestCalculation string `yaml:"request_calculation" default:"when_supported" validate:"oneof=when_supported|when_required"`
	ResponseValidation string `yaml:"response_validation" default:"when_supported" validate:"oneof=when_supported|when_required"`
}

// StorageS3Credentials selects where an object-store credential comes from (R-STOR-031). Every `_file` key is a file-only
// secret.
type StorageS3Credentials struct {
	Source               string `yaml:"source" default:"default" validate:"oneof=default|webIdentity|static"`
	RoleARN              string `yaml:"role_arn" default:""`
	WebIdentityTokenFile string `yaml:"web_identity_token_file" default:"" secret:"file"`
	STSEndpoint          string `yaml:"sts_endpoint" default:""`
	AccessKeyIDFile      string `yaml:"access_key_id_file" default:"" secret:"file"`
	SecretAccessKeyFile  string `yaml:"secret_access_key_file" default:"" secret:"file"`
	SessionTokenFile     string `yaml:"session_token_file" default:"" secret:"file"`
}

// StorageS3SSE is server-side encryption of stored objects.
type StorageS3SSE struct {
	Mode      string `yaml:"mode" default:"none" validate:"oneof=none|s3|kms"`
	KMSKeyID  string `yaml:"kms_key_id" default:""`
	BucketKey bool   `yaml:"bucket_key" default:"true"`
}

// StorageS3HTTP tunes an object-store client's HTTP transport (R-STOR-033).
type StorageS3HTTP struct {
	MaxConnections        int           `yaml:"max_connections" default:"256" validate:"min=1" doc:"Per connection and replica."`
	DialTimeout           time.Duration `yaml:"dial_timeout" default:"5s" validate:"min=100ms"`
	TLSHandshakeTimeout   time.Duration `yaml:"tls_handshake_timeout" default:"5s" validate:"min=100ms"`
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout" default:"30s" validate:"min=1s"`
	IdleConnTimeout       time.Duration `yaml:"idle_conn_timeout" default:"90s"`
	IdleTransferTimeout   time.Duration `yaml:"idle_transfer_timeout" default:"60s"`
	ReceiveWindow         ByteSize      `yaml:"receive_window" default:"256KiB" validate:"min=16KiB,max=1GiB" doc:"HTTP/2 only: how far the store may send ahead of what Quader has read of one object; it bounds what a slow download holds (Q-ARCH-241). The transport's own default is 4 MiB."`
	ProxyURL              string        `yaml:"proxy_url" default:"" doc:"Empty means the environment."`
	ProxyCredentialsFile  string        `yaml:"proxy_credentials_file" default:"" secret:"file"`
	NoProxy               string        `yaml:"no_proxy" default:""`
}

// StorageS3Retry bounds SDK retries.
type StorageS3Retry struct {
	MaxAttempts int           `yaml:"max_attempts" default:"5" validate:"min=1"`
	MaxBackoff  time.Duration `yaml:"max_backoff" default:"20s" validate:"min=100ms"`
}

// StorageS3Breaker is the circuit breaker of an object-store client; one per connection and replica (R-STOR-060).
type StorageS3Breaker struct {
	FailureThreshold int           `yaml:"failure_threshold" default:"5" validate:"min=1"`
	OpenDuration     time.Duration `yaml:"open_duration" default:"10s" validate:"min=100ms"`
}

// StorageS3Multipart configures multipart transfers.
type StorageS3Multipart struct {
	Threshold          ByteSize `yaml:"threshold" default:"100MiB" validate:"min=5MiB"`
	PartSize           ByteSize `yaml:"part_size" default:"16MiB" validate:"min=5MiB"`
	Concurrency        int      `yaml:"concurrency" default:"4" validate:"min=1" doc:"Parallel parts per upload."`
	MaxConcurrentParts int      `yaml:"max_concurrent_parts" default:"64" validate:"min=1" doc:"Per replica, over all connections."`
}

// StorageObjectStore is what every connection's client shares (R-STOR-050, Q-STOR-220): the HTTP transport, the retries, the
// circuit breaker's thresholds, the multipart sizing and the start check.
type StorageObjectStore struct {
	HTTP           StorageS3HTTP      `yaml:"http"`
	Retry          StorageS3Retry     `yaml:"retry"`
	CircuitBreaker StorageS3Breaker   `yaml:"circuit_breaker"`
	Multipart      StorageS3Multipart `yaml:"multipart"`
	StartupCheck   bool               `yaml:"startup_check" default:"true" doc:"The start check of every connection before the first readiness, with its capabilities (R-STOR-035)."`
}

// StorageReplication configures how many copies an upload needs before it is answered (R-STOR-053).
type StorageReplication struct {
	MinCopies int `yaml:"min_copies" default:"1" validate:"min=1,max=8" doc:"Copies written before an upload is answered; at most the number of connections without drain: true. The reconciliation brings a blob to the other connections afterwards."`
}

// StorageReconcile tunes the reconciliation between the connections (R-STOR-056).
type StorageReconcile struct {
	Concurrency     int           `yaml:"concurrency" default:"4" validate:"min=1" doc:"Copies at a time in one run."`
	BytesPerSecond  ByteSize      `yaml:"bytes_per_second" default:"100MiB" validate:"min=1MiB" doc:"Streamed bytes per run; server-side copies are not counted."`
	ServerSide      string        `yaml:"server_side" default:"auto" validate:"oneof=auto|never" doc:"auto copies server-side between two connections on one endpoint."`
	QueueLimit      int           `yaml:"queue_limit" default:"10000" validate:"min=1" doc:"Gaps the pass queues per connection before it waits for the copier."`
	RetryMaxBackoff time.Duration `yaml:"retry_max_backoff" default:"1h" validate:"min=1s"`
	RunDuration     time.Duration `yaml:"run_duration" default:"10m" validate:"min=1m" doc:"A run ends after this and the next scheduled run continues."`
}

// StorageConnection is one element of storage.connections (R-STOR-050): one bucket and prefix on one S3-compatible endpoint
// with everything that may differ between providers (Q-STOR-222). An element of a list is decoded as it is written, so a key
// left out is the zero value; the pointer fields tell an empty prefix and a false switch from a key left out, and
// [StorageConnection.WithDefaults] applies the defaults, which the loader writes back after validation.
type StorageConnection struct {
	ID                         string        `yaml:"id" doc:"Names the connection for its whole life; default for at most one element, never changes, never reused."`
	Endpoint                   string        `yaml:"endpoint" doc:"Empty means AWS endpoint resolution for the region; a host without a scheme means https://."`
	EndpointFile               string        `yaml:"endpoint_file" doc:"A file whose trimmed content is the endpoint (a provisioner's Secret), read once at start; excludes endpoint."`
	Region                     string        `yaml:"region"`
	RegionFile                 string        `yaml:"region_file" doc:"A file whose trimmed content is the region; excludes region."`
	Bucket                     string        `yaml:"bucket" doc:"Required unless bucket_file."`
	BucketFile                 string        `yaml:"bucket_file" doc:"A file whose trimmed content is the bucket; excludes bucket."`
	Prefix                     *string       `yaml:"prefix" doc:"Default quader; fixed once the connection holds data (R-STOR-051)."`
	ForcePathStyle             bool          `yaml:"force_path_style" doc:"True for SeaweedFS, MinIO and Ceph RGW."`
	AllowInsecureEndpoint      bool          `yaml:"allow_insecure_endpoint"`
	CAFile                     string        `yaml:"ca_file" doc:"The connection's trust anchor; empty means the outbound trust store."`
	StorageClass               string        `yaml:"storage_class"`
	PayloadChecksum            string        `yaml:"payload_checksum" doc:"sha256 (default), md5 or none."`
	RequestChecksumCalculation string        `yaml:"request_checksum_calculation" doc:"when_supported (default) or when_required."`
	ResponseChecksumValidation string        `yaml:"response_checksum_validation" doc:"when_supported (default) or when_required."`
	BatchDelete                *bool         `yaml:"batch_delete" doc:"Default true; false for a store without DeleteObjects."`
	CredentialsSource          string        `yaml:"credentials_source" doc:"default (default), webIdentity or static (R-STOR-031)."`
	RoleARN                    string        `yaml:"role_arn"`
	WebIdentityTokenFile       string        `yaml:"web_identity_token_file" secret:"file"`
	STSEndpoint                string        `yaml:"sts_endpoint"`
	AccessKeyIDFile            string        `yaml:"access_key_id_file" secret:"file"`
	SecretAccessKeyFile        string        `yaml:"secret_access_key_file" secret:"file"`
	SessionTokenFile           string        `yaml:"session_token_file" secret:"file"`
	SSEMode                    string        `yaml:"sse_mode" doc:"none (default), s3 or kms (R-STOR-032)."`
	SSEKMSKeyID                string        `yaml:"sse_kms_key_id"`
	SSEBucketKey               *bool         `yaml:"sse_bucket_key" doc:"Default true."`
	ScrubPeriod                time.Duration `yaml:"scrub_period" doc:"Default 720h: every copy is read whole once in this period (R-STOR-027); at least 24h."`
	ScrubBytesPerSecond        ByteSize      `yaml:"scrub_bytes_per_second" doc:"Default 50MiB, per connection and run; at least 1MiB."`
	Drain                      bool          `yaml:"drain" doc:"Takes the connection out: no upload goes to it, its blobs are copied to the others and then deleted here (R-STOR-051)."`
	Encrypted                  bool          `yaml:"encrypted" doc:"The encryption of stored content; true is refused until it is built (R-STOR-050)."`
}

// MaxConnections is the most connections a configuration holds (Q-STOR-247).
const MaxConnections = 8

// DefaultConnectionID is the id of the element that leaves its id out (R-STOR-050).
const DefaultConnectionID = "default"

// The defaults of a connection's element (R-STOR-050).
const (
	defaultConnectionPrefix  = "quader"
	defaultScrubPeriod       = 720 * time.Hour
	defaultScrubBytesPerSec  = 50 * MiB
	minScrubPeriod           = 24 * time.Hour
	minScrubBytesPerSecond   = MiB
	defaultPayloadChecksum   = "sha256"
	defaultChecksumMode      = "when_supported"
	defaultCredentialsSource = "default"
	defaultSSEMode           = "none"
)

// ValidConnectionID reports whether id matches the pattern of a connection id, ^[a-z][a-z0-9-]{0,30}[a-z0-9]$ (R-STOR-050).
func ValidConnectionID(id string) bool {
	if len(id) < 2 || len(id) > 32 || id[0] < 'a' || id[0] > 'z' {
		return false
	}
	for i := 1; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-' && i < len(id)-1:
		default:
			return false
		}
	}
	return true
}

// WithDefaults returns the element with the default of every key that was left out (R-STOR-050).
func (c StorageConnection) WithDefaults() StorageConnection {
	if c.ID == "" {
		c.ID = DefaultConnectionID
	}
	if c.Prefix == nil {
		p := defaultConnectionPrefix
		c.Prefix = &p
	}
	if c.PayloadChecksum == "" {
		c.PayloadChecksum = defaultPayloadChecksum
	}
	if c.RequestChecksumCalculation == "" {
		c.RequestChecksumCalculation = defaultChecksumMode
	}
	if c.ResponseChecksumValidation == "" {
		c.ResponseChecksumValidation = defaultChecksumMode
	}
	if c.BatchDelete == nil {
		t := true
		c.BatchDelete = &t
	}
	if c.CredentialsSource == "" {
		c.CredentialsSource = defaultCredentialsSource
	}
	if c.SSEMode == "" {
		c.SSEMode = defaultSSEMode
	}
	if c.SSEBucketKey == nil {
		t := true
		c.SSEBucketKey = &t
	}
	if c.ScrubPeriod == 0 {
		c.ScrubPeriod = defaultScrubPeriod
	}
	if c.ScrubBytesPerSecond == 0 {
		c.ScrubBytesPerSecond = defaultScrubBytesPerSec
	}
	return c
}

// KeyPrefix is the connection's prefix, quader when it was left out.
func (c StorageConnection) KeyPrefix() string {
	if c.Prefix == nil {
		return defaultConnectionPrefix
	}
	return *c.Prefix
}

// BatchDeletes reports whether the connection deletes with DeleteObjects (true when left out).
func (c StorageConnection) BatchDeletes() bool { return c.BatchDelete == nil || *c.BatchDelete }

// ClientConfig is the client configuration of the connection at position i of storage.connections: the element's keys and
// the shared settings of storage.object_store (Q-STOR-220), with the names its errors give a setting
// ("storage.connections[0] (default): ca_file", as the validation names them). The element should carry its defaults
// (WithDefaults) and its resolved files ([ResolveConnections]).
func ClientConfig(i int, c StorageConnection, o StorageObjectStore) StorageS3 {
	c = c.WithDefaults()
	return StorageS3{
		Names:    StorageS3Names{Settings: "storage." + connectionKey(i, c.ID), HTTP: "storage.object_store.http"},
		Endpoint: c.Endpoint, Region: c.Region, Bucket: c.Bucket, Prefix: c.KeyPrefix(),
		ForcePathStyle: c.ForcePathStyle, AllowInsecureEndpoint: c.AllowInsecureEndpoint, CAFile: c.CAFile,
		StorageClass: c.StorageClass, PayloadChecksum: c.PayloadChecksum,
		Checksums: StorageS3Checksums{RequestCalculation: c.RequestChecksumCalculation, ResponseValidation: c.ResponseChecksumValidation},
		Credentials: StorageS3Credentials{
			Source: c.CredentialsSource, RoleARN: c.RoleARN, WebIdentityTokenFile: c.WebIdentityTokenFile, STSEndpoint: c.STSEndpoint,
			AccessKeyIDFile: c.AccessKeyIDFile, SecretAccessKeyFile: c.SecretAccessKeyFile, SessionTokenFile: c.SessionTokenFile,
		},
		SSE:  StorageS3SSE{Mode: c.SSEMode, KMSKeyID: c.SSEKMSKeyID, BucketKey: *c.SSEBucketKey},
		HTTP: o.HTTP, Retry: o.Retry, CircuitBreaker: o.CircuitBreaker, Multipart: o.Multipart, StartupCheck: o.StartupCheck,
	}
}

// StorageSpool is the replica-local scratch space (R-STOR-004, R-HA-035).
type StorageSpool struct {
	Dir             string   `yaml:"dir" default:"/var/lib/quader/spool" validate:"required"`
	MaxBytes        ByteSize `yaml:"max_bytes" default:"50GiB" validate:"min=1MiB" doc:"Budget shared by all concurrent uploads of a replica; the chart derives it."`
	MinFreeBytes    ByteSize `yaml:"min_free_bytes" default:"2GiB" validate:"min=0"`
	MemoryThreshold ByteSize `yaml:"memory_threshold" default:"1MiB" validate:"min=0"`
	WarnPercent     int      `yaml:"warn_percent" default:"80" validate:"min=1,max=100"`
}

// StorageUpload bounds one upload and its commit (R-STOR-005).
type StorageUpload struct {
	MaxSize         ByteSize      `yaml:"max_size" default:"10GiB" validate:"min=1MiB" doc:"Largest upload; must fit spool.max_bytes on the roles web and all (a worker accepts no uploads)."`
	CommitAttempts  int           `yaml:"commit_attempts" default:"5" validate:"min=1"`
	CommitDeadline  time.Duration `yaml:"commit_deadline" default:"1h" validate:"min=1s"`
	BlobWaitTimeout time.Duration `yaml:"blob_wait_timeout" default:"120s" validate:"min=1s"`
}

// maxUploadParts is the most parts an S3 multipart upload has.
const maxUploadParts = 10_000

// minPartThroughput is the slowest rate (32 MiB/s) at which a part of storage.uploads.part_size is assumed to be sent: a part upload is cut at
// three quarters of the writer lease, and a configuration whose part cannot be sent at this rate within that time would time out every part.
const minPartThroughput = 32 << 20

// StorageUploads configures multi-request upload sessions (R-STOR-011, P1).
type StorageUploads struct {
	PartSize        ByteSize      `yaml:"part_size" default:"16MiB" validate:"min=5MiB,max=5GiB"`
	WriterLease     time.Duration `yaml:"writer_lease" default:"60s" validate:"min=1s" doc:"Also the deadline of one part upload (below it): choose it above the time one part takes to send (R-STOR-011)."`
	SessionTTL      time.Duration `yaml:"session_ttl" default:"4h" validate:"min=1m"`
	SessionMaxAge   time.Duration `yaml:"session_max_age" default:"20h" validate:"min=1m"`
	MaxOpenSessions int           `yaml:"max_open_sessions" default:"100" validate:"min=1" doc:"Most open upload sessions one principal holds on one repository; a start above it answers 429 (R-STOR-011, Q-STOR-176)."`
}

// StorageDeployByChecksum configures checksum-based deploys.
type StorageDeployByChecksum struct {
	Enabled       bool `yaml:"enabled" default:"true"`
	MaxCandidates int  `yaml:"max_candidates" default:"100" validate:"min=1"`
}

// StorageSidecars configures pending sidecar declarations.
type StorageSidecars struct {
	PendingTTL time.Duration `yaml:"pending_ttl" default:"1h" validate:"min=1m"`
}

// StorageDownload tunes downloads (R-STOR-012).
type StorageDownload struct {
	BufferSize ByteSize `yaml:"buffer_size" default:"256KiB" validate:"min=4KiB"`
	Verify     bool     `yaml:"verify" default:"true"`
	HoldBack   ByteSize `yaml:"hold_back" default:"64KiB" validate:"min=1KiB" doc:"Tail withheld until the digest matched; 0 would release corrupted bytes before the check (R-STOR-013)."`
}

// StorageStats batches download statistics (R-STOR-015).
type StorageStats struct {
	FlushInterval time.Duration `yaml:"flush_interval" default:"10s" validate:"min=1s"`
}

// StorageDelete bounds synchronous deletes.
type StorageDelete struct {
	MaxSyncItems int `yaml:"max_sync_items" default:"10000" validate:"min=1"`
	BatchSize    int `yaml:"batch_size" default:"5000" validate:"min=1"`
}

// StorageProperties bounds item properties.
type StorageProperties struct {
	MaxValuesPerItem  int `yaml:"max_values_per_item" default:"1000" validate:"min=1"`
	MaxRecursiveItems int `yaml:"max_recursive_items" default:"100000" validate:"min=1"`
}

// StorageUsage paces the usage fold (R-STOR-003).
type StorageUsage struct {
	FoldInterval time.Duration `yaml:"fold_interval" default:"5s" validate:"min=1s"`
}

// StorageGC configures blob garbage collection (R-STOR-024). Whether a delete goes as DeleteObjects is the connection's
// batch_delete (Q-STOR-222).
type StorageGC struct {
	GracePeriod       time.Duration `yaml:"grace_period" default:"24h" validate:"min=0s"`
	DeleteSettle      time.Duration `yaml:"delete_settle" default:"30s" validate:"min=1s" doc:"Also bounds, as half of it, how long after its object write an upload may insert the blob row (R-STOR-005)."`
	HandoverDelay     time.Duration `yaml:"handover_delay" default:"10m" validate:"min=0s"`
	BatchSize         int           `yaml:"batch_size" default:"1000" validate:"min=1"`
	DeleteConcurrency int           `yaml:"delete_concurrency" default:"8" validate:"min=1"`
}

// StorageOrphans configures the orphan sweep (R-STOR-026).
type StorageOrphans struct {
	MinAge          time.Duration `yaml:"min_age" default:"24h" validate:"min=1s"`
	MultipartMaxAge time.Duration `yaml:"multipart_max_age" default:"24h" validate:"min=1s"`
}

// StorageScrub configures the scrub's runs (R-STOR-027); its period and its throttle are keys of each connection.
type StorageScrub struct {
	RunDuration time.Duration `yaml:"run_duration" default:"50m" validate:"min=1m" doc:"A run of the hourly task ends after this and the next continues."`
}

// Validate checks the cross-field rules of the section.
func (s *Storage) Validate(r *Report) {
	s.validateConnections(r)
	s.validateObjectStore(r)
	if s.Upload.CommitDeadline >= s.Orphans.MinAge/2 {
		r.Add("upload.commit_deadline", "must be below orphans.min_age / 2 (R-STOR-026)")
	}
	if s.Upload.BlobWaitTimeout <= 2*s.GC.DeleteSettle {
		r.Add("upload.blob_wait_timeout", "must exceed 2 times gc.delete_settle (R-STOR-024)")
	}
	if s.Uploads.SessionMaxAge >= s.Orphans.MultipartMaxAge {
		r.Add("uploads.session_max_age", "must be below orphans.multipart_max_age")
	}
	if s.Uploads.WriterLease >= s.Uploads.SessionTTL {
		r.Add("uploads.writer_lease", "must be below uploads.session_ttl: the collection run would abort a session whose claim is still valid (R-STOR-011)")
	}
	if window := s.Uploads.WriterLease - s.Uploads.WriterLease/4; window > 0 && int64(s.Uploads.PartSize)*1000 > minPartThroughput*window.Milliseconds() {
		r.Add("uploads.writer_lease", "a part of uploads.part_size (%s) must be sent within three quarters of the lease at %s/s or more: raise the lease or lower the part size (R-STOR-011)", s.Uploads.PartSize, ByteSize(minPartThroughput))
	}
	if s.Uploads.PartSize > 0 && s.Upload.MaxSize > s.Uploads.PartSize*maxUploadParts {
		r.Add("upload.max_size", "must not exceed %d parts of uploads.part_size (%s), the most an S3 multipart upload has (R-STOR-011)", maxUploadParts, s.Uploads.PartSize)
	}
}

// validateStorageCoupling checks the rules that bind the garbage collector to the
// job lease (R-STOR-024): a run that stops answering is taken over by another
// only after jobs.lease_duration, and the rows it left must not be taken over
// before every delete it sent has surely landed or been refused, which is
// delete_settle after the hand-over rule's cut-off. handover_delay not above
// lease_duration plus twice delete_settle would let a successor finish rows
// while the previous run may still be deleting. Config.Validate (finalize.go)
// calls it.
func (c *Config) validateStorageCoupling(r *Report) {
	need := c.Jobs.LeaseDuration + 2*c.Storage.GC.DeleteSettle
	if c.Storage.GC.HandoverDelay <= need {
		r.Add("storage.gc.handover_delay", "(%s) must exceed jobs.lease_duration (%s) plus 2 times storage.gc.delete_settle (%s) = %s (R-STOR-024)",
			c.Storage.GC.HandoverDelay, c.Jobs.LeaseDuration, c.Storage.GC.DeleteSettle, need)
	}
}

// ValidateFor requires a connection with a bucket where a replica serves and checks the spool against the largest upload on
// the roles that accept uploads.
func (s *Storage) ValidateFor(cmd Command, r *Report) {
	if !cmd.runsReplica() {
		return
	}
	if len(s.Connections) == 0 {
		r.Add("connections", "needs one element at least: a connection with a bucket (R-STOR-050)")
	}
	for i, c := range s.Connections {
		if c.Bucket == "" && c.BucketFile == "" {
			r.Add(connectionKey(i, c.WithDefaults().ID), "bucket or bucket_file is required")
		}
	}
	// The roles that accept the protocols' uploads spool up to upload.max_size (packages, web, all). An api replica spools only the UI's
	// uploads, whose own limit validateUICoupling holds to its spool (R-HA-029, Q-UI-106); a worker spools reports and remote-cache
	// fills, which R-HA-029 does not size to upload.max_size. `config validate` runs without a role and checks nothing here; `serve`
	// always has one.
	if ServesClients(r.Role()) && r.Role() != RoleAPI {
		if s.Upload.MaxSize > s.Spool.MaxBytes {
			r.Add("upload.max_size", "must not exceed spool.max_bytes (is %s, spool.max_bytes is %s)", s.Upload.MaxSize, s.Spool.MaxBytes)
		}
	}
}

// NormalizeEndpoint returns an object-store endpoint as an absolute URL. A value
// without a scheme is a host, optionally with a port, as the provisioner of a
// bucket writes it: it means https:// (Q-STOR-15). Plain HTTP stays an explicit
// http:// together with allow_insecure_endpoint. The empty string stays empty.
func NormalizeEndpoint(raw string) string {
	if raw == "" || strings.Contains(raw, "://") {
		return raw
	}
	return "https://" + raw
}

// finalizeEndpoints writes every connection back with its defaults and the endpoints that had no scheme with https://, so
// every consumer and the effective-configuration view see an absolute URL and the value of every key.
func (s *Storage) finalizeEndpoints(sources map[string]Source) {
	derived := false
	for i, c := range s.Connections {
		c = c.WithDefaults()
		if n := NormalizeEndpoint(c.Endpoint); n != c.Endpoint {
			c.Endpoint, derived = n, true
		}
		s.Connections[i] = c
	}
	if derived {
		sources["storage.connections"] = Source{Kind: "derived", Detail: "an endpoint without a scheme, https:// assumed"}
	}
}

// validateObjectStore applies the rules of the shared client settings.
func (s *Storage) validateObjectStore(r *Report) {
	o := s.ObjectStore
	rejectUserinfo(r, "object_store.http.proxy_url", o.HTTP.ProxyURL, "storage.object_store.http.proxy_credentials_file")
	if o.Multipart.PartSize > o.Multipart.Threshold {
		r.Add("object_store.multipart.part_size", "must not exceed object_store.multipart.threshold")
	}
}

// checkKeyPrefix applies the prefix rules of R-STOR-030, which the backup target
// shares (R-BAK-011): no leading, trailing or doubled slash, at most 256
// characters, and only characters that are safe in an object key.
func checkKeyPrefix(r *Report, key, p string) {
	switch {
	case strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/"):
		r.Add(key, "must not start or end with /")
	case strings.Contains(p, "//"):
		r.Add(key, "must not contain //")
	case len(p) > 256:
		r.Add(key, "must be at most 256 characters")
	default:
		for _, c := range p {
			if !isKeyChar(c) {
				r.Add(key, "contains %q; allowed are letters, digits and !_.*'()/-", c)
				break
			}
		}
	}
}

// isKeyChar reports a character the key prefix may contain (R-STOR-030).
func isKeyChar(c rune) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!_.*'()/-", c)
}

// checkEndpointURL applies the endpoint rule of R-STOR-030 to an endpoint with its scheme: an absolute https URL, http only
// when allowed.
func checkEndpointURL(r *Report, key, endpoint string, allowInsecure bool) {
	if endpoint == "" {
		return
	}
	u, err := url.Parse(endpoint)
	switch {
	case err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http"):
		r.Add(key, "must be an absolute https URL (is %q)", redactUserinfo(endpoint))
	case u.Scheme == "http" && !allowInsecure:
		r.Add(key, "uses http: set allow_insecure_endpoint to allow it")
	}
}
