package oci

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
	"github.com/guided-traffic/quader/internal/formats/oci/rows"
)

// R-OCI-040, Q-OCI-271: a push reads the image's configuration from the repository before it commits and records it with the
// manifest's row, beside the blobs in the manifest's order; a row whose configuration is not read is completed by the next fetch of the
// config, and a fetch of a layer reads nothing.
func TestR_OCI_040_APushReadsTheConfigAndAFetchCompletesARowWithout(t *testing.T) {
	fx := newFixture(t, formattest.HostOptions{})
	cfg := fx.blob("app", []byte(`{"os":"linux","config":{"Env":["A=1"]}}`))
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
	fx.wantStatus(fx.get("app/blobs/"+layer), http.StatusOK, "")
	if fx.twin.calls["PutConfigFacts"] != 0 || fx.twin.manifests[k].ConfigFacts != nil {
		t.Fatal("the fetch of a layer read a configuration")
	}
	fx.wantStatus(fx.get("app/blobs/"+cfg), http.StatusOK, "")
	if fx.twin.calls["PutConfigFacts"] != 1 || fx.twin.manifests[k].ConfigFacts == nil {
		t.Fatalf("the fetch of the config did not complete the row: %d writes", fx.twin.calls["PutConfigFacts"])
	}
	fx.wantStatus(fx.get("app/blobs/"+cfg), http.StatusOK, "")
	if fx.twin.calls["PutConfigFacts"] != 1 {
		t.Error("a configuration that is read is read again")
	}
}
