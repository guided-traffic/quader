package postgres

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// A connection the pool gives up on is let go at once (Q-ARCH-498).
//
// The driver closes a connection whose statement was cut by its context and cleans it up in the background: a cancel request on a
// new connection, the terminate message, and a read until the server closes, each bounded by 15 s. pgxpool's destructor waits for
// that cleanup, and the pool keeps the connection's place, counted as acquired, until it ends. With a database that does not answer
// for a while (a failover, a checkpoint storm, a starved container), every step of the cleanup waits, and a pool of one connection,
// the control pool, hands out nothing for up to 15 s; a shutdown waits for it until its close deadline.
//
// A connection we have given up on needs no courteous goodbye. When the pool destroys a connection that is closed, busy or inside a
// transaction, discard closes its socket and aborts the driver's cancel request, so the cleanup has nothing left to wait for and the
// place is free again at once. The cancel request that ends the statement on the server is sent again outside the pool, bounded by
// database.connect_timeout like any connection setup; until the server has it, or the statement ends by itself, the server may count
// one backend more for the replica than the pool does.

// connLife is the state of one pool connection that discard needs: the socket, and the cancel requests of the driver in flight.
type connLife struct {
	cancelBound time.Duration

	mu        sync.Mutex
	connected bool     // the connection is established; a later dial is a cancel request
	raw       net.Conn // the socket of the connection, below TLS
	discarded bool
	inFlight  map[*abortable]struct{} // the driver's cancel requests that have not ended
}

// ownCancel marks the dial of the cancel request that discard sends itself.
type ownCancel struct{}

// errDiscarded is the answer to a cancel request of the driver for a connection that discard has let go: discard sends its own.
var errDiscarded = errors.New("database: the connection was discarded; its cancel request is sent outside the pool")

const lifeKey = "quader.connLife"

// withDiscard installs the state of discard on one connection's configuration: called by the pool for every new connection.
func withDiscard(cc *pgx.ConnConfig, cancelBound time.Duration) {
	st := &connLife{cancelBound: cancelBound, inFlight: map[*abortable]struct{}{}}
	dial := cc.DialFunc
	cc.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return st.dial(ctx, dial, network, addr)
	}
	after := cc.AfterConnect
	cc.AfterConnect = func(ctx context.Context, pc *pgconn.PgConn) error {
		if after != nil {
			if err := after(ctx, pc); err != nil {
				return err
			}
		}
		st.mu.Lock()
		st.connected = true
		st.mu.Unlock()
		pc.CustomData()[lifeKey] = st
		return nil
	}
}

func (st *connLife) dial(ctx context.Context, base pgconn.DialFunc, network, addr string) (net.Conn, error) {
	st.mu.Lock()
	connected, discarded := st.connected, st.discarded
	st.mu.Unlock()
	switch {
	case !connected:
		// The connection itself (or one of its attempts): the last socket that is dialed is the one that connects.
		conn, err := base(ctx, network, addr)
		if err == nil {
			st.mu.Lock()
			st.raw = conn
			st.mu.Unlock()
		}
		return conn, err
	case ctx.Value(ownCancel{}) != nil:
		return base(ctx, network, addr)
	case discarded:
		return nil, errDiscarded
	}
	// The driver's cancel request of a connection it has just closed: tracked, so that discard can end it, also in its dial.
	dctx, cancel := context.WithCancel(ctx)
	a := &abortable{cancel: cancel, st: st}
	st.mu.Lock()
	if st.discarded {
		st.mu.Unlock()
		cancel()
		return nil, errDiscarded
	}
	st.inFlight[a] = struct{}{}
	st.mu.Unlock()
	conn, err := base(dctx, network, addr)
	if err != nil {
		a.done()
		return nil, err
	}
	a.mu.Lock()
	a.Conn = conn
	aborted := a.aborted
	a.mu.Unlock()
	if aborted {
		_ = conn.Close()
	}
	return a, nil
}

// abortable is a cancel request of the driver: its dial context and, once dialed, its socket.
type abortable struct {
	net.Conn
	cancel  context.CancelFunc
	st      *connLife
	mu      sync.Mutex
	aborted bool
}

func (a *abortable) abort() {
	a.cancel()
	a.mu.Lock()
	a.aborted = true
	c := a.Conn
	a.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

func (a *abortable) done() {
	a.cancel()
	a.st.mu.Lock()
	delete(a.st.inFlight, a)
	a.st.mu.Unlock()
}

// Close ends the cancel request's socket and forgets it.
func (a *abortable) Close() error {
	defer a.done()
	return a.Conn.Close()
}

// givenUp reports whether the pool destroys c because it gave up on it: closed by the driver, busy, or left inside a transaction.
// A healthy idle connection (its lifetime, its idle time, the close of the pool) is closed the courteous way.
func givenUp(c *pgx.Conn) bool {
	pc := c.PgConn()
	return pc != nil && (pc.IsClosed() || pc.IsBusy() || pc.TxStatus() != 'I')
}

// discard lets c go at once if the pool gave up on it: it aborts the driver's cancel request, closes the socket, and sends the cancel
// request again outside the pool, bounded by the configured connect timeout. It reports whether it did. It is the pool's BeforeClose.
func discard(c *pgx.Conn) bool {
	if c == nil || !givenUp(c) {
		return false
	}
	pc := c.PgConn()
	st, ok := pc.CustomData()[lifeKey].(*connLife)
	if !ok {
		return false
	}
	st.mu.Lock()
	st.discarded = true
	raw := st.raw
	inFlight := make([]*abortable, 0, len(st.inFlight))
	for a := range st.inFlight {
		inFlight = append(inFlight, a)
	}
	st.mu.Unlock()
	// The cancel request runs on its own; nothing waits for it. It reads the server's address from the connection, which a closed
	// socket still answers.
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), ownCancel{}, true), st.cancelBound)
	go func() {
		defer cancel()
		_ = pc.CancelRequest(ctx)
	}()
	for _, a := range inFlight {
		a.abort()
	}
	if raw != nil {
		_ = raw.Close()
	}
	return true
}
