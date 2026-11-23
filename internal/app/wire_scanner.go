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
	mscanner "github.com/guided-traffic/quader/internal/malware/scanner"
	"github.com/guided-traffic/quader/internal/malware/updater"
	"github.com/guided-traffic/quader/internal/observability/health"
	"github.com/guided-traffic/quader/internal/vuln/dbloader"
	"github.com/guided-traffic/quader/internal/vuln/imagescan"
	"github.com/guided-traffic/quader/internal/vuln/scanner"
)

// The role scanner (R-ARCH-036, R-HA-065, ADR-212, ADR-229): one area of its own, built without any infrastructure area, and the
// scanner's listener of step 9. The role all builds no scanner of any kind (Q-ARCH-563, Q-ARCH-560): a development process does not
// pretend to scan, and its worker part calls a scanner only when vulnerabilities.images.scanner.url names one.

// AreaScanner is the analyser, the registry client of the pull, the checks of R-VULN-036, the slots and the spool, the reaper and the
// metrics of the role scanner (Q-ARCH-527); of the kind trivy-db the loader of the database alone (Q-ARCH-564).
const AreaScanner Area = "scanner"

// The names of the two scanner units where an operator or an auditor reads them (ha-and-helm.md Q-HA-372, Q-HA-373): the log component
// of the unit's process and the listener label of its listener and of the worker's link to it are the unit's name; its readiness
// check, in the scanner's process and in the process that calls it, is that name in camel case (operations-and-observability.md
// Q-OPS-221).
const (
	unitVulnScannerTrivy         = httpserver.LinkVulnScannerTrivy
	unitMalwareScannerClamav     = httpserver.LinkMalwareScannerClamav
	unitMalwareUpdaterFreshclam  = httpserver.LinkMalwareUpdaterFreshclam
	checkVulnScannerTrivy        = "vulnScannerTrivy"
	checkMalwareScannerClamav    = "malwareScannerClamav"
	checkMalwareUpdaterFreshclam = "malwareUpdaterFreshclam"
)

// scannerArea is the scanner of the replica and the TLS of its listener and of its link to the scan listener: the image scanner (svc),
// the loader of its database (loader) of a process of the kind trivy-db (wire_vuln_loader.go), the malware scanner (malware) of a
// process of the kind clamav (wire_malware_scanner.go), or the updater (updater) of a process of the kind freshclam
// (wire_malware_updater.go).
type scannerArea struct {
	svc     *scanner.Service
	loader  *dbloader.Loader
	malware *mscanner.Service
	updater *updater.Service
	// listenerTLS serves the pair and requires a client certificate of the internal CA; nil without TLS.
	listenerTLS *httpserver.ListenerTLS
	// link is the scanner's side of its pull: the pair it presents and the CA it verifies the scan listener against; nil without TLS.
	link *httpserver.LinkTLS
	// analyser is the version check's result: an error while the binary does not answer its version.
	analyser error
}

// unit is the name of the scanner unit the area is: the name of its listener and of its component.
func (s scannerArea) unit() string {
	switch {
	case s.malware != nil:
		return unitMalwareScannerClamav
	case s.updater != nil:
		return unitMalwareUpdaterFreshclam
	}
	return unitVulnScannerTrivy
}

// built reports whether the area holds a scanner, the loader or the updater.
func (s scannerArea) built() bool {
	return s.svc != nil || s.loader != nil || s.malware != nil || s.updater != nil
}

