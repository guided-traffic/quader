package ansible

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// TestR_REPO_065_WhatAProbeOfTheGalaxyIs: the version document of a collection and a HEAD of a collection's or a role's archive name one
// version without fetching its archive and tell the host of a probe whose content is the archive; the collection document, a version list
// and a GET of the archive do not (Q-ANS-80).
func TestR_REPO_065_WhatAProbeOfTheGalaxyIs(t *testing.T) {
	m := &formattest.MemRemote{Docs: map[string][]byte{}, Artifacts: map[string][]byte{
		ArtifactPath("acme", "tools", "1.0.0"): []byte("archive"), RoleArchivePath("16", "1.0.0"): []byte("role archive"),
	}}
	for _, d := range remoteFixture().Docs {
		m.Docs[d.Path] = d.Body
	}
	o := hostOptions()
	o.Class, o.Remote = formats.Remote, m
	h := formattest.NewHost(New(), o)
	collection := &formats.Probe{Name: "acme.tools", Version: "1.0.0", Files: []formats.Path{{Path: ArtifactPath("acme", "tools", "1.0.0")}}}
	role := &formats.Probe{Name: RolePackage("16"), Version: "1.0.0", Files: []formats.Path{{Path: RoleArchivePath("16", "1.0.0")}}}
	for _, tc := range []struct {
		method, path string
		want         *formats.Probe
	}{
		{http.MethodGet, "v3/collections/acme/tools/", nil},
		{http.MethodGet, "v3/collections/acme/tools/versions/1.0.0/", collection},
		{http.MethodHead, "v3/collections/acme/tools/versions/1.0.0/", collection},
		{http.MethodGet, "v3/artifacts/collections/acme-tools-1.0.0.tar.gz", nil},
		{http.MethodHead, "v3/artifacts/collections/acme-tools-1.0.0.tar.gz", collection},
		{http.MethodHead, "v1/roles/archives/16/1.0.0.tar.gz", role},
		{http.MethodGet, "v1/roles/archives/16/1.0.0.tar.gz", nil},
	} {
		m.Probes = nil
		resp, err := h.Serve(context.Background(), tc.method, tc.path, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		got := m.ProbesSeen()
		switch {
		case tc.want == nil && len(got) != 0:
			t.Errorf("%s %s (%d) is no probe and told the host of %+v", tc.method, tc.path, resp.Code, got)
		case tc.want != nil && (resp.Code != http.StatusOK || len(got) != 1 || !reflect.DeepEqual(got[0], *tc.want)):
			t.Errorf("%s %s: %d %s, probes %+v, want %+v", tc.method, tc.path, resp.Code, resp.Body.String(), got, *tc.want)
		}
	}
}

// TestR_REPO_065_WhatAProbeOfTheGalaxyIsThroughAVirtualRepository: the version document and a HEAD of a collection's archive are probes
// through a virtual repository, told to the host with the request addressed to the virtual repository, which finds the member that
// serves the archive. A GET of an archive and a version no member holds are not.
func TestR_REPO_065_WhatAProbeOfTheGalaxyIsThroughAVirtualRepository(t *testing.T) {
	w := newWorld(t, local("loc", 1, 0), remote("rem", 2, 1))
	w.doc("collections/acme/tools/1.0.0.json", "rem", upstreamVersion("acme", "tools", "1.0.0", "upstream.example", sha(3)))
	collection := "virt acme.tools 1.0.0 " + ArtifactPath("acme", "tools", "1.0.0")
	for _, tc := range []struct {
		method, path string
		want         string
	}{
		{http.MethodGet, versionsPath + "1.0.0/", collection},
		{http.MethodHead, versionsPath + "1.0.0/", collection},
		{http.MethodGet, versionsPath + "3.0.0/", ""},
		{http.MethodGet, "v3/artifacts/collections/acme-tools-1.0.0.tar.gz", ""},
		{http.MethodHead, "v3/artifacts/collections/acme-tools-1.0.0.tar.gz", collection},
	} {
		w.rem.probes = nil
		r, err := w.h.Serve(context.Background(), tc.method, tc.path, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Join(w.rem.probes, "; ")
		if got != tc.want {
			t.Errorf("%s %s (%d): probes %q, want %q", tc.method, tc.path, r.Code, got, tc.want)
		}
	}
}
