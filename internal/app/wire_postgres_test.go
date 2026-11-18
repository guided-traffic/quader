package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"

	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/observability/health"
)

func TestMarkOutage(t *testing.T) {
	t.Run("R-OPS-001 an unreachable or restarting database is an outage the readiness grace applies to", func(t *testing.T) {
		for name, err := range map[string]error{
			"admin shutdown": &pgconn.PgError{Code: "57P01", Message: "terminating connection due to administrator command"},
			"refused":        fmt.Errorf("dial: %w", syscall.ECONNREFUSED),
			"standby":        postgres.ErrStandby,
			"check timeout":  context.DeadlineExceeded,
			// Q-HA-306: on an authenticated connection only a pooler sends 28P01, for its own server login that failed
			// (after a switchover); the request retries through it, and the replica keeps the grace
			"pooler's failed server login": &pgconn.PgError{Code: "28P01", Message: `password authentication failed for user "quader"`},
		} {
			if !health.IsTransient(markOutage(err)) {
				t.Errorf("%s: not marked transient", name)
			}
		}
	})
	t.Run("R-OPS-001 a schema that is behind, a rejected password and success are not outages", func(t *testing.T) {
		for name, err := range map[string]error{
			"schema behind": postgres.ErrSchemaBehind,
			"password":      passwordRejectedAtConnect(t), // Quader's own credential, refused at connect
			"other":         errors.New("boom"),
		} {
			if got := markOutage(err); got == nil || health.IsTransient(got) {
				t.Errorf("%s: got %v", name, got)
			}
		}
		if markOutage(nil) != nil {
			t.Error("nil must stay nil")
		}
	})
}

// passwordRejectedAtConnect is the error pgconn returns when the server refuses the password during authentication: a
// backend that answers the startup message with FATAL 28P01, so the error has the shape it has in production (a
// *pgconn.ConnectError around the *pgconn.PgError), which no struct literal can build (the field is unexported).
func passwordRejectedAtConnect(t *testing.T) error {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		be := pgproto3.NewBackend(c, c)
		for {
			m, err := be.ReceiveStartupMessage()
			if err != nil {
				return
			}
			if _, ok := m.(*pgproto3.SSLRequest); ok {
				_, _ = c.Write([]byte{'N'})
				continue
			}
			break
		}
		be.Send(&pgproto3.ErrorResponse{Severity: "FATAL", Code: "28P01", Message: `password authentication failed for user "quader"`})
		_ = be.Flush()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgconn.Connect(ctx, fmt.Sprintf("postgres://quader:wrong@%s/quader?sslmode=disable", ln.Addr()))
	if err == nil {
		_ = conn.Close(ctx)
		t.Fatal("the fake backend accepted the connection")
	}
	var ce *pgconn.ConnectError
	var pe *pgconn.PgError
	if !errors.As(err, &ce) || !errors.As(err, &pe) || pe.Code != "28P01" {
		t.Fatalf("not a ConnectError around 28P01: %v", err)
	}
	return err
}
