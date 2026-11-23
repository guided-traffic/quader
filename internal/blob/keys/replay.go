package keys

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
)

// The journal's replay of a destruction (ha-and-helm.md R-HA-054, Q-HA-416, Q-HA-417): a restore of a backup taken before the
// destruction, or a point-in-time restore to before it, loads the keys again, and the replay destroys them again. The entry comes from
// a journal object anybody with a connection's runtime credential can write, so it is a claim checked against the restored database
// and the restoring configuration: a connection the configuration names is not retired, and its keys stay. The handler that
// internal/app registers with internal/journal calls ReplayDestroyed; this package may not import the journal (a domain package).

// DestroyedEntry is the journal entry of system.storage.keys_destroyed.
type DestroyedEntry struct {
	Connection  string    `json:"connection"`
	KeyIDs      []string  `json:"keyIds"`
	DestroyedAt time.Time `json:"destroyedAt"`
}

// maxEntryKeys bounds the identifiers an entry may name: the registry's bound of an array in an event (R-OPS-014).
const maxEntryKeys = 1000

// ReplayOutcome is what a replay did with an entry, in the words of internal/journal.
type ReplayOutcome string

// The outcomes of ReplayDestroyed.
const (
	ReplayApplied   ReplayOutcome = "applied"
	ReplayInEffect  ReplayOutcome = "already in effect"
	ReplayUnknown   ReplayOutcome = "subject unknown"
	ReplaySkipped   ReplayOutcome = "skipped"
	ReplayMalformed ReplayOutcome = "malformed"
)

// ReplayResult is the outcome, the note the replay prints as a warning, and the identifiers it changed.
type ReplayResult struct {
	Outcome ReplayOutcome
	Note    string
	Changed []string
}

// ReplayNotifyFunc announces the change area storage_keys for a connection whose keys the replay destroyed or inserted destroyed.
type ReplayNotifyFunc func(tx meta.Tx, connection string, ids []string) error

// ReplayDestroyed replays a destruction in the replay's transaction (Q-HA-416, Q-HA-417). An entry whose connection the restoring
// configuration names is skipped with its note, and so is every entry when the restore names no connection (it cannot tell, so it
// destroys nothing). Otherwise each identifier is applied: a sealed row is destroyed at the entry's time (not later than now), a
// destroyed row is already in effect, and a missing row is inserted destroyed with created_at at that time, so that no later import
// or reuse of the id brings the key back; a place the database holds under another identifier is left (the entry names keys this
// database does not have). The area storage_keys is announced; no audit event is written.
func ReplayDestroyed(ctx context.Context, tx meta.Tx, rows Rows, e DestroyedEntry, configured []string, now time.Time, notify ReplayNotifyFunc) (ReplayResult, error) {
	if rows == nil {
		rows = PG{}
	}
	if why := e.malformed(); why != "" {
		return ReplayResult{Outcome: ReplayMalformed, Note: why}, nil
	}
	switch {
	case len(configured) == 0:
		return ReplayResult{Outcome: ReplaySkipped, Note: fmt.Sprintf("the restore did not name the connections of its configuration; the keys of connection %s are not destroyed", e.Connection)}, nil
	case slices.Contains(configured, e.Connection):
		return ReplayResult{Outcome: ReplaySkipped, Note: fmt.Sprintf("connection %s is in this installation's configuration; its keys are not destroyed", e.Connection)}, nil
	}
	at := e.DestroyedAt.UTC()
	if at.After(now) {
		at = now.UTC()
	}
	have, err := rows.OfConnection(ctx, tx, e.Connection)
	if err != nil {
		return ReplayResult{}, err
	}
	byID, places := map[string]Row{}, map[string]bool{}
	for _, r := range have {
		byID[r.KeyID] = r
		places[fmt.Sprintf("%s/%d", r.Kind, r.Generation)] = true
	}
	res := ReplayResult{Outcome: ReplayUnknown}
	inEffect := false
	for _, id := range e.KeyIDs {
		r, ok := byID[id]
		var changed bool
		switch {
		case ok && r.State == StateDestroyed:
			inEffect = true
		case ok:
			if changed, err = rows.DestroyKey(ctx, tx, id, at); err != nil {
				return ReplayResult{}, err
			}
		default:
			p, _ := ParseID(id)
			if places[fmt.Sprintf("%s/%d", p.Kind, p.Generation)] {
				continue
			}
			if changed, err = rows.Insert(ctx, tx, Row{
				KeyID: id, Connection: e.Connection, Kind: p.Kind, Generation: p.Generation,
				State: StateDestroyed, CreatedAt: at, RetiredAt: at, DestroyedAt: at,
			}); err != nil {
				return ReplayResult{}, err
			}
		}
		if changed {
			res.Changed = append(res.Changed, id)
		}
	}
	switch {
	case len(res.Changed) > 0:
		res.Outcome = ReplayApplied
		if notify != nil {
			if err := notify(tx, e.Connection, res.Changed); err != nil {
				return ReplayResult{}, err
			}
		}
	case inEffect:
		res.Outcome = ReplayInEffect
	}
	return res, nil
}

// malformed says why an entry cannot be read as a destruction, or "" when it can: a connection id, one to maxEntryKeys identifiers of
// that connection, and a time.
func (e DestroyedEntry) malformed() string {
	if !connectionPattern().MatchString(e.Connection) {
		return "connection is not a connection id"
	}
	if len(e.KeyIDs) == 0 || len(e.KeyIDs) > maxEntryKeys {
		return fmt.Sprintf("keyIds must name 1 to %d keys", maxEntryKeys)
	}
	for _, id := range e.KeyIDs {
		if p, ok := ParseID(id); !ok || p.Connection != e.Connection {
			return "keyIds names a key that is not of the connection"
		}
	}
	if e.DestroyedAt.IsZero() {
		return "destroyedAt is not a time"
	}
	return ""
}
