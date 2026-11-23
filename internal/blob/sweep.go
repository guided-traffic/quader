package blob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The orphan sweep (R-STOR-026): the reverse check of collection. It walks each connection's <prefix>/sha256/<h0h1>/ in list
// order, adopts the object of a live blob that no copy row names, marks a copy whose object has another size, and claims an object
// that no blob row stands for through a blob row in state deleting before it deletes it with collection's steps (4) to (6) and their
// hand-over rules (Q-STOR-238). Then it sweeps the connection's staging area: the multipart uploads older than
// storage.orphans.multipart_max_age and the staging objects of the sessions that are over, unknown or pinned to another connection
// (R-STOR-054, Q-STOR-263). It never deletes on the strength of a listing alone where an age is asked for, and confirms a size it
// would act on with a HeadObject (Q-STOR-262).

// SweepTask is the name of the orphan sweep's task and job kind (R-STOR-026).
const SweepTask = "orphan-sweep"

// sweepStagingDone is the prefix of a cursor whose connection is done, its staging area included.
const sweepStagingDone = "staging"

// foreignLogged bounds the foreign keys a sweep names in the log per connection; the result counts all of them.
const foreignLogged = 20

// SweepCursor is where a sweep continues: the last top-level prefix of a connection whose listing ended (two hexadecimal digits), or
// "staging" when that connection is done, its staging area included. The zero cursor starts at prefix 00 of the first connection. It
// is the job's checkpoint (R-STOR-026).
type SweepCursor struct {
	Connection string `json:"connection,omitempty"`
	Prefix     string `json:"prefix,omitempty"`
}

// SweepConnectionResult is what a sweep did on one connection (R-STOR-026). Under the deletion pause and in a dry run the deletions
// are counted as found, not done.
type SweepConnectionResult struct {
	Connection    string `json:"connection"`
	PrefixesDone  int    `json:"prefixesDone"`
	PrefixesTotal int    `json:"prefixesTotal"`
	ObjectsSeen   int    `json:"objectsSeen"`
	// OrphansFound is the objects below sha256/ without a blob row that are older than the run's minAge.
	OrphansFound   int   `json:"orphansFound"`
	OrphansDeleted int   `json:"orphansDeleted"`
	BytesReclaimed int64 `json:"bytesReclaimed"`
	Adopted        int   `json:"adopted"`
	CopiesMarked   int   `json:"copiesMarked"`
	// MultipartAborted is the multipart uploads below the connection's prefix older than storage.orphans.multipart_max_age that were
	// aborted (a dry run: that would be).
	MultipartAborted int `json:"multipartAborted"`
	// StagingDeleted is the staging objects of sessions that are over, unknown or pinned to another connection, and the leftover probe
	// objects, older than storage.uploads.session_max_age, that were deleted (Q-STOR-263).
	StagingDeleted int `json:"stagingDeleted"`
	// ForeignKeys is the keys below the prefix outside the families of R-STOR-059 and below sha256/ outside the layout: reported in
	// the log, never deleted.
	ForeignKeys int `json:"foreignKeys"`
}

// SweepSkip is a connection a sweep did not walk to its end, and why: circuit_open (its circuit was open on the worker),
// unavailable (the store failed during the walk), foreign_marker (its marker names another connection or installation, Q-STOR-262),
// not_configured (the run named a connection the worker does not configure).
type SweepSkip struct {
	Connection string `json:"connection"`
	Reason     string `json:"reason"`
}

// SweepResult is the result of an orphan-sweep run (R-STOR-026).
type SweepResult struct {
	DryRun bool `json:"dryRun"`
	// Paused says that the deletion pause was set during the run: it adopted and marked, deleted nothing from that prefix on, and its
	// checkpoint stays at the prefix before, where a run with resume: true continues (Q-STOR-261).
	Paused bool `json:"paused,omitempty"`
	// ResumedFrom is the cursor the run started after, absent when it started at the beginning.
	ResumedFrom *SweepCursor            `json:"resumedFrom,omitempty"`
	Connections []SweepConnectionResult `json:"connections"`
	Skipped     []SweepSkip             `json:"skipped"`
	Errors      int                     `json:"errors"`
}

