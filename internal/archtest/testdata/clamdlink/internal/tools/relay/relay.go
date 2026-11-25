// Package relay is a tool of the repository, not code of the binary: not checked.
package relay

import "net"

// Dial speaks to clamd over TCP.
func Dial() (net.Conn, error) {
	c, err := net.Dial("tcp", "127.0.0.1:3310")
	if err == nil {
		_, _ = c.Write([]byte("zINSTREAM\x00"))
	}
	return c, err
}
