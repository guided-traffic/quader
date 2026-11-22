// Package verify checks that the copies of every stored blob still have their objects in their stores: the logic of
// `quader admin storage verify` (storage.md R-STOR-058, ha-and-helm.md R-HA-024, R-HA-025). It reads the live blobs with their
// copy rows in pages of a fixed size, so its memory does not grow with the table, asks the store of each present copy's
// connection for its object with one existence check, and reports each missing copy, and each blob none of whose copies is
// there (a lost blob) with the items that reference it. It changes nothing: the marks stay the scrub's and the reads'
// (R-STOR-027, R-STOR-055).
package verify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
)

// Defaults of [Options].
const (
	DefaultPageSize    = 1000
	DefaultConcurrency = 16
	DefaultMaxItems    = 20
	headAttempts       = 3
)

// Copy is one copy row of a live blob.
type Copy struct {
	Connection string
	Key        string
	StoredSize int64
	State      string
}

// Row is a live blob with its copy rows.
type Row struct {
	Digest blob.Digest
	Size   int64
	Copies []Copy
}

// Item names one item that references a blob.
type Item struct {
	Project    string `json:"project"`
	Repository string `json:"repository"`
	Path       string `json:"path"`
	Trashed    bool   `json:"trashed,omitempty"`
}

// Source is the database side of a run.
type Source interface {
	// Page returns up to limit live blobs with a digest after the given one (all of them when first is set), in digest order,
	// each with its copy rows, restricted to the blobs created or changed at or after since when since is not the zero time.
	Page(ctx context.Context, after blob.Digest, first bool, since time.Time, limit int) ([]Row, error)
	// Items returns up to limit items that reference the blob, and the number of all of them.
	Items(ctx context.Context, d blob.Digest, limit int) ([]Item, int64, error)
}

// Objects is the store side of a run: the existence check.
type Objects interface {
	Head(ctx context.Context, key string) (blob.ObjectInfo, error)
}

// Conn is one connection of the configuration: its id, as the copy rows name it, and its client.
type Conn struct {
	ID      string
	Objects Objects
}

