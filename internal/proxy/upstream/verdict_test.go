package upstream

import (
	"testing"

	"github.com/guided-traffic/quader/internal/repository"
)

// TestR_REPO_024_TheVerdictOfTheUpstreamTest: the one place that concludes a test, over every case a person meets. A registry's
// 401 with a Bearer challenge is no error: with an account its token service accepted it is accepted, without one the registry
// answered and asks for a login.
func TestR_REPO_024_TheVerdictOfTheUpstreamTest(t *testing.T) {
	yes, no := true, false
	status := func(s int) *int { return &s }
	fail := func(code string) *repository.UpstreamTestError {
		return &repository.UpstreamTestError{Code: code, Message: "m"}
	}
	allowed := &repository.UpstreamTokenService{Host: "auth.docker.io", Allowed: true, ReceivesCredential: true}
	withheld := &repository.UpstreamTokenService{Host: "auth.example.com", Allowed: true}
	foreign := &repository.UpstreamTokenService{Host: "auth.example.com"}
	for _, c := range []struct {
		name string
		res  repository.UpstreamTestResult
		sent bool
		want string
	}{
		{"an anonymous registry answers 200", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(200), Authenticated: &yes}, false, VerdictAnswered},
		{"an upstream answers 200 to a credential", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(200), Authenticated: &yes}, true, VerdictAccepted},
		{"a token registry without an account", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(401), TokenService: allowed}, false, VerdictLoginAsked},
		{"a token registry accepts the account", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(401), Authenticated: &yes, TokenService: allowed}, true, VerdictAccepted},
		{"a token registry refuses the account", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(401), Authenticated: &no, TokenService: allowed}, true, VerdictRefused},
		{"a token service whose host receives no credential", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(401), TokenService: withheld}, true, VerdictCredentialWithheld},
		{"a token service on a foreign host", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(401), TokenService: foreign, Error: fail(CodeForeignHostDenied)}, true, VerdictTokenServiceDenied},
		{"a token service over plain http", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(401), TokenService: foreign, Error: fail(CodeInsecureRedirect)}, false, VerdictTokenServiceDenied},
		{"a token service that answers 500", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(401), TokenService: allowed, Error: fail(CodeRequestFailed)}, true, VerdictTokenServiceFailed},
		{"a token service whose name does not resolve", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(401), TokenService: allowed, Error: fail(CodeDNSError)}, true, VerdictTokenServiceFailed},
		{"a challenge whose realm is no URL", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(401), Error: fail(CodeRequestFailed)}, false, VerdictUpstreamError},
		{"bearer mode: the registry refuses the stored token", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(401), Authenticated: &no}, true, VerdictRefused},
		{"bearer mode: the registry takes the stored token", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(200), Authenticated: &yes}, true, VerdictAccepted},
		{"a 401 without a credential and without a handshake", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(401), Authenticated: &no}, false, VerdictLoginRequired},
		{"a 403 without a credential", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(403), Authenticated: &no}, false, VerdictLoginRequired},
		{"a 404 of the upstream", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(404), Authenticated: &yes}, false, VerdictUpstreamError},
		{"a 500 of the upstream", repository.UpstreamTestResult{Reachable: true, HTTPStatus: status(500), Authenticated: &yes}, true, VerdictUpstreamError},
		{"a name that does not resolve", repository.UpstreamTestResult{Error: fail(CodeDNSError)}, false, VerdictUnreachable},
		{"an untrusted certificate", repository.UpstreamTestResult{Error: fail(CodeTLSUntrusted)}, false, VerdictUnreachable},
		{"a timeout", repository.UpstreamTestResult{Error: fail(CodeTimeout)}, true, VerdictUnreachable},
		{"a refused connection", repository.UpstreamTestResult{Error: fail(CodeConnectionRefused)}, false, VerdictUnreachable},
		{"too many redirects", repository.UpstreamTestResult{Error: fail(CodeTooManyRedirects)}, false, VerdictUnreachable},
		{"the egress policy refuses the address", repository.UpstreamTestResult{Error: fail(CodeEgressDenied)}, false, VerdictNotSent},
		{"an offline repository", repository.UpstreamTestResult{Error: fail(CodeOffline)}, false, VerdictNotSent},
		{"a credential this replica cannot read", repository.UpstreamTestResult{Error: fail(CodeSecretUnavailable)}, false, VerdictNotSent},
		{"no address in the source network", repository.UpstreamTestResult{Error: fail(CodeSourceNetwork)}, false, VerdictNotSent},
	} {
		if got := verdict(c.res, c.sent); got != c.want {
			t.Errorf("%s: verdict %s, want %s", c.name, got, c.want)
		}
	}
}
