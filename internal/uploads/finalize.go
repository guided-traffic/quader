package uploads

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
)

// CommitFunc commits the staged blob as the item the caller decided on, in one transaction with the end of the session: it runs the
// commit protocol of R-STOR-005 on staged (whose object write is the server-side copy of the staging object) and calls markDone
// in the transaction that links the item, with the id of the item, so that the item and the end of the session are one commit and the
// session records which item it committed. It may be called again after a failure; a commit that finds the blob stored copies nothing.
type CommitFunc func(ctx context.Context, staged *blob.Spooled, markDone func(ctx context.Context, tx meta.Tx, itemID int64) error) error

// Result is the outcome of a finalize. Replayed says that the session was done already: the item is the one its first commit made, and
// nothing was committed again.
type Result struct {
	ItemID   int64
	Replayed bool
	// Received is the recorded byte count, set alongside a refusal of kind RangeNotSatisfiable (a finalize at another offset).
	Received int64
}

// errStale says that the state of the session moved on while a finalize step ran: another request of the same finalize advanced it.
var errStale = errors.New("the upload session moved on")

// errDone says that the session is done: another request of the same finalize committed it.
var errDone = errors.New("the upload session is done")

// Finalize completes the session of the scope with the SHA-256 the client names, after the bytes of last (R-STOR-011 Finalize).
// An offset of zero or more says where the bytes of last start, as for Append: any other received count is an error of kind
// RangeNotSatisfiable, decided under the claim in the statement that takes it, so that a request between a client's check and its finalize
// cannot move the end (an offset below zero appends at the end). A digest that is not the computed one is an error of kind Invalid and keeps the session open, with the bytes of last recorded.
// A session that is completing, assembled or done is a finalize that was cut or is repeated: it continues at the recorded state
// without a claim, its body is not read, and its digest must equal the recorded one. A session that is done answers from its record
// (Result.Replayed) and commits nothing again.
func (s *Service) Finalize(ctx context.Context, id uuid.UUID, sc Scope, offset int64, sum [32]byte, last io.Reader, limit int64, commit CommitFunc) (Result, error) {
	r, err := s.lookup(ctx, id, sc)
	if err != nil {
		return Result{}, err
	}
	switch r.State {
	case StateOpen:
		var cur row
		if cur, err = s.closeOpen(ctx, r, sc, offset, sum, last, limit); err != nil {
			return Result{Received: cur.Received}, err
		}
		r = cur
	case StateCompleting, StateAssembled, StateDone:
		if !bytes.Equal(r.Digest, sum[:]) {
			return Result{}, digestInvalid()
		}
		if r.State == StateDone {
			if s.cleanup(ctx, r) {
				_ = s.rows.markCleaned(ctx, r.ID)
			}
			return Result{ItemID: r.ItemID, Replayed: true}, nil
		}
	default:
		return Result{}, unknown()
	}
	return s.complete(ctx, r, commit)
}

