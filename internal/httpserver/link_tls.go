package httpserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/tlsfiles"
)

// The links between quader's pods (R-ARCH-038, Q-ARCH-529): the worker's call of the scanner and the scanner's pull from the scan
// listener of packages. Each end presents the first pair of server.tls.certificates and verifies the other against
// server.internal_tls.ca_file; the listener of a link requires a client certificate that chains to that CA, so a peer without one
// is refused in the handshake before any HTTP is read. No hostname is checked in a client certificate: the CA admits a peer. Every
// file reloads without a restart, compared by content (R-ARCH-035), and a connection is opened per call, so a rotation reaches the
// next call. Without TLS (server.tls.certificates empty) the links are plain HTTP (Q-HA-322).

// Link names of the two links, the value of the listener label of their material.
const (
	LinkScanner = "scanner"
	LinkScan    = "scan"
)

// ErrLinkNeedsCA is the refusal of a link with TLS and without the internal trust anchor.
var ErrLinkNeedsCA = errors.New("server.internal_tls.ca_file is required for the links between quader's pods while server.tls serves TLS (R-ARCH-038)")

// NewLinkListenerTLS is the TLS of the listener of a link: the scanner's listener or the scan listener. It serves the pairs of
// server.tls and requires a client certificate that chains to server.internal_tls.ca_file, whatever server.tls.client_auth says
// about the client listener (R-ARCH-037). Nil without TLS: the listener serves plain HTTP.
func NewLinkListenerTLS(link string, s config.Server, opts ...tlsfiles.Option) (*ListenerTLS, error) {
	if !s.TLS.Enabled() {
		return nil, nil
	}
	if s.InternalTLS.CAFile == "" {
		return nil, fmt.Errorf("%s listener: %w", link, ErrLinkNeedsCA)
	}
	return NewListenerTLS(link, config.ServerTLS{
		Certificates: s.TLS.Certificates, ClientCAFile: s.InternalTLS.CAFile, ClientAuth: config.ClientAuthRequire,
		MinVersion: s.TLS.MinVersion, ReloadInterval: s.TLS.ReloadInterval,
	}, opts...)
}

// LinkTLS is the client side of a link: the pair it presents and the CA it verifies the far end against, under the link's server
// name. Config builds a configuration per connection from the material of the moment, so a rotated pair or CA is presented and
// trusted from the next connection on.
type LinkTLS struct {
	link       string
	serverName string
	min        uint16
	interval   time.Duration
	material   *tlsfiles.Reloader
	log        *slog.Logger
	// unfit is the generation of the material whose pair was last reported as unfit for client authentication.
	unfit   atomic.Uint64
	running atomic.Bool
}

// NewLinkTLS builds the client side of a link from the server section: the first pair of server.tls.certificates and the CA of
// server.internal_tls.ca_file. serverName is the name verified in the far end's certificate. Nil without TLS: the link is plain.
func NewLinkTLS(link string, s config.Server, serverName string, log *slog.Logger, opts ...tlsfiles.Option) (*LinkTLS, error) {
	if !s.TLS.Enabled() {
		return nil, nil
	}
	if s.InternalTLS.CAFile == "" {
		return nil, fmt.Errorf("the link to the %s: %w", link, ErrLinkNeedsCA)
	}
	minVer, err := tlsVersion(s.TLS.MinVersion)
	if err != nil {
		return nil, fmt.Errorf("the link to the %s: %w", link, err)
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	first := s.TLS.Certificates[0]
	spec := tlsfiles.Spec{Pairs: []tlsfiles.Pair{{CertFile: first.CertFile, KeyFile: first.KeyFile}}, CAFiles: []string{s.InternalTLS.CAFile}}
	l := &LinkTLS{
		link: link, serverName: serverName, min: minVer, interval: s.TLS.ReloadInterval, log: log,
		material: tlsfiles.New(spec, tlsfiles.Labels{Kind: tlsfiles.KindClient, Listener: link}, opts...),
	}
	if l.interval <= 0 {
		l.interval = config.DefaultTLSReloadInterval
	}
	return l, nil
}

// Config is the client configuration of one connection of the link: the CA of the moment as the only roots (a CA that has not
// loaded is an empty pool, which trusts nothing), the link's server name and the pair presented when the far end asks for one.
func (l *LinkTLS) Config() *tls.Config {
	pool := l.material.Pool()
	if pool == nil {
		pool = x509.NewCertPool()
	}
	return &tls.Config{
		MinVersion: l.min, RootCAs: pool, ServerName: l.serverName, NextProtos: []string{"http/1.1"},
		GetClientCertificate: l.material.ClientCertificate,
	}
}

// Loaded returns nil once the pair and the CA have loaded at least once.
func (l *LinkTLS) Loaded() error { return l.material.Loaded() }

// Run polls the files every reload interval until ctx ends; a second call while one runs returns at once.
func (l *LinkTLS) Run(ctx context.Context) {
	if !l.running.CompareAndSwap(false, true) {
		return
	}
	defer l.running.Store(false)
	l.material.Run(ctx, l.interval)
}

// Fit reports whether the loaded pair allows client authentication (R-ARCH-038): a certificate without the extended key usage
// clientAuth, when it lists any, is refused by every listener of a link. The reason is logged once per loaded pair.
func (l *LinkTLS) Fit() error {
	leaves := l.material.Leaves()
	if len(leaves) == 0 {
		return l.material.Loaded()
	}
	err := ClientAuthFit(leaves[0])
	if err != nil {
		if gen := l.material.Generation(); l.unfit.Swap(gen) != gen {
			l.log.Error("the pair presented on a link between quader's pods does not allow client authentication; the far end refuses it",
				"link", l.link, "error", err)
		}
	}
	return err
}

// ClientAuthFit reports whether a certificate may authenticate a client of a link: its extended key usages list clientAuth or any,
// or none at all (R-ARCH-038). A pair issued with serverAuth alone is unfit.
func ClientAuthFit(c *x509.Certificate) error {
	if c == nil {
		return errors.New("no certificate")
	}
	if len(c.ExtKeyUsage) == 0 && len(c.UnknownExtKeyUsage) == 0 {
		return nil
	}
	if slices.Contains(c.ExtKeyUsage, x509.ExtKeyUsageClientAuth) || slices.Contains(c.ExtKeyUsage, x509.ExtKeyUsageAny) {
		return nil
	}
	return fmt.Errorf("the certificate %q lists the extended key usages %v without clientAuth: issue it with the usage client auth, or with none", c.Subject.CommonName, c.ExtKeyUsage)
}

// ClientAuthFit reports whether the listener's pair may also authenticate the process as a client of a link (R-ARCH-038): the
// scanner serves and presents the same pair, and a pair without client authentication is unfit for it.
func (l *ListenerTLS) ClientAuthFit() error {
	leaves := l.material.Leaves()
	if len(leaves) == 0 {
		return l.material.Loaded()
	}
	return ClientAuthFit(leaves[0])
}
