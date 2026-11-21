package auth

import (
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/errs"
)

// R-VULN-049, Q-VULN-206, R-ARCH-037: the image scanning's database routes of the scan listener take no token and admit the image
// scanner's certificate, which the loader of a scanner pod presents, as the principal system:vuln-scanner-trivy without a registry
// grant; a token, a malware scanner's certificate and no certificate are refused with 401 and no challenge.
func TestR_VULN_049_TheScanListenerAdmitsTheImageScannerOnTheDatabaseRoutes(t *testing.T) {
	f := newRegistryFixture(t)
	signer, err := f.signer.ForScans(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s := New(Options{Tokens: hashTokens{f.ft}, Principals: fakeStore{}, Registry: signer, Now: func() time.Time { return regNow }})
	imageTok, _, err := signer.IssueScan(regRepo, "app", scanManifestDigest, regNow)
	if err != nil {
		t.Fatal(err)
	}
	const imageName, malwareName = "quader-vuln-scanner-trivy.ns.svc", "quader-malware-scanner-clamav.ns.svc"
	rules := scanRules()
	// The app's admission (trivyDatabasePeer): the image scanner's name; a plain request has no certificate to ask.
	rules.TrivyDatabase = func(r *http.Request) bool {
		return r.TLS == nil || (len(r.TLS.PeerCertificates) > 0 && len(r.TLS.PeerCertificates[0].DNSNames) > 0 && r.TLS.PeerCertificates[0].DNSNames[0] == imageName)
	}
	layer := registrytoken.TrivyDatabasePath + "/sha256:" + scanManifestDigest[7:]
	for _, p := range []string{registrytoken.TrivyDatabasePath, layer} {
		res, err := s.Authenticate(withPeer(req("GET", p), imageName), rules)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if res.Principal.Kind != principal.KindSystem || res.Principal.Name != registrytoken.ScanSubject || res.Credential.Method != MethodClientCertificate ||
			res.Credential.Registry != nil {
			t.Fatalf("%s: %+v", p, res)
		}
		for name, r := range map[string]*http.Request{
			"an image's scan token":        withPeer(req("GET", p, "Authorization", "Bearer "+imageTok.Reveal()), imageName),
			"a personal access token":      withPeer(req("GET", p, "Authorization", "Bearer "+f.pat), imageName),
			"the malware scanner's":        withPeer(req("GET", p), malwareName),
			"no certificate":               withPeer(req("GET", p)),
			"an escaped path of the route": withPeer(req("GET", "/scan/v1/trivy%2Ddatabase"), imageName),
		} {
			_, err := s.Authenticate(r, rules)
			if err == nil {
				t.Errorf("%s %s: admitted", p, name)
				continue
			}
			if e, ok := errs.As(err); !ok || e.Kind != errs.Unauthenticated || e.Challenge != "" {
				t.Errorf("%s %s: %v", p, name, err)
			}
		}
	}
	// Q-VULN-210: on a listener without TLS there is no certificate to ask, as on the malware scanner's routes (H-1043).
	if _, err := s.Authenticate(req("GET", registrytoken.TrivyDatabasePath), rules); err != nil {
		t.Fatalf("a plain request on a listener without TLS: %v", err)
	}
}
