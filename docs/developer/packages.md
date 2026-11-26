# The package screens' API: read model, README, client setup

R-API-029 (packages, versions, README) and R-API-030 (client setup snippets) are the API behind the package list, the package page, the install panel and
the client setup page of the admin UI ([web-ui.md](web-ui.md#package-screens-and-client-setup)). The contract is the block `M2 packages` of
[api/openapi.yaml](../../api/openapi.yaml); the handlers are [packages_api.go](../../internal/api/v1/packages_api.go) and
[setup_snippets.go](../../internal/api/v1/setup_snippets.go). The decisions are Q-API-164 to Q-API-168 and Q-UI-75 to Q-UI-77 in
[api-and-ui.md](../spec/api-and-ui.md).

## What has to be in your head first

- **A package of the API is a format and a normalized name, across repositories.** The generic rows are per repository (`packages (repo_id, name)`); the
  view aggregates them. Its id is derived, not stored (`view.ID`, Q-API-164).
- **Every aggregate counts only what the caller may read, and the filter is in the statement that counts** (Q-API-165). The decision point classifies each
  repository for the request; the statements take the result as arrays.
- **The core knows no format.** What the generic rows do not hold (description, keywords, licenses, metadata, README) and the order of versions come from the
  format's `ViewAdapter` over its data port (Q-API-166). The order is also stored: every version row carries the format's sort key
  (`ViewAdapter.SortKey`, bytewise the order of `Compare`) and its pre-release answer (`ViewAdapter.Prerelease`, Q-API-433), which the host's package writer writes, so that
  PostgreSQL orders the versions by an index ([Q-API-429](../spec/api-and-ui.md#q-api-429--the-order-of-a-formats-versions-is-a-sort-key-stored-with-every-version-row)).
  A row may lack the key (written before migration 00116, or by a replica of the release before it during an upgrade): the reads give it the
  key its format gives it ([Q-API-430](../spec/api-and-ui.md#q-api-430--a-version-row-without-a-stored-sort-key-is-ordered-by-the-key-its-format-gives-it)),
  and the background migration `version_sort_key` fills it ([below](#the-fill-of-the-sort-keys-q-ha-492)).
- **The server never builds an address from the `Host` of a request**, and never puts a token into a snippet (Q-API-168).
- **A hidden thing and a missing thing look the same**: `404`, the same problem body.

## Files

| Path | Responsibility |
|---|---|
| [internal/api/v1/packages_api.go](../../internal/api/v1/packages_api.go) | The seven handlers; `PackageServices`; the request's scope (`packageScope`); `writeTagged`; the download URL; `GetClientSetup` and `publishTarget` |
| [internal/api/v1/setup_snippets.go](../../internal/api/v1/setup_snippets.go) | Runs a format's `SetupTemplate`s: the credential modes, the package substitution, `shellQuote`, the snippet ids, the token-shape check |
| [internal/packages/view/](../../internal/packages/view) | The read model (domain layer): `Service` (`Scope`, `List`, `Package`, `Versions`, `Version`, `ReadmeOf`), `Topology` (the repositories by database ID, which virtual repository contains which, the name of an ID), `ID`/`ParseID`, the `Adapter` interface and the value types |
| [internal/packages/sortkey/](../../internal/packages/sortkey) | The background migration `version_sort_key` (Q-HA-492): `Fill` with `Enqueue` and `Run`, `Background`, `JobSpec` ([below](#the-fill-of-the-sort-keys-q-ha-492)) |
| [internal/packages/readme/](../../internal/packages/readme) | The README renderer: goldmark, bluemonday, the finishing pass, `HeadingSlug`, `Version`, `PolicyHash` |
| [internal/authz/visible.go](../../internal/authz/visible.go) | `ReadScoper`, `ReadScope`, `ReadShape` (`ReadAll`, `ReadNone`, `ReadRows`): the classification of a repository for a listing |
| [internal/formats/view.go](../../internal/formats/view.go) | The contract: `ViewAdapter`, `ViewVersion`, `ViewFields`, `ViewData`, `ViewFind`, `DataPorts`, `Base.View()`, and `StoredQuery`/`Stored` (the stored versions and tags of one package in one repository, for the exposure check; `ViewAdapter.Stored`) |
| [internal/formathost/view.go](../../internal/formathost/view.go) | `NewViewAdapters`: bridges the registered formats' adapters to `view.Adapters` (the domain package imports no format) |
| [internal/formats/npm/view.go](../../internal/formats/npm/view.go), [internal/formatdata/npm/view.go](../../internal/formatdata/npm/view.go) | npm's adapter and its reads ([npm.md](npm.md#the-package-view)) |
| [internal/meta/postgres/queries/package_view.sql](../../internal/meta/postgres/queries/package_view.sql), [npm_view.sql](../../internal/meta/postgres/queries/npm_view.sql) | The statements |
| [internal/config/ui.go](../../internal/config/ui.go) | `ui.readme.*` and `ui.client_setup.*` ([configuration.md](configuration.md)) |
| [internal/app/wire_packages.go](../../internal/app/wire_packages.go) | The step `packages-api` (`wirePackagesAPI`, a role with a listener): the view over the registered adapters, the engine as classifier, the renderer with the operator's origins. A part that cannot be built (the renderer under `ui.readme.*`) stops the start-up; the operations are never served as `503` by a replica that is ready (Q-ARCH-230). `requireServing` names the package services |

## The request path of a list

1. `ParseList` reads the filters, the sort and the cursor; an unknown parameter or sort is `400`, a cursor of other filters `400 invalid_cursor`.
2. `packageScope`: the anonymous gate (401 while no `public` level opens anything), the snapshot of the repositories (`Reader.List`) for the `Topology`, the request's
   index (`requestHolder`, so every question of the request is answered on one index), `ReadScopeOn` for the physical repositories (no addressed repository: a
   listing across repositories decides each on its own, Q-AUTHZ-168), `Readable` for the
   virtual ones, then `view.Service.Scope`.
3. `Scope` takes the repositories that are `ReadAll` and have no include or exclude patterns (`Repository.Restricts`, R-REPO-005) as a list of repository IDs (`Topology.Physical` is keyed by ID; the statements compare `packages.repo_id`, see [Repositories as IDs in the statements](vulnerabilities.md#repositories-as-ids-in-the-statements-r-vuln-028)). For every other repository that is not `ReadNone` it reads their version triples (`PackageViewCandidates`, which returns `repo_id`,
   at most 200 000, narrowed by format and name prefix), keeps the ones the repository's patterns admit (`Topology.Admits`) and the caller may read (`ReadScope.MayRead` with `Topology.Name(id)`, a map lookup; skipped for a `ReadAll` repository) as `repositoryId US name US version` strings (`rowKey`).
4. `Service.List` asks the adapters for the versions that match `q` (`FindRanked` of `view.Ranker`, which the host's bridge answers with the format's
   `formats.ViewRanker` where it has one and rank 0 otherwise, R-ANS-057) and `keyword` (`Find`) and for the withheld versions (`Withheld`), then runs
   `PackageViewList`: the CTE `vis` holds the versions that are live, not withheld, in the scope and inside the filters (the filter `namespace` is
   the name prefix the format's `catalog.NamespaceRule` makes, R-UI-220), one cheap row each; `qr` zips the matched ids with their ranks; `grouped`
   counts and dates them per format and name and computes `relevance` (3000, 2000 or 1000 by the name, plus the best rank) and, only while
   `signed` is asked, the latest version by the stored key; `page` keeps a package whose latest version carries a verified signature (the
   badge's rule of R-SIG-009 in SQL) when `signed` is asked, applies the keyset condition and the order of the sort (`relevance`: the
   highest first) and the limit; `dl` sums the downloads of the
   page's packages alone (the lead file or any file of a visible version by a join with `item_download_stats`; the document of an OCI version from the
   package's downloaded files, each counted once per visible version or tag that holds its bytes). Names are compared with collation `C`.
5. For the packages of the page it reads the candidates of their latest version (`PackageViewLatest`, Q-API-429): per package row its highest visible release
   by the stored sort key, its highest pre-release when it has no visible release, and a remote OCI repository's highest tag that is no pre-release and its
   highest pre-release tag (the generated column `tag_prerelease`, migration 00126, Q-API-435), each the first visible entry of an index
   (`package_versions_latest_release`, `package_versions_latest_prerelease`, `remote_cache_entries_tag_latest_release`,
   `remote_cache_entries_tag_latest_prerelease`), and every visible row without a stored
   key (Q-API-430; none once the fill has run), and the newest visible version the format does not rank, by its time, of the version rows and of a
   remote's tags (`package_versions_unranked`, `remote_cache_entries_tag_unranked`, Q-API-431). `latestOf` takes the latest among them: a release before a
   pre-release before an unranked version, the adapter's order within the first two, the time within the third; it names the
   repositories whose rows gave a candidate, and asks the adapter for the description and keywords of that version.
6. On an installation that verifies signatures the API reads the badge of the page's latest versions in one statement (`PackageViewVerified`,
   `addVerifiedBadges`): `signature` of an entry is `verified_internal` or `verified_public`, absent otherwise.

The page of a name (`GET /api/v1/packages/namespace`, [namespace_api.go](../../internal/api/v1/namespace_api.go), R-UI-220) reads the format's
`catalog.NamespaceRule`, and for a reader of a repository's configuration (`configScope`) the active local repositories of the format with
their package ownership, their automatic claims and the claims that cover the namespace's prefix (`repository.Service.ClaimsWithin`, one
statement, `ListClaimsOfRepositories`; `ClaimPattern.Within` says all or some). Its packages are the list with the filter `namespace`.

The documentation searched across collections (`GET /api/v1/search/documentation` and the suggestions' kind `documentation`,
[docs_search_api.go](../../internal/api/v1/docs_search_api.go), R-ANS-058) starts at the index of the entries: the versions with a matching
entry in the scope's repositories (`AnsibleDocsCandidates` through `ansible_doc_entries_search`, the data port's `DocCandidates`, at most
20,000), the newest version of each of their package rows (`view.Service.NewestOf`: `PackageViewLatestOfPackages`, each row by its key, and
`latestOf`, the version the package's page opens on), kept where it is a candidate and `Scope.MayReadWiki` allows it, then one statement over
their entries (`AnsibleDocsSearch` through `SearchDocs`). `querybudget`'s `TestR_ANS_058_TheDocumentationSearchCostsAFixedNumberOfStatements`
holds the count, `search`'s `TestR_ANS_057_R_ANS_058_TheSearchOfCollectionsAndOfTheirDocumentationUsesItsIndexes` the indexes.

The find of collections (`AnsibleViewFind`, R-ANS-057) reads the expression index `ansible_collection_versions_search` (migration 00138)
first, in a `MATERIALIZED` step, and joins the matches to the repositories asked; the statement repeats the index's expression word for
word, which the planner needs to use it. The roles a remote read (`AnsibleRoleViewFind`, R-ANS-056) follow with the rows the limit leaves,
by the same rule over owner and name, tags and description; they have no index and build each role's vector, since a remote holds only the
roles a client looked up. A role's display name `<owner>.<name>` is matched by the list's substring and counts for the relevance like the
name (`PackageViewList`).

**Measured once** (2026-11-26, `TestMeasureTheSearchOfCollections` in `querybudget`, run with `QUADER_TEST_MEASURE_FIND=1`; tiny rows, one
version per collection):

| Collections | Find, a word of 1 % | Find, a word of every row | List without a text | List, 1 % | List, every row | Package suggestions |
|---|---|---|---|---|---|---|
| 1,000 | 0.4 ms | 14 ms | 21 ms | 24 ms | 88 ms | 13 ms |
| 10,000 | 2.5 ms | 153 ms | 45 ms | 55 ms | 156 ms | 23 ms |
| 50,000 | 8.1 ms (447 ms with the vector built per row) | 391 ms | 132 ms | 150 ms | 717 ms | 75 ms |

The documentation with 200,000 entries of 2,000 collections among 50,000: the candidates 4.5 ms (a word of 200 entries, bitmap scan of
`ansible_doc_entries_search`) and 31 ms (a word of every entry); the list 22 ms; the suggestions 31 ms (`do`), 41 ms (`doc`), 44 ms (`docker`),
350 to 370 ms for a word of every entry (H-2157). The first build read the newest version of every collection and every entry of those with
documentation: 770 ms. The package suggestions' 75 ms with 50,000 collections are the statements of R-API-064 whatever the text and were not
changed here.

Every transaction of the service runs with the server's JIT compilation off (`Service.read`, Q-API-428).

The version list (`Service.Versions`, [Q-API-417](../spec/api-and-ui.md#q-api-417--the-version-list-of-a-package-is-read-a-page-at-a-time-by-a-keyset-query-in-the-order-of-publication))
first reads the packages of the name in the scope's repositories (`PackageViewHolders`). For a package that one repository holds, in the order of
publication, `keysetPage` reads a page by keyset: `PackageViewVersionPageDesc`/`Asc` over `package_versions` (index `package_versions_listing`) and, for a
remote repository, `PackageViewTagPageDesc`/`Asc` over its OCI tag entries (index `remote_cache_entries_tag_time`, Q-OCI-267); the two ordered lists of a
batch are merged, the pre-release filter (the format's rule) is applied in the service and the next batch starts after the last version read until the
page holds `limit + 1`. The cursor is `[publishedAt, version]`: the version's creation time and its name, compared bytewise, the statements' order. A
package several repositories hold, in the order of publication, goes through `sortedPage`: every visible version's name and creation time
(`PackageViewVersionNames`), grouped, filtered and sorted in the service (H-1683). The order of the format's versions (`sort=-version`, the default,
and `version`) goes through the `walk` of [order.go](../../internal/packages/view/order.go) (`orderPage`), in two parts (Q-API-431): the versions the
format ranks (a key of the class `versionkey.Version`) by their key, then those it does not rank (`NotVersion`, an OCI tag such as `latest`) newest
first by their time; oldest first the other way round. The first part, for one holder or several (Q-API-429): `PackageViewVersionKeyDesc`/`Asc` over `package_versions` (index `package_versions_by_key`) and, for
a remote repository, `PackageViewTagKeyDesc`/`Asc` over its tag entries (index `remote_cache_entries_tag_key`), at least 64 versions of each holder after the
cursor's key, merged by the key (`keyBatch`: a version two holders hold has one key, so its rows meet and it is taken once; the first `n` distinct versions
of the merge are complete); the statements read keys of the class `Version` alone. The second part: for one holder a keyset on the time and the
version, `PackageViewVersionUnrankedDesc`/`Asc` (index `package_versions_unranked`) and `PackageViewTagUnrankedDesc`/`Asc` (index
`remote_cache_entries_tag_unranked`), merged as `keysetPage` merges (`unrankedRows`); for several holders every unranked visible version of the package
(`PackageViewUnrankedNames`), each by its oldest row, sorted in the service (`loadUnranked`, H-1901). The cursor of the first part is `[version]`, its key
`Adapter.SortKey(version)`, the bytes the rows store; that of the second `[publishedAt, version]` (`orderKey`, from `VersionSummary.Unranked`). The
statements of both parts return
after their batch every visible row of the package without a stored key; `keyRows` gives each the key `Adapter.SortKey` gives it, keeps it when it lies
after the cursor's key and in its part, and merges it with the rest, and only the keyed rows count against the batch (Q-API-430). A version's `publishedAt` is its
oldest visible row's in every order. Either way `PackageViewVersions` then reads the rows of the
page's versions alone, by the holders' package ids and the versions, with sizes and downloads. Each row carries the downloads and the `last_download_at` of the items that are a fetch of the version by its format's rule
(`Descriptor.VersionFetch`: the lead file, any file of the version, or an image's manifest by tag and by digest), read from `item_download_stats` in a
lateral subquery of `PackageViewVersions`; a version's `DownloadCount` sums its rows and its `LastDownloadedAt` is the latest of them (`versionGroup.lastDownload`,
R-API-065, [Q-API-295](../spec/api-and-ui.md#q-api-295--a-versions-last-download-and-its-downloads-are-the-fetches-of-it-by-the-formats-rule-read-with-the-version-rows)).
The details (`Service.Version`) read the visible rows of the one version (`oneVersion`: the holders, then `PackageViewVersions` for that version), take the
oldest copy as the primary one, ask the adapter for summary and details, and read the files of the primary version; the visible set of versions the adapter
may ask about (npm's dist-tags) is read only when it asks. `ReadmeOf` asks for details and README. The repositories of a version are the physical
repositories that hold it plus the virtual repositories that contain one of them and that the caller may read, local first.

## The list by content (Q-API-432)

`GET …/versions?group=content` (`Service.Images` in [images.go](../../internal/packages/view/images.go), the handler's `versionListQuery` in
[packages_api.go](../../internal/api/v1/packages_api.go)) lists one entry per content for a format whose fetch rule is the document
(`Adapter.Fetch() == FetchDocument`: OCI); every other format answers its plain list in the order of the versions with empty `aliases`.

1. The content of a version is `package_versions.document_sha256` (written by the host's package writer with `packages.WithDocuments`, kept by
   the trigger `items_document_follow` when a tag's item is overwritten in place, migration 00124, ADR-280) and a remote tag's `blob_sha256`.
   The walk's statements return it (`digest`); `pageKey.digests` holds a version's contents over its holders, each once (`keyBatch` merges
   them when two holders hold one version).
2. `imageWalk` reads the walk of the order of the versions (`order.go`) in batches of 64 and turns each into `(version, content)` pairs
   (`candidatesOf`). `imagesSeen` asks of every admitted holder which pairs an earlier visible version names (`PackageViewImageSeen`, one
   statement per holder and batch; a ranked candidate is seen by a sibling of a higher key, an unranked one by a ranked sibling or, for one
   holder, by an unranked sibling of a later time; each a probe of `package_versions_by_document`, `package_versions_document_unranked`,
   `remote_cache_entries_tag_digest` or `…_unranked`, kept a check per candidate by `OFFSET 0`; the candidates come as rows of the joined
   ordinalities of four arrays, a `MATERIALIZED` CTE). The measurement of four shapes of 100,000 tags is in
   [oci.md](oci.md#the-list-by-content-of-an-image-with-100000-tags-q-api-432-h-1900-measured). Siblings without a stored key
   (`PackageViewImageUnkeyed`) are placed in Go by the key their format gives them (Q-API-430). For several holders the unranked part is read
   whole (H-1901), and its candidates are ordered among themselves by their place in it (`walk.firstOf`).
3. A pair nobody earlier names leads an entry. The walk stops when the page holds `limit + 1` entries (`cutAtVersion` then keeps whole versions
   up to `limit`, at least the first), when the walk ends, or when it read `imageBudget` (1,024) versions: then the page is what it found and
   the cursor the walk's position (`positionOf`), a short or empty page with a cursor (R-API-010, H-1900).
4. `imageSummaries` builds the page: the leading versions' rows (`PackageViewVersions`) and sizes (`adapterSizes`), and per holder
   `PackageViewImageTags` (each content's visible versions, at most 101 ranked and 101 unranked per holder, ordered by `earlier` into
   `aliases`, at most 100, `aliasesMore` past them) and `PackageViewImageDownloads` (the package's files holding the content's bytes, a tag
   item only while its version is visible, H-1902). The handler adds the leading version's `signatures`.

Tests: `TestR_API_029_TheListByContentGroupsAndCutsAtAVersion` (unit, microseconds), `TestR_API_029_TheListByContentIsTheGroupingOfTheListByTag`
(area `ociremote`, about 7 s, 2,206 tiny tag entries and 1,103 manifests by SQL: the grouped pages equal the grouping of the per-tag list, the
budget cuts the trailing commit tags into a short page, and the lead check of 64 candidates reads at most three entries each),
`TestR_API_029_AMovedTagMovesBetweenImagesOfTheListByContent` (area `oci`, about 3 s, three pushes), the `group=content` subtest of
`TestR_API_029_VersionList` (area `packages`).

## The fill of the sort keys (Q-HA-492)

Migration 00116 adds `package_versions.sort_key` and `prerelease` as nullable columns (a row has both or neither, the check
`package_versions_sort_key_whole`): the key is the format's code's, and the release before it keeps writing rows without them while a
rolling upgrade runs (R-HA-020). The background migration `version_sort_key` ([sortkey.go](../../internal/packages/sortkey/sortkey.go),
R-HA-022) fills them:

- A worker that starts calls `Fill.Enqueue` (`startSortKeyFill` in [wire_formats.go](../../internal/app/wire_formats.go)): it queues the job
  `data_migration_version_sort_key` (queue `maintenance`, `coalesce`) unless the `system_state` key `version_sort_key_filled` exists and no row
  lacks a key; a completion that rows without a key followed (a rollback and an upgrade again, a restore of an old backup) is deleted first, so
  the report shows the fill pending again.
- `Fill.Run` reads the rows without a key by id after its cursor (`VersionSortKeyBatch` in
  [version_sort_key.sql](../../internal/meta/postgres/queries/version_sort_key.sql), index `package_versions_unkeyed`, locked with
  `SKIP LOCKED` so it never waits for a writer), computes each key with `formathost.VersionOrder` (the function the package writer stores
  with), writes them (`VersionSortKeyFill`, only where the key is still NULL) and saves its cursor after every batch of
  `jobs.background_migrations.batch_size`, with `pause_between_batches` between them.
- At the end of a pass it asks whether a row without a key is left (`VersionSortKeyUnkeyed`): a replica of the release before can still be
  writing, or a writer held a row. Then it queues the next run a minute later and records no completion (result `waiting`); otherwise it
  records `{"state":"filled","filled":<rows>}` under `version_sort_key_filled`. It names no release it waits for.

`GET /api/v1/system/migrations` lists it as `version_sort_key` (`sortkey.Background`, `app.BackgroundMigrations`). Tests:
`TestR_API_029_VersionsWithoutAStoredKeyKeepTheirPlaceBeforeAndAfterTheFill`
([versions_unkeyed_test.go](../../test/integration/packages/versions_unkeyed_test.go)) and
`TestR_HA_020_TheSortKeyStepPassesOnADatabaseOfThePreviousRelease`
([version_sort_key_test.go](../../test/integration/postgres/version_sort_key_test.go)).

## The suggestion read (R-API-061, R-API-064)

`Service.Suggest` in [suggest.go](../../internal/packages/view/suggest.go) answers the package suggestions of the search field
([http-api.md](http-api.md#the-suggestions-of-the-search-field-r-api-060-to-r-api-064)) without the list's aggregation and without
`PackageViewCandidates`: it classifies the physical repositories of formats with an adapter as the list's `Scope` does (`ReadAll` without
patterns is read whole, every other shape but `ReadNone` in part), asks the adapters for their withheld versions, and in one transaction
runs `SearchCustomPlans` (each statement planned for its text), statement A `SearchPackagesByPrefix` (the names in `[low, high)` of
`packages_name_lower`) and, while A found no more than `limit` readable packages, statement B `SearchPackagesByWords` (the word prefixes
on `packages_name_words`, without A's rows), each at most `SuggestCandidates` (200) rows with a live, served version
([search.sql](../../internal/meta/postgres/queries/search.sql)). A row of a repository read in part is decided on the package's
document, `ReadScope.MayRead(repo, name, "")`, and the repository's patterns, `Topology.Admits` (Q-API-275). A package held by several
repositories is one `Suggestion`, in the first readable repository by qualified name; its rank is computed on the name
(`suggestionRank`), and the API builds the query (`SuggestQuery`: the text, the bounds, the tsquery) from
[search_match.go](../../internal/api/v1/search_match.go). `markDeprecated` then marks the shown suggestions placed in a repository read whole
whose latest version is deprecated: one `PackageViewLatest` for them and one summary read per format (R-API-085, Q-API-443).

**Deprecation** (R-API-085). The summary of a version (`Data.Deprecation`, `Data.Unlisted`) carries it, so the list's rows (their latest
version's), every page of the version list (`fillFromMetadata` reads the summary always and the details only for `include=digest|state`)
and a version's details carry it at no read of their own. The filter `deprecated=true` asks the formats for their deprecated versions
(`Adapter.Deprecated`, the bridge of `formats.DeprecationFinder`, bounded as a search is) and `PackageViewList` keeps a package whose latest
visible version is among them, picked by an aggregate that runs only with the filter. The write is not the view's: the management operation
([deprecation_api.go](../../internal/api/v1/deprecation_api.go)) builds a request of the format and hands it to `formats.Deprecator`.

Two facts that are easy to get wrong: a Go `nil` slice reaches the driver as `NULL`, and `x = ANY(NULL)` under a `NOT` drops every row, so `keys` and `ids` in
[service.go](../../internal/packages/view/service.go) turn them into empty arrays; and `ReadAll` is only returned when nothing narrows (no pattern in the
grant, no scope element, an endpoint in mode `all`), because a repository wrongly classified `ReadAll` is a leak, while one classified `ReadRows` costs time.

## The README

`readme.Renderer.Render` runs goldmark (CommonMark, tables, strikethrough, autolinks, task lists, raw HTML), bluemonday with the allowlist of R-UI-008, and
`finish`, a pass over the parsed fragment that applies what an allowlist cannot say: link targets and `rel`, the image policy, disabled checkboxes and
the `user-content-<slug>` heading anchors. The limits are the source (cut), the HTML (re-rendered from a proportionally shorter source) and two seconds
(abandoned for the escaped text); each sets `truncated`. The ETag is computed from the source, `readme.Version` and `PolicyHash()` before rendering. Raise
`Version` whenever a change makes a source render differently. `HeadingSlug` implements JavaScript's `trim`, `\s` and `toLowerCase` on purpose (Q-UI-76).

## Client setup

`renderSnippets` takes the format's templates of one client and runs each with `text/template` over `formats.SetupData` and the functions `shell`, `powershell`, `cmd` and
`xml`; then `applyCredentials` rewrites the three spellings of the token reference (`formats.SetupTokenRefs()`: POSIX, Windows, PowerShell) by the mode and `substitutePackage`
fills `formats.SetupPackageRef` quoted for the language of the snippet (`quoterFor`), attaching the version as the template's `VersionSpec` says (default `@<version>`). Publish templates are rendered only for a caller who may deploy to the target. `GetClientSetup` refuses an unknown client (the error lists the clients of
the format), a value above 256 bytes, with a control character or in the shape of a token, a `package` or `version` that starts with `-`
(a client reads it as an option, Q-NPM-50) and a `package` the format's name rule (`Descriptor().Names`) does not take; the snippet holds the
normalized name, and `shellQuote` quotes any value that starts with `-`. It answers `503` without a configured base URL. The registry URL is
the base URL plus the format's `protocolBasePath`; `endpointPath` is the same without its scheme.

`scope` is accepted only for a client that has a scoped variant (Q-API-235). A configure template has one exactly when it reads `.Variant`: `hasScopedVariant`
walks the template's parse tree for the reference, so a format declares a scoped variant by using it and has no second field to keep in step. `renderSnippets`
adds the `-scoped` copy only for such a template; `GetClientSetup` answers `400 malformed_request` (the detail names `scope`) when no configure template of the client has one
(`clientHasScopedVariant`). Today that is the four npm clients. A new format whose client has a scope-like variant reads `.Variant` in its configure template and
the ids, the refusal and the UI fixture follow; `TestR_API_030_RenderedSnippetIdsMatchTheUIFixture` renders every format with a scope.

With `bastion=<name>` (R-BAS-007, Q-BAS-97) the base URL is the one the registration reported at its last handshake, read through `PackageServices.Bastions`
(`bastionBaseURLs` in [bastion_baseurl.go](../../internal/app/bastion_baseurl.go): `BastionGet`, then `BastionLastBaseURL`), provided it is an entry of
`PackageServices.AdditionalBaseURLs` (`server.additional_base_urls`; a trailing slash and the case of the host are ignored, and the listed entry is what the
snippets carry); `bastionBase` answers `404` for a name that is no registration, `409 bastion_base_url_unknown` for no reported address or an address that is not listed, and `503`
without the reader. The operation then needs no base URL of the endpoint. The publish templates are not rendered and `publishTarget` is `null`; `SetupData.Bastion` is true,
which npm's `npm.npmrc.configure` template turns into `replace-registry-host=always`.

## Tests

| Tier | Where | What |
|---|---|---|
| Unit | [render_test.go](../../internal/packages/readme/render_test.go) | The XSS corpus (26 vectors), raw HTML, GitHub-style Markdown, links, images by policy, origin validation, limits, the fallback, the heading corpus |
| Unit | [view_test.go](../../internal/packages/view/view_test.go), [visible_test.go](../../internal/authz/visible_test.go) | Ids, topology, latest version, cursors; the classification and its agreement with `DecideOn` over a matrix |
| Unit | [setup_scope_test.go](../../internal/api/v1/setup_scope_test.go) | `TestR_API_030_OnlyAConfigureTemplate…`, `…TheScopedVariantIsDetected…`: the scoped copy only where `.Variant` is read |
| Unit | [setup_snippets_test.go](../../internal/api/v1/setup_snippets_test.go), [packages_api_test.go](../../internal/api/v1/packages_api_test.go) | Modes, quoting, ids, publish gate, token refusal, `503` without the read model or a base URL, tags |
| Unit | [view_test.go](../../internal/formats/npm/view_test.go), [view_test.go](../../internal/formathost/view_test.go), [ui_test.go](../../internal/config/ui_test.go) | npm's metadata members, the bridge, the configuration |
| Unit | [bastion_setup_test.go](../../internal/api/v1/bastion_setup_test.go), [setup_test.go](../../internal/formats/npm/setup_test.go) | `TestR_BAS_007_…`: the Bastion's address and no publish snippet, the switch for npm only, the `404`, `409` and `503` answers, a lookup that fails |
| Integration | [test/integration/bastionsetup](../../test/integration/bastionsetup) | `TestR_BAS_007_ClientSetupForABastionThroughTheStack`: registrations and contacts in PostgreSQL, a replica with `server.additional_base_urls`, the four answers |
| Integration | [test/integration/packages](../../test/integration/packages) | The in-process harness with npm publishes as data: list, filters, every sort walked by cursor, visibility for six callers, a repository with exclude patterns, versions, details, tags, README, setup, an endpoint with rules; `TestR_API_010_APageOfVersionsReadsNoVersionPastItsLimit` bounds the rows a page reads in both orders and the latest version's rows by the plan, and the list's subplans, over 2,000 version rows written by SQL (`env.versions` writes them with the format's key, `testenv.VersionKey`) |
| Web | `server-address.spec.ts`, `package-readme.spec.ts` | The base URL from the formats answer; the heading corpus |

Run the area with `make test-integration` or, for this one area, the invocation of [build-and-test.md](build-and-test.md) with `./test/integration/packages/...`.

## Extension checklists

**A format gets package screens.** (1) `View()` returning an adapter, in `internal/formats/<id>/view.go` ([formats.md](formats.md#the-package-view)); (2) the reads in
`internal/formatdata/<id>/view.go` and the statements in `queries/<id>_view.sql`, behind the format's port; (3) `Descriptor.Names` (the view refuses a format without
package names) and, for the lookup, the normalization the format's name rule gives; (4) setup templates (`SetupTemplates`) with `SetupTokenRef` and `SetupPackageRef`, and
the clients in the UI table (`web/src/app/features/setup/clients.ts`) under the format's own ids; (5) tests in the format's package and a publish-based case in
`test/integration/packages`.

**A field is added to a package or version answer.** Edit the schema in `api/openapi.yaml` (both copies: `make generate` refreshes `internal/api/v1/openapi.yaml`
and the generated server), then `view.Package`/`VersionDetail`, the statement, the handler mapping, `npm run generate:api`, the UI fake and the specs.

## A package with 100,000 versions (H-1683), measured

Measured on 2026-11-24, a measurement and not a test (it is not in the repository): the integration stack (instance 1), one replica, the
installation of [test/integration/packages](../../test/integration/packages) after `seed`, and 100,000 more versions of `left-pad` in `npm-local`
(`0.<g/1000>.<g%1000>`, every tenth `-rc.1`, one second apart) written by SQL with the format's key, then `ANALYZE`; "two holders" is the same
after a publish of `left-pad@1.0.0` into `npm-team`. As `erin`, who reads both repositories; the median of three requests. The machine has 20 cores
and runs other builders beside it; the load average stands beside each set. The numbers of a remote image with 100,000 tags are in
[oci.md](oci.md#the-listing-of-a-remote-with-very-many-tags-h-829-measured).

| Request (`GET`) | Before (03:05, load 19 to 22) | After Q-API-429 (03:52, load 11 to 16) |
|---|---|---|
| `/api/v1/packages?limit=50&repository=it/npm-local` (the package list) | 663 ms | 190 ms |
| `/api/v1/packages?limit=50&sort=-updatedAt` | 630 ms | 189 ms |
| `/api/v1/packages/{id}` (the package) | 594 ms | 152 ms |
| `/api/v1/packages/{id}/versions?limit=50` (first page, one holder) | 27 ms | 23 ms |
| the same with the cursor of the 50,000th version | 24 ms | 24 ms |
| `…&sort=version`, `…&sort=-version` | 404 ms, 387 ms | 29 ms, 27 ms |
| `…&sort=version`, two holders | 376 ms | 27 ms |
| `…&limit=50`, two holders (order of publication, `sortedPage`) | 157 ms | 211 ms (unchanged code; load) |
| `/api/v1/packages/{id}/versions/0.50.1` (one version) | 24 ms | 23 ms |

The plans of `PackageViewList` (`EXPLAIN (ANALYZE, BUFFERS)` with JIT off, as the service runs it): before, 441 ms, of which about 200 ms were the
downloads' subplan once per version of the scope (100,004 loops) and 146 ms the sort for `count(DISTINCT version)` (an external merge of 8.8 MB);
after, 236 ms, the sort for the count still the most of it, and the downloads a join that reads the statistics of the page's packages. The
package list's `PackageViewVersionNames` (61 ms and every version to the service) is replaced by `PackageViewLatest`, which reads at most two
version rows of a package (the bound `TestR_API_010_APageOfVersionsReadsNoVersionPastItsLimit` holds; 0.1 ms for a remote image's latest tag).
The version list in the order of the versions reads 51 rows a page (`PackageViewVersionKeyDesc`) instead of every name and a sort in the service.
What is left is H-1683: the count over every visible version of the scope, and a package several repositories hold in the order of publication.

## What is not built

- The README cache table of R-API-029 (Q-API-167, [H-298](../security/api-edge.md#h-298--a-readme-is-rendered-on-every-request-without-a-tag-and-a-rendering-that-runs-over-its-time-limit-is-not-stopped)).
- Images of files embedded in a package (P1, R-ARCH-029).
- CI snippets: no format supplies one; the npm templates are configure, install and publish, and the API renders the publish template only
  for a caller who may publish.
- A count of a package's versions kept by the commit and the fill: the list counts every visible version of the scope, and a package several repositories
  hold is listed in the order of publication by reading every visible version's name ([H-1683](../spec/api-and-ui.md#h-1683--the-version-list-of-a-package-several-repositories-hold-in-the-order-of-publication-reads-every-version-and-the-package-list-counts-every-version)).
- A decision per row inside the statement: the rows of a narrowed repository are decided in Go ([H-299](../security/permissions.md#h-299--the-cost-of-a-package-list-for-a-caller-with-row-level-grants-depends-on-versions-the-caller-cannot-read)).
- Verified in the integration area: the visibility of six callers, the sorts, filters, cursors, tags and setup answers of npm. Not verified: another format, a virtual
  repository with remote members holding cached versions, and a list above the bound of 200 000 rows decided in Go (a unit test checks the refusal's building blocks only).
