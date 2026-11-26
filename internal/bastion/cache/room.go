package cache

import (
	"context"
	"errors"
	"sync"

	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// ErrNoRoom: the bytes of a fill would take its area past its limit (R-BAS-089). The fill is not kept, the caller passes the
// answer through as for [ErrSpoolFull], and a sweep is started.
var ErrNoRoom = errors.New("cache: the area of the fill has no room for its bytes")

// The areas of the estimate, by index.
const (
	areaArtifacts = iota
	areaDocuments
)

func areaIndex(kind EntryKind) int {
	if kind == EntryDocument {
		return areaDocuments
	}
	return areaArtifacts
}

func areaLabel(i int) string {
	if i == areaDocuments {
		return metrics.AreaDocuments
	}
	return metrics.AreaArtifacts
}

// estimate is the running estimate of the bytes each area holds (R-BAS-089): set from every listing of the areas, raised by
// every fill this replica commits. Between two listings it knows only this replica's commits; with a bucket that several
// replicas share it counts each of them weight (bastion.replicas) times, as if every replica filled as much (Q-BAS-302). It
// never decreases between listings: an eviction or a delete is found by the next listing, so the estimate errs high, never
// low, for what this replica writes. Safe for concurrent use.
type estimate struct {
	mu     sync.Mutex
	weight int64
	// known is false until the first listing; until then the estimate holds only the commits and admits every fill.
	known bool
	used  [2]int64
	// added counts the weighted bytes of the commits since the start and never decreases; listed is what it was when the
	// listing that set used began, so a replica that committed nothing since its last listing need not list again.
	added  [2]int64
	listed [2]int64
}

func newEstimate(weight int) *estimate { return &estimate{weight: int64(max(weight, 1))} }

// mark is taken before a listing starts, for [estimate.rebase].
func (e *estimate) mark() [2]int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.added
}

// rebase sets the estimate from a listing: what it found plus what this replica committed since the listing began. A commit
// the listing saw already is counted twice until the next listing, which errs high.
func (e *estimate) rebase(u Usage, mark [2]int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.used[areaArtifacts] = u.Artifacts + e.added[areaArtifacts] - mark[areaArtifacts]
	e.used[areaDocuments] = u.Documents + e.added[areaDocuments] - mark[areaDocuments]
	e.listed, e.known = mark, true
}

// dirty reports whether this replica committed anything since the listing that set the estimate began.
func (e *estimate) dirty() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.known || e.added != e.listed
}

// isKnown reports whether a listing set the estimate.
func (e *estimate) isKnown() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.known
}

// fits reports whether n more bytes keep the area at or below limit; while no listing succeeded every fill fits.
func (e *estimate) fits(area int, n, limit int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.known || e.used[area]+n*e.weight <= limit
}

// above reports whether n more bytes take the area above mark.
func (e *estimate) above(area int, n, mark int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.used[area]+n*e.weight > mark
}

// add counts a commit of n bytes and returns the area's new estimate.
func (e *estimate) add(area int, n int64) int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.added[area] += n * e.weight
	e.used[area] += n * e.weight
	return e.used[area]
}

// bytes is the area's estimate now.
func (e *estimate) bytes(area int) int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.used[area]
}

// limitOf is the size limit of an area and its high mark (95 %, R-BAS-031).
func (c *Cache) limitOf(area int) (limit, high int64) {
	arts, docs := c.limits()
	limit = arts
	if area == areaDocuments {
		limit = docs
	}
	return limit, percent(limit, sweepHighPercent)
}

// storedSize is what the store holds for plain bytes of an area: the bytes themselves, or with a cache key the object of the
// stored format with its record in front (R-BAS-061), which is what a listing counts.
func (b *backend) storedSize(plain int64) int64 {
	if b.seal == nil || plain < 0 {
		return plain
	}
	n, err := blobcrypt.StoredSize(plain)
	if err != nil {
		return plain
	}
	return frameSize(b.seal.id) + n
}

// expect is told the announced length of a fill when it starts: one that would take its area over the high mark starts a
// sweep now, so that the room is made while the bytes stream rather than after them (R-BAS-089).
func (c *Cache) expect(kind EntryKind, size int64) {
	if size < 0 {
		return
	}
	area := areaIndex(kind)
	if _, high := c.limitOf(area); c.est.above(area, c.back.storedSize(size), high) {
		c.wantSweep()
	}
}

// admit decides whether a verified fill of plain bytes may be committed under sha (R-BAS-089): when its bytes keep the area at
// or below its limit, or when the store holds the content address already, which costs no room. It reports the stored bytes
// to count after the commit (zero for an address held already). A fill without room is counted, starts a sweep and gets
// [ErrNoRoom].
func (c *Cache) admit(ctx context.Context, kind EntryKind, sha string, plain int64) (int64, error) {
	c.firstListing(ctx)
	area := areaIndex(kind)
	stored := c.back.storedSize(plain)
	limit, _ := c.limitOf(area)
	if c.est.fits(area, stored, limit) {
		return stored, nil
	}
	if _, err := c.back.HeadObject(WithArea(ctx, kind), sha); err == nil {
		return 0, nil
	}
	c.m.CacheNoRoom.WithLabelValues(areaLabel(area)).Inc()
	c.log.Debug("a fill is passed through unkept: its area has no room", "area", areaLabel(area), "bytes", stored,
		"estimate", c.est.bytes(area), "limit", limit)
	c.wantSweep()
	return 0, ErrNoRoom
}

// firstListing lists the areas once when no listing has set the estimate yet: the first commit of a replica waits for the
// listing of [Cache.RunSweeper] at start, or makes it, so that a cache that holds bytes from before the start is not taken
// for an empty one (Q-BAS-301). A listing that fails leaves the estimate unknown, and the commit is admitted: a backend that
// cannot list cannot be written to either, and the next commit tries again.
func (c *Cache) firstListing(ctx context.Context) {
	if c.est.isKnown() {
		return
	}
	c.listMu.Lock()
	defer c.listMu.Unlock()
	if c.est.isKnown() {
		return
	}
	_, _ = c.measure(ctx)
}

// committed counts a commit of stored bytes; one that takes its area over the high mark starts a sweep.
func (c *Cache) committed(kind EntryKind, stored int64) {
	if stored <= 0 {
		return
	}
	area := areaIndex(kind)
	if _, high := c.limitOf(area); c.est.add(area, stored) > high {
		c.wantSweep()
	}
}

// wantSweep asks [Cache.RunSweeper] for a sweep. It never blocks: a request while one is pending is the same request, so a
// burst of commits starts one sweep, not one each.
func (c *Cache) wantSweep() {
	select {
	case c.sweepNow <- struct{}{}:
	default:
	}
}
