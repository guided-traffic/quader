package keys

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/secrets"
)

// DocumentFormat is the format of an export of the storage keys (R-STOR-085).
const DocumentFormat = "quader-storage-keys-v1"

// maxDocument bounds an export file the import reads: an installation has a handful of rows of a few hundred bytes each.
const maxDocument = 4 << 20

// Document is the export of the storage keys (R-STOR-085, Q-STOR-312): the rows of storage_keys sealed as they lie, the installation
// they belong to and the root keys they are sealed with. With the key ring it opens every key it lists, without it nothing.
type Document struct {
	Format         string        `json:"format"`
	InstallationID string        `json:"installationId"`
	ExportedAt     time.Time     `json:"exportedAt"`
	RootKeyIDs     []string      `json:"rootKeyIds"`
	Keys           []DocumentKey `json:"keys"`
}

// DocumentKey is one row of the export. A destroyed row has destroyedAt and no sealed value (R-STOR-103).
type DocumentKey struct {
	KeyID       string     `json:"keyId"`
	Connection  string     `json:"connection"`
	Kind        string     `json:"kind"`
	Generation  int        `json:"generation"`
	State       string     `json:"state"`
	CreatedAt   time.Time  `json:"createdAt"`
	RetiredAt   *time.Time `json:"retiredAt,omitempty"`
	DestroyedAt *time.Time `json:"destroyedAt,omitempty"`
	Sealed      string     `json:"sealed,omitempty"`
}

// ExportFunc runs in the transaction that read the rows, before the document leaves: the audit event system.storage.keys_exported
// hangs here, so an export that could not be audited is not printed.
type ExportFunc func(ctx context.Context, tx meta.Tx, d Document) error

// ErrNoKeys is the export of a connection that has no row.
var ErrNoKeys = errors.New("the database has no storage keys for this connection")

// Export reads the rows of storage_keys, of every connection or of the one named, into the document of R-STOR-085. It opens
// nothing and needs no key ring. A destroyed row is listed without a sealed value (R-STOR-103). The export is recorded as a row of
// storage_key_exports in its transaction, with the pod that ran it (instance) and the identifiers it lists, so that a destruction
// reports every export made before it. A named connection without rows is ErrNoKeys; an installation without any is a document
// without keys.
func Export(ctx context.Context, db meta.Runner, rows Rows, installation, instance, connection string, now time.Time, then ExportFunc) (Document, error) {
	if rows == nil {
		rows = PG{}
	}
	var d Document
	err := db.InTx(ctx, func(tx meta.Tx) error {
		list, err := rows.ForExport(ctx, tx, connection)
		if err != nil {
			return err
		}
		if connection != "" && len(list) == 0 {
			return fmt.Errorf("%w: %s", ErrNoKeys, connection)
		}
		d = Document{Format: DocumentFormat, InstallationID: installation, ExportedAt: now.UTC().Truncate(time.Second), RootKeyIDs: []string{}, Keys: []DocumentKey{}}
		for _, r := range list {
			k, root, err := documentKey(r)
			if err != nil {
				return err
			}
			if root != "" && !slices.Contains(d.RootKeyIDs, root) {
				d.RootKeyIDs = append(d.RootKeyIDs, root)
			}
			d.Keys = append(d.Keys, k)
		}
		sort.Strings(d.RootKeyIDs)
		if err := rows.RecordExport(ctx, tx, instance, d.KeyIDs()); err != nil {
			return err
		}
		if then != nil {
			return then(ctx, tx, d)
		}
		return nil
	})
	return d, err
}

// documentKey is the entry of a row and the root key its value is sealed with ("" for a destroyed row).
func documentKey(r Row) (DocumentKey, string, error) {
	k := DocumentKey{KeyID: r.KeyID, Connection: r.Connection, Kind: r.Kind, Generation: r.Generation, State: r.State, CreatedAt: r.CreatedAt.UTC()}
	if !r.RetiredAt.IsZero() {
		t := r.RetiredAt.UTC()
		k.RetiredAt = &t
	}
	if r.State == StateDestroyed {
		t := r.DestroyedAt.UTC()
		k.DestroyedAt = &t
		return k, "", nil
	}
	root, err := secrets.KeyIDOf(r.Sealed)
	if err != nil {
		return k, "", fmt.Errorf("the row %s of storage_keys holds no sealed value: %w", r.KeyID, err)
	}
	k.Sealed = r.Sealed
	return k, root, nil
}

