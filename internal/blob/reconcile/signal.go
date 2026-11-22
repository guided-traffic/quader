package reconcile

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Signal enqueues a run of the reconciliation after a commit that queued a gap (R-STOR-056), so that a new blob reaches its
// other connections within seconds rather than at the next minute's run. A commit calls Notify, which never blocks; one
// goroutine enqueues, at most once per SignalSpacing, so the job table sees at most one statement a second per replica
// whatever the upload rate, and the exclusive key of the kind folds every enqueue into the run that is queued or running.
type Signal struct {
	enqueue func(ctx context.Context) error
	log     *slog.Logger
	ch      chan struct{}
	stop    context.CancelFunc
	done    sync.WaitGroup
}

// SignalSpacing is the least time between two enqueues of one replica.
const SignalSpacing = time.Second

// NewSignal returns a signal that calls enqueue; Start starts it.
func NewSignal(enqueue func(ctx context.Context) error, log *slog.Logger) *Signal {
	if log == nil {
		log = slog.Default()
	}
	return &Signal{enqueue: enqueue, log: log, ch: make(chan struct{}, 1)}
}

// Name implements the composition root's component.
func (s *Signal) Name() string { return "storage-reconcile-signal" }

// Notify asks for a run; a request while one is pending is the same request.
func (s *Signal) Notify() {
	select {
	case s.ch <- struct{}{}:
	default:
	}
}

// Start starts the goroutine that enqueues.
func (s *Signal) Start(ctx context.Context) error {
	ctx, s.stop = context.WithCancel(context.WithoutCancel(ctx))
	s.done.Add(1)
	go func() {
		defer s.done.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.ch:
			}
			ectx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := s.enqueue(ectx); err != nil && ctx.Err() == nil {
				s.log.Warn("the reconciliation could not be signalled after a commit; its next scheduled run copies the blob", "error", err)
			}
			cancel()
			if sleepCtx(ctx, SignalSpacing) != nil {
				return
			}
		}
	}()
	return nil
}

// Stop ends the goroutine; a pending request is left to the scheduled run.
func (s *Signal) Stop(context.Context) error {
	if s.stop != nil {
		s.stop()
	}
	s.done.Wait()
	return nil
}
