// Package throughput caps the bytes a Bastion reads (R-BAS-097, the owner's Q-BAS-331): from the main installation (fills,
// answers passed through, forwarded documents, the continuation of a fill), from the object store by a handover's direct
// read (R-BAS-074), and the public catalogue's pull of W-35. One pacing reader wraps every such body; a read takes its bytes
// from two buckets of the whole Bastion (Q-BAS-332): the total, and for an anonymous caller's read also the anonymous share
// of it.
//
// A bucket is counted in slices of [Slice]: a slice holds a tenth of the second's figure, and a read reserves its next bytes
// in the first slice that has room, then waits until that slice begins (Q-BAS-456). The reservations of the readers therefore
// queue in the order they were made, so N transfers at the cap each read about a N-th of it; a running transfer is slowed and
// never cut. A new transfer (a miss) is admitted when its first bytes find room in a slice that begins within the queue's time
// (limits.slots.queueTimeoutMillis, counted from when it asked for its slot); otherwise it is refused at once, 503 busy with the
// wait until the first slice with room (Q-BAS-457). With Valkey the counters of a slice are the whole Bastion's
// (shared.Bytes); without, each replica holds bytesPerSecond ÷ bastion.replicas.
//
// The public catalogue reads from the total only while a slice is less than half used, so at the cap the catalogue waits first
// and the callers' misses always find half of a slice it did not take (Q-BAS-458). An hourly volume per class, when set,
// refuses the class's next miss once its hour is used up; a transfer that runs is never refused by it (R-BAS-097).
package throughput

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/bastion/shared"
)

// Slice is the window a bucket counts in: a tenth of a second, so a transfer's pace is even within a second and the
// reservations of many readers spread over the second instead of its start.
const Slice = 100 * time.Millisecond

const (
	slicesPerSecond = int64(time.Second / Slice)
	// minChunk and maxChunk bound what one reservation takes: a quarter of the total's slice, so four readers share a slice at
	// least, at least 4 KiB and at most 1 MiB, so a read at 200 MB/s costs at most about 200 claims a second.
	minChunk = 4 << 10
	maxChunk = 1 << 20
	// admitChunk is the most a miss reserves when it is admitted: most answers are documents of kilobytes and a 304, so the
	// admission of many misses at once does not hold the slices that their few large bodies read from.
	admitChunk = 64 << 10
	// sliceGrace is how long a slice's counter outlives the slice: a reservation made into it is released at most this late.
	sliceGrace = 10 * time.Second
	hourTTL    = 2 * time.Hour
)

// Class is on whose behalf the Bastion reads: an anonymous caller, a signed-in caller, or the public catalogue (W-35).
type Class string

// The classes; the values are the labels of the metrics.
const (
	Anonymous Class = metrics.ThroughputAnonymous
	SignedIn  Class = metrics.ThroughputSignedIn
	Catalogue Class = metrics.ThroughputCatalogue
)

// ClassOf is the class of a budget class of the client.
func ClassOf(c mainclient.Class) Class {
	if c == mainclient.Anonymous {
		return Anonymous
	}
	return SignedIn
}

