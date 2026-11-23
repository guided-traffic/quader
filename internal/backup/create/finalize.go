package create

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/backup/listing"
	"github.com/guided-traffic/quader/internal/backup/target"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	storagekeys "github.com/guided-traffic/quader/internal/blob/keys"
)

// finalize is the phase 'finalizing' (R-BAK-009): the fenced lease check, the spot check of the pool, the marker check and the
// manifest, the last object of a backup and its commit point (R-BAK-002).
func (r *runState) finalize(ctx context.Context) error {
	// copying -> finalizing only while the lease has room for the manifest's deadline (R-BAK-010).
	if err := r.h.Fence(ctx, backup.StateFinalizing); err != nil {
		return err
	}
	if err := r.fault(ctx, PointFinalizing); err != nil {
		return err
	}
	if err := r.spotCheck(ctx); err != nil {
		return err
	}
	m, err := r.buildManifest()
	if err != nil {
		return err
	}
	body, err := m.Encode()
	if err != nil {
		return err
	}
	// The marker is read again before a manifest is written (R-BAK-002), and the lease is fenced once more, so that a
	// run that stalled in the spot check cannot write after it was abandoned.
	if mk, found, err := r.readMarker(ctx); err != nil {
		return err
	} else if !found {
		return backup.Failf(backup.ExitForeignTarget, backup.CodeForeignTarget, "the target marker disappeared during the run")
	} else if err := mk.Check(r.facts.InstallationID, r.facts.Incarnation); err != nil {
		return backup.Fail(backup.ExitForeignTarget, backup.CodeForeignTarget, err)
	}
	if err := r.h.Fence(ctx, backup.StateFinalizing); err != nil {
		return err
	}
	pctx, cancel := context.WithTimeout(ctx, r.d.Cfg.FinalizeTimeout)
	defer cancel()
	if _, err := r.d.Target.Put(pctx, r.layout.Manifest(r.id), body, target.PutOptions{ContentType: "application/json"}); err != nil {
		return classify(fmt.Errorf("write the manifest: %w", err))
	}
	r.res.DatabaseBytes = listing.DatabaseBytes(m)
	r.res.BlobsCopied, r.res.BytesCopied, r.res.DamagedBlobs = m.Blobs.Copied, m.Blobs.CopiedBytes, int64(len(m.Blobs.Damaged))
	r.res.Repooled = r.copied.repooled
	if len(r.copied.sourceFailures) > 0 {
		r.res.SourceFailures = r.copied.sourceFailures
	}
	for _, t := range r.snapInfo.Tables {
		if !t.Registered {
			r.res.UnregisteredTables = append(r.res.UnregisteredTables, t.Name)
		}
	}
	sort.Strings(r.res.UnregisteredTables)
	if len(r.res.UnregisteredTables) > 0 {
		r.d.Log.Warn("tables without an export class were exported as data", "tables", r.res.UnregisteredTables)
	}
	return nil
}

// spotCheck sends HeadObject for backup.verify_sample random entries that earlier backups listed, each at the pool object its line
// names (R-BAK-009, R-BAK-032): a missing object fails the run with pool_damaged and the advice to run `quader backup verify`.
func (r *runState) spotCheck(ctx context.Context) error {
	if len(r.list.sample) == 0 {
		return nil
	}
	g, _ := newGroup(ctx)
	work := make(chan format.BlobEntry)
	g.Go(func(ctx context.Context) error {
		defer close(work)
		for _, e := range r.list.sample {
			select {
			case work <- e:
			case <-ctx.Done():
				return nil
			}
		}
		return nil
	})
	for range max(r.d.Cfg.Copy.Concurrency, 1) {
		g.Go(func(ctx context.Context) error {
			for e := range work {
				if ctx.Err() != nil {
					return context.Cause(ctx)
				}
				_, err := r.d.Target.Head(ctx, r.layout.PoolObject(e))
				if errors.Is(err, target.ErrNotFound) {
					return fmt.Errorf("%w: %s is listed by an earlier backup and missing from the pool", backup.ErrPoolDamaged, e.PoolName())
				}
				if err != nil {
					return classify(fmt.Errorf("spot check of %s: %w", e.PoolName(), err))
				}
				r.h.Tick()
			}
			return nil
		})
	}
	return g.Wait()
}

