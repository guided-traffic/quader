package uploads

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Content-MD5, the payload checksum of a store without additional checksums (R-STOR-006)
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"hash"
	"io"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/errs"
)

// A session on an encrypted connection (R-STOR-081, Q-STOR-307): its data key is drawn at its start (or at its move to an encrypted
// connection before its first write) and sealed with the key ring into its row, so that whichever replica takes the writer claim
// opens it; nothing of it stays in a replica after a request. Each request draws a new seed for its nonces. A part is stored as the
// blob's data segments from its offset on, the last part with the final segment; a tail object as tail segments under the tails' own
// key at its session offset; the staging object gets no record, and the commit wraps the session's data key into the content
// object's record (blob.StagedSealed).

// SessionKeys seals the data key of a session into the value of upload_sessions.sealed_data_key and opens it again: the key ring,
// through keys.SealSessionKey and keys.OpenSessionKey (R-STOR-081). The composition root implements it.
type SessionKeys interface {
	Seal(session string, dataKey blobcrypt.Key) (string, error)
	Open(session, sealed string) (blobcrypt.Key, error)
}

// sessionCrypt is the encryption of one request of a session on an encrypted connection: the session's data key, opened for the
// request and destroyed at its end, and the seed the request draws for its nonces.
type sessionCrypt struct {
	key  blobcrypt.Key
	seed [32]byte
}

// errKindChanged is a session whose connection is encrypted while its row holds no data key, or the other way round: its staged
// bytes are not in the form its connection reads.
var errKindChanged = errors.New("the upload session's connection changed its encryption since the session started")

// reserve is what a claim on a session of part size partSize reserves for the segments a request may seal before a record counts
// them (R-STOR-081, Q-STOR-380): one part, its final segment and one more. The claim statement computes the same from the row.
func reserve(partSize int64) int64 {
	return (partSize+blobcrypt.SegmentSize-1)/blobcrypt.SegmentSize + 2
}

// segments is the number of segments plain bytes take, with the final segment when final.
func segments(plain int64, final bool) int64 {
	n := (plain + blobcrypt.SegmentSize - 1) / blobcrypt.SegmentSize
	if final {
		n++
	}
	return n
}

// exhausted is the answer to a request whose session would seal more segments under its data key than one key may
// (blobcrypt.MaxSegmentsPerKey, Q-STOR-318): 409 session_exhausted; the caller aborts the session.
func exhausted() error {
	return errs.E(errs.Conflict, errs.CodeSessionExhausted, "The upload sealed as many segments under its key as one key may; start the upload again.")
}

// isExhausted reports whether the claim of a request on the session r is refused for its segment counter.
func isExhausted(r row) bool {
	return r.SealedKey != "" && r.SealedSegments+reserve(r.PartSize) > blobcrypt.MaxSegmentsPerKey
}

// newSealedKey draws the data key of the session id and returns it sealed for its row.
func (s *Service) newSealedKey(id uuid.UUID) (string, error) {
	if s.keys == nil {
		return "", errs.E(errs.Internal, errs.CodeInternal, "This replica cannot seal the key of an upload to an encrypted connection.")
	}
	k, err := blobcrypt.NewKey(rand.Reader)
	if err != nil {
		return "", errs.E(errs.Internal, errs.CodeInternal, "", errs.WithCause(err))
	}
	defer k.Destroy()
	sealed, err := s.keys.Seal(id.String(), k)
	if err != nil {
		return "", errs.E(errs.Internal, errs.CodeInternal, "The key of the upload could not be sealed.", errs.WithCause(err))
	}
	return sealed, nil
}

// withCrypt opens the data key of the session r for a request on its connection p and draws the request's seed; a plain session is
// returned as it is. The caller closes the pin at the end of the request.
func (s *Service) withCrypt(p pin, r row) (pin, error) {
	switch {
	case !p.conn.Encrypted() && r.SealedKey == "":
		return p, nil
	case !p.conn.Encrypted() || r.SealedKey == "":
		return pin{}, errKindChanged
	case s.keys == nil:
		return pin{}, errs.E(errs.Internal, errs.CodeInternal, "This replica cannot open the key of an upload to an encrypted connection.")
	}
	k, err := s.keys.Open(r.ID.String(), r.SealedKey)
	if err != nil {
		return pin{}, errs.E(errs.Internal, errs.CodeInternal, "The key of the upload does not open on this replica.", errs.WithCause(err))
	}
	seed, err := blobcrypt.NewSeed(rand.Reader)
	if err != nil {
		k.Destroy()
		return pin{}, errs.E(errs.Internal, errs.CodeInternal, "", errs.WithCause(err))
	}
	p.crypt = &sessionCrypt{key: k, seed: seed}
	return p, nil
}

// close destroys the data key the request opened.
func (p pin) close() {
	if p.crypt != nil {
		p.crypt.key.Destroy()
	}
}

// partBody is what a part sends (R-STOR-081): on a plain connection the segment's bytes with their payload checksum; on an encrypted
// one the data segments of the plain bytes seg holds from the session offset offset on, sealed as they are read, with the final
// segment after them when final, and the payload checksum and the SHA-256 of the stored bytes taken in a first pass. seg may be nil
// for a part that holds the final segment alone. It returns the body, its size, its payload checksum, the SHA-256 of what is sent
// and the number of segments it seals.
func (p pin) partBody(seg *blob.Spooled, offset int64, final *blobcrypt.Final) (io.ReadSeeker, int64, blob.Checksum, []byte, int64, error) {
	if p.crypt == nil {
		return seg.Reader(), seg.Size, p.checksumOf(seg), bytes.Clone(seg.SHA256[:]), 0, nil
	}
	var src io.ReaderAt
	var plain int64
	if seg != nil {
		src, plain = readerAt(seg), seg.Size
	}
	sec, err := blobcrypt.NewSection(p.crypt.key, p.crypt.seed, src, offset/blobcrypt.SegmentSize, plain, final)
	if err != nil {
		return nil, 0, blob.Checksum{}, nil, 0, err
	}
	sum, stored, err := p.storedSums(sec)
	if err != nil {
		return nil, 0, blob.Checksum{}, nil, 0, err
	}
	return sec, sec.Size(), sum, stored, segments(plain, final != nil), nil
}

