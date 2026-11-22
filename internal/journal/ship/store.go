package ship

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // the payload checksum a connection with payload_checksum md5 asks for, not a security digest
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/journal"
)

// maxObject bounds what a read accepts. An entry is a few hundred bytes, an update entry a few kilobytes (R-HA-054); a larger
// object below the journal prefix is not one of ours.
const maxObject = 4 << 20

// ConnectionStore is the journal's store on one connection, over the connection's client as the blob store holds it: its guard,
// so the circuit breaker and the request metrics with the label connection see the shipper's writes (storage.md R-STOR-059).
// The writer uses the runtime credential, which is why it can be asked to rewrite an object but never to delete one.
type ConnectionStore struct {
	objects  blob.ObjectStore
	checksum string
}

var _ journal.Store = (*ConnectionStore)(nil)

// NewConnectionStore wraps a connection's client; payloadChecksum is the connection's payload_checksum (sha256, md5 or none),
// which every write carries for the store to verify (R-STOR-006).
func NewConnectionStore(objects blob.ObjectStore, payloadChecksum string) *ConnectionStore {
	return &ConnectionStore{objects: objects, checksum: payloadChecksum}
}

// Put writes an object. The same key with the same bytes is harmless.
func (s *ConnectionStore) Put(ctx context.Context, key string, body []byte) error {
	var sum blob.Checksum
	switch s.checksum {
	case "sha256":
		h := sha256.Sum256(body)
		sum = blob.Checksum{Algorithm: "sha256", Value: h[:]}
	case "md5":
		h := md5.Sum(body) //nolint:gosec // the store's payload checksum
		sum = blob.Checksum{Algorithm: "md5", Value: h[:]}
	}
	return s.objects.Put(ctx, key, bytes.NewReader(body), int64(len(body)), sum, nil)
}

// List calls fn for every key below prefix in lexicographic order.
func (s *ConnectionStore) List(ctx context.Context, prefix string, fn func(key string) error) error {
	token := ""
	for {
		page, err := s.objects.List(ctx, prefix, blob.ListOptions{Token: token})
		if err != nil {
			return err
		}
		for _, o := range page.Objects {
			if err := fn(o.Key); err != nil {
				return err
			}
		}
		if page.Next == "" {
			return nil
		}
		token = page.Next
	}
}

// Get reads an object.
func (s *ConnectionStore) Get(ctx context.Context, key string) ([]byte, error) {
	rc, _, err := s.objects.Get(ctx, key, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(io.LimitReader(rc, maxObject+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", key, err)
	}
	if len(b) > maxObject {
		return nil, fmt.Errorf("journal object %s is larger than %d bytes", key, maxObject)
	}
	return b, nil
}
