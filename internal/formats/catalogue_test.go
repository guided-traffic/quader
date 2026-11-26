package formats

import (
	"context"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// testCatalogue is the floor of the upstream catalogue that the contract's fakes with the class remote declare (R-CUR-007).
var testCatalogue = CatalogueDeclaration{Lookup: true, NoSearch: "a format of the contract's tests", NoLevels: "a format of the contract's tests"}

// anyCanon takes every non-empty version as given (R-CUR-006).
type anyCanon struct{}

func (anyCanon) Canonical(v string) (string, bool) { return v, v != "" }

// catalogueRemote is the upstream catalogue of the contract's fake remote adapters: RemoteCaps.Catalogue finds it.
type catalogueRemote struct{}

func (catalogueRemote) CatalogueCaps(context.Context, discover.Upstream) (discover.CatalogueCaps, error) {
	return discover.CatalogueCaps{Search: discover.SearchNone}, nil
}

func (catalogueRemote) Search(context.Context, discover.Upstream, discover.SearchQuery) (discover.SearchPage, error) {
	return discover.SearchPage{}, discover.ErrNotOffered
}

func (catalogueRemote) Children(context.Context, discover.Upstream, discover.LevelRequest) (discover.LevelPage, error) {
	return discover.LevelPage{}, discover.ErrNotOffered
}

func (catalogueRemote) Package(context.Context, discover.Upstream, string, discover.VersionsRequest) (discover.PackageInfo, error) {
	return discover.PackageInfo{}, nil
}

func (catalogueRemote) VersionProbe(context.Context, discover.Upstream, string, string) (Probe, bool, error) {
	return Probe{}, false, nil
}

// TestR_CUR_007_RegisterRefusesARemoteFormatWithoutACatalogue: a format with the class remote declares its upstream catalogue, whose
// floor is the lookup by exact name, a reason for each call it does not answer and never both, and its version canon; a format without
// the class declares none (Q-CUR-12).
func TestR_CUR_007_RegisterRefusesARemoteFormatWithoutACatalogue(t *testing.T) {
	remote := func(id string, c CatalogueDeclaration, canon catalog.VersionCanon) fake {
		d := valid(id)
		d.Classes, d.Catalogue, d.VersionCanon = []Class{Local, Remote}, c, canon
		d.DownloadUnit = DownloadUnitFile
		return fake{d: d, remote: stubRemote{}}
	}
	for name, c := range map[string]struct {
		decl  CatalogueDeclaration
		canon catalog.VersionCanon
		want  string
	}{
		"no declaration":       {CatalogueDeclaration{}, anyCanon{}, "declares no upstream catalogue"},
		"no lookup":            {CatalogueDeclaration{NoSearch: "x", NoLevels: "y"}, anyCanon{}, "the lookup by exact name is the floor"},
		"search and a reason":  {CatalogueDeclaration{Lookup: true, Search: true, NoSearch: "x", NoLevels: "y"}, anyCanon{}, "search and a reason"},
		"neither search":       {CatalogueDeclaration{Lookup: true, NoLevels: "y"}, anyCanon{}, "search and a reason"},
		"levels and a reason":  {CatalogueDeclaration{Lookup: true, NoSearch: "x", Levels: []string{"scope"}, NoLevels: "y"}, anyCanon{}, "levels and a reason"},
		"neither levels":       {CatalogueDeclaration{Lookup: true, NoSearch: "x"}, anyCanon{}, "levels and a reason"},
		"a level without name": {CatalogueDeclaration{Lookup: true, Search: true, Levels: []string{" "}}, anyCanon{}, "without a name"},
		"no version canon":     {testCatalogue, nil, "no version canon"},
	} {
		t.Run("R-CUR-007 Register panics on "+name, func(t *testing.T) {
			mustPanic(t, remote("cat-bad", c.decl, c.canon), c.want)
		})
	}
	t.Run("R-CUR-007 a format without the class remote declares no catalogue", func(t *testing.T) {
		d := valid("cat-local")
		d.Catalogue = testCatalogue
		mustPanic(t, fake{d: d}, "has no class remote")
	})
	t.Run("R-CUR-007 Register panics on a remote adapter that does not implement the catalogue", func(t *testing.T) {
		f := remote("cat-bare", testCatalogue, anyCanon{})
		f.remote = bareRemote{}
		mustPanic(t, f, "does not implement formats.Catalogue")
	})
	t.Run("R-CUR-007 the floor registers, and the canon reaches the catalog", func(t *testing.T) {
		Register(remote("cat-floor", testCatalogue, anyCanon{}))
		if v, ok := catalog.Default().CanonicalVersion("cat-floor", "1.0"); !ok || v != "1.0" {
			t.Fatalf("CanonicalVersion = %q, %v", v, ok)
		}
	})
	t.Run("R-CUR-007 CapabilitiesOf finds an adapter's catalogue", func(t *testing.T) {
		f := remote("cat-full", CatalogueDeclaration{Lookup: true, Search: true, Levels: []string{"scope"}}, anyCanon{})
		Register(f)
		if RemoteCapabilities("cat-full").Catalogue == nil {
			t.Fatal("RemoteCaps.Catalogue is nil for an adapter that implements Catalogue")
		}
	})
}
