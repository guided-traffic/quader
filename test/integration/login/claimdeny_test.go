//go:build integration

package login

import (
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/oidc"
	"github.com/guided-traffic/quader/internal/config"
)

// TestR_AUTH_008_DenyRefusesEverySignInWithoutTheClaim: with on_missing_claim deny a sign-in without a usable group claim never
// reaches the provisioner, so it stores no state of the claim; it is a failed sign-in, recorded at every attempt, not a change
// of an identity's state (Q-AUTH-400).
func TestR_AUTH_008_DenyRefusesEverySignInWithoutTheClaim(t *testing.T) {
	e := newEnv(t)
	auth := e.defaultAuth()
	auth.OIDC.Providers["corp"] = e.provider(func(p *config.AuthOIDCProvider) { p.GroupSync.OnMissingClaim = config.OnMissingDeny })
	e.start(2, auth)
	e.idp.set(func(f *fakeIDP) { f.user.groups = nil })

	for range 3 {
		resp, body := e.browser().login("corp")
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, oidc.ReasonGroupsClaimMissing) {
			t.Fatalf("%d %s", resp.StatusCode, body)
		}
	}
	var refused int
	for _, ev := range e.audit.events(audit.EventAuthLoginFailed) {
		if ev.Reason == oidc.ReasonGroupsClaimMissing && ev.Outcome == audit.Failure {
			refused++
		}
	}
	if refused != 3 {
		t.Fatalf("%d auth.login.failed with groups_claim_missing, want one per attempt", refused)
	}
	if n := len(e.audit.events(audit.EventUserGroupsClaimMiss)); n != 0 {
		t.Fatalf("%d user.groups.claim_missing for refused sign-ins", n)
	}
	e.prov.mu.Lock()
	calls := len(e.prov.claims)
	e.prov.mu.Unlock()
	if calls != 0 {
		t.Fatalf("the provisioner was called %d times", calls)
	}
}
