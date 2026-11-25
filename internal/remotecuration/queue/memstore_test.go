package queue

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/remotecuration"
)

// memStore is the queue's store in memory, for the unit tests: the rows, the bulks and the jobs a start enqueued, with the order of
// RemoteDownloadDue (originRank, then creation).
type memStore struct {
	mu      sync.Mutex
	rows    []*Row
	bulks   map[string]*Bulk
	jobs    int
	seq     int
	now     func() time.Time
	written []Row // the client failures written
	// examined are the examination rows the ends of attempts wrote, with the request of their row.
	examined []examinedRow
	// limits are the repositories' upstream rate limits (remote_download_limits).
	limits map[int64]Limit
	// syncs are the syncs (remote_download_syncs), usage the repositories' folded bytes.
	syncs map[string]*Sync
	usage map[int64]int64
}

type examinedRow struct {
	Examination
	RequestID string
}

func newMemStore(now func() time.Time) *memStore {
	return &memStore{bulks: map[string]*Bulk{}, now: now, limits: map[int64]Limit{}, syncs: map[string]*Sync{}, usage: map[int64]int64{}}
}

var (
	_ Store       = (*memStore)(nil)
	_ Maintenance = (*memStore)(nil)
)

func (m *memStore) InTx(_ context.Context, fn func(Tx) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn(memTx{m})
}

func (m *memStore) Within(ctx context.Context, _ meta.Tx, fn func(Tx) error) error {
	return m.InTx(ctx, fn)
}

type memTx struct{ m *memStore }

func (t memTx) Meta() meta.Tx { return nil }

func (t memTx) LockRepository(context.Context, int64) (bool, error) { return true, nil }

func (t memTx) Counts(_ context.Context, repoID int64) (w, r int, err error) {
	for _, x := range t.m.rows {
		if x.RepoID != repoID {
			continue
		}
		switch x.State {
		case remotecuration.StateWaiting:
			w++
		case remotecuration.StateRunning:
			r++
		}
	}
	return w, r, nil
}

func (t memTx) Insert(_ context.Context, r Row) (Row, bool, error) {
	for _, x := range t.m.rows {
		if x.RepoID == r.RepoID && x.Name == r.Name && x.Version == r.Version && x.State.Active() {
			return *x, false, nil
		}
	}
	t.m.seq++
	r.ID = fmt.Sprintf("row-%03d", t.m.seq)
	r.State = remotecuration.StateWaiting
	r.CreatedAt = t.m.now().Add(time.Duration(t.m.seq) * time.Microsecond)
	c := r
	t.m.rows = append(t.m.rows, &c)
	return c, true, nil
}

func (t memTx) Active(_ context.Context, repoID int64, name string, versions []string) ([]Row, error) {
	var out []Row
	for _, x := range t.m.rows {
		if x.RepoID == repoID && x.Name == name && slices.Contains(versions, x.Version) && x.State.Active() {
			out = append(out, *x)
		}
	}
	return out, nil
}

func (t memTx) Due(_ context.Context, repoID int64, now time.Time, n int) ([]Row, error) {
	var due []Row
	for _, x := range t.m.rows {
		if x.RepoID == repoID && x.State == remotecuration.StateWaiting && (x.NotBefore == nil || !x.NotBefore.After(now)) {
			due = append(due, *x)
		}
	}
	sort.SliceStable(due, func(i, j int) bool {
		if a, b := originRank(due[i].Origin), originRank(due[j].Origin); a != b {
			return a < b
		}
		return due[i].CreatedAt.Before(due[j].CreatedAt)
	})
	if len(due) > n {
		due = due[:n]
	}
	return due, nil
}

func (t memTx) find(id string) *Row {
	for _, x := range t.m.rows {
		if x.ID == id {
			return x
		}
	}
	return nil
}

func (t memTx) Start(_ context.Context, r Row, now time.Time) (string, error) {
	x := t.find(r.ID)
	t.m.jobs++
	x.State, x.Attempts, x.JobID, x.NotBefore = remotecuration.StateRunning, x.Attempts+1, fmt.Sprintf("job-%d", t.m.jobs), nil
	at := now
	x.StartedAt = &at
	return x.JobID, nil
}

func (t memTx) Get(_ context.Context, id string, _ int64) (Row, bool, error) {
	if x := t.find(id); x != nil {
		return *x, true, nil
	}
	return Row{}, false, nil
}

func (t memTx) Settle(_ context.Context, id, jobID string, s settlement, res Result, _ time.Time) (bool, error) {
	x := t.find(id)
	if x == nil || x.State != remotecuration.StateRunning || x.JobID != jobID {
		return false, nil
	}
	x.State, x.Outcome, x.Failure, x.JobID = s.State, s.Outcome, s.Failure, ""
	if s.Limited {
		x.Attempts = max(x.Attempts-1, 0)
	}
	x.Files, x.Stored, x.Present, x.Refused, x.Bytes = res.Files, x.Stored+res.Stored, res.Present, res.Refused, x.Bytes+res.Bytes
	x.NotBefore = nil
	if !s.NotBefore.IsZero() {
		nb := s.NotBefore
		x.NotBefore = &nb
	}
	if res.Examination != nil {
		t.m.examined = append(t.m.examined, examinedRow{Examination: *res.Examination, RequestID: x.RequestID})
	}
	return true, nil
}

func (t memTx) InsertBulk(_ context.Context, b Bulk) (Bulk, error) {
	t.m.seq++
	b.ID = fmt.Sprintf("bulk-%03d", t.m.seq)
	c := b
	t.m.bulks[b.ID] = &c
	return c, nil
}

