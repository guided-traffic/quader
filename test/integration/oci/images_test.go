//go:build integration

package oci

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/packages/view"
)

// TestR_API_029_AMovedTagMovesBetweenImagesOfTheListByContent: a local image's tags are version rows that store the digest of their
// manifest (package_versions.document_sha256, written by the host's package writer, ADR-280), so the list by content (group=content,
// Q-API-432) groups them: `1.0` and `sha-a` pushed on one manifest are one entry led by `1.0`; when `1.0` is pushed again on another
// manifest its version row stays and its item's content changes in place, and the digest follows (the trigger items_document_follow):
// `1.0` leads the new image, `sha-a` the old. One replica, three tiny pushes; about 2 s.
func TestR_API_029_AMovedTagMovesBetweenImagesOfTheListByContent(t *testing.T) {
	p := newProto(t, protoOptions{replicas: 1})
	a, b := newImage("grouped-a"), newImage("grouped-b")
	for _, push := range []struct {
		tag string
		img image
	}{{"1.0", a}, {"sha-a", a}} {
		want(t, p.pushImage(p.as("admin"), repoLocal, "app", push.tag, push.img), http.StatusCreated, "")
	}
	type entry struct {
		Version string   `json:"version"`
		Digest  string   `json:"digest"`
		Aliases []string `json:"aliases"`
	}
	list := func() []entry {
		t.Helper()
		var page struct {
			Items []entry `json:"items"`
		}
		ans := p.st.Do(p.reps[0], http.MethodGet, "/api/v1/packages/"+view.ID("oci", "app")+"/versions?group=content", p.admin, nil)
		if ans.Status != http.StatusOK || json.Unmarshal(ans.Body, &page) != nil {
			t.Fatalf("the list by content: %d %s", ans.Status, ans.Body)
		}
		return page.Items
	}
	if got := list(); len(got) != 1 || got[0].Version != "1.0" || got[0].Digest != a.digest() || !slices.Equal(got[0].Aliases, []string{"sha-a"}) {
		t.Fatalf("after two tags of one image: %+v", got)
	}
	want(t, p.pushImage(p.as("admin"), repoLocal, "app", "1.0", b), http.StatusCreated, "")
	got := list()
	if len(got) != 2 || got[0].Version != "1.0" || got[0].Digest != b.digest() || len(got[0].Aliases) != 0 ||
		got[1].Version != "sha-a" || got[1].Digest != a.digest() || len(got[1].Aliases) != 0 {
		t.Fatalf("after 1.0 moved: %+v", got)
	}
	if n := p.st.Number(`SELECT count(*) FROM package_versions WHERE document_sha256 IS NULL`); n != 0 {
		t.Errorf("%d version rows of the image store no digest", n)
	}
}