func (r *runState) buildManifest() (*format.Manifest, error) {
	d := r.d
	damaged := append(append([]format.DamagedBlob{}, r.copied.damaged...), r.list.damaged...)
	sort.Slice(damaged, func(i, j int) bool { return damaged[i].SHA256 < damaged[j].SHA256 })
	files := r.tableFiles
	if files == nil {
		files = []format.TableFile{}
	}
	seqs := r.sequences
	if seqs == nil {
		seqs = []format.Sequence{}
	}
	blobs := r.list.file
	blobs.Copied, blobs.CopiedBytes, blobs.Damaged = r.copied.copied, r.copied.bytes, damaged
	m := &format.Manifest{
		Format: format.ManifestFormat, BackupID: r.id, InstallationID: r.facts.InstallationID, Incarnation: r.facts.Incarnation,
		Quader:    format.QuaderInfo{Version: d.Version, Revision: d.Revision},
		Schema:    r.snapInfo.Schema,
		Postgres:  format.PostgresRef{ServerVersion: r.snapInfo.ServerVersion, Isolation: "repeatable read"},
		StartedAt: r.started.UTC(), SnapshotAt: r.snapInfo.At.UTC(), CompletedAt: d.Now().UTC(),
		Options: format.Options{ExcludeRemoteCaches: r.opts.ExcludeRemoteCaches || d.Cfg.Exclude.RemoteCaches},
		Source:  format.Source{Connections: append([]format.ConnectionRef{}, d.SourceConnections...), RevocationJournal: d.Cfg.RevocationJournal.Enabled},
		Tables:  files, Sequences: seqs, Blobs: blobs,
		Secrets:     format.SecretsInfo{KeyIDs: r.snapInfo.KeyIDs},
		StorageKeys: r.storageKeys(),
		Encryption:  format.Encryption{SSE: r.sse},
	}
	if r.prev != nil {
		id := r.prev.id
		m.PreviousBackupID = &id
	}
	return m, nil
}

// prune is the phase 'pruning' (R-BAK-009, R-BAK-016): retention and the pool sweep through the retention package, inside this
// run. The backup is complete by now, so a failure of the sweep is a warning of the run, not a failed backup; only a run that was
// taken over (superseded) must stop writing.
func (r *runState) prune(ctx context.Context) error {
	if r.opts.NoPrune || r.d.Prune == nil {
		return nil
	}
	if err := r.h.Fence(ctx, backup.StatePruning); err != nil {
		return err
	}
	err := r.d.Prune.Prune(ctx, r.h, r.res)
	if err == nil {
		return nil
	}
	if errors.Is(err, backup.ErrSuperseded) {
		return err
	}
	msg := fmt.Sprintf("retention and the pool sweep did not finish: %v; the next run prunes", err)
	r.d.Log.Error("pruning failed after the backup was written", "backup", r.id, "err", err)
	r.res.Warnings = append(r.res.Warnings, msg)
	r.pruneWarned = true
	return nil
}

// storageKeys is the manifest's storageKeys (R-BAK-032): the pool's keys counted from the final list, and the rows of storage_keys
// the snapshot holds, sealed and destroyed.
func (r *runState) storageKeys() *format.StorageKeysInfo {
	sk := &format.StorageKeysInfo{Pool: append([]format.PoolKey{}, r.list.poolKeys...), Sealed: []string{}, Destroyed: []string{}}
	for _, k := range r.snapInfo.StorageKeys {
		if k.Destroyed {
			sk.Destroyed = append(sk.Destroyed, k.ID)
		} else {
			sk.Sealed = append(sk.Sealed, k.ID)
		}
	}
	sort.Strings(sk.Sealed)
	sort.Strings(sk.Destroyed)
	return sk
}

