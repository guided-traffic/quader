package imagescan

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fidelityImage is the fixed image of the fidelity test: Alpine 3.12 with four packages in the package database, two of which the fixture
// database names (testdata/analyser/db, written by internal/tools/trivydbfixture).
var fidelityImage = map[string]string{
	"etc/alpine-release": "3.12.0\n",
	"etc/os-release": "NAME=\"Alpine Linux\"\nID=alpine\nVERSION_ID=3.12.0\nPRETTY_NAME=\"Alpine Linux v3.12\"\n" +
		"HOME_URL=\"https://alpinelinux.org/\"\n",
	"lib/apk/db/installed": apkEntry("musl", "1.1.24-r9", "musl", "MIT") + apkEntry("busybox", "1.31.1-r19", "busybox", "GPL-2.0-only") +
		apkEntry("zlib", "1.2.11-r3", "zlib", "Zlib") + apkEntry("alpine-baselayout", "3.2.0-r7", "alpine-baselayout", "GPL-2.0-only"),
}

func apkEntry(name, version, origin, license string) string {
	return "P:" + name + "\nV:" + version + "\nA:x86_64\nL:" + license + "\no:" + origin + "\nT:" + name + " of the fidelity fixture\n\n"
}

// TestQ_VULN_164_TheRewrittenAnalysisDetectsWhatTheAnalysersOwnDoes is the fidelity of the rewritten analysis (R-VULN-043, Q-VULN-176): the
// pinned analyser analyses a fixed image of a few kilobytes, the worker writes the analysis anew, and the detections of both documents
// against the fixture database are equal - the same findings with every field, the same packages (--list-all-pkgs), the same OS. A member
// the detection reads and the rewrite drops fails it, and so does a raise of the analyser's pin that starts to read a new member. It needs
// the pinned binary: `make test-analyser` names it (QUADER_TEST_TRIVY_BIN); CI runs it in the job container-scan.
func TestQ_VULN_164_TheRewrittenAnalysisDetectsWhatTheAnalysersOwnDoes(t *testing.T) {
	bin := os.Getenv("QUADER_TEST_TRIVY_BIN")
	if bin == "" {
		t.Skip("QUADER_TEST_TRIVY_BIN names the pinned analyser; run `make test-analyser`, which fetches it")
	}
	ctx := context.Background()
	tr, err := NewTrivy(ctx, bin)
	if err != nil {
		t.Fatal(err)
	}
	lim := Limits{Timeout: 2 * time.Minute, OutputBytes: 64 << 20, StderrBytes: 16 << 10}
	layout := filepath.Join(t.TempDir(), "layout")
	writeLayout(t, layout, fidelityImage)

	own := t.TempDir()
	if err := tr.Analyse(ctx, layout, filepath.Join(own, "sbom.json"), own, lim); err != nil {
		t.Fatalf("the analysis: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(own, "sbom.json")) //nolint:gosec // the test's own directory
	if err != nil {
		t.Fatal(err)
	}
	rewritten, err := Rewrite(raw)
	if err != nil {
		t.Fatalf("the rewrite refused the analyser's document: %v", err)
	}
	anew := t.TempDir()
	if err := os.WriteFile(filepath.Join(anew, "sbom.json"), rewritten, 0o600); err != nil {
		t.Fatal(err)
	}

	// The same name in two directories: the report names the document it read, and the two must not differ by that.
	got, want := detectFixture(t, tr, anew, lim), detectFixture(t, tr, own, lim)
	if len(want.findings) != 2 || want.packages != 4 || want.os == nil {
		t.Fatalf("the analyser's own document detected %d findings in %d packages, OS %v; the fixture holds 2 in 4 on Alpine 3.12",
			len(want.findings), want.packages, want.os)
	}
	if !reflect.DeepEqual(got.os, want.os) {
		t.Errorf("the OS of the rewritten document is %v, of the analyser's %v", got.os, want.os)
	}
	if !reflect.DeepEqual(got.results, want.results) {
		t.Errorf("the detection of the rewritten document differs from the analyser's own at:\n%s", strings.Join(differences("Results", got.results, want.results), "\n"))
	}
}

// differences lists the paths at which two decoded JSON values differ, with both values.
func differences(path string, got, want any) []string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			break
		}
		var out []string
		for k := range w {
			out = append(out, differences(path+"."+k, g[k], w[k])...)
		}
		for k := range g {
			if _, ok := w[k]; !ok {
				out = append(out, differences(path+"."+k, g[k], nil)...)
			}
		}
		return out
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			break
		}
		var out []string
		for i := range w {
			out = append(out, differences(path+"["+strconv.Itoa(i)+"]", g[i], w[i])...)
		}
		return out
	}
	if reflect.DeepEqual(got, want) {
		return nil
	}
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	return []string{fmt.Sprintf("%s: rewritten %s, analyser's own %s", path, g, w)}
}

