package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/observability/health"
	"github.com/guided-traffic/quader/internal/vuln/imagescan"
	"github.com/guided-traffic/quader/internal/vuln/scanner"
)

// The role scanner (R-ARCH-036, R-HA-065, ADR-212): one area of its own, built without any infrastructure area, and the scanner's
// listener of step 9. The role all builds the same area and opens the listener on loopback, so that its worker calls its own scanner
// through the same client, token and checks as in the chart (Q-ARCH-531).

// AreaScanner is the analyser, the registry client of the pull, the checks of R-VULN-036, the slots and the spool, the reaper and the
// metrics of the role scanner (Q-ARCH-527).
const AreaScanner Area = "scanner"

// scannerArea is the scanner of the replica and the TLS of its listener and of its link to the scan listener.
type scannerArea struct {
	svc *scanner.Service
	// listenerTLS serves the pair and requires a client certificate of the internal CA; nil without TLS.
	listenerTLS *httpserver.ListenerTLS
	// link is the scanner's side of its pull: the pair it presents and the CA it verifies the scan listener against; nil without TLS.
	link *httpserver.LinkTLS
	// analyser is the version check's result: an error while the binary does not answer its version.
	analyser error
}

// wireScanner builds the scanner. The blob area is optional (Q-ARCH-300): the role all hands it, and the scanner then takes its room
// from the one spool of the process; the role scanner has none and opens the spool alone, without the object store.
func (a *App) wireScanner(ctx context.Context, b blobArea) (scannerArea, error) {
	if a.id.Role == config.RoleAll && !a.imageScanningOn() {
		return scannerArea{}, nil // the role all scans no image: no scanner and no listener
	}
	cfg := a.cfg
	images := cfg.Vulnerabilities.Images
	log := a.log.With("component", "scanner")
	var spool *blob.Spool
	if b.store != nil {
		spool = b.store.Spool()
	} else {
		s, err := blob.NewSpool(cfg.Storage.Spool, blob.WithSpoolLogger(log))
		if err != nil {
			return scannerArea{}, err
		}
		spool = s
	}
	out := scannerArea{}
	var analyser imagescan.Analyser
	if ov := a.o.Vuln; ov != nil && ov.ImageAnalyser != nil {
		analyser = ov.ImageAnalyser
	} else {
		trivy, err := imagescan.NewTrivy(ctx, images.Binary)
		if err != nil {
			// The replica stays not ready and says why (R-HA-065); a call that reaches it meanwhile is analyser_failed.
			log.ErrorContext(ctx, "the analyser does not answer its version: the scanner stays not ready", "binary", images.Binary, "error", err)
			out.analyser = err
			analyser = brokenAnalyser{err: err}
		} else {
			analyser = trivy
		}
	}
	var err error
	if out.listenerTLS, err = httpserver.NewLinkListenerTLS(httpserver.LinkScanner, cfg.Server, a.tlsOptions(httpserver.LinkScanner)...); err != nil {
		return scannerArea{}, err
	}
	serverName := images.Registry.ServerName
	if serverName == "" {
		serverName = hostOf(images.Registry.URL)
	}
	if out.link, err = httpserver.NewLinkTLS(httpserver.LinkScan, cfg.Server, serverName, log, a.tlsOptions(httpserver.LinkScan)...); err != nil {
		return scannerArea{}, err
	}
	m, err := scanner.NewMetrics(a.tele.metrics.Registry())
	if err != nil {
		return scannerArea{}, err
	}
	reg := scanner.Registry{URL: images.Registry.URL}
	if out.link != nil {
		reg.TLS = out.link.Config
	}
	if reg.URL == "" {
		// The role all: its own scan listener, whose address step 9 sets once it is bound (scanListenerBound).
		reg.URL = "http://127.0.0.1:0"
	}
	svc, err := scanner.New(scanner.Options{
		Analyser: analyser, Registry: reg, Space: spoolSpace{spool}, ScanDir: images.ScanDir, Slots: images.Scanner.Slots,
		PullTimeout: images.PullTimeout, Reaper: scanner.NewReaper(log), Metrics: m, Log: log,
		Content: imagescan.ContentLimits{MaxLayers: images.MaxLayers, MaxImageBytes: int64(images.MaxImageSize), MaxUncompressedBytes: int64(images.MaxUncompressedSize), MaxEntries: images.MaxEntries},
		Run:     imagescan.Limits{Timeout: images.Timeout, MemoryBytes: int64(images.MemoryLimit), OutputBytes: imageOutputBytes, StderrBytes: 16 << 10},
	})
	if err != nil {
		return scannerArea{}, err
	}
	if err := svc.Clean(); err != nil {
		return scannerArea{}, fmt.Errorf("the scanner's working directory %s: %w", images.ScanDir, err)
	}
	out.svc = svc
	if err := a.startComponent(ctx, &scannerComponent{area: out}); err != nil {
		return scannerArea{}, err
	}
	if a.id.Role == config.RoleScanner {
		a.health.AddReadiness("scanner", a.scannerReady(out, true), false)
	} else {
		// The role all: the scanner is one part of a process that serves every route. A missing analyser or a pair without client
		// authentication is reported in /readyz?verbose=1 and answered per call, but it does not take the API and the registry
		// down with it (Q-ARCH-551).
		a.health.AddReported("scanner", a.scannerReady(out, false))
	}
	return out, nil
}

