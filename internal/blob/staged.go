package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// The staging area of the multi-request upload sessions (R-STOR-011): every object of a session lives below
// <prefix>/_uploads/<session-id>/, apart from the blobs below <prefix>/sha256/ (R-STOR-001), so that the collection run and the
// orphan sweep tell a staged byte from a stored one by its key alone.

// uploadsDir is the name of the staging area below the prefix.
const uploadsDir = "_uploads/"

// UploadsPrefix is the key prefix of every staging object of every session.
func UploadsPrefix(prefix string) string {
	if prefix == "" {
		return uploadsDir
	}
	return prefix + "/" + uploadsDir
}

// SessionPrefix is the key prefix of the objects of one session; it ends with a slash, so that it never matches the objects of
// another session.
func SessionPrefix(prefix, id string) string { return UploadsPrefix(prefix) + id + "/" }

// SessionKey is the key of an object of one session: "data" is the multipart upload, "tail-<writer>" a tail.
func SessionKey(prefix, id, name string) string { return SessionPrefix(prefix, id) + name }

// ParseSessionKey is the inverse of [SessionKey]: the session id and the object name of a key below the staging area, false for
// any other key.
func ParseSessionKey(prefix, key string) (id, name string, ok bool) {
	rest, ok := strings.CutPrefix(key, UploadsPrefix(prefix))
	if !ok {
		return "", "", false
	}
	id, name, ok = strings.Cut(rest, "/")
	if !ok || id == "" || name == "" {
		return "", "", false
	}
	return id, name, true
}

// ErrStagingGone is returned by a commit of a [Staged] blob whose staging object is not in the bucket: another request of the
// same session finished first and removed it, or the session was aborted.
var ErrStagingGone = errors.New("the staging object of the upload is gone")

// IsNoSuchUpload reports that the object store does not know the multipart upload: it was completed or aborted already.
func IsNoSuchUpload(err error) bool {
	var se *StoreError
	return errors.As(err, &se) && se.Code == "NoSuchUpload"
}

// MapStoreError turns a failure of the object store into the error a request answers with (R-STOR-036): the 503 of an
// unreachable or refusing store, the caller's own cancellation as it is. It is for the code that calls the [ObjectStore] itself,
// as the store does for the writes it makes.
func MapStoreError(err error) error { return mapStoreErr(err) }

// Staged is a blob whose bytes are on the connection conn already, at the staging key, with the digests of the whole content as
// the session computed them while the bytes arrived. It is committed like a spooled body (Commit, CommitAll): the commit
// protocol's object write is a server-side copy of the staging object to the content address on the same connection, and a
// blob that is stored already copies nothing (R-STOR-011 finalize step 3, R-STOR-054). No byte passes through the replica
// unless storage.replication.min_copies asks for more copies. The caller removes the staging object after the commit.
func Staged(conn string, d Digests, key string) *Spooled {
	return &Spooled{Digests: d, staged: key, stagedOn: conn}
}

// StagedKey is the key of the staging object of a blob made by [Staged], empty for a spooled body.
func (s *Spooled) StagedKey() string { return s.staged }

// putStaged is the object write of a staged blob (R-STOR-054): the server-side copy on the session's connection, confirmed by
// its size, recorded present and not verified (the scrub reads it first); on an encrypted connection the copy carries the record or
// the staging object is streamed (finalizeSealed, R-STOR-081). With min_copies above 1 the content object is then read through the
// connection's reader and streamed through the replica's spool to the next writable connections, hashed against the session's
// digests; those copies are verified where the connection enforces its payload checksum, and an encrypted one gets its own data key.
func (s *Store) putStaged(ctx context.Context, sp *Spooled) ([]copyWrite, error) {
	c, ok := s.conns.Get(sp.stagedOn)
	if !ok {
		return nil, storeUnavailable(fmt.Errorf("the connection %q of the upload session is not configured on this replica", sp.stagedOn))
	}
	var first copyWrite
	var err error
	if c.Encrypted() {
		first, err = s.finalizeSealed(ctx, c, sp)
	} else {
		first, err = s.copyStaged(ctx, c, sp)
	}
	if err != nil {
		return nil, err
	}
	writes := []copyWrite{first}
	need := s.minCopies() - 1
	if need == 0 {
		return writes, nil
	}
	var others []*Connection
	for _, o := range s.conns.Writable() {
		if o != c {
			others = append(others, o)
		}
	}
	if len(others) < need {
		s.metrics.uploadUnavailable(s.firstRejecting(), CodeCircuitOpen, sp.Size)
		return writes, s.notEnoughConnections(nil)
	}
	body, _, err := c.ReadBlob(ctx, sp.SHA256, sp.Size, Copy{Connection: c.ID(), ObjectKey: first.key, StoredSize: first.storedSize}, nil, false)
	if err != nil {
		return writes, mapStoreErr(err)
	}
	local, err := s.spool.Ingest(ctx, body, IngestOptions{
		ContentLength: sp.Size, MaxSize: sp.Size,
		Expect: Expected{SHA256: sp.SHA256[:], SHA1: sp.SHA1[:], MD5: sp.MD5[:], SHA512: sp.SHA512[:]},
	})
	_ = body.Close()
	if err != nil {
		if errors.Is(err, ErrDigestMismatch) || errors.Is(err, ErrLengthMismatch) {
			// the staged content is not the session's: the finalize fails and the session stays assembled for the client's retry
			return writes, storeUnavailable(fmt.Errorf("the content object of the session on connection %s is not the blob: %w", c.ID(), err))
		}
		return writes, err
	}
	defer local.Close()
	more, err := s.writeTo(ctx, local, others, need)
	return append(writes, more...), err
}

// copyStaged is the server-side copy of a staged blob on a plain connection (R-STOR-054), confirmed by its size.
func (s *Store) copyStaged(ctx context.Context, c *Connection, sp *Spooled) (copyWrite, error) {
	key := c.ObjectKey(sp.SHA256)
	if err := c.Objects().Copy(ctx, sp.staged, key, sp.Size, nil); err != nil {
		if errors.Is(err, ErrNotFound) {
			return copyWrite{}, fmt.Errorf("copy %s: %w", sp.staged, ErrStagingGone)
		}
		return copyWrite{}, mapStoreErr(err)
	}
	info, err := c.Objects().Head(ctx, key)
	if err != nil {
		return copyWrite{}, mapStoreErr(err)
	}
	if info.Size >= 0 && info.Size != sp.Size { // -1: the store did not say; a plain connection stores the blob's bytes
		return copyWrite{}, fmt.Errorf("object %s on connection %s has %d bytes after the copy, expected %d", key, c.ID(), info.Size, sp.Size)
	}
	return copyWrite{conn: c, key: key, storedSize: sp.Size, at: s.now()}, nil
}

// errNotReadable is what a reader of a staged blob returns: its bytes are in the bucket, not in the spool.
var errNotReadable = errors.New("a staged blob has no spooled content")

type unreadable struct{}

func (unreadable) Read([]byte) (int, error)          { return 0, errNotReadable }
func (unreadable) Seek(int64, int) (int64, error)    { return 0, errNotReadable }
func (unreadable) ReadAt([]byte, int64) (int, error) { return 0, errNotReadable }

var _ io.ReadSeeker = unreadable{}
