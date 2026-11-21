package poolerlog

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// pgbLog renders PgBouncer lines the way the CloudNativePG pooler manager logs them: the kubectl prefix-free JSON record with
// PgBouncer's own line under "record". Each entry is "HH:MM:SS.mmm message".
func pgbLog(entries ...string) string {
	var b strings.Builder
	for _, e := range entries {
		ts, msg, _ := strings.Cut(e, " ")
		r := map[string]any{"level": "info", "msg": "record", "logger": "pgbouncer-manager", "record": map[string]string{
			"timestamp": "2026-11-21 " + ts + " UTC", "pid": "27", "level": "LOG", "msg": msg,
		}}
		j, _ := json.Marshal(r)
		b.Write(j)
		b.WriteByte('\n')
	}
	return b.String()
}

func at(ts string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05.000 MST", "2026-11-21 "+ts+" UTC")
	if err != nil {
		panic(err)
	}
	return t
}

const (
	crashed  = "S-0x1: quader/quader@10.96.71.204:5432 closing because: server conn crashed? (age=148s)"
	connFail = "S-0x1: quader/quader@10.96.71.204:5432 closing because: connect failed (age=0s)"
	authHung = "S-0x2: postgres/cnpg_pooler_pgbouncer@10.96.71.204:5432 closing because: connect failed (age=15s)"
	loginBad = `server login failed: FATAL password authentication failed for user "quader"`
	srvBad   = `S-0x1: quader/quader@10.96.71.204:5432 closing because: password authentication failed for user "quader" (age=0s)`
	cachedCF = "C-0x9: postgres/quader@10.244.2.18:34250 pooler error: server login has been failing, cached error: connect failed (server_login_retry)"
	cachedPW = `C-0x9: quader/quader@10.244.3.14:50854 pooler error: server login has been failing, cached error: password authentication failed for user "quader" (server_login_retry)`
	newConn  = "S-0x1: quader/quader@10.96.71.204:5432 new connection to server (from 10.244.2.12:58770)"
	served   = "C-0x9: quader/quader@10.244.3.14:50868 login attempt: db=quader user=quader tls=TLSv1.3 replication=no"
)

// R-HA-058 Q-HA-370: the instance of the nightly of 2026-11-21 (quader-db-pooler-control-…-6slbk). Its server login on the new
// primary failed, the one client within server_login_retry was refused, then no client came for a minute; the first one after
// that was refused with the old error and started the attempt that succeeded. The window ends a server_login_retry after the
// failed login, 8.6 s after the first line; the late refusal is reported, not counted.
func TestR_HA_058_ARefusalAfterTheInstanceWentWithoutClientsIsNotAMissingServer(t *testing.T) {
	w := Read(pgbLog(
		"00:37:36.141 "+crashed,
		"00:37:36.473 "+connFail,
		"00:37:36.501 "+cachedCF,
		"00:37:42.740 "+connFail,
		"00:37:43.783 "+loginBad,
		"00:37:43.783 "+srvBad,
		"00:37:44.531 "+cachedPW,
		"00:38:45.498 "+cachedPW,
		"00:38:45.499 "+newConn,
		"00:38:46.481 "+served,
	), time.Second)
	if !w.First.Equal(at("00:37:36.141")) || !w.LastServer.Equal(at("00:37:43.783")) || !w.End.Equal(at("00:37:44.783")) {
		t.Fatalf("window %s to %s, last failed server attempt %s; want 00:37:36.141 to 00:37:44.783, 00:37:43.783", w.First, w.End, w.LastServer)
	}
	if w.Length() != 8642*time.Millisecond || w.Lines != 7 || w.Refused != 2 {
		t.Fatalf("length %s, %d lines, %d refused; want 8.642s, 7, 2", w.Length(), w.Lines, w.Refused)
	}
	if w.Late != 1 || !w.LateFirst.Equal(at("00:38:45.498")) || !w.LateLast.Equal(w.LateFirst) {
		t.Fatalf("late refusals %d from %s to %s; want 1 at 00:38:45.498", w.Late, w.LateFirst, w.LateLast)
	}
}