// KeyIDs lists the identifiers the document holds, in its order.
func (d Document) KeyIDs() []string {
	out := make([]string, 0, len(d.Keys))
	for _, k := range d.Keys {
		out = append(out, k.KeyID)
	}
	return out
}

// ImportError is an import that does not fit: a file that is no export, a key the ring cannot open, a file of another
// installation, a key that exists with another value. Nothing was written. Its message names the identifiers and the root keys,
// never a byte of a key or of a sealed value.
type ImportError struct{ msg string }

func (e *ImportError) Error() string { return e.msg }

func importError(format string, args ...any) error {
	return &ImportError{msg: fmt.Sprintf(format, args...)}
}

// ReadDocument reads and checks an export (R-STOR-085): a JSON document of the format, from this or another installation, whose rows
// are rows the table can hold. A file cut short, of another format or with a row the table would refuse is an ImportError that says
// so.
func ReadDocument(r io.Reader) (Document, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxDocument+1))
	if err != nil {
		return Document{}, err
	}
	if len(data) > maxDocument {
		return Document{}, importError("the file is larger than %d bytes; an export of the storage keys is a few kilobytes", maxDocument)
	}
	var d Document
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return Document{}, importError("the file ends before the export does: it was cut short; use a complete copy of the export")
		}
		return Document{}, importError("the file is not an export of the storage keys (JSON of the format %s): %s", DocumentFormat, jsonProblem(err))
	}
	if dec.More() {
		return Document{}, importError("the file holds more than one JSON document; an export is one")
	}
	if d.Format != DocumentFormat {
		return Document{}, importError("the file is of the format %q, not %q", d.Format, DocumentFormat)
	}
	if d.InstallationID == "" {
		return Document{}, importError("the file names no installation (installationId)")
	}
	if len(d.Keys) == 0 {
		return Document{}, importError("the file lists no key")
	}
	return d, checkKeys(d.Keys)
}

// jsonProblem names what the decoder found without quoting the file.
func jsonProblem(err error) string {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		return fmt.Sprintf("invalid JSON at byte %d", syn.Offset)
	case errors.As(err, &typ):
		return fmt.Sprintf("the member %s is not a %s", typ.Field, typ.Type)
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return "it has the member " + strings.TrimPrefix(err.Error(), "json: unknown field ") + ", which an export has not"
	}
	return "it does not decode"
}

// checkKeys holds the rows of a file to what the table holds (R-STOR-083, Q-STOR-324, R-STOR-103): an identifier that names the row's
// kind, connection and generation, a state, a sealed value (a destroyed row has none, and its retiredAt and destroyedAt), one row per
// identifier and per connection, kind and generation, one active row per connection and kind, and a name key that is active or
// destroyed.
func checkKeys(list []DocumentKey) error {
	ids := map[string]bool{}
	places := map[string]bool{}
	active := map[string]bool{}
	for i, k := range list {
		id, ok := ParseID(k.KeyID)
		switch {
		case !ok:
			return importError("key %d of the file has no identifier of storage_keys (sk-<connection>-<generation>-<8 hex> or nk-<connection>-<8 hex>)", i+1)
		case id.Kind != k.Kind || id.Connection != k.Connection || id.Generation != k.Generation:
			return importError("key %s says connection %q, kind %q and generation %d, which its identifier does not name; the file was changed", k.KeyID, k.Connection, k.Kind, k.Generation)
		case k.State != StateActive && k.State != StateRetired && k.State != StateDestroyed:
			return importError("key %s has the state %q, not active, retired or destroyed", k.KeyID, k.State)
		case k.Kind == KindName && k.State == StateRetired:
			return importError("name key %s is retired; a name key is never retired", k.KeyID)
		case k.State == StateDestroyed && (k.Sealed != "" || k.DestroyedAt == nil || k.RetiredAt == nil):
			return importError("key %s is destroyed and must have destroyedAt and retiredAt and no sealed value", k.KeyID)
		case k.State != StateDestroyed && (!secrets.IsSealed(k.Sealed) || k.DestroyedAt != nil):
			return importError("key %s holds no sealed value of the form v1:<root key>:<base64>", k.KeyID)
		case ids[k.KeyID]:
			return importError("the file lists key %s twice", k.KeyID)
		}
		ids[k.KeyID] = true
		place := fmt.Sprintf("%s/%s/%d", k.Connection, k.Kind, k.Generation)
		if places[place] {
			return importError("the file lists two keys of connection %s, kind %s and generation %d", k.Connection, k.Kind, k.Generation)
		}
		places[place] = true
		if k.State == StateActive {
			slot := k.Connection + "/" + k.Kind
			if active[slot] {
				return importError("the file lists two active %s keys of connection %s", k.Kind, k.Connection)
			}
			active[slot] = true
		}
	}
	return nil
}

