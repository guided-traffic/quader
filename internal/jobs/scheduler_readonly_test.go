package jobs

import (
	"context"
	"testing"
)

// TestR_OPS_024_NoMutatingTaskIsEnqueuedBeforeTheReadOnlyModeIsKnown: the scheduler's first tick runs as soon as the replica holds
// the scheduler lease, which can be before the start-up has built the store of the system state. A replica that did not know the
// mode yet answered "not read-only" then, and the tick enqueued every mutating task of an installation in read-only maintenance
// mode. The tick holds the mutating tasks back while the mode is read-only or not known; a known writable mode, and a worker
// without a check, hold nothing. Microseconds, no data.
func TestR_OPS_024_NoMutatingTaskIsEnqueuedBeforeTheReadOnlyModeIsKnown(t *testing.T) {
	answer := func(readOnly, known bool) func(context.Context) (bool, bool) {
		return func(context.Context) (bool, bool) { return readOnly, known }
	}
	for _, c := range []struct {
		name string
		ro   func(context.Context) (bool, bool)
		held bool
	}{
		{"the mode is not known yet", answer(false, false), true},
		{"read-only", answer(true, true), true},
		{"writable", answer(false, true), false},
		{"no check", nil, false},
	} {
		w := &Worker{o: WorkerOptions{ReadOnly: c.ro}}
		if got := w.mutatingHeld(t.Context()); got != c.held {
			t.Errorf("%s: mutating tasks held %v, want %v", c.name, got, c.held)
		}
	}
}
