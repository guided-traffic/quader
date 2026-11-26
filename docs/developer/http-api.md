# Management API: OpenAPI workflow

The contract is [api/openapi.yaml](../../api/openapi.yaml) (OpenAPI 3.1, R-API-002). Handlers live in
[internal/api/v1](../../internal/api/v1); the generated server interface and models in
[internal/api/v1/gen](../../internal/api/v1/gen).

## Workflow

1. Edit `api/openapi.yaml`. Every operation needs `operationId` (lowerCamelCase verb + noun), `tags`,
   `x-quader-permission` in a form the authorization step enforces (see below), and at least one problem
   response (`$ref` to the shared responses).
2. `go generate ./internal/api/...` (or `make generate`) does two things: it copies the document to
   `internal/api/v1/openapi.yaml`, which the binary embeds (`go:embed` cannot reach `../`), and it runs
   `go tool oapi-codegen -config cfg.yaml` (pinned by the `tool` directive of `go.mod`, models and
   `std-http-server` only) into `gen/api.gen.go`.
3. Implement the new method of `gen.ServerInterface` in `handlers.go`. An operation without a handler does
   not compile.
4. Run the tests. `TestDocument` fails on a stale embedded copy, an operation without a declaration, and
   a route that is registered but not documented (or the reverse); `TestAuthorizationStep` sends an anonymous
   request to every operation of the document and fails unless each non-public one answers 401.

`go generate` output is committed; `make generate-check` fails on a diff.

## Conventions

| Topic | Rule |
|---|---|
| Paths | Full path in the document (`/api/v1/system/ping`), so the generated ServeMux pattern is `GET /api/v1/system/ping` |
| Errors | `httpserver.Problem(w, r, err)` with an `*errs.Error`; never write an error body by hand. The wire members are those of `errs.Problem`: `type`, `title`, `status`, `detail`, `instance`, `code`, `requestId`, `errors` |
| JSON | `writeJSON` in `handlers.go`: `application/json; charset=utf-8`, UTC times with `Z`, empty collections as `[]`, "no value" as `null` |
| Caller | `httpserver.PrincipalFrom(ctx)`: kind, ID, name, groups and the credential that proved it (method, token ID and type, scope ceiling, endpoint binding); never read credentials from headers. Ask authorization with `edge.SubjectFrom(r)` (`v1.SubjectFrom(ctx)` delegates): it carries the principal, the epoch and the listener's endpoint, see [m1-contracts.md](m1-contracts.md) |
| Permission check | done before the handler by the authorization step of [authorize.go](../../internal/api/v1/authorize.go), from the operation's declaration; a handler adds a check only where the decision depends on the request body (R-API-008) |
| Authorization index | one per request (R-AUTHZ-015, Q-API-158): for a capability, a role or an action declaration the step calls `Beginner.Begin` ([access.go](../../internal/api/v1/access.go)) before it checks the declaration and stores the `edge.Access` holder in the request's context; the handler asks every further question through `edge.AccessFrom(ctx)` or `requestHolder`. See "One index per request" below |
| Base URL | `httpserver.InfoFrom(ctx).BaseURL`, never `Host` |
| Authorization table | `Document.Operations()` maps the route pattern to the declarations, each decoded into an `Operation.Requirement` at load |
| Registration | `Server.Register(router)` registers through `httpserver.Router` behind `authorizingMux`, which wraps every handler in the check of the operation its pattern names; the composition root then calls `Document.CheckRoutes(router.Patterns())` |

`x-quader-permission` forms the step enforces until the authz area extends it (R-API-008), and nothing else (`LoadDocument` refuses any other form,
so start-up stops before an operation can run with a declaration nobody checks):

| Declaration | Check | M0 operations |
|---|---|---|
| `{public: true}` | none | ping, the document, the problem catalog |
| `{authenticated: true}` | anonymous → 401 with `WWW-Authenticate: Bearer` | — |
| `{authenticated: true, publicSwitch: system.public_version}` | as above, unless the switch is on (`Options.PublicVersion`); the switch names are fixed in `publicSwitches()` | version |
| `{permission: <capability>}` | `Options.Access.Require(ctx, name)`: 401 for an anonymous caller who needs credentials, 403 without the capability; with `Access` nil: 401 anonymous, 403 otherwise; the name must be a capability of R-AUTHZ-010 | info, replicas (`settings:read`, `system.read` in M0) |
| `{permission: <capability>, project: path.<n>, projectRole: owner \| reader}` (`project` may be left out with `reader`) | the holder of the capability passes. Otherwise a member of the project the path names passes when its role is high enough: `reader` admits any member, `owner` an Owner through a session or an `admin: true` token (`ProjectAccess.Owns`), decided as a question without an audit event (`projectRoleAdmits`). A refused caller gets the refusal of the capability (403 and its audit event); when it is no member of the addressed project that refusal becomes the 404 of a project that does not exist (`hideProject`, R-AUTHZ-016, R-AUTHZ-026), so a project the caller cannot see is indistinguishable from a missing one, while a member whose role is too low keeps its 403 | the project, member, policy, service-account, CI-mapping and repository-configuration operations |
| `{role: <role>}` | `Access.Require(ctx, "role:<role>")`: the caller holds the system role | — |
| `{action: <action>, repository: query.<n> \| path.<n>, project: query.<n> \| path.<n>[, package: …][, path: …]}` | `ActionAccess.RequireAction` decides the action on the qualified coordinate `<project>/<repository>` built from the request (R-AUTHZ-015); the roles, policies and the token scope decide it in the engine | — |

A route whose pattern has no operation in the document is denied to everyone with 403 (and `CheckRoutes`
stops start-up for it). The binding of path parameters runs after the check. R-API-008 defines the full
grammar; every form that `parseRequirement` accepts is enforced by `decide` in `authorize.go`.

### One index per request

A request decides on one authorization index (R-AUTHZ-015, Q-AUTHZ-159, Q-API-158). Where the index is taken depends on
the declaration:

| Declaration | Index |
|---|---|
| `permission`, `role` | the step begins (`Server.begin`) before `Access.Require`; `Beginner.Begin(ctx, permission, keys...)` returns the `edge.Access` and the step stores it with `edge.WithAccess` in the request it passes on |
| `action` | the same, with the repository of the request as the key, so a repository created on another replica is known to the index first (Q-AUTHZ-133) |
| `public`, `authenticated` | none: an operation that decides nothing must not start to answer 503 while the index is unconfirmed |

`v1.Access` stays the interface the step and the stand-ins of the tests use; `NewAccess(authz.Decider)` returns the one over
the decision point, whose `Require`, `Holds` and `RequireAction` resolve the holder from the context (`edge.Resolve`) and begin a
second index only for a request that reached them without the step. A handler that decides (the repository read and list,
check-exposure, the permission checks) calls `edge.Resolve` or `requestHolder(ctx, decider, keys...)`, and passes the context
that returns to everything after, so the `s.holds` questions it asks through `Access` use the same holder. `DeciderOf` takes
the per-request form out of a service typed as `authz.Authorizer`; a value without it answers 503, it never decides on an
index of its own.

Two questions stay outside it: the rate limit's administrator exemption runs in a chain stage before the step and asks the
decision point on its own index, and `GET /api/v1/authz/me` calls `Engine.Effective`, which has no `…On` form yet.

## The `/files/` family in the document

The raw file path is part of the document (R-API-002) but not of the generated interface: `exclude-tags: [files]` in
[gen/cfg.yaml](../../internal/api/v1/gen/cfg.yaml). `LoadDocument` keeps those operations apart: `Document.Operations()` holds the
management API (`/api/v1/`), `Document.FileOperations()` the `/files/` ones, and `CheckRoutes` compares the registered patterns
with both (the multi-segment wildcard `{path...}` of the router is the parameter `{path}` of the document). The step of
[authorize.go](../../internal/api/v1/authorize.go) does not run for them: the files service decides every request itself, because it decides per
coordinate and after the body is read (Q-API-33). `apitest.Validator` looks the route of a `/files/` answer up with a single
segment path, so the answers of the files handler are validated like every other, and it keeps an encoded `/` inside its
segment (a package name such as `@acme%2Ffrontend`, R-AUTHZ-019) the way the router matches it, escaped segment by segment
(`TestR_AUTHZ_019_AnEncodedSlashStaysInItsSegment`). The UI's content routes
(`/api/v1/projects/{projectKey}/repositories/{repoKey}/content/{path}`, R-API-025, Q-UI-106) are tagged `files` as well and kept in a third table,
`Document.ContentOperations()`, which `CheckRoutes` compares with the registered patterns (`isContentPattern`); `apitest.Validator`
maps their path like a `/files/` path. The bridge between the two packages in the
unit tests: `TestRoutesAgreeWithTheDocument` in [internal/api/rawfiles](../../internal/api/rawfiles/rawfiles_test.go).

## Endpoints

