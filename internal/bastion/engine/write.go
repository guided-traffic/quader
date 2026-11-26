package engine

import (
	"bytes"
	"io"

	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// write is the flow of a method other than GET and HEAD (R-BAS-095). A read route that takes a body, as the format table's
// readWithBody lists it, is forwarded with its body and the caller's credential. Every other request is a write: the Bastion
// refuses it itself, 403 read_only in the format's error shape, before it reads a byte of the body or looks at the credential,
// and asks the main installation nothing (Q-BAS-336). The anonymous token request of the registry root never comes here: the
// Bastion answers it before (R-BAS-094).
func (e *engine) write(q *rq) {
	if !q.facts.ReadsWithBody(q.r.Method, q.r.URL.EscapedPath()) {
		e.refuse(q, errReadOnly(), metrics.RefusedReadOnly)
		return
	}
	if max := int64(e.cfg.Main.MaxForwardedBody); q.r.ContentLength > max {
		e.refuse(q, errTooLarge(), metrics.RefusedReadOnly)
		return
	}
	if q.form != nil && !e.stepCredentials(q) {
		// The token service's form: its password is refused before anything is sent when it is malformed (R-BAS-051).
		return
	}
	if !e.d.Link.Reachable() {
		e.unreachable(q)
		return
	}
	e.forwardWithBody(q)
}

// forwardWithBody forwards a read route that takes a body (R-BAS-095):
// the caller's credential and the body, at most max_forwarded_body. The answer is pass: nothing is kept.
func (e *engine) forwardWithBody(q *rq) {
	if q.form == nil && !e.stepCredentials(q) {
		return
	}
	limit := int64(e.cfg.Main.MaxForwardedBody)
	fr := e.forwardRequest(q)
	fr.Method = q.r.Method
	body, length, ok := e.requestBody(q, limit)
	if !ok {
		e.refuse(q, errTooLarge(), metrics.RefusedReadOnly)
		return
	}
	fr.ContentLength = length
	if v := e.d.Guard.Miss(q.ctx(), q.addr, guard.Credentialed(!q.cred.anonymous)); !v.Allowed {
		e.refuseVerdict(q, v)
		return
	}
	resp, err := e.d.Main.Do(q.ctx(), mainclient.Forward{
		Request: fr, Class: e.budgetClass(q), Kind: mainclient.KindForward, ClientAddress: q.clientText, Body: body,
	})
	if err != nil {
		e.mainError(q, err)
		return
	}
	defer resp.Body.Close()
	q.mainRequestID = resp.Header.Get("X-Request-Id")
	if !resp.Marked && resp.Status >= 500 {
		e.fail(q, mainclient.ErrUnreachable)
		return
	}
	if err := e.locationError(resp.Header); err != nil {
		e.mainError(q, err)
		return
	}
	e.passThrough(q, resp, nil, metrics.ClassPass, metrics.ResultMiss)
}

// requestBody returns the caller's body for the real request: streamed when its length is known, held in
// memory up to the limit when it is not (H-448).
func (e *engine) requestBody(q *rq, limit int64) (r io.Reader, length int64, ok bool) {
	if q.form != nil {
		return bytes.NewReader(q.form), int64(len(q.form)), true
	}
	if q.r.ContentLength >= 0 {
		return io.LimitReader(q.r.Body, q.r.ContentLength), q.r.ContentLength, true
	}
	b, err := io.ReadAll(io.LimitReader(q.r.Body, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, 0, false
	}
	return bytes.NewReader(b), int64(len(b)), true
}
