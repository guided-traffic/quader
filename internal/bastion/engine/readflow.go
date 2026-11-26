package engine

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/answers"
	"github.com/guided-traffic/quader/internal/bastion/cache"
	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// read is the read flow of R-BAS-018 step 6 (R-BAS-020 to R-BAS-025): the cache entry for the cache key and
// the caller's answers decide between a hit, a check and a miss.
func (e *engine) read(q *rq) {
	if q.credAns != nil && q.credAns.Subject.Kind == iface.SubjectInvalid {
		// A kept invalid answer: the caller costs the main installation one request per period (R-BAS-020).
		e.refuse(q, e.errInvalidToken(q), metrics.RefusedInvalid)
		return
	}
	for attempt := 0; attempt < 2; attempt++ {
		if !e.attempt(q) {
			return
		}
		// A fetch this request waited for has ended: decide again with what it left. The second round
		// does not wait again, so a result that is not shared (a private document, a refusal) costs each
		// waiter its own request, never a queue (R-BAS-025).
		q.collapsed = true
	}
}

// attempt decides once; it reports true when a collapsed fetch ended and the decision is to be made again.
func (e *engine) attempt(q *rq) (again bool) {
	ctx := q.ctx()
	e.computeKey(q) // a fetch the request waited for may have taught the Vary of the target
	e.learnVaryFromCache(ctx, q)
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
	if ent, ok := e.loadEntry(ctx, q.cacheKey); ok && e.fromEntry(q, ent, shape, url) {
		return false
	}
	shape = e.answers.shape(ctx, q.cred.key, q.repo)
	if e.fromNegative(q, shape) {
		return false
	}
	return e.fromPrivateOrMiss(q)
}

// refuseNone answers a read-shape answer none with the stored status, without a request (R-BAS-022).
func (e *engine) refuseNone(q *rq, shape *answerVal) {
	e.refuse(q, e.errShapeNone(q, shape.Status), "")
}

// isFull reports whether the shape answer lets the caller read every cached item of the repository.
func isFull(shape *answerVal) bool { return shape != nil && shape.Read == iface.ReadFull }

// fromEntry decides for a request whose cache key has an entry (R-BAS-022, R-BAS-023, and the steps of R-BAS-085): serve
// it, ask the check, or send the conditional request. It reports false when the entry turned out to be unusable.
//
//   - Step 2, allowed: the caller's shape on the repository is full, or the entry names a package and the caller's package
//     answer on it is full, or the caller's URL answer allows the key.
//   - Step 3: allowed, not withdrawn and fresh (or the main installation unreachable): served. Not allowed, the anonymous
//     caller's fresh copy of the document is served (R-BAS-094).
//   - Step 4: no usable shape answer (and no URL answer that allows): one check for the repository and the entry's package,
//     collapsed per credential, repository and package; then steps 2 and 3, or the conditional request.
//   - Steps 5 and 6: everything else is the conditional request. A caller whose shape is known and partial and who has no
//     package answer for the entry's package is not asked a check: its conditional request costs one round trip as the
//     forward of an uncached path does, so no timing tells it a cached package from an uncached one, and the marking of the
//     answer supplies the package answer.
func (e *engine) fromEntry(q *rq, ent cache.Entry, shape, url *answerVal) bool {
	unreachable := !e.d.Link.Reachable()
	pkg := e.packageAnswer(q.ctx(), q, ent.Repository, ent.Package, shape)
	allowed := isFull(shape) || isFull(pkg) || (url != nil && url.Allowed)
	// The grace period of R-BAS-027 extends the validity of an answer, never past a withdrawal: a content entry
	// of the feed received after validatedAt - 15 s makes the entry unservable while unreachable as well
	// (Q-BAS-122).
	if allowed && !e.withdrawn(ent) && (unreachable || e.fresh(ent)) {
		if !e.serveEntry(q, ent, metrics.ResultHit) {
			return false
		}
		if isFull(shape) {
			e.refreshShape(q, shape)
		}
		return true
	}
	if e.fromAnonymousCopy(q) {
		return true
	}
	if unreachable {
		if priv := e.privGet(q.cred.key, q.cacheKey); priv != nil && e.privUsable(priv) {
			return e.servePrivate(q, priv, metrics.ResultHit)
		}
		e.unreachable(q)
		return true
	}
	if shape == nil && (url == nil || !url.Allowed) {
		var done bool
		if shape, pkg, done = e.checkShape(q, ent.Repository, ent.Package); done {
			return true
		}
		if shape.Read == iface.ReadNone {
			e.refuseNone(q, shape)
			return true
		}
		if (isFull(shape) || isFull(pkg)) && e.fresh(ent) {
			return e.serveEntry(q, ent, metrics.ResultChecked)
		}
	}
	return e.conditional(q, &ent, e.privGet(q.cred.key, q.cacheKey))
}

