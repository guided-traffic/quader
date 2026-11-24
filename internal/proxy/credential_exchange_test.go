package proxy

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
)

// The realm of the Red Hat Automation Hub's sign-on, as it answered on 2026-11-24 (02:29 UTC) a POST of the refresh-token grant with
// a dummy refresh token, recorded with curl: the service's own answers, which the stub's realm of test/ansiblestub copies. No account
// was used; an answer to a valid offline token is not recorded (H-842).
const (
	realmTokenURL = "https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/token"
	// client_id=cloud-services, the default of ansible.clientId: the client exists, the token does not.
	realmInvalidGrant = `{"error":"invalid_grant","error_description":"Invalid refresh token"}`
	// client_id of a client the realm does not know.
	realmInvalidClient = `{"error":"invalid_client","error_description":"Invalid client or Invalid client credentials"}`
)

// TestR_ANS_019_TheRealmsRecordedRefusalsAreUpstreamAuthFailedAndNameTheRealm: both refusals of the real realm, the unknown token's
// 400 invalid_grant and the unknown client's 401 invalid_client, are the 502 upstream_auth_failed of a refused credential (R-REPO-033),
// with the realm's host on the security event and a detail that names the realm, not the upstream. The 401 is the case the stub's realm
// answered with a 400 until the recording: the engine classified it as the upstream's refusal and said so.
func TestR_ANS_019_TheRealmsRecordedRefusalsAreUpstreamAuthFailedAndNameTheRealm(t *testing.T) {
	rem := testRemote()
	treq, err := http.NewRequest(http.MethodPost, realmTokenURL, strings.NewReader("grant_type=refresh_token"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"an unknown refresh token", http.StatusBadRequest, realmInvalidGrant},
		{"an unknown client", http.StatusUnauthorized, realmInvalidClient},
	} {
		t.Run("R-ANS-019 "+tc.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tc.status, Header: http.Header{"Content-Type": {"application/json"}}, Request: treq,
				Body: io.NopCloser(strings.NewReader(tc.body)),
			}
			got := exchangeFailure(rem, tokenFailure(treq, classifyStatus(rem, resp)))
			he, ok := asHandshakeError(got)
			if !ok || he.err == nil || he.fail == nil {
				t.Fatalf("got %#v, want a handshake error with its failure", got)
			}
			if he.err.Code != errs.CodeUpstreamAuthFailed || he.err.Status() != http.StatusBadGateway {
				t.Errorf("%d %s, want 502 %s", he.err.Status(), he.err.Code, errs.CodeUpstreamAuthFailed)
			}
			if !strings.Contains(he.err.Detail, "single sign-on realm") || he.fail.err != he.err || he.fail.host != "sso.redhat.com" {
				t.Errorf("detail %q, event error %v, event host %q: want the realm named in both", he.err.Detail, he.fail.err, he.fail.host)
			}
		})
	}
}
