package audit

import (
	"encoding/json"
	"testing"
)

// TestR_REPO_048_APublishRecordsAnUndeterminedShadowingCheck: PublishGuards writes shadowCheck "undetermined" and the claim's outcome
// into a publish's details, and leaves both out when there is nothing to say; the two publish events that take them (npm.publish,
// package.published of NuGet and Ansible Galaxy) declare both keys with the value the function writes, so no event drops them.
func TestR_REPO_048_APublishRecordsAnUndeterminedShadowingCheck(t *testing.T) {
	d := map[string]any{"package": "acme.widgets"}
	PublishGuards(d, "", false)
	if len(d) != 1 {
		t.Fatalf("nothing to say added %v", d)
	}
	PublishGuards(d, "created", true)
	if d["shadowCheck"] != "undetermined" || d["claim"] != "created" {
		t.Fatalf("details = %v", d)
	}
	for _, name := range []string{EventNPMPublish, EventPackagePublished} {
		et, ok := Lookup(name)
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		var schema struct {
			Properties map[string]struct {
				Type string   `json:"type"`
				Enum []string `json:"enum"`
			} `json:"properties"`
		}
		if err := json.Unmarshal([]byte(et.Schema), &schema); err != nil {
			t.Fatal(err)
		}
		if p, ok := schema.Properties["shadowCheck"]; !ok || len(p.Enum) != 1 || p.Enum[0] != ShadowUndetermined {
			t.Errorf("%s declares shadowCheck as %+v", name, p)
		}
		if p, ok := schema.Properties["claim"]; !ok || p.Type != "string" {
			t.Errorf("%s declares claim as %+v", name, p)
		}
	}
}
