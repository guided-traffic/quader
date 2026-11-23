package keys

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
)

// The destruction of a retired connection's keys (storage.md R-STOR-097 to R-STOR-100, Q-STOR-400 to Q-STOR-405, ADR-255): every key
// of one connection, every generation of its storage key and its name key, in one transaction that locks the key rows and the
// connection's row, reads every holder and the backups' listing, compares the confirmation, and only then sets the rows destroyed.
// Nothing here opens a key: the command runs without the key ring.

// confirmLabel opens the input of the confirmation's hash (R-STOR-099).
const confirmLabel = "quader-storage-keys-destroy-v1\n"

// Confirmation is the value --confirm must carry to destroy the keys ids of a connection of an installation (R-STOR-099, Q-STOR-404):
// <connection>-<8 hex>, the first 4 bytes of SHA-256("quader-storage-keys-destroy-v1\n" ‖ installation ‖ "\n" ‖ the identifiers that
// are not destroyed, sorted, joined by "\n"). Another installation, or another set of keys (a generation drawn or imported since the
// dry run), gives another value.
func Confirmation(installation, connection string, ids []string) string {
	sorted := slices.Sorted(slices.Values(ids))
	sum := sha256.Sum256([]byte(confirmLabel + installation + "\n" + strings.Join(sorted, "\n")))
	return connection + "-" + hex.EncodeToString(sum[:4])
}

// DestroyOptions are the inputs of a destruction.
type DestroyOptions struct {
	Connection string
	// Installation is the installation's id (system_state), which the confirmation binds.
	Installation string
	// Confirm is the value of --confirm; empty is a dry run that reports and prints the confirmation.
	Confirm string
	// DryRun with Confirm compares the confirmation and runs every check, and destroys nothing (a Job's preflight).
	DryRun bool
	// Configured are the ids of the command's own storage.connections, which would load the keys (R-STOR-098).
	Configured []string
	// StaleAfter is system.heartbeat_stale_after: a replica whose heartbeat is older is not live.
	StaleAfter time.Duration
	// Backup is the backup target of the command's configuration; an empty bucket is no backup configured (R-STOR-100, Q-STOR-461).
	Backup BackupTarget
	// Now is the clock of the report and the journal entry; nil is time.Now.
	Now func() time.Time
}

// BackupTarget is the backup target of a configuration: backup.target.bucket and backup.target.prefix.
type BackupTarget struct {
	Bucket, Prefix string
}

// String is the target as a run records it: s3://<bucket>/<prefix>.
func (t BackupTarget) String() string {
	return "s3://" + t.Bucket + "/" + strings.Trim(t.Prefix, "/")
}

// Destruction is what a run of the destruction found and did: the report the command prints as text or JSON. It names identifiers,
// counts and times, never a key or a sealed value.
type Destruction struct {
	Connection string `json:"connection"`
	// State is retired or unrecorded, which allow the destruction; destroyed when every key was destroyed already; or the state that
	// refuses: configured, drained (and not retired), detached.
	State       string          `json:"state"`
	RetiredAt   *time.Time      `json:"retiredAt"`
	DrainedAt   *time.Time      `json:"drainedAt,omitempty"`
	DestroyedAt *time.Time      `json:"destroyedAt,omitempty"`
	Keys        []DestroyKey    `json:"keys"`
	Refusals    []HolderRefusal `json:"refusals"`
	Remaining   Remaining       `json:"remaining"`
	// Confirm is the confirmation, printed by a dry run whose checks pass.
	Confirm string `json:"confirm,omitempty"`
	DryRun  bool   `json:"dryRun"`
	// Destroyed are the identifiers this run destroyed.
	Destroyed []string `json:"destroyed"`
}

// DestroyKey is a key of the connection as the run found it.
type DestroyKey struct {
	KeyID      string `json:"keyId"`
	Kind       string `json:"kind"`
	Generation int    `json:"generation"`
	State      string `json:"state"`
}

// HolderRefusal is a holder that refuses the destruction (R-STOR-098, R-STOR-100): what it is, how many, and the sentence that says
// what to do.
type HolderRefusal struct {
	Holder string `json:"holder"`
	Count  int64  `json:"count"`
	Say    string `json:"say"`
}

// The holders that refuse.
const (
	HolderReplicas = "replicas"
	HolderState    = "state"
	HolderCopies   = "copies"
	HolderQueue    = "queue"
	HolderSessions = "sessions"
	HolderBackups  = "backups"
	HolderBackup   = "backup"
)

// Remaining are the holders that stay able to open what the provider keeps, with the key ring, and that the destruction reports and
// never refuses for (R-STOR-098).
type Remaining struct {
	Exports        ExportsHeld `json:"exports"`
	Backups        BackupsHeld `json:"backups"`
	Database       string      `json:"database"`
	HandedDataKeys int64       `json:"handedDataKeys"`
}

// ExportsHeld are the exports that listed one of the connection's keys (storage_key_exports).
type ExportsHeld struct {
	Count  int64      `json:"count"`
	LastAt *time.Time `json:"lastAt"`
}