// ImportOptions are the import's choices.
type ImportOptions struct {
	// Installation is the id of the installation the command runs in (system_state).
	Installation string
	// OtherInstallation accepts a file of another installation, for a recovery into a new one.
	OtherInstallation bool
}

// ImportReport is what an import did. Destroyed lists the sealed keys of the file it skipped because the database holds their
// connection destroyed; StillSealed the destroyed keys of the file it did not insert because the database holds that key, or its
// connection's keys, sealed: an import never destroys a key (R-STOR-103, Q-STOR-460).
type ImportReport struct {
	Connections        []string `json:"connections"`
	Inserted           int      `json:"inserted"`
	Kept               int      `json:"kept"`
	Destroyed          []string `json:"destroyed"`
	StillSealed        []string `json:"stillSealed"`
	SourceInstallation string   `json:"sourceInstallation"`
	OtherInstallation  bool     `json:"otherInstallation"`
}

// ImportFunc runs in the import's transaction after its rows were written: the audit event system.storage.keys_imported and the
// changes of the area storage_keys hang here.
type ImportFunc func(ctx context.Context, tx meta.Tx, rep ImportReport) error

// Import adds the rows of an export that the database lacks (R-STOR-085, Q-STOR-312, Q-STOR-325, R-STOR-103). It first opens every
// sealed value of the file with the ring, but those of a connection whose keys the database holds destroyed, so a ring that lacks a
// root key or a value that does not open under its identifier is refused before anything is written; a file of another installation
// is refused unless opts.OtherInstallation. In one transaction that locks the rows of every connection and kind the file names, a row
// that exists with the same key is kept, a row whose identifier exists with another key, or whose connection, kind and generation the
// database holds under another identifier, refuses the whole import, and every other row is inserted. A connection of which the
// database holds a destroyed row gets nothing back: its sealed rows are skipped and reported, and only its destroyed rows the database
// lacks are inserted; a destroyed row of the file is never applied to a key the database holds sealed (Q-STOR-460). The highest
// generation of a connection's storage key that is not destroyed, of the database's and the file's, is then the active one; a
// connection's name key is active unless destroyed. Every refusal is an ImportError.
func Import(ctx context.Context, db meta.Runner, rows Rows, ring Sealer, d Document, opts ImportOptions, then ImportFunc) (ImportReport, error) {
	if rows == nil {
		rows = PG{}
	}
	rep := ImportReport{SourceInstallation: d.InstallationID, OtherInstallation: d.InstallationID != opts.Installation, Destroyed: []string{}, StillSealed: []string{}}
	if err := checkKeys(d.Keys); err != nil {
		return rep, err
	}
	if rep.OtherInstallation && !opts.OtherInstallation {
		return rep, importError("the file was exported by installation %s, and this is installation %s; pass --other-installation to recover its keys into this installation", d.InstallationID, opts.Installation)
	}
	conns := map[string]bool{}
	for _, k := range d.Keys {
		conns[k.Connection] = true
	}
	for c := range conns {
		rep.Connections = append(rep.Connections, c)
	}
	sort.Strings(rep.Connections)
	gone, err := destroyedConnections(ctx, db, rows, rep.Connections)
	if err != nil {
		return rep, err
	}
	raw := map[string][]byte{}
	defer func() {
		for _, b := range raw {
			clear(b)
		}
	}()
	for _, k := range d.Keys {
		if k.State == StateDestroyed || gone[k.Connection] {
			continue // nothing of it is inserted or compared
		}
		b, err := ring.Open(keyColumn, secrets.Row(k.KeyID), k.Sealed)
		if err != nil {
			return rep, importError("%s; nothing was imported", openRefusal(k.KeyID, k.Sealed, err))
		}
		raw[k.KeyID] = b
	}
	err = db.InTx(ctx, func(tx meta.Tx) error {
		rep.Inserted, rep.Kept, rep.Destroyed, rep.StillSealed = 0, 0, []string{}, []string{}
		for _, c := range rep.Connections {
			for _, kind := range []string{KindStorage, KindName} {
				if _, err := rows.LockKind(ctx, tx, c, kind); err != nil {
					return err
				}
			}
			if err := importConnection(ctx, tx, rows, ring, c, d.Keys, raw, &rep); err != nil {
				return err
			}
		}
		if then != nil {
			return then(ctx, tx, rep)
		}
		return nil
	})
	return rep, err
}

