# The OCI catalogue's recorded answers (kit check 22)

Answers the registries' operators gave on 2026-11-24, kept as they came, for the fixture of kit check 22
([catalogue_fixture_test.go](../../catalogue_fixture_test.go), remote-curation.md R-CUR-007, formats/oci.md R-OCI-038):

| File | Request |
|---|---|
| `hub-search-1.json`, `hub-search-2.json` | `GET https://hub.docker.com/v2/search/repositories/?page=1&page_size=5&query=theupdateframework` and `page=2` (count 9) |
| `hub-ns-1.json` to `hub-ns-3.json` | `GET https://hub.docker.com/v2/namespaces/nginxinc/repositories?page=1&page_size=5`, pages 1 to 3 (count 14) |
| `hub-tags-1.json` | `GET https://hub.docker.com/v2/namespaces/nginxinc/repositories/ingress-demo/tags?page=1&page_size=5` |
| `quay-search-1.json`, `quay-search-2.json` | `GET https://quay.io/api/v1/find/repositories?page=1&query=rekor-server` and `page=2` |
| `quay-ns.json` | `GET https://quay.io/api/v1/repository?namespace=coreos&public=true` |
| `quay-tags-1.json` | `GET https://quay.io/api/v1/repository/coreos/airlock/tag/?limit=10&onlyActiveTags=true&page=1` |
| `ghcr-tags.json` | `GET https://ghcr.io/v2/homebrew/core/hello/tags/list?n=1000` with an anonymous token |
| `gl-project-1.json`, `gl-project-2.json` | GitLab's project API: the registry repositories of the project gitlab-org/gitlab-runner, `per_page=10`, pages 1 and 2 (the fixture gives them the `X-Next-Page` and `X-Total` the API sent) |
| `gl-tags.json` | the tags list of gitlab-org/gitlab-runner/ubi-fips-base on registry.gitlab.com, `n=1000`, with an anonymous token |
| `suse-cat-probe.json`, `suse-cat-1.json`, `suse-cat-2.json` | `GET https://registry.opensuse.org/v2/_catalog?n=1`, `?n=2` and the page after it; the fixture leaves out the second page's `Link`, so that the listing ends |
| `suse-tags.json` | `GET https://registry.opensuse.org/v2/benchmark/ltp/devel/containers/opensuse/kirk/tags/list?n=1000` |
