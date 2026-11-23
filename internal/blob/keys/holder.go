package keys

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/meta"
)

// DefaultRereadEvery is how often a replica reads a connection's rows again for an identifier it does not hold (R-STOR-083).
const DefaultRereadEvery = 10 * time.Second

// ChangeArea is the name of the change area the holder follows, cluster.AreaStorageKeys, which internal/cluster registers. It is
// spelled here because nothing below internal/blob may depend on internal/cluster (R-STOR-046); a test holds the two equal.
const ChangeArea = "storage_keys"

// Options configure a Holder.
type Options struct {
	// DB runs the transactions; *postgres.DB is one.
	DB meta.Runner
	// Rows is the access to the tables; nil is PostgreSQL.
	Rows Rows
	// Ring seals and opens the keys: the replica's key ring.
	Ring Sealer
	// Random is the source keys and identifiers are drawn from; nil is crypto/rand.Reader, the system's random source.
	Random io.Reader
	// Now is the clock of the re-read's bound; nil is time.Now.
	Now func() time.Time
	// RereadEvery bounds the re-read for an unknown identifier per connection; zero is DefaultRereadEvery.
	RereadEvery time.Duration
	// Log receives what the holder drew and what it could not read again; nil discards it.
	Log *slog.Logger
}

// Holder is the storage keys and name keys of one replica's encrypted connections, by identifier (R-STOR-083): opened from their
// rows at start and kept for the replica's life. A new generation reaches it through the change area storage_keys (ApplyChange marks
// the connection, and the next Active reads its rows again) or through a record that names an identifier it does not hold (Lookup
// reads the rows again, at most once per RereadEvery per connection). Rows are never deleted, and a held key is dropped only when its
// connection's keys are destroyed (R-STOR-101): a change that says so destroys every key the holder holds of the connection at once,
// and a load drops a held key whose row is destroyed and never opens one; a read in flight that holds such a key then fails with
// blobcrypt's refusal of a destroyed key. A record that names a destroyed identifier answers destroyed_key. Close destroys every key. A
// Holder is safe for concurrent use.
type Holder struct {
	o    Options
	rows Rows

	mu     sync.Mutex
	conns  map[string]*held
	closed bool
}

// held is what the replica holds of one connection.
type held struct {
	// reread serializes the reads of the rows for this connection: a second reader for an unknown identifier waits for the first
	// and then finds the key or is inside the bound.
	reread sync.Mutex
	// The rest is guarded by Holder.mu.
	storage map[string]*blobcrypt.StorageKey // every generation, by identifier
	active  *blobcrypt.StorageKey
	name    *blobcrypt.StorageKey
	// destroyed are the identifiers of the connection's destroyed rows with the time of their destruction, as the last load read
	// them (R-STOR-101).
	destroyed map[string]time.Time
	stale     bool
	readAt    time.Time
}

