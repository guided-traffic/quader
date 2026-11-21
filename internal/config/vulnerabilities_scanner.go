package config

import (
	"net"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
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
	URL        string `yaml:"url" default:"" doc:"The scanner's listener, an origin: https://<fullname>-vuln-scanner-trivy.<namespace>.svc:8080 in the chart, which sets it. Required in a worker while the image scanning is on; empty in the role all, where it means the process's own scanner listener over loopback (Q-ARCH-531)."`
	ServerName string `yaml:"server_name" default:"" doc:"The name the worker requires in the scanner's certificate, which must chain to server.internal_tls.ca_file and list it exactly among its DNS names, no wildcard standing for it (Q-ARCH-552); empty is the host of url. The chart sets <fullname>-vuln-scanner-trivy.<namespace>.svc, the scanner's name (R-HA-063)."`
	ClientName string `yaml:"client_name" default:"" doc:"The name the scanner's listener requires in a client certificate, listed exactly among its DNS names (Q-ARCH-552): the worker's. Required in a scanner while server.tls serves TLS; empty in the role all, where the CA alone admits (Q-ARCH-553). The chart sets <fullname>-worker.<namespace>.svc."`
	Slots      int    `yaml:"slots" default:"1" validate:"min=1,max=16" doc:"Analyses one scanner process runs at a time; a call while every slot holds one is answered 503 scanner_busy. The chart checks the scanner's spool against it (Q-HA-318); more than one slot lets the analyses of one pod reach each other (H-1037), so scale by replicas."`
}

// VulnerabilityPullLink is the scanner's side of the pull from the scan listener of packages (R-VULN-042).
type VulnerabilityPullLink struct {
	URL        string `yaml:"url" default:"" doc:"The scan listener of packages, an origin: https://<fullname>-packages-scan.<namespace>.svc:8082 in the chart, which sets it. Required in a scanner; empty in the role all, where it means the process's own scan listener. The scanner reads no other address: a call cannot point it elsewhere."`
	ServerName string `yaml:"server_name" default:"" doc:"The name the scanner requires in the scan listener's certificate, which must chain to server.internal_tls.ca_file and list it exactly among its DNS names (Q-ARCH-552); empty is the host of url. The chart sets <fullname>-packages.<namespace>.svc, the name of packages (R-HA-063). The names the scan listener admits are server.scan_clients."`
	ClientName string `yaml:"client_name" default:"" doc:"The name by which the scan listener knows the image scanner (the kind trivy): a scanner's report of itself is recorded as a replica of vuln-scanner-trivy only from a client certificate that lists it exactly among its DNS names (R-OPS-048, Q-OPS-223). It must be one of server.scan_clients when that list is set, and differ from malware.scan_listener.client_name. Empty: the scan listener records no image scanner (the role all, where no scanner pod runs). The chart sets <fullname>-vuln-scanner-trivy.<namespace>.svc."`
}

// validateLinkNames requires the name the scanner's listener checks while the links speak TLS (R-ARCH-038, Q-ARCH-552): the worker's.
// The role all may leave it empty (Q-ARCH-553); the scan listener's names are server.scan_clients (Server.validateLinks). A name with a
// wildcard or a port is refused: the check compares it exactly with a DNS name of the certificate.
func (c *Config) validateLinkNames(r *Report) {
	images := c.Vulnerabilities.Images
	for _, n := range []struct{ key, name string }{
		{"scanner.server_name", images.Scanner.ServerName},
		{"scanner.client_name", images.Scanner.ClientName},
		{"registry.server_name", images.Registry.ServerName},
		{"registry.client_name", images.Registry.ClientName},
	} {
		if !exactName(n.name) {
			r.Add("vulnerabilities.images."+n.key, "%q must be a DNS name or an IP address without a wildcard, a port or a path: it is compared exactly with a name of the certificate (Q-ARCH-552)", n.name)
		}
	}
	if !c.Server.TLS.Enabled() {
		return
	}
	if r.Role() == RoleScanner && c.Process.ScannerKind != ScannerKindClamav && images.Scanner.ClientName == "" {
		r.Add("vulnerabilities.images.scanner.client_name", "is required in a scanner while server.tls serves TLS: it names the worker, the one kind of pod its listener admits (R-ARCH-038); the chart sets it")
	}
}

// validateScannerReportNames checks the names by which the scan listener tells the kinds of scanner apart when they report themselves
// (R-OPS-048, Q-OPS-223): the image scanner's name must be one the listener's handshake admits, as the malware scanner's must
// (validateMalwareRouteName), and the two must differ, or a report could not be told by its certificate.
func (c *Config) validateScannerReportNames(r *Report) {
	trivy, clamav := c.Vulnerabilities.Images.Registry.ClientName, c.Malware.ScanListener.ClientName
	if trivy != "" && strings.EqualFold(trivy, clamav) {
		r.Add("vulnerabilities.images.registry.client_name", "%q is also malware.scan_listener.client_name: the scan listener tells the kinds of scanner apart by these names, so they must differ (Q-OPS-223)", trivy)
	}
	if trivy == "" || c.Server.ScanListen == "" || len(c.Server.ScanClients) == 0 {
		return
	}
	if !slices.ContainsFunc(c.Server.ScanClients, func(n string) bool { return strings.EqualFold(n, trivy) }) {
		r.Add("vulnerabilities.images.registry.client_name", "%q is not one of server.scan_clients %v: the scan listener would refuse the image scanner in the handshake before it could report itself (Q-OPS-223)", trivy, c.Server.ScanClients)
	}
}

// exactName reports whether a name of a link can be compared exactly with a name of a certificate: a DNS name or an IP address,
// without a wildcard, a port or a path (Q-ARCH-552).
func exactName(name string) bool {
	return !strings.ContainsAny(name, "*/ ") && (!strings.Contains(name, ":") || net.ParseIP(name) != nil)
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
