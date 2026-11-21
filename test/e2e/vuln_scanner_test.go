//go:build e2e

package e2e

// The one case of the role scanner in the set vuln (R-VULN-046, Q-VULN-205): the fixture database of 128 KiB is pushed to the local
// repository the profile names as aquasec/trivy-db:2 and the task vuln-sync-trivy-db records it; the loader of every scanner pod (the
// container database, the kind trivy-db) reads it through the scan listener with the pod's certificate into the volume trivy-db. Then an
// image of about a kilobyte (Alpine 3.12 with four packages, two of which the fixture database names), pushed through the registry API by
// the test's own client, is analysed and detected by the pinned analyser in a scanner pod, the binary its init container copied from the
// analyser's own image (the release image has none). The finding shows the whole way over the chart's links - the worker's calls to
// <fullname>-vuln-scanner-trivy and the scanner's pull from <fullname>-packages-scan, both mutual TLS with the profile's certificate and
// admitted by the policies of R-HA-042 - and the live pod's spec and status show its init container, containers and mounts (R-HA-065,
// R-HA-070). Every scanner pod of both kinds is listed under its kind by its report of itself, an image scanner with the
// database it holds (R-OPS-048, Q-OPS-225).

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	scannerDBKey = "scanner-db"
	scannerDB    = "aquasec/trivy-db"
)

