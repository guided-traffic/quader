package blob

import (
	"context"
	"errors"
	"fmt"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/meta"
)

// StorageKeys is what the store asks of the replica's storage keys (internal/blob/keys, storage.md R-STOR-083): the lookup of the
// storage key a record of an object on one connection names, which reads the connection's rows again once for an identifier the
// replica does not hold.
type StorageKeys interface {
	Lookup(ctx context.Context, connection string) blobcrypt.KeyLookup
}

// WithStorageKeys gives the store the replica's storage keys, which the composition root fills after the start checks.
func WithStorageKeys(k StorageKeys) Option { return func(s *Store) { s.keys = k } }

// ErrNotEncrypted is CopyDataKey for a connection that is not encrypted: its objects have no data key.
var ErrNotEncrypted = errors.New("the connection is not encrypted")

// ErrNoPresentCopy is CopyDataKey for a blob that has no present copy on the connection, or is not live.
var ErrNoPresentCopy = errors.New("the blob has no present copy on the connection")

// CopyDataKey opens the record of the present copy of a blob on an encrypted connection and returns the copy's data key with its
// plaintext size and the identifier of the storage key that wrapped it (R-STOR-083, Q-STOR-320): one HeadObject, then steps 1 to 6 of
// R-STOR-077 against the copy row and the blob row, through the replica's storage keys. A refusal of a step is the blobcrypt.Error of
// its reason and marks nothing here. The caller destroys the key once its bytes are where they go.
//
// It is the room for the handover of one released blob to a Bastion (R-STOR-095, R-STOR-096): an architecture rule
// (internal/archtest, R-STOR-096) allows calls from this package and from internal/downloadlinks only.
func (s *Store) CopyDataKey(ctx context.Context, d Digest, connectionID string) (blobcrypt.DataKey, error) {
	c, ok := s.conns.Get(connectionID)
	switch {
	case !ok:
		return blobcrypt.DataKey{}, fmt.Errorf("blob: this replica does not configure connection %s", connectionID)
	case !c.Encrypted():
		return blobcrypt.DataKey{}, fmt.Errorf("%w: %s", ErrNotEncrypted, connectionID)
	case s.keys == nil:
		return blobcrypt.DataKey{}, fmt.Errorf("blob: this replica holds no storage keys of connection %s", connectionID)
	}
	var cp Copy
	var size int64
	found := false
	err := s.db.InTx(ctx, func(tx meta.Tx) error {
		found = false
		row, ok, err := s.rows.Row(ctx, tx, d)
		if err != nil || !ok || row.State != "live" {
			return err
		}
		copies, err := s.rows.Copies(ctx, tx, d)
		if err != nil {
			return err
		}
		for _, x := range copies {
			if x.Connection == connectionID && x.State == CopyPresent && !x.Retired {
				cp, size, found = x, row.Size, true
			}
		}
		return nil
	})
	if err != nil {
		return blobcrypt.DataKey{}, err
	}
	if !found {
		return blobcrypt.DataKey{}, fmt.Errorf("%w: %s on %s", ErrNoPresentCopy, d.Hex(), connectionID)
	}
	info, err := c.Objects().Head(ctx, cp.ObjectKey)
	if err != nil {
		return blobcrypt.DataKey{}, err
	}
	return blobcrypt.Open(info.Metadata, s.keys.Lookup(ctx, connectionID), blobcrypt.Want{
		Known: true, Digest: d, Size: size, StoredSize: cp.StoredSize, ObjectSize: info.Size,
	})
}
