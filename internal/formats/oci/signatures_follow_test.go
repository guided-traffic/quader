package oci

import (
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/formats/oci/rows"
)

// TestR_SIG_025_AnAttachmentFollowsItsImage: deleting an image's manifest removes its .sig tag and the manifest the tag points at; a
// .sig manifest that a second tag also names stays with that tag; deleting only a tag of the image leaves the image and its
// attachments. Unit, milliseconds, the in-memory host and manifests built by the test.
func TestR_SIG_025_AnAttachmentFollowsItsImage(t *testing.T) {
	newHost := func(t *testing.T) *formattest.Host {
		t.Helper()
		h := formattest.NewHost(New(), formattest.HostOptions{Data: newTwin(), Resolver: proceedResolver{}})
		if err := seed(h); err != nil {
			t.Fatal(err)
		}
		return h
	}
	sigTag := "sha256-" + hexDigits(seedDigest) + ".sig"
	sig := signatureManifest("", descOf("application/vnd.oci.image.config.v1+json", seedConfig), nil, nil, descOf(layerSimpleSigning, sigPayload1))
	pushSig := func(t *testing.T, h *formattest.Host, tags ...string) {
		t.Helper()
		h.PutItem(BlobPath("app", sha(sigPayload1)), "application/octet-stream", sigPayload1)
		for _, tag := range tags {
			if err := pushManifest(h, "app/manifests/"+tag, sig); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("the image's deletion takes its .sig tag and the manifest it points at", func(t *testing.T) {
		h := newHost(t)
		pushSig(t, h, sigTag)
		if resp := del(t, h, "app/manifests/"+seedDigest); resp.Code != http.StatusAccepted {
			t.Fatalf("DELETE: %d %s", resp.Code, resp.Body)
		}
		if stored(h, TagPath("app", sigTag)) || stored(h, ManifestPath("app", sha(sig))) {
			t.Error("the .sig tag or its manifest stayed")
		}
	})
	t.Run("a .sig manifest a second tag names stays", func(t *testing.T) {
		h := newHost(t)
		pushSig(t, h, sigTag, "keep")
		if resp := del(t, h, "app/manifests/"+seedDigest); resp.Code != http.StatusAccepted {
			t.Fatalf("DELETE: %d %s", resp.Code, resp.Body)
		}
		if stored(h, TagPath("app", sigTag)) {
			t.Error("the .sig tag stayed")
		}
		if !stored(h, ManifestPath("app", sha(sig))) || !stored(h, TagPath("app", "keep")) {
			t.Error("the manifest another tag names went")
		}
	})
	t.Run("deleting a tag of the image leaves the image and its attachments", func(t *testing.T) {
		h := newHost(t)
		pushSig(t, h, sigTag)
		if resp := del(t, h, "app/manifests/1"); resp.Code != http.StatusAccepted {
			t.Fatalf("DELETE of the tag: %d %s", resp.Code, resp.Body)
		}
		if !stored(h, ManifestPath("app", seedDigest)) || !stored(h, TagPath("app", sigTag)) || !stored(h, ManifestPath("app", sha(sig))) {
			t.Error("the image or its attachment went with a tag")
		}
	})
}

// TestR_SIG_024_BuildKitsAttestationEntriesAreNoPlatform: the version's data lists the platforms of an index without BuildKit's
// attestation entries (unknown/unknown), which stand apart as the image's attestations. Unit, microseconds.
func TestR_SIG_024_BuildKitsAttestationEntriesAreNoPlatform(t *testing.T) {
	md := tagMetadata(rows.TagView{Digest: digestN(1), MediaType: "application/vnd.oci.image.index.v1+json", Children: []rows.Child{
		{Platform: "linux/amd64", Digest: digestN(2), Stored: true, MediaType: manifestType, ContentSize: 10},
		{Platform: rows.AttestationPlatform, Digest: digestN(3)},
	}})
	platforms, _ := md["platforms"].([]map[string]any)
	attestations, _ := md["attestations"].([]map[string]any)
	if len(platforms) != 1 || platforms[0]["platform"] != "linux/amd64" {
		t.Errorf("platforms %v, want linux/amd64 alone", platforms)
	}
	if len(attestations) != 1 || attestations[0]["digest"] != digestText(digestN(3)) || attestations[0]["platform"] != nil {
		t.Errorf("attestations %v, want the entry of unknown/unknown without a platform", attestations)
	}
}
