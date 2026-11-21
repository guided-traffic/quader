//go:build integration

package upstream

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/test/integration/stack"
)

// TestR_REPO_073_TheOperationsOfTheWellKnownRepositories runs each operation of R-API-083 over HTTP on two replicas, every answer
// checked against the document: its success, each error of its table once, the events of R-REPO-068 and the permissions of
// R-AUTHZ-041 (Q-AUTHZ-303, Q-API-361, Q-API-362, Q-REPO-384). The secrets are references, which need no key ring.
func TestR_REPO_073_TheOperationsOfTheWellKnownRepositories(t *testing.T) {
	f := newFixture(t)
	f.listVirtual("web", "npm-all", "npm", "npmjs")
	f.listVirtual("web", "npm-build", "npm", "npmjs")
	f.listVirtual("ops", "npm-all", "npm", "npmjs")
	// carol owns the project web and holds no capability of the instance; dave reads in it.
	carol, dave := f.e.User("carol"), f.e.User("dave")
	f.e.Member("web", carol, "owner")
	f.e.Member("web", dave, "reader")
	f.start()

	t.Run("R-REPO-073 the list holds the presets in catalog order and the system repositories last", func(t *testing.T) {
		r := f.do(f.admin, "GET", items, nil)
		wantStatus(t, r, 200, "")
		list := r.Items(t)
		if len(list) != 14 || list[0]["key"] != "galaxy" || list[12]["key"] != "scanner-trivy" || list[13]["key"] != "scanner-clamav" {
			t.Fatalf("%d items, first %v, last %v", len(list), list[0]["key"], list[len(list)-1]["key"])
		}
		sys := list[12]
		if sys["state"] != nil || sys["inUse"] != false || fmt.Sprint(sys["fixed"]) != "[state visibility]" || sys["kind"] != "system" {
			t.Errorf("scanner-trivy %v", sys)
		}
		// R-REPO-075, Q-REPO-412: the local system repository has no address and no account an administrator could set.
		clam := list[13]
		addr, _ := clam["address"].(map[string]any)
		if clam["state"] != nil || clam["kind"] != "system" || clam["format"] != "generic" || clam["visibility"] != "private" ||
			clam["displayName"] != "Malware scan database" || fmt.Sprint(clam["fixed"]) != "[state visibility address account]" || addr["url"] != "" {
			t.Errorf("scanner-clamav %v", clam)
		}
		for _, it := range list[:12] {
			if it["state"] != "not-switched-on" || it["inUse"] != nil {
				t.Errorf("%v: state %v, inUse %v", it["key"], it["state"], it["inUse"])
			}
			if _, named := it["listedByNames"]; named {
				t.Errorf("Q-API-363 %v: the list names the virtual repositories", it["key"])
			}
		}
		npm := list[slices.IndexFunc(list, func(it map[string]any) bool { return it["key"] == "npmjs" })]
		if npm["listedBy"] != float64(3) || npm["listedByProjects"] != float64(2) {
			t.Errorf("Q-API-363 npmjs listed by %v in %v projects, want 3 in 2", npm["listedBy"], npm["listedByProjects"])
		}
	})
	t.Run("R-REPO-073 an item carries its ETag, its listing and its archive keys", func(t *testing.T) {
		r := f.do(f.admin, "GET", items+"/npmjs", nil)
		wantStatus(t, r, 200, "")
		if r.Header.Get("ETag") != fmt.Sprintf(`"%v"`, r.JSON(t)["configVersion"]) || r.JSON(t)["listedBy"] != float64(3) || r.JSON(t)["name"] != "upstream/npmjs" {
			t.Errorf("%v %v", r.Header.Get("ETag"), r.JSON(t))
		}
		if names := fmt.Sprint(r.JSON(t)["listedByNames"]); names != "[ops/npm-all web/npm-all web/npm-build]" || r.JSON(t)["listedByProjects"] != float64(2) {
			t.Errorf("Q-API-363 listed by %s in %v projects", names, r.JSON(t)["listedByProjects"])
		}
		deb := f.do(f.admin, "GET", items+"/debian", nil).JSON(t)["address"].(map[string]any)
		if keys, _ := deb["upstreamKeys"].([]any); len(keys) != 9 {
			t.Errorf("debian keys %v", deb["upstreamKeys"])
		}
		wantStatus(t, f.do(f.admin, "GET", items+"/nope", nil), 404, "repository_not_found")
	})
	t.Run("Q-AUTHZ-303 Q-API-362 the auditor reads and is refused the writes; a caller without the capability gets 403", func(t *testing.T) {
		wantStatus(t, f.do(f.auditor, "GET", items, nil), 200, "")
		wantStatus(t, f.do(f.auditor, "PUT", items+"/npmjs/state", map[string]any{"state": "on"}), 403, "")
		wantStatus(t, f.do(f.alice, "GET", items, nil), 403, "")
		wantStatus(t, f.do(f.alice, "GET", items+"/npmjs", nil), 403, "")
	})
	t.Run("R-REPO-068 switching on and off records its events and its listing", func(t *testing.T) {
		on := f.do(f.admin, "PUT", items+"/npmjs/state", map[string]any{"state": "on"})
		wantStatus(t, on, 200, "")
		if on.JSON(t)["state"] != "active" || f.detail("repository.updated", "npmjs", "reason") != "switched_on" ||
			f.e.Number("SELECT count(*) FROM upstream_repository WHERE repository_id = $1 AND first_switched_on_at IS NOT NULL", f.id("npmjs")) != 1 {
			t.Fatalf("%v, reason %s", on.JSON(t)["state"], f.detail("repository.updated", "npmjs", "reason"))
		}
		if f.e.Number(`SELECT count(*) FROM cluster_change WHERE area = 'repo_config' AND payload->'p'->>'key' = 'upstream/npmjs'
		               AND (payload->'p'->>'version')::bigint = $1`, int64(on.JSON(t)["configVersion"].(float64))) != 1 {
			t.Error("R-REPO-068 the switch-on wrote no change of repo_config, so no replica's snapshot takes the member up")
		}
		virtual := f.e.Number("SELECT config_version FROM repositories r JOIN project p ON p.id = r.project_id WHERE p.key = 'web' AND r.key = 'npm-all'")
		off := f.do(f.admin, "PUT", items+"/npmjs/state", map[string]any{"state": "off"})
		wantStatus(t, off, 200, "")
		if off.JSON(t)["state"] != "switched-off" || f.detail("repository.disabled", "npmjs", "reason") != "switched_off" ||
			f.detail("repository.disabled", "npmjs", "listedBy") != `["ops/npm-all", "web/npm-all", "web/npm-build"]` ||
			fmt.Sprint(off.JSON(t)["listedByNames"]) != "[ops/npm-all web/npm-all web/npm-build]" {
			t.Fatalf("%v, %s", off.JSON(t)["state"], f.detail("repository.disabled", "npmjs", "listedBy"))
		}
		if v := f.e.Number("SELECT config_version FROM repositories r JOIN project p ON p.id = r.project_id WHERE p.key = 'web' AND r.key = 'npm-all'"); v != virtual {
			t.Error("Q-REPO-383 the virtual repository that lists it changed")
		}
		before := f.events("repository.updated", "npmjs")
		wantStatus(t, f.do(f.admin, "PUT", items+"/npmjs/state", map[string]any{"state": "off"}), 200, "")
		if f.events("repository.updated", "npmjs") != before {
			t.Error("a write that changes nothing recorded an event")
		}
	})
	t.Run("R-REPO-068 a retired repository is not switched on, and a system repository has no state", func(t *testing.T) {
		f.e.Exec("UPDATE upstream_repository SET retired_at = now() WHERE repository_id = $1", f.id("ubuntu-ports"))
		wantStatus(t, f.do(f.admin, "PUT", items+"/ubuntu-ports/state", map[string]any{"state": "on"}), 409, "upstream_retired")
		wantStatus(t, f.do(f.admin, "PUT", items+"/scanner-trivy/state", map[string]any{"state": "on"}), 409, "managed_by_product")
	})
	t.Run("Q-API-364 the member picker's read answers an Owner the state and the level, and nothing else", func(t *testing.T) {
		const picker = "/api/v1/formats/%s/upstream-repositories"
		wantStatus(t, f.do(stack.Person{}, "GET", fmt.Sprintf(picker, "npm"), nil), 401, "")
		wantStatus(t, f.do(dave, "GET", fmt.Sprintf(picker, "npm"), nil), 403, "")
		wantStatus(t, f.do(carol, "GET", fmt.Sprintf(picker, "nope"), nil), 404, "not_found")
		npm := f.do(carol, "GET", fmt.Sprintf(picker, "npm"), nil)
		wantStatus(t, npm, 200, "")
		got := npm.Items(t)
		if len(got) != 1 || fmt.Sprint(got[0]) != "map[displayName:npm public registry key:npmjs name:upstream/npmjs state:switched-off visibility:internal]" {
			t.Errorf("npm %v", got)
		}
		var keys []string
		for _, format := range []string{"oci", "deb"} {
			for _, it := range f.do(carol, "GET", fmt.Sprintf(picker, format), nil).Items(t) {
				keys = append(keys, fmt.Sprint(it["key"]))
			}
		}
		if strings.Join(keys, ",") != "docker-hub,quay,ghcr,gitlab,debian,debian-security,ubuntu,ubuntu-security" {
			t.Errorf("R-REPO-070 neither scanner-trivy nor the retired ubuntu-ports is a member to choose: %v", keys)
		}
	})
	t.Run("Q-REPO-384 Switch all on switches on what is off and not retired, internal, and leaves the active ones' levels", func(t *testing.T) {
		// quay is lowered to private and switched on: an active repository's level is an administrator's, which Switch all on keeps.
		wantStatus(t, f.do(f.admin, "PUT", items+"/quay/visibility", map[string]any{"level": "private"}), 200, "")
		wantStatus(t, f.do(f.admin, "PUT", items+"/quay/state", map[string]any{"state": "on"}), 200, "")
		r := f.do(f.admin, "POST", items+"/switch-on-all", nil, "Idempotency-Key", "all-on-1")
		wantStatus(t, r, 200, "")
		body := r.JSON(t)
		if n := len(body["items"].([]any)); n != 10 {
			t.Fatalf("switched on %d, want 10 (twelve presets, quay active, ubuntu-ports retired)", n)
		}
		bulk := body["bulkId"].(string)
		if f.e.Number("SELECT count(*) FROM audit_event WHERE action = 'repository.updated' AND details->>'bulkId' = $1", bulk) != 10 {
			t.Error("the events do not carry the bulk ID")
		}
		quay := f.do(f.admin, "GET", items+"/quay", nil).JSON(t)
		trivy := f.do(f.admin, "GET", items+"/scanner-trivy", nil).JSON(t)
		npm := f.do(f.admin, "GET", items+"/npmjs", nil).JSON(t)
		if quay["visibility"] != "private" || trivy["visibility"] != "private" || npm["visibility"] != "internal" || npm["state"] != "active" {
			t.Errorf("quay %v, scanner-trivy %v, npmjs %v %v", quay["visibility"], trivy["visibility"], npm["visibility"], npm["state"])
		}
		again := f.do(f.admin, "POST", items+"/switch-on-all", nil, "Idempotency-Key", "all-on-1")
		if again.Header.Get("Idempotency-Replayed") != "true" {
			t.Error("R-API-014 a retried bulk ran again")
		}
	})
	t.Run("R-REPO-073 the level of one and of all; scanner-trivy's is fixed", func(t *testing.T) {
		wantStatus(t, f.do(f.admin, "PUT", items+"/scanner-trivy/visibility", map[string]any{"level": "internal"}), 409, "managed_by_product")
		wantStatus(t, f.do(f.admin, "PUT", items+"/scanner-clamav/visibility", map[string]any{"level": "internal"}), 409, "managed_by_product")
		r := f.do(f.admin, "PUT", items+"/npmjs/visibility", map[string]any{"level": "public"})
		wantStatus(t, r, 200, "")
		if r.JSON(t)["visibility"] != "public" {
			t.Fatalf("%v", r.JSON(t))
		}
		all := f.do(f.admin, "POST", items+"/set-visibility", map[string]any{"level": "internal"}, "Idempotency-Key", "vis-1")
		wantStatus(t, all, 200, "")
		if n := len(all.JSON(t)["items"].([]any)); n != 2 { // npmjs from public, quay from private
			t.Fatalf("changed %d", n)
		}
	})

	f.e.Exec(`UPDATE repositories SET config = jsonb_set(jsonb_set(config, '{remote,authMode}', '"basic"'), '{remote,password}', '{"secretRef":{"env":"QUADER_CAC_HUB"}}')
	          WHERE id = $1`, f.id("docker-hub"))
	f.e.Exec(`INSERT INTO repository_secrets (repo_id, field, secret_ref) VALUES ($1, '/remote/password', '{"env":"QUADER_CAC_HUB"}')`, f.id("docker-hub"))
	f.e.Exec("UPDATE repositories SET config_version = config_version + 1 WHERE id = $1", f.id("docker-hub"))
	t.Run("R-REPO-069 an address that moves the account's host needs the account; the reset brings the product's back", func(t *testing.T) {
		cur := f.do(f.admin, "GET", items+"/docker-hub", nil)
		tag := cur.Header.Get("ETag")
		wantStatus(t, f.do(f.admin, "PUT", items+"/docker-hub/address", map[string]any{"url": "https://mirror.example.com/"}), 428, "precondition_required")
		wantStatus(t, f.do(f.admin, "PUT", items+"/docker-hub/address", map[string]any{"url": "https://mirror.example.com/"}, "If-Match", `"1"`), 412, "")
		refused := f.do(f.admin, "PUT", items+"/docker-hub/address", map[string]any{"url": "https://mirror.example.com/"}, "If-Match", tag)
		wantStatus(t, refused, 422, "secret_value_required")
		if !hasField(t, refused, "/account/password", "secret_value_required") {
			t.Errorf("errors %s", refused.Body)
		}
		ok := f.do(f.admin, "PUT", items+"/docker-hub/address", map[string]any{"url": "https://mirror.example.com/", "account": nil}, "If-Match", tag)
		wantStatus(t, ok, 200, "")
		addr := ok.JSON(t)["address"].(map[string]any)
		if addr["overridden"] != true || addr["url"] != "https://mirror.example.com/" || ok.JSON(t)["account"].(map[string]any)["authMode"] != "none" {
			t.Fatalf("%v %v", addr, ok.JSON(t)["account"])
		}
		reset := f.do(f.admin, "DELETE", items+"/docker-hub/address", nil, "If-Match", ok.Header.Get("ETag"))
		wantStatus(t, reset, 200, "")
		addr = reset.JSON(t)["address"].(map[string]any)
		if addr["overridden"] != false || addr["url"] != "https://registry-1.docker.io/" {
			t.Fatalf("%v", addr)
		}
		wantStatus(t, f.do(f.admin, "PUT", items+"/npmjs/address", map[string]any{"upstreamKeys": "x"}, "If-Match", "*"), 422, "not_applicable")
	})
	t.Run("R-REPO-069 an account is stored without its secret and removed", func(t *testing.T) {
		set := f.do(f.admin, "PUT", items+"/quay/account", map[string]any{"authMode": "basic", "username": "robot", "password": map[string]any{"secretRef": map[string]any{"env": "QUADER_CAC_QUAY"}}}, "If-Match", "*")
		wantStatus(t, set, 200, "")
		acct := set.JSON(t)["account"].(map[string]any)
		if acct["authMode"] != "basic" || acct["username"] != "robot" || fmt.Sprint(acct["password"]) != "map[secretRef:map[env:QUADER_CAC_QUAY]]" {
			t.Fatalf("%v", acct)
		}
		wantStatus(t, f.do(f.admin, "PUT", items+"/quay/account", map[string]any{"authMode": "basic", "password": map[string]any{"set": false}}, "If-Match", "*"), 422, "invalid_secret")
		gone := f.do(f.admin, "DELETE", items+"/quay/account", nil, "If-Match", set.Header.Get("ETag"))
		wantStatus(t, gone, 200, "")
		if gone.JSON(t)["account"].(map[string]any)["authMode"] != "none" {
			t.Fatalf("%v", gone.JSON(t)["account"])
		}
	})
	t.Run("R-REPO-075 scanner-clamav has no state, no address and no account", func(t *testing.T) {
		wantStatus(t, f.do(f.admin, "PUT", items+"/scanner-clamav/state", map[string]any{"state": "on"}), 409, "managed_by_product")
		wantStatus(t, f.do(f.admin, "PUT", items+"/scanner-clamav/address", map[string]any{"url": "https://db.example.org/"}, "If-Match", "*"), 409, "managed_by_product")
		wantStatus(t, f.do(f.admin, "PUT", items+"/scanner-clamav/account", map[string]any{"authMode": "basic", "username": "u", "password": map[string]any{"secretRef": map[string]any{"env": "QUADER_CAC_QUAY"}}}, "If-Match", "*"), 409, "managed_by_product")
		wantStatus(t, f.do(f.admin, "DELETE", items+"/scanner-clamav/account", nil, "If-Match", "*"), 409, "managed_by_product")
		if f.config("scanner-clamav", "{remote}") != "<none>" || f.config("scanner-clamav", "{disabled}") != "false" {
			t.Fatalf("scanner-clamav: remote %q, disabled %q", f.config("scanner-clamav", "{remote}"), f.config("scanner-clamav", "{disabled}"))
		}
	})
	t.Run("R-REPO-070 scanner-trivy takes an administrator's address", func(t *testing.T) {
		r := f.do(f.admin, "PUT", items+"/scanner-trivy/address", map[string]any{"url": "https://ghcr.example.org/"}, "If-Match", "*")
		wantStatus(t, r, 200, "")
		if f.config("scanner-trivy", "{remote,url}") != "https://ghcr.example.org/" || f.config("scanner-trivy", "{includePatterns,0}") != "aquasec/trivy-db" {
			t.Fatalf("%s %s", f.config("scanner-trivy", "{remote,url}"), f.config("scanner-trivy", "{includePatterns,0}"))
		}
	})
}
