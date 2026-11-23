package create

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"math/rand/v2"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/format"
	storagekeys "github.com/guided-traffic/quader/internal/blob/keys"
)

// listResult is what the diff pass produced, and, once the copies are done, the final blob list (R-BAK-032).
type listResult struct {
	file       format.BlobsInfo // File, SHA256, Referenced, ReferencedBytes: set by the final list
	toCopy     int64
	toCopyByte int64
	// repool counts the entries of the to-copy set that inherited a pool object wrapped under a key of a connection that drains or is
	// gone (R-BAK-032 item 3).
	repool int64
	sample []format.BlobEntry
	// damaged are the referenced blobs whose row is not ok in the snapshot (R-BAK-006 "Source damage"): listed, not copied.
	damaged []format.DamagedBlob
	// poolKeys are the storage keys the final list names with their objects and bytes as stored (the manifest's storageKeys.pool).
	poolKeys []format.PoolKey
}

// statusIter remembers the referenced blobs whose row is not ok while the stream passes.
type statusIter struct {
	it  format.BlobIterator
	bad []format.DamagedBlob
}

func (s *statusIter) Next() (format.BlobEntry, error) {
	e, err := s.it.Next()
	if err == nil && e.Status != "" {
		s.bad = append(s.bad, format.DamagedBlob{SHA256: e.Digest.Hex(), Status: e.Status})
	}
	return e, err
}

// countIter counts the entries that pass.
type countIter struct {
	it format.BlobIterator
	n  int64
}

func (c *countIter) Next() (format.BlobEntry, error) {
	e, err := c.it.Next()
	if err == nil {
		c.n++
	}
	return e, err
}

// teeHash hashes what a reader delivers.
type teeHash struct {
	r io.Reader
	h hash.Hash
}

func (t *teeHash) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	t.h.Write(p[:n])
	return n, err
}

// sampler keeps a uniform sample of the entries that came from earlier backups (R-BAK-009 "Spot check"), with the pool object each
// names: a reservoir, so the blob list never has to be held. A digest the run copies itself is not a candidate.
type sampler struct {
	k    int
	seen int64
	rng  *rand.Rand
	out  []format.BlobEntry
}

func newSampler(k int) *sampler {
	var seed [16]byte
	_, _ = crand.Read(seed[:])
	return &sampler{k: k, rng: rand.New(rand.NewPCG(binary.LittleEndian.Uint64(seed[:8]), binary.LittleEndian.Uint64(seed[8:])))} //nolint:gosec // a sample, not a secret
}

// inherited offers an entry an earlier backup's pool holds.
func (s *sampler) inherited(e format.BlobEntry) {
	if s.k <= 0 {
		return
	}
	s.seen++
	switch {
	case len(s.out) < s.k:
		s.out = append(s.out, e)
	default:
		if j := s.rng.Int64N(s.seen); j < int64(s.k) {
			s.out[j] = e
		}
	}
}

// departing reports whether a pool object wrapped under the storage key id must be copied again from a connection that stays
// (R-BAK-032 item 3): its connection, read from the identifier, is not a connection of the run's configuration with drain: false. An
// identifier that names no connection is inherited as before.
func (r *runState) departing(id string) bool {
	k, ok := storagekeys.ParseID(id)
	return ok && !r.staying[k.Connection]
}

