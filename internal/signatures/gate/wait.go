package gate

import (
	"context"
	"time"
)

// Wait is a download that waits at a gate for the checks of its signatures (R-SIG-067): the priority work is asked, a slot of the
// replica is held, and the download learns of a decision from the signal area signature_verdict or by reading again once a second. A
// serve point that decides the malware gate too waits for both in one wait (R-SIG-069) through the methods; Run is the wait of the gate
// alone.
type Wait struct {
	g        *Gate
	v        *Verdict
	started  time.Time
	deadline time.Time
	ch       chan struct{}
	ended    bool
}

// Begin starts the wait of a pending Verdict: the subjects' pending signatures are due at priority 0, a waiting fetch runs first and the
// job workers are kicked; then the wait takes a slot of the replica. The error is the pending answer when the bound from the request's
// arrival has passed already or every slot is taken, or a store error.
func (g *Gate) Begin(ctx context.Context, v *Verdict) (*Wait, error) {
	now := g.o.Now()
	arrived := v.d.Arrived
	if arrived.IsZero() {
		arrived = now
	}
	var due []Fetch
	for _, f := range v.fetches {
		if f.Queued && !f.Fetched {
			due = append(due, f)
		}
	}
	if err := g.o.Store.GatePrioritize(ctx, v.digests, due); err != nil {
		return nil, err
	}
	if g.o.WakeQueues != nil {
		g.o.WakeQueues(queueSignatures, queueIndex)
	}
	deadline := arrived.Add(g.o.Wait)
	if !deadline.After(now) {
		return nil, g.pending(ctx, v, false)
	}
	select {
	case g.slots <- struct{}{}:
	default:
		return nil, g.pending(ctx, v, false)
	}
	w := &Wait{g: g, v: v, started: now, deadline: deadline, ch: make(chan struct{}, 1)}
	g.mu.Lock()
	for _, d := range v.digests {
		set := g.waiting[d]
		if set == nil {
			set = map[chan struct{}]struct{}{}
			g.waiting[d] = set
		}
		set[w.ch] = struct{}{}
	}
	g.mu.Unlock()
	return w, nil
}

// Deadline is when the wait ends at the latest: signatures.gate.download_wait after the request arrived.
func (w *Wait) Deadline() time.Time { return w.deadline }

// Wake is signalled when a decision may have arrived.
func (w *Wait) Wake() <-chan struct{} { return w.ch }

// Stopped is closed when the replica drains.
func (w *Wait) Stopped() <-chan struct{} { return w.g.stop }

// Poll is how often the wait reads again without a signal.
func (w *Wait) Poll() time.Duration { return w.g.o.Poll }

// Again decides again from the store: Pending while nothing changed.
func (w *Wait) Again(ctx context.Context) (Outcome, error) {
	if err := w.g.judge(ctx, w.v); err != nil {
		return Pending, err
	}
	return w.v.Outcome, nil
}

// Refuse records the refusal the wait ended in and returns its answer.
func (w *Wait) Refuse(ctx context.Context) error { return w.g.Refuse(ctx, w.v) }

// Pending records the pending answer the wait ended in and returns it; a replica that drains records the metric alone.
func (w *Wait) Pending(ctx context.Context, stopped bool) error {
	return w.g.pending(ctx, w.v, stopped)
}

// End releases the slot and the subscription and observes how long the wait took, by its outcome: served, refused or pending.
func (w *Wait) End(outcome string) {
	if w == nil || w.ended {
		return
	}
	w.ended = true
	g := w.g
	g.mu.Lock()
	for _, d := range w.v.digests {
		if set := g.waiting[d]; set != nil {
			delete(set, w.ch)
			if len(set) == 0 {
				delete(g.waiting, d)
			}
		}
	}
	g.mu.Unlock()
	<-g.slots
	if outcome != "" {
		g.o.Metrics.waited(outcome, g.o.Now().Sub(w.started))
	}
}

// Run waits for the gate alone until it decides, the bound passes, the request goes away or the replica drains.
func (w *Wait) Run(ctx context.Context) error {
	outcome := ""
	defer func() { w.End(outcome) }()
	timer := time.NewTimer(time.Until(w.deadline))
	defer timer.Stop()
	poll := time.NewTicker(w.Poll())
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.Stopped():
			outcome = "pending"
			return w.Pending(ctx, true)
		case <-timer.C:
			outcome = "pending"
			return w.Pending(ctx, false)
		case <-w.ch:
		case <-poll.C:
		}
		o, err := w.Again(ctx)
		if err != nil {
			w.g.o.Log.WarnContext(ctx, "a waiting download could not read its signatures", "path", w.v.d.Path, "error", err)
			continue
		}
		switch o {
		case Pass:
			outcome = "served"
			return nil
		case Refused:
			outcome = "refused"
			return w.Refuse(ctx)
		}
	}
}

// Unstored answers a body the remote engine would pass on unstored during an outage of the object store (R-STOR-036): nothing is stored
// to judge, so a gated download is answered pending (R-SIG-067).
func (g *Gate) Unstored(ctx context.Context, d Download) error {
	return g.pending(ctx, &Verdict{d: d}, false)
}

// pending counts the pending answer, records it as the malware gate records its own (at most once a minute per repository, path and
// client address) unless the replica drains, and returns it.
func (g *Gate) pending(ctx context.Context, v *Verdict, stopped bool) error {
	g.o.Metrics.refuse("pending")
	if !stopped {
		g.recordEvent(ctx, v.d, "pending")
	}
	return g.pendingError(v.d)
}
