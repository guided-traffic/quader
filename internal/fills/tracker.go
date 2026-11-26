package fills

import (
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/remotecuration"
)

// Tracker keeps the running downloads of one replica in memory and buffers the ended ones for the next publication (R-OPS-049 to
// R-OPS-051). A download is a unit, what a person asked for: the engine reports every file it reads from an upstream as a part, and the
// parts of one key (proxy.DownloadStart.Unit) make one unit. Every remote engine of the process reports to the same tracker.
type Tracker struct {
	replica string
	now     func() time.Time
	m       *metrics

	mu sync.Mutex
	// units are the running units by key; a unit of one file without a key of its own is keyed "p|<id>".
	units map[string]*unit
	// holds are the holds on units by key (Hold): a held unit runs between its parts, and its holder names the parts it will fetch.
	holds map[string]*hold
	// ended are the units that ended since the last publication took them, the oldest first, at most MaxFinished.
	ended []Finished
}

var _ proxy.DownloadTracker = (*Tracker)(nil)

// TrackerOptions configure [NewTracker].
type TrackerOptions struct {
	// Replica is the pod name the downloads are recorded with.
	Replica string
	// Metrics registers the families of R-OPS-052; nil registers none.
	Metrics prometheus.Registerer
	// RepoLabel caps the label repo (R-OPS-007): the value a repository's series is labelled with in a family; nil labels with the name.
	RepoLabel func(family, name string) string
	// Now replaces the clock, for tests.
	Now func() time.Time
}

// NewTracker creates the tracker of a replica.
func NewTracker(o TrackerOptions) *Tracker {
	t := &Tracker{replica: o.Replica, now: o.Now, units: map[string]*unit{}, holds: map[string]*hold{}}
	if t.now == nil {
		t.now = time.Now
	}
	t.m = newMetrics(o.Metrics, o.RepoLabel, t.Running)
	return t
}

// unit is one running download: the parts of one key on this replica. Every field but the parts' byte counters is read and written
// under t.mu.
type unit struct {
	id, key string
	start   proxy.DownloadStart
	// version is the unit's version: the first part's that has one, or the resolved one (Resolver).
	version string
	// resolve says the version is still to be resolved.
	resolve bool
	// begun is the begin on the replica's clock; with a monotonic reading, the elapsed time is the monotonic one.
	begun time.Time
	// lastEnd is the end of the newest part that ended.
	lastEnd time.Time
	// running are the parts that run; latest is the newest part of each path, whose outcome is the path's.
	running map[*part]struct{}
	latest  map[string]*part
	// endedBytes are the bytes of the parts that ended, every attempt of a path included.
	endedBytes int64
	// hold is the unit's hold while one is taken, nil otherwise.
	hold *hold
	// failed is the failure of the work the unit is for (DownloadHold.Fail), which decides its outcome before its parts'.
	failed *remotecuration.Failure
	win    window
}

// part is one file of a unit: one transfer of an artifact's body.
type part struct {
	t     *Tracker
	u     *unit
	path  string
	total int64
	bytes atomic.Int64
	// counted is the byte counter of the download's repository, resolved once at the begin.
	counted prometheus.Counter
	ended   atomic.Bool
	// outcome and failure are set at the end, under t.mu.
	outcome string
	failure *remotecuration.Failure
}

// hold is the state of Hold on one key: how many holders hold it, and the parts they named with their sizes.
type hold struct {
	n int
	// named says a holder named the parts (Expect): the unit's total is then known.
	named  bool
	expect map[string]int64
}

// Begin implements [proxy.DownloadTracker]: the part runs from now on, in the unit of its key; a key without a running unit begins one.
func (t *Tracker) Begin(s proxy.DownloadStart) proxy.Download {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	key := s.Unit
	u := t.units[key]
	if key == "" || u == nil {
		u = t.newUnit(s, now)
	}
	if u.version == "" && s.Version != "" {
		u.version, u.resolve = s.Version, false
	}
	p := &part{t: t, u: u, path: s.Path, total: s.Total, counted: t.m.bytesOf(s.Repo)}
	u.running[p] = struct{}{}
	u.latest[s.Path] = p
	return p
}

// newUnit begins a unit for the part's facts; t.mu is held.
func (t *Tracker) newUnit(s proxy.DownloadStart, now time.Time) *unit {
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	u := &unit{
		id: id.String(), key: unitKey(s.Unit, id.String()), start: s, version: s.Version, resolve: s.Resolve && s.Version == "",
		begun: now, running: map[*part]struct{}{}, latest: map[string]*part{},
	}
	if s.Unit != "" {
		u.hold = t.holds[s.Unit]
	}
	t.units[u.key] = u
	return u
}

// Running is the number of units that run on the replica now: the gauge of R-OPS-052.
func (t *Tracker) Running() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.units)
}

// Reader implements [proxy.Download]: one atomic addition per read.
func (p *part) Reader(r io.Reader) io.Reader { return &countingReader{r: r, p: p} }

type countingReader struct {
	r io.Reader
	p *part
}

