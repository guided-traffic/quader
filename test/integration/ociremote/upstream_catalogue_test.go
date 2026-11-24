//go:build integration

package ociremote

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_CUR_009_TheCatalogueSaysStoredOfATagARemotePulledThrough: an OCI remote holds a tag as its cache entry and no version row
// (R-OCI-036), so the catalogue's stored state reads the tags the repository lists, as its package screens do (Q-CUR-61). A tag a client
// only checked with a HEAD is not stored; after its GET the lookup of the image says the tag whole and the image partial, and a tag the
// upstream lists and nobody pulled stays none. Integration tier, about 3 s, the stub's image acme/b.
func TestR_CUR_009_TheCatalogueSaysStoredOfATagARemotePulledThrough(t *testing.T) {
	e := newEnv(t, options{})
	rep := e.start(1)[0]
	lookup := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + rAnon + "/upstream/package?name=" + url.QueryEscape("acme/b")
	state := func() (pkg string, tags map[string]string) {
		t.Helper()
		a := e.st.Do(rep, http.MethodGet, lookup, e.admin, nil)
		if a.Status != http.StatusOK {
			t.Fatalf("the lookup of acme/b: %d %s", a.Status, a.Body)
		}
		p := a.JSON(t)
		tags = map[string]string{}
		for _, v := range p["versions"].([]any) {
			m := v.(map[string]any)
			tags[m["version"].(string)], _ = m["stored"].(string)
		}
		pkg, _ = p["stored"].(string)
		return pkg, tags
	}
	pkg, tags := state()
	if _, ok := tags["latest"]; !ok || len(tags) < 2 {
		t.Fatalf("the upstream lists the tags %v of acme/b, want latest and at least one more", tags)
	}

	if a := e.do(rep, http.MethodHead, rAnon, "acme/b/manifests/latest"); a.Status != http.StatusOK {
		t.Fatalf("HEAD of the tag: %d", a.Status)
	}
	if pkg, tags = state(); pkg != "none" || tags["latest"] != "none" {
		t.Errorf("after a HEAD: the image %q, latest %q, want none and none", pkg, tags["latest"])
	}

	a := e.get(rep, rAnon, "acme/b/manifests/latest")
	if a.Status != http.StatusOK {
		t.Fatalf("GET of the tag: %d %s", a.Status, a.Body)
	}
	e.fetched(rAnon, a.Header.Get("Docker-Content-Digest"))
	pkg, tags = state()
	if pkg != "partial" || tags["latest"] != "whole" {
		t.Errorf("after the pull: the image %q, latest %q, want partial and whole", pkg, tags["latest"])
	}
	for tag, st := range tags {
		if tag != "latest" && st != "none" {
			t.Errorf("the tag %s nobody pulled is %q, want none", tag, st)
		}
	}
}
