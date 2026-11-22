package scrub

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // the payload checksum a connection with payload_checksum: md5 asks for, not a digest of trust
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"slices"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
)

// errSource is a source copy that a read showed damaged: the repair marks it and tries the next source.
type errSource struct{ status string }

func (e *errSource) Error() string { return "the source copy is " + e.status }

// repair replaces a copy of d on target that this run marked missing or corrupt with the bytes of a present copy on another
// connection (R-STOR-027, Q-STOR-265): the sources in the order of a reconciliation's (verified before not verified, then by
// list order), each read to its end and checked against the digest while it is written; then the written object is read back
// whole, and only a match records the copy present and verified, and only while its row is still the one found damaged. A
// source found damaged on the way is marked and the next one tried. It returns the outcome ("" when nothing was to be done: the
// copy was repaired or claimed by someone else meanwhile) and the bytes it moved. No guarantee rests on a conditional write.
func (s *Scrubber) repair(ctx context.Context, target *blob.Connection, d blob.Digest, th *throttle) (string, int64) {
	copies, found, err := s.o.Ledger.Copies(ctx, d)
	if err != nil || !found {
		if err != nil {
			s.o.Log.Warn("the scrub could not read the copies of a damaged blob; the reconciliation repairs it", "sha256", d.Hex(), "error", err)
			return RepairFailed, 0
		}
		return "", 0
	}
	var tgt *CopyRow
	var sources []CopyRow
	for i := range copies {
		cp := copies[i]
		switch {
		case cp.Connection == target.ID():
			tgt = &copies[i]
		case cp.State == blob.CopyPresent && !cp.Retired:
			if c, ok := s.o.Connections.Get(cp.Connection); ok {
				if _, rejecting := c.Rejecting(); !rejecting {
					sources = append(sources, cp)
				}
			}
		}
	}
	if tgt == nil || (tgt.State != blob.CopyMissing && tgt.State != blob.CopyCorrupt) {
		return "", 0
	}
	if len(sources) == 0 {
		s.o.Log.Warn("a damaged copy has no whole copy on another connection to be replaced from", "connection", target.ID(), "sha256", d.Hex())
		return RepairNoSource, 0
	}
	slices.SortStableFunc(sources, func(a, b CopyRow) int {
		if av, bv := !a.VerifiedAt.IsZero(), !b.VerifiedAt.IsZero(); av != bv {
			if av {
				return -1
			}
			return 1
		}
		ca, _ := s.o.Connections.Get(a.Connection)
		cb, _ := s.o.Connections.Get(b.Connection)
		return ca.Position() - cb.Position()
	})
	var moved int64
	for _, src := range sources {
		from, _ := s.o.Connections.Get(src.Connection)
		n, err := s.write(ctx, from, src.Copy, target, tgt.ObjectKey, d, th)
		moved += n
		var bad *errSource
		switch {
		case errors.As(err, &bad):
			s.o.Ledger.Damaged(ctx, d, src.Copy, bad.status)
			continue
		case err != nil:
			s.o.Log.Warn("the scrub could not replace a damaged copy; the reconciliation repairs it", "connection", target.ID(), "source", src.Connection,
				"sha256", d.Hex(), "error", err)
			return RepairFailed, moved
		}
		back := blob.Copy{Connection: target.ID(), ObjectKey: tgt.ObjectKey, StoredSize: src.StoredSize}
		out, n2, rerr := readWhole(ctx, target, back, d, th)
		moved += n2
		if out != outcomeOK {
			// A store that applies an overwrite with a delay may still serve the damaged bytes: the copy stays marked and queued.
			s.o.Log.Warn("a replaced copy did not read back whole; it stays marked for the reconciliation", "connection", target.ID(), "sha256", d.Hex(), "error", rerr)
			return RepairFailed, moved
		}
		if _, err := s.o.Ledger.Whole(ctx, d, target.ID(), tgt.State, tgt.StateChangedUs, src.StoredSize); err != nil {
			s.o.Log.Warn("a replaced copy could not be recorded; the reconciliation records it", "connection", target.ID(), "sha256", d.Hex(), "error", err)
			return RepairFailed, moved
		}
		s.o.Log.Info("a damaged copy was replaced from another connection", "connection", target.ID(), "source", src.Connection, "sha256", d.Hex())
		return RepairDone, moved
	}
	return RepairNoSource, moved
}

