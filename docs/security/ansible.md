# Ansible Galaxy: credentials, archives and immutability

Perspective: what a caller can do to a local Ansible Galaxy repository through the protocol, what stands between an uploaded archive and the stored
rows, which credentials the Galaxy clients present and how the stage reads them, and what keeps a deleted version number from coming back. The
mechanisms are the scheme `Token` of the authentication stage ([stage.go](../../internal/auth/stage.go),
[dispatcher.go](../../internal/api/dispatch/dispatcher.go)), the archive reader ([collection](../../internal/formats/ansible/collection/archive.go)), the
publish ([publish.go](../../internal/formats/ansible/publish.go)), the commit and removal hooks ([hooks.go](../../internal/formats/ansible/hooks.go)), the serve
check ([servecheck.go](../../internal/formats/ansible/servecheck.go)), the data port ([internal/formatdata/ansible](../../internal/formatdata/ansible/local_store.go)) and its
journal replay ([journal_replay.go](../../internal/formatdata/ansible/journal_replay.go)). The requirements are
[R-ANS-006 to R-ANS-013, R-ANS-029, R-ANS-031 and R-ANS-061](../spec/formats/ansible.md); the developer view is [ansible.md](../developer/ansible.md). The remote
and virtual classes have their own pages when they are built.

## Credentials

| Client configuration | On the wire | What the stage does |
|---|---|---|
| `token=` in `ansible.cfg`, `ANSIBLE_GALAXY_SERVER_<NAME>_TOKEN`, `--token` | `Authorization: Token <token>` | reads it as a Quader token on a route that declares `TokenScheme`, with the checks of a Bearer token; the method is `galaxy_token` |
| `username=` and `password=`, `~/.netrc` | `Authorization: Basic` | the password is the token, the user name is ignored; needs `auth.clients.basic.enabled` |
| a Bearer token from another tool | `Authorization: Bearer` | as everywhere |

**The scheme `Token` is a credential on the Galaxy routes only.** The stage runs before the dispatcher, so it asks the dispatcher whether the
request reaches a route that declares the scheme (`Dispatcher.TokenSchemeRoute`, the same match the dispatcher serves) and asks it at most once and only
for a header with that scheme. On any other route, the management API included, the header is "not a Quader token" and is rejected without a
lookup, so the scheme cannot be used to slip a token past a route that never agreed to read it. A malformed token, an unknown, revoked or expired
one, one of a type the endpoint does not accept and one bound to another endpoint are 401, as for Bearer; none falls through to anonymous or to the
session cookie, which is never validated next to an `Authorization` header.

**A 401 carries the Basic challenge.** Every 401 of the format answers `WWW-Authenticate: Basic realm="quader"` in the Galaxy shape, so a client that retries on
a challenge sends the credential it can send. The token is never in a URL, a log line or an error detail: the format's error renderer takes the
text of the error and nothing of the request.

## The archive

An uploaded archive is untrusted until the reader has accepted it ([collection](../../internal/formats/ansible/collection/archive.go), R-ANS-010,
R-ANS-031). It is read once, as a stream, never extracted: names with `..`, absolute names and backslashes, hard links, devices, sparse files, repeated
names and data after the archive's end are refused; the entry count, the inflated bytes, the compression ratio of every entry and the size of the three documents it
keeps are bounded, and the reader ends with the context's deadline. The archive must be consistent: `MANIFEST.json` names the digest of `FILES.json`,
`FILES.json` lists every file with its digest and no file of the archive is missing from it or unlisted. `meta/runtime.yml` is decoded into a node tree and
walked for one scalar: an anchor, an alias, a tag, a merge key or more than 1,000 nodes ignore the field, and nothing of it is ever decoded into Go
values. A refusal is a failed import task the client prints and stores nothing; a publish that fails before the inspection (a missing credential, a wrong
`sha256`, a body over the limit) is an HTTP error.

