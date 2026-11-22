//go:build chart

package chart_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The object-store connections in the chart (R-HA-072, storage.md R-STOR-050): the list config.storage.connections, the entries of
// secrets.storageConnections, the files they mount and the keys they set, and the refusals. Render tests only.

func connectionsSet() []string { return []string{filepath.Join(chartDir, "ci", "connections.yaml")} }

// storageVolume returns the volume of the pod spec with the name, or nil.
func storageVolume(pod any, name string) any {
	for _, v := range list(pod, "volumes") {
		if str(v, "name") == name {
			return v
		}
	}
	return nil
}

// storageVolumes lists the names of the pod's volumes of the object-store connections (secret-s3-*), sorted.
func storageVolumes(pod any) []string {
	var out []string
	for _, v := range list(pod, "volumes") {
		if strings.HasPrefix(str(v, "name"), "secret-s3-") {
			out = append(out, str(v, "name"))
		}
	}
	sort.Strings(out)
	return out
}

// itemsOf maps the projected keys of a Secret volume to their file names.
func itemsOf(v any) map[string]string {
	out := map[string]string{}
	for _, it := range list(v, "secret", "items") {
		out[str(it, "key")] = str(it, "path")
	}
	return out
}

func connectionOf(t *testing.T, docs []doc, i int) map[string]any {
	t.Helper()
	l := list(configOf(t, docs), "storage", "connections")
	if i >= len(l) {
		t.Fatalf("storage.connections has %d elements, want element %d", len(l), i)
	}
	return l[i].(map[string]any)
}

