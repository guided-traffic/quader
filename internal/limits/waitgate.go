package limits

import "sync/atomic"

// WaitingShare is the share of an endpoint's request slots that downloads waiting for a parallel slot may hold on a replica:
// one in WaitingShare (R-LIM-024, Q-LIM-60, F-163). A waiting download keeps the request slot of step 4 of the chain
// (server.max_in_flight, or the endpoint's own) for up to limits.parallel.wait; without a bound, a burst of callers at their
// parallelTransfers figure fills the slots with requests that only wait, and the replica sheds every new request, another
// caller's too. Three quarters of the slots stay for requests that are served.
const WaitingShare = 4

// WaitGate bounds the downloads of one endpoint of a replica that wait for a parallel slot. The zero value admits nothing;
// a nil gate admits every wait.
type WaitGate struct {
	max int64
	n   atomic.Int64
}

// NewWaitGate returns the gate of an endpoint with slots request slots: at most slots ÷ WaitingShare waiting downloads, at
// least one.
func NewWaitGate(slots int) *WaitGate {
	return &WaitGate{max: max(1, int64(slots)/WaitingShare)}
}

// Max is the most downloads the gate lets wait at once.
func (g *WaitGate) Max() int64 { return g.max }

// Waiting is the downloads that wait now.
func (g *WaitGate) Waiting() int64 { return g.n.Load() }

// enter takes a place to wait; false when the gate's share is held.
func (g *WaitGate) enter() bool {
	if g == nil {
		return true
	}
	if g.n.Add(1) > g.max {
		g.n.Add(-1)
		return false
	}
	return true
}

// leave gives the place back.
func (g *WaitGate) leave() {
	if g != nil {
		g.n.Add(-1)
	}
}
