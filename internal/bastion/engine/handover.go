package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/cache"
	"github.com/guided-traffic/quader/internal/bastion/handover"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// The handover per blob at the Bastion (R-BAS-072 to R-BAS-077): the offer on a forward, the fill whose source is the
// direct read of the object store, the forward that replaces a handover failing before its first byte, the continuation
// through the main installation after it, the mark of a key whose bytes failed their digest, and the cooldown. The link
// client and the readers are internal/bastion/handover's; what is kept, who follows a fill and when a caller receives its
// last bytes are the fill's, as for a fill from the main installation.

// handoverCooldownMax is how long a replica sends no offer after a failure of the store's path before the first byte
// (R-BAS-075, Q-BAS-236); a field of the engine, which a test shortens, not a value of the chart.
const handoverCooldownMax = 60 * time.Second

// handoverReading is the request log's bastion_handover of a caller whose request ended while the direct read of its fill
// still ran (Q-BAS-290).
const handoverReading = "reading"

// mainHosts are the host names a link of a handover must not name (R-BAS-074): server.base_url, bastion.main.url and
// bastion.main.packages_url.
func mainHosts(base string, urls ...string) []string {
	out := []string{base}
	for _, raw := range urls {
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			out = append(out, strings.ToLower(u.Hostname()))
		}
	}
	return out
}

// offerFor is the offer of R-BAS-072 on a forward, or the zero offer, which sends nothing: a GET forwarded on a miss or as
// the Bastion's own conditional request, while the last handshake said handover: true, the replica is not cooling down and
// the cache key is not marked; never on a probe (which does not come here), a core route of the registry root, the
// conditional request of a caller that follows a running fill, a continuation or a forward that replaces a failed handover.
// The size is the smaller of max_object_bytes and what the spool of fills can take now, so the main installation hands over
// only what the Bastion keeps (Q-BAS-233).
func (e *engine) offerFor(q *rq, kind mainclient.Kind, bl *baseline) iface.HandoverOffer {
	switch {
	case e.d.Handover == nil, q.r.Method != http.MethodGet, q.passOnly, q.noOffer,
		kind != mainclient.KindForward && kind != mainclient.KindConditional,
		bl != nil && bl.run != nil:
		return iface.HandoverOffer{}
	case !e.d.Link.State().Handover, e.coolingDown(), e.marked(q.cacheKey):
		return iface.HandoverOffer{}
	}
	return iface.HandoverOffer{MaxSize: min(int64(e.cfg.Cache.MaxObjectBytes), e.d.Cache.SpoolRoom())}
}

// coolingDown reports whether the replica sends no offer, and clears the gauge once the cooldown is over.
func (e *engine) coolingDown() bool {
	e.coolMu.Lock()
	until := e.coolUntil
	e.coolMu.Unlock()
	if until.IsZero() {
		return false
	}
	if e.now().Before(until) {
		return true
	}
	e.d.Metrics.HandoverCooldown.Set(0)
	return false
}

// startCooldown makes the replica send no offer for the cooldown (R-BAS-075): a Bastion whose network does not reach the
// store costs one more forward per replica and cooldown, not one per miss. The gauge falls back by itself at its end.
func (e *engine) startCooldown() {
	e.coolMu.Lock()
	e.coolUntil = e.now().Add(e.handoverCooldown)
	e.coolMu.Unlock()
	e.d.Metrics.HandoverCooldown.Set(1)
	time.AfterFunc(e.handoverCooldown, func() { _ = e.coolingDown() })
}

// markNoOffer marks a cache key whose handed bytes failed their digest for artifact_revalidate_after: its next fills send
// no offer, so the main installation's verifying read meets the copy and marks it (R-BAS-075).
func (e *engine) markNoOffer(key string) {
	e.noOffer.put(key, e.now().Add(e.cfg.Cache.ArtifactRevalidateAfter), 0)
}

// marked reports whether a cache key is marked.
func (e *engine) marked(key string) bool {
	until, ok := e.noOffer.get(key)
	return ok && e.now().Before(until)
}

