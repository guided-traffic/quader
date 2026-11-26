package nuget

import (
	"net/url"
	"os"
	"path/filepath"

	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/formats/nuget/remote"
)

// catalogueFixture is check 22's fixture (R-CUR-007, R-NUGET-035): answers nuget.org gave on 2026-11-24 (testdata/catalogue/README.md):
// its service index, two pages of two hits of the search owner:jamesnk (totalHits 4), and the version list and registration of
// Newtonsoft.Json.Bson (nine versions, prereleases among them). A repository whose service index names no search resource has none.
func catalogueFixture() *formattest.CatalogueFixture {
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", "catalogue", name))
		if err != nil {
			panic(err)
		}
		return b
	}
	index := read("service-index.json")
	d, err := remote.ParseDiscovery(index)
	if err != nil {
		panic(err)
	}
	search := func(skip string) string {
		u, _ := d.SearchURL(url.Values{"q": {"owner:jamesnk"}, "prerelease": {"true"}, "semVerLevel": {"2.0.0"}, "take": {"2"}, "skip": {skip}})
		return u.String()
	}
	docs := map[string][]byte{
		".nuget/index.json\x00" + documentVariant:                                index,
		PackageDocumentPath("newtonsoft.json.bson") + "\x00" + documentVariant:   read("newtonsoft.json.bson-versions.json"),
		RegistrationIndexPath("newtonsoft.json.bson") + "\x00" + documentVariant: read("newtonsoft.json.bson-registration.json"),
	}
	noSearch := map[string][]byte{}
	for k, v := range docs {
		noSearch[k] = v
	}
	noSearch[".nuget/index.json\x00"+documentVariant] = []byte(`{"version":"3.0.0","resources":[` +
		`{"@id":"https://feed.example/v3-flatcontainer/","@type":"PackageBaseAddress/3.0.0"},` +
		`{"@id":"https://feed.example/v3/registration/","@type":"RegistrationsBaseUrl/3.6.0"}]}`)
	return &formattest.CatalogueFixture{Cases: []formattest.CatalogueCase{{
		Name: "nuget.org", Base: "https://api.nuget.org/v3/index.json",
		Answers: map[string]formattest.CannedAnswer{
			"https://api.nuget.org/v3-flatcontainer/newtonsoft.json.bson/1.0.3/readme": {Body: read("newtonsoft.json-13.0.4-readme.md")},
			search("0"): {Body: read("search-owner-jamesnk-1.json")},
			search("2"): {Body: read("search-owner-jamesnk-2.json")},
		},
		Documents: docs, Search: "owner:jamesnk", Package: "newtonsoft.json.bson", Absent: "acme.absent", Limit: 2,
		// The newest leaf's licence, and the README at the address of the service index's ReadmeUriTemplate (R-CUR-028). Bson 1.0.3 has
		// none (404, recorded 2026-11-26): the README Newtonsoft.Json 13.0.4 has is served at its address so that the case reads one.
		License: "MIT", Readme: "Json.NET",
	}, {
		Name: "a feed without a search resource", Base: "https://feed.example/v3/index.json",
		Documents: noSearch, Package: "newtonsoft.json.bson", Absent: "acme.absent", Limit: 4,
		// A service index without ReadmeUriTemplate/6.13.0 names no README (R-CUR-028).
		License: "MIT", NoReadme: true,
	}}}
}
