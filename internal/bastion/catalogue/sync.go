package catalogue

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/cache"
	"github.com/guided-traffic/quader/internal/bastion/cataloguebounds"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// maxDocGzip bounds the compressed bytes of one document read from the store, as the client bounds a frame.
const maxDocGzip = cataloguebounds.DocumentMaxBytes + cataloguebounds.DocumentMaxBytes/1024 + 1024

// sync is one round of R-BAS-109: the public set, the change list to its end, the documents the store does not hold, verified,
// the index, and the state record. What it fetched stays in the store whether or not the round completes, so a round that fails
// half-way costs the next one only what is left.
func (c *Catalogue) sync(ctx context.Context) error {
	c.round.Lock()
	defer c.round.Unlock()
	c.cleared = false
	c.listHeld(ctx)
	if c.o.SharedStore {
		c.take(ctx, takeAhead)
	}
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	pub, err := c.o.Source.CataloguePublic(cctx)
	cancel()
	if err != nil {
		return fmt.Errorf("the public set: %w", err)
	}
	cur := c.cur.Load()
	if cur != nil {
		// What fell out of the public set is hidden before anything else is read (R-BAS-112).
		c.setSnapshot(cur.withPublic(pub))
	}
	next, changed, err := c.changes(ctx, cur)
	if err != nil {
		return err
	}
	if err := c.fetch(ctx, next, changed); err != nil {
		return err
	}
	idx, err := c.indexOf(ctx, next, cur)
	if err != nil {
		return err
	}
	gui, docs := c.effective()
	next.public, next.repos, next.index, next.gui, next.docs = pub, reposOf(pub), idx, gui, docs
	next.docIndex = c.docsOf(ctx, next, cur, idx)
	next.confirmedAt = c.o.Now()
	if !gui {
		return nil // switched off while the round ran
	}
	c.persist(ctx, next, cur, changed)
	c.setSnapshot(next)
	c.o.Metrics.CatalogueSync.WithLabelValues(metrics.SyncConfirmed).Inc()
	return nil
}

// changes reads the change list from the cursor of cur to its end and answers the next snapshot's shards and cursor, with the
// shards that changed. A reset answer starts the next snapshot empty, so the old one is served until the round swaps it.
func (c *Catalogue) changes(ctx context.Context, cur *snapshot) (*snapshot, map[int]bool, error) {
	next := &snapshot{}
	after := int64(0)
	if cur != nil {
		next.shards, after = cur.shards, cur.cursor
	}
	changed := map[int]bool{}
	cloned := map[int]bool{}
	for {
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		ans, err := c.o.Source.CatalogueChanges(cctx, after, 0)
		cancel()
		if err != nil {
			return nil, nil, fmt.Errorf("the change list after %d: %w", after, err)
		}
		if ans.Reset {
			// The catalogue was built again or restored (R-BAS-108): its list is read from 0 into a state of its own.
			c.log.Info("the public catalogue's change list was reset; it is read again while the replica serves what it holds", "cursor", after)
			next.shards, cloned = [shardCount]*shard{}, map[int]bool{}
			for i := range shardCount {
				changed[i] = true
			}
		}
		for _, e := range ans.Entries {
			n := shardOf(e.Path)
			if !cloned[n] {
				sh := &shard{paths: map[string]docRef{}}
				if old := next.shards[n]; old != nil {
					sh.paths = maps.Clone(old.paths)
				}
				next.shards[n], cloned[n] = sh, true
			}
			changed[n] = true
			if e.Deleted || e.Size > cataloguebounds.DocumentMaxBytes {
				delete(next.shards[n].paths, e.Path)
				continue
			}
			name, err := c.o.Store.CatalogueName(e.SHA256)
			if err != nil {
				return nil, nil, err
			}
			next.shards[n].paths[e.Path] = docRef{sha: e.SHA256, size: e.Size, name: name}
		}
		after = ans.Cursor
		if !ans.More {
			break
		}
	}
	next.cursor = after
	for n := range changed {
		if sh := next.shards[n]; sh != nil && len(sh.paths) == 0 {
			next.shards[n] = nil
		} else if sh != nil {
			sh.doc = ""
		}
	}
	return next, changed, nil
}

