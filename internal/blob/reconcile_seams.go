package blob

import "context"

// The two seams of the reconciliation between the connections (storage.md R-STOR-056, package internal/blob/reconcile): the
// mark of a copy that the source of a streamed copy showed to be damaged, and the signal after a commit that queued a gap.

// MarkCopyDamaged records evidence that a reader outside the download path found about one copy (the source of the
// reconciliation's streamed copy, R-STOR-056): CopyMissing when its connection answered "no such object", which marks the copy
// and looks for its object once more, as a read does (Q-STOR-151); CopyCorrupt when its size or, read to its end, its digest
// was another. The connection's repair is queued and the blob's content state follows from its copies (R-STOR-052, R-STOR-055).
func (s *Store) MarkCopyDamaged(ctx context.Context, d Digest, cp Copy, status string) {
	if status != CopyMissing {
		s.markCopy(ctx, d, cp.Connection, status)
		return
	}
	if c, ok := s.conns.Get(cp.Connection); ok {
		s.copyGone(ctx, d, c, cp)
	}
}

// SetGapSignal sets the function a commit calls after its transaction committed when it queued a gap of another connection:
// the reconciliation is signalled after such a commit and copies the blob within seconds (R-STOR-056). f must not block. It is
// set before the store commits; with one connection a commit queues no gap and nothing is signalled.
func (s *Store) SetGapSignal(f func()) { s.gapSignal.Store(&f) }

// signalGaps calls the gap signal when a member of a commit wrote fewer copies than the connections that hold every blob, so
// that its link round queued the others (recordWrites).
func (s *Store) signalGaps(ms []*member) {
	for _, m := range ms {
		if m.putDone && len(m.writes) < len(s.conns.Targets()) {
			s.signalGap(len(m.writes))
			return
		}
	}
}

// signalGap calls the gap signal when wrote copies leave a connection that holds every blob without one.
func (s *Store) signalGap(wrote int) {
	if wrote >= len(s.conns.Targets()) {
		return
	}
	if f := s.gapSignal.Load(); f != nil {
		(*f)()
	}
}
