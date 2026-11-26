package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/remotecuration"
)

// Database is the PostgreSQL store: transactions and plain reads. *postgres.DB implements it.
type Database interface {
	meta.Runner
	Read(ctx context.Context, fn func(q.DBTX) error) error
}

// Enqueuer enqueues a job in the caller's transaction; *jobs.Queue implements it.
type Enqueuer interface {
	Enqueue(ctx context.Context, tx meta.Tx, e jobs.EnqueueSpec) (jobs.Result, error)
}

// RunArgs are the arguments of a job remote-download: the row it runs.
type RunArgs struct {
	Row string `json:"row"`
}

// RunKey is the dedup key of a row's job: one attempt of a row at a time (exclusive).
func RunKey(rowID string) string { return remotecuration.KindDownload + ":" + rowID }

// PGStore is the Store over PostgreSQL (internal/meta/postgres/queries/remote_downloads.sql).
type PGStore struct {
	db   Database
	jobs Enqueuer
}

var (
	_ Store       = (*PGStore)(nil)
	_ Maintenance = (*PGStore)(nil)
)

// NewPGStore creates the store; jobs enqueues the attempts.
func NewPGStore(db Database, jobs Enqueuer) *PGStore { return &PGStore{db: db, jobs: jobs} }

var errNotPostgres = errors.New("queue: the transaction does not come from the PostgreSQL store")

// InTx implements [Store].
func (s *PGStore) InTx(ctx context.Context, fn func(Tx) error) error {
	return s.db.InTx(ctx, func(tx meta.Tx) error { return s.Within(ctx, tx, fn) })
}

// Within implements [Store].
func (s *PGStore) Within(_ context.Context, tx meta.Tx, fn func(Tx) error) error {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return errNotPostgres
	}
	return fn(&pgTx{s: s, tx: tx, qs: q.New(ptx)})
}

func (s *PGStore) read(ctx context.Context, fn func(qs *q.Queries) error) error {
	return s.db.Read(ctx, func(db q.DBTX) error { return fn(q.New(db)) })
}

// decodeRows reads the JSON documents of rows.
func decodeRows(docs []string) ([]Row, error) {
	out := make([]Row, 0, len(docs))
	for _, d := range docs {
		var r Row
		if err := json.Unmarshal([]byte(d), &r); err != nil {
			return nil, fmt.Errorf("queue: a row does not decode: %w", err)
		}
		if string(r.Ticket) == "null" {
			r.Ticket = nil
		}
		out = append(out, r)
	}
	return out, nil
}

func decodeBulk(doc string) (Bulk, error) {
	var b Bulk
	if err := json.Unmarshal([]byte(doc), &b); err != nil {
		return Bulk{}, fmt.Errorf("queue: a bulk does not decode: %w", err)
	}
	if string(b.Cursor) == "null" {
		b.Cursor = nil
	}
	if string(b.Ticket) == "null" {
		b.Ticket = nil
	}
	return b, nil
}

func first(rows []Row, err error) (Row, bool, error) {
	if err != nil || len(rows) == 0 {
		return Row{}, false, err
	}
	return rows[0], true, nil
}

// firstDoc decodes the first of the documents a statement answered.
func firstDoc(docs []string, err error) (Row, bool, error) {
	if err != nil {
		return Row{}, false, err
	}
	return first(decodeRows(docs))
}

func jsonText(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	if string(b) == "null" {
		return "", nil
	}
	return string(b), nil
}

func us(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMicro()
}

// pgTx is one transaction of the store.
type pgTx struct {
	s  *PGStore
	tx meta.Tx
	qs *q.Queries
}

func (t *pgTx) Meta() meta.Tx { return t.tx }

func (t *pgTx) LockRepository(ctx context.Context, repoID int64) (bool, error) {
	ids, err := t.qs.RemoteDownloadLockRepository(ctx, repoID)
	return len(ids) > 0, err
}

func (t *pgTx) Counts(ctx context.Context, repoID int64) (int, int, error) {
	c, err := t.qs.RemoteDownloadCounts(ctx, repoID)
	return int(c.Waiting), int(c.Running), err
}

