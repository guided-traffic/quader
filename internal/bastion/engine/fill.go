package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/cache"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// onArtifact handles a 200 or 304 of an artifact (R-BAS-022): a 304 serves the cached bytes and renews the
// entry and the caller's URL answer; a 200 is the new content, kept and served. A 304 to the validator of a fill that
// is still running serves the caller from its spool (R-BAS-054).
func (e *engine) onArtifact(q *rq, resp *mainclient.Response, sent time.Time, bl *baseline) {
	switch resp.Status {
	case http.StatusNotModified:
		_ = resp.Body.Close() // the slot is free while the cached bytes are served
		e.putURL(q, sent, true, nil)
		if bl != nil && bl.run != nil {
			q.finish(metrics.ClassArtifact, metrics.ResultChecked)
			e.serveFill(q, bl.run, metrics.SourceCache, true)
			return
		}
		if bl != nil && bl.ent != nil {
			ent := *bl.ent
			ent.ValidatedAt = sent
			ent.Counted = ent.Counted || resp.Marking.Counted
			ent.Package = entryPackage(q, resp.Marking) // replaced by every 304, cleared by one without a package (R-BAS-084)
			_ = e.saveEntry(q.ctx(), q.cacheKey, ent)
			if e.serveEntry(q, ent, metrics.ResultChecked) {
				return
			}
			// The bytes are gone: fetch them.
			e.forward(q, mainclient.KindForward, "", nil)
			return
		}
		e.passThrough(q, resp, nil, metrics.ClassArtifact, metrics.ResultMiss)
	case http.StatusOK:
		e.putURL(q, sent, true, nil)
		e.onArtifactBody(q, resp, sent)
	default:
		e.passThrough(q, resp, nil, metrics.ClassArtifact, metrics.ResultMiss)
	}
}

// onArtifactBody keeps the bytes of an artifact when it can be verified and is within the limits, and passes
// everything else through (R-BAS-021): a HEAD, an answer without Content-Length or above max_object_bytes, an
// ETag that is no content address.
func (e *engine) onArtifactBody(q *rq, resp *mainclient.Response, sent time.Time) {
	size := contentLength(resp.Header)
	sha := contentAddress(resp.Header.Get("Etag"))
	_, keepVary := varyNames(resp.Header)
	if q.r.Method != http.MethodGet || size < 0 || size > int64(e.cfg.Cache.MaxObjectBytes) || sha == "" || !keepVary {
		e.passThrough(q, resp, nil, metrics.ClassArtifact, metrics.ResultMiss)
		return
	}
	e.fillSpool(q, resp, nil, sha, size, sent, cache.EntryArtifact)
}

// fillState is where a fill stands.
type fillState int

const (
	fillRunning fillState = iota
	// fillOK: the whole body arrived, its SHA-256 equals the content address and the commit was tried. The
	// withheld tail may be released.
	fillOK
	// fillFailed: the bytes cannot be trusted or could not be spooled; nothing is kept.
	fillFailed
)

// fillRun is the fill of an artifact, or of a shared document above the memory bound (R-BAS-021, Q-BAS-123,
// Q-BAS-173). It reads the main installation's body into the spool at the speed of the main installation, whatever
// any client does, and releases the budget slot when the body ends. The client of the request that started it and
// every caller of the replica that follows it (R-BAS-054, Q-BAS-172) are served from the spool at their own speed
// ([engine.serveFill]): the bytes each may have are the ones written except for the withheld tail of what it asked
// for, which is released only after the digest equals the content address and the entry is written. The flight of
// the collapsed fetch ends at that point, so a waiter that does not follow never waits for a client.
type fillRun struct {
	e    *engine
	fill *cache.Fill
	kind cache.EntryKind
	sha  string
	size int64
	// key and ent are the cache key and the entry written after a successful commit; key is empty when the
	// response must not be kept (it varies by something the Bastion cannot key on).
	key string
	ent cache.Entry
	// hdr, etag and lastModified are what a caller served from the spool receives and is answered by: the headers the
	// main installation's answer replays (no framing), its validator and its date. counted is its marking's
	// Quader-Bastion-Counted (R-BAS-059).
	hdr          http.Header
	etag         string
	lastModified string
	counted      bool
	// repo and pkg are the repository and the package of the leader's marking (R-BAS-084): a follower whose package answer
	// on pkg is full follows the fill like a full reader (R-BAS-085, Q-BAS-200).
	repo, pkg string
	// direct is the direct read of a handover that is the fill's source (R-BAS-074), nil for a fill from the main
	// installation. Everything else of the fill is the same for both sources.
	direct *directFill
	// class is the budget class of the caller whose request started the fill, the class of its bytes in
	// quader_bastion_fill_bytes_total (R-BAS-090, Q-BAS-307).
	class string
	// pacing is the throughput's pacing of the source the fill reads now (R-BAS-097): while it waits for the cap, the stall
	// bound does not end the fill.
	pacing pacingRef

	mu      sync.Mutex
	written int64
	state   fillState
	notify  chan struct{}
	refs    int
}

