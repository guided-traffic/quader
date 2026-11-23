//go:build integration

package ci

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/quader/internal/ci/subject"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/oidcstub"
)

// keyReads counts the reads of a stored key set (the statement CiKeysGet) a replica sends, as its pgx tracer.
type keyReads struct{ n atomic.Int64 }

func (k *keyReads) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.Contains(d.SQL, "-- name: CiKeysGet ") {
		k.n.Add(1)
	}
	return ctx
}

func (*keyReads) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// pollAll has each replica poll its change log once and apply every change committed before (R-ARCH-018).
func pollAll(t *testing.T, rs ...*testenv.Replica) {
	t.Helper()
	for _, r := range rs {
		if err := r.App.Changes().PollNow(t.Context()); err != nil {
			t.Fatalf("%s: change-log poll: %v", r.Name, err)
		}
	}
}

func TestR_CI_003_ARotationCausesOneFetchAndRandomKidsCauseNone(t *testing.T) {
	d := startDouble(t, oidcstub.Config{})
	// A 60 s throttle: the 100 random kids all fall into it.
	e := newEnv(t, options{valkey: true, kidInterval: "60s"})
	reads := &keyReads{}
	a := e.stack.Start(t, testenv.ReplicaOptions{Name: "web-a", Role: "web", Database: []postgres.Option{postgres.WithQueryTracer(reads)}})
	wantStatus(t, e.do(a, "POST", issuersPath, d.body("gitlab-main")), 201, "")
	is := issuerOf(t, a, d.stub.Issuer())
	first := mint(t, d, oidcstub.MintOptions{})
	if err := keyLookup(t, a, is, first); err != nil {
		t.Fatalf("a token of the stored key: %v", err)
	}

	if _, err := d.stub.Rotate(); err != nil {
		t.Fatal(err)
	}
	d.stub.Reset()
	rotated := mint(t, d, oidcstub.MintOptions{})
	if err := keyLookup(t, a, is, rotated); err != nil {
		t.Fatalf("a token of the rotated key is refused: %v", err)
	}
	if c := d.stub.Counts(); c.JWKS != 1 {
		t.Fatalf("the rotation caused %d key fetches, want exactly 1", c.JWKS)
	}

	d.stub.Reset()
	// The replica's own reads of the stored set are counted as it sends them (keyReads), so the window's ends are exact
	// however slow the host. The table's statistics counted them before, which a backend publishes up to 1.5 s late and a
	// loaded host later still, and every reader of the table with them.
	before := reads.n.Load()
	start := time.Now()
	for i := range 100 {
		tok := mint(t, d, oidcstub.MintOptions{HeaderKID: fmt.Sprintf("random-%d", i)})
		if err := keyLookup(t, a, is, tok); !errors.Is(err, subject.ErrUnknownKey) {
			t.Fatalf("random kid %d: %v, want unknown_signing_key", i, err)
		}
	}
	window := time.Since(start)
	got := reads.n.Load() - before
	if c := d.stub.Counts(); c.JWKS != 0 || c.Discovery != 0 {
		t.Errorf("100 random kids caused fetches: %+v", c)
	}
	if allowed := int64(window/(500*time.Millisecond)) + 2; got > allowed {
		t.Errorf("100 random kids in %v caused %d reads of the stored set, want at most one per cluster.epoch_poll_interval (%d)", window, got, allowed)
	}
}

