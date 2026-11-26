# Delivery by signed links of the object store

Status: specified on 2026-11-22; packages 1 and 2 built on 2026-11-22, package 3 on 2026-11-23: all three are built ([storage.md](../spec/storage.md) R-STOR-014, R-STOR-070 to R-STOR-074;
[usage-and-limits.md](../spec/usage-and-limits.md) R-LIM-040; [api-and-ui.md](../spec/api-and-ui.md) R-UI-200, R-UI-201;
[security/download-links.md](../security/download-links.md)). This page is the build plan: the three packages, what each owns, the order,
the tests and what they cost.

## What has to be in your head first

- **One serve point.** Every stored item a format serves passes `itemStore.Serve` ([ingest.go](../../internal/formathost/ingest.go)) and
  `edge.ServeItem` ([content.go](../../internal/api/edge/content.go)). The link is one more branch there, after the gates and `Admit` and
  instead of `open`; nothing else in the download path changes. `/files/` never gets the branch.
- **Streaming is the fallback of every step.** A condition that does not hold leaves the download as it is today; only `304`, `412`,
  a gate's refusal and `Admit`'s `429` answer by themselves, as they do now.
- **The format says whether its clients check**, per route (`Route.ClientDigest`); the zero value streams, so nothing changes for a format
  that says nothing.
- **Signing is local.** `ObjectStore.Presign` makes no request; the guard lets it bypass the breaker
  ([guard.go](../../internal/blob/guard.go)), so the decision asks the breaker itself.
- **Several connections are built** ([blob-store.md](blob-store.md)): a blob has copies on one to eight connections, and the item lookup
  reads them (`items.Item.Copies`, Q-STOR-275). The connection choice of R-STOR-074 filters those copies (`blob.Connections.ServableOf`), so
  a download by link costs the lookup alone, as a stream does (R-ARCH-027).

## Work packages

