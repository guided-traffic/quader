//go:build integration

package ocilocal

import (
	"context"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// TestR_SIG_025_AnImagesDeletionTakesItsSignatures: an image with a referrer and a cosign .sig tag, as a push leaves them; the delete of
// the image's manifest takes the referrer, the .sig tag and the manifest it points at, and with them their attachments in the signature
// store; a .sig manifest a second tag names stays. The image scanning's selection names the image and no signature manifest
// (R-SIG-024). Integration, about 2 s, bytes.
func TestR_SIG_025_AnImagesDeletionTakesItsSignatures(t *testing.T) {
	e := newEnv(t)
	repo := e.repoID(repoLocal)
	layer := e.put(repoLocal, "app/_blobs/"+dg(sha256Of("layer")), "layer", "application/octet-stream")
	img := e.manifestOf(repoLocal, "app", `{"schemaVersion":2,"mediaType":"`+manifest+`","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"`+dg(layer)+`","size":5},"layers":[]}`, manifest, nil, "", ref{layer, "blob"})
	referrer := e.manifestOf(repoLocal, "app", `{"schemaVersion":2,"mediaType":"`+manifest+`","artifactType":"application/vnd.cncf.notary.signature","config":{"mediaType":"application/vnd.oci.empty.v1+json","digest":"`+dg(layer)+`","size":5},"layers":[{"mediaType":"application/jose+json","digest":"`+dg(layer)+`","size":5}],"subject":{"mediaType":"`+manifest+`","digest":"`+dg(img.digest)+`","size":10}}`,
		manifest, &img.digest, "application/vnd.cncf.notary.signature")
	sigBody := `{"schemaVersion":2,"mediaType":"` + manifest + `","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + dg(layer) + `","size":5},"layers":[{"mediaType":"application/vnd.dev.cosign.simplesigning.v1+json","digest":"` + dg(layer) + `","size":5}]}`
	sig := e.manifestOf(repoLocal, "app", sigBody, manifest, nil, "application/vnd.oci.image.config.v1+json", ref{layer, "blob"})
	sigTag := "sha256-" + hex.EncodeToString(img.digest[:]) + ".sig"
	e.put(repoLocal, "app/_tags/"+sigTag, sigBody, manifest)
	e.tag(repoLocal, "app", "1.0", `{"schemaVersion":2,"mediaType":"`+manifest+`","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"`+dg(layer)+`","size":5},"layers":[]}`)

	attachments := func() int64 {
		return e.st.Number(`SELECT count(*) FROM signature_attachments WHERE repo_id = $1`, repo)
	}
	if n := attachments(); n != 2 {
		t.Fatalf("%d attachments after the push, want the referrer's and the .sig tag's", n)
	}

	// The image scanning's selection: the image only (R-SIG-024).
	ctx := context.Background()
	if err := e.db.InTx(ctx, func(tx meta.Tx) error {
		ptx, _ := postgres.Unwrap(tx)
		_, err := q.New(ptx).VulnImageDiscover(ctx, q.VulnImageDiscoverParams{MaxRows: 100})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := e.strings(`SELECT encode(digest, 'hex') FROM vuln_image_scan WHERE repo_id = $1 ORDER BY 1`, repo); len(got) != 1 || got[0] != hex.EncodeToString(img.digest[:]) {
		t.Errorf("the image scanning selected %v, want the image %x alone", got, img.digest)
	}

	if a := e.st.Do(e.r, http.MethodDelete, "/files/"+project+"/"+repoLocal+"/"+img.path(), e.tokAdmin, nil); a.Status/100 != 2 {
		t.Fatalf("DELETE: %d %s", a.Status, a.Body)
	}
	for _, p := range []string{img.path(), referrer.path(), sig.path(), "app/_tags/" + sigTag} {
		if e.itemCount(repoLocal, p) != 0 {
			t.Errorf("%s stayed", p)
		}
	}
	if n := attachments(); n != 0 {
		t.Errorf("%d attachments stayed", n)
	}
}

// TestR_SIG_025_ASignatureManifestASecondTagNamesStays: the .sig tag goes with its image, and the manifest it points at stays while a
// second tag names it. Integration, about 1 s, bytes.
func TestR_SIG_025_ASignatureManifestASecondTagNamesStays(t *testing.T) {
	e := newEnv(t)
	layer := e.put(repoLocal, "app/_blobs/"+dg(sha256Of("layer")), "layer", "application/octet-stream")
	img := e.manifestOf(repoLocal, "app", `{"image":1}`, manifest, nil, "", ref{layer, "blob"})
	sigBody := `{"cosign":1}`
	sig := e.manifestOf(repoLocal, "app", sigBody, manifest, nil, "")
	sigTag := "sha256-" + hex.EncodeToString(img.digest[:]) + ".sig"
	e.put(repoLocal, "app/_tags/"+sigTag, sigBody, manifest)
	e.tag(repoLocal, "app", "keep", sigBody)
	if a := e.st.Do(e.r, http.MethodDelete, "/files/"+project+"/"+repoLocal+"/"+img.path(), e.tokAdmin, nil); a.Status/100 != 2 {
		t.Fatalf("DELETE: %d %s", a.Status, a.Body)
	}
	if e.itemCount(repoLocal, "app/_tags/"+sigTag) != 0 {
		t.Error("the .sig tag stayed")
	}
	if e.itemCount(repoLocal, sig.path()) != 1 || e.itemCount(repoLocal, "app/_tags/keep") != 1 {
		t.Error("the manifest a second tag names went")
	}
}
