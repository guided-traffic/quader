//go:build e2e

package e2e

// The one case of the role scanner in the set vuln (R-VULN-046): an image of a few hundred bytes, pushed through the registry API by the
// test's own client, is analysed by the pinned analyser in a scanner pod. The stored analysis shows the whole way over the chart's links -
// the worker's call to <fullname>-scanner and the scanner's pull from <fullname>-packages-scan, both mutual TLS with the profile's
// certificate and admitted by the policies of R-HA-042. It asserts the analysis, not a detection, so it needs no database (Q-VULN-168).

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestVuln_R_VULN_041_AnImagePushedToALocalRepositoryIsAnalysedByAScannerPod(t *testing.T) {
	v := vulnFx(t)
	key := "oci-scan-" + v.run
	v.adm.do(http.MethodPost, reposRoute, map[string]any{"key": key, "class": "local", "format": "oci"}).want(t, "create "+key, 201, 409)

	cfg, layer := scannerImageBlobs(t, v.run)
	manifest, err := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": scannerDigest(cfg), "size": len(cfg)},
		"layers": []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar", "digest": scannerDigest(layer), "size": len(layer)}},
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
	v.adm.do(http.MethodPost, base+"/blobs/uploads/?digest="+scannerDigest(layer), layer, "Content-Type", "application/octet-stream").want(t, "the layer", 201)
	v.adm.do(http.MethodPut, base+"/manifests/1.0", manifest, "Content-Type", "application/vnd.oci.image.manifest.v1+json").want(t, "the manifest", 201)

	started := time.Now()
	v.adm.do(http.MethodPost, "/api/v1/system/tasks/vuln-image-scan/run", nil).want(t, "run the image scan", 202, 409)
	hexDigest := strings.TrimPrefix(scannerDigest(manifest), "sha256:")
	t.Cleanup(func() {
		if t.Failed() {
			// The row says why: not due yet (not_before), put off with a code of the installation, or failed with the image's.
			t.Logf("scan row: %s", sql(t, `SELECT concat_ws(' ', state, attempts, error_code, error_detail, not_before, now()) FROM quader.vuln_image_scan WHERE digest = decode('`+hexDigest+`', 'hex')`))
		}
	})
	took := waitFor(t, "the scanner's analysis stored", time.Minute, 2*time.Second, func() bool {
		return sql(t, `SELECT count(*) FROM quader.vuln_image_sbom WHERE digest = decode('`+hexDigest+`', 'hex')`) == "1"
	})
	t.Logf("the analysis was stored %s after the task run (%s after the push)", took, time.Since(started))

	// R-HA-065: a scanner pod holds the TLS pair and the internal CA, and nothing else of the installation. The image has no shell
	// and no ls (distroless), so the running pod's own spec says what the kubelet put below /var/run/secrets (Q-HA-342).
	pods := livePods(t, "app.kubernetes.io/component=scanner")
	if len(pods) < 2 {
		t.Fatalf("%d scanner pods are running, the profile runs two", len(pods))
	}
	raw := kn(t, "get", "pod", pods[0].Name, "-o", "json")
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
			Containers []struct {
				VolumeMounts []struct {
					Name      string `json:"name"`
					MountPath string `json:"mountPath"`
				} `json:"volumeMounts"`
			} `json:"containers"`
		} `json:"spec"`
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
			if vol.Secret.SecretName != "quader-tls-scanner" {
				t.Errorf("the scanner pod mounts the Secret %s", vol.Secret.SecretName)
			}
			for _, it := range vol.Secret.Items {
				items[vol.Name] = append(items[vol.Name], it.Path)
			}
		}
	}
	var files []string
	for _, c := range p.Spec.Containers {
		for _, m := range c.VolumeMounts {
			if strings.HasPrefix(m.MountPath, "/var/run/secrets/kubernetes.io") {
				t.Errorf("the scanner mounts %s", m.MountPath)
			}
			if !strings.HasPrefix(m.MountPath, "/var/run/secrets/quader/") {
				continue
			}
			for _, f := range items[m.Name] {
				files = append(files, strings.TrimPrefix(m.MountPath, "/var/run/secrets/quader/")+"/"+f)
			}
		}
	}
	sort.Strings(files)
	if want := []string{"tls/ca.crt", "tls/tls.crt", "tls/tls.key"}; strings.Join(files, ",") != strings.Join(want, ",") {
		t.Errorf("below /var/run/secrets/quader/ the scanner pod holds %v, want %v", files, want)
	}
}

// scannerImageBlobs are the config and the one layer of the case's image: a file that names the run, so every run pushes a new digest.
func scannerImageBlobs(t *testing.T, run string) (config, layer []byte) {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := []byte("the image of the scanner's e2e case, run " + run + "\n")
	if err := tw.WriteHeader(&tar.Header{Name: "etc/scanner-e2e", Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
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
