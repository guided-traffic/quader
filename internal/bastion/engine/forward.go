package engine

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/answers"
	"github.com/guided-traffic/quader/internal/bastion/cache"
	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// maxDocumentBytes is the memory bound of a document (R-BAS-021): a shared document above it is spooled by its
// content address (Q-BAS-173), any other answer above it passed through.
const maxDocumentBytes = 64 << 20

// baseline is what a conditional request validated: the cache entry, the caller's private copy, or a fill that runs
// on this replica and that the caller follows once the main installation said 304 (R-BAS-054).
type baseline struct {
	ent  *cache.Entry
	priv *privDoc
	run  *fillRun
}

// forward sends the caller's request to the main installation with the caller's credential and handles the
// response (R-BAS-021). validator is the Bastion's own If-None-Match of a conditional request.
func (e *engine) forward(q *rq, kind mainclient.Kind, validator string, bl *baseline) {
	if !e.d.Link.Reachable() {
		e.unreachable(q)
		return
	}
	if v := e.d.Guard.Miss(q.ctx(), q.addr, guard.Credentialed(!q.cred.anonymous)); !v.Allowed {
		e.refuseVerdict(q, v)
		return
	}
	e.send(q, kind, validator, bl)
}

// send is the forward after the guard admitted the miss: the request with the caller's credential, the Bastion's own
// conditional and, where R-BAS-072 allows it, the offer to take a handover; then the answer by its marking.
func (e *engine) send(q *rq, kind mainclient.Kind, validator string, bl *baseline) {
	sent := e.now()
	// The request to the main installation does not end with the client: an artifact fill reads on at the
	// main installation's speed when the client is slow or gone (Q-BAS-123). Everything else ends with the client.
	hold := newHold(q.ctx())
	q.hold = hold
	q.offer = e.offerFor(q, kind, bl)
	resp, err := e.d.Main.Do(hold.ctx, mainclient.Forward{
		Request: e.forwardRequest(q), Class: e.budgetClass(q), Kind: kind, ClientAddress: q.clientText, IfNoneMatch: validator,
		Offer: q.offer,
	})
	if err != nil {
		hold.release()
		e.mainError(q, err)
		return
	}
	defer func() {
		hold.release()
		_ = resp.Body.Close() // the body of a fill that took the response over is replaced by an empty one
	}()
	e.process(q, resp, sent, bl)
}

// mainHold is the context of a request to the main installation: it ends with the client's request unless a
// fill takes it over.
type mainHold struct {
	ctx    context.Context
	cancel context.CancelFunc
	stop   func() bool
	mu     sync.Mutex
	taken  bool
}

func newHold(client context.Context) *mainHold {
	ctx, cancel := context.WithCancel(context.WithoutCancel(client))
	return &mainHold{ctx: ctx, cancel: cancel, stop: context.AfterFunc(client, cancel)}
}

// release ends the request to the main installation with the client's, unless a fill took it over.
func (h *mainHold) release() {
	h.mu.Lock()
	taken := h.taken
	h.mu.Unlock()
	if !taken {
		h.stop()
		h.cancel()
	}
}

// take gives the request to the main installation to a fill: the client going away no longer cancels it. The
// returned function ends it. ok is false when the client went away before: the request is cancelled already.
func (h *mainHold) take() (end func(), ok bool) {
	h.mu.Lock()
	h.taken = true
	h.mu.Unlock()
	return h.cancel, h.stop()
}

// stripRequest are the request headers that are never forwarded (R-BAS-021): the cookie, the forwarded-for
// families the Bastion writes itself, the interface's own headers, the caller's conditionals and range (the
// Bastion's conditionals are its own), the hop-by-hop headers, and the NuGet API key, which is no read
// credential (R-BAS-019).
func stripRequest(h http.Header) {
	drop := map[string]bool{}
	for _, v := range h.Values("Connection") {
		for _, tok := range strings.Split(v, ",") {
			drop[http.CanonicalHeaderKey(strings.TrimSpace(tok))] = true
		}
	}
	for k := range h {
		ck := http.CanonicalHeaderKey(k)
		switch {
		case ck == "Cookie", ck == "Forwarded", ck == "Range", ck == "X-Nuget-Apikey", ck == "Expect",
			strings.HasPrefix(ck, "X-Forwarded-"), strings.HasPrefix(ck, "Quader-"), strings.HasPrefix(ck, "If-"),
			hopByHop(ck), drop[ck]:
			delete(h, k)
		}
	}
}

