package bastionmain

import (
	"context"
	"errors"
	"sync"
)

// ErrHardDisabled is the cause a forward in flight is cancelled with when its Bastion is disabled hard (R-BAS-133).
var ErrHardDisabled = errors.New("bastionmain: the Bastion was disabled hard")

// InFlight holds the forwards each Bastion has in flight on this replica (R-BAS-133, Q-BAS-513): a hard disable cancels them, so a
// running download through the Bastion ends within the snapshot's bound (2 s) of the disable. It is safe for concurrent use; a nil
// InFlight tracks nothing.
type InFlight struct {
	mu sync.Mutex
	by map[string]map[*flight]struct{}
}

type flight struct{ cancel context.CancelCauseFunc }

// NewInFlight returns an empty set.
func NewInFlight() *InFlight { return &InFlight{by: map[string]map[*flight]struct{}{}} }

// Track adds a forward of the Bastion bastionID: the context it is served with, which [InFlight.Cancel] cancels, and the function that
// removes it again when the forward is answered.
func (f *InFlight) Track(ctx context.Context, bastionID string) (context.Context, func()) {
	if f == nil {
		return ctx, func() {}
	}
	ctx, cancel := context.WithCancelCause(ctx)
	fl := &flight{cancel: cancel}
	f.mu.Lock()
	set := f.by[bastionID]
	if set == nil {
		set = map[*flight]struct{}{}
		f.by[bastionID] = set
	}
	set[fl] = struct{}{}
	f.mu.Unlock()
	return ctx, func() {
		f.mu.Lock()
		delete(set, fl)
		if cur, ok := f.by[bastionID]; ok && len(cur) == 0 {
			delete(f.by, bastionID)
		}
		f.mu.Unlock()
		cancel(nil)
	}
}

// Cancel cancels every forward of the Bastion bastionID in flight with [ErrHardDisabled] and reports how many there were.
func (f *InFlight) Cancel(bastionID string) int {
	if f == nil {
		return 0
	}
	f.mu.Lock()
	set := f.by[bastionID]
	flights := make([]*flight, 0, len(set))
	for fl := range set {
		flights = append(flights, fl)
	}
	f.mu.Unlock()
	for _, fl := range flights {
		fl.cancel(ErrHardDisabled)
	}
	return len(flights)
}

// Len reports how many forwards of the Bastion bastionID are in flight.
func (f *InFlight) Len(bastionID string) int {
	if f == nil {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.by[bastionID])
}
