package postgres

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
)

// blackHole accepts connections and never answers: a database whose host is up and whose PostgreSQL is stopped behind a
// proxy, or a route that drops packets. It returns the address.
func blackHole(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
	})
	return l.Addr().String()
}

// TestRetryBudgetBoundsTheWaitForAConnection the finding of the first E5.10 runs: with a database that does not answer, a request waits for a pooled
// connection (the pool is one dial deep, the dial times out after connect_timeout) and used to wait for its own deadline,
// 45 s at a client's timeout. It now fails with the 503 of R-ARCH-023 within retry.max_elapsed.
func TestRetryBudgetBoundsTheWaitForAConnection(t *testing.T) {
	pc, err := pgxpool.ParseConfig("postgres://quader@" + blackHole(t) + "/quader?sslmode=disable&connect_timeout=60")
	if err != nil {
		t.Fatal(err)
	}
	pc.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	r := newRetrier(config.DatabaseRetry{InitialBackoff: 50 * time.Millisecond, MaxBackoff: 200 * time.Millisecond, MaxElapsed: time.Second})

	t.Run("R-HA-007 R-HA-009 R-HA-052 every request that waits for a connection of a database that does not answer ends in the 503 within retry.max_elapsed", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second) // the request's deadline, far beyond the budget
		defer cancel()
		var wg sync.WaitGroup
		took := make([]time.Duration, 4)
		errsGot := make([]error, 4)
		for i := range took {
			wg.Add(1)
			go func() {
				defer wg.Done()
				start := time.Now()
				errsGot[i] = r.doCtx(ctx, func(actx context.Context) error {
					conn, err := acquire(actx, pool)
					if err != nil {
						return err
					}
					conn.Release()
					return nil
				})
				took[i] = time.Since(start)
			}()
		}
		wg.Wait()
		for i := range took {
			var e *errs.Error
			if !errors.As(errsGot[i], &e) || e.Kind != errs.Unavailable {
				t.Errorf("request %d: %v, want the 503 (unavailable)", i, errsGot[i])
			}
			if took[i] > 2*time.Second {
				t.Errorf("request %d took %s, want within retry.max_elapsed (1s) plus one backoff", i, took[i])
			}
		}
	})
}

// TestR_HA_009_ADeadlockRetryAfterTheBudgetStillGetsItsConnection (Q-HA-507): a transaction that waited longer than retry.max_elapsed
// for a lock and lost the deadlock is run again, and the new attempt waits for its connection within a budget that counts from the
// deadlock; it used to find the deadline of its acquire passed and answer 503 database_unavailable ("context deadline exceeded") from a
// database that works (integration area files, TestConcurrentWritersOnOnePath, 2026-11-27, with retry.max_elapsed 2 s and two
// deadlocks of PostgreSQL's deadlock_timeout of 1 s each).
func TestR_HA_009_ADeadlockRetryAfterTheBudgetStillGetsItsConnection(t *testing.T) {
	r := newRetrier(config.DatabaseRetry{InitialBackoff: 10 * time.Millisecond, MaxBackoff: 20 * time.Millisecond, MaxElapsed: 100 * time.Millisecond})
	attempts := 0
	err := r.doCtx(context.Background(), func(actx context.Context) error {
		attempts++
		// The wait for a connection as acquire bounds it: an attempt whose deadline has passed gets none.
		by, ok := actx.Value(retryDeadlineKey{}).(time.Time)
		if !ok {
			t.Fatal("no budget for the connection")
		}
		wait, cancel := context.WithDeadline(actx, by)
		defer cancel()
		if err := wait.Err(); err != nil {
			return err
		}
		if attempts < 3 {
			time.Sleep(150 * time.Millisecond) // the lock wait until the deadlock is detected, longer than the budget
			return &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("after two deadlocks: %v in %d attempts, want success in 3", err, attempts)
	}
}
