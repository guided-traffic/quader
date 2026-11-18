//go:build integration

package postgres

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
)

// stallProxy stands between a replica and the stack's PostgreSQL and stands for a database that does not answer: while it is
// stalled it accepts connections and reads what the client sends, and forwards nothing in either direction and dials nothing,
// until it resumes. A server that is starved, frozen or failing over looks the same from the client. The stack's own PostgreSQL
// stays untouched, so the other tests of the stack are not affected.
type stallProxy struct {
	t        *testing.T
	ln       net.Listener
	upstream string

	mu   sync.Mutex
	gate chan struct{} // closed while the proxy forwards; open while it is stalled
}

func newStallProxy(t *testing.T, upstream string) *stallProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &stallProxy{t: t, ln: ln, upstream: upstream, gate: make(chan struct{})}
	close(p.gate)
	t.Cleanup(func() { p.resume(); _ = ln.Close() })
	go p.accept()
	return p
}

func (p *stallProxy) port() int { return p.ln.Addr().(*net.TCPAddr).Port }

func (p *stallProxy) open() chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gate
}

func (p *stallProxy) stall() {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.gate:
		p.gate = make(chan struct{})
	default:
	}
}

func (p *stallProxy) resume() {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.gate:
	default:
		close(p.gate)
	}
}

func (p *stallProxy) accept() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		go p.serve(c)
	}
}

func (p *stallProxy) serve(c net.Conn) {
	defer c.Close()
	<-p.open()
	up, err := net.Dial("tcp", p.upstream)
	if err != nil {
		return
	}
	defer up.Close()
	done := make(chan struct{}, 2)
	go func() { p.pump(up, c); done <- struct{}{} }()
	go func() { p.pump(c, up); done <- struct{}{} }()
	<-done
}

// pump copies from src to dst and holds every chunk it read while the proxy is stalled.
func (p *stallProxy) pump(dst, src net.Conn) {
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			<-p.open()
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if cw, ok := dst.(*net.TCPConn); ok {
					_ = cw.CloseWrite()
				}
			}
			return
		}
	}
}

// stalledConfig is a migrated database reached through a stall proxy, with a connect timeout of 1 s: the bound of every connection
// setup, the cancel request of a discarded connection among them (Q-ARCH-498).
func stalledConfig(t *testing.T) (config.Database, *stallProxy) {
	t.Helper()
	st := getStack(t)
	cfg, _, _ := migrated(t, st)
	p := newStallProxy(t, net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	cfg.Port = p.port()
	cfg.ConnectTimeout = time.Second
	return cfg, p
}

// R-ARCH-019, R-HA-011, Q-ARCH-498, H-822: a statement on the control connection that its context cuts while the database does not
// answer gives the control pool's one place back within the connect timeout, not after the driver's cleanup of up to 15 s that
// waits for the same database; once the database answers, the control connection serves again within the connect timeout.
func TestR_ARCH_019_ACutControlStatementGivesThePlaceBackWhileTheDatabaseDoesNotAnswer(t *testing.T) {
	cfg, p := stalledConfig(t)
	db, err := postgres.Open(ctxT(t, 30*time.Second), cfg, "pod-stall-place", meta.RoleWeb, quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Ready(ctxT(t, 10*time.Second)); err != nil {
		t.Fatalf("the control connection before the stall: %v", err)
	}
	p.stall()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	_, err = db.ControlPool().Exec(ctx, "SELECT 1")
	cancel()
	if err == nil {
		t.Fatal("a statement answered while the database was stalled")
	}
	cut := time.Now()
	for db.Stats().Control.Acquired != 0 {
		if time.Since(cut) > cfg.ConnectTimeout {
			t.Fatalf("the cut connection still held the control pool's place %s after the cut (connect timeout %s): %+v",
				time.Since(cut).Round(time.Millisecond), cfg.ConnectTimeout, db.Stats().Control)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Logf("the place was free %s after the cut, with the database still stalled", time.Since(cut).Round(time.Millisecond))
	p.resume()
	back := time.Now()
	var last error
	for {
		actx, acancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
		last = db.Ready(actx)
		acancel()
		if last == nil {
			break
		}
		if time.Since(back) > 2*cfg.ConnectTimeout {
			t.Fatalf("the control connection did not serve again within %s of the database answering: %v", 2*cfg.ConnectTimeout, last)
		}
	}
	t.Logf("the control connection served again %s after the database answered", time.Since(back).Round(time.Millisecond))
}

// R-ARCH-024 step 6, R-HA-011, Q-ARCH-499: a stop while the database does not answer, with no connection checked out by anyone,
// ends within its budget and without an error: what is left is the driver's cleanup of a connection already closed and the opening
// of a new one, which the exit of the process ends.
func TestR_ARCH_024_AStopWhileTheDatabaseDoesNotAnswerEndsWithinItsBudget(t *testing.T) {
	cfg, p := stalledConfig(t)
	db, err := postgres.Open(ctxT(t, 30*time.Second), cfg, "pod-stall-stop", meta.RoleWeb, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ready(ctxT(t, 10*time.Second)); err != nil {
		t.Fatalf("the control connection before the stall: %v", err)
	}
	p.stall()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	_, err = db.ControlPool().Exec(ctx, "SELECT 1")
	cancel()
	if err == nil {
		t.Fatal("a statement answered while the database was stalled")
	}
	// The budget of the close phase: twice the connect timeout, far below the 15 s the driver's cleanup may take.
	budget := 2 * cfg.ConnectTimeout
	cctx, ccancel := context.WithTimeout(context.Background(), budget)
	defer ccancel()
	start := time.Now()
	err = db.CloseContext(cctx)
	took := time.Since(start)
	if err != nil {
		t.Fatalf("the stop ended with %v after %s", err, took.Round(time.Millisecond))
	}
	if took >= budget {
		t.Fatalf("the stop took %s, its whole budget", took.Round(time.Millisecond))
	}
	t.Logf("the stop ended after %s with the database still stalled (budget %s)", took.Round(time.Millisecond), budget)
}

// Q-ARCH-498: a discarded connection's statement is still ended on the server. The cancel request goes out on a connection of its
// own, outside the pool, bounded by the connect timeout; the server's backend of the cut statement is gone within that bound and
// does not run the statement to its end.
func TestR_ARCH_019_TheStatementOfADiscardedConnectionEndsOnTheServer(t *testing.T) {
	cfg, _ := stalledConfig(t)
	admin := getStackAdmin(t, getStack(t), cfg)
	db, err := postgres.Open(ctxT(t, 30*time.Second), cfg, "pod-stall-cancel", meta.RoleWeb, quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	var pid int32
	if err := db.ControlPool().QueryRow(ctxT(t, 10*time.Second), "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	_, err = db.ControlPool().Exec(ctx, "SELECT pg_sleep(60)")
	cancel()
	if err == nil {
		t.Fatal("the sleep was not cut")
	}
	cut := time.Now()
	for scalar[int64](t, admin, "SELECT count(*) FROM pg_stat_activity WHERE pid = $1", pid) != 0 {
		if time.Since(cut) > cfg.ConnectTimeout {
			t.Fatalf("the backend %d of the cut statement still ran %s after the cut", pid, time.Since(cut).Round(time.Millisecond))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("the backend of the cut statement was gone %s after the cut", time.Since(cut).Round(time.Millisecond))
}