// forwardRequest is the caller's request without the headers above and without a body.
func (e *engine) forwardRequest(q *rq) *http.Request {
	fr := q.r.Clone(q.ctx())
	fr.Header = q.r.Header.Clone()
	stripRequest(fr.Header)
	if q.cred.dropped {
		// An anonymous registry token is the anonymous caller: the main installation serves the request without a
		// credential where the artifacts are public (R-BAS-052, Q-BAS-168).
		fr.Header.Del("Authorization")
	}
	fr.Body, fr.ContentLength = nil, 0
	return fr
}

// process handles the main installation's response by its marking (R-BAS-021).
func (e *engine) process(q *rq, resp *mainclient.Response, sent time.Time, bl *baseline) {
	q.mainRequestID = resp.Header.Get("X-Request-Id")
	if !resp.Marked {
		// The answer of a proxy in front of the main installation.
		if resp.Status >= 500 {
			e.fail(q, mainclient.ErrUnreachable)
			return
		}
		e.passThrough(q, resp, nil, metrics.ClassPass, metrics.ResultMiss)
		return
	}
	m := resp.Marking
	e.recordAnswers(q, resp, sent)
	if q.passOnly && m.Class != iface.ClassHandover {
		e.onPassOnly(q, resp)
		return
	}
	if m.Class == iface.ClassArtifact || m.Class == iface.ClassShared || m.Class == iface.ClassHandover {
		// The Vary of the response decides the cache key everything below is kept under.
		e.learnVary(q, resp.Header)
	}
	if bl != nil && bl.priv != nil && m.Class != iface.ClassPrivate {
		// The main installation no longer filters a document for this caller: the copy is stale.
		e.priv.remove(privKey(q.cred.key, q.cacheKey))
	}
	switch m.Class {
	case iface.ClassHandover:
		// Never passed to a caller: the answer is consumed, and a value the engine cannot use ends in the forward
		// without the offer (R-BAS-073, R-BAS-075).
		e.onHandover(q, resp, sent)
	case iface.ClassArtifact:
		e.onArtifact(q, resp, sent, bl)
	case iface.ClassShared:
		e.onShared(q, resp, sent, bl)
	case iface.ClassPrivate:
		e.onPrivate(q, resp, sent, bl)
	case iface.ClassAbsent:
		e.onAbsent(q, resp, bl)
	default:
		e.onPass(q, resp, sent)
	}
}

// recordAnswers keeps the credential, shape and package answers a marked response carries (R-BAS-020, R-BAS-084) and
// counts the failed authentication of a credential the main installation called invalid (R-BAS-036). A package answer is
// recorded only from a marking of the request's own repository and only while the caller's shape on it is partial: a full
// shape covers every package.
func (e *engine) recordAnswers(q *rq, resp *mainclient.Response, sent time.Time) {
	m, ctx := resp.Marking, q.ctx()
	sub := m.Subject
	switch sub.Kind {
	case iface.SubjectToken:
		q.subject = sub
		if !q.cred.anonymous {
			e.answers.put(ctx, answers.Credential, q.cred.key, &answerVal{Answer: answerBase(answers.Credential, sent, sub, ""), Subject: sub})
		}
	case iface.SubjectInvalid:
		if insufficientScope(resp.Status, resp.Header) {
			// The registry's insufficient_scope says nothing about the credential: replayed, no answer kept, no failed
			// authentication counted (R-BAS-052).
			break
		}
		e.keepInvalid(q, sent)
	}
	if m.Read.Valid() && q.repo != "" && m.Repository == q.repo && (sub.Kind == iface.SubjectToken || sub.Kind == iface.SubjectAnonymous) {
		read := capRead(q.cred, m.Read)
		status := 0
		if read == iface.ReadNone {
			status = resp.Status
		}
		e.answers.put(ctx, answers.Shape, shapeKey(q.cred.key, q.repo), &answerVal{Answer: answerBase(answers.Shape, sent, sub, q.repo), Read: read, Status: status})
		if read == iface.ReadPartial && m.Package != "" {
			e.putPackage(ctx, q.cred, q.repo, m.Package, m.PackageRead, sent, sub)
		}
	}
}

// entryPackage is the package of the entry a 200 or a 304 writes or renews (R-BAS-084): the marking's, when the marking is
// of the request's repository, and none otherwise, so a marking without a package clears it.
func entryPackage(q *rq, m iface.Marking) string {
	if m.Repository != q.repo {
		return ""
	}
	return m.Package
}

