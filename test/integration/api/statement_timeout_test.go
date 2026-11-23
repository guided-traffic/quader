//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// R-ARCH-019, Q-ARCH-594: every statement of a request ends after database.request_statement_timeout (here 500 ms) and the request
// answers 503 database_timeout; the replica's connection is free again at once. The statement is held up by a lock the test takes on
// the table the request reads, which the session's lock_timeout (5 s) would end only ten times later.
func TestR_ARCH_019_ARequestsStatementEndsAtTheRequestBound(t *testing.T) {
	e := newEnv(t, false, "QUADER_DATABASE_REQUEST_STATEMENT_TIMEOUT=500ms")
	n := e.newNode("web1", nodeOpts{})
	list := func() (int, string, time.Duration) {
		start := time.Now()
		a := e.stack.Do(n.Replica, "GET", "/api/v1/audit/events?limit=5", e.admin.token, nil)
		var p struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(a.Body, &p)
		return a.Status, p.Code, time.Since(start)
	}
	if status, code, _ := list(); status != http.StatusOK {
		t.Fatalf("the list before the lock: %d %s", status, code)
	}
	db := e.openDB("locker")
	locked, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- db.InPgxTx(context.Background(), func(tx pgx.Tx) error {
			if _, err := tx.Exec(context.Background(), "LOCK TABLE quader.audit_event IN ACCESS EXCLUSIVE MODE"); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-done:
		t.Fatalf("the lock: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("the lock was not taken within 30 s")
	}
	status, code, took := list()
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("the lock's transaction: %v", err)
	}
	if status != http.StatusServiceUnavailable || code != "database_timeout" || took > 3*time.Second {
		t.Fatalf("the list under the lock answered %d %q after %s, want 503 database_timeout after the bound of 500 ms", status, code, took)
	}
	if left := e.scalar(`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND state <> 'idle'
		AND query LIKE '%audit_event%' AND pid <> pg_backend_pid()`); left != 0 {
		t.Fatalf("%d statements of the request still run", left)
	}
	if status, code, _ := list(); status != http.StatusOK {
		t.Fatalf("the list after the lock: %d %s", status, code)
	}
}
