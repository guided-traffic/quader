//go:build integration

package oci

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/packages/view"
	"github.com/guided-traffic/quader/test/integration/testenv"
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

// TestR_OCI_040_AnImageStoredBeforeItsFactsIsDescribedFromWhatTheRepositoryHolds: a manifest row stored before its blobs and its
// configuration were kept (migration 00132) shows neither; the task remote-describe, which a worker queues when it starts, queues the job
// oci-describe for it, which reads the stored manifest and then the stored configuration and completes the row without a client's
// request (R-OCI-040, Q-OCI-271); a run over the described store queues nothing. One web replica and one worker, one push of a few
// bytes; about 3 s.
func TestR_OCI_040_AnImageStoredBeforeItsFactsIsDescribedFromWhatTheRepositoryHolds(t *testing.T) {
	p := newProto(t, protoOptions{replicas: 1})
	cfg := []byte(`{"architecture":"amd64","os":"linux","config":{"Env":["MODE=old"],"Labels":{"org.opencontainers.image.title":"old"}}}`)
	layer := []byte("a layer stored before")
	m, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": manifestType,
		"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": digestOf(cfg), "size": len(cfg)},
		"layers": []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": digestOf(layer), "size": len(layer)}},
	})
	img := image{config: cfg, layers: [][]byte{layer}, manifest: m}
	want(t, p.pushImage(p.as("admin"), repoLocal, "facts/old", "1.0", img), http.StatusCreated, "")
	// The row as a manifest stored before the change has it.
	p.st.Exec(`UPDATE oci_manifests SET blobs = NULL, config_digest = NULL, config_facts = NULL WHERE name = 'facts/old'`)

	id := view.ID("oci", "facts/old")
	metadata := func() map[string]any {
		t.Helper()
		a := p.st.Do(p.reps[0], http.MethodGet, "/api/v1/packages/"+id+"/versions/1.0", p.admin, nil)
		if a.Status != http.StatusOK {
			t.Fatalf("the version: %d %s", a.Status, a.Body)
		}
		return a.JSON(t)["metadata"].(map[string]any)
	}
	if md := metadata(); md["blobs"] != nil || md["configs"] != nil {
		t.Fatalf("a row stored before shows its facts: %v", md)
	}

	p.st.Start(t, testenv.ReplicaOptions{Name: "worker", Role: "worker"})
	var md map[string]any
	testenv.WaitFor(t, "the description of the image stored before", 30*time.Second, func() bool {
		md = metadata()
		configs, _ := md["configs"].(map[string]any)
		c, _ := configs[img.digest()].(map[string]any)
		return c != nil && c["image"] != nil
	})
	blobs, _ := md["blobs"].(map[string]any)
	if own, _ := blobs[img.digest()].(map[string]any); own == nil || len(own["layers"].([]any)) != 1 {
		t.Errorf("the layers read from the stored manifest: %v", blobs)
	}
	image := md["configs"].(map[string]any)[img.digest()].(map[string]any)["image"].(map[string]any)
	if env, _ := image["env"].([]any); len(env) != 1 || env[0].(map[string]any)["value"] != "old" {
		t.Errorf("the configuration read from the stored blob: %v", image)
	}

	// A run over the described store finds nothing left.
	testenv.WaitFor(t, "the task to be idle", 60*time.Second, func() bool {
		return p.st.Number(`SELECT count(*) FROM job WHERE kind = 'remote-describe' AND state IN ('queued', 'running')`) == 0
	})
	before := p.st.Number(`SELECT count(*) FROM job WHERE kind = 'oci-describe'`)
	if a := p.st.Do(p.reps[0], http.MethodPost, "/api/v1/system/tasks/remote-describe/run", p.admin, nil); a.Status != http.StatusAccepted {
		t.Fatalf("the run of remote-describe: %d %s", a.Status, a.Body)
	}
	testenv.WaitFor(t, "the run of remote-describe", 60*time.Second, func() bool {
		return p.st.Number(`SELECT count(*) FROM job WHERE kind = 'remote-describe' AND state IN ('queued', 'running')`) == 0
	})
	if after := p.st.Number(`SELECT count(*) FROM job WHERE kind = 'oci-describe'`); after != before {
		t.Errorf("a run over the described store queued %d jobs", after-before)
	}
}