func TestR_CI_003_ANewKeyIsAcceptedOnEveryReplicaWithValkeyStopped(t *testing.T) {
	d := startDouble(t, oidcstub.Config{})
	e := newEnv(t, options{valkey: false})
	a, b, c := e.web("web-a"), e.web("web-b"), e.web("web-c")
	wantStatus(t, e.do(a, "POST", issuersPath, d.body("gitlab-main")), 201, "")
	// Without Valkey the change-log poll carries the new issuer; one poll of each replica applies it, however long a poll takes
	// on a loaded host (a bound of 4 s stood here; the propagation's own bound is TestR_CI_003_ALaggingReplicaLearnsANewIssuerWithinThePollInterval's).
	pollAll(t, b, c)
	for _, r := range []*testenv.Replica{b, c} {
		if _, ok, err := r.App.CIIssuers().Resolve(t.Context(), d.stub.Issuer()); !ok || err != nil {
			t.Fatalf("%s does not resolve the issuer after its change-log poll: %v", r.Name, err)
		}
	}
	old := mint(t, d, oidcstub.MintOptions{})
	isA, isB, isC := issuerOf(t, a, d.stub.Issuer()), issuerOf(t, b, d.stub.Issuer()), issuerOf(t, c, d.stub.Issuer())
	// Fill the copies with a normal read, which is no re-read.
	if keyLookup(t, a, isA, old) != nil || keyLookup(t, b, isB, old) != nil || keyLookup(t, c, isC, old) != nil {
		t.Fatal("the stored key is refused")
	}
	time.Sleep(time.Second)

	if _, err := d.stub.Rotate(); err != nil {
		t.Fatal(err)
	}
	d.stub.Reset()
	tok := mint(t, d, oidcstub.MintOptions{})
	if err := keyLookup(t, a, isA, tok); err != nil {
		t.Fatalf("replica A: %v", err)
	}
	if err := keyLookup(t, b, isB, tok); err != nil {
		t.Fatalf("replica B: %v", err)
	}
	if err := keyLookup(t, c, isC, tok); err != nil {
		t.Fatalf("replica C: %v", err)
	}
	if n := d.stub.Counts().JWKS; n != 1 {
		t.Errorf("%d key fetches for three replicas, want 1", n)
	}
}

func TestR_CI_003_AFetchHoldsNoTransactionOpenAndAnUnchangedSetLeavesTheEpoch(t *testing.T) {
	d := startDouble(t, oidcstub.Config{})
	e := newEnv(t, options{valkey: true})
	a, b := e.web("web-a"), e.web("web-b")
	_ = e.worker("worker-a")
	wantStatus(t, e.do(a, "POST", issuersPath, d.body("gitlab-main")), 201, "")

	t.Run("R-CI-003 no connection is idle in transaction while the key response is delayed 4 s", func(t *testing.T) {
		if err := d.stub.SetFault(oidcstub.Fault{Delay: oidcstub.Duration(4 * time.Second), Target: oidcstub.TargetJWKS}); err != nil {
			t.Fatal(err)
		}
		e.exec("UPDATE ci_issuer_keys SET next_refresh_at = now() - interval '1 minute'")
		e.waitIdle("ci-keys-refresh")
		done := e.succeeded("ci-keys-refresh")
		wantStatus(t, e.do(a, "POST", "/api/v1/system/tasks/ci-keys-refresh/run", nil), 202, "")
		stop := make(chan struct{})
		var idle int64
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				case <-time.After(150 * time.Millisecond):
					// A connection that held its transaction over the fetch is idle in transaction for the whole 4 s of the
					// delayed response; every multi-statement transaction of any replica (a job insert, a gauge read) passes
					// through that state for well under a millisecond between two statements, which is no held connection.
					if n := e.number(`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND state LIKE 'idle in transaction%' AND now() - state_change > interval '1 second'`); n > idle {
						idle = n
					}
				}
			}
		}()
		within(t, "the key request reaching the double", 10*time.Second, func() bool { return d.stub.Counts().JWKS >= 1 })
		within(t, "the run ending", 15*time.Second, func() bool { return e.succeeded("ci-keys-refresh") > done })
		close(stop)
		wg.Wait()
		if idle != 0 {
			t.Errorf("%d connections were idle in transaction during the fetch", idle)
		}
		if err := d.stub.SetFault(oidcstub.Fault{}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("R-CI-003 two replicas that fetch an unchanged set leave the epoch of ci_issuer_keys", func(t *testing.T) {
		before := e.epoch("ci_issuer_keys")
		var wg sync.WaitGroup
		for _, r := range []int{0, 1} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rep := a
				if r == 1 {
					rep = b
				}
				if x := e.do(rep, "POST", issuersPath+"/gitlab-main/keys/refresh", nil); x.Status != 200 {
					t.Errorf("refresh answered %d", x.Status)
				}
			}()
		}
		wg.Wait()
		if after := e.epoch("ci_issuer_keys"); after != before {
			t.Errorf("an unchanged set moved the epoch from %d to %d", before, after)
		}
		if e.events("ci.keys.refreshed") < 3 {
			t.Errorf("%d ci.keys.refreshed events", e.events("ci.keys.refreshed"))
		}
		if _, err := d.stub.Rotate(); err != nil {
			t.Fatal(err)
		}
		wantStatus(t, e.do(a, "POST", issuersPath+"/gitlab-main/keys/refresh", nil), 200, "")
		if e.epoch("ci_issuer_keys") != before+1 {
			t.Errorf("a changed set moved the epoch by %d, want 1", e.epoch("ci_issuer_keys")-before)
		}
	})

	t.Run("R-CI-003 a key store held open 2 s delays no exchange, and a store does not wait for an exchange", func(t *testing.T) {
		id := e.text("SELECT id::text FROM ci_issuers WHERE name = 'gitlab-main'")
		hold := func(sql string) (release func()) {
			conn := e.conn(t)
			tx, err := conn.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(t.Context(), sql, id); err != nil {
				t.Fatal(err)
			}
			return func() { _ = tx.Rollback(t.Context()) }
		}
		// A store holds the key row; an exchange (FOR SHARE on the issuer row) is not delayed: with NOWAIT a lock it would
		// wait for fails at once (55P03), whatever the host's speed; a clock against a second did not tell a wait from a
		// slow host.
		release := hold("SELECT 1 FROM ci_issuer_keys WHERE issuer_id = $1::uuid FOR UPDATE")
		tx := e.conn(t)
		x, err := tx.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := x.Exec(t.Context(), "SELECT 1 FROM ci_issuers WHERE id = $1::uuid FOR SHARE NOWAIT", id); err != nil {
			t.Errorf("an exchange would wait for a key store: %v", err)
		}
		_ = x.Rollback(t.Context())
		release()
		// An exchange holds the issuer row; a key store does not wait for it: the refresh answers while the hold stays, which
		// a store that waited for the row could not do before the hold ends (the replica's lock_timeout would end it first).
		release = hold("SELECT 1 FROM ci_issuers WHERE id = $1::uuid FOR SHARE")
		wantStatus(t, e.do(a, "POST", issuersPath+"/gitlab-main/keys/refresh", nil), 200, "")
		release()
	})
}

