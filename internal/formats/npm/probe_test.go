package npm

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
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