// write streams the blob's bytes from a source copy to key on target, checking the source against the digest on the way: an
// object up to one part in one PutObject, a larger one as a multipart upload of parts read with ranged reads. It holds one part
// in memory. A source that is gone or has another length or digest is an *errSource.
func (s *Scrubber) write(ctx context.Context, from *blob.Connection, src blob.Copy, target *blob.Connection, key string, d blob.Digest, th *throttle) (int64, error) {
	size := src.StoredSize
	part := blob.PartSize(size, s.o.PartSize)
	if size <= part {
		data, err := readSource(ctx, from, src, nil, size, th)
		if err != nil {
			return int64(len(data)), err
		}
		if blob.Digest(sha256.Sum256(data)) != d {
			return size, &errSource{status: blob.CopyCorrupt}
		}
		return size, target.Objects().Put(ctx, key, bytes.NewReader(data), size, payloadChecksum(target, data, d), nil)
	}
	return s.writeParts(ctx, from, src, target, key, d, part, th)
}

func (s *Scrubber) writeParts(ctx context.Context, from *blob.Connection, src blob.Copy, target *blob.Connection, key string, d blob.Digest,
	part int64, th *throttle,
) (moved int64, err error) {
	objects := target.Objects()
	algorithm := target.PayloadChecksum()
	if algorithm == "none" {
		algorithm = ""
	}
	id, err := objects.CreateMultipart(ctx, key, algorithm, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err == nil {
			return
		}
		actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if aerr := objects.AbortMultipart(actx, key, id); aerr != nil {
			s.o.Log.Warn("could not abort a multipart upload of a repair; the orphan sweep will", "connection", target.ID(), "key", key, "error", aerr)
		}
	}()
	whole := sha256.New()
	var parts []blob.Part
	for off, number := int64(0), 1; off < src.StoredSize; off, number = off+part, number+1 {
		n := min(part, src.StoredSize-off)
		data, rerr := readSource(ctx, from, src, &blob.ByteRange{Offset: off, Length: n}, n, th)
		moved += int64(len(data))
		if rerr != nil {
			return moved, rerr
		}
		whole.Write(data)
		sum := partChecksum(algorithm, data)
		p, perr := objects.UploadPart(ctx, key, id, number, bytes.NewReader(data), n, sum)
		if perr != nil {
			return moved, perr
		}
		p.Number, p.Size, p.Checksum = number, n, sum
		parts = append(parts, p)
	}
	if blob.Digest(whole.Sum(nil)) != d {
		return moved, &errSource{status: blob.CopyCorrupt}
	}
	return moved, objects.CompleteMultipart(ctx, key, id, parts)
}

// readSource reads n bytes of a source copy (all of it when rng is nil) into memory. A source the store does not have is an
// *errSource; a whole read of another length is one too; a cut ranged read is an error, not evidence.
func readSource(ctx context.Context, from *blob.Connection, src blob.Copy, rng *blob.ByteRange, n int64, th *throttle) ([]byte, error) {
	body, _, err := from.Read(ctx, src, rng)
	if errors.Is(err, blob.ErrNotFound) {
		return nil, &errSource{status: blob.CopyMissing}
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	buf := bytes.NewBuffer(make([]byte, 0, n))
	if _, err := io.Copy(buf, &throttled{ctx: ctx, r: io.LimitReader(body, n+1), th: th}); err != nil {
		return buf.Bytes(), err
	}
	if int64(buf.Len()) != n {
		if rng == nil {
			return buf.Bytes(), &errSource{status: blob.CopyCorrupt}
		}
		return buf.Bytes(), fmt.Errorf("a ranged read of the source returned %d bytes, want %d", buf.Len(), n)
	}
	return buf.Bytes(), nil
}

// throttled is a reader that waits on the throttle after each read.
type throttled struct {
	ctx context.Context //nolint:containedctx // the reader's calls carry no context of their own
	r   io.Reader
	th  *throttle
}

func (t *throttled) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if werr := t.th.wait(t.ctx, n); werr != nil && err == nil {
		err = werr
	}
	return n, err
}

// payloadChecksum is the payload checksum of a whole write on target (R-STOR-006): the blob's digest for sha256, the MD5 of the
// bytes for md5.
func payloadChecksum(target *blob.Connection, data []byte, d blob.Digest) blob.Checksum {
	switch target.PayloadChecksum() {
	case "sha256":
		return blob.Checksum{Algorithm: "sha256", Value: d[:]}
	case "md5":
		sum := md5.Sum(data) //nolint:gosec // see the import
		return blob.Checksum{Algorithm: "md5", Value: sum[:]}
	}
	return blob.Checksum{}
}

func partChecksum(algorithm string, data []byte) blob.Checksum {
	var h hash.Hash
	switch algorithm {
	case "sha256":
		h = sha256.New()
	case "md5":
		h = md5.New() //nolint:gosec // see the import
	default:
		return blob.Checksum{}
	}
	h.Write(data)
	return blob.Checksum{Algorithm: algorithm, Value: h.Sum(nil)}
}