func TestR_HA_072_OneConnection(t *testing.T) {
	t.Run("one element with a bucket renders that element with its id, and no pod mounts a Secret of the object store", func(t *testing.T) {
		_, docs := render(t, required())
		l := list(configOf(t, docs), "storage", "connections")
		if len(l) != 1 || str(l[0], "id") != "default" || str(l[0], "bucket") != "quader-blobs" || len(l[0].(map[string]any)) != 2 {
			t.Errorf("storage.connections = %v, want [{id: default, bucket: quader-blobs}]", l)
		}
		if at(configOf(t, docs), "storage", "s3") != nil {
			t.Error("storage.s3 is rendered")
		}
		for name, pod := range podSpecs(docs) {
			if v := storageVolumes(pod); len(v) != 0 {
				t.Errorf("%s mounts %v", name, v)
			}
			for _, e := range list(pod, "containers", "0", "env") {
				if strings.HasPrefix(str(e, "name"), "QUADER_STORAGE_S3") || at(e, "valueFrom", "secretKeyRef") != nil {
					t.Errorf("%s sets %s", name, str(e, "name"))
				}
			}
		}
	})

	t.Run("static keys: api, packages and worker mount s3-default with the two keys, the element names both files; the backup pod mounts the read login instead", func(t *testing.T) {
		_, docs := render(t, required(), staticTarget()...)
		for _, role := range []string{"api", "packages", "worker"} {
			pod := at(find(docs, "Deployment", "quader-"+role), "spec", "template", "spec")
			v := storageVolume(pod, "secret-s3-default")
			if str(v, "secret", "secretName") != "quader-s3" {
				t.Fatalf("quader-%s: volume secret-s3-default = %v", role, v)
			}
			if got := itemsOf(v); len(got) != 2 || got["access-key-id"] != "access-key-id" || got["secret-access-key"] != "secret-access-key" {
				t.Errorf("quader-%s: items = %v", role, got)
			}
			m := mountOf(pod, v)
			if str(m, "mountPath") != "/var/run/secrets/quader/s3-default" || at(m, "readOnly") != true || at(m, "subPath") != nil || at(v, "secret", "defaultMode") != 288 {
				t.Errorf("quader-%s: mount %v, mode %v", role, m, at(v, "secret", "defaultMode"))
			}
			if got := storageVolumes(pod); len(got) != 1 {
				t.Errorf("quader-%s mounts %v, want the credential only", role, got)
			}
		}
		c := connectionOf(t, docs, 0)
		if c["access_key_id_file"] != "/var/run/secrets/quader/s3-default/access-key-id" || c["secret_access_key_file"] != "/var/run/secrets/quader/s3-default/secret-access-key" {
			t.Errorf("element = %v", c)
		}
		if _, ok := c["session_token_file"]; ok {
			t.Error("session_token_file without sessionTokenKey")
		}
		backup := cronJobPod(docs)
		if got := storageVolumes(backup); strings.Join(got, ",") != "secret-s3-default-backup" {
			t.Errorf("the backup pod mounts %v, want only secret-s3-default-backup", got)
		}
		if got := itemsOf(storageVolume(backup, "secret-s3-default-backup")); len(got) != 2 || got["access-key-id"] != "access-key-id" {
			t.Errorf("backup login items = %v", got)
		}
		if got := storageVolumes(podSpecs(docs)["quader-migrate"]); len(got) != 0 {
			t.Errorf("the migration Job mounts %v", got)
		}
	})

	t.Run("a session token key and other key names are projected under the chart's file names", func(t *testing.T) {
		_, docs := render(t, required(), "config.storage.connections[0].credentials_source=static", "secrets.storageConnections[0].connectionId=default",
			"secrets.storageConnections[0].existingSecret=k", "secrets.storageConnections[0].accessKeyIdKey=AK", "secrets.storageConnections[0].secretAccessKeyKey=SK",
			"secrets.storageConnections[0].sessionTokenKey=ST")
		got := itemsOf(storageVolume(at(find(docs, "Deployment", "quader-worker"), "spec", "template", "spec"), "secret-s3-default"))
		if len(got) != 3 || got["AK"] != "access-key-id" || got["SK"] != "secret-access-key" || got["ST"] != "session-token" {
			t.Errorf("items = %v", got)
		}
		if connectionOf(t, docs, 0)["session_token_file"] != "/var/run/secrets/quader/s3-default/session-token" {
			t.Errorf("element = %v", connectionOf(t, docs, 0))
		}
	})

	t.Run("a ca of the entry is mounted as ca.crt in the roles and the backup pod and renders the element's ca_file", func(t *testing.T) {
		_, docs := render(t, required(), append(backupSets(), "secrets.storageConnections[0].connectionId=default",
			"secrets.storageConnections[0].ca.existingSecret=s3-ca", "secrets.storageConnections[0].ca.key=root.pem")...)
		if got := connectionOf(t, docs, 0)["ca_file"]; got != "/var/run/secrets/quader/s3-default-ca/ca.crt" {
			t.Errorf("ca_file = %v", got)
		}
		for _, pod := range []any{at(find(docs, "Deployment", "quader-api"), "spec", "template", "spec"), cronJobPod(docs)} {
			v := storageVolume(pod, "secret-s3-default-ca")
			if got := itemsOf(v); len(got) != 1 || got["root.pem"] != "ca.crt" || str(mountOf(pod, v), "mountPath") != "/var/run/secrets/quader/s3-default-ca" {
				t.Errorf("ca volume = %v", v)
			}
		}
	})
}

