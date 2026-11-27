package app

import (
	"reflect"
	"testing"
)

// TestR_OPS_024_TheSchedulersReadOnlyCheckReadsNothingTheWiringStillWrites: the job scheduler asks whether the installation is in
// read-only maintenance mode at its first tick, which runs on the lease manager's goroutine as soon as the replica holds the
// scheduler lease, while the wiring still puts the later steps' results into the replica's parts. The answer reads the store
// wireInfra hands over, never those parts (the same start-up crash as the catalogue's trigger). Run with -race. Milliseconds, no
// data: no store is built yet, so the answer is "not known" without a query, and the scheduler holds its mutating tasks back
// (jobs' TestR_OPS_024_NoMutatingTaskIsEnqueuedBeforeTheReadOnlyModeIsKnown).
func TestR_OPS_024_TheSchedulersReadOnlyCheckReadsNothingTheWiringStillWrites(t *testing.T) {
	a := appFor(t, "worker")
	asked := make(chan [2]bool)
	go func() {
		readOnly, known := a.readOnlyMode(t.Context())
		asked <- [2]bool{readOnly, known}
	}()
	for {
		select {
		case got := <-asked:
			if got[0] || got[1] {
				t.Fatalf("read-only %v, known %v before the store of the system state is built; want neither", got[0], got[1])
			}
			return
		default:
			// A later step of the wiring puts its result.
			a.parts.put(reflect.ValueOf(sessionsArea{}))
		}
	}
}
