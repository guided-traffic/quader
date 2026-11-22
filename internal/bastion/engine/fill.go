package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
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
// request's own while the engine drains). end cancels the request to the main installation; flightDone ends the
// flight of the collapsed fetch.
func (r *fillRun) run(body io.ReadCloser, end, flightDone func()) {
	e := r.e
	defer r.release()
	defer flightDone()
	defer end()
	stall := time.AfterFunc(e.fillStall, end)
	defer stall.Stop()
	defer body.Close()

	buf := make([]byte, 64<<10)
	var total int64
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			stall.Reset(e.fillStall)
			total += int64(n)
			if total > r.size {
				e.log.Warn("the main installation sent more bytes than it announced", "sha256", r.sha, "want", r.size)
				r.finishWith(fillFailed)
				return
			}
			if _, werr := r.fill.Write(buf[:n]); werr != nil {
				// The spool cannot take the bytes, so the transfer cannot be verified: it is cut, never
				// passed on unverified.
				e.log.Warn("the spool of a fill could not be written", "error", werr)
				r.finishWith(fillFailed)
				return
			}
			r.advance(total)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			r.finishWith(fillFailed)
			return
		}
	}
	_ = body.Close() // the response is complete: the budget slot is free while the commit and the clients go on
	if total != r.size {
		e.log.Warn("the main installation sent another number of bytes than it announced", "sha256", r.sha, "got", total, "want", r.size)
		r.finishWith(fillFailed)
		return
	}
	ctx := context.Background()
	err := r.fill.Commit(ctx, r.sha)
	if errors.Is(err, cache.ErrMismatch) {
		e.log.Warn("the main installation sent bytes that do not match their content address", "sha256", r.sha)
		r.finishWith(fillFailed)
		return
	}
	if err == nil && r.key != "" {
		// A failure of the backend is counted by the cache and the fill is still served: the transfer is not
		// a failed request (R-BAS-030).
		_ = e.saveEntry(ctx, r.key, r.ent)
	}
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
	fill, err := e.d.Cache.NewFill(kind)
	if err != nil {
		if kind == cache.EntryDocument {
			e.markVolatile(q.cacheKey)
		}
		e.passThroughRest(q, resp, pre, resp.Body, class)
		return
	}
	fill.Keep() // the committed bytes still answer the clients, and a refused commit does not fail the request
	hdr := replayHeader(resp.Header)
	hdr.Del("Content-Length")
	r := &fillRun{
		e: e, fill: fill, kind: kind, sha: sha, size: size, notify: make(chan struct{}), refs: 2,
		hdr: hdr, etag: resp.Header.Get("Etag"), lastModified: resp.Header.Get("Last-Modified"), counted: resp.Marking.Counted,
	}
	if key, keep := e.learnVary(q, resp.Header); keep {
		r.key, r.ent = key, e.spoolEntry(q, resp, sha, size, sent, kind)
	}
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
		Counted: resp.Marking.Counted,
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
// after it has its own permission: a usable full answer or URL answer, a check that says full, or for an artifact a
// conditional request with the caller's credential naming the fill's validator, answered 304; a refusal is replayed.
// It reports true when the fill is gone or the caller may not follow it (a document for a reader without a full
// shape), and the request is decided again once the flight ended.
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
	allowed := isFull(shape) || (url != nil && url.Allowed && r.kind == cache.EntryArtifact)
	result := metrics.ResultHit
	if !allowed && shape == nil && e.d.Link.Reachable() {
		var done bool
		if shape, done = e.checkShape(q); done {
			return false
		}
		if shape.Read == iface.ReadNone {
			e.refuseNone(q, shape)
			return false
		}
		allowed, result = isFull(shape), metrics.ResultChecked
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
	fill, err := e.d.Cache.NewFill(kind)
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