// SweepOptions are the parameters of one sweep.
type SweepOptions struct {
	// DryRun reports what the sweep would adopt, mark, delete and abort and changes nothing.
	DryRun bool
	// MinAge is the age below which an object without a blob row is left alone; zero is storage.orphans.min_age. It must exceed
	// twice storage.upload.commit_deadline.
	MinAge time.Duration
	// Connection limits the sweep to one connection; empty walks every connection of the worker.
	Connection string
	// Cursor is where the sweep continues.
	Cursor SweepCursor
	// Checkpoint receives the cursor after every prefix and after each connection's staging area.
	Checkpoint func(SweepCursor)
	// Draining is closed when the replica begins its shutdown; the settle wait at the end ends at it with ErrHandedOver.
	Draining <-chan struct{}
}

// SweepJobArgs are the arguments of an orphan-sweep job, the body of POST /api/v1/system/tasks/orphan-sweep/run (R-STOR-026);
// every member is optional.
type SweepJobArgs struct {
	DryRun bool `json:"dryRun"`
	// Resume is true by default: the run starts at the checkpoint of the latest run that did not walk to its end (Q-STOR-261).
	Resume *bool `json:"resume,omitempty"`
	// MinAge is a duration ("24h"); empty is storage.orphans.min_age.
	MinAge string `json:"minAge,omitempty"`
	// Connection is the id of one connection; empty is every one.
	Connection string `json:"connection,omitempty"`

	limits *sweepLimits // the bounds Validate checks; set by NewSweepJobArgs
}

type sweepLimits struct {
	floor time.Duration // twice storage.upload.commit_deadline
	ids   []string
}

// NewSweepJobArgs is the value the API decodes the parameters of a trigger into: its Validate refuses (422) a minAge that is not
// greater than twice storage.upload.commit_deadline, which could reach objects whose uploader has not inserted their row yet
// (R-STOR-005), and a connection the configuration does not name.
func NewSweepJobArgs(cfg config.Storage) *SweepJobArgs {
	l := &sweepLimits{floor: 2 * cfg.Upload.CommitDeadline}
	for _, c := range cfg.Connections {
		l.ids = append(l.ids, c.WithDefaults().ID)
	}
	return &SweepJobArgs{limits: l}
}

// Validate checks the arguments against the bounds of NewSweepJobArgs; a value decoded without them checks only the syntax.
func (a *SweepJobArgs) Validate() error {
	var fields []errs.FieldError
	if a.MinAge != "" {
		d, err := time.ParseDuration(a.MinAge)
		switch {
		case err != nil || d <= 0:
			fields = append(fields, errs.FieldError{Pointer: "/minAge", Code: "invalid", Detail: "a duration such as 24h"})
		case a.limits != nil && d <= a.limits.floor:
			fields = append(fields, errs.FieldError{
				Pointer: "/minAge", Code: "too_short",
				Detail: "must be greater than twice storage.upload.commit_deadline (" + a.limits.floor.String() + "): a shorter age could reach objects whose upload has not inserted their row yet",
			})
		}
	}
	if a.Connection != "" && a.limits != nil && !slices.Contains(a.limits.ids, a.Connection) {
		fields = append(fields, errs.FieldError{Pointer: "/connection", Code: "unknown", Detail: "no connection of storage.connections has this id"})
	}
	if len(fields) == 0 {
		return nil
	}
	return errs.E(errs.Invalid, errs.CodeValidation, "The parameters of the orphan sweep are not valid.", errs.WithFields(fields...))
}

// resume reports the member resume, true when it is absent.
func (a *SweepJobArgs) resume() bool { return a.Resume == nil || *a.Resume }

// minAge is the member minAge, zero when it is absent.
func (a *SweepJobArgs) minAge() (time.Duration, error) {
	if a.MinAge == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(a.MinAge)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("orphan sweep: minAge %q is not a duration", a.MinAge)
	}
	return d, nil
}

// sweep is the state of one Sweep: the walk on top of a collection run's deletion machinery (claims, step 4 per copy, the record and
// the hand-over at the end).
type sweep struct {
	r      *run
	o      SweepOptions
	res    SweepResult
	cutoff time.Time // an object without a blob row older than this is an orphan
	// paused is set once the run read the deletion pause: from then on it deletes and aborts nothing and saves no checkpoint.
	paused bool
}