// tailBody is what a tail object holds (R-STOR-081, Q-STOR-321): on an encrypted connection the tail segments of the plain bytes seg
// holds, which start at the session offset offset, under the tails' key; on a plain one the bytes. It returns the body, its size,
// its payload checksum and the number of segments it seals.
func (p pin) tailBody(seg *blob.Spooled, offset int64) (io.ReadSeeker, int64, blob.Checksum, int64, error) {
	if p.crypt == nil {
		return seg.Reader(), seg.Size, p.checksumOf(seg), 0, nil
	}
	sec, err := blobcrypt.NewTailSection(p.crypt.key, p.crypt.seed, offset, readerAt(seg), seg.Size)
	if err != nil {
		return nil, 0, blob.Checksum{}, 0, err
	}
	sum, _, err := p.storedSums(sec)
	if err != nil {
		return nil, 0, blob.Checksum{}, 0, err
	}
	return sec, sec.Size(), sum, segments(seg.Size, false), nil
}

// storedSums reads a section once for its payload checksum and the SHA-256 of its bytes, and seeks it back to its start.
func (p pin) storedSums(sec io.ReadSeeker) (blob.Checksum, []byte, error) {
	h := sha256.New()
	var m hash.Hash
	w := io.Writer(h)
	if p.algorithm == "md5" {
		m = md5.New() //nolint:gosec // see imports
		w = io.MultiWriter(h, m)
	}
	if _, err := io.Copy(w, sec); err != nil {
		return blob.Checksum{}, nil, err
	}
	if _, err := sec.Seek(0, io.SeekStart); err != nil {
		return blob.Checksum{}, nil, err
	}
	stored := h.Sum(nil)
	switch p.algorithm {
	case "sha256":
		return blob.Checksum{Algorithm: "sha256", Value: bytes.Clone(stored)}, stored, nil
	case "md5":
		return blob.Checksum{Algorithm: "md5", Value: m.Sum(nil)}, stored, nil
	}
	return blob.Checksum{}, stored, nil
}

// readerAt is the content of a spooled segment as the ReaderAt a section reads.
func readerAt(seg *blob.Spooled) io.ReaderAt {
	if ra, ok := seg.ReaderAt(); ok {
		return ra
	}
	return bytes.NewReader(nil)
}

// tailReader of the plain bytes of a tail object (R-STOR-081): on an encrypted connection every tail segment authenticates at its
// place, then the size and the SHA-256 are compared; a refusal is errTailCorrupt. On a plain connection the stored bytes, which the
// caller checks.
func (p pin) openTail(body io.Reader, offset int64, t tailSeg, objectSize int64) (io.Reader, error) {
	if p.crypt == nil {
		return body, nil
	}
	var sum [32]byte
	copy(sum[:], t.SHA256)
	if objectSize < 0 {
		n, err := blobcrypt.TailStoredSize(t.Size)
		if err != nil {
			return nil, errors.Join(errTailCorrupt, err)
		}
		objectSize = n
	}
	r, err := blobcrypt.NewTailReader(body, p.crypt.key, offset, t.Size, sum, objectSize)
	if err != nil {
		return nil, errors.Join(errTailCorrupt, err)
	}
	return tailRefusals{r}, nil
}

// tailRefusals turns a refusal of a tail's stored format into errTailCorrupt, so that the session fails as for a tail that does not
// match its checksum; a transport error passes as it is.
type tailRefusals struct{ r io.Reader }

func (t tailRefusals) Read(b []byte) (int, error) {
	n, err := t.r.Read(b)
	if _, refused := blobcrypt.ReasonOf(err); refused {
		return n, errors.Join(errTailCorrupt, err)
	}
	return n, err
}

// stagedParts are the recorded parts of a session as stored, for the commit that streams the staging object (R-STOR-081 step 3).
func stagedParts(parts []part) []blob.StagedPart {
	out := make([]blob.StagedPart, len(parts))
	for i, pt := range parts {
		out[i] = blob.StagedPart{StoredSize: pt.StoredSize, StoredSHA256: pt.StoredSHA256}
	}
	return out
}

// finalIn reports whether the recorded parts of a completing session already end with the final segment (R-STOR-081): the tail went
// out as the last part with it. Every part but the last holds partSize plain bytes, so before that step the parts hold exactly the
// bytes received before the tail, and after it one part more than those.
func finalIn(r row, parts []part) bool {
	return int64(len(parts))*r.PartSize > r.Received-r.TailSize
}

// tailsOnly commits a sealed session that never sent a part (R-STOR-081 finalize step 4): its tails, decrypted, are the blob, written as
// one request with a new data key (R-STOR-080), not copied. ctx is the finalize's.
func (s *Service) tailsOnly(ctx context.Context, p pin, r row, d blob.Digests) (*blob.Spooled, error) {
	joined, err := s.joinTails(ctx, p, r, r.Tails, nil, nil)
	if err != nil {
		return nil, err
	}
	if joined.Size != d.Size || joined.SHA256 != d.SHA256 {
		_ = joined.Close()
		return nil, s.failSession(ctx, r, "the tails of the upload are not the blob it recorded", errTailCorrupt)
	}
	joined.Digests = d
	return joined, nil
}
