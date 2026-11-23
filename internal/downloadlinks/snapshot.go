package downloadlinks

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/cache"
	"github.com/guided-traffic/quader/internal/cluster"
)

// The replica's snapshot of the row (R-STOR-014): the serve point reads it instead of the database, and the change area
// download_link_settings reloads it within the propagation bound of R-ARCH-018 (2 s), through PostgreSQL's change log when Valkey is
// stopped. A failed load keeps the last snapshot, which before the first load is the default of an installation: off.

// Reload is the snapshot's safety period: a lost change is read again after it, and a link check that has not answered is asked
// again.
const Reload = time.Minute

// Snapshot is the component that keeps a [Links]' settings current and runs the link checks: at its start, when the switch is
// turned on (R-STOR-074) and when a Bastion registration's handover is turned on (R-STOR-096, [Snapshot.CheckLinks]).
type Snapshot struct {
	links *Links
	store *Store

	check  atomic.Bool // a link check run is asked for (CheckLinks)
	kick   chan struct{}
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewSnapshot builds the component and registers its cache on r, which a change of the area reloads; a nil registry reloads it
// only at the safety period.
func NewSnapshot(links *Links, store *Store, r *cache.Registry) *Snapshot {
	x := &Snapshot{links: links, store: store, kick: make(chan struct{}, 1)}
	if r != nil {
		cache.Register(r, "download_link_settings", cache.Options[struct{}, struct{}]{
			MaxEntries: 1, SafetyTTL: Reload, Areas: []string{cluster.AreaDownloadLinkSettings},
			OnChange: func(*cache.Cache[struct{}, struct{}], string, int, json.RawMessage) { x.Reload() },
			OnFlush:  func(string) { x.Reload() },
		})
	}
	return x
}

// CheckLinks asks the loop to run the link checks on every connection now, as when the switch of links is turned on: a Bastion
// registration's handover was turned on, which needs the link check's first part (R-STOR-096).
func (x *Snapshot) CheckLinks() {
	x.check.Store(true)
	x.Reload()
}

// Reload asks the loop to read the row now.
func (x *Snapshot) Reload() {
	select {
	case x.kick <- struct{}{}:
	default:
	}
}

// Name implements the composition root's Component.
func (x *Snapshot) Name() string { return "download-links" }

// Start loads the row once and runs the loop; the first link checks run in the loop, so that a slow store never holds the start.
func (x *Snapshot) Start(ctx context.Context) error {
	x.load(ctx)
	lctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	x.cancel = cancel
	x.wg.Add(1)
	go func() {
		defer x.wg.Done()
		x.links.RunLinkChecks(lctx)
		t := time.NewTicker(Reload)
		defer t.Stop()
		for {
			select {
			case <-lctx.Done():
				return
			case <-t.C:
			case <-x.kick:
			}
			asked := x.check.Swap(false)
			if x.load(lctx) || asked || x.links.unanswered() {
				x.links.RunLinkChecks(lctx)
			}
		}
	}()
	return nil
}

// load reads the row and replaces the snapshot; it reports whether the switch was turned on.
func (x *Snapshot) load(ctx context.Context) bool {
	lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s, err := x.store.Settings(lctx)
	if err != nil {
		if ctx.Err() == nil {
			x.links.o.Log.Warn("the download link settings were not read; the last snapshot stays", "error", err)
		}
		return false
	}
	return x.links.SetSettings(s)
}

// Stop ends the loop.
func (x *Snapshot) Stop(context.Context) error {
	if x.cancel != nil {
		x.cancel()
	}
	x.wg.Wait()
	return nil
}
