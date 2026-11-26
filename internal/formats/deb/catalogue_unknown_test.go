package deb

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// releases is an upstream whose documents are the recorded releases by path; every other path is not there. reads counts Document.
type releases struct {
	docs  map[string][]byte
	reads int
}

func (*releases) Base() *url.URL {
	u, _ := url.Parse("https://download.docker.com/linux/debian/")
	return u
}
func (*releases) Settings() json.RawMessage { return nil }
func (*releases) Get(context.Context, *url.URL, string, int64) (*discover.Response, error) {
	return &discover.Response{Status: 404}, nil
}

func (r *releases) Document(_ context.Context, path, _ string) ([]byte, bool, error) {
	r.reads++
	d, ok := r.docs[path]
	return d, ok, nil
}

func (*releases) Artifact(context.Context, string, int64) ([]byte, bool, error) {
	return nil, false, nil
}

// TestR_CUR_007_ADebianLevelTheUpstreamDoesNotHaveIsUnknown: a suite, a component or an architecture that lists nothing is unknown when
// the upstream has no release of the suite or its release does not name it, from download.docker.com's InRelease of trixie recorded on
// 2026-11-24 (Components stable edge test nightly; Architectures amd64 arm64 armhf s390x ppc64el), and empty when the release names it
// and Quader holds no index of it (Q-CUR-165). Milliseconds, one document of 32 KB.
func TestR_CUR_007_ADebianLevelTheUpstreamDoesNotHaveIsUnknown(t *testing.T) {
	in, err := os.ReadFile(filepath.Join("testdata", "catalogue", "trixie-InRelease"))
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		path    []string
		unknown bool
		reads   int
	}{
		"a suite without a release":                 {[]string{"trixy"}, true, 2},
		"a component the release does not name":     {[]string{"trixie", "unstable"}, true, 1},
		"a component the release names":             {[]string{"trixie", "nightly"}, false, 1},
		"an architecture the release does not name": {[]string{"trixie", "stable", "riscv64"}, true, 1},
		"an architecture the release names":         {[]string{"trixie", "stable", "arm64"}, false, 1},
		"a suite with a release":                    {[]string{"trixie"}, false, 1},
	} {
		t.Run("R-CUR-007 "+name, func(t *testing.T) {
			u := &releases{docs: map[string][]byte{"dists/trixie/InRelease": in}}
			p, err := levelKnown(context.Background(), u, c.path)
			if err != nil {
				t.Fatal(err)
			}
			if (p.Unknown != nil) != c.unknown || !p.Done || u.reads != c.reads {
				t.Fatalf("unknown %v, done %v, reads %d; want unknown %v, %d reads", p.Unknown != nil, p.Done, u.reads, c.unknown, c.reads)
			}
		})
	}
}

// idUpstream is the upstream of a remote repository with an id, which the catalogue's reads of the parsed indexes take.
type idUpstream struct{ *releases }

func (idUpstream) RepositoryID() int64 { return 1 }

// TestR_UI_221_TheCatalogueNamesArchitecturesInDebiansWords: a version of the tab Upstream lists its architectures as the index names
// them (`amd64`), not as an image's platform (`linux/amd64`), the word of every other Debian screen (Q-UI-1310). Milliseconds, the
// eight paragraphs of the catalogue's recorded index.
func TestR_UI_221_TheCatalogueNamesArchitecturesInDebiansWords(t *testing.T) {
	twin, err := newCatalogueTwin()
	if err != nil {
		t.Fatal(err)
	}
	f := New()
	f.Bind(formattest.NewHost(f, formattest.HostOptions{Class: formats.Remote, Data: twin, Resolver: noShadow{}}), nil, nil)
	info, err := remoteAdapter{f: f}.Package(context.Background(), idUpstream{&releases{}}, "docker-secrets-engine", discover.VersionsRequest{Limit: 10})
	if err != nil || len(info.Versions) == 0 {
		t.Fatalf("%v %+v", err, info)
	}
	for _, v := range info.Versions {
		if !slices.Equal(v.Platforms, []string{"amd64"}) {
			t.Fatalf("%s lists %v, want [amd64]", v.Version, v.Platforms)
		}
	}
}
