package engine

import (
	"net/http"

	"github.com/guided-traffic/quader/internal/bastion/cache"
	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
)

// A HEAD of a document is answered from what the Bastion keeps (R-BAS-021, R-BAS-023, Q-BAS-475): a client that only HEADs a
// path (Docker and containerd resolve a tag so, then GET the manifest by its digest) would otherwise send every HEAD to the main
// installation, because a HEAD's answer has no body to keep. The Bastion knows what a path is only from the marking of the main
// installation's answer, so a HEAD miss is forwarded as a HEAD; when the marking names a document (shared or private) the
// Bastion sends the GET that keeps it and answers the HEAD from that (Q-BAS-476). A HEAD of an artifact stays a HEAD: a GET of
// it would be a download at the main installation. A document the Bastion holds already, as an entry or as the caller's copy,
// is revalidated by a conditional GET, as a GET of it would be.

// fillForHead answers a HEAD the main installation answered 200 for a document by the GET that keeps the document, and the
// HEAD from what that GET kept (Q-BAS-476). It reports false, with nothing written, when the answer is to be passed on as it
// is: the request is no HEAD or was sent as a GET already, the status is not 200, the answer declares no length or one above
// limit (the memory bound of the document's class, Q-BAS-477), its Vary cannot be keyed on, or the GET is not admitted or
// fails before it is answered (Q-BAS-478).
func (e *engine) fillForHead(q *rq, resp *mainclient.Response, limit int64) bool {
	if q.r.Method != http.MethodHead || q.asGet || resp.Status != http.StatusOK {
		return false
	}
	if size := contentLength(resp.Header); size < 0 || size > limit {
		return false
	}
	if _, keep := varyNames(resp.Header); !keep {
		return false
	}
	// The HEAD's answer has no body; its slot is free before the GET takes one.
	_ = resp.Body.Close()
	if !e.admitFill(q) {
		return false
	}
	q.asGet = true
	got, sent, done, err := e.ask(q, mainclient.KindForward, "", nil)
	if err != nil {
		q.asGet = false
		e.log.Debug("the GET that keeps a document a HEAD addressed was not answered; the HEAD's answer is passed on", "error", err)
		return false
	}
	defer done()
	e.process(q, got, sent, nil)
	return true
}

// admitFill admits the GET of fillForHead as a request to the main installation (R-BAS-035, R-BAS-096): the link, the miss
// figure of the caller's address and the subject's upstream requests, as for a forward. A refusal writes nothing: the caller
// receives the HEAD's answer the Bastion holds already (Q-BAS-478).
func (e *engine) admitFill(q *rq) bool {
	if !e.d.Link.Reachable() {
		return false
	}
	if v := e.d.Guard.Miss(q.ctx(), q.addr, guard.Credentialed(!q.cred.anonymous)); !v.Allowed {
		return false
	}
	if e.d.Budgets == nil {
		return true
	}
	if q.charge == nil {
		q.pendingUpstream++
		return true
	}
	return q.charge.Upstream(q.ctx()) == nil
}

// headAsGet reports whether the conditional request of a HEAD is sent as a GET (Q-BAS-476): the caller's copy of a document,
// or an entry of a document within the memory bound of a document. A 304 renews what is kept, a 200 replaces it, as for a
// GET. The conditional request of an artifact, and of a document spooled above the memory bound, stays a HEAD.
func (e *engine) headAsGet(q *rq, ent *cache.Entry, priv *privDoc) bool {
	if q.r.Method != http.MethodHead {
		return false
	}
	return priv != nil || (ent != nil && ent.Kind == cache.EntryDocument && ent.Size <= e.docMemory)
}