// putURL keeps the URL answer of this credential and cache key: allowed, or the refusal.
func (e *engine) putURL(q *rq, sent time.Time, allowed bool, ref *refusal) {
	sub := q.subject
	if q.cred.anonymous {
		sub = iface.Subject{Kind: iface.SubjectAnonymous}
	}
	e.answers.put(q.ctx(), answers.URL, urlKey(q.cred.key, q.cacheKey), &answerVal{Answer: answerBase(answers.URL, sent, sub, q.repo), Allowed: allowed, Refusal: ref})
}

// passThrough replays the main installation's response to the caller: its status, headers and body, without
// the headers of the interface (R-BAS-021). pre holds bytes already read from the body.
func (e *engine) passThrough(q *rq, resp *mainclient.Response, pre []byte, class, result string) {
	q.finish(class, result)
	copyHeader(q.w.Header(), replayHeader(resp.Header))
	q.out.source = metrics.SourceMain
	q.w.WriteHeader(resp.Status)
	if q.r.Method == http.MethodHead || resp.Status == http.StatusNoContent || resp.Status == http.StatusNotModified {
		return
	}
	if len(pre) > 0 {
		if _, err := q.out.Write(pre); err != nil {
			return
		}
	}
	buf := make([]byte, 32<<10)
	began, sent := time.Now(), int64(len(pre))
	rc := http.NewResponseController(q.out)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			_ = rc.SetWriteDeadline(time.Now().Add(e.paceChunk)) // a chunk that the client does not take in time ends the transfer
			if _, werr := q.out.Write(buf[:n]); werr != nil {
				return
			}
			sent += int64(n)
			if e.tooSlow(began, sent) {
				// The budget slot is held until the body ends: a client that takes the body slower than the
				// floor does not keep it (R-BAS-025, Q-BAS-123).
				e.log.Warn("a pass-through transfer is cut: the client takes it slower than the floor", "bytes", sent, "after", time.Since(began).Round(time.Second).String())
				panic(http.ErrAbortHandler)
			}
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			panic(http.ErrAbortHandler)
		}
	}
}

// tooSlow reports whether a pass-through transfer has taken longer than the grace period and moved fewer bytes
// per second, on average since it began, than the floor.
func (e *engine) tooSlow(began time.Time, sent int64) bool {
	el := time.Since(began)
	return el > e.paceGrace && float64(sent) < el.Seconds()*float64(e.paceMinRate)
}

// onPass handles every other answer: nothing is kept but a refusal of the read (R-BAS-020). A 403 is kept, unless it
// says Cache-Control: no-store: the refusal of a remote's delivery rules (403 not_allowed_in_repository, R-CUR-004) is
// lifted by an allow, which no feed entry announces, so a kept copy would outlive the allow by up to the answer period
// (Q-CUR-44). A 404 that is not marked absent is never kept, whether it answers a plain fetch or a conditional request:
// it may be a missing name or one the caller may not see, and replaying it only for names the Bastion holds would show
// a narrowed reader which hidden names are cached (R-BAS-024, Q-BAS-42, Q-BAS-124).
func (e *engine) onPass(q *rq, resp *mainclient.Response, sent time.Time) {
	if resp.Status >= http.StatusBadRequest {
		e.markVolatileFor(q) // a refusal is this caller's: the main installation answers another caller otherwise
	} else {
		e.markVolatile(q.cacheKey) // a search or another answer that is nobody's to share
	}
	keepable := resp.Status == http.StatusForbidden && !noStore(resp.Header)
	if q.r.Method == http.MethodGet && keepable {
		body, rest, whole := readBounded(resp.Body, maxRefusalBody)
		if whole {
			h := replayHeader(resp.Header)
			h.Del("Content-Length")
			e.putURL(q, sent, false, &refusal{Status: resp.Status, Header: h, Body: body})
		}
		e.passThroughRest(q, resp, body, rest, metrics.ClassPass)
		return
	}
	e.passThrough(q, resp, nil, metrics.ClassPass, metrics.ResultMiss)
}

// noStore reports whether a response's Cache-Control carries the directive no-store (RFC 9111 section 5.2.2.5).
func noStore(h http.Header) bool {
	for _, v := range h.Values("Cache-Control") {
		for d := range strings.SplitSeq(v, ",") {
			name, _, _ := strings.Cut(strings.TrimSpace(d), "=")
			if strings.EqualFold(name, "no-store") {
				return true
			}
		}
	}
	return false
}

// passThroughRest continues a pass-through after a prefix of the body was read.
func (e *engine) passThroughRest(q *rq, resp *mainclient.Response, pre []byte, rest io.Reader, class string) {
	resp.Body = readCloser{Reader: rest, Closer: resp.Body}
	e.passThrough(q, resp, pre, class, metrics.ResultMiss)
}

