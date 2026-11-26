package formathost

import (
	"net/http/httptest"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/generic"
	"github.com/guided-traffic/quader/internal/formats/npm"
	"github.com/guided-traffic/quader/internal/formats/oci"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// R-OPS-054: the event of a download names the unit its item belongs to by the format's declaration (R-OPS-053): an image's blobs and
// manifests name the image and no version, its tag's item the tag, and a manifest fetched by a tag on a remote (counted on the manifest's
// digest) the tag the request was allowed to read; an npm tarball its name and version; a generic file no unit.
func TestR_OPS_054_TheUnitOfADownloadIsTheFormats(t *testing.T) {
	digest := "sha256:" + "ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12"
	for _, c := range []struct {
		name       string
		f          formats.Format
		path, read string
		want       items.DownloadUnit
	}{
		{"an image's blob", oci.New(), "acme/app/_blobs/" + digest, "", items.DownloadUnit{Package: "acme/app", Key: "acme/app"}},
		{"an image's manifest by digest", oci.New(), "acme/app/_manifests/" + digest, "", items.DownloadUnit{Package: "acme/app", Key: "acme/app"}},
		{"an image's tag", oci.New(), "acme/app/_tags/1.4.0", "", items.DownloadUnit{Package: "acme/app", Version: "1.4.0", Key: "acme/app"}},
		{"a manifest fetched by its tag", oci.New(), "acme/app/_manifests/" + digest, "1.4.0", items.DownloadUnit{Package: "acme/app", Version: "1.4.0", Key: "acme/app"}},
		{"an npm tarball", npm.New(), "@acme/ui/-/ui-2.4.0.tgz", "", items.DownloadUnit{Package: "@acme/ui", Version: "2.4.0", Key: "@acme/ui@2.4.0"}},
		{"a generic file", generic.Format{}, "releases/1.0/app.zip", "", items.DownloadUnit{}},
	} {
		if got := UnitOf(c.f, c.path, c.read); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}

	// The version a request was allowed to read is the request's: the host takes it from the request's state.
	h := &Host{o: Options{Deps: Deps{Formats: func(id string) (formats.Format, bool) { return oci.New(), id == "oci" }}}}
	repo := repository.Repository{Name: qname.Repo{Project: "upstream", Key: "docker-hub"}, Class: repository.Remote, Format: "oci"}
	rq := &formats.Request{Request: httptest.NewRequest("GET", "/v2/acme/app/manifests/1.4.0", nil), Repo: &repo}
	defer h.Begin(rq)()
	stateOf(rq).noteRead("acme/app", "1.4.0")
	stateOf(rq).noteRead("acme/app", "2.0.0") // the first read stays the request's
	if got := h.downloadUnit(rq, "acme/app/_manifests/"+digest); got.Version != "1.4.0" || got.Key != "acme/app" {
		t.Errorf("the manifest of a tag fetched on a remote: %+v", got)
	}
}
