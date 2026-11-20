package outbound

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// The links between quader's own pods (architecture.md R-ARCH-038): the worker's call of the scanner and the scanner's pull from the
// scan listener of packages. Their destinations are the installation's own Services, named by the chart, so the egress address policy
// of the Factory does not apply (it refuses cluster addresses by design), and no outbound proxy is used; what guards them is mutual TLS
// against the installation's internal CA. Like NewDialer, the client lives here so that this package stays the only place that dials.

// LinkOptions configure [LinkClient].
type LinkOptions struct {
	// TLS returns the configuration of one connection: the pair presented and the CA verified against (httpserver.LinkTLS.Config).
	// A configuration that names no server name verifies the host of the address dialled. Nil: the link is plain HTTP (an
	// installation without TLS).
	TLS func() *tls.Config
	// KeepAlive keeps connections for the next request; off, every request opens a connection of its own, so that a Service spreads
	// the requests over its pods.
	KeepAlive bool
	// OnDial is told of every connection opened; nil tells nobody.
	OnDial func()
	// Dial replaces the dialer; tests only.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// LinkClient returns the HTTP client of a link. It follows no redirect: a link reads the one address of its configuration.
func LinkClient(o LinkOptions) *http.Client {
	dial := o.Dial
	if dial == nil {
		d := NewDialer(30*time.Second, nil)
		dial = d.DialContext
	}
	counted := func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dial(ctx, network, addr)
		if err == nil && o.OnDial != nil {
			o.OnDial()
		}
		return conn, err
	}
	t := &http.Transport{DialContext: counted, DisableKeepAlives: !o.KeepAlive, DisableCompression: true, MaxIdleConnsPerHost: 2}
	if o.TLS != nil {
		t.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			raw, err := counted(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			conn := tls.Client(raw, linkConfig(o.TLS(), addr))
			if err := conn.HandshakeContext(ctx); err != nil {
				_ = raw.Close()
				return nil, err
			}
			return conn, nil
		}
	}
	return &http.Client{Transport: t, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// linkConfig is cfg with the host of addr as the name verified when cfg names none.
func linkConfig(cfg *tls.Config, addr string) *tls.Config {
	if cfg == nil {
		cfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if cfg.ServerName != "" {
		return cfg
	}
	out := cfg.Clone()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	out.ServerName = host
	return out
}
