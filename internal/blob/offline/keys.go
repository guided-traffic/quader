package offline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/secrets"
)

// maxExport bounds the export file: an installation has a handful of rows of a few hundred bytes each (the bound of the import).
const maxExport = 4 << 20

// StateDestroyed is the state of a key the export lists without its sealed value: destroyed on command (storage.md R-STOR-097,
// R-STOR-103). The offline tool reads such an entry and answers destroyed_key for an object whose record names it.
const StateDestroyed = "destroyed"

// exportDocument is the export of the storage keys as storage.md R-STOR-085 and R-STOR-103 spell it: the rows of storage_keys sealed
// as they lie, a destroyed key without its sealed value and with the time of its destruction.
type exportDocument struct {
	Format         string      `json:"format"`
	InstallationID string      `json:"installationId"`
	ExportedAt     time.Time   `json:"exportedAt"`
	RootKeyIDs     []string    `json:"rootKeyIds"`
	Keys           []exportKey `json:"keys"`
}

type exportKey struct {
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

// Refusal is an input that does not fit: an export that is no export, a key ring that lacks a root key or does not open a key, an
// --out that exists, a directory that is not empty. The command exits 2. Its message names files, identifiers and root keys, never
// a byte of a key or of a sealed value.
type Refusal struct{ msg string }

func (r *Refusal) Error() string { return r.msg }

func refuse(format string, args ...any) error { return &Refusal{msg: fmt.Sprintf(format, args...)} }

// IsRefusal reports whether err is a Refusal.
func IsRefusal(err error) bool {
	var r *Refusal
	return errors.As(err, &r)
}

// Keys are the storage keys and name keys of an export, opened with the key ring, and the identifiers it lists as destroyed. They
// live in memory until Close destroys them.
type Keys struct {
	storage map[string]*blobcrypt.StorageKey
	// names are the name keys by connection, and destroyedNames the identifiers of a connection's destroyed name key.
	names          map[string]blobcrypt.Key
	destroyedNames map[string]string
	// destroyed are the identifiers the export lists as destroyed, with the time of the destruction.
	destroyed map[string]time.Time
	// Installation is the installation the export names.
	Installation string
}

// LoadKeys reads the export of exportFile and the key ring of the directory ringDir (its files <key id>.key, the format of
// architecture.md R-ARCH-021; only opening, so no active key is named) and opens every sealed key of the export before anything is
// read: a ring that lacks a root key the export is sealed with is refused naming each such root key, and a key that does not open
// under its identifier is refused naming it. Every refusal is a *Refusal.
func LoadKeys(exportFile, ringDir string) (*Keys, error) {
	doc, err := readExport(exportFile)
	if err != nil {
		return nil, err
	}
	ring, err := loadRing(ringDir)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, k := range doc.Keys {
		if k.Sealed == "" {
			continue
		}
		root, err := secrets.KeyIDOf(k.Sealed)
		if err == nil && !ring.Has(root) && !slices.Contains(missing, root) {
			missing = append(missing, root)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, refuse("the key ring in %s lacks the root key %s, which seals the export %s; name the directory of the key ring files that sealed it (the ring's escrow copy, storage.md R-STOR-089)",
			ringDir, strings.Join(missing, ", "), exportFile)
	}
	ks := &Keys{
		storage: map[string]*blobcrypt.StorageKey{}, names: map[string]blobcrypt.Key{}, destroyedNames: map[string]string{},
		destroyed: map[string]time.Time{}, Installation: doc.InstallationID,
	}
	for _, k := range doc.Keys {
		if err := ks.add(ring, k); err != nil {
			ks.Close()
			return nil, err
		}
	}
	return ks, nil
}

// add opens one key of the export into ks, or records it as destroyed.
func (ks *Keys) add(ring *secrets.KeyRing, k exportKey) error {
	if k.State == StateDestroyed {
		ks.destroyed[k.KeyID] = k.DestroyedAt.UTC()
		if k.Kind == keys.KindName {
			ks.destroyedNames[k.Connection] = k.KeyID
		}
		return nil
	}
	raw, err := ring.Open(keys.Column(), secrets.Row(k.KeyID), k.Sealed)
	if err != nil {
		return openRefusal(k, err)
	}
	defer clear(raw)
	if k.Kind == keys.KindName {
		nk, err := blobcrypt.KeyFromBytes(raw)
		if err != nil {
			return refuse("name key %s of the export does not hold a key of %d bytes", k.KeyID, blobcrypt.KeySize)
		}
		ks.names[k.Connection] = nk
		return nil
	}
	sk, err := blobcrypt.NewStorageKey(k.KeyID, raw)
	if err != nil {
		return refuse("storage key %s of the export does not hold a key of %d bytes", k.KeyID, blobcrypt.KeySize)
	}
	ks.storage[k.KeyID] = sk
	return nil
}

// openRefusal is the sentence of a key of the export that does not open with the ring.
func openRefusal(k exportKey, err error) error {
	root, _ := secrets.KeyIDOf(k.Sealed)
	if errors.Is(err, secrets.ErrAuthentication) {
		return refuse("key %s of the export does not open with the root key %q of the key ring: the export was changed, or the ring holds another key of that name", k.KeyID, root)
	}
	return refuse("key %s of the export does not open with the key ring (root key %q): %v", k.KeyID, root, err)
}

// Close destroys every key it holds.
func (ks *Keys) Close() {
	for _, sk := range ks.storage {
		sk.Key.Destroy()
	}
	for _, nk := range ks.names {
		nk.Destroy()
	}
}

// destroyedError is a record that names a key the export lists as destroyed: nothing brings that key back, and the object is
// unreadable with this export (storage.md R-STOR-101, R-STOR-103).
type destroyedError struct {
	id string
	at time.Time
}

func (e *destroyedError) Error() string {
	return fmt.Sprintf("the export lists the storage key %s as destroyed on %s; an export made before the destruction still opens it", e.id, e.at.Format(time.RFC3339))
}

// lookup is the KeyLookup of the export: a storage key it holds, an error for one it lists as destroyed, and nil for any other.
func (ks *Keys) lookup(id string) (*blobcrypt.StorageKey, error) {
	if sk, ok := ks.storage[id]; ok {
		return sk, nil
	}
	if at, ok := ks.destroyed[id]; ok {
		return nil, &destroyedError{id: id, at: at}
	}
	return nil, nil
}

// nameKey is the name key of a connection; status is empty when the export holds it, and otherwise the status of an object whose
// keyed name it would check (destroyed_key or unknown_key) with its detail.
func (ks *Keys) nameKey(connection string) (k blobcrypt.Key, status, detail string) {
	if nk, ok := ks.names[connection]; ok {
		return nk, "", ""
	}
	if id, ok := ks.destroyedNames[connection]; ok {
		return blobcrypt.Key{}, StatusDestroyedKey, fmt.Sprintf("the export lists the name key %s of connection %s as destroyed", id, connection)
	}
	return blobcrypt.Key{}, string(blobcrypt.ReasonUnknownKey), fmt.Sprintf("the export holds no name key of connection %s, which names its objects below keyed/", connection)
}

// readExport reads and checks an export (R-STOR-085, R-STOR-103): one JSON document of the format, whose rows are rows the table can
// hold, a destroyed one without its sealed value. A file cut short, of another format or with a row the table would refuse is a
// Refusal; the message quotes nothing of the file but member names and identifiers.
func readExport(path string) (exportDocument, error) {
	f, err := os.Open(path) //nolint:gosec // G304: the operator names the export file with --keys
	if err != nil {
		return exportDocument{}, refuse("the export of the storage keys %s cannot be read: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxExport+1))
	if err != nil {
		return exportDocument{}, refuse("the export of the storage keys %s cannot be read: %v", path, err)
	}
	if len(data) > maxExport {
		return exportDocument{}, refuse("the file %s is larger than %d bytes; an export of the storage keys is a few kilobytes", path, maxExport)
	}
	var d exportDocument
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return exportDocument{}, refuse("the file %s is not an export of the storage keys (JSON of the format %s): %s", path, keys.DocumentFormat, jsonProblem(err))
	}
	if dec.More() {
		return exportDocument{}, refuse("the file %s holds more than one JSON document; an export is one", path)
	}
	if d.Format != keys.DocumentFormat {
		return exportDocument{}, refuse("the file %s is of the format %q, not %q", path, d.Format, keys.DocumentFormat)
	}
	return d, checkExportKeys(path, d.Keys)
}

// jsonProblem names what the decoder found without quoting the file.
func jsonProblem(err error) string {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return "it ends before the export does: it was cut short"
	case errors.As(err, &syn):
		return fmt.Sprintf("invalid JSON at byte %d", syn.Offset)
	case errors.As(err, &typ):
		return fmt.Sprintf("the member %s is not a %s", typ.Field, typ.Type)
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return "it has the member " + strings.TrimPrefix(err.Error(), "json: unknown field ") + ", which an export has not"
	}
	return "it does not decode"
}

// checkExportKeys holds the rows of an export to what storage_keys holds (R-STOR-083, Q-STOR-324, R-STOR-097): an identifier that
// names the row's kind, connection and generation, one of the three states, a sealed value on every row that is not destroyed and
// none with a time of destruction on one that is, and one row per identifier.
func checkExportKeys(path string, list []exportKey) error {
	seen := map[string]bool{}
	for i, k := range list {
		id, ok := keys.ParseID(k.KeyID)
		switch {
		case !ok:
			return refuse("key %d of the export %s has no identifier of storage_keys (sk-<connection>-<generation>-<8 hex> or nk-<connection>-<8 hex>)", i+1, path)
		case id.Kind != k.Kind || id.Connection != k.Connection || id.Generation != k.Generation:
			return refuse("key %s of the export %s says connection %q, kind %q and generation %d, which its identifier does not name; the file was changed", k.KeyID, path, k.Connection, k.Kind, k.Generation)
		case seen[k.KeyID]:
			return refuse("the export %s lists key %s twice", path, k.KeyID)
		}
		seen[k.KeyID] = true
		switch k.State {
		case StateDestroyed:
			if k.Sealed != "" || k.DestroyedAt == nil {
				return refuse("key %s of the export %s is destroyed, so it has a time of destruction (destroyedAt) and no sealed value", k.KeyID, path)
			}
		case keys.StateActive, keys.StateRetired:
			if !secrets.IsSealed(k.Sealed) || k.DestroyedAt != nil {
				return refuse("key %s of the export %s holds no sealed value of the form v1:<root key>:<base64>", k.KeyID, path)
			}
		default:
			return refuse("key %s of the export %s has the state %q, not active, retired or destroyed", k.KeyID, path, k.State)
		}
	}
	return nil
}

// loadRing reads the key ring of the directory dir: every file <key id>.key of it (not of its subdirectories), in the order of
// their names, each holding 32 bytes or their base64 (R-ARCH-021). A Kubernetes Secret mounted as a volume is such a directory.
func loadRing(dir string) (*secrets.KeyRing, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, refuse("the key ring directory %s cannot be read: %v", dir, err)
	}
	var files []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".key") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			files = append(files, p)
		}
	}
	if len(files) == 0 {
		return nil, refuse("the directory %s holds no key file of the key ring (<key id>.key, architecture.md R-ARCH-021)", dir)
	}
	sort.Strings(files)
	ring, err := secrets.Load(config.Secrets{EncryptionKeyFiles: files, ActiveKeyID: config.KeyIDOf(files[0])})
	if err != nil {
		return nil, refuse("the key ring in %s: %v", dir, err)
	}
	return ring, nil
}
