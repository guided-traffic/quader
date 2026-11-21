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
  use (`MarkSets`).
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
row `files_removed_at`. The store is built by the composition root as the format's port (`formatPorts` in
[wire_formats.go](../../internal/app/wire_formats.go)); the task needs it from there or builds one over the same database.

**Until the task is rebuilt** (package 2 of the change), it commits each file of `freshclam`'s directory at the new layout with a
`rows.Container` that names the header's fields and no verification and no members, removes old versions by the age rule of
`malware.database.keep`, and marks nothing in use: the endpoint serves nothing until `MarkSets` is called.

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
