package blob_test

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/blob"

	"github.com/guided-traffic/quader/internal/meta"
)

// fakeDB simulates the blobs, blob_gc_queue, blob_copies and blob_copy_queue tables for the operations the commit protocol
// and the download path use (the store's seam to the tables, installed with blob.WithRows). Transactions are serialized
// and apply at commit, which is enough to drive the protocol's branches; the integration tier runs the same code against
// PostgreSQL.
type fakeDB struct {
	mu    sync.Mutex
	rows  map[blob.Digest]*fakeRow
	queue map[blob.Digest]bool
	// copies are the copy rows by blob and connection; copyQueue the queued gaps by connection and blob.
	copies    map[blob.Digest]map[string]*blob.Copy
	copyQueue map[string]map[blob.Digest]bool
	// retired are the connections whose copies count for nothing.
	retired map[string]bool
	// beforeCommit runs inside a transaction attempt after its function, for
	// tests that change the world between statements.
	txCount int
	// failTx makes InTx return an error for the n-th transaction (1-based).
	failTx map[int]error
	// gone is called when a poll looks for a row, to let a test delete it.
	onExists func(n int)
	exists   int
	// conflict makes every insert report a concurrent winner whose row is invisible.
	conflict bool
	// now is the database clock of checked_at and verified_at; nil is time.Now.
	now func() time.Time
	// stamps counts the statements that stamped a copy verified.
	stamps int
	// copyReads counts the statements that read a blob's copies (Q-STOR-275).
	copyReads int
	// marks are the moves of a copy to missing or corrupt with what the mark named: "<connection> <state> <source> <reason>"
	// (R-STOR-108).
	marks []string
}

type fakeRow struct {
	state, status string
	size          int64
	checked       time.Time // checked_at; zero is NULL
}

func (f *fakeDB) clock() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
}

func newFakeDB() *fakeDB {
	return &fakeDB{
		rows: map[blob.Digest]*fakeRow{}, queue: map[blob.Digest]bool{}, failTx: map[int]error{},
		copies: map[blob.Digest]map[string]*blob.Copy{}, copyQueue: map[string]map[blob.Digest]bool{}, retired: map[string]bool{},
	}
}

func (f *fakeDB) InTx(ctx context.Context, fn func(meta.Tx) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.txCount++
	if err := f.failTx[f.txCount]; err != nil {
		return err
	}
	tx := &fakeTx{db: f, inserts: map[blob.Digest]*fakeRow{}, statuses: map[blob.Digest]string{}, queue: map[blob.Digest]bool{}}
	snapshot := f.snapshotCopies()
	if err := fn(tx); err != nil {
		f.copies, f.copyQueue = snapshot.copies, snapshot.queue
		tx.RunRollback()
		return err
	}
	if err := tx.RunBefore(ctx, tx); err != nil {
		f.copies, f.copyQueue = snapshot.copies, snapshot.queue
		tx.RunRollback()
		return err
	}
	for d, r := range tx.inserts {
		f.rows[d] = r
	}
	for d, s := range tx.statuses {
		if r := f.rows[d]; r != nil {
			r.status = s
		}
	}
	for d := range tx.queue {
		f.queue[d] = true
	}
	tx.RunAfter()
	return nil
}

// copyState is the copy tables as they were before a transaction, to roll its writes back.
type copyState struct {
	copies map[blob.Digest]map[string]*blob.Copy
	queue  map[string]map[blob.Digest]bool
}

func (f *fakeDB) snapshotCopies() copyState {
	s := copyState{copies: map[blob.Digest]map[string]*blob.Copy{}, queue: map[string]map[blob.Digest]bool{}}
	for d, m := range f.copies {
		s.copies[d] = map[string]*blob.Copy{}
		for c, cp := range m {
			v := *cp
			s.copies[d][c] = &v
		}
	}
	for c, m := range f.copyQueue {
		s.queue[c] = map[blob.Digest]bool{}
		for d := range m {
			s.queue[c][d] = true
		}
	}
	return s
}

// fakeTx is the transaction of the simulation: it holds what the attempt wrote to the blob rows until it commits; its
// writes to the copy tables apply at once and are undone by a rollback.
type fakeTx struct {
	meta.TxState
	db       *fakeDB
	inserts  map[blob.Digest]*fakeRow
	statuses map[blob.Digest]string
	queue    map[blob.Digest]bool
}

func (f *fakeDB) LockShare(_ context.Context, tx meta.Tx, d blob.Digest) (state, status string, found bool, err error) {
	t := tx.(*fakeTx)
	if r, ok := t.inserts[d]; ok {
		return r.state, r.status, true, nil
	}
	if r, ok := f.rows[d]; ok {
		st := r.status
		if s, ok := t.statuses[d]; ok {
			st = s
		}
		return r.state, st, true, nil
	}
	return "", "", false, nil
}

func (f *fakeDB) InsertLive(_ context.Context, tx meta.Tx, sp *blob.Spooled) (bool, error) {
	t := tx.(*fakeTx)
	d := sp.SHA256
	if f.conflict {
		return false, nil
	}
	if _, ok := f.rows[d]; ok {
		return false, nil
	}
	if _, ok := t.inserts[d]; ok {
		return false, nil
	}
	t.inserts[d] = &fakeRow{state: "live", status: "ok", size: sp.Size}
	return true, nil
}

