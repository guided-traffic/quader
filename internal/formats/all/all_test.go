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
		"clamav":  formats.FetchLeadFile, // the container of the version; its detached signature is none (R-CLAMAV-007)
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
		"deb":     {"HEAD deb.pool.get", "HEAD deb.flat.file", "HEAD deb.members.get"},
		"oci":     {"GET oci.manifest.get.tag", "HEAD oci.manifest.get.tag", "HEAD oci.manifest.get.digest", "HEAD oci.blob.get"},
		"generic": nil, // no class remote (R-GENERIC-001)
		"clamav":  nil, // no class remote (R-CLAMAV-001)
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

// TestR_REPO_065_AProbeIsAProbeThroughAVirtualRepositoryToo pins that a format with the classes remote and virtual answers its probes
// through a virtual address as well: every probe route serves the class virtual, unless this list names why the virtual repository
// serves that request on another route that is itself a probe. Each format's own test asserts that the virtual handler tells the host.
func TestR_REPO_065_AProbeIsAProbeThroughAVirtualRepositoryToo(t *testing.T) {
	remoteOnly := map[string]string{
		// A flat repository is an upstream's layout; a virtual repository serves its files by the member path of its index.
		"deb.flat.file": "deb.members.get",
	}
	for _, f := range formats.List() {
		d := f.Descriptor()
		if !d.Supports(formats.Remote) || !d.Supports(formats.Virtual) {
			continue
		}
		routes := map[string]*formats.Route{}
		for _, r := range f.Routes() {
			routes[r.Name] = &r
		}
		probes := map[string]bool{}
		for _, p := range d.Probes {
			probes[p.Route] = true
		}
		for _, p := range d.Probes {
			if routes[p.Route].ServesClass(formats.Virtual) {
				continue
			}
			instead, ok := remoteOnly[p.Route]
			switch {
			case !ok:
				t.Errorf("%s: the probe %s %s does not serve the class virtual; a probe through a virtual repository starts the download too (Q-REPO-367)", d.ID, p.Method, p.Route)
			case !probes[instead] || !routes[instead].ServesClass(formats.Virtual):
				t.Errorf("%s: %s is answered on %s through a virtual repository, which is no probe of the class virtual", d.ID, p.Route, instead)
			}
		}
	}
}

// TestR_OPS_053_EveryRemoteFormatDeclaresWhatOneDownloadIs holds the table of R-OPS-049 "The unit per format" (the owner's rule of
// 2026-11-26, ADR-295): every format with the class remote declares what one download from an upstream is for it, and the declaration
// is the table's. A format added later fails here until its row says what a person asks for when its client downloads.
func TestR_OPS_053_EveryRemoteFormatDeclaresWhatOneDownloadIs(t *testing.T) {
	want := map[formats.ID]formats.DownloadUnit{
		"npm":     formats.DownloadUnitVersion, // a version's tarball
		"nuget":   formats.DownloadUnitVersion, // a version's package, with its symbols package fetched at once
		"ansible": formats.DownloadUnitVersion, // a collection version's archive; a role's archive
		"deb":     formats.DownloadUnitVersion, // a binary package's file; a source package's .dsc with its tarballs
		"oci":     formats.DownloadUnitImage,   // the image with its manifests, configs, layers and attestations
		"generic": formats.DownloadUnitUndeclared,
		"clamav":  formats.DownloadUnitUndeclared,
	}
	for _, f := range formats.List() {
		d := f.Descriptor()
		rule, ok := want[d.ID]
		switch {
		case !ok:
			t.Errorf("%s is not in this list: write its row into R-OPS-049 and here (R-OPS-053)", d.ID)
		case d.DownloadUnit != rule:
			t.Errorf("%s declares the download unit %d, want %d", d.ID, d.DownloadUnit, rule)
		}
	}
}

// TestR_STOR_071_EveryFormatDeclaresWhetherItsClientsCheck lists every registered format with the routes that declare that every client
// of the format checks a digest (Route.ClientDigest.Checks, ADR-243), the only routes that may hand a download to a signed link of the
// object store (R-STOR-070). A format added later, or a declaration changed, fails here until its row says so: a declaration is a
// reviewed act with its evidence, never a default (the rule of evidence and the table of R-STOR-071).
func TestR_STOR_071_EveryFormatDeclaresWhetherItsClientsCheck(t *testing.T) {
	want := map[formats.ID][]string{
		"oci":     {"oci.blob.get"},         // the digest in the path; the manifest by digest declares nothing
		"npm":     nil,                      // Yarn 4 installs a tarball whose object was altered (Q-STOR-354)
		"ansible": {"ansible.artifact.get"}, // artifact.sha256 of the collection version; a role archive declares nothing
		"nuget":   nil,                      // no client's check was read
		"deb":     nil,                      // apt's hash path was not read
		"generic": nil,                      // no routes; /files/ is never a link
		"clamav":  nil,                      // freshclam was not researched, and its routes are Metadata
	}
	seen := map[formats.ID]bool{}
	for _, f := range formats.List() {
		d := f.Descriptor()
		seen[d.ID] = true
		rule, ok := want[d.ID]
		if !ok {
			t.Errorf("%s is not in this list: add its row with the routes whose clients check a digest, none without evidence (R-STOR-071)", d.ID)
			continue
		}
		var got []string
		for _, r := range f.Routes() {
			if !r.ClientDigest.Checks {
				continue
			}
			got = append(got, r.Name)
			if r.ClientDigest.Against == "" || r.ClientDigest.Evidence == "" {
				t.Errorf("%s: route %s declares without what its clients compare against and the evidence", d.ID, r.Name)
			}
		}
		if strings.Join(got, ", ") != strings.Join(rule, ", ") {
			t.Errorf("%s declares that the clients of %v check a digest, want %v", d.ID, got, rule)
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
