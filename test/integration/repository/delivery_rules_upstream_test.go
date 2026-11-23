//go:build integration

package repository

import (
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/wellknown"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// rulesWellKnown writes a remote repository of the project upstream with its row of upstream_repository in one statement, as the
// reconcile of quader migrate does (R-REPO-066: the deferred guard refuses a commit of the one without the other); a repository the
// template holds already takes the document and the level given.
func rulesWellKnown(st *testenv.Stack, key, kind, cfg, level string) {
	if st.Number(`SELECT count(*) FROM repositories WHERE project_id = $1::uuid AND key = $2`, wellknown.ProjectID, key) > 0 {
		st.Exec(`UPDATE repositories SET config = $3::text::jsonb, visibility_artifacts = $4, visibility_wiki = $4,
			config_version = config_version + 1 WHERE project_id = $1::uuid AND key = $2`, wellknown.ProjectID, key, cfg, level)
		return
	}
	st.Exec(`WITH r AS (
			INSERT INTO repositories (key, class, format, config, project_id, visibility_artifacts, visibility_wiki)
			VALUES ($2, 'remote', 'npm', $3::text::jsonb, $1::uuid, $4, $4) RETURNING id)
		INSERT INTO upstream_repository (repository_id, kind) SELECT id, $5 FROM r`, wellknown.ProjectID, key, cfg, level, kind)
}

// TestR_CUR_005_TheRowsOfTheProjectUpstream holds the column "On a repository of the project upstream" of R-CUR-005 for the rules'
// operations on upstream/npmjs (a preset, level internal): the rules are read with repositories:read-config or the role admin, written and
// removed by the role admin only (such a repository has no members, so the roles of web give nothing), checked by every signed-in reader.
// A system repository's rules are the product's: the role admin's write and removal on upstream/scanner-trivy answer 409
// managed_by_product (R-REPO-070) and leave its rules as they were. Data: rule rows, no artifact; one replica, about 3 s.
func TestR_CUR_005_TheRowsOfTheProjectUpstream(t *testing.T) {
	e := newRulesEnv(t)
	const npmjs, trivy = "upstream/npmjs", "upstream/scanner-trivy"
	rulesWellKnown(e.st, "npmjs", string(wellknown.KindPreset), `{"remote":{"url":"https://registry.example.org/"}}`, "internal")
	rulesWellKnown(e.st, wellknown.ScannerTrivyKey, string(wellknown.KindSystem), `{"remote":{"url":"https://registry.example.org/"}}`, "private")
	r := e.st.Start(t, testenv.ReplicaOptions{Name: "web1", Env: []string{"QUADER_API_RATE_LIMITS_ENABLED=false"}})
	do := func(who, method, path string, body any) testenv.Answer {
		t.Helper()
		return e.st.Do(r, method, path, e.tok[who], body)
	}
	target := e.st.MustPutDeliveryRule(r, e.tok["admin"], npmjs, testenv.PackageRule("target", "block"))
	type row struct{ list, put, del, check int }
	for who, want := range map[string]row{
		// A member of web reads upstream/npmjs by its level and holds no manage on it.
		"reader":     {http.StatusForbidden, http.StatusForbidden, http.StatusForbidden, http.StatusOK},
		"maintainer": {http.StatusForbidden, http.StatusForbidden, http.StatusForbidden, http.StatusOK},
		"owner":      {http.StatusForbidden, http.StatusForbidden, http.StatusForbidden, http.StatusOK},
		"admin":      {http.StatusOK, http.StatusCreated, http.StatusNoContent, http.StatusOK},
		// repositories:read-config reads the rules and writes none; repositories:write-config sets the mode, not the rules.
		"auditor":   {http.StatusOK, http.StatusForbidden, http.StatusForbidden, http.StatusOK},
		"repoadmin": {http.StatusOK, http.StatusForbidden, http.StatusForbidden, http.StatusOK},
		"":          {http.StatusUnauthorized, http.StatusUnauthorized, http.StatusUnauthorized, http.StatusUnauthorized},
	} {
		t.Run("R-CUR-005 upstream "+map[bool]string{true: "anonymous", false: who}[who == ""], func(t *testing.T) {
			if a := do(who, http.MethodGet, rulesPath(npmjs), nil); a.Status != want.list {
				t.Errorf("list: %d %s, want %d", a.Status, a.Body, want.list)
			}
			put := do(who, http.MethodPost, rulesPath(npmjs), testenv.PackageRule("by-"+who, "allow"))
			if put.Status != want.put {
				t.Errorf("write: %d %s, want %d", put.Status, put.Body, want.put)
			}
			id := target
			if put.Status == http.StatusCreated {
				id, _ = put.JSON(t)["id"].(string)
			}
			if a := do(who, http.MethodDelete, rulesPath(npmjs)+"/"+id, nil); a.Status != want.del {
				t.Errorf("remove: %d %s, want %d", a.Status, a.Body, want.del)
			}
			if a := do(who, http.MethodPost, rulesPath(npmjs)+"/check", map[string]any{"name": "left-pad", "version": "1.3.0"}); a.Status != want.check {
				t.Errorf("check: %d %s, want %d", a.Status, a.Body, want.check)
			}
		})
	}
	if n := e.st.Number(`SELECT count(*) FROM remote_delivery_rules d JOIN repositories r ON r.id = d.repo_id WHERE r.key = 'npmjs'`); n != 1 {
		t.Errorf("%d rules remain on upstream/npmjs, want the target alone", n)
	}

	t.Run("R-CUR-005 R-REPO-070 a rule's write and removal on a system repository answer 409 managed_by_product", func(t *testing.T) {
		before := e.st.Number(`SELECT count(*) FROM remote_delivery_rules d JOIN repositories r ON r.id = d.repo_id WHERE r.key = $1`, wellknown.ScannerTrivyKey)
		if a := do("admin", http.MethodPost, rulesPath(trivy), testenv.PackageRule("aquasec/trivy-db", "block")); a.Status != http.StatusConflict || a.Code(t) != errs.CodeManagedByProduct {
			t.Errorf("write: %d %s, want 409 managed_by_product", a.Status, a.Body)
		}
		if a := do("admin", http.MethodDelete, rulesPath(trivy)+"/"+target, nil); a.Status != http.StatusConflict || a.Code(t) != errs.CodeManagedByProduct {
			t.Errorf("remove: %d %s, want 409 managed_by_product", a.Status, a.Body)
		}
		if a := do("auditor", http.MethodGet, rulesPath(trivy), nil); a.Status != http.StatusOK {
			t.Errorf("list by repositories:read-config: %d %s, want 200", a.Status, a.Body)
		}
		after := e.st.Number(`SELECT count(*) FROM remote_delivery_rules d JOIN repositories r ON r.id = d.repo_id WHERE r.key = $1`, wellknown.ScannerTrivyKey)
		if after != before {
			t.Errorf("the system repository's rules went from %d to %d", before, after)
		}
	})
}
