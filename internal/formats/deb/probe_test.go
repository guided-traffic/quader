package deb

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// TestR_REPO_065_WhatAProbeOfAnAPTArchiveIs: a HEAD of a package file names one version's file of one architecture without fetching it
// and tells the host of a probe of that file; a GET of it fetches it and a by-hash file names no package (Q-DEB-122).
func TestR_REPO_065_WhatAProbeOfAnAPTArchiveIs(t *testing.T) {
	const (
		pool   = "pool/main/h/hello/hello_1.0-1_amd64.deb"
		byHash = "dists/stable/main/binary-amd64/by-hash/SHA256/0000000000000000000000000000000000000000000000000000000000000000"
	)
	m := &formattest.MemRemote{Artifacts: map[string][]byte{pool: []byte("deb"), byHash: []byte("index")}}
	f := New()
	h := formattest.NewHost(f, formattest.HostOptions{Class: formats.Remote, Data: twinPort{twinStore: newTwin()}, Resolver: noShadow{}, Remote: m})
	info := f.Classify(formats.Path{Path: pool})
	want := formats.Probe{Name: info.Package, Version: info.Version, Files: []formats.Path{{Path: pool}}}
	if want.Name != "hello" || want.Version == "" {
		t.Fatalf("the package file classifies as %+v", info)
	}
	for _, tc := range []struct {
		method, path string
		want         *formats.Probe
	}{
		{http.MethodGet, pool, nil},
		{http.MethodHead, pool, &want},
		{http.MethodHead, byHash, nil},
	} {
		m.Probes = nil
		resp, err := h.Serve(context.Background(), tc.method, tc.path, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		got := m.ProbesSeen()
		switch {
		case resp.Code != http.StatusOK:
			t.Errorf("%s %s: %d %s", tc.method, tc.path, resp.Code, resp.Body.String())
		case tc.want == nil && len(got) != 0:
			t.Errorf("%s %s is no probe and told the host of %+v", tc.method, tc.path, got)
		case tc.want != nil && (len(got) != 1 || !reflect.DeepEqual(got[0], *tc.want)):
			t.Errorf("%s %s: probes %+v, want %+v", tc.method, tc.path, got, *tc.want)
		}
	}
}