func (t *pgTx) Insert(ctx context.Context, r Row) (Row, bool, error) {
	probe, err := jsonText(r.Probe)
	if err != nil {
		return Row{}, false, err
	}
	failure, err := jsonText(r.Failure)
	if err != nil {
		return Row{}, false, err
	}
	if r.ID == "" {
		r.ID = uuid.Must(uuid.NewV7()).String()
	}
	var nb int64
	if r.NotBefore != nil {
		nb = us(*r.NotBefore)
	}
	docs, err := t.qs.RemoteDownloadInsert(ctx, q.RemoteDownloadInsertParams{
		ID: r.ID, RepoID: r.RepoID, Name: r.Name, Version: r.Version, Probe: probe, Origin: string(r.Origin), BulkID: r.BulkID,
		RequestID: r.RequestID, StartedBy: r.StartedBy, Ticket: string(r.Ticket), State: string(remotecuration.StateWaiting),
		MaxAttempts: int32(max(r.MaxAttempts, 1)), NotBeforeUs: nb, Failure: failure, //nolint:gosec // bounded by the configuration
	})
	if err != nil {
		return Row{}, false, err
	}
	if len(docs) == 1 {
		rows, err := decodeRows(docs)
		if err != nil {
			return Row{}, false, err
		}
		return rows[0], true, nil
	}
	active, err := t.qs.RemoteDownloadActive(ctx, q.RemoteDownloadActiveParams{RepoID: r.RepoID, Name: r.Name, Versions: []string{r.Version}})
	if err != nil {
		return Row{}, false, err
	}
	row, found, err := first(decodeRows(active))
	if err == nil && !found {
		err = errors.New("queue: an insert met an active row that ended before it could be read")
	}
	return row, false, err
}

func (t *pgTx) Active(ctx context.Context, repoID int64, name string, versions []string) ([]Row, error) {
	docs, err := t.qs.RemoteDownloadActive(ctx, q.RemoteDownloadActiveParams{RepoID: repoID, Name: name, Versions: versions})
	if err != nil {
		return nil, err
	}
	return decodeRows(docs)
}

func (t *pgTx) Due(ctx context.Context, repoID int64, now time.Time, n int) ([]Row, error) {
	if n <= 0 {
		return nil, nil
	}
	docs, err := t.qs.RemoteDownloadDue(ctx, q.RemoteDownloadDueParams{RepoID: repoID, NowUs: us(now), MaxRows: int32(n)}) //nolint:gosec // small
	if err != nil {
		return nil, err
	}
	return decodeRows(docs)
}

func (t *pgTx) Start(ctx context.Context, r Row, now time.Time) (string, error) {
	if t.s.jobs == nil {
		return "", errors.New("queue: this process has no job queue to start a download on")
	}
	res, err := t.s.jobs.Enqueue(ctx, t.tx, jobs.EnqueueSpec{
		Kind: remotecuration.KindDownload, Args: RunArgs{Row: r.ID}, DedupKey: RunKey(r.ID),
		TargetKind: "repository", TargetKey: strconv.FormatInt(r.RepoID, 10), CreatedBy: r.StartedBy,
	})
	if err != nil {
		return "", err
	}
	n, err := t.qs.RemoteDownloadStart(ctx, q.RemoteDownloadStartParams{ID: r.ID, JobID: res.Job.ID, NowUs: us(now)})
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", fmt.Errorf("queue: row %s is not waiting any more", r.ID)
	}
	return res.Job.ID, nil
}

func (t *pgTx) Get(ctx context.Context, id string, repoID int64) (Row, bool, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Row{}, false, nil //nolint:nilerr // an id of another form names no row
	}
	return firstDoc(t.qs.RemoteDownloadGet(ctx, q.RemoteDownloadGetParams{ID: id, RepoID: repoID}))
}