// fidelityReport is what the test compares of a report: the OS and every result with its findings and packages.
type fidelityReport struct {
	os       any
	results  any
	findings []any
	packages int
}

// detectFixture runs the detection of dir/sbom.json against a copy of the fixture database, listing every package.
func detectFixture(t *testing.T, tr *Trivy, dir string, lim Limits) fidelityReport {
	t.Helper()
	cache := filepath.Join(t.TempDir(), "cache")
	for _, name := range []string{"trivy.db", "metadata.json"} {
		b, err := os.ReadFile(filepath.Join("testdata", "analyser", "db", name)) //nolint:gosec // the test's fixture
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(cache, "db"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cache, "db", name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	access := DatabaseAccess{Args: []string{"--cache-dir", cache, "--list-all-pkgs"}, Digest: "fixture", Release: func() {}}
	// The document by its name in the working directory (dir): the report names what it read, and the two runs must not differ by it.
	if err := tr.Detect(context.Background(), "sbom.json", filepath.Join(dir, "report.json"), dir, access, lim); err != nil {
		t.Fatalf("the detection of %s: %v", dir, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "report.json")) //nolint:gosec // the test's own directory
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Metadata struct {
			OS any `json:"OS"`
		} `json:"Metadata"`
		Results []map[string]any `json:"Results"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	out := fidelityReport{os: doc.Metadata.OS}
	var results []any
	for _, r := range doc.Results {
		if v, ok := r["Vulnerabilities"].([]any); ok {
			out.findings = append(out.findings, v...)
		}
		if p, ok := r["Packages"].([]any); ok {
			out.packages += len(p)
		}
		results = append(results, r)
	}
	out.results = results
	return out
}

// writeLayout writes an OCI layout of one image whose one layer holds files: the form the scanner hands the analyser (BuildLayout).
func writeLayout(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for _, name := range []string{"etc/alpine-release", "etc/os-release", "lib/apk/db/installed"} {
		body := files[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0)}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	diffID := sum(tarBuf.Bytes())
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	if _, err := gz.Write(tarBuf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	layer := gzBuf.Bytes()
	config, err := json.Marshal(map[string]any{
		"architecture": "amd64", "os": "linux", "rootfs": map[string]any{"type": "layers", "diff_ids": []string{diffID}},
		"config": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": sum(config), "size": len(config)},
		"layers": []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": sum(layer), "size": len(layer)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	index, err := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json",
		"manifests": []any{map[string]any{"mediaType": "application/vnd.oci.image.manifest.v1+json", "digest": sum(manifest), "size": len(manifest)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	blobs := filepath.Join(dir, "blobs", "sha256")
	if err := os.MkdirAll(blobs, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, b := range [][]byte{layer, config, manifest} {
		if err := os.WriteFile(filepath.Join(blobs, sum(b)[len("sha256:"):]), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.json"), index, 0o600); err != nil {
		t.Fatal(err)
	}
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
