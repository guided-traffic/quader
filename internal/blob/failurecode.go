package blob

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"syscall"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// The codes of a failure as the history of the stored content records them (storage.md R-STOR-109, R-STOR-110, Q-STOR-517): a short
// word for what went wrong, never the object store's or the SDK's text, which can carry an address, a key or a request ID and stays in
// the log.
const (
	CodeTimeout            = "timeout"             // a timeout on the way to the store or of its answer
	CodeHTTP5xx            = "http_5xx"            // the store answered with a server error
	CodeHTTP4xx            = "http_4xx"            // the store refused the request with another client error
	CodeThrottled          = "throttled"           // the store asked to slow down
	CodeCredentialRejected = "credential_rejected" //nolint:gosec // a code word of the history, no credential: the store refused the credential (R-HA-007)
	CodeAccessDenied       = "access_denied"       // the credential may not do what was asked
	CodeNoSuchBucket       = "no_such_bucket"      // the bucket does not exist
	CodeTLS                = "tls"                 // the TLS handshake failed or the certificate was not trusted
	CodeDNS                = "dns"                 // the endpoint's name did not resolve
	CodeRefused            = "refused"             // the endpoint refused the connection
	CodeNetwork            = "network"             // another failure of the connection
	CodeCircuitOpen        = "circuit_open"        // the connection's circuit is open on this replica (R-STOR-033)
	CodeNotFound           = "not_found"           // the store answered "no such object"
	CodeOther              = "other"               // anything else
)

// FailureCode is the code of a failure of the object store for the history (Q-STOR-517). A refusal of the stored format is its
// reason (blobcrypt.Reason).
func FailureCode(err error) string {
	var se *StoreError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrCircuitOpen):
		return CodeCircuitOpen
	case errors.Is(err, ErrNotFound):
		return CodeNotFound
	case errors.As(err, &se):
		return storeErrorCode(se)
	}
	if reason, ok := blobcrypt.ReasonOf(err); ok {
		return string(reason)
	}
	if code := transportCode(err); code != "" {
		return code
	}
	return CodeOther
}

// storeErrorCode is the code of a classified store error: by the store's code or status, and by the transport's failure when the
// request got no answer.
func storeErrorCode(se *StoreError) string {
	switch {
	case credentialCode(se.Code):
		return CodeCredentialRejected
	case se.Code == "AccessDenied" || se.Code == "AllAccessDisabled" || se.Status == 403:
		return CodeAccessDenied
	case se.Code == "NoSuchBucket":
		return CodeNoSuchBucket
	case se.Status == 429 || se.Code == "SlowDown" || strings.HasPrefix(se.Code, "Throttl") || se.Code == "TooManyRequests":
		return CodeThrottled
	case se.Code == "RequestTimeout":
		return CodeTimeout
	case se.Status >= 500:
		return CodeHTTP5xx
	case se.Status >= 400:
		return CodeHTTP4xx
	}
	if code := transportCode(se.Err); code != "" {
		return code
	}
	if se.Status == 0 {
		return CodeNetwork
	}
	return CodeOther
}

// transportCode is the code of a failure of the connection to the store, empty when err is none.
func transportCode(err error) string {
	if err == nil {
		return ""
	}
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var recordErr tls.RecordHeaderError
	var netErr net.Error
	switch {
	case errors.As(err, &dnsErr):
		return CodeDNS
	case errors.As(err, &certErr), errors.As(err, &unknownAuthority), errors.As(err, &hostnameErr), errors.As(err, &recordErr):
		return CodeTLS
	case errors.Is(err, syscall.ECONNREFUSED):
		return CodeRefused
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return CodeTimeout
	case errors.As(err, &netErr):
		return CodeNetwork
	}
	return ""
}