func TestVuln_R_VULN_041_AnImagePushedToALocalRepositoryIsAnalysedAndDetectedByAScannerPod(t *testing.T) {
	v := vulnFx(t)
	layer := v.scannerDatabase(t)

	key := "oci-scan-" + v.run
	v.adm.do(http.MethodPost, reposRoute, map[string]any{"key": key, "class": "local", "format": "oci"}).want(t, "create "+key, 201, 409)
	cfg, imgLayer := scannerImageBlobs(t, v.run)
	manifest, err := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": scannerDigest(cfg), "size": len(cfg)},
		"layers": []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar", "digest": scannerDigest(imgLayer), "size": len(imgLayer)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	base := "/v2/" + repoRef(key) + "/app"
	// The new repository reaches every packages replica with its grant index; the first upload is retried until it has.
	waitFor(t, "the repository on the replica that takes the upload", time.Minute, time.Second, func() bool {
		r, err := v.adm.send(http.MethodPost, base+"/blobs/uploads/?digest="+scannerDigest(cfg), cfg, "Content-Type", "application/octet-stream")
		return err == nil && r.Status == http.StatusCreated
	})
	v.adm.do(http.MethodPost, base+"/blobs/uploads/?digest="+scannerDigest(imgLayer), imgLayer, "Content-Type", "application/octet-stream").want(t, "the layer", 201)
	v.adm.do(http.MethodPut, base+"/manifests/1.0", manifest, "Content-Type", "application/vnd.oci.image.manifest.v1+json").want(t, "the manifest", 201)

	started := time.Now()
	v.adm.do(http.MethodPost, "/api/v1/system/tasks/vuln-image-scan/run", nil).want(t, "run the image scan", 202, 409)
	hexDigest := strings.TrimPrefix(scannerDigest(manifest), "sha256:")
	t.Cleanup(func() {
		if t.Failed() {
			// The row says why: not due yet (not_before), put off with a code of the installation, or failed with the image's.
			t.Logf("scan row: %s", sql(t, `SELECT concat_ws(' ', state, attempts, error_code, error_detail, not_before, now(), db_digest) FROM quader.vuln_image_scan WHERE digest = decode('`+hexDigest+`', 'hex')`))
		}
	})
	took := waitFor(t, "the scanner's analysis stored", time.Minute, time.Second, func() bool {
		return sql(t, `SELECT count(*) FROM quader.vuln_image_sbom WHERE digest = decode('`+hexDigest+`', 'hex')`) == "1"
	})
	t.Logf("the analysis was stored %s after the task run (%s after the push)", took, time.Since(started))
	// R-VULN-048: the detection through the scanner pod against the fixture database: the image's two advisories, under the database in use.
	took = waitFor(t, "the image detected under the fixture database", 2*time.Minute, time.Second, func() bool {
		return sql(t, `SELECT concat_ws(' ', state, db_digest) FROM quader.vuln_image_scan WHERE digest = decode('`+hexDigest+`', 'hex')`) == "scanned "+layer
	})
	t.Logf("the detection was recorded %s after the analysis (%s after the task run)", took, time.Since(started))
	findings := sql(t, `SELECT string_agg(advisory_id, ',' ORDER BY advisory_id) FROM quader.vuln_finding WHERE ecosystem = 'OCI' AND version = 'sha256:`+hexDigest+`'`)
	if findings != "CVE-2020-28928,CVE-2021-28831" {
		t.Fatalf("the image's findings are %q, want the two advisories of the fixture database", findings)
	}
	if got := sql(t, `SELECT packages FROM quader.vuln_image_scan WHERE digest = decode('`+hexDigest+`', 'hex')`); got != "4" {
		t.Errorf("the image's packages: %s, want the four of its package database", got)
	}

	// R-HA-065, R-HA-070: a scanner pod holds the TLS pair and the internal CA, and nothing else of the installation; its init container
	// trivy copied the analyser from its own image into trivy-bin, which quader mounts read-only; its container quader mounts the database
	// read-only and its container database writes it. The image has no shell and no ls (distroless), so the running pod's own spec and
	// status say what the kubelet put where (Q-HA-342).
	pods := livePods(t, "app.kubernetes.io/component=vuln-scanner-trivy")
	if len(pods) < 2 {
		t.Fatalf("%d scanner pods are running, the profile runs two", len(pods))
	}
	raw := kn(t, "get", "pod", pods[0].Name, "-o", "json")
	type container struct {
		Name         string   `json:"name"`
		Image        string   `json:"image"`
		Command      []string `json:"command"`
		VolumeMounts []struct {
			Name      string `json:"name"`
			MountPath string `json:"mountPath"`
			ReadOnly  bool   `json:"readOnly"`
		} `json:"volumeMounts"`
	}
	var p struct {
		Spec struct {
			AutomountServiceAccountToken *bool `json:"automountServiceAccountToken"`
			Volumes                      []struct {
				Name   string `json:"name"`
				Secret *struct {
					SecretName string `json:"secretName"`
					Items      []struct {
						Key  string `json:"key"`
						Path string `json:"path"`
					} `json:"items"`
				} `json:"secret"`
				Projected *json.RawMessage `json:"projected"`
			} `json:"volumes"`
			InitContainers []container `json:"initContainers"`
			Containers     []container `json:"containers"`
		} `json:"spec"`
		Status struct {
			InitContainerStatuses []struct {
				Name  string `json:"name"`
				State struct {
					Terminated *struct {
						ExitCode int    `json:"exitCode"`
						Reason   string `json:"reason"`
					} `json:"terminated"`
				} `json:"state"`
			} `json:"initContainerStatuses"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.Spec.AutomountServiceAccountToken == nil || *p.Spec.AutomountServiceAccountToken {
		t.Errorf("the scanner pod mounts a service account token")
	}
	items := map[string][]string{}
	for _, vol := range p.Spec.Volumes {
		if vol.Projected != nil {
			t.Errorf("the scanner pod has the projected volume %s (a service account token)", vol.Name)
		}
		if vol.Secret != nil {
			// The scanner holds its own kind's pair and no other kind's (R-HA-063, Q-HA-345).
			if vol.Secret.SecretName != "quader-tls-vuln-scanner-trivy" {
				t.Errorf("the scanner pod mounts the Secret %s", vol.Secret.SecretName)
			}
			for _, it := range vol.Secret.Items {
				items[vol.Name] = append(items[vol.Name], it.Path)
			}
		}
	}
	// The copy: one init container trivy, the image's cp of its binary into trivy-bin and nothing else, ended with exit code 0.
	if len(p.Spec.InitContainers) != 1 || p.Spec.InitContainers[0].Name != "trivy" ||
		strings.Join(p.Spec.InitContainers[0].Command, " ") != "cp /usr/local/bin/trivy /opt/trivy/trivy" ||
		len(p.Spec.InitContainers[0].VolumeMounts) != 1 || p.Spec.InitContainers[0].VolumeMounts[0].Name != "trivy-bin" ||
		p.Spec.InitContainers[0].VolumeMounts[0].MountPath != "/opt/trivy" || p.Spec.InitContainers[0].VolumeMounts[0].ReadOnly {
		t.Errorf("the scanner pod's init containers %+v, want trivy copying the analyser into trivy-bin alone", p.Spec.InitContainers)
	}
	if s := p.Status.InitContainerStatuses; len(s) != 1 || s[0].State.Terminated == nil || s[0].State.Terminated.ExitCode != 0 {
		t.Errorf("the init container's status %+v, want trivy terminated with exit code 0", s)
	}
	var containers []string
	databaseMount, binaryMount := map[string]string{}, map[string]string{}
	for _, c := range p.Spec.Containers {
		containers = append(containers, c.Name)
		var files []string
		for _, m := range c.VolumeMounts {
			if m.Name == "trivy-db" {
				databaseMount[c.Name] = map[bool]string{true: "ro", false: "rw"}[m.ReadOnly] + " " + m.MountPath
			}
			if m.Name == "trivy-bin" {
				binaryMount[c.Name] = map[bool]string{true: "ro", false: "rw"}[m.ReadOnly] + " " + m.MountPath
			}
			if strings.HasPrefix(m.MountPath, "/var/run/secrets/kubernetes.io") {
				t.Errorf("the container %s mounts %s", c.Name, m.MountPath)
			}
			if !strings.HasPrefix(m.MountPath, "/var/run/secrets/quader/") {
				continue
			}
			for _, f := range items[m.Name] {
				files = append(files, strings.TrimPrefix(m.MountPath, "/var/run/secrets/quader/")+"/"+f)
			}
		}
		sort.Strings(files)
		if want := []string{"tls/ca.crt", "tls/tls.crt", "tls/tls.key"}; strings.Join(files, ",") != strings.Join(want, ",") {
			t.Errorf("below /var/run/secrets/quader/ the container %s holds %v, want %v", c.Name, files, want)
		}
	}
	if strings.Join(containers, ",") != "quader,database" || databaseMount["quader"] != "ro /var/lib/quader/trivy-db" ||
		databaseMount["database"] != "rw /var/lib/quader/trivy-db" {
		t.Errorf("the scanner pod's containers %v mount trivy-db as %v, want quader read-only and database read-write", containers, databaseMount)
	}
	if len(binaryMount) != 1 || binaryMount["quader"] != "ro /opt/trivy" {
		t.Errorf("the scanner pod's containers mount trivy-bin as %v, want quader alone, read-only at /opt/trivy", binaryMount)
	}

	// R-OPS-048, Q-OPS-223, Q-OPS-225: every scanner pod of both kinds reports itself to the scan listener with the certificate of its kind,
	// and the API lists it by its pod name under that kind; an image scanner that detected the image is ready, names its Trivy's version
	// and the database its pod holds, with its build time. The malware scanners are listed whatever their state: they wait for a database
	// until the malware case deployed one. The version is the tag of TRIVY_IMAGE: the release image carries no analyser, so the binary that
	// answered it, analysed and detected the image is the one the init container copied (R-HA-070).
	ref, _, _ := strings.Cut(readVersionPin(t, "TRIVY_IMAGE"), "@")
	tag := strings.TrimPrefix(ref[strings.LastIndex(ref, ":")+1:], "v")
	want := map[string]string{}
	for _, p := range pods {
		want[p.Name] = "trivy"
	}
	for _, p := range livePods(t, "app.kubernetes.io/component=malware-scanner-clamav") {
		want[p.Name] = "clamav"
	}
	took = waitFor(t, "every scanner pod listed under its kind", time.Minute, 2*time.Second, func() bool {
		var list struct {
			Items []struct {
				Kind, Name, State string
				ToolVersion       *string
				Database          *struct {
					Version string
					BuiltAt *string
				}
			}
		}
		v.adm.do(http.MethodGet, "/api/v1/system/scanners", nil).want(t, "the scanner replicas", 200).JSON(t, &list)
		listed := 0
		for _, r := range list.Items {
			kind, ok := want[r.Name]
			trivyOK := r.State == "ready" && r.ToolVersion != nil && strings.TrimPrefix(*r.ToolVersion, "v") == tag && r.Database != nil &&
				"sha256:"+r.Database.Version == layer && r.Database.BuiltAt != nil
			if ok && r.Kind == kind && (kind != "trivy" || trivyOK) {
				listed++
			}
		}
		return listed == len(want)
	})
	t.Logf("every scanner pod reported the database in use %s after the detection", took)
}

// scannerDatabase pushes the fixture database (internal/vuln/imagescan/testdata/analyser/db, 128 KiB) as aquasec/trivy-db:2 into the
// local repository the profile names, e2e/scanner-db, as `oras push` does, runs the task vuln-sync-trivy-db and waits for every scanner
// pod's loader to have read it (Q-VULN-205). It returns the layer's digest, the database in use.
func (e *vulnEnv) scannerDatabase(t *testing.T) string {
	t.Helper()
	e.adm.do(http.MethodPost, reposRoute, map[string]any{
		"key": scannerDBKey, "class": "local", "format": "oci",
		"vulnerabilities": map[string]any{"enabled": false},
	}).want(t, "create "+scannerDBKey, 201, 409)
	layer := fixtureDatabaseLayer(t)
	dbConfig := []byte("{}")
	base := "/v2/" + repoRef(scannerDBKey) + "/" + scannerDB
	waitFor(t, "the database's repository on the replica that takes the upload", time.Minute, time.Second, func() bool {
		r, err := e.adm.send(http.MethodPost, base+"/blobs/uploads/?digest="+scannerDigest(dbConfig), dbConfig, "Content-Type", "application/octet-stream")
		return err == nil && (r.Status == http.StatusCreated || r.Status == http.StatusOK)
	})
	e.adm.do(http.MethodPost, base+"/blobs/uploads/?digest="+scannerDigest(layer), layer, "Content-Type", "application/octet-stream").want(t, "the database's layer", 201, 200)
	m, err := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{"mediaType": "application/vnd.aquasec.trivy.config.v1+json", "digest": scannerDigest(dbConfig), "size": len(dbConfig)},
		"layers": []any{map[string]any{"mediaType": "application/vnd.aquasec.trivy.db.layer.v1.tar+gzip", "digest": scannerDigest(layer), "size": len(layer)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.adm.do(http.MethodPut, base+"/manifests/2", m, "Content-Type", "application/vnd.oci.image.manifest.v1+json").want(t, "the database's manifest", 201)
	digest := scannerDigest(layer)
	started := time.Now()
	e.adm.do(http.MethodPost, "/api/v1/system/tasks/vuln-sync-trivy-db/run", nil).want(t, "run the database's task", 202, 409)
	waitFor(t, "the fixture database in use", 2*time.Minute, time.Second, func() bool {
		return sql(t, `SELECT coalesce(etag, '') FROM quader.vuln_source_state WHERE source = 'trivy' AND ecosystem = 'OCI' AND state = 'complete'`) == digest
	})
	recorded := time.Since(started)
	// The loaders log each load; every pod's loader reads it within a poll (5 s before its first database) and the load (Q-VULN-201).
	took := waitFor(t, "every scanner pod's loader to hold the database", 2*time.Minute, 2*time.Second, func() bool {
		pods := livePods(t, "app.kubernetes.io/component=vuln-scanner-trivy")
		for _, p := range pods {
			out, err := kubectl("-n", cfg.namespace, "logs", p.Name, "-c", "database")
			if err != nil || !strings.Contains(out, "the image scanner's database is loaded") || !strings.Contains(out, digest) {
				return false
			}
		}
		return len(pods) > 0
	})
	t.Logf("the task recorded the fixture database %s after its run; every loader held it %s later", recorded, took)
	return digest
}

// fixtureDatabaseLayer is the layer of the fixture database: a gzip tar of its trivy.db and metadata.json at the root, as the publisher's.
func fixtureDatabaseLayer(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"trivy.db", "metadata.json"} {
		b, err := os.ReadFile(filepath.Join(cfg.repo, "internal", "vuln", "imagescan", "testdata", "analyser", "db", name)) //nolint:gosec // the repository's fixture
		if err != nil {
			t.Fatal(err)
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(b)), Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0)}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// scannerImage is the fixture image of the case: Alpine 3.12 with four packages in its package database, two of which the fixture database
// names (CVE-2020-28928 in musl, CVE-2021-28831 in busybox), and a file that names the run, so every run pushes a new digest.
func scannerImage(run string) map[string]string {
	apk := func(name, version, license string) string {
		return "P:" + name + "\nV:" + version + "\nA:x86_64\nL:" + license + "\no:" + name + "\nT:" + name + " of the end-to-end fixture\n\n"
	}
	return map[string]string{
		"etc/alpine-release": "3.12.0\n",
		"etc/os-release":     "NAME=\"Alpine Linux\"\nID=alpine\nVERSION_ID=3.12.0\nPRETTY_NAME=\"Alpine Linux v3.12\"\nHOME_URL=\"https://alpinelinux.org/\"\n",
		"lib/apk/db/installed": apk("musl", "1.1.24-r9", "MIT") + apk("busybox", "1.31.1-r19", "GPL-2.0-only") + apk("zlib", "1.2.11-r3", "Zlib") +
			apk("alpine-baselayout", "3.2.0-r7", "GPL-2.0-only"),
		"etc/scanner-e2e": "the image of the scanner's e2e case, run " + run + "\n",
	}
}

// scannerImageBlobs are the config and the one layer of the case's image.
func scannerImageBlobs(t *testing.T, run string) (config, layer []byte) {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	files := scannerImage(run)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		body := []byte(files[name])
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	layer = buf.Bytes()
	config, err := json.Marshal(map[string]any{
		"architecture": "amd64", "os": "linux", "rootfs": map[string]any{"type": "layers", "diff_ids": []string{scannerDigest(layer)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return config, layer
}

func scannerDigest(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
