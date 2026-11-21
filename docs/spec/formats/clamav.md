# ClamAV definitions format

Status: specification (2026-11-21), not built. The owner's words of 2026-11-21: "Außerdem möchte ich, dass wir das freshclam artefakt als
spezifisches ClamAV-Repo in Upstream einbindbar machen. ClamAV wird also ein eigenständiges Artefakt-Repo, wir ziehen alle verfügbaren
Metadaten aus dem ClamAV-Definitionen. Ein Admin kann in den Upstream-Repos ClamAV-Definition-Updates einschalten, wodurch ein
unternehmens-Endpunkt für alle ClamAV Instanzen bereitgestellt wird, dieser Endpunkt bedient sich automatisch aus dem internen Freshclam
und erhält aktualisierungen."

This page specifies the format `clamav`: the signature databases of ClamAV ("definitions") as a repository kind of their own. It has one
repository, `upstream/clamav`, which the product declares and writes ([repositories-and-proxy.md](../repositories-and-proxy.md) R-REPO-075).
Quader's own malware scanners load the set in use from it ([malware-scanning.md](../malware-scanning.md) R-MAL-010, R-MAL-011), and when
an administrator switches **ClamAV definition updates** on, every ClamAV of the company updates from it with a stock `freshclam`.

| Topic | Owner |
|---|---|
| How a set reaches the repository: the updater `malware-updater-freshclam`, the task `malware-sync-db`, the checks, the verification, the cleanup and the history | [malware-scanning.md](../malware-scanning.md) R-MAL-010, R-MAL-012, R-MAL-032 to R-MAL-034 |
| The repository `upstream/clamav`, the switch **ClamAV definition updates**, its levels, what an administrator may change | [repositories-and-proxy.md](../repositories-and-proxy.md) R-REPO-073, R-REPO-075 |
| The updater's Deployment in the chart | [ha-and-helm.md](../ha-and-helm.md) R-HA-069 |
| The admin page of the switch, the repository's page with its databases and versions | [api-and-ui.md](../api-and-ui.md) R-UI-187, R-UI-188, R-UI-194 |
| The security perspective | [security/clamav-definitions.md](../../security/clamav-definitions.md) |

Conventions on this page:

- `https://quader.example.com` is the public base URL *(example)*. The *endpoint* is
  `https://quader.example.com/api/clamav/upstream/clamav`, the protocol base of `upstream/clamav`.
- *A database* is one of ClamAV's three standard databases, `main`, `daily` and `bytecode`. *A container* is one database file: a `.cvd`
  (the publisher's signed container) or a `.cud` (ClamAV's unsigned container, which only an installation's own repository supplies,
  R-MAL-012). *A set* is one version of `main` and `daily`, with `bytecode` when there is one, named `main-<n>.daily-<n>.bytecode-<n>`
  ([malware-scanning.md](../malware-scanning.md) Q-MAL-82). *The set in use* is the one row of `malware_database`.
- What `freshclam` 1.5.5 requests and how it treats each answer was read in ClamAV's source at the tag `clamav-1.5.5` on 2026-11-21
  (`libfreshclam/libfreshclam_internal.c`, `freshclam/freshclam.c`); no `freshclam` was run against Quader before this page was written
  (R-CLAMAV-009).

## The format

### R-CLAMAV-001 · The format `clamav` and its descriptor
Priority: **P1** · Covers: quader-only

