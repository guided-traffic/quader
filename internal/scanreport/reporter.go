package scanreport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Status is what a scanner tells of itself at a report: its state and the reason a readiness check fails, the version of its tool, the
// signature database it scans with, and its slots.
type Status struct {
	State, Reason, ToolVersion, Database string
	// DatabaseBuiltAt is the build time of the daily the updater's directory holds (the kind freshclam), or of the image scanner's
	// database (the kind trivy, Q-OPS-225); zero for the kind clamav and before a database.
	DatabaseBuiltAt time.Time
	InUse, Total    int
	// Calls are a malware scanner's calls in flight (Q-OPS-460); nil for the other kinds.
	Calls []Call
}

// Identity names the replica in its reports.
type Identity struct {
	// UID is the process's instance UID, the key of its row; Name, Node, Version and Revision as in [Report].
	UID, Name, Node, Version, Revision string
}

// Options configure [NewReporter].
type Options struct {
	// URL is the origin of the scan listener: vulnerabilities.images.registry.url, or malware.scan_listener.url.
	URL string
	// HTTP is the scanner's client of the scan listener, which presents the certificate of its kind (outbound.LinkClient).
	HTTP     *http.Client
	Identity Identity
	// Status reads the scanner's state at each report.
	Status func(ctx context.Context) Status
	// Interval is system.heartbeat_interval; zero is ten seconds.
	Interval time.Duration
	// Timeout bounds one request; zero is five seconds.
	Timeout time.Duration
	// SignOffFor bounds the tries of the sign-off at Stop; zero is SignOffWindow. Stop's context ends them earlier.
	SignOffFor time.Duration
	Log        *slog.Logger

	// after waits between two tries of the sign-off; nil is time.After. A test hands in its own clock.
	after func(time.Duration) <-chan time.Time
	// now reads the clock the sign-off's bound is counted on; nil is time.Now.
	now func() time.Time
}

// SignOffWindow is how long a stopping scanner keeps trying to sign off (Q-HA-495): the scan listener of packages may be between two
// replicas of its own (an upgrade rolls packages and the scanners at once), and a sign-off that does not arrive leaves the row of a
// replica that ended in order listed as lost. The pause between two tries doubles from signOffPause up to signOffPauseMax.
const (
	SignOffWindow   = 10 * time.Second
	signOffPause    = 250 * time.Millisecond
	signOffPauseMax = 2 * time.Second
)

// Reporter sends a scanner's reports (R-OPS-048).
type Reporter struct {
	o        Options
	mu       sync.Mutex
	draining bool
	// failing says a report failed since the last that passed: the first failure is logged, the rest until one passes are not.
	failing bool
	// flight is the report of Drain, which Stop waits for before it signs off.
	flight sync.WaitGroup
	cancel context.CancelFunc
	done   chan struct{}
}

