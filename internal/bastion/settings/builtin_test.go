package settings

import (
	"encoding/json"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/maintest"
)

// TestR_BAS_093_TheTestDoubleSendsTheBuiltInConfiguration: the configuration the test double of the main installation sends
// (maintest.Configuration, a copy, since that package may not import this one) is the built-in configuration with the revision 1 of a
// new registration, so the Bastion's tests run against what a main installation sends for a new registration. The main installation's
// side is held to the same figures by TestR_BAS_093_ANewRegistrationIsTheBuiltInConfiguration (internal/app). Milliseconds.
func TestR_BAS_093_TheTestDoubleSendsTheBuiltInConfiguration(t *testing.T) {
	want := Builtin()
	want.Revision = 1
	g, _ := json.Marshal(maintest.Configuration(false))
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Fatalf("maintest.Configuration differs from the built-in configuration:\n maintest %s\n builtin  %s", g, w)
	}
}
