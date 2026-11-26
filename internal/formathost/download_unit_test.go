package formathost

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible"
	"github.com/guided-traffic/quader/internal/formats/deb"
	"github.com/guided-traffic/quader/internal/formats/generic"
	"github.com/guided-traffic/quader/internal/formats/npm"
	"github.com/guided-traffic/quader/internal/formats/nuget"
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
		{"a generic file", generic.Format{}, "releases/1.0/app.zip", "", items.DownloadUnit{Files: true}},
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

// R-OPS-056: the files of a push name the unit the format's own event of the version names (package.published, npm.publish), so that a
// push is one row: the producers write <package>@<version> of the version (NuGet's lower-case ID and URL form, Ansible's
// namespace.name, npm's name), the image's name (OCI), and the file's own (Debian); a generic file names no package and a folder as its
// change's unit. The paths are the formats' own; the strings are those the producers build. Unit tier, microseconds.
func TestR_OPS_056_APushsFilesNameTheUnitOfItsVersion(t *testing.T) {
	for _, c := range []struct {
		name string
		f    formats.Format
		path string
		want string
	}{
		{"a NuGet package", nuget.New(), "acme.widgets/1.2.3/acme.widgets.1.2.3.nupkg", "acme.widgets" + "@" + "1.2.3"},
		{"a NuGet manifest", nuget.New(), "acme.widgets/1.2.3/acme.widgets.nuspec", "acme.widgets@1.2.3"},
		{"an npm tarball", npm.New(), "left-pad/-/left-pad-1.3.0.tgz", "left-pad@1.3.0"},
		{"an Ansible archive", ansible.New(), "collections/acme/tools/acme-tools-1.4.0.tar.gz", "acme.tools@1.4.0"},
		{"a Debian package", deb.New(), "pool/main/h/hello/hello_2.10-3_amd64.deb", "hello@2.10-3"},
		{"an image's layer", oci.New(), "acme/app/_blobs/sha256:" + strings.Repeat("ab", 32), "acme/app"},
	} {
		if got := UnitOf(c.f, c.path, ""); got.Key != c.want {
			t.Errorf("%s: %+v, want the unit %q", c.name, got, c.want)
		}
	}
	if got := UnitOf(generic.Format{}, "releases/1.0/app.zip", ""); !got.Files || got.Package != "" {
		t.Errorf("a generic file: %+v", got)
	}
}
