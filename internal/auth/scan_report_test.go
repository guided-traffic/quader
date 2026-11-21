package auth

import (
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/errs"
)

// R-OPS-048, Q-OPS-223: the route of the scanner replicas takes no token and admits a request whose certificate names a kind of
// scanner, as the principal system:scanner-report; a token, a certificate of no kind, an escaped path and a listener without the kind
// check are refused with 401 and no challenge.
func TestR_OPS_048_TheScanListenerAdmitsAScannerReportByTheCertificateOfItsKind(t *testing.T) {
	f := newRegistryFixture(t)
	s := New(Options{Tokens: hashTokens{f.ft}, Principals: fakeStore{}, Now: func() time.Time { return regNow }})
	const imageName = "quader-vuln-scanner-trivy.ns.svc"
	rules := scanRules()
	rules.ScannerKind = func(r *http.Request) string {
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 && len(r.TLS.PeerCertificates[0].DNSNames) > 0 && r.TLS.PeerCertificates[0].DNSNames[0] == imageName {
			return "trivy"
		}
		return ""
	}
	path := "/scan/v1/replicas/8a0b5f8e-3c1d-4f2a-9b7e-1c2d3e4f5a6b"
	res, err := s.Authenticate(withPeer(req("PUT", path), imageName), rules)
	if err != nil {
		t.Fatal(err)
	}
	if res.Principal.Kind != principal.KindSystem || res.Principal.Name != ScannerReportSubject || res.Credential.Method != MethodClientCertificate {
		t.Fatalf("result %+v", res)
	}
	noCheck := rules
	noCheck.ScannerKind = nil
	for name, c := range map[string]struct {
		r     *http.Request
		rules EndpointRules
	}{
		"a personal access token":        {withPeer(req("PUT", path, "Authorization", "Bearer "+f.pat), imageName), rules},
		"the worker's certificate":       {withPeer(req("PUT", path), "quader-worker.ns.svc"), rules},
		"no certificate":                 {req("PUT", path), rules},
		"an escaped path of the route":   {withPeer(req("PUT", "/scan/v1/replicas%2F8a0b5f8e-3c1d-4f2a-9b7e-1c2d3e4f5a6b"), imageName), rules},
		"the route without a kind check": {withPeer(req("PUT", path), imageName), noCheck},
	} {
		_, err := s.Authenticate(c.r, c.rules)
		if e, ok := errs.As(err); !ok || e.Kind != errs.Unauthenticated || e.Challenge != "" {
			t.Errorf("%s: %v", name, err)
		}
	}
}