// snapshot returns what a follower needs to decide, and the channel that is closed at the next change.
func (r *fillRun) snapshot() (written int64, st fillState, ch <-chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.written, r.state, r.notify
}

func (r *fillRun) wake() {
	close(r.notify)
	r.notify = make(chan struct{})
}

func (r *fillRun) advance(n int64) {
	r.mu.Lock()
	r.written = n
	r.wake()
	r.mu.Unlock()
}

func (r *fillRun) finishWith(st fillState) {
	r.mu.Lock()
	if r.state == fillRunning {
		r.state = st
		r.wake()
	}
	r.mu.Unlock()
}

// acquire takes a reference for a caller that follows the fill; it is false when the spool file is gone already.
func (r *fillRun) acquire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refs == 0 {
		return false
	}
	r.refs++
	return true
}

// release gives up a reference; the last one removes the spool file.
func (r *fillRun) release() {
	r.mu.Lock()
	r.refs--
	last := r.refs == 0
	r.mu.Unlock()
	if last {
		r.fill.Abort()
	}
}

// run reads the body to its end, verifies and commits. It runs once, in a goroutine of the engine (in the
// request's own while the engine drains). end cancels the request to the main installation (and the direct read of a
// handover); flightDone ends the flight of the collapsed fetch. A direct read that breaks off after its first byte is
// continued from the main installation into the same spool (R-BAS-075); the commit's check of the SHA-256 decides for
// every source. A fill that fails ends its flight before its callers learn it: a request that comes after the failure
// joins no flight, is a miss of its own and is never cut by a fill that no longer runs (R-BAS-054, R-BAS-075), while the
// fill still closes its source. A fill that succeeds ends its flight after its entry is written and before its callers
// receive the withheld tail, the point the flight of a fill ends at (Q-BAS-123).
func (r *fillRun) run(body io.ReadCloser, end, flightDone func()) {
	e := r.e
	defer r.release()
	defer flightDone()
	fail := func() {
		flightDone()
		r.finishWith(fillFailed)
	}
	defer end()
	var stallTimer atomic.Pointer[time.Timer]
	stallTimer.Store(time.AfterFunc(e.fillStall, func() {
		if r.pacing.waiting() {
			// The source waits for the throughput's cap, not for its own bytes: a fill the cap slows is never cut (R-BAS-097).
			if t := stallTimer.Load(); t != nil {
				t.Reset(e.fillStall)
			}
			return
		}
		r.stall(end)
	}))
	stall := stallTimer.Load()
	defer stall.Stop()
	defer func() { _ = body.Close() }() // whichever body is current; a direct read's Close destroys its key

	buf := make([]byte, 64<<10)
	var total int64
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			stall.Reset(e.fillStall)
			total += int64(n)
			if total > r.size {
				e.log.Warn("the source of a fill sent more bytes than it announced", "sha256", r.sha, "want", r.size)
				fail()
				return
			}
			if _, werr := r.fill.Write(buf[:n]); werr != nil {
				// The spool cannot take the bytes, so the transfer cannot be verified: it is cut, never
				// passed on unverified.
				e.log.Warn("the spool of a fill could not be written", "error", werr)
				fail()
				return
			}
			e.d.Metrics.FillBytes.WithLabelValues(r.fillSourceLabel(), r.class).Add(float64(n))
			r.advance(total)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			next, ok := r.turnToMain(rerr, total)
			if !ok {
				fail()
				return
			}
			body = next
			stall.Reset(e.fillStall)
		}
	}
	_ = body.Close() // the response is complete: the budget slot is free (a direct read's key destroyed) while the commit and the clients go on
	if total != r.size {
		e.log.Warn("the source of a fill sent another number of bytes than it announced", "sha256", r.sha, "got", total, "want", r.size)
		fail()
		return
	}
	ctx := context.Background()
	err := r.fill.Commit(ctx, r.sha)
	r.committed(!errors.Is(err, cache.ErrMismatch))
	if errors.Is(err, cache.ErrMismatch) {
		e.log.Warn("the bytes of a fill do not match their content address", "sha256", r.sha)
		fail()
		return
	}
	if err == nil && r.key != "" {
		// A failure of the backend is counted by the cache and the fill is still served: the transfer is not
		// a failed request (R-BAS-030).
		_ = e.saveEntry(ctx, r.key, r.ent)
	}
	// The flight ends before the callers receive their withheld tail: a request that comes after a caller's complete answer
	// is decided on the entry, not on the spool of a fill that is over.
	flightDone()
	r.finishWith(fillOK)
}