// closeOpen is step 1 of a finalize: the bytes of the request are handled as an append, and the computed SHA-256 must equal the
// digest the client names. On a match one statement records the end of the body together with the digest and the state
// completing; on a mismatch the session stays open.
func (s *Service) closeOpen(ctx context.Context, r row, sc Scope, offset int64, sum [32]byte, last io.Reader, limit int64) (out row, err error) {
	var writer string
	if offset >= 0 && offset != r.Received {
		return r, outOfOrder("The chunk does not start at the received count.")
	}
	r, writer, err = s.claim(ctx, r.ID, sc, offset)
	if err != nil {
		return r, err // on a refusal of the claim r is the session as it stands
	}
	held := true
	defer func() {
		if held && !errors.Is(err, blob.ErrFault) {
			s.release(ctx, r.ID, writer)
		}
	}()
	if err = s.fault("finalize.claimed"); err != nil {
		return row{}, err
	}
	st := &streamState{received: r.Received}
	p, err := s.writable(ctx, &r, writer)
	if err == nil {
		p, err = s.withCrypt(p, r)
	}
	if err != nil {
		held = !errors.Is(err, errLostClaim)
		var rcv int64
		rcv, err = s.failed(ctx, r, st, err)
		return row{Received: rcv}, err
	}
	defer p.close()
	var res streamed
	res, err = s.stream(ctx, p, r, writer, last, limit, st)
	if err != nil {
		held = !errors.Is(err, errLostClaim)
		var rcv int64
		rcv, err = s.failed(ctx, r, st, err)
		return row{Received: rcv}, err // the count the 416 of a lost claim reports
	}
	match := bytes.Equal(res.h.sha256.Sum(nil), sum[:])
	e := res.e
	if match {
		e.Completing, e.Digest = true, bytes.Clone(sum[:])
	}
	if !match && !res.changed {
		return row{}, digestInvalid() // nothing to record; the deferred release gives the claim back
	}
	rctx, cancel := recordCtx(ctx)
	err = s.rows.recordEnd(rctx, r.ID, writer, e)
	cancel()
	if err != nil {
		lost := errors.Is(err, errLostClaim)
		held = !lost
		if lost { // see Append: only a record that certainly failed lets the request delete what it wrote
			s.removeObjects(ctx, p, res.newObjects)
		}
		var rcv int64
		rcv, err = s.failed(ctx, r, st, err)
		return row{Received: rcv}, err // the count the 416 of a lost claim reports
	}
	held = false
	s.removeObjects(ctx, p, res.oldObjects)
	if !match {
		return row{}, digestInvalid()
	}
	if err = s.fault("finalize.completing_recorded"); err != nil {
		return row{}, err
	}
	cur, found, gerr := s.rows.get(ctx, r.ID, s.cfg.SessionMaxAge)
	if gerr != nil {
		return row{}, gerr
	}
	if !found {
		return row{}, unknown()
	}
	return cur, nil
}

// complete runs the steps after the digest was recorded: the staging object (completing), the commit (assembled), the removal of
// the staging objects (done). Each is recorded before the next starts and each is idempotent (R-STOR-011 Finalize). Every step runs on
// the session's connection, and the commit's object write is the server-side copy there (R-STOR-054); while that connection cannot
// be asked the finalize answers 503 and the session stays as it is.
func (s *Service) complete(ctx context.Context, r row, commit CommitFunc) (Result, error) {
	p, err := s.connection(r)
	if err == nil {
		p, err = s.withCrypt(p, r)
	}
	if errors.Is(err, errKindChanged) {
		return Result{}, s.failSession(ctx, r, "the connection of the upload changed its encryption", err)
	}
	if err != nil {
		return Result{}, err
	}
	defer p.close()
	key, r, err := s.assemble(ctx, p, r)
	if errors.Is(err, errDone) {
		// another request of this finalize committed; a continuation that put objects after its cleanup (a merge of segments) removes them
		if s.cleanup(ctx, r) {
			_ = s.rows.markCleaned(ctx, r.ID)
		}
		return Result{ItemID: r.ItemID, Replayed: true}, nil
	}
	if err != nil {
		return Result{}, err
	}
	h, err := restoreHashes(r.HashState)
	if err != nil {
		return Result{}, errs.E(errs.Internal, errs.CodeInternal, "The upload session has no valid hash state.", errs.WithCause(err))
	}
	d := h.digests()
	if d.Size != r.Received || !bytes.Equal(d.SHA256[:], r.Digest) {
		return Result{}, errs.E(errs.Internal, errs.CodeInternal, "The hash state of the upload session differs from its record.")
	}
	var item int64
	markDone := func(ctx context.Context, tx meta.Tx, itemID int64) error {
		item = itemID
		return s.rows.markDone(ctx, tx, r.ID, itemID)
	}
	staged, err := s.staged(ctx, p, r, d, key)
	if err == nil {
		err = commit(ctx, staged, markDone)
		_ = staged.Close()
	}
	switch {
	case err == nil:
	case errors.Is(err, errAborted):
		return Result{}, unknown()
	case errors.Is(err, blob.ErrStagingGone), errors.Is(err, errTailGone):
		// The staging object is gone because another request of this finalize committed and cleaned up first; its blob is
		// stored and the item recorded, so this one answers from the record. A session that was aborted is gone for good.
		cur, found, gerr := s.rows.get(ctx, r.ID, s.cfg.SessionMaxAge)
		if gerr != nil {
			return Result{}, gerr
		}
		if !found || cur.State != StateDone {
			return Result{}, unknown()
		}
		return Result{ItemID: cur.ItemID, Replayed: true}, nil
	default:
		return Result{}, err
	}
	// counted when the commit is behind us, before the point at which a crash is simulated: the commit is what makes a session completed
	s.metrics.UploadSession(blob.SessionCompleted, 1)
	if err := s.fault("finalize.committed"); err != nil {
		return Result{}, err
	}
	if s.cleanup(ctx, r) { // the row stays for the replay window of a repeated closing request; the run then deletes it without the store
		mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := s.rows.markCleaned(mctx, r.ID); err != nil {
			s.log.Warn("could not mark an upload session cleaned", "session", r.ID, "error", err)
		}
	}
	return Result{ItemID: item}, nil
}

