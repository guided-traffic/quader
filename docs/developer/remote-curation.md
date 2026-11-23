# Curating a remote repository: the work packages

Status: specified on 2026-11-23; the contracts package (C), the delivery rules (R2) and the downloads ahead with their queue (R4) built on 2026-11-23 and 2026-11-24, the tab strip and the tab Downloads (R5a) on 2026-11-24, the rest not. The specification is [spec/remote-curation.md](../spec/remote-curation.md) with the format
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
| **R3 · Upstream catalogue** | R-CUR-007 to R-CUR-009: the catalogue engine (lent upstream that stores nothing, budget, cache, bounds, second hosts), the five formats' catalogues (kit check 22) and the check of `Register` that a remote adapter implements `formats.Catalogue` (Q-CUR-109), the Debian parse's descriptions, the API with the vulnerability verdicts of unstored versions | C | 2 |
| **R4 · Downloads ahead and the queue** | R-CUR-010 to R-CUR-013: the queue's store, dispatch, run, retries and failures in words; the probe's row; bulks and their expansion; the API; the client failure rows; the charge; a fill without a client is no refused download (Q-CUR-22) | C (the expansion calls the catalogue's interface; its integration case of a bulk lands with whichever of R3 and R4 lands second) | 2 |
| **R2b · Requests, examination, retention** | R-CUR-014 to R-CUR-016: requests and askers, the examination a request starts, allow and decline, the list with client refusals, discard and the retention task, their events, journal and replay | R2, R4 | 1.5 |
| **R5a · The tab strip and the tab Downloads** | R-CUR-018: the strip on a remote's page with the three addresses, the tab Downloads with its states, filters, failure detail, retry and cancel | R4 | 1 |
| **R5b · The tab Upstream and the settings** | R-CUR-018: search, levels, lookup, a package's versions with states and verdicts, the actions, the preview of a bulk, the rule list, the requests panel, the settings section Delivery, the glossary rows | R2, R3, R2b, R5a | 2 |
| **R6 · Clients and documents** | the end-to-end case per format in its own leg; the manual's pages (the tabs, curation, the queue); the developer pages this change touches (formats, proxy, mirror) | R5b | 1 |

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
the web's unit tests of the forms and the status (Vitest, milliseconds each).

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

## R4 · Downloads ahead and the queue

Built on 2026-11-23 (R-CUR-010 to R-CUR-013, Q-CUR-80 to Q-CUR-85). No migration: the tables are 00114's. What it holds:

| File or directory | What |
|---|---|
| [internal/remotecuration/queue](../../internal/remotecuration/queue/queue.go) | `queue.go` the row, the bulk, the settings, the result of a run, the errors (`FullError`, `QueuedError`); `service.go` the service: `Add` (a row, started at once when its repository has a free place), `Start`, `Claim` and `Finish` of a run, `Retry`, `RetryMatching`, `Cancel`, `Queued` and `Active` (`remotecuration.Queue`); `decide.go` the backoff and the end of an attempt (`settle`), pure; `bulk.go` the bulk, its expansion through the `Planner` port and its bounds; `dispatch.go` the task `remote-downloads-dispatch` (orphans, due repositories, one step of every expanding bulk, the bulks' ends, retention, the gauge); `client.go` the rows of clients' failed fetches with their throttle; `metrics.go` the three families of R-CUR-019; `store.go` the ports `Store`, `Tx`, `Reader`, `Maintenance`; `store_pg.go` PostgreSQL |
| [remote_downloads.sql](../../internal/meta/postgres/queries/remote_downloads.sql) | every statement of the queue; a row and a bulk cross the boundary as `to_jsonb` text, decoded by one function each; the repository's row `FOR NO KEY UPDATE` before every start (Q-CUR-82); `RemoteDownloadDebDirs` for Debian's `AnyIndex` scope |
| [background_download.go](../../internal/formathost/background_download.go) | `MirrorHost.RunDownload`, the run of every row: what has nothing to do (`skipped`), the charge (`limits.Engine.Resume`), the plan (the row's probe, else `formats.VersionProber`), `ProbeContent`, the fills in the run's window, the refusals counted and kept, the failure in words (`proxy.Describe`); `MirrorHost.Versions` and `Packages`, the planner of a bulk over the format's mirror plan (sections answer `discover.ErrNotOffered` until R3) |
| [probe.go](../../internal/formathost/probe.go) | the decision on a probe inserts a row of origin `probe` through the port `Downloads` (`Queued`, `AddProbe`) and counts through `ProbeCounter` |
| `version_probe.go` of [npm](../../internal/formats/npm/version_probe.go), [nuget](../../internal/formats/nuget/version_probe.go), [oci](../../internal/formats/oci/version_probe.go), [deb](../../internal/formats/deb/version_probe.go), [ansible](../../internal/formats/ansible/version_probe.go) | `formats.VersionProber`: npm, NuGet and Debian from their mirror plan's rows (`formats.ProbeFromPlan`), OCI the tag's manifest by its digest, Ansible Galaxy the archive after its version document; Debian looks in every verified suite and parsed binary index (`rows.MirrorScope.AnyIndex`, [mirror_store.go](../../internal/formatdata/deb/mirror_store.go)) |
| [catalogue.go](../../internal/formats/catalogue.go), [remote.go](../../internal/formats/remote.go) | `VersionProber`, embedded by `Catalogue`; `RemoteCaps.VersionProbe` |
| [failure.go](../../internal/proxy/failure.go), [classify.go](../../internal/proxy/classify.go), [fetch.go](../../internal/proxy/fetch.go) | `proxy.Exchange` (the step, the address without its query, the status, the excerpt, the Retry-After) as the cause of the engine's error; `Describe`; `ClientFailures` and the leader's record of a client's failure (Q-CUR-84) |
| [fill.go](../../internal/proxy/fill.go), [enforce.go](../../internal/vuln/enforce/enforce.go) | `Miss.NoClient`: a fill without a client refused by the miss gate counts no refused download and writes no event (Q-CUR-22) |
| [background.go](../../internal/limits/background.go) | `Engine.StartDownload`: step 9a of a person's download of one version, no request counted (Q-CUR-85) |
| [access.go](../../internal/api/edge/access.go) | `Access.MayPopulateAct`: the populate check of an explicit act, without the session cookie's refusal (Q-CUR-27) |
| [remote_downloads_api.go](../../internal/api/v1/remote_downloads_api.go), `api/openapi.yaml` (`# >>> T-24 downloads`) | the operations of R-CUR-010 and R-CUR-012 and who may call them (R-CUR-005) |
| [wire_remote_downloads.go](../../internal/app/wire_remote_downloads.go) | the service (built at its first use), the kind `remote-download` (queue `downloads`, one attempt, exclusive per row, 30 minutes), the task `remote-downloads-dispatch` (queue `maintenance`), the ports of the format host and the remote engine, the API's services; it replaces `wire_background_download.go` |

The run's end: a cancelled job ends its row `cancelled`; a job that reached its 30 minutes ends its attempt `bound` (Q-CUR-83); a job
released at shutdown or whose lease was lost leaves its row `running` with its job, which runs again or which the dispatch finds orphaned.
The delivery rules' decider is `remotecuration.DeliverAll` in the API's services until R2 wires its own: who may start a version and the
row's member `delivery` read it.

Not built here, and why: a sub-section's bulk and the counts of a package's preview wait for the catalogue (R3, Q-CUR-80); the tab
**Downloads** is R5a's; the examination's rows (`remote_examinations`) and the request's examination are R2b's.

Tests and what they cost (each run once, R-CUR-022):

| Test | Tier | Run time | Data |
|---|---|---|---|
| `TestR_CUR_011_TheFailureOfAnExchangeInPreciseWords` (`internal/proxy`) | unit | about 20 ms | answers of an `httptest` server of a few hundred bytes, a closed port, a resolver that answers nothing, an untrusted certificate |
| the five tests of [queue_test.go](../../internal/remotecuration/queue/queue_test.go) (order and bound, waiting bound, backoff, retries, client throttle, bulks) | unit | milliseconds | none (a store in memory) |
| `TestR_CUR_010_APersonsDownloadOfOneVersionIsChargedToThePerson` (`internal/limits`) | unit | milliseconds | none |
| `TestR_CUR_014_AFillWithoutAClientIsNoRefusedDownload` (`internal/vuln/enforce`) | unit | milliseconds | none |
| `TestR_CUR_010_ADownloadAheadOfOneVersionAndOfAPackage`, `TestR_CUR_011_AFailureInItsOwnWordsAndTheRetries` (area `npm`) | integration | about 4 s each | the stub's corpus: one package of three versions |
| `TestR_CUR_010_…` of the areas `nuget`, `ociremote`, `debremote`, `ansibleremote` | integration | about 3 s each | one version of each stub's corpus |
| the existing `TestR_REPO_065_*`, `TestQ_REPO_367_*` against the row, one more assertion of the row's counters | integration | unchanged | unchanged |

What a leg gains: the unit job well under a second; the integration shards about 20 s together (npm 8 s, four formats 3 s each). A new
format adds its `version_probe.go` and one `TestR_CUR_010_…` of about 3 s in its own area.

## R2b · Requests, examination, retention

| Area | Files |
|---|---|
| Requests | `internal/remotecuration/requests/` (ask, the examination it starts, allow, decline, withdraw, the list) |
| Retention | `internal/remotecuration/retention/` (discard, the task `remote-examination-cleanup`), its journal entry and replay in `internal/journal/` |
| The API | `internal/api/v1/delivery_requests_api.go`, `api/openapi.yaml` (`# >>> T-24 requests`) |

Tests: integration, `TestR_CUR_014_…`, `TestR_CUR_015_…`, `TestR_CUR_016_…` (npm, about 9 s together, the stub's corpus and the test
`MAL-` record); unit, the replay of a discard (milliseconds). What a leg gains: the shards about 9 s.

## R5a and R5b · The screens

| Package | Files |
|---|---|
| R5a (built) | see the table below |
| R5b | `web/src/app/features/repositories/upstream/` (search, levels, package, preview dialog, rule list, requests panel), `web/src/app/api/upstream-catalogue-api.ts`, `delivery-rules-api.ts`, `delivery-requests-api.ts`, the section **Delivery** of the settings page (`repository-detail.*`, `settings-sections.ts`), `web/src/locale/GLOSSARY.md` and the messages |

R5a, built on 2026-11-24 (R-CUR-018, Q-CUR-130 to Q-CUR-133):

| File | What |
|---|---|
| [repositories.routes.ts](../../web/src/app/features/repositories/repositories.routes.ts) | `…/repositories/{key}/downloads`: the content page with the route's `data.tab` `downloads`, titled **Downloads** |
| [repository-content.ts](../../web/src/app/features/repositories/content/repository-content.ts), `.html` | the input `tab`; the strip on a remote's page for a signed-in caller (`remoteTabs`); the view `downloads`; the address of a local or virtual repository's tab Downloads leads to its content; the repository's `cacheFillAction` for the tab |
| [remote-tabs.ts](../../web/src/app/features/repositories/content/remote-tabs.ts) | the strip: the content tab captioned as its list, **Downloads** with `· n failed`; it reads the counts once on the content tab, the tab Downloads passes its own |
| [downloads/](../../web/src/app/features/repositories/downloads/remote-downloads.ts) | `remote-downloads.ts`, `.html`, `.scss`: the head (states, **Failure**, **Retry all failed**, the pause), the bulk's line, the table, the opened row, the refresh; `download-words.ts`: the words of the states, outcomes, classes and steps and the failure's one line |
| [remote-downloads-api.ts](../../web/src/app/api/remote-downloads-api.ts) | `RemoteDownloadsApi` over R-CUR-012's operations (the start and the preview are R5b's), its provider in `providers.ts`, the fake in [fakes-remote-downloads.ts](../../web/src/app/api/testing/fakes-remote-downloads.ts) |
| [problem-messages-downloads.ts](../../web/src/app/core/http/problem-messages-downloads.ts) | the words of `not_retryable`, `already_queued`, `download_queue_full` |
| [downloads.ts](../../web/e2e/renders/downloads.ts), `screens.ts` | the renders: `repository-npm-remote-downloads` (the seed's queue, then a queue of every state answered in the browser, a failure opened, a bulk) and `repository-oci-probed-downloads` (a probe's row of the server's own) |

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

## R6 · Clients and documents

- One end-to-end case per format in its own leg (`e2e (npm)`, `e2e (nuget)`, `e2e (oci)`, `e2e (deb)`, `e2e (ansible)`), each what only
  its client shows (R-CUR-022): about 10 s per leg; no leg loads another format's client.
- The manual: a page on curating a remote (the tabs, the rules, requests, the queue), linked from the repository's help.
- The developer pages: [formats.md](formats.md) (contract version 11, the catalogue, the version canon), [proxy.md](proxy.md) (step 5a, the
  failure's step), [mirror.md](mirror.md) (the scope), [coordination.md](coordination.md) (the queue `downloads`, the two tasks).

## What was not verified

- The migration is 00114 (00113 is the version list keyset of another package, which lands first); a package that adds a migration takes the next free one when it lands.
- The kit check numbers 21 and 22 hold only if no other check lands first (R-ARCH-010 says so).
- Docker Hub's search, Quay's and GitLab's APIs and npm's replication host were read live once on 2026-11-23; their answers' fields are
  observed, not specified by their operators (R-OCI-038, upstream research).
- Whether a GitLab registry credential is accepted by the GitLab REST API was not tried; the catalogue sends none there.