func TestR_CI_003_StaleAndUnreachable(t *testing.T) {
	d := startDouble(t, oidcstub.Config{})
	// max_stale 1 m needs a refresh bound of 1 m (max_stale is at least max_refresh_interval).
	e := newEnv(t, options{valkey: false, env: []string{
		"QUADER_CI_SIGNING_KEYS_MIN_REFRESH_INTERVAL=10s", "QUADER_CI_SIGNING_KEYS_DEFAULT_REFRESH_INTERVAL=1m",
		"QUADER_CI_SIGNING_KEYS_MAX_REFRESH_INTERVAL=1m", "QUADER_CI_SIGNING_KEYS_MAX_STALE=1m",
	}})
	a, b := e.web("web-a"), e.web("web-b")
	_ = e.worker("worker-a")
	wantStatus(t, e.do(a, "POST", issuersPath, d.body("gitlab-main")), 201, "")
	id := e.text("SELECT id::text FROM ci_issuers WHERE name = 'gitlab-main'")
	tok := mint(t, d, oidcstub.MintOptions{})
	pollAll(t, b)
	if _, ok, err := b.App.CIIssuers().Resolve(t.Context(), d.stub.Issuer()); !ok || err != nil {
		t.Fatalf("web-b does not resolve the issuer after its change-log poll: %v", err)
	}
	isA, isB := issuerOf(t, a, d.stub.Issuer()), issuerOf(t, b, d.stub.Issuer())
	age := func(secs int) {
		e.exec("UPDATE ci_issuer_keys SET fetched_at = now() - make_interval(secs => $1) WHERE issuer_id = $2::uuid", secs, id)
		e.bump("ci_issuer_keys", map[string]string{"issuer": id}) // the copies are dropped, as a change message does
		// Without Valkey the change-log poll carries the change (R-ARCH-018). Waiting for one poll of each replica
		// instead of a fixed time makes the state of both copies certain before the lookups below, however long a
		// poll takes on a loaded machine.
		pollAll(t, a, b)
	}

	t.Run("R-CI-003 a refresh that stores an unchanged set keeps every replica succeeding past max_stale", func(t *testing.T) {
		age(55) // short of max_stale on every replica's copy; the sleeps below bring it to 1 s short
		time.Sleep(1200 * time.Millisecond)
		if keyLookup(t, a, isA, tok) != nil || keyLookup(t, b, isB, tok) != nil {
			t.Fatal("the set 1 s short of max_stale is refused")
		}
		before := e.epoch("ci_issuer_keys")
		wantStatus(t, e.do(a, "POST", issuersPath+"/gitlab-main/keys/refresh", nil), 200, "") // the task's store: same set
		if e.epoch("ci_issuer_keys") != before {
			t.Fatal("an unchanged set sent a change message")
		}
		time.Sleep(6 * time.Second) // the copies are now older than max_stale
		if err := keyLookup(t, a, isA, tok); err != nil {
			t.Errorf("replica A after the copy passed max_stale: %v", err)
		}
		if err := keyLookup(t, b, isB, tok); err != nil {
			t.Errorf("replica B after the copy passed max_stale: %v", err)
		}
	})

	t.Run("R-CI-003 with the issuer unreachable, cached keys work until max_stale and then signing_keys_unavailable", func(t *testing.T) {
		if err := d.stub.SetFault(oidcstub.Fault{Unreachable: true}); err != nil {
			t.Fatal(err)
		}
		age(30)
		time.Sleep(1200 * time.Millisecond)
		if err := keyLookup(t, a, isA, tok); err != nil {
			t.Fatalf("cached keys inside max_stale: %v", err)
		}
		age(61)
		time.Sleep(1200 * time.Millisecond)
		if err := keyLookup(t, a, isA, tok); !errors.Is(err, subject.ErrKeysUnavailable) {
			t.Errorf("replica A past max_stale: %v, want signing_keys_unavailable", err)
		}
		if err := keyLookup(t, b, isB, tok); !errors.Is(err, subject.ErrKeysUnavailable) {
			t.Errorf("replica B past max_stale: %v, want signing_keys_unavailable", err)
		}
		if err := d.stub.SetFault(oidcstub.Fault{}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1200 * time.Millisecond)
		if err := keyLookup(t, a, isA, tok); err != nil {
			t.Errorf("after the issuer is back, the on-demand fetch recovers: %v (last_error %q, age %s s, counts %+v, refresh events %d, failed %d)", err,
				e.text("SELECT coalesce(last_error, '') FROM ci_issuer_keys"), e.text("SELECT extract(epoch FROM now() - fetched_at)::int::text FROM ci_issuer_keys"),
				d.stub.Counts(), e.events("ci.keys.refreshed"), e.events("ci.keys.refresh_failed"))
		}
	})
}