// New returns an empty holder; Ensure and Load fill it.
func New(o Options) (*Holder, error) {
	if o.DB == nil || o.Ring == nil {
		return nil, errors.New("storage keys: the holder needs the database and the key ring")
	}
	if o.Rows == nil {
		o.Rows = PG{}
	}
	if o.Random == nil {
		o.Random = rand.Reader
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.RereadEvery <= 0 {
		o.RereadEvery = DefaultRereadEvery
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return &Holder{o: o, rows: o.Rows, conns: map[string]*held{}}, nil
}

// entry returns the holder's entry of a connection, making it.
func (h *Holder) entry(connection string) *held {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.conns[connection]
	if e == nil {
		e = &held{storage: map[string]*blobcrypt.StorageKey{}}
		h.conns[connection] = e
	}
	return e
}

// Load reads the rows of a connection, opens those the replica does not hold yet with its key ring and makes the active storage
// key the row's that says so. A destroyed row is never opened: its identifier is recorded, and a held key of it is destroyed and
// dropped (R-STOR-101). A key the ring cannot open is a Refusal that names the identifier and the root key.
func (h *Holder) Load(ctx context.Context, connection string) error {
	e := h.entry(connection)
	e.reread.Lock()
	defer e.reread.Unlock()
	return h.load(ctx, connection, e)
}

// load runs Load with e.reread held.
func (h *Holder) load(ctx context.Context, connection string, e *held) error {
	var rows []Row
	if err := h.o.DB.InTx(ctx, func(tx meta.Tx) (err error) {
		rows, err = h.rows.OfConnection(ctx, tx, connection)
		return err
	}); err != nil {
		return err
	}
	readAt := h.o.Now()
	opened := map[string]*blobcrypt.StorageKey{}
	for _, r := range rows {
		if r.State == StateDestroyed || h.holds(e, r.KeyID) {
			continue
		}
		if id, ok := ParseID(r.KeyID); !ok || id.Connection != connection || id.Kind != r.Kind || id.Generation != r.Generation {
			destroyAll(opened)
			return &Refusal{msg: fmt.Sprintf("the row %s of storage_keys does not name connection %s, kind %s and generation %d (Q-STOR-324)", r.KeyID, connection, r.Kind, r.Generation)}
		}
		k, err := openKey(h.o.Ring, r.KeyID, r.Sealed)
		if err != nil {
			destroyAll(opened)
			return err
		}
		opened[r.KeyID] = k
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		destroyAll(opened)
		return errors.New("storage keys: the holder is closed")
	}
	var active *blobcrypt.StorageKey
	destroyed := map[string]time.Time{}
	for _, r := range rows {
		if r.State == StateDestroyed {
			destroyed[r.KeyID] = r.DestroyedAt
			e.drop(r.KeyID)
			continue
		}
		k := opened[r.KeyID]
		switch {
		case r.Kind == KindName && k != nil:
			if e.name == nil {
				e.name = k
			} else {
				k.Key.Destroy() // a second name key the table cannot hold; the held one stays
			}
		case k != nil:
			e.storage[r.KeyID] = k
		}
		if r.Kind == KindStorage && r.State == StateActive {
			active = e.storage[r.KeyID]
		}
	}
	e.active, e.destroyed, e.stale, e.readAt = active, destroyed, false, readAt
	return nil
}

// drop destroys and forgets the key of an identifier the entry holds; the caller holds Holder.mu.
func (e *held) drop(id string) {
	if k := e.storage[id]; k != nil {
		k.Key.Destroy()
		delete(e.storage, id)
		if e.active == k {
			e.active = nil
		}
	}
	if e.name != nil && e.name.ID == id {
		e.name.Key.Destroy()
		e.name = nil
	}
}

// forget destroys and forgets every key the entry holds; the caller holds Holder.mu.
func (e *held) forget() {
	destroyAll(e.storage)
	if e.name != nil {
		e.name.Key.Destroy()
	}
	e.storage, e.active, e.name = map[string]*blobcrypt.StorageKey{}, nil, nil
}

// holds reports whether the entry holds the key of an identifier.
func (h *Holder) holds(e *held, id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return e.storage[id] != nil || (e.name != nil && e.name.ID == id)
}

func destroyAll(m map[string]*blobcrypt.StorageKey) {
	for _, k := range m {
		k.Key.Destroy()
	}
}

// ErrNoKey is the answer for a connection whose active storage key or name key the replica does not hold.
var ErrNoKey = errors.New("this replica holds no such key of the connection")

// Active returns the storage key that wraps new objects on a connection (R-STOR-080). After a change of the area storage_keys it reads
// the connection's rows first; a failed read keeps the key the replica holds, which still wraps validly, and the next call tries
// again.
func (h *Holder) Active(ctx context.Context, connection string) (*blobcrypt.StorageKey, error) {
	e := h.entry(connection)
	h.mu.Lock()
	stale := e.stale
	h.mu.Unlock()
	if stale {
		if err := h.Load(ctx, connection); err != nil {
			h.o.Log.Warn("the storage keys of a connection could not be read again after a change; the held active key wraps on", "connection", connection, "error", err)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if e.active == nil {
		return nil, fmt.Errorf("%w: connection %s, its active storage key", ErrNoKey, connection)
	}
	return e.active, nil
}

// NameKey returns the name key of a connection with keyed names (R-STOR-086).
func (h *Holder) NameKey(connection string) (blobcrypt.Key, error) {
	e := h.entry(connection)
	h.mu.Lock()
	defer h.mu.Unlock()
	if e.name == nil {
		return blobcrypt.Key{}, fmt.Errorf("%w: connection %s, its name key", ErrNoKey, connection)
	}
	return e.name.Key, nil
}

// Lookup returns the lookup of the storage key a record of an object on the connection names (step 2 of R-STOR-077): a key the
// replica holds at once; for any other identifier the connection's rows are read again, at most once per RereadEvery, and the key is
// returned if they hold it; after a change of the area storage_keys the rows are read at once. nil without an error is an identifier
// the connection does not hold (unknown_key); an identifier whose row is destroyed is an error that wraps blobcrypt.ErrDestroyedKey
// (destroyed_key, R-STOR-101), which marks the copy; a failed read is returned as it is and marks nothing.
func (h *Holder) Lookup(ctx context.Context, connection string) blobcrypt.KeyLookup {
	return func(id string) (*blobcrypt.StorageKey, error) {
		e := h.entry(connection)
		h.refresh(ctx, connection, e)
		if k, err := h.storageKey(e, connection, id); k != nil || err != nil {
			return k, err
		}
		e.reread.Lock()
		defer e.reread.Unlock()
		if k, err := h.storageKey(e, connection, id); k != nil || err != nil {
			return k, err
		}
		h.mu.Lock()
		recent := !e.stale && !e.readAt.IsZero() && h.o.Now().Sub(e.readAt) < h.o.RereadEvery
		h.mu.Unlock()
		if recent {
			return nil, nil
		}
		if err := h.load(ctx, connection, e); err != nil {
			return nil, err
		}
		return h.storageKey(e, connection, id)
	}
}

// refresh reads a connection's rows again once after a change of the area storage_keys or a flush marked it, before a held key is
// handed out: a replica that missed a destruction's change catches up at its next poll or flush (R-STOR-101). A failed read keeps
// the keys the replica holds, as Active does, and the next lookup tries again.
func (h *Holder) refresh(ctx context.Context, connection string, e *held) {
	h.mu.Lock()
	stale := e.stale
	h.mu.Unlock()
	if !stale {
		return
	}
	e.reread.Lock()
	defer e.reread.Unlock()
	h.mu.Lock()
	stale = e.stale
	h.mu.Unlock()
	if !stale {
		return
	}
	if err := h.load(ctx, connection, e); err != nil {
		h.o.Log.Warn("the storage keys of a connection could not be read again after a change; the held keys open on", "connection", connection, "error", err)
	}
}

// storageKey is the held key of an identifier, the refusal of a destroyed one, or neither.
func (h *Holder) storageKey(e *held, connection, id string) (*blobcrypt.StorageKey, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if k := e.storage[id]; k != nil {
		return k, nil
	}
	if at, gone := e.destroyed[id]; gone {
		return nil, fmt.Errorf("%w: the storage key %s of connection %s was destroyed on %s (storage.md R-STOR-101)",
			blobcrypt.ErrDestroyedKey, id, connection, at.UTC().Format(time.DateOnly))
	}
	return nil, nil
}

// Held lists the identifiers of the keys the replica holds for a connection: the storage keys of every generation and the name key.
func (h *Holder) Held(connection string) (storage []string, active, name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.conns[connection]
	if e == nil {
		return nil, "", ""
	}
	for id := range e.storage {
		storage = append(storage, id)
	}
	if e.active != nil {
		active = e.active.ID
	}
	if e.name != nil {
		name = e.name.ID
	}
	return storage, active, name
}

// ApplyChange implements cluster.Listener for the change area storage_keys: the connection the payload names is read again before
// its next wrap. A payload of version 2 that carries destroyed says that the connection's keys were destroyed (R-STOR-101,
// Q-STOR-407): the holder destroys and drops every key it holds of the connection at once, whatever identifiers the list names (a
// destruction is of a whole connection, and the list is cut to the change log's bound, Q-STOR-462), and the next use reads the rows,
// which say destroyed. Other areas, and a connection the replica does not hold, change nothing.
func (h *Holder) ApplyChange(area string, _ int, payload json.RawMessage) {
	if area != ChangeArea {
		return
	}
	var p destroyedChange
	if json.Unmarshal(payload, &p) != nil || p.Connection == "" {
		h.FlushArea(area, "unknown_payload")
		return
	}
	h.mu.Lock()
	e := h.conns[p.Connection]
	forgot := e != nil && p.Destroyed != nil
	if e != nil {
		e.stale = true
		if forgot {
			e.forget()
		}
	}
	h.mu.Unlock()
	if forgot {
		h.o.Log.Info("the storage keys of a connection were destroyed; this replica destroyed the keys it held of it",
			"connection", p.Connection, "key_ids", p.Destroyed)
	}
}

// FlushArea implements cluster.Listener: every connection is read again before its next wrap.
func (h *Holder) FlushArea(area, _ string) {
	if area != ChangeArea {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.conns {
		e.stale = true
	}
}

// Close destroys every key the holder holds; a read that still holds one fails with blobcrypt's refusal of a destroyed key. The
// composition root calls it when the replica stops.
func (h *Holder) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for _, e := range h.conns {
		e.forget()
	}
}
