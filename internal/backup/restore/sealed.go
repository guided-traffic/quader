package restore

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/fixup"
	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/backup/target"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	storagekeys "github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/meta/postgres"
)

// The restore of encrypted connections (backup-and-restore.md R-BAK-031): the backup's own storage keys, opened with the key ring the
// restore holds before it writes a blob; the keys of an encrypted sink whose id the backup holds none of, drawn by the restore and kept
// in its marker until the fix-ups insert them (Q-BAK-82); and step 3's table of what it writes for a pool object and the sink:
//
//	pool object                          sink        what step 3 writes
//	plain                                plain       as R-BAK-030
//	plain                                encrypted   through the sink's writer with a new data key, streamed and hashed
//	encrypted, a key of the sink's id    encrypted   the object as it is, under the sink's name of the digest; server-side where the
//	                                                 endpoints allow, else streamed; confirmed by size and record
//	encrypted, a key of another id       encrypted   decrypted with that key and written with a new data key
//	encrypted                            plain       decrypted and streamed, hashed

// checkStorageKeys is the part of check 3 that concerns the storage keys (R-BAK-031): the backup's storage_keys is read and every key
// that is not destroyed opened with the key ring; the pool must name no storage key the backup's database lacks or holds destroyed
// (the restored database could not open its objects); and no connection of the restoring configuration, plain or encrypted, may have an
// id the backup holds destroyed keys of (storage.md R-STOR-102): the restored installation's start would refuse it.
func (r *Restorer) checkStorageKeys(ctx context.Context, st *state) error {
	rows, err := backup.ReadBackupKeys(ctx, r.d.Target, r.d.Layout, st.m)
	if err != nil {
		return err
	}
	if st.m.StorageKeys != nil {
		for _, k := range st.m.StorageKeys.Pool {
			if row, ok := rows[k.KeyID]; !ok || row.Destroyed() {
				return checkFailure(3, "storage keys", backup.CodeKeyRingMismatch,
					fmt.Errorf("the pool names the storage key %s, which the backup's database does not hold: its encrypted pool objects cannot be opened", k.KeyID))
			}
		}
	}
	for _, c := range r.d.Connections {
		for id, row := range rows {
			if row.Connection == c.ID && row.Destroyed() {
				return checkFailure(3, "storage keys", backup.CodeKeyRingMismatch,
					fmt.Errorf("the backup holds the destroyed storage key %s of connection %s: a connection id with destroyed keys is retired for good; give the connection another id (storage.md R-STOR-102)", id, c.ID))
			}
		}
	}
	opened, err := rows.Open(r.d.Keys)
	if err != nil {
		return checkFailure(3, "storage keys", backup.CodeKeyRingMismatch, err)
	}
	st.keys, st.opened = rows, opened
	return nil
}

// drawKeys draws the keys of an encrypted sink whose id the backup holds no storage key of (R-BAK-031, storage.md R-STOR-083's rule;
// its location is empty by check 5): a storage key of generation 1 and, with keyed names, a name key, each sealed with the key ring
// under its new identifier. They go into the marker the first migration commits, so that a resumed restore writes under the same keys.
func (r *Restorer) drawKeys(st *state) ([]postgres.RestoreKey, error) {
	sink, err := r.sink()
	if err != nil || !sink.Encrypted {
		return nil, err
	}
	for _, row := range st.keys {
		if row.Connection == sink.ID && row.Kind == storagekeys.KindStorage {
			return nil, nil // the backup holds the sink's keys: the restored database opens what step 3 writes with them
		}
	}
	kinds := []string{storagekeys.KindStorage}
	if sink.KeyedNames {
		kinds = append(kinds, storagekeys.KindName)
	}
	var out []postgres.RestoreKey
	for _, kind := range kinds {
		k, err := blobcrypt.NewKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		id, err := storagekeys.NewKeyID(kind, sink.ID, 1, rand.Reader)
		if err != nil {
			k.Destroy()
			return nil, err
		}
		sealed, err := storagekeys.SealStorageKey(r.d.Keys, id, k)
		if err != nil {
			return nil, err
		}
		out = append(out, postgres.RestoreKey{KeyID: id, Connection: sink.ID, Kind: kind, Generation: 1, Sealed: sealed})
	}
	r.d.Log.Info("drew the storage keys of the encrypted connection the restore writes into", "connection", sink.ID, "keys", len(out))
	return out, nil
}