// fillSpool spools an artifact, or a shared document above the memory bound, while it streams to the caller, with the
// withheld tail of R-REPO-041 (R-BAS-021, R-BAS-054, Q-BAS-173): the last 64 KiB of what the caller asked for reach it
// only after the SHA-256 equals the content address of the ETag and the fill is committed and its entry written. On a
// mismatch the transfer is aborted and nothing is kept. The caller's conditionals and Range are answered at once from
// the response's validator, date and length, and the range streams from the spool as it grows (Q-BAS-172). pre holds
// bytes of the body already read. The spooling runs without the client ([fillRun]) and the waiters of the flight
// follow it; the verification, the commit under the content address and the signed entry are the cache's (package
// 5); what is the engine's is the tail towards each client.
func (e *engine) fillSpool(q *rq, resp *mainclient.Response, pre []byte, sha string, size int64, sent time.Time, kind cache.EntryKind) {
	class := metrics.ClassArtifact
	if kind == cache.EntryDocument {
		class = metrics.ClassShared
	}
	fill, err := e.d.Cache.NewFill(kind, size)
	if err != nil {
		if kind == cache.EntryDocument {
			e.markVolatile(q.cacheKey)
		}
		e.passThroughRest(q, resp, pre, resp.Body, class)
		return
	}
	r := e.newFillRun(q, resp, fill, sha, size, sent, kind)
	r.pacing.set(resp.Pacing)
	// The fill takes over the response, the request to the main installation and the flight.
	body := resp.Body
	if len(pre) > 0 {
		body = readCloser{Reader: io.MultiReader(bytes.NewReader(pre), resp.Body), Closer: resp.Body}
	}
	resp.Body = http.NoBody
	end := func() {}
	if q.hold != nil {
		end, _ = q.hold.take()
	}
	e.runFill(q, r, body, end)
}

// newFillRun prepares the fill of an answer: the headers a caller served from the spool receives, its validator and date,
// the marking's counting, repository and package, and the entry written after the commit, for a fill from the main
// installation and for one from a direct read alike (R-BAS-074).
func (e *engine) newFillRun(q *rq, resp *mainclient.Response, fill *cache.Fill, sha string, size int64, sent time.Time, kind cache.EntryKind) *fillRun {
	fill.Keep() // the committed bytes still answer the clients, and a refused commit does not fail the request
	hdr := replayHeader(resp.Header, q.facts.ReplayHeaders)
	hdr.Del("Content-Length")
	r := &fillRun{
		e: e, fill: fill, kind: kind, sha: sha, size: size, notify: make(chan struct{}), refs: 2,
		hdr: hdr, etag: resp.Header.Get("Etag"), lastModified: resp.Header.Get("Last-Modified"), counted: resp.Marking.Counted,
		repo: q.repo, pkg: entryPackage(q, resp.Marking), class: string(e.budgetClass(q)),
	}
	if key, keep := e.learnVary(q, resp.Header); keep {
		r.key, r.ent = key, e.spoolEntry(q, resp, sha, size, sent, kind)
	}
	return r
}