// R-HA-058 Q-HA-370 Q-HA-371: the instance of the nightly of 2026-11-20 (quader-db-pooler-rw-…-82mgv). The auth pool's connect
// to the new address got no answer and failed only at PgBouncer's server_connect_timeout of 15 s; until then the instance could
// log in no new client. That failed attempt is the server's, so the window runs to it and a server_login_retry beyond: 21.1 s,
// past the grace of 20 s.
func TestR_HA_058_AConnectThatHangsUntilItsTimeoutCountsToTheEnd(t *testing.T) {
	w := Read(pgbLog(
		"05:15:07.450 "+crashed,
		"05:15:11.508 "+connFail,
		"05:15:15.503 "+loginBad,
		"05:15:15.504 "+srvBad,
		"05:15:15.671 "+cachedCF,
		"05:15:22.689 "+cachedCF,
		"05:15:27.510 "+authHung,
		"05:15:41.160 "+cachedPW,
		"05:15:41.160 "+newConn,
	), time.Second)
	if !w.End.Equal(at("05:15:28.510")) || w.Length() != 21060*time.Millisecond || w.Refused != 2 || w.Late != 1 {
		t.Fatalf("window to %s (%s), %d refused, %d late; want to 05:15:28.510 (21.06s), 2, 1", w.End, w.Length(), w.Refused, w.Late)
	}
}

// R-HA-058: a pool that is asked while its server attempts keep failing shows a window to its last refusal, and nothing late.
func TestR_HA_058_RefusalsBetweenFailedAttemptsAllCount(t *testing.T) {
	w := Read(pgbLog(
		"10:00:00.000 "+crashed,
		"10:00:00.500 "+cachedCF,
		"10:00:01.000 "+connFail,
		"10:00:01.700 "+cachedCF,
		"10:00:02.000 "+connFail,
		"10:00:02.900 "+cachedCF,
	), time.Second)
	if !w.End.Equal(at("10:00:02.900")) || w.Lines != 6 || w.Refused != 3 || w.Late != 0 {
		t.Fatalf("window to %s, %d lines, %d refused, %d late; want to 10:00:02.900, 6, 3, 0", w.End, w.Lines, w.Refused, w.Late)
	}
}

// R-HA-058: a log without a failed server attempt cannot put a refusal beside one, so every failure line counts.
func TestR_HA_058_WithoutAFailedServerAttemptEveryLineCounts(t *testing.T) {
	w := Read(pgbLog(
		"10:00:00.000 "+cachedCF,
		"10:00:30.000 "+cachedPW,
	), time.Second)
	if w.Length() != 30*time.Second || w.Late != 0 || !w.LastServer.IsZero() {
		t.Fatalf("length %s, %d late, last server %s; want 30s, 0, none", w.Length(), w.Late, w.LastServer)
	}
	if empty := Read("not a record\n{\"record\":{}}\n", time.Second); empty.Length() != 0 || empty.Lines != 0 {
		t.Fatalf("a log without failure lines: %+v", empty)
	}
}

// Q-HA-302: what server_login_retry PgBouncer runs with is read from the rendered pgbouncer.ini, its default when unset.
func TestR_HA_058_LoginRetryIsReadFromTheRenderedConfiguration(t *testing.T) {
	for ini, want := range map[string]time.Duration{
		"[pgbouncer]\nserver_login_retry = 1\n":    time.Second,
		"[pgbouncer]\nserver_login_retry = 0.5\n":  500 * time.Millisecond,
		"[pgbouncer]\ndefault_pool_size = 4\n":     15 * time.Second,
		"[pgbouncer]\nserver_login_retry = soon\n": 15 * time.Second,
	} {
		if got := LoginRetry(ini); got != want {
			t.Errorf("LoginRetry(%q) = %s, want %s", ini, got, want)
		}
	}
	if v, ok := Param("server_connect_timeout = 5\n", "server_connect_timeout"); !ok || v != "5" {
		t.Errorf("Param: %q %v", v, ok)
	}
}
