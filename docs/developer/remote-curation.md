# Curating a remote repository: the work packages

Status: specified on 2026-11-23; the contracts package (C), the delivery rules (R2) and the downloads ahead with their queue (R4) built on 2026-11-23 and 2026-11-24, the tab strip and the tab Downloads (R5a), the requests, examination and retention (R2b), the upstream catalogue (R3) and the tab Upstream with the settings' section Delivery (R5b) on 2026-11-24, and the clients and documents (R6) the same day. The specification is [spec/remote-curation.md](../spec/remote-curation.md) with the format
pages' requirements (R-NPM-049, R-NUGET-035, R-OCI-038, R-DEB-036, R-ANS-047) and the changed sentences its section "Changes to other
pages" lists; the security view is [security/remote-curation.md](../security/remote-curation.md). This page cuts the work into packages
for builders, in the order they can land, each a whole feature that leaves `main` working, with the files and directories each owns so
that several builders work at once, the contracts between them, and the tests each writes with what they cost.

## What to have in your head first

- **Three questions, three mechanisms.** What a remote *delivers* is its delivery mode and rules (R-CUR-001); what a person *sees* of the
  upstream is the catalogue, a declared port of the format contract (R-CUR-007); what a remote *fetches ahead* of a client is a row of its
  download queue (R-CUR-011). None of them answers another's question: a download ahead ignores the rules (R-CUR-014), the catalogue
  stores nothing (R-CUR-008), a rule fetches nothing.
- **The rule is a delivery gate, not a pattern** (Q-CUR-6). It is asked at three places: before a fill (`internal/proxy`, step 5a of
  R-REPO-025), at the serve point as the fourth check of `gate.Serve` ([serve.go](../../internal/signatures/gate/serve.go)), and in the
  format's document filters, which already filter for an endpoint's exposure and a caller's version view
  ([versions.go](../../internal/formats/versions.go), R-EP-018). A remote has no patterns any more (R-CUR-002).
- **The probe's background download becomes a row** (R-CUR-013): the decision of [probe.go](../../internal/formathost/probe.go) inserts a
  row instead of a job, and [background_download.go](../../internal/formathost/background_download.go)'s run is the run of every row.
- **Every scan starts at the item store's commit** ([wire_formats.go](../../internal/app/wire_formats.go)), so an examination needs no new
  trigger; it only stores.
- **The contract version is 11** with the catalogue's declaration and the version canon (R-CUR-007, R-ARCH-010; built by C); the
  written contract of R-ARCH-008 is held equal to the code by `TestR_ARCH_008_TheWrittenContractEqualsTheCode`, and its surface by
  `testdata/contract.txt` (`go test ./internal/formats -run TestR_ARCH_025 -update-contract`), so a package that changes the contract
  changes both in the same change.
- **The catalogue's browsing calls are the leaf's** (`discover.Catalogue` in
  [catalogue.go](../../internal/mirror/discover/catalogue.go)): the engine and a bulk's expansion are domain packages and call them without
  importing the contract; `formats.Catalogue` embeds them and adds `VersionProbe`, which only the queue's run in `internal/formathost`
  calls (Q-CUR-109).

## The packages