// Options configure a run.
type Options struct {
	DB Source
	// Connections are the connections of the configuration; a copy on another connection is not checked.
	Connections []Conn
	// Connection limits the checks to the copies on one of them (--connection); empty checks every connection.
	Connection string
	// Since limits the run to the blobs created or changed at or after it; the zero time checks every blob.
	Since time.Time
	// PageSize is the number of blobs read per query; Concurrency the number of simultaneous existence checks; MaxItems the
	// number of items listed per lost blob.
	PageSize, Concurrency, MaxItems int
	Log                             *slog.Logger
	// Sleep waits between the attempts of a check that failed for a reason other than a missing object; tests replace it.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Finding kinds.
const (
	// KindMissing is a copy whose object the store does not have, and a lost blob none of whose checked copies has an object.
	KindMissing = "missing"
	// KindSizeMismatch is a copy whose object has another size than the copy's stored size, and a lost blob one of whose copies
	// had another size.
	KindSizeMismatch = "size_mismatch"
)

// CopyFinding is one copy whose object is missing or of another size: a finding of its connection.
type CopyFinding struct {
	Connection string `json:"connection"`
	SHA256     string `json:"sha256"`
	Key        string `json:"key"`
	Kind       string `json:"kind"`
	StoredSize int64  `json:"storedSize"`
	ObjectSize int64  `json:"objectSize,omitempty"`
}

// Finding is one lost blob, none of whose copies is there with its size, with the items that reference it.
type Finding struct {
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	// Items lists up to Options.MaxItems referencing items; ItemCount is the number of all of them.
	Items     []Item `json:"items"`
	ItemCount int64  `json:"itemCount"`
}

// Unchecked is a copy whose store could not be asked about it (a timeout, a 5xx, throttling) after the attempts, or whose
// connection's checks had ended.
type Unchecked struct {
	Connection string `json:"connection"`
	SHA256     string `json:"sha256"`
	Error      string `json:"error"`
}

// ConnectionReport counts one connection's checks.
type ConnectionReport struct {
	Connection   string `json:"connection"`
	Checked      int64  `json:"checked"`
	Missing      int64  `json:"missing"`
	SizeMismatch int64  `json:"sizeMismatch"`
	Unchecked    int64  `json:"unchecked"`
	// Error is why the connection's checks ended early: a permission or configuration error of its store.
	Error string `json:"error,omitempty"`
}

// Report is the result of a run.
type Report struct {
	Since time.Time `json:"since,omitzero"`
	// Checked is the number of blobs read.
	Checked int64 `json:"checked"`
	// Lost is the number of lost blobs (the number R-HA-025 step 4 prints); Missing counts those none of whose copies has an
	// object, SizeMismatch those one of whose copies had another size.
	Lost         int64              `json:"lost"`
	Missing      int64              `json:"missing"`
	SizeMismatch int64              `json:"sizeMismatch"`
	Findings     []Finding          `json:"findings"`
	Copies       []CopyFinding      `json:"copies"`
	Unchecked    []Unchecked        `json:"unchecked"`
	Connections  []ConnectionReport `json:"connections"`
	// ItemsAffected is the number of items that reference a lost blob.
	ItemsAffected int64 `json:"itemsAffected"`
}

// OK reports whether no copy is missing or of another size and every store answered.
func (r *Report) OK() bool {
	return len(r.Copies) == 0 && len(r.Findings) == 0 && len(r.Unchecked) == 0
}

// ConnectionFindings is the number of copies on the connection that are missing or of another size.
func (r *Report) ConnectionFindings(id string) int64 {
	for _, c := range r.Connections {
		if c.Connection == id {
			return c.Missing + c.SizeMismatch
		}
	}
	return 0
}

// ErrUnknownConnection is the error of a run asked for a connection the configuration does not have.
var ErrUnknownConnection = errors.New("verify: the connection is not in the configuration")

// Run walks the live blobs and checks the object of each present copy on its connection. It returns the report so far with the
// error when the run ends early (the database). A permission or configuration error of a store (for example AccessDenied, which
// a store answers for a missing object when the credential may not list the bucket) ends the checks of that connection, because
// every further check would fail the same way; the run goes on with the others and returns that error with the whole report.
func Run(ctx context.Context, o Options) (*Report, error) {
	if o.DB == nil || len(o.Connections) == 0 {
		return nil, errors.New("verify: a database source and a connection are required")
	}
	o = withDefaults(o)
	rep := &Report{Since: o.Since, Findings: []Finding{}, Copies: []CopyFinding{}, Unchecked: []Unchecked{}}
	st := newState(o, rep)
	if o.Connection != "" && st.byID[o.Connection] == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownConnection, o.Connection)
	}
	var after blob.Digest
	first := true
	for {
		rows, err := o.DB.Page(ctx, after, first, o.Since, o.PageSize)
		if err != nil {
			return rep, fmt.Errorf("reading the blob table: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		first = false
		after = rows[len(rows)-1].Digest
		results := st.checkPage(ctx, rows)
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		for i, row := range rows {
			if err := st.record(ctx, row, results[i]); err != nil {
				return rep, err
			}
		}
		o.Log.Debug("storage verify progress", "checked", rep.Checked, "lost", rep.Lost)
	}
	rep.Connections = st.connections()
	return rep, st.configError()
}

func withDefaults(o Options) Options {
	if o.PageSize <= 0 {
		o.PageSize = DefaultPageSize
	}
	if o.Concurrency <= 0 {
		o.Concurrency = DefaultConcurrency
	}
	if o.MaxItems <= 0 {
		o.MaxItems = DefaultMaxItems
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Sleep == nil {
		o.Sleep = sleep
	}
	return o
}

// connState is one connection's counters and whether its checks ended.
type connState struct {
	conn  Conn
	rep   ConnectionReport
	ended error
}

// state is a run's connections and report.
type state struct {
	o    Options
	rep  *Report
	list []*connState
	byID map[string]*connState
	mu   sync.Mutex
}

func newState(o Options, rep *Report) *state {
	st := &state{o: o, rep: rep, byID: map[string]*connState{}}
	for _, c := range o.Connections {
		cs := &connState{conn: c, rep: ConnectionReport{Connection: c.ID}}
		st.list = append(st.list, cs)
		st.byID[c.ID] = cs
	}
	return st
}

// checked reports the connection state of a copy that this run checks, nil for a copy it leaves (another connection than the
// one named, or one the configuration does not have).
func (st *state) checked(c Copy) *connState {
	if c.State != blob.CopyPresent || (st.o.Connection != "" && c.Connection != st.o.Connection) {
		return nil
	}
	return st.byID[c.Connection]
}

// The results of one copy's check.
type result struct {
	kind       string // KindMissing, KindSizeMismatch, or "" when there
	objectSize int64
	err        error // the copy could not be checked
	skipped    bool  // not a copy this run checks
}

// checkPage asks the stores about every present copy of a page with at most o.Concurrency checks at a time.
func (st *state) checkPage(ctx context.Context, rows []Row) [][]result {
	out := make([][]result, len(rows))
	var wg sync.WaitGroup
	sem := make(chan struct{}, st.o.Concurrency)
	for i, row := range rows {
		out[i] = make([]result, len(row.Copies))
		for j, c := range row.Copies {
			cs := st.checked(c)
			if cs == nil {
				out[i][j] = result{skipped: true}
				continue
			}
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				wg.Wait()
				return out
			}
			wg.Add(1)
			go func(i, j int, cs *connState, c Copy) {
				defer wg.Done()
				defer func() { <-sem }()
				out[i][j] = st.check(ctx, cs, c)
			}(i, j, cs, c)
		}
	}
	wg.Wait()
	return out
}

// check is one existence check with its attempts. A missing object and a size mismatch are results; a transient error that
// outlasts the attempts is a result too (the copy is not checked); a configuration error ends the connection's checks.
func (st *state) check(ctx context.Context, cs *connState, c Copy) result {
	var last error
	for attempt := range headAttempts {
		if err := st.endedErr(cs); err != nil {
			return result{err: err}
		}
		if attempt > 0 {
			if err := st.o.Sleep(ctx, time.Duration(attempt)*time.Second); err != nil {
				return result{err: err}
			}
		}
		info, err := cs.conn.Objects.Head(ctx, c.Key)
		switch {
		case errors.Is(err, blob.ErrNotFound):
			return result{kind: KindMissing}
		case err == nil && info.Size >= 0 && info.Size != c.StoredSize:
			return result{kind: KindSizeMismatch, objectSize: info.Size}
		case err == nil:
			return result{}
		case blob.IsConfig(err):
			st.mu.Lock()
			if cs.ended == nil {
				cs.ended = fmt.Errorf("the object store of connection %s refuses the check of %s: %w", cs.conn.ID, c.Key, err)
			}
			st.mu.Unlock()
			return result{err: err}
		case ctx.Err() != nil:
			return result{err: ctx.Err()}
		}
		last = err
	}
	return result{err: last}
}

func (st *state) endedErr(cs *connState) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if cs.ended != nil {
		return errors.New("not checked: the checks of this connection ended")
	}
	return nil
}

