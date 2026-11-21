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
	// DatabaseBuiltAt is the build time of the daily the updater's directory holds (the kind freshclam); zero for every other kind.
	DatabaseBuiltAt time.Time
	InUse, Total    int
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
	Log     *slog.Logger
}

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

// Stop waits for the report of Drain, ends the loop and signs off: the row is deleted. A sign-off that does not reach the scan
// listener leaves the row draining, and the list stops showing it once its heartbeat is stale (Q-OPS-210).
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
	res, err := r.do(ctx, http.MethodDelete, nil)
	if err == nil {
		_ = res.Body.Close()
		if res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusNotFound {
			return
		}
		err = fmt.Errorf("the scan listener answered %d", res.StatusCode)
	}
	r.o.Log.WarnContext(ctx, "the scanner's sign-off did not reach the scan listener; its row is listed until its heartbeat is stale", "error", err)
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
		ToolVersion: st.ToolVersion, Database: st.Database, Slots: Slots{InUse: st.InUse, Total: st.Total},
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
