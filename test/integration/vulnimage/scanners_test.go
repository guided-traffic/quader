//go:build integration

package vulnimage

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// R-OPS-048, Q-OPS-223, Q-OPS-225: the image scanner in process reports itself on the scan listener with the certificate of its kind, and
// the API lists it as a replica of the kind trivy, ready, with the analyser's version, its one slot and the database its directory holds,
// whose build time the list names while it is the database in use; the kinds name trivy. No image is pushed; about 3 s.
func TestR_OPS_048_TheImageScannerReportsItself(t *testing.T) {
	f := newFlowWith(t, options{laterWorker: true})
	type row struct {
		Kind        string          `json:"kind"`
		Name        string          `json:"name"`
		State       string          `json:"state"`
		ToolVersion *string         `json:"toolVersion"`
		Database    json.RawMessage `json:"database"`
		Slots       struct {
			InUse int `json:"inUse"`
			Total int `json:"total"`
		} `json:"slots"`
	}
	var got row
	var kinds []struct {
		Kind       string `json:"kind"`
		Deployment string `json:"deployment"`
	}
	testenv.WaitFor(t, "the image scanner's ready row", 30*time.Second, func() bool {
		a := f.do("GET", "/api/v1/system/scanners", nil)
		if a.Status != http.StatusOK {
			t.Fatalf("GET /api/v1/system/scanners: %d %s", a.Status, a.Body)
		}
		var list struct {
			Kinds []struct {
				Kind       string `json:"kind"`
				Deployment string `json:"deployment"`
			} `json:"kinds"`
			Items []row `json:"items"`
		}
		if err := json.Unmarshal(a.Body, &list); err != nil {
			t.Fatal(err)
		}
		kinds = list.Kinds
		for _, r := range list.Items {
			if r.Name == "vuln-scanner-trivy" && r.State == "ready" {
				got = r
				return true
			}
		}
		return false
	})
	var db struct {
		Version string  `json:"version"`
		BuiltAt *string `json:"builtAt"`
	}
	if err := json.Unmarshal(got.Database, &db); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "trivy" || got.ToolVersion == nil || *got.ToolVersion != f.an.Version() || got.Slots.Total != 1 ||
		"sha256:"+db.Version != dbDigest(1) || db.BuiltAt == nil || *db.BuiltAt != "2026-11-21T07:02:51Z" {
		t.Fatalf("the row: %+v %s", got, got.Database)
	}
	// Another database in use: the row names the one the pod holds, and the list answers no build time for it.
	f.st.Exec(`UPDATE vuln_source_state SET etag = $1 WHERE source = 'trivy'`, dbDigest(2))
	a := f.do("GET", "/api/v1/system/scanners", nil)
	var other struct {
		Items []struct {
			Database *struct {
				Version string  `json:"version"`
				BuiltAt *string `json:"builtAt"`
			} `json:"database"`
		} `json:"items"`
	}
	if err := json.Unmarshal(a.Body, &other); err != nil || len(other.Items) != 1 || other.Items[0].Database == nil ||
		"sha256:"+other.Items[0].Database.Version != dbDigest(1) || other.Items[0].Database.BuiltAt != nil {
		t.Fatalf("with another database in use: %s", a.Body)
	}
	if len(kinds) == 0 || kinds[0].Kind != "trivy" || kinds[0].Deployment != "vuln-scanner-trivy" {
		t.Fatalf("the kinds: %+v", kinds)
	}
}
