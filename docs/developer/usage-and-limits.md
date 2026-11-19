# Usage and limits

Status: skeleton of the subsystem's developer page. The specification is [spec/usage-and-limits.md](../spec/usage-and-limits.md)
(walk item W-26); nothing of it is built yet. This page names where it runs, the request path with the hook points as they exist on
2026-11-19, and the three work packages that build it in parallel. Each builder fills its sections here as it lands; the section
"Work packages" is removed when the third has landed.

## What has to be in your head first

- **Two budgets per request.** The subject's budget at the place whose rule applies (a budget key), and for an upstream exchange
  the remote's ceiling of the caller's class. Both hold.
- **The subject is not the credential.** A user's sessions and personal tokens share one subject; a technical token is a subject
  across its rotations; a CI token is charged to its GitLab project; an anonymous caller to its address prefix
  ([R-LIM-001](../spec/usage-and-limits.md#r-lim-001--the-subject-a-request-is-charged-to)).
- **Valkey enforces, PostgreSQL remembers.** Counters are windows in Valkey; the usage store is a per-replica buffer flushed to
  PostgreSQL every ten seconds, read by the overview and never by a request.
- **Audit counts everything and applies nothing new.** In `audit` the package classes of R-API-016 are the safety net; in `enforce`
  the profiles take over ([R-LIM-026](../spec/usage-and-limits.md#r-lim-026--audit-and-enforce)).
- **The format contract is version 6.** A route's kind decides what is counted; `Host.Serve` admits downloads
  ([R-LIM-036](../spec/usage-and-limits.md#r-lim-036--the-format-contract-version-6)).

## What runs where

| Role | What it does for the limits |
|---|---|
| `packages` (and `web`, `all`) | stage 7 (subject, place, request figures, block), the download admission in `edge.ServeItem`, step 9a in the remote engine, the counting and pacing writer, the per-replica charge buffer (1 s to Valkey) and usage buffer (10 s to PostgreSQL) |
| `api` (and `web`, `all`) | the limits and usage API, the explanation; protocol requests do not reach it except `/files/` on an endpoint that serves it |
| `worker` | the daily task `limits-cleanup` (fold, retention, erasure); background requests are charged to `system` |

## The request path and its hook points

The functions named exist on 2026-11-19; the change each gets is the specification's.

| Step | File and function today | What the limits add |
|---|---|---|
| Stage 6, authentication | [internal/auth/stage.go](../../internal/auth/stage.go); the token cache entry in [internal/auth/token](../../internal/auth/token) | `httpserver.Credential.TechnicalTokenID` from the cache entry (work package 1); a failed credential marks the request refused (R-LIM-008) |
| Stage 7 | [internal/api/v1/ratelimit.go](../../internal/api/v1/ratelimit.go) `NewRateLimit`, `classify`, `limitedPath`, `isPackagePath`, `ChallengedByLevels`; wired in [internal/app/wire_api.go](../../internal/app/wire_api.go) | `limitedPath` covers `/v2/`; the package classes keyed by the subject and in force in `audit` only; a new `limits.Stage` that derives the subject (`limits.Charge`), resolves the place on `limits.Index`, checks the request windows and the block in the same `MULTI` |
| Repository of the address | [internal/qname](../../internal/qname) `ParseAddress` | the family `registry` for `/v2/<projectKey>/<repoKey>/…`; the host binding of R-EP-007 |
| The format dispatcher | [internal/api/dispatch/dispatcher.go](../../internal/api/dispatch/dispatcher.go) `(*Dispatcher).serveRoute`, `resolve` | wraps the response in the counting and pacing writer for the matched route's `Kind`; marks `repository_not_found` refused |
| The raw-file handler | [internal/api/rawfiles/handler.go](../../internal/api/rawfiles/handler.go) (its call of `edge.ServeItem`) | the same writer; `ServeOptions.Admit` |
| The registry root and token service | [internal/api/registry](../../internal/api/registry) `token.go`, `register.go` | the writer for `/v2/`; `limits.Charge.Retarget` after the token service authenticated (R-LIM-003) |
| Serving a stored item | [internal/api/edge/content.go](../../internal/api/edge/content.go) `ServeItem`, `ServeOptions`; called by `Host.Serve` in [internal/formathost/ingest.go](../../internal/formathost/ingest.go) | `ServeOptions.Admit` before `open` and the status line (R-LIM-021) |
| The remote request | [internal/formathost/remote.go](../../internal/formathost/remote.go) `(*remote).request` | `proxy.Request.Charge` from the request context |
| Step 9 and 9a | [internal/proxy/engine.go](../../internal/proxy/engine.go) `mayFill`, `denyFill`; called from `(*Engine).Artifact` in [artifact.go](../../internal/proxy/artifact.go) and `beforeFetch` in [metadata.go](../../internal/proxy/metadata.go) | `(*Engine).mayFetch` after `mayFill`: upstream figures and ceiling, stale within `maxStaleSeconds` or 429 (R-LIM-022) |
| The upstream exchange | [internal/proxy/fetch.go](../../internal/proxy/fetch.go) `(*Engine).fetch`; the bodies in `(*fetcher).artifact` and `(*fetcher).metadata` | counts one upstream request per exchange; counts and paces the uncached bytes on the leader's charge |
| Collapsing | [internal/proxy/flight.go](../../internal/proxy/flight.go) `collapse`, `runFlight`, `detached` | the leader's charge rides on the flight's request; followers carry none (R-LIM-006) |
| Statistics, for comparison | [internal/items/stats.go](../../internal/items/stats.go) `Stats` | the model of the usage buffer: write-only, flushed in key order, at shutdown |
| Counters | [internal/cluster/limiter.go](../../internal/cluster/limiter.go), [valkey_ratelimit.go](../../internal/cluster/valkey_ratelimit.go), [valkey_keys.go](../../internal/cluster/valkey_keys.go) | per-call policies, windows to 24 hours, `INCRBY`, leases in a sorted set, the key family `lim` |
| Invalidation | [internal/cluster/areas.go](../../internal/cluster/areas.go) | the change area `limits` |

## Package layout (to be created)

| Package | Responsibility |
|---|---|
| `internal/limits` | figures, profiles, rules, `Index` and `Resolve`, the clamp, `Charge` (the subject and budget key of a request), the decisions, the stage, the writer and the pacer, the charge buffer |
| `internal/limits/usage` | the usage buffer, its flush, the reads of the overview, the detail, the export, `limits-cleanup` |
| `internal/limits/store` | the PostgreSQL store of profiles, rules, ceilings, maxima, settings, blocks |
| `internal/api/v1/limits_api.go`, `usage_api.go` | the operations of R-API-082 for limits and usage |

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