// onHandover takes an answer of the class handover (R-BAS-073, R-BAS-074). Its body is empty and is closed at once, so its
// budget slot is free while the direct read runs and the read holds none. The value is checked against the moment, the
// offer, the ETag and the hosts; the fill is prepared; the direct read is opened with a context that outlives the caller,
// as a fill from the main installation does (Q-BAS-123). Every failure up to the first verified bytes ends in the forward
// without the offer (R-BAS-075). The data key never leaves this function except into the link client's read, which
// destroys it; every other path destroys it here.
func (e *engine) onHandover(q *rq, resp *mainclient.Response, sent time.Time) {
	h := resp.Handover
	resp.Handover = nil
	_ = resp.Body.Close()
	if h == nil {
		// A value the interface refuses comes from this main installation's way of handing over, not from one blob: it would
		// be refused at the next miss again (Q-BAS-293).
		e.fallback(q, resp.HandoverErr, true)
		return
	}
	defer h.DataKey.Destroy()
	if refused, err := e.checkHandover(q, resp, *h); err != nil {
		e.fallback(q, err, refused)
		return
	}
	e.putURL(q, sent, true, nil)
	fill, err := e.d.Cache.NewFill(cache.EntryArtifact, h.Size)
	if err != nil {
		e.fallback(q, errors.New("the cache cannot start a fill"), false)
		return
	}
	if e.d.Cache.SpoolRoom() < h.Size {
		fill.Abort()
		e.fallback(q, errors.New("the spool of fills cannot take the blob now"), false)
		return
	}
	end, _ := q.hold.take()
	readCtx, readCancel := context.WithCancel(q.hold.ctx)
	rd, err := e.d.Handover.Open(readCtx, *h)
	if err != nil {
		readCancel()
		end()
		fill.Abort()
		var he *handover.Error
		cool := errors.As(err, &he) && he.Cooldown()
		if he != nil && he.Digest {
			e.markNoOffer(q.cacheKey)
		}
		e.fallback(q, err, cool)
		return
	}
	r := e.newFillRun(q, resp, fill, h.SHA256, h.Size, sent, cache.EntryArtifact)
	r.direct = &directFill{read: rd, cancel: readCancel, key: q.cacheKey, cont: e.continuation(q.hold.ctx, q), m: e.d.Metrics}
	q.handoverOutcome = r.direct.logged
	e.runFill(q, r, rd, func() { readCancel(); end() })
}

// checkHandover holds a handover to R-BAS-073's checks before the read, and to what the fill needs of the answer. refused is
// true for a value the checks refuse: a link on a host of the Bastion or of the main installation, an expiry the Bastion's clock
// has passed, a blob above the offer, a digest that is not the ETag's. Each comes from the main installation's configuration,
// clock or code and would be refused at every miss, so it starts the cooldown like a store that cannot be read (Q-BAS-293).
func (e *engine) checkHandover(q *rq, resp *mainclient.Response, h iface.Handover) (refused bool, err error) {
	switch {
	case e.d.Handover == nil || q.offer.MaxSize < 1:
		return true, errors.New("a handover answers a forward that offered none")
	case resp.Status != http.StatusOK || q.r.Method != http.MethodGet:
		return true, fmt.Errorf("a handover with status %d to a %s", resp.Status, q.r.Method)
	}
	if err := h.Check(iface.HandoverCheck{Now: e.now(), Offer: q.offer, ETag: resp.Header.Get("Etag"), RefusedHosts: e.refusedHosts}); err != nil {
		return true, err
	}
	if _, keep := varyNames(resp.Header); !keep {
		return false, errors.New("the handed file varies by what the Bastion cannot key on")
	}
	return false, nil
}

// fallback replaces a handover that failed before its first byte by the forward without the offer (R-BAS-075): the
// caller sees a delay, and the main installation counts and charges the second answer too (H-1342). cool starts the
// cooldown. The forward is the miss's: the guard counted it once already.
func (e *engine) fallback(q *rq, cause error, cool bool) {
	e.d.Metrics.Handovers.WithLabelValues(metrics.HandoverFallback).Inc()
	q.handoverOutcome = func() string { return metrics.HandoverFallback }
	if cool {
		e.startCooldown()
	}
	reason := "unknown"
	if cause != nil {
		reason = cause.Error() // the texts of iface and handover name neither the link nor the key
	}
	e.log.Info("a handover is replaced by the forward without the offer", "reason", reason, "cooldown", cool)
	q.noOffer = true
	e.send(q, mainclient.KindForward, "", nil)
}

// continuation is the forward that finishes a fill whose direct read broke off after its first byte (R-BAS-075): the
// caller's request as the miss sent it, with the Bastion's own Range from the spooled length and If-Range with the fill's
// validator, without the offer. Its request is taken now, while the caller's request lives; it runs in the fill's goroutine
// with the fill's context.
func (e *engine) continuation(ctx context.Context, q *rq) func(from int64, etag string, size int64) (io.ReadCloser, error) {
	fr := e.forwardRequest(q)
	class, addr := e.budgetClass(q), q.clientText
	return func(from int64, etag string, size int64) (io.ReadCloser, error) {
		resp, err := e.d.Main.Do(ctx, mainclient.Forward{
			Request: fr, Class: class, Kind: mainclient.KindForward, ClientAddress: addr, RangeFrom: from, IfRange: etag,
		})
		if err != nil {
			return nil, err
		}
		if err := continues(resp, from, etag, size); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		return resp.Body, nil
	}
}

