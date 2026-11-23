package create

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/backup/target"
)

// runState is one run of the pipeline: what the phases share.
type runState struct {
	p      *Pipeline
	d      Deps
	h      *backup.Handle
	opts   backup.CreateOptions
	id     format.ID
	runID  string
	layout format.Layout
	facts  backup.SystemFacts

	started time.Time
	res     *backup.Result
	// staying and configured are the ids of the configuration's connections with drain: false and of all of them (R-BAK-032).
	staying, configured map[string]bool

	// Set by the preflight.
	prev          *previousBackup
	checkExisting bool
	sse           string

	// Set by the snapshot phase.
	snap     backup.Snapshot
	snapInfo backup.SnapshotInfo
	snapMu   sync.Mutex
	list     listResult

	// Set by the copy phase.
	tableFiles []format.TableFile
	sequences  []format.Sequence
	copied     copyStats

	writersMu sync.Mutex
	writers   []target.ObjectWriter

	pruneWarned bool
	// warned is set when the run added a warning of its own to the result: the final list names keys of connections that are gone.
	warned bool
}

func (r *runState) fault(ctx context.Context, point string) error {
	if r.d.Fault == nil {
		return nil
	}
	if err := r.d.Fault(ctx, point); err != nil {
		return fmt.Errorf("fault at %s: %w", point, err)
	}
	return nil
}

// execute runs the phases. The first error ends the run; finish records it.
func (r *runState) execute(ctx context.Context) error {
	if err := r.preflight(ctx); err != nil {
		return err
	}
	if err := r.takeSnapshot(ctx); err != nil {
		return err
	}
	if err := r.copying(ctx); err != nil {
		return err
	}
	if err := r.finalize(ctx); err != nil {
		return err
	}
	return r.prune(ctx)
}

// whileTicking calls f and reports progress while it runs, for at most bound: a wait for a lock inside its configured bound is
// progress (R-BAK-013), and the bound makes sure a hang beyond it is still a stall.
func (r *runState) whileTicking(bound time.Duration, f func() error) error {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(max(r.d.Cfg.HeartbeatInterval/2, 10*time.Millisecond))
		defer t.Stop()
		end := time.Now().Add(bound)
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if time.Now().Before(end) {
					r.h.Tick()
				}
			}
		}
	}()
	err := f()
	close(stop)
	<-done
	return err
}

// takeSnapshot is the phase 'snapshot' of R-BAK-009 with the protocol of R-BAK-007 steps 1 to 4.
func (r *runState) takeSnapshot(ctx context.Context) error {
	c := r.d.Cfg
	onLocked := func(ctx context.Context) error {
		// Step 1: the shared migration lock is held; the hold 'all' commits before the snapshot is taken.
		if err := r.h.SetHold(ctx, backup.HoldNone, backup.HoldAll); err != nil {
			return err
		}
		if err := r.fault(ctx, PointHoldAll); err != nil {
			return err
		}
		return r.h.Transition(ctx, backup.StateSnapshot, "")
	}
	var snap backup.Snapshot
	bound := c.Database.MigrationWait + 2*c.Database.LockTimeout + 30*time.Second
	err := r.whileTicking(bound, func() (err error) {
		snap, err = r.d.Snapshot.Open(ctx, backup.SnapshotOptions{
			Registry: r.d.Registry, ExcludeRemoteCaches: r.opts.ExcludeRemoteCaches || c.Exclude.RemoteCaches,
			Parallel: c.Database.Parallel, LockTimeout: c.Database.LockTimeout, MigrationWait: c.Database.MigrationWait,
		}, onLocked)
		return err
	})
	if err != nil {
		return err
	}
	r.snap, r.snapInfo = snap, snap.Info()
	r.h.SetSnapshotAt(r.snapInfo.At)
	opened := r.d.Now()
	r.h.Update(func(p *backup.Progress) { p.SnapshotOpenSince = &opened; p.TablesTotal = len(r.snapInfo.Tables) })
	if err := r.fault(ctx, PointSnapshot); err != nil {
		return err
	}
	return r.diff(ctx)
}

// copying is the phase 'copying': the table export on the open snapshot and the blob copy, in parallel.
func (r *runState) copying(ctx context.Context) error {
	if err := r.h.Transition(ctx, backup.StateCopying, ""); err != nil {
		return err
	}
	g, gctx := newGroup(ctx)
	g.Go(func(ctx context.Context) error { return r.exportAll(ctx) })
	g.Go(func(ctx context.Context) error { return r.copyBlobs(ctx) })
	if err := g.Wait(); err != nil {
		if c := context.Cause(gctx); c != nil && !errors.Is(c, context.Canceled) {
			return c
		}
		return err
	}
	// the final blob list from the provisional one and the pins, then the hold released (R-BAK-032)
	return r.finalList(ctx)
}

// cleanup releases what an ended run may still hold: open uploads, the snapshot and its connections.
func (r *runState) cleanup(ctx context.Context) {
	r.writersMu.Lock()
	ws := r.writers
	r.writers = nil
	r.writersMu.Unlock()
	var wg sync.WaitGroup
	for _, w := range ws {
		wg.Add(1)
		go func() { defer wg.Done(); r.abort(ctx, w) }()
	}
	wg.Wait()
	r.snapMu.Lock()
	snap := r.snap
	r.snap = nil
	r.snapMu.Unlock()
	if snap != nil {
		if err := snap.Close(ctx); err != nil {
			r.d.Log.Warn("closing the backup snapshot failed", "err", err)
		}
	}
}

// abort discards an open upload under a deadline of its own (Deps.AbortTimeout), on a context the signal did not cancel: a run
// that ends on SIGTERM has the grace period of its pod, and a target that hangs must not use it up.
func (r *runState) abort(ctx context.Context, w target.ObjectWriter) {
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.d.AbortTimeout)
	defer cancel()
	if err := w.Abort(actx); err != nil {
		r.d.Log.Warn("could not abort an open upload; the lifecycle rule of the target removes it", "err", err)
	}
}

// newWriter starts a streaming upload and remembers it, so an ended run aborts what it left open.
func (r *runState) newWriter(ctx context.Context, key string) (target.ObjectWriter, error) {
	return r.newWriterHint(ctx, key, 0, nil)
}

// newWriterHint is newWriter for an object of known size, so that the adapter picks a part size that fits it, with the user
// metadata md (an encrypted copy's record, R-BAK-031).
func (r *runState) newWriterHint(ctx context.Context, key string, size int64, md map[string]string) (target.ObjectWriter, error) {
	w, err := r.d.Target.Writer(ctx, key, target.PutOptions{SizeHint: size, Metadata: md})
	if err != nil {
		return nil, err
	}
	r.writersMu.Lock()
	r.writers = append(r.writers, w)
	r.writersMu.Unlock()
	return w, nil
}

// forget drops a writer that was closed or aborted.
func (r *runState) forget(w target.ObjectWriter) {
	r.writersMu.Lock()
	defer r.writersMu.Unlock()
	for i, x := range r.writers {
		if x == w {
			r.writers = append(r.writers[:i], r.writers[i+1:]...)
			return
		}
	}
}

func (r *runState) event(ctx context.Context, ev audit.Event) {
	if r.d.Events == nil {
		return
	}
	if err := r.d.Events.Record(ctx, ev); err != nil {
		r.d.Log.Error("could not write the audit event", "event", ev.Type, "err", err)
	}
}
