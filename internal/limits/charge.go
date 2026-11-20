package limits

import (
	"context"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/qname"
)

// Charge is the subject and the budget key of one request, carried in its context from stage 7 to every place that counts
// (R-LIM-001, R-LIM-017): the counting writer, the admission of a download, step 9a of the remote engine and the reader of an
// upstream body. Bytes and refusals are added to it with atomic operations and reach Valkey and the usage store through the
// replica's buffer, so a write never waits for a round trip.
type Charge struct {
	e *Engine

	Subject    Subject
	Resolution Resolution
	// Key is the budget key (R-LIM-014).
	Key string
	// Mode is the mode the request was decided in.
	Mode Mode
	// Addressed is the repository the request named; zero for none.
	Addressed qname.Repo
	// bg marks the charge of a background download for a probe (R-LIM-037): not paced, no lease; the one Background makes
	// counts one in background_fetches, the one Resume makes on the worker none.
	bg bool

	id      uint64
	started time.Time

	// served is the repository that served the request, for the usage store (a virtual repository's member, R-LIM-006).
	served atomic.Int64

	refused  atomic.Bool
	deferred atomic.Bool
	download atomic.Bool
	// holds are the fetches that charge the request after its client may have left (a detached fill, R-LIM-006): the flush
	// keeps the charge until the last one released it.
	holds     atomic.Int32
	streaming atomic.Int32
	finished  atomic.Bool

	// The deltas the next flush takes.
	pendCached   atomic.Int64
	pendUncached atomic.Int64
	pendUpstream atomic.Int64
	pendRefused  atomic.Int64
	pendRequests atomic.Int64
	pendBg       atomic.Int64
	// upstreamCounted are the upstream requests step 9a counted in Valkey, which the usage store still has to learn.
	upstreamCounted atomic.Int64
	// prepaid is 1 on a resumed background download whose first upstream exchange step 9a counted when the probe was answered
	// (Engine.Resume, Q-LIM-54).
	prepaid atomic.Int32
	// The totals of the request, for the request log.
	totCached   atomic.Int64
	totUncached atomic.Int64

	mu sync.Mutex
	// lease is the request's parallel lease; at most one (R-LIM-007).
	lease *lease
	// ceiling is the ceiling key the uncached bytes of the request are charged to, "" for none.
	ceiling string
	// outcome is the request log's limits_outcome; events the limits the request met.
	outcome string
	limiter string
	events  []event
	// pacedWould marks that a would-limit of a pacing figure was recorded once for this request.
	pacedWould [2]bool
	// refusal is the RateLimit policy of the figure that refused the request in enforce, beside the request figures' (R-LIM-020).
	refusal *Policy
	// gate bounds the waiting downloads of the endpoint the request came through (R-LIM-024); nil lets the download wait.
	gate *WaitGate
}

// SetWaitGate names the gate of the endpoint whose request slot the request holds: a download that would wait for a parallel
// slot while the gate's share is held is refused at once (R-LIM-024, Q-LIM-60). Stage 7 sets it.
func (c *Charge) SetWaitGate(g *WaitGate) {
	if c != nil {
		c.mu.Lock()
		c.gate = g
		c.mu.Unlock()
	}
}

type event struct {
	figure string
	mode   Mode
}

// lease is a parallel lease of a budget key: member <replica>:<download ID>, held until the request ends (R-LIM-024).
type lease struct {
	key     string
	member  string
	taken   time.Time
	renewed time.Time
}

type chargeKey struct{}

// WithCharge stores a charge in a context.
func WithCharge(ctx context.Context, c *Charge) context.Context {
	return context.WithValue(ctx, chargeKey{}, c)
}

// FromContext returns the charge of a request, nil when the request is not charged (not a protocol path, a replica without
// the limits).
func FromContext(ctx context.Context) *Charge {
	c, _ := ctx.Value(chargeKey{}).(*Charge)
	return c
}