// fromNegative replays a negative entry to a caller whose full read of the repository is confirmed
// (R-BAS-024, Q-BAS-42); a caller without a shape answer gets one first.
func (e *engine) fromNegative(q *rq, shape *answerVal) bool {
	n := e.negGet(q.ctx(), q.cacheKey)
	if n == nil || n.Repository != q.repo {
		return false
	}
	if shape == nil {
		var done bool
		if shape, _, done = e.checkShape(q, "", ""); done {
			return true
		}
		if shape.Read == iface.ReadNone {
			e.refuseNone(q, shape)
			return true
		}
	}
	if !isFull(shape) {
		return false
	}
	q.finish(metrics.ClassAbsent, metrics.ResultHit)
	ref := n.Refusal
	e.writeRefusal(q, &ref, metrics.SourceCache)
	return true
}

// fromPrivateOrMiss handles a request without a usable entry: the anonymous caller's fresh copy is served, the caller's
// private copy is validated with a conditional request, anything else is a miss.
func (e *engine) fromPrivateOrMiss(q *rq) (again bool) {
	if e.fromAnonymousCopy(q) {
		return false
	}
	if priv := e.privGet(q.cred.key, q.cacheKey); priv != nil {
		if !e.d.Link.Reachable() {
			if e.privUsable(priv) {
				e.servePrivate(q, priv, metrics.ResultHit)
			} else {
				e.unreachable(q)
			}
			return false
		}
		e.conditional(q, nil, priv)
		return false
	}
	return e.miss(q)
}

// serveEntry serves a cache entry to a caller the main installation allowed (a hit, or after a check). A serve of an
// entry whose fill the main installation counted as a download is reported (R-BAS-039, R-BAS-059): what a download
// is, is the main installation's rule for every format.
func (e *engine) serveEntry(q *rq, ent cache.Entry, result string) bool {
	src := e.entrySource(q.ctx(), q.cacheKey, ent)
	class := metrics.ClassShared
	if ent.Kind == cache.EntryArtifact {
		class = metrics.ClassArtifact
	}
	if ent.Counted {
		src.download = func(q *rq) { e.downloadEvent(q, ent.Repository, ent.SHA256) }
	}
	if !e.serveSource(q, src) {
		return false
	}
	q.finish(class, result)
	return true
}

// fromAnonymousCopy serves the anonymous caller's copy of a document without a request while it is fresh (R-BAS-094,
// R-BAS-023): the main installation's decision for the anonymous subject is the same for every anonymous caller (R-BAS-019,
// R-AUTHZ-029), so the copy it filtered for one of them is every anonymous caller's, and it is kept like a shared document: for
// bastion.cache.metadata_revalidate_after since its last validation, and no longer once a content or grants entry of the feed
// that names its repository was received after it (rule 3 of R-BAS-020), judged as a cache entry is (freshFor). It reports
// whether it answered the request. A copy of a caller with a token is still validated on every use.
func (e *engine) fromAnonymousCopy(q *rq) bool {
	if !q.cred.anonymous {
		return false
	}
	d := e.privGet(q.cred.key, q.cacheKey)
	if d == nil {
		return false
	}
	return e.freshFor(d.ObtainedAt, d.Repository, e.cfg.Cache.MetadataRevalidateAfter) && e.servePrivate(q, d, metrics.ResultHit)
}

// servePrivate serves the caller's private copy of a document.
func (e *engine) servePrivate(q *rq, d *privDoc, result string) bool {
	src := bytesSource(d.Header, d.Body)
	if !e.serveSource(q, src) {
		return false
	}
	q.finish(metrics.ClassPrivate, result)
	return true
}

// checkResult is what the check leader hands to its waiters.
type checkResult struct {
	answer iface.CheckAnswer
	sentAt time.Time
	// packageAt is when the package answer in answer was obtained, when it is older than sentAt: a result storedCheck
	// rebuilt carries a package answer with its own time, which recording it again must not extend (Q-BAS-248). Zero means
	// sentAt.
	packageAt time.Time
}

