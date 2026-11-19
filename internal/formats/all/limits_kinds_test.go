package all_test

import (
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
)

// TestR_LIM_036_EveryRouteOfEveryFormatCountsByItsKind pins, per format, the kind of each route: the kind is the declaration
// of what the download limits count (usage-and-limits.md R-LIM-007, R-LIM-036). Download is a download (admitted before its
// status line, paced, a parallel lease); Metadata, Computed and Volatile are documents (their bytes count); Upload, Delete
// and Admin are writes (requests, package_write). A route added to a format, or a format added to Quader, fails here until
// its kinds are listed, the way R-API-079 holds the fetch of a version. The generic format has no routes: its protocol is
// /files/, whose file is a download of the raw-file handler.
func TestR_LIM_036_EveryRouteOfEveryFormatCountsByItsKind(t *testing.T) {
	want := map[formats.ID]map[string]formats.RouteKind{
		"ansible": {
			"ansible.artifact.get":       formats.Download,
			"ansible.collection.get":     formats.Metadata,
			"ansible.collection.publish": formats.Upload,
			"ansible.import.get":         formats.Metadata,
			"ansible.role.archive":       formats.Download,
			"ansible.role.versions":      formats.Metadata,
			"ansible.roles.find":         formats.Volatile,
			"ansible.root.get":           formats.Computed,
			"ansible.version.docs":       formats.Metadata,
			"ansible.version.get":        formats.Metadata,
			"ansible.versions.list":      formats.Metadata,
		},
		"deb": {
			"deb.dists.by_hash":   formats.Download,
			"deb.dists.get":       formats.Metadata,
			"deb.flat.file":       formats.Download,
			"deb.flat.index":      formats.Metadata,
			"deb.flat.release":    formats.Metadata,
			"deb.members.get":     formats.Download,
			"deb.pool.delete":     formats.Delete,
			"deb.pool.get":        formats.Download,
			"deb.signing_key.get": formats.Computed,
			"deb.upload.put":      formats.Upload,
		},
		"generic": {},
		"npm": {
			"npm.audit.bulk":        formats.Volatile,
			"npm.audit.legacy":      formats.Volatile,
			"npm.deferred.delete":   formats.Admin,
			"npm.deferred.get":      formats.Computed,
			"npm.deferred.post":     formats.Admin,
			"npm.deferred.put":      formats.Admin,
			"npm.disttag.delete":    formats.Admin,
			"npm.disttag.put":       formats.Admin,
			"npm.disttags.get":      formats.Metadata,
			"npm.document.get":      formats.Metadata,
			"npm.ping":              formats.Volatile,
			"npm.publish":           formats.Upload,
			"npm.root":              formats.Computed,
			"npm.tarball.get":       formats.Download,
			"npm.unpublish.delete":  formats.Delete,
			"npm.unpublish.put":     formats.Delete,
			"npm.unpublish.tarball": formats.Delete,
			"npm.version.get":       formats.Metadata,
			"npm.whoami":            formats.Volatile,
		},
		"nuget": {
			"nuget.autocomplete":           formats.Volatile,
			"nuget.delete":                 formats.Delete,
			"nuget.flatcontainer.embedded": formats.Download,
			"nuget.flatcontainer.index":    formats.Metadata,
			"nuget.flatcontainer.nupkg":    formats.Download,
			"nuget.flatcontainer.nuspec":   formats.Download,
			"nuget.flatcontainer.readme":   formats.Metadata,
			"nuget.index":                  formats.Computed,
			"nuget.push":                   formats.Upload,
			"nuget.registration.index":     formats.Metadata,
			"nuget.registration.leaf":      formats.Metadata,
			"nuget.registration.page":      formats.Metadata,
			"nuget.relist":                 formats.Admin,
			"nuget.search":                 formats.Volatile,
			"nuget.symbolpush":             formats.Upload,
			"nuget.symbols":                formats.Download,
			"nuget.vulnerabilities.index":  formats.Computed,
			"nuget.vulnerabilities.page":   formats.Computed,
		},
		"oci": {
			"oci.blob.delete":         formats.Delete,
			"oci.blob.get":            formats.Download,
			"oci.manifest.delete":     formats.Delete,
			"oci.manifest.get.digest": formats.Download,
			"oci.manifest.get.tag":    formats.Metadata,
			"oci.manifest.put":        formats.Upload,
			"oci.referrers":           formats.Volatile,
			"oci.tags.list":           formats.Metadata,
			"oci.upload.cancel":       formats.Upload,
			"oci.upload.patch":        formats.Upload,
			"oci.upload.put":          formats.Upload,
			"oci.upload.start":        formats.Upload,
			"oci.upload.status":       formats.Upload,
		},
	}
	seen := map[formats.ID]bool{}
	for _, f := range formats.List() {
		id := f.Descriptor().ID
		seen[id] = true
		table, ok := want[id]
		if !ok {
			t.Errorf("%s is not in this table: list the kind of each of its routes (R-LIM-036)", id)
			continue
		}
		routes := map[string]bool{}
		for _, r := range f.Routes() {
			routes[r.Name] = true
			k, listed := table[r.Name]
			switch {
			case !listed:
				t.Errorf("%s: route %s (%s) is not in this table: decide whether it is a download, a document or a write (R-LIM-007)", id, r.Name, r.Kind)
			case k != r.Kind:
				t.Errorf("%s: route %s is %s, the table says %s", id, r.Name, r.Kind, k)
			}
		}
		for name := range table {
			if !routes[name] {
				t.Errorf("%s: the table lists %s, which the format does not have", id, name)
			}
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("%s is listed and not registered", id)
		}
	}
}
