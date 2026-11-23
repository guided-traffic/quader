// Package keystest simulates the tables of internal/blob/keys for the unit tests of the package and of the commands that use it:
// storage_keys with its identifier rule, its state CHECKs and its three unique constraints, upload_sessions.sealed_data_key,
// storage_key_exports, and what the destruction reads besides the keys (Holders, set by the test). Transactions are serialized and roll
// back whole; the integration tier runs the same code against PostgreSQL, which is where two replicas that write at once are shown.
package keystest

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/meta"
)

// DB is the simulated database: a meta.Runner and a keys.Rows.
type DB struct {
	mu       sync.Mutex
	rows     map[string]keys.Row
	sessions map[string]string
	exports  []Export
	txs      int
	// Held is what Holders answers for every connection: the state of storage_connections, the replicas, the copy, queue and session
	// rows, the exports' counts and the backup runs. HeldOf, when it names the connection, answers instead. Exports and LastExport are
	// counted from the recorded exports when Held leaves them zero.
	Held   keys.Holders
	HeldOf map[string]keys.Holders
	// Writes counts the statements that changed a row of storage_keys or storage_key_exports.
	Writes int
	// BeforeInsert runs inside a transaction, before an insert is checked, with the database held: a test that calls Put there
	// simulates another writer whose row committed between a read and the insert.
	BeforeInsert func(r keys.Row)
	// FailTx makes the n-th transaction (from 1) fail with its error before it runs.
	FailTx map[int]error
	// Reads counts the statements that read the rows of one connection.
	Reads int
}

var (
	_ meta.Runner = (*DB)(nil)
	_ keys.Rows   = (*DB)(nil)
)

// Export is a row of storage_key_exports.
type Export struct {
	At       time.Time
	Instance string
	KeyIDs   []string
}

// New returns an empty database.
func New() *DB {
	return &DB{rows: map[string]keys.Row{}, sessions: map[string]string{}, FailTx: map[int]error{}, HeldOf: map[string]keys.Holders{}}
}

type tx struct{ meta.TxState }

// InTx runs fn as one transaction: the database is held for it, and an error leaves the tables as they were.
func (d *DB) InTx(ctx context.Context, fn func(meta.Tx) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.txs++
	if err := d.FailTx[d.txs]; err != nil {
		return err
	}
	rows, sessions, exports, writes := maps.Clone(d.rows), maps.Clone(d.sessions), slices.Clone(d.exports), d.Writes
	t := &tx{}
	err := fn(t)
	if err == nil {
		err = t.RunBefore(ctx, t)
	}
	if err != nil {
		d.rows, d.sessions, d.exports, d.Writes = rows, sessions, exports, writes
		t.RunRollback()
		return err
	}
	t.RunAfter()
	return nil
}

// Put writes a row as it is, without a check: a row another writer committed, or one a test damages.
func (d *DB) Put(r keys.Row) { d.rows[r.KeyID] = r }

// Rows returns every row, by identifier. Call it outside a transaction.
func (d *DB) Rows() []keys.Row {
	d.mu.Lock()
	defer d.mu.Unlock()
	return sorted(slices.Collect(maps.Values(d.rows)))
}

// Exports returns the rows of storage_key_exports. Call it outside a transaction.
func (d *DB) Exports() []Export {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.exports)
}

// SetSession writes a session's sealed data key. Call it outside a transaction.
func (d *DB) SetSession(id, sealed string) {
	d.mu.Lock()
	d.sessions[id] = sealed
	d.mu.Unlock()
}

// Session reads a session's sealed data key.
func (d *DB) Session(id string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sessions[id]
}

func sorted(list []keys.Row) []keys.Row {
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.Connection != b.Connection {
			return a.Connection < b.Connection
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Generation != b.Generation {
			return a.Generation < b.Generation
		}
		return a.KeyID < b.KeyID
	})
	return list
}

func (d *DB) of(connection string) []keys.Row {
	var out []keys.Row
	for _, r := range d.rows {
		if connection == "" || r.Connection == connection {
			out = append(out, r)
		}
	}
	return sorted(out)
}

// OfConnection implements keys.Rows.
func (d *DB) OfConnection(_ context.Context, _ meta.Tx, connection string) ([]keys.Row, error) {
	d.Reads++
	return d.of(connection), nil
}

// ForExport implements keys.Rows.
func (d *DB) ForExport(_ context.Context, _ meta.Tx, connection string) ([]keys.Row, error) {
	return d.of(connection), nil
}

// errCheck is the refusal of a row the table's CHECK constraints refuse.
var errCheck = errors.New("keystest: the row violates a check constraint of storage_keys")