// scannerReady is the readiness of the scanner (R-HA-065): the pair and the internal CA have loaded, the pair allows client
// authentication, the analyser answered its version, the spool directory is writable, and the process is not draining. It does not
// depend on packages or Valkey: a scan listener that cannot be reached is the call's answer, not the pod's state. gate marks the
// failures of the configuration as such (the role scanner); the role all reports them only (Q-ARCH-551).
func (a *App) scannerReady(s scannerArea, gate bool) health.CheckFunc {
	dir := a.cfg.Vulnerabilities.Images.ScanDir
	configured := func(err error, reason string) error {
		if gate {
			return health.ConfigError(err, reason)
		}
		return health.WithReason(err, reason)
	}
	return func(context.Context) error {
		if s.svc.Draining() {
			return health.WithReason(errUnavailable, "draining")
		}
		if s.analyser != nil {
			return configured(fmt.Errorf("the analyser: %w", s.analyser), "analyser")
		}
		if s.listenerTLS != nil {
			if err := s.listenerTLS.Loaded(); err != nil {
				return configured(err, "certificate")
			}
		}
		if s.link != nil {
			if err := s.link.Loaded(); err != nil {
				return configured(err, "certificate")
			}
			if err := s.link.Fit(); err != nil {
				return configured(err, "certificate")
			}
		}
		f, err := os.CreateTemp(dir, ".ready-")
		if err != nil {
			return health.WithReason(fmt.Errorf("the spool directory %s is not writable: %w", dir, err), "spool")
		}
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil
	}
}

// brokenAnalyser answers every analysis with the reason its binary does not run.
type brokenAnalyser struct{ err error }

func (b brokenAnalyser) Version() string { return "unavailable" }
func (b brokenAnalyser) Analyse(context.Context, string, string, string, imagescan.Limits) error {
	return &imagescan.ScanError{Code: imagescan.CodeAnalyserFailed, Detail: "the scanner's analyser does not run: " + b.err.Error()}
}

func (b brokenAnalyser) Detect(context.Context, string, string, string, imagescan.DatabaseAccess, imagescan.Limits) error {
	return b.err
}

// scannerComponent drains the scanner at SIGTERM and ends the analyses still running at the shutdown timeout (R-HA-065): both are
// answered 503 scanner_draining, which the worker puts off without an attempt. It runs the reloader of the link's material; the
// listener's runs with its server.
type scannerComponent struct {
	area scannerArea
	link *linkComponent
}

func (c *scannerComponent) Name() string { return "scanner" }

func (c *scannerComponent) Start(ctx context.Context) error {
	if c.area.link != nil {
		c.link = &linkComponent{name: "link-scan", link: c.area.link}
		return c.link.Start(ctx)
	}
	return nil
}

func (c *scannerComponent) Drain(context.Context)   { c.area.svc.Drain() }
func (c *scannerComponent) Quiesce(context.Context) { c.area.svc.Stop() }
func (c *scannerComponent) Stop(ctx context.Context) error {
	c.area.svc.Stop()
	if c.link != nil {
		return c.link.Stop(ctx)
	}
	return nil
}

// scannerListenAddr is the address of the scanner's listener: server.listen in the role scanner, a loopback port in the role all,
// whose client listener holds server.listen (Q-ARCH-531).
func (a *App) scannerListenAddr() string {
	if a.id.Role == config.RoleAll {
		return "127.0.0.1:0"
	}
	return a.cfg.Server.Listen
}

// startScanner binds the scanner's listener and serves POST /scan/v1/analyses and nothing else (R-ARCH-036).
func (a *App) startScanner() error {
	s, ok := lookup[scannerArea](a.parts)
	switch {
	case ok && s.svc == nil && a.id.Role == config.RoleAll:
		return nil
	case !ok || s.svc == nil:
		return errors.New("the scanner's listener: the scanner area is not wired")
	}
	srv, err := a.newServer(httpserver.LinkScanner, a.scannerListenAddr(), scannerRouter(s.svc), s.listenerTLS)
	if err != nil {
		return err
	}
	if err := srv.Bind(); err != nil {
		return fmt.Errorf("the scanner's listener: %w", err)
	}
	a.scannerSrv = srv
	a.bg.Add(1)
	go func() {
		defer a.bg.Done()
		if err := srv.Serve(); err != nil {
			a.fail(fmt.Errorf("the scanner's listener: %w", err))
		}
	}()
	a.log.Info("scanner listener bound", "addr", srv.Addr().String())
	return nil
}

// scannerRouter is the router of the scanner's listener: the one route of the call. Its router is its own; no route family of an
// endpoint, no management API and no package protocol is served there (R-ARCH-036).
func scannerRouter(svc *scanner.Service) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(imagescan.AnalysesPath, svc.Handler())
	return mux
}

// ScannerAddr returns the bound address of the scanner's listener, nil for a replica without one.
func (a *App) ScannerAddr() net.Addr {
	if a.scannerSrv == nil {
		return nil
	}
	return a.scannerSrv.Addr()
}

// hostOf is the host of a URL's origin, the name a link verifies when its server_name is empty.
func hostOf(raw string) string {
	rest := raw
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	if h, _, err := net.SplitHostPort(rest); err == nil {
		return h
	}
	return rest
}

// loopbackOrigin is the origin of a listener bound on loopback: the role all's worker calls its own scanner there, and its scanner
// pulls from its own scan listener there (Q-ARCH-531).
func loopbackOrigin(addr net.Addr, tls bool) string {
	scheme := "http"
	if tls {
		scheme = "https"
	}
	return scheme + "://" + addr.String()
}