// sinkKeys is the storage keys step 3 writes and names with on an encrypted sink (blob.StorageKeys): the backup's keys of the sink's
// id, or the keys the restore drew.
type sinkKeys struct {
	conn   string
	active *blobcrypt.StorageKey
	name   *blobcrypt.StorageKey
	all    map[string]*blobcrypt.StorageKey
}

func (k *sinkKeys) Active(_ context.Context, connection string) (*blobcrypt.StorageKey, error) {
	if connection != k.conn || k.active == nil {
		return nil, fmt.Errorf("the restore holds no active storage key of connection %s", connection)
	}
	return k.active, nil
}

func (k *sinkKeys) NameKey(connection string) (blobcrypt.Key, error) {
	if connection != k.conn || k.name == nil {
		return blobcrypt.Key{}, fmt.Errorf("the restore holds no name key of connection %s", connection)
	}
	return k.name.Key, nil
}

func (k *sinkKeys) Lookup(_ context.Context, connection string) blobcrypt.KeyLookup {
	return func(id string) (*blobcrypt.StorageKey, error) {
		if connection != k.conn {
			return nil, nil
		}
		return k.all[id], nil
	}
}

// prepareSink gives an encrypted sink the keys step 3 and the fix-ups use: the active storage key and the name key of its id from the
// backup's keys, or those of the marker the restore drew. A plain sink needs none.
func (r *Restorer) prepareSink(st *state, marker *postgres.RestoreMarker) error {
	sink, err := r.sink()
	if err != nil || !sink.Encrypted || r.sinkKeys != nil {
		return err
	}
	sk := &sinkKeys{conn: sink.ID, all: map[string]*blobcrypt.StorageKey{}}
	for id, row := range st.keys {
		if row.Connection != sink.ID || row.Destroyed() {
			continue
		}
		k := st.opened.Key(id)
		switch {
		case row.Kind == storagekeys.KindName:
			sk.name = k
		case row.State == storagekeys.StateActive:
			sk.active, sk.all[id] = k, k
		default:
			sk.all[id] = k
		}
	}
	if sk.active == nil && marker != nil {
		for _, rk := range marker.StorageKeys {
			k, err := storagekeys.OpenStorageKey(r.d.Keys, rk.KeyID, rk.Sealed)
			if err != nil {
				return err
			}
			r.drawn = append(r.drawn, k)
			if rk.Kind == storagekeys.KindName {
				sk.name = k
			} else {
				sk.active, sk.all[rk.KeyID] = k, k
			}
		}
	}
	switch {
	case sk.active == nil:
		return backup.Failf(backup.ExitFailed, backup.CodeInternal, "the restore holds no storage key of the encrypted connection %s it writes into: its marker names none and the backup holds none", sink.ID)
	case sink.KeyedNames && sk.name == nil:
		return backup.Failf(backup.ExitFailed, backup.CodeInternal, "the restore holds no name key of the encrypted connection %s it writes into", sink.ID)
	}
	sink.Writer.UseKeys(sk)
	r.sinkKeys = sk
	return nil
}

// sealedOne is step 3 for one entry when the sink is encrypted or the pool object is: the table above.
func (c *copier) sealedOne(ctx context.Context, sink Connection, e format.BlobEntry) error {
	if done, err := c.sealedHas(ctx, sink, e); err != nil || done {
		if done {
			c.skipped.Add(1)
			c.r.progress()
		}
		return err
	}
	pool := c.r.d.Layout.PoolObject(e)
	var row backup.SealedKey
	if e.Encrypted() {
		var ok bool
		if row, ok = c.st.keys[e.Pool]; !ok || row.Destroyed() {
			return backup.Failf(backup.ExitFailed, backup.CodeMissingPoolObject,
				"pool object %s of backup %s is wrapped under the storage key %s, which the restored database does not hold", e.PoolName(), c.st.id, e.Pool)
		}
		if sink.Encrypted && row.Connection == sink.ID {
			return c.asIs(ctx, sink, e, pool)
		}
	}
	body, obj, err := c.r.d.Target.Get(ctx, pool)
	if err != nil {
		if errors.Is(err, target.ErrNotFound) {
			return c.missing(e)
		}
		return err
	}
	defer func() { _ = body.Close() }()
	var plain io.Reader = &progressReader{r: body, f: c.r.progress}
	if e.Encrypted() {
		stored, err := blobcrypt.StoredSize(e.Size)
		if err != nil {
			return err
		}
		dk, err := blobcrypt.Open(obj.Metadata, c.st.opened.Lookup(row.Connection), blobcrypt.Want{
			Known: true, Digest: e.Digest, Size: e.Size, StoredSize: stored, ObjectSize: obj.Size,
		})
		if err != nil {
			return refused(c.st.id, e, err)
		}
		defer dk.Key.Destroy()
		r, err := blobcrypt.NewReader(plain, dk, true)
		if err != nil {
			return refused(c.st.id, e, err)
		}
		plain = r
	}
	if sink.Encrypted {
		_, err = sink.Writer.WriteSealedStream(ctx, blob.Digest(e.Digest), e.Size, plain, c.r.partSize())
	} else {
		err = sink.Store.Put(ctx, e.Digest, e.Size, plain)
	}
	if err != nil {
		var se *blob.StreamSourceError
		if errors.As(err, &se) {
			err = se.Err
		}
		if _, ok := blobcrypt.ReasonOf(err); ok {
			return refused(c.st.id, e, err)
		}
		return fmt.Errorf("copy %s: %w", e.PoolName(), err)
	}
	c.done(false)
	return nil
}

