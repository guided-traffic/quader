// Package catalogue is the public catalogue of the web interface at a Bastion replica (W-35, R-BAS-109 to R-BAS-113): the sync that
// mirrors the registration's catalogue from the main installation through the routes of R-BAS-101, the store of its documents in
// the cache backend, the state that survives a restart where the cache does, the age the pages show, the hooks of the revocation
// feed, and the [Reader] the web interface (W-35's P9) and the anonymous answers of W-34 read. No page view reaches the main
// installation: everything the reader answers is read from the replica's memory and its cache backend.
//
// The sync (R-BAS-109). A round reads the public set, then the change list from the replica's cursor to its end, then the
// documents the list names that the store does not hold, in batches, each one decompressed to at most its size and verified
// against the SHA-256 of its raw bytes before it is kept. A round that completed is the replica's confirmation: its time is
// confirmedAt. A reset of the change list makes the round read the list from 0 into a state of its own while the old state is
// served, and swaps it in when it is complete. With the bucket backend and Valkey a lease lets one replica sync and the others
// take the state it wrote; otherwise each replica syncs, which is safe because the documents are content-addressed and the state
// record is one object, of which a replica takes the stored one when its cursor is ahead of its own.
//
// The store (R-BAS-110). The documents are kept gzip-compressed in the catalogue's tree of the cache backend
// (cache.Cache.PutCatalogueDocument), by the SHA-256 of their raw bytes, so a document many paths hold (an Ansible entry over
// many versions) is kept once. Which path holds which document is a manifest of shardCount shards, each a document of the same
// tree, grouped by the unit a path belongs to (a package, a repository's set-up), so that a change of one package rewrites one
// shard. The state record names the shards, the cursor, confirmedAt, the public set and the switches; it is signed and, with a
// cache key, sealed. A sweep deletes the documents no state names an hour after they were written; the switch gui off deletes the
// whole tree.
//
// The age (R-BAS-111). The reader serves its state whatever its age: confirmedAt and bastion.gui.stale_after are in its [State],
// and the pages say how old they are. A replica that starts with a stored state serves it with the stored confirmedAt and the
// stored switches; one without answers not ready until its first round.
//
// The hooks (R-BAS-112). A feed entry of the kinds grants, settings, principal and content, and a reset of the feed, make the
// replica run a round at once and again after the builder's debounce and run, since the public set and the change list the main
// installation answers are those of its last build; a round reads the public set first and hides what fell out of it before
// anything else. An entry of the kind catalogue makes it run a round at once.
package catalogue

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/cache"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// Source is what the sync reads of the main installation (R-BAS-101); mainclient.HTTPClient implements it, and reads every body
// through the throughput's catalogue class (R-BAS-097).
type Source interface {
	CatalogueChanges(ctx context.Context, after int64, limit int) (iface.CatalogueChanges, error)
	CataloguePublic(ctx context.Context) (iface.CataloguePublic, error)
	CatalogueDocuments(ctx context.Context, paths []string, fn func(f iface.CatalogueFrame, gz []byte) error) error
}

// Store is the catalogue's tree of the cache backend (R-BAS-110); cache.Cache implements it.
type Store interface {
	CatalogueName(sha string) (string, error)
	PutCatalogueDocument(ctx context.Context, sha string, gz []byte) error
	GetCatalogueDocument(ctx context.Context, sha string, limit int64) ([]byte, error)
	DeleteCatalogueObject(ctx context.Context, name string) error
	ListCatalogue(ctx context.Context, fn func(cache.CatalogueObject) error) error
	StoreCatalogueState(ctx context.Context, payload []byte) error
	LoadCatalogueState(ctx context.Context) ([]byte, error)
	DeleteCatalogueState(ctx context.Context) error
}

// Lease picks the one replica of a shared bucket that syncs (shared.State.CatalogueLease); its shape is the sweep lease's.
type Lease interface {
	TryAcquire(ctx context.Context, ttl time.Duration) (held bool, release func(), err error)
}

