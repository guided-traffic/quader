//go:build integration

package upstream

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/repository"
)

// TestR_REPO_077_AnAccountTheConfigurationDeclares: quader migrate applies repositories.upstream_accounts (the stored account goes, the
// declaration is marked and recorded with the actor system:configuration, a second run writes nothing, a key of no remote repository
// fails the run before any write, a declaration that is gone is recorded and cleared); the item shows the account read-only with the
// state of its files on the replica that answers, never a value; the account's operations and an address that moves its hosts answer
// 409 managed_by_config. That the replica sends the account read from the files is the unit tier's and the proxy area's
// TestHandshake's. Integration tier, the area's stack, bytes of data.
func TestR_REPO_077_AnAccountTheConfigurationDeclares(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	write := func(name, v string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("username", "dummy-user-4e\n")
	write("password", "dummy-token-4e\n")
	declared := map[string]config.UpstreamAccount{"docker-hub": {
		UsernameFile: filepath.Join(dir, "username"), PasswordFile: filepath.Join(dir, "password"), SecretName: "quader-upstream-docker-hub",
	}}
	// An administrator's account, stored before the declaration as the operations store it.
	f.e.Exec(`UPDATE repositories SET config = jsonb_set(jsonb_set(jsonb_set(config, '{remote,authMode}', '"basic"'), '{remote,username}', '"robot"'),
	          '{remote,password}', '{"secretRef":{"env":"QUADER_CAC_HUB"}}') WHERE id = $1`, f.id("docker-hub"))
	f.e.Exec(`INSERT INTO repository_secrets (repo_id, field, secret_ref) VALUES ($1, '/remote/password', '{"env":"QUADER_CAC_HUB"}')`, f.id("docker-hub"))
	updated := func() int64 { return f.events("repository.updated", "docker-hub") }
	marked := func() int64 {
		return f.e.Number("SELECT count(*) FROM upstream_repository WHERE repository_id = $1 AND account_configured_at IS NOT NULL", f.id("docker-hub"))
	}

	t.Run("the apply removes the stored account, marks the declaration and records it without a value; a second run writes nothing", func(t *testing.T) {
		before := updated()
		if err := migrateWith(t, f.e, nil, declared); err != nil {
			t.Fatal(err)
		}
		if f.config("docker-hub", "{remote,authMode}") != "none" || f.config("docker-hub", "{remote,username}") != "" ||
			f.e.Number("SELECT count(*) FROM repository_secrets WHERE repo_id = $1", f.id("docker-hub")) != 0 || marked() != 1 {
			t.Fatalf("authMode %s, username %q, mark %d", f.config("docker-hub", "{remote,authMode}"), f.config("docker-hub", "{remote,username}"), marked())
		}
		if updated() != before+1 || f.detail("repository.updated", "docker-hub", "reason") != "configuration" ||
			f.detail("repository.updated", "docker-hub", "account") != `{"source": "configuration", "authMode": "basic"}` {
			t.Fatalf("%d events, reason %s, account %s", updated()-before, f.detail("repository.updated", "docker-hub", "reason"),
				f.detail("repository.updated", "docker-hub", "account"))
		}
		if actor := f.e.Text(`SELECT actor_type || ':' || coalesce(actor_name, '') FROM audit_event WHERE action = 'repository.updated'
		                      AND repository = 'upstream/docker-hub' ORDER BY occurred_at DESC, id DESC LIMIT 1`); actor != "system:configuration" {
			t.Errorf("actor %s", actor)
		}
		if n := f.e.Number(`SELECT count(*) FROM audit_event WHERE details::text LIKE '%dummy-%'`); n != 0 {
			t.Fatalf("%d audit events hold a value of the files", n)
		}
		if err := migrateWith(t, f.e, nil, declared); err != nil || updated() != before+1 {
			t.Fatalf("a second run: %v, %d events", err, updated()-before)
		}
	})

	t.Run("a key that names no remote repository of the project fails the run with the key named and writes nothing", func(t *testing.T) {
		before := updated()
		for _, key := range []string{"clamav", "docker-hbu"} {
			bad := map[string]config.UpstreamAccount{"docker-hub": declared["docker-hub"], key: {PasswordFile: "/p", UsernameFile: "/u"}}
			err := migrateWith(t, f.e, nil, bad)
			if err == nil || !strings.Contains(err.Error(), "repositories.upstream_accounts."+key) {
				t.Errorf("%s: %v", key, err)
			}
		}
		if updated() != before {
			t.Fatal("a refused run wrote")
		}
	})

	f.accounts = repository.ConfiguredAccounts{"docker-hub": {
		AuthMode: repository.AuthBasic, UsernameFile: declared["docker-hub"].UsernameFile, PasswordFile: declared["docker-hub"].PasswordFile,
		SecretName: "quader-upstream-docker-hub",
	}}
	f.start()

	t.Run("R-REPO-073 a registry whose recent downloads all completed answers its last download and no failure", func(t *testing.T) {
		// A row is one download, its unit's key the one the tracker writes for an image (R-OPS-049, migration 00140).
		f.e.Exec(`INSERT INTO upstream_download_finished (id, repo_id, name, version, path, bytes, started_at, finished_at, duration_ms, outcome,
		          origin, replica_name, unit, parts) VALUES (gen_random_uuid(), $1, 'library/alpine', '3.19', 'library/alpine/_manifests/latest', 512,
		          now() - interval '2 seconds', now() - interval '1 second', 1000, 'completed', 'client', 'quader-packages-0',
		          'p|library/alpine/_manifests/latest', 1)`, f.id("docker-hub"))
		defer f.e.Exec(`DELETE FROM upstream_download_finished WHERE repo_id = $1`, f.id("docker-hub"))
		got := f.do(f.admin, "GET", items+"/docker-hub", nil)
		wantStatus(t, got, 200, "")
		if acct := got.JSON(t)["account"].(map[string]any); acct["lastDownloadAt"] == nil || acct["lastFailure"] != nil {
			t.Fatalf("%v", acct)
		}
	})

	t.Run("the item shows the declaration read-only with the files' state on the answering replica, and the writes are refused", func(t *testing.T) {
		got := f.do(f.admin, "GET", items+"/docker-hub", nil)
		wantStatus(t, got, 200, "")
		acct := got.JSON(t)["account"].(map[string]any)
		conf, _ := acct["configuration"].(map[string]any)
		if acct["source"] != "configuration" || acct["authMode"] != "basic" || acct["username"] != nil || acct["password"] != nil ||
			conf["secretName"] != "quader-upstream-docker-hub" || conf["files"] != "readable" || conf["since"] == nil {
			t.Fatalf("%v", acct)
		}
		if strings.Contains(string(got.Body), "dummy-") {
			t.Fatal("the answer holds a value of the files")
		}
		if got.JSON(t)["accountRaisesLimit"] != false {
			t.Errorf("accountRaisesLimit %v for a repository that sends an account", got.JSON(t)["accountRaisesLimit"])
		}
		write("password", "")
		if conf := f.do(f.admin, "GET", items+"/docker-hub", nil).JSON(t)["account"].(map[string]any)["configuration"].(map[string]any); conf["files"] != "empty" {
			t.Errorf("an empty file reads %v", conf["files"])
		}
		write("password", "dummy-token-4e\n")

		tag := got.Header.Get("ETag")
		wantStatus(t, f.do(f.admin, "PUT", items+"/docker-hub/account", map[string]any{"authMode": "basic", "username": "u", "password": map[string]any{"value": "v"}}, "If-Match", tag), 409, "managed_by_config")
		wantStatus(t, f.do(f.admin, "DELETE", items+"/docker-hub/account", nil, "If-Match", tag), 409, "managed_by_config")
		wantStatus(t, f.do(f.admin, "PUT", items+"/docker-hub/address", map[string]any{"url": "https://mirror.example.com/"}, "If-Match", tag), 409, "managed_by_config")
		wantStatus(t, f.do(f.admin, "PUT", items+"/docker-hub/address", map[string]any{"account": nil}, "If-Match", tag), 409, "managed_by_config")
		// A preset whose vendor documents no higher limit with an account never offers one (Q-UI-1160).
		if quay := f.do(f.admin, "GET", items+"/quay", nil).JSON(t); quay["accountRaisesLimit"] != false {
			t.Errorf("quay: accountRaisesLimit %v", quay["accountRaisesLimit"])
		}
	})

	t.Run("a declaration that is gone is recorded and its mark cleared; the account is the administrator's again", func(t *testing.T) {
		before := updated()
		if err := migrateWith(t, f.e, nil, nil); err != nil {
			t.Fatal(err)
		}
		if marked() != 0 || updated() != before+1 || f.detail("repository.updated", "docker-hub", "reason") != "configuration_removed" ||
			f.detail("repository.updated", "docker-hub", "account") != `{"source": "configuration", "authMode": "none"}` {
			t.Fatalf("mark %d, %d events, reason %s", marked(), updated()-before, f.detail("repository.updated", "docker-hub", "reason"))
		}
	})
}
