package fixup

import (
	"context"
	"errors"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

func init() {
	Register(Fixup{Name: "storage-copies", Order: OrderStorageUsage + 10, Apply: recordCopies})
}

// copyRowsBatch is the copy rows of an encrypted sink inserted per statement.
const copyRowsBatch = 1000

// recordCopies records where the restored blobs lie (backup-and-restore.md R-BAK-030, R-BAK-031, storage.md R-STOR-052): the copy
// tables are transient and come back empty. Every connection of the restoring configuration gets its row in storage_connections with
// the bucket and the prefix of its element and its fixed properties encrypted and keyed_names, and every restored live blob a copy row
// on the connection step 3 wrote into, present (missing for a blob the backup listed as damaged), verified when the restore streamed
// and hashed every blob (Q-BAK-72). On an encrypted sink the rows carry the name of its naming function, S(L) and the storage key of
// each object (R-BAK-031), and the keys the restore drew for it, which the load's storage_keys does not hold, are inserted first. No
// queue row is written: the reconciliation's first pass finds every other connection's gaps after the workers start.
func recordCopies(ctx context.Context, tx meta.Tx, env Env) error {
	if env.Sink == "" {
		return errors.New("the restore names no connection it wrote the blobs into")
	}
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	prefix := ""
	for _, c := range env.Connections {
		if err := qs.RestoreRecordConnection(ctx, q.RestoreRecordConnectionParams{
			ID: c.ID, Bucket: c.Bucket, Prefix: c.Prefix, Encrypted: c.Encrypted, KeyedNames: c.KeyedNames,
		}); err != nil {
			return err
		}
		if c.ID == env.Sink && c.Prefix != "" {
			prefix = c.Prefix + "/"
		}
	}
	for _, k := range env.StorageKeys {
		if err := qs.RestoreInsertStorageKey(ctx, q.RestoreInsertStorageKeyParams{
			KeyID: k.KeyID, ConnectionID: k.Connection, Kind: k.Kind, Generation: int32(k.Generation), SealedKey: k.Sealed, //nolint:gosec // a generation of at most 999999
		}); err != nil {
			return err
		}
	}
	verifiedAt := env.Now.UTC().Format(time.RFC3339Nano)
	if env.SinkCopies == nil {
		_, err = qs.RestoreRecordCopies(ctx, q.RestoreRecordCopiesParams{
			ConnectionID: env.Sink, KeyPrefix: prefix, Verified: env.SinkVerified, VerifiedAt: verifiedAt,
		})
		return err
	}
	var batch []SinkCopy
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		arg := q.RestoreRecordCopyRowsParams{ConnectionID: env.Sink, Verified: env.SinkVerified, VerifiedAt: verifiedAt}
		for _, c := range batch {
			d := c.Digest
			arg.Digests = append(arg.Digests, d[:])
			arg.ObjectKeys = append(arg.ObjectKeys, c.ObjectKey)
			arg.StoredSizes = append(arg.StoredSizes, c.StoredSize)
			arg.StorageKeyIds = append(arg.StorageKeyIds, c.StorageKeyID)
		}
		batch = batch[:0]
		_, err := qs.RestoreRecordCopyRows(ctx, arg)
		return err
	}
	if err := env.SinkCopies(ctx, func(c SinkCopy) error {
		batch = append(batch, c)
		if len(batch) >= copyRowsBatch {
			return flush()
		}
		return nil
	}); err != nil {
		return err
	}
	return flush()
}