// Options configure a [Pacer].
type Options struct {
	// Bytes are the counters (shared.State.Bytes): the whole Bastion's with Valkey, the replica's share without.
	Bytes shared.Bytes
	// Figures returns the configuration applied now (settings.Holder.Current): a new figure holds for the next reservation of
	// every transfer, also of one that runs.
	Figures func() *settings.Applied
	Metrics *metrics.Metrics
	// Now and Sleep are the clock; nil means time.Now and a timer. Tests inject both.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

// Pacer is a replica's pacing of the bytes it reads. It implements [mainclient.Throughput] and is safe for concurrent use.
type Pacer struct {
	o Options

	mu sync.Mutex
	// frontier is per bucket the first slice not known to be full, a hint that saves a claim of every full slice to the
	// reservations after the first that found it full.
	frontier map[string]int64
}

var _ mainclient.Throughput = (*Pacer)(nil)

// New returns the pacer.
func New(o Options) *Pacer {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Sleep == nil {
		o.Sleep = sleep
	}
	return &Pacer{o: o, frontier: map[string]int64{}}
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

// Export sets the gauge of the configured figures; the server calls it with every configuration applied.
func (p *Pacer) Export(a *settings.Applied) {
	if p.o.Metrics == nil {
		return
	}
	p.o.Metrics.ThroughputLimit.WithLabelValues(metrics.BucketTotal).Set(float64(a.Throughput.BytesPerSecond))
	p.o.Metrics.ThroughputLimit.WithLabelValues(metrics.BucketAnonymous).Set(float64(a.Throughput.AnonymousBytesPerSecond()))
}

// bucket is one bucket a read takes from, as the configuration applied now sizes it.
type bucket struct {
	name string
	// key names the bucket's frontier: the catalogue's own limit of the total has its own.
	key   string
	limit int64 // bytes per slice
}

// plan is what a read of a class takes from: its buckets, the size of one reservation, and whether it is paced at all.
type plan struct {
	buckets []bucket
	chunk   int64
	paced   bool
	// closed is set when a bucket of the class holds nothing (an anonymous share of 0): a new miss is refused, a running
	// transfer reads from the total only.
	closed bool
}

func perSlice(bytesPerSecond int64) int64 { return max(bytesPerSecond/slicesPerSecond, 1) }

// planOf sizes the buckets of a class from the configuration applied now.
func (p *Pacer) planOf(c Class) plan {
	t := p.o.Figures().Throughput
	if t.BytesPerSecond <= 0 {
		return plan{}
	}
	total := perSlice(t.BytesPerSecond)
	pl := plan{paced: true}
	switch c {
	case Catalogue:
		pl.buckets = []bucket{{name: metrics.BucketTotal, key: "catalogue", limit: max(total/2, 1)}}
	case Anonymous:
		if anon := t.AnonymousBytesPerSecond(); t.AnonymousShare < 1 && anon > 0 {
			pl.buckets = append(pl.buckets, bucket{name: metrics.BucketAnonymous, key: metrics.BucketAnonymous, limit: perSlice(anon)})
		} else if t.AnonymousShare <= 0 || anon <= 0 {
			pl.closed = true
		}
		fallthrough
	default:
		pl.buckets = append(pl.buckets, bucket{name: metrics.BucketTotal, key: metrics.BucketTotal, limit: total})
	}
	smallest := total
	for _, b := range pl.buckets {
		smallest = min(smallest, b.limit)
	}
	// Every class reserves the same chunk, a quarter of the total's slice, so that two classes at the cap take turns in equal
	// steps; a smaller bucket bounds it.
	pl.chunk = min(max(total/4, minChunk), maxChunk, smallest)
	return pl
}

func (p *Pacer) sliceOf(t time.Time) int64 { return t.UnixNano() / int64(Slice) }

func startOf(idx int64) time.Time { return time.Unix(0, idx*int64(Slice)) }

// ttl is how long the counter of a slice is kept from now.
func (p *Pacer) ttl(idx int64) time.Duration {
	return max(startOf(idx+1).Sub(p.o.Now()), 0) + sliceGrace
}

// part is the bytes a reservation holds in one bucket.
type part struct {
	name string
	idx  int64
	n    int64
}

// reservation is the bytes a read may take from the beginning of slice idx on: n bytes, held in each of parts.
type reservation struct {
	idx   int64
	n     int64
	parts []part
}

// release gives back what of the reservation was not read.
func (p *Pacer) release(ctx context.Context, r reservation, unread int64) {
	if unread <= 0 {
		return
	}
	ctx = context.WithoutCancel(ctx)
	for _, pt := range r.parts {
		if pt.idx >= p.sliceOf(p.o.Now()) {
			p.o.Bytes.Release(ctx, pt.name, pt.idx, p.ttl(pt.idx), min(unread, pt.n))
		}
	}
}

// reserve reserves up to want bytes of the plan's buckets in the first slice from now on that has room in each, at most in
// slice until (until < 0: without a bound). ok is false when no slice up to until had room; at is then the first slice past it.
func (p *Pacer) reserve(ctx context.Context, pl plan, want int64, until int64) (r reservation, at int64, ok bool) {
	from := p.sliceOf(p.o.Now())
	r = reservation{idx: from, n: want}
	for _, b := range pl.buckets {
		idx, got, ok := p.claim(ctx, b, r.idx, r.n, until)
		if !ok {
			p.release(ctx, r, r.n)
			return reservation{}, idx, false
		}
		// The buckets before this one hold more than this one granted: what this one did not grant goes back to them.
		if got < r.n {
			for i := range r.parts {
				p.o.Bytes.Release(context.WithoutCancel(ctx), r.parts[i].name, r.parts[i].idx, p.ttl(r.parts[i].idx), r.parts[i].n-got)
				r.parts[i].n = got
			}
		}
		r.n, r.idx = got, max(r.idx, idx)
		r.parts = append(r.parts, part{name: b.name, idx: idx, n: got})
	}
	return r, 0, true
}

// claim takes up to want bytes of the bucket in the first slice from from on that has room. It walks the slices one by one,
// starting at the bucket's frontier, and moves the frontier past every slice it found full.
func (p *Pacer) claim(ctx context.Context, b bucket, from, want, until int64) (idx, got int64, ok bool) {
	p.mu.Lock()
	if f := p.frontier[b.key]; f > from {
		from = f
	}
	p.mu.Unlock()
	for idx = from; ; idx++ {
		if until >= 0 && idx > until {
			return idx, 0, false
		}
		if ctx.Err() != nil {
			return idx, 0, false
		}
		if got = p.o.Bytes.Claim(ctx, b.name, idx, p.ttl(idx), want, b.limit); got > 0 {
			return idx, got, true
		}
		p.mu.Lock()
		if p.frontier[b.key] <= idx {
			p.frontier[b.key] = idx + 1
		}
		p.mu.Unlock()
	}
}

// hourName is the counter of a class's hourly volume.
func hourName(c Class) string { return "hour-" + string(c) }

func hourOf(t settings.Throughput, c Class) int64 {
	switch c {
	case Anonymous:
		return t.Hour.Anonymous
	case SignedIn:
		return t.Hour.SignedIn
	}
	return t.Hour.Catalogue
}

// hourUsed reports whether the class's hourly volume is used up, and the wait until it is not: the hour is a sliding window
// over two counters of whole hours, as the limiters of R-ARCH-034 are.
func (p *Pacer) hourUsed(ctx context.Context, c Class, limit int64) (bool, time.Duration) {
	now := p.o.Now()
	idx := now.UnixNano() / int64(time.Hour)
	cur, prev := p.o.Bytes.Add(ctx, hourName(c), idx, hourTTL, 0)
	elapsed := float64(now.UnixNano()%int64(time.Hour)) / float64(time.Hour)
	if float64(prev)*(1-elapsed)+float64(cur) < float64(limit) {
		return false, 0
	}
	return true, hourWait(float64(cur), float64(prev), elapsed, float64(limit))
}

// hourWait is the time until the sliding estimate prev x (1 - elapsed) + cur falls below limit while nothing is added: within
// the current hour as the previous one's weight falls, else in the next one, whose previous counter is cur.
func hourWait(cur, prev, elapsed, limit float64) time.Duration {
	h := float64(time.Hour)
	if prev > 0 && cur < limit {
		if x := 1 - (limit-cur)/prev; x < 1 {
			return time.Duration((max(x, elapsed) - elapsed) * h)
		}
	}
	y := 0.0
	if cur > 0 {
		y = max(0, 1-limit/cur)
	}
	return time.Duration(((1 - elapsed) + y) * h)
}

// Admit implements [mainclient.Throughput]: the class's hourly volume, then its first bytes in a slice that begins within the
// queue's time since the miss asked for its slot.
func (p *Pacer) Admit(ctx context.Context, class mainclient.Class, since time.Time) (mainclient.Admission, error) {
	adm, err := p.admit(ctx, ClassOf(class), since)
	if err != nil {
		return nil, err
	}
	return adm, nil
}

func (p *Pacer) admit(ctx context.Context, c Class, since time.Time) (*admission, error) {
	a := p.o.Figures()
	if limit := hourOf(a.Throughput, c); limit > 0 {
		if used, wait := p.hourUsed(ctx, c, limit); used {
			p.refused(c, metrics.ThroughputHour)
			return nil, &mainclient.ThroughputError{RetryAfter: wait, Hour: true, HourLimit: limit}
		}
	}
	pl := p.planOf(c)
	adm := &admission{p: p, ctx: ctx, class: c}
	if !pl.paced {
		return adm, nil
	}
	now := p.o.Now()
	until := p.sliceOf(since.Add(a.Slots.QueueTimeout))
	if pl.closed {
		p.refused(c, metrics.ThroughputBusy)
		return nil, &mainclient.ThroughputError{RetryAfter: a.Slots.QueueTimeout}
	}
	r, at, ok := p.reserve(ctx, pl, min(pl.chunk, admitChunk), max(until, p.sliceOf(now)))
	if !ok {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p.refused(c, metrics.ThroughputBusy)
		return nil, &mainclient.ThroughputError{RetryAfter: max(startOf(at).Sub(now), Slice)}
	}
	adm.first, adm.held = r, true
	return adm, nil
}

func (p *Pacer) refused(c Class, reason string) {
	if p.o.Metrics != nil {
		p.o.Metrics.ThroughputRefused.WithLabelValues(string(c), reason).Inc()
	}
}

// Running implements [mainclient.Throughput].
func (p *Pacer) Running(ctx context.Context, class mainclient.Class) mainclient.Admission {
	return &admission{p: p, ctx: ctx, class: ClassOf(class)}
}

// Reader paces a body that runs already: a handover's direct read, which was admitted as the miss that offered it (R-BAS-074).
func (p *Pacer) Reader(ctx context.Context, class mainclient.Class, body io.ReadCloser) mainclient.PacedBody {
	return p.Running(ctx, class).Pace(body)
}

// CatalogueReader paces a read of the public catalogue (W-35): from the total only, below the callers' misses (Q-BAS-458), and
// never refused.
func (p *Pacer) CatalogueReader(ctx context.Context, body io.ReadCloser) mainclient.PacedBody {
	return (&admission{p: p, ctx: ctx, class: Catalogue}).Pace(body)
}

// admission is an admitted transfer and its first reservation.
type admission struct {
	p     *Pacer
	ctx   context.Context
	class Class
	first reservation
	held  bool
	once  sync.Once
}

// Pace implements [mainclient.Admission].
func (a *admission) Pace(body io.ReadCloser) mainclient.PacedBody {
	r := &reader{p: a.p, ctx: a.ctx, class: a.class, body: body}
	a.once.Do(func() {
		if a.held {
			r.res, r.left = a.first, a.first.n
		}
	})
	return r
}

// Cancel implements [mainclient.Admission].
func (a *admission) Cancel() {
	a.once.Do(func() {
		if a.held {
			a.p.release(a.ctx, a.first, a.first.n)
		}
	})
}

// reader is a body read at the pace of its class's buckets.
type reader struct {
	p     *Pacer
	ctx   context.Context
	class Class
	body  io.ReadCloser

	mu sync.Mutex
	// res is the reservation being read, left what of it is not read yet; unflushed is what was read and is not counted in
	// the class's hour yet.
	res       reservation
	left      int64
	unflushed int64
	done      bool

	waiting atomic.Bool
	waited  atomic.Int64
}

// Waiting implements [mainclient.Pacing].
func (r *reader) Waiting() bool { return r.waiting.Load() }

// Waited implements [mainclient.Pacing].
func (r *reader) Waited() time.Duration { return time.Duration(r.waited.Load()) }

// Read implements io.Reader: it reads at most what the reservation holds, and reserves the next bytes when that is read.
func (r *reader) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return r.body.Read(b)
	}
	r.mu.Lock()
	left := r.left
	r.mu.Unlock()
	limit := int64(len(b))
	if left == 0 {
		next, paced, err := r.next()
		if err != nil {
			return 0, err
		}
		if paced {
			left = next
		}
	}
	if left > 0 {
		limit = min(limit, left)
	}
	n, err := r.body.Read(b[:limit])
	r.mu.Lock()
	if left > 0 {
		r.left = max(r.left-int64(n), 0)
	}
	r.unflushed += int64(n)
	r.mu.Unlock()
	if err != nil {
		r.finish()
	}
	return n, err
}

