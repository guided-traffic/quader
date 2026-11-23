package blob

import (
	"context"
	"crypto/md5" //nolint:gosec // Content-MD5 payload checksum for stores without additional checksums (R-STOR-006)
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

const (
	maxParts = 10000
	mib      = 1 << 20
)

// writeObjects writes the content of sp to the first storage.replication.min_copies writable connections in parallel
// (R-STOR-053, R-STOR-006): one request up to storage.object_store.multipart.threshold, a multipart upload from the spool
// file above it, each with its connection's payload checksum. A write that fails with a transient error or meets an open
// circuit is repeated on the next writable connection within the same request, while the spool still holds the bytes
// (Q-STOR-229). Fewer than min_copies successful writes are an error with the 503 of R-STOR-036; the objects that were
// written are returned with it, for the caller to leave to the orphan sweep.
func (s *Store) writeObjects(ctx context.Context, sp *Spooled) ([]copyWrite, error) {
	if sp.staged != "" {
		return s.putStaged(ctx, sp)
	}
	return s.writeTo(ctx, sp, s.conns.Writable(), s.minCopies())
}

// writeTo writes sp to need of the candidates, in their order, falling over to the next on a transient failure.
func (s *Store) writeTo(ctx context.Context, sp *Spooled, cands []*Connection, need int) ([]copyWrite, error) {
	if len(cands) < need {
		return nil, s.notEnoughConnections(nil)
	}
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		w   copyWrite
		err error
	}
	results := make(chan result, len(cands))
	next, inflight := 0, 0
	start := func() {
		c := cands[next]
		next++
		inflight++
		go func() {
			w, err := s.putOn(wctx, c, sp)
			results <- result{w: w, err: err}
		}()
	}
	for inflight < need && next < len(cands) {
		start()
	}
	var (
		writes  []copyWrite
		lastErr error
		fatal   error
	)
	for inflight > 0 {
		r := <-results
		inflight--
		switch {
		case r.err == nil:
			writes = append(writes, r.w)
		case fallsOver(r.err):
			lastErr = r.err
			s.log.Warn("a write to a connection failed; the next writable connection takes it", "connection", r.w.conn.ID(), "sha256", sp.SHA256.Hex(), "error", r.err)
			if fatal == nil && next < len(cands) && len(writes)+inflight < need {
				start()
			}
		default:
			if fatal == nil {
				fatal = r.err
				cancel()
			}
		}
	}
	slices.SortFunc(writes, func(a, b copyWrite) int { return a.conn.Position() - b.conn.Position() })
	switch {
	case fatal != nil:
		return writes, mapStoreErr(fatal)
	case len(writes) < need:
		return writes, s.notEnoughConnections(lastErr)
	}
	return writes, nil
}

// fallsOver reports a write failure that the next writable connection takes over (Q-STOR-229): a transient error or an open
// circuit. Any other failure fails the request.
func fallsOver(err error) bool { return IsTransient(err) || errors.Is(err, ErrCircuitOpen) }

// notEnoughConnections is the 503 of a commit that reached fewer than min_copies connections (R-STOR-053), with the
// Retry-After of the circuit that will probe first.
func (s *Store) notEnoughConnections(cause error) error {
	if cause != nil {
		return storeUnavailable(cause)
	}
	if err := s.RejectUpload(); err != nil {
		return err
	}
	return storeUnavailable(&circuitOpenError{retry: retryAfter})
}

// putOn writes the content of sp to its key on one connection (R-STOR-006) and returns the copy row the write makes; an encrypted
// connection gets the stored format with its record (R-STOR-080, putSealed).
func (s *Store) putOn(ctx context.Context, c *Connection, sp *Spooled) (copyWrite, error) {
	if c.Encrypted() {
		return s.putSealed(ctx, c, sp)
	}
	w := copyWrite{conn: c, key: c.ObjectKey(sp.SHA256), storedSize: sp.Size, verified: c.Verifies()}
	var err error
	if sp.Size <= int64(s.cfg.ObjectStore.Multipart.Threshold) {
		err = c.Objects().Put(ctx, w.key, sp.Reader(), sp.Size, wholeChecksum(c, sp), nil)
	} else {
		err = s.putMultipart(ctx, c, w.key, sp, nil)
	}
	w.at = s.now()
	return w, err
}

// mapStoreErr turns a store failure into the error a request answers with. The
// original error stays the cause.
func mapStoreErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrCircuitOpen), IsTransient(err), IsConfig(err):
		// before the context cases: a deadline that ended a call inside the transport (a dial that timed out) arrives as a
		// transient StoreError wrapping context.DeadlineExceeded, and the client is owed the 503 of R-STOR-036
		return storeUnavailable(err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}
	return err
}

// wholeChecksum is the payload checksum of a single request on a connection. SHA-256 and MD5 of the whole content are known
// from the hashing pass.
func wholeChecksum(c *Connection, sp *Spooled) Checksum {
	switch c.PayloadChecksum() {
	case "sha256":
		return Checksum{Algorithm: "sha256", Value: sp.SHA256[:]}
	case "md5":
		return Checksum{Algorithm: "md5", Value: sp.MD5[:]}
	}
	return Checksum{}
}