// runFill starts the fill's reading, which takes over the flight and ends the request to its source with end, hands the
// fill to the waiters of the flight (R-BAS-054), and serves the request's caller from the spool.
func (e *engine) runFill(q *rq, r *fillRun, body io.ReadCloser, end func()) {
	class := metrics.ClassArtifact
	if r.kind == cache.EntryDocument {
		class = metrics.ClassShared
	}
	flightDone := q.flight.take()
	if q.call != nil && r.key == q.cacheKey {
		q.call.startFill(r)
	}
	defer r.release()
	if !e.background(func() { r.run(body, end, flightDone) }) {
		r.run(body, end, flightDone) // the engine drains: the fill runs in the request, which releases its reference
	}
	q.finish(class, metrics.ResultMiss)
	e.serveFill(q, r, metrics.SourceMain, false)
}

// pacingRef holds the pacing of a fill's current source, which the continuation of a direct read replaces (R-BAS-075) while the
// stall bound's timer reads it.
type pacingRef struct {
	mu sync.Mutex
	p  mainclient.Pacing
}

func (r *pacingRef) set(p mainclient.Pacing) {
	r.mu.Lock()
	r.p = p
	r.mu.Unlock()
}

// waiting reports whether the source waits for the throughput's cap now.
func (r *pacingRef) waiting() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.p != nil && r.p.Waiting()
}

// spoolEntry is the signed entry of a committed fill (R-BAS-021, R-BAS-023): the headers a cached answer replays,
// the repository, the validation time and whether the main installation counted the serving as a download.
func (e *engine) spoolEntry(q *rq, resp *mainclient.Response, sha string, size int64, sent time.Time, kind cache.EntryKind) cache.Entry {
	h := keepHeader(resp.Header, q.facts.ReplayHeaders)
	if h.Get("Etag") == "" {
		h.Set("Etag", `"`+sha+`"`)
	}
	repo := resp.Marking.Repository
	if repo == "" {
		repo = q.repo
	}
	return cache.Entry{
		Kind: kind, SHA256: sha, Size: size, Header: h, Repository: repo, Status: http.StatusOK, ValidatedAt: sent,
		Counted: resp.Marking.Counted, Package: entryPackage(q, resp.Marking),
	}
}

// serveFill answers a caller from a running or committed fill (R-BAS-054): the conditionals and Range of the caller
// decided at once from the answer's validator, date and length, then the bytes of the range from the spool as it
// grows. fromCache is set for a caller that follows the fill of another request: its bytes are served from the
// Bastion, and a serve of a counted answer is reported as a download (R-BAS-059).
func (e *engine) serveFill(q *rq, r *fillRun, label string, fromCache bool) {
	hdr := r.hdr.Clone()
	hdr.Set("Accept-Ranges", "bytes")
	p := planFor(q.r, r.etag, r.lastModified, r.size)
	switch {
	case p.notModified:
		e.notModified(q, hdr)
		return
	case p.unsat:
		e.rangeNotSatisfiable(q, r.size)
		return
	}
	status, length := http.StatusOK, r.size
	if p.partial {
		status, length = http.StatusPartialContent, p.end-p.start+1
		hdr.Set("Content-Range", "bytes "+strconv.FormatInt(p.start, 10)+"-"+strconv.FormatInt(p.end, 10)+"/"+strconv.FormatInt(r.size, 10))
	}
	hdr.Set("Content-Length", strconv.FormatInt(length, 10))
	// A download is checked before its first byte, a hit's against the cached bytes, the fill's own caller's against the uncached
	// (R-BAS-096); a refused caller leaves the fill running into the cache.
	if q.r.Method != http.MethodHead && (r.kind == cache.EntryArtifact || q.fam == familyFiles) && !e.admitTransfer(q, fromCache) {
		return
	}
	copyHeader(q.w.Header(), hdr)
	q.w.WriteHeader(status)
	if q.r.Method == http.MethodHead {
		return
	}
	q.out.source = label
	if e.streamFill(q, r, p.start, p.end) && fromCache && r.counted {
		e.downloadEvent(q, r.ent.Repository, r.sha)
	}
}

