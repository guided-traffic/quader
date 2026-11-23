package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// txn is the meta.Tx of one attempt. It carries the hooks and the value slot
// (meta.TxState) and the pgx transaction, which only this package and the
// stores that call Unwrap can reach.
type txn struct {
	meta.TxState
	tx pgx.Tx
}

var _ meta.Tx = (*txn)(nil)

// Unwrap returns the pgx transaction behind a meta.Tx, for the stores of
// infrastructure packages that run sqlc queries on it (q.New(ptx)). It reports
// false for a meta.Tx that this package did not create. Domain code and the
// formats have no use for it: R-ARCH-008 keeps them from running SQL, and the
// architecture test keeps them from importing this package.
func Unwrap(tx meta.Tx) (pgx.Tx, bool) {
	t, ok := tx.(*txn)
	if !ok {
		return nil, false
	}
	return t.tx, true
}

var _ meta.Runner = (*DB)(nil)

// InTx runs fn in a READ COMMITTED transaction on the request pool and repeats
// it after a serialization failure or deadlock (three times) and after a
// failover error (until retry.max_elapsed). fn must be safe to run again: each
// attempt starts a new transaction and the earlier one was rolled back. It runs
// the Before hooks last, inside the transaction, and the After hooks once COMMIT
// succeeded; the OnRollback hooks run when an attempt ends without a commit. A
// retried attempt registers its hooks again; the hooks of a failed attempt are
// dropped.
func (db *DB) InTx(ctx context.Context, fn func(meta.Tx) error) error {
	return db.transact(ctx, db.main, func(ptx pgx.Tx) (*txn, error) {
		t := &txn{tx: ptx}
		if err := fn(t); err != nil {
			return t, err
		}
		return t, t.RunBefore(ctx, t)
	})
}

// Tx is InTx under its earlier name.
//
// It exists only until the callers outside this package (the coordination
// integration tests and the e2e driver) use InTx; do not add new ones.
func (db *DB) Tx(ctx context.Context, fn func(meta.Tx) error) error { return db.InTx(ctx, fn) }

// InPgxTx is InTx for code that needs the pgx transaction itself and no hooks:
// the stores of this package and the tests of the retry logic.
func (db *DB) InPgxTx(ctx context.Context, fn func(pgx.Tx) error) error {
	return db.transact(ctx, db.main, func(ptx pgx.Tx) (*txn, error) { return nil, fn(ptx) })
}

// beginOptions are the options of a READ COMMITTED transaction for ctx. A context with a statement bound
// (meta.WithStatementTimeout, the request path of R-ARCH-019) opens the transaction with SET LOCAL statement_timeout in the
// same simple query as its BEGIN, so the bound costs no round trip of its own and holds through a transaction-mode pooler.
func beginOptions(ctx context.Context) pgx.TxOptions {
	o := pgx.TxOptions{IsoLevel: pgx.ReadCommitted}
	if d, ok := meta.StatementTimeout(ctx); ok {
		o.BeginQuery = "BEGIN ISOLATION LEVEL READ COMMITTED; SET LOCAL statement_timeout = " + strconv.FormatInt(max(d.Milliseconds(), 1), 10)
	}
	return o
}

// Read runs a read on the request pool with the retry and the error mapping of
// InTx: an error that a failover or a restart of the server or pooler produces
// (SQLSTATE 57P01, a refused or timed-out connection, a standby) is retried
// until retry.max_elapsed, and the failure that outlasts it is the database
// being unavailable (errs.Unavailable, `database_unavailable`, 503). Any other
// error is returned as it is.
//
// fn runs on one pooled connection, outside a transaction: it may issue several
// statements, but they do not see one snapshot, and it must consume every row it
// reads before it returns. It must be safe to run again, because a retry starts
// over on a new connection. Handlers use Read instead of the raw pool, which
// has no retry (R-ARCH-019, R-ARCH-023). On a request path whose context carries a
// statement bound, each statement runs with a deadline of that bound on its own
// context: the driver cancels it at the server, and the read answers 503
// database_timeout, as a transaction's statement past its SET LOCAL does; the read
// stays one round trip per statement (Q-ARCH-650).
func (db *DB) Read(ctx context.Context, fn func(q.DBTX) error) error {
	err := db.retry.doCtx(ctx, func(actx context.Context) error { return readOnce(actx, db.main, fn) })
	if errors.Is(err, errStatementBound) {
		return errs.E(errs.Unavailable, errs.CodeDatabaseTimeout, "the database did not answer in time", errs.WithCause(err))
	}
	if err != nil && IsStatementTimeout(err) {
		return Unavailable(err)
	}
	return err
}

// readOnce is one attempt of Read. A connection that proves wrong is destroyed
// instead of returned to the pool, so the retry finds the new primary.
func readOnce(ctx context.Context, pool *pgxpool.Pool, fn func(q.DBTX) error) error {
	conn, err := acquire(ctx, pool)
	if err != nil {
		return err
	}
	// Whatever way fn ends, the connection goes back or is destroyed. A panic
	// leaves it in an unknown state (a half-read result, an open statement): it
	// is destroyed, and the panic goes on to the recovery stage (CR2-3).
	settled := false
	defer func() {
		if !settled {
			_ = conn.Hijack().Close(context.Background())
		}
	}()
	var target q.DBTX = conn
	var bc *boundedConn
	if d, ok := meta.StatementTimeout(ctx); ok {
		bc = &boundedConn{DBTX: conn, d: d}
		target = bc
	}
	err = fn(target)
	settled = true
	// A deadline that passed after its statement had ended does not count: the read failed only when fn did.
	if bc != nil && bc.end() && err != nil {
		// A statement passed the bound: the driver closed the connection and cancelled the statement at the server. The read ends
		// here; it is not run again, and it is not a failover.
		_ = conn.Hijack().Close(context.Background())
		return fmt.Errorf("%w: %w", errStatementBound, err)
	}
	if conn.Conn().IsClosed() || classify(err) == retryFailover {
		_ = conn.Hijack().Close(context.Background())
		if err != nil && classify(err) == retryNo {
			err = &connClosedError{err: err}
		}
		return err
	}
	conn.Release()
	return err
}