// refused is a pool object whose stored format a reason refused: the restore stops, as for a missing pool object, which verify
// --deep --key-ring reports (R-BAK-031).
func refused(id format.ID, e format.BlobEntry, err error) error {
	return backup.Failf(backup.ExitFailed, backup.CodeMissingPoolObject, "pool object %s of backup %s does not authenticate: %v; quader backup verify --deep --key-ring reports it", e.PoolName(), id, err)
}

// sealedHas reports whether the sink holds the blob already (a resumed restore): on an encrypted sink an object at its name of S(L)
// bytes whose record opens with the sink's keys, bound to the blob (R-STOR-077 steps 1 to 6); on a plain one an object of its size.
func (c *copier) sealedHas(ctx context.Context, sink Connection, e format.BlobEntry) (bool, error) {
	if !sink.Encrypted {
		return sink.Store.Has(ctx, e.Digest, e.Size)
	}
	name, err := sink.Writer.Name(blob.Digest(e.Digest))
	if err != nil {
		return false, err
	}
	stored, err := blobcrypt.StoredSize(e.Size)
	if err != nil {
		return false, err
	}
	info, err := sink.Writer.Objects().Head(ctx, name)
	switch {
	case errors.Is(err, blob.ErrNotFound):
		return false, nil
	case err != nil:
		return false, err
	case info.Size != stored:
		return false, nil
	}
	dk, err := blobcrypt.Open(info.Metadata, c.r.sinkKeys.Lookup(ctx, sink.ID), blobcrypt.Want{
		Known: true, Digest: e.Digest, Size: e.Size, StoredSize: stored, ObjectSize: info.Size,
	})
	if err != nil {
		return false, nil //nolint:nilerr // an object that does not open is written again
	}
	dk.Key.Destroy()
	return true, nil
}

// asIs writes an encrypted pool object wrapped under a key of the sink's own id as it is, with its record, under the sink's name of
// the digest (R-BAK-031): server-side where the endpoints allow, else streamed through the pod; confirmed by its size and its record.
func (c *copier) asIs(ctx context.Context, sink Connection, e format.BlobEntry, pool string) error {
	name, err := sink.Writer.Name(blob.Digest(e.Digest))
	if err != nil {
		return err
	}
	stored, err := blobcrypt.StoredSize(e.Size)
	if err != nil {
		return err
	}
	head, err := c.r.d.Target.Head(ctx, pool)
	if err != nil {
		if errors.Is(err, target.ErrNotFound) {
			return c.missing(e)
		}
		return err
	}
	server := false
	if !c.streamOnly.Load() {
		src := target.ObjectRef{
			Endpoint: c.r.d.TargetEndpoint, Bucket: c.r.d.TargetBucket, Key: pool, Size: stored, Metadata: head.Metadata,
			Progress: func(int64) { c.r.progress() },
		}
		switch err := sink.Store.CopyAs(ctx, src, name); {
		case err == nil:
			server = true
		case errors.Is(err, target.ErrServerCopyUnavailable):
			if c.mode == "always" {
				return backup.Failf(backup.ExitFailed, backup.CodeInternal, "backup.copy.server_side is always, but the blob store cannot copy from the target: %w", err)
			}
			if c.streamOnly.CompareAndSwap(false, true) {
				c.r.d.Log.Info("server-side copy is not available, streaming from now on")
			}
		case errors.Is(err, target.ErrNotFound):
			return c.missing(e)
		default:
			return fmt.Errorf("copy %s: %w", e.PoolName(), err)
		}
	}
	if !server {
		if err := c.streamAsIs(ctx, sink, pool, name, stored); err != nil {
			return err
		}
	}
	info, err := sink.Writer.Objects().Head(ctx, name)
	if err != nil {
		return err
	}
	if info.Size != stored || info.Metadata[blobcrypt.MetaKey] != e.Pool {
		return fmt.Errorf("the object %s on connection %s has %d bytes and the record of %q after the copy, want %d and %s", name, sink.ID, info.Size,
			info.Metadata[blobcrypt.MetaKey], stored, e.Pool)
	}
	c.asIsN.Add(1)
	c.done(server)
	return nil
}

