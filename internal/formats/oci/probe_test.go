package oci

import (
	"context"
	"crypto/sha256"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

func probePathsOf(p formats.Probe) []string {
	out := make([]string, len(p.Files))
	for i, f := range p.Files {
		out[i] = f.Path
	}
	return out
}

// TestR_REPO_065_WhatAProbeOfTheRegistryIs: the manifest of a tag by GET or HEAD, a HEAD of a manifest by digest and a HEAD of a blob
// tell the host of a probe; a pull by digest, a GET of a blob and a tag the upstream does not have do not (Q-OCI-224).
func TestR_REPO_065_WhatAProbeOfTheRegistryIs(t *testing.T) {
	cfg, layer := []byte("cfg"), []byte("layer")
	image := imageManifestBytes(sha(cfg), sha(layer))
	fx, mem := remoteFixture(t, map[string][]byte{TagPath("app", "1"): image}, map[string][]byte{
		ManifestPath("app", sha(image)): image, BlobPath("app", sha(layer)): layer,
	}, nil)
	seen := func() []formats.Probe { p := mem.ProbesSeen(); mem.Probes = nil; return p }

	t.Run("the manifest of a tag names the image, its config and its layers", func(t *testing.T) {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			fx.wantStatus(fx.request(method, "app/manifests/1", nil, ""), http.StatusOK, "")
			got := seen()
			if len(got) != 1 {
				t.Fatalf("%s: %d probes", method, len(got))
			}
			p := got[0]
			want := []string{ManifestPath("app", sha(image)), BlobPath("app", sha(cfg)), BlobPath("app", sha(layer))}
			if p.Name != "app" || p.Version != "1" || !reflect.DeepEqual(probePathsOf(p), want) {
				t.Errorf("%s: probe %+v, want the files %v", method, p, want)
			}
			if p.Document.Path != TagPath("app", "1") || p.Digest != sha256.Sum256(image) {
				t.Errorf("%s: the probe names the entry %q with digest %x", method, p.Document.Path, p.Digest)
			}
		}
	})
	t.Run("a pull by digest and a GET of a blob fetch their content and are none", func(t *testing.T) {
		fx.wantStatus(fx.get("app/manifests/"+sha(image)), http.StatusOK, "")
		fx.wantStatus(fx.get("app/blobs/"+sha(layer)), http.StatusOK, "")
		if got := seen(); len(got) != 0 {
			t.Fatalf("probes %+v", got)
		}
	})
	t.Run("a HEAD by digest and a HEAD of a blob name one file", func(t *testing.T) {
		fx.wantStatus(fx.request(http.MethodHead, "app/manifests/"+sha(image), nil, ""), http.StatusOK, "")
		fx.wantStatus(fx.request(http.MethodHead, "app/blobs/"+sha(layer), nil, ""), http.StatusOK, "")
		got := seen()
		if len(got) != 2 || !reflect.DeepEqual(probePathsOf(got[0]), []string{ManifestPath("app", sha(image))}) ||
			got[0].Version != sha(image) || !reflect.DeepEqual(probePathsOf(got[1]), []string{BlobPath("app", sha(layer))}) {
			t.Fatalf("probes %+v", got)
		}
	})
	t.Run("a tag the upstream does not have is none", func(t *testing.T) {
		fx.wantStatus(fx.get("app/manifests/2"), http.StatusNotFound, "MANIFEST_UNKNOWN")
		if got := seen(); len(got) != 0 {
			t.Fatalf("probes %+v", got)
		}
	})
}

// remoteMemberResolver plans one remote member for every path of a virtual repository.
type remoteMemberResolver struct {
	proceedResolver
	member formats.RepoRef
}

func (r remoteMemberResolver) Plan(context.Context, *formats.Request, formats.Path, formats.PathInfo) (*formats.VirtualPlan, error) {
	return &formats.VirtualPlan{Admitted: true, Members: []formats.PlannedMember{{Member: r.member, Tier: formats.TierRemote, MayFill: true}}}, nil
}