// BackupsHeld are the retained backups whose database holds the keys sealed, as the newest retention listing records them.
type BackupsHeld struct {
	// Recorded is false when no backup is configured and the database records no backup run (R-STOR-100 case 1).
	Recorded bool `json:"recorded"`
	// Count is the number of retained backups whose sealedKeyIds name one of the keys; null when a retained backup records none.
	Count          *int64     `json:"count"`
	Target         string     `json:"target,omitempty"`
	RunID          string     `json:"runId,omitempty"`
	ListedAt       *time.Time `json:"listedAt,omitempty"`
	LeaveNotBefore *time.Time `json:"leaveNotBefore,omitempty"`
}

// remainingDatabase is what PostgreSQL keeps of the sealed values after the destruction (H-1372, Q-STOR-412).
const remainingDatabase = "PostgreSQL: old row versions, the write-ahead log and any point-in-time archive until they turn over"

// The refusals of a destruction that are not a holder's: each is a wrong call (exit 2).
var (
	// ErrConfigured is a connection in the command's own storage.connections.
	ErrConfigured = errors.New("the connection is in this command's storage.connections")
	// ErrConfirmMismatch is a --confirm that is not the confirmation of the keys now.
	ErrConfirmMismatch = errors.New("the confirmation does not match")
)

// ErrHeld is a destruction that a holder refuses (exit 5); the Destruction lists the refusals.
var ErrHeld = errors.New("the destruction is refused while something still holds the connection's keys")

// DestroyFunc runs in the destroying transaction after the rows were destroyed: the change of the area storage_keys and the audit
// event system.storage.keys_destroyed with its journal entry hang here. An error rolls the destruction back.
type DestroyFunc func(ctx context.Context, tx meta.Tx, d Destruction) error

// Destroy destroys every key of a retired or unrecorded connection (R-STOR-098, R-STOR-099), in one transaction and in this order:
// it locks the connection's key rows and its row of storage_connections; refuses a connection without rows (ErrNoKeys) and one in the
// command's own configuration (ErrConfigured); reports a connection whose keys are all destroyed already, writing nothing; reads every
// holder of R-STOR-098 and the backups' listing of R-STOR-100, and refuses with ErrHeld while one holds; compares the confirmation
// (ErrConfirmMismatch); and only then sets the rows destroyed and calls then. Without a confirmation, or with DryRun, it runs the same
// reads and writes nothing.
func Destroy(ctx context.Context, db meta.Runner, rows Rows, o DestroyOptions, then DestroyFunc) (Destruction, error) {
	if rows == nil {
		rows = PG{}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	var d Destruction
	err := db.InTx(ctx, func(tx meta.Tx) error {
		d = Destruction{
			Connection: o.Connection, DryRun: o.Confirm == "" || o.DryRun, Keys: []DestroyKey{}, Refusals: []HolderRefusal{}, Destroyed: []string{},
			Remaining: Remaining{Database: remainingDatabase},
		}
		list, err := rows.LockConnection(ctx, tx, o.Connection)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return fmt.Errorf("%w: %s", ErrNoKeys, o.Connection)
		}
		all, live := d.listKeys(list)
		if len(live) == 0 {
			return nil // every key was destroyed already: nothing to do, no event
		}
		if slices.Contains(o.Configured, o.Connection) {
			return fmt.Errorf("%w: %s; remove it from the configuration this command runs with", ErrConfigured, o.Connection)
		}
		h, err := rows.Holders(ctx, tx, o.Connection, all, o.StaleAfter)
		if err != nil {
			return err
		}
		now := o.Now().UTC()
		d.judgeHolders(h)
		d.judgeBackups(h, live, o.Backup, now)
		if len(d.Refusals) > 0 {
			return ErrHeld
		}
		want := Confirmation(o.Installation, o.Connection, live)
		switch {
		case o.Confirm == "":
			d.Confirm = want
			return nil
		case subtle.ConstantTimeCompare([]byte(o.Confirm), []byte(want)) != 1:
			return fmt.Errorf("%w: --confirm %s does not match the keys of connection %s now; the dry run prints the current confirmation", ErrConfirmMismatch, o.Confirm, o.Connection)
		case o.DryRun:
			d.Confirm = want
			return nil
		}
		ids, err := rows.Destroy(ctx, tx, o.Connection)
		if err != nil {
			return err
		}
		d.Destroyed, d.DestroyedAt = ids, &now
		for i := range d.Keys {
			if slices.Contains(ids, d.Keys[i].KeyID) {
				d.Keys[i].State = StateDestroyed
			}
		}
		if then != nil {
			return then(ctx, tx, d)
		}
		return nil
	})
	return d, err
}

