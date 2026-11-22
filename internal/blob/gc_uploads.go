package blob

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// This file is the collection run's contact with the multi-request upload sessions (R-STOR-011 Expiry, R-STOR-025): the run aborts
// the sessions that nobody continues and removes the multipart upload and the staging objects of the sessions that are aborted or
// done, each on the session's own connection (R-STOR-054). What no session row stands for below a connection's staging area (a
// replica that crashed between creating the upload and recording the session, a repository that was deleted with its sessions, a
// session whose row went while its connection could not be asked) is the orphan sweep's (R-STOR-026, sweep.go).

// sessionBatch bounds the rows one statement of the session step takes.
const sessionBatch = 200

// WithUploadSessions makes the run expire and clean up the upload sessions with the idle time and the maximum age of cfg
// (storage.uploads.session_ttl, session_max_age). Without it a run leaves the sessions alone. The orphan sweep's staging step reads
// the maximum age from cfg as well when it is set, and from the collector's storage section otherwise.
func WithUploadSessions(cfg config.StorageUploads) CollectorOption {
	return func(c *Collector) { c.uploads = &cfg }
}

// sessionLimits is the idle time and the maximum age of the sessions: WithUploadSessions's, else the storage section's.
func (c *Collector) sessionLimits() config.StorageUploads {
	if c.uploads != nil {
		return *c.uploads
	}
	return c.storage.Uploads
}

// RemoveSession removes everything a session staged: its multipart upload and every object below its prefix. An upload the store
// does not know any more (completed or aborted already) and an object that is gone are success. The first failure is returned,
// after every step was tried.
func RemoveSession(ctx context.Context, objects ObjectStore, prefix, id, uploadID string) error {
	var errs []error
	if uploadID != "" {
		if err := objects.AbortMultipart(ctx, SessionKey(prefix, id, "data"), uploadID); err != nil && !IsNoSuchUpload(err) && !errors.Is(err, ErrNotFound) {
			errs = append(errs, fmt.Errorf("abort the multipart upload of session %s: %w", id, err))
		}
	}
	token := ""
	for {
		page, err := objects.List(ctx, SessionPrefix(prefix, id), ListOptions{Token: token})
		if err != nil {
			errs = append(errs, fmt.Errorf("list the objects of session %s: %w", id, err))
			break
		}
		for _, o := range page.Objects {
			if err := objects.Delete(ctx, o.Key); err != nil && !errors.Is(err, ErrNotFound) {
				errs = append(errs, fmt.Errorf("delete %s: %w", o.Key, err))
			}
		}
		if page.Next == "" {
			break
		}
		token = page.Next
	}
	return errors.Join(errs...)
}

// uploadSessions is the session step of a run.
func (r *run) uploadSessions(ctx context.Context) error {
	u := r.c.uploads
	if u == nil {
		return nil
	}
	if err := r.expireSessions(ctx, u); err != nil {
		return err
	}
	if r.opts.DryRun {
		return nil
	}
	return r.cleanSessions(ctx, u)
}

// expireSessions aborts the live sessions without a recorded request for the idle time and the open ones older than the maximum
// age. A dry run only counts them.
func (r *run) expireSessions(ctx context.Context, u *config.StorageUploads) error {
	if r.opts.DryRun {
		var n int64
		err := r.c.db.InTx(ctx, func(tx meta.Tx) error {
			qs, err := queries(tx)
			if err != nil {
				return err
			}
			n, err = qs.UploadCountExpired(ctx, q.UploadCountExpiredParams{TtlSeconds: u.SessionTTL.Seconds(), MaxAgeSeconds: u.SessionMaxAge.Seconds()})
			return err
		})
		r.res.UploadSessionsAborted += int(n)
		return err
	}
	for r.inBudget() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if stop, err := r.sessionPause(ctx); stop || err != nil {
			return err
		}
		var ids []string
		err := r.c.db.InTx(ctx, func(tx meta.Tx) error {
			qs, err := queries(tx)
			if err != nil {
				return err
			}
			ids, err = qs.UploadExpire(ctx, q.UploadExpireParams{
				TtlSeconds: u.SessionTTL.Seconds(), MaxAgeSeconds: u.SessionMaxAge.Seconds(), MaxRows: sessionBatch,
			})
			return err
		})
		if err != nil {
			return fmt.Errorf("expire upload sessions: %w", err)
		}
		r.res.UploadSessionsAborted += len(ids)
		r.c.metrics.UploadSession(SessionExpired, len(ids))
		if len(ids) < sessionBatch {
			return nil
		}
	}
	return nil // the rest is the next run's
}

// sessionPause re-reads the deletion pause before a batch of the session step (R-STOR-024): while it is set the step removes and aborts
// nothing, and the run ends as a paused one. The refusal of a request past its age does not depend on it.
func (r *run) sessionPause(ctx context.Context) (stop bool, err error) {
	paused, err := r.paused(ctx)
	if err != nil {
		return true, err
	}
	if paused {
		r.res.Paused = true
	}
	return paused, nil
}

// canonicalID reports that id is a session id in the one form the service writes, the lower-case hyphenated UUID: the parser accepts
// others (braced, 32 hexadecimal digits, urn:uuid:), and a directory in one of those is not a session of quader.
func canonicalID(id string) bool {
	u, err := uuid.Parse(id)
	return err == nil && u.String() == id
}

// cleanRow is a session that waits for the removal of its objects, on its connection (R-STOR-054). TooOld says that it is older than
// the maximum age, so that its row may go without its objects when its connection cannot be asked.
type cleanRow struct {
	ID, S3UploadID, Conn string
	TooOld               bool
}

// sessionBudget is the time the expiry and the cleaning of one run may take: they go on batch after batch until it is spent, and the rest
// is the next run's, so a backlog never keeps the blob collection below them waiting for hours (R-STOR-025).
const sessionBudget = 2 * time.Minute