// Sweep runs one orphan sweep (R-STOR-026). Cancelling ctx is a graceful stop as for a collection run: the store calls are cancelled
// and awaited, the deletes that succeeded are recorded and the other claims are handed over. A returned error does not mean nothing
// happened; the result says what did.
func (c *Collector) Sweep(ctx context.Context, o SweepOptions) (res SweepResult, err error) {
	minAge := o.MinAge
	if minAge == 0 {
		minAge = c.storage.Orphans.MinAge
	}
	if floor := 2 * c.storage.Upload.CommitDeadline; minAge <= floor {
		return SweepResult{DryRun: o.DryRun}, fmt.Errorf("orphan sweep: minAge %s is not greater than twice storage.upload.commit_deadline (%s)", minAge, floor)
	}
	r := &run{c: c, opts: RunOptions{DryRun: o.DryRun, Draining: o.Draining}, claimed: map[Digest]claim{}}
	s := &sweep{r: r, o: o, cutoff: c.now().Add(-minAge)}
	s.res = SweepResult{DryRun: o.DryRun, Connections: []SweepConnectionResult{}, Skipped: []SweepSkip{}}
	if o.Cursor != (SweepCursor{}) {
		cur := o.Cursor
		s.res.ResumedFrom = &cur
	}
	defer func() {
		if !errors.Is(err, ErrFault) {
			r.finalize(ctx)
		}
		s.res.Errors += r.res.Errors
		res = s.res
	}()

	conns := c.conns.List()
	start := 0
	if o.Cursor.Connection != "" {
		if i := slices.IndexFunc(conns, func(cn *Connection) bool { return cn.ID() == o.Cursor.Connection }); i >= 0 {
			start = i
		}
	}
	walked := false
	for i := start; i < len(conns); i++ {
		conn := conns[i]
		if o.Connection != "" && conn.ID() != o.Connection {
			continue
		}
		walked = true
		after := ""
		if i == start && o.Cursor.Connection == conn.ID() {
			after = o.Cursor.Prefix
		}
		if err = s.connection(ctx, conn, after); err != nil {
			return s.res, err
		}
	}
	if o.Connection != "" && !walked {
		s.res.Skipped = append(s.res.Skipped, SweepSkip{Connection: o.Connection, Reason: "not_configured"})
	}
	if !o.DryRun && !s.paused {
		err = r.settle(ctx)
	}
	return s.res, err
}

// deletionsOff reports whether the run may not delete or abort: a dry run, or the deletion pause seen.
func (s *sweep) deletionsOff() bool { return s.o.DryRun || s.paused }

// checkpoint saves the cursor, unless the run walks under the deletion pause: its checkpoint then stays before the first prefix it
// walked without deleting, where a resumed run starts (Q-STOR-261).
func (s *sweep) checkpoint(conn, prefix string) {
	if s.paused || s.o.Checkpoint == nil {
		return
	}
	s.o.Checkpoint(SweepCursor{Connection: conn, Prefix: prefix})
}

// readPause reads the deletion pause before a prefix (R-STOR-024).
func (s *sweep) readPause(ctx context.Context) error {
	if s.o.DryRun || s.paused {
		return nil
	}
	p, err := s.r.paused(ctx)
	if err != nil {
		return err
	}
	if p {
		s.paused, s.res.Paused = true, true
	}
	return nil
}

func (s *sweep) skip(conn, reason string) {
	s.res.Skipped = append(s.res.Skipped, SweepSkip{Connection: conn, Reason: reason})
}

// connection walks one connection from after its prefix after: the foreign keys at its top level when it starts at the beginning,
// the 256 prefixes below sha256/, then its staging area. A connection whose circuit is open is skipped and named; one whose store
// fails during the walk is named with what was done, and the walk goes on with the next connection.
func (s *sweep) connection(ctx context.Context, conn *Connection, after string) error {
	s.res.Connections = append(s.res.Connections, SweepConnectionResult{Connection: conn.ID(), PrefixesTotal: 256})
	res := &s.res.Connections[len(s.res.Connections)-1]
	if _, rejecting := conn.Rejecting(); rejecting {
		s.skip(conn.ID(), "circuit_open")
		return nil
	}
	if foreign, err := s.foreignMarker(ctx, conn); err != nil || foreign {
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.res.Errors++
			s.skip(conn.ID(), "unavailable")
			return nil
		}
		s.skip(conn.ID(), "foreign_marker")
		return nil
	}
	first := 0
	switch after {
	case "":
	case sweepStagingDone:
		res.PrefixesDone = 256
		return nil
	default:
		n, err := strconv.ParseUint(after, 16, 8)
		if err != nil || len(after) != 2 {
			return fmt.Errorf("orphan sweep: the checkpoint names the prefix %q", after)
		}
		first, res.PrefixesDone = int(n)+1, int(n)+1
	}
	failed := func(err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.res.Errors++
		reason := "unavailable"
		if errors.Is(err, ErrCircuitOpen) {
			reason = "circuit_open"
		}
		s.skip(conn.ID(), reason)
		s.r.c.log.Warn("the orphan sweep leaves a connection", "connection", conn.ID(), "error", err)
		return nil
	}
	if first == 0 {
		if err := s.foreign(ctx, conn, res); err != nil {
			return failed(err)
		}
	}
	for p := first; p < 256; p++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.readPause(ctx); err != nil {
			return err
		}
		hh := fmt.Sprintf("%02x", p)
		if err := s.prefix(ctx, conn, hh, res); err != nil {
			if errors.Is(err, ErrFault) {
				return err
			}
			return failed(err)
		}
		res.PrefixesDone++
		s.checkpoint(conn.ID(), hh)
	}
	if err := s.staging(ctx, conn, res); err != nil {
		return failed(err)
	}
	s.checkpoint(conn.ID(), sweepStagingDone)
	return nil
}