// fetch reads the documents the next snapshot names and the store does not hold, by one path of each, in batches, and keeps
// each after its verification. A frame that is missing takes its path out of the snapshot (the catalogue no longer holds it;
// its deletion follows in the list); a frame that names newer bytes for its path gives the path those. A document that does not
// verify fails the round: it is fetched again in the next.
func (c *Catalogue) fetch(ctx context.Context, next *snapshot, changed map[int]bool) error {
	for range 1000 {
		want := c.missing(next)
		if len(want) == 0 {
			return nil
		}
		paths := make([]string, 0, min(len(want), cataloguebounds.BatchMaxPaths))
		for p := range want {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		paths = paths[:min(len(paths), cataloguebounds.BatchMaxPaths)]
		got := 0
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		err := c.o.Source.CatalogueDocuments(cctx, paths, func(f iface.CatalogueFrame, gz []byte) error {
			got++
			return c.keep(cctx, next, changed, f, gz)
		})
		cancel()
		if err != nil {
			return fmt.Errorf("the documents: %w", err)
		}
		if got == 0 {
			return errors.New("the batch of documents answered no frame")
		}
	}
	return errors.New("the documents were not all fetched in 1000 batches")
}

// missing are the paths to ask for: one path of every document of next the store does not hold.
func (c *Catalogue) missing(next *snapshot) map[string]docRef {
	c.heldMu.Lock()
	defer c.heldMu.Unlock()
	want := map[string]docRef{}
	seen := map[string]bool{}
	next.each(func(p string, r docRef) {
		if _, ok := c.held[r.name]; ok || seen[r.sha] {
			return
		}
		seen[r.sha] = true
		want[p] = r
	})
	return want
}

// keep verifies and stores the document of one frame.
func (c *Catalogue) keep(ctx context.Context, next *snapshot, changed map[int]bool, f iface.CatalogueFrame, gz []byte) error {
	n := shardOf(f.Path)
	sh := next.shards[n]
	if sh == nil {
		return nil
	}
	if f.Missing {
		c.cloneShard(next, n, changed)
		delete(next.shards[n].paths, f.Path)
		return nil
	}
	if f.Size > cataloguebounds.DocumentMaxBytes {
		return fmt.Errorf("%w: %s is larger than a document may be", errIntegrity, f.Path)
	}
	if _, err := open(gz, f.SHA256, f.Size); err != nil {
		return fmt.Errorf("%s: %w", f.Path, err)
	}
	name, err := c.o.Store.CatalogueName(f.SHA256)
	if err != nil {
		return err
	}
	if err := c.o.Store.PutCatalogueDocument(ctx, f.SHA256, gz); err != nil {
		return fmt.Errorf("keeping %s: %w", f.Path, err)
	}
	c.addHeld(name, int64(len(gz)))
	if r := sh.paths[f.Path]; r.sha != f.SHA256 {
		c.cloneShard(next, n, changed)
		next.shards[n].paths[f.Path] = docRef{sha: f.SHA256, size: f.Size, name: name}
	}
	return nil
}

// cloneShard makes shard n of next its own before a change: a shard of next that a round did not change is the served one's.
func (c *Catalogue) cloneShard(next *snapshot, n int, changed map[int]bool) {
	if changed[n] {
		return
	}
	old := next.shards[n]
	sh := &shard{paths: map[string]docRef{}}
	if old != nil {
		sh.paths = maps.Clone(old.paths)
	}
	next.shards[n], changed[n] = sh, true
}

// persist writes the shards that changed and the state record (R-BAS-110): the record when the cursor, the public set, the
// switches or a shard changed, and otherwise at most every refreshEvery for its confirmedAt. A failure is logged: the replica
// serves what it holds, and only a restart would find an older state.
func (c *Catalogue) persist(ctx context.Context, next, cur *snapshot, changed map[int]bool) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), storeTimeout)
	defer cancel()
	var errs []error
	for n := range changed {
		sh := next.shards[n]
		if sh == nil {
			continue
		}
		raw, err := sh.encode()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		gz, sha, err := compress(raw)
		if err == nil {
			err = c.o.Store.PutCatalogueDocument(ctx, sha, gz)
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if name, err := c.o.Store.CatalogueName(sha); err == nil {
			c.addHeld(name, int64(len(gz)))
		}
		sh.doc = sha
	}
	if len(errs) > 0 {
		c.log.Warn("the public catalogue's state could not be kept; a restart reads an older one", "error", errors.Join(errs...))
		return
	}
	due := cur == nil || len(changed) > 0 || cur.cursor != next.cursor || cur.gui != next.gui || cur.docs != next.docs ||
		!samePublic(cur.public, next.public) || next.confirmedAt.Sub(c.lastWrite) >= refreshEvery
	if !due {
		return
	}
	payload, err := iface.Marshal(next.record(c.o.Replica))
	if err == nil {
		err = c.o.Store.StoreCatalogueState(ctx, payload)
	}
	if err != nil {
		c.log.Warn("the public catalogue's state record could not be written; a restart reads an older one", "error", err)
		return
	}
	c.lastWrite = next.confirmedAt
}

