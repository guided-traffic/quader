package oci

import (
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/formats/oci/rows"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
)

// TestR_OCI_038_AManifestByDigestFollowsTheTagsThatNameIt: in an image with a version rule, a manifest by digest is delivered when a
// delivered tag's stored document is the manifest or an index that names it, and refused when only a refused tag names it; the
// referrers of the refused manifest answer the refusal too (R-OCI-038, Q-CUR-10).
func TestR_OCI_038_AManifestByDigestFollowsTheTagsThatNameIt(t *testing.T) {
	kept := imageManifestBytes(sha([]byte("cfg")), sha([]byte("kept layer")))
	refused := imageManifestBytes(sha([]byte("cfg")), sha([]byte("refused layer")))
	child := imageManifestBytes(sha([]byte("cfg")), sha([]byte("child layer")))
	index := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`)
	m := &formattest.MemRemote{
		Docs: map[string][]byte{TaglistPath("app"): []byte(`{"name":"app","tags":["legacy","multi","stable"]}`)},
		Artifacts: map[string][]byte{
			ManifestPath("app", sha(kept)): kept, ManifestPath("app", sha(refused)): refused, ManifestPath("app", sha(child)): child,
		},
	}
	fx := newFixture(t, formattest.HostOptions{Class: formats.Remote, Remote: m, Delivery: &formattest.DeliveryRules{
		Mode: rc.AllExceptBlocked, Rules: []rules.Rule{{Kind: rc.KindVersion, Name: "app", Version: "legacy", Effect: rc.Block}},
	}})
	fx.twin.putTag("app", "stable", hexOf(sha(kept)))
	fx.twin.putTag("app", "legacy", hexOf(sha(refused)))
	fx.twin.putTag("app", "multi", hexOf(sha(index)))
	fx.twin.putManifest(rows.Manifest{Name: "app", Digest: hexOf(sha(index))}, rows.Reference{Target: hexOf(sha(child)), Kind: rows.KindManifest})

	fx.wantStatus(fx.get("app/manifests/"+sha(kept)), http.StatusOK, "")
	fx.wantStatus(fx.get("app/manifests/"+sha(child)), http.StatusOK, "")
	for _, p := range []string{"app/manifests/" + sha(refused), "app/referrers/" + sha(refused)} {
		resp := fx.get(p)
		fx.wantStatus(resp, http.StatusForbidden, "DENIED")
		if !strings.Contains(resp.Body.String(), "is not allowed in") || resp.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %q %s, want the refusal's text with no-store", p, resp.Header().Get("Cache-Control"), resp.Body)
		}
	}
}

// TestR_CUR_026_ATagThatIsNoVersionIsOutside: under a range of the image's allow (R-CUR-026, Q-CUR-223, Q-CUR-230) the tags list
// lists the tags inside it, a tag that is no version of the tag order (latest) lies outside it with a tag above it, a manifest by
// digest follows the tags that name it with the reason outsideRange, and latest is switched on alone by a version rule of its own.
func TestR_CUR_026_ATagThatIsNoVersionIsOutside(t *testing.T) {
	kept := imageManifestBytes(sha([]byte("cfg")), sha([]byte("kept layer")))
	above := imageManifestBytes(sha([]byte("cfg")), sha([]byte("above layer")))
	m := &formattest.MemRemote{
		Docs:      map[string][]byte{TaglistPath("app"): []byte(`{"name":"app","tags":["1.4.2","2.0.0","latest"]}`)},
		Artifacts: map[string][]byte{ManifestPath("app", sha(kept)): kept, ManifestPath("app", sha(above)): above},
	}
	rng := rc.Range{From: "1.0.0", To: "2.0.0"}
	fx := newFixture(t, formattest.HostOptions{Class: formats.Remote, Remote: m, Delivery: &formattest.DeliveryRules{
		Mode: rc.AllExceptBlocked, Rules: []rules.Rule{{Kind: rc.KindPackage, Name: "app", Effect: rc.Allow, Range: rng}},
	}})
	fx.twin.putTag("app", "1.4.2", hexOf(sha(kept)))
	fx.twin.putTag("app", "2.0.0", hexOf(sha(above)))
	fx.twin.putTag("app", "latest", hexOf(sha(above)))

	resp := fx.get("app/tags/list")
	fx.wantStatus(resp, http.StatusOK, "")
	if body := resp.Body.String(); !strings.Contains(body, `"1.4.2"`) || strings.Contains(body, `"2.0.0"`) || strings.Contains(body, "latest") {
		t.Errorf("the tags list under the range is %s, want 1.4.2 alone", body)
	}
	fx.wantStatus(fx.get("app/manifests/"+sha(kept)), http.StatusOK, "")
	for _, p := range []string{"app/manifests/" + sha(above), "app/manifests/latest", "app/manifests/2.0.0"} {
		resp := fx.get(p)
		fx.wantStatus(resp, http.StatusForbidden, "DENIED")
		if !strings.Contains(resp.Body.String(), "outside the versions a rule of the repository allows") {
			t.Errorf("%s: %s, want the refusal with the reason outsideRange", p, resp.Body)
		}
	}

	set := fx.h.Repo().Delivery()
	if set == nil || !set.NarrowsVersions("app") {
		t.Fatal("an image with a range decides its versions apart from the image")
	}
	withLatest, err := rules.Compile(rc.AllExceptBlocked, 1, []rules.Rule{
		{Kind: rc.KindPackage, Name: "app", Effect: rc.Allow, Range: rng}, {Kind: rc.KindVersion, Name: "app", Version: "latest", Effect: rc.Allow},
	}, nil, ociCanon())
	if err != nil {
		t.Fatal(err)
	}
	if d := withLatest.Decide("app", "latest"); !d.Delivered || d.By.Kind != rc.KindVersion {
		t.Errorf("latest with a version rule of its own is %+v, want delivered by it", d)
	}
	if d := withLatest.Inherited("app", "latest"); d.Delivered || d.Reason != rc.ReasonOutsideRange {
		t.Errorf("latest inherits %+v, want outsideRange: its switch writes the version rule", d)
	}
}

// ociCanon is the rule set's canon of the format: its name rule, its version canon and its tag order.
func ociCanon() rules.Canon {
	d := New().Descriptor()
	return rules.Canon{Name: d.Names.Normalize, Version: d.VersionCanon.Canonical, Order: New().View()}
}
