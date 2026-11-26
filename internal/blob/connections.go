package blob

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/config"
)

// Connection is one object-store connection of a replica (R-STOR-050): one bucket and prefix on one endpoint, reached through
// its own client and its own circuit breaker (R-STOR-060). Its id names it for its whole life, in the copy rows, the label
// connection and the API. A Connection is safe for concurrent use.
type Connection struct {
	cfg      config.StorageConnection // with its defaults and its files read
	objects  ObjectStore
	position int

	mu   sync.RWMutex
	caps Capabilities
	link LinkCheck
	// keys are the replica's storage keys and metrics the store's families, which the store gives its connections (bind): the
	// reader and the naming function of an encrypted connection need the keys (R-STOR-082, R-STOR-086).
	keys    StorageKeys
	metrics *Metrics
}

// NewConnection builds the connection of one element. cfg should be resolved (config.ResolveConnections); its defaults are
// applied here as well. objects is the connection's client, usually its [Guard]; on an encrypted connection it is wrapped so that a
// write of a blob's object without the record of the stored format is refused before it is sent (sealedOnly, R-STOR-075).
func NewConnection(cfg config.StorageConnection, objects ObjectStore) *Connection {
	cfg = cfg.WithDefaults()
	if cfg.Encrypted {
		objects = sealOnly(objects, cfg.KeyPrefix())
	}
	return &Connection{cfg: cfg, objects: objects}
}

// bind gives the connection the replica's storage keys and the store's metrics (New). A store without keys, or without metrics,
// leaves what an earlier store gave.
func (c *Connection) bind(keys StorageKeys, m *Metrics) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if keys != nil {
		c.keys = keys
	}
	if m != nil {
		c.metrics = m
	}
}

// sealing is the binding of bind.
func (c *Connection) sealing() (StorageKeys, *Metrics) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.keys, c.metrics
}

// ID is the connection's id.
func (c *Connection) ID() string { return c.cfg.ID }

// Position is the connection's place in this replica's list, 1 for the first: its write and read order (Q-STOR-221).
func (c *Connection) Position() int { return c.position }

// Element names the connection's element in an error as the validation of the configuration names it:
// "storage.connections[0] (default):", the index counted from 0 like the list's.
func (c *Connection) Element() string {
	return fmt.Sprintf("storage.connections[%d] (%s):", max(c.position-1, 0), c.ID())
}

// Config is the connection's element with its defaults and its files read.
func (c *Connection) Config() config.StorageConnection { return c.cfg }

// Prefix is the connection's key prefix.
func (c *Connection) Prefix() string { return c.cfg.KeyPrefix() }

// Bucket is the connection's bucket.
func (c *Connection) Bucket() string { return c.cfg.Bucket }

// Drain reports whether the connection is being taken out (drain: true): no upload goes to it (R-STOR-051).
func (c *Connection) Drain() bool { return c.cfg.Drain }

// Encrypted reports the connection's fixed property encrypted (R-STOR-075): every blob object it holds is in the stored format of
// R-STOR-076 with its record, written and read through the connection's encrypting writer and decrypting reader.
func (c *Connection) Encrypted() bool { return c.cfg.Encrypted }

// KeyedNames reports whether the connection's objects get keyed names (R-STOR-086): an encrypted connection with keyed_names true,
// its default.
func (c *Connection) KeyedNames() bool { return c.cfg.UsesKeyedNames() }

// Objects is the connection's client; on an encrypted connection it refuses a write of a blob's object without a record.
func (c *Connection) Objects() ObjectStore { return c.objects }

// ObjectKey is the connection's naming function: the name of a blob's object on this connection when it is written
// (R-STOR-062). A plain connection, and an encrypted one with keyed_names: false, name an object by its digest under the
// connection's prefix (R-STOR-001); an encrypted one with keyed names by the keyed name of R-STOR-086, empty when the replica holds
// no name key of it (a write to the empty name fails; [Connection.Name] says why). Every reader takes the name from the copy
// row, never from this function.
func (c *Connection) ObjectKey(d Digest) string {
	name, _ := c.Name(d)
	return name
}