// Figures are the effective figures of the budget key.
func (c *Charge) Figures() Figures {
	if c == nil {
		return Figures{}
	}
	return c.Resolution.Figures
}

// Enforcing reports whether the decisions of the request apply.
func (c *Charge) Enforcing() bool { return c != nil && c.Mode == ModeEnforce }

// MarkRefused marks the request as refused (R-LIM-008): the decision point refused it, its credential failed, or its
// repository does not exist. The counting reads the mark, never the status code.
func (c *Charge) MarkRefused() {
	if c != nil && !c.refused.Swap(true) {
		c.pendRefused.Add(1)
	}
}

// Refused reports the mark.
func (c *Charge) Refused() bool { return c != nil && c.refused.Load() }

// Served records the repository that served the request (its ID), for the usage store.
func (c *Charge) Served(repoID int64) {
	if c != nil && repoID > 0 {
		c.served.Store(repoID)
	}
}

// ServedRepo is the repository that served the request, 0 when none is known.
func (c *Charge) ServedRepo() int64 {
	if c == nil {
		return 0
	}
	return c.served.Load()
}

// AddCached counts n bytes Quader wrote to the client from what it held (R-LIM-006).
func (c *Charge) AddCached(n int) {
	if c == nil || n <= 0 {
		return
	}
	c.pendCached.Add(int64(n))
	c.totCached.Add(int64(n))
}

// AddUncached counts n bytes Quader read from an upstream in an exchange the request leads (R-LIM-006).
func (c *Charge) AddUncached(n int) {
	if c == nil || n <= 0 {
		return
	}
	c.pendUncached.Add(int64(n))
	c.totUncached.Add(int64(n))
}

// AddUpstream counts upstream requests beyond the one step 9a counted for the fetch (a revalidation followed by a GET):
// they reach Valkey at the next flush.
func (c *Charge) AddUpstream(n int) {
	if c != nil && n > 0 {
		c.pendUpstream.Add(int64(n))
	}
}

// BeginStream marks that the leader's stream writes to the client: those writes are uncached bytes counted where they were
// read, and the counting writer neither counts nor paces them (R-LIM-006). EndStream ends the mark.
func (c *Charge) BeginStream() {
	if c != nil {
		c.streaming.Add(1)
	}
}

// EndStream ends a BeginStream.
func (c *Charge) EndStream() {
	if c != nil {
		c.streaming.Add(-1)
	}
}

// Streaming reports whether the leader's stream writes now.
func (c *Charge) Streaming() bool { return c != nil && c.streaming.Load() > 0 }

// Download reports whether the request is a download that was admitted (R-LIM-007): its body is paced.
func (c *Charge) Download() bool { return c != nil && c.download.Load() }

// Totals are the cached and uncached bytes of the request so far.
func (c *Charge) Totals() (cached, uncached int64) {
	if c == nil {
		return 0, 0
	}
	return c.totCached.Load(), c.totUncached.Load()
}

// Outcome is the request log's limits_outcome: admitted, limited:<figure>, would_limit:<figure>, paced, blocked.
func (c *Charge) Outcome() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.outcome == "" {
		return "admitted"
	}
	return c.outcome
}

// Limiter is the request log's limiter of a refusal of the limits (limits.<figure>), "" when the limits refused nothing.
func (c *Charge) Limiter() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.limiter
}

// CounterMode is the request record's limit_mode of a refusal of the download limits (R-OPS-010): cluster while the replica
// counts in Valkey, fallback while it counts its share alone (R-LIM-019).
func (c *Charge) CounterMode() string {
	if c != nil && c.e != nil && c.e.fallback.Load() {
		return "fallback"
	}
	return "cluster"
}