// listKeys fills the report's keys and returns every identifier and those that are not destroyed; with none left it records the
// state destroyed and the time of the last destruction.
func (d *Destruction) listKeys(list []Row) (all, live []string) {
	var last time.Time
	for _, r := range list {
		d.Keys = append(d.Keys, DestroyKey{KeyID: r.KeyID, Kind: r.Kind, Generation: r.Generation, State: r.State})
		all = append(all, r.KeyID)
		if r.State == StateDestroyed {
			if r.DestroyedAt.After(last) {
				last = r.DestroyedAt
			}
			continue
		}
		live = append(live, r.KeyID)
	}
	if len(live) == 0 {
		d.State, d.DestroyedAt = StateDestroyed, timePtr(last)
	}
	// the storage keys by generation, then the name key
	slices.SortStableFunc(d.Keys, func(a, b DestroyKey) int {
		if a.Kind != b.Kind {
			return strings.Compare(b.Kind, a.Kind)
		}
		return a.Generation - b.Generation
	})
	return all, live
}

// judgeHolders records the connection's state and the holders of R-STOR-098 that refuse, and the ones it reports.
func (d *Destruction) judgeHolders(h Holders) {
	c := d.Connection
	d.RetiredAt, d.DrainedAt = timePtr(h.RetiredAt), timePtr(h.DrainedAt)
	if h.LiveReplicas > 0 {
		d.refuse(HolderReplicas, h.LiveReplicas, fmt.Sprintf("%d live replicas have connection %s in their storage.connections (the replica registry, heartbeats younger than system.heartbeat_stale_after) and hold its keys; remove the element and roll the replicas out", h.LiveReplicas, c))
	}
	switch {
	case !h.Recorded:
		d.State = "unrecorded"
	case !h.RetiredAt.IsZero():
		d.State = StateRetired
	case !h.DrainedAt.IsZero():
		d.State = "drained"
		d.refuse(HolderState, 1, fmt.Sprintf("connection %s is drained but not retired: the reconciliation retires it once no live replica has it in its configuration; remove the element and roll the replicas out, or give it up with quader admin storage forget-connection %s", c, c))
	case h.LiveReplicas > 0:
		d.State = "configured"
		d.refuse(HolderState, 1, fmt.Sprintf("connection %s is in use (filling, active or draining): set drain: true, wait until it is drained, then remove its element", c))
	default:
		d.State = "detached"
		d.refuse(HolderState, 1, fmt.Sprintf("connection %s is detached: its element was removed while it held copies, and putting the element back must restore everything (Q-STOR-225); put it back with drain: true and wait until it is drained, or give it up with quader admin storage forget-connection %s", c, c))
	}
	if h.Copies > 0 {
		d.refuse(HolderCopies, h.Copies, fmt.Sprintf("%d copy rows lie on connection %s or under one of its keys, and their objects are read with these keys; drain the connection, or give it up with quader admin storage forget-connection %s", h.Copies, c, c))
	}
	if h.Queued > 0 {
		d.refuse(HolderQueue, h.Queued, fmt.Sprintf("%d queue rows of the reconciliation name connection %s, which is not settled; the reconciliation removes them when it retires the connection", h.Queued, c))
	}
	if h.Sessions > 0 {
		d.refuse(HolderSessions, h.Sessions, fmt.Sprintf("%d upload sessions lie on connection %s, and a finalize wraps under its active key; they end within storage.uploads.session_max_age", h.Sessions, c))
	}
	d.Remaining.Exports = ExportsHeld{Count: h.Exports, LastAt: timePtr(h.LastExport)}
	d.Remaining.HandedDataKeys = h.HandedDataKeys
}

func (d *Destruction) refuse(holder string, n int64, say string) {
	d.Refusals = append(d.Refusals, HolderRefusal{Holder: holder, Count: n, Say: say})
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	t = t.UTC()
	return &t
}

// destroyedChange is the payload of the change area storage_keys: {"connection"} in version 1 (a new generation, an import), and in
// version 2 with "destroyed", the identifiers a destruction destroyed (R-STOR-101, Q-STOR-407). A payload with destroyed, even an
// empty list, makes a holder forget every key of the connection.
type destroyedChange struct {
	Connection string   `json:"connection"`
	Destroyed  []string `json:"destroyed"`
}

// DestroyedChangeVersion is the payload version of a destruction's change of the area storage_keys.
const DestroyedChangeVersion = 2

// maxChangeBytes bounds the JSON of a destruction's change below the change log's limit of a payload (cluster.ErrPayloadTooLarge, 1 KiB
// less its envelope); a test holds it below that limit.
const maxChangeBytes = 900

// DestroyedChange is the payload of version 2 of the change area storage_keys for a destruction of a connection's keys ids. The list is
// cut from its end to fit the change log's bound, down to an empty list: a holder forgets every key of the connection whatever the
// list names (Q-STOR-462).
func DestroyedChange(connection string, ids []string) any {
	p := struct {
		Connection string   `json:"connection"`
		Destroyed  []string `json:"destroyed"`
	}{Connection: connection, Destroyed: slices.Clone(ids)}
	if p.Destroyed == nil {
		p.Destroyed = []string{}
	}
	for len(p.Destroyed) > 0 {
		if b, err := json.Marshal(p); err == nil && len(b) <= maxChangeBytes {
			break
		}
		p.Destroyed = p.Destroyed[:len(p.Destroyed)-1]
	}
	return p
}
