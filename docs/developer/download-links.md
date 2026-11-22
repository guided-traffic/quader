# Delivery by signed links of the object store

Status: specified on 2026-11-22; package 1 built on 2026-11-22, packages 2 and 3 not built ([storage.md](../spec/storage.md) R-STOR-014, R-STOR-070 to R-STOR-074;
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
- **Several connections are stage 1**, specified in parallel. Until it is built there is one connection, unencrypted, named `default` in
  the answers; write the connection choice of R-STOR-074 as a function of "the connections that hold the blob" so that stage 1 fills it.

## Work packages

| # | Package | Owns (directories) | Needs |
|---|---|---|---|
| 1 | The format contract's declaration and kit check 20 | `internal/formats/` (`route.go`: `ClientDigest`; `formats.go`: `Register`'s refusals; `testdata/contract.txt`), `internal/formats/formattest/` (`checks_links.go`, the defects in `testformat/`), the declarations in `internal/formats/oci/routes.go`, `npm/routes.go`, `ansible/routes.go`, the table test in `internal/formats/all/all_test.go`; the Go block of R-ARCH-008 | nothing |
| 2 | The serve point, the store and the limits | `internal/blob/` (`Presign` with options and the link's expiry, the in-memory store's signing, the link check in `conformance.go`, run per connection at start), a new domain package `internal/downloadlinks/` (the row's store and the audit event `storage.download_links.changed`, the replica's snapshot, the host rule, "can serve links" per connection, the metrics), `internal/formathost/` (`links.go` and the branch in `ingest.go`), `internal/api/edge/` (`ServeOptions.Link`, the `307`), `internal/limits/` (the pace check of R-LIM-040), `internal/items/` (`ServedEvent` with the link's fields), `internal/repository/` (the closed member `storage`, `downloadLinks`, `not_applicable`), the migration and statements under `internal/meta/postgres/`, the change area in `internal/cluster/areas.go`, the request log's `delivery`, the removal of `storage.s3.presign` from `internal/config/storage.go`, the wiring in `internal/app/`; the integration case in `test/integration/blob/`, the end-to-end cases in `test/e2e/` | package 1 (it reads `rq.Route.ClientDigest`) |
| 3 | The API, the settings page and the repository's opt-out on the screen | `internal/api/v1/download_links_api.go`, `api/openapi.yaml` and what `make generate` writes from it, `web/src/app/features/settings/` (the page `/admin/settings/storage` and its route), `web/src/app/features/repositories/` (the section **Download links**), `web/src/locale/`, the manual's pages of the administrator and of a project Owner | package 2's store and repository member; the screens can be built beside package 2 against the answers R-STOR-014 specifies |

The handover per blob to a Bastion builds on package 2: its decision is a file of `internal/downloadlinks` beside the links', and it shares
the public addresses, the host rule, the link check (whose two parts package 2 records apart) and the signing
([bastion-handover.md](bastion-handover.md), [storage.md](../spec/storage.md) R-STOR-095, R-STOR-096).

Package 1 is small and lands first; it is built: `formats.Route.ClientDigest` and `formats.ClientDigest`
([route.go](../../internal/formats/route.go)), `checkClientDigest` in `Validate`, kit check 20 (`formattest.CheckLinks`), the declarations of
`oci.blob.get`, `npm.tarball.get` and `ansible.artifact.get`, and the table test; nothing reads the declaration yet. A package that needs
the routes whose clients check (the settings' `routes`, the repository's `not_applicable`) walks `formats.List()` and each format's
`Routes()` for `ClientDigest.Checks`. Package 2 is the whole feature behind the API; package 3 makes it reachable. The directories do not
overlap; `internal/app/` is package 2's alone, and package 3 mounts its handler through the router the API already has.

## The decision, step by step

R-STOR-070 lists fourteen steps. Where each is answered:

| Steps | Where | Reads |
|---|---|---|
| 1 route, 2 Bastion, 3 system read | `formathost/links.go` | `rq.Route.ClientDigest`, `bastionmain.RecognitionFrom`, `systemRead` |
| 4 switch and opt-out | `formathost/links.go` | the snapshot of `downloadlinks`, the repository snapshot (addressed and holder) |
| 5 content, 6 gates | `ingest.go`, after `serveChecks` | the item row: content state, `row.Malware`, the block, the signature gate's outcome |
| 7 conditionals, 8 range, 11 `Admit` | `edge.ServeItem`, as today | the request, the row |
| 9 size, 10 pace | `formathost/links.go` | the row's size, `limits.FromContext(ctx).Figures()` |
| 12 connection, 13 credential and signing | `downloadlinks` | the connections' breakers and copies, the settings, the credential's expiry |
| 14 issue | `edge.ServeItem` (the `307`), `formathost` (`Served`, `AddCached`, metrics) | |

`edge.ServeItem` calls `ServeOptions.Link` after `Admit` and only for a `GET` that would carry the whole body; `Link` answers the link or
"stream". Steps 1 to 6, 9 and 10 are computed before, in `formathost`, so that `Link` is nil for every download that cannot be one.

## Tests and what they cost

By the rule "Tests stay runnable" of the repository's working rules: every behaviour once, at the cheapest tier; nothing that grows with
the number of formats outside a format's own leg; bytes to kilobytes. Estimates, not measured.

| Tier | What | Package | Time | Data |
|---|---|---|---|---|
| Unit | `Register`'s refusals, kit check 20 with its two defects, the table of declarations | 1 | measured in the unit job's mode (race detector, atomic coverage): under 0.01 s, 0.64 s, 0.02 s; check 20 adds 0.07 s to npm's kit, 0.04 s to Ansible Galaxy's and under 0.01 s to every other format's | none: the test format's and the formats' own seeds of bytes |
| Unit | `Presign` with options and the credential's bound, the in-memory store's links, the link check, the host rule, the conditions table of R-STOR-070 with the in-memory store, the `307`, the pace check and the charge, the repository member, what a link counts | 2 | about 2 s together | bytes |
| Conformance | `checkPresign` asks for the three overrides and checks them (job `s3-conformance`, against Silo) | 2 | milliseconds on the job's 35 to 38 s of tests | 2 KiB |
| Integration | area `blob`: the switch through the API reaches another replica within 2 s, Valkey stopped too; a link fetched by a plain client from the stack's store | 2 | about 3 s in the shard that holds `blob` | one tarball of kilobytes |
| End-to-end | one case in each leg of a format that declares (`oci`: Docker and Podman; `npm`: npm, pnpm, Yarn; `ansible`: `ansible-galaxy`): a fresh one-file artifact through a `307`, then its object altered in the bucket and refused by every client | 2 | about 30 s (`oci`), 20 s (`npm`), 15 s (`ansible`) | kilobytes |
| Unit (API) | the operations, `If-Match`, the reasons, the test link | 3 | about 1 s | none |
| Web unit | the page and the repository's section (Vitest) | 3 | about 2 s | none |

What each pipeline job gains: the unit job about 3 s (package 1's share measured: about 0.8 s); the web unit job about 2 s; `s3-conformance` under a second; the integration shard
that holds `blob` about 3 s; `e2e (oci)` about 30 s, `e2e (npm)` about 20 s, `e2e (ansible)` about 15 s; every other leg nothing. A change
of the serve point is shared code and starts the npm leg, whose case shows the link with a real client.

With 25 formats: a format that declares adds one case of the same kind to its own leg (15 to 30 s) and its kit's milliseconds; a format
that does not declare adds the kit's milliseconds only; no leg loads another format's clients, and the tests of the serve point stay the
same size.

## Not verified

- Anything against the owner's object store: whether its signed links honour the three overrides, how long they may live, whether they
  serve a range (its documentation is silent; the software behind it documents the overrides). The link check of every start answers it
  on the installation.
- Whether the client pods of the end-to-end legs reach the development store `quader-s3.quader.svc:9000` and trust its certificate.
- Whether Podman, CRI-O, the Docker Engine's older pull path, pnpm and Yarn refuse altered bytes: the end-to-end cases show it.