// Options configure a [Catalogue].
type Options struct {
	Source Source
	Store  Store
	// Lease returns the lease of the sync while the replicas share a bucket and Valkey, nil otherwise: then this replica syncs.
	Lease func() Lease
	// SharedStore is true for the bucket backend: other replicas write the same state record, and a replica takes the stored one
	// when it is ahead of its own.
	SharedStore bool
	// Kept is whether the store survives a restart: the bucket backend or a persistent volume (R-BAS-111).
	Kept bool
	// Replica is the pod's name, written into the state record for the log.
	Replica string
	// PollInterval is bastion.gui.poll_interval, StaleAfter bastion.gui.stale_after, MaxBytes bastion.gui.max_bytes.
	PollInterval time.Duration
	StaleAfter   time.Duration
	MaxBytes     int64
	Metrics      *metrics.Metrics
	Log          *slog.Logger
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Errors of the reader.
var (
	// ErrOff: the web interface is off (the switch gui, R-BAS-102). The pages answer 404 not_found.
	ErrOff = errors.New("catalogue: the web interface is off")
	// ErrNotReady: the web interface is on and the replica holds no state of the catalogue yet: its first round has not
	// completed and it found none stored (R-BAS-111). The pages answer 503 not_ready.
	ErrNotReady = errors.New("catalogue: no state of the public catalogue yet")
	// ErrQuery: a search or a page the reader does not take: an unknown sort, a cursor it did not write.
	ErrQuery = errors.New("catalogue: the query is not one the catalogue answers")
)

const (
	// shardCount is the number of shards of the manifest: a change of one package rewrites one of them.
	shardCount = 256
	// callTimeout bounds one read of the main installation: a batch of 8 MiB paced at a slow throughput (R-BAS-097) still ends.
	callTimeout = 2 * time.Minute
	// storeTimeout bounds the reads and writes of the state at a start and a round.
	storeTimeout = 30 * time.Second
	// refreshEvery is how often a replica writes its state record when nothing but confirmedAt changed (Q-BAS-374).
	refreshEvery = time.Minute
	// sweepEvery is how often the syncing replica deletes the documents no state names, and sweepGrace how old such a document
	// must be: a replica of a shared bucket that wrote it may not have named it yet (Q-BAS-375).
	sweepEvery = 5 * time.Minute
	sweepGrace = time.Hour
	// followUp is when an entry of the feed makes the replica run its second round: after the builder's debounce of 5 s and its
	// run (R-BAS-105, Q-BAS-377).
	followUp = 8 * time.Second
	// maxShardBytes bounds the compressed bytes of a shard of the manifest.
	maxShardBytes = 256 << 20
)

// Catalogue is a replica's public catalogue: the sync, the store and the reader. It is safe for concurrent use.
type Catalogue struct {
	o   Options
	log *slog.Logger

	// cur is the state served: nil while the replica holds none.
	cur atomic.Pointer[snapshot]

	// round serializes the rounds, the load, the follower's take and the deletion of the tree.
	round sync.Mutex
	// held is what the store holds, by base name, with the bytes as stored; known is false until the first listing.
	heldMu    sync.Mutex
	held      map[string]int64
	heldKnown bool
	// pages are the parsed pages of the index by the SHA-256 of their document.
	pages map[string][]*Entry
	// lastWrite is when the state record was written last, lastSweep when the tree was swept last, cleared whether the tree was
	// deleted since the switch went off.
	lastWrite time.Time
	lastSweep time.Time
	cleared   bool

	swMu sync.Mutex
	sw   switches

	wake chan struct{}
}

// switches are the switches the replica applies: gui and docs of its configuration (R-BAS-102). decided is false while the
// replica applies the built-in configuration, before any handshake and without a stored one: the stored state's switches hold
// then, and nothing is deleted (R-BAS-111).
type switches struct {
	gui, docs, decided bool
}

// New returns the catalogue in the state of a replica that holds nothing yet; [Catalogue.Load] reads what is stored.
func New(o Options) *Catalogue {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Metrics == nil {
		o.Metrics, _ = metrics.New(nil)
	}
	if o.PollInterval <= 0 {
		o.PollInterval = 10 * time.Second
	}
	c := &Catalogue{
		o: o, log: o.Log.With("component", "catalogue"), held: map[string]int64{}, pages: map[string][]*Entry{},
		wake: make(chan struct{}, 1),
	}
	o.Metrics.CatalogueStaleAfter.Set(o.StaleAfter.Seconds())
	o.Metrics.CatalogueMaxBytes.Set(float64(o.MaxBytes))
	o.Metrics.CatalogueConfirmed.Set(0)
	o.Metrics.GUIEnabled.Set(0)
	return c
}

// SetSwitches applies the switches of the configuration the replica applies (R-BAS-093, R-BAS-102); decided is false for the
// built-in configuration. The switch gui off, decided, makes the state unreadable at once; the loop deletes the tree.
func (c *Catalogue) SetSwitches(sw iface.Switches, decided bool) {
	c.swMu.Lock()
	prev := c.sw
	c.sw = switches{gui: sw.GUI, docs: sw.Docs, decided: decided}
	c.swMu.Unlock()
	gui, _ := c.effective()
	if decided && !sw.GUI {
		c.setSnapshot(nil)
	}
	c.export(gui)
	if prev != c.sw {
		c.Wake()
	}
}

// effective is the switches in force: the configuration's once decided, else the stored state's.
func (c *Catalogue) effective() (gui, docs bool) {
	c.swMu.Lock()
	sw := c.sw
	c.swMu.Unlock()
	if sw.decided {
		return sw.gui, sw.docs
	}
	if s := c.cur.Load(); s != nil {
		return s.gui, s.docs
	}
	return false, false
}

func (c *Catalogue) decided() bool {
	c.swMu.Lock()
	defer c.swMu.Unlock()
	return c.sw.decided
}

func (c *Catalogue) export(gui bool) {
	v := 0.0
	if gui {
		v = 1
	}
	c.o.Metrics.GUIEnabled.Set(v)
}

// Wake makes the loop run a round now.
func (c *Catalogue) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Feed is the hook of the revocation feed (R-BAS-112): the link calls it with the entries of a page it applied, and with one entry
// of the kind grants after a reset of the feed.
func (c *Catalogue) Feed(entries []iface.FeedEntry) {
	now, later := false, false
	for _, e := range entries {
		switch e.Kind {
		case iface.FeedCatalogue:
			now = true
		case iface.FeedGrants, iface.FeedSettings, iface.FeedPrincipal, iface.FeedContent:
			// At once for what the main installation answers already, and again once its builder has run after its debounce:
			// the public set and the change list are those of the last build (R-BAS-101, R-BAS-105).
			now, later = true, true
		}
	}
	if now {
		c.Wake()
	}
	if later {
		time.AfterFunc(followUp, c.Wake)
	}
}

// Run runs a round at once and then every poll interval, and whenever it is woken, until ctx ends.
func (c *Catalogue) Run(ctx context.Context) {
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-c.wake:
		}
		c.Tick(ctx)
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
		t.Reset(c.o.PollInterval)
	}
}