func (t *pgTx) Settle(ctx context.Context, id, jobID string, s settlement, res Result, now time.Time) (bool, error) {
	failure, err := jsonText(s.Failure)
	if err != nil {
		return false, err
	}
	n, err := t.qs.RemoteDownloadSettle(ctx, q.RemoteDownloadSettleParams{
		ID: id, JobID: jobID, State: string(s.State), Outcome: string(s.Outcome), Failure: failure,
		Files: int32(res.Files), Stored: int32(res.Stored), Present: int32(res.Present), Refused: int32(res.Refused), //nolint:gosec // bounded by MaxProbeFiles
		Bytes: res.Bytes, NotBeforeUs: us(s.NotBefore), NowUs: us(now), Uncounted: s.Limited,
	})
	if err != nil || n == 0 || res.Examination == nil {
		return n > 0, err
	}
	// The version the attempt stored while the rules did not deliver it is held for examination (R-CUR-016), with the request that
	// caused the row.
	row, found, err := t.Get(ctx, id, 0)
	if err != nil || !found {
		return true, err
	}
	ex := res.Examination
	return true, t.qs.RemoteExaminationPut(ctx, q.RemoteExaminationPutParams{
		RepoID: row.RepoID, Name: ex.Name, Version: ex.Version, RequestID: row.RequestID, Files: nonNil(ex.Files),
	})
}

func (t *pgTx) Limit(ctx context.Context, repoID int64) (Limit, bool, error) {
	return firstLimit(t.qs.RemoteDownloadLimitGet(ctx, repoID))
}

func (t *pgTx) PutLimit(ctx context.Context, l Limit) error {
	failure, err := jsonText(l.Failure)
	if err != nil {
		return err
	}
	return t.qs.RemoteDownloadLimitPut(ctx, q.RemoteDownloadLimitPutParams{
		RepoID: l.RepoID, SinceUs: us(l.Since), UntilUs: us(l.Until), Waits: int32(max(l.Waits, 1)), Hinted: l.Hinted, //nolint:gosec // a count of waits
		Failure: failure,
	})
}

func (t *pgTx) ClearLimit(ctx context.Context, repoID int64) error {
	return t.qs.RemoteDownloadLimitClear(ctx, repoID)
}

// firstLimit decodes the limit a statement answered; found is false for none.
func firstLimit(docs []string, err error) (Limit, bool, error) {
	if err != nil || len(docs) == 0 {
		return Limit{}, false, err
	}
	var l Limit
	if err := json.Unmarshal([]byte(docs[0]), &l); err != nil {
		return Limit{}, false, fmt.Errorf("queue: a limit does not decode: %w", err)
	}
	return l, true, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (t *pgTx) InsertBulk(ctx context.Context, b Bulk) (Bulk, error) {
	if b.ID == "" {
		b.ID = uuid.Must(uuid.NewV7()).String()
	}
	if o := b.RowOrigin(); o != remotecuration.OriginBulk && o != remotecuration.OriginSync {
		ticket := ""
		if len(b.Ticket) > 0 && string(b.Ticket) != "null" {
			ticket = string(b.Ticket)
		}
		doc, err := t.qs.RemoteDownloadBulkInsertOf(ctx, q.RemoteDownloadBulkInsertOfParams{
			ID: b.ID, RepoID: b.RepoID, Level: string(b.Level), Subject: b.Subject, MaxVersions: int32(b.MaxVersions), //nolint:gosec // bounded by the configuration
			MaxBytes: b.MaxBytes, IncludePrereleases: b.IncludePrereleases, StartedBy: b.StartedBy, Origin: string(b.Origin),
			RequestID: b.RequestID, Ticket: ticket,
		})
		if err != nil {
			return Bulk{}, err
		}
		return decodeBulk(doc)
	}
	origin := remotecuration.OriginBulk
	if b.Origin == remotecuration.OriginSync {
		origin = remotecuration.OriginSync
	}
	doc, err := t.qs.RemoteDownloadBulkInsert(ctx, q.RemoteDownloadBulkInsertParams{
		ID: b.ID, RepoID: b.RepoID, Level: string(b.Level), Subject: b.Subject, MaxVersions: int32(b.MaxVersions), //nolint:gosec // bounded by the configuration
		MaxBytes: b.MaxBytes, IncludePrereleases: b.IncludePrereleases, StartedBy: b.StartedBy,
		FromVersion: b.FromVersion, ToVersion: b.ToVersion, ToInclusive: b.ToInclusive, Origin: string(origin), SyncID: b.SyncID,
	})
	if err != nil {
		return Bulk{}, err
	}
	return decodeBulk(doc)
}

func (t *pgTx) LockBulk(ctx context.Context, id string) (Bulk, bool, error) {
	docs, err := t.qs.RemoteDownloadBulkLock(ctx, id)
	if err != nil || len(docs) == 0 {
		return Bulk{}, false, err
	}
	b, err := decodeBulk(docs[0])
	return b, err == nil, err
}

func (t *pgTx) UpdateBulk(ctx context.Context, b Bulk) error {
	failure, err := jsonText(b.Failure)
	if err != nil {
		return err
	}
	_, err = t.qs.RemoteDownloadBulkUpdate(ctx, q.RemoteDownloadBulkUpdateParams{
		ID: b.ID, State: string(b.State), VersionsFound: int32(b.VersionsFound), VersionsQueued: int32(b.VersionsQueued), //nolint:gosec // bounded by max_versions
		VersionsRefused: int32(b.VersionsRefused), //nolint:gosec // bounded by max_versions
		Cursor:          string(b.Cursor), Failure: failure,
	})
	return err
}

func (t *pgTx) BulkBytes(ctx context.Context, id string) (int64, error) {
	return t.qs.RemoteDownloadBulkBytes(ctx, id)
}

func (t *pgTx) Retry(ctx context.Context, id string, repoID int64, maxAttempts int, startedBy string, ticket []byte) (Row, bool, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Row{}, false, nil //nolint:nilerr // an id of another form names no row
	}
	return firstDoc(t.qs.RemoteDownloadRetry(ctx, q.RemoteDownloadRetryParams{
		ID: id, RepoID: repoID, MaxAttempts: int32(maxAttempts), StartedBy: startedBy, Ticket: string(ticket), //nolint:gosec // small
	}))
}

