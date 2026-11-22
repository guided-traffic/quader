# Delivery by signed links of the object store

Status: specified on 2026-11-22; packages 1 and 2 built on 2026-11-22, package 3 not built ([storage.md](../spec/storage.md) R-STOR-014, R-STOR-070 to R-STOR-074;
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
overlap; `internal/app/` is package 2's alone, and package 3 mounts its handler through the router the API already has.

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
the number of formats outside a format's own leg; bytes to kilobytes. Measured where a row says so; package 3's rows are estimates.

| Tier | What | Package | Time | Data |
|---|---|---|---|---|
| Unit | `Register`'s refusals, kit check 20 with its two defects, the table of declarations | 1 | measured in the unit job's mode (race detector, atomic coverage): under 0.01 s, 0.64 s, 0.02 s; check 20 adds 0.07 s to npm's kit, 0.04 s to Ansible Galaxy's and under 0.01 s to every other format's | none: the test format's and the formats' own seeds of bytes |
| Unit | `Presign` with options and the credential's bound, the in-memory store's links, the link check, the host rule, the conditions table of R-STOR-070 with the in-memory store, the `307`, the charge (a caller with a speed limit gets a link, Q-LIM-99), the repository member, what a link counts | 2 | measured in the unit job's mode (race detector, atomic coverage): 0.17 s for the fourteen tests together, the conditions table 0.07 s and the S3 adapter's link 0.06 s, the others under 0.01 s each | bytes: objects of 2 KiB and less |
| Conformance | `checkPresign` asks for the three overrides and checks them (job `s3-conformance`, against Silo) | 2 | measured against the stack's SeaweedFS: 0.01 s | 2 KiB |
| Integration | area `blob`: the switch through the store reaches another replica within 2 s with Valkey stopped (measured 0.8 s); a link of an OCI blob fetched by a plain client from the stack's store; area `querybudget`: a download by link costs one statement | 2 | measured: 4.2 s for the `blob` case (two replicas started; the switch reached the other in 0.85 s), 8.0 s for the `querybudget` case (a third replica started, the blob uploaded) | one blob of 40 bytes |
| End-to-end | one case in each leg of a format that declares (`oci`: Docker and Podman; `ansible`: `ansible-galaxy` of both images): a fresh one-file artifact through a `307`, then its object altered in the bucket (a gzip stream's header time flipped) and refused by every client | 2 | measured once on a kind cluster on 2026-11-22: 36 s for the OCI case, 54 s for the Ansible case (most of it the wait for the first `307` and the client pods' commands) | kilobytes |
| Unit (API) | the operations, `If-Match`, the reasons, the test link | 3 | about 1 s | none |
| Web unit | the page and the repository's section (Vitest) | 3 | about 2 s | none |

What each pipeline job gains: the unit job about 3 s (package 1's share measured: about 0.8 s; package 2's 0.17 s of tests and the
compile of one package more); the web unit job about 2 s (package 3); `s3-conformance` under a second; the integration shard that holds
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
