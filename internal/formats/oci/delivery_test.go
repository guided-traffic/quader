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