**Symbolic links** ([Q-ANS-91](../spec/formats/ansible.md#q-ans-91--a-symbolic-link-that-stays-inside-a-collection-is-admitted-and-never-followed)).
`ansible-galaxy collection build` writes a symbolic link for a file or directory that stays inside the collection, and collections on the public Galaxy
hold them (`ansible.posix` 2.2.2 holds ten). Quader admits a link whose target, resolved against the link's own directory, is relative and stays inside
the archive, and refuses, naming the entry, a link to an absolute path, one that leaves the archive, one resolved through another link and an entry below
a link. What an administrator should know: Quader stores and serves such an archive unchanged, through a publish and through a remote, and **never follows
a link itself**: every read it makes of an archive (the two documents, `meta/runtime.yml`, the README, the documentation's sources, the signature check)
reads regular entries only, and a path reached only through a link is absent to it. The links take effect only when a client extracts the archive, and
there the client's own containment check is what protects the machine that installs: ansible-core refuses a link whose target leaves the collection's
directory (read in ansible-core 2.17.14 and 2.21.6). A client older than that check, or another tool that extracts the archive without one, is outside
what Quader can protect.

**The identity is the archive's.** The name and the version are read from `MANIFEST.json` and checked against the grammar of R-ANS-002 (lower case, a
semantic version); the permission on the name, its ownership and the shadowing check of a first publish are decided on that identity before the commit and
decided again inside the commit transaction under the package row's lock. The collection's `dependencies` are resolved by the client through the same
server, so claims, pin rules and the shadowing check keep an internal name away from a public member (R-ANS-022).

## Immutability and the tombstone

A version is never accepted again with other bytes: the same bytes are a repeat (a new task, nothing else changes), other bytes are 409
`version_exists`, and two versions that differ only in build metadata are one number. A deletion removes the item, the row and the generic version, and
leaves a tombstone keyed by the number with the digest that was deleted; a tombstoned number is 409 `version_tombstoned` whatever the bytes, and is checked
before the stored version, so a number a restore's replay withheld is refused as withheld. Only `manage` clears a tombstone, and the clearing is an audit
event.

**The journal.** `ansible.version.deleted` and `ansible.tombstone.cleared` are written in the transaction of the change and flagged for the revocation
journal. A restore from an older backup brings a deleted version back; the replay of the journaled deletion withholds its number again, drops the restored
row and deletes nothing from the store, and the serve check refuses the archive that stayed (it is one statement by primary key on a download of a local
repository). An entry is a claim, not a fact: it acts only on a local Ansible Galaxy repository the entry names and on a collection name, a version and a digest of
the valid forms; an entry about another repository finds nothing, an incomplete one is skipped. A forged entry can cost the availability of one version until an
administrator clears its tombstone, never data or access. The replay of the clearing frees the number as the administrator did.

## What the management API adds

**The screens' publish is the client's publish** (R-ANS-061, [Q-ANS-140](../spec/formats/ansible.md#q-ans-140--the-screens-publish-sends-the-archive-to-the-clients-publish-route-with-a-token-the-management-api-issues)). The role `api`, where the
browser's session lives, issues the caller a personal access token of five minutes whose scope is `read` and `deploy` on the repository (and a
virtual repository's deployment target) and nothing else (`createAnsibleUploadToken`, [ansible_upload.go](../../internal/api/v1/ansible_upload.go)).
The browser sends the archive with it as `Authorization: Bearer` to the client's own publish route, which the role `packages` serves with every
check of `ansible-galaxy collection publish`, the shadowing probes of a first version among them; `packages` sees a token, never the session,
and `api` makes no probe and fills no cache (R-ARCH-001). The task is read back through the management API. There is no second road into the
commit. The archive is the body as it is: the browser reads nothing of it but its name and size, and the file name is not compared with the
manifest ([Q-ANS-142](../spec/formats/ansible.md#q-ans-142--the-name-and-the-version-come-from-the-manifest-and-the-file-name-is-not-compared)).

**How the screen's upload is protected.**

| What the request carries | Answer | Where it is decided |
|---|---|---|
| The session cookie without the CSRF token, with a wrong token, or with an `Origin` that is not the endpoint's, asking for the upload token | `403 csrf_failed` in the management API's problem shape; no token is issued | The authentication stage ([stage.go](../../internal/auth/stage.go) `bySession`, `CheckCSRF`) on the role `api`, before the router and the operation |
| The session with its CSRF token | A token of the caller for five minutes, scoped to the repository, created via `ui`, the audit event `token.created`; answered with `no-store` | The operation's `deploy` check, then the token service |
| The upload token on the publish route | The publish; the event `package.published` names the caller and the upload token | The role `packages`: the client's publish route with every check |
| The upload token on another repository | Refused by the token's scope | The decision point |
| The session cookie, with or without its CSRF token, on the Galaxy route `POST …/v3/artifacts/collections/` | `401` with the Basic challenge, as before the screen's upload existed: the cookie is no credential for a write below `/api/<format>/` (`sessionAllowed`) | The authentication stage and the dispatcher's coarse check |
| A body above `ansible.max_archive_bytes` | `413`, as for the client's token; a declared length is refused before the body is read | The handler's spool |
| Any of it while the installation is in read-only maintenance | `503` with `Retry-After` for the token and for the publish | The management API's write gate and the dispatcher's |

The token is a credential in the page's memory for at most five minutes; it can do nothing but read and publish into the one repository, and
it is in the caller's list of tokens, where the caller can revoke it, until it expires. Proven by
`TestR_ANS_061_TheScreensPublishRunsOnTheRolesTheChartRuns` and `TestR_ANS_061_TheScreensUploadWithASessionIsGuardedAsEveryManagementWrite`
(integration `ansiblelocal`, a replica of the role `api` and one of the role `packages`).

The import tasks are read with `read` on the repository, the tombstones are listed and cleared with `manage`. A task names the collection and the version and
nothing else of the archive; its id is a random UUID. A virtual repository shows its deployment target's tasks only to a caller who may read the virtual
repository. Tasks are deleted after `ansible.import_task_keep` by a task that touches no version.

A collection is deprecated as a whole through `PUT …/packages/{collection}/deprecation` (R-ANS-060, R-API-085), decided by the format as a
publish into the collection is: deploy on the collection document's path and the ownership of the collection, on the request's index
([deprecation.go](../../internal/formats/ansible/deprecation.go)); a collection none of whose versions the caller may read is answered as
missing. The message, at most 1,024 characters of publisher text, is kept for Quader's screens, which show it as text, and is in the
audit event `package.metadata_changed`; the collection document carries the flag alone. A remote's flag is its upstream's and cannot be set.

## What this does not cover

- [H-2180](../spec/formats/ansible.md#h-2180--a-signature-cannot-be-given-on-the-screen) - a signature cannot be given on the screen; a version uploaded into a gated repository is refused to readers until one is posted through the API.
- [H-2182](../spec/formats/ansible.md#h-2182--the-screens-upload-needs-the-packages-family-on-the-uis-own-endpoint) - the screen's upload needs the packages family on the UI's own endpoint.
- [H-2181](../spec/formats/ansible.md#h-2181--a-refusal-of-the-archives-identity-comes-after-its-bytes-were-sent) - a refusal of the archive's identity comes after its bytes were sent.

- [H-841](../spec/formats/ansible.md#h-841--collections-are-not-evaluated-for-vulnerabilities) - collections are not evaluated for vulnerabilities (no OSV ecosystem).
- [H-843](../spec/formats/ansible.md#h-843--a-publish-inspects-the-archive-inside-the-request) - the publish inspects the archive inside the request, under the core's 30-second deadline.
- [H-845](../spec/formats/ansible.md#h-845--galaxy-namespaces-are-not-entities-of-quader) - Galaxy namespaces are not entities of Quader.
- [H-851](../spec/formats/ansible.md#h-851--a-collection-archive-beyond-the-cores-reader-bounds-is-refused) - an archive beyond the core's reader bounds is refused.
- [H-852](../spec/formats/ansible.md#h-852--the-restore-of-a-version-from-the-trash-is-not-served) - the restore of a version from the trash is not served.
- [H-853](../spec/formats/ansible.md#h-853--the-readme-of-a-version-is-read-by-streaming-the-archive-up-to-it) - the README of a version is read by streaming the archive up to it.
- [H-854](../spec/formats/ansible.md#h-854--a-caller-whose-grants-hide-versions-makes-the-list-read-all-versions-of-the-collection) - a caller whose grants hide versions makes the list read all versions of the collection.
