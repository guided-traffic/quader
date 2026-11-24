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
