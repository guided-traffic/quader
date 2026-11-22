package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// ErrNotFound is returned by an ObjectStore for a missing key. It is not a
// failure of the store: the circuit breaker ignores it (R-STOR-033).
var ErrNotFound = errors.New("object not found")

// ErrCircuitOpen is returned by a Guard while its breaker is open: the blob
// operation fails fast (R-STOR-033, R-STOR-036).
var ErrCircuitOpen = errors.New("object store circuit is open")

// ErrorKind classifies a StoreError for the breaker and the startup check.
type ErrorKind uint8

// The kinds of StoreError.
const (
	// KindTransient: network errors, timeouts, 5xx, throttling and credential
	// retrieval failures. Counted by the breaker, retried by the startup check.
	KindTransient ErrorKind = iota
	// KindConfig: AccessDenied, NoSuchBucket, invalid region or credentials.
	// The startup check stops the process on these (R-STOR-035).
	KindConfig
	// KindOther: any other 4xx. Neither counted nor retried.
	KindOther
)

// StoreError is an error of the object store with its classification.
type StoreError struct {
	Op     string // the operation, for example "PutObject"
	Key    string // the key, when the operation has one
	Code   string // the store's error code, for example "AccessDenied"
	Status int    // the HTTP status, 0 when the request got no answer
	Kind   ErrorKind
	Err    error
}

func (e *StoreError) Error() string {
	s := "object store " + e.Op
	if e.Key != "" {
		s += " " + e.Key
	}
	if e.Code != "" {
		s += ": " + e.Code
	}
	if e.Status != 0 {
		s += fmt.Sprintf(" (HTTP %d)", e.Status)
	}
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}

// Unwrap returns the underlying error.
func (e *StoreError) Unwrap() error { return e.Err }

// IsTransient reports whether err counts against the circuit breaker.
func IsTransient(err error) bool {
	var se *StoreError
	return errors.As(err, &se) && se.Kind == KindTransient
}

// IsConfig reports whether err is a configuration or permission problem of the
// store that retrying does not fix.
func IsConfig(err error) bool {
	var se *StoreError
	return errors.As(err, &se) && se.Kind == KindConfig
}

// credentialCode reports whether a store error code names the credential as the cause: a key the store does not know, a
// signature that does not match the secret, an expired or malformed token. They are the "credential errors" of R-STOR-033
// that count against the circuit, unlike an AccessDenied for one operation or a missing bucket, which are answers about
// the request.
func credentialCode(code string) bool {
	switch code {
	case "InvalidAccessKeyId", "SignatureDoesNotMatch", "ExpiredToken", "InvalidToken", "AuthorizationHeaderMalformed":
		return true
	}
	return false
}

// IsCredentialRejection reports whether err is the store's refusal of the credential quader signed with (a revoked key,
// a wrong secret, an expired token).
func IsCredentialRejection(err error) bool {
	var se *StoreError
	return errors.As(err, &se) && se.Kind == KindConfig && credentialCode(se.Code)
}

// Checksum is the payload checksum a request carries for the store to verify
// (R-STOR-006, the payload_checksum of a connection).
type Checksum struct {
	// Algorithm is "sha256", "md5" or empty for none.
	Algorithm string
	Value     []byte
}

// Metadata is the user metadata of an object (R-STOR-062): keys of lower-case letters, digits and hyphens, the prefix
// "quader-" reserved for Quader, at most MaxMetadataBytes of keys and values in all. Stage 1 writes none; the start check
// establishes whether a connection keeps it and whether a copy replaces it (R-STOR-035), for the encryption of stage 2.
type Metadata map[string]string

// MaxMetadataBytes bounds the keys and values of one object's metadata: the 2 KiB AWS documents for user metadata.
const MaxMetadataBytes = 2048

// CheckMetadata refuses metadata that a store may not keep as written: a key outside [a-z0-9-]+, or more than
// MaxMetadataBytes of keys and values.
func CheckMetadata(md Metadata) error {
	n := 0
	for k, v := range md {
		if k == "" {
			return errors.New("metadata: an empty key")
		}
		for i := 0; i < len(k); i++ {
			if c := k[i]; (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return fmt.Errorf("metadata: the key %q is not [a-z0-9-]+", k)
			}
		}
		n += len(k) + len(v)
	}
	if n > MaxMetadataBytes {
		return fmt.Errorf("metadata: %d bytes of keys and values, at most %d", n, MaxMetadataBytes)
	}
	return nil
}

