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

// recordCopies records where the restored blobs lie (backup-and-restore.md R-BAK-030, storage.md R-STOR-052): the copy tables
// are transient and come back empty. Every connection of the restoring configuration gets its row in storage_connections with
// the bucket and the prefix of its element, and every restored live blob a copy row on the connection step 3 wrote into, present
// (missing for a blob the backup listed as damaged), verified when the restore streamed and hashed every blob (Q-BAK-72). No
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
		if err := qs.RestoreRecordConnection(ctx, q.RestoreRecordConnectionParams{ID: c.ID, Bucket: c.Bucket, Prefix: c.Prefix}); err != nil {
			return err
		}
		if c.ID == env.Sink && c.Prefix != "" {
			prefix = c.Prefix + "/"
		}
	}
	_, err = qs.RestoreRecordCopies(ctx, q.RestoreRecordCopiesParams{
		ConnectionID: env.Sink, KeyPrefix: prefix, Verified: env.SinkVerified, VerifiedAt: env.Now.UTC().Format(time.RFC3339Nano),
	})
	return err
}