// staged is what the commit of a finalize writes (R-STOR-011, R-STOR-081): the staging object at key on a plain connection; on an
// encrypted one the staging object with the session's data key and its parts as stored, or, for a blob that never filled a part, its
// tails decrypted into the spool, which the commit writes with a new data key. The caller closes it.
func (s *Service) staged(ctx context.Context, p pin, r row, d blob.Digests, key string) (*blob.Spooled, error) {
	if p.crypt == nil {
		return blob.Staged(p.conn.ID(), d, key), nil
	}
	parts, err := s.rows.parts(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return s.tailsOnly(ctx, p, r, d)
	}
	return blob.StagedSealed(p.conn.ID(), d, key, p.crypt.key, stagedParts(parts)), nil
}

// assemble brings a completing session to assembled and returns the key of its staging object (step 2 of a finalize). An assembled
// session already has one; a session that is done is errDone, with the row. A session on an encrypted connection that never sent a
// part has none: its commit writes its tails (staged).
func (s *Service) assemble(ctx context.Context, p pin, r row) (string, row, error) {
	for range 3 {
		if r.State == StateCompleting {
			err := s.completeUpload(ctx, p, r)
			if errors.Is(err, errStale) {
				cur, found, gerr := s.rows.get(ctx, r.ID, s.cfg.SessionMaxAge)
				if gerr != nil {
					return "", row{}, gerr
				}
				if !found || cur.State == StateAborted {
					return "", row{}, unknown()
				}
				r = cur
				continue
			}
			if err != nil {
				return "", row{}, err
			}
			if err := s.fault("finalize.after_complete"); err != nil {
				return "", row{}, err
			}
			if err := s.rows.setAssembled(ctx, r.ID); err != nil {
				return "", row{}, err
			}
			cur, found, err := s.rows.get(ctx, r.ID, s.cfg.SessionMaxAge)
			if err != nil {
				return "", row{}, err
			}
			if !found || cur.State == StateAborted {
				return "", row{}, unknown()
			}
			r = cur
		}
		switch r.State {
		case StateAborted:
			return "", row{}, unknown()
		case StateDone:
			return "", r, errDone
		}
		parts, err := s.rows.parts(ctx, r.ID)
		if err != nil {
			return "", row{}, err
		}
		if len(parts) == 0 && p.crypt != nil {
			return "", r, nil // a sealed blob of less than one part is written from its tails, not copied (R-STOR-081 step 4)
		}
		if len(parts) == 0 && len(r.Tails) == 1 {
			return p.tailKey(r.ID, r.Tails[0].Writer), r, nil // a blob of less than one part is its single tail segment
		}
		return p.dataKey(r.ID), r, nil
	}
	return "", row{}, errs.E(errs.Unavailable, errs.CodeUnavailable, "The upload could not be completed; retry.", errs.WithRetryAfter(2*time.Second))
}