// Name is ObjectKey with the reason a keyed name cannot be made: the replica holds no name key of the connection.
func (c *Connection) Name(d Digest) (string, error) {
	if !c.KeyedNames() {
		return Key(c.Prefix(), d), nil
	}
	keys, _ := c.sealing()
	if keys == nil {
		return "", fmt.Errorf("blob: this replica holds no name key of connection %s", c.ID())
	}
	nk, err := keys.NameKey(c.ID())
	if err != nil {
		return "", fmt.Errorf("blob: the name key of connection %s: %w", c.ID(), err)
	}
	return blobcrypt.KeyedName(nk, c.Prefix(), d)
}

// PayloadChecksum is the payload checksum the connection's writes carry: sha256, md5 or none (R-STOR-006).
func (c *Connection) PayloadChecksum() string { return c.cfg.PayloadChecksum }

// BatchDelete reports whether collection deletes on this connection with DeleteObjects.
func (c *Connection) BatchDelete() bool { return c.cfg.BatchDeletes() }

// ScrubPeriod is the period within which the scrub reads every copy of the connection whole (R-STOR-027).
func (c *Connection) ScrubPeriod() time.Duration { return c.cfg.ScrubPeriod }

// ScrubBytesPerSecond is the scrub's throttle on this connection.
func (c *Connection) ScrubBytesPerSecond() int64 { return int64(c.cfg.ScrubBytesPerSecond) }

// Rejecting reports whether the connection's circuit is open on this replica, and the Retry-After a client is given; a
// client without a circuit breaker never rejects.
func (c *Connection) Rejecting() (time.Duration, bool) {
	r, ok := c.objects.(interface{ Rejecting() (time.Duration, bool) })
	if !ok {
		return 0, false
	}
	return r.Rejecting()
}

// Circuit is the state of the connection's circuit on this replica: closed, open or half_open (R-OPS-003).
func (c *Connection) Circuit() string {
	if g, ok := c.objects.(interface{ Circuit() string }); ok {
		return g.Circuit()
	}
	return CircuitClosed
}

// Capabilities is what the start check established for the connection (R-STOR-035); the zero value before a check.
func (c *Connection) Capabilities() Capabilities {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.caps
}

// SetCapabilities records what a start check established.
func (c *Connection) SetCapabilities(caps Capabilities) {
	c.mu.Lock()
	c.caps = caps
	c.mu.Unlock()
}

// Verifies reports whether a write through this connection shows the whole stored object to be the blob: Quader hashed
// the bytes and the store enforces the payload checksum the write carried (R-STOR-052). It is false before the start check
// established the enforcement, and with payload_checksum: none.
func (c *Connection) Verifies() bool {
	return c.cfg.PayloadChecksum != "none" && c.Capabilities().PayloadChecksumEnforced
}

// LinkCheck is the result of the connection's link check on this replica (R-STOR-074); the zero value before one ran.
func (c *Connection) LinkCheck() LinkCheck {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.link
}

// SetLinkCheck records the result of a link check.
func (c *Connection) SetLinkCheck(r LinkCheck) {
	c.mu.Lock()
	c.link = r
	c.mu.Unlock()
}

// Presign signs a GET of key as o says with the connection's runtime credential, through its guard: a connection whose circuit
// is open signs nothing (R-STOR-062, R-STOR-072).
func (c *Connection) Presign(ctx context.Context, key string, o PresignOptions) (Link, error) {
	return c.objects.Presign(ctx, key, o)
}

// Fetcher is what fetches the connection's links, the HTTP client its store is reached with; nil when it has none. The link check
// fetches with it (R-STOR-074).
func (c *Connection) Fetcher() Fetcher { return FetcherOf(c.objects) }

// Capabilities is what the start check found a connection to support (R-STOR-035, Q-STOR-248), as the column
// storage_connections.capabilities stores it and the API shows it (R-STOR-061).
type Capabilities struct {
	// PayloadChecksumEnforced: a write with a wrong payload checksum was refused, so a write whose checksum the store
	// accepted is the bytes Quader hashed. False with payload_checksum: none.
	PayloadChecksumEnforced bool `json:"payloadChecksumEnforced"`
	// BatchDelete: DeleteObjects removed two probe keys with one request; false when the connection has batch_delete: false.
	BatchDelete bool `json:"batchDelete"`
	// Metadata: an object's metadata survived a write and was returned by HeadObject.
	Metadata bool `json:"metadata"`
	// CopyReplacesMetadata: a copy of an object onto itself with replaced metadata changed it.
	CopyReplacesMetadata bool `json:"copyReplacesMetadata"`
	// ServerSideCopyFrom names, per other connection on the same endpoint, whether a copy of a probe object of its bucket into
	// this connection's bucket, signed with this connection's credential, worked (the reconciliation's server-side copy,
	// R-STOR-056). A connection on another endpoint, or one that could not be asked, is absent.
	ServerSideCopyFrom map[string]bool `json:"serverSideCopyFrom,omitempty"`
	// SSEApplied is the server-side encryption the store reported for the start check's probe object, the actual state of today's
	// writes beside the configured sse_mode (R-STOR-032, Q-STOR-519); nil when the check wrote no probe it could ask about.
	SSEApplied *SSEApplied `json:"sseApplied,omitempty"`
	// CheckedAt is when the start check that found these ran; zero before one.
	CheckedAt time.Time `json:"checkedAt,omitzero"`
}