// diff is steps 3 and 4 of the hold protocol of R-BAK-007 around the merge of R-BAK-006: the referenced set of the snapshot is
// merged with the previous backup's list in one pass that writes the provisional blob list (R-BAK-032) and inserts the to-copy set
// as pins, in batches that commit one by one. Then the hold turns from 'all' to 'pinned', in a transaction of its own.
//
// A line of the provisional list names the pool object it inherits, PoolPlain for a blob damaged in the source (listed, not in the
// pool), or PoolPending for a digest of the to-copy set: a new one, pinned without a pool object, and an inherited one whose pool
// object is wrapped under a key of a connection that drains or is gone, pinned with the pool object it inherits (the re-pool,
// Q-BAK-80). The final list resolves PoolPending from the pins after the copies.
func (r *runState) diff(ctx context.Context) error {
	rawRefs, err := r.snap.Referenced(ctx)
	if err != nil {
		return err
	}
	refs := &statusIter{it: rawRefs}
	var known format.BlobIterator = &format.EntryList{}
	var prevHash *teeHash
	var prevCount *countIter
	if r.prev != nil {
		rc, _, err := r.d.Target.Get(ctx, r.layout.BlobList(r.prev.id))
		if err != nil {
			return classify(fmt.Errorf("%w: the blob list of %s: %w", backup.ErrPoolDamaged, r.prev.id, err))
		}
		defer rc.Close()
		prevHash = &teeHash{r: rc, h: sha256.New()}
		rd, err := format.NewBlobListReader(prevHash)
		if err != nil {
			return fmt.Errorf("%w: the blob list of %s: %w", backup.ErrPoolDamaged, r.prev.id, err)
		}
		defer rd.Close()
		prevCount = &countIter{it: rd}
		skip := map[string]bool{}
		for sha := range r.prev.manifest.DamagedSet() {
			skip[sha] = true
		}
		known = format.Without(prevCount, skip)
	}

	w, err := r.newWriter(ctx, r.layout.PendingBlobList(r.id))
	if err != nil {
		return classify(err)
	}
	lw, err := format.NewPendingBlobListWriter(w, r.d.Cfg.CompressionLevel)
	if err != nil {
		return err
	}
	smp := newSampler(r.d.Cfg.VerifySample)
	var batch []format.BlobEntry
	var pinned, pinnedBytes, repool int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := r.d.Store.AddPins(ctx, r.runID, batch); err != nil {
			return classify(fmt.Errorf("insert the pins: %w", err))
		}
		pinned += int64(len(batch))
		batch = batch[:0]
		n := pinned
		r.h.Update(func(p *backup.Progress) { p.BlobsToCopy, p.PinsLeft = n, n })
		return nil
	}
	pin := func(e format.BlobEntry) error {
		pinnedBytes += e.Size
		batch = append(batch, e)
		if len(batch) >= r.d.PinBatch {
			return flush()
		}
		return nil
	}
	err = format.Diff(refs, known,
		func(e format.BlobEntry) error {
			r.h.Tick()
			line := e
			switch {
			case e.Status != "":
				line.Pool = format.PoolPlain // damaged in the source: listed, not copied, so that the first backup after the repair copies it
			case e.Pool == "":
				line.Pool = format.PoolPending // a new digest: the to-copy callback pins it
			case e.Encrypted() && r.departing(e.Pool):
				line.Pool = format.PoolPending
				repool++
				if err := pin(format.BlobEntry{Digest: e.Digest, Size: e.Size, Pool: e.Pool}); err != nil {
					return err
				}
			default:
				smp.inherited(e)
			}
			return lw.Add(line)
		},
		func(e format.BlobEntry) error {
			if e.Status != "" {
				return nil
			}
			return pin(format.BlobEntry{Digest: e.Digest, Size: e.Size})
		})
	if err != nil {
		return err
	}
	if prevCount != nil {
		if err := r.verifyPreviousList(prevHash, prevCount); err != nil {
			return err
		}
	}
	if err := flush(); err != nil {
		return err
	}
	if err := lw.Close(); err != nil {
		return err
	}
	if _, err := w.Close(ctx); err != nil {
		r.forget(w)
		return classify(fmt.Errorf("write the provisional blob list: %w", err))
	}
	r.forget(w)
	n, bytes := lw.Count()
	r.list = listResult{toCopy: pinned, toCopyByte: pinnedBytes, repool: repool, sample: smp.out, damaged: refs.bad}
	r.res.BlobCount, r.res.BlobBytes = n, bytes

	// Step 3 is done: every pin is committed. Step 4, in a separate transaction.
	if err := r.fault(ctx, PointPinsCommitted); err != nil {
		return err
	}
	to := backup.HoldPinned
	if pinned == 0 {
		to = backup.HoldNone
	}
	if err := r.h.SetHold(ctx, backup.HoldAll, to); err != nil {
		return err
	}
	return r.fault(ctx, PointHoldPinned)
}

// verifyPreviousList reads the rest of the previous backup's blob list and compares its SHA-256 and entry count with the
// manifest, so that the diff trusts the list only if it is the one the manifest names (Q-BAK-30 asks the same of the sweep).
// It runs before the hold turns 'pinned'.
func (r *runState) verifyPreviousList(h *teeHash, c *countIter) error {
	for {
		_, err := c.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: the blob list of %s: %w", backup.ErrPoolDamaged, r.prev.id, err)
		}
	}
	if _, err := io.Copy(io.Discard, h); err != nil {
		return fmt.Errorf("%w: the blob list of %s: %w", backup.ErrPoolDamaged, r.prev.id, err)
	}
	want := r.prev.manifest.Blobs
	if got := hex.EncodeToString(h.h.Sum(nil)); got != want.SHA256 || c.n != want.Referenced {
		return fmt.Errorf("%w: the blob list of %s has %d entries and the SHA-256 %s, its manifest says %d and %s",
			backup.ErrPoolDamaged, r.prev.id, c.n, got, want.Referenced, want.SHA256)
	}
	return nil
}
