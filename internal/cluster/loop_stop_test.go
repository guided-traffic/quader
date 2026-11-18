package cluster

import (
	"context"
	"testing"
	"time"
)

// R-ARCH-024 step 6, Q-ARCH-503: the stop of a control loop waits for the call in flight and cuts it only when the stop's own
// context ends; a loop starts no call once the stop has begun.
func TestR_ARCH_024_ALoopStopWaitsForTheCallInFlight(t *testing.T) {
	// loop runs calls on life.ctx until the stop; each call reports when it has begun and blocks until finish is closed or its
	// context ends, and reports how it ended.
	type call struct{ cut bool }
	start := func(life *loopLife, finish <-chan struct{}) (began chan struct{}, ended chan call) {
		began, ended = make(chan struct{}, 8), make(chan call, 8)
		life.run(func() {
			for {
				select {
				case <-life.stopping:
					return
				default:
				}
				if life.stopped() {
					return
				}
				began <- struct{}{}
				select {
				case <-finish:
					ended <- call{}
				case <-life.ctx.Done():
					ended <- call{cut: true}
					return
				}
			}
		})
		return began, ended
	}

	t.Run("a call in flight ends normally and the stop returns after it", func(t *testing.T) {
		life := newLoopLife(context.Background())
		finish := make(chan struct{})
		began, ended := start(life, finish)
		<-began
		stopped := make(chan struct{})
		go func() { life.stop(context.Background()); close(stopped) }()
		select {
		case <-stopped:
			t.Fatal("the stop returned while the call was in flight")
		case c := <-ended:
			t.Fatalf("the call ended before it was let finish (cut %v)", c.cut)
		case <-time.After(100 * time.Millisecond):
		}
		close(finish)
		<-stopped
		if c := <-ended; c.cut {
			t.Fatal("the call in flight was cut")
		}
		select {
		case <-began:
			t.Fatal("a call began after the stop")
		default:
		}
	})

	t.Run("a call still running when the stop's context ends is cut then", func(t *testing.T) {
		life := newLoopLife(context.Background())
		began, ended := start(life, make(chan struct{}))
		<-began
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		life.stop(ctx)
		if c := <-ended; !c.cut {
			t.Fatal("the call was not cut at the stop's deadline")
		}
	})

	t.Run("a stop with an ended context cuts at once", func(t *testing.T) {
		life := newLoopLife(context.Background())
		began, ended := start(life, make(chan struct{}))
		<-began
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		life.stop(ctx)
		if c := <-ended; !c.cut {
			t.Fatal("the call was not cut")
		}
	})
}