// TestR_REPO_065_WhatAProbeOfTheRegistryIsThroughAVirtualRepository: the same requests are probes through a virtual address, told to
// the host with the request of the remote member that answered them (the walk serves a member's answer through the member's own
// handler), whose content and charge the host then decides on; a GET of a blob and a tag no member holds are none. Milliseconds.
func TestR_REPO_065_WhatAProbeOfTheRegistryIsThroughAVirtualRepository(t *testing.T) {
	cfg, layer := []byte("cfg"), []byte("layer")
	image := imageManifestBytes(sha(cfg), sha(layer))
	mem := &formattest.MemRemote{
		Docs:      map[string][]byte{TagPath("app", "1"): image},
		Artifacts: map[string][]byte{ManifestPath("app", sha(image)): image, BlobPath("app", sha(layer)): layer},
	}
	member := repository.Repository{ID: "2", Name: qname.Repo{Project: "it", Key: "docker-hub"}, Class: repository.Remote, Format: string(ID), Lifecycle: "active"}
	fx := newFixture(t, formattest.HostOptions{
		Class: formats.Virtual, RepoKey: "docker-all", Remote: mem,
		Resolver: remoteMemberResolver{member: formats.RepoRef{ID: 2, Name: member.Name, Class: formats.Remote, Format: ID}},
	})
	*fx.h.Repo() = repository.Assemble(*fx.h.Repo(), []repository.Repository{member}, nil, 60)
	tagFiles := []string{ManifestPath("app", sha(image)), BlobPath("app", sha(cfg)), BlobPath("app", sha(layer))}
	for _, tc := range []struct {
		method, path string
		files        []string // nil: no probe
	}{
		{http.MethodGet, "app/manifests/1", tagFiles},
		{http.MethodHead, "app/manifests/1", tagFiles},
		{http.MethodGet, "app/blobs/" + sha(layer), nil},
		{http.MethodHead, "app/blobs/" + sha(layer), []string{BlobPath("app", sha(layer))}},
		{http.MethodGet, "app/manifests/2", nil},
	} {
		mem.Probes, mem.ProbedOn = nil, nil
		resp := fx.request(tc.method, tc.path, nil, "")
		got, on := mem.ProbesSeen(), mem.ProbedOnSeen()
		switch {
		case tc.files == nil && len(got) != 0:
			t.Errorf("%s %s (%d) is no probe and told the host of %+v", tc.method, tc.path, resp.Code, got)
		case tc.files != nil && (resp.Code != http.StatusOK || len(got) != 1 || !reflect.DeepEqual(probePathsOf(got[0]), tc.files)):
			t.Errorf("%s %s: %d, probes %+v, want the files %v", tc.method, tc.path, resp.Code, got, tc.files)
		case tc.files != nil && (on[0].Name != member.Name || on[0].Class != formats.Remote):
			t.Errorf("%s %s was told on %+v, want the remote member", tc.method, tc.path, on[0])
		}
	}
}

// TestR_OCI_026_ATagOfAnIndexNamesThePlatformsOfTheRepositorysList: the probe of a tag whose manifest is an index names the manifests
// of the platforms of oci.platforms (linux/amd64 and linux/arm64 while the repository sets none) and their attestations, and none of
// another platform.
func TestR_OCI_026_ATagOfAnIndexNamesThePlatformsOfTheRepositorysList(t *testing.T) {
	_, amdD := imageManifest("amd", "base", "amd")
	_, armD := imageManifest("arm", "base", "arm")
	_, s390D := imageManifest("s390", "base", "s390")
	_, attD := imageManifest("att-amd", "sbom")
	index := imageIndex(
		indexEntry{digest: amdD, platform: "linux/amd64"},
		indexEntry{digest: armD, platform: "linux/arm64/v8"},
		indexEntry{digest: s390D, platform: "linux/s390x"},
		indexEntry{digest: attD, annotations: map[string]string{"vnd.docker.reference.type": "attestation-manifest", "vnd.docker.reference.digest": amdD}},
	)
	fx, mem := remoteFixture(t, map[string][]byte{TagPath("multi", "1"): index}, nil, nil)
	fx.wantStatus(fx.get("multi/manifests/1"), http.StatusOK, "")
	got := mem.ProbesSeen()
	if len(got) != 1 {
		t.Fatalf("%d probes", len(got))
	}
	paths := probePathsOf(got[0])
	sort.Strings(paths)
	want := []string{ManifestPath("multi", amdD), ManifestPath("multi", armD), ManifestPath("multi", attD), ManifestPath("multi", sha(index))}
	sort.Strings(want)
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("files %v, want %v (no linux/s390x)", paths, want)
	}
}

