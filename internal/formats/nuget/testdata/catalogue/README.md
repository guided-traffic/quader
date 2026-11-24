# The NuGet catalogue's recorded answers (kit check 22)

Answers nuget.org gave on 2026-11-24, kept as they came, for the fixture of kit check 22
([catalogue_fixture_test.go](../../catalogue_fixture_test.go), remote-curation.md R-CUR-007, formats/nuget.md R-NUGET-035):

| File | Request |
|---|---|
| `service-index.json` | `GET https://api.nuget.org/v3/index.json` |
| `search-owner-jamesnk-1.json`, `search-owner-jamesnk-2.json` | the search resource with `q=owner:jamesnk&prerelease=true&semVerLevel=2.0.0&take=2` and `skip=0`, `skip=2` (totalHits 4) |
| `newtonsoft.json.bson-versions.json` | `GET https://api.nuget.org/v3-flatcontainer/newtonsoft.json.bson/index.json` |
| `newtonsoft.json.bson-registration.json` | `GET https://api.nuget.org/v3/registration5-gz-semver2/newtonsoft.json.bson/index.json`, decompressed |