// destroyedConnections reads which of the connections the database holds a destroyed row of. A connection never leaves that state, so
// the import's transaction, which reads the rows again under their locks, finds at least these.
func destroyedConnections(ctx context.Context, db meta.Runner, rows Rows, conns []string) (map[string]bool, error) {
	gone := map[string]bool{}
	err := db.InTx(ctx, func(tx meta.Tx) error {
		for _, c := range conns {
			have, err := rows.OfConnection(ctx, tx, c)
			if err != nil {
				return err
			}
			gone[c] = hasDestroyed(have)
		}
		return nil
	})
	return gone, err
}

// hasDestroyed reports whether a connection's rows hold a destroyed one.
func hasDestroyed(rows []Row) bool {
	for _, r := range rows {
		if r.State == StateDestroyed {
			return true
		}
	}
	return false
}

// importConnection merges the file's rows of one connection into the database's, inside the import's transaction.
func importConnection(ctx context.Context, tx meta.Tx, rows Rows, ring Sealer, connection string, file []DocumentKey, raw map[string][]byte, rep *ImportReport) error {
	have, err := rows.OfConnection(ctx, tx, connection)
	if err != nil {
		return err
	}
	byID, byPlace := map[string]Row{}, map[string]Row{}
	for _, r := range have {
		byID[r.KeyID] = r
		byPlace[fmt.Sprintf("%s/%d", r.Kind, r.Generation)] = r
	}
	gone := hasDestroyed(have)
	for _, k := range file {
		if k.Connection != connection {
			continue
		}
		if k.State == StateDestroyed {
			if err := importDestroyed(ctx, tx, rows, k, byID, byPlace, len(have) > 0 && !gone, rep); err != nil {
				return err
			}
			continue
		}
		if gone {
			rep.Destroyed = append(rep.Destroyed, k.KeyID) // nothing brings a key of a destroyed connection back
			continue
		}
		if r, ok := byID[k.KeyID]; ok {
			if err := sameKey(ring, r, raw[k.KeyID]); err != nil {
				return err
			}
			rep.Kept++
			continue
		}
		if r, ok := byPlace[fmt.Sprintf("%s/%d", k.Kind, k.Generation)]; ok {
			return importError("connection %s holds %s as its %s key of generation %d, the file %s: the two are different keys, so the file is not an export of this database's keys; nothing was imported",
				connection, r.KeyID, k.Kind, k.Generation, k.KeyID)
		}
		row := Row{
			KeyID: k.KeyID, Connection: k.Connection, Kind: k.Kind, Generation: k.Generation, Sealed: k.Sealed, State: StateRetired,
			CreatedAt: k.CreatedAt, RetiredAt: time.Now().UTC(),
		}
		if k.RetiredAt != nil {
			row.RetiredAt = *k.RetiredAt
		}
		if err := insertImported(ctx, tx, rows, row); err != nil {
			return err
		}
		rep.Inserted++
	}
	return activateHighest(ctx, tx, rows, connection)
}