// continues holds the answer to a continuation to what continues the spool: a 206 of the bytes from the spooled length to
// the end, with the fill's ETag. Anything else fails the fill as a fill fails today.
func continues(resp *mainclient.Response, from int64, etag string, size int64) error {
	want := fmt.Sprintf("bytes %d-%d/%d", from, size-1, size)
	switch {
	case resp.Status != http.StatusPartialContent:
		return fmt.Errorf("the continuation was answered %d, not 206", resp.Status)
	case resp.Header.Get("Content-Range") != want:
		return fmt.Errorf("the continuation's range %q is not %q", resp.Header.Get("Content-Range"), want)
	case resp.Header.Get("Etag") != etag:
		return errors.New("the continuation's ETag is not the fill's")
	}
	if n := contentLength(resp.Header); n >= 0 && n != size-from {
		return fmt.Errorf("the continuation announces %d bytes, not %d", n, size-from)
	}
	return nil
}

// directFill is what a fill whose source is a direct read holds besides the spool: the read, the cancel that ends it at the
// stall bound, the continuation, and the handover's one outcome.
type directFill struct {
	read   *handover.Read
	cancel context.CancelFunc
	key    string
	cont   func(from int64, etag string, size int64) (io.ReadCloser, error)
	m      *metrics.Metrics
	// turned is set once the fill reads from the main installation; the stall bound then ends the fill's request.
	turned atomic.Bool

	mu      sync.Mutex
	outcome string
}

// finish records the handover's outcome once and counts it (Q-BAS-290): the first call wins.
func (d *directFill) finish(outcome string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.outcome != "" {
		return
	}
	d.outcome = outcome
	d.m.Handovers.WithLabelValues(outcome).Inc()
}

// logged is the request log's bastion_handover: the outcome, or reading while the read runs.
func (d *directFill) logged() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.outcome == "" {
		return handoverReading
	}
	return d.outcome
}

// turnToMain decides what follows a failed read of a fill (R-BAS-075). A fill from the main installation, or one that reads
// from it already, fails. A direct read whose bytes are not the blob (the plain digest, the encrypted plaintext's digest)
// fails the fill and marks the key. Any other break after the first byte (a cut, the stall bound, a later segment that does
// not open) closes the direct read, which destroys its key, and continues the spool from the main installation.
func (r *fillRun) turnToMain(err error, spooled int64) (io.ReadCloser, bool) {
	d := r.direct
	if d == nil || d.turned.Load() {
		return nil, false
	}
	_ = d.read.Close()
	if errors.Is(err, handover.ErrDigest) {
		r.e.log.Warn("a handed blob is not its digest; its key sends no offer for the revalidation period", "sha256", r.sha)
		r.e.markNoOffer(d.key)
		d.finish(metrics.HandoverRefused)
		return nil, false
	}
	if spooled <= 0 || spooled >= r.size {
		d.finish(metrics.HandoverRefused)
		return nil, false
	}
	d.turned.Store(true)
	d.finish(metrics.HandoverContinued)
	r.e.log.Info("a direct read broke off; the fill continues from the main installation", "sha256", r.sha, "from", spooled, "error", err.Error())
	body, cerr := d.cont(spooled, r.etag, r.size)
	if cerr != nil {
		r.e.log.Warn("the continuation of a fill failed", "sha256", r.sha, "error", cerr.Error())
		return nil, false
	}
	return body, true
}

// stall ends what the fill reads now when no byte arrived within the stall bound: the direct read, which the run then
// continues from the main installation, or the request to the main installation, which fails the fill.
func (r *fillRun) stall(end func()) {
	if d := r.direct; d != nil && !d.turned.Load() {
		d.cancel()
		return
	}
	end()
}

// fillSourceLabel is the source of the bytes a fill writes now (quader_bastion_fill_bytes_total).
func (r *fillRun) fillSourceLabel() string {
	if d := r.direct; d != nil && !d.turned.Load() {
		return metrics.SourceStore
	}
	return metrics.SourceMain
}

// committed records the end of a fill's commit for its handover: a read from the store, or a fill refused at its digest,
// which marks the key.
func (r *fillRun) committed(ok bool) {
	d := r.direct
	if d == nil {
		return
	}
	if ok {
		d.finish(metrics.HandoverRead)
		return
	}
	r.e.markNoOffer(d.key)
	d.finish(metrics.HandoverRefused)
}
