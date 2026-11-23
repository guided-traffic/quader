package cluster

import (
	"bufio"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSentinel is a minimal Sentinel for one primary name: it answers what valkey-go's Sentinel client and Quader's
// probe ask (HELLO, SENTINEL SENTINELS, SENTINEL GET-MASTER-ADDR-BY-NAME, SUBSCRIBE and UNSUBSCRIBE of the event
// channels) and, on failover, names another primary and publishes +switch-master to every subscribed connection, as a
// Sentinel does once the election has promoted a replica.
type fakeSentinel struct {
	ln   net.Listener
	name string

	mu      sync.Mutex
	primary string // host:port the Sentinel names
	subs    []*sentinelConn
}

type sentinelConn struct {
	mu   sync.Mutex
	conn net.Conn
}

func (c *sentinelConn) write(s string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := io.WriteString(c.conn, s)
	return err
}

func newFakeSentinel(t *testing.T, name, primary string) *fakeSentinel {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSentinel{ln: ln, name: name, primary: primary}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(&sentinelConn{conn: conn})
		}
	}()
	return s
}

func (s *fakeSentinel) addr() string { return s.ln.Addr().String() }

// failover names primary from now on and publishes +switch-master <name> <old-ip> <old-port> <new-ip> <new-port>.
func (s *fakeSentinel) failover(t *testing.T, primary string) {
	t.Helper()
	s.mu.Lock()
	old := s.primary
	s.primary = primary
	subs := append([]*sentinelConn(nil), s.subs...)
	s.mu.Unlock()
	oh, op, _ := net.SplitHostPort(old)
	nh, np, _ := net.SplitHostPort(primary)
	msg := push("message", "+switch-master", strings.Join([]string{s.name, oh, op, nh, np}, " "))
	for _, c := range subs {
		_ = c.write(msg) // a connection the client has closed is not a subscriber any more
	}
}

func (s *fakeSentinel) serve(c *sentinelConn) {
	defer c.conn.Close()
	r := bufio.NewReader(c.conn)
	for {
		args, err := readCommand(r)
		if err != nil {
			return
		}
		if c.write(s.reply(c, args)) != nil {
			return
		}
	}
}

func (s *fakeSentinel) reply(c *sentinelConn, args []string) string {
	switch strings.ToUpper(args[0]) {
	case "HELLO":
		return "%3\r\n" + bulk("server") + bulk("valkey") + bulk("version") + bulk("9.1.0") + bulk("mode") + bulk("sentinel")
	case "PING":
		return "+PONG\r\n"
	case "CLIENT":
		return "+OK\r\n"
	case "SUBSCRIBE", "UNSUBSCRIBE":
		kind := strings.ToLower(args[0])
		if kind == "subscribe" {
			s.mu.Lock()
			s.subs = append(s.subs, c)
			s.mu.Unlock()
		}
		var out strings.Builder
		for i, ch := range args[1:] {
			n := i + 1
			if kind == "unsubscribe" {
				n = 0
			}
			out.WriteString(">3\r\n" + bulk(kind) + bulk(ch) + ":" + strconv.Itoa(n) + "\r\n")
		}
		return out.String()
	case "SENTINEL":
		if len(args) < 3 || args[2] != s.name {
			return "*-1\r\n"
		}
		switch strings.ToUpper(args[1]) {
		case "SENTINELS":
			return "*0\r\n"
		case "GET-MASTER-ADDR-BY-NAME":
			s.mu.Lock()
			host, port, _ := net.SplitHostPort(s.primary)
			s.mu.Unlock()
			return "*2\r\n" + bulk(host) + bulk(port)
		}
	}
	return "-ERR unknown command '" + args[0] + "'\r\n"
}

func push(kind, channel, message string) string {
	return ">3\r\n" + bulk(kind) + bulk(channel) + bulk(message)
}

// F-249: a replica that starts while Sentinel fails the primary over. The Sentinel still names the old primary, which
// gives no answer; that is silence, not a verdict, so the start keeps waiting (Answered open, no start-up error). Once
// the Sentinel names the promoted replica, the next attempt is answered there and the start goes on bound to it.
func TestR_HA_008_StartDuringASentinelFailoverWaitsForTheNewPrimary(t *testing.T) {
	oldPrimary, _ := silentServer(t, func([]string) string { return "-ERR the old primary never answers\r\n" })
	newPrimary := accessServer(t, func(args []string) (string, bool) {
		if strings.EqualFold(args[0], "ROLE") {
			return "*3\r\n" + bulk("master") + ":0\r\n*0\r\n", true
		}
		return "", false
	})
	sentinel := newFakeSentinel(t, "quader", oldPrimary.addr())

	cfg := sentinelCfg(sentinel.addr())
	cfg.ConnectTimeout = 100 * time.Millisecond
	v, _ := newTestValkey(t, cfg)

	// NewValkey returned after the first attempt: the Sentinel answered and named a primary that did not.
	if isClosed(v.Answered()) {
		t.Fatalf("Answered is closed while the named primary gives no answer (status %+v)", v.Status())
	}
	if err := v.StartupError(); err != nil {
		t.Fatalf("a Sentinel that names a primary that does not answer is silence, not a start-up error: %v", err)
	}
	st := v.Status()
	// The attempt ended at the old primary's deadline while the one Sentinel answered.
	if v.Available() || st.ReasonCode != ReasonTimeout {
		t.Fatalf("status during the failover = %+v, want unavailable with the reason %q", st, ReasonTimeout)
	}

	sentinel.failover(t, newPrimary.addr())
	select {
	case <-v.Answered():
	case <-time.After(5 * time.Second):
		t.Fatalf("no answer within 5 s of the Sentinel naming the new primary (status %+v)", v.Status())
	}
	if err := v.StartupError(); err != nil {
		t.Fatalf("start-up error after the failover: %v", err)
	}
	if !v.Available() {
		t.Fatalf("not available after the new primary answered (status %+v)", v.Status())
	}
	if got := v.Status().Primary; got != newPrimary.addr() {
		t.Fatalf("bound to %q, want the promoted primary %q", got, newPrimary.addr())
	}
}
