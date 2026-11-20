package deb

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
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

// poolResolver serves every package file through a virtual repository and plans one remote member the caller may fill.
type poolResolver struct {
	noShadow
	member formats.RepoRef
}

func (r poolResolver) Artifact(_ context.Context, _ *formats.Request, _ formats.Path, _ formats.PathInfo, w http.ResponseWriter) error {
	w.WriteHeader(http.StatusOK)
	return nil
}

func (r poolResolver) Plan(context.Context, *formats.Request, formats.Path, formats.PathInfo) (*formats.VirtualPlan, error) {
	return &formats.VirtualPlan{Admitted: true, Members: []formats.PlannedMember{{Member: r.member, Tier: formats.TierRemote, MayFill: true}}}, nil
}

// TestQ_REPO_367_WhatAProbeOfAnAPTArchiveIsThroughAVirtualRepository: a HEAD of a package file by its own path is told to the host with
// the request addressed to the virtual repository, which finds the member that serves it; a HEAD by the member path of the merged index
// is the probe of that member, told with the member's request. A GET of either is none.
func TestQ_REPO_367_WhatAProbeOfAnAPTArchiveIsThroughAVirtualRepository(t *testing.T) {
	const pool = "pool/main/h/hello/hello_1.0-1_amd64.deb"
	member := repository.Repository{ID: "2", Name: qname.Repo{Project: "it", Key: "debian"}, Class: repository.Remote, Format: string(ID), Lifecycle: "active"}
	m := &formattest.MemRemote{Artifacts: map[string][]byte{pool: []byte("deb")}}
	f := New()
	res := poolResolver{member: formats.RepoRef{ID: 2, Name: member.Name, Class: formats.Remote, Format: ID}}
	h := formattest.NewHost(f, formattest.HostOptions{Class: formats.Virtual, RepoKey: "all", Data: twinPort{twinStore: newTwin()}, Resolver: res, Remote: m})
	*h.Repo() = repository.Assemble(*h.Repo(), []repository.Repository{member}, nil, 60)
	info := f.Classify(formats.Path{Path: pool})
	want := formats.Probe{Name: info.Package, Version: info.Version, Files: []formats.Path{{Path: pool}}}
	for _, tc := range []struct {
		method, path, on string
	}{
		{http.MethodGet, pool, ""},
		{http.MethodHead, pool, "all"},
		{http.MethodGet, "members/debian/" + pool, ""},
		{http.MethodHead, "members/debian/" + pool, "debian"},
	} {
		m.Probes, m.ProbedOn = nil, nil
		resp, err := h.Serve(context.Background(), tc.method, tc.path, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		got, on := m.ProbesSeen(), m.ProbedOnSeen()
		switch {
		case resp.Code != http.StatusOK:
			t.Errorf("%s %s: %d %s", tc.method, tc.path, resp.Code, resp.Body.String())
		case tc.on == "" && len(got) != 0:
			t.Errorf("%s %s is no probe and told the host of %+v", tc.method, tc.path, got)
		case tc.on != "" && (len(got) != 1 || !reflect.DeepEqual(got[0], want) || on[0].Name.Key != tc.on):
			t.Errorf("%s %s: probes %+v on %+v, want %+v on %s", tc.method, tc.path, got, on, want, tc.on)
		}
	}
}