type readCloser struct {
	io.Reader
	io.Closer
}

// readBounded reads up to limit bytes of r. whole is true when the end of r was reached within the limit;
// otherwise rest continues after the bytes returned.
func readBounded(r io.Reader, limit int64) (body []byte, rest io.Reader, whole bool) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || int64(len(body)) > limit {
		return body, r, false
	}
	return body, strings.NewReader(""), true
}

// onAbsent handles a 404 marked absent: kept per cache key for the negative period and replayed to full readers
// (R-BAS-024). It removes a cache entry the main installation says no longer exists (R-BAS-022).
func (e *engine) onAbsent(q *rq, resp *mainclient.Response, bl *baseline) {
	if bl != nil && bl.ent != nil {
		e.dropEntry(q.ctx(), q.cacheKey)
	}
	if q.r.Method != http.MethodGet || resp.Status != http.StatusNotFound {
		e.markVolatile(q.cacheKey)
		e.passThrough(q, resp, nil, metrics.ClassAbsent, metrics.ResultMiss)
		return
	}
	body, rest, whole := readBounded(resp.Body, maxRefusalBody)
	if whole {
		h := replayHeader(resp.Header)
		h.Del("Content-Length")
		e.negPut(q.ctx(), q.cacheKey, &negEntry{Repository: resp.Marking.Repository, StoredAt: e.now(), Refusal: refusal{Status: resp.Status, Header: h, Body: body}})
	} else {
		e.markVolatile(q.cacheKey)
	}
	e.passThroughRest(q, resp, body, rest, metrics.ClassAbsent)
}

// onPrivate handles a document filtered for the caller (R-BAS-023): the caller's last copy is kept in memory,
// validated with a conditional request on every use.
func (e *engine) onPrivate(q *rq, resp *mainclient.Response, sent time.Time, bl *baseline) {
	e.markVolatileFor(q) // the document is filtered for this caller; a full reader's fetch is shareable
	sub := q.subject
	if resp.Status == http.StatusNotModified && bl != nil && bl.priv != nil {
		_ = resp.Body.Close()
		d := *bl.priv
		d.ObtainedAt = sent
		e.privPut(q.cred.key, q.cacheKey, &d)
		e.servePrivate(q, &d, metrics.ResultChecked)
		return
	}
	if resp.Status == http.StatusNotModified && bl != nil && bl.ent != nil {
		// The Bastion's own condition named the shared entry's validator and the main installation answered 304 for
		// this caller: the caller's rendition is the entry's bytes (R-NPM-013's validators are derived from what the
		// caller may read). The entry is served as this caller's 200, without renewing it: it is not this caller's
		// to renew (R-BAS-023). Passing the 304 on would answer a request that carried no condition.
		_ = resp.Body.Close()
		if !e.serveEntry(q, *bl.ent, metrics.ResultChecked) {
			e.forward(q, mainclient.KindForward, "", nil)
		}
		return
	}
	limit := min(e.docMemory, int64(e.cfg.Cache.PrivateDocumentsMaxBytes))
	if q.r.Method != http.MethodGet || resp.Status != http.StatusOK || limit <= 0 {
		e.passThrough(q, resp, nil, metrics.ClassPrivate, metrics.ResultMiss)
		return
	}
	body, rest, whole := readBounded(resp.Body, limit)
	if !whole {
		e.passThroughRest(q, resp, body, rest, metrics.ClassPrivate)
		return
	}
	if etag := resp.Header.Get("Etag"); etag != "" {
		d := &privDoc{Answer: answerBase(answers.URL, sent, sub, q.repo), Status: resp.Status, Header: replayHeader(resp.Header), Body: body, ETag: etag}
		e.privPut(q.cred.key, q.cacheKey, d)
	}
	e.serveBuffered(q, resp, body, metrics.ClassPrivate)
}

// serveBuffered answers from a document held in memory, with the conditionals of the caller: the document came
// from the main installation, so its bytes count as served from there.
func (e *engine) serveBuffered(q *rq, resp *mainclient.Response, body []byte, class string) {
	h := replayHeader(resp.Header)
	h.Del("Content-Length")
	src := bytesSource(h, body)
	src.label = metrics.SourceMain
	q.finish(class, metrics.ResultMiss)
	e.serveSource(q, src)
}

