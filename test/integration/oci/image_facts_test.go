//go:build integration

package oci

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/guided-traffic/quader/internal/packages/view"
)

// TestR_OCI_040_AnImagesFactsStandOnItsPageForWhoMayReadItsConfig: a push reads the image's configuration from the repository before
// it commits (R-OCI-040), so the version's page shows the configuration Buildx wrote (the fixture of the reader's unit test), the
// layers in the manifest's order and the image's facts: the annotation's description wins over the label's, a label gives what no
// annotation gives (R-OCI-039), and the word for the artifact is image (R-OCI-041). A person whose grant excludes the blobs' paths reads
// the version, its layers and its annotations, and none of the configuration or the labels (Q-OCI-273). The package list carries the
// annotation's description, and a second tag of the image stands as the version's other tag (R-OCI-041). One replica, one push of
// three blobs of bytes and a second tag; about 2 s.
func TestR_OCI_040_AnImagesFactsStandOnItsPageForWhoMayReadItsConfig(t *testing.T) {
	p := newProto(t, protoOptions{replicas: 1, setup: func(p *proto) {
		p.pathUser("noblobs", []string{"read"}, nil, []string{"**/_blobs/**"})
	}})
	cfg, err := os.ReadFile("../../../internal/formats/oci/manifest/testdata/buildx-config.json")
	if err != nil {
		t.Fatal(err)
	}
	layer := []byte("one layer of bytes")
	m, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": manifestType,
		"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": digestOf(cfg), "size": len(cfg)},
		"layers": []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": digestOf(layer), "size": len(layer)}},
		"annotations": map[string]string{
			"org.opencontainers.image.description": "From the annotation",
			"org.opencontainers.image.url":         "https://example.com/app",
		},
	})
	img := image{config: cfg, layers: [][]byte{layer}, manifest: m}
	want(t, p.pushImage(p.as("admin"), repoLocal, "facts/app", "1.0", img), http.StatusCreated, "")

	id := view.ID("oci", "facts/app")
	detail := func(who string) map[string]any {
		t.Helper()
		tok := p.admin
		if who != "admin" {
			tok = p.quaderToken[who]
		}
		a := p.st.Do(p.reps[0], http.MethodGet, "/api/v1/packages/"+id+"/versions/1.0", tok, nil)
		if a.Status != http.StatusOK {
			t.Fatalf("%s reads the version: %d %s", who, a.Status, a.Body)
		}
		return a.JSON(t)["metadata"].(map[string]any)
	}

	md := detail("admin")
	if md["kind"] != "image" || md["description"] != "From the annotation" || md["homepage"] != "https://example.com/app" {
		t.Errorf("the image's facts: kind %v, description %v, homepage %v", md["kind"], md["description"], md["homepage"])
	}
	blobs, _ := md["blobs"].(map[string]any)
	own, _ := blobs[img.digest()].(map[string]any)
	if layers, _ := own["layers"].([]any); len(layers) != 1 || layers[0].(map[string]any)["digest"] != digestOf(layer) {
		t.Errorf("the layers: %v", own)
	}
	configs, _ := md["configs"].(map[string]any)
	c, _ := configs[img.digest()].(map[string]any)
	image, _ := c["image"].(map[string]any)
	env, _ := image["env"].([]any)
	if len(env) != 3 || env[2].(map[string]any)["value"] != "hello world" || image["user"] != "1000:1000" {
		t.Fatalf("the configuration read at the push: %v", c)
	}
	if h, _ := image["history"].([]any); len(h) != 11 {
		t.Errorf("the history has %d lines, want 11", len(h))
	}
	labeled, _ := md["labeled"].(map[string]any)
	if labeled["licenses"] != "Apache-2.0" || labeled["repositoryUrl"] != "https://example.com/quader/fixture" || labeled["description"] != nil {
		t.Errorf("the labels give what the annotations do not: %v", labeled)
	}

	md = detail("noblobs")
	if _, ok := md["configs"]; ok {
		t.Error("a person who may not read the config blob reads the configuration")
	}
	if _, ok := md["labeled"]; ok {
		t.Error("a person who may not read the config blob reads the labels' facts")
	}
	if md["description"] != "From the annotation" || md["blobs"] == nil {
		t.Errorf("the annotations and the layers stand for every reader of the version: %v", md)
	}

	// R-OCI-041: the other tags of the image stand with the version.
	want(t, p.putManifest(p.next(), p.as("admin"), repoLocal, "facts/app", "latest", manifestType, m), http.StatusCreated, "")
	a := p.st.Do(p.reps[0], http.MethodGet, "/api/v1/packages/"+id+"/versions/1.0", p.admin, nil)
	if aliases, _ := a.JSON(t)["aliases"].([]any); len(aliases) != 1 || aliases[0] != "latest" {
		t.Errorf("the other tags of the image: %v", a.JSON(t)["aliases"])
	}

	list := p.st.Do(p.reps[0], http.MethodGet, "/api/v1/packages?repository="+url.QueryEscape(project+"/"+repoLocal), p.admin, nil)
	var page struct {
		Items []struct {
			Name        string  `json:"name"`
			Description *string `json:"description"`
		} `json:"items"`
	}
	if list.Status != http.StatusOK || json.Unmarshal(list.Body, &page) != nil {
		t.Fatalf("the package list: %d %s", list.Status, list.Body)
	}
	for _, it := range page.Items {
		if it.Name == "facts/app" && (it.Description == nil || *it.Description != "From the annotation") {
			t.Errorf("the list's description: %v", it.Description)
		}
	}
}
