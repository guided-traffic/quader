# Usage and limits

The download limits of [spec/usage-and-limits.md](../spec/usage-and-limits.md) (walk item W-26): who a request is charged to,
which rule applies, the counters in Valkey, the decisions at stage 7, before a download and before an upstream exchange, the pacing of
a body, the usage store behind the overview, the API and the screens. The technical tokens the limits charge are identity's
([identity.md](identity.md)); their limit and their last 24 hours are columns of the token list, described here.

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
| Repository of the address | [qname/address.go](../../internal/qname/address.go) `ParseProtocolAddress`; [wire_limits.go](../../internal/app/wire_limits.go) `limitsRepo` | `/api/<format>/…`, `/files/…` and `/v2/<project>/<key>/…` (`ParseRegistryAddress`, Q-LIM-41); on a host bound to a repository of the registry format (R-EP-007) every path below `/v2/` but `/v2/`, `/v2/_catalog` and `/v2/_token` names the bound repository (`dispatch.Dispatcher.BoundRepository`, the reading the match of `/v2/` uses); a path that names none is decided at the instance |
| A refusal's answer | [errors.go](../../internal/limits/errors.go) `refusal`, [engine.go](../../internal/limits/engine.go) `Engine.refuse`; [httplimit.go](../../internal/limits/httplimit/httplimit.go) `SetRefusalPolicy` | The problem of R-LIM-020 with "Your limits: <base URL>/ui/profile/usage" for a signed-in caller (the base URL of the endpoint the request came in on, `limits.Options.BaseURL`); the charge keeps the refusing figure's RateLimit policy (`"cached-hour"`, `"uncached-hour"`, `"upstream-hour"`, `"day"`, `"parallel"`, `"refused-hour"`, `qu` for bytes and downloads), which the counting writer adds to the 429 it writes beside the request figures' (stage 7 adds it to a refusal of its own) |
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
| The request record | [httplimit.go](../../internal/limits/httplimit/httplimit.go) `Log`; [dispatcher.go](../../internal/api/dispatch/dispatcher.go) `serveRoute` | `observability.LimitsFields`: `limits_subject`, `limits_place`, `limits_rule`, `limits_outcome`, `bytes_cached`, `bytes_uncached` as fields of their own, null off the protocol paths; the dispatcher records the repository and the format of what it serves, the stage the addressed repository where nothing else did (`project` is the repository's project); a refusal's `limit_mode` is the counter's mode, `cluster` or `fallback` (Q-LIM-51) |

## Package layout

| Package | Layer | Responsibility |
|---|---|---|
| [`internal/limits`](../../internal/limits) | domain | `figures.go` (figures, overrides), `subject.go` (subjects, address prefixes, `SubjectOf`), `model.go` (places, rules, profiles, ceilings, maxima, settings, the built-ins), `index.go` (`Index`, `Resolve`, the clamp), `charge.go` (`Charge` and its context), `engine.go` (`Check`, `Admit`, `MayFetch`, `FailedCredential`, `Retarget`, `System`), `flush.go` (the charge buffer, blocks, `LiftBlock`, `Live`), `pacer.go` (pacing, `CountingReader`), `errors.go` (the refusals of R-LIM-020), `metrics.go` |
| [`internal/limits/store`](../../internal/limits/store) | domain | The PostgreSQL store: `Snapshot` for the index, the writes of the API (audited, announcing the area `limits`), the record of blocks, `Names` (the names of the users, technical tokens, CI projects and personal tokens an answer names, read once per answer) and the searches of the rule dialog's suggestions |
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
| `TestR_LIM_034_TheRuleOfABudgetKeyIsTheOneThatAppliesThereNow` | `Index.ResolveAt`, the rule of a budget key in the detail (Q-LIM-52) |
| `TestR_LIM_001_TheSubjectOfEveryCaller`, `TestR_LIM_002_AddressPrefix`, `TestR_LIM_004_Figures` | Subjects, prefixes, figures |
| `TestR_LIM_017_RequestWindowsAndCost`, `TestR_LIM_021_…`, `TestR_LIM_022_StepNineA`, `TestR_LIM_023_Pacing`, `TestR_LIM_024_ParallelDownloads`, `TestR_LIM_025_TheBlock` | The engine on in-memory counters: windows, cost, Audit, the admission, 9a and the ceiling, pacing shares and the floor, leases and their fallback share, blocks and their lift |
| `TestR_LIM_036_EveryRouteOfEveryFormatCountsByItsKind` ([formats/all](../../internal/formats/all)) | The kind of every route of the six formats |
| Check 16 of the format kit ([checks_admission.go](../../internal/formats/formattest/checks_admission.go)) | Every format answers 429 before any byte when the admission refuses; the defect `WritesOwnDownload` fails it |
| [ratelimit_test.go](../../internal/api/v1/ratelimit_test.go) | The package classes on `/files/` and `/v2/`, keyed by the subject |
| [ifmatch_test.go](../../internal/api/v1/ifmatch_test.go) | A project's first maximum by `PUT` without `If-Match`, a replace with it (Q-API-354) |
| [requestlog_test.go](../../internal/observability/requestlog_test.go) | The limits' fields of the request record, null off the protocol paths |
| Integration set `limits` ([test/integration/limits](../../test/integration/limits)) | A cached download's round trips, Audit and Enforce, a failed credential, the overview; `TestR_LIM_001_ARegistryTokenOfATechnicalTokenIsChargedToTheTechnicalToken` (a docker-style exchange with a technical token's secret, then a `/v2/` request); `TestR_LIM_028_ADeletedTechnicalTokensRuleGoesAndItsUsageStays` (the token record's `limit`, a run of `limits-cleanup`, which also runs the fold of the hourly rows into the daily sums: `FoldUsageDays` grouped by an expression its subqueries named ungrouped and failed every run until 2026-11-19); `TestR_LIM_033_AProjectsFirstMaximumTakesNoIfMatch`; `TestR_LIM_019_WithValkeyStoppedTheReplicaDecidesOnItsShare` |

What no test here holds yet: a replica that dies with leases held (R-LIM-024's expiry of its leases after `limits.parallel.lease`;
the integration stack stops a replica only gracefully, which releases its leases), and the e2e set `limits` of R-LIM-039 (the six
clients against a small profile in both modes on the kind cluster).

## The screens

R-UI-177 to R-UI-186. Every screen reads the API through one service, `LimitsApi`; its shapes are the generated client's.

| File | Responsibility |
|---|---|
| [web/src/app/api/limits-types.ts](../../web/src/app/api/limits-types.ts) | The shapes of R-LIM-031 to R-LIM-035 as `Schema<'…'>` of the generated client (`api/openapi.yaml`): the unions of a rule's "who", a place, a usage subject and a credential are discriminated by `kind` in the document, so a screen narrows on it |
| [web/src/app/api/limits-api.ts](../../web/src/app/api/limits-api.ts) | `LimitsApi` and `HttpLimitsApi`: profiles, rules of a place (`rulesPath`), ceilings, settings and the mode preview, a project's maximum (a first one without `If-Match`, Q-API-354), `projectRows` (`GET /api/v1/limits/projects` and the project list for the display names, Q-UI-756), blocks, the explanation, the subjects of the rule dialog, the usage lists (the instance's filter **Project** is `UsageScope.projectFilter`, `project=`, Q-UI-755), details and CSV address |
| [web/src/app/api/testing/fakes-limits.ts](../../web/src/app/api/testing/fakes-limits.ts) | `FakeLimitsApi` and the fixtures from the specification's examples: the built-in profiles and an own one, rules of the three places, a maximum, ceilings, a block, a usage list with a long name, an IPv6 prefix, a caller at 100 %, an unlimited one, a blocked one, a CI project and the system, a detail with 288 five-minute points |
| [web/src/app/features/limits/](../../web/src/app/features/limits/) | `figures.ts` (groups, units, decimal MB and GB, the compact line), `callers.ts` (how a rule's "who", a subject and a place are named), `caller-cell.ts`, `figure-fields.ts` (the eleven fields, with a profile's values as base for overrides), `limit-rules.ts` (`<quader-limit-rules>`, the table of every place), `rule-dialog.ts`, `limit-mode.ts` (the mode line and the switch's dialog), `limits-frame.ts` and the four tabs `limits-*-tab.ts`, `profile-editor.ts`, `maximum-dialog.ts`, `project-limits-section.ts`, `repository-limits-section.ts` (with the ceilings), `technical-token-limit.ts` (the cell **Limit** with its second line, the last 24 hours, and `TechnicalTokenLimits`, R-UI-181), `limits.routes.ts` |
| [web/src/app/features/usage/](../../web/src/app/features/usage/) | `usage-overview.ts` (the overview of the instance and of a project, its filters in the address, Q-UI-755), `usage-detail.ts` (a caller's page), `profile-usage.ts` (the profile's tab), `usage-budgets.ts` (the limits per budget key), `limit-explain.ts`, `usage-history.ts` (three charts and the table, Q-UI-754), `usage-tables.ts`, `usage-meter.ts`, `usage-words.ts`, `usage.routes.ts` with `usageGuard` |
| [web/src/app/shared/download-link.ts](../../web/src/app/shared/download-link.ts) | `quaderDownload` on a download link: a one-byte probe, the notice of a refusal (R-UI-186, Q-UI-753); the words are [problem-messages-limits.ts](../../web/src/app/core/http/problem-messages-limits.ts) |
| [web/src/app/shared/ui/principal-field.ts](../../web/src/app/shared/ui/principal-field.ts) | The kinds `technical_token` and `ci_project`, whose matches come from `GET /api/v1/limits/subjects` (every technical token by its or its account's name, the CI projects tokens were issued for, Q-LIM-50) |

- **Routes and side bars.** `/admin/usage`, `/admin/usage/{kind}/{id}`, `/admin/limits` with its tabs and
  `/admin/limits/profiles/{new,key}` ([app.routes.ts](../../web/src/app/app.routes.ts)); `/projects/{project}/usage` and its callers
  ([project-context.routes.ts](../../web/src/app/features/projects/project-context.routes.ts)); `/profile/usage`. The side-bar entries
  are in [navigation.ts](../../web/src/app/core/navigation.ts) (Q-UI-750), the profile's tab in `profile-nav.ts`.
- **Technical tokens (R-UI-181, Q-UI-759, Q-UI-761).** The section **Technical tokens** of a service account's page
  ([technical-tokens.ts](../../web/src/app/features/service-accounts/technical-tokens.ts)) reads the profiles and the last 24 hours once
  with the list and hands them to [technical-token-table.ts](../../web/src/app/features/service-accounts/technical-token-table.ts)
  (`TokenLimitsView`): the column **Limit** between **Token** (with the secret's expiry beside its hint) and **Last used**, the token record's `limit` (R-AUTH-045) on the row's
  line and the last 24 hours as the cell's second line ("95,695 requests · 518.8 GB in 24 h", "Not used in 24 h"); the limit opens
  the rule dialog (**Set limit**, **Change limit** with **Remove limit** in it), and the action cell keeps **Rotate** and **Revoke**.
  The column shows for `limits:read` and an Owner of the account's project, its second line for whoever reads the usage of the scope;
  the list of ended tokens shows no limit, so that both lists are tables at 1,024 px.
- **A caller's page (R-UI-184, Q-UI-765).** The title is `subjectTitle` of [callers.ts](../../web/src/app/features/limits/callers.ts)
  (a user's display name, a token's name, a CI project's path, an address), the reference under it `subjectReference`; the route's
  title resolver `callerTitle` in [usage.routes.ts](../../web/src/app/features/usage/usage.routes.ts) reads the same name for the
  breadcrumb's last step and the window's title.
- **The token record's `limit`.** [technical_tokens_api.go](../../internal/api/v1/technical_tokens_api.go) `tokenLimits` reads the rules
  of the account's place that name one of its tokens from the store; the read tag of a token follows its rule's change
  (`technicalETag`), a write compares the revision only.
- **Renders.** The screens `admin-usage*`, `admin-limits*`, `project-usage*`, `project-settings-limits`, `repository-settings-limits`,
  `profile-usage` and the service accounts' pages (`*-service-account*`, with the state `set-limit`) in
  [screens.ts](../../web/e2e/renders/screens.ts); Enforce is rendered through the page's routes (`inEnforce`), never by switching the
  shared database. `seedLimits` in [seed.ts](../../web/e2e/renders/seed.ts) writes the profile, the rules, a maximum and the ceilings
  through the API; `seedUsage` writes a day of five-minute and a month of hourly usage of every kind of caller, apart by orders of
  magnitude, the limit events, a block and the CI issuer of a CI project into the profile's database through its container
  (`psql`): requests cannot make a day's history in the seed's time, and Quader has no path that writes it.
- **The overview's columns.** The usage overview names `narrowBelow="80rem"` on its data table: **Upstream requests** and **Refused**
  give way below 80 rem of the list, so the list is a table, not stacked rows, beside the side bar at 1440 px (Q-UI-760). A caller's
  history table is the shared data table too.
- **Profile names.** A rule of the detail and the explanation carries `profileName` (Q-LIM-53): the profile tab of a user names the
  profile its limits come from without reading the profiles.
- **Tests.** [figures.spec.ts](../../web/src/app/features/limits/figures.spec.ts), [limits.spec.ts](../../web/src/app/features/limits/limits.spec.ts),
  [usage.spec.ts](../../web/src/app/features/usage/usage.spec.ts) against the fakes, each screen through axe.
