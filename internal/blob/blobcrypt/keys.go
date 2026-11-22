package blobcrypt

import (
	"crypto/cipher"
	"crypto/subtle"
	"errors"
	"io"
	"log/slog"
	"strconv"
)

// errDestroyed is returned by every use of a Key after its Destroy.
var errDestroyed = errors.New("blobcrypt: the key was destroyed")

// Key is a secret of 32 bytes: a data key, a storage key or a name key. Its bytes live in memory the package owns; copies of
// a Key share them, so Destroy clears them for every copy, and a destroyed Key refuses every use. A Key is not destroyed
// while another goroutine uses it. String, GoString and LogValue render nothing of it.
type Key struct {
	m *keyMaterial
}

type keyMaterial struct {
	b    [KeySize]byte
	live bool
}

// NewKey draws a key from random, which is crypto/rand.Reader everywhere but in the vectors.
func NewKey(random io.Reader) (Key, error) {
	m := &keyMaterial{live: true}
	if _, err := io.ReadFull(random, m.b[:]); err != nil {
		clear(m.b[:])
		return Key{}, errors.New("blobcrypt: drawing a key: " + err.Error())
	}
	return Key{m: m}, nil
}

// KeyFromBytes copies a key of 32 bytes, for a key opened from a sealed row; the caller clears b.
func KeyFromBytes(b []byte) (Key, error) {
	if len(b) != KeySize {
		return Key{}, errors.New("blobcrypt: a key is " + strconv.Itoa(KeySize) + " bytes, not " + strconv.Itoa(len(b)))
	}
	m := &keyMaterial{live: true}
	copy(m.b[:], b)
	return Key{m: m}, nil
}

// Bytes returns a copy of the key's bytes, for sealing it into a row with the key ring (an upload session's data key,
// R-STOR-081); the caller clears the copy.
func (k Key) Bytes() ([]byte, error) {
	b, err := k.bytes()
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), b...), nil
}

// Destroy overwrites the key's bytes. The AES key schedules already built from it stay in memory until they are collected.
func (k Key) Destroy() {
	if k.m != nil {
		clear(k.m.b[:])
		k.m.live = false
	}
}

// Equal compares two keys' bytes in constant time; a destroyed or empty key equals nothing.
func (k Key) Equal(o Key) bool {
	a, errA := k.bytes()
	b, errB := o.bytes()
	return errA == nil && errB == nil && subtle.ConstantTimeCompare(a, b) == 1
}

// bytes returns the key's own bytes, not a copy.
func (k Key) bytes() ([]byte, error) {
	if k.m == nil || !k.m.live {
		return nil, errDestroyed
	}
	return k.m.b[:], nil
}

func (k Key) String() string       { return "blobcrypt.Key(redacted)" }
func (k Key) GoString() string     { return k.String() }
func (k Key) LogValue() slog.Value { return slog.StringValue(k.String()) }

// aead returns AES-256-GCM under the key HKDF(k, "", label) derives: the segment key or the key of the staging tails. The
// derived bytes are cleared once the cipher holds them.
func (k Key) aead(label string) (cipher.AEAD, error) {
	b, err := k.bytes()
	if err != nil {
		return nil, err
	}
	return newGCM(derive(b, nil, label))
}

// StorageKey is a storage key of a connection with its identifier (R-STOR-083). String and LogValue render the identifier
// only.
type StorageKey struct {
	ID  string
	Key Key
}

// NewStorageKey copies a storage key opened from its row; the caller clears raw.
func NewStorageKey(id string, raw []byte) (*StorageKey, error) {
	if !ValidKeyID(id) {
		return nil, errors.New("blobcrypt: " + strconv.Quote(id) + " is not a key identifier (3 to 64 characters of [a-z0-9-])")
	}
	k, err := KeyFromBytes(raw)
	if err != nil {
		return nil, err
	}
	return &StorageKey{ID: id, Key: k}, nil
}

func (s *StorageKey) String() string       { return "storage key " + s.ID }
func (s *StorageKey) GoString() string     { return s.String() }
func (s *StorageKey) LogValue() slog.Value { return slog.StringValue(s.String()) }

// ValidKeyID says whether id is a key identifier as quader-key carries it: 3 to 64 characters of [a-z0-9-] (R-STOR-077).
func ValidKeyID(id string) bool {
	if len(id) < 3 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// DataKey is the data key of one stored object as its record binds it: the key, the blob's digest and plaintext size, and the
// identifier of the storage key that wrapped it (R-STOR-077). Open returns one; a writer builds one to Wrap. String and
// LogValue render the size and the identifier, never the key.
type DataKey struct {
	Key    Key
	KeyID  string
	Digest [32]byte
	Size   int64
}

// Commitment is C = HKDF(DEK, "", FMT ‖ " commitment"), which the record carries (R-STOR-077).
func (d DataKey) Commitment() ([32]byte, error) { return commitment(d.Key) }

func (d DataKey) String() string {
	return "blobcrypt.DataKey(redacted, " + strconv.FormatInt(d.Size, 10) + " bytes, wrapped by " + d.KeyID + ")"
}
func (d DataKey) GoString() string     { return d.String() }
func (d DataKey) LogValue() slog.Value { return slog.StringValue(d.String()) }

func commitment(k Key) ([32]byte, error) {
	var c [32]byte
	b, err := k.bytes()
	if err != nil {
		return c, err
	}
	copy(c[:], derive(b, nil, labelCommitment))
	return c, nil
}

// NewSeed draws the seed of one write from random, which is crypto/rand.Reader everywhere but in the vectors (R-STOR-080).
// A seed is not secret, since the nonces it yields are stored, but it is never logged.
func NewSeed(random io.Reader) ([32]byte, error) {
	var s [32]byte
	if _, err := io.ReadFull(random, s[:]); err != nil {
		return s, errors.New("blobcrypt: drawing a seed: " + err.Error())
	}
	return s, nil
}