| # | Package | Owns (directories) | Needs |
|---|---|---|---|
| 1 | The format contract's declaration and kit check 20 | `internal/formats/` (`route.go`: `ClientDigest`; `formats.go`: `Register`'s refusals; `testdata/contract.txt`), `internal/formats/formattest/` (`checks_links.go`, the defects in `testformat/`), the declarations in `internal/formats/oci/routes.go`, `npm/routes.go`, `ansible/routes.go`, the table test in `internal/formats/all/all_test.go`; the Go block of R-ARCH-008 | nothing |
| 2 | The serve point, the store and the limits | `internal/blob/` (`Presign` with options and the link's expiry, the in-memory store's signing, the link check in `conformance.go`, run per connection at start), a new domain package `internal/downloadlinks/` (the row's store and the audit event `storage.download_links.changed`, the replica's snapshot, the host rule, "can serve links" per connection, the metrics), `internal/formathost/` (`links.go` and the branch in `ingest.go`), `internal/api/edge/` (`ServeOptions.Link`, the `307`), `internal/limits/` (the charge of R-LIM-040; no pace check since Q-LIM-99), `internal/items/` (`ServedEvent` with the link's fields), `internal/repository/` (the closed member `storage`, `downloadLinks`, `not_applicable`), the migration and statements under `internal/meta/postgres/`, the change area in `internal/cluster/areas.go`, the request log's `delivery`, the wiring in `internal/app/`; the integration case in `test/integration/blob/`, the end-to-end cases in `test/e2e/` | package 1 (it reads `rq.Route.ClientDigest`) |
| 3 | The API, the settings page and the repository's opt-out on the screen | `internal/api/v1/download_links_api.go`, `api/openapi.yaml` and what `make generate` writes from it, `web/src/app/features/settings/` (the page `/admin/settings/storage` and its route), `web/src/app/features/repositories/` (the section **Download links**), `web/src/locale/`, the manual's pages of the administrator and of a project Owner | package 2's store and repository member; the screens can be built beside package 2 against the answers R-STOR-014 specifies |

The handover per blob to a Bastion builds on package 2: its decision is a file of `internal/downloadlinks` beside the links', and it shares
the public addresses, the host rule, the link check (whose two parts package 2 records apart) and the signing
([bastion-handover.md](bastion-handover.md), [storage.md](../spec/storage.md) R-STOR-095, R-STOR-096).

Package 1 is small and lands first; it is built: `formats.Route.ClientDigest` and `formats.ClientDigest`
([route.go](../../internal/formats/route.go)), `checkClientDigest` in `Validate`, kit check 20 (`formattest.CheckLinks`), the declarations of
`oci.blob.get`, `npm.tarball.get` and `ansible.artifact.get`, and the table test; package 2's serve point reads the declaration, and its npm
case turned `npm.tarball.get` back ([Q-STOR-354](../spec/storage.md#q-stor-354--yarn-4-installs-a-tarball-whose-object-was-altered-so-npms-tarball-route-declares-nothing)). A package that needs
the routes whose clients check (the settings' `routes`, the repository's `not_applicable`) walks `formats.List()` and each format's
`Routes()` for `ClientDigest.Checks`. Package 2 is the whole feature behind the API; package 3 makes it reachable. The directories do not
overlap but for the wiring: package 3 added `downloadLinksAPI` to `internal/app/` and mounts its handler through the router the API already has.

## Package 2 as built

Built on 2026-11-22. What each file does:

| File | Responsibility |
|---|---|
| [blob/objects.go](../../internal/blob/objects.go) | `ObjectStore.Presign(ctx, key, PresignOptions) (Link, error)`: the life, the three overrides, the address; `LinkLife` (the credential's bound, `MinCredentialLife` 5 s, `ErrCredentialExpiring`) |
| [blob/linkcheck.go](../../internal/blob/linkcheck.go) | `CheckLink` and `LinkCheck` with its two parts (`Works`, `Overrides`); the conformance check `checkPresign` runs the same body |
| [blob/connections.go](../../internal/blob/connections.go), [copies.go](../../internal/blob/copies.go), [guard.go](../../internal/blob/guard.go) | a connection's `LinkCheck`, guarded `Presign` and `HTTPClient`; `Connections.ServableOf` over copies read already; the guard forwards `HTTPClient` |
| [blob/s3/client.go](../../internal/blob/s3/client.go) | `Presign`: the credential retrieved once and signed with, the overrides in the signed query, `BaseEndpoint` the public address |
| [blob/blobtest/presign.go](../../internal/blob/blobtest/presign.go) | the in-memory store's links: signed with a key of its own, answered by its `RoundTrip` (403 for a bad signature or an expiry, 400 with a credential, the overrides unless `IgnoreOverride`), `SetCredentialExpiry` |
| [downloadlinks/settings.go](../../internal/downloadlinks/settings.go) | `Settings`, `Defaults`, `Patch`, `Validate` (what package 3's `PATCH` refuses with `422`), `ParseAddress`, `CheckAddress`, `LinkHost` |
| [downloadlinks/hosts.go](../../internal/downloadlinks/hosts.go) | `HostRule` from `server.*` and the endpoints, `Refuses` |
| [downloadlinks/store.go](../../internal/downloadlinks/store.go) | the row: `Settings`, `Update` (stale is `412`, the audit event with `before` and `after`, the journal entry when switched off, the change area); the replay of a switch turned off |
| [downloadlinks/links.go](../../internal/downloadlinks/links.go) | `Links`: the snapshot, each connection's `ConnectionState` (`Usable`, `Reasons`, `HandoverUsable`, `HandoverReasons`), the gauge, `RunLinkChecks`, `Sign` (steps 12 and 13) |
| [downloadlinks/snapshot.go](../../internal/downloadlinks/snapshot.go) | the component: the row at start and on every change of the area, the link checks at start, when the switch turns on and while unanswered (Q-STOR-351) |
| [downloadlinks/metrics.go](../../internal/downloadlinks/metrics.go) | the four families of R-STOR-073 and the declined reasons |
| [formathost/download_links.go](../../internal/formathost/download_links.go) | `itemStore.linkOptions`: steps 1 to 6, 8 and 9 (step 10 is no condition, Q-LIM-99), `ServeOptions.Link` and `Linked` (the counting and the charge of step 14) |
| [signatures/gate/serve.go](../../internal/signatures/gate/serve.go) | `Serve.RunDecided`: whether every gate passed on a decided state at its first decision (Q-STOR-350); a writer's read is recorded there, once, link or stream (Q-SIG-240) |
| [api/edge/content.go](../../internal/api/edge/content.go) | `ServeOptions.Link` after `Admit` for a `GET` answered `200`, the `307` (`writeRedirect`), `Linked` |
| [limits/links.go](../../internal/limits/links.go) | `Charge.ChargeLink`, the charge at the issue; no figure of the limits is a condition of a link (Q-LIM-99) |
| [repository/storage.go](../../internal/repository/storage.go) | the closed `storage`, `downloadLinks` and its `not_applicable`; `Repository.DownloadLinks`; the narrowing part `downloadLinks` and its replay |
| [formats/formats.go](../../internal/formats/formats.go), [catalog.go](../../internal/formats/catalog/catalog.go) | `catalog.Entry.ClientDigest`, derived by `Register` from the routes, which the repository model reads |
| [items/stats.go](../../internal/items/stats.go), [audit/events_files.go](../../internal/audit/events_files.go), [events_storage.go](../../internal/audit/events_storage.go) | `ServedEvent.Delivery` and `LinkExpiresAt` in `artifact.downloaded`; `storage.download_links.changed` |
| [observability/requestlog.go](../../internal/observability/requestlog.go) | the request log's `delivery` (`RequestInfo.SetDelivery`) |
| [meta/postgres/migrations/00108_download_links.sql](../../internal/meta/postgres/migrations/00108_download_links.sql), [queries/download_links.sql](../../internal/meta/postgres/queries/download_links.sql) | the row and the change area's epoch; the four statements |
| [cluster/areas.go](../../internal/cluster/areas.go) | `AreaDownloadLinkSettings` |
| [app/wire_downloadlinks.go](../../internal/app/wire_downloadlinks.go) | the step `download_links` on every role with a client listener; the format host takes its `Links` |
| [test/e2e/cmd/driver/links.go](../../test/e2e/cmd/driver/links.go) | `driver links on\|off` and `driver overwrite <sha256>` for the end-to-end cases |

What package 3 finds: `downloadLinksArea{store, links}` in the wiring (`part[downloadLinksArea](a.parts)`); the `GET` of the settings is
`store.Settings` with `links.States()` for `connections` and `formats.List()` with each route's `ClientDigest.Checks` for `routes`; the
`PATCH` is `store.Settings`, `Settings.Apply(Patch)`, `downloadlinks.Validate(next, links.ConnectionNames(), links.Rule())` and
`store.Update(actor, next, version)`, whose stale revision is `errs.Stale` (`412`); the switch's journal flag and the change area are the
store's. The test link writes its probe through the connection (`blob.Connections.Get`) and signs with `Connection.Presign` and
`PresignOptions{Address: <entered>}`. The `GET /api/v1/download-links/switch` reads `links.Settings().Enabled` and the catalog's
`ClientDigest`.

What the handover's builder finds: the public addresses (`Settings.PublicAddresses`), the host rule (`HostRule`, `LinkHost`), the signing
(`Connection.Presign` with `PresignOptions` without overrides), the link check's first part (`LinkCheck.Works`) and the decided
`ConnectionState.HandoverUsable` with its reasons; the serve point takes a second option beside `ServeOptions.Link`.

**The handover beside the links** (package 2 of [bastion-handover.md](bastion-handover.md), built on 2026-11-23): the serve point's second
option, decided after the gates and `Admit` as a link is, and only for a recognized forward of a Bastion that offered one.

| File | Responsibility |
|---|---|
| [downloadlinks/handover.go](../../internal/downloadlinks/handover.go) | `Links.HandoverOptions`: step 1 of R-STOR-095 (the `Recognition`'s offer, the registration's switch from `Options.Handovers`), the serve point's steps 2 and 3 as `HandoverDownload.Artifact` and `Decided`, steps 5, 7 (`handoverCopy`: plain copies first, then the first encrypted one), 9 and, for an encrypted copy, 8 (`dataKey`, through `Options.DataKeys`) in `handover`, step 10 (`trace`: `bastion.data_key.handed` through `Options.Runner` and `Options.Audit` before the answer is noted), the counting of step 11 (`ChargeLink`, `Record` with `Delivery: "handover"`, the request log's `delivery`, the metric of the kind `link` or `link_and_key`); the closed reasons of the declined offers; `KeysHeld` |
| [downloadlinks/metrics.go](../../internal/downloadlinks/metrics.go) | `quader_storage_download_handovers_total{bastion, kind}` and `quader_storage_download_handovers_declined_total{reason}` beside the links' families |
| [downloadlinks/snapshot.go](../../internal/downloadlinks/snapshot.go) | `Snapshot.CheckLinks`: the link checks again when a registration's handover was turned on (R-STOR-096) |
| [formathost/download_links.go](../../internal/formathost/download_links.go) | `itemStore.handoverOptions`: a route of the kind `Download`, never a system read, the gates' `decided`; it does not read `ClientDigest` |
| [api/rawfiles/handler.go](../../internal/api/rawfiles/handler.go), [files/download.go](../../internal/files/download.go) | every file of `/files/` (`Stat` reports `Decided` from `RunDecided`), `Service.HandedOver`; `/files/` still never gets a client's link (`TestR_STOR_070_FilesIsNeverALink`) |
| [api/edge/content.go](../../internal/api/edge/content.go) | `ServeOptions.Handover` before `Link`: the file's `200` with its headers and `Content-Length: 0`, the object not opened, then `HandedOver` |
| [bastionmain/absence.go](../../internal/bastionmain/absence.go), [api/bastionmark/mark.go](../../internal/api/bastionmark/mark.go) | `NoteHandover` and `MarksHandover`; the marking writes the class `handover` and `Quader-Bastion-Handover` from the noted value |
| [bastionmain/snapshot.go](../../internal/bastionmain/snapshot.go), [handover.go](../../internal/bastionmain/handover.go) | `RegistrationSnapshot`, the snapshot of the registrations (whose handover is on, and since E13 the networks each Bastion connects from), and the replay of `bastion.handover.disabled` |

## Package 3 as built

Built on 2026-11-23. What each file does:

| File | Responsibility |
|---|---|
| [api/v1/download_links_api.go](../../internal/api/v1/download_links_api.go) | The four operations: `GET`/`PATCH …/settings` (the row through `DownloadLinkStore`, the states of `Links.States()`, `routes` from `formats.List()`), `POST …/test`, `GET …/switch` (the snapshot's `enabled` and the formats of the routes); the `422` of `downloadlinks.Validate` at its pointers, the `409 connection_unavailable` of a test |
| [downloadlinks/testlink.go](../../internal/downloadlinks/testlink.go) | `Links.TestLink`: the address checked as a save checks it, the probe `_probe/link-test-<random>` written through the connection's store, the link with its overrides against the typed address, the probe's deletion when the link's life is over, the sweep of probes older than an hour (Q-STOR-356) |
| [errs/codes.go](../../internal/errs/codes.go) | `connection_unavailable` (`409`) |
| [app/wire_downloadlinks.go](../../internal/app/wire_downloadlinks.go), [app.go](../../internal/app/app.go) | `downloadLinksAPI`: the area's store and replica side for the operations; a role without the area answers them `503` |
| [api/openapi.yaml](../../api/openapi.yaml) | The tag `download-links`, the four operations, `DownloadLinkSettings` and its parts, and `RepositoryStorageSettings`, the closed member `storage` of a repository document |
| [web/…/download-links-api.ts](../../web/src/app/api/download-links-api.ts), [core/download-links-switch.ts](../../web/src/app/core/download-links-switch.ts) | The client of the operations; the switch read once per page load for the repository's section |
| [settings/storage/download-links-section.ts](../../web/src/app/features/settings/storage/download-links-section.ts), [.html](../../web/src/app/features/settings/storage/download-links-section.html) | **Admin area › Settings › Storage** (R-UI-200): the status, the connections that can serve links, the switch, **Details** with the fields, the test link and the routes; the merge patch of the changed members |
| [repositories/repository-download-links.ts](../../web/src/app/features/repositories/repository-download-links.ts) | The repository's section **Download links** (R-UI-201, Q-UI-956); its save is `saveDownloadLinks` of [repository-detail.ts](../../web/src/app/features/repositories/repository-detail.ts) |
| [manual/admin/download-links.md](../manual/admin/download-links.md) | The administrator's page and the Owner's opt-out, with what a link means for a client behind a proxy or a firewall |

The integration case of area `blob` switches on and off through `PATCH` on the first replica (the `428` and an auditor's `403` first), so
the store's change area, the snapshot and the serve point are reached through the operation an administrator uses.

## The decision, step by step

R-STOR-070 lists fourteen steps. Where each is answered:

| Steps | Where | Reads |
|---|---|---|
| 1 route, 2 Bastion, 3 system read | `formathost/links.go` | `rq.Route.ClientDigest`, `bastionmain.RecognitionFrom`, `systemRead` |
| 4 switch and opt-out | `formathost/links.go` | the snapshot of `downloadlinks`, the repository snapshot (addressed and holder) |
| 5 content, 6 gates | `ingest.go`, after `serveChecks` | the item row: content state, `row.Malware`, the block, the signature gate's outcome |
| 7 conditionals, 8 range, 11 `Admit` | `edge.ServeItem`, as today | the request, the row |
| 9 size, 10 pace (no condition since Q-LIM-99) | `formathost/download_links.go` | the row's size |
| 12 connection, 13 credential and signing | `downloadlinks` | the connections' breakers and copies, the settings, the credential's expiry |
| 14 issue | `edge.ServeItem` (the `307`), `formathost` (`Served`, `AddCached`, metrics) | |

`edge.ServeItem` calls `ServeOptions.Link` after `Admit` and only for a `GET` that would carry the whole body; `Link` answers the link or
"stream". Steps 1 to 6, 9 and 10 are computed before, in `formathost`, so that `Link` is nil for every download that cannot be one.

## Tests and what they cost

By the rule "Tests stay runnable" of the repository's working rules: every behaviour once, at the cheapest tier; nothing that grows with
the number of formats outside a format's own leg; bytes to kilobytes. Every row is measured.

| Tier | What | Package | Time | Data |
|---|---|---|---|---|
| Unit | `Register`'s refusals, kit check 20 with its two defects, the table of declarations | 1 | measured in the unit job's mode (race detector, atomic coverage): under 0.01 s, 0.64 s, 0.02 s; check 20 adds 0.07 s to npm's kit, 0.04 s to Ansible Galaxy's and under 0.01 s to every other format's | none: the test format's and the formats' own seeds of bytes |
| Unit | `Presign` with options and the credential's bound, the in-memory store's links, the link check, the host rule, the conditions table of R-STOR-070 with the in-memory store, the `307`, the charge (a caller with a speed limit gets a link, Q-LIM-99), the repository member, what a link counts | 2 | measured in the unit job's mode (race detector, atomic coverage): 0.17 s for the fourteen tests together, the conditions table 0.07 s and the S3 adapter's link 0.06 s, the others under 0.01 s each | bytes: objects of 2 KiB and less |
| Conformance | `checkPresign` asks for the three overrides and checks them (job `s3-conformance`, against Silo) | 2 | measured against the stack's SeaweedFS: 0.01 s | 2 KiB |
| Integration | area `blob`: the switch through the store reaches another replica within 2 s with Valkey stopped (measured 0.8 s); a link of an OCI blob fetched by a plain client from the stack's store; area `querybudget`: a download by link costs one statement | 2 | measured: 4.2 s for the `blob` case (two replicas started; the switch reached the other in 0.85 s), 8.0 s for the `querybudget` case (a third replica started, the blob uploaded) | one blob of 40 bytes |
| End-to-end | one case in each leg of a format that declares (`oci`: Docker and Podman; `ansible`: `ansible-galaxy` of both images): a fresh one-file artifact through a `307`, then its object altered in the bucket (a gzip stream's header time flipped) and refused by every client | 2 | measured once on a kind cluster on 2026-11-22: 36 s for the OCI case, 54 s for the Ansible case (most of it the wait for the first `307` and the client pods' commands) | kilobytes |
| Unit (API) | the operations, `If-Match`, `403`, `422` at the pointers, the reasons, the test link and its `409` (`TestR_STOR_014_TheOperations`, `TestR_STOR_074_TheConnectionAndItsAddress`), and the test link's probe, life and sweep (`TestR_STOR_074_TheTestLink`) | 3 | measured in the unit job's mode: 0.11 s and 0.04 s of their own (the shared parse of the document, about 7 s, is paid once per test binary by whichever test asks first), under 0.01 s | bytes: the probe's 44 |
| Integration | area `blob`: the case of R-STOR-014 switches on and off through `PATCH`, after a `428` and an auditor's `403` | 3 | measured: the package 5.5 s, as before (three requests more) | one blob of 40 bytes |
| Web unit | the page (12 cases) and the repository's section (6 cases) (Vitest) | 3 | measured: 0.24 s and 0.25 s | none |

What each pipeline job gains: the unit job about 3 s (package 1's share measured: about 0.8 s; package 2's 0.17 s of tests and the
compile of one package more; package 3's 0.16 s); the web unit job about 0.5 s (package 3); the job `ui-layout` the two screens' pictures
(measured on a stack of the package's own with two browsers: `admin-settings-storage` 55 s as the administrator and 2 s as the auditor,
`repository-settings-links` 39 s each as the Owner and the administrator, about 1.1 minutes of the job's wall time with its three
browsers); `s3-conformance` under a second; the integration shard that holds
`blob` about 4 s, the one that holds `querybudget` about 8 s; `e2e (oci)` about 36 s and `e2e (ansible)` about 54 s; every other leg
nothing. A change of the serve point is shared code: it starts the npm leg, which has no case of the links since npm declares nothing
(Q-STOR-354), so the clients' check of a link is shown by the OCI and Ansible legs when their format changes and in the nightly.

With 25 formats: a format that declares adds one case of the same kind to its own leg (15 to 30 s) and its kit's milliseconds; a format
that does not declare adds the kit's milliseconds only; no leg loads another format's clients, and the tests of the serve point stay the
same size.

## Not verified

- CRI-O and the Docker Engine's older pull path against an altered link: the OCI case runs the leg's Docker Engine and Podman.
- The npm case was run once and removed with npm's declaration (Q-STOR-354): it showed npm 12, pnpm 12 and Yarn 1 refusing an altered
  tarball and Yarn 4.18.1 installing it; its refusals took 71 s (npm) and 141 s (pnpm), the clients' retries.
- Anything against the owner's object store: whether its signed links honour the three overrides, how long they may live, whether they
  serve a range (its documentation is silent; the software behind it documents the overrides). The link check of every start answers it
  on the installation.
- The end-to-end legs in CI: the cases ran on a local kind cluster only, where the client pods of all three legs reached the development
  store `quader-s3.quader.svc:9000` and trusted its certificate.