// TestR_REPO_065_TheContentOfAProbedVersion: ProbeContent reads every manifest through the upstream and returns what it names: of an
// index the manifests of the platforms of oci.platforms with their attestations, of each image its config and its layers, without a
// foreign layer and without the platforms the list leaves out.
func TestR_REPO_065_TheContentOfAProbedVersion(t *testing.T) {
	amd, amdD := imageManifest("amd", "base", "amd")
	arm, armD := imageManifest("arm", "base", "arm")
	s390, s390D := imageManifest("s390", "base", "s390")
	index := imageIndex(
		indexEntry{digest: amdD, platform: "linux/amd64"},
		indexEntry{digest: armD, platform: "linux/arm64"},
		indexEntry{digest: s390D, platform: "linux/s390x"},
	)
	idxD := sha(index)
	foreign := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digestOfString("wcfg") + `","size":2},"layers":[{"mediaType":"application/vnd.docker.image.rootfs.foreign.diff.tar.gzip","digest":"` + digestOfString("wlayer") + `","size":3,"urls":["https://example.com/l"]}]}`)
	for name, tc := range map[string]struct {
		settings string
		files    []string
		want     []string
		not      []string
	}{
		"an index with the default platforms": {
			settings: `{}`, files: []string{ManifestPath("img", idxD)},
			want: []string{ManifestPath("img", amdD), ManifestPath("img", armD), "img/_blobs/" + digestOfString("layer-base")},
			not:  []string{ManifestPath("img", s390D), "img/_blobs/" + digestOfString("layer-s390")},
		},
		"an index with every platform": {
			settings: `{"platforms":[]}`, files: []string{ManifestPath("img", idxD)},
			want: []string{ManifestPath("img", s390D), "img/_blobs/" + digestOfString("layer-s390")},
		},
		"an image with a foreign layer": {
			settings: `{}`, files: []string{ManifestPath("img", sha(foreign))},
			want: []string{"img/_blobs/" + digestOfString("wcfg")},
			not:  []string{"img/_blobs/" + digestOfString("wlayer")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			u := newFake(t, "https://registry.example.com/", tc.settings)
			for d, b := range map[string][]byte{idxD: index, amdD: amd, armD: arm, s390D: s390, sha(foreign): foreign} {
				u.artifacts[ManifestPath("img", d)] = b
			}
			p := formats.Probe{Name: "img", Version: "1"}
			for _, f := range tc.files {
				p.Files = append(p.Files, formats.Path{Path: f})
			}
			items, err := adapter().ProbeContent(context.Background(), u, p)
			if err != nil {
				t.Fatal(err)
			}
			have := map[string]bool{}
			for _, it := range items {
				if have[it.Path] {
					t.Errorf("%s listed twice", it.Path)
				}
				have[it.Path] = true
			}
			for _, w := range append(tc.want, tc.files...) {
				if !have[w] {
					t.Errorf("missing %s in %v", w, keysOf(have))
				}
			}
			for _, n := range tc.not {
				if have[n] {
					t.Errorf("%s is listed", n)
				}
			}
		})
	}
}

func keysOf(m map[string]bool) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}
