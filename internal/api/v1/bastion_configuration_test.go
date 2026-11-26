package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// The operations of a Bastion's configuration that need no database (R-BAS-093, R-BAS-096): the permissions of a patch of the
// registration and the subject's limits in the check's answer. What the service writes is test/integration/bastionreg's.

// holdsOnly holds the capabilities it names and no other.
type holdsOnly map[string]bool

func (h holdsOnly) Require(_ context.Context, c string) error {
	if h[c] {
		return nil
	}
	return errs.E(errs.Forbidden, errs.CodeForbidden, "missing "+c)
}

func (holdsOnly) RequireAction(context.Context, authz.Coordinate) error { return nil }

// TestR_BAS_093_ThePatchOfTheLimitsNeedsLimitsWrite (Q-BAS-325): a patch of limits with endpoints:write alone answers 403, a patch of
// another member with limits:write alone 403, an unknown preset 422 and limits that are no object 400, each before the service
// writes. Milliseconds, no database.
func TestR_BAS_093_ThePatchOfTheLimitsNeedsLimitsWrite(t *testing.T) {
	regs := bastionmain.NewRegistrations(bastionmain.RegistrationOptions{Max: 4}) // no database: every answer below comes before it
	patch := func(access Access, body string) *httptest.ResponseRecorder {
		s := newSetup(t, func(o *Options) { o.Access, o.Bastions, o.API.Config.MaxRequestBodyBytes = access, regs, config.MiB }, authed)
		return s.do(http.MethodPatch, "/api/v1/bastions/partners", body, map[string]string{"Content-Type": "application/merge-patch+json", "If-Match": `"1"`})
	}
	problem(t, patch(holdsOnly{"endpoints:write": true}, `{"limits":{"mode":"audit"}}`), http.StatusForbidden, errs.CodeForbidden)
	problem(t, patch(holdsOnly{"endpoints:write": true}, `{"limits":{"preset":"internal"}}`), http.StatusForbidden, errs.CodeForbidden)
	problem(t, patch(holdsOnly{"limits:write": true}, `{"description":"x"}`), http.StatusForbidden, errs.CodeForbidden)
	problem(t, patch(holdsOnly{"limits:write": true}, `{"limits":{"mode":"audit"},"switches":{"gui":true}}`), http.StatusForbidden, errs.CodeForbidden)
	if w := patch(allowAll{}, `{"limits":{"preset":"fast"}}`); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an unknown preset: %d %s", w.Code, w.Body)
	}
	if w := patch(allowAll{}, `{"limits":[1]}`); w.Code != http.StatusBadRequest {
		t.Fatalf("limits that are no object: %d %s", w.Code, w.Body)
	}
}

// TestR_BAS_096_TheCheckNamesTheSubjectsLimits: the check's answer names a token subject's key and rule at the Bastion's place when
// the main installation resolves them, for the recognized registration, and leaves the member out otherwise. Milliseconds.
func TestR_BAS_096_TheCheckNamesTheSubjectsLimits(t *testing.T) {
	var asked string
	cs := newCheckSetupWith(t, checkedCaller(nil), func(b *BastionServices) {
		b.SubjectLimits = func(_ context.Context, _ httpserver.Principal, bastionID string) *iface.SubjectLimits {
			asked = bastionID
			return &iface.SubjectLimits{Key: "technical_token:0192f1c4-0000-7000-8000-0000000000e3", Rule: "0193a0c6-0000-7000-8000-000000000001"}
		}
	})
	ans := decodeCheck(t, cs.post(iface.PathCheck, `{"credential":"qdr_sat_x","clientAddress":"203.0.113.7","repositories":["it/npm-all"]}`))
	if l := ans.Subject.Limits; l == nil || l.Rule != "0193a0c6-0000-7000-8000-000000000001" || asked != "0192f1c4-0000-7000-8000-0000000000b1" {
		t.Fatalf("subject %+v, asked for %q", ans.Subject, asked)
	}
	plain := newCheckSetup(t, checkedCaller(nil))
	if ans := decodeCheck(t, plain.post(iface.PathCheck, `{"credential":"qdr_sat_x","clientAddress":"203.0.113.7","repositories":["it/npm-all"]}`)); ans.Subject.Limits != nil {
		t.Fatalf("without the resolution: %+v", ans.Subject)
	}
}