// Tick is one turn of the loop: with the web interface off and decided, the tree is deleted once; on, a round of the sync, or
// with a lease another replica holds, the take of the state it wrote. Tests call it.
func (c *Catalogue) Tick(ctx context.Context) {
	gui, _ := c.effective()
	c.export(gui)
	if !gui {
		if c.decided() {
			c.clear(ctx)
		}
		return
	}
	var lease Lease
	if c.o.Lease != nil {
		lease = c.o.Lease()
	}
	if lease != nil {
		held, release, _ := lease.TryAcquire(ctx, 3*c.o.PollInterval)
		if !held {
			c.follow(ctx)
			return
		}
		defer release()
	}
	if err := c.sync(ctx); err != nil && ctx.Err() == nil {
		c.o.Metrics.CatalogueSync.WithLabelValues(metrics.SyncFailed).Inc()
		c.log.Warn("a round of the public catalogue's sync failed; the replica serves what it holds", "error", err)
	}
	c.maybeSweep(ctx)
}

// setSnapshot swaps the state served and exports what the metrics read of it.
func (c *Catalogue) setSnapshot(s *snapshot) {
	c.cur.Store(s)
	if s == nil {
		c.o.Metrics.CatalogueConfirmed.Set(0)
		c.o.Metrics.CatalogueDocuments.Set(0)
		return
	}
	c.o.Metrics.CatalogueConfirmed.Set(float64(s.confirmedAt.UnixMilli()) / 1000)
	c.o.Metrics.CatalogueDocuments.Set(float64(s.count()))
}
