package manifest

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/oci/rows"
)

// R-OCI-042: a Helm chart's configuration is its Chart.yaml as JSON, read as an image's is: the facts, the lists bounded, a value of a
// type Helm refuses read as nothing.
func TestR_OCI_042_ReadsAChartsConfiguration(t *testing.T) {
	deps := make([]map[string]string, maxChartDependencies+2)
	for i := range deps {
		deps[i] = map[string]string{"name": fmt.Sprintf("dep%d", i), "version": "^1.0.0"}
	}
	doc, _ := json.Marshal(map[string]any{
		"apiVersion": "v2", "name": "web", "version": "0.3.0", "appVersion": "2.4.1", "deprecated": true,
		"keywords": []string{"web"}, "maintainers": []map[string]string{{"name": "Ops", "email": "ops@example.com"}},
		"dependencies": deps, "annotations": map[string]string{"artifacthub.io/license": "MIT"},
	})
	got := ReadConfig(ConfigKind(MediaTypeHelmConfig), doc)
	c := got.Chart
	if got.Kind != rows.ConfigChart || c == nil || c.AppVersion != "2.4.1" || !c.Deprecated || len(c.Maintainers) != 1 ||
		len(c.Dependencies) != maxChartDependencies || c.Omitted == nil || c.Omitted.Other != 2 || c.Annotations[0].Value != "MIT" {
		t.Fatalf("%+v %+v", got, c)
	}
	if bad := ReadConfig(rows.ConfigChart, []byte(`{"deprecated":"yes"}`)); bad.Unreadable != rows.UnreadableInvalid {
		t.Errorf("a value of the wrong type: %+v", bad)
	}
}
