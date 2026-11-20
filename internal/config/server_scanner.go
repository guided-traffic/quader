package config

import (
	"fmt"
	"net"
)

// ScanEndpoint is the reserved name of the endpoint of the scan listener (R-EP-001, R-EP-002, R-ARCH-037): built in, never an
// entry of server.endpoints.
const ScanEndpoint = "scan"

// ScanMaxInFlight is the in-flight bound of the scan listener on one replica (R-ARCH-037).
const ScanMaxInFlight = 64

// ServerInternalTLS is the trust anchor of the links between quader's pods (R-ARCH-038): the worker's call of the scanner and the
// scanner's pull from the scan listener of packages. Each end presents the first pair of server.tls.certificates and verifies the
// other against this CA and by the names of vulnerabilities.images (Q-ARCH-552); the scanner's listener and the scan listener refuse a
// peer without such a certificate in the handshake.
type ServerInternalTLS struct {
	CAFile string `yaml:"ca_file" default:"" doc:"PEM bundle of the CA the links between quader's pods verify against, and that a client certificate of the scanner's listener and of the scan listener must chain to (R-ARCH-038). Read again every server.tls.reload_interval. The chart sets /var/run/secrets/quader/tls/ca.crt, the ca.crt of the pod's own Secret (R-HA-063). Needs server.tls.certificates: without TLS the links are plain HTTP."`
}

// validateLinks checks the scan listener and the internal trust anchor (R-ARCH-037, R-ARCH-038).
func (s *Server) validateLinks(r *Report) {
	if _, reserved := s.Endpoints[ScanEndpoint]; reserved {
		r.Add("endpoints."+ScanEndpoint, "names the reserved endpoint of the scan listener (R-EP-001); its listener is server.scan_listen and it has no settings")
	}
	if s.ScanListen != "" {
		if _, port, err := net.SplitHostPort(s.ScanListen); err != nil {
			r.Add("scan_listen", "%q is not a host:port address", s.ScanListen)
		} else if port != "0" && port != "" {
			others := []struct{ key, addr string }{{"listen", s.Listen}, {"ops_listen", s.OpsListen}}
			for _, name := range s.furtherNames() {
				others = append(others, struct{ key, addr string }{"endpoints." + name + ".listen", s.Endpoints[name].Listen})
			}
			for _, o := range others {
				if _, p, err := net.SplitHostPort(o.addr); err == nil && p == port {
					r.Add("scan_listen", "uses port %s, which server.%s already uses", port, o.key)
				}
			}
		}
	}
	for i, name := range s.ScanClients {
		if name == "" || !exactName(name) {
			r.Add(fmt.Sprintf("scan_clients[%d]", i), "%q must be a DNS name without a wildcard, a port or a path: it is compared exactly with a name of the certificate (Q-ARCH-552)", name)
		}
	}
	if role := r.Role(); s.ScanListen != "" && s.TLS.Enabled() && (role == RolePackages || role == RoleWeb) && len(s.ScanClients) == 0 {
		r.Add("scan_clients", "is required while scan_listen is open with TLS: it names the kinds of pod the scan listener admits, the scanners (R-ARCH-038); the chart sets it")
	}
	if s.InternalTLS.CAFile != "" && !s.TLS.Enabled() {
		r.Add("internal_tls.ca_file", "is set while tls.certificates is empty: the links between quader's pods present and serve the pair of server.tls, so without one they are plain HTTP and no client certificate can be asked (R-ARCH-038)")
	}
}