func TestR_HA_072_SeveralConnectionsAndAProvisioner(t *testing.T) {
	_, docs := render(t, connectionsSet())

	t.Run("a provisioner's Secret renders bucket_file, region_file and endpoint_file and leaves the plain keys out", func(t *testing.T) {
		c := connectionOf(t, docs, 0)
		for k, want := range map[string]string{
			"id": "default", "bucket_file": "/var/run/secrets/quader/s3-default-location/bucket", "region_file": "/var/run/secrets/quader/s3-default-location/region",
			"endpoint_file": "/var/run/secrets/quader/s3-default-location/endpoint", "access_key_id_file": "/var/run/secrets/quader/s3-default/access-key-id",
		} {
			if c[k] != want {
				t.Errorf("%s = %v, want %s", k, c[k], want)
			}
		}
		for _, k := range []string{"bucket", "region", "endpoint", "ca_file"} {
			if _, ok := c[k]; ok {
				t.Errorf("element default has %s", k)
			}
		}
		s := connectionOf(t, docs, 1)
		if s["id"] != "secondary" || s["bucket"] != "quader-blobs-secondary" || s["ca_file"] != "/var/run/secrets/quader/s3-secondary-ca/ca.crt" ||
			s["access_key_id_file"] != "/var/run/secrets/quader/s3-secondary/access-key-id" {
			t.Errorf("element secondary = %v", s)
		}
	})

	t.Run("the pods that write mount the credentials of both, every pod that reads the location and the CA; the backup pod no runtime key", func(t *testing.T) {
		for _, role := range []string{"api", "packages", "worker"} {
			pod := at(find(docs, "Deployment", "quader-"+role), "spec", "template", "spec")
			if got := strings.Join(storageVolumes(pod), ","); got != "secret-s3-default,secret-s3-default-location,secret-s3-secondary,secret-s3-secondary-ca" {
				t.Errorf("quader-%s mounts %s", role, got)
			}
			loc := itemsOf(storageVolume(pod, "secret-s3-default-location"))
			if len(loc) != 3 || loc["S3_BUCKET"] != "bucket" || loc["S3_REGION"] != "region" || loc["S3_ENDPOINT"] != "endpoint" {
				t.Errorf("quader-%s: location items = %v", role, loc)
			}
			cred := itemsOf(storageVolume(pod, "secret-s3-default"))
			if len(cred) != 2 || cred["AWS_ACCESS_KEY_ID"] != "access-key-id" || cred["AWS_SECRET_ACCESS_KEY"] != "secret-access-key" {
				t.Errorf("quader-%s: credential items = %v, want only the two keys", role, cred)
			}
		}
		if got := strings.Join(storageVolumes(cronJobPod(docs)), ","); got != "secret-s3-default-backup,secret-s3-default-location,secret-s3-secondary-backup,secret-s3-secondary-ca" {
			t.Errorf("the backup pod mounts %s", got)
		}
		for name, pod := range podSpecs(docs) {
			if strings.HasSuffix(name, "-migrate") || strings.Contains(name, "scanner") || strings.Contains(name, "updater") {
				if got := storageVolumes(pod); len(got) != 0 {
					t.Errorf("%s mounts %v", name, got)
				}
			}
		}
	})

	t.Run("backup.source names the read login of the first connection, the one the backup reads (Q-HA-396)", func(t *testing.T) {
		src := at(configOf(t, docs), "backup", "source")
		if at(src, "separate_credentials") != true || str(src, "credentials", "source") != "static" ||
			str(src, "credentials", "access_key_id_file") != "/var/run/secrets/quader/s3-default-backup/access-key-id" ||
			str(src, "credentials", "secret_access_key_file") != "/var/run/secrets/quader/s3-default-backup/secret-access-key" {
			t.Errorf("backup.source = %v", src)
		}
		_, d := render(t, connectionsSet(), "secrets.storageConnections[0].backupSource.existingSecret=", "config.storage.connections[0].credentials_source=default",
			"secrets.storageConnections[0].accessKeyIdKey=", "secrets.storageConnections[0].secretAccessKeyKey=")
		if at(configOf(t, d), "backup", "source") != nil {
			t.Error("backup.source is rendered although the first connection has no backupSource")
		}
	})

	t.Run("restore and post-restore Jobs mount the journal reader of the first connection with source static", func(t *testing.T) {
		_, d := render(t, connectionsSet(), restoreSets("restore.journalCredentials.source=static", "secrets.storageConnections[0].journalReader.existingSecret=quader-s3-versions",
			"secrets.storageConnections[1].journalReader.existingSecret=quader-s3-secondary-versions")...)
		pod := restorePod(d)
		if got := strings.Join(storageVolumes(pod), ","); got != "secret-s3-default,secret-s3-default-journal,secret-s3-default-location,secret-s3-secondary,secret-s3-secondary-ca,secret-s3-secondary-journal" {
			t.Errorf("the restore Job mounts %s", got)
		}
		if cm := str(find(d, "ConfigMap", "quader-restore"), "data", "credentials.yaml"); !strings.Contains(cm, "/var/run/secrets/quader/s3-default-journal/secret-access-key") {
			t.Errorf("credentials.yaml = %s", cm)
		}
		for name, p := range podSpecs(d) {
			if name != "quader-restore" && storageVolume(p, "secret-s3-default-journal") != nil {
				t.Errorf("%s mounts the journal reader", name)
			}
		}
	})
}