// next flushes what was read into the class's hour, reserves the next bytes without a bound, and waits until their slice
// begins. paced is false while the throughput is unlimited: the read is not bounded then.
func (r *reader) next() (n int64, paced bool, err error) {
	r.flush()
	pl := r.p.planOf(r.class)
	if !pl.paced {
		return 0, false, nil
	}
	r.waiting.Store(true)
	defer r.waiting.Store(false)
	began := r.p.o.Now()
	res, _, ok := r.p.reserve(r.ctx, pl, pl.chunk, -1)
	if !ok {
		return 0, true, r.ctx.Err()
	}
	if wait := startOf(res.idx).Sub(r.p.o.Now()); wait > 0 {
		if err := r.p.o.Sleep(r.ctx, wait); err != nil {
			r.p.release(r.ctx, res, res.n)
			return 0, true, err
		}
	}
	r.account(r.p.o.Now().Sub(began))
	r.mu.Lock()
	r.res, r.left = res, res.n
	r.mu.Unlock()
	return res.n, true, nil
}

func (r *reader) account(d time.Duration) {
	if d <= 0 {
		return
	}
	r.waited.Add(int64(d))
	if r.p.o.Metrics != nil {
		r.p.o.Metrics.ThroughputWait.WithLabelValues(string(r.class)).Add(d.Seconds())
	}
}

// flush counts what was read into the class's hourly volume, when the class has one.
func (r *reader) flush() {
	r.mu.Lock()
	n := r.unflushed
	r.unflushed = 0
	r.mu.Unlock()
	if n <= 0 || hourOf(r.p.o.Figures().Throughput, r.class) <= 0 {
		return
	}
	idx := r.p.o.Now().UnixNano() / int64(time.Hour)
	r.p.o.Bytes.Add(context.WithoutCancel(r.ctx), hourName(r.class), idx, hourTTL, n)
}

// finish gives back what the reservation holds unread and counts the hour, once.
func (r *reader) finish() {
	r.mu.Lock()
	if r.done {
		r.mu.Unlock()
		return
	}
	r.done = true
	res, left := r.res, r.left
	r.left = 0
	r.mu.Unlock()
	r.p.release(r.ctx, res, left)
	r.flush()
}

// Close implements io.Closer.
func (r *reader) Close() error {
	err := r.body.Close()
	r.finish()
	return err
}