// errStatementBound is a statement of a read outside a transaction that passed the request's statement bound (Q-ARCH-650).
var errStatementBound = errors.New("database: a statement passed the request's statement bound")

// boundedConn is the connection of a read outside a transaction on a request path (Q-ARCH-650): every statement runs with a deadline
// of d on the context it is given, which the driver turns into a cancel request at the server. A statement outside a transaction has
// no SET LOCAL to hold the bound, and a transaction around it would cost a BEGIN and a COMMIT. The deadlines end with the read (end).
type boundedConn struct {
	q.DBTX
	d time.Duration

	mu   sync.Mutex
	ctxs []context.Context
	ends []context.CancelFunc
}

func (b *boundedConn) bound(ctx context.Context) context.Context {
	c, cancel := context.WithTimeoutCause(ctx, b.d, errStatementBound)
	b.mu.Lock()
	b.ctxs, b.ends = append(b.ctxs, c), append(b.ends, cancel)
	b.mu.Unlock()
	return c
}

func (b *boundedConn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	c := b.bound(ctx)
	tag, err := b.DBTX.Exec(c, sql, args...)
	if err == nil {
		b.done(c) // the statement has ended: its deadline must not count against the rest of the read
	}
	return tag, err
}

func (b *boundedConn) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return b.DBTX.Query(b.bound(ctx), sql, args...)
}

func (b *boundedConn) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return b.DBTX.QueryRow(b.bound(ctx), sql, args...)
}

// done ends the deadline of a statement that has ended.
func (b *boundedConn) done(c context.Context) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, x := range b.ctxs {
		if x == c {
			b.ends[i]()
			b.ctxs, b.ends = append(b.ctxs[:i], b.ctxs[i+1:]...), append(b.ends[:i], b.ends[i+1:]...)
			return
		}
	}
}

// end ends the deadlines of the read's statements and reports whether one of them passed the bound before the read ended. The
// function of the read has consumed its rows by then (Read's contract).
func (b *boundedConn) end() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	passed := false
	for _, c := range b.ctxs {
		if errors.Is(context.Cause(c), errStatementBound) {
			passed = true
		}
	}
	for _, cancel := range b.ends {
		cancel()
	}
	return passed
}

// transact is the core of InTx and InPgxTx: retry around one attempt. The After
// hooks of the attempt that committed run here, once, after the retry loop.
func (db *DB) transact(ctx context.Context, pool *pgxpool.Pool, body func(pgx.Tx) (*txn, error)) error {
	var committed *txn
	err := db.retry.doCtx(ctx, func(actx context.Context) error {
		var err error
		committed, err = attempt(actx, pool, body)
		return err
	})
	if err != nil {
		if IsStatementTimeout(err) {
			return Unavailable(err)
		}
		return err
	}
	if committed != nil {
		committed.RunAfter()
	}
	return nil
}

// attempt runs one transaction. A connection that proves wrong (closed, or a
// demoted primary that is now read-only) is destroyed instead of returned to
// the pool, so the retry finds the new primary.
func attempt(ctx context.Context, pool *pgxpool.Pool, body func(pgx.Tx) (*txn, error)) (committed *txn, err error) {
	conn, err := acquire(ctx, pool)
	if err != nil {
		return nil, err
	}
	broken := false
	defer func() {
		if broken {
			_ = conn.Hijack().Close(context.Background())
			return
		}
		conn.Release()
	}()

	tx, err := conn.BeginTx(ctx, beginOptions(ctx))
	if err != nil {
		broken = conn.Conn().IsClosed() || classify(err) != retryNo
		return nil, err
	}
	finished := false
	var t *txn
	defer func() {
		if finished && err == nil {
			return
		}
		if !finished {
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if rerr := tx.Rollback(rctx); rerr != nil && !errors.Is(rerr, pgx.ErrTxClosed) {
				broken = true
			}
		}
		// The attempt ends without a commit: a failure in the body, in a Before
		// hook or in COMMIT, or a panic (err is nil then, finished is false).
		if t != nil {
			t.RunRollback()
		}
	}()

	t, err = body(tx)
	if err != nil {
		if conn.Conn().IsClosed() {
			broken = true
			if classify(err) == retryNo {
				err = &connClosedError{err: err}
			}
		} else if classify(err) == retryFailover {
			broken = true
		}
		return nil, err
	}
	finished = true
	if err := tx.Commit(ctx); err != nil {
		var pe *pgconn.PgError
		switch {
		case conn.Conn().IsClosed() && !errors.As(err, &pe):
			broken = true
			return nil, &outcomeUnknownError{err: err}
		case classify(err) == retryFailover:
			broken = true
		}
		return nil, err
	}
	return t, nil
}
