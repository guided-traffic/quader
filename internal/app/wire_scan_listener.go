package app

import (
	"fmt"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// The scan listener of packages (R-ARCH-037, R-EP-002, Q-ARCH-528): one more listener of a packages process (and of web and all), on
// server.scan_listen, whose endpoint is the reserved scan. It is built like a client endpoint - its own router of the family packages,
// its own chain with the endpoint bound - and differs in what the endpoint says: no host check, no rate-limit class, an in-flight bound of
// 64, no cache fill, a stage that accepts a scan token and nothing else, and a listener that requires a client certificate of the
// internal CA (R-ARCH-038). The kinds of the role scanner are its clients: the image scanner's pull and its loader's read of the
// database, the malware scanner's reads, the updater's seed, and every scanner's report of itself.

// imageScanningOn reports whether the configuration scans images: the scanning and its image half are on.
func (a *App) imageScanningOn() bool {
	v := a.cfg.Vulnerabilities
	return v.Enabled && v.Images.Enabled
}

// scanListenAddr is the address of the scan listener this process opens, "" for none: server.scan_listen on the main Deployment of
// packages (never a pool), on web and on all. The role all builds no scanner of its own (Q-ARCH-563), so it opens the listener only
// when the setting names an address.
func (a *App) scanListenAddr() string {
	addr := a.cfg.Server.ScanListen
	switch a.id.Role {
	case config.RolePackages, config.RoleWeb:
		if a.id.Pool != "" {
			return ""
		}
		return addr
	case config.RoleAll:
		return addr
	}
	return ""
}

// planScanListener adds the scan listener's endpoint to the endpoints this process opens.
func (a *App) planScanListener() error {
	addr := a.scanListenAddr()
	if addr == "" {
		return nil
	}
	r := config.ResolvedEndpoint{
		Name: config.ScanEndpoint, Listen: addr, Surface: []string{config.SurfacePackages}, MaxInFlight: config.ScanMaxInFlight,
		Exposure: config.ExposureAll, ReplicaHeader: "never",
	}
	ep, err := httpserver.NewEndpoint(r, nil, httpserver.ForRoleFamilies(a.sub.Families))
	if err != nil {
		return fmt.Errorf("the scan listener: %w", err)
	}
	a.eps = append(a.eps, &endpointRuntime{cfg: r, ep: ep})
	return nil
}

// isScanListener reports whether an endpoint is the scan listener's.
func isScanListener(rt *endpointRuntime) bool { return rt.cfg.Name == config.ScanEndpoint }

// scanListenerTLS is the TLS of the scan listener: the pair of server.tls and a required client certificate of the internal CA
// that lists one of the names of server.scan_clients, the kinds of scanner, whatever server.tls.client_auth says about the client
// listener (R-ARCH-037, R-ARCH-038). Nil without TLS.
func (a *App) scanListenerTLS() (*httpserver.ListenerTLS, error) {
	return httpserver.NewLinkListenerTLS(httpserver.LinkScan, a.cfg.Server, a.cfg.Server.ScanClients, a.tlsOptions(httpserver.LinkScan)...)
}
