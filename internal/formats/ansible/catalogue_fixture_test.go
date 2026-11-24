package ansible

import (
	"os"
	"path/filepath"

	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// catalogueFixture is check 22's fixture (R-CUR-007, R-ANS-047): answers the public Galaxy gave on 2026-11-24
// (testdata/catalogue/README.md): its API root, two pages of the keyword search keycloak_realm (count 3), three pages of the namespace
// osism (six collections; the index answers through a redirect, whose final body is kept at the address the format asks), and the
// version list and the collection document of osism.sonic (two versions).
func catalogueFixture() *formattest.CatalogueFixture {
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", "catalogue", name))
		if err != nil {
			panic(err)
		}
		return b
	}
	const (
		api    = "https://galaxy.ansible.com/api/v3/"
		search = api + "plugin/ansible/search/collection-versions/?is_highest=true&keywords=keycloak_realm&limit=2&offset="
		ns     = api + "collections/?limit=2&namespace=osism&offset="
	)
	return &formattest.CatalogueFixture{Cases: []formattest.CatalogueCase{{
		Name: "the public Galaxy", Base: "https://galaxy.ansible.com/api/", Limit: 2,
		Answers: map[string]formattest.CannedAnswer{
			search + "0": {Body: read("gal-search-1.json")},
			search + "2": {Body: read("gal-search-2.json")},
			ns + "0":     {Body: read("gal-ns-0.json")},
			ns + "2":     {Body: read("gal-ns-2.json")},
			ns + "4":     {Body: read("gal-ns-4.json")},
		},
		Documents: map[string][]byte{
			RootDocument + "\x00" + documentVariant:                             read("gal-root.json"),
			VersionsDocumentPath("osism", "sonic") + "\x00" + offsetVariant(0):  read("gal-versions.json"),
			CollectionDocumentPath("osism", "sonic") + "\x00" + documentVariant: read("gal-collection.json"),
		},
		Search: "keycloak_realm", Level: []string{"osism"}, Package: "osism.sonic", Absent: "osism.absent",
	}, {
		// A Galaxy that answers the keyword search with 404 has none for the cache period (R-ANS-047).
		Name: "a Galaxy without the keyword search", Base: "https://galaxy.example/api/", Limit: 2,
		Documents: map[string][]byte{
			RootDocument + "\x00" + documentVariant:                            read("gal-root.json"),
			VersionsDocumentPath("osism", "sonic") + "\x00" + offsetVariant(0): read("gal-versions.json"),
		},
		Answers: map[string]formattest.CannedAnswer{
			"https://galaxy.example/api/v3/collections/?limit=2&namespace=osism&offset=0": {Body: read("gal-ns-0.json")},
			"https://galaxy.example/api/v3/collections/?limit=2&namespace=osism&offset=2": {Body: read("gal-ns-2.json")},
			"https://galaxy.example/api/v3/collections/?limit=2&namespace=osism&offset=4": {Body: read("gal-ns-4.json")},
		},
		Search: "keycloak_realm", ExpectSearchUnavailable: true, Level: []string{"osism"}, Package: "osism.sonic", Absent: "osism.absent",
	}}}
}
