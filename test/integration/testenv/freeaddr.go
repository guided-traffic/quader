package testenv

import (
	"context"
	"net"
	"sync"
	"testing"
)

// handedOut are the addresses FreeAddr returned in this process. The kernel gives a port back to the next listener on port 0 as
// soon as the last one closed, so two calls in a row can return the same address; a replica started with the same address for two
// of its listeners refuses its configuration ("uses port N, which server.listen already uses"), and two servers of one test that
// got one address collide (Q-HA-285, F-047).
var handedOut = struct {
	sync.Mutex
	addrs map[string]bool
}{addrs: map[string]bool{}}

// FreeAddr returns a loopback address nothing listens on and that no earlier call of this process returned, for a listener the
// test starts later (a replica's process, a server bound after its address went into a configuration).
func FreeAddr(t testing.TB) string {
	t.Helper()
	handedOut.Lock()
	defer handedOut.Unlock()
	for range 100 {
		l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := l.Addr().String()
		_ = l.Close()
		if !handedOut.addrs[addr] {
			handedOut.addrs[addr] = true
			return addr
		}
	}
	t.Fatal("no free address that was not handed out before")
	return ""
}