// TestR_HA_072_Refusals: each refusal of R-HA-072 fails the render and names the value and the connection (R-HA-029).
func TestR_HA_072_Refusals(t *testing.T) {
	entry := func(extra ...string) []string {
		return append([]string{"secrets.storageConnections[0].connectionId=default"}, extra...)
	}
	second := func(id, bucket string, extra ...string) []string {
		return append([]string{"config.storage.connections[1].id=" + id, "config.storage.connections[1].bucket=" + bucket}, extra...)
	}
	for _, c := range []struct {
		name string
		base []string
		sets []string
		want []string
	}{
		{"the list empty", required(), []string{"config.storage.connections=null"}, []string{"config.storage.connections is empty"}},
		{"nine elements", required(), func() []string {
			var s []string
			for i := 1; i < 9; i++ {
				s = append(s, fmt.Sprintf("config.storage.connections[%d].id=c%d", i, i), fmt.Sprintf("config.storage.connections[%d].bucket=b%d", i, i))
			}
			return s
		}(), []string{"config.storage.connections has 9 elements", "at most 8"}},
		{"an id outside the pattern", required(), []string{"config.storage.connections[0].id=Primary"}, []string{"config.storage.connections[0].id", "Primary", "^[a-z][a-z0-9-]{0,30}[a-z0-9]$"}},
		{"two elements without id", required(), []string{"config.storage.connections[1].bucket=b2"}, []string{"config.storage.connections[0] and [1]", "\"default\"", "at most one element may leave it out"}},
		{"two elements with one id", required(), append(second("default", "b2"), "config.storage.connections[0].id=default"), []string{"config.storage.connections[0] and [1]", "\"default\""}},
		{"no bucket and no bucketKey", required(), []string{"config.storage.connections[0].bucket="}, []string{"config.storage.connections[0] (default) has no bucket", "bucketKey"}},
		{"a bucket and a bucketKey", required(), entry("secrets.storageConnections[0].existingSecret=p", "secrets.storageConnections[0].bucketKey=S3_BUCKET"), []string{"config.storage.connections[0] (default) has bucket", "bucketKey"}},
		{"an endpoint and an endpointKey", required(), entry("config.storage.connections[0].endpoint=s3.example.com", "secrets.storageConnections[0].existingSecret=p", "secrets.storageConnections[0].endpointKey=E"), []string{"config.storage.connections[0] (default) has endpoint", "endpointKey"}},
		{"a key of the Secret without the Secret", required(), entry("secrets.storageConnections[0].regionKey=S3_REGION"), []string{"secrets.storageConnections[0] (connection default)", "existingSecret"}},
		{"static without an entry", required(), []string{"config.storage.connections[0].credentials_source=static"}, []string{"config.storage.connections[0] (default)", "credentials_source: static", "secrets.storageConnections", "existingSecret"}},
		{"an existingSecret nothing reads", required(), entry("secrets.storageConnections[0].existingSecret=quader-s3"), []string{"existingSecret quader-s3 for connection default", "nothing reads it", "credentials_source: static"}},
		{"an entry of no connection", required(), []string{"secrets.storageConnections[0].connectionId=archive"}, []string{"secrets.storageConnections[0].connectionId", "archive", "names no element"}},
		{"two entries of one connection", required(), []string{"secrets.storageConnections[0].connectionId=default", "secrets.storageConnections[1].connectionId=default"}, []string{"secrets.storageConnections[0] and [1]", "connectionId \"default\""}},
		{"encrypted true", required(), []string{"config.storage.connections[0].encrypted=true"}, []string{"config.storage.connections[0] (default): encrypted: true is not supported yet: the encryption of stored content is not built (storage.md R-STOR-050)"}},
		{"drain on every element", required(), []string{"config.storage.connections[0].drain=true"}, []string{"every element of config.storage.connections has drain: true"}},
		{"min_copies above the connections without drain", required(), append(second("archive", "b2", "config.storage.connections[1].drain=true"), "config.storage.replication.min_copies=2"), []string{"config.storage.replication.min_copies (2)", "without drain: true (1)"}},
		{"two elements at one location", required(), second("nested", "quader-blobs", "config.storage.connections[1].prefix=quader/a"), []string{"config.storage.connections[0] (default) and config.storage.connections[1] (nested) lie at one location", "\"quader\"", "\"quader/a\""}},
		{"one location under two spellings of the endpoint", required(), append(second("other", "quader-blobs", "config.storage.connections[1].endpoint=https://s3.example.com"), "config.storage.connections[0].endpoint=s3.example.com"), []string{"lie at one location", "https://s3.example.com"}},
		{"the backup target at a connection's location", required(), append(second("archive", "quader-archive"), "config.backup.target.bucket=quader-archive"), []string{"config.backup.target.bucket (quader-archive)", "config.storage.connections[1] (archive)"}},
		{"a key file the entry sets", required(), entry("config.storage.connections[0].credentials_source=static", "secrets.storageConnections[0].existingSecret=k", "config.storage.connections[0].access_key_id_file=/x"), []string{"config.storage.connections[0].access_key_id_file (default) is managed by the chart"}},
		{"a key file without an entry", required(), []string{"config.storage.connections[0].secret_access_key_file=/x"}, []string{"config.storage.connections[0].secret_access_key_file (default) is managed by the chart"}},
		{"a bucket file the entry sets", required(), entry("config.storage.connections[0].bucket=", "secrets.storageConnections[0].existingSecret=p", "secrets.storageConnections[0].bucketKey=B", "config.storage.connections[0].bucket_file=/x"), []string{"config.storage.connections[0].bucket_file (default) is managed by the chart"}},
		{"static without a read login while the backup is on", required(), backupSets("config.storage.connections[0].credentials_source=static", "secrets.storageConnections[0].connectionId=default", "secrets.storageConnections[0].existingSecret=k"), []string{"backup.enabled with config.storage.connections[0] (default) credentials_source: static needs backupSource"}},
		{"a journal reader missing for source static", required(), restoreSets("restore.journalCredentials.source=static"), []string{"restore.journalCredentials.source: static", "journalReader", "connection default"}},
		{"a typo in an element (schema)", required(), []string{"config.storage.connections[0].bukcet=x"}, []string{"bukcet"}},
		{"a typo in an entry (schema)", required(), entry("secrets.storageConnections[0].bucketkey=x"), []string{"bucketkey"}},
		{"an unknown credentials source (schema)", required(), []string{"config.storage.connections[0].credentials_source=file"}, []string{"credentials_source"}},
	} {
		t.Run(c.name, func(t *testing.T) { renderFails(t, c.base, c.sets, c.want...) })
	}

	t.Run("an element without id is default and takes the entry of default; another endpoint, bucket or a prefix beside it passes", func(t *testing.T) {
		render(t, required(), "config.storage.connections[1].id=archive", "config.storage.connections[1].bucket=quader-blobs", "config.storage.connections[1].prefix=archive")
		render(t, required(), "config.storage.connections[1].id=archive", "config.storage.connections[1].bucket=quader-blobs", "config.storage.connections[1].endpoint=https://other.example.com")
		render(t, required(), "config.storage.connections[1].id=archive", "config.storage.connections[1].bucket=quader-archive", "config.storage.connections[1].drain=true")
	})
}