// PartSize is the part size of a multipart upload of size bytes: the configured
// size, raised so that the content fits into 10,000 parts, rounded up to whole
// MiB (R-STOR-006).
func PartSize(size, configured int64) int64 {
	ps := configured
	if need := (size + maxParts - 1) / maxParts; need > ps {
		ps = (need + mib - 1) / mib * mib
	}
	return ps
}

// putMultipart writes sp as a multipart upload from the spool file: in parts of the plaintext, each stored as it is (sl nil) or as its
// segments under the write's data key, the last part with the final segment, and the record on the upload (R-STOR-080). The completed
// object is confirmed with HeadObject: its size, and on an encrypted connection the storage key its record names.
func (s *Store) putMultipart(ctx context.Context, c *Connection, key string, sp *Spooled, sl *sealing) (err error) {
	objects := c.Objects()
	algorithm := c.PayloadChecksum()
	if algorithm == "none" {
		algorithm = ""
	}
	partSize := PartSize(sp.Size, int64(s.cfg.ObjectStore.Multipart.PartSize))
	n := int((sp.Size + partSize - 1) / partSize)
	var md Metadata
	want := sp.Size // stage 1 stores the blob's bytes, so the stored size is its size
	if sl != nil {
		if partSize%blobcrypt.SegmentSize != 0 {
			return fmt.Errorf("connection %s: a part of %d bytes holds no whole segments (R-STOR-075)", c.ID(), partSize)
		}
		md, want = sl.md, sl.stored
	}

	id, err := objects.CreateMultipart(ctx, key, algorithm, md)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			return
		}
		// The request context may be what failed: abort with one of our own.
		actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if aerr := objects.AbortMultipart(actx, key, id); aerr != nil {
			s.log.Warn("could not abort a multipart upload; the orphan sweep will", "connection", c.ID(), "key", key, "upload_id", id, "error", aerr)
		}
	}()

	parts := make([]Part, n)
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	local := make(chan struct{}, max(1, s.cfg.ObjectStore.Multipart.Concurrency))
	fail := func(e error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = e
			cancel()
		}
		mu.Unlock()
	}
launch:
	for i := 0; i < n; i++ {
		select {
		case local <- struct{}{}:
		case <-pctx.Done():
			break launch
		}
		select {
		case s.parts <- struct{}{}:
		case <-pctx.Done():
			<-local
			break launch
		}
		off := int64(i) * partSize
		size := min(partSize, sp.Size-off)
		wg.Add(1)
		go func(i int, off, size int64) {
			defer wg.Done()
			defer func() { <-s.parts; <-local }()
			p, perr := uploadPart(pctx, objects, key, id, i+1, sp, off, size, algorithm, sl, i == n-1)
			if perr != nil {
				fail(perr)
				return
			}
			parts[i] = p
		}(i, off, size)
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := objects.CompleteMultipart(ctx, key, id, parts); err != nil {
		return err
	}
	info, err := objects.Head(ctx, key)
	if err != nil {
		return err
	}
	if info.Size >= 0 && info.Size != want { // -1: the store did not say
		// The completed object is not what was sent. It is not deleted here: a
		// concurrent uploader may have written the right bytes to the same key.
		return fmt.Errorf("object %s on connection %s has %d bytes after the multipart upload, expected %d", key, c.ID(), info.Size, want)
	}
	if sl != nil && info.Metadata[blobcrypt.MetaKey] != sl.keyID {
		// the record is not the one written (R-STOR-080): the commit falls over to the next connection, the object is the orphan sweep's
		return fmt.Errorf("object %s on connection %s carries the record of storage key %q after the multipart upload, %s was written",
			key, c.ID(), info.Metadata[blobcrypt.MetaKey], sl.keyID)
	}
	return nil
}

// uploadPart sends one part read directly from the spool file: the plain bytes [off, off+size), or with sl their segments under the
// write's data key, encrypted as they are read (the final segment after the last part's), with the payload checksum of what is sent.
func uploadPart(ctx context.Context, objects ObjectStore, key, id string, number int, sp *Spooled, off, size int64, algorithm string, sl *sealing, last bool) (Part, error) {
	body, n := sp.Section(off, size), size
	if sl != nil {
		sec, err := sl.section(readerAt(body), off, size, last)
		if err != nil {
			return Part{}, fmt.Errorf("seal part %d: %w", number, err)
		}
		body, n = sec, sec.Size()
	}
	sum, err := sendChecksum(algorithm, body)
	if err != nil {
		return Part{}, fmt.Errorf("hash part %d: %w", number, err)
	}
	p, err := objects.UploadPart(ctx, key, id, number, body, n, sum)
	if err != nil {
		return Part{}, err
	}
	p.Number, p.Size, p.Checksum = number, n, sum
	return p, nil
}

// sendChecksum is the payload checksum of the bytes body yields, in algorithm ("sha256", "md5" or none), taken in a first pass;
// body is back at its start afterwards, for the request that sends it.
func sendChecksum(algorithm string, body io.ReadSeeker) (Checksum, error) {
	var h hash.Hash
	switch algorithm {
	case "sha256":
		h = sha256.New()
	case "md5":
		h = md5.New() //nolint:gosec // see imports
	default:
		return Checksum{}, nil
	}
	if _, err := io.Copy(h, body); err != nil {
		return Checksum{}, err
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return Checksum{}, err
	}
	return Checksum{Algorithm: algorithm, Value: h.Sum(nil)}, nil
}
