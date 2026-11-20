// Package usage is the usage store of the download limits (usage-and-limits.md R-LIM-027 to R-LIM-029, R-LIM-034): the
// per-replica buffer that the charge buffer of internal/limits feeds every second and that is written to PostgreSQL every
// limits.usage.flush_interval, the reads of the overview, and the daily task limits-cleanup. It is a write-only buffer
// (R-ARCH-032), as internal/items/stats.go is: no request reads it.
package usage

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// OverflowRef is the subject of the overflow rows a full buffer merges new keys into (R-LIM-028).
const OverflowRef = "overflow"

// key is a row of the store without its granularity: the five-minute bucket, the subject, the credential, the place, the
// serving repository and the virtual repository of another project it was reached through (Q-LIM-63).
type key struct {
	start      int64 // Unix seconds of the five-minute bucket
	kind       limits.SubjectKind
	ref        string
	credential string
	place      string
	repo       int64
	via        int64
}

type counts struct {
	requests, refused, cached, uncached, upstream, background int64
	limits                                                    limits.Figures
}

type eventKey struct {
	start  int64
	kind   limits.SubjectKind
	ref    string
	place  string
	repo   int64
	figure string
	mode   limits.Mode
}

// Options configure [NewBuffer].
type Options struct {
	Runner   meta.Runner
	Interval time.Duration
	// MaxKeys is limits.usage.max_buffered_keys (100,000).
	MaxKeys  int
	Log      *slog.Logger
	Registry prometheus.Registerer
}

// Buffer sums the consumption of the replica per row and writes it every interval in one transaction: one upsert for the
// rows and one for the limit events, in key order (R-LIM-028). A failed flush keeps its batch merged with what arrived
// meanwhile; at MaxKeys a new key is merged into an overflow row per repository and subject kind, so a flood of distinct
// addresses cannot grow the replica's memory without bound.
type Buffer struct {
	o Options

	mu     sync.Mutex
	rows   map[key]*counts
	events map[eventKey]int64

	keys     prometheus.Gauge
	failures prometheus.Counter
	overflow prometheus.Counter

	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// NewBuffer creates the buffer and registers quader_limits_usage_buffer_keys, quader_limits_usage_flush_failures_total and
// quader_limits_usage_overflow_total.
func NewBuffer(o Options) *Buffer {
	if o.Interval <= 0 {
		o.Interval = 10 * time.Second
	}
	if o.MaxKeys <= 0 {
		o.MaxKeys = 100_000
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	b := &Buffer{
		o: o, rows: map[key]*counts{}, events: map[eventKey]int64{}, stop: make(chan struct{}), done: make(chan struct{}),
		keys: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "quader_limits_usage_buffer_keys",
			Help: "Rows the replica's usage buffer holds for its next flush.",
		}),
		failures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "quader_limits_usage_flush_failures_total",
			Help: "Flushes of the usage buffer that failed and were kept for the next interval.",
		}),
		overflow: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "quader_limits_usage_overflow_total",
			Help: "Charges merged into an overflow row because the usage buffer was full.",
		}),
	}
	if o.Registry != nil {
		for _, c := range []prometheus.Collector{b.keys, b.failures, b.overflow} {
			_ = o.Registry.Register(c)
		}
	}
	return b
}

func bucket5m(t time.Time) int64 { return t.Unix() - t.Unix()%300 }

