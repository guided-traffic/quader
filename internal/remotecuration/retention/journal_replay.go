package retention

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
)

// The replay of remote.examination.discarded after a restore (R-CUR-016, ha-and-helm.md R-HA-054). The restore returned the repository
// to the backup's state, so content a discard removed after the restore point may be stored again. The replay removes nothing itself
// (a replay never deletes content, R-HA-054): it makes the subject's examination rows due, adding one for a stored version that had
// none, and enqueues a run of the task remote-examination-cleanup, which a worker runs once the installation serves again. That run
// evicts what is stored of each due version while the rules, as the restored and replayed state has them, still do not deliver it, and
// leaves a delivered version (Q-CUR-144). So the replay does what the entry says and nothing else: what a rule allowed in between is
// not removed.

func init() {
	journal.Register(journal.Handler{
		Domain: "remotecuration",
		Events: []string{audit.EventRemoteExaminationDiscarded},
		Apply:  replayDiscard,
	})
}

// maxEntryName and maxEntryVersion bound each string of an entry before it reaches a statement: the columns' own bounds.
const (
	maxEntryName    = 1024
	maxEntryVersion = 256
)

// decodeDiscard parses an entry; the second result says why it is no entry the replay can act on, "" for a usable one.
func decodeDiscard(raw json.RawMessage) (discardEntry, int64, string) {
	var e discardEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		return e, 0, "the entry is not a remote.examination.discarded entry"
	}
	id, err := strconv.ParseInt(e.RepositoryID, 10, 64)
	switch {
	case err != nil || id <= 0:
		return e, 0, "the entry is incomplete: no repository"
	case e.Name == "" || len(e.Name) > maxEntryName:
		return e, 0, "the entry is incomplete: no package name"
	case len(e.Version) > maxEntryVersion:
		return e, 0, "the entry is incomplete: the version is too long"
	}
	return e, id, ""
}

// cleanupSpec is the kind of the task remote-examination-cleanup as the application registers it, minus its handler (jobs.EnqueueTx).
func cleanupSpec() jobs.KindSpec {
	return jobs.KindSpec{Queue: "maintenance", DedupMode: jobs.Exclusive, MaxAttempts: 1}
}

// CleanupKindSpec is the registration of the task's kind with its handler: one run at a time on the queue maintenance.
func CleanupKindSpec(h jobs.Handler) jobs.KindSpec {
	s := cleanupSpec()
	s.Handler = h
	return s
}

// due is the fetched_at of an examination row the replay made due: the epoch, older than any repository's days.
var due = time.Unix(0, 0)

// replayDiscard applies one remote.examination.discarded entry.
func replayDiscard(ctx context.Context, tx meta.Tx, _ journal.Env, o journal.Object) (journal.Result, error) {
	res := journal.Result{ID: o.ID, Event: o.Event}
	e, repoID, why := decodeDiscard(o.Entry)
	if why != "" {
		res.Outcome = journal.Skipped
		res.Detail, _ = json.Marshal(map[string]string{"reason": why}) //nolint:errcheck // a map of strings marshals
		return res, nil
	}
	res.Subject = e.Key + " " + e.Name
	if e.Version != "" {
		res.Subject += "@" + e.Version
	}
	qs, err := queries(tx)
	if err != nil {
		return res, err
	}
	exists, err := qs.RemoteRepositoryExists(ctx, repoID)
	if err != nil {
		return res, err
	}
	if !exists {
		res.Outcome = journal.SubjectUnknown
		return res, nil
	}
	versions, inEffect, err := replayVersions(ctx, qs, repoID, e)
	if err != nil {
		return res, err
	}
	if len(versions) == 0 {
		res.Outcome = journal.SubjectUnknown
		return res, nil
	}
	if inEffect {
		res.Outcome = journal.AlreadyInEffect
		return res, nil
	}
	for _, v := range versions {
		if err := qs.RemoteExaminationMakeDue(ctx, q.RemoteExaminationMakeDueParams{RepoID: repoID, Name: e.Name, Version: v}); err != nil {
			return res, err
		}
	}
	if _, err := jobs.EnqueueTx(ctx, tx, cleanupSpec(), jobs.EnqueueSpec{
		Kind: rc.TaskExaminationCleanup, DedupKey: rc.TaskExaminationCleanup, Args: struct{}{},
	}); err != nil {
		return res, err
	}
	res.Outcome = journal.Applied
	return res, nil
}

// replayVersions are the versions of the entry's subject in the restored database: the version, or every version of the package that
// has an examination row or is stored. inEffect is true when each has an examination row that is due already (a replay resumed).
func replayVersions(ctx context.Context, qs *q.Queries, repoID int64, e discardEntry) ([]string, bool, error) {
	exams, err := qs.RemoteExaminationsOf(ctx, q.RemoteExaminationsOfParams{RepoID: repoID, Name: e.Name, Version: e.Version})
	if err != nil {
		return nil, false, err
	}
	stored, err := qs.RemoteStoredVersionFiles(ctx, q.RemoteStoredVersionFilesParams{RepoID: repoID, Name: e.Name})
	if err != nil {
		return nil, false, err
	}
	dueRows := map[string]bool{}
	var versions []string
	seen := map[string]bool{}
	for _, x := range exams {
		dueRows[x.Version] = !x.FetchedAt.Time.After(due)
		if !seen[x.Version] {
			seen[x.Version] = true
			versions = append(versions, x.Version)
		}
	}
	for _, s := range stored {
		if (e.Version == "" || s.Version == e.Version) && !seen[s.Version] {
			seen[s.Version] = true
			versions = append(versions, s.Version)
		}
	}
	if e.Version != "" && !seen[e.Version] {
		// The version is neither examined nor stored by the package rows; a format whose remote writes no version rows (OCI) may still
		// store it, and the retention's own plan finds its recorded files only through a row.
		versions = append(versions, e.Version)
	}
	inEffect := len(versions) > 0
	for _, v := range versions {
		if !dueRows[v] {
			inEffect = false
		}
	}
	return versions, inEffect, nil
}
