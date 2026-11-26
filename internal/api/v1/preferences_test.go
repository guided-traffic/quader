package v1

import (
	"encoding/json"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_API_021_PreferencesTimeFormat holds the member timeFormat of the preferences (R-UI-250) at the merge of a patch: a form is
// written, null resets it to the installation's default (empty), an absent member keeps it, an unknown form is refused at its pointer,
// and the representation answers null for the default.
func TestR_API_021_PreferencesTimeFormat(t *testing.T) {
	patch := func(body string) prefsPatchBody {
		var in prefsPatchBody
		if err := json.Unmarshal([]byte(body), &in); err != nil {
			t.Fatal(err)
		}
		return in
	}
	cur := prefs{locale: "de", keyboardShortcuts: true, revision: 3}
	next, err := patch(`{"timeFormat": "iso"}`).apply(cur)
	if err != nil || next.timeFormat != "iso" || next.locale != "de" {
		t.Fatalf("set: %+v, %v", next, err)
	}
	kept, err := patch(`{"theme": "dark"}`).apply(next)
	if err != nil || kept.timeFormat != "iso" {
		t.Fatalf("absent member: %+v, %v", kept, err)
	}
	reset, err := patch(`{"timeFormat": null}`).apply(next)
	if err != nil || reset.timeFormat != "" {
		t.Fatalf("null: %+v, %v", reset, err)
	}
	if got := reset.representation().TimeFormat; got != nil {
		t.Errorf("the default answers %q, want null", *got)
	}
	if got := next.representation().TimeFormat; got == nil || *got != "iso" {
		t.Errorf("the representation lost the form: %v", got)
	}
	_, err = patch(`{"timeFormat": "yyyy-MM-dd"}`).apply(cur)
	e, ok := errs.As(err)
	if !ok || len(e.Fields) != 1 || e.Fields[0].Pointer != "/timeFormat" {
		t.Fatalf("unknown form: %v", err)
	}
}
