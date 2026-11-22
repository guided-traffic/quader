package keys

import (
	"context"
	"errors"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Row is a row of storage_keys as it lies: the sealed value, never an opened key.
type Row struct {
	KeyID      string
	Connection string
	Kind       string
	Generation int
	Sealed     string
	State      string
	CreatedAt  time.Time
	// RetiredAt is zero while the row is active.
	RetiredAt time.Time
}

// SessionKey is the sealed data key of an upload session: its id and the value of upload_sessions.sealed_data_key.
type SessionKey struct {
	ID     string
	Sealed string
}

// Rows is the package's access to storage_keys and to upload_sessions.sealed_data_key inside a transaction. The PostgreSQL
// implementation runs the statements of queries/storage_keys.sql; keystest.DB simulates the tables for the unit tests, and the
// integration tier runs the same code against PostgreSQL.
type Rows interface {
	// OfConnection reads the rows of one connection, by kind and generation.
	OfConnection(ctx context.Context, tx meta.Tx, connection string) ([]Row, error)
	// ForExport reads the rows of every connection, or of the one named when connection is not empty.
	ForExport(ctx context.Context, tx meta.Tx, connection string) ([]Row, error)
	// Insert inserts a row and reports false, inserting nothing, when a unique constraint holds one already: its identifier, the
	// connection's generation of its kind, or the connection's active key of its kind.
	Insert(ctx context.Context, tx meta.Tx, r Row) (inserted bool, err error)
	// LockKind locks and reads the rows of one kind of a connection, so that two writers of that kind run one after the other.
	LockKind(ctx context.Context, tx meta.Tx, connection, kind string) ([]Row, error)
	// Retire makes an active row retired and reports whether it did.
	Retire(ctx context.Context, tx meta.Tx, keyID string) (bool, error)
	// Activate makes a retired row active and reports whether it did.
	Activate(ctx context.Context, tx meta.Tx, keyID string) (bool, error)
	// SealedAfter reads, locked, at most batch rows whose identifier sorts after after, in that order: the re-encryption walk.
	SealedAfter(ctx context.Context, tx meta.Tx, after string, batch int) ([]Row, error)
	// UpdateSealed replaces a row's sealed value.
	UpdateSealed(ctx context.Context, tx meta.Tx, keyID, sealed string) error
	// SessionKeysAfter reads, locked, at most batch sessions with a sealed data key whose id sorts after after (the empty string
	// before every id).
	SessionKeysAfter(ctx context.Context, tx meta.Tx, after string, batch int) ([]SessionKey, error)
	// UpdateSessionKey replaces a session's sealed data key.
	UpdateSessionKey(ctx context.Context, tx meta.Tx, id, sealed string) error
}

// PG is the PostgreSQL implementation of Rows.
type PG struct{}

var _ Rows = PG{}

var errForeignTx = errors.New("storage keys: the transaction is not one of internal/meta/postgres")

func queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errForeignTx
	}
	return q.New(ptx), nil
}

func fromStorageKeys(list []q.StorageKey) []Row {
	out := make([]Row, 0, len(list))
	for _, r := range list {
		row := Row{
			KeyID: r.KeyID, Connection: r.ConnectionID, Kind: r.Kind, Generation: int(r.Generation), Sealed: r.SealedKey,
			State: r.State, CreatedAt: r.CreatedAt.Time.UTC(),
		}
		if r.RetiredAt.Valid {
			row.RetiredAt = r.RetiredAt.Time.UTC()
		}
		out = append(out, row)
	}
	return out
}

// OfConnection implements Rows.
func (PG) OfConnection(ctx context.Context, tx meta.Tx, connection string) ([]Row, error) {
	qs, err := queries(tx)
	if err != nil {
		return nil, err
	}
	list, err := qs.StorageKeysOfConnection(ctx, connection)
	return fromStorageKeys(list), err
}

// ForExport implements Rows.
func (PG) ForExport(ctx context.Context, tx meta.Tx, connection string) ([]Row, error) {
	qs, err := queries(tx)
	if err != nil {
		return nil, err
	}
	list, err := qs.StorageKeysForExport(ctx, connection)
	return fromStorageKeys(list), err
}

