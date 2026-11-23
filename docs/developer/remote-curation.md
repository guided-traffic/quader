# Curating a remote repository: the work packages

Status: specified on 2026-11-23, not built. The specification is [spec/remote-curation.md](../spec/remote-curation.md) with the format
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
- **The contract version rises to 8** with the catalogue's declaration and the version canon (R-CUR-007, R-ARCH-010); the written
  contract of R-ARCH-008 is held equal to the code by `TestR_ARCH_008_TheWrittenContractEqualsTheCode`, so the package that adds the
  interface adds it there in the same change.

## The packages

| Package | Delivers | Depends on | Builder-days (estimate) |
|---|---|---|---|
| **C · Contracts** | the migration of every table, the domain package's skeleton and its layer row, the catalogue's interface and types, the version canon, the descriptor's declaration and contract version 8 with every format's declaration (the floor: lookup), the error codes, the audit events' registrations, the configuration keys, the job queue `downloads` | — | 0.5 |
| **R2 · Delivery rules** | R-CUR-001 to R-CUR-006, R-CUR-017 (the mirror's scope), the rules' API; the refusal before a fill, at the serve point and in the five formats' documents (kit check 22); the virtual walk; the journal and the Bastion's feed; the listing marks; a remote without patterns | C | 2.5 |
| **R3 · Upstream catalogue** | R-CUR-007 to R-CUR-009: the catalogue engine (lent upstream that stores nothing, budget, cache, bounds, second hosts), the five formats' catalogues (kit check 21), the Debian parse's descriptions, the API with the vulnerability verdicts of unstored versions | C | 2 |
| **R4 · Downloads ahead and the queue** | R-CUR-010 to R-CUR-013: the queue's store, dispatch, run, retries and failures in words; the probe's row; bulks and their expansion; the API; the client failure rows; the charge; a fill without a client is no refused download (Q-CUR-22) | C (the expansion calls the catalogue's interface; its integration case of a bulk lands with whichever of R3 and R4 lands second) | 2 |
| **R2b · Requests, examination, retention** | R-CUR-014 to R-CUR-016: requests and askers, the examination a request starts, allow and decline, the list with client refusals, discard and the retention task, their events, journal and replay | R2, R4 | 1.5 |
| **R5a · The tab strip and the tab Downloads** | R-CUR-018: the strip on a remote's page with the three addresses, the tab Downloads with its states, filters, failure detail, retry and cancel | R4 | 1 |
| **R5b · The tab Upstream and the settings** | R-CUR-018: search, levels, lookup, a package's versions with states and verdicts, the actions, the preview of a bulk, the rule list, the requests panel, the settings section Delivery, the glossary rows | R2, R3, R2b, R5a | 2 |
| **R6 · Clients and documents** | the end-to-end case per format in its own leg; the manual's pages (the tabs, curation, the queue); the developer pages this change touches (formats, proxy, mirror) | R5b | 1 |

**Who works at once** (at most four builders): C alone first (half a day); then R2, R3 and R4 side by side; then R2b and R5a side by side
(and whichever of the three is still open); then R5b; then R6. A research or a planner does not take a builder's place.

## C · Contracts

Owns (and nobody else writes them while C is open):

| File or directory | What |
|---|---|
| `internal/meta/postgres/migrations/<next>_remote_curation.sql` | `remote_delivery_rules`, `repositories.delivery_rules_revision`, `remote_delivery_requests`, `remote_delivery_request_askers`, `remote_download_bulks`, `remote_download`, `remote_examinations`, `deb_remote_descriptions` and the two columns `provides` and `description` of `deb_remote_entries`, with the indexes of the spec; export class `data` in the table registry (R-BAK-004) |
| `internal/remotecuration/doc.go`, `contracts.go` | the domain package and its ports: `rules.Decider` (`Decide(repo, name, version)`, `Package(repo, name)`), `queue.Lookup`, `requests.Lookup`, each with a stub that answers "delivered", "none queued" and "no request", so R3 and R4 can show states before R2 and R2b land |
| `internal/archtest` (the layer rows) | `internal/remotecuration/...` as a domain package beside `internal/mirror` |
| `internal/formats/catalogue.go` | the interface `Catalogue` and its types (R-CUR-007), `ErrNotOffered`; `RemoteCaps.Catalogue` found by `CapabilitiesOf` |
| `internal/formats/catalog/` | `VersionCanon` beside `Names`; `UpstreamPreset.CatalogueHosts` |
| `internal/formats/format.go`, `register.go` | `Descriptor.Catalogue` (the calls declared and the reasons), the checks of `Register`, `ContractVersion = 8` |
| every format's `format.go` with the class remote | its declaration (lookup, and reasons for the rest until R3), its version canon; `TestR_CUR_007_EveryRemoteFormatDeclaresItsCatalogue` in `internal/formats/all` |
| `docs/spec/architecture.md` R-ARCH-008's written contract | the interface and the descriptor member, as the test holds them |
| `internal/errs/codes.go` | `not_allowed_in_repository`, `too_many_rules`, `invalid_version`, `catalogue_not_offered`, `catalogue_not_found`, `catalogue_budget_exhausted`, `catalogue_answer_too_large`, `download_queue_full`, `already_queued`, `not_retryable`, `already_delivered`, `too_many_requests`, `delivered` |
| `internal/audit/events_remote_curation.go` | the events of R-CUR-019 with their schemas, modes and journal flags; `remote.examination.discarded` classified `none` in `internal/bastionmain/feed.go` and given its row in the table of R-HA-054 in `docs/spec/ha-and-helm.md` in the same change (`TestR_HA_054_EveryListedEventIsFlaggedAndEntryRequired` holds a listed event to its registration, so the row cannot precede the code) |
| `internal/config/remote_curation.go`, `jobs.go`, `maintenance.go` | the keys of R-CUR-020 with their defaults, the queue `downloads` and its workers, the two tasks' schedules |

Tests: unit only (`internal/formats`, `internal/formats/all`, `internal/audit`, `internal/config`, `internal/archtest`), milliseconds; the
migration is applied by every integration area's database, so the shards gain no test and about one second of migration.

## R2 · Delivery rules

| Area | Files |
|---|---|
| The rules: decision, compile, store, API | `internal/remotecuration/rules/`, `internal/api/v1/delivery_rules_api.go`, `api/openapi.yaml` (marked section `# >>> T-24 rules`) |
| The repository model | `internal/repository/` (`remote.delivery`, `remote.mirror.include`, the refusal of patterns on a remote, the rule set in the snapshot, the narrowing comparator's parts `rules` and `deliveryMode`) |
| Before a fill | `internal/proxy/artifact.go`, `metadata.go` (step 5a) |
| At the serve point | `internal/signatures/gate/serve.go` (the fourth check), `internal/formathost/` serve wiring |
| Documents | the filters of `internal/formats/npm`, `nuget`, `oci` (`PathsAllowed` joins the rules in `internal/formathost/host.go`), `ansible` (version list pages, `highest_version`); `internal/formats/deb` and `internal/formatdata/deb` (the virtual merge drops refused rows; a rules change enqueues `deb-index` of the virtual repositories) |
| The refusal's shape | each format's `errors.go` (`WriteError`), kit check 22 in `internal/formats/formattest` |
| The virtual walk | `internal/repository/routing.go`, `internal/formathost/virtual_remotes.go`, each format's virtual handlers where they end the walk |
| The listing | `internal/packages/view/` (no pattern hiding for a remote; `available` and `unavailableReason`), `internal/vuln/report/report.go` |
| The mirror | `internal/mirror/` (scope and rules, the fingerprint), `internal/formathost/mirror_host.go` (admission) |
| Journal and feed | `internal/journal/` (replay of the parts), `internal/bastionmain/feed.go` |
| The record of a refusal | the upsert into `remote_delivery_requests` (`refusals`) with the event's throttle, a writer in `internal/remotecuration/rules` |

Contracts to the others: `rules.Decider` (C's port, implemented here); the refusal's code and text; the listing's `delivery` state for R3
and the screens.

Tests (R-CUR-022): unit, the decision table and the comparator (about 1 s); kit check 22 for five formats (milliseconds each); integration,
one case per format in its area (`TestR_CUR_003_…`, about 2 s each, 10 s in all), plus `TestR_CUR_002_…`, `TestR_CUR_004_…`,
`TestR_CUR_005_…`, `TestR_CUR_006_…` and the mirror's `TestR_CUR_017_…` (about 10 s); the existing tests that set patterns on a remote
are rewritten to rules in the same change (their count is listed in the commit). Data: the stubs' existing corpora. What a leg gains: the
unit job about 1 s, the integration shards about 20 s.

## R3 · Upstream catalogue

| Area | Files |
|---|---|
| The engine | `internal/remotecuration/catalogue/` (the lent upstream that stores nothing, the budget on the cluster limiter, the cache, the bounds, the shape fallback) |
| The lent upstream | `internal/proxy/catalogue_upstream.go` (a `discover.Upstream` over the remote's client that answers `Document` from the store or the network without storing and refuses `Artifact`) |
| The formats | `internal/formats/{npm,nuget,oci,deb,ansible}/catalogue.go`; the presets' `CatalogueHosts` in `oci/format.go` and `npm/format.go`; the Debian parse in `internal/formatdata/deb/remote_store.go` (provides, short and long descriptions) |
| The verdicts of unstored versions | a port over `Evaluator.EvaluateMiss` in `internal/vuln/` |
| The API | `internal/api/v1/upstream_catalogue_api.go`, `api/openapi.yaml` (`# >>> T-24 catalogue`) |
| The kit | check 21 in `internal/formats/formattest` with each format's fixture of canned answers |

Tests: unit, the engine with a fake format and upstream (about 1 s); kit check 21 for five formats (milliseconds each; canned answers of a
few kilobytes in each format's fixture); integration, `TestR_CUR_008_…` and `TestR_CUR_009_…` (npm stub with a search route, about 5 s)
and the Debian search (`TestR_DEB_036_…`, about 3 s). What a leg gains: the unit job about 1 s, the shards about 8 s.

## R4 · Downloads ahead and the queue

| Area | Files |
|---|---|
| The queue | `internal/remotecuration/queue/` (store, order, dispatch task, retention, bulk expansion through the catalogue's interface) |
| The run | `internal/formathost/background_download.go` (generalized: a probe or `VersionProbe`, the failure in words, the row's counters), `internal/formathost/probe.go` (the row instead of the job), `internal/app/wire_remote_downloads.go` (replacing `wire_background_download.go`) |
| The failure's words | `internal/proxy/classify.go` (the step beside the code), the excerpt and the address without its query |
| A fill without a client | `internal/proxy/fill.go`, `internal/vuln/enforce/enforce.go` (no count and no event of a refused download, Q-CUR-22) |
| The client failure rows | a recorder in `internal/proxy` behind a port of `internal/remotecuration/queue` |
| The charge | `internal/limits` (the ticket of a person's operation) |
| The API | `internal/api/v1/remote_downloads_api.go`, `api/openapi.yaml` (`# >>> T-24 downloads`) |
| Metrics | `quader_remote_downloads_total`, `quader_remote_downloads`, `quader_remote_download_failures_total` (replacing `quader_remote_background_downloads_total`) |

Tests: unit, the classification table of about 30 failures from `httptest` and a fake resolver, the queue's order and retries (about 1 s);
integration, `TestR_CUR_010_…` (npm, about 3 s), one download ahead per other format (about 2 s each), `TestR_CUR_011_…` (failure and
retry, about 3 s), and the existing `TestR_REPO_065_*` and `TestQ_REPO_367_*` against the row. What a leg gains: the unit job about 1 s, the
shards about 15 s.

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
| R5a | `web/src/app/features/repositories/repositories.routes.ts` (the two addresses), the tab strip in the content page (`repository-content.*`), `web/src/app/features/repositories/downloads/`, `web/src/app/api/remote-downloads-api.ts`, the walk's screens in `web/e2e` |
| R5b | `web/src/app/features/repositories/upstream/` (search, levels, package, preview dialog, rule list, requests panel), `web/src/app/api/upstream-catalogue-api.ts`, `delivery-rules-api.ts`, `delivery-requests-api.ts`, the section **Delivery** of the settings page (`repository-detail.*`, `settings-sections.ts`), `web/src/locale/GLOSSARY.md` and the messages |

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
- The developer pages: [formats.md](formats.md) (contract version 8, the catalogue, the version canon), [proxy.md](proxy.md) (step 5a, the
  failure's step), [mirror.md](mirror.md) (the scope), [coordination.md](coordination.md) (the queue `downloads`, the two tasks).

## What was not verified

- No migration number is fixed: the next free one when C lands (00112 is the last on 2026-11-23).
- The kit check numbers 21 and 22 hold only if no other check lands first (R-ARCH-010 says so).
- Docker Hub's search, Quay's and GitLab's APIs and npm's replication host were read live once on 2026-11-23; their answers' fields are
  observed, not specified by their operators (R-OCI-038, upstream research).
- Whether a GitLab registry credential is accepted by the GitLab REST API was not tried; the catalogue sends none there.
