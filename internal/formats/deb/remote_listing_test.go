package deb

import (
	"context"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/deb/rows"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// entryPort is the data port with the parsed index rows of a remote (R-DEB-022) the test names.
type entryPort struct {
	twinPort
	entries map[string]rows.Entry
}

func (p entryPort) EntryDigest(_ context.Context, _ int64, path string) (rows.Entry, bool, error) {
	e, ok := p.entries[path]
	return e, ok, nil
}

// TestQ_REPO_330_ARemoteListsThePackageFileItCachedAndItsRemovalTakesTheVersion: the fill of a pool file of a remote writes the version
// its parsed index row names, the first file its lead and another architecture's beside it (FetchAnyFile counts both, R-API-079); a
// source package's file, a file no parsed index lists and an index get no row; the removal of the lead takes the version with it
// (R-API-081).
func TestQ_REPO_330_ARemoteListsThePackageFileItCachedAndItsRemovalTakesTheVersion(t *testing.T) {
	const (
		amd = "pool/main/h/hello/hello_1.0-1_amd64.deb"
		arm = "pool/main/h/hello/hello_1.0-1_arm64.deb"
		dsc = "pool/main/h/hello/hello_1.0-1.dsc"
	)
	port := entryPort{twinPort: twinPort{twinStore: newTwin()}, entries: map[string]rows.Entry{
		amd: {Package: "hello", Version: "1.0-1", Architecture: "amd64", Path: amd},
		arm: {Package: "hello", Version: "1.0-1", Architecture: "arm64", Path: arm},
		dsc: {Package: "hello", Version: "1.0-1", Architecture: "source", Path: dsc},
	}}
	f := New()
	h := formattest.NewHost(f, formattest.HostOptions{Class: formats.Remote, Data: port, Resolver: noShadow{}, Remote: &formattest.MemRemote{}})
	ctx := context.Background()
	fill := func(path string) formats.ItemRef {
		t.Helper()
		it := h.PutItem(path, "application/octet-stream", []byte(path))
		if err := h.Write(ctx, nil, func(tx formats.Tx, w formats.PackageWriter) error {
			return f.OnCommit(ctx, tx, formats.CommitEvent{Origin: formats.OriginFill, Repo: h.Ref(), Item: it, Path: formats.Path{Path: path}, Packages: w, Data: port})
		}); err != nil {
			t.Fatalf("fill %s: %v", path, err)
		}
		return it
	}
	for _, p := range []string{dsc, "pool/main/o/other/other_2.0_amd64.deb", "dists/stable/InRelease"} {
		fill(p)
	}
	if _, found, _ := h.Packages().Package(ctx, h.Ref(), "other"); found {
		t.Fatal("a file no parsed index lists has a package")
	}
	if vs, _, _ := h.Packages().Versions(ctx, h.Ref(), "hello"); len(vs) != 0 {
		t.Fatalf("a source package's file made versions %v", vs)
	}

	lead := fill(amd)
	fill(arm)
	vs, _, err := h.Packages().Versions(ctx, h.Ref(), "hello")
	if err != nil || len(vs) != 1 || vs[0].Version != "1.0-1" || vs[0].LeadItem != lead.ID {
		t.Fatalf("versions = %+v, %v: want 1.0-1 led by the amd64 file", vs, err)
	}

	if err := h.Remove(ctx, nil, "delete", amd); err != nil {
		t.Fatal(err)
	}
	if vs, _, _ := h.Packages().Versions(ctx, h.Ref(), "hello"); len(vs) != 0 {
		t.Fatalf("the version outlived its lead file: %+v", vs)
	}
}
