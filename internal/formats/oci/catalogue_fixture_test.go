package oci

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// catalogueFixture is check 22's fixture (R-CUR-007, R-OCI-038): one case per preset and one registry without a preset, each served the
// answers its operator gave on 2026-11-24 (testdata/catalogue/README.md) at the URLs the format sends, with a level each preset's
// operator does not have.
func catalogueFixture() *formattest.CatalogueFixture {
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", "catalogue", name))
		if err != nil {
			panic(err)
		}
		return b
	}
	ok := func(name string) formattest.CannedAnswer { return formattest.CannedAnswer{Body: read(name)} }
	const (
		hub    = "https://hub.docker.com/v2/"
		quay   = "https://quay.io/api/v1/"
		gl     = "https://gitlab.com/api/v4/projects/gitlab-org%2Fgitlab-runner/registry/repositories?page="
		suse   = "https://registry.opensuse.org/v2/"
		runner = "gitlab-org/gitlab-runner/ubi-fips-base"
		// glAbsent is the registry listing of a project gitlab.com does not have.
		glAbsent = "https://gitlab.com/api/v4/projects/guidedtraffic%2Fabsent/registry/repositories?page=1&per_page=10"
	)
	// hubOrganization is the account of an organization: a redirect to its own document.
	hubOrganization := formattest.CannedAnswer{Status: http.StatusPermanentRedirect, Header: http.Header{"Location": {"/v2/orgs/guidedtraffic"}}}
	return &formattest.CatalogueFixture{Cases: []formattest.CatalogueCase{{
		Name: "Docker Hub", Base: "https://registry-1.docker.io/", Limit: 5,
		Answers: map[string]formattest.CannedAnswer{
			hub + "search/repositories/?page=1&page_size=5&query=theupdateframework":      ok("hub-search-1.json"),
			hub + "search/repositories/?page=2&page_size=5&query=theupdateframework":      ok("hub-search-2.json"),
			hub + "namespaces/nginxinc/repositories?page=1&page_size=5":                   ok("hub-ns-1.json"),
			hub + "namespaces/nginxinc/repositories?page=2&page_size=5":                   ok("hub-ns-2.json"),
			hub + "namespaces/nginxinc/repositories?page=3&page_size=5":                   ok("hub-ns-3.json"),
			hub + "namespaces/nginxinc/repositories/ingress-demo/tags?page=1&page_size=5": ok("hub-tags-1.json"),
			hub + "users/guidedtraffic":                                                   hubOrganization,
		},
		Search: "theupdateframework", Level: []string{"nginxinc"}, Package: "nginxinc/ingress-demo", Absent: "nginxinc/absent-image",
		// The owner's namespace typed with the hyphen of his domain: no namespace holds one, so nothing asks for it, and the name
		// without it is the organization Docker Hub has (Q-CUR-165).
		UnknownLevel: []string{"guided-traffic"}, UnknownSuggestion: "guidedtraffic",
	}, {
		Name: "Quay", Base: "https://quay.io/", Limit: 10,
		Answers: map[string]formattest.CannedAnswer{
			quay + "find/repositories?page=1&query=rekor-server":                        ok("quay-search-1.json"),
			quay + "find/repositories?page=2&query=rekor-server":                        ok("quay-search-2.json"),
			quay + "repository?namespace=coreos&public=true":                            ok("quay-ns.json"),
			quay + "repository/coreos/airlock/tag/?limit=10&onlyActiveTags=true&page=1": ok("quay-tags-1.json"),
			quay + "repository?namespace=guidedtraffic&public=true":                     ok("quay-ns-guidedtraffic.json"),
			quay + "organization/guidedtraffic":                                         {Status: http.StatusNotFound, Body: read("quay-org-guidedtraffic.json")},
			quay + "users/guidedtraffic":                                                {Status: http.StatusNotFound, Body: read("quay-user-guidedtraffic.json")},
		},
		Search: "rekor-server", Level: []string{"coreos"}, Package: "coreos/airlock", Absent: "coreos/absent-image",
		UnknownLevel: []string{"guidedtraffic"},
	}, {
		Name: "ghcr.io", Base: "https://ghcr.io/", Limit: 3,
		Answers: map[string]formattest.CannedAnswer{"https://ghcr.io/v2/homebrew/core/hello/tags/list?n=1000": ok("ghcr-tags.json")},
		Package: "homebrew/core/hello", Absent: "homebrew/core/absent",
	}, {
		Name: "GitLab", Base: "https://registry.gitlab.com/", Limit: 10,
		Answers: map[string]formattest.CannedAnswer{
			gl + "1&per_page=10": {Body: read("gl-project-1.json"), Header: http.Header{"X-Next-Page": {"2"}, "X-Total": {"20"}}},
			gl + "2&per_page=10": {Body: read("gl-project-2.json"), Header: http.Header{"X-Next-Page": {""}, "X-Total": {"20"}}},
			"https://registry.gitlab.com/v2/" + runner + "/tags/list?n=1000": ok("gl-tags.json"),
			glAbsent: {Status: http.StatusNotFound, Body: read("gl-project-absent.json")},
		},
		Level: []string{"gitlab-org/gitlab-runner"}, Package: runner, Absent: "gitlab-org/gitlab-runner/absent",
		UnknownLevel: []string{"guidedtraffic/absent"},
	}, {
		// A registry without a preset whose catalog answers: its root lists images. The second page's Link is left out so that the
		// listing ends.
		Name: "a registry without a preset", Base: "https://registry.opensuse.org/", Limit: 2,
		Answers: map[string]formattest.CannedAnswer{
			suse + "_catalog?n=1": ok("suse-cat-probe.json"),
			suse + "_catalog?n=2": {Body: read("suse-cat-1.json"), Header: http.Header{
				"Link": {`</v2/_catalog?n=2&last=benchmark/ltp/devel/containers/opensuse/ltp>; rel="next"`},
			}},
			suse + "_catalog?last=benchmark%2Fltp%2Fdevel%2Fcontainers%2Fopensuse%2Fltp&n=2": ok("suse-cat-2.json"),
			suse + "benchmark/ltp/devel/containers/opensuse/kirk/tags/list?n=1000":           ok("suse-tags.json"),
		},
		Level: []string{}, Package: "benchmark/ltp/devel/containers/opensuse/kirk", Absent: "benchmark/absent",
	}}}
}
