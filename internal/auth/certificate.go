package auth

import (
	"net/http"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/auth/principal"
)

// MaxCertificateName bounds the subject and the issuer of a client certificate as the records name them (formats/clamav.md
// R-CLAMAV-010, Q-CLAMAV-12): a longer one is cut at a UTF-8 boundary and ends in [TruncatedMarker].
const MaxCertificateName = 255

// TruncatedMarker ends a certificate's name that was cut, as it ends every cut field of the request log (R-OPS-010).
const TruncatedMarker = "…[truncated]"

// byClientCertificate is the certificate subject of a request whose connection carries a chain the listener verified against the
// endpoint's tls.client_ca_file (R-AUTH-001 item 4, R-EP-010 item 5): no principal row, the kind certificate, the leaf's subject as
// its name, and the leaf's subject, issuer and serial as its credential. ok is false for a request without a verified chain, which
// the stage then decides as one without a credential. The chain is the handshake's own (tls.ConnectionState.VerifiedChains): nothing
// of a header is read (Q-CLAMAV-8).
func byClientCertificate(r *http.Request) (Result, bool) {
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.VerifiedChains[0]) == 0 {
		return Result{}, false
	}
	leaf := r.TLS.VerifiedChains[0][0]
	c := &ClientCertificate{
		Subject: cutName(leaf.Subject.String()), Issuer: cutName(leaf.Issuer.String()), Serial: leaf.SerialNumber.Text(16),
	}
	return Result{
		Principal:  principal.Principal{Kind: principal.KindCertificate, Name: c.Subject, Status: principal.StatusActive},
		Credential: Credential{Method: MethodClientCertificate, Certificate: c},
	}, true
}

// cutName bounds a certificate's name to MaxCertificateName bytes, cut at a UTF-8 boundary, with the marker after the cut.
func cutName(s string) string {
	if len(s) <= MaxCertificateName {
		return s
	}
	end := MaxCertificateName
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + TruncatedMarker
}