func (t *pgTx) RetryMatching(ctx context.Context, repoID int64, class, bulkID string, maxAttempts, n int) ([]string, error) {
	return t.qs.RemoteDownloadRetryMatching(ctx, q.RemoteDownloadRetryMatchingParams{
		RepoID: repoID, Class: class, BulkID: bulkID, MaxAttempts: int32(maxAttempts), MaxRows: int32(n), //nolint:gosec // small
	})
}

func (t *pgTx) CancelWaiting(ctx context.Context, id string, repoID int64) (Row, bool, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Row{}, false, nil //nolint:nilerr // an id of another form names no row
	}
	return firstDoc(t.qs.RemoteDownloadCancelWaiting(ctx, q.RemoteDownloadCancelWaitingParams{ID: id, RepoID: repoID}))
}

func (t *pgTx) CancelBulk(ctx context.Context, b Bulk) error {
	if _, err := t.qs.RemoteDownloadCancelBulkWaiting(ctx, b.ID); err != nil {
		return err
	}
	b.State = BulkCancelled
	return t.UpdateBulk(ctx, b)
}

// List implements [Reader].
func (s *PGStore) List(ctx context.Context, f Filter) (out []Row, err error) {
	p := q.RemoteDownloadListParams{
		RepoID: f.RepoID, State: f.State, Class: f.Class, Origin: f.Origin, BulkID: f.BulkID, Name: f.Name, AfterRank: -1,
		MaxRows: int32(min(max(f.Limit, 1), 1000)), //nolint:gosec // bounded
	}
	if f.After != nil {
		p.AfterRank, p.AfterUs, p.AfterID = int32(rank(string(f.After.State))), f.After.CreatedAt.UnixMicro(), f.After.ID //nolint:gosec // 0 to 2
	}
	err = s.read(ctx, func(qs *q.Queries) error {
		rows, e := qs.RemoteDownloadList(ctx, p)
		if e != nil {
			return e
		}
		docs := make([]string, len(rows))
		for i, r := range rows {
			docs[i] = r.Doc
		}
		out, e = decodeRows(docs)
		return e
	})
	return out, err
}

