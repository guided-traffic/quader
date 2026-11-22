package fixup

import (
	"context"

	"github.com/guided-traffic/quader/internal/meta"
)

func init() {
	Register(Fixup{Name: "storage-copies", Order: OrderStorageUsage + 10, Apply: recordUnboundCopies})
}

// recordUnboundCopies records the copies of the blobs the restore loaded (storage.md R-STOR-052): the copy tables are
// transient and come back empty, and the restore wrote every blob into the first connection. Until the restore records its
// copies per configured connection (backup-and-restore.md R-BAK-030), they go on the connection default with an empty bucket
// and keys without the prefix, which the first replica that configures default binds to its element, as it does after the
// migration of the connections (storage.md Q-STOR-250).
func recordUnboundCopies(ctx context.Context, tx meta.Tx, _ Env) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	if err := qs.RestoreUnboundConnection(ctx); err != nil {
		return err
	}
	return qs.RestoreUnboundCopies(ctx)
}