func TestR_CI_003_ARemovedKeyIsNoLongerAccepted(t *testing.T) {
	d := startDouble(t, oidcstub.Config{})
	e := newEnv(t, options{valkey: true})
	a := e.web("web-a")
	wantStatus(t, e.do(a, "POST", issuersPath, d.body("gitlab-main")), 201, "")
	is := issuerOf(t, a, d.stub.Issuer())
	oldTok := mint(t, d, oidcstub.MintOptions{})
	oldKID := tokenHeader(t, oldTok).KID
	if _, err := d.stub.Rotate(); err != nil {
		t.Fatal(err)
	}
	newTok := mint(t, d, oidcstub.MintOptions{})
	if keyLookup(t, a, is, newTok) != nil || keyLookup(t, a, is, oldTok) != nil {
		t.Fatal("both published keys are accepted")
	}
	if !d.stub.RemoveKey(oldKID) {
		t.Fatal("the key is not in the double")
	}
	wantStatus(t, e.do(a, "POST", issuersPath+"/gitlab-main/keys/refresh", nil), 200, "")
	if err := keyLookup(t, a, is, oldTok); !errors.Is(err, subject.ErrUnknownKey) {
		t.Errorf("the removed key: %v, want unknown_signing_key", err)
	}
	if err := keyLookup(t, a, is, newTok); err != nil {
		t.Errorf("the remaining key: %v", err)
	}
}