// Add implements limits.UsageSink.
func (b *Buffer) Add(d limits.UsageDelta) {
	if b == nil {
		return
	}
	start := bucket5m(d.At)
	k := key{start: start, kind: d.Subject.Kind, ref: d.Subject.Ref, credential: d.Subject.Credential, place: d.Place.Key(), repo: d.RepoID, via: d.ViaRepoID}
	if d.Subject.Kind != limits.KindUser {
		k.credential = ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	c := b.rows[k]
	if c == nil {
		if len(b.rows) >= b.o.MaxKeys {
			b.overflow.Inc()
			k.ref, k.credential = OverflowRef, ""
			c = b.rows[k]
		}
		if c == nil {
			c = &counts{}
			b.rows[k] = c
		}
	}
	c.requests += d.Requests
	c.refused += d.Refused
	c.cached += d.Cached
	c.uncached += d.Uncached
	c.upstream += d.Upstream
	c.background += d.Background
	c.limits = d.Limits
	for _, ev := range d.Events {
		ek := eventKey{start: start, kind: k.kind, ref: k.ref, place: k.place, repo: k.repo, figure: ev.Figure, mode: ev.Mode}
		if _, ok := b.events[ek]; !ok && len(b.events) >= b.o.MaxKeys {
			b.overflow.Inc()
			ek.ref = OverflowRef
		}
		b.events[ek] += ev.Count
	}
	b.keys.Set(float64(len(b.rows)))
}

// Start runs the flush loop until Stop or ctx ends.
func (b *Buffer) Start(ctx context.Context) {
	go func() {
		defer close(b.done)
		t := time.NewTicker(b.o.Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-b.stop:
				return
			case <-t.C:
				if err := b.Flush(context.WithoutCancel(ctx)); err != nil {
					b.o.Log.Warn("the usage of the download limits was not written; it is kept for the next interval", "error", err)
				}
			}
		}
	}()
}

// Stop ends the loop and flushes what is left (R-ARCH-024: a drain loses nothing).
func (b *Buffer) Stop(ctx context.Context) {
	b.once.Do(func() { close(b.stop) })
	select {
	case <-b.done:
	case <-time.After(time.Second):
	}
	if err := b.Flush(ctx); err != nil {
		b.o.Log.Warn("the usage of the download limits was not written at shutdown", "error", err)
	}
}

// row is one row of the upsert, in the JSON form of UpsertUsageBuckets.
type row struct {
	G    string `json:"g"`
	S    string `json:"s"`
	K    string `json:"k"`
	R    string `json:"r"`
	C    string `json:"c"`
	P    string `json:"p"`
	Repo int64  `json:"repo"`
	Via  int64  `json:"via"`
	RQ   int64  `json:"rq"`
	RF   int64  `json:"rf"`
	CB   int64  `json:"cb"`
	UB   int64  `json:"ub"`
	UP   int64  `json:"up"`
	BG   int64  `json:"bg"`
	LRH  *int64 `json:"lrh"`
	LCH  *int64 `json:"lch"`
	LUH  *int64 `json:"luh"`
	LUP  *int64 `json:"lup"`
	LBD  *int64 `json:"lbd"`
}

type eventRow struct {
	G    string `json:"g"`
	S    string `json:"s"`
	K    string `json:"k"`
	R    string `json:"r"`
	P    string `json:"p"`
	Repo int64  `json:"repo"`
	F    string `json:"f"`
	M    string `json:"m"`
	N    int64  `json:"n"`
}

func limitOf(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}

func stamp(unix int64) string { return time.Unix(unix, 0).UTC().Format(time.RFC3339) }

