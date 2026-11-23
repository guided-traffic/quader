package blob

import (
	"context"
	"errors"
	"fmt"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// StorageKeys is what the store asks of the replica's storage keys (internal/blob/keys, storage.md R-STOR-083): the active storage key a
// write wraps its data key under (R-STOR-080), the name key of a connection with keyed names (R-STOR-086), and the lookup of the
// storage key a record of an object on one connection names, which reads the connection's rows again once for an identifier the
// replica does not hold (R-STOR-082). *keys.Holder implements it. The store never destroys a key it gets here: the holder owns them.
type StorageKeys interface {
	Active(ctx context.Context, connection string) (*blobcrypt.StorageKey, error)
	NameKey(connection string) (blobcrypt.Key, error)
	Lookup(ctx context.Context, connection string) blobcrypt.KeyLookup
}

// WithStorageKeys gives the store the replica's storage keys, which the composition root fills after the start checks. The store
// hands them to its encrypted connections, whose readers and naming function need them.
func WithStorageKeys(k StorageKeys) Option { return func(s *Store) { s.keys = k } }

// ErrNotEncrypted is CopyDataKey for a connection that is not encrypted: its objects have no data key.
var ErrNotEncrypted = errors.New("the connection is not encrypted")

// ErrNoPresentCopy is CopyDataKey for a copy that is not present, or lies on a retired connection.
var ErrNoPresentCopy = errors.New("the copy is not present")

// CopyDataKey opens the record of one copy of the blob d (of size bytes, the blob row's) on an encrypted connection and returns the
// copy's data key with its plaintext size and the identifier of the storage key that wrapped it (R-STOR-083, Q-STOR-320): one
// HeadObject of the copy's object, then steps 1 to 6 of R-STOR-077 against that copy row and the blob's digest and size, through the
// replica's storage keys. It reads no row: the copy and the size are the ones the caller's own lookup read for the request, as a
// download's reader takes them (Connection.ReadBlob), so the handover of an encrypted copy costs no statement here
// (storage.md Q-STOR-470). The key is the key of that object's own record: each encrypted copy has a data key of its own
// (R-STOR-087), and the record binds it to the digest and the sizes. A refusal of a step is the blobcrypt.Error of its reason
// (unknown_key for a key the replica does not hold) and marks nothing here. The caller destroys the key once its bytes are where
// they go.
//
// It is the room for the handover of one released blob to a Bastion (R-STOR-095, R-STOR-096): an architecture rule
// (internal/archtest, R-STOR-096) allows calls from this package and from internal/downloadlinks only.
func (s *Store) CopyDataKey(ctx context.Context, d Digest, size int64, cp Copy) (blobcrypt.DataKey, error) {
	c, ok := s.conns.Get(cp.Connection)
	switch {
	case !ok:
		return blobcrypt.DataKey{}, fmt.Errorf("blob: this replica does not configure connection %s", cp.Connection)
	case !c.Encrypted():
		return blobcrypt.DataKey{}, fmt.Errorf("%w: %s", ErrNotEncrypted, cp.Connection)
	case s.keys == nil:
		return blobcrypt.DataKey{}, fmt.Errorf("blob: this replica holds no storage keys of connection %s", cp.Connection)
	case cp.State != CopyPresent || cp.Retired:
		return blobcrypt.DataKey{}, fmt.Errorf("%w: %s on %s is %s", ErrNoPresentCopy, d.Hex(), cp.Connection, cp.State)
	}
	info, err := c.Objects().Head(ctx, cp.ObjectKey)
	if err != nil {
		return blobcrypt.DataKey{}, err
	}
	return blobcrypt.Open(info.Metadata, s.keys.Lookup(ctx, cp.Connection), blobcrypt.Want{
		Known: true, Digest: d, Size: size, StoredSize: cp.StoredSize, ObjectSize: info.Size,
	})
}
