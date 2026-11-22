package keys

import (
	"errors"
	"fmt"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/secrets"
)

// The two sealed columns of the stage (R-STOR-083, R-ARCH-021), registered with the key ring so that its per-key counts, its canary,
// the compatibility check of the replicas' rings and the task secrets-reencrypt cover them.
var (
	// keyColumn is storage_keys.sealed_key: a storage key or name key sealed under its row's key_id.
	keyColumn = secrets.Register("storage_keys", "key_id", "sealed_key")
	// sessionColumn is upload_sessions.sealed_data_key: the data key of an open upload session on an encrypted connection, sealed
	// under the session's id (R-STOR-081, Q-STOR-307).
	sessionColumn = secrets.Register("upload_sessions", "id", "sealed_data_key")
)

// Column is storage_keys.sealed_key, for the walk of secrets-reencrypt and the key ring's status.
func Column() secrets.Column { return keyColumn }

// SessionColumn is upload_sessions.sealed_data_key, for the walk of secrets-reencrypt and the key ring's status.
func SessionColumn() secrets.Column { return sessionColumn }

// Sealer is what the package needs of the key ring: sealing a value for a row of a registered column under the active root key and
// opening one. *secrets.KeyRing is one.
type Sealer interface {
	Seal(c secrets.Column, row secrets.RowKey, plaintext []byte) (string, error)
	Open(c secrets.Column, row secrets.RowKey, sealed string) ([]byte, error)
}

// Resealer seals a value under the active root key when it lies under another (the walk of secrets-reencrypt). *secrets.KeyRing
// is one.
type Resealer interface {
	Reseal(c secrets.Column, row secrets.RowKey, sealed string) (string, bool, error)
}

// SealSessionKey seals the data key of the upload session id into the value of upload_sessions.sealed_data_key (R-STOR-081): any
// replica that takes the session's writer claim opens it, and it opens under no other session.
func SealSessionKey(ring Sealer, id string, dataKey blobcrypt.Key) (string, error) {
	raw, err := dataKey.Bytes()
	if err != nil {
		return "", err
	}
	defer clear(raw)
	return ring.Seal(sessionColumn, secrets.Row(id), raw)
}

// OpenSessionKey opens the data key of the upload session id from its row. The caller destroys the key when its request is done.
func OpenSessionKey(ring Sealer, id, sealed string) (blobcrypt.Key, error) {
	raw, err := ring.Open(sessionColumn, secrets.Row(id), sealed)
	if err != nil {
		return blobcrypt.Key{}, fmt.Errorf("the data key of upload session %s does not open with this replica's key ring: %w", id, err)
	}
	defer clear(raw)
	return blobcrypt.KeyFromBytes(raw)
}

// sealKey seals a key drawn for the row id of storage_keys and destroys the key: what lies in memory after the call is the seal.
func sealKey(ring Sealer, id string, k blobcrypt.Key) (string, error) {
	defer k.Destroy()
	raw, err := k.Bytes()
	if err != nil {
		return "", err
	}
	defer clear(raw)
	return ring.Seal(keyColumn, secrets.Row(id), raw)
}

// openKey opens the row id of storage_keys into a key that carries its identifier. A failure names the identifier and the root key
// the value is sealed with, never a byte of either value.
func openKey(ring Sealer, id, sealed string) (*blobcrypt.StorageKey, error) {
	raw, err := ring.Open(keyColumn, secrets.Row(id), sealed)
	if err != nil {
		return nil, openRefusal(id, sealed, err)
	}
	defer clear(raw)
	return blobcrypt.NewStorageKey(id, raw)
}

// openRefusal is the sentence of a key that does not open with the ring at hand.
func openRefusal(id, sealed string, err error) error {
	root, perr := secrets.KeyIDOf(sealed)
	switch {
	case perr != nil || errors.Is(err, secrets.ErrMalformed):
		return &Refusal{msg: fmt.Sprintf("key %s holds no sealed value of the form v1:<root key>:<base64>", id)}
	case errors.Is(err, secrets.ErrUnknownKey):
		return &Refusal{msg: fmt.Sprintf("key %s is sealed with the root key %q, which this key ring lacks; configure the key ring that holds it (secrets.encryption_key_files, architecture.md R-ARCH-021)", id, root)}
	case errors.Is(err, secrets.ErrAuthentication):
		return &Refusal{msg: fmt.Sprintf("key %s does not open with the root key %q of this key ring: the value was changed, or it was sealed for another identifier (a sealed key opens only under its own)", id, root)}
	}
	return fmt.Errorf("key %s: %w", id, err)
}

// Refusal is a state of the keys that retrying does not change and that an operator answers: a location that holds objects of keys
// the database lacks, a key the ring cannot open, an import that does not fit the database. Its message says what is missing and what
// to do; it never holds a byte of a key or of a sealed value.
type Refusal struct{ msg string }

func (r *Refusal) Error() string { return r.msg }

// IsRefusal reports whether err is a Refusal.
func IsRefusal(err error) bool {
	var r *Refusal
	return errors.As(err, &r)
}