| Package | Delivers | Depends on | Builder-days (estimate) |
|---|---|---|---|
| **C · Contracts** (built) | the migration of every table, the domain package's skeleton and its layer row, the catalogue's interface and types, the version canon, the descriptor's declaration and contract version 11 with every format's declaration (the floor: lookup), the error codes, the audit events' registrations, the configuration keys, the job queue `downloads` | — | 0.5 |
| **R2 · Delivery rules** (built) | R-CUR-001 to R-CUR-006, R-CUR-017 (the mirror's scope), the rules' API; the refusal before a fill, at the serve point and in the five formats' documents (kit check 23); the virtual walk; the journal and the Bastion's feed; the listing marks; a remote without patterns | C | 2.5 |
| **R3 · Upstream catalogue** (built) | R-CUR-007 to R-CUR-009: the catalogue engine (lent upstream that stores nothing, budget, cache, bounds, second hosts), the five formats' catalogues (kit check 22) and the check of `Register` that a remote adapter implements `formats.Catalogue` (Q-CUR-109), the Debian parse's descriptions, the API with the vulnerability verdicts of unstored versions | C | 2 |
| **R4 · Downloads ahead and the queue** | R-CUR-010 to R-CUR-013: the queue's store, dispatch, run, retries and failures in words; the probe's row; bulks and their expansion; the API; the client failure rows; the charge; a fill without a client is no refused download (Q-CUR-22) | C (the expansion calls the catalogue's interface; its integration case of a bulk lands with whichever of R3 and R4 lands second) | 2 |
| **R2b · Requests, examination, retention** (built) | R-CUR-014 to R-CUR-016: requests and askers, the examination a request starts, allow and decline, the list with client refusals, discard and the retention task, their events, journal and replay | R2, R4 | 1.5 |
| **R5a · The tab strip and the tab Downloads** (built) | R-CUR-018: the strip on a remote's page with the three addresses, the tab Downloads with its states, filters, failure detail, retry and cancel | R4 | 1 |
| **R5b · The tab Upstream and the settings** (built) | R-CUR-018: search, levels, lookup, a package's versions with states and verdicts, the actions, the preview of a bulk, the rule list, the requests panel, the settings section Delivery, the glossary rows | R2, R3, R2b, R5a | 2 |
| **R6 · Clients and documents** (built) | the end-to-end case per format in its own leg; the manual's pages (the tabs, curation, the queue); the developer pages this change touches (formats, proxy, mirror) | R5b | 1 |

**Who works at once** (at most four builders): C alone first (half a day); then R2, R3 and R4 side by side; then R2b and R5a side by side
(and whichever of the three is still open); then R5b; then R6. A research or a planner does not take a builder's place.

## C · Contracts

Built on 2026-11-23. What it holds, and what the next packages build on:

| File or directory | What |
|---|---|
| [00114_remote_curation.sql](../../internal/meta/postgres/migrations/00114_remote_curation.sql) | `remote_delivery_requests`, `remote_delivery_request_askers`, `remote_delivery_rules`, `repositories.delivery_rules_revision`, `remote_download_bulks`, `remote_download`, `remote_examinations`, the columns `provides`, `description` and `description_md5` of `deb_remote_entries`, and `deb_remote_descriptions`, with the indexes of the spec (and the two it gained, Q-CUR-16 and the SQL blocks of R-CUR-011 and R-CUR-015); export class `data` in [tables_remote_curation.go](../../internal/meta/postgres/tables_remote_curation.go) and, for the Debian table, [tables_deb.go](../../internal/formatdata/deb/tables_deb.go); `delivery_rules_revision` a registered revision column (R-BAK-022); `upstream_repository_purge` replaced with three more deletes (`remote_download`, `remote_download_bulks`, `remote_examinations`), the rules and requests kept (Q-CUR-111, held by `TestR_REPO_067_ThePurgeOfARetiredRepository` in `test/integration/upstream`) |
| [internal/remotecuration](../../internal/remotecuration/contracts.go) (`doc.go`, `contracts.go`) | the vocabulary (`Mode`, `RuleKind`, `Effect`, `Reason`, `DecidedBy`, `Decision`; the queue's `State`, `Origin`, `Outcome`, `FailureClass` with `Retryable`, `FailureStep`, `Failure`; `RequestState`), the names `KindDownload` (`remote-download`), `QueueDownloads` (`downloads`), `TaskDispatch` and `TaskExaminationCleanup`, and the ports in plain values (Q-CUR-110): `Decider` (`Decide(ctx, repoID, name, version) Decision`, `Package(ctx, repoID, name) Decision`), `Queue` (`Active(ctx, repoID, name, versions)`), `Requests` (`Open(ctx, repoID, name, versions)`), with the stubs `DeliverAll`, `NoQueue` and `NoRequests` |
| [layers.go](../../internal/archtest/layers.go) | `internal/remotecuration/...` as a domain package beside `internal/mirror` |
| [discover/catalogue.go](../../internal/mirror/discover/catalogue.go) | the browsing calls `discover.Catalogue` (`CatalogueCaps`, `Search`, `Children`, `Package`), their types and `ErrNotOffered` (Q-CUR-109) |
| [formats/catalogue.go](../../internal/formats/catalogue.go) | `formats.Catalogue` (the leaf's calls and `VersionProbe`), `CatalogueDeclaration` and its check in `Register`; `RemoteCaps.Catalogue` found by `CapabilitiesOf` ([remote.go](../../internal/formats/remote.go)) |
| [catalog.go](../../internal/formats/catalog/catalog.go) | `VersionCanon` beside `Names`, `Entry.VersionCanon`, `Registry.CanonicalVersion(format, version)`; `UpstreamPreset.CatalogueHosts` (checked like `ExtraHosts`, declared by no preset yet) |
| [formats.go](../../internal/formats/formats.go) | `Descriptor.Catalogue`, `Descriptor.VersionCanon`, `ContractVersion = 11`; [testdata/contract.txt](../../internal/formats/testdata/contract.txt) |
| every format's `format.go` with the class remote (npm, NuGet, OCI, Debian, Ansible Galaxy) | its declaration (the lookup, and for search and levels a reason until R3; NuGet's "no levels" is final) and its version canon; pinned by `TestR_CUR_007_EveryRemoteFormatDeclaresItsCatalogue` and `TestR_CUR_006_EveryRemoteFormatHasItsVersionCanon` in [internal/formats/all](../../internal/formats/all/catalogue_test.go) |
| `docs/spec/architecture.md` R-ARCH-008 and R-ARCH-010 | the interface, the declaration and the descriptor members as `TestR_ARCH_008_TheWrittenContractEqualsTheCode` holds them; the version and the checks of `Register` |
| [codes.go](../../internal/errs/codes.go) (`>>> remote curation`) | `not_allowed_in_repository` (403), `too_many_rules` (422), `invalid_version` (422), `catalogue_not_offered` (400), `catalogue_not_found` (404), `catalogue_budget_exhausted` (429), `catalogue_answer_too_large` (502), `download_queue_full` (429), `already_queued` (409), `not_retryable` (409), `already_delivered` (409), `too_many_requests` (429), `delivered` (409) |
| [events_remote_curation.go](../../internal/audit/events_remote_curation.go) | the twelve events of R-CUR-019 with their schemas, modes, webhook and journal flags; `remote.examination.discarded` classified as yielding no entry in [feed.go](../../internal/bastionmain/feed.go), its row in the table of R-HA-054, its replay handler pending in `cmd/quader/journal_handlers_test.go` until R2b; the audit log's names of the twelve in `web/src/app/features/audit/audit-labels.ts` with their German texts |
| [remote_curation.go](../../internal/config/remote_curation.go), [jobs.go](../../internal/config/jobs.go), [maintenance.go](../../internal/config/maintenance.go) | `proxy.delivery.*`, `proxy.catalogue.*` (with `cache_ttl` and `timeout` as durations), `proxy.downloads.*`, `jobs.workers.downloads` and the queue `downloads`, the tasks' schedules |

Not in C, and so the next package's: the registration of the kind `remote-download` and of the two tasks with the job framework (R4
registers the kind and the dispatch task, R2b the cleanup task), the check of `Register` that a remote
adapter implements `formats.Catalogue` (R3, with the five catalogues, Q-CUR-109), the repository members `remote.delivery.*` (R2).

Tests (unit, about 0.1 s together): `internal/formats` (the declaration's checks), `internal/formats/all` (the pins and the canons),
`internal/audit` (the events), `internal/config` (the defaults and bounds), `internal/archtest` (the layer row), `internal/remotecuration`
(the vocabulary held to the migration's CHECK lists, the retry classes, the stubs). The migration is applied by every integration area's
database, so the shards gain no test and about one second of migration.

## R2 · Delivery rules

| Area | Files |
|---|---|
| The rules: decision, compile, store, API | `internal/remotecuration/rules/`, `internal/api/v1/delivery_rules_api.go`, `api/openapi.yaml` (marked section `# >>> T-24 rules`) |
| The repository model | `internal/repository/` (`remote.delivery`, `remote.mirror.include`, the refusal of patterns on a remote, the rule set in the snapshot, the narrowing comparator's parts `rules` and `deliveryMode`) |
| Before a fill | `internal/proxy/artifact.go`, `metadata.go` (step 5a) |
| At the serve point | `internal/signatures/gate/serve.go` (the fourth check), `internal/formathost/` serve wiring |
| Documents | the filters of `internal/formats/npm`, `nuget`, `oci` (`PathsAllowed` joins the rules in `internal/formathost/host.go`), `ansible` (version list pages, `highest_version`); `internal/formats/deb` and `internal/formatdata/deb` (the virtual merge drops refused rows; a rules change enqueues `deb-index` of the virtual repositories) |
| The refusal's shape | each format's `errors.go` (`WriteError`), kit check 23 in `internal/formats/formattest` |
| The virtual walk | `internal/repository/routing.go`, `internal/formathost/virtual_remotes.go`, each format's virtual handlers where they end the walk |
| The listing | `internal/packages/view/` (no pattern hiding for a remote; `available` and `unavailableReason`), `internal/vuln/report/report.go` |
| The mirror | `internal/mirror/` (scope and rules, the fingerprint), `internal/formathost/mirror_host.go` (admission) |
| Journal and feed | `internal/journal/` (replay of the parts), `internal/bastionmain/feed.go` |
| The record of a refusal | the upsert into `remote_delivery_requests` (`refusals`) with the event's throttle, a writer in `internal/remotecuration/rules` |

Contracts to the others: `remotecuration.Decider` (C's port, implemented here); the refusal's code and text; the listing's `delivery` state for R3
and the screens.

**As built** (2026-11-23, [Q-CUR-40](../spec/remote-curation.md#q-cur-40--a-refusals-count-is-summed-on-the-replica-and-written-every-five-seconds)
to [Q-CUR-42](../spec/remote-curation.md#q-cur-42--the-places-the-rules-decide-as-built-kit-check-23-oci-and-debian-in-the-kit), which also
lists what of R2 is not built):

| File or directory | What |
|---|---|
| [rules](../../internal/remotecuration/rules/rules.go) (leaf) | the compiled `Set` (`Compile`, `Decide`, `Package`, `Fold`, `Fingerprint`, `KeepsAway`, `Blocks`, `Names`, `VersionRules`), the `Refusal` and its text, the journal's `Narrowing` |
| [rules/store](../../internal/remotecuration/rules/store/store.go) (domain) | the rules' store (`Put`/`PutTx`, `Delete`/`DeleteTx`, `List`, `Canonical`), the revision, the change area, the events and `repository.narrowed`; [record.go](../../internal/remotecuration/rules/store/record.go): the record of a refusal (event, metric, request-list count) |
| [delivery_rules.sql](../../internal/meta/postgres/queries/delivery_rules.sql) | the statements of the snapshot, the API, the replay and the record |
| `internal/repository` | `remote.delivery`, `remote.mirror.include`, no patterns on a remote ([delivery.go](../../internal/repository/delivery.go): the set in the snapshot, `Decider`, `WithRules` for tests); the parts `rules` and `deliveryMode` and their replay ([journal_replay_delivery.go](../../internal/repository/journal_replay_delivery.go)); the search guard ([withheld.go](../../internal/repository/withheld.go)); scanner-trivy's product rule ([upstream_store.go](../../internal/repository/upstream_store.go)) |
| [delivery.go](../../internal/formats/delivery.go) | what a format uses: `Delivery`, `Undelivered`, `DeliveryView`, `DocumentView`, `DocumentFingerprint`, `MemberDelivery`; `Host.RecordRefusal` called by `ServeRoute` |
| `internal/formathost` | step 5a (`undelivered` in remote.go, a member's in virtual_remotes.go), the fourth check (`rulesRefusal` in signature_gate.go), the mirror's admission (mirror_host.go), `RecordRefusal` |
| `internal/proxy`, `internal/signatures/gate`, `internal/virtual`, `internal/errs/member.go` | `Request.Undelivered`; `Serve.Rules`; the walk's first refusal |
| each format's `delivery.go` | the documents and the merge of a virtual repository; OCI's manifest by digest; Debian's merge in `internal/formatdata/deb` |
| [checks_delivery.go](../../internal/formats/formattest/checks_delivery.go) | kit check 23 |
| [delivery_rules_api.go](../../internal/api/v1/delivery_rules_api.go), [wire_delivery_rules.go](../../internal/app/wire_delivery_rules.go) | the API and the area `delivery rules` |

Tests (R-CUR-022): unit, the decision table and the comparator (about 1 s); kit check 23 for five formats (milliseconds each); integration,
one case per format in its area (`TestR_CUR_003_…`, about 2 s each, 10 s in all), plus `TestR_CUR_002_…`, `TestR_CUR_004_…`,
`TestR_CUR_005_…`, `TestR_CUR_006_…` and the mirror's `TestR_CUR_017_…` (about 10 s); the existing tests that set patterns on a remote
are rewritten to rules in the same change (their count is listed in the commit). Data: the stubs' existing corpora. What a leg gains: the
unit job about 1 s, the integration shards about 20 s.

**What the landing added** (the three conditions before R2 landed, and the three tests its first report left open):

| Where | What |
|---|---|
| [validate_mirror.go](../../internal/repository/validate_mirror.go) `UnboundedMirror`, [engine.go](../../internal/mirror/engine.go) `Dispatch`, [pass.go](../../internal/mirror/pass.go) `begin`, [mirror_api.go](../../internal/api/v1/mirror_api.go) | the bound of R-REPO-052 at a pass: a stored mirror of a preset without a bounding `remote.mirror.include` and without `maxBytes` is not dispatched, its pass ends `skipped: unbounded`, its status is `unbounded` and its run answers 422 `mirror_unbounded` (Q-CUR-43); one predicate with the save's `checkMirrorBound` |
| [mirror-settings.ts](../../web/src/app/features/repositories/mirror-settings.ts), [repository-create.html](../../web/src/app/features/repositories/repository-create.html), [repository-detail.html](../../web/src/app/features/repositories/repository-detail.html), [repository-model.ts](../../web/src/app/features/repositories/repository-model.ts), [mirror-status.ts](../../web/src/app/features/repositories/mirror-status.ts) | no pattern fields on a remote's forms (nor sent by a create or a save); **Mirror only these packages** (`remote.mirror.include`) in the mirror's form; the state **No bound** and the run's refusal in the status |
| [forward.go](../../internal/bastion/engine/forward.go) `onPass`, `noStore` | a Bastion keeps no `403` that says `Cache-Control: no-store`, so an allow reaches its clients at once (Q-CUR-44) |
| [store.go](../../internal/repository/store.go) `toDocument` | a remote's document is read without the patterns a section stored before the rules held, so its updates and the reconcile's write of `upstream/scanner-trivy` are not refused for them (Q-CUR-45) |

Their tests: `TestR_CUR_002_AStoredRemotesPatternsAreNotRead` (unit, milliseconds); `TestMirrorBound` (the save's cases against `UnboundedMirror`) and `TestR_REPO_052_AMirrorWithoutABoundDoesNotRun` (unit,
milliseconds, no data); `TestR_REPO_056_MirrorStatusDocument` and `TestR_REPO_056_MirrorRun` (unit, the API's fakes);
`TestR_CUR_004_ARuleRefusalReachesTheClientThroughABastionAndIsNotKept` (unit, `internal/bastion/engine`, milliseconds, one 1 kB tarball);
`TestR_CUR_005_TheRowsOfTheProjectUpstream` (integration, area `repository`, about 0.5 s on one replica, rule rows only);
`TestR_CUR_006_TheRulesAndTheModeNarrowedAfterARestorePointAreReplayed` (integration, area `backup`, about 1.2 s, about 100 rule rows);
the web's unit tests of the forms and the status (Vitest, milliseconds each); the subtest of `TestR_CUR_010_ADownloadAheadOfOneVersionAndOfAPackage`
(area `npm`, about 0.1 s, one tarball) that holds R4's queue to the rules' decider: a reader may not start a version a rule blocks, a
Maintainer may, and its row reads `blocked`.

## R3 · Upstream catalogue

| Area | Files |
|---|---|
| The engine | `internal/remotecuration/catalogue/` (the lent upstream that stores nothing, the budget on the cluster limiter, the cache, the bounds, the shape fallback) |
| The lent upstream | `internal/proxy/catalogue_upstream.go` (a `discover.Upstream` over the remote's client that answers `Document` from the store or the network without storing and refuses `Artifact`) |
| The formats | `internal/formats/{npm,nuget,oci,deb,ansible}/catalogue.go`; the presets' `CatalogueHosts` in `oci/format.go` and `npm/format.go`; the Debian parse in `internal/formatdata/deb/remote_store.go` (provides, short descriptions, `Description-md5`, and the long descriptions of `i18n/Translation-en` as an index of their own, Q-CUR-16) |
| The verdicts of unstored versions | a port over `Evaluator.EvaluateMiss` in `internal/vuln/` |
| The API | `internal/api/v1/upstream_catalogue_api.go`, `api/openapi.yaml` (`# >>> T-24 catalogue`) |
| The kit | check 22 in `internal/formats/formattest` with each format's fixture of canned answers |

Tests: unit, the engine with a fake format and upstream (about 1 s); kit check 22 for five formats (milliseconds each; canned answers of a
few kilobytes in each format's fixture); integration, `TestR_CUR_008_…` and `TestR_CUR_009_…` (npm stub with a search route, about 5 s)
and the Debian search (`TestR_DEB_036_…`, about 3 s). What a leg gains: the unit job about 1 s, the shards about 8 s.

**As built** (2026-11-24, [Q-CUR-60](../spec/remote-curation.md#q-cur-60--the-catalogues-upstream-is-lent-by-the-mirror-host-which-the-roles-of-the-management-api-now-build)
to [Q-CUR-65](../spec/remote-curation.md#q-cur-65--a-bulk-and-a-preview-read-the-catalogue-a-preview-is-one-page-of-the-bulks-bound); what is
not built is [Q-CUR-61](../spec/remote-curation.md#q-cur-61--a-rows-stored-state-is-what-the-repository-holds-the-stored-versions-verdicts-stay-the-version-views)'s
verdicts in the row and [Q-CUR-62](../spec/remote-curation.md#q-cur-62--debian-searches-the-name-provides-and-the-short-description-the-long-descriptions-and-read-the-index-wait)'s
Debian long descriptions and **Read the index**):

| File or directory | What |
|---|---|
| [catalogue](../../internal/remotecuration/catalogue/catalogue.go) (domain) | the engine: `Caps`, `Search`, `Children`, `Package`, `Versions` (a page of any size, for the preview); the budget per repository and per person (`Budget`, the cluster limiter, [window.go](../../internal/remotecuration/catalogue/window.go) where none is wired), spent before every request to the network; the per-replica cache of parsed pages ([cache.go](../../internal/remotecuration/catalogue/cache.go)), which keeps a page that found nothing only when the call spent a unit, that is asked the upstream (`foundNothing`, Q-CUR-164: Debian's not-found before a parse is asked again); the bounds (text, page, timeout, size); a capability an unexpected answer turned off for the cache period; the two metric families ([metrics.go](../../internal/remotecuration/catalogue/metrics.go)); `Stored` ([stored.go](../../internal/remotecuration/catalogue/stored.go)), which versions of a page a repository holds |
| [discover/catalogue.go](../../internal/mirror/discover/catalogue.go), [discover/page.go](../../internal/mirror/discover/page.go) | `UnavailableError` (a route the upstream does not answer, or an answer not of the expected shape, with the search's hint while it is off), `SearchHint` (the codes of the sentence under the search field, Q-CUR-166), `RepositoryIDer`, the page bounds, `VersionFacts.UnpackedSize`; `PageVersions`, `PageTaggedVersions` (npm's: the filter holds in the version or in the name of a dist-tag that points at it, Q-CUR-182), `PageHits`, `CutText`, `FormatLevel`, `ParseLevel`; `LevelPage.Unknown` (`UnknownLevel`, `NameRule`) and its helpers `Forbidden`, `Exists` (a `HEAD` of an account of the name) and `EmptyOrUnknown` ([Q-CUR-165](../spec/remote-curation.md#q-cur-165--a-level-the-upstream-does-not-have-says-so-with-the-formats-naming-rule-and-one-suggestion-built-from-it)) |
| [proxy/catalogue_upstream.go](../../internal/proxy/catalogue_upstream.go) | `Engine.Peruse`: a document read from the upstream as a fetch builds its request (the format's request, the URL map, the discovery document) and sent as a discovery request; nothing written |
| [formathost/catalogue_upstream.go](../../internal/formathost/catalogue_upstream.go) | `MirrorHost.CatalogueUpstream`, the lent upstream: the stored copy by the cache view (the rules refuse nothing there, `catalogueFlow`), else `Peruse`; `Artifact` refused; the preset's catalogue hosts as hosts without a credential; a document read once per call; the circuit, a pre-emption and the offline mode as the API's answers |
| [background_download.go](../../internal/formathost/background_download.go) | the bulk's planner over the catalogue: `Versions` pages `Package`, `Packages` pages `Children` of the section's level, on an upstream that reaches the hosts the tab's catalogue reaches (`catalogueHosts`: a preset's catalogue hosts without a credential), while a row's run keeps the format's mirror hosts (Q-CUR-167) |
| `catalogue.go` of [npm](../../internal/formats/npm/catalogue.go), [nuget](../../internal/formats/nuget/catalogue.go), [oci](../../internal/formats/oci/catalogue.go), [deb](../../internal/formats/deb/catalogue.go), [ansible](../../internal/formats/ansible/catalogue.go) | each format's `formats.Catalogue`; OCI's capabilities by preset (Docker Hub's Hub API with its fallback, Quay's API, GitLab's project API, ghcr.io's tags list only, a registry's `_catalog`), its presets' `CatalogueHosts` in [format.go](../../internal/formats/oci/format.go); Debian's over the data port's `CatalogueStore` |
| [catalogue_store.go](../../internal/formatdata/deb/catalogue_store.go), [remote_parse.go](../../internal/formatdata/deb/remote_parse.go) | the Debian rows the catalogue reads; the parse keeps `provides`, the short description and `description_md5` |
| [oci/catalogue.go](../../internal/formats/oci/catalogue.go) `Package` | an image's tags newest first by the tag order on every upstream (`newestTagFirst`, Q-CUR-180): Docker Hub's and Quay's APIs read whole up to `wholeTagPages` (5) pages of 100 and sorted, an image with more from the distribution tags list; attachment tags dropped (Q-OCI-268); the bulk's planner takes them in this order, so the newest run first |
| [upstream_catalogue.sql](../../internal/meta/postgres/queries/upstream_catalogue.sql) | the stored versions of a page: the version rows, and an OCI remote's listed tags with the view `oci_remote_tags`'s rules written per entry (Q-CUR-156); Debian's directories, indexes, search and rows |
| [checks_catalogue.go](../../internal/formats/formattest/checks_catalogue.go) | kit check 22 and its fixture types; `testformat.CatalogueRawVersions` |
| [upstream_catalogue_api.go](../../internal/api/v1/upstream_catalogue_api.go), `api/openapi.yaml` (`# >>> T-24 catalogue`) | the five operations and each row's state; the verdicts over [vuln.MissVerdicts](../../internal/vuln/miss_verdicts.go) and `evaluate.Service.Covers` |
| [remote_downloads_api.go](../../internal/api/v1/remote_downloads_api.go) | R4's rest: the preview's counts (`preview`) and a section's start (`sectionOffered`) |
| [wire_upstream_catalogue.go](../../internal/app/wire_upstream_catalogue.go) | the engine of the process, the limiters `catalogue.repository` and `catalogue.person`, the lender over the area `mirror-engine`, now built on `api` and `web` too ([steps.go](../../internal/app/steps.go)) |

Tests and what they cost (each run once):

| Test | Tier | Run time | Data |
|---|---|---|---|
| `TestR_CUR_008_TheCatalogueEngine` (`internal/remotecuration/catalogue`) | unit | milliseconds | none (a fake format and upstream) |
| kit check 22 of npm, NuGet, OCI (five cases), Debian, Ansible Galaxy (two cases) and the test format | unit | milliseconds per format | recorded answers in each format's `testdata/catalogue/`: npm 11 KB, NuGet 56 KB, OCI 43 KB, Debian 41 KB, Ansible 14 KB |
| `TestR_OCI_038_DockerHubsSearchFallsBackWhenItsAnswerIsNotAPage` (`internal/formats/oci`) | unit | milliseconds | answers of a few bytes |
| `TestR_CUR_007_DockerHubsAndQuaysTagsComeInTheTagOrder`, `TestR_SIG_024_AttachmentTagsAreNoVersionsOfTheUpstreamCatalogue` (`internal/formats/oci`): Docker Hub and Quay read whole up to five pages and sorted by the tag order, the tags list above that, no attachment tag from any source (Q-CUR-180, Q-OCI-268) | unit | milliseconds | canned answers of a few hundred bytes |
| `TestR_CUR_007_TheSearchHintsOfTheAPIAreTheCatalogues` (`internal/api/v1`) | unit | microseconds on the shared parse | none |
| "the hint under the search field is the German sentence for its code in the German build" (`remote-upstream.spec.ts`) | Vitest | milliseconds | the German translation file's 14 hint units |
| `TestR_CUR_007_ALevelTheRegistryDoesNotHaveIsUnknown` (`internal/formats/oci`), `TestR_CUR_007_AGalaxyNamespaceItDoesNotHaveIsUnknown` (`internal/formats/ansible`), `TestR_CUR_007_ADebianLevelTheUpstreamDoesNotHaveIsUnknown` (`internal/formats/deb`), the case's unknown level of kit check 22 (Docker Hub, Quay, GitLab, npm, the Galaxy, Debian), `TestR_CUR_007_AnUnknownLevelIsKeptAndAPageWithEntriesIsKnown` (the engine), `TestR_CUR_009_AnUnknownLevelNamesTheUpstream` (the API) | unit | milliseconds each | recorded answers of a few hundred bytes each, and Debian's `InRelease` of 32 KB (Q-CUR-165) |
| `TestR_DEB_036_TheParseKeepsWhatTheCatalogueSearches` (`internal/formatdata/deb`) | unit | milliseconds | the Debian fixture's 8 KB |
| `TestR_CUR_007_RegisterRefusesARemoteFormatWithoutACatalogue` (the adapter's case) | unit | milliseconds | none |
| `TestR_CUR_009_TheVersionFilterFindsAVersionByTheNameOfItsTag` (`internal/mirror/discover`): a tag's name finds its version past the first page, `latest` first (Q-CUR-182) | unit | under 1 ms | 150 version strings in memory |
| `TestR_CUR_008_TheCatalogueReadsWithoutStoringAndAnswersFromItsCache` (area `npm`) | integration | about 1.8 s | the stub's corpus, its new search route |
| `TestR_CUR_009_TheRowsCarryTheRepositorysStateAndAReaderSeesWhatIsDelivered` (area `npm`) | integration | about 0.9 s | the stub's corpus, one tarball |
| `TestR_DEB_036_TheCatalogueSearchesTheIndexesTheRemoteParsed` (area `debremote`) | integration | about 1.6 s (it looks the package up before the parse and waits on the catalogue, Q-CUR-164) | the stub archive |
| `TestR_CUR_009_TheCatalogueSaysStoredOfATagARemotePulledThrough` (area `ociremote`) | integration | about 2.3 s | the stub's image `acme/b`: one index of 1.3 KB pulled |
| `TestR_CUR_010_ASubSectionIsPreviewedAndDownloadedThroughTheCatalogue` (area `ansibleremote`) | integration | about 3.2 s | two archives of the stub's corpus |
| the preview's counts in `TestR_CUR_010_ADownloadAheadOfOneVersionAndOfAPackage` (area `npm`) | integration | unchanged | unchanged |

What a leg gains: the unit job well under a second; the integration shards about 9.8 s (npm 2.7 s, debremote 1.5 s, ansibleremote
3.2 s, ociremote 2.3 s). A new format adds its `catalogue.go`, a kit fixture of a few kilobytes and no integration case of its own unless its catalogue
reads rows of its own (as Debian's).

## R4 · Downloads ahead and the queue

Built on 2026-11-23 (R-CUR-010 to R-CUR-013, Q-CUR-80 to Q-CUR-85). No migration: the tables are 00114's. What it holds:

| File or directory | What |
|---|---|
| [internal/remotecuration/queue](../../internal/remotecuration/queue/queue.go) | `queue.go` the row, the bulk, the settings, the result of a run, the errors (`FullError`, `QueuedError`); `service.go` the service: `Add` (a row, started at once when its repository has a free place), `Start`, `Claim` and `Finish` of a run, `Retry`, `RetryMatching`, `Cancel`, `Queued` and `Active` (`remotecuration.Queue`); `decide.go` the backoff and the end of an attempt (`settle`), pure; `limit.go` an upstream's rate limit that holds the queue
(`Limit`, `nextLimit`, `endsLimit`, `limitPlaces`, `RunLimitWait`; R-CUR-011, Q-CUR-170 to Q-CUR-173), read and written by `Finish`'s
`limitAfter` and `fill` under the repository's lock; `bulk.go` the bulk, its expansion through the `Planner` port and its bounds; `sync.go` the downloads kept in sync (R-CUR-027, Q-CUR-240 to Q-CUR-250): `Sync`, `CreateSync`, `RunSync`, `PauseSync`, `DeleteSync`, `startSyncs` (the dispatch's look at every due sync in its own transaction under the repository's lock, `syncCheck` the order of R-CUR-027) and `filterOf` (what a run's step leaves out: what the `Decider` does not deliver, what `SyncRepositories.Fixed` says is a stored version row); `sync_pg.go` the syncs over PostgreSQL; `range.go` a package's range of versions (`Range`, `Order` over the format's view adapter and canon, which the edge builds from the registered format, since the domain imports no format; `Selection` and `Picker`, which take a package's versions in the catalogue's order: the range, then the pre-releases, then the newest `maxVersions`; Q-CUR-190 to Q-CUR-192), used by the planner and by the preview alike; `dispatch.go` the task `remote-downloads-dispatch` (orphans, due repositories, one step of every expanding bulk, the bulks' ends, retention, the gauge); `client.go` the rows of clients' failed fetches with their throttle; `metrics.go` the three families of R-CUR-019; `store.go` the ports `Store`, `Tx`, `Reader` (with `ListBulks`, a page of a repository's bulks with their rows by state, Q-CUR-168), `Maintenance`; `store_pg.go` PostgreSQL |
| [remote_downloads.sql](../../internal/meta/postgres/queries/remote_downloads.sql) | every statement of the queue; a row and a bulk cross the boundary as `to_jsonb` text, decoded by one function each; the repository's row `FOR NO KEY UPDATE` before every start (Q-CUR-82); `RemoteDownloadDebDirs` for Debian's `AnyIndex` scope; `RemoteDownloadList` lists rows queued together (a bulk's, one creation time) by id ascending, the order they run (Q-CUR-181) |
| [background_download.go](../../internal/formathost/background_download.go) | `MirrorHost.RunDownload`, the run of every row: what has nothing to do (`skipped`), the charge (`limits.Engine.Resume`), the plan (the row's probe, else `formats.VersionProber`), `ProbeContent`, the fills in the run's window, the refusals counted and kept, the failure in words (`proxy.Describe`); `MirrorHost.Versions` and `Packages`, the planner of a bulk over the format's catalogue (R3); `Versions` takes the bulk's `queue.Selection` and applies it with a `queue.Picker` over the repository's format while it pages |
| [probe.go](../../internal/formathost/probe.go) | the decision on a probe inserts a row of origin `probe` through the port `Downloads` (`Queued`, `AddProbe`) and counts through `ProbeCounter` |
| `version_probe.go` of [npm](../../internal/formats/npm/version_probe.go), [nuget](../../internal/formats/nuget/version_probe.go), [oci](../../internal/formats/oci/version_probe.go), [deb](../../internal/formats/deb/version_probe.go), [ansible](../../internal/formats/ansible/version_probe.go) | `formats.VersionProber`: npm, NuGet and Debian from their mirror plan's rows (`formats.ProbeFromPlan`), OCI the tag's manifest by its digest, Ansible Galaxy the archive after its version document; Debian looks in every verified suite and parsed binary index (`rows.MirrorScope.AnyIndex`, [mirror_store.go](../../internal/formatdata/deb/mirror_store.go)) |
| [catalogue.go](../../internal/formats/catalogue.go), [remote.go](../../internal/formats/remote.go) | `VersionProber`, embedded by `Catalogue`; `RemoteCaps.VersionProbe` |
| [failure.go](../../internal/proxy/failure.go), [classify.go](../../internal/proxy/classify.go), [fetch.go](../../internal/proxy/fetch.go) | `proxy.Exchange` (the step, the address without its query, the status, the excerpt, the Retry-After) as the cause of the engine's error; `Describe` (with `target` from `upstream.RefusedTarget`, the cause of a refused address); `ClientFailures` and the leader's record of a client's failure (Q-CUR-84) |
| [fill.go](../../internal/proxy/fill.go), [enforce.go](../../internal/vuln/enforce/enforce.go) | `Miss.NoClient`: a fill without a client refused by the miss gate counts no refused download and writes no event (Q-CUR-22) |
| [background.go](../../internal/limits/background.go) | `Engine.StartDownload`: step 9a of a person's download of one version, no request counted (Q-CUR-85) |
| [access.go](../../internal/api/edge/access.go) | `Access.MayPopulateAct`: the populate check of an explicit act, without the session cookie's refusal (Q-CUR-27) |
| [remote_downloads_api.go](../../internal/api/v1/remote_downloads_api.go), `api/openapi.yaml` (`# >>> T-24 downloads`) | the operations of R-CUR-010 and R-CUR-012 and who may call them (R-CUR-005); a package's range: `rangeSyntax` (the members, only for a package), `downloadRange` (ranked by the repository's format, canonical, not empty), `previewPackage` (the preview's count by the planner's `Picker`, paging on with a range) and the bulk's `bounds` (`boundsOf`) |
| [00129_remote_download_bulk_range.sql](../../internal/meta/postgres/migrations/00129_remote_download_bulk_range.sql) | the bulk's range: `from_version`, `to_version`, `to_inclusive` of `remote_download_bulks` (Q-CUR-193) |
| [download_syncs_api.go](../../internal/api/v1/download_syncs_api.go), [00130_remote_download_syncs.sql](../../internal/meta/postgres/migrations/00130_remote_download_syncs.sql) | the syncs' operations (`GET`/`POST download-syncs`, `run`, `pause`, `resume`, `DELETE`; `mayKeepInSync`, `syncError`) and the preview's `sync` (`syncPreview`, `firstRun`); the table of syncs and the origin `sync` (R-CUR-027) |
| [mirror_host.go](../../internal/formathost/mirror_host.go) | `fillPath` asks the rules for a row of the origin `sync` (Q-CUR-243); `MirrorHost.Offline` in `background_download.go` for a sync's look |
| [wire_remote_downloads.go](../../internal/app/wire_remote_downloads.go) | the service (built at its first use) with the syncs' ports (`syncRepositories`: `remote.syncs` and whether the repository may run, the version rows a run leaves out in the format's canon; the delivery rules' decider; `syncRunEvent`, the event of a run the dispatch starts with the actor `system:download-sync`), the kind `remote-download` (queue `downloads`, one attempt, exclusive per row, 30 minutes), the task `remote-downloads-dispatch` (queue `maintenance`), the ports of the format host and the remote engine, the API's services; it replaces `wire_background_download.go` |

The run's end: a cancelled job ends its row `cancelled`; a job that reached its 30 minutes ends its attempt `bound` (Q-CUR-83); a job
released at shutdown or whose lease was lost leaves its row `running` with its job, which runs again or which the dispatch finds orphaned.
The delivery rules' decider is `repository.Decider` over the replica's snapshot, wired with R2's landing (`remoteDownloadsAPI` in [wire_remote_downloads.go](../../internal/app/wire_remote_downloads.go)): who may start a version and the
row's member `delivery` read it.

Not built here, and why: a sub-section's bulk and the counts of a package's preview waited for the catalogue and are R3's (Q-CUR-65); the tab
**Downloads** is R5a's; the examination's rows (`remote_examinations`) and the request's examination are R2b's (built, below).

Tests and what they cost (each run once, R-CUR-022):

| Test | Tier | Run time | Data |
|---|---|---|---|
| `TestR_CUR_011_TheFailureOfAnExchangeInPreciseWords` (`internal/proxy`) | unit | about 20 ms | answers of an `httptest` server of a few hundred bytes, a closed port, a resolver that answers nothing, an untrusted certificate |
| the six tests of [queue_test.go](../../internal/remotecuration/queue/queue_test.go) (order and bound, waiting bound, backoff, retries, client throttle, bulks, a package's range of versions) | unit | milliseconds | none (a store in memory; seven version strings for the range) |
| the three tests of [limit_test.go](../../internal/remotecuration/queue/limit_test.go) (an upstream's limit holds the queue, a row that ran before it, the times), `TestR_CUR_011_ARunOfTheQueueWaitsOutOnlyAShortRateLimit` (`internal/mirror`), `TestR_CUR_011_ARateLimitKeepsOnlyTheHeadersOfTheLimit` (`internal/proxy/upstream`) | unit | milliseconds | none (a store in memory, a fake clock) |
| `TestR_CUR_011_AnUpstreamsRateLimitHoldsTheQueue` (area `npm`) | integration | about 2 s | two tarballs of the stub's corpus, a few hundred bytes each |
| `TestR_CUR_010_APersonsDownloadOfOneVersionIsChargedToThePerson` (`internal/limits`) | unit | milliseconds | none |
| `TestR_CUR_014_AFillWithoutAClientIsNoRefusedDownload` (`internal/vuln/enforce`) | unit | milliseconds | none |
| `TestR_CUR_010_ADownloadAheadOfOneVersionAndOfAPackage`, `TestR_CUR_011_AFailureInItsOwnWordsAndTheRetries` (area `npm`) | integration | about 4 s each (the range's subtest 0.1 s of it) | the stub's corpus: one package of three versions; for the range another package of at least four, two tarballs queued |
| `TestR_CUR_010_…` of the areas `nuget`, `ociremote`, `debremote`, `ansibleremote` | integration | about 3 s each | one version of each stub's corpus |
| the existing `TestR_REPO_065_*` against the row, one more assertion of the row's counters | integration | unchanged | unchanged |
| `TestR_CUR_027_ADueSyncStartsABulkOnceAtATime`, `TestR_CUR_027_ASyncsExpansionLeavesOutWhatIsStoredAndWhatIsNotDelivered` ([sync_test.go](../../internal/remotecuration/queue/sync_test.go)) | unit | milliseconds | none (the store in memory, a fixed clock) |
| `TestR_CUR_027_ASyncRunsAgainAndStoresWhatIsNewAndDelivered` (area `npm`), `TestR_CUR_027_ASyncFetchesATagThatMovedAgain` (area `ociremote`) | integration | about 4 s each | three tarballs and two the stub publishes; one image of the OCI stub moved once |

What a leg gains: the unit job well under a second; the integration shards about 20 s together (npm 8 s, four formats 3 s each). A new
format adds its `version_probe.go` and one `TestR_CUR_010_…` of about 3 s in its own area.

## R2b · Requests, examination, retention

Built on 2026-11-24 (R-CUR-014 to R-CUR-016, [Q-CUR-140](../spec/remote-curation.md#q-cur-140--an-examination-row-names-the-files-its-runs-stored-a-discard-removes-what-belongs-to-the-version-alone)
to [Q-CUR-148](../spec/remote-curation.md#q-cur-148--every-persons-ask-is-recorded-the-last-asker-who-withdraws-ends-an-entry-no-client-was-refused)).
Migration [00115_remote_examination_files.sql](../../internal/meta/postgres/migrations/00115_remote_examination_files.sql): the files of an
examination row, the origin, request and charge of a bulk, and why an entry's examination did not start. What it holds:

| File or directory | What |
|---|---|
| [requests](../../internal/remotecuration/requests/requests.go) (domain) | `requests.go`: `Check` (the subject in canonical forms, `already_delivered`, the catalogue's lookup when wired, Q-CUR-145) and `AskTx` (one open entry per subject, the bound per person, the examination of the entry's first person: a row of origin `request`, or of a package a bulk of its newest versions, Q-CUR-141, charged by the caller's `Charge`); `AllowTx` (the rule through `rules/store.PutTx`, the state, the downloads: the examination's failed rows again, else a row or a bulk of origin `allow`, or a bulk of every version); `DeclineTx` (the reason, the examination's waiting rows and bulks cancelled, its stored versions discarded, Q-CUR-143); `WithdrawTx` (Q-CUR-148); `list.go`: `List`, `ViewTx`, `Get` and `Open` (`remotecuration.Requests`) |
| [retention](../../internal/remotecuration/retention/retention.go) (domain) | `retention.go`: `DiscardTx` (the versions of a subject the rules do not deliver, their files by the examination row and the package rows filtered by the format's `Classify`, Q-CUR-140, removed through `items.Store.RemoveIn`, the rows, the event with its journal entry) and `Cleanup` (the task: rows older than a day in batches of 1,000, a delivered version's row dropped, Q-CUR-142, an expired one discarded as `system:examination-cleanup`; the entries without an asker after `request_retention`); `journal_replay.go`: the replay of `remote.examination.discarded` (the rows due, a run of the task queued, Q-CUR-144) |
| [delivery_requests.sql](../../internal/meta/postgres/queries/delivery_requests.sql) | every statement of the requests, the examination rows, the bulk of a request and the stored files of a version |
| [queue](../../internal/remotecuration/queue/queue.go) | `Result.Examination`, written with the end of an attempt (`store_pg.go` `Settle`); a bulk's `Origin`, `RequestID` and `Ticket`, which its rows take |
| [background_download.go](../../internal/formathost/background_download.go) `examinationOf`, [mirror_host.go](../../internal/formathost/mirror_host.go) `admitted` | a run that stored a file of a version the rules do not deliver holds it for examination with its files; a row of the queue is admitted past the rules, a pass of the mirror is not (Q-CUR-147) |
| [rules/store](../../internal/remotecuration/rules/store/store.go) `examinationsDelivered` | an allow written, or a block removed, removes the examination rows of its subject that the rules then deliver (`Target.Mode` carries the caller's mode) |
| [delivery_requests_api.go](../../internal/api/v1/delivery_requests_api.go), `api/openapi.yaml` (`# >>> T-24 requests`) | the list, the ask, allow, decline, withdraw and the discard, and who may call them (R-CUR-005) |
| [wire_delivery_requests.go](../../internal/app/wire_delivery_requests.go) | both services, built at their first use in every role; the task `remote-examination-cleanup` (queue `maintenance`, mutating; it left `tasksNotBuilt`); the API's services |

Not built here: the gauge `quader_remote_delivery_requests_open` (R-CUR-019); the catalogue's lookup of an ask, which R3 wires into
`requests.Options.Lookup`; the screens (R5b).

Tests and what they cost (each run once):

| Test | Tier | Run time | Data |
|---|---|---|---|
| `TestR_CUR_015_ARequestsBulkQueuesTheNewestVersionsAsTheRequestsRows`, `TestR_CUR_016_TheEndOfAnAttemptWritesItsExamination` (`internal/remotecuration/queue`) | unit | milliseconds | none (the store in memory) |
| `TestR_CUR_016_ARunHoldsAnUndeliveredVersionForExamination`, the download-ahead rows of `TestR_CUR_017_TheMirrorAdmitsWhatItsScopeAndTheRulesAdmit` (`internal/formathost`) | unit | milliseconds | none |
| `TestR_CUR_016_TheReplayOfADiscardReadsItsEntry` (`internal/remotecuration/retention`) | unit | milliseconds | none |
| `TestR_CUR_014_AnExaminationIsStoredScannedAndNotDelivered`, `TestR_CUR_015_ARequestIsExaminedAllowedOrDeclined`, `TestR_CUR_016_WhatWasExaminedIsRemovedAfterItsDaysOrADiscard` (area `npm`) | integration | about 2 s, 6 s and 3 s | the stub's corpus: four tarballs of a few kilobytes |
| `TestR_CUR_016_ADiscardAfterARestorePointIsReplayed` (area `backup`) | integration | about 1 s | two examination rows, no artifact |

What a leg gains: the unit job well under a second; the integration shards about 12 s (npm 11 s, backup 1 s). No format adds anything:
the examination, the discard and the retention are the same for every format with a remote class (Q-CUR-140).

## R5a and R5b · The screens

| Package | Files |
|---|---|
| R5a (built) | see the table below |
| R5b (built) | see the second table below |

R5a, built on 2026-11-24 (R-CUR-018, Q-CUR-130 to Q-CUR-133):

| File | What |
|---|---|
| [repositories.routes.ts](../../web/src/app/features/repositories/repositories.routes.ts) | `…/repositories/{key}/downloads`: the content page with the route's `data.tab` `downloads`, titled **Downloads** |
| [repository-content.ts](../../web/src/app/features/repositories/content/repository-content.ts), `.html` | the input `tab`; **Upstream** and **Downloads** in the strip of a remote's page for a signed-in caller (`remoteTabs`); the view `downloads`; the address of a local or virtual repository's tab Downloads leads to its content; the repository's `cacheFillAction` for the tab |
| [repository-tabs.ts](../../web/src/app/features/repositories/content/repository-tabs.ts) | the strip of every repository's page (`remote` for **Upstream** and **Downloads**, `details` for **Details**, the last, Q-UI-1067): the content tab captioned as its list, **Downloads** with `· n failed`; it reads the counts once on the content tab, the tab Downloads passes its own |
| [downloads/](../../web/src/app/features/repositories/downloads/remote-downloads.ts) | `remote-downloads.ts`, `.html`, `.scss`: the table **Bulk downloads** over the head (a bulk's state, counters, failure in words, **Cancel** or **Start again**, Q-CUR-168), the head (states, **Failure**, **Retry all failed**, the pause), the bulk's line, the table of rows, the opened row, the refresh of both; `download-syncs.ts`, `.html`: the section **Kept in sync** over the bulks (R-CUR-027, Q-CUR-250), its rows' menu and its own live read (`DOWNLOADS_REFRESH_MS` of `downloads-refresh.ts` while a run is in progress); `download-words.ts`: the words of the states, outcomes, classes and steps, the interval of the syncs (`syncIntervalSentence`), a bulk's range of versions (`versionRangeText`, "From 5.0.0 up to 5.4.7", on the bulk's line in the table and above its rows, Q-CUR-193), the failure's one line, and `failureSentence`, the failure in the screen's language from its code and `target`, else its class (Q-CUR-169; a new code of the engine or the queue gets a sentence here and a row in Q-CUR-169's table) |
| [remote-downloads-api.ts](../../web/src/app/api/remote-downloads-api.ts) | `RemoteDownloadsApi` over R-CUR-012's operations (the start and the preview are R5b's), its provider in `providers.ts`, the fake in [fakes-remote-downloads.ts](../../web/src/app/api/testing/fakes-remote-downloads.ts) |
| [problem-messages-downloads.ts](../../web/src/app/core/http/problem-messages-downloads.ts) | the words of `not_retryable`, `already_queued`, `download_queue_full` |
| [downloads.ts](../../web/e2e/renders/downloads.ts), `screens.ts` | the renders: `repository-npm-remote-downloads` (the seed's queue, then bulks and a queue of every state answered in the browser, a failure opened, a bulk, the bulk that failed before its first row) and `repository-oci-probed-downloads` (a probe's row of the server's own) |

Tests and what they cost: [remote-downloads.spec.ts](../../web/src/app/features/repositories/downloads/remote-downloads.spec.ts), web unit,
twelve cases, about 0.7 s, rows of a few hundred bytes in memory; the renders gain two screens (the walk's four states as three users,
one state as one user, at three widths and two schemes: about 45 s on the development machine). No Go, no integration or end-to-end case: the queue's API is R4's and
held there.

What is rendered before each lands (rule 11; both schemes, 1440 px and 375 px, as `sam`, `priya` and `ada`, realistic data, never committed):
R5a, the strip on an npm and an OCI remote and its absence on a local one, the tab Downloads empty, with waiting and running rows, with a
failed row opened, with a bulk; R5b, every state of R-CUR-018: a search with states, a reader in mode `onlyAllowed`, a package with
thousands of versions, ghcr.io's lookup only, the upstream down, the budget used up, a too large document, Debian with a suite not read, the
requests panel, the preview, the rule list, the settings section. Tests: web unit (Vitest, seconds), the UI layout checks gain about 20
screens (about 60 s).

R5b, built on 2026-11-24 (R-CUR-018, [Q-CUR-150](../spec/remote-curation.md#q-cur-150--the-strip-holds-the-content-upstream-and-downloads-for-every-signed-in-reader-the-strip-counts-the-open-requests-itself)
to [Q-CUR-155](../spec/remote-curation.md#q-cur-155--the-catalogues-own-states-are-sentences-of-the-screen-the-upstreams-own-sentences-stand-as-the-upstream-wrote-them)).
No Go and no API change: the screens speak the operations of R2, R3, R4 and R2b as built. What every format shows comes from the
repository's capabilities (`GET …/upstream`), so a new format adds nothing to the screens but, where it names a level of a new kind or a
verdict that cannot exist, one word in [upstream-words.ts](../../web/src/app/features/repositories/upstream/upstream-words.ts).

| File | What |
|---|---|
| [upstream-catalogue-api.ts](../../web/src/app/api/upstream-catalogue-api.ts), [delivery-rules-api.ts](../../web/src/app/api/delivery-rules-api.ts), [delivery-requests-api.ts](../../web/src/app/api/delivery-requests-api.ts) | the catalogue's five operations (read without the client's retry, so a budget is not spent twice), the rules' list, write and removal, the requests' list, ask, allow, decline, withdraw and the discard; their providers in `providers.ts` (`// >>> T-24 upstream`), their fakes in [fakes-upstream-catalogue.ts](../../web/src/app/api/testing/fakes-upstream-catalogue.ts) |
| [remote-downloads-api.ts](../../web/src/app/api/remote-downloads-api.ts) | `preview` and `start` (R-CUR-010); the fake's in [fakes-remote-downloads.ts](../../web/src/app/api/testing/fakes-remote-downloads.ts) |
| [remote-upstream.ts](../../web/src/app/features/repositories/upstream/remote-upstream.ts), `.html`, `.scss` | the tab: the mode's line, the requests, the field of the search and the lookup, the levels, the list of hits opened in place, the catalogue's states; the query in the address |
| [upstream-package.ts](../../web/src/app/features/repositories/upstream/upstream-package.ts), `.html`, `.scss` | an opened package: its head with its acts and, in a row, only the rule the row does not name (`inRow`, Q-UI-1192), the versions with their filter (by a tag's name too, Q-CUR-182), paging, flags and npm tags, states, rule, cache, verdicts and acts |
| [dist-tags.ts](../../web/src/app/features/packages/dist-tags.ts) | `<quader-dist-tags>`: a version's npm tags as badges with their tooltip, at most three and "+N more" (Q-UI-1190, Q-UI-1191), shared with the package's and the version's page; `tagsByVersion` |
| [upstream-actions.ts](../../web/src/app/features/repositories/upstream/upstream-actions.ts) | the tab's acts and the caller's rights (`manage` or `admin`, the fill of a client), provided by the tab; its counter `changed` makes the views read again after a write |
| [delivery-requests-panel.ts](../../web/src/app/features/repositories/upstream/delivery-requests-panel.ts), [request-dialogs.ts](../../web/src/app/features/repositories/upstream/request-dialogs.ts) | the requests set apart; the dialogs of the ask, the allow and the decline |
| [delivery-rules-dialog.ts](../../web/src/app/features/repositories/upstream/delivery-rules-dialog.ts), [download-preview-dialog.ts](../../web/src/app/features/repositories/upstream/download-preview-dialog.ts) | the rule list; the preview of a bulk with **Once** or **Keep in sync** (R-CUR-027: the first run's count, the interval's sentence, a package not delivered or kept in sync already said instead of a set-up), its bounds, and for a package the range of versions (**From version**, **Up to version** with **included** or **below it**, the opened package's first page of versions as the fields' `datalist`, which the dialog reads itself; `rangeErrorText`, a refused bound in the screen's words at its field, Q-CUR-194) |
| [upstream-words.ts](../../web/src/app/features/repositories/upstream/upstream-words.ts) | the words of the modes, the states, the levels, the flags, the verdicts and the catalogue's refusals |
| [repository-tabs.ts](../../web/src/app/features/repositories/content/repository-tabs.ts), [repository-content.ts](../../web/src/app/features/repositories/content/repository-content.ts), `.html`, [repositories.routes.ts](../../web/src/app/features/repositories/repositories.routes.ts) | **Upstream** in the strip with its count, the route `…/upstream` |
| [repository-delivery.ts](../../web/src/app/features/repositories/repository-delivery.ts), `repository-detail.ts`, `.html` (`T-24 delivery`) | the settings' section **Delivery** |
| [repository-syncs.ts](../../web/src/app/features/repositories/repository-syncs.ts), `repository-detail.ts`, `.html` | the settings' section **Kept in sync** (`remote.syncs`, `#syncs`, R-CUR-027) |
| [problem-messages-downloads.ts](../../web/src/app/core/http/problem-messages-downloads.ts) | the words of the catalogue's, the rules' and the requests' codes |
| [upstream.ts](../../web/e2e/renders/upstream.ts), `screens.ts` | the renders: the npm walk of eleven states as three users (German too), an OCI lookup, a Debian remote's suites and search, the OCI remote of this installation with the server's own answers, the section Delivery of two remotes |

Tests and what they cost: [remote-upstream.spec.ts](../../web/src/app/features/repositories/upstream/remote-upstream.spec.ts), web unit,
fourteen cases, about 1.5 s, answers of a few kilobytes in memory (the npm tags' badge, "+N more" and the filter by a tag, and the
state said once, two cases of about 0.2 s together, Q-UI-1190 to Q-UI-1192);
[dist-tags.spec.ts](../../web/src/app/features/packages/dist-tags.spec.ts), one case of pure functions, milliseconds; [repository-delivery.spec.ts](../../web/src/app/features/repositories/repository-delivery.spec.ts),
two cases, about 0.2 s; one case of [remote-downloads.spec.ts](../../web/src/app/features/repositories/downloads/remote-downloads.spec.ts)
changed for the third tab. The renders gain six screens: about 4 minutes on the development machine, most of it the npm walk; its package
answers 24 versions, since a page of 100 stacked at 390 px is a picture of about 20,000 px. No Go, no integration or end-to-end case: the
operations are R2's, R3's, R4's and R2b's and held there; the end-to-end case of each format is R6's.

## R6 · Clients and documents

Built on 2026-11-24 ([Q-CUR-160](../spec/remote-curation.md#q-cur-160--an-oci-referrers-tag-is-no-version-it-follows-its-subject) to
[Q-CUR-163](../spec/remote-curation.md#q-cur-163--the-background-engine-of-the-role-api-shares-the-caches-of-its-read-only-view)).

| File | What |
|---|---|
| [curation_clients_env_test.go](../../test/e2e/curation_clients_env_test.go) | what each format's case shares: `onlyAllowed`, `allowRule`, `downloadAhead` (a row of the queue to its end), `awaitOnEveryPackagesReplica` (a rule is in every packages replica's snapshot when it serves by it), `noteCuration` (the artifact `curation-clients.json`, the rows of the manual's table), `caseClock` |
| `test/e2e/<format>_curation_test.go` | `Test<Format>Curation_R_CUR_004_…`, one per format in its own set: the client refused (what it prints), an allow of one version through the API (the client takes it where it resolves a range or "the newest"), a version downloaded ahead and allowed that the client takes from the cache (the stub's count); the npm set's expression in [e2e.sh](../../deploy/dev/e2e.sh) names `Curation` |
| [24-upstream.spec.ts](../../web/e2e/tests/24-upstream.spec.ts) | the journey of the tab Upstream: a Reader looks up and asks, a Maintainer allows from the requests, the version is delivered; `TestE1_11Journeys` publishes its package to the npm test upstream (`QUADER_E2E_UPSTREAM_PACKAGE`) |
| [remote-curation.md](../manual/user/remote-curation.md) (manual) | the page of curating a remote: the rules, the tab Upstream, requests, allowing, downloads ahead, the tab Downloads, the clients' table; the help of the tabs Upstream and Downloads opens it ([help-topics.ts](../../web/src/app/core/help-topics.ts)) |

What the cases found and what holds each now, at its cheapest tier:

| Defect | Fixed in | Held by |
|---|---|---|
| A manifest by digest of an image with version rules answered 500: the decision read the stored tags with no transaction | `TagTargets` in [store_signatures.go](../../internal/formatdata/oci/store_signatures.go) | `TestR_CUR_003_AnOCIRemoteInModeOnlyAllowedDeliversTheAllowedTagOnly` (area `ociremote`) |
| Docker's referrers tag (`sha256-<hex>`) was refused as a version, and the pull of an allowed tag failed (Q-CUR-160) | `Classify` in [paths.go](../../internal/formats/oci/paths.go), `remoteManifestGet` in [remote_handlers.go](../../internal/formats/oci/remote_handlers.go) | the same case |
| A Debian remote APT read by hash parsed no index: no rows for the catalogue, a download ahead or the pool digests (Q-CUR-162) | `byHashFilled` in [hooks.go](../../internal/formats/deb/hooks.go) | `TestR_DEB_023_AnIndexFetchedByItsHashIsParsed` (area `debremote`) |
| Every catalogue call of the role `api` panicked: its background engine registered the caches of the read-only view a second time (Q-CUR-163) | `proxy.Options.View`, [wire_virtual_engine.go](../../internal/app/wire_virtual_engine.go), [wire_mirror_host.go](../../internal/app/wire_mirror_host.go) | `TestR_CUR_009_TheCatalogueAnswersOnAReplicaOfTheRoleAPI` (area `npm`) |

Tests and what they cost: the five cases, 2.7 to 11.4 s each in their own leg beyond the set's fixture, bytes to kilobytes
([e2e.md](e2e.md#what-a-new-format-adds-to-the-tests) has the table); the journey about 6 s in the set `access-long` (5.9 s measured, the npm test upstream and one package of a few hundred bytes); the integration cases
about 2 s each (`ociremote` gains two subtests, `debremote` and `npm` one case each). No leg loads another format's client.

The developer pages this change touches: [mirror.md](mirror.md) (the scope `remote.mirror.include` and the rules in the pass's admission,
the engine of the role `api`), [coordination.md](coordination.md) (the queue `downloads` and the two tasks), [deb.md](deb.md) (the parse
of a by-hash index); [formats.md](formats.md) and [proxy.md](proxy.md) held what R2, R3 and R4 changed already.

## What was not verified

- The migration is 00114 (00113 is the version list keyset of another package, which lands first); a package that adds a migration takes the next free one when it lands.
- Docker Hub's search, Quay's and GitLab's APIs and npm's replication host were read live once on 2026-11-23; their answers' fields are
  observed, not specified by their operators (R-OCI-038, upstream research).
- Whether a GitLab registry credential is accepted by the GitLab REST API was not tried; the catalogue sends none there.