// StateCounts implements [Reader].
func (s *PGStore) StateCounts(ctx context.Context, repoID int64, bulkID string) (c Counts, err error) {
	err = s.read(ctx, func(qs *q.Queries) error {
		r, e := qs.RemoteDownloadStateCounts(ctx, q.RemoteDownloadStateCountsParams{RepoID: repoID, BulkID: bulkID})
		c = Counts{Waiting: r.Waiting, Running: r.Running, Failed: r.Failed, Done: r.Done}
		return e
	})
	return c, err
}

// Active implements [Reader].
func (s *PGStore) Active(ctx context.Context, repoID int64, name string, versions []string) (out []Row, err error) {
	err = s.read(ctx, func(qs *q.Queries) error {
		docs, e := qs.RemoteDownloadActive(ctx, q.RemoteDownloadActiveParams{RepoID: repoID, Name: name, Versions: versions})
		if e != nil {
			return e
		}
		out, e = decodeRows(docs)
		return e
	})
	return out, err
}

// GetRow implements [Reader].
func (s *PGStore) GetRow(ctx context.Context, id string, repoID int64) (r Row, found bool, err error) {
	if _, perr := uuid.Parse(id); perr != nil {
		return Row{}, false, nil
	}
	err = s.read(ctx, func(qs *q.Queries) error {
		r, found, err = firstDoc(qs.RemoteDownloadGet(ctx, q.RemoteDownloadGetParams{ID: id, RepoID: repoID}))
		return err
	})
	return r, found, err
}

// GetBulk implements [Reader].
func (s *PGStore) GetBulk(ctx context.Context, id string, repoID int64) (b Bulk, counts map[string]int64, found bool, err error) {
	if _, perr := uuid.Parse(id); perr != nil {
		return Bulk{}, nil, false, nil //nolint:nilerr // an id of another form names no bulk
	}
	err = s.read(ctx, func(qs *q.Queries) error {
		docs, e := qs.RemoteDownloadBulkGet(ctx, q.RemoteDownloadBulkGetParams{ID: id, RepoID: repoID})
		if e != nil || len(docs) == 0 {
			return e
		}
		if b, e = decodeBulk(docs[0]); e != nil {
			return e
		}
		rows, e := qs.RemoteDownloadBulkStateCounts(ctx, id)
		if e != nil {
			return e
		}
		counts = map[string]int64{}
		for _, r := range rows {
			counts[r.State] = r.N
		}
		found = true
		return nil
	})
	return b, counts, found, err
}

// ListBulks implements [Reader].
func (s *PGStore) ListBulks(ctx context.Context, f BulkFilter) (out []Bulk, counts map[string]map[string]int64, err error) {
	p := q.RemoteDownloadBulkListParams{RepoID: f.RepoID, MaxRows: int32(min(max(f.Limit, 1), 1000))} //nolint:gosec // bounded
	if f.After != nil {
		p.AfterUs, p.AfterID = f.After.CreatedAt.UnixMicro(), f.After.ID
	}
	err = s.read(ctx, func(qs *q.Queries) error {
		docs, e := qs.RemoteDownloadBulkList(ctx, p)
		if e != nil {
			return e
		}
		out, counts = make([]Bulk, 0, len(docs)), map[string]map[string]int64{}
		ids := make([]string, 0, len(docs))
		for _, doc := range docs {
			b, e := decodeBulk(doc)
			if e != nil {
				return e
			}
			out, ids = append(out, b), append(ids, b.ID)
		}
		if len(ids) == 0 {
			return nil
		}
		rows, e := qs.RemoteDownloadBulksStateCounts(ctx, ids)
		if e != nil {
			return e
		}
		for _, r := range rows {
			if counts[r.BulkID] == nil {
				counts[r.BulkID] = map[string]int64{}
			}
			counts[r.BulkID][r.State] = r.N
		}
		return nil
	})
	return out, counts, err
}

// Waiting implements [Reader].
func (s *PGStore) Waiting(ctx context.Context, repoID int64) (n int, err error) {
	err = s.read(ctx, func(qs *q.Queries) error {
		v, e := qs.RemoteDownloadWaiting(ctx, repoID)
		n = int(v)
		return e
	})
	return n, err
}

