package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
)

// readsEndpoints holds endpoints:read and no other capability.
type readsEndpoints struct{}

func (readsEndpoints) Require(_ context.Context, c string) error {
	if c == "endpoints:read" {
		return nil
	}
	return errs.E(errs.Forbidden, errs.CodeForbidden, "missing "+c)
}

func (readsEndpoints) RequireAction(context.Context, authz.Coordinate) error { return nil }

// TestR_BAS_071_TheOperationsOfTheSwitchAndTheHandshake: PATCH takes switches.handover as a boolean with endpoints:write (a caller
// with endpoints:read is refused before the service, a string is 400, and a null reads as absent, patchEnabled); POST
// /api/v1/bastions does not take it (400); the handshake says what the registration's configuration says. What the service writes,
// records and journals, and that a null switches nothing, is test/integration/bastionreg's (TestR_BAS_071_TheSwitchIsPerRegistration).
func TestR_BAS_071_TheOperationsOfTheSwitchAndTheHandshake(t *testing.T) {
	regs := bastionmain.NewRegistrations(bastionmain.RegistrationOptions{Max: 4}) // no database: every answer below comes before it
	patch := func(t *testing.T, access Access, body string) *httptest.ResponseRecorder {
		t.Helper()
		s := newSetup(t, func(o *Options) { o.Access, o.Bastions, o.API.Config.MaxRequestBodyBytes = access, regs, config.MiB }, authed)
		return s.do(http.MethodPatch, "/api/v1/bastions/partners", body, map[string]string{"Content-Type": "application/merge-patch+json", "If-Match": `"1"`})
	}
	t.Run("R-BAS-071 a caller with endpoints:read is refused", func(t *testing.T) {
		problem(t, patch(t, readsEndpoints{}, `{"switches":{"handover":true}}`), http.StatusForbidden, errs.CodeForbidden)
	})
	t.Run(`R-BAS-071 {"handover":"yes"} is refused`, func(t *testing.T) {
		if w := patch(t, allowAll{}, `{"switches":{"handover":"yes"}}`); w.Code != http.StatusBadRequest {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	})
	t.Run("R-BAS-071 a null switch reads as absent", func(t *testing.T) {
		for _, raw := range []string{"null", " null "} {
			if v, err := patchEnabled([]byte(raw), "/handover"); v != nil || err != nil {
				t.Fatalf("%q: %v %v: a null decodes into false and would switch off", raw, v, err)
			}
		}
	})
	t.Run("R-BAS-071 POST /api/v1/bastions does not take handover", func(t *testing.T) {
		s := newSetup(t, func(o *Options) { o.Bastions, o.API.Config.MaxRequestBodyBytes = regs, config.MiB }, authed)
		if w := s.do(http.MethodPost, "/api/v1/bastions", `{"name":"partners","switches":{"handover":true}}`, nil); w.Code != http.StatusBadRequest {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	})
	const id = "0192f1c4-0000-7000-8000-0000000000b1"
	for _, on := range []bool{false, true} {
		t.Run("R-BAS-071 the handshake says the configuration's switch", func(t *testing.T) {
			svc := BastionServices{
				Feed: stillFeed{}, AnonymousReads: func(context.Context, string) bool { return false },
				Configuration: func(context.Context, bastionmain.Recognition) (iface.Configuration, error) {
					return newConfiguration(t, 2, iface.Switches{Handover: on}), nil
				},
			}
			s := newSetup(t, func(o *Options) { o.Bastion = svc }, nil)
			r := httptest.NewRequest(http.MethodGet, iface.PathHandshake, nil)
			r = r.WithContext(bastionmain.WithRecognition(r.Context(), bastionmain.Recognition{
				Bastion: "partners", BastionID: id, TokenID: bastionTokenID, TokenExpiresAt: time.Date(2027, 11, 2, 0, 0, 0, 0, time.UTC),
				Route: true, Interface: iface.InterfaceVersion,
			}))
			w := httptest.NewRecorder()
			s.handler.ServeHTTP(w, r)
			var h iface.Handshake
			if err := iface.DecodeStrict(w.Body, &h); err != nil || w.Code != http.StatusOK || h.Configuration.Switches.Handover != on ||
				h.Configuration.Validate() != nil {
				t.Fatalf("%d %v: handover %v, want %v", w.Code, err, h.Configuration.Switches.Handover, on)
			}
		})
	}
}
