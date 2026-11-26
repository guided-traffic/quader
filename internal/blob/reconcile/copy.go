package reconcile

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Content-MD5 payload checksum for stores without additional checksums (R-STOR-006)
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// The copy of one blob onto one connection from one source copy (R-STOR-056): server-side within one endpoint, streamed through
// the worker otherwise. These functions talk to the object stores only; the queue step records what they wrote.

// errFallBack is a server-side copy that did not work for a reason a streamed copy may not share: the pair is left to streaming
// for the rest of the run (a refused copy) or for this copy (a copy of another size).
var errFallBack = errors.New("the server-side copy falls back to streaming")

// sourceError is a failure that concerns the source copy. status is the evidence it is (blob.CopyMissing: the store answered
// "no such object"; blob.CopyCorrupt: a size or, read to its end, a digest other than the blob's), empty when it is none (a
// transient error); the next source is tried either way. reason is why, as the finding of the mark records it (storage.md R-STOR-108).
type sourceError struct {
	status string
	reason string
	err    error
}

func (e *sourceError) Error() string {
	if e.status != "" {
		return "source copy " + e.status + ": " + e.err.Error()
	}
	return "source: " + e.err.Error()
}

func (e *sourceError) Unwrap() error { return e.err }

// targetError is a failure of the target connection: the copy fails and is tried again after its back-off.
type targetError struct{ err error }

func (e *targetError) Error() string { return "target: " + e.err.Error() }
func (e *targetError) Unwrap() error { return e.err }

// reason is the failure reason of a target's error (Q-STOR-258).
func (e *targetError) reason() string {
	if blob.IsTransient(e.err) || errors.Is(e.err, blob.ErrCircuitOpen) {
		return ReasonUnavailable
	}
	return ReasonRefused
}

// sourceFailure classifies an error of a source read: "no such object", a range the object does not have, a stream that is not the
// blob, and a refusal of the connection's reader whose reason marks (R-STOR-082: every reason of the stored format but unknown_key)
// are evidence about the copy; anything else is not, unknown_key included, which the export's import can still answer (Q-STOR-311).
func sourceFailure(err error) error {
	var se *blob.StoreError
	switch {
	case errors.Is(err, blob.ErrNotFound):
		return &sourceError{status: blob.CopyMissing, reason: blob.ReasonNotFound, err: err}
	case errors.As(err, &se) && se.Status == http.StatusRequestedRangeNotSatisfiable:
		return &sourceError{status: blob.CopyCorrupt, reason: blob.ReasonSize, err: err}
	case errors.Is(err, blob.ErrStreamNotBlob):
		return &sourceError{status: blob.CopyCorrupt, reason: blob.ReasonDigest, err: err}
	}
	if reason, ok := blobcrypt.ReasonOf(err); ok && reason.MarksCorrupt() {
		return &sourceError{status: blob.CopyCorrupt, reason: string(reason), err: err}
	}
	return &sourceError{err: err}
}

// made is one copy written: how, whether the write showed the whole object to be the blob, when the write ended (the insert
// bound counts from there, R-STOR-005), the bytes that passed the worker, and the copy row it makes: the object's name, its stored
// size and, on an encrypted connection, the storage key that wrapped its data key (R-STOR-080).
type made struct {
	mode     string
	verified bool
	at       time.Time
	bytes    int64
	key      string
	stored   int64
	keyID    string
}

// target is the blob a copy makes: its digest and its plain size.
type target struct {
	digest blob.Digest
	size   int64
}

// copier makes copies for one run: it holds the run's throttle and the pairs whose server-side copy the store refused.
type copier struct {
	partSize   int64 // storage.object_store.multipart.part_size: a streamed copy holds one part (Q-STOR-255)
	serverSide bool  // storage.reconcile.server_side: auto
	limit      *limiter
	now        func() time.Time
	log        *slog.Logger

	mu       sync.Mutex
	refusals map[[2]string]bool // (source, target) pairs streamed for the rest of the run
}

