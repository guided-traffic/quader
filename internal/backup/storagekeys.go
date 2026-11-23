package backup

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"

	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/backup/target"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	storagekeys "github.com/guided-traffic/quader/internal/blob/keys"
)

// The storage keys a backup holds (backup-and-restore.md R-BAK-031): the rows of its own storage_keys, read from the table file of
// the backup, sealed as they lie under the root keys of the backup's time. A restore opens them with the key ring it holds before it
// writes a blob, and `quader backup verify --deep` checks the identifier of every encrypted pool object against them, opening them
// only with --key-ring.

// StorageKeysTable is the table whose file holds a backup's storage keys (storage.md R-STOR-083).
const StorageKeysTable = "storage_keys"

// SealedKey is one row of a backup's storage_keys.
type SealedKey struct {
	ID         string
	Connection string
	Kind       string
	Generation int
	State      string
	// Sealed is the value of sealed_key, empty for a destroyed key, whose row keeps no value (storage.md R-STOR-097).
	Sealed string
}

// Destroyed reports a key whose row says it was destroyed: nothing opens it (storage.md R-STOR-097, R-STOR-102).
func (k SealedKey) Destroyed() bool { return k.State == "destroyed" || k.Sealed == "" }

// SealedKeys is a backup's storage keys by identifier.
type SealedKeys map[string]SealedKey

// maxKeyRows bounds the rows read from a backup's storage_keys: a key per connection and generation, at most eight connections.
const maxKeyRows = 1 << 16

// ReadBackupKeys reads the rows of storage_keys from the table file of the backup m (R-BAK-031). A backup whose manifest lists no
// such table (a database without the table) holds no key. The file is checked against the manifest's size and SHA-256.
func ReadBackupKeys(ctx context.Context, st target.Store, l format.Layout, m *format.Manifest) (SealedKeys, error) {
	t, ok := m.Table(StorageKeysTable)
	if !ok {
		return SealedKeys{}, nil
	}
	idx := map[string]int{}
	for i, c := range t.Columns {
		idx[c[0]] = i
	}
	for _, c := range []string{"key_id", "connection_id", "kind", "generation", "state", "sealed_key"} {
		if _, ok := idx[c]; !ok {
			return nil, fmt.Errorf("the table file of storage_keys of backup %s has no column %s", m.BackupID, c)
		}
	}
	rc, _, err := st.Get(ctx, l.Dir(m.BackupID)+t.File)
	if err != nil {
		return nil, fmt.Errorf("the storage keys of backup %s: %w", m.BackupID, err)
	}
	defer func() { _ = rc.Close() }()
	sum := format.NewSumWriter(io.Discard)
	zr, err := zstd.NewReader(io.TeeReader(rc, sum), zstd.WithDecoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	out := SealedKeys{}
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		if len(out) >= maxKeyRows {
			return nil, fmt.Errorf("the storage keys of backup %s hold more than %d rows", m.BackupID, maxKeyRows)
		}
		f := strings.Split(sc.Text(), "\t")
		if len(f) != len(t.Columns) {
			return nil, fmt.Errorf("a row of storage_keys of backup %s has %d columns, the manifest names %d", m.BackupID, len(f), len(t.Columns))
		}
		k := SealedKey{
			ID: copyText(f[idx["key_id"]]), Connection: copyText(f[idx["connection_id"]]), Kind: copyText(f[idx["kind"]]),
			State: copyText(f[idx["state"]]), Sealed: copyText(f[idx["sealed_key"]]),
		}
		k.Generation, _ = strconv.Atoi(copyText(f[idx["generation"]]))
		out[k.ID] = k
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("the storage keys of backup %s: %w", m.BackupID, err)
	}
	if _, err := io.Copy(io.Discard, io.TeeReader(rc, sum)); err != nil {
		return nil, err
	}
	if sum.Bytes() != t.CompressedBytes || sum.SHA256() != t.SHA256 {
		return nil, fmt.Errorf("%w: the table file of storage_keys of backup %s does not match its manifest", ErrPoolDamaged, m.BackupID)
	}
	return out, nil
}

// copyText is a field of PostgreSQL's COPY text format: \N is NULL (empty here), and a backslash escapes a tab, a newline, a
// carriage return or a backslash. The values of storage_keys (identifiers, states, sealed values of base64) need no other escape.
func copyText(f string) string {
	if f == `\N` {
		return ""
	}
	if !strings.Contains(f, `\`) {
		return f
	}
	var b strings.Builder
	for i := 0; i < len(f); i++ {
		if f[i] != '\\' || i+1 == len(f) {
			b.WriteByte(f[i])
			continue
		}
		i++
		switch f[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		default:
			b.WriteByte(f[i])
		}
	}
	return b.String()
}

// OpenedKeys is the storage keys of a backup opened with a key ring: the lookup a reader of the backup's encrypted pool objects
// passes to blobcrypt.Open, per connection, and the name keys. Close destroys every key.
type OpenedKeys struct {
	keys map[string]*blobcrypt.StorageKey
}

// ErrDestroyedKey is a key of a backup whose row says it was destroyed: no pool object wrapped under it can be read (storage.md
// R-STOR-097, R-STOR-102).
var ErrDestroyedKey = errors.New("the storage key was destroyed")

// Open opens every key of the backup that is not destroyed with the ring. A key the ring cannot open is a refusal that names the
// identifier and the root key (storage keys' Refusal).
func (bk SealedKeys) Open(ring storagekeys.Sealer) (*OpenedKeys, error) {
	o := &OpenedKeys{keys: map[string]*blobcrypt.StorageKey{}}
	for id, k := range bk {
		if k.Destroyed() {
			continue
		}
		sk, err := storagekeys.OpenStorageKey(ring, id, k.Sealed)
		if err != nil {
			o.Close()
			return nil, err
		}
		o.keys[id] = sk
	}
	return o, nil
}

// Lookup is the lookup of the storage keys of one connection: a record that names a key of another connection, or one the backup
// does not hold, is unknown_key (R-STOR-077 step 2).
func (o *OpenedKeys) Lookup(connection string) blobcrypt.KeyLookup {
	return func(id string) (*blobcrypt.StorageKey, error) {
		k, ok := storagekeys.ParseID(id)
		if !ok || k.Connection != connection || k.Kind != storagekeys.KindStorage {
			return nil, nil
		}
		return o.keys[id], nil
	}
}

// Key is the opened key of an identifier, nil when the backup holds none or it is destroyed.
func (o *OpenedKeys) Key(id string) *blobcrypt.StorageKey { return o.keys[id] }

// NameKey is the name key of a connection (nk-<connection>-…), nil when the backup holds none.
func (o *OpenedKeys) NameKey(connection string) *blobcrypt.StorageKey {
	for id, sk := range o.keys {
		if k, ok := storagekeys.ParseID(id); ok && k.Kind == storagekeys.KindName && k.Connection == connection {
			return sk
		}
	}
	return nil
}

// Close destroys every key.
func (o *OpenedKeys) Close() {
	if o == nil {
		return
	}
	for _, k := range o.keys {
		k.Key.Destroy()
	}
	o.keys = nil
}
