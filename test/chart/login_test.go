//go:build chart

package chart_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The login secrets of the chart: secrets.breakGlass (R-AUTH-014) and secrets.oidc[] (R-AUTH-004),
// mounted into api pods only (R-HA-030, R-AUTH-042, Q-HA-79), as a directory so that a rotated Secret reaches the pod
// (Q-AUTH-14).

// loginSets is the minimum a login render needs: a provider of config.auth.oidc.providers for the
// secrets.oidc entry.
func loginSets(extra ...string) []string {
	return append([]string{
		"config.auth.oidc.providers.corp.issuer=https://idp.example.com",
		"config.auth.oidc.providers.corp.client_id=quader",
		"config.auth.oidc.providers.corp.display_name=Corp",
	}, extra...)
}

func volumeNamed(pod any, name string) any {
	for _, v := range list(pod, "volumes") {
		if str(v, "name") == name {
			return v
		}
	}
	return nil
}

func TestLoginSecrets(t *testing.T) {
	t.Run("R-HA-030 R-AUTH-014 secrets.breakGlass mounts the password into api pods as a directory and renders password_file", func(t *testing.T) {
		_, docs := render(t, required(), "secrets.breakGlass.existingSecret=quader-break-glass")
		cfg := configOf(t, docs)
		if got := str(cfg, "auth", "break_glass", "password_file"); got != "/var/run/secrets/quader/break-glass/password" {
			t.Errorf("auth.break_glass.password_file = %q", got)
		}
		if at(cfg, "auth", "break_glass", "enabled") != nil {
			t.Errorf("auth.break_glass.enabled = %v, want the quader default (not set by the chart)", at(cfg, "auth", "break_glass", "enabled"))
		}
		pod := at(find(docs, "Deployment", "quader-api"), "spec", "template", "spec")
		vol := secretVolume(pod, "quader-break-glass")
		if vol == nil {
			t.Fatal("quader-api does not mount quader-break-glass")
		}
		m := mountOf(pod, vol)
		if str(m, "mountPath") != "/var/run/secrets/quader/break-glass" || at(m, "readOnly") != true || at(m, "subPath") != nil {
			t.Errorf("mount = %v, want a read-only directory mount without subPath", m)
		}
		items := list(vol, "secret", "items")
		if len(items) != 1 || str(items[0], "key") != "password" || str(items[0], "path") != "password" {
			t.Errorf("items = %v", items)
		}
		if at(vol, "secret", "defaultMode") != 288 {
			t.Errorf("defaultMode = %v, want 0440", at(vol, "secret", "defaultMode"))
		}
	})

	t.Run("R-AUTH-042 the break-glass Secret is not mounted into worker pods or the migration Job", func(t *testing.T) {
		_, docs := render(t, required(), "secrets.breakGlass.existingSecret=quader-break-glass")
		for _, pod := range []any{
			at(find(docs, "Deployment", "quader-packages"), "spec", "template", "spec"),
			at(find(docs, "Deployment", "quader-worker"), "spec", "template", "spec"),
			at(find(docs, "Job", "quader-migrate"), "spec", "template", "spec"),
		} {
			if secretVolume(pod, "quader-break-glass") != nil || volumeNamed(pod, "secret-break-glass") != nil {
				t.Error("a pod that is not api mounts the break-glass Secret")
			}
		}
	})

	t.Run("R-HA-030 R-AUTH-042 every pod of role api mounts the login secrets, its pools and endpoint pools included, and no other pod does", func(t *testing.T) {
		_, docs := render(t, []string{filepath.Join(chartDir, "ci", "roles-pools.yaml")}, "secrets.breakGlass.existingSecret=quader-break-glass")
		api := 0
		for _, d := range ofKind(docs, "Deployment") {
			n := str(d, "metadata", "name")
			mounted := secretVolume(at(d, "spec", "template", "spec"), "quader-break-glass") != nil
			isAPI := n == "quader-api" || strings.HasPrefix(n, "quader-api-")
			if isAPI {
				api++
			}
			if mounted != isAPI {
				t.Errorf("%s mounts the break-glass Secret = %v, want %v (role api only)", n, mounted, isAPI)
			}
		}
		if api != 3 {
			t.Errorf("%d api Deployments in ci/roles-pools.yaml (main, path pool, endpoint pool), want 3", api)
		}
	})

	t.Run("R-HA-030 the bridge's web pods mount the login secrets, the packages pods of the same release do not", func(t *testing.T) {
		_, docs := render(t, []string{filepath.Join(chartDir, "ci", "bridge.yaml")}, "secrets.breakGlass.existingSecret=quader-break-glass")
		for name, want := range map[string]bool{"quader-web": true, "quader-web-ep-customers": true, "quader-api": true, "quader-packages": false, "quader-worker": false} {
			if got := secretVolume(at(find(docs, "Deployment", name), "spec", "template", "spec"), "quader-break-glass") != nil; got != want {
				t.Errorf("%s mounts the break-glass Secret = %v, want %v", name, got, want)
			}
		}
	})

	t.Run("R-HA-030 R-AUTH-014 without secrets.breakGlass nothing is mounted and the account is off", func(t *testing.T) {
		_, docs := render(t, required())
		cfg := configOf(t, docs)
		if at(cfg, "auth", "break_glass", "enabled") != false {
			t.Errorf("auth.break_glass.enabled = %v, want false", at(cfg, "auth", "break_glass", "enabled"))
		}
		if at(cfg, "auth", "break_glass", "password_file") != nil {
			t.Error("auth.break_glass.password_file rendered without secrets.breakGlass")
		}
		for _, role := range []string{"api", "packages", "worker"} {
			if volumeNamed(at(find(docs, "Deployment", "quader-"+role), "spec", "template", "spec"), "secret-break-glass") != nil {
				t.Errorf("quader-%s mounts a break-glass volume without secrets.breakGlass", role)
			}
		}
	})

	t.Run("R-HA-030 a different key of the break-glass Secret is projected as password", func(t *testing.T) {
		_, docs := render(t, required(), "secrets.breakGlass.existingSecret=bg", "secrets.breakGlass.key=hash")
		vol := secretVolume(at(find(docs, "Deployment", "quader-api"), "spec", "template", "spec"), "bg")
		items := list(vol, "secret", "items")
		if len(items) != 1 || str(items[0], "key") != "hash" || str(items[0], "path") != "password" {
			t.Errorf("items = %v", items)
		}
	})

	t.Run("R-HA-029 auth.break_glass.password_file is chart-managed", func(t *testing.T) {
		renderFails(t, required(), []string{"config.auth.break_glass.password_file=/x"}, "config.auth.break_glass.password_file", "managed")
	})

	t.Run("R-HA-030 R-AUTH-004 secrets.oidc mounts the client secret into api pods and renders client_secret_file of the provider", func(t *testing.T) {
		_, docs := render(t, required(), loginSets("secrets.oidc[0].providerId=corp", "secrets.oidc[0].existingSecret=quader-oidc-corp")...)
		cfg := configOf(t, docs)
		if got := str(cfg, "auth", "oidc", "providers", "corp", "client_secret_file"); got != "/var/run/secrets/quader/oidc-corp/client-secret" {
			t.Errorf("client_secret_file = %q", got)
		}
		if got := str(cfg, "auth", "oidc", "providers", "corp", "client_id"); got != "quader" {
			t.Errorf("the provider's own keys were lost: client_id = %q", got)
		}
		pod := at(find(docs, "Deployment", "quader-api"), "spec", "template", "spec")
		vol := secretVolume(pod, "quader-oidc-corp")
		if vol == nil {
			t.Fatal("quader-api does not mount quader-oidc-corp")
		}
		m := mountOf(pod, vol)
		if str(m, "mountPath") != "/var/run/secrets/quader/oidc-corp" || at(m, "readOnly") != true || at(m, "subPath") != nil {
			t.Errorf("mount = %v", m)
		}
		items := list(vol, "secret", "items")
		if len(items) != 1 || str(items[0], "key") != "client-secret" || str(items[0], "path") != "client-secret" {
			t.Errorf("items = %v", items)
		}
		for _, pod := range []any{
			at(find(docs, "Deployment", "quader-packages"), "spec", "template", "spec"),
			at(find(docs, "Deployment", "quader-worker"), "spec", "template", "spec"),
			at(find(docs, "Job", "quader-migrate"), "spec", "template", "spec"),
		} {
			if secretVolume(pod, "quader-oidc-corp") != nil {
				t.Error("R-AUTH-042: a pod that is not api mounts the client secret")
			}
		}
	})

	t.Run("R-HA-030 secrets.oidc takes a key and several providers", func(t *testing.T) {
		_, docs := render(t, required(), loginSets(
			"config.auth.oidc.providers.second.issuer=https://idp2.example.com",
			"config.auth.oidc.providers.second.client_id=quader",
			"config.auth.oidc.providers.second.display_name=Second",
			"secrets.oidc[0].providerId=corp", "secrets.oidc[0].existingSecret=a", "secrets.oidc[0].key=secret",
			"secrets.oidc[1].providerId=second", "secrets.oidc[1].existingSecret=b")...)
		cfg := configOf(t, docs)
		for id, path := range map[string]string{"corp": "oidc-corp", "second": "oidc-second"} {
			if got := str(cfg, "auth", "oidc", "providers", id, "client_secret_file"); got != "/var/run/secrets/quader/"+path+"/client-secret" {
				t.Errorf("provider %s client_secret_file = %q", id, got)
			}
		}
		pod := at(find(docs, "Deployment", "quader-api"), "spec", "template", "spec")
		if got := str(list(secretVolume(pod, "a"), "secret", "items")[0], "key"); got != "secret" {
			t.Errorf("key of provider corp = %q", got)
		}
	})

	t.Run("R-HA-029 secrets.oidc is validated: provider ID, Secret, a provider to bind and the managed key", func(t *testing.T) {
		renderFails(t, required(), loginSets("secrets.oidc[0].providerId=Corp", "secrets.oidc[0].existingSecret=x"), "providerId")
		renderFails(t, required(), loginSets("secrets.oidc[0].providerId=corp"), "existingSecret")
		renderFails(t, required(), loginSets("secrets.oidc[0].providerId=other", "secrets.oidc[0].existingSecret=x"), "secrets.oidc[0].providerId", "config.auth.oidc.providers")
		renderFails(t, required(), loginSets("secrets.oidc[0].providerId=corp", "secrets.oidc[0].existingSecret=x",
			"secrets.oidc[1].providerId=corp", "secrets.oidc[1].existingSecret=y"), "two entries", "corp")
		renderFails(t, required(), loginSets("secrets.oidc[0].providerId=corp", "secrets.oidc[0].existingSecret=x",
			"config.auth.oidc.providers.corp.client_secret_file=/x"), "config.auth.oidc.providers.corp.client_secret_file", "managed")
	})

	t.Run("R-HA-030 without secrets.oidc no client secret is rendered", func(t *testing.T) {
		_, docs := render(t, required(), loginSets("config.auth.oidc.providers.corp.client_auth_method=none")...)
		if at(configOf(t, docs), "auth", "oidc", "providers", "corp", "client_secret_file") != nil {
			t.Error("client_secret_file rendered without secrets.oidc")
		}
	})

	t.Run("R-HA-030 a provider with a client secret and no entry in secrets.oidc is refused, whatever the global extra mounts hold", func(t *testing.T) {
		renderFails(t, required(), loginSets(), "config.auth.oidc.providers.corp", "client_secret_basic", "secrets.oidc")
		renderFails(t, required(), loginSets("config.auth.oidc.providers.corp.client_auth_method=client_secret_post",
			"extraEnv[0].name=QUADER_CORP", "extraEnv[0].value=x"), "config.auth.oidc.providers.corp", "client_secret_post", "extraEnv")
		render(t, required(), loginSets("secrets.oidc[0].providerId=corp", "secrets.oidc[0].existingSecret=x")...)
	})

	t.Run("R-OPS-028 the rendered login configuration passes the binary", func(t *testing.T) {
		bin := quader(t)
		_, docs := render(t, required(), loginSets("secrets.oidc[0].providerId=corp", "secrets.oidc[0].existingSecret=x", "secrets.breakGlass.existingSecret=bg")...)
		cfgPath := filepath.Join(t.TempDir(), "quader.yaml")
		if err := os.WriteFile(cfgPath, []byte(str(find(docs, "ConfigMap", "quader-config"), "data", "quader.yaml")), 0o600); err != nil {
			t.Fatal(err)
		}
		pod := at(find(docs, "Deployment", "quader-api"), "spec", "template", "spec")
		cmd := exec.Command(bin, "admin", "config", "validate", "--config", cfgPath, "--format", "json")
		cmd.Env = podEnviron(pod)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("quader admin config validate: %v\n%s", err, out)
		}
	})
}
