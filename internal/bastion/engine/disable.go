package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/errs"
)

// The disable of the registration at the replica (R-BAS-133, Q-BAS-514): while the configuration it applies is disabled, every new
// request is answered 503 with the detail of the Bastion's own unavailable and Retry-After retryDisabled, capped by the format, in
// the format's shape; a hard disable also cuts every request in flight and every fill. The replica counts the requests it runs, which
// its status reports as the confirmation's running.

// retryDisabled is the Retry-After of a disabled replica's answer: a constant of the Bastion, not a setting.
const retryDisabled = 300 * time.Second

// errCut is the cause a hard disable cancels a request and a fill with.
var errCut = errors.New("engine: the Bastion was disabled hard")

// errDisabled is a disabled replica's answer: the code and detail of the Bastion's own 503 unavailable (R-BAS-029), which names
// nothing of the main installation and does not say that an administrator acted (R-BAS-099).
func errDisabled() *errs.Error {
	return errs.E(errs.Unavailable, errs.CodeUnavailable, errs.DetailUnavailable, errs.WithRetryAfter(retryDisabled))
}

// operation is the replica's own account of its work: the requests it runs and the context a hard disable cancels.
type operation struct {
	running atomic.Int64
	mu      sync.Mutex
	cut     context.Context
	cancel  context.CancelCauseFunc
}

func newOperation() *operation {
	o := &operation{}
	o.cut, o.cancel = context.WithCancelCause(context.Background())
	return o
}

// current is the context the next hard disable cancels.
func (o *operation) current() context.Context {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cut
}

// cutAll cancels every request and fill bound so far and starts a new context for the ones after.
func (o *operation) cutAll() {
	o.mu.Lock()
	cancel := o.cancel
	o.cut, o.cancel = context.WithCancelCause(context.Background())
	o.mu.Unlock()
	cancel(errCut)
}

// bind returns a context that ends with parent or with the next hard disable, and the function that releases it.
func (o *operation) bind(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	stop := context.AfterFunc(o.current(), func() { cancel(errCut) })
	return ctx, func() { stop(); cancel(nil) }
}

// watchDisable cuts what runs when the applied configuration turns disabled hard (R-BAS-133).
func (e *engine) watchDisable() {
	h := e.d.Link.Configuration()
	if h == nil {
		return
	}
	var prev iface.DisableMode
	h.Watch(func(a *settings.Applied) {
		if a.Disabled == iface.DisableHard && prev != iface.DisableHard {
			e.op.cutAll()
		}
		prev = a.Disabled
	})
}

// stepDisabled answers a request while the configuration the request read at its start is disabled (R-BAS-133), and counts every
// other one as running until it ends.
func (e *engine) stepDisabled(q *rq) bool {
	if a := settings.From(q.ctx()); a != nil && a.Disabled != "" {
		e.refuse(q, errDisabled(), metrics.RefusedDisabled)
		return false
	}
	e.op.running.Add(1)
	q.running = true
	return true
}

// Operation implements [Engine]: the way of the disable the replica applies and the requests it runs (R-BAS-133).
func (e *engine) Operation() iface.Operation {
	out := iface.Operation{Running: e.op.running.Load()}
	if h := e.d.Link.Configuration(); h != nil {
		if d := h.Current().Disabled; d != "" {
			out.Disabled = &d
		}
	}
	return out
}
