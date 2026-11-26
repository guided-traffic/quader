package app

import (
	"encoding/json"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/limits"
)

// TestR_BAS_093_ANewRegistrationIsTheBuiltInConfiguration: what the main installation sends for a new registration (its default limits
// and the Internet preset's five kind rules with the two Bastion profiles' built-in figures, R-LIM-046) is the Bastion's built-in
// configuration (settings.Builtin, Q-BAS-327, Q-BAS-448) but for the revision and the rules' IDs, so a replica without its stored
// configuration is exactly as strict as a new registration, and the figures of the two sides cannot drift apart. The composition root
// is the one package that may import both sides (R-BAS-001). Milliseconds, no data.
func TestR_BAS_093_ANewRegistrationIsTheBuiltInConfiguration(t *testing.T) {
	ids := map[string]string{limits.ProfileBastionAnonymous: settings.BuiltinRuleAnonymous, limits.ProfileBastionSignedIn: settings.BuiltinRuleSignedIn}
	var rules []bastionmain.PlaceRule
	for kind, profile := range bastionmain.Presets()[iface.PresetInternet].Kinds {
		id, ok := ids[profile]
		if !ok {
			t.Fatalf("the Internet preset gives %s the profile %s, which the built-in configuration does not hold", kind, profile)
		}
		rules = append(rules, bastionmain.PlaceRule{
			ID: id, WhoKind: limits.WhoEvery, WhoRef: kind, Profile: profile, Builtin: true, Figures: limits.BuiltinProfiles()[profile],
		})
	}
	got, err := bastionmain.Configuration(1, iface.Switches{}, bastionmain.DefaultLimits(), rules)
	if err != nil {
		t.Fatal(err)
	}
	want := settings.Builtin()
	want.Revision = 1
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Fatalf("a new registration's configuration differs from the Bastion's built-in one:\n main    %s\n bastion %s", g, w)
	}
}
