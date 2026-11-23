package keys

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// The kinds of a row of storage_keys (R-STOR-083).
const (
	// KindStorage is a storage key: it wraps the data keys of the objects of its connection, one active per connection.
	KindStorage = "storage"
	// KindName is a name key: it turns a digest into an object's keyed name (R-STOR-086), one per connection with keyed names.
	KindName = "name"
)

// The states of a row.
const (
	// StateActive wraps new objects (a storage key) or names them (a name key).
	StateActive = "active"
	// StateRetired opens what it wrapped and wraps nothing new.
	StateRetired = "retired"
	// StateDestroyed is a key that is gone: its row keeps the identifier, the connection, the kind and the generation, its sealed
	// value is NULL, and nothing opens it again (R-STOR-097).
	StateDestroyed = "destroyed"
)

// MaxGeneration is the highest generation of a storage key the table holds.
const MaxGeneration = 999999

// connectionPattern is a connection id (R-STOR-050), the pattern of storage_connections.id.
func connectionPattern() *regexp.Regexp { return regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`) }

// keyIDPattern is the CHECK of storage_keys.key_id (R-STOR-083).
func keyIDPattern() *regexp.Regexp {
	return regexp.MustCompile(`^(sk-[a-z][a-z0-9-]{0,30}[a-z0-9]-[1-9][0-9]{0,5}|nk-[a-z][a-z0-9-]{0,30}[a-z0-9])-[0-9a-f]{8}$`)
}

// ID is what an identifier names: its kind, its connection and its generation (1 for a name key). The identifier carries all three
// (Q-STOR-324), so a sealed key, which opens only under its own identifier, cannot be moved to another connection.
type ID struct {
	Kind       string
	Connection string
	Generation int
}

// ParseID reads an identifier: sk-<connection>-<generation>-<8 hex> or nk-<connection>-<8 hex>. ok is false for anything else.
func ParseID(id string) (ID, bool) {
	if !keyIDPattern().MatchString(id) {
		return ID{}, false
	}
	rest := id[3 : len(id)-9] // without the kind's prefix and "-<8 hex>"
	if strings.HasPrefix(id, "nk-") {
		return ID{Kind: KindName, Connection: rest, Generation: 1}, connectionPattern().MatchString(rest)
	}
	cut := strings.LastIndexByte(rest, '-')
	if cut < 0 {
		return ID{}, false
	}
	gen, err := strconv.Atoi(rest[cut+1:])
	if err != nil || gen < 1 || gen > MaxGeneration || !connectionPattern().MatchString(rest[:cut]) {
		return ID{}, false
	}
	return ID{Kind: KindStorage, Connection: rest[:cut], Generation: gen}, true
}

// newID draws the identifier of a new key: the kind's prefix, the connection, the generation of a storage key and 8 random hex
// digits, so that a key drawn again for the same connection and generation (after a database was lost) never opens an object of its
// predecessor by mistake (R-STOR-083).
func newID(kind, connection string, generation int, random io.Reader) (string, error) {
	var b [4]byte
	if _, err := io.ReadFull(random, b[:]); err != nil {
		return "", fmt.Errorf("drawing the identifier of a key: %w", err)
	}
	var id string
	switch kind {
	case KindStorage:
		id = "sk-" + connection + "-" + strconv.Itoa(generation) + "-" + hex.EncodeToString(b[:])
	case KindName:
		id = "nk-" + connection + "-" + hex.EncodeToString(b[:])
	default:
		return "", errors.New("no kind of key " + strconv.Quote(kind))
	}
	if got, ok := ParseID(id); !ok || got.Connection != connection || got.Generation != generation && kind == KindStorage {
		return "", fmt.Errorf("connection %q and generation %d make no key identifier", connection, generation)
	}
	return id, nil
}