// Insert implements keys.Rows with the table's constraints: the identifier rule (Q-STOR-324), and the primary key, the generation of a
// connection's kind and its active key, each of which makes the insert insert nothing.
func (d *DB) Insert(_ context.Context, _ meta.Tx, r keys.Row) (bool, error) {
	if d.BeforeInsert != nil {
		d.BeforeInsert(r)
	}
	id, ok := keys.ParseID(r.KeyID)
	if !ok || id.Kind != r.Kind || id.Connection != r.Connection || id.Generation != r.Generation || !checked(r) {
		return false, errCheck
	}
	for _, o := range d.rows {
		same := o.Connection == r.Connection && o.Kind == r.Kind
		if o.KeyID == r.KeyID || same && o.Generation == r.Generation || same && o.State == keys.StateActive && r.State == keys.StateActive {
			return false, nil
		}
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	d.rows[r.KeyID] = r
	d.Writes++
	return true, nil
}

// checked holds a row to the state CHECKs of storage_keys (00107, 00112): a state of the three, retired_at set unless active, and a
// destroyed row without its sealed value and with destroyed_at, every other row with its sealed value and without.
func checked(r keys.Row) bool {
	destroyed := r.State == keys.StateDestroyed
	return (r.State == keys.StateActive || r.State == keys.StateRetired || destroyed) &&
		(r.State == keys.StateActive) == r.RetiredAt.IsZero() &&
		destroyed == (r.Sealed == "") && destroyed == !r.DestroyedAt.IsZero()
}

// LockKind implements keys.Rows; the transaction holds the database already.
func (d *DB) LockKind(_ context.Context, _ meta.Tx, connection, kind string) ([]keys.Row, error) {
	var out []keys.Row
	for _, r := range d.of(connection) {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out, nil
}

// Retire implements keys.Rows.
func (d *DB) Retire(_ context.Context, _ meta.Tx, keyID string) (bool, error) {
	r, ok := d.rows[keyID]
	if !ok || r.State != keys.StateActive {
		return false, nil
	}
	r.State, r.RetiredAt = keys.StateRetired, time.Now().UTC()
	d.rows[keyID] = r
	d.Writes++
	return true, nil
}

// Activate implements keys.Rows, with the rule of one active key per connection and kind.
func (d *DB) Activate(_ context.Context, _ meta.Tx, keyID string) (bool, error) {
	r, ok := d.rows[keyID]
	if !ok || r.State != keys.StateRetired {
		return false, nil
	}
	for _, o := range d.rows {
		if o.Connection == r.Connection && o.Kind == r.Kind && o.State == keys.StateActive {
			return false, errors.New("keystest: a second active key violates storage_keys_active")
		}
	}
	r.State, r.RetiredAt = keys.StateActive, time.Time{}
	d.rows[keyID] = r
	d.Writes++
	return true, nil
}

// SealedAfter implements keys.Rows.
func (d *DB) SealedAfter(_ context.Context, _ meta.Tx, after string, batch int) ([]keys.Row, error) {
	ids := slices.Sorted(maps.Keys(d.rows))
	var out []keys.Row
	for _, id := range ids {
		if id > after && len(out) < batch && d.rows[id].Sealed != "" {
			out = append(out, d.rows[id])
		}
	}
	return out, nil
}

// UpdateSealed implements keys.Rows.
func (d *DB) UpdateSealed(_ context.Context, _ meta.Tx, keyID, sealed string) error {
	if r, ok := d.rows[keyID]; ok && r.State != keys.StateDestroyed {
		r.Sealed = sealed
		d.rows[keyID] = r
		d.Writes++
	}
	return nil
}

// SessionKeysAfter implements keys.Rows.
func (d *DB) SessionKeysAfter(_ context.Context, _ meta.Tx, after string, batch int) ([]keys.SessionKey, error) {
	ids := slices.Sorted(maps.Keys(d.sessions))
	var out []keys.SessionKey
	for _, id := range ids {
		if id > after && len(out) < batch && d.sessions[id] != "" {
			out = append(out, keys.SessionKey{ID: id, Sealed: d.sessions[id]})
		}
	}
	return out, nil
}

// UpdateSessionKey implements keys.Rows.
func (d *DB) UpdateSessionKey(_ context.Context, _ meta.Tx, id, sealed string) error {
	if d.sessions[id] != "" {
		d.sessions[id] = sealed
	}
	return nil
}

// LockConnection implements keys.Rows; the transaction holds the database already.
func (d *DB) LockConnection(_ context.Context, _ meta.Tx, connection string) ([]keys.Row, error) {
	return d.of(connection), nil
}

// Destroy implements keys.Rows: the UPDATE of R-STOR-099 step 4.
func (d *DB) Destroy(_ context.Context, _ meta.Tx, connection string) ([]string, error) {
	var out []string
	now := time.Now().UTC()
	for _, r := range d.of(connection) {
		if r.State == keys.StateDestroyed {
			continue
		}
		d.rows[r.KeyID] = destroyed(r, now)
		out = append(out, r.KeyID)
		d.Writes++
	}
	return out, nil
}

// DestroyKey implements keys.Rows.
func (d *DB) DestroyKey(_ context.Context, _ meta.Tx, keyID string, at time.Time) (bool, error) {
	r, ok := d.rows[keyID]
	if !ok || r.State == keys.StateDestroyed {
		return false, nil
	}
	d.rows[keyID] = destroyed(r, at.UTC())
	d.Writes++
	return true, nil
}

func destroyed(r keys.Row, at time.Time) keys.Row {
	r.State, r.Sealed, r.DestroyedAt = keys.StateDestroyed, "", at
	if r.RetiredAt.IsZero() {
		r.RetiredAt = at
	}
	return r
}

// RecordExport implements keys.Rows.
func (d *DB) RecordExport(_ context.Context, _ meta.Tx, instance string, ids []string) error {
	d.exports = append(d.exports, Export{At: time.Now().UTC(), Instance: instance, KeyIDs: slices.Clone(ids)})
	d.Writes++
	return nil
}

// Holders implements keys.Rows with Held or HeldOf; the exports are counted from the recorded ones when the answer leaves them zero.
func (d *DB) Holders(_ context.Context, _ meta.Tx, connection string, ids []string, _ time.Duration) (keys.Holders, error) {
	h, ok := d.HeldOf[connection]
	if !ok {
		h = d.Held
	}
	if h.Exports == 0 {
		for _, e := range d.exports {
			if slices.ContainsFunc(e.KeyIDs, func(id string) bool { return slices.Contains(ids, id) }) {
				h.Exports++
				h.LastExport = e.At
			}
		}
	}
	return h, nil
}