// streamAsIs streams the pool object, ciphertext and record as they are, onto the sink: one request up to a part, a multipart upload
// of parts read into memory one at a time above.
func (c *copier) streamAsIs(ctx context.Context, sink Connection, pool, name string, stored int64) (err error) {
	body, obj, err := c.r.d.Target.Get(ctx, pool)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	if blobcrypt.CheckRecordForm(obj.Metadata) != nil {
		return fmt.Errorf("the pool object %s carries no record of the stored format", pool)
	}
	md := blob.Metadata(obj.Metadata)
	objects := sink.Writer.Objects()
	r := &progressReader{r: body, f: c.r.progress}
	part := max(blob.PartSize(stored, c.r.partSize()), 5<<20)
	if stored <= part {
		b, err := io.ReadAll(io.LimitReader(r, stored+1))
		if err != nil {
			return err
		}
		if int64(len(b)) != stored {
			return fmt.Errorf("the pool object %s has %d bytes, its list says %d", pool, len(b), stored)
		}
		return objects.Put(ctx, name, bytes.NewReader(b), stored, blob.Checksum{}, md)
	}
	id, err := objects.CreateMultipart(ctx, name, "", md)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = objects.AbortMultipart(context.WithoutCancel(ctx), name, id)
		}
	}()
	buf := make([]byte, part)
	var parts []blob.Part
	for off, n := int64(0), 1; off < stored; off, n = off+part, n+1 {
		ln := min(part, stored-off)
		if _, err := io.ReadFull(r, buf[:ln]); err != nil {
			return err
		}
		p, err := objects.UploadPart(ctx, name, id, n, bytes.NewReader(buf[:ln]), ln, blob.Checksum{})
		if err != nil {
			return err
		}
		p.Number, p.Size = n, ln
		parts = append(parts, p)
	}
	return objects.CompleteMultipart(ctx, name, id, parts)
}

// sinkCopies yields the copy row of every blob of the backup's list on an encrypted sink (R-BAK-031): its name, S(L) and the storage
// key of its object, which is the pool object's own when step 3 wrote it as it is and the sink's active key otherwise.
func (r *Restorer) sinkCopies(st *state, sink Connection) func(ctx context.Context, yield func(fixup.SinkCopy) error) error {
	return func(ctx context.Context, yield func(fixup.SinkCopy) error) error {
		rc, _, err := r.d.Target.Get(ctx, r.d.Layout.BlobList(st.id))
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		list, err := format.NewBlobListReader(rc)
		if err != nil {
			return err
		}
		defer list.Close()
		damaged := st.m.DamagedSet()
		for {
			e, err := list.Next()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			name, err := sink.Writer.Name(blob.Digest(e.Digest))
			if err != nil {
				return err
			}
			stored, err := blobcrypt.StoredSize(e.Size)
			if err != nil {
				return err
			}
			key := r.sinkKeys.active.ID
			if row, ok := st.keys[e.Pool]; ok && e.Encrypted() && row.Connection == sink.ID {
				key = e.Pool
			}
			if _, bad := damaged[e.Digest.Hex()]; bad {
				key = ""
			}
			if err := yield(fixup.SinkCopy{Digest: e.Digest, ObjectKey: name, StoredSize: stored, StorageKeyID: key}); err != nil {
				return err
			}
			r.progress()
		}
	}
}

// drawnRows is the marker's drawn keys as the fix-up inserts them.
func drawnRows(m *postgres.RestoreMarker) []fixup.StorageKeyRow {
	if m == nil {
		return nil
	}
	out := make([]fixup.StorageKeyRow, 0, len(m.StorageKeys))
	for _, k := range m.StorageKeys {
		out = append(out, fixup.StorageKeyRow{KeyID: k.KeyID, Connection: k.Connection, Kind: k.Kind, Generation: k.Generation, Sealed: k.Sealed})
	}
	return slices.Clip(out)
}
