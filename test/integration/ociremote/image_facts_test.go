//go:build integration

package ociremote

import (
	"net/url"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_OCI_040_ARemotesConfigurationIsReadByItsFirstFetch: a client fetches a manifest before its config, so the version's page
// says the configuration is not read yet until the fetch of the config through the repository reads the stored blob and completes the
// row (R-OCI-040, Q-OCI-271); the stub's configuration carries the labels title and revision, which become the image's facts of the
// labels (R-OCI-039). One replica, one image of the stub (a manifest and three blobs of a few KiB); about 2 s.
func TestR_OCI_040_ARemotesConfigurationIsReadByItsFirstFetch(t *testing.T) {
	e := newEnv(t, options{})
	rep := e.start(1)[0]
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
	if c := config(); c["unreadable"] != "not-read" {
		t.Fatalf("before the config is fetched: %v, want not-read", c)
	}
	e.blob(rep, rAnon, "acme/a", img.Config.Digest)
	c := config()
	image, _ := c["image"].(map[string]any)
	labels, _ := image["labels"].([]any)
	if c["unreadable"] != nil || len(labels) != 2 {
		t.Fatalf("after the fetch of the config: %v", c)
	}
	if h, _ := image["history"].([]any); len(h) != 2 {
		t.Errorf("the history has %d lines, want the stub's 2", len(h))
	}
}
