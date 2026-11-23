//go:build integration

package files

import (
	"net/http"
	"testing"
)

// ociPush pushes an image of one layer to the name as the user through the real routes: the config and the layer by monolithic uploads,
// then the manifest by tag.
func (f *uploadFixture) ociPush(replica int, user, repo, name, tag string, layer []byte) response {
	f.e.t.Helper()
	cfg := []byte("{}")
	for _, blob := range [][]byte{cfg, layer} {
		wantOCI(f.e.t, f.do(replica, user, http.MethodPost, repo+"/"+name+"/blobs/uploads/?digest="+digestOf(blob), blob), http.StatusCreated, "")
	}
	m := ociManifest(digestOf(cfg), digestOf(layer))
	return f.do(replica, user, http.MethodPut, repo+"/"+name+"/manifests/"+tag, m, "Content-Type", ociCT)
}

// The ownership rule of R-AUTHZ-018 is the real one here (the package writer's seam over authz.Ownership; an OCI repository enforces ownership by
// default). A deployer without `manage` pushes a new image: a session, a monolithic upload, a mount and the manifest are not refused, because a
// name with no version is the first version of its owner. The same deployer is refused on a name another principal owns, at every start and at the
// finish of a session that began before the name was owned, which answers as a session the caller may not continue: 404 BLOB_UPLOAD_UNKNOWN (Q-OCI-115).
func TestR_OCI_006_ADeployerPushesANewImageAndIsRefusedOnAnOwnedName(t *testing.T) {
	f := newUploadFixture(t)
	// a fresh name: every start is allowed to a plain deployer
	f.start(0, "writer2", "oci-local", "fresh/session")
	layer := []byte("a layer of a new image")
	wantOCI(t, f.do(1, "writer2", http.MethodPost, "oci-local/fresh/mono/blobs/uploads/?digest="+digestOf(layer), layer), http.StatusCreated, "")
	src := f.seed("oci-source", "base", []byte("a mounted layer"))
	wantOCI(t, f.do(2, "mover", http.MethodPost, "oci-local/fresh/mount/blobs/uploads/?mount="+src+"&from=it/oci-source/base", nil), http.StatusCreated, "")
	wantOCI(t, f.ociPush(0, "writer2", "oci-local", "fresh/image", "1", layer), http.StatusCreated, "")

	// a name the writer owns: the writer's push claims it
	wantOCI(t, f.ociPush(1, "writer", "oci-local", "owned/app", "1", layer), http.StatusCreated, "")
	late, _ := f.start(0, "writer2", "oci-local", "late/app") // a session of writer2 on a name that has no owner yet
	wantOCI(t, f.ociPush(2, "writer", "oci-local", "late/app", "1", layer), http.StatusCreated, "")
	for i := range f.replicas {
		wantOCI(t, f.do(i, "writer2", http.MethodPost, "oci-local/owned/app/blobs/uploads/", nil), http.StatusForbidden, "DENIED")
		wantOCI(t, f.do(i, "writer2", http.MethodPost, "oci-local/owned/app/blobs/uploads/?digest="+digestOf(layer), layer), http.StatusForbidden, "DENIED")
		wantOCI(t, f.do(i, "mover", http.MethodPost, "oci-local/owned/app/blobs/uploads/?mount="+src+"&from=it/oci-source/base", nil), http.StatusForbidden, "DENIED")
	}
	if f.blobItems("owned/app/_blobs/"+src) != 0 {
		t.Fatal("a mount into an owned name was linked")
	}
	// the finish of the session that began before the name was owned asks the rule again, with the body unread
	wantOCI(t, f.do(1, "writer2", http.MethodPut, late+"?digest="+digestOf([]byte("late bytes")), []byte("late bytes")), http.StatusNotFound, "BLOB_UPLOAD_UNKNOWN")
	if f.blobItems("late/app/_blobs/"+digestOf([]byte("late bytes"))) != 0 {
		t.Fatal("a blob was stored")
	}
	// the owner is not refused
	f.start(2, "writer", "oci-local", "owned/app")
}