The API is registered on every endpoint whose surface holds `api` (R-EP-005): the default endpoint always, a further
endpoint only by configuration (`surface: [packages, api]`). On any other endpoint `/api/v1/…` falls through to the
catch-all `404` problem, so `GET /api/v1/system/ping` on a package-only endpoint answers `404`, not `403`. A request on
an endpoint whose host check fails answers `421` `misdirected_request` before routing, whatever the path
([endpoints.md](endpoints.md)). The problem catalog lists the code (`GET /api/v1/problems/misdirected_request`).
`Document.CheckRoutes` runs for each endpoint that serves `api`.

## Operations

| Operation | Path | Access (`x-quader-permission`) |
|---|---|---|
| `getPing` | `GET /api/v1/system/ping` | public; `200 OK` while the replica serves (ready, or draining after it was ready, Q-OPS-30), `503 UNAVAILABLE` otherwise |
| `getVersion` | `GET /api/v1/system/version` | authenticated, or anonymous with `system.public_version` |
| `getSystemInfo` | `GET /api/v1/system/info` | `settings:read` |
| `listReplicas` | `GET /api/v1/system/replicas` | `settings:read` |
| `listScanners` | `GET /api/v1/system/scanners` | `settings:read` |
| `listStorageConnections`, `getStorageConnection` | `GET /api/v1/system/storage-connections`, `…/{id}` | `settings:read`; every object-store connection with its state, the registry's replicas, the queue, the capabilities, the storage keys by identifier, the answering replica's settings and the latest summary's figures, `404` for an id never seen ([blob-store.md](blob-store.md#the-storage-summary-and-the-connections-list-r-stor-028-r-stor-061), R-STOR-061) |
| `getStorageSummary` | `GET /api/v1/storage/summary` | `settings:read`; the latest snapshot of the task `storage-summary`, `404` before the first run; a read never computes (R-STOR-028, R-OPS-023) |
| `listDamagedBlobs`, `recheckDamagedBlob` | `GET /api/v1/storage/damaged-blobs`, `POST …/{sha256}/recheck` | `settings:read`, `settings:write` ([blob-store.md](blob-store.md#the-scrub-r-stor-027), R-STOR-027) |
| `getSystemOverview` | `GET /api/v1/system/overview` | `settings:read`; not opened by `system.bootstrap_anonymous_read` ([System overview](#system-overview-r-ops-004-q-ops-175)) |
| `getOpenApiJson` / `getOpenApiYaml` | `GET /api/v1/openapi.json`, `.yaml` | public; `servers[0].url` is the public base URL |
| `getProblemType` | `GET /api/v1/problems/{code}` | public; resolves the `type` URI of a problem |
| `listJobs` | `GET /api/v1/system/jobs` | authenticated; `settings:read` sees every job, any other caller the jobs it started (R-API-015) |
| `getJob` | `GET /api/v1/system/jobs/{id}` | authenticated; as above, a job the caller may not see answers `404`; `Retry-After: 2` while it has not ended |
| `cancelJob` | `POST /api/v1/system/jobs/{id}/cancel` | authenticated; `settings:write` or the caller who started the job through a session or an inheriting token (`403` for a reader who did not, and for the starter's narrowed token, Q-API-215), `409` for a finished job |
| `listTasks` | `GET /api/v1/system/tasks` | `settings:read` |
| `runTask` | `POST /api/v1/system/tasks/{name}/run` | `settings:write`; optional `Idempotency-Key`; `202` with the job, `409` with the running job |
| `getEncryptionStatus` | `GET /api/v1/system/encryption` | `settings:read` |
| `getMigrationStatus` | `GET /api/v1/system/migrations` | `settings:read`; the report of `quader migrate status` from the same code (`postgres.Report` on the replica's pool, [database.md](database.md#the-migration-report-postgresreport-quader-migrate-status-get-apiv1systemmigrations)), for the answering replica's binary; `503` without a database |
| `listFormats` | `GET /api/v1/formats` | public; 401 for an anonymous caller while no `public` level opens anything ([repositories.md](repositories.md#http-operations)); each format with its `upstreamPresets` (R-REPO-062) |
| `getRepositoryDefaults` | `GET /api/v1/projects/{projectKey}/repository-defaults?format=&class=` | `repositories:write-config`, or an Owner of the project (`projectRole: owner`); the defaults of a new repository and a free key (R-REPO-063); [repository_defaults_api.go](../../internal/api/v1/repository_defaults_api.go) renders the service's document without its key, and `registryPresets`, the installation's presets of the format for the class remote (R-REPO-076) |
| `getRegistryPresets`, `setRegistryPresets` | `GET`, `PUT /api/v1/settings/registry-presets` | `repositories:read-config`, `repositories:write-config`; the installation's registry presets as one document under `If-Match` (R-REPO-076, R-API-084, Q-API-383); [registry_presets_api.go](../../internal/api/v1/registry_presets_api.go) reads the body strictly against a preset's four members, so a credential member is `400`, and `repository.Service.SetRegistryPresets` validates, writes and audits |
| `listRepositories`, `listProjectRepositories`, `getRepository` | `GET /api/v1/repositories` (every project, `project` filter), `GET /api/v1/projects/{projectKey}/repositories`, `…/{repoKey}` | public; the handler decides what the caller sees (R-AUTHZ-016) |
| `listPackages`, `lookupPackage`, `getPackage`, `listPackageVersions`, `getPackageVersion`, `getPackageVersionReadme` | `GET /api/v1/packages`, `/lookup`, `/{packageId}`, `/{packageId}/versions`, `/{packageId}/versions/{version}`, `…/readme` | public; the read model filters by what the caller may read (Q-API-162, Q-API-165); [packages.md](packages.md) |
| `getClientSetup` | `GET /api/v1/projects/{projectKey}/repositories/{repoKey}/client-setup` | `{action: read, repository: path.repoKey, project: path.projectKey}`; the publish snippets also need `deploy` on the target; `bastion=<name>` renders on the address the Bastion reported and has no publish snippets; `path` (and `sha256`) of a generic repository fills a file's download and check, or a folder's upload (`setupFile`, R-GENERIC-017) ([packages.md](packages.md#client-setup)) |
| `getRepositoryGuide` | `GET /api/v1/projects/{projectKey}/repositories/{repoKey}/guide` | `{action: read, repository: path.repoKey, project: path.projectKey}` ([The guides of the formats](#the-guides-of-the-formats-r-api-053)) |
| `getFormatGuide` | `GET /api/v1/formats/{format}/guide` | public; 401 for an anonymous caller while no `public` level opens anything, like `listFormats` |
| `createProjectRepository`, `replaceRepository`, `updateRepository`, `deleteRepository` | `POST /api/v1/projects/{projectKey}/repositories`, `PUT`, `PATCH`, `DELETE` on `…/{repoKey}` | `repositories:write-config`, or an Owner of the project (`projectRole: owner`) |
| `moveRepository` | `POST /api/v1/projects/{projectKey}/repositories/{repoKey}/move` | `repositories:write-config` or an Owner of the source project, recent authentication; the handler asks the second half, `checkMoveTarget`: an Owner of the source must own the target as well (a target it is no member of answers 404, a lower role 403); the project service refuses with `repository_in_use` and `already_exists`; answers 200 with the document at the new address, `ETag`, `Location` and `Quader-Authz-Epoch` |
| `listProjectMembers`, `setProjectMember`, `removeProjectMember` | `GET /api/v1/projects/{projectKey}/members`, `PUT`, `DELETE …/members/{principalRef}` | `projects:read` or a member (`reader`); the writes `projects:write` or an Owner, recent authentication; the handlers map the body and the status (201 added, 200 role set, `Quader-Authz-Epoch`); the last-Owner rule (409 `last_owner`) is the project service's and `bypassLastOwner` is the caller's `projects:write` (Q-AUTHZ-217); `PUT` needs no `If-Match` (the pair is the identity) |
| `listProjectPermissionPolicies` and the item operations | `/api/v1/projects/{projectKey}/policies[/{name}]` | `policies:read`/`policies:write` or an Owner. The API names a project's policy within its project; the policy store addresses it as `<projectKey>/<name>`. `storedPolicyName`, `storedPolicy` and `viewPolicy` ([project_policies_api.go](../../internal/api/v1/project_policies_api.go)) are the whole mapping; the instance-wide list shows the same view with `project` set |
| `validatePermissionPolicy` | `POST /api/v1/authz/validate-policy` | `authenticated: true`, decided in the handler (`mayValidate`): `policies:read`, or for a body that names a `project` an Owner of it (a project the caller is no member of answers 404) |
| the project service-account operations | `/api/v1/projects/{projectKey}/service-accounts[/{name}[/disable\|/enable\|/tokens]]` | `identities:read`/`identities:write` or an Owner; the token create is `authenticated: true` and decided in the handler: an administrator, an Owner (delegated token, Q-AUTHZ-177) or a token of the account itself; the name in the route is the name within the project, the principal's stored name is `<projectKey>/<name>` |
| `getRepositoryClaims`, `putRepositoryClaims` | `/api/v1/projects/{projectKey}/repositories/{repoKey}/claims` | `{action: read\|manage, repository: path.repoKey, project: path.projectKey}`; a global claim also needs `repositories:write-config`, asked in the handler ([virtual.md](virtual.md)) |
| `getItem`, `deleteItem`, `listChildren`, `getItemStats` | `/api/v1/projects/{projectKey}/repositories/{repoKey}/items`, `/children`, `/stats` | `{action: read\|delete, repository: path.repoKey, project: path.projectKey, path: query.path}` ([files.md](files.md)) |
| `downloadContent`, `headContent`, `deployContent` | `GET`, `HEAD`, `PUT /api/v1/projects/{projectKey}/repositories/{repoKey}/content/{path}` | as the `/files/` operations with the same method, decided by the files handler; the session cookie only ([files.md](files.md#the-content-routes)) |
| `downloadFile`, `headFile`, `deployFile`, `deleteFile` | `GET`, `HEAD`, `PUT`, `DELETE /files/{projectKey}/{repoKey}/{path}` | `{action: read\|deploy\|delete, …}`, in the document but decided by the files handler |
| the signature operations (tag `signatures`) | `/api/v1/signatures/…`, `/api/v1/packages/{packageId}/versions/{version}/signatures`, `/api/v1/projects/{projectKey}/repositories/{repoKey}/signatures`, `/api/v1/projects/{projectKey}/signatures/anchors[/{id}]` | the reads of an artifact as its metadata (`public` declaration decided in the handler, or the item's `read`); anchors, blocks and the switch `settings:read` / `security-settings:write`; a project's anchors its members and its Owner ([The operations of signatures](#the-operations-of-signatures-r-sig-034)) |
| the vulnerability operations (tag `vulnerabilities`) | `/api/v1/vulnerabilities/…`, `/api/v1/packages/{packageId}/…/vulnerabilities`, `POST /api/v1/projects/{projectKey}/repositories/{repoKey}/vulnerabilities/rescan` | reads `authenticated`, narrowed in SQL by what the caller can read (filters `project` and `repository`, a qualified name; a bare key is 400; the item and the CSV carry `project`); status changes `vulnerabilities:triage`; sources `settings:read`; claimed-name matches `repositories:read-config`; rescan `manage` on the repository ([vulnerabilities.md](vulnerabilities.md#e--api-and-triage)) |

`bridgeAccess` ([access.go](../../internal/app/access.go)) puts the dev-profile switch in front of the authorization
engine ([authorization.md](authorization.md)): `system.bootstrap_anonymous_read` opens `settings:read` for `GET /api/v1/system/info` and `GET /api/v1/system/replicas` (Q-AUTHZ-71) to anonymous callers on the **default endpoint only**, and only
while that endpoint admits anonymous requests; a further endpoint that serves `api` answers 401 (Q-OPS-36). The route
check at start compares the document with the registered and the dropped patterns of each endpoint that serves `api`.

## Packages and client setup operations

The contract of R-API-029 and R-API-030 is in the document's block `M2 packages` (tag `packages`, the `PackageSummary`, `PackageList`,
`PackageVersionSummary`, `PackageVersionList`, `PackageVersion`, `PackageFile`, `PackageReadme`, `ClientSetup` and `ClientSnippet` schemas, the parameters
`PackageId` and `PackageVersionPath`). The admin UI is generated from it. The decisions are Q-API-159 to Q-API-168. The implementation, the read model
behind it, the README renderer and the setup snippets are described in [packages.md](packages.md); this page keeps the conventions the handlers rely on.

[packages_api.go](../../internal/api/v1/packages_api.go) implements the seven operations on `Options.Packages` (`PackageServices`, filled by
[wire_packages.go](../../internal/app/wire_packages.go)). A replica without the read model (the shell profile, no database, no decision point) answers
them `503 unavailable`: never an empty list, because an empty list says "no package". Every operation is declared `public` (Q-API-162) and
decides in the handler; the client setup declares `{action: read, repository: path.repoKey, project: path.projectKey}`.

- the lists use `ParseList`: the sorts and filters of the document, a cursor bound to the filter fingerprint, an unknown parameter or sort answering
  `400`; the cursor values are the sort values of the last item and the tie-breakers (name and format; version);
- the summary, the details and the README answer a strong `ETag` through `NotModified`; the summary and the details hash the bytes they send, the README
  hashes its inputs so that a request that holds the tag renders nothing;
- `GET /api/v1/formats` carries the `baseUrl` member (Q-UI-75), filled from the request's resolved information only when its source is the configuration,
  and each format's `clients` (`{id, displayName}` from `Descriptor().Clients`, Q-API-177).

## The guides of the formats (R-API-053)

The document's block `M2 guides` holds the two operations and the schemas `Guide` (with its `snippets`, R-API-070) and `GuideSection`, and `FormatClient`
sits next to `FormatDescriptor`. Decisions Q-API-175 to Q-API-179 and Q-API-310. [guide_api.go](../../internal/api/v1/guide_api.go) implements both on `Options.Packages`:

| Step | Repository guide | Format guide |
|---|---|---|
| Services | `packageServices()` and `PackageServices.Guides`, else 503 | `PackageServices.Guides`, else 503; the anonymous gate first |
| Format and client | `guideFormat` and `guideClient`: the repository's format must have a guide and the client must be one of its clients, else `400` | an unknown format is `404`; then as the repository guide, and the class must be one of the format's, else `400` |
| Addresses | `setupAddress` (shared with the client setup): the endpoint's configured base URL plus the protocol path with the key, else `503` | the same with `formats.GuideRepositoryPlaceholder` for the key |
| Caller | `requestHolder`, `publishTarget` (the target and the publish snippets only with `deploy`), `ReadableAnonymously` for `RequiresAuth` | `SubjectFrom(ctx)`: anonymous or not, its endpoint; the publish snippets for a signed-in caller of a class other than remote |
| Rendering | `renderGuide`: credentials `env`, or `none` for an anonymous caller; the setup snippets of the client through `renderSnippets` into `snippets`; each applicable section in topic order through `formats.ExecuteGuide`, `applyCredentials`, the renderer; a section that says nothing is dropped, and `keepsSection` drops `publish` and `maintain` for a reader who may not publish to the repository; every text and snippet is checked for the shape of a token | the same, without `keepsSection` |

`exposureRules` reads the endpoint the request arrived on (`httpserver.EndpointFrom`, its exposure mode), so the `endpoint` section appears only on an
endpoint with rules. The guide renderer is a second `readme.Renderer` with a fixed 256 KiB source bound and no image origins
([wire_packages.go](../../internal/app/wire_packages.go), Q-API-178): a guide is the format's own text, which `ui.readme.max_source_bytes` must not cut.

Tests: [guide_api_test.go](../../internal/api/v1/guide_api_test.go) (`TestR_API_053_*`: the snippets equal the client setup's for every client; `TestR_API_070_*`: no publishing for a reader who may not publish, the publish snippets of a class guide; the
order of the sections per class, the target only with `deploy`, an anonymous caller, the guide of a format and its refusals, `503` without a base URL,
no token shape, the endpoint section, the clients of the catalog, a format without clients; `TestR_GENERIC_016_*` for generic and curl) and [guide_test.go](../../test/integration/packages/guide_test.go) against the stack
(the same through the real chain and the endpoint `partners` with exposure rules).

## The suggestions of the search field (R-API-060 to R-API-064)

The document's block `T-22 search` holds `getSearchSuggestions` (tag `search`) and the schemas `SearchSuggestions` and its five
`…Suggestions` members with their items. Decisions Q-API-270 to Q-API-281. It is **no list** of R-API-010: one request answers the best
`limit` matches of each kind, there is no cursor, and the results page of the UI reads the three lists with `q` instead
([Q-API-270](../spec/api-and-ui.md#q-api-270--one-operation-answers-the-suggestions-and-the-results-page-reads-the-three-lists)), so the
operation does not use `ParseList` and checks its own three parameters.

[search_api.go](../../internal/api/v1/search_api.go) runs, in this order, so that no status depends on the text:

| Step | What | Answer |
|---|---|---|
| Parameters | `parseSuggest`: `q` trimmed, 2 to 100 characters, lower-cased, its terms; `kind` repeated or comma-separated from the six kinds, default `repository,package,project`; `limit` 1 to 10, default 5 | `400 malformed_request` with the parameter as the pointer (`suggestParamError`) |
| Anonymous gate | `anonymousGate`, as on the lists | `401` while nothing is `public` |
| Admin kinds | `requireIdentitiesRead` for `user`, `group` or `service_account` (`wantsIdentities`), before any lookup, the same body for every text | `403` |
| Kinds | `suggest`: one holder of the request (`requestHolder`), then `suggestRepositories` (the index's `Repositories()`, decided as `visibleRepositories` decides), `suggestPackages` (`view.Service.Suggest` on the package list's read scope), `suggestProjects` (`SearchProjects`, `projectListed`), `suggestIdentities` (`principal.Manager.SuggestUsers`, `SuggestGroups`, `SuggestServiceAccounts`; the admin area's principal field reads these three kinds, R-API-078) | `200`, `Cache-Control: no-store`; `503` for a kind whose service the replica lacks |

The ranks of R-API-062, the terms, the words, the tsquery text and the prefix bounds are in
[search_match.go](../../internal/api/v1/search_match.go); `cutRanked` takes the readable matches only, so the filter always runs before
the cut and `more` counts readable matches (Q-API-276). The repository list's `name` (R-API-063, `repositoryNames`, at most 50, the
instance-wide list only) is how the UI confirms its stored entries in one request.

A parameter the generated wrapper cannot bind (a required one that is missing, one that cannot be read as its type) answers `400
malformed_request` with the parameter's name as the pointer (`bindError` in [handlers.go](../../internal/api/v1/handlers.go)), as a
handler's own check of a parameter does.

Tests: [search_api_test.go](../../internal/api/v1/search_api_test.go) (`TestR_API_060_*`: the parameter errors, the members per kind;
`TestR_API_061_*`: the 403 before any lookup, the cut after the filter on a fake decider), [search_match_test.go](../../internal/api/v1/search_match_test.go)
(`TestR_API_062_*`: the ranks, the words and the bounds), and the integration area [search](../../test/integration/search) on the worked rows
of R-AUTHZ-035 (levels, an external user, a scoped token, a path-scoped grant, hidden existence, an endpoint with exposure rules, the
administrator's users and groups, the indexes' plans, `name` on the list), with the statement count in
[querybudget](../../test/integration/querybudget/search_test.go).

## The operations of signatures (R-SIG-034)

The document's block `signatures` holds the operations under the tag `signatures` and the schemas `Signature…`, `NewSignatureAnchor`,
`NewSignatureBlock` and `ProjectSignatureAnchorList`, with the parameter `SignatureAnchorId`; the member `signatures` (`SignatureStatus`)
is added to `Item`, `PackageFile` and `PackageVersion`. The Ansible operations that attach a signature to a local version (R-SIG-031) are
documented with the Ansible format. The handlers run on `Options.Signatures` (`SignatureServices`,
[signatures_services.go](../../internal/api/v1/signatures_services.go)); without its `Store` every operation answers `503` and no answer
carries the member. The domain is [signatures.md](signatures.md).

| operationId | Method and path | Permission (R-SIG-021) | File |
|---|---|---|---|
| `getPackageVersionSignatures` | GET `/api/v1/packages/{packageId}/versions/{version}/signatures` (`repository=`) | public declaration; the package view's scope decides which repositories hold the version for the caller (`read`, or `content:metadata-read`) | [signatures_views.go](../../internal/api/v1/signatures_views.go) |
| `getFileSignatures` | GET `/api/v1/projects/{projectKey}/repositories/{repoKey}/signatures?path=` | `{action: read, …, path: query.path}`, as `getItem`; a folder and a virtual repository's path answer `404` | signatures_views.go |
| `getSignatureObject` | GET `/api/v1/signatures/objects/{sha256}` | public declaration; `read` on a repository that holds an attachment (malware's scope of finds), `404` otherwise | signatures_views.go |
| `getSignatureSettings`, `updateSignatureSettings` | GET, PATCH `/api/v1/signatures/settings` (merge patch, `If-Match` required) | `settings:read`; `security-settings:write` and `x-quader-recent-auth` | [signatures_api.go](../../internal/api/v1/signatures_api.go) |
| `listSignatureAnchors`, `createSignatureAnchor` | GET (`scope=`), POST `/api/v1/signatures/anchors` | `settings:read`; `security-settings:write` | signatures_api.go |
| `getSignatureAnchor`, `updateSignatureAnchor`, `deleteSignatureAnchor` | GET, PATCH (`If-Match` required), DELETE `/api/v1/signatures/anchors/{id}` | `settings:read`; `security-settings:write` (removes a project anchor whatever the switch says) | signatures_api.go |
| `listProjectSignatureAnchors`, `createProjectSignatureAnchor` | GET, POST `/api/v1/projects/{projectKey}/signatures/anchors` | `settings:read` or a member (`projectRole: reader`); `security-settings:write` or an Owner (`projectRole: owner`) | signatures_api.go |
| `updateProjectSignatureAnchor`, `deleteProjectSignatureAnchor` | PATCH, DELETE `…/signatures/anchors/{id}` | `security-settings:write` or an Owner; an anchor of another project or scope is `404` | signatures_api.go |
| `listSignatureBlocks`, `createSignatureBlock`, `deleteSignatureBlock` | GET, POST `/api/v1/signatures/blocks`, DELETE `…/{id}` | `settings:read`; `security-settings:write` | signatures_api.go |
| `listSignatureFindings` | GET `/api/v1/signatures/findings` (`kind`, `repository`, `q`, cursor) | signed in; the files the caller may read as metadata, as `listMalwareFinds` | [signatures_findings.go](../../internal/api/v1/signatures_findings.go) |

What the handlers decide and what they leave to the store:

- **Scope of the trust rows** ([Q-SIG-26](../spec/signatures.md#q-sig-26--the-scope-is-the-project-of-the-addressed-repository)):
  `scopeProject` hands the store the project of the addressed repository (its uuid as text) and `""`, the installation's scope, for a
  repository of the project `upstream`. A virtual repository's entry asks with its own project for the members that hold the version and
  that the caller reads (`members`), and each signature names the member in `repository`.
- **Subjects** (`versionSubjects`): per holding repository the blobs of the version's files at the paths the package view read, looked up
  in that repository through the files service (`sigItems`, decided as `getItem` decides), so a holder whose file has other bytes is asked
  for its own; an OCI tag adds its manifest and each platform's manifest while the repository's tag points at that manifest, as a file or,
  in a remote, as the document it holds the tag as (`versionDocuments`: one `Store.VersionBlobs` for every holder, its `Document` blobs; a
  remote's tag that names another manifest adds that one, Q-SIG-219); npm adds the subject name `<name>@<version>` of the registry
  signature (Q-SIG-50). Without the files service the subjects are the view's and no entry carries an origin.
- **Entries**: the views of the roles `publisher`, `provenance` and `registry` in `signatures`, those of the role `attestation` in
  `attestations`, ordered by role and receipt; `state` and `stale` are `signatures.Badge` over them. The signer is the read's
  `signatures.Signer`, returned as text. A signature this installation made (stage 2: npm's registry signature of a local version, read
  by its subject name like an upstream's) carries `source: "issued"` and `issuedBy` (`identity`, `subject`, `fingerprint`, `state`) from
  the view's `IssuedBy` (`SignatureViews` joins the signing identity), and `keyId` its npm keyid; it has no trust row and never decides the
  badge (R-SIG-055). Quader's receipt attestation, issued the same way, adds `attestation`, its statement decoded from the stored bundle
  (`bundle.Payload`, `statement.Read`, bounded as the statement is; absent when it does not decode, Q-SIG-234), and `servedAs` (`referrer`,
  `api`) with `servedReason` (`hides_signature`, `no_referrers_api`) for an image served through the API (Q-SIG-233), from the view's
  `Receipt`, which `SignatureViews` reads from `signing_attestations` for an issued signature alone
  ([signatures_receipt.go](../../internal/api/v1/signatures_receipt.go)). `covering` lists the releases the format's coverage names (`SignatureServices.Coverage`, bound
  to Debian's `formats.SignatureCoverage`; [signatures_covering.go](../../internal/api/v1/signatures_covering.go)), each with the
  signatures attached to it in the repository and its state read as a publisher's (R-SIG-030).
- **Origin** (`signatureOrigin`, R-SIG-010): the store's `artifact_origins` row, else `fetched` from the item's upstream URL, else the
  principal the item names. The CI snapshot is read by claim name (`project_path`, `ref_protected`, ...); `user_email` is never mapped.
- **Anchors**: a create is read with the material members kept as raw JSON, checked for the key's grammar, the title and exactly the
  kind's member (`anchorInput`), and the store validates the material (422 at the member: `invalid_value`, `secret_material`). A PATCH
  merges into the stored material of the kind (`anchorPatch`) and hands the store only the members the patch names, with the If-Match
  revision as the version (`*` is 0, no check). `409 anchor_managed`, `403 project_anchors_disabled`, `409 anchor_exists`,
  `409 block_exists` and `412` are the store's errors, passed through. The Owner's DELETE of a project anchor is refused in the handler
  while the switch is off (`ownerWhileSwitchOn`), because the store removes a project anchor for an administrator whatever it says.
- **A project's list** carries the switch and, in `installation`, the installation's applied anchors without material, note and creator,
  because the project's members read the page without `settings:read` (R-SIG-037).
- **The original** is an attachment with `nosniff` and `Content-Security-Policy: sandbox; default-src 'none'`: its media type is the
  format's and so the uploader's.
- **The member `signatures`** (`signatureMember` for the item, `addSignatureStatus` for a version and its files): one `Store.Counts` per
  answer over the repository the file or the version was read from, absent for a folder, while `signatures.enabled` is false and without
  a store. The version list carries it per row (`versionListSignatures`, [signatures_versionlist.go](../../internal/api/v1/signatures_versionlist.go)):
  one `Store.VersionBlobs` for the page's files in the rows' repositories (and a remote's tag documents, Q-SIG-219) and one `Store.Counts`
  per project of those repositories, none per row; a version's state is the best of its files' signatures in every repository that holds it
  for the caller.
- **The repository member `signatures`** (`RepositorySignatureSettings`): `fetch` on a remote repository switches the background fetch of
  R-SIG-026; on a local or a virtual one it answers `422 not_applicable` at `/signatures/fetch` (R-SIG-040). On a well-known repository of
  `upstream` an administrator sets it as the narrowing members; on a system repository it is the product's (`managed_by_product`, Q-SIG-128).
- **The format descriptor** of `listFormats` carries `signatureKinds` (`FormatSignatureKind`: `name`, `role`), read from each format's
  `Signatures().Declaration()` (`signatureKindsOf`, [repositories_api.go](../../internal/api/v1/repositories_api.go)); the details of a version
  without a signature name the kinds of the roles that count for the badge (R-SIG-009, Q-SIG-127).
- The anchor and block IDs of a path are strings of digits in the document (`signatureID`): the generated wrapper would answer an
  unreadable integer `400` before the `428` of a missing `If-Match` (R-API-012); any other text names nothing and answers `404`.

Tests: [signatures_api_test.go](../../internal/api/v1/signatures_api_test.go) (`TestR_SIG_021_Permissions`: the table of R-SIG-021 for
an administrator, a security-admin, an auditor, an Owner of `web` with the switch on and off, a member and an anonymous caller;
`TestR_SIG_034_TheSwitch`, `TestR_SIG_034_Anchors`, `TestR_SIG_020_Blocks`, `TestR_SIG_034_Objects`, `TestR_SIG_038_Findings`) runs
every answer through the response validator against a store in memory ([signatures_fakes_test.go](../../internal/api/v1/signatures_fakes_test.go));
[signatures_views_test.go](../../internal/api/v1/signatures_views_test.go) covers the version's answer through a virtual repository, the
scopes, OCI's platforms, the origin and the member. The store behind them is tested against the stack by `TestR_SIG_034`.

## The signing identity (R-SIG-055)

The operations of Quader's own signing identity are under the tag `signatures` with the schemas `SigningOverview`, `SigningIdentity`,
`SigningRefusal`, `SigningBacklog`, `SigningSubject`, `SigningKeyRequest`, `SigningCertificateUpload` and `SigningIdentityImport`. The
handlers run on `Options.Signing` (`SigningServices`, [signing_api.go](../../internal/api/v1/signing_api.go)): the store of
`internal/signing/store`, the replica's snapshot of the identities for the public reads, `signing.enabled`, whether the configuration names
the identity's files (`Managed`, which every write hands the store) and the announce period. Without the store every operation answers
`503`. No answer carries key material; the import's key and passphrase reach the store and nothing else. The domain is
[signatures.md](signatures.md#stage-2-quader-signs).

| operationId | Method and path | Permission | Refusals |
|---|---|---|---|
| `getSigningIdentity` | GET `/api/v1/signing/identity` | `settings:read` | — |
| `createSigningKey` | POST `/api/v1/signing/identity/key` (`201`, the pending identity with `csr`) | `security-settings:write`, `x-quader-recent-auth` | `409 signing_identity_managed`, `409 signing_identity_pending`, `422 validation_failed` at the member |
| `uploadSigningCertificate` | PUT `/api/v1/signing/identity/certificate` (exempt from `If-Match`: it binds to the one pending key) | as above | `409 signing_no_pending_key`, `409 signing_identity_announced`, `409 signing_identity_managed`, `409 signing_identity_exists`, `422 signing_key_mismatch`, `422 validation_failed` with the check of R-SIG-045 |
| `importSigningIdentity` | POST `/api/v1/signing/identity/import` (`201`) | as above | as the upload, the key's refusals at `/key` |
| `discardSigningKey` | DELETE `/api/v1/signing/identity/pending` | as above | `404` |
| `removeSigningIdentity` | DELETE `/api/v1/signing/identity` | as above | `404`, `409 signing_identity_managed` |
| `retireSigningIdentity` | POST `/api/v1/signing/identities/{id}/retire` | as above | `404`, `409 signing_identity_not_previous`, `409 signing_key_in_use` |
| `getSigningTrustedRoot` | GET `/api/v1/signing/trusted-root.json` (Sigstore's trusted root media type, cacheable 60 s) | public | `404` without a current or previous identity |
| `getSigningChain` | GET `/api/v1/signing/chain.pem` (`application/pem-certificate-chain`, cacheable 60 s) | public | `404` without a current identity |
| `getAttestation` | GET `/api/v1/projects/{projectKey}/repositories/{repoKey}/attestation?path=` (the bundle as `application/vnd.dev.sigstore.bundle.v0.3+json`, `Content-Disposition: attachment; filename="<file>.quader.sigstore.json"`, `private, no-cache`; R-SIG-054) | `read` on the path, decided as the item (R-STOR-016) | `404` for a path the caller may not read, a folder, a file Quader has not attested, an attestation of a retired identity; `503` without `SigningServices.Attestations` |

The response validator of the tests decodes `application/pem-certificate-chain` as text ([apitest](../../internal/api/v1/apitest/apitest.go)).

The client setup (R-API-030) answers the tab `verify` while an identity is current and signing is on, for every client of a format whose
declaration attests something and never for a Bastion: the snippet `<client>.verify.attestation` of [setup_verify.go](../../internal/api/v1/setup_verify.go),
written by the core (R-SIG-055, Q-SIG-188). Its marks are the example image or the placeholders `<digest>`, `<path>` and `<file>`, and for a
file the token's reference by the credential mode.

## Conventions every resource inherits (WP1.6)

The helpers live in [internal/api/v1](../../internal/api/v1). A handler calls them; it never re-implements a rule.

| Rule | Helper | File | Notes |
|---|---|---|---|
| Strict JSON body: content type (415), size (413), unknown or duplicate member, wrong type and a nesting deeper than 64 levels (400 `malformed_request` with `errors[0].pointer`, R-API-003, R-API-017, Q-API-75) | `ReadBody`, `DecodeJSON`, `StrictUnmarshal` | [decode.go](../../internal/api/v1/decode.go) | The request type is the response type, so a body obtained by GET can be sent back and read-only members are accepted and ignored. Member names compare exactly. |
| Cursor pagination, filters, sort (R-API-010, R-API-011) | `ParseList`, `ListSpec`, `ListQuery`, `NewPage`, `MapPage`, `WritePage` | [paging.go](../../internal/api/v1/paging.go) | `limit` default `api.page_size_default`, clamped to `api.page_size_max` (Q-API-16). An unknown parameter or sort field is 400; a cursor for other filters or another sort is 400 `invalid_cursor`. |
| `ETag` from a revision column, `If-Match`, `If-None-Match` (R-API-012) | `ETag`, `SetETag`, `ParseIfMatch`, `expectedRevision`, `Precondition.Revision/Matches`, `Failed`, `NotModified`; `errs.Stale`, `errs.AnyRevision` | [etag.go](../../internal/api/v1/etag.go), [revision.go](../../internal/errs/revision.go) | 428 without `If-Match` on PUT/PATCH (the binder, below); 412 with `currentEtag`, from the one `errs.Stale`. |
| JSON merge patch (R-API-013) | `ReadMergePatch`, `MergePatch`, `MergePatcher`, `ApplyMergePatch`, `CheckImmutable` | [patch.go](../../internal/api/v1/patch.go) | RFC 7396, the one implementation (Q-API-157): numbers keep their literal (an integer beyond 2^53 survives), a duplicate member and a nesting beyond 64 levels are 400, `ReadMergePatch` refuses a body that is not an object (`null` included). An immutable member that differs is 422 `validation_failed` with its pointer, compared by literal. An area that stores the resource takes the merge as a function: `MergePatcher(patch)` is the `repository.Patcher`. |
| Idempotency keys, stored in PostgreSQL (R-API-014, R-API-051) | `Idempotency.Do`, `IdempotentOp`, `Outcome` | [idempotency.go](../../internal/api/v1/idempotency.go) | The row is inserted in the transaction of the change. |
| Item paths and reserved names (R-API-004) | `CheckItemPath`, `CheckReservedName` | [path.go](../../internal/api/v1/path.go) | The rules of `repository.CleanPath` on the decoded path: no backslash, at most 4096 bytes and 255 per segment, the length checked first. `invalid_path` is registered by the files area. |
| Rate limits, CORS, response headers (R-API-016, R-API-017, R-API-018) | `NewRateLimit`, `CORS`, `EdgeStage` | [ratelimit.go](../../internal/api/v1/ratelimit.go), [cors.go](../../internal/api/v1/cors.go) | The chain's stage 7; wired by [wire_api.go](../../internal/app/wire_api.go). |
| A value set in YAML cannot be changed through the API | `errs.CodeManagedByConfig` (409 `managed_by_config`) | [codes.go](../../internal/errs/codes.go) | R-API-037. M1 has no settings resource of its own; the owner of a settings document uses the code (Q-API-19). |

Problem extensions: `errs.WithExtension(name, value)` adds a member (`currentEtag`, `job`, `tokenId`); `errs.WithBadRequest()`
keeps a 400 that lists fields; `errs.Unprocessable` (422 without fields) and `errs.PreconditionRequired` (428) are kinds.

### One mechanism for `If-Match` (Q-API-28, Q-API-42)

- A `PUT` or `PATCH` that needs the header declares `x-quader-if-match: required` and references the shared parameter
  `#/components/parameters/IfMatch` (required) and no other `If-Match`; a `DELETE` references `IfMatchOptional`. The generated
  binder refuses a request without a required header before the handler, `bindError` turns that into the 428 of R-API-012,
  and a handler never checks the header again: it reads `expectedRevision(pre)` (the revision, `errs.AnyRevision` for `*`,
  0 for a tag the server never issued) and passes it to the write.
- The write is conditional on that revision in the same statement or under the row lock, and a mismatch is `errs.Stale(what,
  current)`: 412 with `currentEtag` and the cause `*errs.StaleRevision`. The domain packages return it themselves
  (repository, authz policies and rules, the principal manager, the authentication settings); `Failed` is the same error for
  a handler that compares in memory first. A merge patch computes on the revision it read and writes conditional on that
  one, also for `*` (policies, exposure rules).
- [ifmatch_test.go](../../internal/api/v1/ifmatch_test.go) reads the document: every `PUT` and `PATCH` requires the header with
  the shared parameter or is on the short exemption list (membership deltas, role bindings, file content), and every required
  operation answers 428 without the header.

### Recent authentication (R-API-007, Q-API-40)

`x-quader-recent-auth: true` on an operation is read into `Operation.RecentAuth` when the document loads (any other value stops
start-up). `Server.authorize` ([authorize.go](../../internal/api/v1/authorize.go)) calls `requireRecentAuth` once `decide`
allowed the caller, so a caller who may not act gets 403 and not an invitation to sign in again. It looks the session up
(`SessionService.Validate`), compares `authenticated_at` with `auth.sessions.recent_auth_window` and answers 401
`reauthentication_required`; a token is not asked; a replica without a session store answers 503. No handler checks it. The
operations that declare it are listed in `recentAuthOperations` in
[recent_auth_test.go](../../internal/api/v1/recent_auth_test.go); adding a declaration without extending that list fails the test.

### Operations on the caller's own account (R-API-021, Q-API-214)

`x-quader-owner-rights: required` on an operation is read into `Operation.OwnerRights` (`parseOwnerRights`; any other value, or the
attribute on a declaration other than `authenticated: true`, stops start-up). `Server.authorize` calls `ownerRightsGate` once `decide`
admitted the caller, before the recent-authentication check: a token whose scope does not inherit its owner's rights
(`Credential.Scope != nil && !Scope.Inherit`, `narrowedToken`) gets 403 `forbidden` and the handler does not run; a session, the
break-glass session and an inheriting token pass, and an anonymous caller still gets 401. The handlers keep the check of the principal
kind (a service account and a CI token have no profile) and never look at the scope. In
[owner_rights_test.go](../../internal/api/v1/owner_rights_test.go), `ownerRightsOperations` lists the operations that declare it, and
`TestR_API_021_EveryAuthenticatedWriteDeclaresItsTokenRule` walks the document: an operation with an unsafe method and
`authenticated: true` must declare the attribute or be in `ownerRightsExempt` with the rule that bounds a narrowed token there (the
decision point, the derivation rule, the line of the token operations (Q-AUTH-199), the scope's `manage` for an owner change
(Q-AUTHZ-240), a session or an inheriting token for the starter of a job (Q-API-215), or the presenting token itself). A new such
operation therefore fails the test until it is classified. The reads are closed the same way: `TestR_API_021_EveryAuthenticatedReadIsClassified`
walks every authenticated `GET`, which declares the attribute (`listOwnSessions`) or is in `ownerRightsReads` with the reason it may answer
a narrowed token. The attribute on an operation of `/files/` or of the content routes stops start-up: the files handler decides those, and
the shared step never wraps them. A rule that needs the record of the target cannot be a route attribute;
it lives in the handler or the domain service and uses `narrowedToken` (or `Subject.Scope` in `internal/authz`), and its entry in
the exempt list names it.

### Authorization epoch of a write

`authz.HeaderEpoch` is the header name. A plain answer sets it with `setEpoch`; an idempotent create gives its outcome
`Finish: finishEpoch(&epoch)` ([authz_api.go](../../internal/api/v1/authz_api.go)), so the stored outcome and every replay carry
the epoch of the first answer (Q-API-44).

### Idempotency in detail

`Idempotency.Do(w, r, body, IdempotentOp{Run: ...})` is the whole interface. Without the header it runs `Run` in a
transaction of its own. With it:

1. One transaction: `SET LOCAL lock_timeout` (1 s, for the claim only), `INSERT ... ON CONFLICT (principal_id, idem_key)`; a row past
   `expires_at` is recycled. The principal must be authenticated and have a UUID.
2. Row inserted: `Run(ctx, tx)` does the change and returns an `Outcome`; the status, headers and body are stored in the
   same transaction, and the body is read back in the database's rendering so the first answer and every replay are
   byte-identical (Q-API-17).
3. The claim runs under a short `lock_timeout` that is restored to the session value right after the insert. The operation runs
   behind a savepoint: a refusal (below 500) rolls back to it, drops the hooks the operation registered (`meta.TxState.Mark` and
   `Discard`), stores the 4xx outcome on the claimed row and commits, so a duplicate that waited finds the stored answer
   (Q-API-137).
4. Row exists: a different method, route or body is 422 `idempotency_key_reused`; the stored outcome is replayed with
   `Idempotency-Replayed: true`.
5. A duplicate that arrives while the first transaction is open blocks on the row for the lock timeout, then answers 409
   `idempotency_in_progress` with `Retry-After: 1`.
6. `Run` returning an `*errs.Error` below 500 is stored as above (replayed like any outcome); any other error rolls everything
   back and stores nothing.
7. A response with a secret sets `Outcome.SecretID`: the body is sent to this client and never stored; a replay calls
   `IdempotentOp.SecretReplay` (the identity area answers 409 `token_already_issued` with `tokenId`).
8. A value that exists only after the commit hooks ran, such as the epoch of a write (`Quader-Authz-Epoch`, raised by the change
   log just before `COMMIT`), is set by `Outcome.Finish`. `Do` registers it as a `Before` hook after the ones `Run` registered, so
   it sees their results, and the stored outcome, and with it every replay, carries the value.

The task `idempotency-cleanup` ([cleanup.go](../../internal/api/v1/cleanup.go), hourly `13 * * * *`) deletes expired rows.

## How to add a management resource

The worked example is the widget resource of the contract tests,
[test/integration/api/widgets_test.go](../../test/integration/api/widgets_test.go): a keyed collection with list, get,
create, update (merge patch and PUT) and delete. Follow its shape; the widgets are served by a hand-written router, so
they call `pre.Require()` themselves, where a resource behind the generated wrapper relies on the binder (below). The steps:

1. **Table.** Migration with `revision bigint NOT NULL DEFAULT 1`, `created_at`, `updated_at`; a composite index
   `(sort field, id)` for every sortable field (R-API-010). Register the revision column with
   `meta.RegisterRevisionColumn` and the table in `internal/backup/tables` ([database.md](database.md)).
2. **Queries** in `internal/meta/postgres/queries/<area>.sql`: a keyset list query, `Get`, `Insert`, `Update`, `Delete`
   (below). Identifiers and times travel as text and are cast in SQL; a filter that is not sent is an empty array, never a
   nil slice (a nil slice is SQL `NULL` and matches nothing).
3. **Document.** Add the paths and schemas in your `>>> M1 <area>` markers of [api/openapi.yaml](../../api/openapi.yaml):
   - every operation: `operationId`, `tags`, `x-quader-permission`, the success response with an example, and the problem
     responses it can produce: `$ref: "#/components/responses/…"` for `BadRequest`, `Unauthenticated`, `Forbidden`,
     `NotFound`, `Conflict`, `PreconditionFailed`, `PreconditionRequired`, `Unprocessable`, `PayloadTooLarge`,
     `UnsupportedMediaType`, `Internal`, `Unavailable`, and **`TooManyRequests` on every operation** (the rate-limit stage can
     answer 429 anywhere; `TestEveryOperationCanBeRateLimited` fails otherwise);
   - lists: parameters `$ref: "#/components/parameters/Limit"` and `Cursor`, the filters and `sort`, response header `Link`,
     and a list schema `{items: [...], nextCursor: [string, "null"]}` (copy `JobList`);
   - `PUT`/`PATCH`: `x-quader-if-match: required`, the shared parameter `IfMatch` (no inline `If-Match`), responses 412 and 428 (a
     missing header is answered 428 by `bindError`, because the generated wrapper rejects a missing required header before the
     handler runs; `TestR_API_012_OneMechanismForAMissingIfMatch` checks the declaration); `DELETE` references `IfMatchOptional`;
     the `GET` and the write answer the `ETag` header (`#/components/headers/ETag`); `PATCH` takes `application/merge-patch+json`;
   - an operation that mints a credential or widens permissions adds `x-quader-recent-auth: true` (the shared step enforces it);
   - an `authenticated: true` operation that changes the caller's own account (profile, preferences, sessions) adds
     `x-quader-owner-rights: required`; any other unsafe `authenticated: true` operation goes into `ownerRightsExempt` of
     [owner_rights_test.go](../../internal/api/v1/owner_rights_test.go) with the rule that bounds a narrowed token there;
   - creating `POST` and `DELETE` with 202: `x-quader-idempotency-key: optional`, parameter `IdempotencyKey`, response header
     `Idempotency-Replayed`, and `Location` on 201/202;
   - a collection whose key sits next to a literal route (`/users/me`): list the literals as `x-quader-reserved-names` in the
     key schema; `TestOpenAPIDocument` derives them from the paths and fails on a missing one.
   Run `make generate`.
4. **Handlers** in `internal/api/v1/<resource>_api.go` (they implement the generated method; the authorization step has run):

   ```go
   // list
   lq, err := ParseList(r, ListSpec{Filters: []string{"color"}, Sorts: []string{"name", "createdAt"}, DefaultSort: "name"}, s.o.API.Config)
   rows, err := store.List(ctx, lq.Filter, lq.After, lq.FetchLimit())      // one keyset query, in the sort order, id last
   page := NewPage(lq, rows, func(r Row) []string { return []string{r.Name, r.ID} })   // the key the store compares
   WritePage(w, r, MapPage(page, toRepresentation))

   // get
   tag := ETag(rep.Revision, "")                  // "<revision>-<view>" where the representation depends on the caller
   if NotModified(w, r, tag) { return }
   SetETag(w, rep.Revision, ""); writeJSON(w, r, 200, rep)

   // create: strict body, validation, reserved name, then the change inside the idempotent operation
   body, err := ReadBody(w, r, MediaJSON, limit); err = StrictUnmarshal(body, &in)
   err = CheckReservedName("/key", in.Key, reserved...)
   err = s.idem.Do(w, r, body, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
       ...insert, record the audit event with RecordTx...
       return Outcome{Status: 201, Body: rep, Header: map[string]string{"Location": base + path, "ETag": ETag(rep.Revision, "")}}, nil
   }})

   // update (PATCH): 428 first, then the patch on the current representation, immutable members, one UPDATE with the revision
   pre, err := ParseIfMatch(r)                      // the header is there: the binder answered 428 otherwise
   raw, err := ReadMergePatch(w, r, limit)
   next, err := ApplyMergePatch(current, raw); err = CheckImmutable(current, next, "key", "format")
   expected := expectedRevision(pre)               // errs.AnyRevision for If-Match: *
   // UPDATE t SET ..., revision = revision + 1 WHERE key = $1 AND ($2::bigint < 0 OR revision = $2) RETURNING ...
   // zero rows: the resource is gone (404) or changed: return errs.Stale("The widget", currentRevision)

   // delete: honours If-Match when sent (expectedRevision), 204 or 202 with a job
   ```
   A change that also writes an audit event, enqueues a job or notifies a change area does so in the same transaction
   (`RecordTx`, `Queue.Enqueue(ctx, tx, …)`, `cluster.Notify`).
5. **Read from PostgreSQL.** A management `GET` never reads an in-process cache (R-API-052); the answer shows a write that
   committed on another replica at once. No state between requests lives in a process (R-ARCH-032): the cursor holds the
   position, the idempotency row the outcome, the revision column the version.
6. **Tests.** Fill an `apitest.Resource` and call `apitest.RunContract` with a `Do` that spreads requests over replicas: it
   checks pagination (`limit=1` walk, `invalid_cursor`), ETag with `If-Match`, merge patch and idempotency keys on your
   resource (E1.6). Wrap the handler in `apitest.Validator.Handler` so every answer is validated against the document, and
   assert `Uncovered(...)` is empty for your operations. The read-back right after a create repeats a `GET` that is
   answered `503 authz_stale` with `Retry-After` for up to 30 s and logs each repeat (Q-API-238); every other call and every
   other status is the answer at once.

## Contract tests and response validation

[apitest](../../internal/api/v1/apitest) is test support (no production package imports it):

| Function | Purpose |
|---|---|
| `LoadDocument(spec)` | Parses and validates the document as OpenAPI 3.1 (kin-openapi supports the 3.1 constructs the document uses: type arrays with `null`, `oneOf` with a null branch). |
| `New(t, spec)`, `Validator.Handler(t, next)` | Wraps a handler; every answer is matched to its operation by method and path, and status (an undeclared status fails), headers and body are validated against the response definition. |
| `Validator.Check`, `Recorder` | Validate one answer. |
| `Validator.Uncovered(ids...)` | The operations no answer reached; a test that owns operations asserts it is empty. |
| `Resource.RevisionMember` | The member that grows with every write (default `revision`); the suite does not count it as a change when it checks that a PUT or a patch left the other members alone. |
| `ReservedNameProblems(spec)` | The R-API-004 check on `x-quader-reserved-names`. |
| `RunContract(t, Resource)` | The convention tests on any collection (E1.6): create/Location, strict members, `limit=1` traversal, cursors, ETag/304, `If-Match` 428/412/200/`*`, merge patch, PUT round trip, idempotency replay. A resource whose owning page names other codes sets `MalformedCode` (unknown member) and `ImmutableCode` (immutable member), and `AltPatch` when a patch that changes nothing writes nothing (the repository resource, Q-REPO-21). |

`TestResponsesMatchTheDocument` ([openapi_test.go](../../internal/api/v1/openapi_test.go)) sends every `GET` operation of the
document, anonymous and authenticated, through the chain and validates the answers. The integration tests of
[test/integration/api](../../test/integration/api) validate the system operations across several in-process replicas that
share one database and one Valkey.

## Jobs, tasks and the key-ring status

- **Jobs** ([jobs_api.go](../../internal/api/v1/jobs_api.go)): rows of `job`, read from PostgreSQL by every replica. The list
  is newest first over `(created_at, id)` (indexes `job_created`, `job_kind_created`), filtered by `kind` and `state`
  (repeat the parameter). `replica` is the name of the replica that claimed the job (`job.ran_by`, set by the claim and kept
  when the job ends, Q-OPS-46); `triggeredBy` the principal.
- **Tasks** ([tasks_api.go](../../internal/api/v1/tasks_api.go)): `jobs.Registry.ResolveTasks` merges the registered tasks with
  `maintenance.tasks`. A manual-only task (`jobs.TaskSpec.ManualOnly`) has no schedule and no `maintenance.tasks` entry
  (an entry stops start-up and names the key). `POST …/run` checks the optional parameters against the type the task
  declares in `Services.TaskArgs` (`blob.GCJobArgs` for `gc` and `gc-full`, `blob.NewSweepJobArgs` for `orphan-sweep`) and, when that type
  has a `Validate` method, its bounds (a `422` with the field, the orphan sweep's `minAge` and `connection`), enqueues in a transaction with the
  `system.task_triggered` event, and answers `409` with the existing job when the exclusive key (`gc` for both collection
  tasks) is held. A `web` replica only enqueues; the integration test `TestGarbageCollectionTrigger` shows the run on the
  `worker`.
- **Key-ring status** ([encryption_api.go](../../internal/api/v1/encryption_api.go)): `secrets.KeyRing.BuildReport`
  ([report.go](../../internal/secrets/report.go)), the one report that `quader admin secrets status`
  ([admin_secrets.go](../../cmd/quader/admin_secrets.go)) prints too: the answering replica's ring, the sealed counts from
  [listq](../../internal/meta/postgres/listq/sealed.go) over the registered columns, the canary rows of `secret_key_canary` and
  the rings of the replica registry. `secrets.RingsCompatible` is the rule; the trigger of `secrets-reencrypt` refuses with
  `409 key_ring_incompatible` while it is false.
  At start-up `verifyKeyRingCanaries` ([wire_api.go](../../internal/app/wire_api.go)) refuses a ring that lacks a key that sealed
  values reference, seeds the canary of every key ID that has none, and refuses a ring that cannot open an existing canary.
  The job body of `secrets-reencrypt` ([wire_api.go](../../internal/app/wire_api.go), `registerSecretsReencrypt`) calls
  the walks of the five registered sealed columns (`repository.ResealSecrets` for `repository_secrets.sealed`,
  `debdata.ResealSigningKeys` for `deb_signing_keys.private_key`, `signingkey.Reseal` for `signing_identities.sealed_key`, `keys.Reseal` for
  `storage_keys.sealed_key` and `upload_sessions.sealed_data_key`) and
  records `columns` (the number of registered columns), `resealed`, `current` and `failed` as its result; it fails permanently
  when `failed` is above zero (a value the ring of this replica cannot open is left as it is), and fails permanently, naming the
  column, for a column registered later without a walk of its own. A feature that registers another column writes its walk
  and adds it to the handler (the sealed counter in `listq` is the pattern).

## System overview (R-OPS-004, Q-OPS-175)

`GET /api/v1/system/overview` ([overview_api.go](../../internal/api/v1/overview_api.go)) is what the admin dashboard
shows besides the replica registry. The answering replica counts on one connection of the request pool
(`API.Store.Read`, which rides out a failover and ends as `503 database_unavailable`; a replica without the store
answers 503 too) with the five static statements of [overview.sql](../../internal/meta/postgres/queries/overview.sql).
`endpoints` is the number of configured endpoints (`EndpointServices.Endpoints`, filled with `cfg.Server.ResolveEndpoints()`
in [wire_exposure.go](../../internal/app/wire_exposure.go)), not a count of the database.

| Member | Statement | What counts | Cost |
|---|---|---|---|
| `counts.repositories` | `OverviewRepositories` | `lifecycle = 'active'`, grouped by `class` and `format`; the handler counts a format the catalog marks system only as `system` (`shownClass`, Q-UI-1217); `byClass` always holds `local`, `remote`, `virtual` and `system`, 0 for a class without a repository | a scan of `repositories`, one row per repository |
| `counts.users`, `groups`, `serviceAccounts`, `bastions` | `OverviewPrincipals` | the principals of the kinds `user`, `group`, `service_account` and `bastion`, disabled ones included | the unique index on `principal (kind, name)` matches the predicate |
| `counts.packages`, `packageVersions`, `activeTokens`, `projects` | `OverviewContent` | every package; versions with `trashed_at IS NULL`; tokens with `revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`, of every type; every project | linear in packages and in live versions (the predicate is that of `package_versions_live`); the token predicate lies within the partial index `api_token_expiry_active` (`WHERE revoked_at IS NULL`); `project` is small |
| `storage.blobs`, `bytes` | `OverviewStorage` | the single row of `storage_usage_totals` that the usage fold maintains (live blobs, R-STOR-003), never a sum over `blobs`; it trails the deltas by the fold's interval | one row by primary key |
| `storage.pendingDeletion` | `OverviewStorage` | the rows of `blob_gc_queue`, the candidates of the garbage collection | linear in the queue, which the collection empties |
| `jobs.queued`, `running`, `failedLast24h` | `OverviewJobs` | `queued`; `running` and `cancelling`; `failed` with `finished_at` within 24 hours of PostgreSQL's clock | one scalar subquery per partial index whose predicate it repeats: `job_fetch`, `job_lease`, `job_finished` |

Whether PostgreSQL picks those indexes, and whether the scans are index-only, depends on its statistics and the
visibility map; the plans have not been measured on a seeded table. What bounds the cost is the cache: the replica keeps
the answer for `cacheSeconds` (30, `overviewCacheSeconds`), so a dashboard that refreshes every few seconds counts once
per period and replica. The cache is a field of the `Server` instance (`overviewCache`, no package-level state,
R-ARCH-032): one count runs at a time, a request that finds the cache stale while a count runs waits for it (or for its
own context), a failed count is never kept, and a count that panics still releases the waiting requests.
`computedAt` is the start of the count. Tests: `TestR_OPS_004_SystemOverview` and `TestR_OPS_004_OverviewCache`
([overview_api_test.go](../../internal/api/v1/overview_api_test.go), the statements answered by a fake connection), and
against PostgreSQL `TestR_OPS_004_OverviewStatements` ([overview_test.go](../../test/integration/postgres/overview_test.go)).

## The Bastion interface (R-BAS-008)

The four operations with the tag `bastion` are `x-quader-permission: {bastion: true}` and `x-quader-bastion-interface: <n>`; the document refuses a version other than the
build's. They are not part of the v1 promise but of the interface version, their bodies are the types of [internal/bastion/iface](../../internal/bastion/iface), and every
answer carries `Quader-Bastion-Interface`. The shared authorization step admits them on the recognition alone; their handlers are
[bastion_handshake_api.go](../../internal/api/v1/bastion_handshake_api.go), [bastion_check_api.go](../../internal/api/v1/bastion_check_api.go),
[bastion_feed_api.go](../../internal/api/v1/bastion_feed_api.go) and [bastion_downloads_api.go](../../internal/api/v1/bastion_downloads_api.go); the services they take are
`v1.BastionServices` (a nil member answers `503`). The registration of the caller of the stage is a principal of the kind `bastion` and the general limits charge it
to the class `read` under its token whatever the method. Read [bastion.md](bastion.md#package-2-the-main-installations-interface) for the flow.

## Rate limits, CORS and response headers

The chain's stage 7 is `v1.EdgeStage(CORS, rate limit)` built by `wireAPI` in [wire_api.go](../../internal/app/wire_api.go) and returned as
`apiArea.rateLimit` ([areas.go](../../internal/app/areas.go)). Order: the security headers of R-API-017 on every `/api/v1/` answer (`Cache-Control: no-store` unless the
handler sets another, `Content-Security-Policy`, `Referrer-Policy`; `X-Content-Type-Options` and `X-Request-Id` come from the
chain), then CORS, then the limiter, so a preflight is answered before it counts. The preflight of an allowed origin is
answered earlier still, by `Chain.Preflight` (`v1.Preflight`, between the address stage and authentication): a browser sends
it without credentials, and authentication would refuse it (R-API-018, Q-API-138). `CORS` keeps the same answer for a chain
without that stage. Each of these rules applies only where the endpoint serves the family of the path (`familyServed`,
`apiRequest`; Q-ARCH-306): on `packages`, `OPTIONS /api/v1/x` from an allowed origin is decided like `OPTIONS /does-not-exist`,
and on `api` a request to `/files/` or `/api/<format>/` carries no `RateLimit-Policy`.

The classes are `api_read`, `api_write` and `api_anonymous` for `/api/v1/`, and `api_package_write` for the unsafe methods of
every protocol path (`/api/<format>/`, `/files/`, `/v2/`) in mode `enforce` only ([ratelimit.go](../../internal/api/v1/ratelimit.go)
`protocolStage`, `packageClass`; Q-API-344). A protocol path is otherwise limited by the download limits
([usage-and-limits.md](usage-and-limits.md)), in `enforce` only: in `audit` nothing there is refused or slowed (Q-LIM-4); a
protocol request without the engine of the download limits (a test of the management API's classes alone) is limited by nothing.
`api_package_write` has a budget and buckets of its own (`api.rate_limits.package_write`), keyed by the caller's subject, so a
publish loop does not use up the management budget of its token. The download limits skip a request that
`RateLimitOptions.Challenged` reports: the composition root passes `v1.ChallengedByLevels` over the installed grant index, which
reports a package request whose repository the index holds with artifacts that are not `public` (a key it does not hold is
charged: its decision may read PostgreSQL first), so the challenge a NuGet client draws before it sends its credentials costs no
budget (Q-API-290). The class an endpoint applies on top is [endpoints.md](endpoints.md). The mechanism is [the cluster limiter](valkey.md#rate-limit-counters). Every answer on a limited
route that a class charged carries `RateLimit-Policy` and `RateLimit`; a refusal is `429 rate_limited` with `Retry-After` and counts in
`quader_api_rate_limited_total{class}`. Enabling `api.rate_limits.exempt_admins` needs the authz area's `content:bypass`
check (`IsAdmin` in `RateLimitOptions`); without the authorizer nobody is exempt.

**Consequence for tests and the development profile.** The defaults apply to anonymous callers too (5 requests per second
per client address, burst 20). A test or script that polls `/api/v1/system/replicas` through `system.bootstrap_anonymous_read`
faster than that gets `429`; set `api.rate_limits.enabled: false` (`QUADER_API_RATE_LIMITS_ENABLED=false`) in such an
environment.

## Not yet in M1

`/api/v1/storage/gc-runs` (the domain view of runs) is not built: Q-API-9 chose one path per function, the job list filters by
`kind` and carries each run's result, and no M1 screen needs more (Q-API-20). The audit event types are registered in
[events_api.go](../../internal/audit/events_api.go); the recorder is the audit area's, and the composition root passes
`audit.Nop` only in a shell without infrastructure (`auditRecorder` in `wire_api.go`). Response compression (R-API-017) and the 30 s bound on a
synchronous handler are not implemented; the server-wide timeouts of R-ARCH-005 apply.