// ObjectInfo describes a stored object.
type ObjectInfo struct {
	Key string
	// Size is the object's size in bytes, or -1 when the store's answer did not
	// say (a response without Content-Length). Callers must not read -1 as an
	// empty or a damaged object.
	Size         int64
	ETag         string
	LastModified time.Time
	// Metadata is the object's user metadata as Head and Get return it, nil when it has none; List leaves it nil.
	Metadata Metadata
}

// ByteRange selects Length bytes starting at Offset; a negative Length means
// to the end of the object.
type ByteRange struct {
	Offset int64
	Length int64
}

// Part is a finished part of a multipart upload.
type Part struct {
	Number   int
	ETag     string
	Checksum Checksum
	Size     int64
}

// MultipartUpload is an unfinished multipart upload found in the bucket.
type MultipartUpload struct {
	Key       string
	UploadID  string
	Initiated time.Time
}

// ListOptions narrows a listing.
type ListOptions struct {
	// Delimiter groups keys below a common prefix into ListPage.Prefixes.
	Delimiter string
	// StartAfter lists keys after this one.
	StartAfter string
	// Token continues a listing from ListPage.Next.
	Token string
	// Max is the largest page; zero means the store's default.
	Max int
}

// ListPage is one page of ListObjectsV2.
type ListPage struct {
	Objects  []ObjectInfo
	Prefixes []string
	Next     string // empty when the listing is complete
}

// DeleteFailure is a key a batch delete did not remove.
type DeleteFailure struct {
	Key  string
	Code string
}

// ObjectStore is the object-store client quader needs (R-STOR-030). Keys are
// full keys including the configured prefix. Implementations return
// ErrNotFound for a missing key and a *StoreError for every other store error.
// The interface is small on purpose: internal/blob/s3 implements it, Guard
// decorates it and internal/blob/blobtest fakes it.
type ObjectStore interface {
	// Put writes body, size bytes long, with one request, and md as the object's metadata (nil for none). The body is
	// seekable so that a retry can send it again.
	Put(ctx context.Context, key string, body io.ReadSeeker, size int64, sum Checksum, md Metadata) error
	// CreateMultipart starts a multipart upload whose object gets md as its metadata (nil for none); algorithm is the
	// payload checksum algorithm the parts will carry ("sha256", "md5" or "").
	CreateMultipart(ctx context.Context, key, algorithm string, md Metadata) (uploadID string, err error)
	// UploadPart sends one part.
	UploadPart(ctx context.Context, key, uploadID string, number int, body io.ReadSeeker, size int64, sum Checksum) (Part, error)
	// CompleteMultipart makes the object visible.
	CompleteMultipart(ctx context.Context, key, uploadID string, parts []Part) error
	// AbortMultipart discards an upload and its parts.
	AbortMultipart(ctx context.Context, key, uploadID string) error
	// Copy copies the object srcKey, size bytes long, to dstKey inside the store, with no byte passing through the caller
	// (CopyObject up to 5 GiB, UploadPartCopy above). dstKey is replaced if it exists; a missing srcKey is ErrNotFound.
	// md nil keeps the source's metadata (x-amz-metadata-directive COPY); a value replaces it (REPLACE).
	Copy(ctx context.Context, srcKey, dstKey string, size int64, md Metadata) error
	// Head returns the size and modification time of an object.
	Head(ctx context.Context, key string) (ObjectInfo, error)
	// Get opens an object, optionally a range of it. The caller closes the body.
	Get(ctx context.Context, key string, rng *ByteRange) (io.ReadCloser, ObjectInfo, error)
	// Delete removes an object; a missing key is success.
	Delete(ctx context.Context, key string) error
	// DeleteMany removes up to 1000 objects with one request (DeleteObjects).
	// Keys the store reports as not deleted come back in failed.
	DeleteMany(ctx context.Context, keys []string) (failed []DeleteFailure, err error)
	// List lists keys below prefix (ListObjectsV2).
	List(ctx context.Context, prefix string, opts ListOptions) (ListPage, error)
	// ListMultipart lists unfinished multipart uploads below prefix.
	ListMultipart(ctx context.Context, prefix string) ([]MultipartUpload, error)
	// Presign signs a GET of the key as o says, with the store's runtime credential, and returns the link with its expiry
	// (R-STOR-072). It sends no request. A credential that ends within MinCredentialLife signs nothing (ErrCredentialExpiring).
	Presign(ctx context.Context, key string, o PresignOptions) (Link, error)
}