func (t memTx) LockBulk(_ context.Context, id string) (Bulk, bool, error) {
	b, ok := t.m.bulks[id]
	if !ok || b.State != BulkExpanding {
		return Bulk{}, false, nil
	}
	return *b, true, nil
}

func (t memTx) UpdateBulk(_ context.Context, b Bulk) error {
	c := b
	t.m.bulks[b.ID] = &c
	return nil
}

func (t memTx) BulkBytes(_ context.Context, id string) (n int64, err error) {
	for _, x := range t.m.rows {
		if x.BulkID == id {
			n += x.Bytes
		}
	}
	return n, nil
}

func (t memTx) Retry(_ context.Context, id string, _ int64, maxAttempts int, startedBy string, ticket []byte) (Row, bool, error) {
	x := t.find(id)
	if x == nil || (x.State != remotecuration.StateFailed && x.State != remotecuration.StateCancelled) {
		return Row{}, false, nil
	}
	x.State, x.Attempts, x.MaxAttempts, x.NotBefore, x.Outcome = remotecuration.StateWaiting, 0, maxAttempts, nil, ""
	if x.Origin == remotecuration.OriginClient {
		x.Origin, x.StartedBy, x.Ticket = remotecuration.OriginPerson, startedBy, ticket
	}
	return *x, true, nil
}

func (t memTx) RetryMatching(context.Context, int64, string, string, int, int) ([]string, error) {
	return nil, nil
}

func (t memTx) CancelWaiting(_ context.Context, id string, _ int64) (Row, bool, error) {
	x := t.find(id)
	if x == nil || x.State != remotecuration.StateWaiting {
		return Row{}, false, nil
	}
	x.State = remotecuration.StateCancelled
	return *x, true, nil
}

func (t memTx) CancelBulk(_ context.Context, b Bulk) error {
	for _, x := range t.m.rows {
		if x.BulkID == b.ID && x.State == remotecuration.StateWaiting {
			x.State = remotecuration.StateCancelled
		}
	}
	t.m.bulks[b.ID].State = BulkCancelled
	return nil
}

func (t memTx) Limit(_ context.Context, repoID int64) (Limit, bool, error) {
	l, ok := t.m.limits[repoID]
	return l, ok, nil
}

func (t memTx) PutLimit(_ context.Context, l Limit) error {
	t.m.limits[l.RepoID] = l
	return nil
}

func (t memTx) ClearLimit(_ context.Context, repoID int64) error {
	delete(t.m.limits, repoID)
	return nil
}

func (m *memStore) Limit(ctx context.Context, repoID int64) (Limit, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return memTx{m}.Limit(ctx, repoID)
}

func (m *memStore) PruneLimits(_ context.Context, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for id, l := range m.limits {
		if l.Holds(now) {
			continue
		}
		if w, r, _ := (memTx{m}).Counts(context.Background(), id); w+r == 0 {
			delete(m.limits, id)
			n++
		}
	}
	return n, nil
}

func (m *memStore) List(context.Context, Filter) ([]Row, error) { return nil, nil }

func (m *memStore) StateCounts(context.Context, int64, string) (Counts, error) { return Counts{}, nil }

func (m *memStore) Active(_ context.Context, repoID int64, name string, versions []string) ([]Row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Row
	for _, x := range m.rows {
		if x.RepoID == repoID && x.Name == name && slices.Contains(versions, x.Version) && x.State.Active() {
			out = append(out, *x)
		}
	}
	return out, nil
}

func (m *memStore) GetRow(_ context.Context, id string, _ int64) (Row, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return memTx{m}.Get(context.Background(), id, 0)
}

func (m *memStore) GetBulk(_ context.Context, id string, _ int64) (Bulk, map[string]int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.bulks[id]
	if !ok {
		return Bulk{}, nil, false, nil
	}
	return *b, nil, true, nil
}

func (m *memStore) ListBulks(context.Context, BulkFilter) ([]Bulk, map[string]map[string]int64, error) {
	return nil, nil, nil
}

func (m *memStore) Waiting(_ context.Context, repoID int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, _, err := memTx{m}.Counts(context.Background(), repoID)
	return w, err
}

func (m *memStore) Orphans(context.Context, time.Time) ([]int64, error) { return nil, nil }

func (m *memStore) DueRepositories(_ context.Context, now time.Time) ([]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[int64]bool{}
	var out []int64
	for _, x := range m.rows {
		if x.State == remotecuration.StateWaiting && (x.NotBefore == nil || !x.NotBefore.After(now)) && !seen[x.RepoID] {
			seen[x.RepoID] = true
			out = append(out, x.RepoID)
		}
	}
	return out, nil
}

func (m *memStore) ExpandingBulks(context.Context) ([]Bulk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Bulk
	for _, b := range m.bulks {
		if b.State == BulkExpanding {
			out = append(out, Bulk{ID: b.ID, RepoID: b.RepoID})
		}
	}
	return out, nil
}

func (m *memStore) SettleBulks(context.Context) (int, error)                   { return 0, nil }
func (m *memStore) Prune(context.Context, time.Time, time.Time) (int64, error) { return 0, nil }
func (m *memStore) Gauge(context.Context) ([]GaugeRow, error)                  { return nil, nil }
func (m *memStore) ClientFailure(_ context.Context, r Row, _ string, _ int) error {
	m.written = append(m.written, r)
	return nil
}

// byID finds a row.
func (m *memStore) byID(id string) Row {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *memTx{m}.find(id)
}
