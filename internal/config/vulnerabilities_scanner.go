package config

import (
	"net/url"
	"path/filepath"
)

// finalizeScanDir derives vulnerabilities.images.scan_dir from the spool when it is unset (Q-VULN-184): the working files of the
// image scanning lie below the spool, whose budget admits them (R-VULN-036), in every role and outside the chart too.
func (i *VulnerabilityImages) finalizeScanDir(spoolDir string, sources map[string]Source) {
	if i.ScanDir != "" || spoolDir == "" {
		return
	}
	i.ScanDir = filepath.Join(spoolDir, "imagescan")
	sources["vulnerabilities.images.scan_dir"] = Source{Kind: "derived", Detail: "storage.spool.dir"}
}

// VulnerabilityScannerLink is the worker's side of the call to the scanner (R-VULN-041). The chart sets both addresses; outside
// it the role all leaves them empty and calls its own scanner listener over loopback (Q-ARCH-531).
type VulnerabilityScannerLink struct {
	URL        string `yaml:"url" default:"" doc:"The scanner's listener, an origin: https://<fullname>-scanner.<namespace>.svc:8080 in the chart, which sets it. Required in a worker while the image scanning is on; empty in the role all, where it means the process's own scanner listener over loopback (Q-ARCH-531)."`
	ServerName string `yaml:"server_name" default:"" doc:"The name the worker verifies in the scanner's certificate against server.internal_tls.ca_file; empty is the host of url. The chart sets <fullname>.<namespace>.svc, the name its certificate carries."`
	Slots      int    `yaml:"slots" default:"1" validate:"min=1,max=16" doc:"Analyses one scanner process runs at a time; a call while every slot holds one is answered 503 scanner_busy. The chart checks the scanner's spool against it (Q-HA-318); more than one slot lets the analyses of one pod reach each other (H-1037), so scale by replicas."`
}

// VulnerabilityPullLink is the scanner's side of the pull from the scan listener of packages (R-VULN-042).
type VulnerabilityPullLink struct {
	URL        string `yaml:"url" default:"" doc:"The scan listener of packages, an origin: https://<fullname>-packages-scan.<namespace>.svc:8082 in the chart, which sets it. Required in a scanner; empty in the role all, where it means the process's own scan listener. The scanner reads no other address: a call cannot point it elsewhere."`
	ServerName string `yaml:"server_name" default:"" doc:"The name the scanner verifies in the scan listener's certificate against server.internal_tls.ca_file; empty is the host of url. The chart sets <fullname>.<namespace>.svc."`
}

// Validate checks the addresses of the links and, for the role scanner, that it names the scan listener it pulls from
// (R-VULN-025, R-VULN-042). An address is an http or https origin; http is for an installation without TLS (Q-HA-322). The role all
// may leave both empty: its own listeners (Q-ARCH-531).
func (i *VulnerabilityImages) Validate(r *Report) {
	checkLinkURL(r, "scanner.url", i.Scanner.URL)
	checkLinkURL(r, "registry.url", i.Registry.URL)
	if r.Role() == RoleScanner && i.Registry.URL == "" {
		r.Add("registry.url", "is required in a scanner: it names the scan listener of packages the scanner pulls from (R-VULN-042); the chart sets it")
	}
}

// validateScannerLink requires the scanner's address in a worker while the image scanning is on (R-VULN-025, R-VULN-041): the
// worker calls it for every analysis. Vulnerabilities.Validate calls it, because it needs both switches.
func (v *Vulnerabilities) validateScannerLink(r *Report) {
	if r.Role() == RoleWorker && v.Enabled && v.Images.Enabled && v.Images.Scanner.URL == "" {
		r.Add("images.scanner.url", "is required in a worker while the image scanning is on: it names the scanner's listener the worker calls (R-VULN-041); the chart sets it")
	}
}

// checkLinkURL refuses a link address that is not an http or https origin, naming the key and never printing a password.
func checkLinkURL(r *Report, key, raw string) {
	if raw == "" {
		return
	}
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Host == "":
		r.Add(key, "must be an http or https origin (is %q)", redactUserinfo(raw))
	case u.Scheme != "https" && u.Scheme != "http":
		r.Add(key, "%q must use the scheme https, or http for an installation without TLS", redactUserinfo(raw))
	case u.User != nil, (u.Path != "" && u.Path != "/"), u.RawQuery != "", u.Fragment != "":
		r.Add(key, "%q must be an origin: a scheme, a host and a port, nothing else", redactUserinfo(raw))
	}
}
