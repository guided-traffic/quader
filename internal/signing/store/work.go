package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signing"
)

// The work of the job signing (R-SIG-049): a row per item and kind in signing_work. The packages that sign write it in the commit's
// transaction (Enqueue); the job reads the due rows by priority and time, locks one per operation, and deletes it when its operation is
// done or spends an attempt when it could not run.

// Work is a row of signing_work.
type Work struct {
	Kind      string
	ItemID    int64
	Reason    string
	Priority  int
	NotBefore time.Time
	Attempts  int
}

// Key is the row's key as the in-flight list of SigningDueWork names it.
func (w Work) Key() string { return w.Kind + ":" + strconv.FormatInt(w.ItemID, 10) }

// Backlog is the work of one kind and priority (R-SIG-055, R-SIG-057): the rows that wait or are due, the failed ones, and the oldest
// due time of what waits (zero when nothing is due).
type Backlog struct {
	Kind      string
	Priority  int
	Failed    bool
	Rows      int64
	OldestDue time.Time
}

// Enqueue writes work for items of one kind in the caller's transaction and wakes the job (R-SIG-049): a row that waits keeps the higher
// priority and the earlier time; a failed one starts its attempts again. delay postpones the first attempt (an attestation that waits
// for its checks, R-SIG-053).
func (s *Store) Enqueue(ctx context.Context, tx meta.Tx, kind, reason string, priority int, items []int64, delay time.Duration) error {
	if len(items) == 0 {
		return nil
	}
	if priority < signing.PriorityPublish || priority > signing.PriorityDiscovery {
		return fmt.Errorf("signing work: priority %d is out of 0 to 3", priority)
	}
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	if _, err := qs.SigningEnqueueWork(ctx, q.SigningEnqueueWorkParams{
		Kind: kind, Reason: reason, Priority: int16(priority), ItemIds: items, DelaySecs: delay.Seconds(), //nolint:gosec // 0 to 3
	}); err != nil {
		return fmt.Errorf("enqueue %s work: %w", kind, err)
	}
	return s.wake(ctx, tx)
}

// EnqueueWhileCurrent writes work for items of one kind in the caller's transaction, as Enqueue does, but only while an identity is
// current (Q-SIG-158): the work of a publish before the installation's first identity is left to discovery, so an installation that
// never signs keeps no rows. It wakes the job when it wrote a row.
func (s *Store) EnqueueWhileCurrent(ctx context.Context, tx meta.Tx, kind, reason string, priority int, items []int64) error {
	if len(items) == 0 {
		return nil
	}
	if priority < signing.PriorityPublish || priority > signing.PriorityDiscovery {
		return fmt.Errorf("signing work: priority %d is out of 0 to 3", priority)
	}
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	n, err := qs.SigningEnqueueWorkWhileCurrent(ctx, q.SigningEnqueueWorkWhileCurrentParams{
		Kind: kind, Reason: reason, Priority: int16(priority), ItemIds: items, //nolint:gosec // 0 to 3
	})
	if err != nil {
		return fmt.Errorf("enqueue %s work: %w", kind, err)
	}
	if n == 0 {
		return nil
	}
	return s.wake(ctx, tx)
}

// Served answers the ids of the identities whose signatures of a kind the documents served when the job last compared (Q-SIG-160);
// found is false before the first comparison.
func (s *Store) Served(ctx context.Context, kind string) (ids []int64, found bool, err error) {
	err = s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		r, err := qs.SigningServed(ctx, kind)
		if err != nil {
			return err
		}
		ids, found = []int64{}, r.Found
		return json.Unmarshal([]byte(r.Ids), &ids)
	})
	return ids, found, err
}

// SetServed stores what the documents serve of a kind after the job raised the revisions a change of it needed.
func (s *Store) SetServed(ctx context.Context, kind string, ids []int64) error {
	if ids == nil {
		ids = []int64{}
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		return qs.SigningSetServed(ctx, q.SigningSetServedParams{Kind: kind, Ids: string(raw)})
	})
}

// DueWork answers at most limit rows of the kinds that are due, by priority, time and item, without the rows in flight.
func (s *Store) DueWork(ctx context.Context, kinds []string, limit int, inFlight []string) ([]Work, error) {
	var out []Work
	if inFlight == nil {
		inFlight = []string{} // = ANY(NULL) would exclude every row
	}
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.SigningDueWork(ctx, q.SigningDueWorkParams{Kinds: kinds, InFlight: inFlight, MaxRows: int64(limit)})
		if err != nil {
			return err
		}
		out = make([]Work, 0, len(rows))
		for _, r := range rows {
			out = append(out, Work{
				Kind: r.Kind, ItemID: r.ItemID, Reason: r.Reason, Priority: int(r.Priority), NotBefore: parseTime(r.NotBefore),
				Attempts: int(r.Attempts),
			})
		}
		return nil
	})
	return out, err
}