// importDestroyed inserts a destroyed row of the file that the database lacks (R-STOR-103). A key the database holds destroyed is kept;
// one it holds sealed, or a key of a connection whose keys it holds sealed (heldSealed), is reported and left: an import never
// destroys a key, and a destroyed row would retire a live connection's id (Q-STOR-460). A place the database holds under another
// identifier, which only a destroyed connection's other key can be here, is left as it is.
func importDestroyed(ctx context.Context, tx meta.Tx, rows Rows, k DocumentKey, byID, byPlace map[string]Row, heldSealed bool, rep *ImportReport) error {
	if r, ok := byID[k.KeyID]; ok {
		if r.State == StateDestroyed {
			rep.Kept++
		} else {
			rep.StillSealed = append(rep.StillSealed, k.KeyID)
		}
		return nil
	}
	if heldSealed {
		rep.StillSealed = append(rep.StillSealed, k.KeyID)
		return nil
	}
	if _, ok := byPlace[fmt.Sprintf("%s/%d", k.Kind, k.Generation)]; ok {
		return nil
	}
	if err := insertImported(ctx, tx, rows, Row{
		KeyID: k.KeyID, Connection: k.Connection, Kind: k.Kind, Generation: k.Generation, State: StateDestroyed,
		CreatedAt: k.CreatedAt, RetiredAt: *k.RetiredAt, DestroyedAt: *k.DestroyedAt,
	}); err != nil {
		return err
	}
	rep.Inserted++
	return nil
}

// insertImported inserts a row of the file; a row another writer inserted meanwhile fails the import.
func insertImported(ctx context.Context, tx meta.Tx, rows Rows, row Row) error {
	inserted, err := rows.Insert(ctx, tx, row)
	if err != nil {
		return err
	}
	if !inserted {
		return fmt.Errorf("key %s of connection %s was written by another writer during the import; run the import again", row.KeyID, row.Connection)
	}
	return nil
}

// sameKey compares the key of a row of the database with the file's, opened, in constant time.
func sameKey(ring Sealer, r Row, file []byte) error {
	b, err := ring.Open(keyColumn, secrets.Row(r.KeyID), r.Sealed)
	if err != nil {
		return importError("%s (the row of this database); nothing was imported", openRefusal(r.KeyID, r.Sealed, err))
	}
	defer clear(b)
	if subtle.ConstantTimeCompare(b, file) != 1 {
		return importError("key %s exists in this database with another value; the file is not an export of this database's keys, or the key was drawn anew; nothing was imported", r.KeyID)
	}
	return nil
}

// activateHighest makes the storage key of the highest generation of a connection that is not destroyed the active one, and its name
// key active unless destroyed (Q-STOR-325, R-STOR-103): a connection whose keys are all destroyed has none.
func activateHighest(ctx context.Context, tx meta.Tx, rows Rows, connection string) error {
	all, err := rows.OfConnection(ctx, tx, connection)
	if err != nil {
		return err
	}
	var highest, active, name Row
	for _, r := range all {
		switch {
		case r.State == StateDestroyed:
		case r.Kind == KindName:
			name = r
		case r.Generation > highest.Generation:
			highest = r
		}
		if r.Kind == KindStorage && r.State == StateActive {
			active = r
		}
	}
	if highest.KeyID != "" && highest.KeyID != active.KeyID {
		if active.KeyID != "" {
			if _, err := rows.Retire(ctx, tx, active.KeyID); err != nil {
				return err
			}
		}
		if _, err := rows.Activate(ctx, tx, highest.KeyID); err != nil {
			return err
		}
	}
	if name.KeyID != "" && name.State != StateActive {
		if _, err := rows.Activate(ctx, tx, name.KeyID); err != nil {
			return err
		}
	}
	return nil
}