// TestR_HA_072_MovedValues: the values of the one store of before fail the render and name the keys that took their place, so an
// upgrade with old values stops at helm and starts no pod.
func TestR_HA_072_MovedValues(t *testing.T) {
	for _, c := range []struct {
		set  string
		want []string
	}{
		{"config.storage.s3.bucket=quader-blobs", []string{"config.storage.s3 is gone", "config.storage.connections", "credentials_source", "secrets.storageConnections"}},
		{"config.storage.s3.credentials.source=static", []string{"config.storage.s3 is gone", "credentials.source became credentials_source"}},
		{"secrets.s3.existingSecret=quader-s3", []string{"secrets.s3 is gone", "secrets.storageConnections", "connectionId: default", "bucketKey"}},
		{"secrets.s3CA.existingSecret=quader-s3-tls", []string{"secrets.s3CA is gone", "the field ca", "secrets.storageConnections"}},
		{"secrets.backupSource.existingSecret=quader-s3-read", []string{"secrets.backupSource is gone", "the field backupSource", "secrets.storageConnections"}},
		{"secrets.journalReader.existingSecret=quader-journal", []string{"secrets.journalReader is gone", "the field journalReader", "secrets.storageConnections"}},
	} {
		t.Run(c.set, func(t *testing.T) { renderFails(t, required(), []string{c.set}, c.want...) })
	}
}

// installationBefore and installationAfter are one installation's object-store values before and after the move to the list
// (a bucket of an in-cluster object store whose provisioner writes the two key names below, and a CA from a trust Secret).
const installationBefore = `
config:
  storage:
    s3:
      bucket: example-blobs
      region: de-1
      endpoint: https://object-store.example.svc
      force_path_style: true
      credentials:
        source: static
secrets:
  s3:
    existingSecret: example-blobs
    accessKeyIdKey: AWS_ACCESS_KEY_ID
    secretAccessKeyKey: AWS_SECRET_ACCESS_KEY
  s3CA:
    existingSecret: internal-truststore
    key: ca.crt
`

