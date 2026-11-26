# The npm catalogue's recorded answers (kit check 22)

Answers the public registry and its replication host gave on 2026-11-24, kept as they came, for the fixture of kit check 22
([catalogue_fixture_test.go](../../catalogue_fixture_test.go), remote-curation.md R-CUR-007, formats/npm.md R-NPM-049):

| File | Request |
|---|---|
| `search-tufjs-1.json`, `search-tufjs-2.json` | `GET https://registry.npmjs.org/-/v1/search?text=tufjs&size=2&from=0` and `from=2` (total 4) |
| `scope-tufjs-1.json` | `GET https://replicate.npmjs.com/registry/_all_docs?startkey="@tufjs/"&endkey="@tufjs/\ufff0"&limit=3` |
| `scope-tufjs-2.json` | the same with `startkey="@tufjs/cli"&limit=4` |
| `left-pad.json` | the full document of `left-pad` as test/npmstub/testdata/recorded holds it (two versions) |

## A scope the registry does not have (Q-CUR-165)

Answers of 2026-11-24, 08:23 local time. A `HEAD` of a scope's account answered the status of its `GET`.

| File | Request | Answer |
|---|---|---|
| `scope-guidedtraffic.json` | `GET https://replicate.npmjs.com/registry/_all_docs?startkey="@guidedtraffic/"&endkey="@guidedtraffic/￰"&limit=3` | 200, no rows: the key range does not tell an unknown scope from an empty one |
| `org-guidedtraffic.json` | `GET https://registry.npmjs.org/-/org/guidedtraffic/package` | 404 `Scope not found` |
| (not kept) | `GET https://registry.npmjs.org/-/org/tufjs/package` | 200; a user's scope answers 200 as well (`/-/org/sindresorhus/package`) |

## The README of a package not stored (R-CUR-028)

`left-pad-readme.json` is `left-pad.json` with the members `readme` and `readmeFilename` of the full document the public registry
answered on 2026-11-26 (`GET https://registry.npmjs.org/left-pad`, 15 versions, its `readme` 871 bytes of Markdown). The registry writes
the README of the latest version only, at the top of the document; `left-pad.json`, which has neither member, is a package without one.
