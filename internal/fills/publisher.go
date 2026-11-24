package fills

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Sink is where a publisher writes; *Store implements it.
type Sink interface {
	Publish(ctx context.Context, p Publication) error
	Sweep(ctx context.Context, olderThan time.Duration) (int, error)
}

// PublisherOptions configure [NewPublisher].
type PublisherOptions struct {
	Tracker *Tracker
	Sink    Sink
	// InstanceUID and Replica name the replica's live row: its instance UID (replica.instance_uid) and pod name.
	InstanceUID, Replica string
	// Interval is proxy.upstream_downloads.publish_interval; zero is two seconds.
	Interval time.Duration
	Log      *slog.Logger
	// Now replaces the clock, for tests.
	Now func() time.Time
}

// Publisher writes a replica's running downloads and its ended ones to PostgreSQL every interval and sweeps the rows of replicas that
// stopped (R-OPS-050, R-OPS-051). It is a component of the composition root: Start begins the loop, Flush writes the last publication
// and removes the live row (the close step of R-ARCH-024), Stop ends the loop.
type Publisher struct {
	o PublisherOptions

	// mu serializes the publications of the loop and of Flush.
	mu sync.Mutex
	// hasRow says the replica's live row exists as far as the publisher knows.
	hasRow    bool
	lastSweep time.Time
	lastWarn  time.Time

	cancel context.CancelFunc
	done   chan struct{}
}

// NewPublisher creates the publisher.
func NewPublisher(o PublisherOptions) *Publisher {
	if o.Interval <= 0 {
		o.Interval = 2 * time.Second
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Publisher{o: o}
}

// Name implements the component.
func (p *Publisher) Name() string { return "upstream-downloads" }

// Start begins the loop of publications. It never fails: a publication that fails is counted, logged at most once a minute, and its
// ended downloads go out with the next one.
func (p *Publisher) Start(context.Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel, p.done = cancel, make(chan struct{})
	go p.loop(ctx)
	return nil
}

func (p *Publisher) loop(ctx context.Context) {
	defer close(p.done)
	t := time.NewTicker(p.o.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.Tick(ctx)
		}
	}
}

// Flush writes the last publication of a replica that shuts down: the ended downloads (the fetches the shutdown cancelled among them)
// and the removal of its live row (R-OPS-050).
func (p *Publisher) Flush(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.o.Tracker.snapshot()
	pub := Publication{InstanceUID: p.o.InstanceUID, Replica: p.o.Replica, DeleteLive: true, Ended: s.Ended}
	if err := p.o.Sink.Publish(ctx, pub); err != nil {
		p.o.Tracker.requeue(s.Ended)
		return err
	}
	p.hasRow = false
	return nil
}

// Stop ends the loop.
func (p *Publisher) Stop(context.Context) error {
	if p.cancel != nil {
		p.cancel()
		<-p.done
	}
	return nil
}

// Tick is one publication (R-OPS-050): a replica with running downloads upserts its live row; one whose last download ended removes it;
// one with nothing running and nothing changed writes nothing. Ended downloads are inserted and the table trimmed. At most every
// MinSweepAge the rows of replicas that stopped publishing are swept (R-OPS-051).
func (p *Publisher) Tick(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.o.Tracker.snapshot()
	pub := Publication{InstanceUID: p.o.InstanceUID, Replica: p.o.Replica, Running: s.Running, Omitted: s.Omitted, Ended: s.Ended}
	if len(s.Running) == 0 {
		pub.DeleteLive = p.hasRow
	}
	if len(pub.Running) > 0 || pub.DeleteLive || len(pub.Ended) > 0 {
		if err := p.o.Sink.Publish(ctx, pub); err != nil {
			p.o.Tracker.requeue(s.Ended)
			p.failed(err)
		} else {
			p.hasRow = len(pub.Running) > 0
		}
	}
	p.sweep(ctx)
}

// sweep ends the downloads of replicas whose live row is older than SweepIntervals intervals, at least MinSweepAge, at most once every
// MinSweepAge.
func (p *Publisher) sweep(ctx context.Context) {
	now := p.o.Now()
	if !p.lastSweep.IsZero() && now.Sub(p.lastSweep) < MinSweepAge {
		return
	}
	p.lastSweep = now
	if n, err := p.o.Sink.Sweep(ctx, max(SweepIntervals*p.o.Interval, MinSweepAge)); err != nil {
		p.failed(err)
	} else if n > 0 {
		p.o.Log.Info("the running downloads of replicas that stopped publishing ended interrupted", "replicas", n)
	}
}

// failed counts a failed publication and warns at most once a minute.
func (p *Publisher) failed(err error) {
	p.o.Tracker.m.failures.Inc()
	now := p.o.Now()
	if p.lastWarn.IsZero() || now.Sub(p.lastWarn) >= time.Minute {
		p.lastWarn = now
		p.o.Log.Warn("publishing the downloads from upstreams failed; the next interval tries again", "error", err)
	}
}