// completeUpload is the state completing: the tail becomes the last part and the multipart upload is completed; a blob of less
// than one part has no multipart upload, and its single tail segment, or the merge of its segments, or an empty object, is the
// staging object. On an encrypted connection the last part carries the final segment, after the tail's data segments or alone when
// the blob ends at a part boundary (R-STOR-081 finalize step 1), and a blob of less than one part assembles nothing.
func (s *Service) completeUpload(ctx context.Context, p pin, r row) error {
	if err := s.fault("finalize.completing"); err != nil {
		return err
	}
	parts, err := s.rows.parts(ctx, r.ID)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		if p.crypt != nil {
			return s.dropUnrecorded(ctx, p, r)
		}
		return s.completeSmall(ctx, p, r)
	}
	if r.TailSize > 0 && (p.crypt == nil || !finalIn(r, parts)) {
		if r, err = s.tailAsPart(ctx, p, r); err != nil {
			return err
		}
		if parts, err = s.rows.parts(ctx, r.ID); err != nil {
			return err
		}
	}
	if p.crypt != nil && !finalIn(r, parts) {
		err = s.completeWithFinal(ctx, p, r)
	} else {
		err = s.completeParts(ctx, p, r, parts, nil)
	}
	if err != nil {
		return err
	}
	return s.confirmStaging(ctx, p, r)
}

// completeParts completes the multipart upload of the staging object with the recorded parts as sent and, when given, the part that
// holds the final segment alone.
func (s *Service) completeParts(ctx context.Context, p pin, r row, parts []part, final *blob.Part) error {
	cp := make([]blob.Part, 0, len(parts)+1)
	for _, pt := range parts {
		bp := blob.Part{Number: pt.Number, ETag: pt.ETag, Size: pt.StoredSize}
		if p.algorithm == "sha256" {
			bp.Checksum = blob.Checksum{Algorithm: "sha256", Value: pt.StoredSHA256}
		}
		cp = append(cp, bp)
	}
	if final != nil {
		cp = append(cp, *final)
	}
	err := p.objects().CompleteMultipart(ctx, p.dataKey(r.ID), r.UploadID, cp)
	switch {
	case err == nil:
	case blob.IsNoSuchUpload(err):
		// Another request completed it, or an earlier attempt did and was cut before it recorded the next state: the object of
		// the session's size says so.
	case isInvalidPart(err):
		// The parts changed under this continuation (the tail was sent as a part by another one), or the store refused a part that
		// the session recorded.
		now, perr := s.rows.parts(ctx, r.ID)
		if perr != nil {
			return perr
		}
		if len(now) != len(parts) {
			return errStale
		}
		return s.failSession(ctx, r, "the multipart upload refused a part it was sent", err)
	default:
		return blob.MapStoreError(err)
	}
	return nil
}

// completeWithFinal completes the staging object of a sealed session that ends at a part boundary (R-STOR-081): the final segment goes
// out as a part of its own, 68 bytes, and the upload is completed with it, under the short claim of the finalize so that one
// continuation sends it. The part is not recorded: a continuation cut before the completion sends it again under its own seed.
func (s *Service) completeWithFinal(ctx context.Context, p pin, r row) error {
	writer := uuid.New().String()
	rctx, cancel := recordCtx(ctx)
	ok, err := s.rows.claimCompleting(rctx, r.ID, writer, s.cfg.WriterLease)
	cancel()
	if err != nil {
		return err
	}
	if !ok {
		cur, found, gerr := s.rows.get(ctx, r.ID, s.cfg.SessionMaxAge)
		if gerr != nil {
			return gerr
		}
		if !found || cur.State != StateCompleting {
			return errStale
		}
		return errs.E(errs.Unavailable, errs.CodeUnavailable, "The upload is being completed by another request; retry.", errs.WithRetryAfter(time.Second))
	}
	defer s.release(ctx, r.ID, writer)
	cur, found, err := s.rows.get(ctx, r.ID, s.cfg.SessionMaxAge)
	if err != nil {
		return err
	}
	if !found || cur.State != StateCompleting {
		return errStale
	}
	parts, err := s.rows.parts(ctx, cur.ID)
	if err != nil {
		return err
	}
	if finalIn(cur, parts) || cur.TailSize > 0 || len(parts) == 0 {
		return errStale
	}
	final := blobcrypt.Final{Size: cur.Received}
	copy(final.Digest[:], cur.Digest)
	number := parts[len(parts)-1].Number + 1
	sent, _, err := s.sendPart(ctx, p, cur.ID, cur.UploadID, number, nil, cur.Received, &final, s.completingRenewer(cur.ID, writer))
	if err != nil {
		if errors.Is(err, errLostClaim) {
			return errStale
		}
		return err
	}
	last := blob.Part{Number: number, ETag: sent.ETag, Size: sent.StoredSize}
	if p.algorithm == "sha256" {
		last.Checksum = blob.Checksum{Algorithm: "sha256", Value: sent.StoredSHA256}
	}
	return s.completeParts(ctx, p, cur, parts, &last)
}

