package upstream

import (
	"net/http"

	"github.com/guided-traffic/quader/internal/repository"
)

// The verdicts of the upstream test (R-REPO-024, Q-REPO-488): what the test concluded from everything it saw, stable like the codes
// of the API (R-ARCH-025). The verdict is made here and nowhere else, so that every screen and every other reader of the answer
// says the same and none derives it from a status again: the 401 of a registry's Bearer challenge is no error when its token
// service accepted the account.
const (
	// VerdictAccepted: the upstream answered, and the credential sent was accepted, by the upstream or by its token service.
	VerdictAccepted = "accepted"
	// VerdictAnswered: the upstream answered with success, and no credential took part.
	VerdictAnswered = "answered"
	// VerdictLoginAsked: a registry answered with a Bearer challenge whose token service the repository may reach, and no
	// credential was judged: the repository has none. The registry works; whether it lets one in without an account is not tested.
	VerdictLoginAsked = "login_asked"
	// VerdictCredentialWithheld: as VerdictLoginAsked, but the repository has a credential and the token service's host does not
	// receive it, so the registry is used without it.
	VerdictCredentialWithheld = "credential_withheld" //nolint:gosec // G101: the name of a verdict, not a credential
	// VerdictRefused: the credential sent was refused, by the upstream (401, 403) or by its token service.
	VerdictRefused = "refused"
	// VerdictLoginRequired: the upstream refused a request that carried no credential (401, 403 without a token handshake).
	VerdictLoginRequired = "login_required"
	// VerdictTokenServiceDenied: the registry's token service is on a host the repository may not reach, or is reached over
	// plain http from an https upstream; it was not asked.
	VerdictTokenServiceDenied = "token_service_denied"
	// VerdictTokenServiceFailed: the registry answered, and its token service failed or gave no verdict on the credential.
	VerdictTokenServiceFailed = "token_service_failed"
	// VerdictUpstreamError: the upstream answered with an error of its own (404, 500, any other status from 400 on), or with a
	// challenge the test cannot read.
	VerdictUpstreamError = "upstream_error"
	// VerdictUnreachable: no answer came (the name, the connection, the certificate, the time, the redirects).
	VerdictUnreachable = "unreachable"
	// VerdictNotSent: the probe was not sent: the repository or the installation is offline, the egress policy refuses the
	// address, the replica has no address in the source network, or the credential cannot be read on this replica.
	VerdictNotSent = "not_sent"
)

// notSent reports whether an error code is one of a probe that never left this replica.
func notSent(code string) bool {
	switch code {
	case CodeOffline, CodeEgressDenied, CodeSourceNetwork, CodeSecretUnavailable:
		return true
	}
	return false
}

// verdict concludes a result. credentialSent is whether the probe carried the repository's credential.
func verdict(r repository.UpstreamTestResult, credentialSent bool) string {
	if !r.Reachable {
		if r.Error != nil && notSent(r.Error.Code) {
			return VerdictNotSent
		}
		return VerdictUnreachable
	}
	if ts := r.TokenService; ts != nil {
		switch {
		case !ts.Allowed:
			return VerdictTokenServiceDenied
		case r.Error != nil:
			return VerdictTokenServiceFailed
		case r.Authenticated != nil && *r.Authenticated:
			return VerdictAccepted
		case r.Authenticated != nil:
			return VerdictRefused
		case credentialSent && !ts.ReceivesCredential:
			return VerdictCredentialWithheld
		}
		return VerdictLoginAsked
	}
	if r.Error != nil || r.HTTPStatus == nil {
		return VerdictUpstreamError
	}
	switch status := *r.HTTPStatus; {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		if credentialSent {
			return VerdictRefused
		}
		return VerdictLoginRequired
	case status >= http.StatusBadRequest:
		return VerdictUpstreamError
	case credentialSent:
		return VerdictAccepted
	}
	return VerdictAnswered
}

// concluded returns the result with its verdict.
func concluded(r repository.UpstreamTestResult, credentialSent bool) repository.UpstreamTestResult {
	r.Verdict = verdict(r, credentialSent)
	return r
}
