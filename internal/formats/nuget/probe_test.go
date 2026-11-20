package nuget

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// TestR_REPO_065_WhatAProbeOfNuGetIs: the registration leaf of a version, the .nuspec and a HEAD of the .nupkg name one version without
// fetching its package and tell the host of a probe whose content is the .nupkg; the registration index, the flat container's version
// list and a GET of the .nupkg do not (Q-NUGET-124).
func TestR_REPO_065_WhatAProbeOfNuGetIs(t *testing.T) {
	docs := registrationUpstream([]string{"1.0.0", "1.1.0"}, 64, true)
	docs["acme.widgets/index.json"] = []byte(`{"versions":["1.0.0","1.1.0"]}`)
	m := &formattest.MemRemote{Docs: docs, Artifacts: map[string][]byte{
		NupkgPath("acme.widgets", "1.0.0"): []byte("PK-package"), NuspecPath("acme.widgets", "1.0.0"): []byte("<package/>"),
	}}
	h := remoteHost(t, m)
	probe := func(v string) *formats.Probe {
		return &formats.Probe{Name: "acme.widgets", Version: v, Files: []formats.Path{{Path: NupkgPath("acme.widgets", v)}}}
	}
	for _, tc := range []struct {
		method, path string
		want         *formats.Probe
	}{
		{http.MethodGet, "v3/registration-semver2/acme.widgets/index.json", nil},
		{http.MethodGet, "v3/flatcontainer/acme.widgets/index.json", nil},
		{http.MethodGet, "v3/registration-semver2/acme.widgets/1.1.0.json", probe("1.1.0")},
		{http.MethodHead, "v3/registration-semver2/acme.widgets/1.1.0.json", probe("1.1.0")},
		{http.MethodGet, "v3/flatcontainer/acme.widgets/1.0.0/acme.widgets.nuspec", probe("1.0.0")},
		{http.MethodGet, "v3/flatcontainer/acme.widgets/1.0.0/acme.widgets.1.0.0.nupkg", nil},
		{http.MethodHead, "v3/flatcontainer/acme.widgets/1.0.0/acme.widgets.1.0.0.nupkg", probe("1.0.0")},
		{http.MethodGet, "v3/flatcontainer/acme.widgets/9.9.9/acme.widgets.nuspec", nil},
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
			t.Errorf("%s %s: %d, probes %+v, want %+v", tc.method, tc.path, resp.Code, got, *tc.want)
		}
	}
}

// TestQ_REPO_367_WhatAProbeOfNuGetIsThroughAVirtualRepository: the same requests are probes through a virtual repository, told to
// the host with the request addressed to the virtual repository, which finds the member that serves the .nupkg; the registration index,
// the version list, a GET of the .nupkg and the leaf of a version no member holds are not.
func TestQ_REPO_367_WhatAProbeOfNuGetIsThroughAVirtualRepository(t *testing.T) {
	res := &mergeResolver{docs: docsOf(
		[]formats.MemberDocument{remoteFlat("remote", 0, "1.0.0", "1.1.0")},
		[]formats.MemberDocument{remotePart("remote", 0, "", "1.0.0", "1.1.0")},
	)}
	f := New()
	res.f = f
	m := &formattest.MemRemote{}
	h := formattest.NewHost(f, formattest.HostOptions{Class: formats.Virtual, RepoKey: "all", Data: newDocStore(), Resolver: res, Remote: m})
	probe := func(v string) *formats.Probe {
		return &formats.Probe{Name: "acme.widgets", Version: v, Files: []formats.Path{{Path: NupkgPath("acme.widgets", v)}}}
	}
	for _, tc := range []struct {
		method, path string
		want         *formats.Probe
	}{
		{http.MethodGet, "v3/registration-semver2/acme.widgets/index.json", nil},
		{http.MethodGet, "v3/flatcontainer/acme.widgets/index.json", nil},
		{http.MethodGet, "v3/registration-semver2/acme.widgets/1.1.0.json", probe("1.1.0")},
		{http.MethodHead, "v3/registration-semver2/acme.widgets/1.1.0.json", probe("1.1.0")},
		{http.MethodGet, "v3/registration-semver2/acme.widgets/9.9.9.json", nil},
		{http.MethodGet, "v3/flatcontainer/acme.widgets/1.0.0/acme.widgets.nuspec", probe("1.0.0")},
		{http.MethodGet, "v3/flatcontainer/acme.widgets/1.0.0/acme.widgets.1.0.0.nupkg", nil},
		{http.MethodHead, "v3/flatcontainer/acme.widgets/1.0.0/acme.widgets.1.0.0.nupkg", probe("1.0.0")},
	} {
		m.Probes, m.ProbedOn = nil, nil
		resp, err := h.Serve(context.Background(), tc.method, tc.path, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		got, on := m.ProbesSeen(), m.ProbedOnSeen()
		switch {
		case tc.want == nil && len(got) != 0:
			t.Errorf("%s %s (%d) is no probe and told the host of %+v", tc.method, tc.path, resp.Code, got)
		case tc.want != nil && (resp.Code != http.StatusOK || len(got) != 1 || !reflect.DeepEqual(got[0], *tc.want)):
			t.Errorf("%s %s: %d, probes %+v, want %+v", tc.method, tc.path, resp.Code, got, *tc.want)
		case tc.want != nil && (on[0].Name.Key != "all" || on[0].Class != formats.Virtual):
			t.Errorf("%s %s was told on %+v, want the virtual repository", tc.method, tc.path, on[0])
		}
	}
}
