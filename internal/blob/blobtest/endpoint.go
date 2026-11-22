package blobtest

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
)

// Endpoint is the in-memory stores of the buckets of one endpoint: a store of the set copies an object of another bucket of
// the set into its own bucket (blob.BucketCopier), as the server-side copy between two connections on one endpoint does
// (R-STOR-035, R-STOR-056).
type Endpoint struct {
	name    string
	mu      sync.Mutex
	buckets map[string]*Store
}

// NewEndpoint returns an endpoint without buckets; name is what Location reports, as a comparable endpoint
// (https://<name>).
func NewEndpoint(name string) *Endpoint {
	return &Endpoint{name: "https://" + name, buckets: map[string]*Store{}}
}

// Bucket returns the store of a bucket of the endpoint, made on first use.
func (e *Endpoint) Bucket(name string) *Store {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s, ok := e.buckets[name]; ok {
		return s
	}
	s := New()
	s.endpoint, s.bucket = e, name
	e.buckets[name] = s
	return s
}

func (e *Endpoint) store(bucket string) (*Store, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.buckets[bucket]
	return s, ok
}

var _ blob.BucketCopier = (*Store)(nil)

// Location implements blob.BucketCopier: the endpoint and the bucket, empty for a store of its own.
func (s *Store) Location() (endpoint, bucket string) {
	if s.endpoint == nil {
		return "", ""
	}
	return s.endpoint.name, s.bucket
}

// CopyFrom implements blob.BucketCopier: the object srcKey of the bucket srcBucket of the same endpoint is copied to dstKey,
// its metadata kept when md is nil and replaced otherwise. A store of its own, or a bucket the endpoint does not have,
// answers NotImplemented and NoSuchBucket; a missing source is blob.ErrNotFound. The fault hook sees the operation
// "copy_from" with the source key.
func (s *Store) CopyFrom(_ context.Context, srcBucket, srcKey, dstKey string, size int64, md blob.Metadata) error {
	if err := s.enter("copy_from", srcKey); err != nil {
		return err
	}
	if s.endpoint == nil {
		return &blob.StoreError{Op: "CopyObject", Key: srcKey, Code: "NotImplemented", Status: 501, Kind: blob.KindOther}
	}
	src, ok := s.endpoint.store(srcBucket)
	if !ok {
		return &blob.StoreError{Op: "CopyObject", Key: srcKey, Code: "NoSuchBucket", Status: 404, Kind: blob.KindConfig}
	}
	src.mu.Lock()
	o, ok := src.objects[srcKey]
	var c *object
	if ok {
		c = copied(o, md, s.Now())
	}
	src.mu.Unlock()
	if !ok {
		return blob.ErrNotFound
	}
	if size >= 0 && int64(len(c.data)) != size {
		return fmt.Errorf("copy of %s/%s: the object has %d bytes, declared %d", srcBucket, srcKey, len(c.data), size)
	}
	s.mu.Lock()
	s.objects[dstKey] = c
	s.mu.Unlock()
	return nil
}

// SetMetadata replaces an object's metadata without counting, as a store that keeps none or another party would.
func (s *Store) SetMetadata(key string, md blob.Metadata) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.objects[key]; ok {
		o.meta = cloneMeta(md)
	}
}

// copied is the object a copy of o makes: its bytes, and its metadata unless md replaces it.
func copied(o *object, md blob.Metadata, now time.Time) *object {
	meta := o.meta
	if md != nil {
		meta = md
	}
	return &object{data: bytes.Clone(o.data), meta: cloneMeta(meta), modified: now}
}

func cloneMeta(md blob.Metadata) blob.Metadata {
	if len(md) == 0 {
		return nil
	}
	return maps.Clone(md)
}