// SSEApplied is the server-side encryption a store reported it applied to an object (R-STOR-032, R-STOR-035): the algorithm
// (AES256, aws:kms, aws:kms:dsse; empty when the store reported none) and the KMS key it named. Neither is secret.
type SSEApplied struct {
	Algorithm string `json:"algorithm"`
	KMSKeyID  string `json:"kmsKeyId,omitempty"`
}

// Connections is the ordered set of a replica's connections (R-STOR-050): the list order of its configuration is the write
// order and the read order (Q-STOR-221).
type Connections struct {
	list []*Connection
	byID map[string]*Connection
}

// NewConnections orders the connections as given and numbers their positions from 1. It refuses an empty set, more than
// config.MaxConnections and two connections with one id.
func NewConnections(conns ...*Connection) (*Connections, error) {
	if len(conns) == 0 {
		return nil, errors.New("blob: the store needs one connection at least (storage.connections)")
	}
	if len(conns) > config.MaxConnections {
		return nil, fmt.Errorf("blob: %d connections, at most %d (Q-STOR-247)", len(conns), config.MaxConnections)
	}
	cs := &Connections{byID: make(map[string]*Connection, len(conns))}
	for i, c := range conns {
		if _, dup := cs.byID[c.ID()]; dup {
			return nil, fmt.Errorf("blob: two connections with the id %q", c.ID())
		}
		c.position = i + 1
		cs.byID[c.ID()] = c
		cs.list = append(cs.list, c)
	}
	return cs, nil
}

// SingleConnection is the set of one connection with the defaults of an element whose id is default and whose prefix is
// prefix, reached through objects: the store of the one connection of a test or a tool.
func SingleConnection(prefix string, objects ObjectStore) *Connections {
	p := prefix
	cs, _ := NewConnections(NewConnection(config.StorageConnection{Prefix: &p}, objects))
	return cs
}

// List is the connections in list order.
func (cs *Connections) List() []*Connection { return slices.Clone(cs.list) }

// Get is the connection of an id, false when this replica does not configure it.
func (cs *Connections) Get(id string) (*Connection, bool) {
	c, ok := cs.byID[id]
	return c, ok
}

// First is the first connection of the list.
func (cs *Connections) First() *Connection { return cs.list[0] }

// Targets is the connections that hold every blob: those without drain: true, in list order (R-STOR-052).
func (cs *Connections) Targets() []*Connection {
	var out []*Connection
	for _, c := range cs.list {
		if !c.Drain() {
			out = append(out, c)
		}
	}
	return out
}

// Writable is the connections an upload goes to now: without drain: true and with a closed circuit on this replica, in list
// order (R-STOR-053).
func (cs *Connections) Writable() []*Connection {
	var out []*Connection
	for _, c := range cs.list {
		if _, rejecting := c.Rejecting(); !c.Drain() && !rejecting {
			out = append(out, c)
		}
	}
	return out
}

// readOrder sorts copies into the order a read tries them (R-STOR-055): the copies on connections this replica configures,
// in list order, a draining connection's last. Copies on other connections are left out.
func (cs *Connections) readOrder(copies []Copy) []Copy {
	var out []Copy
	for _, cp := range copies {
		if _, ok := cs.byID[cp.Connection]; ok {
			out = append(out, cp)
		}
	}
	slices.SortStableFunc(out, func(a, b Copy) int {
		ca, cb := cs.byID[a.Connection], cs.byID[b.Connection]
		if ca.Drain() != cb.Drain() {
			if ca.Drain() {
				return 1
			}
			return -1
		}
		return ca.position - cb.position
	})
	return out
}
