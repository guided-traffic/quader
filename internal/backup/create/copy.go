package create

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/backup/target"
)

// copyStats is the outcome of the copy phase.
type copyStats struct {
	copied  int64 // pins confirmed in the pool, damaged ones excluded
	bytes   int64
	damaged []format.DamagedBlob
	// sourceFailures counts, per connection, the copies that could not be taken (R-BAK-030).
	sourceFailures map[string]int64
}

// errSourceMissing, errSourceSize and errSourceCorrupt are the ways a copy is damaged (R-BAK-006 "Source damage", R-BAK-030):
// not there, of another size than its stored size, and of the right size with other content.
var (
	errSourceMissing = errors.New("the source object is missing or shorter than its stored size")
	errSourceSize    = errors.New("the source object has another size than its stored size")
	errSourceCorrupt = errors.New("the source object does not hash to its digest")
)

const unpinBatch = 500

// copyWork is one pinned digest with the present copies the run may read it from.
type copyWork struct {
	e      format.BlobEntry
	copies []backup.SourceCopy
}

// copier is the state of the copy phase.
type copier struct {
	r     *runState
	lim   *limiter
	mu    sync.Mutex
	stats copyStats
	done  []format.Digest // confirmed, pin not yet deleted

	serverMu  sync.Mutex
	serverOff map[string]bool // connections whose copies the target cannot copy server-side
}

// copyBlobs is step 5 of the protocol of R-BAK-007 and the copy of R-BAK-006: every pin is copied into the pool and deleted once
// the object is confirmed; with the last pin gone the hold becomes 'none'. The present copies of a page of pins are read once per
// page, when the copy reaches it (R-BAK-030, Q-BAK-70).
func (r *runState) copyBlobs(ctx context.Context) error {
	if r.list.toCopy == 0 {
		return r.fault(ctx, PointCopied)
	}
	c := &copier{r: r, lim: newLimiter(int64(r.d.Cfg.Copy.BytesPerSecond)), serverOff: map[string]bool{}, stats: copyStats{sourceFailures: map[string]int64{}}}
	c.lim.tick, c.lim.slice = r.h.Tick, max(r.d.Cfg.HeartbeatInterval/2, 10*time.Millisecond)
	g, _ := newGroup(ctx)
	work := make(chan copyWork, 2*r.d.Cfg.Copy.Concurrency)
	g.Go(func(ctx context.Context) error {
		defer close(work)
		var after format.Digest
		for {
			page, err := r.d.Pins.Pins(ctx, r.runID, after, 1000)
			if err != nil {
				return classify(fmt.Errorf("read the pins: %w", err))
			}
			if len(page) == 0 {
				return nil
			}
			digests := make([]format.Digest, len(page))
			for i, e := range page {
				digests[i] = e.Digest
			}
			copies, err := r.d.Source.Copies(ctx, digests)
			if err != nil {
				return classify(fmt.Errorf("read the copies of the pins: %w", err))
			}
			for _, e := range page {
				select {
				case work <- copyWork{e: e, copies: copies[e.Digest]}:
				case <-ctx.Done():
					return nil
				}
			}
			after = page[len(page)-1].Digest
		}
	})
	for range max(r.d.Cfg.Copy.Concurrency, 1) {
		g.Go(func(ctx context.Context) error {
			for w := range work {
				if ctx.Err() != nil {
					return context.Cause(ctx)
				}
				if err := c.one(ctx, w); err != nil {
					return err
				}
			}
			return nil
		})
	}
	tctx, stopTicker := context.WithCancel(ctx)
	var tw sync.WaitGroup
	tw.Add(1)
	go func() {
		defer tw.Done()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if err := c.unpin(tctx, false); err != nil && tctx.Err() == nil {
					r.d.Log.Warn("could not delete confirmed pins; the next batch tries again", "err", err)
				}
			case <-tctx.Done():
				return
			}
		}
	}()
	err := g.Wait()
	stopTicker()
	tw.Wait()
	if err != nil {
		return err
	}
	if err := c.unpin(ctx, true); err != nil {
		return err
	}
	c.mu.Lock()
	r.copied = c.stats
	c.mu.Unlock()
	sort.Slice(r.copied.damaged, func(i, j int) bool { return r.copied.damaged[i].SHA256 < r.copied.damaged[j].SHA256 })
	if err := r.h.SetHold(ctx, backup.HoldPinned, backup.HoldNone); err != nil {
		return err
	}
	return r.fault(ctx, PointCopied)
}

