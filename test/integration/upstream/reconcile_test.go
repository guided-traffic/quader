//go:build integration

package upstream

import (
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/catalog"
)

// TestR_REPO_067_Reconcile runs the reconcile of `quader migrate` against the migration session of a test database (R-REPO-066,
// R-REPO-067, Q-REPO-374 to Q-REPO-377): a fresh database ends with the 14 repositories of the catalog, a second run writes nothing,
// and a changed, overridden, dropped and redeclared preset each get their step. No upstream is contacted; the data are rows.
func TestR_REPO_067_Reconcile(t *testing.T) {
	f := newFixture(t)

	t.Run("R-REPO-066 a fresh database ends with the twelve presets off and internal (Q-REPO-388) and the two system repositories", func(t *testing.T) {
		n := f.e.Number(`SELECT count(*) FROM repositories r JOIN project p ON p.id = r.project_id JOIN upstream_repository u ON u.repository_id = r.id
		                 WHERE p.key = 'upstream'`)
		if n != 14 {
			t.Fatalf("%d repositories", n)
		}
		if off := f.e.Number(`SELECT count(*) FROM repositories r JOIN upstream_repository u ON u.repository_id = r.id
		                      WHERE u.kind = 'preset' AND (r.config->>'disabled')::boolean AND u.first_switched_on_at IS NULL AND r.class = 'remote'
		                        AND r.visibility_artifacts = 'internal' AND r.visibility_wiki = 'internal'`); off != 12 {
			t.Fatalf("%d presets off, never switched on and internal", off)
		}
		if got := f.config("docker-hub", "{remote,url}"); got != "https://registry-1.docker.io/" {
			t.Errorf("docker-hub url %s", got)
		}
		if got := f.config("docker-hub", "{description}"); got != "Docker Hub" {
			t.Errorf("docker-hub description %s", got)
		}
		if f.events("repository.created", "npmjs") != 1 || f.detail("repository.created", "npmjs", "project") != "upstream" {
			t.Errorf("repository.created of npmjs: %d", f.events("repository.created", "npmjs"))
		}
		if actor := f.e.Text("SELECT actor_type || ':' || coalesce(actor_name, '') FROM audit_event WHERE action = 'repository.created' AND repository = 'upstream/npmjs'"); actor != "system:catalog" {
			t.Errorf("actor %s", actor)
		}
	})
	t.Run("R-REPO-070 scanner-trivy holds the image scanning's database and nothing else", func(t *testing.T) {
		if scope, ok := f.trivyScope("scanner-trivy"); !ok {
			t.Errorf("scanner-trivy delivers %s, want onlyAllowed with the product's rule for aquasec/trivy-db alone", scope)
		}
		if f.config("scanner-trivy", "{disabled}") != "false" ||
			f.config("scanner-trivy", "{remote,url}") != "https://mirror.gcr.io/" || f.config("scanner-trivy", "{remote,cache,unusedRetentionHours}") != "48" ||
			f.config("scanner-trivy", "{vulnerabilities,enabled}") != "false" {
			t.Fatalf("scanner-trivy: %s", f.e.Text("SELECT r.config::text FROM repositories r JOIN upstream_repository u ON u.repository_id = r.id WHERE u.kind = 'system'"))
		}
		if lv := f.e.Text("SELECT visibility_artifacts || '/' || visibility_wiki FROM repositories WHERE id = $1", f.id("scanner-trivy")); lv != "private/private" {
			t.Errorf("levels %s", lv)
		}
	})
	t.Run("R-REPO-075 Q-REPO-411 Q-REPO-413 clamav is a local repository of the format clamav, private, its clients off", func(t *testing.T) {
		row := f.e.Text(`SELECT r.class || '/' || r.format || '/' || r.visibility_artifacts || '/' || r.visibility_wiki || '/' || u.kind || '/' || u.clients_enabled
		                 FROM repositories r JOIN upstream_repository u ON u.repository_id = r.id WHERE r.id = $1`, f.id("clamav"))
		if row != "local/clamav/private/private/system/false" {
			t.Fatalf("clamav %s", row)
		}
		if f.config("clamav", "{disabled}") != "false" || f.config("clamav", "{remote}") != "<none>" ||
			f.config("clamav", "{malware}") != "<none>" || f.config("clamav", "{description}") != "ClamAV definitions" {
			t.Fatalf("clamav: %s", f.e.Text("SELECT config::text FROM repositories WHERE id = $1", f.id("clamav")))
		}
	})
	t.Run("Q-REPO-379 the Debian and Ubuntu presets carry their archive keys", func(t *testing.T) {
		if fps := f.config("debian", "{deb,upstreamKeyFingerprints}"); !strings.Contains(fps, "04B54C3CDCA79751B16BC6B5225629DF75B188BD") {
			t.Errorf("debian keys %s", fps)
		}
		if fps := f.config("ubuntu-ports", "{deb,upstreamKeyFingerprints}"); !strings.Contains(fps, "F6ECB3762474EDA9D21B7022871920D1991BC93C") {
			t.Errorf("ubuntu-ports keys %s", fps)
		}
		// Q-REPO-301: a remote Debian repository is a member of a virtual repository only with deb.upstreamKeys, the armored keys
		// the member check reads; every APT preset carries them.
		if n := f.e.Number(`SELECT count(*) FROM repositories r JOIN upstream_repository u ON u.repository_id = r.id
		                     WHERE r.format = 'deb' AND coalesce(r.config #>> '{deb,upstreamKeys}', '') NOT LIKE '-----BEGIN PGP PUBLIC KEY BLOCK-----%'`); n != 0 {
			t.Errorf("Q-REPO-301 %d APT presets without armored deb.upstreamKeys", n)
		}
	})
	t.Run("R-REPO-067 a second run against the same catalog writes nothing and records nothing", func(t *testing.T) {
		before := f.e.Number("SELECT count(*) FROM audit_event WHERE repository LIKE 'upstream/%'")
		versions := f.e.Number("SELECT sum(config_version) FROM repositories r JOIN upstream_repository u ON u.repository_id = r.id")
		if err := reconcile(t, f.e, nil); err != nil {
			t.Fatal(err)
		}
		if after := f.e.Number("SELECT count(*) FROM audit_event WHERE repository LIKE 'upstream/%'"); after != before {
			t.Fatalf("events %d, then %d", before, after)
		}
		if v := f.e.Number("SELECT sum(config_version) FROM repositories r JOIN upstream_repository u ON u.repository_id = r.id"); v != versions {
			t.Fatalf("versions %d, then %d", versions, v)
		}
	})

	// An account on npmjs (a reference, which needs no key ring) and an administrator's address on quay, as the operations store them.
	f.e.Exec(`UPDATE repositories SET config = jsonb_set(jsonb_set(jsonb_set(config, '{remote,authMode}', '"basic"'), '{remote,username}', '"pull"'),
	          '{remote,password}', '{"secretRef":{"env":"QUADER_CAC_NPM"}}') WHERE id = $1`, f.id("npmjs"))
	f.e.Exec(`INSERT INTO repository_secrets (repo_id, field, secret_ref) VALUES ($1, '/remote/password', '{"env":"QUADER_CAC_NPM"}')`, f.id("npmjs"))
	f.e.Exec(`UPDATE repositories SET config = jsonb_set(config, '{remote,url}', '"https://mirror.example.com/"') WHERE id = $1`, f.id("quay"))
	f.e.Exec("UPDATE upstream_repository SET address_overridden = true WHERE repository_id = $1", f.id("quay"))

	t.Run("R-REPO-067 step 2 the product's address follows the catalog; the account stays while its host stays", func(t *testing.T) {
		cat := catalogWith(t, func(_ catalog.ID, ps []catalog.UpstreamPreset) []catalog.UpstreamPreset {
			for i := range ps {
				switch ps[i].ID {
				case "npmjs":
					ps[i].URL = "https://registry.npmjs.org/v2/"
				case "quay":
					ps[i].URL = "https://quay.example.com/"
				}
			}
			return ps
		})
		if err := reconcile(t, f.e, cat); err != nil {
			t.Fatal(err)
		}
		if got := f.config("npmjs", "{remote,url}"); got != "https://registry.npmjs.org/v2/" {
			t.Fatalf("npmjs url %s", got)
		}
		if f.detail("repository.updated", "npmjs", "reason") != "catalog" || f.config("npmjs", "{remote,authMode}") != "basic" ||
			f.detail("repository.updated", "npmjs", "accountRemoved") != "<none>" {
			t.Errorf("reason %s, auth %s", f.detail("repository.updated", "npmjs", "reason"), f.config("npmjs", "{remote,authMode}"))
		}
		if got := f.config("quay", "{remote,url}"); got != "https://mirror.example.com/" {
			t.Errorf("Q-REPO-377 the administrator's address of quay was written over: %s", got)
		}
		if f.e.Number("SELECT count(*) FROM remote_cache_invalidations WHERE repo_id = $1", f.id("npmjs")) == 0 {
			t.Error("R-REPO-011 the move of the upstream invalidated nothing")
		}
	})
	t.Run("Q-REPO-377 step 2 an account goes when the product moves its host", func(t *testing.T) {
		cat := catalogWith(t, func(_ catalog.ID, ps []catalog.UpstreamPreset) []catalog.UpstreamPreset {
			for i := range ps {
				if ps[i].ID == "npmjs" {
					ps[i].URL = "https://npm.example.org/"
				}
			}
			return ps
		})
		if err := reconcile(t, f.e, cat); err != nil {
			t.Fatal(err)
		}
		if f.config("npmjs", "{remote,authMode}") != "none" || f.config("npmjs", "{remote,password}") != "<none>" && f.config("npmjs", "{remote,password}") != "" {
			t.Fatalf("the account stayed: %s %s", f.config("npmjs", "{remote,authMode}"), f.config("npmjs", "{remote,password}"))
		}
		if f.detail("repository.updated", "npmjs", "accountRemoved") != "true" ||
			f.e.Number("SELECT count(*) FROM upstream_repository WHERE repository_id = $1 AND account_removed_at IS NOT NULL", f.id("npmjs")) != 1 ||
			f.e.Number("SELECT count(*) FROM repository_secrets WHERE repo_id = $1", f.id("npmjs")) != 0 {
			t.Fatal("the removal of the account is not recorded")
		}
	})
	t.Run("R-REPO-067 step 3 the override flag goes once the product's address caught up with the administrator's", func(t *testing.T) {
		cat := catalogWith(t, func(_ catalog.ID, ps []catalog.UpstreamPreset) []catalog.UpstreamPreset {
			for i := range ps {
				if ps[i].ID == "quay" {
					ps[i].URL = "https://mirror.example.com/"
				}
			}
			return ps
		})
		if err := reconcile(t, f.e, cat); err != nil {
			t.Fatal(err)
		}
		if f.e.Number("SELECT count(*) FROM upstream_repository WHERE repository_id = $1 AND NOT address_overridden", f.id("quay")) != 1 {
			t.Fatal("quay still reads overridden")
		}
	})

	f.listVirtual("web", "galaxy-all", "ansible", "galaxy")
	t.Run("Q-REPO-376 step 5 a preset the catalog no longer declares is retired and keeps its rows", func(t *testing.T) {
		cat := catalogWith(t, func(id catalog.ID, ps []catalog.UpstreamPreset) []catalog.UpstreamPreset {
			if id == "ansible" {
				return nil
			}
			return ps
		})
		if err := reconcile(t, f.e, cat); err != nil {
			t.Fatal(err)
		}
		if f.e.Number("SELECT count(*) FROM upstream_repository WHERE repository_id = $1 AND retired_at IS NOT NULL", f.id("galaxy")) != 1 {
			t.Fatal("galaxy is not retired")
		}
		if f.detail("repository.disabled", "galaxy", "reason") != "retired" || f.detail("repository.disabled", "galaxy", "listedBy") != `["web/galaxy-all"]` {
			t.Errorf("repository.disabled %s %s", f.detail("repository.disabled", "galaxy", "reason"), f.detail("repository.disabled", "galaxy", "listedBy"))
		}
		if f.e.Number("SELECT count(*) FROM revocation_outbox WHERE event = 'repository.disabled'") == 0 {
			t.Error("R-AUTHZ-031 the retirement is not journaled")
		}
		if f.e.Number("SELECT count(*) FROM job WHERE kind = 'upstream-purge' AND target_key = 'upstream/galaxy'") != 1 {
			t.Error("no purge of galaxy was queued")
		}
		if f.e.Number("SELECT count(*) FROM virtual_members WHERE member_id = $1", f.id("galaxy")) != 1 {
			t.Error("the virtual repository of web no longer lists galaxy")
		}
	})
	t.Run("R-REPO-067 step 4 a retired preset declared again is no longer retired and stays off", func(t *testing.T) {
		if err := reconcile(t, f.e, nil); err != nil {
			t.Fatal(err)
		}
		if f.e.Number("SELECT count(*) FROM upstream_repository WHERE repository_id = $1 AND retired_at IS NULL", f.id("galaxy")) != 1 ||
			f.config("galaxy", "{disabled}") != "true" {
			t.Fatal("galaxy is still retired, or was switched on")
		}
	})
	t.Run("R-REPO-067 step 6 a declared key of another format fails the run and changes nothing", func(t *testing.T) {
		cat := catalogWith(t, func(id catalog.ID, ps []catalog.UpstreamPreset) []catalog.UpstreamPreset {
			switch id {
			case "npm":
				return nil
			case "oci":
				return append(ps, catalog.UpstreamPreset{ID: "npmjs", DisplayName: "npm as OCI", URL: "https://oci.example.org/"})
			}
			return ps
		})
		before := f.e.Number("SELECT count(*) FROM audit_event")
		err := reconcile(t, f.e, cat)
		if err == nil || !strings.Contains(err.Error(), "upstream/npmjs") {
			t.Fatalf("err = %v", err)
		}
		if f.e.Number("SELECT count(*) FROM audit_event") != before || f.config("npmjs", "{remote,url}") != "https://registry.npmjs.org/" {
			t.Fatal("the failed run changed something")
		}
	})
}
