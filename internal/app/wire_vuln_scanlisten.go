package app

import (
	"net/http"

	"github.com/guided-traffic/quader/internal/api/scanlisten"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/vuln/store"
)

// The image scanning's database routes of the scan listener of packages (vulnerability-scanning.md R-VULN-049, Q-VULN-206, R-ARCH-037):
// the loader of every pod of vuln-scanner-trivy reads the database in use here with the pod's certificate. The routes are mounted on the
// scan listener's router only; the stage of the listener (internal/auth, scan.go) admits their requests by the certificate.

// trivyDatabasePeer is the admission of the database routes by the client certificate (Q-VULN-206): a request over TLS must present a
// certificate that lists vulnerabilities.images.registry.client_name, the image scanner's name, which the handshake verified against the
// internal CA. Nil while the name is empty: the routes then admit every client the listener admits. A request without TLS has no
// certificate to ask and passes, as on the malware scanner's routes (Q-VULN-210, H-1043).
func (a *App) trivyDatabasePeer() func(*http.Request) bool {
	name := a.cfg.Vulnerabilities.Images.Registry.ClientName
	if name == "" {
		return nil
	}
	names := []string{name}
	return func(r *http.Request) bool {
		if r.TLS == nil {
			return true
		}
		return len(r.TLS.PeerCertificates) > 0 && httpserver.PeerName(r.TLS.PeerCertificates[0], names) != ""
	}
}

// registerTrivyDatabaseRoutes mounts the two routes on the scan listener's router while the image scanning is on, on a replica with the
// database and the repository layer.
func (a *App) registerTrivyDatabaseRoutes(router *httpserver.Router) {
	if !a.imageScanningOn() {
		return
	}
	in := part[infra](a.parts)
	if in.db == nil {
		return
	}
	images := a.cfg.Vulnerabilities.Images
	scanlisten.RegisterTrivyDatabase(router, scanlisten.TrivyOptions{
		Source: trivyDatabaseSource{a: a, rows: store.NewImages(in.db), repo: images.Database.Repository, image: images.Database.Image},
		Log:    a.log.With("component", "scan-listener"),
	})
}