func samePublic(a, b iface.CataloguePublic) bool {
	if len(a.Projects) != len(b.Projects) || len(a.Repositories) != len(b.Repositories) {
		return false
	}
	for i := range a.Projects {
		if a.Projects[i] != b.Projects[i] {
			return false
		}
	}
	for i := range a.Repositories {
		if a.Repositories[i] != b.Repositories[i] {
			return false
		}
	}
	return true
}

// Load reads the stored state at a start (R-BAS-111): the record, its shards and the index. A replica that finds one serves it
// with its confirmedAt and its switches, also while the main installation is unreachable; one that finds none, or one that does
// not verify, holds nothing and answers not ready until its first round. It reports whether it loaded a state.
func (c *Catalogue) Load(ctx context.Context) bool {
	c.round.Lock()
	defer c.round.Unlock()
	c.listHeld(ctx)
	return c.take(ctx, takeStart)
}

// follow is the turn of a replica whose bucket another replica syncs (R-BAS-109): it takes the state that replica wrote when it is
// ahead of its own.
func (c *Catalogue) follow(ctx context.Context) {
	c.round.Lock()
	defer c.round.Unlock()
	if c.take(ctx, takeNewer) {
		c.o.Metrics.CatalogueSync.WithLabelValues(metrics.SyncFollowed).Inc()
	}
}

// takeMode says when [Catalogue.take] serves the stored state: at a start always; before a round of a shared bucket when its cursor
// is ahead of the one served, so that the round continues from the newest state; at a follower's turn also when it was confirmed
// later, so that a public set the syncing replica read reaches the follower at its next turn.
type takeMode int

const (
	takeStart takeMode = iota
	takeAhead
	takeNewer
)

// take reads the stored record and serves it by the mode. It reads only the shards whose documents differ from the ones served. It
// reports whether it swapped.
func (c *Catalogue) take(ctx context.Context, mode takeMode) bool {
	ctx, cancel := context.WithTimeout(ctx, storeTimeout)
	defer cancel()
	payload, err := c.o.Store.LoadCatalogueState(ctx)
	if err != nil {
		if mode == takeStart && !errors.Is(err, cache.ErrNotFound) {
			c.log.Warn("the stored state of the public catalogue is not used; the replica syncs it again", "error", err)
		}
		return false
	}
	rec, err := decodeRecord(payload)
	if err != nil {
		c.log.Warn("the stored state of the public catalogue does not decode; the replica syncs it again", "error", err)
		return false
	}
	cur := c.cur.Load()
	switch {
	case mode == takeStart || cur == nil || rec.Cursor > cur.cursor:
	case mode == takeNewer && rec.Cursor == cur.cursor && rec.ConfirmedAt.After(cur.confirmedAt):
	default:
		return false
	}
	next := &snapshot{
		cursor: rec.Cursor, confirmedAt: rec.ConfirmedAt, gui: rec.GUI, docs: rec.Docs, public: rec.Public, repos: reposOf(rec.Public),
	}
	for n, doc := range rec.Shards {
		if doc == "" {
			continue
		}
		if cur != nil && cur.shards[n] != nil && cur.shards[n].doc == doc {
			next.shards[n] = cur.shards[n]
			continue
		}
		raw, err := c.readShard(ctx, doc)
		if err == nil {
			next.shards[n], err = decodeShard(raw, n, c.o.Store.CatalogueName)
		}
		if err != nil {
			c.log.Warn("a shard of the stored state of the public catalogue cannot be read; the replica syncs it again", "shard", n, "error", err)
			return false
		}
		next.shards[n].doc = doc
	}
	idx, err := c.indexOf(ctx, next, cur)
	if err != nil {
		c.log.Warn("the index of the stored state of the public catalogue cannot be read; the replica syncs it again", "error", err)
		return false
	}
	next.index = idx
	next.docIndex = c.docsOf(ctx, next, cur, idx)
	if gui, _ := c.effective(); c.decided() && !gui {
		return false
	}
	c.setSnapshot(next)
	if mode == takeStart {
		c.lastWrite = rec.ConfirmedAt
		c.log.Info("the stored state of the public catalogue is served", "confirmedAt", rec.ConfirmedAt.Format(time.RFC3339), "documents", next.count())
	}
	return true
}

// readShard reads a stored shard of the manifest by its content address and verifies it; no list names its size, so it is read to
// at most maxShardBytes and held to its SHA-256 alone.
func (c *Catalogue) readShard(ctx context.Context, sha string) ([]byte, error) {
	gz, err := c.o.Store.GetCatalogueDocument(ctx, sha, maxShardBytes)
	if err != nil {
		return nil, err
	}
	return openAny(gz, sha, maxShardBytes)
}