const installationAfter = `
config:
  storage:
    connections:
      - id: default
        bucket: example-blobs
        region: de-1
        endpoint: https://object-store.example.svc
        force_path_style: true
        credentials_source: static
secrets:
  storageConnections:
    - connectionId: default
      existingSecret: example-blobs
      accessKeyIdKey: AWS_ACCESS_KEY_ID
      secretAccessKeyKey: AWS_SECRET_ACCESS_KEY
      ca:
        existingSecret: internal-truststore
        key: ca.crt
`

func TestR_HA_072_AnInstallationMoves(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "values.yaml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("its values of before fail the render and name the new keys", func(t *testing.T) {
		renderFails(t, append(required(), write(t, installationBefore)), nil, "config.storage.s3 is gone", "config.storage.connections", "secrets.storageConnections")
	})
	t.Run("its values of after render the same store: the bucket and address in the element, the keys and the CA as files", func(t *testing.T) {
		_, docs := render(t, append(required(), write(t, installationAfter)))
		l := list(configOf(t, docs), "storage", "connections")
		if len(l) != 1 {
			t.Fatalf("storage.connections = %v", l)
		}
		c := l[0].(map[string]any)
		for k, want := range map[string]any{
			"id": "default", "bucket": "example-blobs", "region": "de-1", "endpoint": "https://object-store.example.svc", "force_path_style": true,
			"credentials_source": "static", "access_key_id_file": "/var/run/secrets/quader/s3-default/access-key-id",
			"secret_access_key_file": "/var/run/secrets/quader/s3-default/secret-access-key", "ca_file": "/var/run/secrets/quader/s3-default-ca/ca.crt",
		} {
			if c[k] != want {
				t.Errorf("%s = %v, want %v", k, c[k], want)
			}
		}
		pod := at(find(docs, "Deployment", "quader-packages"), "spec", "template", "spec")
		if got := itemsOf(storageVolume(pod, "secret-s3-default")); got["AWS_ACCESS_KEY_ID"] != "access-key-id" || got["AWS_SECRET_ACCESS_KEY"] != "secret-access-key" {
			t.Errorf("credential items = %v", got)
		}
		if str(storageVolume(pod, "secret-s3-default-ca"), "secret", "secretName") != "internal-truststore" {
			t.Error("the CA is not mounted from internal-truststore")
		}
	})
}

// TestR_HA_072_TheElementSchemaIsTheBinarys (Q-HA-397): the chart's schema checks an element of config.storage.connections against
// the keys of storage.md R-STOR-050, so a typo fails at helm; the binary's schema (internal/config/schema.json, generated from the
// configuration types) is the source, and this test holds the two equal: the same keys, the same enums where both have one.
func TestR_HA_072_TheElementSchemaIsTheBinarys(t *testing.T) {
	element := func(t *testing.T, schema map[string]any, path ...string) map[string]any {
		t.Helper()
		node := any(schema)
		for _, p := range path {
			node = at(node, "properties", p)
		}
		props, _ := at(node, "items", "properties").(map[string]any)
		if len(props) == 0 {
			t.Fatalf("no element properties at %v", path)
		}
		return props
	}
	chart := element(t, readJSONFile(t, filepath.Join(chartDir, "values.schema.json")), "config", "storage", "connections")
	binary := element(t, readJSONFile(t, filepath.Join(repoRoot, "internal", "config", "schema.json")), "storage", "connections")
	for k := range binary {
		if _, ok := chart[k]; !ok {
			t.Errorf("the binary's element has %s, the chart's schema does not", k)
		}
	}
	for k, c := range chart {
		b, ok := binary[k]
		if !ok {
			t.Errorf("the chart's schema has %s, the binary's element does not", k)
			continue
		}
		if ce, be := list(c, "enum"), list(b, "enum"); len(ce) > 0 && len(be) > 0 && fmt.Sprint(ce) != fmt.Sprint(be) {
			t.Errorf("%s: enum %v in the chart, %v in the binary", k, ce, be)
		}
	}
}
