package cluster

import (
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// silentServer is a fakeServer whose connections get no answer while silent is set: it accepts them and reads what
// the client sends, so the client's deadline ends the attempt, the way a starved or partitioned Valkey does.
func silentServer(t *testing.T, handler func(args []string) string) (*fakeServer, *atomic.Bool) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeServer{ln: ln}
	var silent atomic.Bool
	silent.Store(true)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if silent.Load() {
				go func() {
					_, _ = io.Copy(io.Discard, conn)
					_ = conn.Close()
				}()
				continue
			}
			go s.serve(conn, handler)
		}
	}()
	return s, &silent
}

// accessServer answers like a Valkey 9.1 and lets reply decide the commands of the access check.
func accessServer(t *testing.T, reply func(args []string) (string, bool)) *fakeServer {
	return newFakeServer(t, func(args []string) string {
		if r, ok := reply(args); ok {
			return r
		}
		switch strings.ToUpper(args[0]) {
		case "HELLO":
			return helloReplyFor("valkey", "9.1.0")
		case "CLIENT":
			return "+OK\r\n"
		case "PING":
			return "+PONG\r\n"
		case "SET":
			return "+OK\r\n"
		case "EVAL", "EVALSHA":
			return "*2\r\n:1\r\n_\r\n"
		}
		return "-ERR unknown command\r\n"
	})
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestValkeyFirstAnswer(t *testing.T) {
	t.Run("R-HA-008 R-HA-056 a first attempt that meets silence is no answer, and a later attempt that is answered is", func(t *testing.T) {
		srv, silent := silentServer(t, func(args []string) string {
			switch strings.ToUpper(args[0]) {
			case "HELLO":
				return helloReplyFor("valkey", "9.1.0")
			case "SET", "CLIENT":
				return "+OK\r\n"
			case "PING":
				return "+PONG\r\n"
			}
			return "-ERR unknown command\r\n"
		})
		cfg := standaloneCfg(srv.addr())
		cfg.ConnectTimeout = 100 * time.Millisecond
		v, _ := newTestValkey(t, cfg)
		if isClosed(v.Answered()) {
			t.Fatalf("Answered is closed after an attempt that got no answer (status %+v)", v.Status())
		}
		if err := v.StartupError(); err != nil {
			t.Fatalf("silence is not a start-up error: %v", err)
		}
		if code := v.Status().ReasonCode; code != ReasonTimeout {
			t.Fatalf("reason code = %q, want %q", code, ReasonTimeout)
		}
		silent.Store(false)
		select {
		case <-v.Answered():
		case <-time.After(5 * time.Second):
			t.Fatalf("no answer after the server started answering (status %+v)", v.Status())
		}
		if !v.Available() || v.StartupError() != nil {
			t.Fatalf("an accepted credential: available %v, start-up error %v", v.Available(), v.StartupError())
		}
	})

	t.Run("R-HA-008 R-HA-056 a refusal after silence is an answer and a start-up error", func(t *testing.T) {
		srv, silent := silentServer(t, func([]string) string {
			return "-WRONGPASS invalid username-password pair or user is disabled.\r\n"
		})
		cfg := standaloneCfg(srv.addr())
		cfg.ConnectTimeout = 100 * time.Millisecond
		v, _ := newTestValkey(t, cfg)
		if isClosed(v.Answered()) {
			t.Fatal("Answered is closed before the server said anything")
		}
		silent.Store(false)
		select {
		case <-v.Answered():
		case <-time.After(5 * time.Second):
			t.Fatalf("no answer after the server refused (status %+v)", v.Status())
		}
		err := v.StartupError()
		if err == nil || !strings.Contains(err.Error(), "WRONGPASS") || v.Status().ReasonCode != ReasonAuthFailed {
			t.Fatalf("start-up error %v, reason code %q", err, v.Status().ReasonCode)
		}
	})

	t.Run("R-HA-008 silence that lasts keeps Answered open and is no start-up error", func(t *testing.T) {
		srv, _ := silentServer(t, func([]string) string { return "+OK\r\n" })
		cfg := standaloneCfg(srv.addr())
		cfg.ConnectTimeout = 50 * time.Millisecond
		v, _ := newTestValkey(t, cfg)
		// Several attempts: 50 ms each, the backoff 50 to 200 ms.
		time.Sleep(400 * time.Millisecond)
		if isClosed(v.Answered()) || v.StartupError() != nil || v.Available() {
			t.Fatalf("answered %v, start-up error %v, available %v", isClosed(v.Answered()), v.StartupError(), v.Available())
		}
	})

	t.Run("R-ARCH-031 R-ARCH-034 a user without the take script is refused at the first connection", func(t *testing.T) {
		srv := accessServer(t, func(args []string) (string, bool) {
			if strings.EqualFold(args[0], "EVAL") {
				return "-NOPERM User quader has no permissions to run the 'eval' command\r\n", true
			}
			return "", false
		})
		reg := prometheus.NewRegistry()
		v, err := NewValkey(t.Context(), standaloneCfg(srv.addr()), "test-pod", "web", nil, reg, WithKeyspaceID(testKeyspace), WithScriptCheck())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(v.Close)
		if !isClosed(v.Answered()) {
			t.Fatalf("a refusal is an answer (status %+v)", v.Status())
		}
		serr := v.StartupError()
		if serr == nil || !strings.Contains(serr.Error(), "NOPERM") || !strings.Contains(serr.Error(), "take script (EVAL)") {
			t.Fatalf("start-up error = %v", serr)
		}
		if v.Available() || v.Status().ReasonCode != ReasonAuthFailed {
			t.Fatalf("status = %+v", v.Status())
		}
	})

	t.Run("R-ARCH-031 R-ARCH-030 a user whose key pattern does not cover the prefix is refused at the first connection", func(t *testing.T) {
		srv := accessServer(t, func(args []string) (string, bool) {
			if strings.EqualFold(args[0], "SET") {
				return "-NOPERM No permissions to access a key\r\n", true
			}
			return "", false
		})
		v, _ := newTestValkey(t, standaloneCfg(srv.addr()))
		serr := v.StartupError()
		if serr == nil || !strings.Contains(serr.Error(), "probe key") || v.Status().ReasonCode != ReasonAuthFailed {
			t.Fatalf("start-up error = %v, status %+v", serr, v.Status())
		}
	})

	t.Run("R-ARCH-031 the scripts allowed: the client connects after EVAL and EVALSHA with no key; without the check none is sent", func(t *testing.T) {
		srv := accessServer(t, func([]string) (string, bool) { return "", false })
		v, err := NewValkey(t.Context(), standaloneCfg(srv.addr()), "test-pod", "web", nil, prometheus.NewRegistry(), WithKeyspaceID(testKeyspace), WithScriptCheck())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(v.Close)
		if !isClosed(v.Answered()) || !v.Available() || v.StartupError() != nil {
			t.Fatalf("status = %+v, start-up error %v", v.Status(), v.StartupError())
		}
		var eval, evalsha bool
		for _, c := range srv.seen() {
			switch {
			case strings.EqualFold(c[0], "EVAL") && len(c) >= 3 && c[2] == "0":
				eval = true
			case strings.EqualFold(c[0], "EVALSHA") && len(c) >= 3 && c[1] == takeScriptSHA && c[2] == "0":
				evalsha = true
			}
		}
		if !eval || !evalsha {
			t.Fatalf("EVAL %v, EVALSHA %v with no key; commands %v", eval, evalsha, srv.seen())
		}

		plain := accessServer(t, func([]string) (string, bool) { return "", false })
		w, _ := newTestValkey(t, standaloneCfg(plain.addr()))
		eventually(t, "availability", 5*time.Second, w.Available)
		for _, c := range plain.seen() {
			if strings.EqualFold(c[0], "EVAL") || strings.EqualFold(c[0], "EVALSHA") {
				t.Fatalf("a client without WithScriptCheck sent %v", c)
			}
		}
	})

	t.Run("R-HA-057 an access check that fails for another reason does not hold the client back", func(t *testing.T) {
		srv := accessServer(t, func(args []string) (string, bool) {
			if strings.EqualFold(args[0], "SET") {
				return "-OOM command not allowed when used memory > 'maxmemory'.\r\n", true
			}
			return "", false
		})
		v, _ := newTestValkey(t, standaloneCfg(srv.addr()))
		if !isClosed(v.Answered()) || v.StartupError() != nil || v.current() == nil {
			t.Fatalf("answered %v, start-up error %v, connected %v", isClosed(v.Answered()), v.StartupError(), v.current() != nil)
		}
	})
}
