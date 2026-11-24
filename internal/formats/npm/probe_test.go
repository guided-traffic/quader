package npm

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/formats/npm/npmtest"
)

// TestR_REPO_065_WhatAProbeOfNpmIs: the version document, by its number or a dist-tag, and a HEAD of the tarball name one version without
// fetching its tarball and tell the host of a probe whose content is the tarball; the package document, a GET of the tarball and a version
// the document does not list do not (Q-NPM-89).
func TestR_REPO_065_WhatAProbeOfNpmIs(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(upstreamBody("@acme/ui", "2026-10-30T11:00:00.000Z", "1.0.0", "1.1.0"), &doc); err != nil {
		t.Fatal(err)
	}
	doc["dist-tags"] = map[string]string{"latest": "1.1.0"}
	body, _ := json.Marshal(doc)
	eng := &scriptedRemote{
		metadata: func(string) (*formats.RemoteDocument, error) { return stored(body), nil },
		artifact: func(_ formats.Path, _ formats.Digests, w http.ResponseWriter) error {
			w.WriteHeader(http.StatusOK)
			return nil
		},
	}
	h, _, _ := remoteHost(t, eng)
	tarball := func(v string) []formats.Path { return []formats.Path{{Path: "@acme/ui/-/ui-" + v + ".tgz"}} }
	for _, tc := range []struct {
		method, path string
		status       int
		want         *formats.Probe
	}{
		{http.MethodGet, "@acme%2fui", http.StatusOK, nil},
		{http.MethodGet, "@acme%2fui/1.0.0", http.StatusOK, &formats.Probe{Name: "@acme/ui", Version: "1.0.0", Files: tarball("1.0.0")}},
		{http.MethodHead, "@acme%2fui/1.0.0", http.StatusOK, &formats.Probe{Name: "@acme/ui", Version: "1.0.0", Files: tarball("1.0.0")}},
		{http.MethodGet, "@acme%2fui/latest", http.StatusOK, &formats.Probe{Name: "@acme/ui", Version: "1.1.0", Files: tarball("1.1.0")}},
		{http.MethodGet, "@acme%2fui/9.9.9", http.StatusNotFound, nil},
		{http.MethodGet, "@acme%2fui/-/ui-1.0.0.tgz", http.StatusOK, nil},
		{http.MethodHead, "@acme%2fui/-/ui-1.0.0.tgz", http.StatusOK, &formats.Probe{Name: "@acme/ui", Version: "1.0.0", Files: tarball("1.0.0")}},
	} {
		eng.probes = nil
		resp, err := h.Serve(context.Background(), tc.method, tc.path, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Code != tc.status {
			t.Errorf("%s %s: %d, want %d", tc.method, tc.path, resp.Code, tc.status)
			continue
		}
		switch {
		case tc.want == nil && len(eng.probes) != 0:
			t.Errorf("%s %s is no probe and told the host of %+v", tc.method, tc.path, eng.probes)
		case tc.want != nil && (len(eng.probes) != 1 || !reflect.DeepEqual(eng.probes[0], *tc.want)):
			t.Errorf("%s %s: probes %+v, want %+v", tc.method, tc.path, eng.probes, *tc.want)
		}
	}
}

// TestR_REPO_065_WhatAProbeOfNpmIsThroughAVirtualRepository: the same requests are probes through a virtual repository, told to the
// host with the request addressed to the virtual repository, which finds the member that serves the tarball; the package document and a
// GET of the tarball are not.
func TestR_REPO_065_WhatAProbeOfNpmIsThroughAVirtualRepository(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(upstreamBody("@acme/ui", "2026-10-30T11:00:00.000Z", "1.0.0", "1.1.0"), &doc); err != nil {
		t.Fatal(err)
	}
	doc["dist-tags"] = map[string]string{"latest": "1.1.0"}
	body, _ := json.Marshal(doc)
	res := &resolverFake{
		merged: func() (*formats.Merged, error) {
			return &formats.Merged{Bytes: body, MergeFingerprint: [32]byte{1}}, nil
		},
		artifact: func(_ formats.Path, w http.ResponseWriter) error {
			w.WriteHeader(http.StatusOK)
			return nil
		},
	}
	m := &formattest.MemRemote{}
	h := formattest.NewHost(New(), formattest.HostOptions{Class: formats.Virtual, RepoKey: "npm-all", Data: npmtest.New(), Resolver: res, Remote: m})
	tarball := func(v string) []formats.Path { return []formats.Path{{Path: "@acme/ui/-/ui-" + v + ".tgz"}} }
	for _, tc := range []struct {
		method, path string
		want         *formats.Probe
	}{
		{http.MethodGet, "@acme%2fui", nil},
		{http.MethodGet, "@acme%2fui/1.0.0", &formats.Probe{Name: "@acme/ui", Version: "1.0.0", Files: tarball("1.0.0")}},
		{http.MethodHead, "@acme%2fui/1.0.0", &formats.Probe{Name: "@acme/ui", Version: "1.0.0", Files: tarball("1.0.0")}},
		{http.MethodGet, "@acme%2fui/latest", &formats.Probe{Name: "@acme/ui", Version: "1.1.0", Files: tarball("1.1.0")}},
		{http.MethodGet, "@acme%2fui/-/ui-1.0.0.tgz", nil},
		{http.MethodHead, "@acme%2fui/-/ui-1.0.0.tgz", &formats.Probe{Name: "@acme/ui", Version: "1.0.0", Files: tarball("1.0.0")}},
	} {
		m.Probes, m.ProbedOn = nil, nil
		resp, err := h.Serve(context.Background(), tc.method, tc.path, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		got, on := m.ProbesSeen(), m.ProbedOnSeen()
		switch {
		case tc.want == nil && len(got) != 0:
			t.Errorf("%s %s is no probe and told the host of %+v", tc.method, tc.path, got)
		case tc.want != nil && (resp.Code != http.StatusOK || len(got) != 1 || !reflect.DeepEqual(got[0], *tc.want)):
			t.Errorf("%s %s: %d, probes %+v, want %+v", tc.method, tc.path, resp.Code, got, *tc.want)
		case tc.want != nil && (on[0].Name.Key != "npm-all" || on[0].Class != formats.Virtual):
			t.Errorf("%s %s was told on %+v, want the virtual repository", tc.method, tc.path, on[0])
		}
	}
}