// wireScanner builds the scanner of the role scanner: the image scanner of the kind trivy, the loader of its database of the kind
// trivy-db, the malware scanner of the kind clamav or the updater of the kind freshclam. The role all builds none (Q-ARCH-563). The
// blob area is optional (Q-ARCH-300): the role scanner has none and opens the spool alone, without the object store.
func (a *App) wireScanner(ctx context.Context, _ blobArea) (scannerArea, error) {
	switch {
	case a.id.Role == config.RoleAll:
		return scannerArea{}, nil // the role all scans no image and no blob: no scanner and no listener
	case a.isMalwareScanner():
		return a.wireMalwareScanner(ctx)
	case a.isMalwareUpdater():
		return a.wireMalwareUpdater(ctx)
	case a.isTrivyLoader():
		return a.wireTrivyLoader(ctx)
	}
	cfg := a.cfg
	images := cfg.Vulnerabilities.Images
	log := a.log.With("component", unitVulnScannerTrivy)
	// The spool exports the two gauges every spool exports, on the scanner's registry (Q-OPS-237): QuaderSpoolNearlyFull reads them, and
	// the scrape's label role names the unit, vuln-scanner-trivy.
	spool, err := blob.NewSpool(cfg.Storage.Spool, blob.WithSpoolLogger(log), blob.WithSpoolRoom(blob.NewSpoolMetrics(a.tele.metrics)))
	if err != nil {
		return scannerArea{}, err
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
	if out.listenerTLS, err = httpserver.NewLinkListenerTLS(httpserver.LinkVulnScannerTrivy, cfg.Server, nonEmpty(images.Scanner.ClientName),
		a.tlsOptions(httpserver.LinkVulnScannerTrivy)...); err != nil {
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
	svc, err := scanner.New(scanner.Options{
		Analyser: analyser, Registry: reg, Space: spoolSpace{spool}, ScanDir: images.ScanDir, DatabaseDir: images.Database.Dir, Slots: images.Scanner.Slots,
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
	a.health.AddReadiness(checkVulnScannerTrivy, a.scannerReady(out), false)
	return out, nil
}

// scannerReady is the readiness of the scanner (R-HA-065): the pair and the internal CA have loaded, the pair allows client
// authentication, the analyser answered its version, the spool directory is writable, and the process is not draining. It does not
// depend on packages or Valkey: a scan listener that cannot be reached is the call's answer, not the pod's state. Nor on the database:
// analyses need none (Q-VULN-168), and a detection before the pod's first load is answered database_loading (R-VULN-049).
func (a *App) scannerReady(s scannerArea) health.CheckFunc {
	dir := a.cfg.Vulnerabilities.Images.ScanDir
	configured := health.ConfigError
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

func (b brokenAnalyser) Detect(context.Context, string, string, string, string, imagescan.Limits) error {
	return &imagescan.ScanError{Code: imagescan.CodeAnalyserFailed, Detail: "the scanner's analyser does not run: " + b.err.Error()}
}

// scannerComponent drains the scanner at SIGTERM and ends the analyses still running at the shutdown timeout (R-HA-065): both are
// answered 503 scanner_draining, which the worker puts off without an attempt. It runs the reloader of the link's material; the
// listener's runs with its server.
type scannerComponent struct {
	area scannerArea
	link *linkComponent
}

func (c *scannerComponent) Name() string { return c.area.unit() }

func (c *scannerComponent) Start(ctx context.Context) error {
	if c.area.link != nil {
		c.link = &linkComponent{name: "link-scan", link: c.area.link}
		return c.link.Start(ctx)
	}
	return nil
}

func (c *scannerComponent) Drain(context.Context) {
	switch {
	case c.area.malware != nil:
		c.area.malware.Drain()
	case c.area.updater != nil:
		c.area.updater.Drain()
	case c.area.loader != nil:
		// The loader serves no call: a load in flight ends with its component (trivyLoaderLoop).
	default:
		c.area.svc.Drain()
	}
}

func (c *scannerComponent) Quiesce(context.Context) { c.stopScans() }

// stopScans ends the scans still running: the analyses and the detections, or the malware scanner's calls; the updater and the loader
// run none (a listing or a file in flight ends with its server, a load with its component).
func (c *scannerComponent) stopScans() {
	switch {
	case c.area.malware != nil:
		c.area.malware.Stop()
	case c.area.updater != nil, c.area.loader != nil:
	default:
		c.area.svc.Stop()
	}
}

func (c *scannerComponent) Stop(ctx context.Context) error {
	c.stopScans()
	if c.link != nil {
		return c.link.Stop(ctx)
	}
	return nil
}

// startScanner binds the scanner's listener on server.listen and serves POST /scan/v1/analyses and POST /scan/v1/detections, or a
// malware scanner's POST /scan/v1/malware, or the updater's listing, and nothing else (R-ARCH-036). The loader of the kind trivy-db opens
// no listener but the operations listener (Q-HA-379).
func (a *App) startScanner() error {
	s, ok := lookup[scannerArea](a.parts)
	switch {
	case ok && !s.built() && a.id.Role == config.RoleAll:
		return nil
	case !ok || !s.built():
		return errors.New("the scanner's listener: the scanner area is not wired")
	case s.loader != nil:
		return nil
	}
	var router http.Handler
	switch {
	case s.malware != nil:
		router = malwareScannerRouter(s.malware)
	case s.updater != nil:
		router = s.updater.Handler() // the listing and the files (R-MAL-032), its router its own
	default:
		router = scannerRouter(s.svc)
	}
	srv, err := a.newServer(s.unit(), a.cfg.Server.Listen, router, s.listenerTLS)
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

// scannerRouter is the router of the scanner's listener: the routes of the two calls, the analysis and the detection (R-VULN-041,
// R-VULN-048). Its router is its own; no route family of an endpoint, no management API and no package protocol is served there
// (R-ARCH-036).
func scannerRouter(svc *scanner.Service) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(imagescan.AnalysesPath, svc.Handler())
	mux.Handle(imagescan.DetectionsPath, svc.DetectHandler())
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

// nonEmpty is the one name a listener of a link admits as a list, or none when it is empty (the role all, Q-ARCH-553).
func nonEmpty(name string) []string {
	if name == "" {
		return nil
	}
	return []string{name}
}