// Insert implements Rows.
func (PG) Insert(ctx context.Context, tx meta.Tx, r Row) (bool, error) {
	qs, err := queries(tx)
	if err != nil {
		return false, err
	}
	p := q.InsertStorageKeyParams{KeyID: r.KeyID, ConnectionID: r.Connection, Kind: r.Kind, Generation: int32(r.Generation), //nolint:gosec // G115: ParseID bounds the generation by MaxGeneration
		SealedKey: r.Sealed, State: r.State}
	if !r.CreatedAt.IsZero() {
		p.CreatedAt = r.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !r.RetiredAt.IsZero() {
		p.RetiredAt = r.RetiredAt.UTC().Format(time.RFC3339Nano)
	}
	n, err := qs.InsertStorageKey(ctx, p)
	return n == 1, err
}

// LockKind implements Rows.
func (PG) LockKind(ctx context.Context, tx meta.Tx, connection, kind string) ([]Row, error) {
	qs, err := queries(tx)
	if err != nil {
		return nil, err
	}
	list, err := qs.LockStorageKeysOfKind(ctx, q.LockStorageKeysOfKindParams{ConnectionID: connection, Kind: kind})
	out := make([]Row, 0, len(list))
	for _, r := range list {
		out = append(out, Row{KeyID: r.KeyID, Connection: connection, Kind: kind, Generation: int(r.Generation), State: r.State, Sealed: r.SealedKey})
	}
	return out, err
}

// Retire implements Rows.
func (PG) Retire(ctx context.Context, tx meta.Tx, keyID string) (bool, error) {
	qs, err := queries(tx)
	if err != nil {
		return false, err
	}
	n, err := qs.RetireStorageKey(ctx, keyID)
	return n == 1, err
}

// Activate implements Rows.
func (PG) Activate(ctx context.Context, tx meta.Tx, keyID string) (bool, error) {
	qs, err := queries(tx)
	if err != nil {
		return false, err
	}
	n, err := qs.ActivateStorageKey(ctx, keyID)
	return n == 1, err
}

// SealedAfter implements Rows.
func (PG) SealedAfter(ctx context.Context, tx meta.Tx, after string, batch int) ([]Row, error) {
	qs, err := queries(tx)
	if err != nil {
		return nil, err
	}
	list, err := qs.StorageKeysSealedAfter(ctx, q.StorageKeysSealedAfterParams{AfterID: after, Batch: int64(batch)})
	out := make([]Row, 0, len(list))
	for _, r := range list {
		out = append(out, Row{KeyID: r.KeyID, Sealed: r.SealedKey})
	}
	return out, err
}

// UpdateSealed implements Rows.
func (PG) UpdateSealed(ctx context.Context, tx meta.Tx, keyID, sealed string) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	return qs.UpdateStorageKeySealed(ctx, q.UpdateStorageKeySealedParams{KeyID: keyID, Sealed: sealed})
}

// minUUID sorts before every session id.
const minUUID = "00000000-0000-0000-0000-000000000000"

// SessionKeysAfter implements Rows. The walk starts at the nil uuid, which no session has (ids are random).
func (PG) SessionKeysAfter(ctx context.Context, tx meta.Tx, after string, batch int) ([]SessionKey, error) {
	qs, err := queries(tx)
	if err != nil {
		return nil, err
	}
	if after == "" {
		after = minUUID
	}
	list, err := qs.SessionDataKeysSealedAfter(ctx, q.SessionDataKeysSealedAfterParams{AfterID: after, Batch: int64(batch)})
	out := make([]SessionKey, 0, len(list))
	for _, r := range list {
		out = append(out, SessionKey{ID: r.ID, Sealed: r.SealedDataKey})
	}
	return out, err
}

// UpdateSessionKey implements Rows.
func (PG) UpdateSessionKey(ctx context.Context, tx meta.Tx, id, sealed string) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	return qs.UpdateSessionDataKeySealed(ctx, q.UpdateSessionDataKeySealedParams{ID: id, Sealed: sealed})
}