// storedCheck rebuilds the result of a check from the answers a check that ended meanwhile stored, so the caller
// that reached the collapse after that check left it uses them and sends none of its own (R-BAS-022). The package answer
// on pkg is rebuilt as well when the shape is partial (R-BAS-084); without one the result holds no package, and the caller
// sends the conditional request of step 5, as any caller whose shape is known. It reports false when the store holds no
// usable shape answer and, for a token, no usable credential answer of a valid token.
func (e *engine) storedCheck(ctx context.Context, q *rq, pkg string) (checkResult, bool) {
	shape := e.answers.shape(ctx, q.cred.key, q.repo)
	if shape == nil {
		return checkResult{}, false
	}
	sub := iface.Subject{Kind: iface.SubjectAnonymous}
	if !q.cred.anonymous {
		cred := e.answers.credential(ctx, q.cred.key)
		if cred == nil || cred.Subject.Kind != iface.SubjectToken {
			return checkResult{}, false
		}
		sub = cred.Subject
	}
	rr := iface.RepositoryRead{Read: shape.Read, Status: shape.Status}
	r := checkResult{
		answer: iface.CheckAnswer{Subject: sub, Repositories: map[string]iface.RepositoryRead{q.repo: rr}},
		sentAt: shape.ObtainedAt,
	}
	if pkg != "" && shape.Read == iface.ReadPartial {
		if p := e.answers.pkg(ctx, q.cred.key, q.repo, pkg); p != nil {
			r.answer.Packages = iface.PackageReads{{Repository: q.repo, Package: pkg, Read: p.Read}}
			r.packageAt = p.ObtainedAt
		}
	}
	return r, true
}

// checkShape asks the check of R-BAS-011 for the caller's credential and the addressed repository and, for an entry of
// that repository that names a package, for the pair (repository, package) (R-BAS-085 step 4): one request per
// credential, repository and package, collapsed (R-BAS-022). It returns the shape answer and the package answer the check
// gave; done is true when it answered the request itself.
func (e *engine) checkShape(q *rq, entryRepo, entryPkg string) (shape, pkg *answerVal, done bool) {
	if !e.d.Link.Reachable() {
		e.unreachable(q)
		return nil, nil, true
	}
	if v := e.d.Guard.Miss(q.ctx(), q.addr, guard.Credentialed(!q.cred.anonymous)); !v.Allowed {
		e.refuseVerdict(q, v)
		return nil, nil, true
	}
	name := ""
	if entryRepo == q.repo && len(checkPackages(q.repo, entryPkg)) == 1 {
		name = entryPkg
	}
	key := "check|" + q.cred.key + "|" + q.repo
	if name != "" {
		key += "|" + name
	}
	res, err := e.checks.do(q.ctx(), key, func(ctx context.Context) (any, error) {
		// This caller read its answers before the check of an earlier leader stored its own, and led only
		// after that check left the collapse: what the earlier check stored answers it.
		if r, ok := e.storedCheck(ctx, q, name); ok {
			return r, nil
		}
		req := iface.CheckRequest{ClientAddress: q.clientText, Repositories: []string{q.repo}, Packages: checkPackages(q.repo, name)}
		if !q.cred.anonymous {
			t := q.cred.token
			req.Credential = &t
		}
		sent := e.now()
		ans, err := e.d.Main.Check(ctx, e.budgetClass(q), req)
		return checkResult{answer: ans, sentAt: sent}, err
	})
	if err != nil {
		e.mainError(q, err)
		return nil, nil, true
	}
	r := res.(checkResult)
	return e.applyCheck(q, name, r)
}

func (e *engine) budgetClass(q *rq) mainclient.Class {
	if q.cred.anonymous {
		return mainclient.Anonymous
	}
	return mainclient.Authenticated
}

// applyCheck keeps the credential, shape and package answers of a check and turns the subjects the Bastion does not
// serve into their answers (R-BAS-011, R-BAS-029).
func (e *engine) applyCheck(q *rq, pkgName string, r checkResult) (shape, pkg *answerVal, done bool) {
	sub := r.answer.Subject
	shape, pkg = e.recordCheck(q.ctx(), q.cred, q.repo, pkgName, q.addr, r)
	switch sub.Kind {
	case iface.SubjectInvalid:
		e.refuse(q, e.errInvalidToken(q), metrics.RefusedInvalid)
		return nil, nil, true
	case iface.SubjectAnonymousRefused:
		e.refuse(q, e.errCredentialsRequired(q), metrics.RefusedInvalid)
		return nil, nil, true
	case iface.SubjectThrottled:
		after := time.Second
		if sub.RetryAfterSeconds != nil {
			after = time.Duration(*sub.RetryAfterSeconds) * time.Second
		}
		e.refuseVerdict(q, guard.Verdict{Code: iface.CodeRateLimited, RetryAfter: after, Reason: metrics.RefusedRateLimited})
		return nil, nil, true
	}
	if shape == nil {
		// A subject kind this Bastion does not know: the interface versions would differ.
		e.fail(q, mainclient.ErrUnreachable)
		return nil, nil, true
	}
	q.subject = sub
	return shape, pkg, false
}

