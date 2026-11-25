package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The syncs over PostgreSQL (internal/meta/postgres/queries/remote_downloads.sql, migration 00130, R-CUR-027).

func decodeSync(doc string) (Sync, error) {
	var s Sync
	if err := json.Unmarshal([]byte(doc), &s); err != nil {
		return Sync{}, fmt.Errorf("queue: a sync does not decode: %w", err)
	}
	return s, nil
}

func firstSync(docs []string, err error) (Sync, bool, error) {
	if err != nil || len(docs) == 0 {
		return Sync{}, false, err
	}
	s, err := decodeSync(docs[0])
	return s, err == nil, err
}

// validID reports whether id is a UUID: an id of another form names no sync.
func validID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

func (t *pgTx) InsertSync(ctx context.Context, s Sync) (Sync, bool, error) {
	if s.ID == "" {
		s.ID = uuid.Must(uuid.NewV7()).String()
	}
	now := time.Now()
	if s.LastRunAt != nil {
		now = *s.LastRunAt
	}
	docs, err := t.qs.RemoteDownloadSyncInsert(ctx, q.RemoteDownloadSyncInsertParams{
		ID: s.ID, RepoID: s.RepoID, Level: string(s.Level), Subject: s.Subject, MaxVersions: int32(s.MaxVersions), //nolint:gosec // bounded by the configuration
		MaxBytes: s.MaxBytes, IncludePrereleases: s.IncludePrereleases, FromVersion: s.FromVersion, ToVersion: s.ToVersion,
		ToInclusive: s.ToInclusive, NextRunUs: us(s.NextRunAt), NowUs: us(now), CreatedBy: s.CreatedBy,
	})
	return firstSync(docs, err)
}

func (t *pgTx) SyncOfSubject(ctx context.Context, repoID int64, level Level, subject string) (Sync, bool, error) {
	return firstSync(t.qs.RemoteDownloadSyncOfSubject(ctx, q.RemoteDownloadSyncOfSubjectParams{RepoID: repoID, Level: string(level), Subject: subject}))
}

func (t *pgTx) SyncCount(ctx context.Context, repoID int64) (int, error) {
	n, err := t.qs.RemoteDownloadSyncCount(ctx, repoID)
	return int(n), err
}

func (t *pgTx) LockSync(ctx context.Context, repoID int64, id string) (Sync, bool, error) {
	if !validID(id) {
		return Sync{}, false, nil
	}
	return firstSync(t.qs.RemoteDownloadSyncLock(ctx, q.RemoteDownloadSyncLockParams{ID: id, RepoID: repoID}))
}

func (t *pgTx) LockDueSync(ctx context.Context, id string, now time.Time) (Sync, bool, error) {
	return firstSync(t.qs.RemoteDownloadSyncLockDue(ctx, q.RemoteDownloadSyncLockDueParams{ID: id, NowUs: us(now)}))
}

func (t *pgTx) SyncRan(ctx context.Context, id string, now, next time.Time, outcome SyncOutcome) error {
	return t.qs.RemoteDownloadSyncRan(ctx, q.RemoteDownloadSyncRanParams{ID: id, NowUs: us(now), NextRunUs: us(next), Outcome: string(outcome)})
}

func (t *pgTx) SetSyncPaused(ctx context.Context, repoID int64, id string, paused bool) (Sync, bool, error) {
	if !validID(id) {
		return Sync{}, false, nil
	}
	return firstSync(t.qs.RemoteDownloadSyncSetPaused(ctx, q.RemoteDownloadSyncSetPausedParams{ID: id, RepoID: repoID, Paused: paused}))
}

func (t *pgTx) DeleteSync(ctx context.Context, repoID int64, id string) error {
	_, err := t.qs.RemoteDownloadSyncDelete(ctx, q.RemoteDownloadSyncDeleteParams{ID: id, RepoID: repoID})
	return err
}

func (t *pgTx) LastRun(ctx context.Context, syncID string) (Bulk, bool, error) {
	docs, err := t.qs.RemoteDownloadSyncLastBulks(ctx, []string{syncID})
	if err != nil || len(docs) == 0 {
		return Bulk{}, false, err
	}
	b, err := decodeBulk(docs[0])
	return b, err == nil, err
}

func (t *pgTx) StoredBytes(ctx context.Context, repoID int64) (int64, error) {
	return t.qs.RemoteDownloadSyncUsage(ctx, repoID)
}

