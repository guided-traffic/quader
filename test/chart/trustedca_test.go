//go:build chart

package chart_test

import (
	"strings"
	"testing"
)

// trustedCAPods are the pod specs of the release that read the shared configuration file, by name: the three roles, the
// migration Job, the backup CronJob, the restore Job and the post-restore Job. Each needs the bundle, because the paths of the
// bundle's files are in that file (R-HA-043).
func trustedCAPods(t *testing.T, sets ...string) map[string]any {
	t.Helper()
	pods := map[string]any{}
	_, docs := render(t, required(), append(backupSets(), sets...)...)
	for name, pod := range podSpecs(docs) {
		if name == "quader-vuln-scanner-trivy" || name == "quader-malware-scanner-clamav" || name == "quader-malware-updater-freshclam" {
			// The kinds of the role scanner open no connection that they would verify with the bundle and mount none (R-HA-065, R-HA-068, R-HA-069).
			if volumeNamed(pod, "secret-trusted-ca") != nil {
				t.Errorf("%s mounts the trusted CA bundle", name)
			}
			continue
		}
		pods[name] = pod
	}
	pods["quader-backup"] = cronJobPod(docs)
	_, rdocs := render(t, required(), append(restoreSets(), sets...)...)
	for name, pod := range podSpecs(rdocs) {
		if strings.HasPrefix(name, "quader-restore") {
			pods[name] = pod
		}
	}
	_, pdocs := render(t, required(), append(postRestoreSets(), sets...)...)
	pods["post-restore"] = postRestorePod(pdocs)
	return pods
}