// keepInvalid keeps the main installation's statement that a credential is invalid, like an allowance, and
// counts the failed authentication (R-BAS-020, R-BAS-036).
func (e *engine) keepInvalid(q *rq, o time.Time) {
	sub := iface.Subject{Kind: iface.SubjectInvalid}
	if !q.cred.anonymous {
		e.answers.put(q.ctx(), answers.Credential, q.cred.key, &answerVal{Answer: answerBase(answers.Credential, o, sub, ""), Subject: sub})
		e.d.Guard.FailedAuth(q.ctx(), q.addr)
	}
}

// conditional sends the conditional request of R-BAS-022 and R-BAS-023: the caller's own credential and the
// validator of the cached bytes, or of the caller's private copy.
func (e *engine) conditional(q *rq, ent *cache.Entry, priv *privDoc) bool {
	validator := ""
	switch {
	case priv != nil:
		validator = priv.ETag
	case ent != nil:
		validator = entryETag(*ent)
	}
	e.forward(q, mainclient.KindConditional, validator, &baseline{ent: ent, priv: priv})
	return true
}

// miss forwards the request; concurrent misses for one cache key share one fetch per replica and, with Valkey
// and the bucket backend, across replicas (R-BAS-025). It reports true when the request waited for a fetch and
// is to be decided again.
func (e *engine) miss(q *rq) (again bool) {
	if q.collapsed || e.isVolatileFor(q) {
		e.forward(q, mainclient.KindForward, "", nil)
		return false
	}
	if q.cacheKey != q.baseKey {
		// A fetch that began before this replica knew the Vary of the target leads under the plain key, and its
		// entry is not stored yet, although the names are known since the response arrived: a request that
		// computed the full key meanwhile would otherwise lead a second fetch of the same document.
		if c := e.fetches.running(q.baseKey); c != nil {
			return e.waitLeader(q, c)
		}
	}
	key := q.cacheKey
	c, leader := e.fetches.join(key)
	if !leader {
		return e.waitLeader(q, c)
	}
	if e.leftEntry(q) {
		// The flight this request would have waited for ended between its look at the cache and its join: the
		// leader's entry is there now, so decide again instead of fetching a second time (R-BAS-018, R-BAS-025).
		e.fetches.finish(key, c, nil, nil)
		return true
	}
	held, release := e.remoteLock(q)
	// The flight ends when the request returns, or, for an artifact fill that outlives the request, when the
	// fill is committed or lost: the waiters wait for the main installation's speed, never for a client's.
	q.flight = newHandle(func() {
		if release != nil {
			release()
		}
		e.fetches.finish(key, c, nil, nil)
	})
	defer q.flight.done()
	if !held {
		// Another replica fetches the key: wait for it, then decide again.
		return e.waitRemote(q)
	}
	q.call = c // a fill of the answer is handed to the waiters of this flight (R-BAS-054)
	e.forward(q, mainclient.KindForward, "", nil)
	return false
}

// leftEntry reports whether a fetch that ended after this request looked at the cache has left an entry for it. The
// request decided "miss" on what it read before, and only a request that joins as the leader can have missed the
// end of a flight that way: the leader stores its entry before it ends the flight, so a flight that is gone has
// left its entry. The Vary of the target is learned again first, because the entry is kept under the key it names.
func (e *engine) leftEntry(q *rq) bool {
	e.computeKey(q)
	e.learnVaryFromCache(q.ctx(), q)
	_, ok := e.loadEntry(q.ctx(), q.cacheKey)
	return ok
}

