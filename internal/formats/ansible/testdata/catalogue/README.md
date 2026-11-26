# The Ansible Galaxy catalogue's recorded answers (kit check 22)

Answers the public Galaxy gave on 2026-11-24, kept as they came, for the fixture of kit check 22
([catalogue_fixture_test.go](../../catalogue_fixture_test.go), remote-curation.md R-CUR-007, formats/ansible.md R-ANS-047):

| File | Request |
|---|---|
| `gal-root.json` | `GET https://galaxy.ansible.com/api/` |
| `gal-search-1.json`, `gal-search-2.json` | `GET https://galaxy.ansible.com/api/v3/plugin/ansible/search/collection-versions/?is_highest=true&keywords=keycloak_realm&limit=2&offset=0` and `offset=2` (count 3) |
| `gal-ns-0.json`, `gal-ns-2.json`, `gal-ns-4.json` | `GET https://galaxy.ansible.com/api/v3/collections/?limit=2&namespace=osism&offset=0`, `2` and `4`: the final body of its redirect to the collection index |
| `gal-versions.json` | the version list of osism.sonic, `limit=100&offset=0` |
| `gal-collection.json` | the collection document of osism.sonic |
| `gal-version.json` | `GET https://galaxy.ansible.com/api/v3/plugin/ansible/content/published/collections/index/osism/sonic/versions/2.0.1/`, recorded 2026-11-26 with `files`, `manifest` and `metadata.contents` emptied (124 KiB to 2 KiB): the tags and the signatures of the highest version (R-ANS-047, Q-ANS-129) |

## A namespace the Galaxy does not have (Q-CUR-165)

Answers of 2026-11-24, 08:23 local time. A `HEAD` of a namespace's document answered the status of its `GET`.

| File | Request | Answer |
|---|---|---|
| `gal-ns-guidedtraffic.json` | `GET https://galaxy.ansible.com/api/v3/collections/?limit=2&namespace=guidedtraffic&offset=0`: the final body of its redirect to the collection index | 200, count 0 |
| `gal-namespace-guidedtraffic.json` | `GET https://galaxy.ansible.com/api/v3/namespaces/guidedtraffic/` | 404 |
| `gal-namespace-netbox_community.json` | `GET https://galaxy.ansible.com/api/v3/namespaces/netbox_community/` | 200: the namespace of the GitHub organisation netbox-community |