// Flush writes the buffer: the five-minute rows, the hourly rows they fold into, and the limit events of both, in one
// transaction. A batch that fails stays in the buffer.
func (b *Buffer) Flush(ctx context.Context) error {
	b.mu.Lock()
	rows, events := b.rows, b.events
	b.rows, b.events = map[key]*counts{}, map[eventKey]int64{}
	b.keys.Set(0)
	b.mu.Unlock()
	if len(rows) == 0 && len(events) == 0 {
		return nil
	}
	out := make([]row, 0, 2*len(rows))
	hourly := map[key]*counts{}
	for k, c := range rows {
		out = append(out, rowOf("5m", k, c))
		hk := k
		hk.start = k.start - k.start%3600
		h := hourly[hk]
		if h == nil {
			h = &counts{}
			hourly[hk] = h
		}
		h.requests += c.requests
		h.refused += c.refused
		h.cached += c.cached
		h.uncached += c.uncached
		h.upstream += c.upstream
		h.background += c.background
		h.limits = c.limits
	}
	for k, c := range hourly {
		out = append(out, rowOf("1h", k, c))
	}
	sortRows(out)
	evs := make([]eventRow, 0, 2*len(events))
	hourlyEv := map[eventKey]int64{}
	for k, n := range events {
		evs = append(evs, eventRowOf("5m", k, n))
		hk := k
		hk.start = k.start - k.start%3600
		hourlyEv[hk] += n
	}
	for k, n := range hourlyEv {
		evs = append(evs, eventRowOf("1h", k, n))
	}
	sort.Slice(evs, func(i, j int) bool {
		a, c := evs[i], evs[j]
		return a.G+a.S+a.K+a.R+a.P+strconv.FormatInt(a.Repo, 10)+a.F+a.M < c.G+c.S+c.K+c.R+c.P+strconv.FormatInt(c.Repo, 10)+c.F+c.M
	})
	err := b.write(ctx, out, evs)
	if err != nil {
		b.failures.Inc()
		b.mu.Lock()
		for k, c := range rows {
			cur := b.rows[k]
			if cur == nil {
				b.rows[k] = c
				continue
			}
			cur.requests += c.requests
			cur.refused += c.refused
			cur.cached += c.cached
			cur.uncached += c.uncached
			cur.upstream += c.upstream
			cur.background += c.background
		}
		for k, n := range events {
			b.events[k] += n
		}
		b.keys.Set(float64(len(b.rows)))
		b.mu.Unlock()
	}
	return err
}

func rowOf(g string, k key, c *counts) row {
	return row{
		G: g, S: stamp(k.start), K: string(k.kind), R: k.ref, C: k.credential, P: k.place, Repo: k.repo, Via: k.via,
		RQ: c.requests, RF: c.refused, CB: c.cached, UB: c.uncached, UP: c.upstream, BG: c.background,
		LRH: limitOf(c.limits[limits.RequestsPerHour]), LCH: limitOf(c.limits[limits.CachedBytesPerHour]),
		LUH: limitOf(c.limits[limits.UncachedBytesPerHour]), LUP: limitOf(c.limits[limits.UpstreamRequestsPerHour]),
		LBD: limitOf(c.limits[limits.BytesPerDay]),
	}
}

func eventRowOf(g string, k eventKey, n int64) eventRow {
	return eventRow{G: g, S: stamp(k.start), K: string(k.kind), R: k.ref, P: k.place, Repo: k.repo, F: k.figure, M: string(k.mode), N: n}
}

// sortRows puts the rows in the order of the store's unique key, the order every replica writes in.
func sortRows(rs []row) {
	sort.Slice(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		switch {
		case a.G != b.G:
			return a.G < b.G
		case a.S != b.S:
			return a.S < b.S
		case a.K != b.K:
			return a.K < b.K
		case a.R != b.R:
			return a.R < b.R
		case a.C != b.C:
			return a.C < b.C
		case a.P != b.P:
			return a.P < b.P
		case a.Repo != b.Repo:
			return a.Repo < b.Repo
		}
		return a.Via < b.Via
	})
}

func (b *Buffer) write(ctx context.Context, rows []row, events []eventRow) error {
	rawRows, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	rawEvents, err := json.Marshal(events)
	if err != nil {
		return err
	}
	return b.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return nil
		}
		qs := q.New(ptx)
		if len(rows) > 0 {
			if _, err := qs.UpsertUsageBuckets(ctx, string(rawRows)); err != nil {
				return err
			}
		}
		if len(events) > 0 {
			if _, err := qs.UpsertUsageEvents(ctx, string(rawEvents)); err != nil {
				return err
			}
		}
		return nil
	})
}
