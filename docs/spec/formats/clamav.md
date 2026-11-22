# ClamAV definitions format

Status: the format, its data port, the endpoint, the switch and the API are built (2026-11-21, [developer/clamav.md](../../developer/clamav.md)),
and so are the updater and the database task that fills the repository with verified containers, marks the set in use and cleans up
(R-MAL-010, R-MAL-032 to R-MAL-034), and the screens: the switch in the admin area, the registry's section with the one line of the public
and the token variant and its section **Updater**, the databases, their versions and what each holds (R-UI-187, R-UI-188, R-UI-194). Built
since as well: the client certificates of R-CLAMAV-010 (the endpoint key `auth.client_certificates`, the certificate subject and its rule,
its records, the chart's `ingress.tlsPassthrough`, the read-only lines of the fold **Visibility**) and the setup of the certificate and of
the plain download of R-CLAMAV-006 ([developer/clamav.md](../../developer/clamav.md#the-client-certificates)). The measurement of
R-CLAMAV-009 against the owner's installation is not made yet; those on kind clusters of the development agent are recorded in
[developer/clamav.md](../../developer/clamav.md#measured-on-a-kind-cluster). The owner's words of 2026-11-21: "Außerdem möchte ich, dass wir das freshclam artefakt als
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
| The endpoint that admits a ClamAV installation by its client certificate: the endpoint keys, the chart's passthrough, the certificate subject | R-CLAMAV-010 on this page; [endpoints.md](../endpoints.md) R-EP-008, R-EP-010; [authorization.md](../authorization.md) R-AUTHZ-002 |
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
- The format declares no signatures ([signatures.md](../signatures.md) R-SIG-002,
  [Q-SIG-96](../signatures.md#q-sig-96--the-clamav-definitions-declare-no-signature-their-containers-signature-is-the-imports)): the
  verification of a container is this format's own metadata (R-CLAMAV-003), decided by the import before the container is stored.

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
  (`internal/formatdata/clamav`, table `clamav_version`, migration 00094). A commit at another path, or one without its payload, is
  refused ([Q-CLAMAV-17](#q-clamav-17--a-commit-without-its-metadata-is-refused)). The cleanup removes items through the item store, and
  `OnItemsRemoved` marks the version's files as removed and keeps the side row and the version row as history (R-MAL-034,
  [Q-CLAMAV-15](#q-clamav-15--the-version-row-stays-as-history-when-its-files-go)).

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
| build time | the header's field 9 (seconds; field 2, the text, kept as written) | the date and time, and the age: now minus the build time; a `daily` built more than 7 days ago is marked **Outdated**, the age past which ClamAV's engine warns "The virus database is older than 7 days!", which it checks for `daily` alone ([api-and-ui.md](../api-and-ui.md#q-ui-912--only-daily-is-outdated-by-its-age-as-clamav-warns-only-about-daily) Q-UI-912) |
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
past the bounds; web unit (R-UI-194's spec): **Outdated** on a `daily` past 7 days and not at 7, never on `main` or `bytecode`.

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
  answers `404` as for a repository that does not exist ([repositories-and-proxy.md](../repositories-and-proxy.md) R-REPO-075). Public
  by default, a level below it selectable as a detail (the owner's decision of 2026-11-21,
  [Q-REPO-415](../repositories-and-proxy.md#q-repo-415--who-reads-the-clamav-definitions-when-they-are-switched-on)); at `internal`
  and `public` a ClamAV installation is also admitted by its client certificate on an endpoint that admits certificates (R-CLAMAV-010).
- The malware gate never asks about an item of a system repository (R-MAL-013), and the vulnerability block has nothing to block: the
  format has no vulnerability adapter.
- **Through a Bastion** ([bastion.md](../bastion.md#r-bas-056--the-clamav-definitions-through-a-bastion) R-BAS-056; specified, not
  built): the endpoint is served to the anonymous caller and to tokens, a container is kept in the Bastion's cache whatever its size,
  `freshclam`'s check with `Range` and `If-Modified-Since` is answered from it, and the Bastion's own rate-limit refusals of the format
  are `503`, never `429` (the format's `catalog.Facts.Bastion`). The client certificates of R-CLAMAV-010 do not reach a Bastion
  ([Q-BAS-176](../bastion.md#q-bas-176--the-clamav-definitions-through-a-bastion-anonymous-and-tokens-no-client-certificate)).

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

- On an endpoint that admits client certificates (R-CLAMAV-010) a client without a certificate, with an expired one or with one of
  another CA gets no status at all: its TLS handshake fails before a request is read, which `freshclam` counts as a failed attempt.
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

- **One line**, the address written without its trailing slash, which `freshclam` adds itself
  ([Q-CLAMAV-16](#q-clamav-16--the-snippet-writes-the-address-without-its-trailing-slash)). `PrivateMirror` replaces the publisher's network, its DNS record and its patch files; existing `DatabaseMirror` lines may
  stay, `PrivateMirror` wins. The scheme is written out: `freshclam` reads a `PrivateMirror` without one as `http://`. `ScriptedUpdates`,
  `DNSDatabaseInfo` and `Checks` need no change (the checks are the client's own, 12 a day by default; Quader serves each in one request
  while the copy is current).
- **The certificate.** Where the installation's certificate is issued by an internal CA, the client must trust it: on Linux the
  environment variable `CURL_CA_BUNDLE=/etc/pki/quader-ca.pem` *(example)* for `freshclam` (for the systemd unit a drop-in with
  `Environment=`), or the CA in the system store; on macOS and Windows the system store. `freshclam.conf` has no option for it.
- **A level below Public** (the owner's decision of 2026-11-21,
  [repositories-and-proxy.md](../repositories-and-proxy.md#q-repo-415--who-reads-the-clamav-definitions-when-they-are-switched-on)
  Q-REPO-415), two ways, shown as two variants of the guide behind the disclosure **Authenticate** (the plain path at Public needs
  neither):
  - **With a token:** the client sends a Quader token as the password of the URL, any user name:
    `PrivateMirror https://clamav:${QUADER_TOKEN}@quader.example.com/api/clamav/upstream/clamav`. The guide says plainly that `freshclam`
    logs the whole URL, the token with it, at the level `INFO` of its log, and that this way is undocumented by ClamAV; the token is one
    of a service account made for the purpose, with no grant beyond reading this repository.
  - **With a client certificate** (R-CLAMAV-010), shown only while an endpoint admits client certificates: `freshclam` 1.2 or later,
    the address of that endpoint in the line, and the certificate, its key and the CA in the environment of `freshclam`, which
    `freshclam.conf` has no option for:

    ```ini
    # /etc/clamav/freshclam.conf (example)
    PrivateMirror https://clamav.quader.example.com/api/clamav/upstream/clamav

    # /etc/systemd/system/clamav-freshclam.service.d/quader.conf (the unit's name is the distribution's; example)
    [Service]
    Environment=FRESHCLAM_CLIENT_CERT=/etc/clamav/tls/client.crt
    Environment=FRESHCLAM_CLIENT_KEY=/etc/clamav/tls/client.key
    Environment=CURL_CA_BUNDLE=/etc/pki/quader-ca.pem
    ```

    The guide says that the certificate must be issued by a CA the endpoint admits (the operator names it), that the key file is
    readable by the user `freshclam` runs as and nobody else, that an encrypted key needs `FRESHCLAM_CLIENT_KEY_PASSWD` in the same
    environment (where it is as readable as the unit), that the certificate works at Internal and Public and not at Private, and that
    nothing of the certificate is written into `freshclam`'s log. The guide names the address of every endpoint on which a certificate
    reads the repository now, and says without one that the operator sets one up
    ([Q-CLAMAV-18](#q-clamav-18--the-guide-names-the-endpoints-on-which-a-certificate-reads-the-repository-now)).
- **Without an updater** ([Q-CLAMAV-14](#q-clamav-14--an-installation-without-an-updater-downloads-the-three-files-from-fixed-addresses)):
  an installation that runs no `freshclam` (a `clamd` fed by a script, a host that takes a copy) downloads the three databases from
  fixed addresses, which always serve the version in use (R-CLAMAV-004), into its database directory and reloads `clamd`:

  ```sh
  # example; the directory is the distribution's
  cd /var/lib/clamav
  for db in main daily bytecode; do
    curl -fsS -o "$db.cvd.part" "https://quader.example.com/api/clamav/upstream/clamav/$db.cvd" && mv "$db.cvd.part" "$db.cvd"
  done
  clamdscan --reload
  ```

  The guide shows it behind the disclosure **Without freshclam**. With a token the script hands `curl` the header as configuration on
  standard input, as the generic format's snippets do ([generic.md](generic.md#q-generic-9--curl-receives-the-token-as-configuration-on-standard-input)
  Q-GENERIC-9), so neither a log nor the process list shows it; with a client certificate it adds `--cert`, `--key` and `--cacert` and
  uses the endpoint's address. Each run fetches the whole files, about 110 MiB; the
  signed `.cvd` is verified by `clamd` when it loads. A ClamAV that enforces FIPS limits also needs each `<db>-<version>.cvd.sign`,
  whose name carries the version the repository's page shows; the guide names it and the plain script leaves it out.
- **What the guide covers** (the required topics of check 8): what the repository is and that Quader's own scanning uses the same set;
  `configure` (the line, the CA; the plain download for an installation without an updater); `authenticate` (only where the level
  needs it, the two ways above); `install` (run `freshclam` once and read "daily.cvd updated" or "is up-to-date"); `troubleshooting`:
  a `404` for every database (the switch is off, or the level hides it from the caller: a certificate at Private), a `401` (the token),
  "Can't verify database integrity" (a file changed on the way: compare its SHA-256 with the repository's page), a curl error such as
  "SSL certificate problem" or "alert certificate required" on the endpoint of the certificates (no certificate, an expired one, or
  one of a CA the endpoint does not admit), a client that waits after a refusal of another server (`freshclam.dat` holds a cool-down
  until it ends; Quader never causes one).

Acceptance criteria: unit (the kit's check 8, milliseconds): the template and the guide for every reader variant (public, token,
client certificate, without an updater), no credential outside `${QUADER_TOKEN}`, no private key in any variant; web unit (R-UI-188's
spec): the snippet shows only while the switch is on, the certificate variant only while an endpoint admits client certificates.

### R-CLAMAV-010 · A ClamAV installation admitted by its client certificate
Priority: **P1** · Covers: quader-only

The owner's decision of 2026-11-21, "Visibility des Endpunkts: b + zusätzlich Client-Zertifikate"
([repositories-and-proxy.md](../repositories-and-proxy.md#q-repo-415--who-reads-the-clamav-definitions-when-they-are-switched-on)
Q-REPO-415), with his goal: Quader is the company's central endpoint for ClamAV, and nothing further stands in front of a ClamAV
installation. A stock `freshclam` 1.2 or later presents a TLS client certificate on every request when the environment variables
`FRESHCLAM_CLIENT_CERT` and `FRESHCLAM_CLIENT_KEY` are set (`FRESHCLAM_CLIENT_KEY_PASSWD` for an encrypted key); `freshclam.conf` has no
option for it, and `freshclam` sends no token and no header of its own (read in ClamAV 1.5.5's source and manual on 2026-11-21).

- **An endpoint of its own whose TLS ends at Quader**
  ([Q-CLAMAV-8](#q-clamav-8--a-client-certificate-reaches-quader-on-an-endpoint-of-its-own-whose-tls-ends-at-quader)): a further endpoint
  ([endpoints.md](../endpoints.md) R-EP-001) of the surface `packages` with `tls.client_ca_file`, the CAs whose certificates are
  admitted (R-EP-004), and `auth.client_certificates: true` (R-EP-010). An edge that ends TLS cannot hand a client's certificate on, so
  the endpoint is reached through TLS passthrough at the ingress (`endpoints.<name>.ingress.tlsPassthrough`, R-EP-008) or through a
  Service of its own (`endpoints.<name>.service.type: LoadBalancer`). Quader never reads a certificate, or the outcome of a check, from a
  header. In the chart's values (every value an example unless marked):

  ```yaml
  endpoints:
    clamav:
      port: 8444
      baseUrl: https://clamav.quader.example.com   # the host the ClamAV installations name
      surface: [packages]                          # default
      exposure: all                                # with rules: a rule that names upstream/clamav (R-EP-012)
      auth:
        clientCertificates: true                   # default false
      tls:
        existingSecret: quader-clamav-tls          # a pair for clamav.quader.example.com that the clients trust
        clientCaSecret: company-device-ca          # key ca.crt: the CAs whose certificates are admitted
      ingress:
        enabled: true
        className: haproxy
        tlsPassthrough: true                       # default false
      networkPolicy:
        from: [{namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: ingress-haproxy}}}]
  ```

- **Who is admitted**
  ([Q-CLAMAV-11](#q-clamav-11--a-missing-expired-or-foreign-certificate-is-refused-in-the-handshake)): the endpoint's TLS requires a
  certificate that chains to one of the CAs of `tls.client_ca_file` and is valid at the handshake (Go's `RequireAndVerifyClientCert`,
  R-EP-004). A connection without one, with an expired one, one not valid yet, or one of another CA fails in the handshake, before any
  request is read: no status reaches the client, and `freshclam` counts a failed attempt and tries again at its next check, never a
  cool-down (R-CLAMAV-005). Nothing changes for a request on any other endpoint.
- **As what: the certificate subject**
  ([Q-CLAMAV-9](#q-clamav-9--a-certificate-holder-is-a-subject-of-its-own-that-reads-at-internal-the-repositories-that-admit-certificates)):
  a request on such an endpoint that carries none of the credentials of
  [identity-and-authentication.md](../identity-and-authentication.md) R-AUTH-001 items 1 to 3 is the certificate subject, principal
  kind `certificate`: no principal row, no group, no grant, no role, no membership, no owner record, never named by a policy
  (`422 principal_not_allowed`), neither internal nor external ([authorization.md](../authorization.md) R-AUTHZ-030). The decision point
  decides it by one fixed rule ([authorization.md](../authorization.md) R-AUTHZ-002): `read` of a repository that admits certificate
  readers while that repository's artifacts are `internal` or `public`; never `populate`, a write or a capability; the endpoint's
  exposure bounds it as it bounds every subject (R-EP-013). A refusal is the `404` of a repository the caller cannot see. In the words
  told to the owner: a valid certificate counts as a reader of the level internal, on these repositories and on no other.
  - **Which repositories admit certificate readers**
    ([Q-CLAMAV-13](#q-clamav-13--the-admission-is-a-key-of-the-endpoint-and-a-fact-of-the-repository-built-for-upstreamclamav-only)):
    `upstream/clamav` while **ClamAV definition updates** is on, and no other. The grant index derives the fact
    (`repoInfo.certificateReaders`) from the declaration's flag `Clients` and `upstream_repository.clients_enabled`
    ([repositories-and-proxy.md](../repositories-and-proxy.md) R-REPO-075); nothing new is stored.
  - **The level `private`** admits members and grants only. A certificate is neither, so the certificate subject is refused there and
    the admin page says so (R-UI-188); a certificate is never bound to a service account.
  - **A token wins.** A request on that endpoint that carries a token is decided by the token and the endpoint's token rules
    (R-AUTH-001, R-EP-010); a refused token never falls through to the certificate.
- **The CAs and their rotation**
  ([Q-CLAMAV-10](#q-clamav-10--the-cas-are-the-endpoints-client-ca-bundle-from-a-secret-reloaded-without-a-restart)): one PEM bundle,
  one or more CAs, from the Secret `tls.clientCaSecret` (key `ca.crt`), mounted as a volume and rendered as `tls.client_ca_file`. The
  listener's reloader reads it every `server.tls.reload_interval` ([architecture.md](../architecture.md) R-ARCH-035): a CA added to the
  Secret admits its certificates, and a CA taken out refuses them, from the next handshake on, without a restart; connections already
  open keep what they negotiated until they close. Its earliest expiry is
  `quader_tls_certificate_expiry_timestamp_seconds{kind="listener",listener="client:clamav"}` with the bundle's file as `name`. The
  operator of the installation changes it, through the chart; no screen of Quader edits a CA.
- **Answers.** Every request that reaches HTTP on the endpoint carries a verified certificate, so a `401` happens only for a token that
  is refused; the rest is the table of R-CLAMAV-005: a certificate subject refused by the level, by the switch or by the exposure gets
  `404`, a request figure `503` with `Retry-After`, and nothing answers `403` or `429`.
- **What is recorded**
  ([Q-CLAMAV-12](#q-clamav-12--the-certificates-subject-stands-where-a-user-stands-and-its-serial-where-a-token-stands)): the
  certificate's subject (the leaf's subject as an RFC 4514 string, at most 255 bytes, cut at a UTF-8 boundary with `…[truncated]`)
  stands where the user of a token stands, and its serial (lowercase hexadecimal) where the token stands:
  - the request log ([operations-and-observability.md](../operations-and-observability.md) R-OPS-010): `principal` the subject,
    `principal_type` `certificate`, `principal_id` and `token_id` `null`, `auth_method` `client_certificate`, `client_cert_serial` the
    serial and `client_cert_issuer` the issuer as an RFC 4514 string under the same bound;
  - the usage store ([usage-and-limits.md](../usage-and-limits.md) R-LIM-001, R-LIM-027): the subject kind `certificate` with the
    subject as `subject_ref` and the serial as `credential_ref`, so a renewed certificate of the same subject keeps the subject's budget
    and shows as a second credential; `limits_subject` is `certificate:<subject>`;
  - an endpoint rate-limit class keyed `principal` keys the certificate subject on its subject (R-EP-015);
  - an audit event of the request (`authz.denied`) names the actor of the kind `certificate` with the subject and the serial.
  - A refused handshake reaches none of them: Go's server writes `http: TLS handshake error from <address>: <reason>` to the process log
    at `WARN` (`httpserver.Server`'s `ErrorLog`), with the TCP peer's address
    ([H-1180](../../security/clamav-definitions.md#h-1180--a-refused-handshake-leaves-only-a-line-of-the-process-log)).
- **The client** is set up by R-CLAMAV-006, the variant "With a client certificate".
- **Another format later** uses the same admission by letting its repositories admit certificate readers; the endpoint key, the
  subject, its rule and its records stay as they are. Nothing of it is built for another format.
- **Not through a Bastion.** A Bastion's TLS ends at the Bastion and its forward reaches the default endpoint, which admits no
  certificates; Quader reads no certificate from a header. A ClamAV installation outside the company updates through a Bastion at
  `public` or with a token, and with its certificate only from this endpoint
  ([bastion.md](../bastion.md#h-1247--the-certificate-admission-of-the-clamav-definitions-does-not-reach-a-bastion) H-1247).

Acceptance criteria:

- Unit (`internal/config`, milliseconds): `auth.client_certificates: true` without `tls.client_ca_file`, on the default endpoint, and
  with `auth.anonymous: true` each stop startup naming the key.
- Unit (`internal/auth`, milliseconds): on an endpoint with the key, a request whose connection carries a verified chain and no token
  is the certificate subject with its subject, issuer and serial; with a `Bearer` token it is the token's subject; with a refused token
  it is a `401`; on an endpoint without the key the same connection is decided as today.
- Unit (`internal/authz`, milliseconds): the rule over a table: `read` of `upstream/clamav` at `internal` and `public` while on
  admitted; at `private`, while off, of another repository, a write, `populate` and a capability refused with `DenyHidden`; an exposure
  rule that hides `upstream/clamav` refuses it.
- Unit (`internal/limits`, `internal/httpserver`, milliseconds): the subject `certificate` with its reference and credential; the
  request log's fields, the subject cut at 255 bytes.
- Chart (`test/chart`, seconds): the cases of R-EP-008 for `auth.clientCertificates` and `ingress.tlsPassthrough`.
- Integration (area `malware`, in R-CLAMAV-009's case, about 1 s more): a CA, a second CA and three client certificates made by the test
  with `crypto/x509` (valid, expired, of the second CA), an endpoint `clamav` on `127.0.0.1:0` with the first CA and the key: the valid
  certificate reads `daily.cvd` at `internal` (`200`), at `private` gets `404`, and with the switch off `404`; without a certificate,
  with the expired one and with the second CA's the handshake fails; the usage store holds the subject `certificate` with the serial; a
  request log record names `principal_type` `certificate`.

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
| Unit (`internal/config`, `internal/auth`, `internal/authz`, `internal/limits`, `internal/httpserver`) | the client certificates of R-CLAMAV-010: the endpoint key's validation, the certificate subject and the token that wins, the fixed rule's table, the usage subject, the request log's fields | under a second | certificates made by the test |
| Chart (`test/chart`) | `auth.clientCertificates` and `ingress.tlsPassthrough` rendered and refused (R-EP-008) | about 2 s (an estimate: a few `helm template` runs) | none |
| Integration (area `malware`, the same case) | the client certificates end to end on an endpoint of the test with a CA of the test: a valid certificate `200` at `internal`, `404` at `private` and while off; no certificate, an expired one and one of another CA refused in the handshake; the usage row and the request log's subject (R-CLAMAV-010) | about 1 s | two CAs and three certificates made by the test, the same containers |
| Integration (area `upstream`) | the switch, the level while off and on, the refused delete and create (R-REPO-075, R-CLAMAV-001) | about 2 s | none |
| Measured once, not a test | a real `freshclam` 1.5.5 (the pinned image) against the endpoint of the owner's installation after the release that builds it: `PrivateMirror` at `public`, a run on an empty directory (each database fetched whole with its `.sign`, verified by `freshclam`), a second run ("up-to-date"); a run with a token in the URL and the log line that shows it; and, after the release that builds R-CLAMAV-010 and where his installation has an endpoint that admits certificates, a run with `FRESHCLAM_CLIENT_CERT` and `FRESHCLAM_CLIENT_KEY` and one without them, with the line `freshclam` logs for the refused handshake; and the plain download of R-CLAMAV-006 once, into the directory of a `clamd` 1.5.5 that then reloads ([Q-CLAMAV-7](#q-clamav-7--the-real-freshclam-is-measured-once-against-the-real-definitions)) | minutes, once | ClamAV's real databases, about 110 MiB, on the owner's installation |

What every leg gains: the unit run under a second, the integration shard that runs `malware` about 5 s, the one that runs `upstream`
about 2 s, and the chart job about 2 s. With 25 formats the cost is the same: the format is one more kit run of milliseconds, and the
client certificates are tested once, on this format, because the admission is the same for any format that would use it.

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

### Q-CLAMAV-8 · A client certificate reaches Quader on an endpoint of its own whose TLS ends at Quader

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. It is the
default the owner was told on 2026-11-21 before he left.

Context: the owner's installations put HAProxy Ingress in front of `api` and `packages`; it ends the client's TLS and speaks TLS to
Quader (`ingress.backendTLS`), so a client's certificate never reaches Quader today. Read in HAProxy Ingress's documentation of its
configuration keys on 2026-11-21: `ssl-passthrough` is a key of a host, puts that host in TCP mode and leaves TLS to the backend, with no
path inspection and one backend; `auth-tls-secret` with `auth-tls-verify-client` makes the controller check a client certificate and send
`X-SSL-Client-SHA1`, `X-SSL-Client-DN` and `X-SSL-Client-CN` (and `X-SSL-Client-Cert` with `auth-tls-cert-header`) to the backend, and
answers `495` or `496` for a missing or invalid one. Whether it removes those headers when a client sends them was not found in that page.
Today's `tls.client_ca_file` of a further endpoint pins the edge proxy's own certificate (R-EP-004, R-EP-008).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A further endpoint whose TLS ends at Quader, reached through TLS passthrough of the ingress or a Service of its own (chosen) | Quader verifies the certificate itself with Go's TLS; the chart renders the passthrough per controller | The check, its outcome and the certificate's fields are Quader's own; the CA is in one place and reloads like every TLS material (R-ARCH-035); no header is trusted for a credential; the listener, its reloader and the per-endpoint CA exist today | A host name of its own for the endpoint; behind a passthrough Quader sees the controller's address as the client's ([H-1179](../../security/clamav-definitions.md#h-1179--behind-a-tls-passthrough-the-clients-address-is-the-ingress-controllers)); Traefik's Ingress cannot pass TLS through |
| (b) The ingress verifies the certificate and forwards the outcome in headers that Quader accepts from trusted proxies only | `auth-tls-*` at HAProxy Ingress, `X-SSL-Client-*` read by the authentication stage | One host for everything; the client's address stays visible | A credential read from a header: whoever reaches the port from a trusted address can claim any subject; the CA, its rotation and the expiry check live in the controller, a different set of names and statuses per controller (`495`, `496`); the subject and serial are parsed from header text |
| (c) Both | Two ways in | Each installation chooses | Two code paths, two sets of tests and two security cases for one decision |

**Choice:** (a). **Rationale:** a certificate is a credential, and the place that verifies a credential is the one that decides on it;
everything (a) needs exists today but the key that turns a verified certificate into a subject. The development agent would revisit
this if an installation cannot give the endpoint a host of its own.
**Applied in the spec:** R-CLAMAV-010; [endpoints.md](../endpoints.md) R-EP-008, R-EP-010.

### Q-CLAMAV-9 · A certificate holder is a subject of its own that reads at internal the repositories that admit certificates

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. It keeps the
default the owner was told: a valid certificate counts as a reader of the level internal, with no principal.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The certificate subject: no principal, decided by a fixed rule, `read` of a repository that admits certificate readers at `internal` and `public`, not at `private` (chosen) | One subject kind and one rule in the decision point, like the system subjects | Nothing to administer per installation of ClamAV: the CA is the list of who is in; it cannot read anything else, whatever an administrator binds or whatever other repository is `internal`; `private` keeps its meaning, members only | A ClamAV installation cannot read a `private` repository by its certificate (a token of a member's service account does); all certificates of a CA are alike |
| (b) The certificate's subject bound to a service account (a table of bindings and a screen) | A certificate holder is a principal | Grants, membership and `private` work as for any principal | A binding per installation, a screen, a renamed subject on every renewal policy of the CA; the administration the owner's goal ("nothing further stands in front of a ClamAV installation") avoids |
| (c) The endpoint's CA admits at every level | The CA is the only door | Simplest | `private` would mean nothing on the endpoint |
| (d) An internal subject (R-AUTHZ-030) | As a signed-in user | No new kind | It would read every `internal` repository the endpoint exposes, of every project |

**Choice:** (a). **Applied in the spec:** R-CLAMAV-010; [authorization.md](../authorization.md) R-AUTHZ-002;
[identity-and-authentication.md](../identity-and-authentication.md) R-AUTH-001.

### Q-CLAMAV-10 · The CAs are the endpoint's client CA bundle, from a Secret, reloaded without a restart

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `tls.client_ca_file` of the endpoint, one PEM bundle of one or more CAs from the chart's `tls.clientCaSecret`, read by the listener's reloader (chosen) | TLS material like every other | Built today (`httpserver.NewListenerTLS` reads the bundle through the reloader); rotates without a restart (R-ARCH-035); its expiry is a metric; the operator, who owns the company's PKI, holds it | An administrator of Quader cannot change it from a screen |
| (b) CAs uploaded by an administrator in the admin area | Stored in PostgreSQL, sent to every replica | Changed without the operator | A trust anchor of the TLS layer in the database, read by every replica's handshake; a second way beside the files every other piece of TLS uses |
| (c) The installation's internal CA (`server.internal_tls`) | The CA of Quader's own pods | Nothing to configure | It issues Quader's pods, not the company's hosts; it would admit every pod of Quader as a ClamAV installation |

**Choice:** (a). **Applied in the spec:** R-CLAMAV-010; [endpoints.md](../endpoints.md) R-EP-004, R-EP-008.

### Q-CLAMAV-11 · A missing, expired or foreign certificate is refused in the handshake

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: `freshclam` treats a failed connection and a `401` as a failed attempt and a `403` as a cool-down of 24 hours (R-CLAMAV-005).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The endpoint requires a verified certificate in the handshake (`RequireAndVerifyClientCert`, as `tls.client_ca_file` does today) (chosen) | No request without a certificate reaches HTTP | Nothing new; nothing of Quader runs for a stranger; `freshclam` tries again at its next check | The client reads a TLS error, not a status with a reason; Quader writes only a line of its process log ([H-1180](../../security/clamav-definitions.md#h-1180--a-refused-handshake-leaves-only-a-line-of-the-process-log)) |
| (b) `verify_if_given`, then `401` for a request without a certificate | A status | A person with `curl` reads a problem document | A certificate that does not verify still fails the handshake, so only the missing case changes; the endpoint then serves token callers and strangers the same routes |
| (c) `404` for a request without a certificate | Hidden | — | Reads as "no definitions here" to the person who forgot the certificate |

**Choice:** (a). **Applied in the spec:** R-CLAMAV-010, R-CLAMAV-005, R-CLAMAV-006 (its troubleshooting).

### Q-CLAMAV-12 · The certificate's subject stands where a user stands, and its serial where a token stands

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The subject as the usage store's subject and the request log's `principal`, the serial as the credential, the issuer in the request log (chosen) | A subject kind `certificate` | A renewed certificate keeps its holder's budget and history, as a user keeps them across tokens; the record names a host in words an operator recognises | Two CAs that issue the same subject share one budget; a subject longer than 255 bytes is cut |
| (b) The certificate's SHA-256 as the subject | Exact | Unique | Every renewal is a new subject with a fresh budget; the overview shows a hash, not a host |
| (c) One subject for every certificate | `certificate` | Nothing per certificate | No way to tell which installation fetched what, or to limit one |

**Choice:** (a). **Applied in the spec:** R-CLAMAV-010; [usage-and-limits.md](../usage-and-limits.md) R-LIM-001, R-LIM-027;
[operations-and-observability.md](../operations-and-observability.md) R-OPS-010.

### Q-CLAMAV-13 · The admission is a key of the endpoint and a fact of the repository, built for upstream/clamav only

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `auth.client_certificates` of an endpoint, a subject kind, and a fact of the repository that only `upstream/clamav` has while its switch is on (chosen) | General where it costs nothing, narrow where it admits | Another format later turns the fact into a setting of its repositories and changes nothing else; today no other repository is readable by a certificate | A key of the endpoint that serves one repository today |
| (b) A setting "client certificates admitted" on every repository now | A field on every settings page | Ready for every format | A setting for a client that only `freshclam` needs today, in front of every plain user (CLAUDE.md "simple by default") |
| (c) A listener for ClamAV only | `/api/clamav/` on a port of its own | Smallest | A second kind of listener beside the endpoints, with its own TLS, metrics and chart objects; nothing reusable |

**Choice:** (a). **Applied in the spec:** R-CLAMAV-010; [endpoints.md](../endpoints.md) R-EP-010.

### Q-CLAMAV-14 · An installation without an updater downloads the three files from fixed addresses

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. It follows
the owner's goal that Quader is the company's central endpoint for ClamAV.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The guide names the three addresses `<endpoint>/{main,daily,bytecode}.cvd` and a plain download with `curl` (chosen) | The routes of R-CLAMAV-004, which always serve the version in use | Nothing new to build; a script can carry a token as a header that no log shows, or a client certificate | Each run fetches the whole files; the `.sign` files a FIPS-limited ClamAV needs carry the version in their names |
| (b) Aliases `<db>.cvd.sign` without a version | One more route | A script needs no version | A path no client of ClamAV asks for, to save a script one `sigtool --info` |
| (c) Only `freshclam` | The one line | Simplest | An installation without `freshclam` would need a server in front of it, which the owner's goal rules out |

**Choice:** (a). **Applied in the spec:** R-CLAMAV-006.


### Q-CLAMAV-15 · The version row stays as history when its files go

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-MAL-034 keeps a version's metadata after the cleanup removed its files, and R-UI-194 lists such a version as **Files removed** on
the repository's package screens, which list the generic version rows.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The version row stays with no file; the side row, keyed by the version row, carries `files_removed_at`; the task deletes both after `malware.rescan.cycle` (chosen) | One row per version on every screen | The package screens list the history without a read of their own; the side row goes with its version by the foreign key | A version without a file stands in the generic rows until the history ends |
| (b) The version row goes with its files; the side row, keyed by repository, database and version, stays | Generic rows only for stored versions | The generic rows say what is stored | The screens need a second list for the history; a side row with a foreign key to the repository joins the purge's list |

**Choice:** (a). **Applied in the spec:** R-CLAMAV-002, R-CLAMAV-007; [developer/clamav.md](../../developer/clamav.md).

### Q-CLAMAV-16 · The snippet writes the address without its trailing slash

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: `freshclam` joins its `PrivateMirror` and a file name with a slash of its own (read in ClamAV 1.5.5's source), and a path with
an empty segment is refused by the dispatcher as `400 invalid_path`. A setup template sees the registry URL with its trailing slash.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A template function `trimslash` beside `schemeless` and `hostname` (chosen) | The template writes `{{trimslash .RegistryURL}}` | One function, usable by any later client that joins a path itself | A function more in the setup templates' set |
| (b) A field of `SetupData` with the address without the slash | Data | No function | A field of the frozen contract for one format |
| (c) Serve `//daily.cvd` too | Tolerate the empty segment | No template change | Every route and the dispatcher's clean path would have to admit an empty segment |

**Choice:** (a). **Applied in the spec:** R-CLAMAV-006.

### Q-CLAMAV-17 · A commit without its metadata is refused

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `OnCommit` refuses a commit at a path outside the layout and one without the payload of its kind (`rows.Container`, `rows.Signature`) whose database and version agree with the path (chosen) | The only writer states what it stores | No version reaches the screens that they cannot describe; a wrong path fails the task's run, not a screen | A commit by any other code needs the payload too |
| (b) The hook writes what the path gives and leaves the metadata empty | Lenient | Any commit succeeds | Versions without build time, level or verification |

**Choice:** (a). **Applied in the spec:** R-CLAMAV-002.

### Q-CLAMAV-18 · The guide names the endpoints on which a certificate reads the repository now

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the guide of R-CLAMAV-006 shows the certificate's way "only while an endpoint admits client certificates", and the line names
that endpoint's address. The guide's templates see `GuideData` only, which held no endpoint but the request's, and the format contract
is frozen (R-ARCH-025).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A field `GuideData.CertificateURLs`: the registry URL on every endpoint with `auth.client_certificates` on which a certificate subject reads the repository now, asked of the decision point (chosen) | An addition to the contract, the version unchanged ([ADR-232](../../decisions.md#adr-232--the-guide-data-names-the-addresses-on-which-a-client-certificate-reads-the-repository)) | The guide shows the way exactly where it works: the switch on, the level internal or public, the endpoint's exposure admitting it; the decision point answers it, not a second rule | One more field of a frozen contract |
| (b) A placeholder host in the guide, the admin page lists the real addresses | No contract change | Nothing new | The reader of **Set up client** copies a line that names no real host, and sees the way where no endpoint admits certificates |
| (c) Every endpoint with the key, whatever the repository's level and the exposure | A list of the configuration | Simplest | A way that answers `404` at Private or under an exposure that hides the repository |

**Choice:** (a). **Applied in the spec:** R-CLAMAV-006. Code: `GuideData.CertificateURLs` in
[guide.go](../../../internal/formats/guide.go), `certificateURLs` in [guide_api.go](../../../internal/api/v1/guide_api.go),
`Access.ReadableByCertificate` in [access.go](../../../internal/api/edge/access.go); the kit runs every guide over a variant with an address
([formattest/guide.go](../../../internal/formats/formattest/guide.go)).

## What this does not cover

The gaps of the endpoint are on [security/clamav-definitions.md](../../security/clamav-definitions.md#what-this-does-not-cover).
