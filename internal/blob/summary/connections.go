package summary

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The states of a connection (storage.md R-STOR-051), derived from its row and the replica registry, never set by hand.
const (
	StateFilling  = "filling"
	StateActive   = "active"
	StateDraining = "draining"
	StateDrained  = "drained"
	StateDetached = "detached"
	StateRetired  = "retired"
)

// Row is what PostgreSQL knows of one connection at the time of a request (R-STOR-061): its row of storage_connections, the live
// replicas of the registry that configure it, the queue and its storage keys.
type Row struct {
	ID, Bucket, Prefix  string
	Encrypted           bool
	KeyedNames          bool
	Capabilities        *blob.Capabilities
	FilledAt, DrainedAt *time.Time
	RetiredAt           *time.Time
	LastFullPassAt      *time.Time
	// LiveReplicas counts the live replicas that report an object-store view; Configured those that have the connection in their
	// configuration, CircuitOpen those of them with its circuit open, and Undrained whether one of them configures it without drain.
	LiveReplicas, Configured, CircuitOpen int
	Undrained                             bool
	// Queued and OldestQueuedAt are the reconciliation's queue rows for the connection (R-STOR-056).
	Queued         int64
	OldestQueuedAt *time.Time
	HoldsCopies    bool
	Keys           []StorageKey
}

// StorageKey is a storage key of a connection by identifier (R-STOR-088, Q-API-405): never the key or its sealed value.
type StorageKey struct {
	ID         string
	Generation int
	State      string
	CreatedAt  time.Time
}

// State derives the connection's state (R-STOR-051) from what PostgreSQL knows, the same for every replica that answers: retired once
// retired_at is set; while a live replica configures it without drain, filling until its first full pass without a gap (filled_at) and
// active after; drained once drained_at is set; detached while no live replica configures it; draining while every live replica that
// configures it has drain true. A connection that no live replica configures and that holds no copy is detached too: it is in no
// configuration, and forget-connection retires it.
func (r Row) State() string {
	switch {
	case r.RetiredAt != nil:
		return StateRetired
	case r.Configured > 0 && r.Undrained && r.FilledAt == nil:
		return StateFilling
	case r.Configured > 0 && r.Undrained:
		return StateActive
	case r.DrainedAt != nil:
		return StateDrained
	case r.Configured == 0:
		return StateDetached
	}
	return StateDraining
}

