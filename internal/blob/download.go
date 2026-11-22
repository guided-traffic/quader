package blob

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
)

// OpenOptions select what Open returns.
type OpenOptions struct {
	// Range reads part of the blob. Ranged reads cannot be verified (R-STOR-013).
	Range *ByteRange
	// Verify verifies a full read even when storage.download.verify is off: a caller whose bytes reach a browser as
	// code of quader's origin, the shared asset store of the UI (Q-UI-143), never serves an object it has not checked.
	Verify bool
	// Copies are the blob's copy rows as the caller read them with a statement of its own, the item lookup of a download
	// (R-ARCH-027, Q-STOR-275), when CopiesRead is set; Open then asks PostgreSQL nothing. Without CopiesRead Open reads them
	// (Store.Copies).
	Copies     []Copy
	CopiesRead bool
}

// Open opens the content of a blob for reading. size is the blob row's size.
//
// The read takes the blob's present copies on the connections this replica configures, in list order, a draining
// connection's last, and leaves out a connection whose circuit is open (R-STOR-055). A copy is left for the next one before
// the first byte, when its connection answers "no such object", fails transiently or answers with a size other than the
// copy's stored size; a failure after the first byte aborts the response, and the client's retry or resume takes the next copy
// (Q-STOR-232). With no copy to take the read answers 503: ErrNotFound (content_unavailable) when no configured connection
// holds a present copy, the store's 503 when the copies' connections fail; neither marks anything.
//
// A full read is verified unless storage.download.verify is false
// (R-STOR-013, Q-STOR-6): the SHA-256 of the bytes is computed while they are
// read and the last storage.download.hold_back bytes are not released before the
// body has ended and the digest matched. On a mismatch the reader returns
// ErrCorrupt instead of the held bytes, so the caller aborts the response and
// the client sees a truncated transfer, never a complete corrupted file. A verified read stamps its copy's verified_at at
// most once a day (Q-STOR-233).
//
// Only evidence about a stored object marks its copy: "no such object" marks it missing, a size other than its stored size
// or a body read to its end with another digest corrupt; the blob's content state follows from its copies (R-STOR-052), so a
// copy's damage changes the blob only when no other copy is present. A transport error or an open circuit marks nothing.
//
// The reader is charged to the memory of the request of ctx (WithMemory) from
// before the object store is asked until it is closed: DownloadCost of the
// object, or RangeCost of the range.
//
// The copies are o.Copies when the caller read them with its own statement (o.CopiesRead, Q-STOR-275): a download takes them
// from its item lookup, so that it costs that one statement (R-ARCH-027). They are as current as that statement, as a separate
// read just before the object store was asked was; what changed since is what a read meets anyway and falls over on.
func (s *Store) Open(ctx context.Context, d Digest, size int64, o OpenOptions) (io.ReadCloser, error) {
	copies := o.Copies
	if !o.CopiesRead {
		var err error
		if copies, err = s.Copies(ctx, d); err != nil {
			return nil, err
		}
	}
	undo := func() {}
	if s.memory != nil {
		cost := DownloadCost(s.cfg, size)
		if o.Range != nil {
			cost = RangeCost(s.cfg, size, *o.Range)
		}
		undo = s.memory(ctx, cost)
	}
	var unavailable error // the last transient failure or open circuit
	var corrupt error     // the last copy found of another size
	for _, cp := range s.conns.readOrder(copies) {
		if cp.State != CopyPresent {
			continue
		}
		c, _ := s.conns.Get(cp.Connection)
		if after, rejecting := c.Rejecting(); rejecting {
			unavailable = &circuitOpenError{retry: after}
			continue
		}
		body, info, err := c.Read(ctx, cp, o.Range)
		switch {
		case errors.Is(err, ErrNotFound):
			s.metrics.fellOver(c.ID(), fallOverNotFound)
			s.copyGone(ctx, d, c, cp)
			continue
		case err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && ctx.Err() != nil:
			undo()
			return nil, err // the caller gave up
		case err != nil:
			s.metrics.fellOver(c.ID(), fallOverUnavailable)
			unavailable = err
			continue
		}
		if info.Size >= 0 && info.Size != cp.StoredSize { // -1: the response did not say; the digest check at the end decides
			_ = body.Close()
			s.metrics.fellOver(c.ID(), fallOverSize)
			s.markCopy(ctx, d, c.ID(), CopyCorrupt)
			corrupt = fmt.Errorf("%w: the object on connection %s has %d bytes, its copy row says %d", ErrCorrupt, c.ID(), info.Size, cp.StoredSize)
			continue
		}
		if o.Range != nil || (!s.cfg.Download.Verify && !o.Verify) {
			return &chargedBody{ReadCloser: body, undo: undo}, nil
		}
		return &verifyReader{
			src: body, store: s, ctx: ctx, digest: d, size: cp.StoredSize, copy: cp, undo: undo,
			hash: sha256.New(), hold: int(s.cfg.Download.HoldBack),
			capacity: verifyBuffer(int(s.cfg.Download.BufferSize), int(s.cfg.Download.HoldBack), cp.StoredSize),
		}, nil
	}
	undo()
	switch {
	case unavailable != nil:
		return nil, mapStoreErr(unavailable)
	case corrupt != nil:
		return nil, corrupt
	}
	return nil, fmt.Errorf("%w: no connection of this replica holds a present copy of %s", ErrNotFound, d.Hex())
}

