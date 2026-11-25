//go:build integration

package ociremote

import (
	"net/url"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_OCI_040_ARemotesConfigurationIsReadOnceItIsStored: a client fetches a manifest before its config, so the version's page says
// the configuration is read once the image is pulled through the repository (not-stored); the commit of the config the client then
// fetches queues the job oci-describe, which reads the stored blob and completes the row (R-OCI-040, Q-OCI-271), and the page then
// shows it. The stub's configuration carries the labels title and revision, which become the image's facts of the labels
// (R-OCI-039). One web replica and one worker, one image of the stub (a manifest and three blobs of a few KiB); about 3 s.
func TestR_OCI_040_ARemotesConfigurationIsReadOnceItIsStored(t *testing.T) {
	e := newEnv(t, options{})
	rep := e.start(1)[0]
	e.st.Start(t, testenv.ReplicaOptions{Name: "worker", Role: "worker"})
	img, err := e.stub.Image("acme/a", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	a := e.get(rep, rAnon, "acme/a/manifests/1.0")
	if a.Status != 200 {
		t.Fatalf("GET of the tag: %d", a.Status)
	}
	digest := a.Header.Get("Docker-Content-Digest")
	e.fetched(rAnon, digest)
	id := e.listing(rep, e.reader, rAnon)["acme/a"]["id"].(string)
	config := func() map[string]any {
		t.Helper()
		d := e.st.Do(rep, "GET", "/api/v1/packages/"+id+"/versions/1.0?repository="+url.QueryEscape(testenv.DefaultProject+"/"+rAnon), e.reader, nil)
		if d.Status != 200 {
			t.Fatalf("the tag's page: %d %s", d.Status, d.Body)
		}
		md := d.JSON(t)["metadata"].(map[string]any)
		configs, _ := md["configs"].(map[string]any)
		c, _ := configs[digest].(map[string]any)
		if c == nil {
			t.Fatalf("the page names no configuration of %s: %v", digest, md)
		}
		return c
	}
	if c := config(); c["unreadable"] != "not-stored" {
		t.Fatalf("before the config is fetched: %v, want not-stored", c)
	}
	e.blob(rep, rAnon, "acme/a", img.Config.Digest)
	var c map[string]any
	testenv.WaitFor(t, "the read of the stored configuration", 30*time.Second, func() bool {
		c = config()
		return c["unreadable"] != "not-read"
	})
	image, _ := c["image"].(map[string]any)
	labels, _ := image["labels"].([]any)
	if c["unreadable"] != nil || len(labels) != 2 {
		t.Fatalf("after the fetch of the config: %v", c)
	}
	if h, _ := image["history"].([]any); len(h) != 2 {
		t.Errorf("the history has %d lines, want the stub's 2", len(h))
	}

	// A configuration whose download the serve point would not serve the caller is held, with the code of that refusal (R-OCI-040):
	// the remote's delivery rules block the image, as they block a pull of its config.
	e.st.MustPutDeliveryRule(rep, e.admin, rAnon, testenv.PackageRule("acme/a", "block"))
	e.st.WaitDelivered(t, rep, e.admin, rAnon, "acme/a", "", false)
	d := e.st.Do(rep, "GET", "/api/v1/packages/"+id+"/versions/1.0?repository="+url.QueryEscape(testenv.DefaultProject+"/"+rAnon), e.reader, nil)
	md, _ := d.JSON(t)["metadata"].(map[string]any)
	held, _ := md["contentHeld"].(map[string]any)
	if d.Status != 200 || md["configs"] != nil || md["labeled"] != nil || held["code"] != "not_allowed_in_repository" {
		t.Errorf("a blocked image's page: %d, configs %v, contentHeld %v", d.Status, md["configs"] != nil, md["contentHeld"])
	}
	if b := e.get(rep, rAnon, "acme/a/blobs/"+img.Config.Digest); b.Status != 403 {
		t.Errorf("the pull of the config the page holds: %d, want the refusal", b.Status)
	}
}