// clear deletes the catalogue's tree once after the switch gui went off (R-BAS-102): the state record and every document.
func (c *Catalogue) clear(ctx context.Context) {
	c.round.Lock()
	defer c.round.Unlock()
	if c.cleared {
		return
	}
	c.setSnapshot(nil)
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	err := c.o.Store.DeleteCatalogueState(ctx)
	if err == nil {
		err = c.o.Store.ListCatalogue(ctx, func(o cache.CatalogueObject) error {
			return c.o.Store.DeleteCatalogueObject(ctx, o.Name)
		})
	}
	if err != nil {
		c.log.Warn("the public catalogue's tree could not be deleted after the web interface was switched off; the next turn tries again", "error", err)
		return
	}
	c.heldMu.Lock()
	c.held, c.heldKnown = map[string]int64{}, true
	c.heldMu.Unlock()
	c.pages = map[string][]*Entry{}
	c.trees = map[string]*docTree{}
	c.cleared = true
	c.o.Metrics.CatalogueBytes.Set(0)
	c.log.Info("the web interface is off: the public catalogue's tree is deleted")
}

// listHeld lists the tree when the replica does not know what it holds: at the first round or load, and after a listing failed.
func (c *Catalogue) listHeld(ctx context.Context) {
	c.heldMu.Lock()
	known := c.heldKnown
	c.heldMu.Unlock()
	if !known {
		c.relist(ctx, nil)
	}
}

// relist lists the tree, sets what the replica knows it holds and the bytes gauge, and calls fn for every object.
func (c *Catalogue) relist(ctx context.Context, fn func(cache.CatalogueObject)) {
	held := map[string]int64{}
	err := c.o.Store.ListCatalogue(ctx, func(o cache.CatalogueObject) error {
		held[o.Name] = o.Size
		if fn != nil {
			fn(o)
		}
		return nil
	})
	if err != nil {
		c.log.Warn("the public catalogue's tree could not be listed", "error", err)
		return
	}
	c.heldMu.Lock()
	c.held, c.heldKnown = held, true
	total := int64(0)
	for _, n := range held {
		total += n
	}
	c.heldMu.Unlock()
	c.o.Metrics.CatalogueBytes.Set(float64(total))
}

func (c *Catalogue) addHeld(name string, n int64) {
	c.heldMu.Lock()
	defer c.heldMu.Unlock()
	if _, ok := c.held[name]; !ok {
		c.held[name] = n
		c.o.Metrics.CatalogueBytes.Add(float64(n))
	}
}

// lost forgets a document the store does not hold or that failed its verification, so that the next round fetches it again, and
// wakes the loop (R-BAS-113).
func (c *Catalogue) lost(name string) {
	c.heldMu.Lock()
	if n, ok := c.held[name]; ok {
		delete(c.held, name)
		c.o.Metrics.CatalogueBytes.Sub(float64(n))
	}
	c.heldMu.Unlock()
	c.Wake()
}

// maybeSweep deletes, every sweepEvery, the documents that the state served does not name and that were written more than
// sweepGrace ago (R-BAS-110), and sets the bytes gauge from the listing.
func (c *Catalogue) maybeSweep(ctx context.Context) {
	c.round.Lock()
	defer c.round.Unlock()
	now := c.o.Now()
	if now.Sub(c.lastSweep) < sweepEvery {
		return
	}
	c.lastSweep = now
	cur := c.cur.Load()
	if cur == nil {
		return
	}
	keep := map[string]bool{}
	cur.each(func(_ string, r docRef) { keep[r.name] = true })
	for _, sh := range cur.shards {
		if sh != nil && sh.doc != "" {
			if name, err := c.o.Store.CatalogueName(sh.doc); err == nil {
				keep[name] = true
			}
		}
	}
	var gone []string
	c.relist(ctx, func(o cache.CatalogueObject) {
		if !keep[o.Name] && now.Sub(o.Modified) >= sweepGrace {
			gone = append(gone, o.Name)
		}
	})
	for _, name := range gone {
		if err := c.o.Store.DeleteCatalogueObject(ctx, name); err != nil {
			c.log.Warn("a document of the public catalogue no state names could not be deleted", "error", err)
			return
		}
		c.heldMu.Lock()
		if n, ok := c.held[name]; ok {
			delete(c.held, name)
			c.o.Metrics.CatalogueBytes.Sub(float64(n))
		}
		c.heldMu.Unlock()
	}
	if len(gone) > 0 {
		c.log.Info("the public catalogue's sweep deleted the documents no state names", "documents", len(gone))
	}
}
