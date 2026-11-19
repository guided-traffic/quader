package all

import (
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
)

// TestR_API_079_EveryFormatWithPackageScreensCountsTheFetchesOfItsVersionsByItsProtocol holds the rule of what a fetch of a version
// is for every registered format (ADR-205): each format with package screens declares it, and each declaration is the one its protocol
// calls for. A format added to Quader is in this list or the test fails.
func TestR_API_079_EveryFormatWithPackageScreensCountsTheFetchesOfItsVersionsByItsProtocol(t *testing.T) {
	want := map[formats.ID]formats.VersionFetch{
		"npm":     formats.FetchLeadFile, // the tarball; the packument and the provenance bundle are none
		"nuget":   formats.FetchLeadFile, // the .nupkg; the registration, the flat-container index and the .nuspec are none
		"ansible": formats.FetchLeadFile, // the collection archive; the version documents are none
		"deb":     formats.FetchAnyFile,  // the package file of every architecture; the indexes are none
		"oci":     formats.FetchDocument, // the tag's manifest by the tag or by its digest; a HEAD and the platform manifests are none
	}
	seen := map[formats.ID]bool{}
	for _, f := range formats.List() {
		d := f.Descriptor()
		if f.View() == nil {
			if _, listed := want[d.ID]; listed {
				t.Errorf("%s has no package screens any more: take it out of this list", d.ID)
			}
			continue
		}
		seen[d.ID] = true
		rule, ok := want[d.ID]
		switch {
		case !ok:
			t.Errorf("%s has package screens and is not in this list: decide what a fetch of its versions is (R-API-079)", d.ID)
		case d.VersionFetch != rule:
			t.Errorf("%s declares the fetch %s, want %s", d.ID, d.VersionFetch, rule)
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("%s is listed and has no package screens", id)
		}
	}
}

// TestR_REPO_065_EveryFormatDeclaresItsProbes pins, per format, the requests that name exactly one version or file of a remote
// repository without fetching its content and so may start its background download (W-25). A new format with the class remote, and a
// route that becomes a probe or stops being one, fails here until the table says so; a format without the class remote declares none.
func TestR_REPO_065_EveryFormatDeclaresItsProbes(t *testing.T) {
	want := map[formats.ID][]string{
		"npm":     {"GET npm.version.get", "HEAD npm.version.get", "HEAD npm.tarball.get"},
		"nuget":   {"GET nuget.registration.leaf", "HEAD nuget.registration.leaf", "GET nuget.flatcontainer.nuspec", "HEAD nuget.flatcontainer.nuspec", "HEAD nuget.flatcontainer.nupkg"},
		"ansible": {"GET ansible.version.get", "HEAD ansible.version.get", "HEAD ansible.artifact.get", "HEAD ansible.role.archive"},
		"deb":     {"HEAD deb.pool.get", "HEAD deb.flat.file"},
		"oci":     {"GET oci.manifest.get.tag", "HEAD oci.manifest.get.tag", "HEAD oci.manifest.get.digest", "HEAD oci.blob.get"},
		"generic": nil, // no class remote (R-GENERIC-001)
	}
	seen := map[formats.ID]bool{}
	for _, f := range formats.List() {
		d := f.Descriptor()
		seen[d.ID] = true
		rule, ok := want[d.ID]
		if !ok {
			t.Errorf("%s is not in this list: declare its probes (R-REPO-065)", d.ID)
			continue
		}
		var got []string
		for _, p := range d.Probes {
			got = append(got, p.Method+" "+p.Route)
		}
		if strings.Join(got, ", ") != strings.Join(rule, ", ") {
			t.Errorf("%s declares the probes %v, want %v", d.ID, got, rule)
		}
		if !d.Supports(formats.Remote) && len(d.Probes) > 0 {
			t.Errorf("%s has no class remote and declares probes", d.ID)
		}
		if d.Supports(formats.Remote) && len(d.Probes) == 0 {
			t.Errorf("%s has the class remote and declares no probe; the owner's rule holds every format (R-REPO-065)", d.ID)
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("%s is listed and not registered", id)
		}
	}
}

func TestCatalog(t *testing.T) {
	t.Run("R-GENERIC-001 R-REPO-003 the generic format is in the catalog and lists only the local class", func(t *testing.T) {
		f, ok := formats.Lookup("generic")
		if !ok {
			t.Fatal("generic is not registered by formats/all")
		}
		d := f.Descriptor()
		if d.ID != "generic" || !d.Supports(formats.Local) || d.Supports(formats.Remote) || d.Supports(formats.Virtual) {
			t.Fatalf("descriptor = %+v", d)
		}
	})
}