// unpin deletes the pins of confirmed digests in batches of at most 1,000; with force it deletes every one that is left.
func (c *copier) unpin(ctx context.Context, force bool) error {
	for {
		c.mu.Lock()
		n := len(c.done)
		if n == 0 || (!force && n < unpinBatch) {
			c.mu.Unlock()
			return nil
		}
		n = min(n, 1000)
		batch := append([]format.Digest(nil), c.done[:n]...)
		c.mu.Unlock()
		if err := c.r.d.Store.DeletePins(ctx, c.r.runID, batch); err != nil {
			return classify(fmt.Errorf("delete the pins: %w", err))
		}
		c.mu.Lock()
		c.done = c.done[n:]
		c.mu.Unlock()
		c.r.h.Tick()
		if !force {
			return nil
		}
	}
}

// confirm records a digest whose pin may go and updates the progress.
func (c *copier) confirm(e format.BlobEntry, damaged *format.DamagedBlob) {
	c.mu.Lock()
	c.done = append(c.done, e.Digest)
	if damaged != nil {
		c.stats.damaged = append(c.stats.damaged, *damaged)
	} else {
		c.stats.copied++
		c.stats.bytes += e.Size
	}
	copied, bytes, dmg := c.stats.copied, c.stats.bytes, int64(len(c.stats.damaged))
	c.mu.Unlock()
	total := c.r.list.toCopy
	c.r.h.Update(func(p *backup.Progress) {
		p.BlobsCopied, p.BytesCopied = copied, bytes
		p.PinsLeft = max(total-copied-dmg, 0)
	})
}

// one copies a single pinned digest into the pool and confirms it. It takes one present copy after the other in the order of
// R-BAK-030 and moves on when a copy is missing, of another size, does not hash to the digest or its connection does not answer;
// only when no copy succeeded is the blob damaged in the source (R-BAK-006), or, when a connection did not answer, the run ends
// temporarily (Q-BAK-71).
func (c *copier) one(ctx context.Context, w copyWork) error {
	r, e := c.r, w.e
	key := r.layout.Pool(e.Digest)
	if r.checkExisting {
		if o, err := r.d.Target.Head(ctx, key); err == nil && o.Size == e.Size {
			c.confirm(e, nil)
			return nil
		} else if err != nil && !errors.Is(err, target.ErrNotFound) {
			return classify(fmt.Errorf("check the pool object %s: %w", e.Digest.Hex(), err))
		}
	}
	status := "missing"
	var unavailable error
	for _, cp := range c.order(w.copies) {
		mode, err := c.fromCopy(ctx, e, cp, key)
		if err == nil {
			return c.confirmPool(ctx, e, key, mode)
		}
		reason, next := failureReason(err)
		if !next {
			return classify(fmt.Errorf("copy %s from connection %s: %w", e.Digest.Hex(), cp.Connection, err))
		}
		c.failed(cp, e, reason, err)
		switch reason {
		case "corrupt", "size":
			status = "corrupt"
		case "unavailable":
			unavailable = err
		}
	}
	if unavailable != nil {
		return classify(fmt.Errorf("no copy of %s could be read, and a connection did not answer: the blob is not known to be damaged, the next run copies it: %w", e.Digest.Hex(), unavailable))
	}
	if len(w.copies) == 0 {
		r.d.Log.Warn("a referenced blob has no present copy on a connection of this configuration", "sha256", e.Digest.Hex())
	}
	return c.damaged(ctx, e, status)
}

// order sorts the copies of a blob into the order the run tries them (R-BAK-030, Q-BAK-62): first the copies the target can copy
// server-side (on its endpoint, server_side not never and not refused for that connection in this run), a verified one before one
// that is not; then the others; within each, in the order of the connections with a draining connection's last, as a read does
// (storage.md R-STOR-055).
func (c *copier) order(copies []backup.SourceCopy) []backup.SourceCopy {
	out := slices.Clone(copies)
	server := func(cp backup.SourceCopy) bool { return c.serverSide(cp) }
	slices.SortStableFunc(out, func(a, b backup.SourceCopy) int {
		sa, sb := server(a), server(b)
		switch {
		case sa != sb:
			return boolFirst(sa)
		case sa && a.Verified != b.Verified:
			return boolFirst(a.Verified)
		case a.Drain != b.Drain:
			return boolFirst(b.Drain)
		}
		return a.Position - b.Position
	})
	return out
}

