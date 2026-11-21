package auth

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_CLAMAV_010_TheCertificateSubject: on an endpoint with auth.client_certificates a request whose connection carries a verified
// chain and no token is the certificate subject with the leaf's subject, issuer and serial; a Bearer token on it is the token's subject;
// a refused token is a 401 and never falls through to the certificate; on an endpoint without the key the same connection is decided as
// one without a credential (R-AUTH-001 item 4, R-EP-010 item 5, formats/clamav.md R-CLAMAV-010, Q-CLAMAV-9, Q-CLAMAV-12).
func TestR_CLAMAV_010_TheCertificateSubject(t *testing.T) {
	sat := newToken(t, token.TypeSAT)
	ft := &fakeTokens{known: map[string]token.Info{sat: {ID: "t2", Type: token.TypeSAT, Owner: alice, Scope: token.Scope{Inherit: true, Endpoints: []string{"clamav"}}}}}
	s := newStage(ft, Options{})
	leaf := &x509.Certificate{
		Subject:      pkix.Name{CommonName: "scan-01.example.com", Organization: []string{"Example"}},
		Issuer:       pkix.Name{CommonName: "Example Device CA"},
		SerialNumber: big.NewInt(0x0a1b2c),
	}
	withChain := func() *tls.ConnectionState {
		return &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{leaf}}}
	}
	ep := EndpointRules{Name: "clamav", TokenTypes: []token.Type{token.TypeSAT}, BoundTokensOnly: true, ClientCertificates: true}

	t.Run("R-CLAMAV-010 a verified chain without a token is the certificate subject", func(t *testing.T) {
		r := req("GET", "/api/clamav/upstream/clamav/daily.cvd")
		r.TLS = withChain()
		res, err := s.Authenticate(r, ep)
		if err != nil {
			t.Fatal(err)
		}
		c := res.Credential.Certificate
		if res.Anonymous || res.Principal.Kind != principal.KindCertificate || res.Principal.ID != "" || res.Principal.Name != "CN=scan-01.example.com,O=Example" ||
			res.Credential.Method != MethodClientCertificate || c == nil || c.Subject != res.Principal.Name || c.Issuer != "CN=Example Device CA" || c.Serial != "a1b2c" {
			t.Fatalf("result = %+v, certificate %+v", res, c)
		}
	})
	t.Run("R-CLAMAV-010 a token wins over the certificate", func(t *testing.T) {
		r := req("GET", "/api/clamav/upstream/clamav/daily.cvd", "Authorization", "Bearer "+sat)
		r.TLS = withChain()
		res, err := s.Authenticate(r, ep)
		if err != nil || res.Principal.ID != alice.ID || res.Credential.Certificate != nil {
			t.Fatalf("result = %+v, %v", res, err)
		}
	})
	t.Run("R-CLAMAV-010 a refused token is a 401 and does not fall through to the certificate", func(t *testing.T) {
		r := req("GET", "/api/clamav/upstream/clamav/daily.cvd", "Authorization", "Bearer "+newToken(t, token.TypeSAT))
		r.TLS = withChain()
		_, err := s.Authenticate(r, ep)
		wantErr(t, err, 401, errs.CodeInvalidToken)
	})
	t.Run("R-EP-010 on an endpoint without the key the certificate is no credential", func(t *testing.T) {
		r := req("GET", "/api/clamav/upstream/clamav/daily.cvd")
		r.TLS = withChain()
		other := ep
		other.ClientCertificates = false
		_, err := s.Authenticate(r, other)
		wantErr(t, err, 401, errs.CodeCredentialsRequired)
		res, err := s.Authenticate(r, defaultRules())
		if err != nil || !res.Anonymous {
			t.Fatalf("default endpoint: %+v, %v", res, err)
		}
	})
	t.Run("Q-CLAMAV-12 a subject longer than 255 bytes is cut at a UTF-8 boundary", func(t *testing.T) {
		long := strings.Repeat("ä", 200) // 400 bytes
		got := cutName(long)
		if !strings.HasSuffix(got, TruncatedMarker) || len(strings.TrimSuffix(got, TruncatedMarker)) != 254 {
			t.Fatalf("cut = %d bytes", len(got))
		}
		if cutName("CN=x") != "CN=x" {
			t.Fatal("a short name is cut")
		}
	})
}
