package app

import (
	"context"
	"fmt"
	"sync"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/observability/health"
	"github.com/guided-traffic/quader/internal/vuln/dbloader"
)

// The loader of the image scanner's database: the role scanner of the kind trivy-db (process.scanner_kind, architecture.md R-ARCH-036,
// Q-ARCH-564; vulnerability-scanning.md R-VULN-049), the container database of a pod of vuln-scanner-trivy. It builds the area scanner
// with the loader alone: it reads the database in use from the scan listener's database routes with the pod's pair and writes it into
// the directory the scanner's container mounts read-only. It opens no listener but the operations listener (the chart's :8083), reports
// nothing of itself (the scanner of the pod reports the database), and logs as the unit, vuln-scanner-trivy (Q-HA-373). The role all
// builds none of it.

// isTrivyLoader reports whether the process is the loader of the image scanner's database.
func (a *App) isTrivyLoader() bool {
	return a.id.Role == config.RoleScanner && a.cfg.Process.ScannerKind == config.ScannerKindTrivyDB
}

// wireTrivyLoader builds the area scanner of the loader.
func (a *App) wireTrivyLoader(ctx context.Context) (scannerArea, error) {
	images := a.cfg.Vulnerabilities.Images
	log := a.log.With("component", unitVulnScannerTrivy)
	out := scannerArea{}
	serverName := images.Registry.ServerName
	if serverName == "" {
		serverName = hostOf(images.Registry.URL)
	}
	var err error
	if out.link, err = httpserver.NewLinkTLS(httpserver.LinkScan, a.cfg.Server, serverName, log, a.tlsOptions(httpserver.LinkScan)...); err != nil {
		return scannerArea{}, err
	}
	listener := dbloader.Listener{URL: images.Registry.URL}
	if out.link != nil {
		listener.TLS = out.link.Config
	}
	o := dbloader.Options{
		Listener: listener, Dir: images.Database.Dir, Budget: int64(images.Database.DirBudget), Timeout: images.Database.Timeout, Log: log,
	}
	if ov := a.o.Vuln; ov != nil && ov.LoaderPoll > 0 {
		o.PollBefore, o.PollAfter = ov.LoaderPoll, ov.LoaderPoll
	}
	if out.loader, err = dbloader.New(o); err != nil {
		return scannerArea{}, fmt.Errorf("the loader of the image scanner's database: %w", err)
	}
	if err := a.startComponent(ctx, &scannerComponent{area: out}); err != nil {
		return scannerArea{}, err
	}
	if err := a.startComponent(ctx, &trivyLoaderLoop{l: out.loader}); err != nil {
		return scannerArea{}, err
	}
	a.health.AddReadiness(checkVulnScannerTrivy, a.trivyLoaderReady(out), false)
	return out, nil
}

// trivyLoaderReady is the loader's readiness: its pair and the internal CA have loaded and the pair allows client authentication. The
// chart probes its liveness only (Q-HA-379): the scanner's readiness and report carry what matters of it, and a load in progress is no
// reason to restart it.
func (a *App) trivyLoaderReady(s scannerArea) health.CheckFunc {
	return func(context.Context) error {
		if s.link != nil {
			if err := s.link.Loaded(); err != nil {
				return health.ConfigError(err, "certificate")
			}
			if err := s.link.Fit(); err != nil {
				return health.ConfigError(err, "certificate")
			}
		}
		return nil
	}
}

// trivyLoaderLoop runs the loader's loop in the background from the start of the process to its stop, so that the start and the
// probes wait for neither the scan listener nor the object store behind it.
type trivyLoaderLoop struct {
	l      *dbloader.Loader
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (c *trivyLoaderLoop) Name() string { return unitVulnScannerTrivy + "-database" }

func (c *trivyLoaderLoop) Start(ctx context.Context) error {
	lctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	c.cancel = cancel
	c.wg.Go(func() { c.l.Run(lctx) })
	return nil
}

func (c *trivyLoaderLoop) Stop(context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	return nil
}
