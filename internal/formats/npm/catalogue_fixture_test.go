package npm

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// catalogueFixture is check 22's fixture (R-CUR-007, R-NPM-049): answers the public registry and its replication host gave on
// 2026-11-24 (testdata/catalogue/README.md), served by the kit's fake upstream at the URLs the format sends with a page of two.
func catalogueFixture(t testing.TB) *formattest.CatalogueFixture {
	t.Helper()
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", "catalogue", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	const (
		search   = "https://registry.npmjs.org/-/v1/search?text=tufjs&size=2&from="
		scope    = "https://replicate.npmjs.com/registry/_all_docs?startkey=%22%40tufjs%2F"
		endLimit = "%22&endkey=%22%40tufjs%2F%EF%BF%B0%22&limit="
		// unknownScope is the key range of a scope the registry does not have.
		unknownScope = "https://replicate.npmjs.com/registry/_all_docs?startkey=%22%40guidedtraffic%2F%22&endkey=%22%40guidedtraffic%2F%EF%BF%B0%22&limit=3"
	)
	return &formattest.CatalogueFixture{Cases: []formattest.CatalogueCase{{
		Name: "the public registry", Base: publicRegistryUpstream,
		Answers: map[string]formattest.CannedAnswer{
			search + "0":                   {Body: read("search-tufjs-1.json")},
			search + "2":                   {Body: read("search-tufjs-2.json")},
			scope + endLimit + "3":         {Body: read("scope-tufjs-1.json")},
			scope + "cli" + endLimit + "4": {Body: read("scope-tufjs-2.json")},
			unknownScope:                   {Body: read("scope-guidedtraffic.json")},
			"https://registry.npmjs.org/-/org/guidedtraffic/package": {Status: http.StatusNotFound, Body: read("org-guidedtraffic.json")},
		},
		Documents: map[string][]byte{"left-pad\x00" + documentVariant: read("left-pad.json")},
		Search:    "tufjs", Level: []string{"@tufjs"}, Package: "left-pad", Absent: "left-pad-absent", Limit: 2,
		// A scope the registry does not have: its key range is empty, and so is that of a scope it has without a scoped package, so the
		// scope's account tells them apart (Q-CUR-165).
		UnknownLevel: []string{"@guidedtraffic"},
	}, {
		// Another registry has no scope level, and one whose search route answers 404 says so (R-NPM-049).
		Name: "a registry without the search route", Base: "https://npm.example.org/",
		Documents: map[string][]byte{"left-pad\x00" + documentVariant: read("left-pad.json")},
		Package:   "left-pad", Absent: "left-pad-absent", Limit: 2, Search: "tufjs",
		ExpectSearchUnavailable: true,
	}}}
}
