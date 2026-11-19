package all

import (
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
