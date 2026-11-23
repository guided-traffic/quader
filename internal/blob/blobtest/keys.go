package blobtest

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// Keys is the storage keys of a replica as the tests of the jobs and the backup give them to encrypted connections (blob.StorageKeys,
// storage.md R-STOR-083): per connection the storage keys it holds, the active one and a name key. It is safe for concurrent use.
type Keys struct {
	mu     sync.Mutex
	conns  map[string]*heldConn
	random func([]byte) (int, error)
}

type heldConn struct {
	keys   map[string]*blobcrypt.StorageKey
	active string
	name   *blobcrypt.Key
}

// NewKeys returns an empty set.
func NewKeys() *Keys { return &Keys{conns: map[string]*heldConn{}, random: rand.Read} }

// Draw draws a storage key of the generation for the connection, makes it the active one and, on its first key, a name key; it
// returns the storage key's identifier (sk-<connection>-<generation>-<8 hex>, the rule of R-STOR-083).
func (k *Keys) Draw(connection string, generation int) string {
	k.mu.Lock()
	defer k.mu.Unlock()
	c := k.conns[connection]
	if c == nil {
		c = &heldConn{keys: map[string]*blobcrypt.StorageKey{}}
		nk, err := blobcrypt.NewKey(rand.Reader)
		if err != nil {
			panic(err)
		}
		c.name = &nk
		k.conns[connection] = c
	}
	var tag [4]byte
	var raw [blobcrypt.KeySize]byte
	if _, err := k.random(tag[:]); err != nil {
		panic(err)
	}
	if _, err := k.random(raw[:]); err != nil {
		panic(err)
	}
	id := fmt.Sprintf("sk-%s-%d-%x", connection, generation, tag)
	sk, err := blobcrypt.NewStorageKey(id, raw[:])
	if err != nil {
		panic(err)
	}
	c.keys[id], c.active = sk, id
	return id
}

// Add gives the connection a storage key the test made elsewhere (a backup's key, opened with its key ring), as its active one, and,
// when nameKey is not nil, that name key.
func (k *Keys) Add(connection string, sk *blobcrypt.StorageKey, nameKey *blobcrypt.Key) {
	k.mu.Lock()
	defer k.mu.Unlock()
	c := k.conns[connection]
	if c == nil {
		c = &heldConn{keys: map[string]*blobcrypt.StorageKey{}}
		k.conns[connection] = c
	}
	c.keys[sk.ID], c.active = sk, sk.ID
	if nameKey != nil {
		c.name = nameKey
	}
}

// Forget drops a storage key the replica held: a record that names it is unknown_key from then on (Q-STOR-311).
func (k *Keys) Forget(connection, id string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if c := k.conns[connection]; c != nil {
		delete(c.keys, id)
		if c.active == id {
			c.active = ""
		}
	}
}

// Active implements blob.StorageKeys.
func (k *Keys) Active(_ context.Context, connection string) (*blobcrypt.StorageKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if c := k.conns[connection]; c != nil && c.keys[c.active] != nil {
		return c.keys[c.active], nil
	}
	return nil, errors.New("the replica holds no active storage key of the connection")
}

// NameKey implements blob.StorageKeys.
func (k *Keys) NameKey(connection string) (blobcrypt.Key, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if c := k.conns[connection]; c != nil && c.name != nil {
		return *c.name, nil
	}
	return blobcrypt.Key{}, errors.New("the replica holds no name key of the connection")
}

// Lookup implements blob.StorageKeys: nil for an identifier the connection does not hold.
func (k *Keys) Lookup(_ context.Context, connection string) blobcrypt.KeyLookup {
	return func(id string) (*blobcrypt.StorageKey, error) {
		k.mu.Lock()
		defer k.mu.Unlock()
		if c := k.conns[connection]; c != nil {
			return c.keys[id], nil
		}
		return nil, nil
	}
}
