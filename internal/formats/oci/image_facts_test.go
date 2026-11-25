package oci

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
	"github.com/guided-traffic/quader/internal/formats/oci/rows"
)

// R-OCI-040, Q-OCI-271: a push reads the image's configuration from the repository before it commits and records it with the
// manifest's row, beside the blobs in the manifest's order; for a row whose configuration is not read, the commit of the config blob,
// whatever stores it, queues its read, the commit of a layer queues nothing, and a client's fetch reads nothing.
func TestR_OCI_040_APushReadsTheConfigAndTheConfigsCommitQueuesARowWithout(t *testing.T) {
	fx := newFixture(t, formattest.HostOptions{})
	cfgBytes := []byte(`{"os":"linux","config":{"Env":["A=1"]}}`)
	cfg := fx.blob("app", cfgBytes)
	layer := fx.blob("app", []byte("lay"))
	m := imageManifestBytes(cfg, layer)
	fx.wantStatus(fx.put("app/manifests/1", m, ctImage), http.StatusCreated, "")

	sum, _ := manifest.ParseDigest(sha(m))
	k := mkey{fx.h.Ref().ID, "app", sum}
	row := fx.twin.manifests[k]
	var facts rows.ConfigFacts
	if json.Unmarshal(row.ConfigFacts, &facts) != nil || facts.Image == nil || facts.Image.OS != "linux" || len(facts.Image.Env) != 1 {
		t.Fatalf("the push recorded the configuration %s", row.ConfigFacts)
	}
	var blobs rows.ImageBlobs
	if json.Unmarshal(row.Blobs, &blobs) != nil || blobs.Config == nil || blobs.Config.Digest != cfg || len(blobs.Layers) != 1 ||
		blobs.Layers[0].Digest != layer {
		t.Fatalf("the blobs: %s", row.Blobs)
	}

	// A row whose configuration is not read yet, as a remote's fill leaves it.
	row.ConfigFacts = nil
	fx.twin.manifests[k] = row
	commit := func(digest string, data []byte) {
		t.Helper()
		s := sha256.Sum256(data)
		ev := formats.CommitEvent{
			Path: formats.Path{Path: BlobPath("app", digest)}, Digests: formats.Digests{SHA256: s[:]},
			Repo: fx.h.Ref(), Data: fx.twin,
		}
		if err := fx.f.OnCommit(context.Background(), nil, ev); err != nil {
			t.Fatalf("the commit of %s: %v", digest, err)
		}
	}
	commit(layer, []byte("lay"))
	if len(fx.twin.configReads) != 0 {
		t.Fatalf("the commit of a layer queued %v", fx.twin.configReads)
	}
	fx.wantStatus(fx.get("app/blobs/"+cfg), http.StatusOK, "")
	if len(fx.twin.configReads) != 0 || fx.twin.manifests[k].ConfigFacts != nil {
		t.Fatal("a client's fetch of the config read it")
	}
	commit(cfg, cfgBytes)
	if len(fx.twin.configReads) != 1 || fx.twin.configReads[0] != k {
		t.Fatalf("the commit of the config queued %v, want the manifest's read", fx.twin.configReads)
	}
}