| Field | Value | Meaning |
|---|---|---|
| `name` | `clamav` | The format ID |
| `displayName` | `ClamAV definitions` | |
| `classes` | `["local"]` | The repository holds what the product stores; there is no remote or virtual class (R-CLAMAV-008) |
| `systemOnly` | `true` | Only a declaration of the project `upstream` creates a repository of the format ([Q-CLAMAV-4](#q-clamav-4--the-format-is-system-only-a-fact-of-the-catalog)) |
| `layout` | `clamav` | R-CLAMAV-002 |
| `protocolBasePath` | `/api/clamav/{project}/{key}/` | |
| `upstreamPresets` | `[]` | |
| `patternSubject` | `path` | |
| package names | `main`, `daily`, `bytecode`, compared as written | One package per database ([Q-CLAMAV-1](#q-clamav-1--one-package-per-database)) |
| `priorityMembers`, `pinsAndClaims`, `caps`, `wiki` | `false` | |
| ownership | none | Nothing but the database task writes the repository |
| settings | none | |
| `clients` | `[{"id": "freshclam", "displayName": "freshclam"}]` | R-CLAMAV-006 |

- **System only.** `catalog.Facts.SystemOnly` is a fact of the catalog that every layer reads (a contract addition,
  [ADR-225](../../decisions.md#adr-225--the-clamav-updater-is-a-deployment-of-its-own-and-clamavs-definitions-are-a-repository-format)):
  `POST /api/v1/projects/{project}/repositories` with `"format": "clamav"` answers `422 validation_failed` with `format_system_only` at
  `/format` in every project; `GET /api/v1/formats` lists the format with `systemOnly: true`, and the creation form does not offer it;
  `Register` refuses a format that is system only and declares the class `remote` or `virtual`, or a route of the kind `Upload`. The per-class
  enums of the OpenAPI document (`TestR_ARCH_010_PerClassFormatEnums`) leave a system-only format out of the create schemas.
- The format declares its signatures once the format contract asks a format to declare them; until then the verification of a container
  is this format's own metadata (R-CLAMAV-003).

Acceptance criteria: unit (`internal/formats/clamav`, under a second): the descriptor; `Register` refuses a system-only test format with a
remote class or an `Upload` route; integration (area `upstream`, about 1 s): a create of a `clamav` repository in `web` answers 422 with
`format_system_only`.

### R-CLAMAV-002 · Packages, versions and the layout
Priority: **P1** · Covers: quader-only

- **A package is a database**, **a version is one version of it**, as its container's header names it (`daily` `28190`). The scanners
  still use whole sets: the set in use is the row of `malware_database`, and a verdict names the set it was made with
  ([malware-scanning.md](../malware-scanning.md) R-MAL-002, R-MAL-010). Which version of each database belongs to the set in use is marked
  on the format's own rows (`in_use`, and `previous` for the set before it); the database task sets both in the transaction that records the
  set in use and in a put-back (R-MAL-010).
- **The layout** of the stored items:

  | Path | What |
  |---|---|
  | `<db>/<version>/<db>.cvd` | the publisher's container, byte for byte as `freshclam` fetched it |
  | `<db>/<version>/<db>-<version>.cvd.sign` | its detached signature, when `freshclam` fetched one (ClamAV 1.5 and later) |
  | `<db>/<version>/<db>.cud` | an unsigned container, only from an installation's own repository (R-MAL-012) |

  An unchanged `main.cvd` is one item for every set that names its version, so a new `daily` costs its own size only.
- **Classify**: a path of the layout is content of the package `<db>` and the version `<version>`; `<db>.cvd` and `<db>-<n>.cvd.sign` at the
  root are the routes of R-CLAMAV-004, which serve a stored item of the layout and are no stored path themselves; every other path is no
  path of the format.
- **Written by one writer.** The database task commits each new container and signature in process, as system work with the actor
  `system:malware-database`, with the container's metadata of R-CLAMAV-003 as the commit's payload; the format's `OnCommit` hook writes the
  package row, the version row and the side row of the metadata in the commit's transaction, through the format's data port
  (`internal/formatdata/clamav`, table `clamav_version`, migration 00091). The cleanup removes items through the item store, and
  `OnItemsRemoved` marks the version's files as removed and keeps the side row as history (R-MAL-034).

Acceptance criteria: unit (the kit's checks 3, 9 and 10, milliseconds): the classification of the layout and the root routes, the hook's
rows for a committed container, the three names; integration (area `malware`, R-MAL-010's case): two sets that share `main` hold one
`main.cvd` item.

### R-CLAMAV-003 · What a version shows
Priority: **P1** · Covers: quader-only

Everything the container and its signature file hold, stored per version in `clamav_version` and shown on the repository's page
([api-and-ui.md](../api-and-ui.md) R-UI-194). The database task reads it from the bytes it stores (R-MAL-033):

| Field | From | Shown as |
|---|---|---|
| database and version | the container's file name and its header's field 3 | `daily` · `28190` |
| build time | the header's field 9 (seconds; field 2, the text, kept as written) | the date and time, and the age: now minus the build time, marked **Outdated** from 7 days on, the age at which ClamAV's engine warns "The virus database is older than 7 days!" |
| signatures | the header's field 4 | the number |
| functionality level | the header's field 5 | the level and the first ClamAV release whose engine meets it, from the table of ClamAV's page "Versions and Functionality Levels" that the format keeps in its code ("ClamAV 0.103 or later"); a level above the table's newest entry reads "Newer than ClamAV <newest>" |
| builder | the header's field 8 | the name |
| archive digest | the header's field 6 (MD5) and the MD5 the task computed over the archive | under **Verification**, with the outcome |
| verification | R-MAL-033 | **ClamAV's signature verified** (the header's signature, checked against ClamAV's public key), **Not verified: from the repository ops/clamav-db** (an installation's own repository), **Unsigned container** (a `.cud`) |
| detached signature | the `.cvd.sign` file: its name, its size and the number of signatures it holds (its lines after `#clamsign-1.0`) | **Detached signature: present** with the number, or **none**; Quader passes it on and does not verify it ([Q-MAL-95](../malware-scanning.md#q-mal-95--quader-verifies-the-headers-signature-and-passes-the-detached-signature-on)) |
| size and digests | the stored item: size, SHA-256 (and the digests every item has, R-STOR-002) | |
| member files | the container's tar, read after its signature verified: each member's name, size and SHA-256 as the container's own index (the member `<db>.info`) lists them, and the number of its lines that are neither empty nor a comment | a table; the header's signature count is the total that counts, and a member's line count is labelled **Lines**, because one line is one signature for most kinds of signature file and not for every one (not verified for every kind) |
| state | the format's rows | **In use**, **Previous** (the set before it, which a put-back needs), **Superseded** (stored until the cleanup), **Files removed** (history) |
| times | stored at, files removed at | |

- A member list that the bounds of R-MAL-033 cut short says "Members not read: the container holds more than the bounds allow." and the
  version is stored all the same: its signature verified it.
- Nothing of a version is a secret, and nothing of it is markup: names and texts are shown as text.

Acceptance criteria: unit (`internal/formats/clamav`, under a second, containers of hundreds of bytes written by the test): the header's
fields, the level table's boundary and its "newer than" case, a member list from a test container's index with line counts, the cut list
past the bounds; web unit (R-UI-194's spec): **Outdated** at 7 days and not at 6.

## The endpoint

### R-CLAMAV-004 · The routes a stock `freshclam` asks
Priority: **P1** · Covers: quader-only

A stock `freshclam` (1.0 to 1.5) with `PrivateMirror <endpoint>` uses no DNS record and no patch files. For each database it asks for the
first 512 bytes of `<db>.cld`, then of `<db>.cvd` (`Range: bytes=0-511` and `If-Modified-Since` set to the build time of its own copy), then,
when the version is newer than its own, the whole `<db>.cvd`, and from 1.5 on `<db>-<version>.cvd.sign`, where a `404` is no error. Every
request is a `GET`; it follows up to three redirects and sends `Connection: close` and no other header of its own.

The routes, all `GET` and `HEAD`, the action `read`, the kind `Metadata` ([Q-CLAMAV-2](#q-clamav-2--the-routes-are-of-the-kind-metadata)):

| Name | Path below the protocol base | Serves |
|---|---|---|
| `container` | `{db}.cvd`, `{db}` one of `main`, `daily`, `bytecode` | the item `<db>/<v>/<db>.cvd` of the version `<v>` of `{db}` in use; `404` while the set in use holds no `.cvd` of it (none yet, or a `.cud` of an installation's repository) |
| `signature` | `{db}-{version}.cvd.sign` | the item `<db>/<version>/<db>-<version>.cvd.sign` while it is stored; `404` otherwise |

- `{db}.cld`, any other database name and any other path match no route and answer `404`; a client then asks for `.cvd`, as it does of the
  publisher's own network, which serves no `.cld` either.
- **The version in use, not the newest stored** ([Q-MAL-99](../malware-scanning.md#q-mal-99--the-endpoint-serves-the-set-in-use)): the
  endpoint serves what Quader's own scanners load. A version a scanner's `clamd` refused is put back (R-MAL-010) and is not served.
- **Range and conditions** are every stored file's (`edge.ServeItem`, [storage.md](../storage.md) R-STOR-012): `Range: bytes=0-511` answers
  `206` with those bytes; `If-Modified-Since` at or after the item's last change answers `304`. The item changes when the task commits it,
  which is after the build time its header states, so a `304` goes only to a client whose copy was built at or after the commit: the same
  version or a newer one, and `freshclam` never steps back to an older one. A client whose copy is older gets `206` and the header, compares
  the versions and fetches the file. A wrong `304`, which would hide an update, cannot happen this way; a client that is current pays one
  request of 512 bytes.
- **Counted** like every served item: the download statistics of the version's item, the usage store under the caller's subject
  ([usage-and-limits.md](../usage-and-limits.md) R-LIM-006). A document route is admitted by no byte figure (R-LIM-021), so a download of a
  container is never refused in the middle and never refused before it starts by a byte figure; a request figure applies (R-CLAMAV-005).
- **Who reads** is the repository's level while **ClamAV definition updates** is on, and nobody while it is off: every request then
  answers `404` as for a repository that does not exist ([repositories-and-proxy.md](../repositories-and-proxy.md) R-REPO-075).
- The malware gate never asks about an item of a system repository (R-MAL-013), and the vulnerability block has nothing to block: the
  format has no vulnerability adapter.

Acceptance criteria: unit (the kit's checks 1, 2, 3, 7, 12 and 13, milliseconds): the two routes and their examples, `daily.cld` and
`safebrowsing.cvd` unmatched, every content case the same for three callers; integration (area `malware`, about 3 s, a set of test
containers of a few hundred bytes): the requests of `freshclam` 1.5.5 replayed in its order by Go's client (R-CLAMAV-009).

### R-CLAMAV-005 · Answers a `freshclam` can live with: never `403` and never `429`
Priority: **P1** · Covers: quader-only

A stock `freshclam` puts itself on a cool-down of 24 hours after any `403`, and after any `429` of `Retry-After` or 4 hours when the header
is missing or its libcurl is older than 7.66 (Red Hat Enterprise Linux 8 ships 7.61), whatever the server; it stores the cool-down and
updates nothing until it ends. A `401`, a `404` and a `5xx` cost it the attempt only: it tries `MaxAttempts` times (3), five seconds apart,
then the next mirror, and again at its next check.

The format's `WriteError` therefore renders ([Q-CLAMAV-3](#q-clamav-3--a-refusal-is-404-or-503-never-403-or-429)):

| Error kind | Status | Why |
|---|---|---|
| no credential, or one that is not valid, where the level needs one | `401` with `WWW-Authenticate: Basic realm="quader"` | `freshclam` sends the credentials of its URL as Basic (R-CLAMAV-006) |
| `Forbidden` (a read the caller may not make, an exposure rule, any other refusal of the decision point) | `404` | a `403` would stop that client for a day |
| `RateLimited` (a request figure, a block after refused requests, a rate-limit class of the endpoint, the throttle of failed credentials) | `503` with the error's `Retry-After` | a `429` would stop a client on an older libcurl for four hours |
| every other error | its own status (`404`, `405`, `421`, `500`, `503`) | |

- The body is a problem document with the error's `code` and `detail` (R-API-009), for a person who asks with `curl`; `freshclam` logs the
  status and reads nothing of it.
- The chain's own refusals on the prefix (`401`, `421`, `429`, `503`) reach this rendering through the error shape of the prefix
  ([architecture.md](../architecture.md) R-ARCH-023), so no refusal of any stage of a request to the endpoint is a `403` or a `429`.
- The malware gate's answers never reach it (R-MAL-013), and the format has no `Download` route: the kit's checks 16 and 17, which hold a
  `Download` route to `429` and `403`, have nothing to run over (R-CLAMAV-008).

Acceptance criteria: unit (`internal/formats/clamav`, milliseconds): each row of the table through `WriteError`, `Retry-After` kept on the
`503`; integration (area `malware`, in R-CLAMAV-009's case): a request figure of one request per hour on the anonymous subject in `enforce`
answers the second request `503` with `Retry-After`; an external user's token on an internal endpoint answers `404`.

### R-CLAMAV-006 · Setting up a ClamAV installation
Priority: **P1** · Covers: quader-only

The snippet of the client `freshclam` and the guide (R-UI-049, [api-and-ui.md](../api-and-ui.md) R-API-053), shown on the repository's
**Set up client** page and on the admin page of the switch while **ClamAV definition updates** is on:

```ini
# /etc/clamav/freshclam.conf (the path of the distribution's package; example)
PrivateMirror https://quader.example.com/api/clamav/upstream/clamav
```

- **One line.** `PrivateMirror` replaces the publisher's network, its DNS record and its patch files; existing `DatabaseMirror` lines may
  stay, `PrivateMirror` wins. The scheme is written out: `freshclam` reads a `PrivateMirror` without one as `http://`. `ScriptedUpdates`,
  `DNSDatabaseInfo` and `Checks` need no change (the checks are the client's own, 12 a day by default; Quader serves each in one request
  while the copy is current).
- **The certificate.** Where the installation's certificate is issued by an internal CA, the client must trust it: on Linux the
  environment variable `CURL_CA_BUNDLE=/etc/pki/quader-ca.pem` *(example)* for `freshclam` (for the systemd unit a drop-in with
  `Environment=`), or the CA in the system store; on macOS and Windows the system store. `freshclam.conf` has no option for it.
- **A level below Public** ([repositories-and-proxy.md](../repositories-and-proxy.md#q-repo-415--who-reads-the-clamav-definitions-when-they-are-switched-on)
  Q-REPO-415, put to the owner): the client sends a Quader token as the password of the URL, any user name:
  `PrivateMirror https://clamav:${QUADER_TOKEN}@quader.example.com/api/clamav/upstream/clamav`. The guide says plainly that `freshclam`
  logs the whole URL, the token with it, at the level `INFO` of its log, and that this way is undocumented by ClamAV; the token is one of a
  service account made for the purpose, with no grant beyond reading this repository. A client certificate, which `freshclam` 1.2 and
  later can present, authenticates nothing on the endpoint: Quader maps no client certificate to a principal on the package surface.
- **What the guide covers** (the required topics of check 8): what the repository is and that Quader's own scanning uses the same set;
  `configure` (the line, the CA); `authenticate` (only where the level needs it, as above); `install` (run `freshclam` once and read
  "daily.cvd updated" or "is up-to-date"); `troubleshooting`: a `404` for every database (the switch is off, or the level hides it from the
  caller), a `401` (the token), "Can't verify database integrity" (a file changed on the way: compare its SHA-256 with the repository's
  page), a client that waits after a refusal of another server (`freshclam.dat` holds a cool-down until it ends; Quader never causes one).

Acceptance criteria: unit (the kit's check 8, milliseconds): the template and the guide for every reader variant, no credential outside
`${QUADER_TOKEN}`; web unit (R-UI-188's spec): the snippet shows only while the switch is on.

### R-CLAMAV-007 · The package view
Priority: **P1** · Covers: quader-only

The format returns a `ViewAdapter` (R-API-029, [developer/formats.md](../../developer/formats.md#the-package-view)), so the repository's
packages and versions show on the screens of every repository:

- `Compare` orders versions as integers; `Describe` returns the metadata of R-CLAMAV-003; `Stored` lists the stored versions of a database,
  newest first, and the history rows without files after them; `Withheld` names none; `Find` searches the three names.
- `VersionFetch` is `FetchLeadFile`: a version is fetched when its container is served, which counts the version's downloads and its last
  download.
- The version in use is the first row a database shows; the package list shows each database with the version in use, its build time and
  its age.

Acceptance criteria: unit (the kit's check 11, milliseconds): `Compare` is a total order over `28189`, `28190`, `9`; `Stored` over an empty
repository and over a seeded database with one history row.

### R-CLAMAV-008 · What the format leaves out
Priority: **P1** · Covers: quader-only

- **No upload, no delete, no client write.** The format has no route of the kinds `Upload`, `Delete` or `Admin`; `/files/` deploys nothing
  into it (its protocol base is not `/files/`, [developer/formats.md](../../developer/formats.md)); the repository refuses a delete by hand
  (R-REPO-075). The kit's check 6 (no `Upload` route), 4 and 5 (no remote or virtual class), 15 (no wiki content) and 16 and 17 (no
  `Download` route) are skipped with these reasons.
- **No patch files.** No `<db>-<n>.cdiff`: `freshclam` with `DatabaseMirror` pointed at the endpoint would wait while it is one version
  behind and the patch is missing, so the setup names `PrivateMirror` only
  ([Q-CLAMAV-6](#q-clamav-6--privatemirror-only-no-patch-files)).
- **No `.cld`.** The updater keeps whole signed containers (Q-MAL-93), so a `.cld` (a container patched on the client) is never stored and
  never served.
- **No databases but the three.** `safebrowsing` is discontinued by its publisher, `test` is the publisher's and of no use here, `valhalla`
  is published for future use; third-party signature sets are out of scope ([malware-scanning.md](../malware-scanning.md) R-MAL-012).
- **No settings, no vulnerability adapter, no package ownership, no exposure narrowing by package** (the path patterns of an endpoint
  apply as to any repository).

Acceptance criteria: unit (`TestR_ARCH_010_Conformance` of the format, milliseconds): the kit passes with exactly these skips, each with
its reason.

### R-CLAMAV-009 · The tests and what they cost
Priority: **P1** · Covers: quader-only

Each behaviour once, at the cheapest tier that shows it (CLAUDE.md "Tests stay runnable"). The format adds no end-to-end leg and nothing to
another format's leg.

| Tier | What | Run time | Data |
|---|---|---|---|
| Unit (`internal/formats/clamav`) | the descriptor and the system-only refusals, the routes, `Classify`, the header and the level table, the members, `WriteError`'s table, the view, the kit with its skips | under a second | containers of hundreds of bytes built by the test |
| Integration (area `malware`) | the requests of `freshclam` 1.5.5 replayed in its order by Go's client against `upstream/clamav` on the stack with **ClamAV definition updates** on: `daily.cld` `404`; `daily.cvd` with `Range: bytes=0-511` and an `If-Modified-Since` before the commit `206` and 512 bytes, with one after it `304`; the whole `daily.cvd` `200` with its SHA-256; `daily-<n>.cvd.sign` `200` and that of a version not stored `404`; after a new set is recorded, the same first request `206` with the new version; anonymous on `public`; on `internal` `401` with the `Basic` challenge without a token and `200` with a token as the Basic password; the `503` of a request figure; an external user's `404` | about 4 s | three test containers of a few hundred bytes |
| Integration (area `upstream`) | the switch, the level while off and on, the refused delete and create (R-REPO-075, R-CLAMAV-001) | about 2 s | none |
| Measured once, not a test | a real `freshclam` 1.5.5 (the pinned image) against the endpoint of the owner's installation after the release that builds it: `PrivateMirror` at `public`, a run on an empty directory (each database fetched whole with its `.sign`, verified by `freshclam`), a second run ("up-to-date"), and, if the owner chooses a lower level (Q-REPO-415), a run with a token in the URL and the log line that shows it ([Q-CLAMAV-7](#q-clamav-7--the-real-freshclam-is-measured-once-against-the-real-definitions)) | minutes, once | ClamAV's real databases, about 110 MiB, on the owner's installation |

What every leg gains: the unit run under a second, the integration shard that runs `malware` about 4 s and the one that runs `upstream`
about 2 s. With 25 formats the cost is the same: the format is one more kit run of milliseconds.

## Decisions

Each question below was decided by the development agent from the listed options, and the owner reviews the choices at the walkthrough.

### Q-CLAMAV-1 · One package per database

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A package per database (`main`, `daily`, `bytecode`), a version per container version (chosen) | `daily` · `28190` | The unit `freshclam` updates and ClamAV publishes; each version's metadata is one header's; `main` is stored and shown once for every set that names it | The set a verdict names is not a version of the repository: the set in use is the row's, marked on the versions |
| (b) One package, a version per set (`main-63.daily-28190.bytecode-339`) | The layout before | The scanners' unit | A long name per version; one version shows three headers; `main` repeats in every version |
| (c) A package per set | | | Nothing groups the sets |

**Choice:** (a). **Rationale:** the screens show what the definition files are, and the set stays where it is used, on the database row and
on each verdict. The development agent would revisit this if the scanners needed to load a set that is not the one in use. **Applied in
the spec:** R-CLAMAV-002, R-CLAMAV-007.

### Q-CLAMAV-2 · The routes are of the kind Metadata

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: `daily.cvd` at the endpoint is a different file after every update; the format contract's `Download` is "immutable bytes for a
path" ([developer/formats.md](../../developer/formats.md#what-a-bastion-needs-of-a-format)), and a `Download` route is admitted by the byte
figures and held by the kit to `429` and `403`.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `Metadata` for both routes (chosen) | A document of the version in use | True to the contract: the bytes under the path change; no byte figure refuses a download; the kit's checks 16 and 17 do not apply, so no answer of the format has to be a `403` or a `429` | The usage store counts the bytes as a document's, not a download's |
| (b) `Download` for both | | Counted as downloads | Breaks the contract's rule for `daily.cvd`; the kit would demand `429` and `403` in the format's shape |
| (c) `Metadata` for `.cvd`, `Download` for `.sign` | Exact | The signature file's name does carry its version | One `Download` route brings checks 16 and 17 back for a 9 KB file |

**Choice:** (a). **Applied in the spec:** R-CLAMAV-004, R-CLAMAV-005, R-CLAMAV-008.

### Q-CLAMAV-3 · A refusal is 404 or 503, never 403 or 429

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `Forbidden` as `404`, `RateLimited` as `503` with `Retry-After`, in the format's `WriteError` (chosen) | One place for every stage | No client stops for a day or four hours; every stage of the request, the chain's included, renders through it | A person with `curl` reads the reason in the body, not in the status |
| (b) `429` with `Retry-After`, `403` kept | The general statuses | Honest statuses | A `403` stops a client for 24 hours; a `429` for 4 hours where libcurl is older than 7.66 |
| (c) Keep every refusal away from the endpoint (no limits, no levels) | | Nothing to render | A public endpoint with no request figure |

**Choice:** (a). **Rationale:** the statuses are for `freshclam`, and the body still names the reason. The development agent would revisit
this if ClamAV's client stopped treating `403` and `429` as a cool-down. **Applied in the spec:** R-CLAMAV-005.

### Q-CLAMAV-4 · The format is system only, a fact of the catalog

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `catalog.Facts.SystemOnly`: no create, not in the creation form, refused with a remote or virtual class or an `Upload` route (chosen) | A contract addition (ADR-225) | The UI's rule "simple by default" holds: nobody is offered a repository kind they cannot fill; every layer reads one fact | An addition to the frozen surface |
| (b) A local class users may create | A plain format | No contract change | An empty repository nothing can write, offered in every creation form |
| (c) The core refuses the format by its name | No fact | No contract change | The core would know a format by name, which the catalog exists to avoid |

**Choice:** (a). **Applied in the spec:** R-CLAMAV-001; [decisions.md](../../decisions.md) ADR-225.

### Q-CLAMAV-5 · The member files are read by the worker after the signature verified

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The database task reads the container's tar after its header's signature verified, under the bounds of `untrusted` (chosen) | Members from the container's own signed index, line counts | What the owner asked for, every piece of metadata; the parsed bytes are the publisher's, which the signature proves before a byte of the tar is read | The worker, which holds the installation's secrets, decompresses a signed archive of up to 90 MB a few times a day |
| (b) The updater reads the members and lists them | No parsing in the worker | Parsing where no secret is | The metadata would come from the pod the worker trusts least |
| (c) The header only | No tar | Least work | The members, which ClamAV's `sigtool` shows too, would be missing |

**Choice:** (a). **Applied in the spec:** R-CLAMAV-003; [malware-scanning.md](../malware-scanning.md) R-MAL-033.

### Q-CLAMAV-6 · `PrivateMirror` only, no patch files

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Whole containers, the setup names `PrivateMirror` (chosen) | No DNS record, no patches | What ClamAV's guide of private mirrors names for a mirror kept by `freshclam`; nothing Quader does not have is asked | A client fetches a whole `daily.cvd` (about 22 MiB) per new version, from the company's own installation |
| (b) Serve patch files too, for `DatabaseMirror` | A copy of the publisher's network | Less traffic inside the company | Patches would have to be fetched as well, the updater would need `ScriptedUpdates yes`, and the stored daily would become a `.cld` without a container signature (Q-MAL-93) |

**Choice:** (a). **Applied in the spec:** R-CLAMAV-006, R-CLAMAV-008.

### Q-CLAMAV-7 · The real `freshclam` is measured once, against the real definitions

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. It sets aside
the first plan of a case in the end-to-end set `vuln`.

Context: a stock `freshclam` checks the signature of every container it fetches against ClamAV's own key; the only containers that pass are
the publisher's, about 110 MiB, which no test may download (Q-MAL-90). The set of the profile `vuln` is unsigned (`.cud`), which the endpoint
does not serve as a `.cvd`, and a `.cvd` the tests could write is refused by `freshclam` and by the scanners' `clamd` alike.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The requests replayed by Go at the integration tier, and the real `freshclam` run once against the owner's installation after the release, its outcome written into R-CLAMAV-009 (chosen) | The protocol held where it is cheapest; the real client measured on real data | Every answer the endpoint owes is held on every push; the real run proves the whole path once, `.sign` and the client's verification included | A change of `freshclam`'s requests in a later release is seen at the review of a feature line of the pin (Q-MAL-80), not on a push |
| (b) A case in `e2e (vuln)` with test containers | One tier more | Runs the real binary on every run of the set | It can show only the requests before `freshclam`'s signature check fails, which (a) holds at a cheaper tier |
| (c) A case with ClamAV's real files | | The whole path | A download of 110 MiB from ClamAV's network per run, against its rules for automated clients |

**Choice:** (a). **Applied in the spec:** R-CLAMAV-009.

## What this does not cover

The gaps of the endpoint are on [security/clamav-definitions.md](../../security/clamav-definitions.md#what-this-does-not-cover).
