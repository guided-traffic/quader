package blob

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// The writer of an encrypted connection (R-STOR-080) and the commit of an upload session's staging object on one (R-STOR-081).

// sealing is the encryption of one write of a blob to an encrypted connection (R-STOR-080): a data key drawn for it and for no other
// object, the seed its nonces derive from, and its record under the connection's active storage key. The write destroys the key
// when it is done; the ciphertext is never kept, every section encrypts the spool as it is read.
type sealing struct {
	key    blobcrypt.Key
	seed   [32]byte
	final  blobcrypt.Final
	keyID  string
	md     Metadata
	stored int64
}

// newSealing draws the data key and the seed of a write of the blob d to c and wraps the key under c's active storage key.
func (s *Store) newSealing(ctx context.Context, c *Connection, d Digests) (*sealing, error) {
	if s.keys == nil {
		return nil, fmt.Errorf("blob: this replica holds no storage keys of connection %s", c.ID())
	}
	sk, err := s.keys.Active(ctx, c.ID())
	if err != nil {
		return nil, fmt.Errorf("blob: the active storage key of connection %s: %w", c.ID(), err)
	}
	stored, err := blobcrypt.StoredSize(d.Size)
	if err != nil {
		return nil, err
	}
	seed, err := blobcrypt.NewSeed(rand.Reader)
	if err != nil {
		return nil, err
	}
	dek, err := blobcrypt.NewKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	rec, err := blobcrypt.Wrap(sk, blobcrypt.DataKey{Key: dek, Digest: d.SHA256, Size: d.Size}, rand.Reader)
	if err != nil {
		dek.Destroy()
		return nil, err
	}
	return &sealing{key: dek, seed: seed, final: blobcrypt.Final{Size: d.Size, Digest: d.SHA256}, keyID: sk.ID, md: rec.Metadata(), stored: stored}, nil
}

// section is the stored form of the plain bytes [off, off+n) of the blob, which src holds from its offset 0: the data segments from
// off / 64 KiB on, and the final segment after them when last.
func (w *sealing) section(src io.ReaderAt, off, n int64, last bool) (*blobcrypt.Section, error) {
	var f *blobcrypt.Final
	if last {
		f = &w.final
	}
	return blobcrypt.NewSection(w.key, w.seed, src, off/blobcrypt.SegmentSize, n, f)
}

// readerAt is a section of the spool as the ReaderAt a Section reads: both kinds of section the spool makes (a bytes.Reader in
// memory, an io.SectionReader of the file) are one.
func readerAt(r io.ReadSeeker) io.ReaderAt {
	if ra, ok := r.(io.ReaderAt); ok {
		return ra
	}
	return unreadable{}
}

// putSealed writes sp to the encrypted connection c (R-STOR-080): a new data key, the connection's active storage key and the name
// of its naming function; one PutObject of S(L) bytes with the record up to multipart.threshold, Quader's multipart above it. The
// payload checksum is taken over the stored bytes, so a section is encrypted once to hash it and once to send it. The copy row
// records S(L), the storage key and, where the connection enforces payload checksums, verified: Quader encrypted what it hashed.
func (s *Store) putSealed(ctx context.Context, c *Connection, sp *Spooled) (copyWrite, error) {
	w := copyWrite{conn: c}
	name, err := c.Name(sp.SHA256)
	if err != nil {
		return w, err
	}
	sl, err := s.newSealing(ctx, c, sp.Digests)
	if err != nil {
		return w, err
	}
	defer sl.key.Destroy()
	w.key, w.storedSize, w.keyID, w.verified = name, sl.stored, sl.keyID, c.Verifies()
	if sp.Size <= int64(s.cfg.ObjectStore.Multipart.Threshold) {
		sec, serr := sl.section(readerAt(sp.Section(0, sp.Size)), 0, sp.Size, true)
		if serr != nil {
			return w, serr
		}
		sum, serr := sendChecksum(payloadAlgorithm(c), sec)
		if serr != nil {
			return w, serr
		}
		err = c.Objects().Put(ctx, name, sec, sec.Size(), sum, sl.md)
	} else {
		err = s.putMultipart(ctx, c, name, sp, sl)
	}
	w.at = s.now()
	return w, err
}

// payloadAlgorithm is the payload checksum of c's writes, empty for none.
func payloadAlgorithm(c *Connection) string {
	if a := c.PayloadChecksum(); a != "none" {
		return a
	}
	return ""
}