// doneReplayWindow is how long a finished session stays, so that a closing request repeated after a cut answers from its record
// (Q-STOR-176). It is not the idle time: a finished session has nothing left to wait for.
const doneReplayWindow = 15 * time.Minute

// inBudget reports that the session step has time left; the first call starts the clock.
func (r *run) inBudget() bool {
	if r.sessionStart.IsZero() {
		r.sessionStart = time.Now()
	}
	return time.Since(r.sessionStart) < sessionBudget
}

// cleanSessions deletes the rows that need no call to the store (the sessions that staged nothing and those whose objects the request
// that ended them removed), then removes the objects of the others on their connections and deletes their rows. A session whose
// connection this replica does not configure, or whose circuit is open, keeps its row until its connection answers, unless it is older
// than the maximum age: then the row goes and the orphan sweep of that connection removes its objects (R-STOR-054). A session whose
// objects cannot be removed goes to the back of the line, so that a few bad rows do not stop the cleaning, and a batch in which nothing
// moved ends the step.
func (r *run) cleanSessions(ctx context.Context, u *config.StorageUploads) error {
	r.sessionStart = time.Time{} // the cleaning has a budget of its own: a backlog of expiry does not starve it
	for r.inBudget() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if stop, err := r.sessionPause(ctx); stop || err != nil {
			return err
		}
		bulk, rows, err := r.sessionsToClean(ctx, u)
		if err != nil {
			return err
		}
		r.res.UploadSessionsCleaned += int(bulk)
		cleaned := 0
		for _, row := range rows {
			done, err := r.cleanSession(ctx, row)
			if err != nil {
				return err
			}
			if done {
				cleaned++
			}
		}
		r.res.UploadSessionsCleaned += cleaned
		if (bulk < sessionBatch && len(rows) < sessionBatch) || (bulk == 0 && cleaned == 0) {
			return nil
		}
	}
	return nil
}

// sessionsToClean deletes the rows that need no call to the store and lists the next sessions whose objects are to be removed.
func (r *run) sessionsToClean(ctx context.Context, u *config.StorageUploads) (int64, []cleanRow, error) {
	window, maxAge := doneReplayWindow.Seconds(), u.SessionMaxAge.Seconds()
	var bulk int64
	var rows []cleanRow
	err := r.c.db.InTx(ctx, func(tx meta.Tx) error {
		bulk, rows = 0, rows[:0]
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		if bulk, err = qs.UploadDeleteCleanedBulk(ctx, q.UploadDeleteCleanedBulkParams{WindowSeconds: window, MaxRows: sessionBatch}); err != nil {
			return err
		}
		aborted, err := qs.UploadListAborted(ctx, q.UploadListAbortedParams{MaxAgeSeconds: maxAge, MaxRows: sessionBatch})
		if err != nil {
			return err
		}
		for _, a := range aborted {
			rows = append(rows, cleanRow{ID: a.ID, S3UploadID: a.S3UploadID, Conn: a.ConnectionID, TooOld: a.TooOld})
		}
		if len(rows) >= sessionBatch {
			return nil
		}
		done, err := qs.UploadListDone(ctx, q.UploadListDoneParams{
			MaxAgeSeconds: maxAge, WindowSeconds: window, MaxRows: int32(sessionBatch - len(rows)), //nolint:gosec // at most sessionBatch
		})
		if err != nil {
			return err
		}
		for _, d := range done {
			rows = append(rows, cleanRow{ID: d.ID, S3UploadID: d.S3UploadID, Conn: d.ConnectionID, TooOld: d.TooOld})
		}
		return nil
	})
	if err != nil {
		return 0, nil, fmt.Errorf("list upload sessions to clean: %w", err)
	}
	return bulk, rows, nil
}

// cleanSession removes the objects of one session on its connection and deletes its row; it reports whether the row went. An error
// is returned only when the run must stop (its context, the database).
func (r *run) cleanSession(ctx context.Context, row cleanRow) (bool, error) {
	conn, ok := r.c.conns.Get(row.Conn)
	reachable := ok
	if ok {
		_, rejecting := conn.Rejecting()
		reachable = !rejecting
	}
	if reachable {
		err := RemoveSession(ctx, conn.Objects(), conn.Prefix(), row.ID, row.S3UploadID)
		switch {
		case err == nil:
			return true, r.deleteSessionRow(ctx, row.ID)
		case ctx.Err() != nil:
			return false, ctx.Err()
		case !errors.Is(err, ErrCircuitOpen):
			r.res.Errors++
			r.c.log.Warn("could not remove the objects of an upload session", "session", row.ID, "connection", row.Conn, "error", err)
			return false, r.bumpSession(ctx, row.ID)
		}
		// the circuit opened while the session's objects were removed: as for an unreachable connection
	}
	if row.TooOld {
		// the session's connection cannot be asked and the row is past the maximum age: the orphan sweep of that connection removes
		// what the session staged there (R-STOR-054, R-STOR-026)
		return true, r.deleteSessionRow(ctx, row.ID)
	}
	return false, r.bumpSession(ctx, row.ID)
}

func (r *run) deleteSessionRow(ctx context.Context, id string) error {
	if err := r.c.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		_, err = qs.UploadDeleteCleaned(ctx, id)
		return err
	}); err != nil {
		return fmt.Errorf("delete an upload session: %w", err)
	}
	return nil
}

// bumpSession moves a session to the back of the line of the cleaning.
func (r *run) bumpSession(ctx context.Context, id string) error {
	if err := r.c.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		return qs.UploadBumpActivity(ctx, id)
	}); err != nil {
		return fmt.Errorf("move an upload session back in the line: %w", err)
	}
	return nil
}
