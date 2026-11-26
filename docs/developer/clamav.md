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
  format with the class `remote` or `virtual`, `formats.Validate` one with a route of the kind `Upload`. Its names have no ownership
  (`nameIn` of [ownership.go](../../internal/authz/ownership.go), Q-CLAMAV-19). The UI reads the fact in one place,
  [repository-class.ts](../../web/src/app/features/repositories/repository-class.ts): `creatableFormats` leaves it out of the chooser of
  **New repository** (its form's address shows the chooser with a note, Q-UI-1201), `shownClass` shows its repository as **System**
  instead of **Local** (Q-UI-1203), `classFilterQuery` maps a list's filter Local or System onto the API's `class` and `format`
  (Q-UI-1202). No code compares the name `clamav` for any of it; `CLAMAV_DEFINITIONS_FORMAT` of
  [fakes-catalog.ts](../../web/src/app/api/testing/fakes-catalog.ts) is in the fake's catalog by default.
- **A package is a database, a version one version of its container.** The stored layout is `<db>/<version>/<db>.cvd` (or `.cud`) with
  `<db>/<version>/<db>-<version>.cvd.sign` beside it ([format.go](../../internal/formats/clamav/format.go) `ParsePath`,
  `ContainerPath`, `SignaturePath`). Nothing but the database task commits into the repository.
- **A commit carries the version's metadata.** `OnCommit` ([hooks.go](../../internal/formats/clamav/hooks.go)) refuses a commit at any
  other path and a commit without the payload of its kind: `rows.Container` for a container, `rows.Signature` for a detached signature
  ([Q-CLAMAV-17](../spec/formats/clamav.md#q-clamav-17--a-commit-without-its-metadata-is-refused)).
- **The cleanup keeps the version row.** `OnItemsRemoved` marks the side row `files_removed_at` and leaves the version row, so the
  package screens list the version as history ([Q-CLAMAV-15](../spec/formats/clamav.md#q-clamav-15--the-version-row-stays-as-history-when-its-files-go));
  the database task deletes the history once the rescan cycle of the installation's settings has passed (`DeleteHistory`, R-MAL-037).
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
3. **The record.** `PGRow.Record` makes the set the version in use, marks the format's rows of both sets (`MarkSets`), requests the rescans of
   what was downloaded lately (the floor of R-MAL-009), wakes the job `malware-scan` and records `malware.database.imported`, in one
   transaction.
4. **The cleanup.** Every version that neither the set in use nor the one before it names loses its folder (`items.Store.Delete`); the
   history rows whose files went longer than the rescan cycle ago are deleted (`DeleteHistory`). After a put-back
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

## The UI development profile's way to `clamd`

The product reaches `clamd` by one way, the Unix socket of the malware scanner's pod (`malware.scanner.socket`,
[clamd.go](../../internal/malware/clamd/clamd.go) dials the network `unix` and nothing else; R-MAL-007). `clamd` opens no TCP socket in
the chart, so a taken-over `clamd` has no listener, and the pod needs none: both ends of the socket are in one pod. The owner's condition
of 2026-11-25, when ClamAV was to run on his development machine, keeps it so: "Ich will dass davon nichts in die ausgelieferten Images
wandert und ich will keinen neuen offenen Port im produktiven quader."
`TestR_MAL_007_TheProductReachesClamdByTheSocketOfThePodOnly` ([clamdlink.go](../../internal/archtest/clamdlink.go)) fails when a package
of the binary dials `clamd` by another network or by a network from the configuration, replaces the client's dial (`SetDialer`, the
client's own tests only), or speaks `clamd`'s protocol past the client.

The UI development profile ([web-ui.md](web-ui.md#the-malware-scanner),
[Q-ARCH-1065](../spec/architecture.md#q-arch-1065--the-ui-development-profile-runs-the-malware-scanner-as-the-charts-pod-on-docker))
therefore runs the chart's pod on Docker rather than a link of its own: the scanner's Quader and `clamd` in two containers that share the
socket's directory and the database directory as volumes, inside Docker Desktop's VM, which shares files with macOS but no socket. The
scanner cannot run on the host in any case: its loader writes the database into the constant `database.ClamdDir`, `/var/lib/clamav`,
which no key changes and which a developer's machine does not have. The profile's `clamd.conf` and `freshclam.conf` are the chart's with
the default values (`TestR_HA_068_TheUIProfileRunsTheChartsClamdConf`, `TestR_HA_069_TheUIProfileRunsTheChartsFreshclamConf`), and the
signatures come through the updater, the database task and `upstream/clamav` as in an installation, from a cache that `freshclam` fills at
most once a day per machine.

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

### The client certificates

A second measurement, not a test (R-CLAMAV-010), made once on 2026-11-21 by the development agent before the change landed: the dev
profile on a cluster of its own (`QUADER_DEV_CLUSTER=quader-clamcert`, the image of the change under a tag of its own), with
`config.malware.enabled: true` and a further endpoint `clamav` (`port: 8445`, `exposure: all`, `auth.clientCertificates: true`, its pair
from the dev CA's Issuer, `clientCaSecret` holding a CA made for the measurement with `openssl`), reached through its own Service
`quader-ep-clamav` (no Ingress). `system.bootstrap_anonymous_read` had to be `false`: the configuration refuses it beside an `https` endpoint
outside `localhost`. The updater fetched ClamAV's real set (`main` 63, `daily` 28190, `bytecode` 339) before the first step. **ClamAV
definition updates** switched on at Internal through the API (`clients.certificateAddresses` named the endpoint). The client: a stock
`freshclam` 1.5.5 of the pinned image in a pod of the namespace, `PrivateMirror` with `MaxAttempts 1`; the plain downloads with `curl` 8 of a
kind node, because the ClamAV image ships no `curl`. The host's load rose to about 85 during the run (other work), every pod of the
release restarted once in the middle, and the steps after it were run again; the cluster was deleted afterwards.

| Case | What `freshclam` or `curl` did | Exit, numbers |
|---|---|---|
| (a) a certificate of the admitted CA, `FRESHCLAM_CLIENT_CERT`, `FRESHCLAM_CLIENT_KEY`, `CURL_CA_BUNDLE` (the dev CA) | empty directory: per database `<db>.cld` "file not found", `<db>.cvd` header `OK`, the whole file, "Database test passed", "daily.cvd updated (version: 28190, …)"; a second run "database is up-to-date" for all three | 0 and 0; the first run 20 s with the test loads |
| (b) no certificate | "remote_cvdhead: Download failed (56) … Failure when receiving data from the peer" for `.cld` and `.cvd`, "Update failed" | 11, no cool-down written |
| (c) a certificate of another CA | the same as (b) | 11 |
| (d) the default endpoint at Internal, a technical token as the password in the URL | the three databases fetched and tested; every line that names the address shows the token (here replaced by `<token>`), as H-1164 says | 0, 10 s |
| (d) the same without a token | "Unexpected response (401)" | — |
| (e) the plain download with the token as a header on standard input (`--config -`), default endpoint | `main` 89,072,577 bytes, `daily` 23,446,798, `bytecode` 281,702, each `200` | under 0.4 s each; without the token `401` |
| (e) the plain download with `--cert`, `--key`, `--cacert` on the endpoint of the certificates | the same three files, `200` | under 0.4 s each; without a certificate curl's `(56) … tlsv13 alert certificate required` |
| the level Private | the certificate's `daily.cvd` answered `404`; Internal again afterwards | — |

The records: the request log's line of a certificate subject `{"endpoint":"clamav","status":200,"principal":"CN=scan-01.example.com,O=Example",
"principal_type":"certificate","principal_id":null,"token_id":null,"auth_method":"client_certificate","client_cert_serial":"65fef6…0f1b",
"client_cert_issuer":"CN=Example Device CA","limits_subject":"certificate:CN=scan-01.example.com,O=Example","limits_rule":"builtin:technical_token",
"remote_addr":"10.244.1.1"}`; the usage store's row `certificate | CN=scan-01.example.com,O=Example | 65fef6…0f1b | 18 requests | 112,832,785
bytes`; each refused handshake one `WARN` line `http: TLS handshake error from <address>: EOF` with `"listener":"client:clamav"` and
nothing else (H-1180). Not measured: a passthrough through HAProxy Ingress (the chart's rendering is the chart test's), an expired
certificate with a real client (the integration case covers it), the owner's installation.

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

## The client certificates

A ClamAV installation admitted by its client certificate ([formats/clamav.md](../spec/formats/clamav.md) R-CLAMAV-010, Q-CLAMAV-8 to
Q-CLAMAV-13, Q-CLAMAV-18; [endpoints.md](endpoints.md#credentials-per-endpoint-r-ep-010-r-ep-011)). Nothing of it is ClamAV's code: the
endpoint key, the subject and its records are general, and only the repository fact names `upstream/clamav`.

| Step | Where | What |
|---|---|---|
| The key | [endpoints.go](../../internal/config/endpoints.go) `EndpointAuth.ClientCertificates`, `validateClientCertificates` | `auth.client_certificates` of a further endpoint; refused on the default endpoint, without `tls.client_ca_file`, and with `auth.anonymous: true` |
| The handshake | `httpserver.NewListenerTLS` (unchanged) | `tls.client_ca_file` makes the listener `RequireAndVerifyClientCert` against the bundle, read again by the reloader; a missing, expired or foreign certificate fails before a request is read |
| The subject | [certificate.go](../../internal/auth/certificate.go) `byClientCertificate`, after the API key, the `Authorization` header and the session in `Stage.Authenticate` | the leaf of `r.TLS.VerifiedChains[0]`: the principal kind `certificate` without an ID, its name the subject (RFC 4514 by `pkix.Name.String`, cut at 255 bytes with `…[truncated]`), the credential `client_certificate` with the subject, the issuer and the serial in lower-case hexadecimal; never a header |
| The edge | [m1_convert.go](../../internal/app/m1_convert.go) `principalFromResult` | `httpserver.KindCertificate`, `Credential.ClientCert` |
| The rule | [certificate.go](../../internal/authz/certificate.go) `decideCertificate` in `evaluateWith`, `readShape` | the exposure first, then `read` of a repository whose `repoInfo.certificateReaders` is set while the part read is `internal` or `public`; every other action, populate and capability refused (`DenyHidden`); no role (`rolesOf`), never internal (`Subject.Internal`) |
| The fact | [index.go](../../internal/authz/index.go) `addRepositories`, the query `AuthzBuildRepositories` | `certificateReaders` = `upstream_repository.clients_enabled` of a repository of `upstream` whose declaration has `Clients` (`wellknown.Declared`); nothing new is stored |
| The budget | [subject.go](../../internal/limits/subject.go) `CallerCertificate`, `KindCertificate`; [model.go](../../internal/limits/model.go) `everyOf`; [buffer.go](../../internal/limits/usage/buffer.go) | the subject is the certificate's subject, the serial the credential column; the technical tokens' kind default (Q-LIM-96); the CHECK of `usage_bucket.subject_kind` admits `certificate` (migration 00097) |
| The records | [requestlog.go](../../internal/observability/requestlog.go), [wire_observability.go](../../internal/app/wire_observability.go) `recordPrincipal`; [audit.go](../../internal/authz/audit.go) | `principal_type` `certificate`, `client_cert_serial`, `client_cert_issuer` (null for every other request); `authz.denied` names the actor of the kind `certificate` and `client_cert_serial` in its details; an endpoint class keyed `principal` keys on the subject ([endpoint_ratelimit.go](../../internal/api/v1/endpoint_ratelimit.go)) |
| The item | [upstream_repositories_api.go](../../internal/api/v1/upstream_repositories_api.go) `certificateAddresses` | `clients.certificateAddresses`: the protocol base on every endpoint with the key, by name |
| The guide | [guide_api.go](../../internal/api/v1/guide_api.go) `certificateURLs`, `GuideData.CertificateURLs` ([ADR-232](../decisions.md#adr-232--the-guide-data-names-the-addresses-on-which-a-client-certificate-reads-the-repository)); [guide/](../../internal/formats/clamav/guide/) | the addresses on which a certificate subject reads the repository now (`Access.ReadableByCertificate`); the sections `authenticate.md` (token and certificate) and `configure.md` (the plain download, with a token on standard input or with `--cert`) |
| The chart | [_endpoints.tpl](../../deploy/helm/quader/templates/_endpoints.tpl) `quader.passthroughAnnotations`, [endpoint-ingress.yaml](../../deploy/helm/quader/templates/endpoint-ingress.yaml), [_validate.tpl](../../deploy/helm/quader/templates/_validate.tpl) | `auth.clientCertificates` rendered as `auth.client_certificates`; `ingress.tlsPassthrough` per controller (Q-EP-96) |
| The page | [upstream-registry.html](../../web/src/app/features/upstream/upstream-registry.html), [clamav-setup.ts](../../web/src/app/features/upstream/clamav-setup.ts) `freshclamCertificate`, `plainDownload` | the lines of the fold **Visibility** (Q-UI-905), the certificate's variant behind **Authenticate**, **Without freshclam** (Q-UI-914) |

The integration harness takes such an endpoint without a change: `testenv.ReplicaOptions.Endpoints` is `server.endpoints` as JSON, and a
further endpoint with its own `tls.certificates` and `tls.client_ca_file` serves TLS on a stack without TLS
([clamav_cert_test.go](../../test/integration/malware/clamav_cert_test.go)).

## The screens

The UI of the definitions ([api-and-ui.md](../spec/api-and-ui.md) R-UI-187, R-UI-188, R-UI-193, R-UI-194; Q-UI-903 to Q-UI-905, Q-UI-908 to Q-UI-913):

| File | Responsibility |
|---|---|
| [system-clients.ts](../../web/src/app/core/system-clients.ts) | `SystemClients.served` and `systemClosed`: whether `upstream/clamav` answers its clients now, asked once per screen (Q-UI-908); every screen that leaves out an address, a download or **Set up client** of a system repository asks it |
| [upstream-project.ts](../../web/src/app/core/upstream-project.ts) | `mayServeClients`, `CLAMAV_KEY`: the one system repository that may serve clients |
| [upstream-registries.ts](../../web/src/app/features/upstream/upstream-registries.ts), [upstream-actions.ts](../../web/src/app/features/upstream/upstream-actions.ts) | The row's switch **ClamAV definition updates** and its two questions (`toggleClients`) |
| [upstream-registry.html](../../web/src/app/features/upstream/upstream-registry.html), [clamav-setup.ts](../../web/src/app/features/upstream/clamav-setup.ts) | The section of the page: the switch, the endpoint's address, the level with the fold **Visibility** and the warning where Public is not allowed, the one line of `freshclam.conf` (`freshclamLines`, Q-UI-909) |
| [scanner-replicas.ts](../../web/src/app/shared/scanner/scanner-replicas.ts) | The updater's replicas (kind `freshclam`): in the section **Updater** open (`view="open"`), on the malware settings in a fold, on the System page; no slots, the definitions it holds (Q-UI-913) |
| [clamav-definitions.ts](../../web/src/app/views/packages/clamav-definitions.ts) | Reading a version's metadata (`clamavFacts`), the age and **Outdated** on a `daily` past 7 days (Q-UI-912), the states and their order, the name of a set |
| [clamav-databases.ts](../../web/src/app/features/repositories/content/clamav-databases.ts) | The repository's page: a row per database with the version in use, its build time and age, and the sets above it (Q-UI-910) |
| [package-detail.ts](../../web/src/app/features/packages/package-detail.ts), [package-version-view.html](../../web/src/app/views/packages/package-version-view.html), [version-command.ts](../../web/src/app/views/packages/version-command.ts) | A database's versions with their state (`include=state`, Q-API-384); a version's header fields, its detached signature and members; the install panel's way to the setup (Q-UI-911) |

The renders answer the definitions' calls in the browser ([clamav.ts](../../web/e2e/renders/clamav.ts)): the profile runs no updater, and
only the database task writes the repository. The seed switches **ClamAV definition updates** on at Public; the walks change the switch's
state in the answers of the list and the item.

## Tests

| Test | Tier | Time | Data |
|---|---|---|---|
| [cvd_test.go](../../internal/formats/clamav/cvd/cvd_test.go): the header, the level table, the members, the bounds, the detached signature | unit | milliseconds | containers of hundreds of bytes |
| [conformance_test.go](../../internal/formats/clamav/conformance_test.go): the kit with exactly six skips | unit | milliseconds | the same |
| [format_test.go](../../internal/formats/clamav/format_test.go): the descriptor, the refusals of a system-only format, the routes, the version in use, `WriteError`'s table, the snippet, the view, the refused commits | unit | milliseconds | the same |
| `TestR_UI_194_StatesComeFromTheFormatsMetadataInOneRead` ([view_test.go](../../internal/packages/view/view_test.go)): `include=state` | unit | milliseconds | none |
| The web specs of the screens: [clamav-definitions.spec.ts](../../web/src/app/views/packages/clamav-definitions.spec.ts), [clamav-setup.spec.ts](../../web/src/app/features/upstream/clamav-setup.spec.ts), [clamav-databases.spec.ts](../../web/src/app/features/repositories/content/clamav-databases.spec.ts) and the R-UI-187, R-UI-188, R-UI-192 to R-UI-194 cases of the neighbouring specs | web unit | about 3 s together | the fixture of [fakes-clamav.ts](../../web/src/app/api/testing/fakes-clamav.ts) |
| `TestR_REPO_075_TheSwitchOfClamAVDefinitionUpdates` ([clamav_test.go](../../test/integration/upstream/clamav_test.go)) | integration, area `upstream` | about 2 s | rows |
| `TestR_CLAMAV_009_AStockFreshclamUpdatesFromTheEndpoint` ([clamav_test.go](../../test/integration/malware/clamav_test.go)) | integration, area `malware` | about 7 s (two waits of 2.5 s for the replicas to read a switch) | three containers of a few hundred bytes |
| [updater_test.go](../../internal/malware/updater/updater_test.go): the listing, the files, the drain, the status and the gauge, the seed | unit | 0.5 s | containers of hundreds of bytes in a temporary directory |
| [task_test.go](../../internal/malware/database/task_test.go): the import from the updater's real handler, the verification and its refusals, the states before a first set, the checks, the put-back, the legacy signature | unit | 0.4 s | containers of hundreds of bytes, one RSA key of 1024 bits made per process |
| `TestR_MAL_010_TheTaskImportsFromTheUpdater` ([database_test.go](../../test/integration/malware/database_test.go)) | integration, area `malware` | about 4.5 s | a TLS stack with the updater's process, a worker and a web replica; containers of a few hundred bytes |
| `TestR_MAL_012_AnInstallationWithoutInternet` (the same file) | integration, area `malware` | about 2 s | two unsigned containers |
| `TestR_CLAMAV_010_ClientCertificatesNeedAFurtherEndpointThatVerifiesThem` ([endpoints_auth_test.go](../../internal/config/endpoints_auth_test.go)), `TestR_CLAMAV_010_TheCertificateSubject` ([certificate_test.go](../../internal/auth/certificate_test.go)), `TestR_CLAMAV_010_TheCertificateSubjectsRule` ([certificate_test.go](../../internal/authz/certificate_test.go)), `TestR_CLAMAV_010_TheCertificateSubjectsBudget` ([index_test.go](../../internal/limits/index_test.go)), `TestR_CLAMAV_010_ACertificateSubjectsRowNamesTheSerial` ([buffer_test.go](../../internal/limits/usage/buffer_test.go)), the request log's case in [requestlog_test.go](../../internal/observability/requestlog_test.go) | unit | milliseconds each | certificates as values, no key |
| `TestEndpoints/R-EP-008_Q-EP-96_R-CLAMAV-010_…` ([endpoints_test.go](../../test/chart/endpoints_test.go)) | chart | about 0.6 s (eight `helm template` runs) | none |
| The client certificates in `TestR_CLAMAV_009_AStockFreshclamUpdatesFromTheEndpoint` (`clientCertificates`, [clamav_cert_test.go](../../test/integration/malware/clamav_cert_test.go)) | integration, area `malware` | about 1.1 s more (0.9 s of it the usage flush of a second) | two CAs and four ECDSA certificates made by the test (the endpoint's pair and three clients), the case's containers |
