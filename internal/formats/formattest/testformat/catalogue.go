package testformat

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// The floor of the upstream catalogue (remote-curation.md R-CUR-007): a lookup by exact name reads the package's document, whose
// versions are listed in reverse text order; the format searches nothing and lists no levels.

var _ formats.Catalogue = remoteAdapter{}

// CatalogueCaps implements discover.Catalogue.
func (remoteAdapter) CatalogueCaps(context.Context, discover.Upstream) (discover.CatalogueCaps, error) {
	return discover.CatalogueCaps{
		Search: discover.SearchNone, SearchHint: discover.SearchHintNoSearch,
		LevelsReason: "the kit's test format has no levels", Sizes: "unknown",
	}, nil
}

// Search implements discover.Catalogue.
func (remoteAdapter) Search(context.Context, discover.Upstream, discover.SearchQuery) (discover.SearchPage, error) {
	return discover.SearchPage{}, discover.ErrNotOffered
}

// Children implements discover.Catalogue.
func (remoteAdapter) Children(context.Context, discover.Upstream, discover.LevelRequest) (discover.LevelPage, error) {
	return discover.LevelPage{}, discover.ErrNotOffered
}

// testDocument is the part of a package document the catalogue reads.
type testDocument struct {
	Name     string `json:"name"`
	Versions map[string]struct {
		Dist struct {
			Tarball string `json:"tarball"`
		} `json:"dist"`
	} `json:"versions"`
}

func (remoteAdapter) document(ctx context.Context, u discover.Upstream, name string) (testDocument, bool, error) {
	raw, found, err := u.Document(ctx, name, "")
	if err != nil || !found {
		return testDocument{}, false, err
	}
	var d testDocument
	if err := json.Unmarshal(raw, &d); err != nil {
		return testDocument{}, false, err
	}
	return d, true, nil
}

// Package implements discover.Catalogue.
func (a remoteAdapter) Package(ctx context.Context, u discover.Upstream, name string, rq discover.VersionsRequest) (discover.PackageInfo, error) {
	d, found, err := a.document(ctx, u, name)
	if err != nil || !found {
		return discover.PackageInfo{}, err
	}
	vs := make([]string, 0, len(d.Versions))
	for v := range d.Versions {
		vs = append(vs, v)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(vs)))
	all := make([]discover.VersionFacts, 0, len(vs))
	for _, v := range vs {
		if a.f.has(string(CatalogueRawVersions)) {
			v = "v " + v
		}
		all = append(all, discover.VersionFacts{Version: v})
	}
	info := discover.PackageInfo{Found: true, Name: name}
	info.Versions, info.Total, info.Cursor, info.Done = discover.PageVersions(all, rq)
	return info, nil
}

// VersionProbe implements formats.VersionProber: the version's tarball at the repository's path.
func (a remoteAdapter) VersionProbe(ctx context.Context, u discover.Upstream, name, version string) (formats.Probe, bool, error) {
	d, found, err := a.document(ctx, u, name)
	if err != nil || !found {
		return formats.Probe{}, false, err
	}
	if _, ok := d.Versions[version]; !ok {
		return formats.Probe{}, false, nil
	}
	return formats.Probe{Name: name, Version: version, Files: []formats.Path{{Path: name + "/-/" + name + "-" + version + ".tgz"}}}, true, nil
}
