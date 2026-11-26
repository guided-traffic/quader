package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/outbound"
)

// Options are what [New], [NewVolume] and [NewBucket] need besides the configuration. Every field is optional.
type Options struct {
	// Metrics receives the errors, corrupt objects, evictions and gauges; nil creates collectors that are
	// registered nowhere.
	Metrics *metrics.Metrics
	Logger  *slog.Logger
	// Factory is the outbound HTTP client factory the bucket's client is built with (R-ARCH-022); nil creates one
	// with the system roots.
	Factory *outbound.Factory
	// TempDir is where the bucket backend spools fills: the pod's temporary volume. Empty is os.TempDir().
	TempDir string
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// HoldBack is the withheld tail of a verified read; zero is [DefaultHoldBack].
	HoldBack int
	// Fault lets a test make the storage fail; nil in production.
	Fault *Fault
	// WrapTransport lets a test count the requests the bucket's client sends; nil leaves the transport alone.
	WrapTransport func(http.RoundTripper) http.RoundTripper
	// Replicas is bastion.replicas: with the bucket backend each replica counts its own commits this many times in its
	// estimate of the areas, because the others write into the same bucket unseen until its next listing (Q-BAS-302). Zero
	// is one.
	Replicas int
}

func (o *Options) fill() {
	if o.Metrics == nil {
		o.Metrics, _ = metrics.New(nil)
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.HoldBack <= 0 {
		o.HoldBack = DefaultHoldBack
	}
}

func newBackend(s store, o Options) *backend {
	if o.Fault != nil {
		s = &faultStore{store: s, f: o.Fault}
	}
	return &backend{
		s: s, now: o.Now, lay: plainLayout{}, spoolDir: s.tmpDir(),
		count: func(op string) { o.Metrics.CacheErrors.WithLabelValues(op).Inc() },
		onRefused: func(key string, err error) {
			o.Metrics.CacheCorrupt.Inc()
			o.Logger.Error("a file of the audit spool failed the stored format and was deleted", "object", key, "error", err)
		},
	}
}

// NewVolume returns the backend of a local directory (R-BAS-030).
func NewVolume(dir string, o Options) (Backend, error) {
	o.fill()
	s, err := newVolumeStore(dir, o.Now)
	if err != nil {
		return nil, err
	}
	return newBackend(s, o), nil
}

// NewBucket returns the backend of the Bastion's own bucket (R-BAS-030).
func NewBucket(ctx context.Context, cfg config.BastionCacheBucket, o Options) (Backend, error) {
	o.fill()
	b, err := openBucket(ctx, cfg, o)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func openBucket(ctx context.Context, cfg config.BastionCacheBucket, o Options) (*backend, error) {
	s3cfg := cfg.StorageS3()
	f := o.Factory
	if f == nil {
		f = outbound.New(config.Outbound{TLS: config.OutboundTLS{UseSystemRoots: true}})
	}
	c, err := s3.NewFromFactory(ctx, s3cfg, f)
	if err != nil {
		return nil, fmt.Errorf("cache bucket: %w", err)
	}
	if o.WrapTransport != nil {
		hc := c.HTTPClient()
		hc.Transport = o.WrapTransport(hc.Transport)
	}
	s, err := newBucketStore(c, cfg.Prefix, o.TempDir, o.Now())
	if err != nil {
		return nil, err
	}
	return newBackend(s, o), nil
}

// Cache is what the engine and the download buffer use (work packages 4 and 7): a [Backend] with the signing of
// entries, the verified read, the fill, the throttled access marker and the sweep around it. It adds nothing a
// backend does not allow; it is the place where R-BAS-022, R-BAS-031 and R-BAS-032 are applied once.
type Cache struct {
	back   *backend
	signer *Signer
	cfg    config.BastionCache
	m      *metrics.Metrics
	log    *slog.Logger
	now    func() time.Time
	hold   int
	// spool bounds the bytes of the fills in progress (bastion.cache.spool_max_bytes, Q-BAS-131).
	spool *spoolBudget
	// est is the running estimate of the areas' bytes that admits a commit (R-BAS-089); sweepNow asks the sweeper for a
	// sweep of the size, at most one pending.
	est      *estimate
	sweepNow chan struct{}
	// listMu orders the first listing of the start and of the first commit, so that only one of them lists. gap is [sweepGap].
	listMu sync.Mutex
	gap    time.Duration

	// started is when the cache was built; the sweep of other layouts runs in the sweeps of the first hour after it
	// and as long as a sweep still finds something (R-BAS-061, Q-BAS-215).
	started      time.Time
	foreignFound atomic.Bool

	ready atomic.Bool

	touchMu sync.Mutex
	touched map[string]time.Time
}

// New opens the backend of cfg.Backend and returns the cache. The signer holds the signing key of
// bastion.signing_key_file (R-BAS-032); it may be nil only where the configuration allows no key (the volume
// backend without Valkey), and entries are then stored without a signature (Q-BAS-98). With
// bastion.cache.encryption_key_file New reads the cache key, and every object the cache writes is encrypted or
// sealed below the tree of its identifier (R-BAS-061); a key file that cannot be read stops the start. New does not
// touch the backend's storage beyond preparing directories; [Cache.Ready] is the first check.
func New(ctx context.Context, cfg config.BastionCache, signer *Signer, o Options) (*Cache, error) {
	o.fill()
	seal, err := openSealer(cfg.EncryptionKeyFile)
	if err != nil {
		return nil, fmt.Errorf("encryption_key_file: %w", err)
	}
	var b *backend
	switch cfg.Backend {
	case "volume":
		s, err := newVolumeStore(cfg.Volume.Dir, o.Now)
		if err != nil {
			return nil, err
		}
		b = newBackend(s, o)
	case "bucket":
		if b, err = openBucket(ctx, cfg.Bucket, o); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("cache: unknown backend %q", cfg.Backend)
	}
	if seal != nil {
		spool := b.s.tmpDir() // the bucket's spool is the pod's already
		if cfg.Backend == "volume" {
			if spool, err = prepareSpool(o.TempDir, o.Now()); err != nil {
				return nil, err
			}
		}
		b.sealWith(seal, spool)
		o.Logger.Info("the cache is encrypted in the stored format", "key_id", seal.id)
	}
	return build(b, signer, cfg, o), nil
}

// build assembles the cache around a backend and publishes the capacity gauges.
func build(b *backend, signer *Signer, cfg config.BastionCache, o Options) *Cache {
	weight := 1
	if cfg.Backend == "bucket" {
		weight = o.Replicas
	}
	c := &Cache{
		back: b, signer: signer, cfg: cfg, m: o.Metrics, log: o.Logger, now: o.Now, hold: o.HoldBack, touched: map[string]time.Time{},
		spool: newSpoolBudget(int64(cfg.SpoolMaxBytes)), started: o.Now(), est: newEstimate(weight), sweepNow: make(chan struct{}, 1), gap: sweepGap,
	}
	arts, docs := c.limits()
	c.m.CacheCapacity.WithLabelValues(metrics.AreaArtifacts).Set(float64(arts))
	c.m.CacheCapacity.WithLabelValues(metrics.AreaDocuments).Set(float64(docs))
	return c
}

// Backend returns the backend, for the audit spool (R-BAS-039) and for tests.
func (c *Cache) Backend() Backend { return c.back }

// limits are the size limits of the areas (R-BAS-031): documents get their share, artifacts the rest, so that
// documents never evict artifacts and the two together stay within max_bytes (Q-BAS-88).
func (c *Cache) limits() (artifacts, documents int64) {
	documents = int64(c.cfg.DocumentsMaxBytes)
	return max(int64(c.cfg.MaxBytes)-documents, 0), documents
}

// Ready is the check that gates the first readiness (R-BAS-043): it checks the backend until one check
// succeeds and afterwards answers nil without a request. A backend that fails later is a cache that misses
// (R-BAS-030), not an unready replica.
func (c *Cache) Ready(ctx context.Context) error {
	if c.ready.Load() {
		return nil
	}
	if err := c.back.Check(ctx); err != nil {
		return err
	}
	c.ready.Store(true)
	return nil
}

func validEntry(e Entry) bool {
	return (e.Kind == EntryArtifact || e.Kind == EntryDocument) && validHash(e.SHA256) && e.Size >= 0
}

// StoreEntry signs the entry under its cache key and stores it. The bytes it points at are committed before
// (a [Fill]), so a reader never finds an entry without its bytes.
func (c *Cache) StoreEntry(ctx context.Context, cacheKey string, e Entry) error {
	if !validEntry(e) {
		return fmt.Errorf("%w: entry of kind %q with content address %q", ErrInvalid, e.Kind, shorten(e.SHA256))
	}
	rec, err := c.signer.SignEntry(cacheKey, e)
	if err != nil {
		return err
	}
	return c.back.PutEntry(ctx, KeyHash(cacheKey), rec)
}

// StoreConfiguration keeps the configuration a replica applied, configuration/current.json (R-BAS-093, Q-BAS-327): signed with
// the signing key where entries are signed, and with a cache key an object of the stored format bound to its name. The bucket
// backend's replicas share the one object; the last replica that applied a configuration wrote it.
func (c *Cache) StoreConfiguration(ctx context.Context, payload []byte) error {
	rec := payload
	if c.signer != nil {
		rec = c.signer.Sign(DomainConfiguration, fileConfiguration, payload)
	}
	return c.back.putConfiguration(ctx, rec)
}

// LoadConfiguration reads the stored configuration and returns its payload. A missing object is [ErrNotFound]; one whose
// signature fails, or with a cache key one the stored format refuses, is [ErrBadSignature]: a replica then applies the built-in
// configuration (Q-BAS-327).
func (c *Cache) LoadConfiguration(ctx context.Context) ([]byte, error) {
	rec, err := c.back.getConfiguration(ctx)
	if err != nil {
		return nil, err
	}
	if c.signer == nil {
		return rec, nil
	}
	return c.signer.Verify(DomainConfiguration, fileConfiguration, rec)
}

// DeleteEntry removes the entry of a cache key.
func (c *Cache) DeleteEntry(ctx context.Context, cacheKey string) error {
	return c.back.DeleteEntry(ctx, KeyHash(cacheKey))
}

// LookupEntry finds the entry of a cache key and checks it (R-BAS-031, R-BAS-032). [ErrNotFound] is a miss and
// covers four cases: no entry; an entry whose signature fails, or with a cache key one the stored format refuses,
// which is deleted and counted as stale; an entry whose bytes are gone, which is deleted and counted as missing;
// an entry of another kind than the key's bytes. Bytes whose size is not the entry's, or with a cache key not a
// size of the format, are corrupt: deleted, counted, a miss. Any other error is the backend's, already counted in
// quader_bastion_cache_errors_total, and the caller serves the request as a miss.
func (c *Cache) LookupEntry(ctx context.Context, cacheKey string) (Entry, ObjectInfo, error) {
	kh := KeyHash(cacheKey)
	rec, err := c.back.GetEntry(ctx, kh)
	if errors.Is(err, ErrBadSignature) {
		c.drop(ctx, kh, metrics.EvictStale, "an entry failed the stored format")
		return Entry{}, ObjectInfo{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, ObjectInfo{}, err
	}
	e, err := c.signer.VerifyEntry(cacheKey, rec)
	if err == nil && !validEntry(e) {
		err = ErrBadSignature
	}
	if err != nil {
		c.drop(ctx, kh, metrics.EvictStale, "an entry failed its signature")
		return Entry{}, ObjectInfo{}, ErrNotFound
	}
	info, err := c.back.HeadObject(WithArea(ctx, e.Kind), e.SHA256)
	if errors.Is(err, ErrNotFound) {
		c.drop(ctx, kh, metrics.EvictMissing, "an entry's bytes are gone")
		return Entry{}, ObjectInfo{}, ErrNotFound
	}
	if errors.Is(err, ErrCorrupt) {
		c.corrupt(ctx, e, kh, err)
		return Entry{}, ObjectInfo{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, ObjectInfo{}, err
	}
	if info.Size != e.Size {
		c.corrupt(ctx, e, kh, fmt.Errorf("%w: the object has %d bytes, the entry says %d", ErrCorrupt, info.Size, e.Size))
		return Entry{}, ObjectInfo{}, ErrNotFound
	}
	return e, info, nil
}

func (c *Cache) drop(ctx context.Context, keyHash, reason, why string) {
	if err := c.back.DeleteEntry(ctx, keyHash); err != nil {
		c.log.Warn("could not delete a cache entry", "reason", why, "error", err)
		return
	}
	c.m.CacheEvictions.WithLabelValues(reason).Inc()
}

// corrupt handles bytes that failed verification: it counts, deletes the object and the entry of the key that
// found it, and logs. Other entries that point at the object are removed when they are read (their bytes are
// gone).
func (c *Cache) corrupt(ctx context.Context, e Entry, keyHash string, cause error) {
	c.m.CacheCorrupt.Inc()
	c.log.Error("a cached object failed verification", "sha256", e.SHA256, "kind", string(e.Kind), "error", cause)
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := c.back.DeleteObject(WithArea(dctx, e.Kind), e.SHA256); err != nil {
		c.log.Error("could not delete a corrupt cached object", "sha256", e.SHA256, "error", err)
	}
	if keyHash != "" {
		_ = c.back.DeleteEntry(dctx, keyHash)
	}
}

// Open opens the bytes of an entry found under cacheKey for a full read, verified while they are served with
// the hold-back (R-BAS-032): the last 64 KiB are released only when the SHA-256 and size equal the entry's. A
// mismatch makes Read return [ErrCorrupt] instead of the held bytes, so the caller aborts the transfer; the
// object and the entry of cacheKey are deleted and quader_bastion_cache_corrupt_total is counted, and the next
// lookup is a miss. [ErrNotFound] says the bytes are gone; the entry is deleted as in [Cache.LookupEntry]. With a
// cache key every segment is authenticated before its bytes reach the verifying reader, and a record or a segment
// that the stored format refuses is [ErrCorrupt] as well: before the first byte from Open, later from Read.
func (c *Cache) Open(ctx context.Context, cacheKey string, e Entry) (io.ReadCloser, error) {
	rc, err := c.back.GetObject(WithArea(ctx, e.Kind), e.SHA256, 0, -1)
	if errors.Is(err, ErrNotFound) {
		c.drop(ctx, KeyHash(cacheKey), metrics.EvictMissing, "an entry's bytes are gone")
	}
	if errors.Is(err, ErrCorrupt) {
		c.corrupt(ctx, e, KeyHash(cacheKey), err)
	}
	if err != nil {
		return nil, err
	}
	return newVerifyReader(rc, e.SHA256, e.Size, c.hold, func(cause error) {
		c.corrupt(ctx, e, KeyHash(cacheKey), cause)
	}), nil
}

// OpenRange opens length bytes of an entry's object from offset (length -1 is to the end). A range cannot be
// verified against the digest, as in R-STOR-013; the digest of a whole read is checked by the next [Cache.Open]
// (H-451). With a cache key every segment of the window is authenticated before its bytes leave, and a refusal is
// [ErrCorrupt] and deletes the object (R-BAS-061).
func (c *Cache) OpenRange(ctx context.Context, e Entry, offset, length int64) (io.ReadCloser, error) {
	rc, err := c.back.GetObject(WithArea(ctx, e.Kind), e.SHA256, offset, length)
	if errors.Is(err, ErrCorrupt) {
		c.corrupt(ctx, e, "", err)
	}
	if err != nil || c.back.seal == nil {
		return rc, err
	}
	return &judgedRange{rc: rc, onCorrupt: func(cause error) { c.corrupt(ctx, e, "", cause) }}, nil
}

// judgedRange hands the first refusal of a sealed range to onCorrupt.
type judgedRange struct {
	rc        io.ReadCloser
	onCorrupt func(error)
}

func (j *judgedRange) Read(p []byte) (int, error) {
	n, err := j.rc.Read(p)
	if errors.Is(err, ErrCorrupt) && j.onCorrupt != nil {
		j.onCorrupt(err)
		j.onCorrupt = nil
	}
	return n, err
}

func (j *judgedRange) Close() error { return j.rc.Close() }

// Touch writes the access marker of an entry's bytes, at most once per bastion.cache.access_resolution per
// replica and content address (R-BAS-031). It is called for a serve from the cache only, never for a fill, so the marker
// also says that the bytes were served twice and protects them in the sweep (R-BAS-098, Q-BAS-460). It is best effort: a
// failure is counted and not returned.
func (c *Cache) Touch(ctx context.Context, e Entry) {
	now := c.now()
	c.touchMu.Lock()
	if last, ok := c.touched[e.SHA256]; ok && now.Sub(last) < c.cfg.AccessResolution {
		c.touchMu.Unlock()
		return
	}
	if len(c.touched) >= maxTouched {
		for k, t := range c.touched {
			if now.Sub(t) >= c.cfg.AccessResolution {
				delete(c.touched, k)
			}
		}
	}
	if len(c.touched) >= maxTouched {
		clear(c.touched) // a flood of distinct objects: forgetting costs a few extra markers
	}
	c.touched[e.SHA256] = now
	c.touchMu.Unlock()
	if err := c.back.Touch(WithArea(ctx, e.Kind), e.SHA256); err != nil {
		c.touchMu.Lock()
		delete(c.touched, e.SHA256)
		c.touchMu.Unlock()
	}
}

// forgetTouch drops the throttle of a content address this replica just committed: a sweep deletes the marker with the
// bytes, so the next serve of the refilled bytes from the cache writes it again and protects them (Q-BAS-464).
func (c *Cache) forgetTouch(sha string) {
	c.touchMu.Lock()
	delete(c.touched, sha)
	c.touchMu.Unlock()
}

// maxTouched bounds the memory of the throttle: past it, markers older than the resolution are forgotten.
const maxTouched = 100_000
