//go:build integration

package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/packages/view"
)

// chartArchive is a chart package of a few hundred bytes, laid out as `helm package` writes one.
func chartArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"Chart.yaml", "README.md", "values.yaml"} {
		body, ok := files[name]
		if !ok {
			continue
		}
		if err := tw.WriteHeader(&tar.Header{Name: "web/" + name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
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

// TestR_OCI_042_AChartsPageShowsItsFactsReadmeAndValues: a Helm chart pushed as Helm's registry client pushes one (the config Chart.yaml
// as JSON, the content layer the package; the requests in process, no helm binary) is the kind helm-chart; its version carries the
// chart's facts read at the push (its description fills the one the annotations leave), its README (wiki content, rendered by the
// README renderer) and its values read from the content layer on demand. One replica, three blobs of bytes; about 2 s.
func TestR_OCI_042_AChartsPageShowsItsFactsReadmeAndValues(t *testing.T) {
	p := newProto(t, protoOptions{replicas: 1})
	cfg, _ := json.Marshal(map[string]any{
		"apiVersion": "v2", "name": "web", "version": "0.3.0", "appVersion": "2.4.1", "description": "The portal's web API.",
		"type": "application", "deprecated": true, "keywords": []string{"web", "api"}, "home": "https://example.com/web",
		"dependencies": []map[string]string{{"name": "redis", "version": "^19.0.0", "repository": "oci://registry.example.com/charts"}},
	})
	content := chartArchive(t, map[string]string{
		"Chart.yaml":  "apiVersion: v2\nname: web\nversion: 0.3.0\n",
		"README.md":   "# web\n\n| Key | Default |\n|---|---|\n| replicaCount | 2 |\n",
		"values.yaml": "replicaCount: 2\n",
	})
	m, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": manifestType,
		"config":      map[string]any{"mediaType": "application/vnd.cncf.helm.config.v1+json", "digest": digestOf(cfg), "size": len(cfg)},
		"layers":      []any{map[string]any{"mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip", "digest": digestOf(content), "size": len(content)}},
		"annotations": map[string]string{"org.opencontainers.image.title": "web", "org.opencontainers.image.version": "0.3.0"},
	})
	want(t, p.pushImage(p.as("admin"), repoLocal, "charts/web", "0.3.0", image{config: cfg, layers: [][]byte{content}, manifest: m}), http.StatusCreated, "")

	id := view.ID("oci", "charts/web")
	a := p.st.Do(p.reps[0], http.MethodGet, "/api/v1/packages/"+id+"/versions/0.3.0", p.admin, nil)
	if a.Status != http.StatusOK {
		t.Fatalf("the chart's version: %d %s", a.Status, a.Body)
	}
	d := a.JSON(t)
	md := d["metadata"].(map[string]any)
	if md["kind"] != "helm-chart" || d["hasReadme"] != true {
		t.Fatalf("kind %v, hasReadme %v", md["kind"], d["hasReadme"])
	}
	configs, _ := md["configs"].(map[string]any)
	c, _ := configs[digestOf(m)].(map[string]any)
	chart, _ := c["chart"].(map[string]any)
	if chart["appVersion"] != "2.4.1" || chart["deprecated"] != true || len(chart["dependencies"].([]any)) != 1 {
		t.Errorf("the chart's facts: %v", c)
	}
	if labeled, _ := md["labeled"].(map[string]any); labeled["description"] != "The portal's web API." {
		t.Errorf("the chart's description fills the annotations': %v", md["labeled"])
	}
	if files, _ := md["chartFiles"].(map[string]any); files["values"] != "replicaCount: 2\n" {
		t.Errorf("the values: %v", md["chartFiles"])
	}
	// R-API-085: the Chart.yaml's deprecated is the version's deprecation, without a message.
	if dep, _ := d["deprecation"].(map[string]any); dep == nil || dep["scope"] != "version" || dep["message"] != nil {
		t.Errorf("the chart's deprecation: %v", d["deprecation"])
	}
	r := p.st.Do(p.reps[0], http.MethodGet, "/api/v1/packages/"+id+"/versions/0.3.0/readme", p.admin, nil)
	if r.Status != http.StatusOK || !strings.Contains(r.JSON(t)["html"].(string), "<table>") {
		t.Errorf("the README: %d %s", r.Status, r.Body)
	}

	// The lists name the kind, so that a chart's row offers Helm's commands (R-OCI-041, R-OCI-042): the package list by its latest
	// version, the list by content with include=kind by each entry's leading version.
	list := p.st.Do(p.reps[0], http.MethodGet, "/api/v1/packages?repository="+url.QueryEscape(project+"/"+repoLocal), p.admin, nil)
	for _, it := range list.JSON(t)["items"].([]any) {
		if pkg := it.(map[string]any); pkg["name"] == "charts/web" && (pkg["kind"] != "helm-chart" || pkg["deprecation"] == nil) {
			t.Errorf("the package list's kind and deprecation of the chart: %v %v", pkg["kind"], pkg["deprecation"])
		}
	}
	// The filter deprecated finds the chart by the statement OCIViewDeprecated (R-API-085).
	deprecated := p.st.Do(p.reps[0], http.MethodGet, "/api/v1/packages?deprecated=true&repository="+url.QueryEscape(project+"/"+repoLocal), p.admin, nil)
	if found, _ := deprecated.JSON(t)["items"].([]any); len(found) != 1 || found[0].(map[string]any)["name"] != "charts/web" {
		t.Errorf("the filter deprecated: %d %s", deprecated.Status, deprecated.Body)
	}
	vs := p.st.Do(p.reps[0], http.MethodGet, "/api/v1/packages/"+id+"/versions?group=content&include=kind", p.admin, nil)
	items, _ := vs.JSON(t)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["kind"] != "helm-chart" || items[0].(map[string]any)["deprecation"] == nil {
		t.Errorf("the list by content's kind: %d %s", vs.Status, vs.Body)
	}
}