// chargedBody gives the memory of a reader back when it is closed.
type chargedBody struct {
	io.ReadCloser
	undo func()
}

func (c *chargedBody) Close() error {
	err := c.ReadCloser.Close()
	c.undo()
	return err
}

// verifyBuffer is the length of a verified download's one buffer:
// storage.download.buffer_size, at least twice the hold-back so that a refill
// clears at least as much as it keeps, and no more than the object plus the byte
// that would show it is longer (Q-ARCH-241).
func verifyBuffer(bufferSize, hold int, size int64) int {
	c := max(bufferSize, 2*hold, 4096)
	if size < int64(c) {
		c = int(size) + 1
	}
	return c
}

// copyGone records a read that a connection answered with "no such object": evidence about that copy (R-STOR-055). The copy
// is missing from then on, its repair is queued, and the blob is missing only when no other copy is present; a commit of the
// same content writes the object again (R-STOR-005, R-HA-024).
//
// A repair may have written the object again between the read and the mark; the mark, which changes only a present copy,
// would then undo it. So the object is looked for once more on the same connection after a mark, and a copy whose object is
// there again with its stored size is present (Q-STOR-151). Whichever order the repair and the mark take, the copy ends
// present when the object is there: a repair after the look sets it present itself.
func (s *Store) copyGone(ctx context.Context, d Digest, c *Connection, cp Copy) {
	if !s.markCopy(ctx, d, c.ID(), CopyMissing) {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	info, err := c.Objects().Head(ctx, cp.ObjectKey)
	if err != nil || (info.Size >= 0 && info.Size != cp.StoredSize) {
		return // gone, of another size, or the store cannot say: the mark stays
	}
	s.copyBack(ctx, d, c.ID())
}

// MissingRecheckInterval is how long a blob marked missing is answered from PostgreSQL alone before a read asks the object
// store again (Q-STOR-153): the Retry-After a client is given for a missing object, so that the client that retries is the one
// whose read looks. Per blob and across replicas, the store is asked at most once per interval.
const MissingRecheckInterval = 30 * time.Second

// RecheckMissing looks for the objects of a blob marked missing once more, so that a mark does not outlive the loss it
// records (Q-STOR-153, R-STOR-055): each missing copy on a connection of this replica whose circuit is closed is asked with
// one HeadObject, and an object that is back with the copy's stored size (an operator restored it, or the store answered 404
// for objects that exist, as with a wrong prefix that was corrected) sets that copy present and the blob ok, and the caller
// serves it. At most one read per blob and MissingRecheckInterval asks the store, whichever replica: the read whose statement
// moves the row's checked_at, the recheck claim of a blob without a present copy; every other read is refused from
// PostgreSQL as before (R-STOR-012). It reports whether the row is ok now; a blob that is not marked missing, objects that are
// still gone, of another size, and a store that cannot answer are false, and the interval starts again. size is the blob's
// size, which each copy's stored size replaces for its own check.
func (s *Store) RecheckMissing(ctx context.Context, d Digest, _ int64) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	var claimed bool
	var copies []Copy
	if err := s.db.InTx(ctx, func(tx meta.Tx) error {
		var err error
		if claimed, err = s.rows.ClaimRecheck(ctx, tx, d, MissingRecheckInterval); err != nil || !claimed {
			return err
		}
		copies, err = s.rows.Copies(ctx, tx, d)
		return err
	}); err != nil {
		s.log.Warn("could not claim the recheck of a blob marked missing", "sha256", d.Hex(), "error", err)
		return false
	}
	if !claimed {
		return false
	}
	back := false
	for _, cp := range s.conns.readOrder(copies) {
		if cp.State != CopyMissing {
			continue
		}
		c, _ := s.conns.Get(cp.Connection)
		if _, rejecting := c.Rejecting(); rejecting {
			continue
		}
		info, err := c.Objects().Head(ctx, cp.ObjectKey)
		switch {
		case errors.Is(err, ErrNotFound):
			continue
		case err != nil:
			s.log.Warn("could not recheck a copy of a blob marked missing", "sha256", d.Hex(), "connection", c.ID(), "error", err)
			continue
		case info.Size >= 0 && info.Size != cp.StoredSize:
			s.log.Error("the object of a copy marked missing is back with another size", "sha256", d.Hex(), "connection", c.ID(),
				"size", info.Size, "want", cp.StoredSize)
			continue
		}
		back = s.copyBack(ctx, d, c.ID()) || back
	}
	if !back {
		return false
	}
	row, err := s.Lookup(ctx, d)
	return err == nil && row.ContentStatus == StatusOK
}