// PresignOptions is what a signed link is signed for (R-STOR-072, Q-STOR-293): its life, the three headers of the store's
// answer a link can ask for, and the origin it names.
type PresignOptions struct {
	// Life is how long the link is valid, at least a second; the signing credential's own expiry bounds it (LinkLife).
	Life time.Duration
	// ContentType, ContentDisposition and CacheControl are asked of the store's answer as response-content-type,
	// response-content-disposition and response-cache-control; empty asks for none.
	ContentType, ContentDisposition, CacheControl string
	// Address is the origin the link names, https://host[:port] (the public address of R-STOR-074); empty names the store's own
	// endpoint.
	Address string
}

// Link is a signed link and the instant it expires.
type Link struct {
	URL       string
	ExpiresAt time.Time
}

// MinCredentialLife is how much longer the signing credential has to live for a link to be signed (R-STOR-072, R-STOR-070
// step 13): a link that would end within it is not worth a round trip of the client.
const MinCredentialLife = 5 * time.Second

// ErrCredentialExpiring is the refusal of a link whose signing credential ends within MinCredentialLife.
var ErrCredentialExpiring = errors.New("the credential that signs the link expires within 5 seconds")

// LinkLife is the life of a link signed now for life by a credential that expires at expires (the zero time for a credential
// without an expiry, such as a static key): the earlier of the two, in whole seconds, since the signature's X-Amz-Expires is
// an integer (R-STOR-072, Q-STOR-290). A credential that ends within MinCredentialLife is ErrCredentialExpiring; a life below
// one second is an error of the caller.
func LinkLife(now time.Time, life time.Duration, expires time.Time) (time.Duration, error) {
	if life < time.Second {
		return 0, fmt.Errorf("a link lives at least a second, not %s", life)
	}
	if !expires.IsZero() {
		left := expires.Sub(now)
		if left < MinCredentialLife {
			return 0, ErrCredentialExpiring
		}
		life = min(life, left)
	}
	return life.Truncate(time.Second), nil
}

// BucketCopier is implemented by an [ObjectStore] that copies an object of another bucket on its own endpoint into its own
// bucket with its own credential, with no byte passing through the caller: the server-side copy between two connections on
// one endpoint (R-STOR-035 capability, R-STOR-056). The source's credential plays no part; this store's credential must be
// allowed to read the source.
type BucketCopier interface {
	// Location is the store's endpoint as the location rule compares it and its bucket (R-STOR-050).
	Location() (endpoint, bucket string)
	// CopyFrom copies srcKey of srcBucket, size bytes long, to dstKey. md nil keeps the source's metadata, a value replaces it.
	CopyFrom(ctx context.Context, srcBucket, srcKey, dstKey string, size int64, md Metadata) error
}

// MultipartMarker is the position after which a page of unfinished multipart uploads continues; the zero value starts at the first.
type MultipartMarker struct{ Key, UploadID string }

// MultipartPager is implemented by an [ObjectStore] that lists unfinished multipart uploads a page at a time. The staging area can hold
// many (a session that was started and never continued keeps its upload until the sweep), so the sweep of R-STOR-011 pages when it can
// and reads them all at once only from a store that cannot.
type MultipartPager interface {
	// ListMultipartPage lists at most max uploads below prefix after the marker; next is the zero marker when the listing is complete.
	ListMultipartPage(ctx context.Context, prefix string, after MultipartMarker, max int) (page []MultipartUpload, next MultipartMarker, err error)
}
