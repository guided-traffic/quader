package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
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

// TestR_BAS_093_TheReadOfARegistrationNeedsEndpointsReadOrLimitsRead: a caller that holds neither endpoints:read nor limits:read
// is refused 403 before the service reads (the shared step admits every authenticated caller). What either one reads is
// test/integration/bastionreg's. Milliseconds, no database.
func TestR_BAS_093_TheReadOfARegistrationNeedsEndpointsReadOrLimitsRead(t *testing.T) {
	regs := bastionmain.NewRegistrations(bastionmain.RegistrationOptions{Max: 4})
	for _, access := range []holdsOnly{{}, {"limits:write": true}, {"endpoints:write": true}, {"usage:read": true}} {
		s := newSetup(t, func(o *Options) { o.Access, o.Bastions = access, regs }, authed)
		problem(t, s.do(http.MethodGet, "/api/v1/bastions/partners", "", nil), http.StatusForbidden, errs.CodeForbidden)
	}
}

// TestR_BAS_093_ThePresetsAreReadWithLimitsRead (Q-UI-1485): GET /api/v1/bastions/presets answers the three presets of
// bastionmain.Presets in the card's order with the profiles of the five kind rules, the flood figures and the mode, to a holder of
// limits:read, and 403 to a holder of endpoints:read alone. Milliseconds, no database.
func TestR_BAS_093_ThePresetsAreReadWithLimitsRead(t *testing.T) {
	get := func(access Access) *httptest.ResponseRecorder {
		s := newSetup(t, func(o *Options) { o.Access = access }, authed)
		return s.do(http.MethodGet, "/api/v1/bastions/presets", "", nil)
	}
	problem(t, get(holdsOnly{"endpoints:read": true}), http.StatusForbidden, errs.CodeForbidden)
	w := get(holdsOnly{"limits:read": true})
	var body struct {
		Items []struct {
			Name  string            `json:"name"`
			Kinds map[string]string `json:"kinds"`
			Flood iface.Flood       `json:"flood"`
			Mode  string            `json:"mode"`
		} `json:"items"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body.Items) != 3 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	want := bastionmain.Presets()
	for i, name := range []iface.LimitsPreset{iface.PresetInternet, iface.PresetPartnerNetwork, iface.PresetInternal} {
		got, p := body.Items[i], want[name]
		if got.Name != string(name) || got.Mode != string(p.Mode) || !reflect.DeepEqual(got.Flood, p.Flood) {
			t.Errorf("item %d: %+v, want %s", i, got, name)
		}
		for api, store := range map[string]string{"anonymous": "anonymous", "user": "user", "technicalToken": "technical_token", "ci": "ci", "administrator": "administrator"} {
			if got.Kinds[api] != p.Kinds[store] {
				t.Errorf("%s: the kind %s names %q, the server applies %q", name, api, got.Kinds[api], p.Kinds[store])
			}
		}
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

// TestR_BAS_104_TheRegistrationSaysTheBuildersLastOutcome: the member catalogue of a registration (the card's line) is absent without
// a state, pending before the first run completed, built after one, and stopped with its reason, which is one of the document's.
func TestR_BAS_104_TheRegistrationSaysTheBuildersLastOutcome(t *testing.T) {
	at := time.Date(2026, 11, 27, 9, 30, 0, 0, time.FixedZone("CET", 3600))
	if got := toBastion(bastionmain.Registration{}).Catalogue; got != nil {
		t.Fatalf("no state: %+v", got)
	}
	for _, c := range []struct {
		state   bastionmain.CatalogueOutcome
		outcome gen.BastionCatalogueOutcomeOutcome
		reason  string
	}{
		{bastionmain.CatalogueOutcome{}, gen.BastionCatalogueOutcomeOutcomePending, ""},
		{bastionmain.CatalogueOutcome{CheckedAt: &at, BuiltAt: &at}, gen.BastionCatalogueOutcomeOutcomeBuilt, ""},
		{bastionmain.CatalogueOutcome{Stopped: "bastion_base_url_unknown", CheckedAt: &at}, gen.BastionCatalogueOutcomeOutcomeStopped, "bastion_base_url_unknown"},
		{bastionmain.CatalogueOutcome{Stopped: "anonymous_requests_refused", CheckedAt: &at}, gen.BastionCatalogueOutcomeOutcomeStopped, "anonymous_requests_refused"},
	} {
		got := toBastion(bastionmain.Registration{Catalogue: &c.state}).Catalogue
		reason := ""
		if got.Reason != nil {
			reason = string(*got.Reason)
			if !got.Reason.Valid() {
				t.Errorf("%q is no reason of the document", reason)
			}
		}
		if got.Outcome != c.outcome || reason != c.reason {
			t.Errorf("%+v: %s %q, want %s %q", c.state, got.Outcome, reason, c.outcome, c.reason)
		}
		if got.CheckedAt != nil && got.CheckedAt.Location() != time.UTC {
			t.Errorf("checkedAt not in UTC: %v", got.CheckedAt)
		}
	}
}

// TestR_BAS_131_TheSwitchHideRepositoriesIsReadAndPatched: the registration's representation carries switches.hideRepositories, and a
// merge patch reads it as the other switches: absent and null are unchanged, a boolean is the switch, any other value is refused. The
// write, the revision and the 412 of a stale ETag are the service's (test/integration/bastionreg). Microseconds, no database.
func TestR_BAS_131_TheSwitchHideRepositoriesIsReadAndPatched(t *testing.T) {
	sw := toBastion(bastionmain.Registration{Switches: iface.Switches{GUI: true, HideRepositories: true}}).Switches
	if sw == nil || !sw.HideRepositories || !sw.Gui || sw.Docs {
		t.Fatalf("the representation's switches: %+v", sw)
	}
	raw, err := json.Marshal(sw)
	if err != nil || string(raw) != `{"docs":false,"gui":true,"handover":false,"hideRepositories":true}` {
		t.Fatalf("the switches as JSON: %s %v", raw, err)
	}
	on, off := true, false
	for body, want := range map[string]*bool{
		`{"hideRepositories":true}`:  &on,
		`{"hideRepositories":false}`: &off,
		`{"hideRepositories":null}`:  nil,
		`{"gui":true}`:               nil,
	} {
		var p bastionmain.Patch
		if err := patchSwitches(json.RawMessage(body), &p); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if !reflect.DeepEqual(p.HideRepositories, want) {
			t.Errorf("%s reads %v, want %v", body, p.HideRepositories, want)
		}
	}
	var p bastionmain.Patch
	if err := patchSwitches(json.RawMessage(`{"hideRepositories":"yes"}`), &p); err == nil {
		t.Error("a string is read as the switch")
	}
}