// StagedPart is a part of an upload session's staging object as it was sent (upload_session_parts.stored_size and stored_sha256,
// R-STOR-081).
type StagedPart struct {
	StoredSize   int64
	StoredSHA256 []byte
}

// sealedStaging is what the commit of a session's staging object on an encrypted connection needs: the session's data key, which
// sealed the staging object's segments, and its parts as stored.
type sealedStaging struct {
	key   blobcrypt.Key
	parts []StagedPart
}

// StagedSealed is [Staged] for a session on an encrypted connection (R-STOR-081): the staging object holds the blob's data segments
// and its final segment sealed under the session's data key dataKey, and parts are its parts as stored, in order (a final segment
// sent as a part of its own is the rest of the object and needs no entry). The commit wraps dataKey into the content object's record
// and never keeps it; the caller destroys it after the commit.
func StagedSealed(conn string, d Digests, key string, dataKey blobcrypt.Key, parts []StagedPart) *Spooled {
	sp := Staged(conn, d, key)
	sp.sealed = &sealedStaging{key: dataKey, parts: parts}
	return sp
}

// finalizeSealed is the object write of a session's staging object on the encrypted connection c (R-STOR-081 finalize steps 2 and 3):
// where a copy replaces an object's metadata, the copy to the content name carries the record, a wrap of the session's data key
// under the active storage key bound to the digest, confirmed by HeadObject and not verified (the scrub reads it first); where it
// does not, the staging object is streamed into a multipart upload of the content name with the record, its parts as stored,
// authenticated and hashed on the way, and the copy is verified where the connection enforces payload checksums.
func (s *Store) finalizeSealed(ctx context.Context, c *Connection, sp *Spooled) (copyWrite, error) {
	w := copyWrite{conn: c}
	switch {
	case sp.sealed == nil:
		return w, fmt.Errorf("the staging object %s on encrypted connection %s comes without the session's data key", sp.staged, c.ID())
	case s.keys == nil:
		return w, fmt.Errorf("blob: this replica holds no storage keys of connection %s", c.ID())
	}
	sk, err := s.keys.Active(ctx, c.ID())
	if err != nil {
		return w, fmt.Errorf("blob: the active storage key of connection %s: %w", c.ID(), err)
	}
	name, err := c.Name(sp.SHA256)
	if err != nil {
		return w, err
	}
	stored, err := blobcrypt.StoredSize(sp.Size)
	if err != nil {
		return w, err
	}
	rec, err := blobcrypt.Wrap(sk, blobcrypt.DataKey{Key: sp.sealed.key, Digest: sp.SHA256, Size: sp.Size}, rand.Reader)
	if err != nil {
		return w, err
	}
	md := Metadata(rec.Metadata())
	w.key, w.storedSize, w.keyID = name, stored, sk.ID
	if c.Capabilities().CopyReplacesMetadata {
		if err := c.Objects().Copy(ctx, sp.staged, name, stored, md); err != nil {
			if errors.Is(err, ErrNotFound) {
				return w, fmt.Errorf("copy %s: %w", sp.staged, ErrStagingGone)
			}
			return w, mapStoreErr(err)
		}
		w.at = s.now()
		return w, confirmSealed(ctx, c, name, stored, md)
	}
	if err := s.streamStaged(ctx, c, sp, name, stored, md); err != nil {
		return w, err
	}
	w.verified, w.at = c.Verifies(), s.now()
	return w, nil
}

// confirmSealed confirms an object written with a record (R-STOR-081): HeadObject must answer the stored size and the record's
// storage key.
func confirmSealed(ctx context.Context, c *Connection, name string, stored int64, md Metadata) error {
	info, err := c.Objects().Head(ctx, name)
	if err != nil {
		return mapStoreErr(err)
	}
	if info.Size >= 0 && info.Size != stored {
		return fmt.Errorf("object %s on connection %s has %d bytes after the copy, expected %d", name, c.ID(), info.Size, stored)
	}
	if info.Metadata[blobcrypt.MetaKey] != md[blobcrypt.MetaKey] {
		return fmt.Errorf("object %s on connection %s carries the record of storage key %q after the copy, %s was written", name, c.ID(),
			info.Metadata[blobcrypt.MetaKey], md[blobcrypt.MetaKey])
	}
	return nil
}

// abortTimeout bounds the abort of a multipart upload that failed, which runs with a context of its own.
const abortTimeout = 30 * time.Second

// errStreamEnded unblocks the writer of a verification that ended early.
var errStreamEnded = errors.New("the verification of the staging object ended")