// streamFill copies the bytes [start, end] of a fill to the caller as they are written. While the fill runs the last
// withheldTail bytes of the range stay back, so a range shorter than the tail is served whole after the commit; a
// fill that failed cuts the transfer, so the caller sees an incomplete response, never a complete one with wrong
// bytes. The caller's speed decides nothing but its own pace. It reports whether the whole range was delivered.
func (e *engine) streamFill(q *rq, r *fillRun, start, end int64) bool {
	buf := make([]byte, 32<<10)
	off, stop := start, end+1
	for {
		written, st, ch := r.snapshot()
		if st == fillFailed {
			panic(http.ErrAbortHandler)
		}
		limit := min(written, stop)
		if st == fillRunning {
			limit = min(limit, stop-withheldTail)
		}
		for off < limit {
			n, err := r.fill.Reader().ReadAt(buf[:min(int64(len(buf)), limit-off)], off)
			if n > 0 {
				if _, werr := q.out.Write(buf[:n]); werr != nil {
					return false
				}
				off += int64(n)
			}
			if err != nil && !errors.Is(err, io.EOF) {
				panic(http.ErrAbortHandler)
			}
			if n == 0 {
				break
			}
		}
		if st == fillOK {
			if off < stop {
				panic(http.ErrAbortHandler) // the spool holds fewer bytes than the committed fill: never a complete answer
			}
			return true
		}
		select {
		case <-ch:
		case <-q.ctx().Done():
			return false
		}
	}
}

// followFill serves a caller of a cache key whose fill runs on this replica from its spool (R-BAS-054, Q-BAS-172),
// after it has its own permission: a usable full answer, a package answer full on the fill's package (the leader's
// marking named it, R-BAS-085), a URL answer, a check that says either, or for an artifact a conditional request with the
// caller's credential naming the fill's validator, answered 304; a refusal is replayed. It reports true when the fill is
// gone or the caller may not follow it (a document for a reader without a full shape or package answer), and the request
// is decided again once the flight ended.
func (e *engine) followFill(q *rq, r *fillRun) (again bool) {
	if !r.acquire() {
		return true
	}
	defer r.release()
	ctx := q.ctx()
	shape := e.answers.shape(ctx, q.cred.key, q.repo)
	if shape != nil && shape.Read == iface.ReadNone {
		e.refuseNone(q, shape)
		return false
	}
	url := e.answers.url(ctx, q.cred.key, q.cacheKey)
	if url != nil && url.Refusal != nil {
		q.finish(metrics.ClassPass, metrics.ResultHit)
		e.writeRefusal(q, url.Refusal, metrics.SourceCache)
		return false
	}
	pkg := e.packageAnswer(ctx, q, r.repo, r.pkg, shape)
	allowed := isFull(shape) || isFull(pkg) || (url != nil && url.Allowed && r.kind == cache.EntryArtifact)
	result := metrics.ResultHit
	if !allowed && shape == nil && e.d.Link.Reachable() {
		var done bool
		if shape, pkg, done = e.checkShape(q, r.repo, r.pkg); done {
			return false
		}
		if shape.Read == iface.ReadNone {
			e.refuseNone(q, shape)
			return false
		}
		allowed, result = isFull(shape) || isFull(pkg), metrics.ResultChecked
	}
	class := metrics.ClassArtifact
	if r.kind == cache.EntryDocument {
		class = metrics.ClassShared
	}
	switch {
	case allowed:
		q.finish(class, result)
		e.serveFill(q, r, metrics.SourceCache, true)
		return false
	case r.kind == cache.EntryDocument:
		return true
	}
	e.forward(q, mainclient.KindConditional, r.etag, &baseline{run: r})
	return false
}

// keepBytes stores the bytes of a shared document through a cache fill and returns their content address.
func (e *engine) keepBytes(ctx context.Context, kind cache.EntryKind, body []byte) (string, error) {
	fill, err := e.d.Cache.NewFill(kind, int64(len(body)))
	if err != nil {
		return "", err
	}
	defer fill.Abort()
	if _, err := fill.Write(body); err != nil {
		return "", err
	}
	sum := fill.SHA256()
	return sum, fill.Commit(ctx, sum)
}
