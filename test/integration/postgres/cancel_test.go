//go:build integration

package postgres

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// otherInstanceProxy stands for two instances of a pooler behind one Service that are not peers (H-1662). The connections a replica
// holds reach the stack's PgBouncer, the instance that holds the client. Once elsewhere is set, every new connection reaches "the
// other instance", which never forwards anything and is closed: a cancel request opens such a connection and never reaches the
// instance that holds its client. A real instance would first finish the TLS handshake and read the cancel key it does not know;
// the client cannot tell the two apart, and the cancel request is encrypted like its connection (pgx sends it over TLS), so the proxy
// cannot pick it out by its bytes.
type otherInstanceProxy struct {
	ln        net.Listener
	upstream  string
	elsewhere atomic.Bool
	dropped   atomic.Int64
}

func newOtherInstanceProxy(t *testing.T, upstream string) *otherInstanceProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &otherInstanceProxy{ln: ln, upstream: upstream}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go p.serve(c)
		}
	}()
	return p
}

func (p *otherInstanceProxy) port() int { return p.ln.Addr().(*net.TCPAddr).Port }

func (p *otherInstanceProxy) serve(c net.Conn) {
	defer c.Close()
	if p.elsewhere.Load() {
		p.dropped.Add(1)
		return
	}
	up, err := net.Dial("tcp", p.upstream)
	if err != nil {
		return
	}
	defer up.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, c); done <- struct{}{} }()
	go func() { _, _ = io.Copy(c, up); done <- struct{}{} }()
	// Either side closing ends both: the instance sees its client leave when the replica closes the connection.
	<-done
}

// R-ARCH-019, Q-ARCH-1041, H-1662: a read outside a transaction that passes the request's bound answers database_timeout, and its
// statement ends at the server within client_connection_check_interval although its cancel request reached a pooler instance that
// does not hold the client: the driver closes the connection, the pooler closes the server connection of a client that left in the
// middle of a statement, and the server's check sees its client gone. Without the role default the statement ran on to its own end
// or to the session's statement_timeout (30 s).
func TestR_ARCH_019_AStatementWhoseCancelIsDroppedEndsWhenItsClientIsGone(t *testing.T) {
	st := getStack(t)
	cfg, name, admin := migrated(t, st)
	for _, set := range []string{
		"statement_timeout = '30s'", "lock_timeout = '5s'", "idle_in_transaction_session_timeout = '60s'",
		"client_connection_check_interval = '1s'", "search_path = quader",
	} {
		mustExec(t, admin, fmt.Sprintf("ALTER ROLE %s IN DATABASE %s SET %s", st.user, name, set))
	}
	pooled := st.pooled(cfg)
	proxy := newOtherInstanceProxy(t, net.JoinHostPort(cfg.Host, strconv.Itoa(st.bouncerPort)))
	pooled.Port = proxy.port()
	db := open(t, pooled, "pod-cancel-dropped", meta.RoleWeb)
	read := func(ctx context.Context, sql string) error {
		return db.Read(ctx, func(tx q.DBTX) error {
			_, err := tx.Exec(ctx, sql)
			return err
		})
	}
	// The read's connection is open and idle in the pool; from here every new connection reaches the other instance.
	if err := read(ctxT(t, 10*time.Second), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	proxy.elsewhere.Store(true)

	marker := "h1662-" + name
	err := read(meta.WithStatementTimeout(ctxT(t, 20*time.Second), 300*time.Millisecond), "SELECT pg_sleep(20) /* "+marker+" */")
	cut := time.Now()
	if e, ok := errs.As(err); !ok || e.Code != errs.CodeDatabaseTimeout {
		t.Fatalf("the read past its bound: %v, want database_timeout", err)
	}
	running := func() int64 {
		return scalar[int64](t, admin, `SELECT count(*) FROM pg_stat_activity WHERE state = 'active'
			AND query LIKE '%' || $1::text || '%' AND query NOT LIKE '%pg_stat_activity%'`, marker)
	}
	for running() != 0 {
		if since := time.Since(cut); since > cfg.Session.ClientConnectionCheckInterval+2*time.Second {
			t.Fatalf("the statement still ran %s after its bound; connections that reached the other instance: %d",
				since.Round(time.Millisecond), proxy.dropped.Load())
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Logf("the statement ended %s after its bound; connections that reached the other instance: %d",
		time.Since(cut).Round(time.Millisecond), proxy.dropped.Load())
	if proxy.dropped.Load() == 0 {
		t.Fatal("no cancel request reached the other instance: the case did not stand for a dropped cancel")
	}
}