// streamStaged copies the staging object of a session into a multipart upload of name created with the record md (R-STOR-081
// finalize step 3): each part as it was stored, read with one ranged GetObject into memory, compared with its recorded SHA-256 and
// sent with it as its payload checksum, while one reader authenticates every segment, the final segment and the SHA-256 of the
// plaintext under the session's data key. A difference fails the finalize with 503 and leaves the session assembled for the client's
// retry; nothing of it completes the upload.
func (s *Store) streamStaged(ctx context.Context, c *Connection, sp *Spooled, name string, stored int64, md Metadata) (err error) {
	layout := sp.sealed.parts
	var sum int64
	for _, p := range layout {
		sum += p.StoredSize
	}
	switch {
	case sum < stored:
		layout = append(layout, StagedPart{StoredSize: stored - sum}) // the final segment, sent as a part of its own
	case sum > stored:
		return fmt.Errorf("the parts of the staging object %s on connection %s hold %d bytes, the blob is stored in %d", sp.staged, c.ID(), sum, stored)
	}
	mismatch := func(cause error) error {
		return storeUnavailable(fmt.Errorf("the staging object of the session on connection %s is not the blob: %w", c.ID(), cause))
	}
	objects, alg := c.Objects(), payloadAlgorithm(c)
	id, err := objects.CreateMultipart(ctx, name, alg, md)
	if err != nil {
		return mapStoreErr(err)
	}
	defer func() {
		if err == nil {
			return
		}
		actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abortTimeout)
		defer cancel()
		if aerr := objects.AbortMultipart(actx, name, id); aerr != nil {
			s.log.Warn("could not abort a multipart upload; the orphan sweep will", "connection", c.ID(), "key", name, "upload_id", id, "error", aerr)
		}
	}()
	pr, pw := io.Pipe()
	verdict := make(chan error, 1)
	go func() {
		r, verr := blobcrypt.NewReader(pr, blobcrypt.DataKey{Key: sp.sealed.key, Digest: sp.SHA256, Size: sp.Size}, true)
		if verr == nil {
			_, verr = io.Copy(io.Discard, r)
		}
		pr.CloseWithError(errStreamEnded)
		verdict <- verr
	}()
	waited := false
	wait := func() error {
		if waited {
			return nil
		}
		waited = true
		return <-verdict
	}
	defer func() {
		pw.CloseWithError(errStreamEnded)
		_ = wait()
	}()
	var largest int64
	for _, p := range layout {
		largest = max(largest, p.StoredSize)
	}
	buf := make([]byte, largest) // one part in memory, as the reconciliation's streamed copy holds one
	parts := make([]Part, 0, len(layout))
	var off int64
	for i, p := range layout {
		b := buf[:p.StoredSize]
		if err := readRange(ctx, objects, sp.staged, off, b); err != nil {
			if errors.Is(err, ErrNotFound) {
				return fmt.Errorf("read %s: %w", sp.staged, ErrStagingGone)
			}
			return mapStoreErr(err)
		}
		if h := sha256.Sum256(b); p.StoredSHA256 != nil && !bytes.Equal(h[:], p.StoredSHA256) {
			return mismatch(fmt.Errorf("part %d has another SHA-256 than the one recorded when it was sent", i+1))
		}
		if _, werr := pw.Write(b); werr != nil {
			return mismatch(wait())
		}
		sum := probeChecksum(alg, b)
		pt, uerr := objects.UploadPart(ctx, name, id, i+1, bytes.NewReader(b), p.StoredSize, sum)
		if uerr != nil {
			return mapStoreErr(uerr)
		}
		pt.Number, pt.Size, pt.Checksum = i+1, p.StoredSize, sum
		parts = append(parts, pt)
		off += p.StoredSize
	}
	_ = pw.Close()
	if verr := wait(); verr != nil {
		return mismatch(verr)
	}
	if err := objects.CompleteMultipart(ctx, name, id, parts); err != nil {
		return mapStoreErr(err)
	}
	return confirmSealed(ctx, c, name, stored, md)
}

// readRange fills b with the bytes of key from off on, with one ranged GetObject.
func readRange(ctx context.Context, objects ObjectStore, key string, off int64, b []byte) error {
	body, _, err := objects.Get(ctx, key, &ByteRange{Offset: off, Length: int64(len(b))})
	if err != nil {
		return err
	}
	defer body.Close()
	if _, err := io.ReadFull(body, b); err != nil {
		return fmt.Errorf("read %d bytes of %s from %d: %w", len(b), key, off, err)
	}
	return nil
}