// foreignMarker reports whether the connection's location belongs to another connection or installation by its marker (R-STOR-051):
// the sweep then neither adopts nor deletes there, because the objects it finds may be another installation's blobs (Q-STOR-262). An
// absent marker (a start without the start check) is no such evidence.
func (s *sweep) foreignMarker(ctx context.Context, conn *Connection) (bool, error) {
	m, found, err := ReadMarker(ctx, conn.Objects(), conn.Prefix())
	switch {
	case errors.Is(err, ErrNotMarker):
	case err != nil:
		return false, err
	case !found:
		return false, nil
	case m.ConnectionID == conn.ID() && (s.r.c.installation == "" || m.InstallationID == s.r.c.installation):
		return false, nil
	}
	s.r.c.log.Error("the orphan sweep leaves a connection whose marker names another connection or installation", "connection", conn.ID(),
		"marker", MarkerKey(conn.Prefix()), "markerConnection", m.ConnectionID, "markerInstallation", m.InstallationID)
	return true, nil
}

// foreign reports the keys at the top of the connection's prefix outside the families of R-STOR-059 and the entries of sha256/ that
// are no two-digit prefix; it never deletes one.
func (s *sweep) foreign(ctx context.Context, conn *Connection, res *SweepConnectionResult) error {
	top := ""
	if conn.Prefix() != "" {
		top = conn.Prefix() + "/"
	}
	// keyed/ takes the place of sha256/ on an encrypted connection with keyed names (R-STOR-086)
	families := map[string]bool{blobFamily(conn): true, top + "_uploads/": true, top + "_probe/": true, top + "_journal/": true}
	err := s.listAll(ctx, conn, top, "/", func(page ListPage) error {
		for _, p := range page.Prefixes {
			if !families[p] {
				s.foreignKey(conn, res, p)
			}
		}
		for _, o := range page.Objects {
			if o.Key != MarkerKey(conn.Prefix()) {
				s.foreignKey(conn, res, o.Key)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	sha := blobFamily(conn)
	return s.listAll(ctx, conn, sha, "/", func(page ListPage) error {
		for _, p := range page.Prefixes {
			if hh := strings.TrimSuffix(strings.TrimPrefix(p, sha), "/"); !isPrefixHex(hh) {
				s.foreignKey(conn, res, p)
			}
		}
		for _, o := range page.Objects {
			s.foreignKey(conn, res, o.Key)
		}
		return nil
	})
}

func isPrefixHex(s string) bool {
	if len(s) != 2 {
		return false
	}
	for i := range 2 {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (s *sweep) foreignKey(conn *Connection, res *SweepConnectionResult, key string) {
	res.ForeignKeys++
	if res.ForeignKeys <= foreignLogged {
		s.r.c.log.Warn("a key outside quader's layout is left alone", "connection", conn.ID(), "key", key)
	}
}

// listAll lists every page below prefix.
func (s *sweep) listAll(ctx context.Context, conn *Connection, prefix, delimiter string, fn func(ListPage) error) error {
	token := ""
	for {
		page, err := conn.Objects().List(ctx, prefix, ListOptions{Delimiter: delimiter, Token: token, Max: 1000})
		if err != nil {
			return err
		}
		if err := fn(page); err != nil {
			return err
		}
		if page.Next == "" || page.Next == token {
			return nil
		}
		token = page.Next
	}
}

// listed is an object of a listing below sha256/ with the digest its name carries, or below keyed/, whose name carries none until
// the object's record named it (placed).
type listed struct {
	o ObjectInfo
	d Digest
	// keyID and stored are what an object's record says, for an object of an encrypted connection the sweep placed by its record:
	// the storage key it names and S(L) of the size its wrap binds (R-STOR-087).
	keyID  string
	stored int64
}

// prefix walks one top-level prefix of a connection, a page of the listing at a time, and drops the rows whose deletes have settled.
func (s *sweep) prefix(ctx context.Context, conn *Connection, hh string, res *SweepConnectionResult) error {
	err := s.listAll(ctx, conn, blobFamily(conn)+hh+"/", "", func(page ListPage) error {
		var items []listed
		for _, o := range page.Objects {
			res.ObjectsSeen++
			if conn.KeyedNames() {
				if !isKeyedName(conn.Prefix(), o.Key) {
					s.foreignKey(conn, res, o.Key)
					continue
				}
				items = append(items, listed{o: o})
				continue
			}
			d, ok := ParseKey(conn.Prefix(), o.Key)
			if !ok {
				s.foreignKey(conn, res, o.Key)
				continue
			}
			items = append(items, listed{o: o, d: d})
		}
		if conn.Encrypted() {
			return s.sealedPage(ctx, conn, items, res)
		}
		return s.page(ctx, conn, items, res)
	})
	if err != nil || s.deletionsOff() {
		return err
	}
	// the rows whose delete has settled go now, so that an upload of such content does not wait for the end of the walk
	_, err = s.r.finishEligible(ctx)
	if s.r.res.Paused {
		s.paused, s.res.Paused = true, true
	}
	return err
}

// page decides each object of a page by its rows (R-STOR-026).
func (s *sweep) page(ctx context.Context, conn *Connection, items []listed, res *SweepConnectionResult) error {
	if len(items) == 0 {
		return nil
	}
	rows, err := s.lookup(ctx, conn, items)
	if err != nil {
		return err
	}
	var claims []Digest
	for _, it := range items {
		row := rows[it.o.Key]
		switch {
		case row.HasCopy:
			// a copy row of a live blob in state deleting is a drain's claim: only a present copy is judged
			if row.CopyState == CopyPresent && it.o.Size >= 0 && it.o.Size != row.StoredSize && s.mark(ctx, conn, it, row.StoredSize) {
				res.CopiesMarked++
			}
		case row.BlobState == "live":
			// a draining connection's object is the drain's: one it deleted may still be listed by a store that applies deletes late
			// (Q-STOR-262)
			if !conn.Drain() && s.adopt(ctx, conn, it, row.BlobSize) {
				res.Adopted++
			}
		case row.BlobState == "deleting":
			// collection or the claim of an earlier sweep owns it
		default:
			if it.o.LastModified.IsZero() || !it.o.LastModified.Before(s.cutoff) {
				continue // younger than minAge, or of no known age: an upload may still insert its row
			}
			res.OrphansFound++
			if s.deletionsOff() {
				continue
			}
			ok, err := s.claim(ctx, conn, it)
			if err != nil {
				return err
			}
			if ok {
				claims = append(claims, it.d)
			}
		}
	}
	if len(claims) == 0 {
		return nil
	}
	deleted, reclaimed := s.r.res.Deleted, s.r.res.BytesReclaimed
	err = s.r.deleteObjects(ctx, claims)
	res.OrphansDeleted += s.r.res.Deleted - deleted
	res.BytesReclaimed += s.r.res.BytesReclaimed - reclaimed
	if s.r.res.Paused {
		s.paused, s.res.Paused = true, true
	}
	return err
}

// lookup reads the copy row (by the name on the connection) and the blob row (by the digest) of every object of a page.
func (s *sweep) lookup(ctx context.Context, conn *Connection, items []listed) (map[string]q.SweepLookupRow, error) {
	keys, digests := make([]string, len(items)), make([][]byte, len(items))
	for i, it := range items {
		keys[i], digests[i] = it.o.Key, it.d.Bytes()
	}
	out := make(map[string]q.SweepLookupRow, len(items))
	err := s.r.c.db.InTx(ctx, func(tx meta.Tx) error {
		clear(out)
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.SweepLookup(ctx, q.SweepLookupParams{Keys: keys, Digests: digests, ConnectionID: conn.ID()})
		if err != nil {
			return err
		}
		for _, row := range rows {
			out[row.ObjectKey] = row
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("look up the rows of a listing: %w", err)
	}
	return out, nil
}

// sizeNow asks the store for an object's size once more: one listing is no evidence for a mark (Q-STOR-262). false when the store
// does not answer with a size.
func sizeNow(ctx context.Context, conn *Connection, key string) (int64, bool) {
	info, err := conn.Objects().Head(ctx, key)
	if err != nil || info.Size < 0 {
		return 0, false
	}
	return info.Size, true
}

// mark marks a present copy corrupt whose object has another size than its stored size, after a HeadObject confirmed the size, and
// queues its repair (R-STOR-026, R-STOR-056); the blob's state follows from its copies (R-STOR-052).
func (s *sweep) mark(ctx context.Context, conn *Connection, it listed, stored int64) bool {
	if s.o.DryRun {
		return true
	}
	if size, ok := sizeNow(ctx, conn, it.o.Key); !ok || size == stored {
		return false
	}
	return s.r.c.markCopy(ctx, it.d, conn.ID(), CopyCorrupt)
}

// adopt records the object of a live blob that no copy row names (R-STOR-026): present and not verified, so the scrub reads it first,
// or corrupt with a queued repair when its size, confirmed by a HeadObject, is not the blob's. The blob row is locked as for a mark,
// because the adoption re-derives its content state: a blob that no connection held whole is whole again (Q-STOR-262).
func (s *sweep) adopt(ctx context.Context, conn *Connection, it listed, blobSize int64) bool {
	if s.o.DryRun {
		return true
	}
	state := CopyPresent
	if it.o.Size >= 0 && it.o.Size != blobSize {
		size, ok := sizeNow(ctx, conn, it.o.Key)
		if !ok {
			return false
		}
		if size != blobSize {
			state = CopyCorrupt
		}
	}
	return s.adoptRow(ctx, conn, it, blobSize, state, "")
}

// adoptRow inserts the adopted copy row of the object it of a live blob: its stored size, its state and, on an encrypted connection,
// the storage key its record names; a corrupt one queues its repair.
func (s *sweep) adoptRow(ctx context.Context, conn *Connection, it listed, stored int64, state, keyID string) bool {
	var adopted bool
	var dm damage
	err := s.r.c.db.InTx(ctx, func(tx meta.Tx) error {
		adopted, dm = false, damage{}
		rows := pgRows{}
		lock, found, err := rows.LockForMark(ctx, tx, it.d)
		if err != nil || !found || lock != "live" {
			return err
		}
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		n, err := qs.SweepAdoptCopy(ctx, q.SweepAdoptCopyParams{
			Sha256: it.d.Bytes(), ConnectionID: conn.ID(), ObjectKey: it.o.Key, StoredSize: stored, StorageKeyID: keyID, State: state,
			WrittenUnix: float64(it.o.LastModified.UnixNano()) / 1e9,
		})
		if err != nil || n == 0 {
			return err
		}
		adopted = true
		if state == CopyCorrupt {
			if err := rows.QueueCopy(ctx, tx, conn.ID(), it.d); err != nil {
				return err
			}
		}
		dm, err = rederiveRows(ctx, rows, tx, it.d)
		return err
	})
	if err != nil {
		s.r.res.Errors++
		s.r.c.log.Warn("the orphan sweep could not adopt an object", "connection", conn.ID(), "sha256", it.d.Hex(), "error", err)
		return false
	}
	if adopted {
		s.r.c.log.Info("the orphan sweep adopted the object of a live blob", "connection", conn.ID(), "sha256", it.d.Hex(), "state", state)
		if state == CopyCorrupt {
			s.r.c.metrics.copyDamaged(conn.ID(), CopyCorrupt)
		}
		s.r.c.reportDamage(it.d, dm)
	}
	return adopted
}

// claim inserts the deleting blob row and the deleting copy row of an orphan (R-STOR-026, Q-STOR-238) and registers the claim for
// steps (4) to (6). false when a blob row exists by now: the object is the next run's.
func (s *sweep) claim(ctx context.Context, conn *Connection, it listed) (bool, error) {
	if err := s.r.fault("sweep:claim"); err != nil {
		return false, err
	}
	start := s.r.c.now() // the reading before the claim, as for collection's (R-STOR-024 hand-over rules)
	size := max(it.o.Size, 0)
	var n int64
	err := s.r.c.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		n, err = qs.SweepClaimOrphan(ctx, q.SweepClaimOrphanParams{
			Sha256: it.d.Bytes(), Size: size, ConnectionID: conn.ID(), ObjectKey: it.o.Key,
			WrittenUnix: float64(it.o.LastModified.UnixNano()) / 1e9,
		})
		return err
	})
	if err != nil {
		return false, fmt.Errorf("claim an orphan: %w", err)
	}
	if n == 0 {
		return false, nil
	}
	s.r.claimed[it.d] = claim{start: start, size: size}
	return true, nil
}

// markCopy records evidence about one copy found by the sweep, as the store's mark does (R-STOR-055): a present copy becomes status,
// its repair is queued, and the blob's content state follows from its copies.
func (c *Collector) markCopy(ctx context.Context, d Digest, conn, status string) bool {
	var changed bool
	var dm damage
	err := c.db.InTx(ctx, func(tx meta.Tx) error {
		changed, dm = false, damage{}
		rows := pgRows{}
		state, found, err := rows.LockForMark(ctx, tx, d)
		if err != nil || !found || state != "live" {
			return err
		}
		if changed, err = rows.SetCopyState(ctx, tx, d, conn, CopyPresent, status); err != nil || !changed {
			return err
		}
		if err := rows.QueueCopy(ctx, tx, conn, d); err != nil {
			return err
		}
		dm, err = rederiveRows(ctx, rows, tx, d)
		return err
	})
	if err != nil {
		c.log.Error("could not mark a copy damaged", "sha256", d.Hex(), "connection", conn, "status", status, "error", err)
		return false
	}
	if changed {
		c.metrics.copyDamaged(conn, status)
		c.log.Warn("a copy of a blob is damaged", "sha256", d.Hex(), "connection", conn, "status", status)
		c.reportDamage(d, dm)
	}
	return changed
}

// rederiveRows sets the blob's content state to what its copies give (R-STOR-052) inside tx, which holds the blob's mark lock.
func rederiveRows(ctx context.Context, rows Rows, tx meta.Tx, d Digest) (damage, error) {
	copies, err := rows.Copies(ctx, tx, d)
	if err != nil {
		return damage{}, err
	}
	status := DeriveContentStatus(copies)
	changed, err := rows.SetContentStatus(ctx, tx, d, status)
	return damage{blobChanged: changed, status: status}, err
}

// reportDamage counts and logs a change of a blob's content state that the sweep caused, as the store's report does.
func (c *Collector) reportDamage(d Digest, dm damage) {
	switch {
	case !dm.blobChanged:
	case dm.status == StatusOK:
		c.log.Info("a blob has a present copy again", "sha256", d.Hex())
	default:
		c.metrics.damage(dm.status)
		c.log.Error("no connection holds the object of a blob whole", "sha256", d.Hex(), "status", dm.status)
	}
}

// staging is the sweep of a connection's staging area (R-STOR-026, R-STOR-054, Q-STOR-263): the multipart uploads below its prefix
// older than storage.orphans.multipart_max_age are aborted, and the objects below _uploads/ of sessions that are over, unknown or
// pinned to another connection, and the leftover probe objects, are deleted once older than storage.uploads.session_max_age. An open
// session's multipart upload is never that old, because session_max_age is shorter than multipart_max_age.
func (s *sweep) staging(ctx context.Context, conn *Connection, res *SweepConnectionResult) error {
	now := s.r.c.now()
	if err := s.abortUploads(ctx, conn, now.Add(-s.r.c.storage.Orphans.MultipartMaxAge), res); err != nil {
		return err
	}
	cutoff := now.Add(-s.r.c.sessionLimits().SessionMaxAge)
	root := UploadsPrefix(conn.Prefix())
	err := s.listAll(ctx, conn, root, "/", func(page ListPage) error {
		for _, o := range page.Objects {
			s.foreignKey(conn, res, o.Key)
		}
		var ids []string
		for _, p := range page.Prefixes {
			id := strings.TrimSuffix(strings.TrimPrefix(p, root), "/")
			if !canonicalID(id) {
				s.foreignKey(conn, res, p)
				continue
			}
			ids = append(ids, id)
		}
		known, err := s.knownSessions(ctx, ids)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if row, ok := known[id]; ok && row.ConnectionID == conn.ID() && row.State != "done" && row.State != "aborted" {
				continue // a live session of this connection: its row decides
			}
			if err := s.deleteOlder(ctx, conn, SessionPrefix(conn.Prefix(), id), cutoff, res); err != nil {
				if ctx.Err() != nil || errors.Is(err, ErrCircuitOpen) {
					return err
				}
				s.res.Errors++
				s.r.c.log.Warn("could not remove the staging objects of a session", "connection", conn.ID(), "session", id, "error", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.deleteOlder(ctx, conn, probePrefix(conn.Prefix()), cutoff, res)
}

// abortUploads aborts the multipart uploads below the connection's prefix that were initiated before cutoff.
func (s *sweep) abortUploads(ctx context.Context, conn *Connection, cutoff time.Time, res *SweepConnectionResult) error {
	prefix := ""
	if conn.Prefix() != "" {
		prefix = conn.Prefix() + "/"
	}
	after := MultipartMarker{}
	for {
		ups, next, err := listUploads(ctx, conn.Objects(), prefix, after)
		if err != nil {
			return err
		}
		for _, up := range ups {
			if up.Initiated.IsZero() || !up.Initiated.Before(cutoff) {
				continue
			}
			if s.deletionsOff() {
				res.MultipartAborted++
				continue
			}
			if err := conn.Objects().AbortMultipart(ctx, up.Key, up.UploadID); err != nil && !IsNoSuchUpload(err) && !errors.Is(err, ErrNotFound) {
				if ctx.Err() != nil || errors.Is(err, ErrCircuitOpen) {
					return err
				}
				s.res.Errors++
				s.r.c.log.Warn("could not abort a multipart upload", "connection", conn.ID(), "key", up.Key, "error", err)
				continue
			}
			res.MultipartAborted++
		}
		if next == (MultipartMarker{}) || next == after {
			return nil
		}
		after = next
	}
}

// deleteOlder deletes the objects below prefix last modified before cutoff.
func (s *sweep) deleteOlder(ctx context.Context, conn *Connection, prefix string, cutoff time.Time, res *SweepConnectionResult) error {
	var errs []error
	err := s.listAll(ctx, conn, prefix, "", func(page ListPage) error {
		for _, o := range page.Objects {
			if o.LastModified.IsZero() || !o.LastModified.Before(cutoff) {
				continue
			}
			if s.deletionsOff() {
				res.StagingDeleted++
				continue
			}
			if err := conn.Objects().Delete(ctx, o.Key); err != nil && !errors.Is(err, ErrNotFound) {
				if ctx.Err() != nil || errors.Is(err, ErrCircuitOpen) {
					return err
				}
				errs = append(errs, err)
				continue
			}
			res.StagingDeleted++
		}
		return nil
	})
	if err != nil {
		return err
	}
	return errors.Join(errs...)
}

// listUploads reads one page of the unfinished multipart uploads below prefix; a store that cannot page answers with all of them as one.
func listUploads(ctx context.Context, objects ObjectStore, prefix string, after MultipartMarker) ([]MultipartUpload, MultipartMarker, error) {
	if p, ok := objects.(MultipartPager); ok {
		return p.ListMultipartPage(ctx, prefix, after, sweepPage)
	}
	if after != (MultipartMarker{}) {
		return nil, MultipartMarker{}, nil
	}
	ups, err := objects.ListMultipart(ctx, prefix)
	return ups, MultipartMarker{}, err
}

// sweepPage is the number of multipart uploads the staging step lists at a time.
const sweepPage = 500

// knownSessions returns the rows of the session ids that have one, asked in chunks.
func (s *sweep) knownSessions(ctx context.Context, ids []string) (map[string]q.UploadKnownSessionsRow, error) {
	out := make(map[string]q.UploadKnownSessionsRow, len(ids))
	for len(ids) > 0 {
		chunk := ids[:min(len(ids), sessionBatch)]
		ids = ids[len(chunk):]
		var rows []q.UploadKnownSessionsRow
		err := s.r.c.db.InTx(ctx, func(tx meta.Tx) error {
			qs, err := queries(tx)
			if err != nil {
				return err
			}
			rows, err = qs.UploadKnownSessions(ctx, chunk)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("read upload sessions: %w", err)
		}
		for _, row := range rows {
			out[row.ID] = row
		}
	}
	return out, nil
}

// resumePoint is the checkpoint a run with resume: true starts at (Q-STOR-261); the zero cursor when there is none.
func (c *Collector) resumePoint(ctx context.Context, connection string, dryRun bool) (SweepCursor, error) {
	var raw []byte
	err := c.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		raw, err = qs.SweepResumePoint(ctx, q.SweepResumePointParams{Connection: connection, DryRun: dryRun})
		return err
	})
	if err != nil {
		return SweepCursor{}, fmt.Errorf("read the checkpoint to resume at: %w", err)
	}
	var cur SweepCursor
	if len(raw) == 0 || string(raw) == "null" {
		return cur, nil
	}
	if err := json.Unmarshal(raw, &cur); err != nil {
		return SweepCursor{}, nil //nolint:nilerr // a checkpoint that cannot be read starts the sweep at the beginning
	}
	return cur, nil
}