// finalList writes the final blob list after the copies (R-BAK-032 item 2, Q-BAK-75): a streaming merge of the provisional list
// with the run's pins, both in digest order, in which every PoolPending line takes the pool object its pin records (PoolPlain for a
// blob the copy found damaged in the source, which is not in the pool); then the provisional list is deleted and the hold released,
// since the pins have served. It runs at the end of the phase copying, once the table export and the copy are done, where the hold
// may still change (R-BAK-007). It counts the pool's keys for the manifest and warns while lines name keys of connections the
// configuration no longer has.
func (r *runState) finalList(ctx context.Context) error {
	rc, _, err := r.d.Target.Get(ctx, r.layout.PendingBlobList(r.id))
	if err != nil {
		return classify(fmt.Errorf("read the provisional blob list: %w", err))
	}
	defer rc.Close()
	pending, err := format.NewPendingBlobListReader(rc)
	if err != nil {
		return err
	}
	defer pending.Close()
	damaged := make(map[format.Digest]bool, len(r.copied.damaged))
	for _, d := range r.copied.damaged {
		if dg, err := format.ParseDigest(d.SHA256); err == nil {
			damaged[dg] = true
		}
	}
	pins := &pinStream{r: r, ctx: ctx}
	w, err := r.newWriter(ctx, r.layout.BlobList(r.id))
	if err != nil {
		return classify(err)
	}
	sum := format.NewSumWriter(w)
	lw, err := format.NewBlobListWriter(sum, r.d.Cfg.CompressionLevel)
	if err != nil {
		return err
	}
	keys := map[string]*format.PoolKey{}
	gone := map[string]int64{}
	for {
		e, err := pending.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("the provisional blob list: %w", err)
		}
		if e.Pool == format.PoolPending {
			p, err := pins.at(e.Digest)
			if err != nil {
				return err
			}
			switch {
			case p != "":
				e.Pool = p
			case damaged[e.Digest]:
				e.Pool = format.PoolPlain
			default:
				return fmt.Errorf("the pin of %s names no pool object after the copies", e.Digest.Hex())
			}
		}
		if err := lw.Add(e); err != nil {
			return err
		}
		r.h.Tick()
		if !e.Encrypted() {
			continue
		}
		k := keys[e.Pool]
		if k == nil {
			k = &format.PoolKey{KeyID: e.Pool}
			keys[e.Pool] = k
		}
		stored, err := blobcrypt.StoredSize(e.Size)
		if err != nil {
			return err
		}
		k.Objects++
		k.Bytes += stored
		if id, ok := storagekeys.ParseID(e.Pool); ok && !r.configured[id.Connection] {
			gone[e.Pool]++
		}
	}
	if err := lw.Close(); err != nil {
		return err
	}
	obj, err := w.Close(ctx)
	r.forget(w)
	if err != nil {
		return classify(fmt.Errorf("write the blob list: %w", err))
	}
	if obj.Size != sum.Bytes() {
		return fmt.Errorf("the target stored %d bytes of the blob list, %d were sent", obj.Size, sum.Bytes())
	}
	n, bytes := lw.Count()
	r.list.file = format.BlobsInfo{File: format.BlobListName, SHA256: sum.SHA256(), Referenced: n, ReferencedBytes: bytes}
	r.list.poolKeys = r.list.poolKeys[:0]
	for _, id := range slices.Sorted(maps.Keys(keys)) {
		r.list.poolKeys = append(r.list.poolKeys, *keys[id])
	}
	if err := r.d.Target.Delete(ctx, []string{r.layout.PendingBlobList(r.id)}); err != nil {
		r.d.Log.Warn("the provisional blob list could not be deleted; retention removes it with the backup", "backup", r.id, "err", err)
	}
	if len(gone) > 0 {
		parts := make([]string, 0, len(gone))
		for _, id := range slices.Sorted(maps.Keys(gone)) {
			parts = append(parts, fmt.Sprintf("%s (%d)", id, gone[id]))
		}
		r.res.Warnings = append(r.res.Warnings, "the blob list names pool objects wrapped under storage keys of connections this configuration no longer has, "+
			"and no connection that stays could give them: "+strings.Join(parts, ", ")+" (R-BAK-032)")
		r.warned = true
	}
	if r.list.toCopy > 0 {
		return r.h.SetHold(ctx, backup.HoldPinned, backup.HoldNone)
	}
	return nil
}

// pinStream pages through the run's pins in digest order for the final list.
type pinStream struct {
	r     *runState
	ctx   context.Context //nolint:containedctx // the stream's pages are read within the final list's call
	page  []format.BlobEntry
	after format.Digest
	done  bool
}

// at returns the pool the pin of d records, advancing through the pins; a digest without a pin is an error.
func (p *pinStream) at(d format.Digest) (string, error) {
	for {
		for len(p.page) > 0 && p.page[0].Digest.Less(d) {
			p.page = p.page[1:]
		}
		if len(p.page) > 0 {
			if p.page[0].Digest == d {
				pool := p.page[0].Pool
				p.page = p.page[1:]
				return pool, nil
			}
			return "", fmt.Errorf("the provisional blob list names %s for a copy, and the run holds no pin of it", d.Hex())
		}
		if p.done {
			return "", fmt.Errorf("the provisional blob list names %s for a copy, and the run holds no pin of it", d.Hex())
		}
		page, err := p.r.d.Pins.Pins(p.ctx, p.r.runID, p.after, 1000)
		if err != nil {
			return "", classify(fmt.Errorf("read the pins: %w", err))
		}
		if len(page) == 0 {
			p.done = true
			continue
		}
		p.page, p.after = page, page[len(page)-1].Digest
	}
}
