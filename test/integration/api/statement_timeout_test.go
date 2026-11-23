//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// R-ARCH-019, Q-ARCH-650: every statement of a request ends after database.request_statement_timeout (here 500 ms) and the request
// answers 503 database_timeout, in both shapes a request reads: a read outside a transaction (the audit list, DB.Read: a deadline on
// the statement's context, which the driver cancels at the server) and a transaction (a file's download, the item store's InTx: SET
// LOCAL statement_timeout in the query that opens it). The statement is held up by a lock the test takes on the table the request
// reads, which the session's lock_timeout (5 s) would end only ten times later; afterwards no statement of the request runs on and the
// request answers again.
func TestR_ARCH_019_ARequestsStatementEndsAtTheRequestBound(t *testing.T) {
	e := newEnv(t, false, "QUADER_DATABASE_REQUEST_STATEMENT_TIMEOUT=500ms")
	e.stack.Repository("files-bound", "local", "generic", `{}`)
	n := e.newNode("web1", nodeOpts{})
	filePath := "/files/" + testenv.DefaultProject + "/files-bound/a.txt"
	if a := e.stack.Do(n.Replica, "PUT", filePath, e.admin.token, []byte("bytes of a file")); a.Status != http.StatusCreated {
		t.Fatalf("the upload: %d %s", a.Status, a.Body)
	}
	db := e.openDB("locker")
	for _, tc := range []struct {
		name, table, path string
	}{
		{"a read outside a transaction", "audit_event", "/api/v1/audit/events?limit=5"},
		{"a transaction", "items", filePath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			get := func() (int, string, time.Duration) {
				start := time.Now()
				a := e.stack.Do(n.Replica, "GET", tc.path, e.admin.token, nil)
				var p struct {
					Code string `json:"code"`
				}
				_ = json.Unmarshal(a.Body, &p)
				return a.Status, p.Code, time.Since(start)
			}
			if status, code, _ := get(); status != http.StatusOK {
				t.Fatalf("before the lock: %d %s", status, code)
			}
			locked, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			go func() {
				done <- db.InPgxTx(context.Background(), func(tx pgx.Tx) error {
					if _, err := tx.Exec(context.Background(), "LOCK TABLE quader."+tc.table+" IN ACCESS EXCLUSIVE MODE"); err != nil {
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
			status, code, took := get()
			close(release)
			if err := <-done; err != nil {
				t.Fatalf("the lock's transaction: %v", err)
			}
			if status != http.StatusServiceUnavailable || code != "database_timeout" || took > 3*time.Second {
				t.Fatalf("under the lock: %d %q after %s, want 503 database_timeout after the bound of 500 ms", status, code, took)
			}
			left := int64(-1)
			testenv.WaitFor(t, "no statement of the request left on "+tc.table, 5*time.Second, func() bool {
				left = e.scalar(`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND state <> 'idle'
					AND query LIKE '%' || $1::text || '%' AND query NOT LIKE '%pg_stat_activity%'`, tc.table)
				return left == 0
			})
			if status, code, _ := get(); status != http.StatusOK {
				t.Fatalf("after the lock: %d %s", status, code)
			}
		})
	}
}