func (f *fakeDB) Exists(_ context.Context, _ meta.Tx, d blob.Digest) (bool, error) {
	f.exists++
	if f.onExists != nil {
		f.onExists(f.exists)
	}
	_, ok := f.rows[d]
	return ok, nil
}

func (f *fakeDB) Enqueue(_ context.Context, tx meta.Tx, d blob.Digest) error {
	tx.(*fakeTx).queue[d] = true
	return nil
}

func (f *fakeDB) Row(_ context.Context, _ meta.Tx, d blob.Digest) (blob.Row, bool, error) {
	r, ok := f.rows[d]
	if !ok {
		return blob.Row{}, false, nil
	}
	return blob.Row{Digests: blob.Digests{SHA256: d, Size: r.size}, State: r.state, ContentStatus: r.status}, true, nil
}

func (f *fakeDB) ClaimRecheck(_ context.Context, _ meta.Tx, d blob.Digest, interval time.Duration) (bool, error) {
	r := f.rows[d]
	if r == nil || r.state != "live" || r.status != "missing" || (!r.checked.IsZero() && f.clock().Sub(r.checked) < interval) {
		return false, nil
	}
	r.checked = f.clock()
	return true, nil
}

func (f *fakeDB) Copies(_ context.Context, _ meta.Tx, d blob.Digest) ([]blob.Copy, error) {
	f.copyReads++
	var out []blob.Copy
	for _, cp := range f.copies[d] {
		c := *cp
		c.Retired = f.retired[c.Connection]
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Connection < out[j].Connection })
	return out, nil
}

func (f *fakeDB) LockForMark(_ context.Context, tx meta.Tx, d blob.Digest) (string, bool, error) {
	if r, ok := tx.(*fakeTx).inserts[d]; ok {
		return r.state, true, nil
	}
	if r, ok := f.rows[d]; ok {
		return r.state, true, nil
	}
	return "", false, nil
}

// putCopy writes a copy row directly, as a test sets up the world.
func (f *fakeDB) putCopy(d blob.Digest, cp blob.Copy) {
	if f.copies[d] == nil {
		f.copies[d] = map[string]*blob.Copy{}
	}
	f.copies[d][cp.Connection] = &cp
}

// copyOf is the copy row of a blob on a connection, false when there is none.
func (f *fakeDB) copyOf(d blob.Digest, conn string) (blob.Copy, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp, ok := f.copies[d][conn]
	if !ok {
		return blob.Copy{}, false
	}
	return *cp, true
}

// queued reports whether a gap of the connection is queued for the blob.
func (f *fakeDB) queued(conn string, d blob.Digest) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copyQueue[conn][d]
}

// status is the blob's content state.
func (f *fakeDB) status(d blob.Digest) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r := f.rows[d]; r != nil {
		return r.status
	}
	return ""
}

func (f *fakeDB) PutCopy(_ context.Context, _ meta.Tx, d blob.Digest, w blob.CopyWrite) error {
	if old, ok := f.copies[d][w.Connection]; ok && old.State == blob.CopyDeleting {
		return nil
	}
	cp := blob.Copy{Connection: w.Connection, ObjectKey: w.Key, StoredSize: w.StoredSize, StorageKeyID: w.StorageKeyID, State: blob.CopyPresent, CheckedAt: f.clock()}
	if w.Verified {
		cp.VerifiedAt = f.clock()
	}
	f.putCopy(d, cp)
	return nil
}

func (f *fakeDB) SetCopyState(_ context.Context, _ meta.Tx, d blob.Digest, conn, from, to string, dmg blob.Damage) (bool, error) {
	cp, ok := f.copies[d][conn]
	if !ok || cp.State != from {
		return false, nil
	}
	cp.State, cp.CheckedAt = to, f.clock()
	if to == blob.CopyMissing || to == blob.CopyCorrupt {
		f.marks = append(f.marks, conn+" "+to+" "+dmg.Source+" "+dmg.Reason)
	}
	return true, nil
}

func (f *fakeDB) StampVerified(_ context.Context, _ meta.Tx, d blob.Digest, conn string, interval time.Duration) error {
	cp, ok := f.copies[d][conn]
	if !ok || cp.State != blob.CopyPresent || (!cp.VerifiedAt.IsZero() && f.clock().Sub(cp.VerifiedAt) < interval) {
		return nil
	}
	cp.VerifiedAt, cp.CheckedAt = f.clock(), f.clock()
	f.stamps++
	return nil
}

func (f *fakeDB) QueueCopy(_ context.Context, _ meta.Tx, conn string, d blob.Digest) error {
	if f.copyQueue[conn] == nil {
		f.copyQueue[conn] = map[blob.Digest]bool{}
	}
	f.copyQueue[conn][d] = true
	return nil
}

func (f *fakeDB) SetContentStatus(_ context.Context, tx meta.Tx, d blob.Digest, status string) (bool, error) {
	t := tx.(*fakeTx)
	r := f.rows[d]
	if ins, ok := t.inserts[d]; ok {
		r = ins
	}
	if r == nil || r.state != "live" {
		return false, nil
	}
	cur := r.status
	if s, ok := t.statuses[d]; ok {
		cur = s
	}
	if cur == status {
		return false, nil
	}
	t.statuses[d] = status
	if status != blob.StatusOK {
		r.checked = f.clock()
	}
	return true, nil
}