// NewReporter returns a reporter that has sent nothing; Start begins its loop.
func NewReporter(o Options) *Reporter {
	if o.Interval <= 0 {
		o.Interval = 10 * time.Second
	}
	if o.Timeout <= 0 {
		o.Timeout = 5 * time.Second
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.SignOffFor <= 0 {
		o.SignOffFor = SignOffWindow
	}
	if o.after == nil {
		o.after = time.After
	}
	if o.now == nil {
		o.now = time.Now
	}
	return &Reporter{o: o}
}

// Start sends the first report at once and then one every interval, until Stop.
func (r *Reporter) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.cancel, r.done = cancel, make(chan struct{})
	go func() {
		defer close(r.done)
		t := time.NewTicker(r.o.Interval)
		defer t.Stop()
		for {
			r.Send(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// Drain marks the replica draining and sends that report at once (R-ARCH-024 step 1), without waiting for it: the row says so until
// the sign-off. The scan listener keeps draining as the last state of a row, so a report of the loop that overtakes it changes nothing.
func (r *Reporter) Drain() {
	r.mu.Lock()
	r.draining = true
	r.mu.Unlock()
	r.flight.Add(1)
	go func() {
		defer r.flight.Done()
		r.Send(context.Background())
	}()
}

// Stop waits for the report of Drain, ends the loop and signs off: the row is deleted. The sign-off is tried again while the scan
// listener cannot be reached or answers 5xx or 429, for SignOffFor and within ctx (Q-HA-495); a sign-off that never arrives leaves the
// row in its last state: draining, which the list stops showing once its heartbeat is stale (Q-OPS-210), or the state before when the
// draining report did not arrive either, which is listed as lost (H-1995).
func (r *Reporter) Stop(ctx context.Context) {
	sent := make(chan struct{})
	go func() {
		r.flight.Wait()
		close(sent)
	}()
	select {
	case <-sent:
	case <-ctx.Done():
	}
	if r.cancel != nil {
		r.cancel()
		select {
		case <-r.done:
		case <-ctx.Done():
		}
	}
	if err := r.signOff(ctx); err != nil {
		r.o.Log.WarnContext(ctx, "the scanner's sign-off did not reach the scan listener; its row is listed until its heartbeat is stale", "error", err)
	}
}

// signOff sends the DELETE until the scan listener takes it (204, or 404 for a row it does not hold), refuses it for good (any other
// 4xx: another try would be refused alike), or the bound or ctx ends the tries; it returns the last failure.
func (r *Reporter) signOff(ctx context.Context) error {
	until := r.o.now().Add(r.o.SignOffFor)
	pause := signOffPause
	for {
		res, err := r.do(ctx, http.MethodDelete, nil)
		if err == nil {
			_ = res.Body.Close()
			switch code := res.StatusCode; {
			case code == http.StatusNoContent || code == http.StatusNotFound:
				return nil
			case code < 500 && code != http.StatusTooManyRequests && code != http.StatusRequestTimeout:
				return fmt.Errorf("the scan listener answered %d", code)
			default:
				err = fmt.Errorf("the scan listener answered %d", code)
			}
		}
		if ctx.Err() != nil || !r.o.now().Add(pause).Before(until) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-r.o.after(pause):
		}
		pause = min(2*pause, signOffPauseMax)
	}
}

// Send sends one report now.
func (r *Reporter) Send(ctx context.Context) {
	st := r.o.Status(ctx)
	r.mu.Lock()
	if r.draining {
		st.State, st.Reason = StateDraining, ""
	}
	r.mu.Unlock()
	id := r.o.Identity
	rep := Report{
		Name: id.Name, Node: id.Node, Version: id.Version, Revision: id.Revision, State: st.State, Reason: st.Reason,
		ToolVersion: st.ToolVersion, Database: st.Database, Slots: Slots{InUse: st.InUse, Total: st.Total}, Calls: st.Calls,
	}
	if !st.DatabaseBuiltAt.IsZero() {
		at := st.DatabaseBuiltAt.UTC()
		rep.DatabaseBuiltAt = &at
	}
	body, err := json.Marshal(rep)
	if err == nil {
		var res *http.Response
		if res, err = r.do(ctx, http.MethodPut, body); err == nil {
			_ = res.Body.Close()
			if res.StatusCode != http.StatusNoContent {
				err = fmt.Errorf("the scan listener answered %d", res.StatusCode)
			}
		}
	}
	r.note(ctx, err)
}

// note logs the first failed report of a run and the report that ends it.
func (r *Reporter) note(ctx context.Context, err error) {
	if ctx.Err() != nil {
		return
	}
	r.mu.Lock()
	first, recovered := err != nil && !r.failing, err == nil && r.failing
	r.failing = err != nil
	r.mu.Unlock()
	switch {
	case first:
		r.o.Log.WarnContext(ctx, "the scanner's report of itself did not reach the scan listener: the administrator sees its row as lost until one does",
			"error", err)
	case recovered:
		r.o.Log.InfoContext(ctx, "the scanner's report of itself reaches the scan listener again")
	}
}

func (r *Reporter) do(ctx context.Context, method string, body []byte) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, r.o.Timeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(r.o.URL, "/")+Path(r.o.Identity.UID), rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := r.o.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	// The body is read before the context's cancel: a report's answer is empty or a short problem.
	b, rerr := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	_ = res.Body.Close()
	res.Body = io.NopCloser(bytes.NewReader(b))
	if rerr != nil {
		return nil, rerr
	}
	return res, nil
}