// Limit implements [Reader].
func (s *PGStore) Limit(ctx context.Context, repoID int64) (l Limit, found bool, err error) {
	err = s.read(ctx, func(qs *q.Queries) error {
		l, found, err = firstLimit(qs.RemoteDownloadLimitGet(ctx, repoID))
		return err
	})
	return l, found, err
}

// PruneLimits implements [Maintenance].
func (s *PGStore) PruneLimits(ctx context.Context, now time.Time) (n int64, err error) {
	err = s.db.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errNotPostgres
		}
		n, err = q.New(ptx).RemoteDownloadLimitsPrune(ctx, us(now))
		return err
	})
	return n, err
}

// Orphans implements [Maintenance].
func (s *PGStore) Orphans(ctx context.Context, now time.Time) (ids []int64, err error) {
	err = s.db.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errNotPostgres
		}
		ids, err = q.New(ptx).RemoteDownloadOrphans(ctx, us(now))
		return err
	})
	return ids, err
}

// DueRepositories implements [Maintenance].
func (s *PGStore) DueRepositories(ctx context.Context, now time.Time) (ids []int64, err error) {
	err = s.read(ctx, func(qs *q.Queries) (e error) {
		ids, e = qs.RemoteDownloadDueRepositories(ctx, us(now))
		return e
	})
	return ids, err
}

// ExpandingBulks implements [Maintenance].
func (s *PGStore) ExpandingBulks(ctx context.Context) (out []Bulk, err error) {
	err = s.read(ctx, func(qs *q.Queries) error {
		rows, e := qs.RemoteDownloadBulksExpanding(ctx)
		for _, r := range rows {
			out = append(out, Bulk{ID: r.ID, RepoID: r.RepoID})
		}
		return e
	})
	return out, err
}

// SettleBulks implements [Maintenance].
func (s *PGStore) SettleBulks(ctx context.Context) (n int, err error) {
	err = s.db.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errNotPostgres
		}
		ids, e := q.New(ptx).RemoteDownloadBulksSettled(ctx)
		n = len(ids)
		return e
	})
	return n, err
}

// Prune implements [Maintenance].
func (s *PGStore) Prune(ctx context.Context, doneBefore, failedBefore time.Time) (n int64, err error) {
	err = s.db.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errNotPostgres
		}
		qs := q.New(ptx)
		if n, err = qs.RemoteDownloadPrune(ctx, q.RemoteDownloadPruneParams{DoneBeforeUs: us(doneBefore), FailedBeforeUs: us(failedBefore)}); err != nil {
			return err
		}
		_, err = qs.RemoteDownloadPruneBulks(ctx, us(doneBefore))
		return err
	})
	return n, err
}

// Gauge implements [Maintenance].
func (s *PGStore) Gauge(ctx context.Context) (out []GaugeRow, err error) {
	err = s.read(ctx, func(qs *q.Queries) error {
		rows, e := qs.RemoteDownloadGauge(ctx)
		for _, r := range rows {
			out = append(out, GaugeRow{RepoID: r.RepoID, State: r.State, N: r.N})
		}
		return e
	})
	return out, err
}

// ClientFailure implements [Maintenance].
func (s *PGStore) ClientFailure(ctx context.Context, r Row, trim int) error {
	failure, err := jsonText(r.Failure)
	if err != nil || failure == "" {
		return err
	}
	return s.db.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errNotPostgres
		}
		qs := q.New(ptx)
		ids, err := qs.RemoteDownloadClientFailureUpdate(ctx, q.RemoteDownloadClientFailureUpdateParams{
			RepoID: r.RepoID, Name: r.Name, Version: r.Version, Failure: failure,
		})
		if err != nil || len(ids) > 0 {
			return err
		}
		if err := qs.RemoteDownloadClientFailureInsert(ctx, q.RemoteDownloadClientFailureInsertParams{
			ID: uuid.Must(uuid.NewV7()).String(), RepoID: r.RepoID, Name: r.Name, Version: r.Version, Failure: failure,
		}); err != nil {
			return err
		}
		_, err = qs.RemoteDownloadClientFailureTrim(ctx, q.RemoteDownloadClientFailureTrimParams{RepoID: r.RepoID, MaxRows: int32(max(trim, 1))}) //nolint:gosec // bounded by the configuration
		return err
	})
}
