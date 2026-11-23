package blob

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// ErrPlaintextRefused is the refusal of a write of a blob's object on an encrypted connection that does not carry the record of the
// stored format (R-STOR-075, R-STOR-080): such a write would put bytes into the bucket that the connection's reader cannot open and,
// from every writer that does not know the format (the reconciliation's stream, the scrub's repair), the blob in the clear.
var ErrPlaintextRefused = errors.New("an encrypted connection takes a blob's object only in the stored format, with its record")

// sealedOnly is the client of an encrypted connection as every caller sees it ([Connection.Objects]). A write whose key is a blob's
// object (below <prefix>/sha256/ or <prefix>/keyed/) is refused before it is sent unless its metadata is a record of the stored
// format (R-STOR-077): Put, CreateMultipart and a Copy that replaces the metadata; a Copy that keeps the source's metadata and a copy
// from another bucket are refused there altogether, since a server-side copy into an encrypted connection would carry another
// connection's bytes (Q-STOR-308). Everything else passes as it is: the staging area of the sessions, whose objects the session seals
// itself (R-STOR-081), the marker, the probes and the journal, which stay plain (Q-STOR-315).
type sealedOnly struct {
	ObjectStore
	families []string
}

// sealOnly wraps the client of an encrypted connection whose prefix is prefix.
func sealOnly(objects ObjectStore, prefix string) ObjectStore {
	if _, done := objects.(*sealedOnly); done {
		return objects
	}
	return &sealedOnly{ObjectStore: objects, families: []string{KeyPrefix(prefix), blobcrypt.KeyedPrefix(prefix)}}
}

// blobKey reports whether key names a blob's object.
func (s *sealedOnly) blobKey(key string) bool {
	for _, f := range s.families {
		if strings.HasPrefix(key, f) {
			return true
		}
	}
	return false
}

// isRecord reports whether md carries the four keys of a record of the stored format (R-STOR-077). It does not open it: the
// writers of the store make records, and this guard only keeps every other writer out.
func isRecord(md Metadata) bool {
	return md[blobcrypt.MetaFormat] == blobcrypt.Format && md[blobcrypt.MetaKey] != "" && md[blobcrypt.MetaWrap] != "" &&
		md[blobcrypt.MetaCommitment] != ""
}

func (s *sealedOnly) refuse(op, key string) error {
	return &StoreError{Op: op, Key: key, Code: "PlaintextRefused", Kind: KindOther, Err: ErrPlaintextRefused}
}

// Put implements ObjectStore.
func (s *sealedOnly) Put(ctx context.Context, key string, body io.ReadSeeker, size int64, sum Checksum, md Metadata) error {
	if s.blobKey(key) && !isRecord(md) {
		return s.refuse("PutObject", key)
	}
	return s.ObjectStore.Put(ctx, key, body, size, sum, md)
}

// CreateMultipart implements ObjectStore.
func (s *sealedOnly) CreateMultipart(ctx context.Context, key, algorithm string, md Metadata) (string, error) {
	if s.blobKey(key) && !isRecord(md) {
		return "", s.refuse("CreateMultipartUpload", key)
	}
	return s.ObjectStore.CreateMultipart(ctx, key, algorithm, md)
}

// Copy implements ObjectStore.
func (s *sealedOnly) Copy(ctx context.Context, srcKey, dstKey string, size int64, md Metadata) error {
	if s.blobKey(dstKey) && !isRecord(md) {
		return s.refuse("CopyObject", dstKey)
	}
	return s.ObjectStore.Copy(ctx, srcKey, dstKey, size, md)
}

// Rejecting forwards the circuit of the wrapped client ([Guard]); a client without one never rejects.
func (s *sealedOnly) Rejecting() (time.Duration, bool) {
	if r, ok := s.ObjectStore.(interface{ Rejecting() (time.Duration, bool) }); ok {
		return r.Rejecting()
	}
	return 0, false
}

// Circuit forwards the state of the wrapped client's circuit.
func (s *sealedOnly) Circuit() string {
	if g, ok := s.ObjectStore.(interface{ Circuit() string }); ok {
		return g.Circuit()
	}
	return CircuitClosed
}

// Fetcher forwards the HTTP client the wrapped client's links are fetched with (FetcherOf), nil when it has none.
func (s *sealedOnly) Fetcher() Fetcher { return FetcherOf(s.ObjectStore) }

// Location implements [BucketCopier]: the wrapped client's, empty when it has none.
func (s *sealedOnly) Location() (endpoint, bucket string) {
	if c, ok := s.ObjectStore.(BucketCopier); ok {
		return c.Location()
	}
	return "", ""
}

// CopyFrom implements [BucketCopier]: a copy from another bucket into a blob's object of an encrypted connection is refused
// (Q-STOR-308); any other key passes to the wrapped client.
func (s *sealedOnly) CopyFrom(ctx context.Context, srcBucket, srcKey, dstKey string, size int64, md Metadata) error {
	if s.blobKey(dstKey) {
		return s.refuse("CopyObject", dstKey)
	}
	c, ok := s.ObjectStore.(BucketCopier)
	if !ok {
		return &StoreError{Op: "CopyObject", Key: srcKey, Code: "NotImplemented", Status: 501, Kind: KindOther, Err: errNoBucketCopy}
	}
	return c.CopyFrom(ctx, srcBucket, srcKey, dstKey, size, md)
}

// ListMultipartPage implements [MultipartPager] as the [Guard] does: paged when the wrapped client pages, whole otherwise.
func (s *sealedOnly) ListMultipartPage(ctx context.Context, prefix string, after MultipartMarker, max int) ([]MultipartUpload, MultipartMarker, error) {
	if p, ok := s.ObjectStore.(MultipartPager); ok {
		return p.ListMultipartPage(ctx, prefix, after, max)
	}
	if after != (MultipartMarker{}) {
		return nil, MultipartMarker{}, nil
	}
	ups, err := s.ListMultipart(ctx, prefix)
	return ups, MultipartMarker{}, err
}