// boolFirst is the comparison that sorts the side for which first is true before the other.
func boolFirst(first bool) int {
	if first {
		return -1
	}
	return 1
}

// serverSide reports whether the run copies cp server-side: its connection is on the target's endpoint, server_side is not
// never, and the target did not refuse a server-side copy from that connection in this run.
func (c *copier) serverSide(cp backup.SourceCopy) bool {
	if !cp.ServerSide || c.r.d.Cfg.Copy.ServerSide == "never" {
		return false
	}
	c.serverMu.Lock()
	defer c.serverMu.Unlock()
	return !c.serverOff[cp.Connection]
}

// failureReason says whether an error of a copy lets the run take the next copy, and why: a missing object, another size, other
// content, or a connection that did not answer. Any other error (the target, a wrong read login) fails the run.
func failureReason(err error) (string, bool) {
	switch {
	case errors.Is(err, errSourceCorrupt):
		return "corrupt", true
	case errors.Is(err, errSourceSize):
		return "size", true
	case errors.Is(err, errSourceMissing), errors.Is(err, backup.ErrBlobNotFound), errors.Is(err, target.ErrNotFound):
		return "missing", true
	case errors.Is(err, backup.ErrCopyUnavailable):
		return "unavailable", true
	}
	return "", false
}

// failed counts and logs a copy the run could not take (R-BAK-030: the backup pod marks no copy; the reads and the scrub of the
// installation find the same damage).
func (c *copier) failed(cp backup.SourceCopy, e format.BlobEntry, reason string, err error) {
	c.mu.Lock()
	c.stats.sourceFailures[cp.Connection]++
	c.mu.Unlock()
	c.r.d.Log.Warn("a copy of a referenced blob could not be taken; trying the next", "sha256", e.Digest.Hex(), "connection", cp.Connection, "reason", reason, "err", err)
}

// fromCopy writes the pool object from one copy: its size first, then server-side where the target can, otherwise through the pod.
func (c *copier) fromCopy(ctx context.Context, e format.BlobEntry, cp backup.SourceCopy, key string) (target.CopyMode, error) {
	size, err := c.r.d.Source.Size(ctx, cp)
	if err != nil {
		return "", err
	}
	if size != cp.StoredSize {
		return "", fmt.Errorf("%w: %d bytes, %d stored", errSourceSize, size, cp.StoredSize)
	}
	return c.transfer(ctx, e, cp, key)
}

// confirmPool checks the pool object a copy wrote and counts it (R-BAK-006 "Confirmation").
func (c *copier) confirmPool(ctx context.Context, e format.BlobEntry, key string, mode target.CopyMode) error {
	r := c.r
	o, err := r.d.Target.Head(ctx, key)
	if err != nil {
		return classify(fmt.Errorf("confirm the pool object %s: %w", e.Digest.Hex(), err))
	}
	if o.Size != e.Size {
		return fmt.Errorf("the pool object %s has %d bytes, %d were copied", e.Digest.Hex(), o.Size, e.Size)
	}
	if o.SHA256 != "" && o.SHA256 != e.Digest.Hex() {
		return fmt.Errorf("%w: the pool object %s reports the checksum %s", backup.ErrPoolDamaged, e.Digest.Hex(), o.SHA256)
	}
	r.d.Metrics.Copied(string(mode), e.Size)
	c.confirm(e, nil)
	return nil
}

// damaged tells source damage from a lost hold (R-BAK-006 "Source damage"): the blob row is read at READ COMMITTED. A row that is
// live means the object is damaged in the source (R-STOR-027), recorded and the run goes on; a row that is deleting or gone means
// the hold was lost, and the run fails without a manifest.
func (c *copier) damaged(ctx context.Context, e format.BlobEntry, status string) error {
	state, _, found, err := c.r.d.BlobState.BlobState(ctx, e.Digest)
	if err != nil {
		return classify(fmt.Errorf("read the blob row %s: %w", e.Digest.Hex(), err))
	}
	if !found || state != "live" {
		return fmt.Errorf("%w: %s is %s", backup.ErrHoldLost, e.Digest.Hex(), map[bool]string{true: "in state " + state, false: "gone"}[found])
	}
	c.r.d.Log.Warn("a referenced blob is damaged in the source", "sha256", e.Digest.Hex(), "status", status)
	c.confirm(e, &format.DamagedBlob{SHA256: e.Digest.Hex(), Status: status})
	return nil
}

