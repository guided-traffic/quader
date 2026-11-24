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

// Tracker keeps the running downloads of one replica in memory and buffers the ended ones for the next publication (R-OPS-050,
// R-OPS-051). Every remote engine of the process reports to the same tracker.
type Tracker struct {
	replica string
	now     func() time.Time
	m       *metrics

	mu      sync.Mutex
	running map[string]*download
	// ended are the downloads that ended since the last publication took them, the oldest first, at most MaxFinished.
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
	t := &Tracker{replica: o.Replica, now: o.Now, running: map[string]*download{}}
	if t.now == nil {
		t.now = time.Now
	}
	t.m = newMetrics(o.Metrics, o.RepoLabel, t.Running)
	return t
}

// download is one running download.
type download struct {
	t     *Tracker
	id    string
	start proxy.DownloadStart
	// begun is the begin on the replica's clock; with a monotonic reading, the elapsed time is the monotonic one.
	begun time.Time
	bytes atomic.Int64
	// counted is the byte counter of the download's repository, resolved once at the begin.
	counted prometheus.Counter
	ended   atomic.Bool
	// win is read and written under t.mu, by the publication only.
	win window
}

// Begin implements [proxy.DownloadTracker]: the download is running from now on.
func (t *Tracker) Begin(s proxy.DownloadStart) proxy.Download {
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	d := &download{t: t, id: id.String(), start: s, begun: t.now(), counted: t.m.bytesOf(s.Repo)}
	t.mu.Lock()
	t.running[d.id] = d
	t.mu.Unlock()
	return d
}

// Running is the number of downloads that run on the replica now.
func (t *Tracker) Running() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.running)
}

// Reader implements [proxy.Download]: one atomic addition per read.
func (d *download) Reader(r io.Reader) io.Reader { return &countingReader{r: r, d: d} }

type countingReader struct {
	r io.Reader
	d *download
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.d.bytes.Add(int64(n))
		c.d.counted.Add(float64(n))
	}
	return n, err
}

// End implements [proxy.Download]: the download leaves the running set and waits in the buffer for the next publication. A second call
// does nothing.
func (d *download) End(outcome string, f *remotecuration.Failure) {
	if d.ended.Swap(true) {
		return
	}
	t := d.t
	end := t.now()
	fin := d.finished(end, d.bytes.Load(), end.Sub(d.begun))
	fin.Outcome = outcome
	if f != nil && outcome == OutcomeFailed {
		fin.FailureClass, fin.FailureCode = string(f.Class), f.Code
	}
	t.m.ended(d.start.Repo, outcome)
	t.mu.Lock()
	delete(t.running, d.id)
	t.ended = keepNewest(append(t.ended, fin))
	t.mu.Unlock()
}

// finished is the download as a finished row, ended at end after elapsed.
func (d *download) finished(end time.Time, bytes int64, elapsed time.Duration) Finished {
	s := d.start
	f := Finished{
		ID: d.id, RepoID: s.RepoID, Name: s.Name, Version: s.Version, Path: s.Path, Bytes: bytes, StartedAt: d.begun.UTC(),
		FinishedAt: end.UTC(), DurationMs: max(elapsed.Milliseconds(), 0), Origin: s.Cause.Origin, Replica: d.t.replica,
	}
	if s.Total >= 0 {
		total := s.Total
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

// keepNewest bounds the buffer of ended downloads to the newest MaxFinished (R-OPS-051): older ones would be trimmed by the insert anyway.
func keepNewest(ended []Finished) []Finished {
	if len(ended) <= MaxFinished {
		return ended
	}
	return append(ended[:0], ended[len(ended)-MaxFinished:]...)
}

// Snapshot is what one publication writes (R-OPS-050, R-OPS-051).
type Snapshot struct {
	// Running are at most MaxPublished running downloads, the longest running first.
	Running []Entry
	// Omitted is the number of running downloads left out of Running.
	Omitted int
	// Ended are the downloads that ended since the last snapshot, the oldest first.
	Ended []Finished
}

// snapshot takes the publication's samples and the buffer of ended downloads. A publication that fails hands the ended downloads back
// with requeue.
func (t *Tracker) snapshot() Snapshot {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	all := make([]*download, 0, len(t.running))
	for _, d := range t.running {
		all = append(all, d)
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].begun.Equal(all[j].begun) {
			return all[i].begun.Before(all[j].begun)
		}
		return all[i].id < all[j].id
	})
	s := Snapshot{Ended: t.ended}
	t.ended = nil
	for i, d := range all {
		elapsed := now.Sub(d.begun)
		bytes := d.bytes.Load()
		speed, ok := d.win.speed(elapsed, bytes) // every running download is sampled, the left-out ones too
		if i >= MaxPublished {
			s.Omitted++
			continue
		}
		e := d.entry(bytes, elapsed)
		if ok {
			e.Speed = &speed
		}
		s.Running = append(s.Running, e)
	}
	return s
}

// requeue hands back the ended downloads of a publication that failed: they go out with the next one, in front of the newer ones.
func (t *Tracker) requeue(ended []Finished) {
	if len(ended) == 0 {
		return
	}
	t.mu.Lock()
	t.ended = keepNewest(append(ended, t.ended...))
	t.mu.Unlock()
}

// entry is the running download as its replica publishes it.
func (d *download) entry(bytes int64, elapsed time.Duration) Entry {
	f := d.finished(time.Time{}, bytes, elapsed)
	e := Entry{
		ID: f.ID, RepoID: f.RepoID, Name: f.Name, Version: f.Version, Path: f.Path, Bytes: bytes, Total: f.Total, StartedAt: f.StartedAt,
		ElapsedMs: f.DurationMs, Origin: f.Origin, QueueOrigin: f.QueueOrigin,
	}
	if f.QueueRow != nil {
		e.QueueRow = *f.QueueRow
	}
	if f.By != nil {
		e.By = *f.By
	}
	return e
}