// Rows reads every connection ever seen with the registry's live replicas (heartbeat younger than staleAfter), the queue and the
// storage keys, in two short reads.
func Rows(ctx context.Context, db *postgres.DB, staleAfter time.Duration) ([]Row, error) {
	var list []q.StorageConnectionsListRow
	var keys []q.StorageConnectionKeysRow
	err := db.Read(ctx, func(c q.DBTX) error {
		qs := q.New(c)
		var err error
		if list, err = qs.StorageConnectionsList(ctx, staleAfter.Seconds()); err != nil {
			return err
		}
		keys, err = qs.StorageConnectionKeys(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	byConn := map[string][]StorageKey{}
	for _, k := range keys {
		byConn[k.ConnectionID] = append(byConn[k.ConnectionID], StorageKey{
			ID: k.KeyID, Generation: int(k.Generation), State: k.State, CreatedAt: time.UnixMicro(k.CreatedUs).UTC(),
		})
	}
	out := make([]Row, 0, len(list))
	for _, l := range list {
		r := Row{
			ID: l.ID, Bucket: l.Bucket, Prefix: l.Prefix, Encrypted: l.Encrypted, KeyedNames: l.KeyedNames,
			FilledAt: micros(l.FilledUs), DrainedAt: micros(l.DrainedUs), RetiredAt: micros(l.RetiredUs), LastFullPassAt: micros(l.LastFullPassUs),
			LiveReplicas: int(l.LiveReplicas), Configured: int(l.Configured), CircuitOpen: int(l.CircuitOpen), Undrained: l.Undrained,
			Queued: l.Queued, OldestQueuedAt: micros(l.OldestQueuedUs), HoldsCopies: l.HoldsCopies, Keys: byConn[l.ID],
		}
		var caps blob.Capabilities
		if len(l.Capabilities) > 0 && string(l.Capabilities) != "{}" && json.Unmarshal(l.Capabilities, &caps) == nil {
			r.Capabilities = &caps
		}
		out = append(out, r)
	}
	return out, nil
}

// micros is a time of microseconds since the epoch, nil for 0.
func micros(us int64) *time.Time {
	if us == 0 {
		return nil
	}
	t := time.UnixMicro(us).UTC()
	return &t
}

// Local is what the answering replica's configuration says of one of its connections (R-STOR-061): its place in the list and the
// non-secret settings of its element. No credential, no file path and no key.
type Local struct {
	Position         int
	Endpoint         string
	Region           string
	CredentialSource string
	Drain            bool
	ScrubPeriod      time.Duration
	Settings         Settings
}

// Settings are the non-secret settings of an element that the admin area shows on demand (api-and-ui.md R-UI-196).
type Settings struct {
	ForcePathStyle             bool
	PayloadChecksum            string
	RequestChecksumCalculation string
	ResponseChecksumValidation string
	BatchDelete                bool
	StorageClass               string
	SSEMode                    string
	ScrubBytesPerSecond        int64
}

// View is one connection of the API's list: the row with its state, what the answering replica configures (Local, nil for a connection
// it does not configure) and the figures of the latest summary (Figures, nil before the first or for a connection the summary did not
// count).
type View struct {
	Row
	State   string
	Local   *Local
	Figures *Connection
	// ComputedAt is the latest summary's time; zero without one.
	ComputedAt time.Time
}

// Views joins the rows, the answering replica's connections by id and the latest summary into the list of the API (R-STOR-061): the
// connections the replica configures in the order of its configuration, then the others by id. latest may be nil.
func Views(rows []Row, local map[string]Local, latest *Summary) []View {
	figures := map[string]*Connection{}
	var at time.Time
	if latest != nil {
		at = latest.ComputedAt
		for i := range latest.Connections {
			figures[latest.Connections[i].ID] = &latest.Connections[i]
		}
	}
	out := make([]View, 0, len(rows))
	for _, r := range rows {
		v := View{Row: r, State: r.State(), Figures: figures[r.ID]}
		if v.Figures != nil {
			v.ComputedAt = at
		}
		if l, ok := local[r.ID]; ok {
			v.Local = &l
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Local, out[j].Local
		switch {
		case a != nil && b != nil:
			return a.Position < b.Position
		case a != nil || b != nil:
			return a != nil
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// List is the API's list (R-STOR-061): the rows, the answering replica's connections and the latest summary, read at the time of the
// request.
func List(ctx context.Context, db *postgres.DB, staleAfter time.Duration, local map[string]Local) ([]View, error) {
	rows, err := Rows(ctx, db, staleAfter)
	if err != nil {
		return nil, err
	}
	s, err := Latest(ctx, db)
	switch {
	case errors.Is(err, ErrNone):
		return Views(rows, local, nil), nil
	case err != nil:
		return nil, err
	}
	return Views(rows, local, &s), nil
}

// LocalOf is what the replica's configuration says of each of its connections, by id.
func LocalOf(conns *blob.Connections) map[string]Local {
	out := map[string]Local{}
	if conns == nil {
		return out
	}
	for _, c := range conns.List() {
		el := c.Config().WithDefaults()
		out[c.ID()] = Local{
			Position: c.Position(), Endpoint: el.Endpoint, Region: el.Region, CredentialSource: el.CredentialsSource, Drain: el.Drain,
			ScrubPeriod: el.ScrubPeriod,
			Settings: Settings{
				ForcePathStyle: el.ForcePathStyle, PayloadChecksum: el.PayloadChecksum, RequestChecksumCalculation: el.RequestChecksumCalculation,
				ResponseChecksumValidation: el.ResponseChecksumValidation, BatchDelete: el.BatchDeletes(), StorageClass: el.StorageClass,
				SSEMode: el.SSEMode, ScrubBytesPerSecond: int64(el.ScrubBytesPerSecond),
			},
		}
	}
	return out
}