// transfer writes the pool object from one copy, server-side where the target can, otherwise through the pod (R-BAK-006 "Copy").
func (c *copier) transfer(ctx context.Context, e format.BlobEntry, cp backup.SourceCopy, key string) (target.CopyMode, error) {
	r := c.r
	mode := r.d.Cfg.Copy.ServerSide
	if mode == "always" && !c.serverSide(cp) {
		return "", backup.Fail(backup.ExitFailed, backup.CodeInternal, fmt.Errorf("backup.copy.server_side is always, but the copy of %s on connection %s cannot be copied server-side: its endpoint is not the target's, or the target refused it", e.Digest.Hex(), cp.Connection))
	}
	if c.serverSide(cp) {
		if err := c.lim.wait(ctx, cp.StoredSize); err != nil {
			return "", err
		}
		ref := r.d.Source.Ref(cp)
		ref.Size = cp.StoredSize
		ref.Progress = func(int64) { r.h.Tick() } // a long copy inside the store is progress (R-BAK-013)
		err := r.d.Target.Copy(ctx, ref, key)
		switch {
		case err == nil:
			r.h.Tick()
			return target.CopyServer, nil
		case errors.Is(err, target.ErrServerCopyUnavailable):
			if mode == "always" {
				return "", backup.Fail(backup.ExitFailed, backup.CodeInternal, fmt.Errorf("backup.copy.server_side is always but the store cannot copy server-side: %w", err))
			}
			c.disableServer(cp.Connection)
		default:
			return "", err
		}
	}
	return target.CopyStream, c.stream(ctx, e, cp, key)
}

// disableServer remembers that the target rejected a server-side copy from a connection: with 'auto' the run streams that
// connection's copies from then on.
func (c *copier) disableServer(conn string) {
	c.serverMu.Lock()
	if !c.serverOff[conn] {
		c.r.d.Log.Info("the target cannot copy server-side from this connection; copying through the pod", "connection", conn)
	}
	c.serverOff[conn] = true
	c.serverMu.Unlock()
}

// tickReader reports progress for every read and applies the rate limit.
type tickReader struct {
	ctx context.Context
	rc  io.Reader
	c   *copier
}

func (t *tickReader) Read(p []byte) (int, error) {
	n, err := t.rc.Read(p)
	if n > 0 {
		t.c.r.h.Tick()
		if lerr := t.c.lim.wait(t.ctx, int64(n)); lerr != nil {
			return n, lerr
		}
	}
	return n, err
}

// stream copies through the pod: ranged reads of the copy in parts of backup.copy.part_size, piped into the upload while the
// digest is computed; a stream that does not hash to the digest aborts the upload.
func (c *copier) stream(ctx context.Context, e format.BlobEntry, cp backup.SourceCopy, key string) error {
	r := c.r
	w, err := r.newWriterHint(ctx, key, cp.StoredSize)
	if err != nil {
		return err
	}
	fail := func(err error) error {
		r.abort(ctx, w)
		r.forget(w)
		return err
	}
	h := sha256.New()
	part := int64(r.d.Cfg.Copy.PartSize)
	for off := int64(0); off < cp.StoredSize; off += part {
		length := min(part, cp.StoredSize-off)
		rc, err := r.d.Source.Open(ctx, cp, off, length)
		if err != nil {
			if errors.Is(err, backup.ErrBlobNotFound) {
				err = errSourceMissing
			}
			return fail(err)
		}
		n, err := io.Copy(io.MultiWriter(w, h), &tickReader{ctx: ctx, rc: rc, c: c})
		_ = rc.Close()
		if err != nil {
			return fail(err)
		}
		if n != length {
			return fail(fmt.Errorf("%w: a read of %d bytes returned %d", errSourceMissing, length, n))
		}
	}
	var sum format.Digest
	copy(sum[:], h.Sum(nil))
	if sum != e.Digest {
		return fail(errSourceCorrupt)
	}
	if _, err := w.Close(ctx); err != nil {
		r.forget(w)
		return err
	}
	r.forget(w)
	return nil
}