// onShared handles a document the same for every full reader (R-BAS-023): kept under its cache key.
func (e *engine) onShared(q *rq, resp *mainclient.Response, sent time.Time, bl *baseline) {
	if resp.Status == http.StatusNotModified && bl != nil && bl.ent != nil {
		_ = resp.Body.Close() // the slot is free while the cached bytes are served
		ent := *bl.ent
		ent.ValidatedAt = sent
		ent.Counted = ent.Counted || resp.Marking.Counted
		ent.Package = entryPackage(q, resp.Marking)
		_ = e.saveEntry(q.ctx(), q.cacheKey, ent)
		if !e.serveEntry(q, ent, metrics.ResultChecked) {
			e.forward(q, mainclient.KindForward, "", nil)
		}
		return
	}
	if resp.Status == http.StatusNotModified && bl != nil && bl.ent == nil && bl.priv != nil {
		// The Bastion's own condition named the validator of the caller's private copy and the main installation
		// no longer filters the document for the caller: the copy is current, and it answers the caller, who sent
		// no condition, as a 200. The copy itself is gone (process); the shared entry is not this caller's to
		// renew (R-BAS-023, Q-BAS-107).
		_ = resp.Body.Close()
		src := bytesSource(bl.priv.Header, bl.priv.Body)
		if e.serveSource(q, src) {
			q.finish(metrics.ClassShared, metrics.ResultChecked)
			return
		}
	}
	if q.r.Method != http.MethodGet || resp.Status != http.StatusOK {
		e.passThrough(q, resp, nil, metrics.ClassShared, metrics.ResultMiss)
		return
	}
	limit := min(e.docMemory, int64(e.cfg.Cache.DocumentsMaxBytes))
	body, rest, whole := readBounded(resp.Body, limit)
	if !whole {
		if sha, size := e.spoolable(resp); sha != "" {
			// Above the memory bound with a content address: filled through the spool like an artifact, kept in the
			// documents' area and revalidated like a document (Q-BAS-173).
			e.fillSpool(q, resp, body, sha, size, sent, cache.EntryDocument)
			return
		}
		e.markVolatile(q.cacheKey)
		e.passThroughRest(q, resp, body, rest, metrics.ClassShared)
		return
	}
	key, keep := e.learnVary(q, resp.Header)
	if keep {
		e.storeDocument(q, key, resp, body, sent)
	} else {
		e.markVolatile(q.cacheKey)
	}
	e.serveBuffered(q, resp, body, metrics.ClassShared)
}

// spoolable returns the content address and the length of a shared document above the memory bound that can be filled
// through the spool (Q-BAS-173): a strong ETag that is a content address, a Content-Length within max_object_bytes and
// within documents_max_bytes (the cache's bound of a document), and a Vary the Bastion can key on. sha is empty for any
// other.
func (e *engine) spoolable(resp *mainclient.Response) (sha string, size int64) {
	size = contentLength(resp.Header)
	limit := min(int64(e.cfg.Cache.MaxObjectBytes), int64(e.cfg.Cache.DocumentsMaxBytes))
	if _, keep := varyNames(resp.Header); !keep || size < 0 || size > limit {
		return "", 0
	}
	return contentAddress(resp.Header.Get("Etag")), size
}

// storeDocument writes a shared document through a cache fill and its entry; a failure of the backend, counted
// by the cache, only means it is not kept (R-BAS-030).
func (e *engine) storeDocument(q *rq, key string, resp *mainclient.Response, body []byte, sent time.Time) {
	ctx := context.WithoutCancel(q.ctx())
	sha, err := e.keepBytes(ctx, cache.EntryDocument, body)
	if err != nil {
		return
	}
	h := keepHeader(resp.Header, q.facts.ReplayHeaders)
	_ = e.saveEntry(ctx, key, cache.Entry{
		Kind: cache.EntryDocument, SHA256: sha, Size: int64(len(body)), Header: h, Repository: resp.Marking.Repository,
		Status: resp.Status, ValidatedAt: sent, Counted: resp.Marking.Counted, Package: entryPackage(q, resp.Marking),
	})
}

// contentAddress reads the SHA-256 of a strong ETag that is a content address (R-STOR-012: ETag "<sha256 hex>"; the
// registry's "sha256:<hex>", R-BAS-053). A weak validator is none.
func contentAddress(etag string) string {
	if strings.HasPrefix(etag, "W/") {
		return ""
	}
	etag = strings.TrimPrefix(strings.Trim(etag, `"`), "sha256:")
	if len(etag) != 64 {
		return ""
	}
	for i := 0; i < len(etag); i++ {
		c := etag[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ""
		}
	}
	return etag
}

// contentLength reads a response's Content-Length, -1 when absent or malformed.
func contentLength(h http.Header) int64 {
	n, err := strconv.ParseInt(h.Get("Content-Length"), 10, 64)
	if err != nil || n < 0 {
		return -1
	}
	return n
}