// RefusalPolicy is the RateLimit policy of the figure that refused the request (R-LIM-020, Q-LIM-23): set in enforce by every
// refusal but the request figures', whose policies stage 7 sends on every answer.
func (c *Charge) RefusalPolicy() (Policy, bool) {
	if c == nil {
		return Policy{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.refusal == nil {
		return Policy{}, false
	}
	return *c.refusal, true
}

// note records a decision that limited or would have limited the request, once per figure.
func (c *Charge) note(figure string, limited bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	mode := ModeAudit
	prefix := "would_limit:"
	if limited {
		mode, prefix = ModeEnforce, "limited:"
	}
	for _, ev := range c.events {
		if ev.figure == figure && ev.mode == mode {
			return
		}
	}
	c.events = append(c.events, event{figure: figure, mode: mode})
	if c.outcome == "" || limited {
		c.outcome = prefix + figure
	}
	if limited {
		c.limiter = "limits." + figure
	}
}

// DeferRequestFigures marks a request whose subject its handler decides (the registry's token service, R-LIM-003): stage 7
// checks its block and class but counts its request windows only when the handler calls Retarget.
func (c *Charge) DeferRequestFigures() {
	if c != nil {
		c.deferred.Store(true)
	}
}

// Retarget charges the request figures of a token request to the subject the registry's token service authenticated
// (R-LIM-003): the subject of the presented Quader token when it was valid, the address when there was none or it failed
// (an anonymous caller). The usage store records the request under that subject. It counts once.
func (c *Charge) Retarget(ctx context.Context, caller Caller) {
	if c == nil || c.e == nil {
		return
	}
	s, _ := SubjectOf(caller)
	if !c.deferred.Swap(false) && s.String() == c.Subject.String() {
		return
	}
	c.e.retarget(ctx, c, s)
}

// Background returns the charge a background download started for this request's probe carries (R-LIM-037), for the
// repository repo that fetches: the probe's subject, place and budget key; it takes no lease and is not paced. The seam W-25's
// trigger calls in the handler of the probe.
func (c *Charge) Background(repoID int64) *Charge {
	if c == nil {
		return nil
	}
	bg := &Charge{
		e: c.e, Subject: c.Subject, Resolution: c.Resolution, Key: c.Key, Mode: c.Mode, Addressed: c.Addressed,
		bg: true, id: c.e.nextID(), started: c.e.now(),
	}
	bg.served.Store(repoID)
	bg.pendBg.Store(1)
	c.e.track(bg)
	return bg
}

// Admit is [Engine.Admit] for the charge: the check before a download starts. A nil charge admits.
func (c *Charge) Admit(ctx context.Context) error {
	if c == nil || c.e == nil {
		return nil
	}
	return c.e.Admit(ctx, c)
}

// MayFetch is [Engine.MayFetch] for the charge: step 9a of R-REPO-025. A nil charge passes.
func (c *Charge) MayFetch(ctx context.Context, repoID int64, artifactGet bool) error {
	if c == nil || c.e == nil {
		return nil
	}
	return c.e.MayFetch(ctx, c, repoID, artifactGet)
}

// Reader is [Engine.Reader] for the charge: the counted and paced upstream body. A nil charge counts nothing.
func (c *Charge) Reader(ctx context.Context, r io.Reader) *CountingReader {
	if c == nil || c.e == nil {
		return &CountingReader{ctx: ctx, r: r}
	}
	return c.e.Reader(ctx, c, r)
}

// Hold keeps the charge in the buffer for a fetch that may outlive the request; Release ends it.
func (c *Charge) Hold() {
	if c != nil {
		c.holds.Add(1)
	}
}

// Release ends a Hold.
func (c *Charge) Release() {
	if c != nil {
		c.holds.Add(-1)
	}
}

// Done ends a background charge: its deltas are flushed and it is dropped from the buffer.
func (c *Charge) Done() {
	if c != nil && c.e != nil && (c.bg || c.Subject.Kind == KindSystem) {
		c.finished.Store(true)
	}
}

// memberOf is the lease member of a download: <replica ID>:<download ID>.
func (e *Engine) memberOf(c *Charge) string {
	return e.o.Replica + ":" + strconv.FormatUint(c.id, 36)
}
