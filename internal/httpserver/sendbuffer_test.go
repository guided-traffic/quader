package httpserver

import (
	"net"
	"net/http"
	"syscall"
	"testing"
)

func TestSendBuffer(t *testing.T) {
	t.Run("R-ARCH-005 every accepted connection gets server.send_buffer as its SO_SNDBUF, so the kernel holds a bounded part of a response its client does not read", func(t *testing.T) {
		for _, n := range []int{0, 128 << 10} {
			s, err := NewServer(ServerConfig{Name: "test", Addr: "127.0.0.1:0", SendBuffer: n, Handler: http.NotFoundHandler()})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Bind(); err != nil {
				t.Fatal(err)
			}
			c, err := net.Dial("tcp", s.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			a, err := s.ln.Accept()
			if err != nil {
				t.Fatal(err)
			}
			rc, err := a.(*lingerConn).tcp.SyscallConn()
			if err != nil {
				t.Fatal(err)
			}
			var got int
			var serr error
			if err := rc.Control(func(fd uintptr) { got, serr = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_SNDBUF) }); err != nil || serr != nil {
				t.Fatal(err, serr)
			}
			// Linux reports twice what was set (its bookkeeping, which the guard charges), other systems what was set.
			if n > 0 && got != n && got != 2*n {
				t.Errorf("SO_SNDBUF %d, want %d or %d", got, n, 2*n)
			}
			if n == 0 && (got == 128<<10 || got == 256<<10) {
				t.Errorf("SO_SNDBUF %d without server.send_buffer: the listener set it", got)
			}
			_ = a.Close()
			_ = c.Close()
			_ = s.ln.Close()
		}
	})
}
