# The npm catalogue's recorded answers (kit check 22)

Answers the public registry and its replication host gave on 2026-11-24, kept as they came, for the fixture of kit check 22
([catalogue_fixture_test.go](../../catalogue_fixture_test.go), remote-curation.md R-CUR-007, formats/npm.md R-NPM-049):

| File | Request |
|---|---|
| `search-tufjs-1.json`, `search-tufjs-2.json` | `GET https://registry.npmjs.org/-/v1/search?text=tufjs&size=2&from=0` and `from=2` (total 4) |
| `scope-tufjs-1.json` | `GET https://replicate.npmjs.com/registry/_all_docs?startkey="@tufjs/"&endkey="@tufjs/\ufff0"&limit=3` |
| `scope-tufjs-2.json` | the same with `startkey="@tufjs/cli"&limit=4` |
| `left-pad.json` | the full document of `left-pad` as test/npmstub/testdata/recorded holds it (two versions) |
