# Usage and limits

The download limits of [spec/usage-and-limits.md](../spec/usage-and-limits.md) (walk item W-26): who a request is charged to,
which rule applies, the counters in Valkey, the decisions at stage 7, before a download and before an upstream exchange, the pacing of
a body, the usage store behind the overview, and the API. Work package 2 (accounting and enforcement) is built; the technical tokens
(package 1) and the screens (package 3) land beside it. The section "Work packages" is removed when the third has landed.

## What has to be in your head first

- **Two budgets per request.** The subject's budget at the place whose rule applies (a budget key), and for an upstream exchange
  the remote's ceiling of the caller's class. Both hold.
- **The subject is not the credential.** A user's sessions and personal tokens share one subject; a technical token is a subject
  across its rotations; a CI token is charged to its GitLab project; an anonymous caller to its address prefix
  ([R-LIM-001](../spec/usage-and-limits.md#r-lim-001--the-subject-a-request-is-charged-to)). `limits.SubjectOf` is the one
  derivation; `httplimit.CallerOf` converts the principal of a request.
- **Valkey enforces, PostgreSQL remembers.** Counters are windows in Valkey (key family `lim`); the usage store is a per-replica buffer
  flushed to PostgreSQL every ten seconds, read by the overview and never by a request.
- **A request writes no row and costs at most three round trips:** one at stage 7, one before a download from the cache, one at step
  9a for a miss. Bytes, refusals, the end of a download and lease renewals ride on the replica's flush, once a second.
- **Audit counts everything and applies nothing new.** In `audit` the package classes of R-API-016 are the safety net; in `enforce`
  the profiles take over ([R-LIM-026](../spec/usage-and-limits.md#r-lim-026--audit-and-enforce)).
- **The format contract is version 6.** A route's kind decides what is counted; `Host.Serve` admits downloads, and check 16 of the
  format kit fails a format that writes a download's body itself
  ([R-LIM-036](../spec/usage-and-limits.md#r-lim-036--the-format-contract-version-6)).

## What runs where

| Role | What it does for the limits |
|---|---|
| every role | the area `download_limits` ([wire_limits.go](../../internal/app/wire_limits.go)): the store, the compiled index (`limitsIndex`, rebuilt on the change areas `limits` and `repo_config` and once a minute), the engine (`limits.Engine`) with its charge buffer (flush every `limits.flush_interval`), the usage buffer (flush every `limits.usage.flush_interval`) |
| `packages`, `web`, `all` | stage 7 on the protocol paths (`protocolStage` in [ratelimit.go](../../internal/api/v1/ratelimit.go)), the admission in `edge.ServeItem` (through `Host.Serve` and `/files/`), step 9a and the counted body in the remote engine, the registry token's `Retarget` |
| `api`, `web`, `all` | the limits and usage operations ([limits_api.go](../../internal/api/v1/limits_api.go), [usage_api.go](../../internal/api/v1/usage_api.go)); `/files/` on an endpoint that serves it |
| `worker`, `all` | the daily task `limits-cleanup` (`25 3 * * *`, [usage/cleanup.go](../../internal/limits/usage/cleanup.go)); the upstream requests and uncached bytes of mirror passes, recorded under `system` |

## The request path

| Step | File and function | What the limits do |
|---|---|---|
| Stage 6, authentication | [chain.go](../../internal/httpserver/chain.go) `Chain.AuthFailed`, wired to `App.limitsFailedCredential` | A refused credential on a protocol path is one request and one refused request of the client's address (`Engine.FailedCredential`, R-LIM-003); nothing of the subject the token names |
| Stage 7 | [ratelimit.go](../../internal/api/v1/ratelimit.go) `protocolStage.serve` | Skips a preflight and a request the decision point challenges (Q-API-290). Derives the caller (`httplimit.CallerOf`, the administrator through `authz.Index.HoldsAdmin`), resolves the place (`Engine.Begin` → `Index.Resolve`), and runs one batch (`Engine.Check`): the burst and hour windows of the budget key, its block marker, and the package class of R-API-016 in force (`cluster.Limiter.BatchTake`). Wraps the response in `httplimit.Writer` and puts the charge in the context; at the end writes the log fields and finishes the charge |
| Repository of the address | [qname/address.go](../../internal/qname/address.go) `ParseProtocolAddress` | `/api/<format>/…`, `/files/…` and `/v2/<project>/<key>/…` (`ParseRegistryAddress`, Q-LIM-41); a path that names none is decided at the instance |
| The format dispatcher | [dispatcher.go](../../internal/api/dispatch/dispatcher.go) `serveRoute` | Marks a refusal of the decision point (`DenyHidden`, `DenyVisible`) and a credentialed `repository_not_found` as refused (R-LIM-008); records the repository that served |
| The raw-file handler | [rawfiles/handler.go](../../internal/api/rawfiles/handler.go) `get`, `refusedAccess` | The same refusal mark; `ServeOptions.Admit` for a file |
| The registry's token service | [registry/token.go](../../internal/api/registry/token.go) `Token.ServeHTTP` | Stage 7 deferred the request windows (`DeferRequestFigures`); the handler charges them once, to the token's subject or to the address (`Charge.Retarget`, Q-LIM-48) |
| Serving a stored item | [edge/content.go](../../internal/api/edge/content.go) `ServeItem` `ServeOptions.Admit`; [formathost/ingest.go](../../internal/formathost/ingest.go) `itemStore.Serve` | For a `GET` of a `Download` route, after the conditionals and the range and before `open`: the hour and day byte windows and a parallel lease, one batch (`Engine.Admit`); a refusal is returned unwritten |
| Step 9 and 9a | [proxy/fetch.go](../../internal/proxy/fetch.go) `fetcher.run` | The fetch's first act: `Charge.MayFetch` counts the upstream request for the budget key and the ceiling, reads the uncached hour windows, takes the lease of an artifact `GET`. A refusal is the leader's own outcome (`reqState.budget`, Q-LIM-43); `metadataAnswer` serves an expired document stale within `maxStaleSeconds` (`detail=stale-budget`). A `HEAD` the engine forwards (`artifact.go` `head`) and a forwarded search (`query.go` `queryFetch`) pass the same check |
| The upstream exchange | [proxy/fetch.go](../../internal/proxy/fetch.go) `fetcher.send`, `fetcher.bodyReader` | Every exchange after the first is one more upstream request; the body is counted as uncached bytes and paced at `uncachedBytesPerSecond` (`limits.CountingReader`) |
| The leader's stream | [proxy/integrity.go](../../internal/proxy/integrity.go) `stream.flushLocked` | Marks the charge streaming while it writes: the writer passes those bytes uncounted (they were counted where read) |
| Collapsing, detached fetch | [proxy/flight.go](../../internal/proxy/flight.go) | The flight runs on the leader's context, so on its charge; `Charge.Hold` keeps it in the buffer until the fetch ended, also after the client left (Q-LIM-5) |
| Background requests | `fetcher.run` | `Engine.System(repo)`: upstream requests and uncached bytes recorded under `system`, checked against nothing |
| The flush | [limits/flush.go](../../internal/limits/flush.go) `Engine.Flush` | Once a second, one batch: `INCRBY` of the bytes and refusals of every budget key and ceiling, `ZREM` of ended leases, `ZADD XX` renewals of leases older than 20 s, `ZCARD` of paced keys, the block markers found due; then the deltas to the usage buffer |
| The usage buffer | [limits/usage/buffer.go](../../internal/limits/usage/buffer.go) `Buffer.Flush` | Every ten seconds one transaction: one upsert of the five-minute and hourly rows, one of the limit events, in key order |

## Package layout

| Package | Layer | Responsibility |
|---|---|---|
| [`internal/limits`](../../internal/limits) | domain | `figures.go` (figures, overrides), `subject.go` (subjects, address prefixes, `SubjectOf`), `model.go` (places, rules, profiles, ceilings, maxima, settings, the built-ins), `index.go` (`Index`, `Resolve`, the clamp), `charge.go` (`Charge` and its context), `engine.go` (`Check`, `Admit`, `MayFetch`, `FailedCredential`, `Retarget`, `System`), `flush.go` (the charge buffer, blocks, `LiftBlock`, `Live`), `pacer.go` (pacing, `CountingReader`), `errors.go` (the refusals of R-LIM-020), `metrics.go` |
| [`internal/limits/store`](../../internal/limits/store) | domain | The PostgreSQL store: `Snapshot` for the index, the writes of the API (audited, announcing the area `limits`), the record of blocks |
| [`internal/limits/usage`](../../internal/limits/usage) | domain | The usage buffer, the reads of the overview and the detail, `limits-cleanup` |
| [`internal/limits/httplimit`](../../internal/limits/httplimit) | edge | `ProtocolPath`, `Repo`, `CallerOf`, the counting and pacing `Writer`, the log fields, the `RateLimit` fields |
| [`internal/cluster/valkey_limits.go`](../../internal/cluster/valkey_limits.go) | infrastructure | `LimitBatch`, `LimitCounters` (Valkey), `MemoryLimits` (the replica's fallback and the unit tests), `Estimate`, `RoomAfter` |

Migrations: [00081_limits.sql](../../internal/meta/postgres/migrations/00081_limits.sql) (profiles with the five built-ins, rules with
the built-in rule set, ceilings, project maxima, settings, blocks, the change area `limits`) and
[00082_usage.sql](../../internal/meta/postgres/migrations/00082_usage.sql); queries in
[limits.sql](../../internal/meta/postgres/queries/limits.sql) and [limits_usage.sql](../../internal/meta/postgres/queries/limits_usage.sql).

## Configuration

```yaml
limits:
  flush_interval: 1s              # default
  parallel:
    lease: 60s                    # default
    wait: 10s                     # default
  pacing:
    min_bytes_per_second: 65536   # default
  usage:
    flush_interval: 10s           # default
    max_buffered_keys: 100000     # default
    summary_retention: 9600h      # default: 400 days
maintenance:
  tasks:
    limits-cleanup: {enabled: true, schedule: "25 3 * * *"}   # default
```

The profiles, the rules, the ceilings, the mode and the block time are stored and changed through the API, not configured.

## What a request costs, measured

`BenchmarkR_LIM_017_CachedDownload` ([engine_test.go](../../internal/limits/engine_test.go)) runs stage 7, the admission and the end
of a cached download against counting in-memory counters: **2 round trips per download** on the request's path (the flush of every
second runs off it), about 6 µs and 71 allocations of the engine per download on the development machine. A miss adds the round trip
of step 9a. No PostgreSQL statement runs on a request: the index is in memory, the usage store is written by the buffer.

## Tests

| Test | What it holds |
|---|---|
| `TestR_LIM_013_WorkedExamples`, `TestR_LIM_016_TheClamp` | The worked examples of R-LIM-013 and R-LIM-016 on `Index.Resolve` and `Index.CapFor` |
| `TestR_LIM_001_TheSubjectOfEveryCaller`, `TestR_LIM_002_AddressPrefix`, `TestR_LIM_004_Figures` | Subjects, prefixes, figures |
| `TestR_LIM_017_RequestWindowsAndCost`, `TestR_LIM_021_…`, `TestR_LIM_022_StepNineA`, `TestR_LIM_023_Pacing`, `TestR_LIM_024_ParallelDownloads`, `TestR_LIM_025_TheBlock` | The engine on in-memory counters: windows, cost, Audit, the admission, 9a and the ceiling, pacing shares and the floor, leases and their fallback share, blocks and their lift |
| `TestR_LIM_036_EveryRouteOfEveryFormatCountsByItsKind` ([formats/all](../../internal/formats/all)) | The kind of every route of the six formats |
| Check 16 of the format kit ([checks_admission.go](../../internal/formats/formattest/checks_admission.go)) | Every format answers 429 before any byte when the admission refuses; the defect `WritesOwnDownload` fails it |
| [ratelimit_test.go](../../internal/api/v1/ratelimit_test.go) | The package classes on `/files/` and `/v2/`, keyed by the subject |

## Work packages

Three packages, built in parallel worktrees on the base of the specification commit. Each runs the gates of its builder brief; the
development agent merges them in the order **1, 2, 3**.

### Work package 1 · Technical tokens on three levels

- **Requirements:** [R-AUTH-045](../spec/identity-and-authentication.md#r-auth-045--technical-tokens),
  [R-AUTH-046](../spec/identity-and-authentication.md#r-auth-046--service-accounts-of-a-repository),
  [R-AUTHZ-039](../spec/authorization.md#r-authz-039--the-actions-of-a-repositorys-service-account), the rows of technical tokens and
  repository service accounts in [R-API-082](../spec/api-and-ui.md#r-api-082--the-operations-of-usage-limits-and-technical-tokens),
  [R-UI-181](../spec/api-and-ui.md#r-ui-181--technical-tokens-at-the-instance-a-project-and-a-repository) without its **Limit** and
  **Last 24 hours** columns and its **Set limit** action, the identity part of R-LIM-038; the amended R-AUTH-020, R-AUTH-022,
  R-AUTH-026, R-AUTH-027, R-AUTH-030, R-AUTH-041, R-AUTHZ-001, R-AUTHZ-026, R-REPO-060.
- **Owns:** `internal/auth/principal` (service accounts, the repository level), `internal/auth/token` (technical tokens, the cache
  entry, rotation, new secrets, `token-cleanup`), `internal/authz` (`index.go` and the decision: the account grants, source
  `account:<reference>`), `internal/project/move.go` (renaming a repository's accounts), `internal/api/v1/project_accounts_api.go`,
  `groups_api.go` (the instance accounts' routes), `tokens_api.go`, a new `repository_accounts_api.go`, `internal/app/wire_identity.go`,
  `web/src/app/features/service-accounts/`, the repository settings' section **Service accounts** as a component of its own.
- **Migrations:** 00080 (`technical_token`, `api_token.technical_token_id` and its check, `service_account.repository_id`, its
  guard `AI005`, `service_account_repository_grant`, the trigger that deletes the principal of an account removed with its
  repository), 00081 reserved.
- **Shared, and who owns the conflict:** `internal/httpserver/principal.go` (adds `Credential.TechnicalTokenID`; package 1 owns it,
  package 2 adds the identical line if it lands first); `api/openapi.yaml` (the identity operations; the development agent merges by
  operation); `internal/config` (`max_technical_tokens_per_account`; package 2 owns the file's conflict);
  `web/src/app/features/repositories/settings-sections.ts` and `repository-detail.ts` (one entry for its section; package 3 owns the
  conflict); `web/e2e/renders/screens.ts`, `seed.ts`, the locale files (appended; package 3 owns the conflict).
- **Tests:** unit in the packages it owns, including `TestR_AUTHZ_039_…` for the decision and the explanation; integration sets
  `idapi`, `identity`, `projects`, `authz` (the acceptance criteria of R-AUTH-045, R-AUTH-046, R-AUTHZ-039); renders of the service
  account page at the three levels (light and dark, 1440 and 390, `admin` and `power`), the repository settings' section, the
  create dialog with the name field, the token row of an expired token.
- **Must not touch:** `internal/proxy`, `internal/limits`, `internal/api/edge`, `internal/api/v1/ratelimit.go`, the limits and usage
  screens.
- **Built.** What the next packages build on:
  - Package 2 reads the budget subject from `httpserver.Credential.TechnicalTokenID` (empty for every credential but a SAT; for a
    registry token the underlying token's), set by the stage from the token cache entry (`token.Info.TechnicalTokenID`). The row is
    `technical_token` (stable ID across rotations and new secrets); `TechnicalToken.limit` of the API is always `null` until the limits
    area fills it ([technical_tokens_api.go](../../internal/api/v1/technical_tokens_api.go), `toTechnicalToken`).
  - Package 3 adds the columns **Limit** and **Last 24 hours** and the action **Set limit** in
    [technical-token-table.ts](../../web/src/app/features/service-accounts/technical-token-table.ts): the two columns between
    **Token** and **Expires** (`columns`, and the row's cells between its `th` and the time `td`, where a comment marks the place),
    **Set limit** before **Revoke** in the action cell. The table is used twice in
    [technical-tokens.ts](../../web/src/app/features/service-accounts/technical-tokens.ts), for the active and for the ended tokens
    (Q-UI-740), and serves the three levels.
  - The repository settings' section is `RepositoryServiceAccounts`
    ([repository-service-accounts.ts](../../web/src/app/features/service-accounts/repository-service-accounts.ts)), opened by
    `repository-detail.ts` as the lazy section `service-accounts`; an account's page at a repository is the repository's child route
    `service-accounts/:name` ([repositories.routes.ts](../../web/src/app/features/repositories/repositories.routes.ts)).

### Work package 2 · Accounting and enforcement

- **Requirements:** [R-LIM-001](../spec/usage-and-limits.md#r-lim-001--the-subject-a-request-is-charged-to) to R-LIM-037 and
  R-LIM-039, [R-API-016](../spec/api-and-ui.md#r-api-016--rate-limits), the limits and usage rows of R-API-082,
  [R-AUTHZ-040](../spec/authorization.md#r-authz-040--capabilities-for-download-limits-and-usage), step 9a of
  [R-REPO-025](../spec/repositories-and-proxy.md#r-repo-025--remote-request-path), the amended R-ARCH-005 step 7, R-ARCH-031,
  R-ARCH-034, R-OPS-007, R-OPS-010, R-OPS-016, R-OPS-019.
- **Owns:** `internal/limits/…` (new), `internal/api/v1/ratelimit.go`, `limits_api.go`, `usage_api.go` (new),
  `internal/api/edge/content.go`, `internal/api/dispatch/dispatcher.go` (the writer), `internal/api/rawfiles/handler.go` (the
  writer and `Admit`), `internal/api/registry/token.go` (`Retarget`), `internal/proxy` (`Request.Charge`, `mayFetch`, the leader's
  charge, the counted and paced body), `internal/formathost/remote.go` and `ingest.go` (passing the charge and `Admit`),
  `internal/qname` (the registry family), `internal/cluster` (the limiter changes, the family `lim`, the area `limits`),
  `internal/config` (the `limits` section, the comments of `api.rate_limits`), `internal/formats/formats.go` (`ContractVersion` 6),
  every format's `ContractVersion` literal, `internal/formats/testdata/contract.txt`, `internal/formats/formattest` (the kit check),
  `internal/formats/all/all_test.go` (the kind table), `internal/app/wire_api.go` and a new `wire_limits.go`, `deploy/dev/valkey.yaml`
  and the integration stack's Valkey user (the ACL), the integration set `test/integration/limits` (new), the e2e set `limits`.
- **Migrations:** 00082 (`limit_profile` with the five built-ins, `limit_assignment` with the built-in rules, `limit_ceiling`,
  `limit_project_maximum`, `limit_settings`, `limit_block`), 00083 (`usage_bucket`, `usage_limit_event`, `usage_daily_summary`),
  00084 and 00085 reserved.
- **Shared, and who owns the conflict:** `internal/httpserver/principal.go` (reads `Credential.TechnicalTokenID`; package 1 owns it);
  `api/openapi.yaml` (the limits and usage operations); `docs/developer/` pages of the proxy, Valkey and formats (sections of its own).
- **Tests:** unit as R-LIM-039; the integration set `limits` with three replicas through `testenv`, Valkey stopped, a replica killed
  with leases held; the sets `proxy`, `ociremote`, `files`, `packages`, `api` again (the stage moved); the contract test and the
  kind table for the six formats; the e2e set `e2e (limits)`. The attribution of technical tokens is tested with a credential
  carrying `TechnicalTokenID` before package 1 lands, against real tokens after.
- **Must not touch:** the identity tables and their services, the screens.

### Work package 3 · The screens

- **Requirements:** [R-UI-177](../spec/api-and-ui.md#r-ui-177--download-limits-in-the-admin-area) to R-UI-180,
  [R-UI-182](../spec/api-and-ui.md#r-ui-182--the-mode-switch) to R-UI-186, and the **Limit** and **Last 24 hours** columns and the
  **Set limit** action of R-UI-181.
- **Owns:** `web/src/app/features/limits/` and `web/src/app/features/usage/` (new: the admin pages, the profile editor, the rule
  table and dialog `<quader-limit-rules>`, the overview, the detail, the mode switch), the project settings' and the repository
  settings' section **Download limits** as components of their own, the profile's tab **Usage**, the download-refused notice in the
  shared error handling, the principal field's two kinds (`web/src/app/shared/ui/principal-field.ts`), the routes and side-bar
  entries (`app.routes.ts`, `project-context.routes.ts`, `core/navigation.ts`, `profile.routes.ts`, `profile-nav.ts`).
- **Migrations:** none.
- **Shared, and who owns the conflict:** `settings-sections.ts`, `repository-detail.ts`, the project settings page, the service
  account page of package 1 (it adds the two columns and the action there after package 1 landed), `screens.ts`, `seed.ts`, the
  locale files; package 3 owns each conflict.
- **Builds against fakes:** the generated client of the operations of R-API-082 once package 2's `api/openapi.yaml` exists; until
  then hand-written types from the JSON examples of R-LIM-031 to R-LIM-035, replaced by the generated ones before it lands, and
  component tests against fixtures of those examples.
- **Verifiable only after package 2 landed:** every render with real data (the seed needs the operations), the overview's sort and
  states on stored usage, the CSV export, the mode preview's numbers, the explanation, the notice of a real 429. After package 1:
  the token columns and **Set limit**.
- **Tests:** component tests per screen; renders of every screen of R-UI-177 to R-UI-186 in both modes (`audit`, `enforce`),
  light and dark, 1440 and 390, as `admin`, `power` (an Owner) and `plain` (own usage only), with long names, an IPv6 prefix, an
  empty state each; the journeys it changes type-check.
- **Must not touch:** Go code.

### Reserved identifiers

| Package | Requirement IDs (amend only) | Decision IDs it may take | Migrations |
|---|---|---|---|
| 1 | R-AUTH-045, R-AUTH-046, R-AUTHZ-039, R-UI-181 | Q-AUTH-418 to Q-AUTH-429, Q-AUTHZ-299 to Q-AUTHZ-309, Q-UI-740 to Q-UI-749 | 00080, 00081 |
| 2 | R-LIM-001 to R-LIM-039, R-API-016, R-API-082, R-AUTHZ-040 | Q-LIM-40 to Q-LIM-79, Q-API-350 to Q-API-369, Q-ARCH-513 to Q-ARCH-522, Q-REPO-332 to Q-REPO-339 | 00082 to 00085 |
| 3 | R-UI-177 to R-UI-180, R-UI-182 to R-UI-186 | Q-UI-750 to Q-UI-779 | none |

A gap a builder finds takes the next free `H-` number at the time it lands, after H-1012.
