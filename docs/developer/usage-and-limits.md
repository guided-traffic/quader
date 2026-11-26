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
- **A refused request fills no request window.** Stage 7 decides and counts in one step, a script inside its one round trip (the
  take, Q-LIM-57); every limiter of R-ARCH-034 takes the same way (Q-LIM-61). A caller that keeps sending is held
  to its request figure's rate, nothing more: a request figure's refusal is not a refused request and never moves `refusedPerHour`
  (the owner's decision, Q-LIM-58). Step 9a takes the same way, and what a check after a take refuses is given back to it: an upstream
  request the parallel lease refused, a request the endpoint's class admitted and stage 7 refused (Q-LIM-110).
- **A request writes no row and costs at most three round trips:** one at stage 7, one before a download from the cache, one at step
  9a for a miss. Bytes, refusals, the end of a download and lease renewals ride on the replica's flush, once a second.
- **Enforce is the default; Audit counts everything and applies nothing.** A new instance starts in `enforce` (`DefaultSettings`,
  the migration's default). In `audit` nothing on a protocol path is refused or slowed, no package class of R-API-016 either
  (`packageClass` in [ratelimit.go](../../internal/api/v1/ratelimit.go) answers `package_write` for an unsafe method in `enforce`
  and nothing else; Q-LIM-4, the owner's decision of 2026-11-19)
  ([R-LIM-026](../spec/usage-and-limits.md#r-lim-026--audit-and-enforce)). The render seed switches its shared database to `audit`
  (`seedAuditMode`), so the seed's own requests are never refused and the pictures show what Audit records; an integration test
  of another mechanism that sends more than a profile admits (hundreds of parallel transfers of one user) asks for `audit` with
  `Stack.LimitsMode` of [testenv](../../test/integration/testenv/stack.go) before its replica starts.
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
| Stage 7 | [ratelimit.go](../../internal/api/v1/ratelimit.go) `protocolStage.serve` | Skips a preflight and a request the decision point challenges (Q-API-290, on the registry paths Q-API-357, and there a write without a credential at any level, Q-OCI-225). Derives the caller (`httplimit.CallerOf`, the administrator through `authz.Index.HoldsAdmin`), resolves the place (`Engine.Begin` → `Index.Resolve`), names the endpoint's gate of waiting downloads (`waitGates.of`, `Charge.SetWaitGate`, Q-LIM-60), and runs one batch (`Engine.Check`): one take (`cluster.OpTake`, the script of [valkey_take.go](../../internal/cluster/valkey_take.go)) of the burst and hour windows of the budget key, its block marker, and the package class of R-API-016 in force (`cluster.Limiter.BatchTake`), which counts the request in every window or, when anything refuses it, in none (Q-LIM-57); a request a request figure refuses is not marked refused (`checkWindow`, Q-LIM-58). A refusal gives the request back to the endpoint's class of R-EP-015, which took it on its own before the stage (`giveBackEndpointClass` in [endpoint_ratelimit.go](../../internal/api/v1/endpoint_ratelimit.go), `cluster.Limiter.GiveBack`, Q-LIM-110); so does a refusal of a class of R-API-016 on the management API. Wraps the response in `httplimit.Writer` and puts the charge in the context; at the end writes the log fields and finishes the charge |
| Repository of the address | [qname/address.go](../../internal/qname/address.go) `ParseProtocolAddress`; [wire_limits.go](../../internal/app/wire_limits.go) `limitsRepo` | `/api/<format>/…`, `/files/…` and `/v2/<project>/<key>/…` (`ParseRegistryAddress`, Q-LIM-41); on a host bound to a repository of the registry format (R-EP-007) every path below `/v2/` but `/v2/`, `/v2/_catalog` and `/v2/_token` names the bound repository (`dispatch.Dispatcher.BoundRepository`, the reading the match of `/v2/` uses); a path that names none is decided at the instance |
| A refusal's answer | [errors.go](../../internal/limits/errors.go) `refusal`, [engine.go](../../internal/limits/engine.go) `Engine.refuse`; [httplimit.go](../../internal/limits/httplimit/httplimit.go) `SetRefusalPolicy` | The problem of R-LIM-020 with "Your limits: <base URL>/ui/profile/usage" for a signed-in caller (the base URL of the endpoint the request came in on, `limits.Options.BaseURL`); the charge keeps the refusing figure's RateLimit policy (`"cached-hour"`, `"uncached-hour"`, `"upstream-hour"`, `"day"`, `"parallel"`, `"refused-hour"`, `qu` for bytes and downloads), which the counting writer adds to the 429 it writes beside the request figures' (stage 7 adds it to a refusal of its own) |
| The format dispatcher | [dispatcher.go](../../internal/api/dispatch/dispatcher.go) `serveRoute` | Marks a refusal of the decision point (`DenyHidden`, `DenyVisible`) and a credentialed `repository_not_found` as refused (R-LIM-008); records the repository that served |
| The raw-file handler | [rawfiles/handler.go](../../internal/api/rawfiles/handler.go) `get`, `refusedAccess` | The same refusal mark; `ServeOptions.Admit` for a file |
| The registry's token service | [registry/token.go](../../internal/api/registry/token.go) `Token.ServeHTTP` | Stage 7 deferred the request windows (`DeferRequestFigures`); the handler charges them once, to the token's subject or to the address (`Charge.Retarget`, Q-LIM-48). `Engine.retarget` moves the request's own charge to that subject; it makes no second charge |
| Serving a stored item | [edge/content.go](../../internal/api/edge/content.go) `ServeItem` `ServeOptions.Admit`; [formathost/ingest.go](../../internal/formathost/ingest.go) `itemStore.Serve` | For a `GET` of a `Download` route, after the conditionals and the range and before `open`: the hour and day byte windows and a parallel lease, one batch (`Engine.Admit`); a refusal is returned unwritten. A lease above the figure waits (`Engine.parallel`) only while the endpoint's gate has room (`WaitGate`, a quarter of the endpoint's `max_in_flight`), else it is refused at once |
| Step 9 and 9a | [proxy/fetch.go](../../internal/proxy/fetch.go) `fetcher.run` | The fetch's first act: `Charge.MayFetch` runs one take (Q-LIM-110): the upstream request windows of the budget key and the ceiling take the request, their uncached hour windows are only checked (`cluster.WindowTake.Peek`); beside it the lease of an artifact `GET`. A request a figure refuses is counted nowhere; one the lease refuses (also after its wait) is given back (`Engine.giveBack`, `cluster.OpGiveBack`); the usage store and the metric count an upstream request only when it goes on. A refusal is the leader's own outcome (`reqState.budget`, Q-LIM-43); `metadataAnswer` serves an expired document stale within `maxStaleSeconds` (`detail=stale-budget`). A `HEAD` the engine forwards (`artifact.go` `head`) and a forwarded search (`query.go` `queryFetch`) pass the same check |
| The upstream exchange | [proxy/fetch.go](../../internal/proxy/fetch.go) `fetcher.send`, `fetcher.bodyReader` | Every exchange after the first is one more upstream request; the body is counted as uncached bytes and paced at `uncachedBytesPerSecond` (`limits.CountingReader`) |
| The leader's stream | [proxy/integrity.go](../../internal/proxy/integrity.go) `stream.flushLocked` | Marks the charge streaming while it writes: the writer passes those bytes uncounted (they were counted where read) |
| Collapsing, detached fetch | [proxy/flight.go](../../internal/proxy/flight.go) | The flight runs on the leader's context, so on its charge; `Charge.Hold` keeps it in the buffer until the fetch ended, also after the client left (Q-LIM-5) |
| Background requests | `fetcher.run` | `Engine.System(repo)`: upstream requests and uncached bytes recorded under `system`, checked against nothing; a request that carries `proxy.Request.Charge` (a probe's background download) is charged to it instead, `Charge.Exchange` counting every exchange after the first |
| A probe's background download | [formathost/probe.go](../../internal/formathost/probe.go) `decide`; [limits/background.go](../../internal/limits/background.go) | The decision runs `Charge.StartBackground`: a charge of the probe's subject and budget key (`Charge.Background`) passes step 9a now, counted for the key and the ceiling; a refusal in enforce starts nothing and counts no background download. The admitted charge travels with the job as a `limits.Ticket`, and the worker resumes it (`Engine.Resume`): uncached bytes for the key and the ceiling, no lease, no pacing (R-LIM-037, Q-LIM-54). Through a virtual repository the key is the one stage 7 resolved at the virtual repository and `StartBackground` is called with the serving member's ID: its ceiling, its usage row (Q-LIM-2, Q-REPO-367) |
| The flush | [limits/flush.go](../../internal/limits/flush.go) `Engine.Flush` | Once a second, one batch: `INCRBY` of the bytes and refusals of every budget key and ceiling, `ZREM` of ended leases, `ZADD XX` renewals of leases older than 20 s, `ZCARD` of paced keys, the block markers found due; then the deltas to the usage buffer. A request's own count and the limits it met go to the usage buffer only at the first flush after the request ended (`Engine.Finish`), when its subject (after `Retarget`) and the repository that served it (after `Served`, `ServedThrough`) are final; its bytes, upstream requests and refusal go at every flush, under the subject and repository known then. A long download's request is therefore in the five-minute bucket of its end. Ended leases are dropped and renewed on the replica's share too, and a drop whose batch ran on the share is kept (`Engine.unsent`) and sent with every flush until one reaches Valkey or the lease has ended there (Q-LIM-95): a lease can be in either place, or in both after a take that ran into the command timeout and that Valkey ran afterwards |
| The usage buffer | [limits/usage/buffer.go](../../internal/limits/usage/buffer.go) `Buffer.Flush` | Every ten seconds one transaction: one upsert of the five-minute and hourly rows, one of the limit events, in key order; a row's key holds the serving repository and the virtual repository of another project it was reached through ([below](#the-usage-of-a-well-known-member-through-a-teams-virtual-repository)) |
| The request record | [httplimit.go](../../internal/limits/httplimit/httplimit.go) `Log`; [dispatcher.go](../../internal/api/dispatch/dispatcher.go) `serveRoute` | `observability.LimitsFields`: `limits_subject`, `limits_place`, `limits_rule`, `limits_outcome`, `bytes_cached`, `bytes_uncached` as fields of their own, null off the protocol paths; the dispatcher records the repository and the format of what it serves, the stage the addressed repository where nothing else did (`project` is the repository's project); a refusal's `limit_mode` is the counter's mode, `cluster` or `fallback` (Q-LIM-51) |

## Package layout

| Package | Layer | Responsibility |
|---|---|---|
| [`internal/limits`](../../internal/limits) | domain | `figures.go` (figures, overrides), `subject.go` (subjects, address prefixes, `SubjectOf`), `model.go` (places, rules, profiles, ceilings, maxima, settings, the built-ins), `index.go` (`Index`, `Resolve`, the clamp), `charge.go` (`Charge` and its context), `engine.go` (`Check`, `Admit`, `MayFetch`, `FailedCredential`, `Retarget`, `System`), `waitgate.go` (`WaitGate`, `WaitingShare`: the waiting transfers' share of an endpoint's request slots), `background.go` (`Ticket`, `StartBackground`, `Resume`, `Exchange`), `flush.go` (the charge buffer, blocks, `LiftBlock`, `Live`), `pacer.go` (pacing, `CountingReader`), `errors.go` (the refusals of R-LIM-020), `metrics.go` |
| [`internal/limits/store`](../../internal/limits/store) | domain | The PostgreSQL store: `Snapshot` for the index, the writes of the API (audited, announcing the area `limits`), the record of blocks, `Names` (the names of the users, technical tokens, CI projects and personal tokens an answer names, read once per answer) and the searches of the rule dialog's suggestions |
| [`internal/limits/usage`](../../internal/limits/usage) | domain | The usage buffer, the reads of the overview and the detail, `limits-cleanup` |
| [`internal/limits/httplimit`](../../internal/limits/httplimit) | edge | `ProtocolPath`, `Repo`, `CallerOf`, the counting and pacing `Writer`, the log fields, the `RateLimit` fields |
| [`internal/cluster/valkey_limits.go`](../../internal/cluster/valkey_limits.go), [`valkey_take.go`](../../internal/cluster/valkey_take.go) | infrastructure | `LimitBatch`, `LimitCounters` (Valkey), `MemoryLimits` (the replica's fallback and the unit tests), `Estimate`, `RoomAfter`; the take (`OpTake`, `WindowTake` with `Peek` for a window that is only checked, the script, `EVAL` after `NOSCRIPT`) and its give-back (`OpGiveBack` in the same script, `MemoryLimits.Return`; `Limiter.GiveBack` for a limiter's own take, Q-LIM-110) |

Migrations: [00081_limits.sql](../../internal/meta/postgres/migrations/00081_limits.sql) (profiles with the five built-ins, rules with
the built-in rule set, ceilings, project maxima, settings, blocks, the change area `limits`),
[00082_usage.sql](../../internal/meta/postgres/migrations/00082_usage.sql),
[00085_limits_request_bursts.sql](../../internal/meta/postgres/migrations/00085_limits_request_bursts.sql) (the built-ins' request
figures from the measurement of Q-LIM-59; `BuiltinProfiles` in `model.go` holds the same numbers),
[00087_usage_via.sql](../../internal/meta/postgres/migrations/00087_usage_via.sql) (`usage_bucket.via_repository_id` in the row's key,
Q-LIM-63) and [00141_limits_parallel_transfers.sql](../../internal/meta/postgres/migrations/00141_limits_parallel_transfers.sql) (the
figure `parallelDownloads` renamed `parallelTransfers` in the column, the overrides and the limit events, Q-LIM-293; the built-ins'
parallel transfers from the measurement of Q-LIM-294, set only where an administrator left the old value, Q-LIM-295; its test
`TestR_HA_020_TheParallelTransfersStepKeepsAnAdministratorsValue` in the integration area `postgres`); queries in
[limits.sql](../../internal/meta/postgres/queries/limits.sql) and [limits_usage.sql](../../internal/meta/postgres/queries/limits_usage.sql).

## The usage of a well-known member through a team's virtual repository

A virtual repository of a team may list a repository of the built-in project `upstream` ([wellknown.md](wellknown.md)). Which limits
apply to such a request is Q-LIM-2's rule, unchanged ([Q-LIM-62](../spec/usage-and-limits.md#q-lim-62--a-request-through-a-virtual-repository-to-a-well-known-member-is-decided-at-the-addressed-repository-and-the-members-ceilings-bound-it)):
stage 7 resolves the place at the addressed virtual repository, so the team's rules decide, and step 9a charges the ceiling of the
remote that fetches, the well-known member's (`Charge.MayFetch` with the member's ID), so its ceilings bound every project's callers
together. Nothing in the engine knows the project `upstream`.

What it records ([Q-LIM-63](../spec/usage-and-limits.md#q-lim-63--the-usage-of-a-well-known-member-through-a-teams-virtual-repository-is-recorded-with-that-virtual-repository),
[Q-LIM-75](../spec/usage-and-limits.md#q-lim-75--via_repository_id-names-the-request-a-well-known-member-served-and-nothing-else)):

- The bridge's tiers ([formathost/virtual.go](../../internal/formathost/virtual.go), `tiers.through`) call
  `Charge.ServedThrough(member, virtual)` when a member of `upstream` serves a request of a virtual repository of another project:
  before a stored item is served, and before the fetch of the remote tier, putting the addressed repository back after a fetch that
  found nothing. `Charge.viaOf` names the virtual repository only while that member is the repository that served.
- The flush hands `UsageDelta.ViaRepoID` to the usage buffer, which keys the row by it; `UpsertUsageBuckets` writes
  `via_repository_id` (NULL for a virtual repository deleted since). One member reached through two virtual repositories is two rows.
- The reads of a scope (`UsageTotals`, `UsageByPlace`, `UsageHistory`, `UsageRepositories`, `UsageCredentials`) take a row whose
  serving repository or whose `via_repository_id` is in the scope, so a project's Owner, and the overview narrowed to the virtual
  repository, see what the team's builds took from the central repository (R-LIM-029), and the central repository's own scope keeps
  its total.
- Not carried: a merged document of several members (no member served it; its row is the addressed repository's, as before), the limit
  events (`usage_limit_event` keeps the serving repository) and the daily sums.

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

## A multi-platform push against package_write, measured

Measured once on 2026-11-25, a measurement and not a test (Q-LIM-56's open question), repeated by

```shell
QUADER_TEST_MEASURE_PUSH=2x65,2x70,2x100 test/integration/run.sh -run TestMeasurePackageWriteUnderAMultiPlatformPush -v ./test/integration/limits/
```

([push_measure_test.go](../../test/integration/limits/push_measure_test.go); without the variable it is skipped). The set-up: the integration
stack (instance 1, then instance 2), one replica with Valkey, the classes of R-API-016 at their defaults
(`QUADER_API_RATE_LIMITS_ENABLED=true` and nothing else), the built-in profiles, Enforce; a local OCI repository; per push a person of its
own with a token of the default scope (the class's bucket is the subject). The client sends what a registry client sends: the version check
without a credential (the challenge, not charged), the token by the password grant (`POST /v2/_token`, one unsafe request), per platform the
configuration and the layers, five blobs at a time, each a `HEAD` and then docker push's `POST`, `PATCH`, `PUT` or buildx's `POST`, `PUT`
with the body, then a manifest per platform and the index. Every layer is a few dozen bytes. Each push runs once as a client that ends at
the first 429 (docker did on the kind cluster on 2026-11-20) and once as one that waits each `Retry-After` and sends the request again. The
development host has 20 cores; load average 8 to 11 from two other builders.

| Push (platforms × layers, requests per blob) | Unsafe requests | Burst 400 (06:25, load 8): a client that ends at the first 429 | Burst 400: a client that honours `Retry-After` | Burst 1,600 (06:34, load 11) |
|---|---|---|---|---|
| 2 × 65, docker push | 400 | passes, 2.9 s | passes, 3.0 s | — |
| 2 × 70, docker push | 430 | 429 at the 400th, after 2.5 s, `Retry-After` 32 s | finishes in 34.9 s, 15 answers 429 | — |
| 2 × 100, docker push | 610 | 429 at the 400th, after 2.5 s, `Retry-After` 30 s | finishes in 51.1 s, 101 answers 429 | passes, 4.7 s |
| 2 × 70, buildx | 288 | passes, 2.6 s | passes | — |
| 2 × 100, buildx | 408 | 429 at the 400th, after 3.6 s, `Retry-After` 15 s | finishes in 15.9 s, 4 answers 429 | passes, 4.9 s |
| 2 × 127, docker push / buildx | 772 / 516 | — | — | passes, 6.2 s / 4.8 s |
| 4 × 127, docker push / buildx | 1,542 / 1,030 | — | — | passes, 10.7 s / 9.8 s |
| 5 × 127, docker push | 1,927 | — | — | 429 at the 1,600th after 11.4 s, `Retry-After` 140 s; a client that honours it finishes in 2 min 52 s |

The busiest second held 105 to 166 unsafe requests in every run: the rate of 10 a second never decides a push, the burst does. The 429
names the class (`RateLimit: "package_write";r=0;t=30`, `RateLimit-Policy: "package_write";q=400;w=40`); the profile's request figures
(Standard, 4,000 in 20 s) had room left in each run. What it meant: with the burst of 400, docker's first run of a push of an image of two
platforms with 66 or more distinct layers each failed, and a second run or a client that waits finished within a minute. The burst is
1,600 since [Q-LIM-292](../spec/usage-and-limits.md#q-lim-292--the-burst-of-package_write-holds-the-push-of-the-largest-image-on-four-platforms):
four platforms of the largest image docker builds. Not measured: the real clients against the new default (the kind cluster's case of
Q-LIM-56 pushes one platform of 40 layers), and whether a client other than docker retries a 429 of a push.

## Tests

| Test | What it holds |
|---|---|
| `TestR_LIM_013_WorkedExamples`, `TestR_LIM_016_TheClamp` | The worked examples of R-LIM-013 and R-LIM-016 on `Index.Resolve` and `Index.CapFor` |
| `TestR_LIM_034_TheRuleOfABudgetKeyIsTheOneThatAppliesThereNow` | `Index.ResolveAt`, the rule of a budget key in the detail (Q-LIM-52) |
| `TestR_LIM_001_TheSubjectOfEveryCaller`, `TestR_LIM_002_AddressPrefix`, `TestR_LIM_004_Figures` | Subjects, prefixes, figures |
| `TestR_LIM_017_RequestWindowsAndCost`, `TestR_LIM_017_ARefusedRequestFillsNoRequestWindow`, `TestR_LIM_008_ARequestFiguresRefusalIsNoRefusedRequest`, `TestR_LIM_021_…`, `TestR_LIM_022_StepNineA`, `TestR_LIM_023_Pacing`, `TestR_LIM_024_ParallelTransfers`, `TestR_LIM_025_TheBlock` | The engine on in-memory counters: windows, cost, Audit, a refused request in no window (a retry every second admitted again, a true `Retry-After`, the same on the fallback's share), a request figure's refusal that is no refused request in Enforce, Audit and on the share while the refusals R-LIM-008 names still block, the admission, 9a and the ceiling, pacing shares and the floor, leases and their fallback share, a lease given up on the share and in Valkey while the batches move between them (taken on one and ended on the other, a take Valkey ran after the timeout, a waiting download's drop, a drop kept no longer than the lease's end; Q-LIM-95), the waiting share refused at once beside another caller, blocks and their lift |
| `TestR_LIM_022_ARefusedUpstreamRequestIsCountedNowhere`, `TestR_LIM_024_ACrashedReplicasLeasesEndAtTheirScore` ([engine_test.go](../../internal/limits/engine_test.go)) | Step 9a counts nowhere what it refuses (a retry admitted once the window has room, a refusal by the ceiling or by the caller's own figure leaves the other untouched, a download the parallel lease refuses gives its upstream request back in Valkey and on the share, the usage store counts what went upstream, audit counts and records); a crashed replica's leases, an engine left alone without a flush, end at their score a minute after it took them and the next download on another replica is admitted (Q-LIM-110, R-LIM-024); milliseconds |
| `TestR_LIM_020_TheRateLimitPolicyOfEveryFigureThatRefusesBesideTheRequestFigures` ([httplimit_test.go](../../internal/limits/httplimit/httplimit_test.go)) | The six figures that refuse beside the request figures, each refused by the engine, with the `RateLimit-Policy` and `RateLimit` members `SetRefusalPolicy` writes; milliseconds |
| `TestLimiterCluster`, `TestLimiterGiveBack` ([limiter_test.go](../../internal/cluster/limiter_test.go)) | The limiters of R-ARCH-034 take nothing when they refuse: a caller at twice the rate gets the rate, a retry every second at a burst of one is admitted again; a take is given back in the cluster and on the replica's bucket, a refusal and a shared take give nothing back |
| `TestR_EP_015_ARequestStage7RefusesIsGivenBackToTheEndpointsClass` ([endpoint_ratelimit_test.go](../../internal/api/v1/endpoint_ratelimit_test.go)) | A request the endpoint's class admitted and stage 7 refused (a request figure of the limits, the read class of R-API-016) is given back to the class; milliseconds |
| `TestR_LIM_017_AGiveBackTakesBackWhatATakeCountedAndAPeekAddsNothing` ([units_test.go](../../test/integration/valkey/units_test.go)) | The script against the stack's Valkey with its ACL: a peeked window is not added to, a give-back takes the units back and never below 0, a limiter's `GiveBack`; milliseconds |
| `TestR_LIM_036_EveryRouteOfEveryFormatCountsByItsKind` ([formats/all](../../internal/formats/all)) | The kind of every route of the six formats |
| Check 16 of the format kit ([checks_admission.go](../../internal/formats/formattest/checks_admission.go)) | Every format answers 429 before any byte when the admission refuses; the defect `WritesOwnDownload` fails it |
| [ratelimit_test.go](../../internal/api/v1/ratelimit_test.go) | In `audit` nothing on a protocol path is refused by a class; in `enforce` `package_write` on `/api/<format>/`, `/files/` and `/v2/`, keyed by the subject (Q-LIM-4, Q-LIM-27) |
| [ifmatch_test.go](../../internal/api/v1/ifmatch_test.go) | A project's first maximum by `PUT` without `If-Match`, a replace with it (Q-API-354) |
| [requestlog_test.go](../../internal/observability/requestlog_test.go) | The limits' fields of the request record, null off the protocol paths |
| `TestR_LIM_027_TheVirtualRepositoryAWellKnownMemberServedThroughNamesTheRowWhileItServes` ([charge_via_test.go](../../internal/limits/charge_via_test.go)), `TestR_LIM_027_AWellKnownMemberThroughTwoVirtualRepositoriesIsTwoRows` ([buffer_test.go](../../internal/limits/usage/buffer_test.go)) | `ServedThrough` and `viaOf`; the buffer keys a row by the virtual repository (Q-LIM-63); milliseconds |
| `TestR_LIM_027_ARequestIsRecordedInTheRowOfTheRepositoryThatServedIt` ([charge_via_test.go](../../internal/limits/charge_via_test.go)), `TestR_LIM_003_ATokenRequestIsRecordedOnceUnderTheSubjectItsHandlerAuthenticated` ([engine_test.go](../../internal/limits/engine_test.go)) | A flush of the charge buffer between the steps of one request (after the dispatcher's `Served` and before the member's `ServedThrough`; after stage 7 and before the token handler's `Retarget`, with a valid and with a failed credential): the request and the limit it met are recorded once, in the row of the repository that served it and under the subject the handler authenticated, never under the addressed repository or the address, and no charge stays in the buffer; milliseconds |
| `TestR_LIM_027_AWellKnownMemberServedThroughATeamsVirtualRepositoryRecordsTheVirtualRepository` ([via_test.go](../../test/integration/limits/via_test.go)) | A tarball of the test upstream fetched through `npm-all` at `upstream/npmjs`: the row with `via_repository_id`, none without it, and the overview narrowed to `npm-all` listing the reader (R-LIM-029); prints the reader's rows when it fails; about 3 s, one tarball of about 1 KiB |
| Integration set `limits` ([test/integration/limits](../../test/integration/limits)) | A cached download's round trips, Audit and Enforce, a failed credential, the overview; `TestR_LIM_001_ARegistryTokenOfATechnicalTokenIsChargedToTheTechnicalToken` (a docker-style exchange with a technical token's secret, then a `/v2/` request: exactly two requests of the token, none of the address); `TestR_LIM_001_ACITokenIsChargedToItsCIProject` ([ci_test.go](../../test/integration/limits/ci_test.go): the issuer double of the set `ci`, an exchange, one download recorded under `ci` as `<issuer ID>:<project_id>` and none under an address; about 6 s); `TestR_LIM_005_ARequestThroughABoundHostIsChargedToTheBoundRepository` ([bound_test.go](../../test/integration/limits/bound_test.go): a further endpoint whose host is bound to an OCI repository with a rule of its own; the short path there and the full name on the default host meet the repository's rule, the short path on the default host does not; about 3.5 s); `TestR_LIM_028_ADeletedTechnicalTokensRuleGoesAndItsUsageStays` (the token record's `limit`, a run of `limits-cleanup`, which also runs the fold of the hourly rows into the daily sums: `FoldUsageDays` grouped by an expression its subqueries named ungrouped and failed every run until 2026-11-19; two hourly rows of two people 31 days old become one daily sum of two people and are gone); `TestR_LIM_033_AProjectsFirstMaximumTakesNoIfMatch`; `TestR_LIM_019_WithValkeyStoppedTheReplicaDecidesOnItsShare` |
| Integration set `limits`, [figures_test.go](../../test/integration/limits/figures_test.go) | Each figure and subject once, without a format's client (Q-LIM-55): `TestR_LIM_021_…` (the day's volume: the crossing download completes, the next is refused before its first byte; the hour's requests), `TestR_LIM_024_…` (a download slowed to the floor while a parallel one is refused; neither in Audit), `TestR_LIM_024_WaitingDownloadsHoldAtMostTheirShareOfTheRequestSlots` (eight slots: two downloads wait, the next is refused at once, another caller is served; about 10 s), `TestR_LIM_017_ARefusedRequestFillsNoRequestWindowAndAHammeringCallerIsNotBlocked` (a retry every second at one request a second admitted again, a `Retry-After` that holds, a token that sends 20 requests a second for 3 s held to its figure's rate, not blocked, its `refusedPerHour` at 0; the take against the stack's Valkey with Sentinel; about 17 s), `TestR_LIM_025_…` (a token's refused writes block its budget key and nothing else, a reader's misses block nothing, an address blocked by wrong credentials serves a valid one, the lift, no block in Audit), `TestR_LIM_001_EachSubjectSpendsItsOwnBudget` (a user's two personal tokens share one budget; two technical tokens of one account have two, at the instance, the project and the repository; an address and an IPv6 `/64`; an administrator), `TestR_LIM_022_…` (the anonymous ceiling of an npm remote against a second address, a signed-in caller served, its own upstream bytes used up while the cached document is served), `TestR_LIM_026_TheSwitchReachesEveryReplicaWithinSeconds`. A rule there is the profile Unlimited with the one figure under test as an override; `X-Forwarded-For` from a trusted loopback makes two addresses |
| One case per format's e2e set (`Test<Format>Limits_R_LIM_020_…`, [e2e.md](e2e.md#what-a-new-format-adds-to-the-tests)) | What the format's client prints at a refusal, whether it retries, how it exits: the manual's table of the clients |

**Which row a request lands in is tested at the unit tier, with a flush between its steps.** On the integration stack the charge
buffer flushes every 200 ms at whatever moment of a request, so a row that depends on the flush's timing passes most runs there and
fails a few: `TestR_LIM_001_ARegistryTokenOfATechnicalTokenIsChargedToTheTechnicalToken` and
`TestR_LIM_027_AWellKnownMemberServedThroughATeamsVirtualRepositoryRecordsTheVirtualRepository` did until 2026-11-22, when the flush
recorded the request before `Retarget` or `ServedThrough`. A test that counts requests asserts the exact count: a lower bound
(`n < 2` fails) let a token request counted twice pass. How wide that window was on the runners was never measured, and it no longer
matters: a request's count and the limits it met reach the usage buffer only at the first flush after it ended (`Engine.Flush` takes
`pendRequests` and the events of an ended charge only), so no flush, whenever it falls, records a request before `Retarget` or
`ServedThrough`. `TestR_LIM_027_ARequestIsRecordedInTheRowOfTheRepositoryThatServedIt` and
`TestR_LIM_003_ATokenRequestIsRecordedOnceUnderTheSubjectItsHandlerAuthenticated` hold that with a flush between the steps; the
integration cases hold only that the rows arrive.

A crashed replica is the unit tier's: the stack stops a replica only gracefully, which releases its leases, and what a crash leaves
is an engine that neither drops nor renews them (`TestR_LIM_024_ACrashedReplicasLeasesEndAtTheirScore`).

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