func newCopier(partSize int64, serverSide bool, limit *limiter, now func() time.Time, log *slog.Logger) *copier {
	return &copier{partSize: partSize, serverSide: serverSide, limit: limit, now: now, log: log, refusals: map[[2]string]bool{}}
}

// copyBlob writes the blob onto dst from the source copy src on srcConn (R-STOR-087's four cases): between two plain connections
// server-side within one endpoint and streamed across; into an encrypted connection always streamed through the connection's writer
// with a new data key, from a plain or an encrypted source alike (Q-STOR-308); from an encrypted source into a plain one streamed
// through the source's reader, which decrypts and authenticates.
func (c *copier) copyBlob(ctx context.Context, b target, src blob.Copy, srcConn, dst *blob.Connection) (made, error) {
	if dst.Encrypted() {
		return c.sealed(ctx, b, src, srcConn, dst)
	}
	if c.serverSideFor(srcConn, dst) {
		m, err := c.server(ctx, b, src, srcConn, dst)
		if !errors.Is(err, errFallBack) {
			return m, err
		}
	}
	return c.stream(ctx, b, src, srcConn, dst)
}

// serverSideFor reports whether a copy from src to dst is made server-side (R-STOR-056): with server_side auto, between two
// connections without encryption on one endpoint, where dst's start check found a copy from src to work (R-STOR-035,
// Q-STOR-254) and the store has not refused one in this run.
func (c *copier) serverSideFor(src, dst *blob.Connection) bool {
	if !c.serverSide || src.Encrypted() || dst.Encrypted() || !dst.Capabilities().ServerSideCopyFrom[src.ID()] {
		return false
	}
	sc, ok1 := src.Objects().(blob.BucketCopier)
	dc, ok2 := dst.Objects().(blob.BucketCopier)
	if !ok1 || !ok2 {
		return false
	}
	se, _ := sc.Location()
	de, _ := dc.Location()
	if se == "" || se != de {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.refusals[[2]string{src.ID(), dst.ID()}]
}

// server copies the source object into dst's bucket with dst's credential and confirms the copy by its size: present, not
// verified (Q-STOR-234). A refusal leaves the pair to streaming for the rest of the run.
func (c *copier) server(ctx context.Context, b target, src blob.Copy, srcConn, dst *blob.Connection) (made, error) {
	sc := srcConn.Objects().(blob.BucketCopier)
	dc := dst.Objects().(blob.BucketCopier)
	_, bucket := sc.Location()
	key := dst.ObjectKey(b.digest)
	err := dc.CopyFrom(ctx, bucket, src.ObjectKey, key, src.StoredSize, nil)
	switch {
	case err == nil:
	case errors.Is(err, blob.ErrNotFound):
		return made{}, &sourceError{status: blob.CopyMissing, reason: blob.ReasonNotFound, err: err}
	case blob.IsTransient(err), errors.Is(err, blob.ErrCircuitOpen), ctx.Err() != nil:
		return made{}, &targetError{err: err}
	default:
		c.mu.Lock()
		c.refusals[[2]string{srcConn.ID(), dst.ID()}] = true
		c.mu.Unlock()
		c.log.Warn("a server-side copy between two connections was refused; the pair is streamed for the rest of the run",
			"source", srcConn.ID(), "connection", dst.ID(), "error", err)
		return made{}, errFallBack
	}
	at := c.now()
	info, err := dst.Objects().Head(ctx, key)
	switch {
	case err != nil:
		return made{}, &targetError{err: err}
	case info.Size >= 0 && info.Size != src.StoredSize:
		c.log.Warn("a server-side copy has another size than its source's copy row; it is streamed instead", "source", srcConn.ID(),
			"connection", dst.ID(), "sha256", b.digest.Hex(), "size", info.Size, "stored_size", src.StoredSize)
		return made{}, errFallBack
	}
	return made{mode: modeServer, at: at, key: key, stored: src.StoredSize}, nil
}

// sealed copies the blob into the encrypted connection dst (R-STOR-087): the source is read whole through its connection's reader,
// bound to the blob's digest and size, and throttled; dst's writer hashes the plaintext as it passes and seals it under a new data key
// with one part in memory (Q-STOR-440). A plaintext that is not the blob completes nothing and is evidence against the source, as a
// refusal of the source's reader is by its reason.
func (c *copier) sealed(ctx context.Context, b target, src blob.Copy, srcConn, dst *blob.Connection) (made, error) {
	body, info, err := srcConn.ReadBlob(ctx, b.digest, b.size, src, nil, true)
	if err != nil {
		return made{}, sourceFailure(err)
	}
	defer body.Close()
	if !srcConn.Encrypted() && info.Size >= 0 && info.Size != src.StoredSize {
		return made{}, &sourceError{status: blob.CopyCorrupt, reason: blob.ReasonSize, err: fmt.Errorf("the object has %d bytes, its copy row %d", info.Size, src.StoredSize)}
	}
	cw, err := dst.WriteSealedStream(ctx, b.digest, b.size, &throttledReader{ctx: ctx, r: body, l: c.limit}, c.partSize)
	var se *blob.StreamSourceError
	switch {
	case errors.As(err, &se):
		return made{}, sourceFailure(se.Err)
	case err != nil:
		return made{}, &targetError{err: err}
	}
	return made{mode: modeStream, verified: cw.Verified, at: c.now(), bytes: b.size, key: cw.Key, stored: cw.StoredSize, keyID: cw.StorageKeyID}, nil
}

// stream reads the blob from the source copy through the run's throttle and writes it to dst with dst's payload checksums,
// hashing it as it passes (Q-STOR-255): one request up to one part, a multipart upload of ranged reads above. A digest other
// than the blob's never completes a write and is evidence against the source.
func (c *copier) stream(ctx context.Context, b target, src blob.Copy, srcConn, dst *blob.Connection) (made, error) {
	if b.size <= c.partSize {
		return c.streamOne(ctx, b, src, srcConn, dst)
	}
	return c.streamParts(ctx, b, src, srcConn, dst)
}

func (c *copier) streamOne(ctx context.Context, b target, src blob.Copy, srcConn, dst *blob.Connection) (made, error) {
	body, info, err := srcConn.ReadBlob(ctx, b.digest, b.size, src, nil, false)
	if err != nil {
		return made{}, sourceFailure(err)
	}
	defer body.Close()
	if info.Size >= 0 && info.Size != src.StoredSize {
		return made{}, &sourceError{status: blob.CopyCorrupt, reason: blob.ReasonSize, err: fmt.Errorf("the object has %d bytes, its copy row %d", info.Size, src.StoredSize)}
	}
	buf := make([]byte, b.size)
	r := &throttledReader{ctx: ctx, r: body, l: c.limit}
	if _, err := io.ReadFull(r, buf); err != nil {
		return made{}, sourceFailure(err)
	}
	if n, _ := io.ReadFull(r, make([]byte, 1)); n > 0 {
		return made{}, &sourceError{status: blob.CopyCorrupt, reason: blob.ReasonSize, err: fmt.Errorf("the object is longer than %d bytes", b.size)}
	}
	if sha256.Sum256(buf) != b.digest {
		return made{}, &sourceError{status: blob.CopyCorrupt, reason: blob.ReasonDigest, err: errors.New("the object's SHA-256 is not the blob's digest")}
	}
	key := dst.ObjectKey(b.digest)
	if err := dst.Objects().Put(ctx, key, bytes.NewReader(buf), b.size, wholeChecksum(dst, b.digest, buf), nil); err != nil {
		return made{}, &targetError{err: err}
	}
	return made{mode: modeStream, verified: dst.Verifies(), at: c.now(), bytes: b.size, key: key, stored: b.size}, nil
}

func (c *copier) streamParts(ctx context.Context, b target, src blob.Copy, srcConn, dst *blob.Connection) (_ made, err error) {
	objects, key := dst.Objects(), dst.ObjectKey(b.digest)
	alg := dst.PayloadChecksum()
	if alg == "none" {
		alg = ""
	}
	ps := blob.PartSize(b.size, c.partSize)
	n := int((b.size + ps - 1) / ps)
	id, err := objects.CreateMultipart(ctx, key, alg, nil)
	if err != nil {
		return made{}, &targetError{err: err}
	}
	defer func() {
		if err == nil {
			return
		}
		actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if aerr := objects.AbortMultipart(actx, key, id); aerr != nil {
			c.log.Warn("could not abort the multipart upload of a streamed copy; the orphan sweep will", "connection", dst.ID(), "key", key, "error", aerr)
		}
	}()
	whole := sha256.New()
	buf := make([]byte, ps)
	parts := make([]blob.Part, 0, n)
	for i := range n {
		off := int64(i) * ps
		ln := min(ps, b.size-off)
		if err := c.readPart(ctx, b, src, srcConn, off, buf[:ln], i == 0); err != nil {
			return made{}, err
		}
		whole.Write(buf[:ln])
		sum := partChecksum(alg, buf[:ln])
		p, err := objects.UploadPart(ctx, key, id, i+1, bytes.NewReader(buf[:ln]), ln, sum)
		if err != nil {
			return made{}, &targetError{err: err}
		}
		p.Number, p.Size, p.Checksum = i+1, ln, sum
		parts = append(parts, p)
	}
	if !bytes.Equal(whole.Sum(nil), b.digest[:]) {
		return made{}, &sourceError{status: blob.CopyCorrupt, reason: blob.ReasonDigest, err: errors.New("the object's SHA-256 is not the blob's digest")}
	}
	if err := objects.CompleteMultipart(ctx, key, id, parts); err != nil {
		return made{}, &targetError{err: err}
	}
	at := c.now()
	info, err := objects.Head(ctx, key)
	switch {
	case err != nil:
		return made{}, &targetError{err: err}
	case info.Size >= 0 && info.Size != b.size:
		// not deleted: a concurrent writer of the same key writes the same bytes (A6)
		return made{}, &targetError{err: fmt.Errorf("%s object %s has %d bytes after the multipart upload, expected %d", dst.Element(), key, info.Size, b.size)}
	}
	return made{mode: modeStream, verified: dst.Verifies(), at: at, bytes: b.size, key: key, stored: b.size}, nil
}

// readPart fills buf with the bytes of the blob from off on, with one ranged read of the source copy through the run's throttle
// (on an encrypted source the segments of the window, authenticated). The first read compares the object's size, as the store
// reports it, with the copy's stored size.
func (c *copier) readPart(ctx context.Context, b target, src blob.Copy, srcConn *blob.Connection, off int64, buf []byte, first bool) error {
	body, info, err := srcConn.ReadBlob(ctx, b.digest, b.size, src, &blob.ByteRange{Offset: off, Length: int64(len(buf))}, false)
	if err != nil {
		return sourceFailure(err)
	}
	defer body.Close()
	if first && info.Size >= 0 && info.Size != src.StoredSize {
		return &sourceError{status: blob.CopyCorrupt, reason: blob.ReasonSize, err: fmt.Errorf("the object has %d bytes, its copy row %d", info.Size, src.StoredSize)}
	}
	if _, err := io.ReadFull(&throttledReader{ctx: ctx, r: body, l: c.limit}, buf); err != nil {
		return sourceFailure(err)
	}
	return nil
}

// wholeChecksum is the payload checksum of a single request on dst: SHA-256 is the blob's digest.
func wholeChecksum(dst *blob.Connection, d blob.Digest, data []byte) blob.Checksum {
	switch dst.PayloadChecksum() {
	case "sha256":
		return blob.Checksum{Algorithm: "sha256", Value: d[:]}
	case "md5":
		return partChecksum("md5", data)
	}
	return blob.Checksum{}
}

// partChecksum is the payload checksum of one request's bytes.
func partChecksum(alg string, data []byte) blob.Checksum {
	var h hash.Hash
	switch alg {
	case "sha256":
		h = sha256.New()
	case "md5":
		h = md5.New() //nolint:gosec // see imports
	default:
		return blob.Checksum{}
	}
	h.Write(data)
	return blob.Checksum{Algorithm: alg, Value: h.Sum(nil)}
}