// dropUnrecorded aborts a multipart upload of a session that recorded no part: it was created by a request that never recorded it.
func (s *Service) dropUnrecorded(ctx context.Context, p pin, r row) error {
	if r.UploadID == "" {
		return nil
	}
	if err := p.objects().AbortMultipart(ctx, p.dataKey(r.ID), r.UploadID); err != nil && !blob.IsNoSuchUpload(err) {
		return blob.MapStoreError(err)
	}
	return nil
}

// confirmStaging checks that the completed staging object exists with the session's size as stored: the bytes received, or S(L) of
// them on an encrypted connection (R-STOR-081).
func (s *Service) confirmStaging(ctx context.Context, p pin, r row) error {
	info, err := p.objects().Head(ctx, p.dataKey(r.ID))
	if err != nil {
		if !errors.Is(err, blob.ErrNotFound) {
			return blob.MapStoreError(err)
		}
		// no object: the other continuation finished and has cleaned up, or the session is lost
		cur, found, gerr := s.rows.get(ctx, r.ID, s.cfg.SessionMaxAge)
		if gerr != nil {
			return gerr
		}
		if found && (cur.State == StateAssembled || cur.State == StateDone) {
			return errStale
		}
		return s.failSession(ctx, r, "the staging object does not exist after the multipart upload", err)
	}
	want := r.Received
	if p.crypt != nil {
		if want, err = blobcrypt.StoredSize(r.Received); err != nil {
			return err
		}
	}
	if info.Size >= 0 && info.Size != want {
		return s.failSession(ctx, r, fmt.Sprintf("the staging object has %d bytes, the session received %d stored in %d", info.Size, r.Received, want), blob.ErrCorrupt)
	}
	return nil
}

// completeSmall prepares the staging object of a blob that has no part: nothing (one segment is the object), the merge of several
// segments, or an empty object.
func (s *Service) completeSmall(ctx context.Context, p pin, r row) error {
	// an upload that was created and never recorded is not this session's; one that was recorded has a part
	if err := s.dropUnrecorded(ctx, p, r); err != nil {
		return err
	}
	switch len(r.Tails) {
	case 1:
		return nil
	case 0:
		empty := sha256.Sum256(nil)
		var sum blob.Checksum
		if p.algorithm == "sha256" {
			sum = blob.Checksum{Algorithm: "sha256", Value: empty[:]}
		}
		return blob.MapStoreError(p.objects().Put(ctx, p.dataKey(r.ID), bytes.NewReader(nil), 0, sum, nil))
	}
	merged, err := s.joinTails(ctx, p, r, r.Tails, nil, nil)
	if err != nil {
		if errors.Is(err, errTailGone) {
			// This continuation holds no claim. The segments are gone because another one finished and cleaned up (the session moved on),
			// or because they were lost.
			cur, found, gerr := s.rows.get(ctx, r.ID, s.cfg.SessionMaxAge)
			if gerr != nil {
				return gerr
			}
			if !found || cur.State != StateCompleting {
				return errStale
			}
			return s.tailLost(ctx, r, err)
		}
		return err
	}
	defer merged.Close()
	return blob.MapStoreError(p.objects().Put(ctx, p.dataKey(r.ID), merged.Reader(), merged.Size, p.checksumOf(merged), nil))
}

