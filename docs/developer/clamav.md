# The format of ClamAV's definitions

The format `clamav` ([formats/clamav.md](../spec/formats/clamav.md) R-CLAMAV-001 to R-CLAMAV-009,
[ADR-225](../decisions.md#adr-225--the-clamav-updater-is-a-deployment-of-its-own-and-clamavs-definitions-are-a-repository-format)):
ClamAV's databases `main`, `daily` and `bytecode` as a repository kind of their own, with one repository, `upstream/clamav`, which the
product declares and the malware scanning's database task writes in process. The security perspective is
[security/clamav-definitions.md](../security/clamav-definitions.md); the format contract is [formats.md](formats.md).

## What has to be in your head first

- **System only.** `catalog.Facts.SystemOnly` is a fact of the catalog: no caller creates a repository of the format (`422
  validation_failed` with `format_system_only` at `/format`, `createTx` in [service.go](../../internal/repository/service.go)), `GET
  /api/v1/formats` says `systemOnly: true`, and the per-class enums of the create schemas leave it out. The catalog refuses a system-only
  format with the class `remote` or `virtual`, `formats.Validate` one with a route of the kind `Upload`.
- **A package is a database, a version one version of its container.** The stored layout is `<db>/<version>/<db>.cvd` (or `.cud`) with
  `<db>/<version>/<db>-<version>.cvd.sign` beside it ([format.go](../../internal/formats/clamav/format.go) `ParsePath`,
  `ContainerPath`, `SignaturePath`). Nothing but the database task commits into the repository.
- **A commit carries the version's metadata.** `OnCommit` ([hooks.go](../../internal/formats/clamav/hooks.go)) refuses a commit at any
  other path and a commit without the payload of its kind: `rows.Container` for a container, `rows.Signature` for a detached signature
  ([Q-CLAMAV-17](../spec/formats/clamav.md#q-clamav-17--a-commit-without-its-metadata-is-refused)).
- **The cleanup keeps the version row.** `OnItemsRemoved` marks the side row `files_removed_at` and leaves the version row, so the
  package screens list the version as history ([Q-CLAMAV-15](../spec/formats/clamav.md#q-clamav-15--the-version-row-stays-as-history-when-its-files-go));
  the database task deletes the history once `malware.rescan.cycle` has passed (`DeleteHistory`).
- **The endpoint serves what the side rows mark in use.** `<db>.cvd` serves the container of the version whose row says `in_use`
  ([handlers.go](../../internal/formats/clamav/handlers.go)); the database task marks the rows in the transaction that records the set in
  use and in a put-back (`MarkSets`, through `database.PGRow`).
- **Nothing from the updater is stored unverified.** The task checks the archive's MD5 against the header and the header's signature
  against ClamAV's public key ([verify.go](../../internal/malware/database/verify.go)) before it commits a container, and reads its
  members only then; an installation's own repository is copied unverified (R-MAL-033).
- **No 403 and no 429.** `WriteError` renders a `Forbidden` as `404` and a `RateLimited` as `503` with its `Retry-After`, and puts the
  `Basic` challenge on a `401` (`Rendered`): every stage of a request to the endpoint, the chain's included, renders through it.

## Files

| File | Responsibility |
|---|---|
| [format.go](../../internal/formats/clamav/format.go) | The descriptor, the two routes (`clamav.container.get`, `clamav.signature.get`, both `Metadata`, Q-CLAMAV-2), `Match`, the layout (`ParsePath`, `ContainerPath`, `SignaturePath`), `Classify`, `VersionPath`, the paths-only exposure adapter |
| [handlers.go](../../internal/formats/clamav/handlers.go) | The handlers of the two routes; `WriteError` and `Rendered`, the table of R-CLAMAV-005 |
| [hooks.go](../../internal/formats/clamav/hooks.go) | `OnCommit`, `OnItemsRemoved` |
| [port.go](../../internal/formats/clamav/port.go) | `Store`, the data port the format declares |
| [signatures.go](../../internal/formats/clamav/signatures.go) | `Signatures`: none, with the reason that a container's signature is ClamAV's file format, checked by the import ([Q-SIG-96](../spec/signatures.md#q-sig-96--the-clamav-definitions-declare-no-signature-their-containers-signature-is-the-imports)) |
| [view.go](../../internal/formats/clamav/view.go) | The package view: `Compare` as integers, `Stored`, `Describe` with `Metadata` (R-CLAMAV-003, R-CLAMAV-007) |
| [setup.go](../../internal/formats/clamav/setup.go), [guide/](../../internal/formats/clamav/guide/) | The snippets of `freshclam` and its guide (R-CLAMAV-006) |
| [cvd/](../../internal/formats/clamav/cvd/) | A leaf: the header of a container (`ParseHeader`, every field), the names (`Container`, `Sign`, `SignName`), the table of functionality levels (`LevelOf`), the member list of an archive (`ReadMembers`, `DefaultBounds`), the count of a detached signature (`SignatureCount`) |
| [cvd/cvdtest/](../../internal/formats/clamav/cvd/cvdtest/) | Containers of a few hundred bytes for the tests: every header field, the archive's MD5 in field 6, an index |
| [rows/](../../internal/formats/clamav/rows/) | The values of the data port |
| [internal/formatdata/clamav](../../internal/formatdata/clamav/store.go) | The data port over `clamav_version` (migration [00094_clamav.sql](../../internal/meta/postgres/migrations/00094_clamav.sql), statements in [clamav.sql](../../internal/meta/postgres/queries/clamav.sql)) and the methods of the database task |

## The interface of the database task

The malware scanning's task `malware-sync-db` ([internal/malware/database](../../internal/malware/database/task.go), R-MAL-010,
R-MAL-033, R-MAL-034) writes and reads the format's rows through these, all decided here and not left open by the specification:

| What | Where | Use |
|---|---|---|
| `rows.Container` | [rows.go](../../internal/formats/clamav/rows/rows.go) | the payload of the commit of a container at `ContainerPath(db, v, file)`: `DB` and `Version` (must agree with the path), `BuildTimeText`, `BuiltAt`, `Signatures`, `FunctionalityLevel`, `Builder`, `HeaderMD5`, `ComputedMD5`, `Verification` (`signature_verified`, `not_verified_repository`, `unsigned`), `Source` (the repository a copied container came from), `Members`, `MembersRead` |
| `rows.Signature` | rows.go | the payload of the commit of a detached signature at `SignaturePath(db, v)`: `DB`, `Version`, `Signatures` (`cvd.SignatureCount`) |
| `(*clamav.Store).MarkSets(ctx, tx, repoID, inUse, previous []rows.DBVersion)` | [store.go](../../internal/formatdata/clamav/store.go) | in the transaction that records the set in use and in a put-back: marks the versions each set names, clears every other mark |
| `(*clamav.Store).List(ctx, tx, repoID)` | store.go | every side row of the repository (`rows.Version` with `State()`), for the cleanup |
| `(*clamav.Store).DeleteHistory(ctx, tx, repoID, before)` | store.go | deletes the version rows whose files went before `before`, with their side rows, and raises their packages' revision |
| `cvd.ParseHeader`, `cvd.ReadMembers`, `cvd.SignatureCount`, `cvd.DBs`, `cvd.IsDB` | [cvd/](../../internal/formats/clamav/cvd/) | the parsing; the task verifies the MD5 and the signature itself |

The removal of a version's files goes through the item store (`items.Store.Delete` of the folder `<db>/<version>`), whose hook marks the
row `files_removed_at`. The composition root builds a second store over the same database for the task
(`newMalwareDatabase` in [wire_malware_database.go](../../internal/app/wire_malware_database.go)) and hands it to `database.PGRow` as
`Sets`: the marks run in the record's and the put-back's transactions, the list and the history in transactions of their own.

## How a set arrives

The task `malware-sync-db` ([task.go](../../internal/malware/database/task.go), R-MAL-010, R-MAL-033, R-MAL-034):

1. **The source.** With `malware.database.source: updater` (the default) it reads the listing of the updater `malware-updater-freshclam`
   over mutual TLS ([updater.Client](../../internal/malware/updater/client.go)); without `malware.updater.url` it does nothing. With
   `repository` it reads the root of the repository `malware.database.repository` names. A listing that names a `.cld`, a `.cud` or any
   other file is `database_invalid`; one without `main` and `daily` waits while no set was ever in use and fails `database_missing` after
   (`missing`, Q-MAL-98); an updater that does not answer the same with `updater_unavailable`.
2. **Only what is new.** Each file whose path `<db>/<version>/<file>` `upstream/clamav` holds already is taken as it is (an unchanged
   `main.cvd`); every other one is fetched into the spool and checked: the header names the listed version (else the updater replaced it,
   and the run ends without a failure), the archive's MD5 is the header's and the header's signature verifies against `Options.Key`
   (ClamAV's key unless a test sets its own; `signature_invalid` otherwise), the members are read (`cvd.ReadMembers`, 1,000 entries and
   2 GiB). A detached signature is at most 64 KiB and starts with `#clamsign-1.0`. Only when every new file passed are they committed,
   each with its `rows.Container` or `rows.Signature`.
3. **The record.** `PGRow.Record` makes the set the version in use, marks the format's rows of both sets (`MarkSets`), marks the recent
   rescans and records `malware.database.imported`, in one transaction.
4. **The cleanup.** Every version that neither the set in use nor the one before it names loses its folder (`items.Store.Delete`); the
   history rows whose files went longer than `malware.rescan.cycle` ago are deleted (`DeleteHistory`). After a put-back
   (`DatabaseRefused`) the cleanup keeps the version put back and the refused one, until the next set (Q-MAL-102).

## The legacy signature

ClamAV signs a container's archive in its header: field 6 is the archive's MD5 in hexadecimal, field 7 an RSA signature over it. The
algorithm is libclamav's `cli_versig` and `cli_decodesig` in `libclamav/dsig.c`, which ClamAV 1.5.5's `libclamav_rust/src/cvd.rs`
(`verify_rsa_dsig`) calls with the MD5 it computed: the field is a number written in 6-bit digits of the alphabet `a-z A-Z 0-9 + /`,
the first character the lowest digit; raised to the exponent `100001027` modulo ClamAV's 1024-bit modulus (`CLI_ESTR`, `CLI_NSTR`) it
is at most 16 bytes, which, padded with zeros in front and written as 32 lower-case hexadecimal digits, must equal the MD5.
[verify.go](../../internal/malware/database/verify.go) does the same with `math/big`. Verified on 2026-11-21 against ClamAV's own files
`main.cvd` 63, `daily.cvd` 28189 and `bytecode.cvd` 339 (fetched by a stock `freshclam` 1.5.5 the day before): all three verify, and a
changed character of a signature does not. The tests sign with a key of their own (`cvdtest.Key`, `cvdtest.Signed`, `cvdtest.SignMD5`).

## The updater

[internal/malware/updater](../../internal/malware/updater/updater.go) is the `quader` container of `malware-updater-freshclam`, the role
`scanner` of the kind `freshclam` ([wire_malware_updater.go](../../internal/app/wire_malware_updater.go), R-MAL-032):

| File | Responsibility |
|---|---|
| [updater.go](../../internal/malware/updater/updater.go) | `List`: the directory through an `os.Root`, regular files only, no link: `main.cvd`, `daily.cvd`, `bytecode.cvd` with a header that parses and the `.cvd.sign` of each one's version; `Open` of a listed file; `ToolVersion` from `.quader-tool-version` |
| [service.go](../../internal/malware/updater/service.go) | the routes `GET /updater/v1/database` and `/{name}`, the readiness (`Ready`: seeded, not draining), the drain, `Status` for the report, the gauge `quader_malware_updater_database_age_seconds`, and `Seed`: the set in use from the scan listener's database routes, each file checked against the listing's SHA-256 and renamed into place, a newer container kept, then the marker `.quader-seeded`, also after a failure or 60 seconds |
| [client.go](../../internal/malware/updater/client.go) | the worker's side of the link: `Listing`, `Open`, `ErrUnavailable`, `ErrGone` |

The scan listener admits the updater's certificate (`malware.scan_listener.updater_client_name`) on the database routes as the system
subject `malware-updater-freshclam` (`registrytoken.UpdaterSubject`, `malwareDatabasePeer` in
[wire_malware_scanlisten.go](../../internal/app/wire_malware_scanlisten.go)) and records its report as the kind `freshclam`: the set its
directory holds with the daily's build time, no slots (migration 00095, `scanreport.Report.DatabaseBuiltAt`).

## Measured on a kind cluster

A measurement, not a test (R-CLAMAV-009, Q-CLAMAV-7), made once on 2026-11-21 by the development agent before the change went to the
owner's installation: the dev profile of [e2e.md](e2e.md) (`deploy/dev/up.sh`, a cluster of four kind nodes of its own, the release
image of the change, `config.malware.enabled: true`, everything else the profile's), on a developer machine shared with other work,
the pinned ClamAV 1.5.5 image, ClamAV's real network. The cluster was deleted afterwards.

| Step | What was seen | Numbers |
|---|---|---|
| The updater starts | `quader-malware-updater-freshclam` 2/2 ready; `packages` was not listening yet in the updater's first second, and that first build asked the scan listener once, so the seed wrote the marker at once (no set was in use anyway). The change now asks again every 2 s within the seed's 60 s; the restart below ran that build | ready 5 s after the container started |
| `freshclam` fetches from ClamAV's network | `daily.cvd` 28190, `main.cvd` 63, `bytecode.cvd` 339 whole, each with its `.cvd.sign` (ClamAV's network serves them) | 4 s for about 113 MB |
| The worker's first run | came before `freshclam` had finished: "no signature database is in use yet; the task waits", nothing recorded (Q-MAL-98); the next run was the schedule's, up to 30 minutes later, so the import now asks again every 30 seconds while no set is in use (Q-MAL-104, built after this measurement) | 3 s too early |
| The import (a worker restarted, so its run at start) | the set `main-63.daily-28190.bytecode-339` recorded from `upstream/clamav`; all three containers `signature_verified` against ClamAV's key, the computed MD5 equal to the header's, the members read (11, 24 and 86), the three detached signatures stored (9,078 bytes, 1 signature each), all three marked `in_use` | 6 files, 113 MB |
| The scanners | both `malware-scanner-clamav` pods loaded the set; after a later rollout one loaded it 10.4 s after its start | — |
| The updater restarted (a rollout) | seeded from the set in use: 6 files in 0.5 s; `freshclam` then found all three "up-to-date" and fetched nothing from ClamAV's network | — |
| The report | `GET /api/v1/system/scanners` lists the kind `freshclam` (`reporting`) with its pod: `ready`, freshclam 1.5.5, the set and its build time, slots 0 of 0 | — |
| A stock `freshclam` 1.5.5 with `PrivateMirror https://quader-packages.quader.svc:8080/api/clamav/upstream/clamav`, the switch on at public | first run, empty directory: per database `<db>.cld` 404, `<db>.cvd` with `Range: bytes=0-511` 206, `<db>.cvd` 200, `<db>-<version>.cvd.sign` 200; every database "updated", `freshclam`'s own test load passed, exit 0. Second run: per database `.cld` 404 and `.cvd` with `Range` and `If-Modified-Since` (the build time) 206; "up-to-date", exit 0 | 7 s for the first run with the test loads, under a second for the second |

Seen on the way: the first client request was answered `421`, because the dev profile's default endpoint checks hosts and the in-cluster
name was none of them (`endpoints.default.hosts` gained it); `freshclam` counts a `421` as a failed attempt, no cool-down. The second
run's `206` and not `304` is the rule of R-CLAMAV-004: the item changed after the build time the client sends.

Not measured here: a token in the client's URL, the plain download into a `clamd`, the owner's installation, and a newer daily arriving
while the updater runs (the cluster lived about 15 minutes).

## The switch of the clients

`upstream/clamav` is declared with `wellknown.Declaration.Clients` ([wellknown.go](../../internal/wellknown/wellknown.go)). The switch is
`upstream_repository.clients_enabled`, written by `Service.SetUpstreamClients` ([upstream_ops.go](../../internal/repository/upstream_ops.go),
`PUT /api/v1/upstream-repositories/{key}/clients`): on at `public` (or `internal` with the warning `public_not_allowed_lowered`), off with
both levels back at `private`. Every write records `repository.updated` with `clients` among its changes; switching off journals the part
`clientsOff` of `repository.narrowed`, which the replay applies again ([journal_replay.go](../../internal/repository/journal_replay.go),
[Q-REPO-420](../spec/repositories-and-proxy.md#q-repo-420--switching-the-clients-off-is-the-journal-part-clientsoff)).
`UpstreamFacts.Closed()` is what the dispatcher, `/files/` and the lists read: a system repository answers no client unless its clients are
on. `Repository.RefusesHandDelete()` refuses every hand delete of a local system repository in `files.Service.Delete` (Q-REPO-416). The
malware gate passes every item of a system repository (`gate.Download.System`, Q-MAL-100).

## Tests

| Test | Tier | Time | Data |
|---|---|---|---|
| [cvd_test.go](../../internal/formats/clamav/cvd/cvd_test.go): the header, the level table, the members, the bounds, the detached signature | unit | milliseconds | containers of hundreds of bytes |
| [conformance_test.go](../../internal/formats/clamav/conformance_test.go): the kit with exactly six skips | unit | milliseconds | the same |
| [format_test.go](../../internal/formats/clamav/format_test.go): the descriptor, the refusals of a system-only format, the routes, the version in use, `WriteError`'s table, the snippet, the view, the refused commits | unit | milliseconds | the same |
| `TestR_REPO_075_TheSwitchOfClamAVDefinitionUpdates` ([clamav_test.go](../../test/integration/upstream/clamav_test.go)) | integration, area `upstream` | about 2 s | rows |
| `TestR_CLAMAV_009_AStockFreshclamUpdatesFromTheEndpoint` ([clamav_test.go](../../test/integration/malware/clamav_test.go)) | integration, area `malware` | about 7 s (two waits of 2.5 s for the replicas to read a switch) | three containers of a few hundred bytes |
| [updater_test.go](../../internal/malware/updater/updater_test.go): the listing, the files, the drain, the status and the gauge, the seed | unit | 0.5 s | containers of hundreds of bytes in a temporary directory |
| [task_test.go](../../internal/malware/database/task_test.go): the import from the updater's real handler, the verification and its refusals, the states before a first set, the checks, the put-back, the legacy signature | unit | 0.4 s | containers of hundreds of bytes, one RSA key of 1024 bits made per process |
| `TestR_MAL_010_TheTaskImportsFromTheUpdater` ([database_test.go](../../test/integration/malware/database_test.go)) | integration, area `malware` | about 4.5 s | a TLS stack with the updater's process, a worker and a web replica; containers of a few hundred bytes |
| `TestR_MAL_012_AnInstallationWithoutInternet` (the same file) | integration, area `malware` | about 2 s | two unsigned containers |