// waitLeader waits for the fetch another request of this replica leads for the same cache key. Once the leader's
// answer became a fill of the request's cache key, the request follows the fill from its spool on its own permission
// and does not wait for its end (R-BAS-054, Q-BAS-172). A wait for the leader's answer that outlasts collapseWait is
// not extended and not turned into a request of its own, which would multiply the load on a main installation that is
// slow: it answers 503 busy with Retry-After, and the retry finds the entry or joins the fetch again
// (R-BAS-025, Q-BAS-123).
func (e *engine) waitLeader(q *rq, c *call) (again bool) {
	t := time.NewTimer(e.collapseWait)
	defer t.Stop()
	started := c.started
	for {
		select {
		case <-c.done:
			return true
		case <-started:
			// The fill's bytes answer the request only when they are the variant of its own cache key.
			started = nil
			e.computeKey(q)
			e.learnVaryFromCache(q.ctx(), q)
			if c.run.key != q.cacheKey {
				continue
			}
			if !e.followFill(q, c.run) {
				return false
			}
			// The fill was gone, or the caller may not follow it: decide again once the flight ended.
			select {
			case <-c.done:
				return true
			case <-q.ctx().Done():
				q.finish(metrics.ClassPass, metrics.ResultError)
				return false
			}
		case <-q.ctx().Done():
			q.finish(metrics.ClassPass, metrics.ResultError)
			return false
		case <-t.C:
			e.refuse(q, errBusy(), metrics.RefusedBusy)
			return false
		}
	}
}

// isVolatileFor reports whether recent fetches of the request's cache key left nothing shareable (a search, a
// document too large to keep) or nothing shareable for this caller (a private document, a refusal): such a fetch
// is not collapsed, so no waiter pays a second round trip for nothing. A mark that depends on the caller is the
// caller's own and never switches collapsing off for anyone else (Q-BAS-125).
func (e *engine) isVolatileFor(q *rq) bool {
	return e.isVolatile(q.cacheKey) || e.isVolatile(volatileKeyOf(q))
}

// isVolatile reports whether the mark of a key is set and has not run out.
func (e *engine) isVolatile(key string) bool {
	until, ok := e.volatile.get(key)
	return ok && e.now().Before(until)
}

// volatileKeyOf is the mark of the caller for the request's cache key; a cache key starts with "/", a
// credential key never does, so the two kinds of mark cannot meet.
func volatileKeyOf(q *rq) string { return q.cred.key + "|" + q.cacheKey }

// markVolatile marks the cache key for every caller: what the main installation answered does not depend on
// who asked.
func (e *engine) markVolatile(key string) { e.volatile.put(key, e.now().Add(volatileTTL), 0) }

// markVolatileFor marks the cache key for this caller alone: the answer depended on the caller's rights.
func (e *engine) markVolatileFor(q *rq) {
	e.volatile.put(volatileKeyOf(q), e.now().Add(volatileTTL), 0)
}

// remoteLock takes the fetch lock across replicas when Valkey and the bucket backend make a fetch of one
// replica useful to the others (R-BAS-025). held is true when the caller fetches; a lock that cannot be taken
// for any reason other than another holder only makes this replica fetch itself.
func (e *engine) remoteLock(q *rq) (held bool, release func()) {
	if !e.remoteCollapsing() {
		return true, nil
	}
	ok, rel, err := e.d.Shared.Flights.TryAcquire(q.ctx(), hashKey(q.cacheKey))
	if err != nil {
		// Valkey could not be asked: held is true and the lock is an optimization, so this replica fetches
		// itself (R-BAS-034).
		e.log.Debug("the fetch lock could not be asked; fetching without it", "error", err)
		return true, rel
	}
	return ok, rel
}

func (e *engine) remoteCollapsing() bool {
	return e.cfg.Cache.Backend == "bucket" && e.d.Shared != nil && e.d.Shared.Flights != nil && e.d.Shared.Shared != nil && e.d.Shared.Shared()
}

// remoteWaitMax is how long a replica waits for another replica's fetch when bastion.cache.fill_wait_max is not set:
// the lifetime of the lock.
const remoteWaitMax = 30 * time.Second

// waitRemote waits until the fetch lock of the key is free, then reports true so the request is decided again: a hit
// once the other replica's fill committed. The holder extends its lock while its fill runs (cluster.FlightLock), so
// the wait lasts as long as the fill, sending nothing, up to bastion.cache.fill_wait_max; then the replica fetches
// itself (R-BAS-054, H-1245). A lock whose holder is gone expires and is free.
func (e *engine) waitRemote(q *rq) (again bool) {
	deadline := time.NewTimer(e.fillWaitMax)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-q.ctx().Done():
			q.finish(metrics.ClassPass, metrics.ResultError)
			return false
		case <-deadline.C:
			e.forward(q, mainclient.KindForward, "", nil)
			return false
		case <-tick.C:
			ok, rel, err := e.d.Shared.Flights.TryAcquire(q.ctx(), hashKey(q.cacheKey))
			if err != nil || ok {
				// The lock is free, or Valkey cannot say (then it is an optimization that is skipped).
				if rel != nil {
					rel()
				}
				return true
			}
		}
	}
}
