package cluster

import (
	"context"
	"sync"
)

// loopLife is the life of the background loops of one component whose calls run on the control connection: the change-log poll,
// the heartbeat of the registry and the count of the fetch share (Q-ARCH-503). Their stop is a step of the close phase
// (R-ARCH-024 step 6), and a stop that cancelled the loops' context cut a statement a loop happened to have in flight on a database
// that answered: the driver closed the control connection, the pool discarded it and the server was sent a cancel request, once in
// every few hundred stops (F-136). A stop now ends the loops between their calls: no loop starts a call after it, a call in flight
// is waited for, and only a call still running when the stop's own context ends, a database that does not answer, is cut.
type loopLife struct {
	// ctx is the context of the loops' calls; it ends when the stop gives up waiting for them.
	ctx    context.Context
	cancel context.CancelFunc
	// stopping is closed by stop: a loop returns instead of starting another call.
	stopping chan struct{}
	once     sync.Once
	wg       sync.WaitGroup
}

// newLoopLife starts the life of a component's loops, detached from parent's end: only stop ends them.
func newLoopLife(parent context.Context) *loopLife {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	return &loopLife{ctx: ctx, cancel: cancel, stopping: make(chan struct{})}
}

// run starts one loop of the component.
func (l *loopLife) run(loop func()) {
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		loop()
	}()
}

// stopped reports whether stop has begun: a loop asks it before every call, so a tick that raced the stop starts none.
func (l *loopLife) stopped() bool {
	select {
	case <-l.stopping:
		return true
	default:
	}
	return false
}

// stop ends the loops: none starts another call, the calls in flight are waited for until ctx ends and cut then. It returns when
// every loop has returned. A second call waits as the first does.
func (l *loopLife) stop(ctx context.Context) {
	l.once.Do(func() { close(l.stopping) })
	done := make(chan struct{})
	go func() { l.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		l.cancel()
		<-done
	}
	l.cancel()
}
