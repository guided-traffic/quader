# Ansible Galaxy repositories

The Ansible Galaxy format (`internal/formats/ansible`) serves collections to `ansible-galaxy` from local, remote and virtual repositories,
and roles by translation from a remote. This page is for a contributor to the format: the address and the routes, the files, the leaf
packages, the schema, the scheme `Token` of the authentication stage and the data port. The specification is
[formats/ansible.md](../spec/formats/ansible.md); the decision for the contract is
[ADR-191](../decisions.md#adr-191--the-format-contract-gains-the-route-flag-for-the-galaxy-token-scheme-and-the-hub-token-exchange-is-an-optional-interface-of-the-remote-adapter).

Built: all three classes. The format registers with its route table, the grammars, the classification of paths, the Galaxy error shape,
the settings schema and the data port with its tables; the scheme `Token` is in the authentication stage. The local class (this page)
publishes, serves the read operations and the download, records the import tasks, deletes with tombstones and replays them, reindexes,
sweeps old tasks, takes the signatures a client posts and reads its rows for the package screens. The remote class with its mirror and the
hub exchange ([ansible-remote.md](ansible-remote.md)), the virtual class ([ansible-virtual.md](ansible-virtual.md)), a version's
documentation ([ansible-docs.md](ansible-docs.md)), the setup, the guide, the screens and the end-to-end set are built too. Every class
registers a handler for every route it serves; the 501 `ansible_not_implemented` of a route without one is the safety net of a new class
and no route answers it today. [What is built and what is not](#what-is-built-and-what-is-not) names what is left.

## What has to be in your head first

- **One format, `ansible`**, at `/api/ansible/<projectKey>/<repoKey>/` (Q-ANS-1). The repository URL itself is what a client is configured
  with: it answers the discovery document, and every other route is below it (`v3/…`, `v1/…`, Q-ANS-3).
- **Collections are the stored artifact; roles exist by translation** on remote and virtual repositories only (Q-ANS-6). A local repository
  lists `v3` alone in its discovery document and has no `v1` API; a request below `v1/` answers 404 in the `v1` shape with the sentence that
  says why ([errors.go](../../internal/formats/ansible/errors.go)).
- **The route table is complete and frozen** ([routes.go](../../internal/formats/ansible/routes.go)): eleven routes, each with the classes that
  serve it. The table grows with the classes registered by `class_*.go` files (a route none of whose classes is registered is left out), and
  a route that several classes serve is one route whose handler decides by class: `Match` does not know the class (Q-DEB-45). A handler is
  registered by class and route name ([handlers.go](../../internal/formats/ansible/handlers.go)); one that no file registered answers 501.
- **Every route declares `TokenScheme`.** `ansible-galaxy` sends a configured token as `Authorization: Token <token>`; the stage reads that
  scheme only on a route that declares it, from the dispatcher's own match, and nowhere else (ADR-191, Q-ANS-4).
- **A version is stored and served as written; it is compared by semantic-version precedence with build metadata ignored.** The data port
  orders a list by `sort_key`, a byte string whose memcmp order is the precedence ([version](../../internal/formats/ansible/version/version.go)).
- **The documents are rendered from one neutral form** ([galaxydoc](../../internal/formats/ansible/galaxydoc/galaxydoc.go)): a database row, a
  document an upstream sent and a merged member document all become a `Version` and are rendered with the repository's own addresses, so
  the three classes serve the same bytes for the same version, and nothing of the upstream's addresses reaches a client.
- **An archive is read once, as a stream** ([collection](../../internal/formats/ansible/collection/archive.go)): every member goes through a
  digest, `MANIFEST.json`, `FILES.json` and `meta/runtime.yml` are kept, and the archive is accepted only when it is consistent. It is never
  extracted. The bounds are the core's (Q-ANS-30).

## The files

```
internal/formats/ansible/
├── format.go             # Descriptor, class registry (registerClass), New
├── routes.go, match.go   # the route table and the matchers of the ten protocol shapes
├── handlers.go           # registerHandler(class, route, build); the 501 of a route without one
├── paths.go              # Classify, canonical paths, PackageDocumentPath, VersionPath, CanonicalPath
├── names.go              # the package-name rule (Normalize)
├── errors.go             # WriteError: errors[] below v3/ and at the root, {"default"} below v1/
├── inspect.go            # Inspect: the archive reader and the path/identity check
├── publish.go            # the publish: the multipart reader, the order of the checks, the import task, the repeat
├── handlers_local.go     # the read operations, the download and the task read of the local class
├── hooks.go              # OnCommit (the version, its row and task), OnItemsRemoved (the tombstone), the fill and removal hooks of other classes
├── view.go               # the package screens' adapter; servecheck.go: the withheld archive
├── bind.go               # the replica: the ansible: section, quader_ansible_publish_total, registerBinder for the other classes
├── store.go              # the data port: Store = LocalStore + RemoteStore; ViewStore for the screens
├── ansibletest/          # the in-memory twin of the port for the unit tests and the conformance kit
├── settings.go, settings.schema.json  # the ansible member of the repository document (authUrl, clientId)
├── class_local.go        # the local class registers itself
├── rows/                 # leaf: the values that cross the data port, the CollectionUUID
├── version/              # leaf: semver 2.0.0, Compare, SortKey
├── collection/           # leaf: the archive reader (ReadArchive), the name rule, runtime.yml; collectiontest builds archives for tests
└── galaxydoc/            # leaf: Render*/ParseUpstream* of the Galaxy documents; testdata/ holds the golden documents
internal/formatdata/ansible/   # the data port: port.go, tables.go, jobs.go, local_store.go, view_store.go, reindex.go, journal_replay.go, remote_store.go
internal/app/wire_ansible.go   # the blob reads, the reindex handler, the management store
test/integration/ansiblelocal/ # the local class over HTTP on the stack
internal/auth/                 # stage.go, auth.go: the scheme Token (galaxy_token_test.go)
internal/api/dispatch/         # TokenSchemeRoute beside APIKeyRoute
internal/api/v1/ansible_*.go   # the management operations (tasks, tombstones, signatures, the documentation)
internal/config/ansible.go     # the ansible: section
```

| File | Responsibility |
|---|---|
| `handlers_remote.go`, `remote*.go` | The remote class and the roles: [ansible-remote.md](ansible-remote.md). `mirror.go` is the mirror's lister and `hub.go` the exchange of a hub behind single sign-on |
| `class_virtual.go`, `handlers_virtual.go`, `virtual*.go`, `merge/` | The virtual class: [ansible-virtual.md](ansible-virtual.md) |
| [`setup.go`](../../internal/formats/ansible/setup.go), [`guide.go`](../../internal/formats/ansible/guide.go), `guide/*.md` | The client surface: the eight setup templates (ids `ansible-galaxy.<name>.<section>`, Q-ANS-48; the token only as the variable `ANSIBLE_GALAXY_SERVER_QUADER_TOKEN`, Q-ANS-22; the netrc line uses the template function `hostname`) and the guide sections per class, which appear with the class the format registers; kit check 8 runs over them |

## The routes

| Route | Method and path (below the repository URL) | Kind | Action | Classes |
|---|---|---|---|---|
| `ansible.root.get` | `GET ` (empty) | Computed | read | all |
| `ansible.collection.get` | `GET v3/collections/{ns}/{name}/` | Metadata | read | all |
| `ansible.versions.list` | `GET v3/collections/{ns}/{name}/versions/` | Metadata | read | all |
| `ansible.version.get` | `GET v3/collections/{ns}/{name}/versions/{version}/` | Metadata | read | all |
| `ansible.version.docs` | `GET v3/collections/{ns}/{name}/versions/{version}/docs-blob/` | Metadata | read of the wiki ([ansible-docs.md](ansible-docs.md)) | all |
| `ansible.artifact.get` | `GET v3/artifacts/collections/{file}` | Download | read | all |
| `ansible.collection.publish` | `POST v3/artifacts/collections/` | Upload | deploy | local, virtual |
| `ansible.import.get` | `GET v3/imports/collections/{id}/` | Metadata | read | local, virtual |
| `ansible.roles.find` | `GET v1/roles/` | Volatile | read | remote, virtual |
| `ansible.role.versions` | `GET v1/roles/{id}/versions/` | Metadata | read | remote, virtual |
| `ansible.role.archive` | `GET v1/roles/archives/{id}/{file}` | Download | read | remote, virtual |
| `ansible.roles.search` | `GET v1/search/roles/` | Volatile | read | remote, virtual |

The trailing slash is optional on every path and no route redirects: a redirect would drop the `Authorization` header of some clients.

## The screens' publish (R-ANS-061)

**Upload collection** takes three calls (Q-ANS-140): `createAnsibleUploadToken`
([ansible_upload.go](../../internal/api/v1/ansible_upload.go)) issues the caller's personal access token of five minutes scoped to the
repository and its deployment target; the dialog sends the archive with it to `publishPath`, the client's publish route, which the role
`packages` serves (on `web` and `all` the same process); `getAnsibleImportTask` reads the task back on the management API. The role `api`
builds no format host and makes no shadowing probe (R-ARCH-001, Q-ARCH-294), which is why the publish is not run there. The client of the
three calls is `HttpAnsibleApi.publish` ([ansible-api.ts](../../web/src/app/api/ansible-api.ts)), whose `galaxyFailure` reads a refusal of the
route, in the Galaxy shape, as a problem with the management API's code. The dialog is
[ansible-upload-dialog.ts](../../web/src/app/features/repositories/content/ansible-upload-dialog.ts); it sends the file through the
XHR-based client the deploy dialog uses ([upload-transfer.ts](../../web/src/app/api/upload-transfer.ts)) and shows the task with the view
the tab Import log uses ([ansible-import-task.ts](../../web/src/app/features/packages/ansible-import-task.ts)). A page that names no repository (a
role's, a namespace's) opens it through `collectionUpload` ([ansible-upload-targets.ts](../../web/src/app/features/repositories/content/ansible-upload-targets.ts)),
which lists the Ansible Galaxy repositories of the page's scope the reader may deploy into. Tests: integration `ansiblelocal`
`screen_publish_test.go` and `screen_session_test.go` on a replica of the role `api` and one of the role `packages` (the guard of the split
roles), `ansiblevirtual` `screen_publish_test.go`; the unit tests of the client and the dialog, and the dialog's states
(`ansible-upload.states.spec.ts`, `make ui-states`).

## Signatures a client posts (signatures.md R-SIG-031)

A local version takes detached OpenPGP signatures of its `MANIFEST.json` through the management API
(`addAnsibleVersionSignature`, `removeAnsibleVersionSignature`, [ansible_signatures.go](../../internal/api/v1/ansible_signatures.go)). The
port ([signatures_store.go](../../internal/formatdata/ansible/signatures_store.go)) reads the signature without a key
([untrusted/openpgp.go](../../internal/formats/untrusted/openpgp.go) `OpenPGPSignatureFacts`: one packet, the issuer fingerprint, the hash
prefix against `MANIFEST.json`, which `collection.Archive.Manifest` keeps), stores it in `ansible_version_signatures` (migration 00096),
raises the package's revision, records it in the signature store with the source `upload` at the archive's item and writes the audit event
`ansible.signature.added` in the same transaction (`ansible.signature.removed` for a removal; Q-SIG-107). The version document
renders `signatures[]` from the rows ([virtual.go](../../internal/formats/ansible/virtual.go) `versionOfRow`, `postedSignatures`). Tests:
integration `ansiblelocal` [signatures_test.go](../../test/integration/ansiblelocal/signatures_test.go).

## Paths and names

A protocol path is what a client requests; a canonical path is where the item lives (`collections/<ns>/<name>/<ns>-<name>-<version>.tar.gz`,
and for a remote the stored documents beside it: `collection.json`, `versions.json`, `<version>.json`, `api-root.json`, `roles/…`).
`Classify` maps a name or a version that does not parse to metadata of no version, so it costs a refetch and never freezes a list. The
package name is `<ns>.<name>`, each part `[a-z0-9_]{1,64}`, not starting with `_`, with a letter. `Names.Normalize` folds upper case (a remote may
serve what the client's grammar `[a-zA-Z0-9_]` accepts) and a publish refuses it (`invalid_name`). `classifyRole` in [paths.go](../../internal/formats/ansible/paths.go)
is the one place the remote class's work package may refine when it settles whether the name machinery takes `role/<id>` (R-ANS-020).

## Errors

`WriteError` renders `{"errors":[{"status","code","title","detail"}]}` below `v3/` and at the root and `{"default": …}` below `v1/`. The code is the
registry code without the prefix: `ansible_version_exists` is `version_exists`; `unauthenticated` is `not_authenticated`, `forbidden` is
`permission_denied`, `malformed_request` is `invalid_request`, and a 502 or 503 of an upstream is `upstream_unavailable` (Q-ANS-29). The codes
are in [codes.go](../../internal/errs/codes.go) (`CodeAnsible…`), the table that holds their kinds is the registry there.

## The scheme Token

`formats.Route.TokenScheme` marks a route that takes `Authorization: Token <quader token>`; `auth.EndpointRules.TokenSchemeRoute` reports
whether the request reaches one, answered by `Dispatcher.TokenSchemeRoute` (the same match as `APIKeyRoute`, [dispatcher.go](../../internal/api/dispatch/dispatcher.go));
`credentialHeader` in [stage.go](../../internal/auth/stage.go) returns the secret with `MethodGalaxyToken` (`galaxy_token`, the label of
the request log, of `quader_auth_attempts_total` and of the audit event) only then. The stage asks the route at most once and only for a
header with that scheme. Everywhere else the header is "not a Quader token" and is rejected without a lookup. The tests are in
[galaxy_token_test.go](../../internal/auth/galaxy_token_test.go).

## The schema and the data port

[00068_ansible_collections](../../internal/meta/postgres/migrations/00068_ansible_collections.sql) and
[00069_ansible_import_tasks](../../internal/meta/postgres/migrations/00069_ansible_import_tasks.sql) create the tables of R-ANS-011; the
table of the announced digests of a remote, [00070_ansible_upstream_versions](../../internal/meta/postgres/migrations/00070_ansible_upstream_versions.sql), comes with the remote class, and the facts of the roles a remote read
(`ansible_roles`, the migration `ansible_roles`, R-ANS-056) with the group `RoleStore` of the port ([roles_store.go](../../internal/formatdata/ansible/roles_store.go)). The port is `Store` in [store.go](../../internal/formats/ansible/store.go),
implemented by [internal/formatdata/ansible](../../internal/formatdata/ansible/port.go). It speaks repository ids and the values of the `rows` leaf
(Q-ANS-33), carries no revision column (Q-ANS-31), and its reindex step is `RebuildRows`, the enqueue being the `Reindexer` the endpoint
already finds (Q-ANS-32). The job kinds `ansible-reindex` and `ansible-import-task-sweep` are registered in [jobs.go](../../internal/formatdata/ansible/jobs.go).

## The local class

**Publish** ([publish.go](../../internal/formats/ansible/publish.go)). The body is spooled once (multipart in either order of `file` and `sha256`, or the archive as the
raw body, Q-ANS-34); the digest is compared before the inspection; `Inspect` reads the archive ([inspect.go](../../internal/formats/ansible/inspect.go)); a refusal
is a failed task and 202; then `Authorize`, `CheckOwnership`, the immutability and tombstone verdict (`numberTaken`), the shadowing check and
`Ingest().Commit`. `OnCommit` ([hooks.go](../../internal/formats/ansible/hooks.go)) repeats the verdicts under the package row lock, claims the name, inserts the
generic version, writes the row through the port (`PutVersion`, with `package.published` and no journal entry) and records the completed task, all in the
commit transaction. A commit of identical content runs no hook and the handler records the repeat's task itself. The task's messages are the stored line,
the reader's warnings, the import's own warnings of a missing changelog, README, `requires_ansible` and licence
([import_warnings.go](../../internal/formats/ansible/import_warnings.go), R-ANS-062; the changelog is the generator's finder, `Generator.HasChangelog`) and the
generator's warnings.

**Reads** ([handlers_local.go](../../internal/formats/ansible/handlers_local.go)). Every document is computed from the rows per request with a weak `ETag` of its
bytes (Q-ANS-35); a caller whose grants hide versions sees a list cut from the versions it may read (`formats.CallerVersions`). The download is the stored item through
`Items().Serve` with `Content-Type: application/gzip`.

**Removal.** `OnItemsRemoved` turns the removal of an archive item into the removal of the version: the generic version, the row (if the cascade of a permanent
delete did not take it), the tombstone keyed by `version.Number` (build metadata dropped) and the journaled `ansible.version.deleted`. The serve check refuses an
archive whose number is tombstoned.

**Reindex, sweep, replay.** `POST …/reindex` is `Store.Reindex` ([reindex.go](../../internal/formatdata/ansible/reindex.go)), which the endpoint finds as `api/v1.Reindexer`; the
job `ansible-reindex` steps through the archives by path, reads each manifest through `rows.ArchiveReader` (implemented in
[wire_ansible.go](../../internal/app/wire_ansible.go)), rewrites or creates the rows, skips tombstoned numbers and raises the revision of what it changed. The task
`ansible-import-task-sweep` deletes tasks older than `ansible.import_task_keep`. The replay handlers are in [journal_replay.go](../../internal/formatdata/ansible/journal_replay.go).

**How the other classes hook in.** A class file registers `registerClass`, its handlers with `registerHandler(class, route, build)` and `registerBinder` (per-replica metrics), all from `init`.
The remote adapter implements `FillCommitter` (the commit of a cached archive) and `RemovalHook` (the removal of its items), which `OnCommit` and
`OnItemsRemoved` find on it; a virtual adapter may implement `RemovalHook` too.

## Tests

| Tier | Where | What |
|---|---|---|
| Unit | the leaf packages | semver precedence against a reference and fuzz; the archive reader against crafted archives and fuzz (`collectiontest` builds them); the documents against golden files (`go test ./internal/formats/ansible/galaxydoc -update` rewrites them) |
| Format | `internal/formats/ansible` | the descriptor, the route table, the matchers, `Classify`, the error shapes, the settings, the real handlers and hooks over the twin (`publish_test.go`), the serve check, the conformance kit with a publish seed |
| Stage | `internal/auth`, `internal/api/dispatch` | the scheme with and without the declaration, the metric label, the cookie; the dispatcher's answer to the stage ([tokenscheme_test.go](../../internal/api/dispatch/tokenscheme_test.go)) |
| Integration | [test/integration/ansiblelocal](../../test/integration/ansiblelocal) | the publish over HTTP in both orders and as a raw body, the counting of rows, the tasks, immutability, tombstones and their replay, credentials, `TestR_AUTHZ_007_AnsibleActionMap`, the reindex and the sweep, the package screens, two replicas without Valkey |

## How to add a Galaxy field to the documents

1. The document: add the member in [galaxydoc.go](../../internal/formats/ansible/galaxydoc/galaxydoc.go) (a version's metadata members are in `metadataKeys`, in the order the
   Galaxy writes them) and rewrite the golden files with `-update`; read the diff.
2. The row: a field a document shows from the archive goes into `collection.Archive` and its reader, into `rows.VersionRow` and the column of a new migration, into
   `queries/ansible_local.sql` and `make generate`, and into `Store.PutVersion` and `RebuildStep`, so the reindex restores it.
3. The tests: the crafted archive in `collectiontest`, the golden file, and the integration test that reads the document.

## What is built and what is not

Built: the skeleton, the leaves, the scheme, the tables, the data port, the local, remote and virtual classes, the setup, the guide, the
screens (the import tasks and tombstones tabs, the package details; `web/src/app/features/repositories/ansible-*.ts`), the mirror, the hub
exchange and the end-to-end set with the two ansible-core clients ([e2e.md](e2e.md#the-ansible-tests)); the virtual and mirror rows run (H-868 narrows what they do not prove). Not built yet: the restore from the trash (H-852). The deploy dialog of the raw files is not offered (Q-ANS-34); the screens publish with **Upload collection** (R-ANS-061).
