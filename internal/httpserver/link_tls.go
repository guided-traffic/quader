package httpserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/tlsfiles"
)

// The links between quader's pods (R-ARCH-038, Q-ARCH-552): the worker's call of the scanner and the scanner's pull from the scan
// listener of packages. Each end presents the first pair of server.tls.certificates, its own kind's pair in the chart, and verifies the
// other against server.internal_tls.ca_file; the listener of a link requires a client certificate that chains to that CA and lists the
// link's client name, and the client requires the server name in the server's certificate, each listed exactly among the DNS names:
// a wildcard does not stand for it and the common name is not read. A peer that fails is refused in the handshake before any HTTP is
// read. Every file reloads without a restart, compared by content (R-ARCH-035), and a connection is opened per call, so a rotation
// reaches the next call. Without TLS (server.tls.certificates empty) the links are plain HTTP and no name is checked (Q-HA-322).

// Link names of the two links, the value of the listener label of their material.
const (
	LinkScanner = "scanner"
	LinkScan    = "scan"
)

// ErrLinkNeedsCA is the refusal of a link with TLS and without the internal trust anchor.
var ErrLinkNeedsCA = errors.New("server.internal_tls.ca_file is required for the links between quader's pods while server.tls serves TLS (R-ARCH-038)")

// ErrPeerName is the refusal of a peer whose certificate of the internal CA does not list the name the link requires (Q-ARCH-552).
var ErrPeerName = errors.New("the certificate does not list the name of the kind of pod this link admits")

// NewLinkListenerTLS is the TLS of the listener of a link: the scanner's listener or the scan listener. It serves the pairs of
// server.tls and requires a client certificate that chains to server.internal_tls.ca_file, whatever server.tls.client_auth says
// about the client listener (R-ARCH-037), and that lists one of clientNames among its DNS names (Q-ARCH-552): the kinds of pod the
// listener admits, one for the scanner's listener (the worker), one per kind of scanner for the scan listener. A route may narrow it to
// one kind with PeerName. No names let the CA alone admit, which only the role all does (Q-ARCH-553). Nil without TLS: the listener
// serves plain HTTP.
func NewLinkListenerTLS(link string, s config.Server, clientNames []string, opts ...tlsfiles.Option) (*ListenerTLS, error) {
	if !s.TLS.Enabled() {
		return nil, nil
	}
	if s.InternalTLS.CAFile == "" {
		return nil, fmt.Errorf("%s listener: %w", link, ErrLinkNeedsCA)
	}
	l, err := NewListenerTLS(link, config.ServerTLS{
		Certificates: s.TLS.Certificates, ClientCAFile: s.InternalTLS.CAFile, ClientAuth: config.ClientAuthRequire,
		MinVersion: s.TLS.MinVersion, ReloadInterval: s.TLS.ReloadInterval,
	}, opts...)
	if err != nil || len(clientNames) == 0 {
		return l, err
	}
	names := slices.Clone(clientNames)
	l.verify = func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return fmt.Errorf("%s listener: no client certificate: %w", link, ErrPeerName)
		}
		if PeerName(cs.PeerCertificates[0], names) == "" {
			return fmt.Errorf("%s listener admits %s only: %w", link, strings.Join(names, ", "), HasName(cs.PeerCertificates[0], names[0]))
		}
		return nil
	}
	return l, nil
}

// PeerName is the first of names that the certificate lists exactly among its DNS names (HasName), or "" for none: the kind of pod a
// verified client certificate of a link belongs to, for a route that admits one kind of those its listener admits (Q-ARCH-552).
func PeerName(c *x509.Certificate, names []string) string {
	for _, n := range names {
		if HasName(c, n) == nil {
			return n
		}
	}
	return ""
}

// HasName reports whether the certificate lists name among its DNS names, exactly and without regard to case (Q-ARCH-552): a
// wildcard name does not stand for it, and the subject's common name is not read. The error names what the certificate lists.
func HasName(c *x509.Certificate, name string) error {
	if c == nil {
		return ErrPeerName
	}
	for _, n := range c.DNSNames {
		if strings.EqualFold(n, name) {
			return nil
		}
	}
	return fmt.Errorf("%w: it lists %q", ErrPeerName, c.DNSNames)
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
// server.internal_tls.ca_file. serverName is the name verified in the far end's certificate: a DNS name must be listed exactly among
// its DNS names (Q-ARCH-552), an IP address is verified against its IP addresses by Go's rules, and an empty one is the host of the
// address dialled. Nil without TLS: the link is plain.
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
// loaded is an empty pool, which trusts nothing), the link's server name and the pair presented when the far end asks for one. Go
// verifies the chain and the name first; the exact check of a DNS name runs after it, so it narrows Go's rules and widens nothing.
func (l *LinkTLS) Config() *tls.Config {
	pool := l.material.Pool()
	if pool == nil {
		pool = x509.NewCertPool()
	}
	cfg := &tls.Config{
		MinVersion: l.min, RootCAs: pool, ServerName: l.serverName, NextProtos: []string{"http/1.1"},
		GetClientCertificate: l.material.ClientCertificate,
	}
	if name := l.serverName; name != "" && net.ParseIP(name) == nil {
		cfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return fmt.Errorf("the link to the %s: %w", l.link, ErrPeerName)
			}
			if err := HasName(cs.PeerCertificates[0], name); err != nil {
				return fmt.Errorf("the link to the %s requires %s: %w", l.link, name, err)
			}
			return nil
		}
	}
	return cfg
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
