//go:build integration

package upstream

import (
	"fmt"
	"testing"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/api/v1/apitest"
	"github.com/guided-traffic/quader/test/integration/stack"
)

// TestR_REPO_075_TheSwitchOfClamAVDefinitionUpdates: upstream/clamav starts with its clients off and its level private and fixed;
// setUpstreamRepositoryClients switches them on at public (the level then the administrator's), records repository.updated with clients
// among its changes, and switches them off again with the level back at private and a journaled repository.narrowed; where the
// installation allows no public level the switch-on lands at internal with a warning; every other repository has no such switch; no
// repository of the format clamav is created in a project (R-REPO-073, R-REPO-075, R-CLAMAV-001, Q-REPO-414, Q-API-382). The answers of
// the endpoint itself are the malware area's (R-CLAMAV-009). About 2 s, rows only.
func TestR_REPO_075_TheSwitchOfClamAVDefinitionUpdates(t *testing.T) {
	f := newFixture(t)
	f.e.Project("web")
	f.start()
	clients := func(r stack.Response) map[string]any {
		c, _ := r.JSON(t)["clients"].(map[string]any)
		return c
	}

	t.Run("R-REPO-075 the clients are off and the level is private and fixed", func(t *testing.T) {
		r := f.do(f.admin, "GET", items+"/clamav", nil)
		wantStatus(t, r, 200, "")
		if clients(r)["enabled"] != false || r.JSON(t)["visibility"] != "private" || fmt.Sprint(r.JSON(t)["fixed"]) != "[state visibility address account]" {
			t.Fatalf("%s", r.Body)
		}
		wantStatus(t, f.do(f.admin, "PUT", items+"/clamav/visibility", map[string]any{"level": "internal"}), 409, "managed_by_product")
	})
	t.Run("R-API-083 only clamav has the switch", func(t *testing.T) {
		wantStatus(t, f.do(f.admin, "PUT", items+"/scanner-trivy/clients", map[string]any{"enabled": true}), 409, "managed_by_product")
		wantStatus(t, f.do(f.admin, "PUT", items+"/npmjs/clients", map[string]any{"enabled": true}), 409, "managed_by_product")
		wantStatus(t, f.do(f.auditor, "PUT", items+"/clamav/clients", map[string]any{"enabled": true}), 403, "")
	})
	t.Run("R-REPO-075 switching on opens the repository at public, and the level is then the administrator's", func(t *testing.T) {
		r := f.do(f.admin, "PUT", items+"/clamav/clients", map[string]any{"enabled": true})
		wantStatus(t, r, 200, "")
		if clients(r)["enabled"] != true || r.JSON(t)["visibility"] != "public" || fmt.Sprint(r.JSON(t)["fixed"]) != "[state address account]" {
			t.Fatalf("%s", r.Body)
		}
		if f.e.Text("SELECT clients_enabled::text FROM upstream_repository WHERE repository_id = $1", f.id("clamav")) != "true" {
			t.Fatal("clients_enabled is not stored")
		}
		if f.detail("repository.updated", "clamav", "reason") != "clients_on" ||
			f.e.Number(`SELECT count(*) FROM audit_event WHERE action = 'repository.updated' AND repository = 'upstream/clamav'
			            AND details->'changes' ? 'clients'`) != 1 {
			t.Fatalf("event: reason %s", f.detail("repository.updated", "clamav", "reason"))
		}
		lv := f.do(f.admin, "PUT", items+"/clamav/visibility", map[string]any{"level": "internal"})
		wantStatus(t, lv, 200, "")
		if lv.JSON(t)["visibility"] != "internal" {
			t.Fatalf("%s", lv.Body)
		}
	})
	t.Run("R-REPO-075 switching off closes it, sets the level back to private and journals the narrowing", func(t *testing.T) {
		r := f.do(f.admin, "PUT", items+"/clamav/clients", map[string]any{"enabled": false})
		wantStatus(t, r, 200, "")
		if clients(r)["enabled"] != false || r.JSON(t)["visibility"] != "private" {
			t.Fatalf("%s", r.Body)
		}
		if f.detail("repository.updated", "clamav", "reason") != "clients_off" {
			t.Fatalf("reason %s", f.detail("repository.updated", "clamav", "reason"))
		}
		journaled := f.e.Number(`SELECT count(*) FROM revocation_outbox WHERE event = 'repository.narrowed' AND entry->>'key' = 'upstream/clamav'
		                         AND entry->>'clientsOff' = 'true'`)
		if journaled != 1 {
			t.Fatalf("%d journaled narrowings", journaled)
		}
	})
	t.Run("R-REPO-075 without a public level the switch-on lands at internal with a warning", func(t *testing.T) {
		closed := f.e.NewReplica("web-closed", stack.Options{NoPublic: true, Validator: apitest.New(t, v1.EmbeddedYAML())})
		r := closed.Do(f.admin.Name, "PUT", items+"/clamav/clients", map[string]any{"enabled": true})
		wantStatus(t, r, 200, "")
		ws, _ := r.JSON(t)["warnings"].([]any)
		if r.JSON(t)["visibility"] != "internal" || len(ws) != 1 || ws[0].(map[string]any)["code"] != "public_not_allowed_lowered" {
			t.Fatalf("%s", r.Body)
		}
	})
	t.Run("R-CLAMAV-001 no repository of the format clamav is created", func(t *testing.T) {
		r := f.do(f.admin, "POST", "/api/v1/projects/web/repositories", map[string]any{"key": "defs", "class": "local", "format": "clamav"})
		wantStatus(t, r, 422, "format_system_only")
		if !hasField(t, r, "/format", "format_system_only") {
			t.Fatalf("%s", r.Body)
		}
		f2 := f.do(f.admin, "GET", "/api/v1/formats", nil)
		for _, it := range f2.JSON(t)["formats"].([]any) {
			m := it.(map[string]any)
			if (m["name"] == "clamav") != (m["systemOnly"] == true) {
				t.Errorf("%v: systemOnly %v", m["name"], m["systemOnly"])
			}
		}
	})
}