// ListSyncs implements [Reader].
func (s *PGStore) ListSyncs(ctx context.Context, f BulkFilter) (out []Sync, last map[string]Bulk, counts map[string]map[string]int64, err error) {
	p := q.RemoteDownloadSyncListParams{RepoID: f.RepoID, MaxRows: int32(min(max(f.Limit, 1), 1000))} //nolint:gosec // bounded
	if f.After != nil {
		p.AfterUs, p.AfterID = f.After.CreatedAt.UnixMicro(), f.After.ID
	}
	err = s.read(ctx, func(qs *q.Queries) error {
		docs, e := qs.RemoteDownloadSyncList(ctx, p)
		if e != nil {
			return e
		}
		out = make([]Sync, 0, len(docs))
		ids := make([]string, 0, len(docs))
		for _, doc := range docs {
			sy, e := decodeSync(doc)
			if e != nil {
				return e
			}
			out, ids = append(out, sy), append(ids, sy.ID)
		}
		last, counts, e = lastRuns(ctx, qs, ids)
		return e
	})
	return out, last, counts, err
}

// lastRuns reads the newest run of each sync, by the sync's ID, and the counts of its rows by state, by the bulk's ID.
func lastRuns(ctx context.Context, qs *q.Queries, ids []string) (map[string]Bulk, map[string]map[string]int64, error) {
	last, counts := map[string]Bulk{}, map[string]map[string]int64{}
	if len(ids) == 0 {
		return last, counts, nil
	}
	docs, err := qs.RemoteDownloadSyncLastBulks(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	bulkIDs := make([]string, 0, len(docs))
	for _, doc := range docs {
		b, err := decodeBulk(doc)
		if err != nil {
			return nil, nil, err
		}
		last[b.SyncID] = b
		bulkIDs = append(bulkIDs, b.ID)
	}
	if len(bulkIDs) == 0 {
		return last, counts, nil
	}
	rows, err := qs.RemoteDownloadBulksStateCounts(ctx, bulkIDs)
	if err != nil {
		return nil, nil, err
	}
	for _, r := range rows {
		if counts[r.BulkID] == nil {
			counts[r.BulkID] = map[string]int64{}
		}
		counts[r.BulkID][r.State] = r.N
	}
	return last, counts, nil
}

// GetSync implements [Reader].
func (s *PGStore) GetSync(ctx context.Context, repoID int64, id string) (sy Sync, run *Bulk, counts map[string]int64, found bool, err error) {
	if !validID(id) {
		return Sync{}, nil, nil, false, nil
	}
	err = s.read(ctx, func(qs *q.Queries) error {
		var e error
		if sy, found, e = firstSync(qs.RemoteDownloadSyncGet(ctx, q.RemoteDownloadSyncGetParams{ID: id, RepoID: repoID})); e != nil || !found {
			return e
		}
		last, all, e := lastRuns(ctx, qs, []string{id})
		if b, ok := last[id]; ok {
			run, counts = &b, all[b.ID]
		}
		return e
	})
	return sy, run, counts, found, err
}

// DueSyncs implements [Maintenance].
func (s *PGStore) DueSyncs(ctx context.Context, now time.Time) (out []Sync, err error) {
	err = s.read(ctx, func(qs *q.Queries) error {
		rows, e := qs.RemoteDownloadSyncsDue(ctx, us(now))
		for _, r := range rows {
			out = append(out, Sync{ID: r.ID, RepoID: r.RepoID})
		}
		return e
	})
	return out, err
}

// SyncGauge implements [Maintenance].
func (s *PGStore) SyncGauge(ctx context.Context) (out []GaugeRow, err error) {
	err = s.read(ctx, func(qs *q.Queries) error {
		rows, e := qs.RemoteDownloadSyncGauge(ctx)
		for _, r := range rows {
			out = append(out, GaugeRow{RepoID: r.RepoID, State: r.State, N: r.N})
		}
		return e
	})
	return out, err
}

// FixedVersions reads the live version rows of a package by its names (Q-CUR-244); the composition root's [SyncRepositories.Fixed]
// compares them in the format's canonical form.
func (s *PGStore) FixedVersions(ctx context.Context, repoID int64, names []string) (out []string, err error) {
	err = s.read(ctx, func(qs *q.Queries) (e error) {
		out, e = qs.RemoteDownloadFixedVersions(ctx, q.RemoteDownloadFixedVersionsParams{RepoID: repoID, Names: names})
		return e
	})
	return out, err
}