func TestR_CI_003_AJkuHeaderCausesNoRequest(t *testing.T) {
	// The key lookup takes the kid and the algorithm and nothing else of a token: no header member names a
	// URL for it (RFC 8725 §3.10). The end-to-end form with the exchange is the W7 suite.
	d := startDouble(t, oidcstub.Config{})
	e := newEnv(t, options{valkey: true})
	a := e.web("web-a")
	wantStatus(t, e.do(a, "POST", issuersPath, d.body("gitlab-main")), 201, "")
	is := issuerOf(t, a, d.stub.Issuer())
	d.stub.Reset()
	tok := mint(t, d, oidcstub.MintOptions{Headers: map[string]any{"jku": "https://attacker.example/keys", "x5u": "https://attacker.example/c"}})
	if err := keyLookup(t, a, is, tok); err != nil {
		t.Fatal(err)
	}
	if c := d.stub.Counts(); c.JWKS != 0 || c.Discovery != 0 {
		t.Errorf("%+v", c)
	}
	if got := e.number(`SELECT count(*) FROM audit_event WHERE details::text LIKE '%attacker.example%'`); got != 0 {
		t.Errorf("%d events name the attacker host", got)
	}
}

func TestR_CI_003_TheFailureIsRecordedWithoutABody(t *testing.T) {
	d := startDouble(t, oidcstub.Config{})
	e := newEnv(t, options{valkey: true})
	a := e.web("web-a")
	wantStatus(t, e.do(a, "POST", issuersPath, d.body("gitlab-main", map[string]any{"validate": false})), 201, "")

	t.Run("R-CI-003 a key URL at the metadata address gets no connection and last_error has the class", func(t *testing.T) {
		if err := d.stub.SetFault(oidcstub.Fault{JWKSURI: "https://169.254.169.254/keys"}); err != nil {
			t.Fatal(err)
		}
		r := e.do(a, "POST", issuersPath+"/gitlab-main/keys/refresh", nil)
		wantStatus(t, r, 502, "upstream_failed")
		k := e.do(a, "GET", issuersPath+"/gitlab-main/keys", nil).JSON(t)
		if le, _ := k["lastError"].(string); le != "egress_denied host=169.254.169.254" {
			t.Errorf("lastError = %q", le)
		}
		if n := e.events("ci.keys.refresh_failed"); n != 1 {
			t.Errorf("%d refresh_failed events", n)
		}
		if got := e.text(`SELECT details->>'errorClass' || ' ' || (details->>'host') FROM audit_event WHERE action = 'ci.keys.refresh_failed'`); got != "egress_denied 169.254.169.254" {
			t.Errorf("event = %q", got)
		}
	})

	t.Run("R-CI-003 a key URL that answers 200 with a non-JSON body leaves no byte of it anywhere", func(t *testing.T) {
		if err := d.stub.SetFault(oidcstub.Fault{NotJSON: true, Target: oidcstub.TargetJWKS}); err != nil {
			t.Fatal(err)
		}
		r := e.do(a, "POST", issuersPath+"/gitlab-main/keys/refresh", nil)
		wantStatus(t, r, 502, "")
		k := e.do(a, "GET", issuersPath+"/gitlab-main/keys", nil)
		for what, text := range map[string]string{
			"the answer": string(r.Body), "the key status": string(k.Body),
			"the row":    e.text("SELECT coalesce(last_error, '') FROM ci_issuer_keys"),
			"the events": e.text("SELECT coalesce(string_agg(details::text, ' '), '') FROM audit_event WHERE action LIKE 'ci.keys.%'"),
		} {
			if strings.Contains(text, oidcstub.NonJSONMarker) {
				t.Errorf("%s holds a byte of the body", what)
			}
		}
		if le, _ := k.JSON(t)["lastError"].(string); !strings.HasPrefix(le, "invalid_response") {
			t.Errorf("lastError = %q", le)
		}
		// The failed first fetch left a placeholder row and no set.
		if keys, _ := k.JSON(t)["keys"].([]any); len(keys) != 0 || k.JSON(t)["fetchedAt"] != nil {
			t.Errorf("the status shows a set: %s", k.Body)
		}
	})
}