func (c *countingReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	if n > 0 {
		c.p.bytes.Add(int64(n))
		c.p.counted.Add(float64(n))
	}
	return n, err
}

// End implements [proxy.Download]: the part leaves the running set; a unit with no running part, no hold and no linger left ends and
// waits in the buffer for the next publication. A second call does nothing.
func (p *part) End(outcome string, f *remotecuration.Failure) {
	if p.ended.Swap(true) {
		return
	}
	t := p.t
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	u := p.u
	delete(u.running, p)
	u.endedBytes += p.bytes.Load()
	p.outcome = outcome
	if outcome == OutcomeFailed {
		p.failure = f
	}
	u.lastEnd = now
	t.endIfDone(u, now)
}

// endIfDone ends a unit that has no running part, no hold, and whose linger has passed (an open unit, proxy.UnitLinger); t.mu is held.
func (t *Tracker) endIfDone(u *unit, now time.Time) {
	if len(u.running) > 0 || u.hold != nil {
		return
	}
	if u.start.Open && now.Sub(u.lastEnd) < proxy.UnitLinger {
		return
	}
	fin := u.finished(u.lastEnd, u.bytes(), u.lastEnd.Sub(u.begun))
	fin.Outcome, fin.FailureClass, fin.FailureCode, fin.FailurePath = u.outcome()
	fin.Replica = t.replica
	t.m.ended(u.start.Repo, fin.Outcome)
	delete(t.units, u.key)
	t.ended = keepNewest(append(t.ended, fin))
}

// bytes are the unit's bytes: those of the parts that ended and those read so far by the running ones; t.mu is held.
func (u *unit) bytes() int64 {
	n := u.endedBytes
	for p := range u.running {
		n += p.bytes.Load()
	}
	return n
}