// record counts the results of one blob's copies and, for a lost blob, lists its items.
func (st *state) record(ctx context.Context, row Row, results []result) error {
	rep := st.rep
	rep.Checked++
	there, open, sized := false, false, false
	for j, c := range row.Copies {
		r := results[j]
		if r.skipped {
			// a present copy this run does not check may still be there: the blob is not lost on its account
			open = open || c.State == blob.CopyPresent
			continue
		}
		cs := st.byID[c.Connection]
		cs.rep.Checked++
		switch {
		case r.err != nil:
			open = true
			cs.rep.Unchecked++
			rep.Unchecked = append(rep.Unchecked, Unchecked{Connection: c.Connection, SHA256: row.Digest.Hex(), Error: r.err.Error()})
		case r.kind == "":
			there = true
		default:
			if r.kind == KindMissing {
				cs.rep.Missing++
			} else {
				cs.rep.SizeMismatch++
				sized = true
			}
			rep.Copies = append(rep.Copies, CopyFinding{
				Connection: c.Connection, SHA256: row.Digest.Hex(), Key: c.Key, Kind: r.kind, StoredSize: c.StoredSize, ObjectSize: r.objectSize,
			})
		}
	}
	if there || open {
		return nil
	}
	items, n, err := st.o.DB.Items(ctx, row.Digest, st.o.MaxItems)
	if err != nil {
		return fmt.Errorf("listing the items of blob %s: %w", row.Digest.Hex(), err)
	}
	if items == nil {
		items = []Item{}
	}
	f := Finding{Kind: KindMissing, SHA256: row.Digest.Hex(), Size: row.Size, Items: items, ItemCount: n}
	if sized {
		f.Kind = KindSizeMismatch
		rep.SizeMismatch++
	} else {
		rep.Missing++
	}
	rep.Lost++
	rep.ItemsAffected += n
	rep.Findings = append(rep.Findings, f)
	return nil
}

func (st *state) connections() []ConnectionReport {
	out := make([]ConnectionReport, 0, len(st.list))
	for _, cs := range st.list {
		if st.o.Connection != "" && cs.conn.ID != st.o.Connection {
			continue
		}
		r := cs.rep
		if cs.ended != nil {
			r.Error = cs.ended.Error()
		}
		out = append(out, r)
	}
	return out
}

// configError is the first connection's error that ended its checks.
func (st *state) configError() error {
	for _, cs := range st.list {
		if cs.ended != nil {
			return cs.ended
		}
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
