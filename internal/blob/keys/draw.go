package keys

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/meta"
)

// Connection is what Ensure needs of an encrypted connection: its id, whether its objects get keyed names (R-STOR-075, R-STOR-086),
// whether it drains, and its location, which the refusal lists.
type Connection struct {
	ID         string
	KeyedNames bool
	// Drain is drain: true: a draining connection takes no write, so it draws nothing and holds what its rows have.
	Drain   bool
	Prefix  string
	Objects blob.ObjectStore
}

// Ensure makes sure the replica holds the keys of an encrypted connection (R-STOR-083): it draws the storage key of generation 1 and,
// with keyed names, the name key when the connection has no such row, and then loads the connection's rows.
//
// A key is drawn only over an empty location: when the location already holds an object below sha256/ or keyed/ (one listing of
// each, of one key) and the database has no row of the kind, those objects were written under keys of another database, and Ensure
// draws nothing and returns a Refusal that says what is missing and that the export's import is the way back. The listing comes
// first and the rows are read again in the transaction that would insert, so an object another replica wrote under the keys it drew
// meanwhile is never taken for one of another database: a replica commits its rows before it wraps anything under them.
//
// Two replicas that draw at once insert one key of each kind: the table holds one row per connection, kind and generation and one
// active row per connection and kind, the insert of the slower replica inserts nothing, and it reads the winner's rows in the same
// transaction.
//
// A connection of which storage_keys holds a destroyed row is refused, drawing and loading nothing, whether it drains or not: its id is
// retired for good (R-STOR-102). The restore Job draws keys in a path of its own and refuses such an id at its check 3 (internal/backup/
// restore, backup-and-restore.md R-BAK-031).
func (h *Holder) Ensure(ctx context.Context, c Connection) error {
	if err := h.draw(ctx, c); err != nil {
		return err
	}
	return h.Load(ctx, c.ID)
}

// missing names the kinds of key a connection lacks: a storage key without an active row, a name key without a row when its objects
// get keyed names.
func missing(rows []Row, keyedNames bool) []string {
	var storage, name bool
	for _, r := range rows {
		switch {
		case r.Kind == KindStorage && r.State == StateActive:
			storage = true
		case r.Kind == KindName:
			name = true
		}
	}
	var out []string
	if !storage {
		out = append(out, KindStorage)
	}
	if keyedNames && !name {
		out = append(out, KindName)
	}
	return out
}

func (h *Holder) draw(ctx context.Context, c Connection) error {
	var rows []Row
	if err := h.o.DB.InTx(ctx, func(tx meta.Tx) (err error) {
		rows, err = h.rows.OfConnection(ctx, tx, c.ID)
		return err
	}); err != nil {
		return err
	}
	if err := retiredForGood(c.ID, rows); err != nil {
		return err
	}
	if len(missing(rows, c.KeyedNames)) == 0 || c.Drain {
		return nil
	}
	used, err := LocationUsed(ctx, c.Objects, c.Prefix)
	if err != nil {
		return fmt.Errorf("connection %s: listing its location before its keys are drawn: %w", c.ID, err)
	}
	var drawn []string
	err = h.o.DB.InTx(ctx, func(tx meta.Tx) error {
		drawn = nil
		rows, err := h.rows.OfConnection(ctx, tx, c.ID)
		if err != nil {
			return err
		}
		if err := retiredForGood(c.ID, rows); err != nil {
			return err
		}
		lack := missing(rows, c.KeyedNames)
		switch {
		case len(lack) == 0:
			return nil // another replica drew them between the first read and this one
		case used:
			return usedRefusal(c.ID, lack)
		}
		for _, kind := range lack {
			id, err := h.drawOne(ctx, tx, c.ID, kind, nextGeneration(rows, kind))
			if err != nil {
				return err
			}
			if id != "" {
				drawn = append(drawn, id)
			}
		}
		after, err := h.rows.OfConnection(ctx, tx, c.ID)
		if err != nil {
			return err
		}
		if lack := missing(after, c.KeyedNames); len(lack) > 0 {
			return fmt.Errorf("connection %s: its %s key was neither drawn nor found; another writer holds its generation", c.ID, strings.Join(lack, " and "))
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, id := range drawn {
		h.o.Log.Info("a key of an encrypted connection was drawn and sealed with the key ring; export the storage keys again (quader admin storage keys export)",
			"connection", c.ID, "key_id", id)
	}
	return nil
}

// drawOne draws a key of kind for a connection, seals it under its new identifier and inserts it active. It returns the identifier, or
// "" when a row another replica inserted first holds the place.
func (h *Holder) drawOne(ctx context.Context, tx meta.Tx, connection, kind string, generation int) (string, error) {
	id, err := newID(kind, connection, generation, h.o.Random)
	if err != nil {
		return "", err
	}
	k, err := blobcrypt.NewKey(h.o.Random)
	if err != nil {
		return "", err
	}
	sealed, err := sealKey(h.o.Ring, id, k) // destroys k
	if err != nil {
		return "", err
	}
	inserted, err := h.rows.Insert(ctx, tx, Row{KeyID: id, Connection: connection, Kind: kind, Generation: generation, Sealed: sealed, State: StateActive})
	if err != nil || !inserted {
		return "", err
	}
	return id, nil
}

// nextGeneration is the generation a drawn key of kind takes: 1 for a name key, one above the highest for a storage key.
func nextGeneration(rows []Row, kind string) int {
	if kind == KindName {
		return 1
	}
	g := 0
	for _, r := range rows {
		if r.Kind == KindStorage && r.Generation > g {
			g = r.Generation
		}
	}
	return g + 1
}

// retiredForGood refuses a connection of which storage_keys holds a destroyed row (R-STOR-102, Q-STOR-411): its id is retired for good,
// so nothing is drawn or loaded for it, whatever its element says. It is the same sentence the start check gives.
func retiredForGood(connection string, rows []Row) error {
	var at time.Time
	found := false
	for _, r := range rows {
		if r.State == StateDestroyed {
			found = true
			if r.DestroyedAt.After(at) {
				at = r.DestroyedAt
			}
		}
	}
	if !found {
		return nil
	}
	return &Refusal{msg: blob.RetiredForGood(connection, at)}
}

// usedRefusal is the refusal over a location that holds objects (R-STOR-083).
func usedRefusal(connection string, lack []string) error {
	what := "storage keys"
	if len(lack) == 1 && lack[0] == KindName {
		what = "name key"
	}
	return &Refusal{msg: fmt.Sprintf("connection %s holds objects but the database has no %s for it; import the exported storage keys (quader admin storage keys import) or empty the location (storage.md R-STOR-083)", connection, what)}
}

// LocationUsed reports whether a connection's location holds an object below sha256/ or keyed/: one listing of each, of one key
// (R-STOR-083). The marker, the probes, the journal and the staging area of the sessions lie elsewhere and do not count.
func LocationUsed(ctx context.Context, objects blob.ObjectStore, prefix string) (bool, error) {
	if objects == nil {
		return false, errors.New("no object store to list")
	}
	for _, p := range []string{blob.KeyPrefix(prefix), blobcrypt.KeyedPrefix(prefix)} {
		page, err := objects.List(ctx, p, blob.ListOptions{Max: 1})
		if err != nil {
			return false, err
		}
		if len(page.Objects) > 0 {
			return true, nil
		}
	}
	return false, nil
}