// LockWork locks a row of work for its operation in tx; ok is false when another replica holds it or it is done.
func LockWork(ctx context.Context, tx meta.Tx, w Work) (ok bool, err error) {
	qs, err := queries(tx)
	if err != nil {
		return false, err
	}
	rows, err := qs.SigningLockWork(ctx, q.SigningLockWorkParams{Kind: w.Kind, ItemID: w.ItemID})
	return len(rows) == 1, err
}

// DoneWork removes a row of work whose operation is done, in the operation's transaction.
func DoneWork(ctx context.Context, tx meta.Tx, w Work) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	return qs.SigningDoneWork(ctx, q.SigningDoneWorkParams{Kind: w.Kind, ItemID: w.ItemID})
}

// PostponeWork makes a row due again at notBefore in the operation's transaction, spending no attempt (signing.Later, R-SIG-053).
func PostponeWork(ctx context.Context, tx meta.Tx, w Work, notBefore time.Time) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	return qs.SigningPostponeWork(ctx, q.SigningPostponeWorkParams{Kind: w.Kind, ItemID: w.ItemID, NotBefore: stamp(notBefore)})
}

// DeferWork spends an attempt of a row (R-SIG-049): due again at notBefore, or failed with the code of the attempt.
func (s *Store) DeferWork(ctx context.Context, w Work, notBefore time.Time, failed bool, code string) error {
	return s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		return qs.SigningDeferWork(ctx, q.SigningDeferWorkParams{
			Kind: w.Kind, ItemID: w.ItemID, NotBefore: stamp(notBefore), Failed: failed, Code: bounded(code, 64),
		})
	})
}

// NextDue answers the earliest time a row of the kinds that waits becomes due; zero when nothing waits.
func (s *Store) NextDue(ctx context.Context, kinds []string) (time.Time, error) {
	var out time.Time
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		text, err := qs.SigningNextDue(ctx, kinds)
		out = parseTime(text)
		return err
	})
	return out, err
}

// Backlog answers the work per kind, priority and whether it failed.
func (s *Store) Backlog(ctx context.Context) ([]Backlog, error) {
	var out []Backlog
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		var err error
		out, err = backlog(ctx, qs)
		return err
	})
	return out, err
}

func backlog(ctx context.Context, qs *q.Queries) ([]Backlog, error) {
	rows, err := qs.SigningBacklog(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Backlog, 0, len(rows))
	for _, r := range rows {
		out = append(out, Backlog{Kind: r.Kind, Priority: int(r.Priority), Failed: r.Failed, Rows: r.Rows, OldestDue: parseTime(r.OldestDue)})
	}
	return out, nil
}

// RetryFailedDaily makes the failed work due again once a day (R-SIG-049): the first call after a day passed claims the retry and answers
// the rows it made due.
func (s *Store) RetryFailedDaily(ctx context.Context) (int64, error) {
	var n int64
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		claimed, err := qs.SigningClaimDailyRetry(ctx)
		if err != nil || claimed == 0 {
			return err
		}
		n, err = qs.SigningUnfailWork(ctx)
		return err
	})
	return n, err
}

// ClaimFailedReport answers whether signing.failed may be recorded now for the kind and code: at most once an hour each (R-SIG-057).
func (s *Store) ClaimFailedReport(ctx context.Context, kind, code string) (bool, error) {
	var ok bool
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		n, err := qs.SigningClaimFailedReport(ctx, kind+"/"+code)
		ok = n == 1
		return err
	})
	return ok, err
}

// Discovery answers discovery's cursor per kind (R-SIG-049): the last item id it looked at.
func (s *Store) Discovery(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		st, err := qs.SigningSettings(ctx)
		if err != nil {
			return err
		}
		if len(st.Discovery) > 0 {
			return json.Unmarshal(st.Discovery, &out)
		}
		return nil
	})
	return out, err
}

// SetDiscovery stores discovery's cursor of a kind.
func (s *Store) SetDiscovery(ctx context.Context, kind string, after int64) error {
	return s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		return qs.SigningSetDiscovery(ctx, q.SigningSetDiscoveryParams{Kind: kind, AfterID: after})
	})
}

// Attestation answers the bundle of Quader's receipt attestation of the live file at a path of a repository (R-SIG-054), while the
// identity that made it is current or previous; found is false for a file without one, or one of a retired identity (R-SIG-048).
func (s *Store) Attestation(ctx context.Context, repoID int64, path string) (bundle []byte, found bool, err error) {
	err = s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.SigningAttestationOf(ctx, q.SigningAttestationOfParams{RepoID: repoID, Path: path})
		if err != nil || len(rows) == 0 {
			return err
		}
		bundle, found = rows[0].Raw, true
		return nil
	})
	return bundle, found, err
}
