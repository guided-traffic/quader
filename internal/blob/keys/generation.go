package keys

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/meta"
)

// Generation is what a new generation did (R-STOR-083): the connection, the new active storage key and the one it retired.
type Generation struct {
	Connection   string `json:"connection"`
	KeyID        string `json:"keyId"`
	Generation   int    `json:"generation"`
	RetiredKeyID string `json:"retiredKeyId"`
}

// GenerationFunc runs in the transaction of a new generation, after its rows were written: the audit event
// system.storage.key_generation_added and the change of the area storage_keys hang here. An error rolls the generation back.
type GenerationFunc func(ctx context.Context, tx meta.Tx, g Generation) error

// ErrNoStorageKey is the refusal of a new generation for a connection without an active storage key.
var ErrNoStorageKey = errors.New("the connection has no active storage key")

// NewGeneration draws generation n + 1 of a connection's storage key and makes it active, and retires generation n, in one
// transaction that locks the connection's storage keys, so two such commands on one connection run one after the other
// (`quader admin storage keys new-generation`, R-STOR-083). The ring must open the generation it retires, so the new key is sealed
// under a ring the installation uses. New objects are wrapped under it from the moment a replica learns it;
// older generations keep opening what they wrapped. A connection that has no active storage key (it is not encrypted, or no replica
// has started with it) is refused with ErrNoStorageKey. random is the system's random source; nil is crypto/rand.Reader.
func NewGeneration(ctx context.Context, db meta.Runner, rows Rows, ring Sealer, random io.Reader, connection string, then GenerationFunc) (Generation, error) {
	if rows == nil {
		rows = PG{}
	}
	if random == nil {
		random = rand.Reader
	}
	var g Generation
	err := db.InTx(ctx, func(tx meta.Tx) error {
		g = Generation{Connection: connection}
		list, err := rows.LockKind(ctx, tx, connection, KindStorage)
		if err != nil {
			return err
		}
		if err := retiredForGood(connection, list); err != nil {
			return err
		}
		highest := 0
		var active Row
		for _, r := range list {
			if r.State == StateActive {
				active = r
			}
			highest = max(highest, r.Generation)
		}
		if active.KeyID == "" {
			return fmt.Errorf("%w: connection %s; it is not an encrypted connection, or no replica has started with it yet", ErrNoStorageKey, connection)
		}
		// The ring must open the generation it retires: a new key sealed under a ring the installation does not use would open on no
		// replica.
		k, err := openKey(ring, active.KeyID, active.Sealed)
		if err != nil {
			return err
		}
		k.Key.Destroy()
		g.RetiredKeyID = active.KeyID
		if highest >= MaxGeneration {
			return &Refusal{msg: fmt.Sprintf("connection %s has used all %d generations of its storage key; replace the connection", connection, MaxGeneration)}
		}
		g.Generation = highest + 1
		if g.KeyID, err = newID(KindStorage, connection, g.Generation, random); err != nil {
			return err
		}
		fresh, err := blobcrypt.NewKey(random)
		if err != nil {
			return err
		}
		sealed, err := sealKey(ring, g.KeyID, fresh) // destroys fresh
		if err != nil {
			return err
		}
		// retire first: the table holds one active storage key per connection
		if ok, err := rows.Retire(ctx, tx, g.RetiredKeyID); err != nil || !ok {
			return errors.Join(err, fmt.Errorf("connection %s: its active storage key %s changed while it was retired", connection, g.RetiredKeyID))
		}
		inserted, err := rows.Insert(ctx, tx, Row{KeyID: g.KeyID, Connection: connection, Kind: KindStorage, Generation: g.Generation, Sealed: sealed, State: StateActive})
		if err != nil {
			return err
		}
		if !inserted {
			return fmt.Errorf("connection %s: generation %d of its storage key exists already", connection, g.Generation)
		}
		if then != nil {
			return then(ctx, tx, g)
		}
		return nil
	})
	return g, err
}