// TestR_HA_043_TrustedCA (R-HA-043, R-REPO-035, R-HA-065): the chart mounts the CA bundle of a Secret or a ConfigMap at
// /etc/quader/ca/ in every pod of the release but the scanner's, lists its files in proxy.outbound.tls.ca_files, and keeps PostgreSQL's CA apart from it.
func TestR_HA_043_TrustedCA(t *testing.T) {
	t.Run("R-HA-043 without a source nothing is mounted and the configuration names no bundle", func(t *testing.T) {
		_, docs := render(t, required())
		for name, pod := range podSpecs(docs) {
			if volumeNamed(pod, "secret-trusted-ca") != nil {
				t.Errorf("%s mounts a trusted CA bundle without trustedCA.existingSecret or trustedCA.existingConfigMap", name)
			}
		}
		if at(configOf(t, docs), "proxy", "outbound", "tls", "ca_files") != nil {
			t.Error("proxy.outbound.tls.ca_files is set without a trusted CA bundle")
		}
	})

	t.Run("R-HA-043 a ConfigMap source (a trust-manager Bundle target) is mounted without subPath in every pod, items only", func(t *testing.T) {
		pods := trustedCAPods(t, "trustedCA.existingConfigMap=corp-ca-bundle")
		if len(pods) < 7 {
			t.Fatalf("only %d pods checked: %v", len(pods), keysOf(pods))
		}
		for name, pod := range pods {
			vol := volumeNamed(pod, "secret-trusted-ca")
			if vol == nil {
				t.Errorf("%s does not mount the trusted CA bundle", name)
				continue
			}
			if got := str(vol, "configMap", "name"); got != "corp-ca-bundle" {
				t.Errorf("%s: the volume names the ConfigMap %q, want corp-ca-bundle", name, got)
			}
			if at(vol, "secret") != nil {
				t.Errorf("%s: the volume of a ConfigMap source is also a Secret volume", name)
			}
			if got := str(vol, "configMap", "items", "0", "key"); got != "ca-bundle.crt" {
				t.Errorf("%s: the projected key is %q, want the default ca-bundle.crt", name, got)
			}
			m := mountOf(pod, vol)
			if m == nil || str(m, "mountPath") != "/etc/quader/ca" || at(m, "readOnly") != true || at(m, "subPath") != nil {
				t.Errorf("%s: mount = %v, want a read-only mount at /etc/quader/ca without subPath (a rotated source must reach the pod)", name, m)
			}
		}
	})

	t.Run("R-HA-043 a Secret source is mounted the same way", func(t *testing.T) {
		pods := trustedCAPods(t, "trustedCA.existingSecret=corp-ca")
		for name, pod := range pods {
			vol := volumeNamed(pod, "secret-trusted-ca")
			if vol == nil || str(vol, "secret", "secretName") != "corp-ca" || at(vol, "configMap") != nil {
				t.Errorf("%s: volume = %v, want a projection of the Secret corp-ca", name, vol)
			}
		}
	})

	t.Run("R-HA-043 the files of keys go into proxy.outbound.tls.ca_files after those of config, and use_system_roots stays as it is", func(t *testing.T) {
		_, docs := render(t, required(), "trustedCA.existingConfigMap=corp-ca-bundle",
			"trustedCA.keys={root.pem,intermediate.pem}", "config.proxy.outbound.tls.ca_files={/etc/mine/extra.pem}")
		cfg := configOf(t, docs)
		var got []string
		for _, f := range list(cfg, "proxy", "outbound", "tls", "ca_files") {
			got = append(got, f.(string))
		}
		if want := "/etc/mine/extra.pem,/etc/quader/ca/root.pem,/etc/quader/ca/intermediate.pem"; strings.Join(got, ",") != want {
			t.Errorf("ca_files = %v, want %s", got, want)
		}
		if at(cfg, "proxy", "outbound", "tls", "use_system_roots") != nil {
			t.Error("the chart sets use_system_roots; the bundle is added to the system roots, so it must leave the key alone")
		}
		if at(cfg, "storage", "connections", "0", "ca_file") != nil {
			t.Error("the ca_file of a connection is set without trustedCA.useForObjectStore")
		}
	})

	t.Run("R-HA-043 PostgreSQL trusts its own CA only: the database CA path is the one of secrets.databaseCA and the bundle is not in it", func(t *testing.T) {
		_, docs := render(t, required(), "trustedCA.existingConfigMap=corp-ca-bundle", "secrets.databaseCA.existingSecret=quader-db-ca")
		cfg := configOf(t, docs)
		if got := str(cfg, "database", "ssl_root_cert_file"); got != "/var/run/secrets/quader/database-ca/ca.crt" {
			t.Errorf("database.ssl_root_cert_file = %q, want the file of secrets.databaseCA", got)
		}
		pod := at(find(docs, "Deployment", "quader-api"), "spec", "template", "spec")
		if str(volumeNamed(pod, "secret-database-ca"), "secret", "secretName") != "quader-db-ca" {
			t.Error("the database CA Secret is not mounted")
		}
		m := mountOf(pod, volumeNamed(pod, "secret-database-ca"))
		if str(m, "mountPath") != "/var/run/secrets/quader/database-ca" {
			t.Errorf("the database CA is mounted at %q, which is not its own directory", str(m, "mountPath"))
		}
	})

	t.Run("R-HA-043 R-HA-072 useForObjectStore makes the one file the ca_file of every connection without a ca of its own", func(t *testing.T) {
		_, docs := render(t, required(), "trustedCA.existingConfigMap=corp-ca-bundle", "trustedCA.useForObjectStore=true", "trustedCA.keys={s3-ca.pem}",
			"config.storage.connections[1].id=own-ca", "config.storage.connections[1].bucket=b2",
			"secrets.storageConnections[0].connectionId=own-ca", "secrets.storageConnections[0].ca.existingSecret=b2-ca")
		cfg := configOf(t, docs)
		if got := str(cfg, "storage", "connections", "0", "ca_file"); got != "/etc/quader/ca/s3-ca.pem" {
			t.Errorf("ca_file of default = %q, want /etc/quader/ca/s3-ca.pem", got)
		}
		if got := str(cfg, "storage", "connections", "1", "ca_file"); got != "/var/run/secrets/quader/s3-own-ca-ca/ca.crt" {
			t.Errorf("ca_file of own-ca = %q, want the file of its own ca", got)
		}
		renderFails(t, required(), []string{
			"trustedCA.existingConfigMap=corp-ca-bundle", "trustedCA.useForObjectStore=true", "trustedCA.keys={s3-ca.pem}",
			"config.storage.connections[0].ca_file=/x",
		}, "config.storage.connections[0].ca_file", "managed by the chart")
	})

	t.Run("R-HA-043 the values the bundle cannot serve are refused and name the value", func(t *testing.T) {
		renderFails(t, required(), []string{"trustedCA.existingSecret=a", "trustedCA.existingConfigMap=b"}, "trustedCA.existingSecret", "trustedCA.existingConfigMap")
		renderFails(t, required(), []string{"trustedCA.existingSecret=a", "trustedCA.keys=null"}, "trustedCA.keys")
		renderFails(t, required(), []string{"trustedCA.useForObjectStore=true"}, "trustedCA.useForObjectStore", "trustedCA.existingSecret")
		renderFails(t, required(), []string{"trustedCA.existingSecret=a", "trustedCA.useForObjectStore=true", "trustedCA.keys={a.pem,b.pem}"}, "exactly one file", "trustedCA.keys")
		renderFails(t, required(), []string{"trustedCA.existingSecret=a", "trustedCA.keys={../etc/passwd}"}, "/trustedCA/keys/0")
	})
}

func keysOf(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