// tailAsPart sends the recorded tail, its segments joined, as the last part. One continuation does it under a short claim of its own
// (another one waits for the release or the lease and is told to retry), so that the part is uploaded once and its ETag is the one
// the session records. It returns the session as it stands afterwards.
func (s *Service) tailAsPart(ctx context.Context, p pin, r row) (row, error) {
	writer := uuid.New().String()
	rctx, cancel := recordCtx(ctx)
	ok, err := s.rows.claimCompleting(rctx, r.ID, writer, s.cfg.WriterLease)
	cancel()
	if err != nil {
		return row{}, err
	}
	if !ok {
		cur, found, gerr := s.rows.get(ctx, r.ID, s.cfg.SessionMaxAge)
		if gerr != nil {
			return row{}, gerr
		}
		if !found || cur.State != StateCompleting || cur.TailSize == 0 {
			return row{}, errStale
		}
		return row{}, errs.E(errs.Unavailable, errs.CodeUnavailable, "The upload is being completed by another request; retry.", errs.WithRetryAfter(time.Second))
	}
	clean := false
	defer func() {
		if !clean {
			s.release(ctx, r.ID, writer)
		}
	}()
	// the row read before the claim may be older than the tail's removal by the continuation that held the claim before
	cur, found, err := s.rows.get(ctx, r.ID, s.cfg.SessionMaxAge)
	if err != nil {
		return row{}, err
	}
	if !found || cur.State != StateCompleting {
		return row{}, errStale
	}
	if cur.TailSize == 0 {
		return cur, nil
	}
	joined, err := s.joinTails(ctx, p, cur, cur.Tails, nil, s.completingRenewer(cur.ID, writer))
	if err != nil {
		if errors.Is(err, errLostClaim) { // another continuation took the step, or the session moved on
			return row{}, errStale
		}
		if errors.Is(err, errTailGone) { // the claim was valid when the segments were read
			return row{}, s.tailLost(ctx, cur, err)
		}
		return row{}, err
	}
	defer joined.Close()
	parts, err := s.rows.parts(ctx, cur.ID)
	if err != nil {
		return row{}, err
	}
	number := 1
	if n := len(parts); n > 0 {
		number = parts[n-1].Number + 1
	}
	var final *blobcrypt.Final
	if p.crypt != nil {
		f := blobcrypt.Final{Size: cur.Received}
		copy(f.Digest[:], cur.Digest)
		final = &f
	}
	sent, _, err := s.sendPart(ctx, p, cur.ID, cur.UploadID, number, joined, int64(number-1)*cur.PartSize, final, s.completingRenewer(cur.ID, writer))
	if err != nil {
		if errors.Is(err, errLostClaim) {
			return row{}, errStale
		}
		return row{}, err
	}
	rctx, cancel = recordCtx(ctx)
	err = s.rows.completeTail(rctx, cur.ID, writer, sent)
	cancel()
	if err != nil {
		if errors.Is(err, errLostClaim) {
			return row{}, errStale
		}
		clean = true // the record may have landed: the segments stay until the end of the session
		return row{}, err
	}
	clean = true
	for _, t := range cur.Tails {
		s.removeObject(ctx, p, p.tailKey(cur.ID, t.Writer))
	}
	out, _, err := s.rows.get(ctx, cur.ID, s.cfg.SessionMaxAge)
	return out, err
}

// isInvalidPart reports that the store refused to complete the upload because a part is not the one that was recorded.
func isInvalidPart(err error) bool {
	var se *blob.StoreError
	return errors.As(err, &se) && (se.Code == "InvalidPart" || se.Code == "InvalidPartOrder")
}
