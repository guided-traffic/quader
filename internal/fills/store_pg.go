package fills

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Database is the PostgreSQL store: transactions and plain reads. *postgres.DB implements it.
type Database interface {
	meta.Runner
	Read(ctx context.Context, fn func(q.DBTX) error) error
}

// Store writes and reads the two tables of the downloads from upstreams (internal/meta/postgres/queries/upstream_downloads.sql).
type Store struct {
	db Database
}

// NewStore creates the store.
func NewStore(db Database) *Store { return &Store{db: db} }

var errNotPostgres = errors.New("fills: the transaction does not come from the PostgreSQL store")

// Publication is what one publication of a replica writes, in one transaction.
type Publication struct {
	InstanceUID, Replica string
	// Running are the running downloads; a publication with some upserts the replica's live row.
	Running []Entry
	Omitted int
	// DeleteLive removes the replica's live row: its last running download ended, or it shuts down.
	DeleteLive bool
	// Ended are inserted (an end replaces an earlier one of the same id) and the table is trimmed to MaxFinished.
	Ended []Finished
}

// Publish writes a publication (R-OPS-050, R-OPS-051).
func (s *Store) Publish(ctx context.Context, p Publication) error {
	var running, ended []byte
	var err error
	if len(p.Running) > 0 {
		if running, err = json.Marshal(p.Running); err != nil {
			return err
		}
	}
	if len(p.Ended) > 0 {
		if ended, err = json.Marshal(p.Ended); err != nil {
			return err
		}
	}
	return s.inTx(ctx, func(qs *q.Queries) error {
		switch {
		case running != nil:
			if err := qs.UpstreamDownloadLiveUpsert(ctx, q.UpstreamDownloadLiveUpsertParams{
				InstanceUid: p.InstanceUID, ReplicaName: p.Replica, Downloads: string(running), Omitted: int32(min(p.Omitted, 1<<30)), //nolint:gosec // bounded above
			}); err != nil {
				return err
			}
		case p.DeleteLive:
			if err := qs.UpstreamDownloadLiveDelete(ctx, p.InstanceUID); err != nil {
				return err
			}
		}
		if ended == nil {
			return nil
		}
		if err := qs.UpstreamDownloadFinishedInsert(ctx, string(ended)); err != nil {
			return err
		}
		return qs.UpstreamDownloadFinishedTrim(ctx)
	})
}

// Sweep removes the live rows written before olderThan and records their downloads as interrupted, ended at the row's last write, in
// one transaction (R-OPS-051). It returns the number of rows it removed.
func (s *Store) Sweep(ctx context.Context, olderThan time.Duration) (int, error) {
	n := 0
	err := s.inTx(ctx, func(qs *q.Queries) error {
		rows, err := qs.UpstreamDownloadSweep(ctx, olderThan.Seconds())
		if err != nil {
			return err
		}
		n = len(rows)
		var all []Finished
		for _, r := range rows {
			var entries []Entry
			if err := json.Unmarshal([]byte(r.Downloads), &entries); err != nil {
				continue // a row this release cannot read ends without its downloads
			}
			all = append(all, Interrupted(r.ReplicaName, time.UnixMicro(r.WrittenUs).UTC(), entries)...)
		}
		if len(all) == 0 {
			return nil
		}
		b, err := json.Marshal(all)
		if err != nil {
			return err
		}
		if err := qs.UpstreamDownloadInterruptedInsert(ctx, string(b)); err != nil {
			return err
		}
		return qs.UpstreamDownloadFinishedTrim(ctx)
	})
	return n, err
}

// ReadUpstreamDownloads reads what the API answers (R-OPS-052): every live row with its age, the newest MaxFinished finished downloads,
// and the current names of the repositories and principals they name. The statements run on one connection outside a transaction; a row
// that changes between them is read as it is then, which the next poll corrects.
func (s *Store) ReadUpstreamDownloads(ctx context.Context) (View, error) {
	var v View
	err := s.db.Read(ctx, func(db q.DBTX) error {
		qs := q.New(db)
		v = View{Live: nil, Finished: nil}
		nowUs, err := qs.UpstreamDownloadNow(ctx)
		if err != nil {
			return err
		}
		v.Now = time.UnixMicro(nowUs).UTC()
		live, err := qs.UpstreamDownloadLiveList(ctx)
		if err != nil {
			return err
		}
		for _, r := range live {
			l := Live{InstanceUID: r.InstanceUid, Replica: r.ReplicaName, WrittenAt: time.UnixMicro(r.WrittenUs).UTC(),
				Age: max(time.Duration(r.AgeUs)*time.Microsecond, 0), Omitted: int(r.Omitted)}
			if err := json.Unmarshal([]byte(r.Downloads), &l.Downloads); err != nil {
				l.Downloads = nil // a row this release cannot read counts its omitted downloads only
			}
			v.Live = append(v.Live, l)
		}
		docs, err := qs.UpstreamDownloadFinishedList(ctx)
		if err != nil {
			return err
		}
		for _, d := range docs {
			var f Finished
			if err := json.Unmarshal([]byte(d), &f); err != nil {
				return err
			}
			v.Finished = append(v.Finished, f)
		}
		return s.names(ctx, qs, &v)
	})
	return v, err
}

// names resolves the repositories and principals of a view by id.
func (s *Store) names(ctx context.Context, qs *q.Queries, v *View) error {
	repoIDs, principals := map[int64]bool{}, map[string]bool{}
	for _, l := range v.Live {
		for _, e := range l.Downloads {
			repoIDs[e.RepoID] = true
			if e.By != "" {
				principals[e.By] = true
			}
		}
	}
	for _, f := range v.Finished {
		repoIDs[f.RepoID] = true
		if f.By != nil {
			principals[*f.By] = true
		}
	}
	v.Repos, v.Principals = map[int64]RepoName{}, map[string]string{}
	if len(repoIDs) > 0 {
		ids := make([]int64, 0, len(repoIDs))
		for id := range repoIDs {
			ids = append(ids, id)
		}
		rows, err := qs.UpstreamDownloadRepositoryNames(ctx, ids)
		if err != nil {
			return err
		}
		for _, r := range rows {
			v.Repos[r.ID] = RepoName{Project: r.Project, Key: r.Key, Format: r.Format}
		}
	}
	if len(principals) > 0 {
		ids := make([]string, 0, len(principals))
		for id := range principals {
			if validUUID(id) {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return nil
		}
		rows, err := qs.UpstreamDownloadPrincipalNames(ctx, ids)
		if err != nil {
			return err
		}
		for _, r := range rows {
			v.Principals[r.ID] = r.Name
		}
	}
	return nil
}

func (s *Store) inTx(ctx context.Context, fn func(qs *q.Queries) error) error {
	return s.db.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errNotPostgres
		}
		return fn(q.New(ptx))
	})
}