// outcome is the unit's outcome from the newest part of each path (R-OPS-049): failed when one failed, with its class, code and path;
// else cancelled when one was cancelled; else completed. t.mu is held.
func (u *unit) outcome() (outcome, class, code, path string) {
	if f := u.failed; f != nil {
		return OutcomeFailed, string(f.Class), f.Code, f.File
	}
	paths := make([]string, 0, len(u.latest))
	for path := range u.latest {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	outcome = OutcomeCompleted
	for _, path := range paths {
		p := u.latest[path]
		switch p.outcome {
		case OutcomeFailed:
			if p.failure != nil {
				return OutcomeFailed, string(p.failure.Class), p.failure.Code, path
			}
			return OutcomeFailed, "", "", path
		case OutcomeCancelled:
			outcome = OutcomeCancelled
		}
	}
	return outcome, "", "", ""
}

// total is the unit's size where it is known (R-OPS-049 "The facts of a unit"); t.mu is held:
//   - a unit whose holder named its parts: the lengths of the parts that began (else the named size) and the named sizes of those that
//     did not;
//   - an open unit without such a hold (a client's pull of an image, a mirror pass's image): none, because a part nobody named may join;
//   - any other: the lengths of its parts.
//
// A part without a length and without a named size leaves the unit without a total.
func (u *unit) total() (int64, bool) {
	h := u.hold
	if h != nil && !h.named {
		h = nil
	}
	if h == nil && (u.start.Open || u.held()) {
		return 0, false
	}
	var sum int64
	for path, p := range u.latest {
		switch {
		case p.total >= 0:
			sum += p.total
		case h != nil && h.expect[path] > 0:
			sum += h.expect[path]
		default:
			return 0, false
		}
	}
	if h != nil {
		for path, size := range h.expect {
			if _, begun := u.latest[path]; begun {
				continue
			}
			if size <= 0 {
				return 0, false
			}
			sum += size
		}
	}
	return sum, true
}

// held says the unit has a hold whose holder has not named its parts yet: its total is not known before they are.
func (u *unit) held() bool { return u.hold != nil && !u.hold.named }

// finished is the unit as a finished row, ended at end after elapsed; t.mu is held.
func (u *unit) finished(end time.Time, bytes int64, elapsed time.Duration) Finished {
	s := u.start
	f := Finished{
		ID: u.id, Unit: u.key, RepoID: s.RepoID, Name: s.Name, Version: u.version, Path: s.Path, Parts: len(u.latest), Bytes: bytes,
		StartedAt: u.begun.UTC(), FinishedAt: end.UTC(), DurationMs: max(elapsed.Milliseconds(), 0), Origin: s.Cause.Origin,
		resolve: u.resolve,
	}
	if total, ok := u.total(); ok {
		f.Total = &total
	}
	if s.Cause.Origin == proxy.DownloadOriginQueue {
		f.QueueOrigin = string(s.Cause.QueueOrigin)
		if validUUID(s.Cause.QueueRow) {
			row := s.Cause.QueueRow
			f.QueueRow = &row
		}
	}
	if validUUID(s.Cause.Principal) {
		by := s.Cause.Principal
		f.By = &by
	}
	return f
}

// Hold implements [proxy.DownloadTracker]: the unit of key runs between its parts until the last holder releases it.
func (t *Tracker) Hold(key string) proxy.DownloadHold {
	t.mu.Lock()
	defer t.mu.Unlock()
	h := t.holds[key]
	if h == nil {
		h = &hold{expect: map[string]int64{}}
		t.holds[key] = h
	}
	h.n++
	if u := t.units[key]; u != nil {
		u.hold = h
	}
	return &holdHandle{t: t, key: key, h: h}
}

type holdHandle struct {
	t        *Tracker
	key      string
	h        *hold
	released atomic.Bool
}

// Expect implements [proxy.DownloadHold]: a path that began already counts with its own length.
func (hh *holdHandle) Expect(path string, size int64) {
	t := hh.t
	t.mu.Lock()
	defer t.mu.Unlock()
	hh.h.named = true
	if u := t.units[hh.key]; u != nil {
		if _, begun := u.latest[path]; begun {
			return
		}
	}
	hh.h.expect[path] = size
}

// Settle implements [proxy.DownloadHold].
func (hh *holdHandle) Settle(path string) {
	t := hh.t
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(hh.h.expect, path)
}

// Fail implements [proxy.DownloadHold]: a unit of the key that has a part fails with f.
func (hh *holdHandle) Fail(f remotecuration.Failure) {
	t := hh.t
	t.mu.Lock()
	defer t.mu.Unlock()
	if u := t.units[hh.key]; u != nil {
		u.failed = &f
	}
}

// Release implements [proxy.DownloadHold]: the last holder's release ends a unit that has no running part.
func (hh *holdHandle) Release() {
	if hh.released.Swap(true) {
		return
	}
	t := hh.t
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	hh.h.n--
	if hh.h.n > 0 {
		return
	}
	delete(t.holds, hh.key)
	if u := t.units[hh.key]; u != nil && u.hold == hh.h {
		u.hold = nil
		if u.lastEnd.IsZero() {
			u.lastEnd = now
		}
		t.endIfDone(u, now)
	}
}

// keepNewest bounds the buffer of ended units to the newest MaxFinished (R-OPS-051): older ones would be trimmed by the insert anyway.
func keepNewest(ended []Finished) []Finished {
	if len(ended) <= MaxFinished {
		return ended
	}
	return append(ended[:0], ended[len(ended)-MaxFinished:]...)
}

// Snapshot is what one publication writes (R-OPS-050, R-OPS-051).
type Snapshot struct {
	// Running are at most MaxPublished running units, the longest running first.
	Running []Entry
	// Omitted is the number of running units left out of Running.
	Omitted int
	// Ended are the units that ended since the last snapshot, the oldest first.
	Ended []Finished
}

// snapshot ends the open units whose linger passed, takes the publication's samples and the buffer of ended units. A publication that
// fails hands the ended units back with requeue.
func (t *Tracker) snapshot() Snapshot {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	all := make([]*unit, 0, len(t.units))
	for _, u := range t.units {
		all = append(all, u)
	}
	for _, u := range all {
		if len(u.running) == 0 {
			t.endIfDone(u, now)
		}
	}
	all = all[:0]
	for _, u := range t.units {
		all = append(all, u)
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].begun.Equal(all[j].begun) {
			return all[i].begun.Before(all[j].begun)
		}
		return all[i].id < all[j].id
	})
	s := Snapshot{Ended: t.ended}
	t.ended = nil
	for i, u := range all {
		elapsed := now.Sub(u.begun)
		bytes := u.bytes()
		speed, ok := u.win.speed(elapsed, bytes) // every running unit is sampled, the left-out ones too
		if i >= MaxPublished {
			s.Omitted++
			continue
		}
		e := u.entry(bytes, elapsed)
		if ok {
			e.Speed = &speed
		}
		s.Running = append(s.Running, e)
	}
	return s
}

// requeue hands back the ended units of a publication that failed: they go out with the next one, in front of the newer ones.
func (t *Tracker) requeue(ended []Finished) {
	if len(ended) == 0 {
		return
	}
	t.mu.Lock()
	t.ended = keepNewest(append(ended, t.ended...))
	t.mu.Unlock()
}

// resolved records the version the resolver found for a running unit, so that it is not asked again.
func (t *Tracker) resolved(id, version string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, u := range t.units {
		if u.id == id && u.version == "" {
			u.version, u.resolve = version, false
		}
	}
}

// entry is the running unit as its replica publishes it; t.mu is held.
func (u *unit) entry(bytes int64, elapsed time.Duration) Entry {
	f := u.finished(time.Time{}, bytes, elapsed)
	e := Entry{
		ID: f.ID, Unit: f.Unit, RepoID: f.RepoID, Name: f.Name, Version: f.Version, Path: f.Path, Parts: f.Parts, Bytes: bytes,
		Total: f.Total, StartedAt: f.StartedAt, ElapsedMs: f.DurationMs, Origin: f.Origin, QueueOrigin: f.QueueOrigin, resolve: f.resolve,
	}
	if f.QueueRow != nil {
		e.QueueRow = *f.QueueRow
	}
	if f.By != nil {
		e.By = *f.By
	}
	return e
}