// verifyReader hashes what it reads and withholds the last hold bytes. It reads
// into one pooled buffer: buf[off:ready] is cleared and not yet handed out,
// buf[ready:k] is the withheld tail, which the next refill moves to the front.
type verifyReader struct {
	src      io.ReadCloser
	undo     func()
	store    *Store
	ctx      context.Context
	digest   Digest
	size     int64 // the copy's stored size
	copy     Copy  // the copy the bytes come from
	hash     hash.Hash
	hold     int
	capacity int

	buf           *[]byte // from Buffer on the first read, back with Close
	off, ready, k int
	total         int64
	done          bool  // the source ended and the digest matched
	err           error // sticky
}

func (v *verifyReader) Read(p []byte) (int, error) {
	for {
		if v.off < v.ready {
			n := copy(p, (*v.buf)[v.off:v.ready])
			v.off += n
			return n, nil
		}
		if v.err != nil {
			return 0, v.err
		}
		if v.done {
			return 0, io.EOF
		}
		v.fill()
	}
}

// WriteTo writes the cleared bytes straight from the buffer, so a download needs
// no copy buffer of its own.
func (v *verifyReader) WriteTo(w io.Writer) (int64, error) {
	var written int64
	for {
		if v.off < v.ready {
			n, err := w.Write((*v.buf)[v.off:v.ready])
			v.off += n
			written += int64(n)
			if err != nil {
				return written, err
			}
			continue
		}
		if v.err != nil {
			return written, v.err
		}
		if v.done {
			return written, nil
		}
		v.fill()
	}
}

// fill moves the withheld tail to the front and reads until the buffer is full
// or the source ended. Then everything but the last hold bytes is cleared, or
// everything once the digest matched. A body longer than the blob has nothing
// more cleared; it is read to its end, where finish decides.
func (v *verifyReader) fill() {
	if v.buf == nil {
		v.buf = Buffer(v.capacity)
	}
	b := (*v.buf)[:v.capacity]
	v.k = copy(b, b[v.ready:v.k])
	v.off, v.ready = 0, 0
	for v.k < len(b) {
		n, err := v.src.Read(b[v.k:])
		if n > 0 {
			v.hash.Write(b[v.k : v.k+n])
			v.total += int64(n)
			v.k += n
		}
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			if v.err = v.finish(); v.err == nil {
				v.ready, v.done = v.k, true
			}
			return
		default:
			v.err = err // a transport error: abort, no status change
			return
		}
	}
	if v.total > v.size {
		v.k = 0
		return
	}
	v.ready = max(0, v.k-v.hold)
}

// finish compares the complete body with the blob and the copy: a match stamps the copy verified (Q-STOR-233), a mismatch
// marks it corrupt (R-STOR-055).
func (v *verifyReader) finish() error {
	var sum Digest
	v.hash.Sum(sum[:0])
	if v.total == v.size && sum == v.digest {
		v.store.stampVerified(v.ctx, v.digest, v.copy)
		return nil
	}
	v.store.markCopy(v.ctx, v.digest, v.copy.Connection, CopyCorrupt)
	return fmt.Errorf("%w: read %d bytes with SHA-256 %s from connection %s, expected %d bytes with %s", ErrCorrupt, v.total, sum.Hex(),
		v.copy.Connection, v.size, v.digest.Hex())
}

// Close closes the source and releases the buffer.
func (v *verifyReader) Close() error {
	Release(v.buf)
	v.buf, v.off, v.ready, v.k = nil, 0, 0, 0
	if v.err == nil {
		v.err = errReadAfterClose
	}
	err := v.src.Close()
	if v.undo != nil {
		v.undo()
	}
	return err
}

var errReadAfterClose = errors.New("blob: read after close")
