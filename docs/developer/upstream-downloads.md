# Downloads from upstreams

The overview of what Quader loads from the upstreams of its remote repositories: the running downloads with their progress and speed,
and the last 100 finished with their duration and average speed ([operations-and-observability.md](../spec/operations-and-observability.md)
R-OPS-049 to R-OPS-052, [ADR-279](../decisions.md#adr-279--administrators-see-the-downloads-from-upstreams-live-and-the-last-100-finished)).
The security view is [security/upstream-downloads.md](../security/upstream-downloads.md). Status: the backend and the API are built (T-26
package A), and so are the top bar's button and its panel (T-26 package B, [api-and-ui.md](../spec/api-and-ui.md) R-UI-210 to
R-UI-212), which read the schemas named below through `web/src/app/api/upstream-downloads-api.ts`.

## What has to be in your head first

- **One hook.** A download is the body of an artifact after a success answer: `fetcher.artifact` in
  [artifact.go](../../internal/proxy/artifact.go) begins it after `admitBody` and ends it with the fetch's result. A metadata document goes
  through `fetcher.metadata`, a hit never reaches the fetch, a request that waits for another one's flight reaches no fetch of its own, a
  `HEAD` goes through `Engine.head`: none of them is a download, and none of them needs a check of its own.
- **The engine does not know PostgreSQL.** `proxy.DownloadTracker` ([downloads.go](../../internal/proxy/downloads.go)) is a port, like
  `proxy.ClientFailures`; the architecture test forbids `internal/proxy` to import `internal/fills`.
- **Writes grow with the replicas, not with the downloads.** A fetch adds its bytes to an atomic counter; the publisher writes one row per
  replica every `proxy.upstream_downloads.publish_interval` (2 s), whatever the number of downloads (Q-OPS-420).
- **Nothing lives in Valkey.** Both tables are PostgreSQL, export class `transient`.

## The package map

| File | Responsibility |
|---|---|
| [internal/proxy/downloads.go](../../internal/proxy/downloads.go) | The port (`DownloadTracker`, `DownloadStart`, `DownloadCause`, `Download`), the origins, `beginDownload` (facts, the counted body), `downloadOutcome` (a fetch's result to `completed`, `cancelled`, `failed` with the class and code of `proxy.Describe`) |
| [internal/proxy/artifact.go](../../internal/proxy/artifact.go) | `fetcher.artifact`: begin after `admitBody`, `artifactBody`, end with its result |
| [internal/proxy/types.go](../../internal/proxy/types.go), [engine.go](../../internal/proxy/engine.go) | `Request.Cause` (nil is a client's), `Options.Downloads` (nil sees none) |
| [internal/formathost/mirror_host.go](../../internal/formathost/mirror_host.go) | `filler.origin`, `backgroundFacts.cause` (set by `apply` on every background request), `passUpstream.queue` |
| [internal/formathost/background_download.go](../../internal/formathost/background_download.go) | `queueCause`: a row's run hands its origin, id, version and starter to `downloadUpstream` |
| [internal/fills/types.go](../../internal/fills/types.go) | The bounds (200 per replica, 100 finished, 3 and 10 intervals, 30 s), the outcomes, `Entry` (a member of the live row's JSON), `Finished` (a row of the finished table), `View` (what the API reads), `Interrupted` |
| [internal/fills/tracker.go](../../internal/fills/tracker.go) | `Tracker`: the running set, the counting reader, the buffer of ended downloads (the newest 100), `snapshot` and `requeue` |
| [internal/fills/window.go](../../internal/fills/window.go) | The speed over 10 s from the samples of the publications (Q-OPS-421) |
| [internal/fills/publisher.go](../../internal/fills/publisher.go) | `Publisher`, a component: the loop of `Tick`, the sweep at most every 30 s, `Flush` in the close step, the failure count and its warning once a minute |
| [internal/fills/store_pg.go](../../internal/fills/store_pg.go) | `Store`: `Publish` (upsert or delete of the live row, insert and trim of the finished rows, one transaction), `Sweep`, `ReadUpstreamDownloads` (the live rows with their age, the newest 100, the names by id, and `Limited`: the download queues an upstream's rate limit holds, from `remote_download_limits`, remote-curation.md Q-CUR-176) |
| [internal/fills/metrics.go](../../internal/fills/metrics.go) | The four families of R-OPS-052, the label `repo` capped by R-OPS-007, `Tracker.Forget` |
| [internal/meta/postgres/migrations/00118_upstream_downloads.sql](../../internal/meta/postgres/migrations/00118_upstream_downloads.sql), [queries/upstream_downloads.sql](../../internal/meta/postgres/queries/upstream_downloads.sql), [tables_upstream_downloads.go](../../internal/meta/postgres/tables_upstream_downloads.go) | The two tables, their statements (rows cross as JSON text, so the package needs no driver types) and their export class |
| [internal/app/wire_upstream_downloads.go](../../internal/app/wire_upstream_downloads.go) | The area `upstream_downloads`: one tracker and one publisher per process, the store |
| [internal/api/v1/upstream_downloads_api.go](../../internal/api/v1/upstream_downloads_api.go) | The two handlers over the port `UpstreamDownloadReader`: freshness, elapsed time, names, the order of the running list |

## The life of one download

1. **Begin.** `fetcher.artifact` admits the body, then `beginDownload` hands the tracker the facts: the repository's id, name and format,
   the request's package and version (a queue row's version when the request has none, an OCI blob), the path, the announced length or
   -1, and the cause: `Request.Cause`, or a client's with `Caller.Principal`. The tracker gives the download a UUIDv7 and wraps the body.
2. **Reads.** Every read of the body adds its bytes to the download's atomic counter and to `quader_remote_upstream_download_bytes_total`;
   the counted reader sits under the size limit and the charge of the download limits, so the bytes are those read before the spool.
3. **End.** `endDownload` maps the fetch's result: `stored` is `completed` (a duplicate flight that found it stored too); a fetch whose
   context ended (a pre-emption, a cancelled job, the shutdown) or that reports `ErrPreempted` is `cancelled`; every other result is
   `failed` with `Describe`'s class and code; an empty result (the body went to its client unstored) is `failed` with the class `storage`
   and the code `object_store_unavailable`. The download leaves the running set and joins the buffer of ended ones.
4. **Publication.** `Publisher.Tick` takes a snapshot: every running download is sampled, the 200 that run longest go into the live row
   as JSON with their bytes, speed and elapsed milliseconds, the rest are counted in `omitted`. With something running the row is
   upserted; when the last download ended it is deleted; with nothing running and no row nothing is written. The ended downloads are
   inserted (`ON CONFLICT (id) DO UPDATE`) and the table is trimmed to the newest 100 by `(finished_at, id)`, in the same transaction. A
   publication that fails hands its ended downloads back to the buffer.
5. **The read.** `Store.ReadUpstreamDownloads` reads PostgreSQL's clock, every live row with its age on that clock, the newest 100
   finished rows, the queues an upstream's rate limit holds (`UpstreamDownloadLimited`: at most 100 rows of `remote_download_limits` whose
   time lies ahead, each with an index count of its waiting rows, the repository's name and its upstream URL, which the handler turns
   into the preset's name or the host, `waiting` of the answer) and the names of the repositories (project, key, format) and principals
   (not `anonymous`, not `authenticated`) they name. The handler leaves out a row older than three intervals, adds the age to the elapsed time, leaves out an entry whose repository
   is gone (it still counts in `runningCount`), and orders the running entries by their begin.

## Speed and the samples

`window.speed` keeps one sample per publication. The base of the speed is the newest sample at or before 10 s ago, so the window is
between 10 s and 10 s plus one interval; before 10 s have passed it is the begin; in the first second there is no speed. A download that
stalls for 10 s reads 0. Samples older than the base are dropped, so a download holds about 7 samples at the default interval.

## A replica that dies

Every publisher sweeps at most every 30 s: in one transaction `UpstreamDownloadSweep` deletes the live rows older than 10 intervals (at
least 30 s) and returns them, and `Interrupted` makes their downloads finished rows of the outcome `interrupted`, ended at the row's last
write with that write's bytes and elapsed time; `ON CONFLICT (id) DO NOTHING` never replaces a recorded end. A replica that only stalled
and later ends the download writes its end over the interrupted row, which has the same id (H-1845). The downloads a swept row left out
of its 200 get no end.

## Wiring per role

- Every serving role (`api`, `packages`, `worker`, `web`, `all`) builds the area `upstream_downloads` (it is in the common column of the
  role table): one tracker, one publisher started as a component, and the store. The engines of the process share the tracker: the
  client engine of `packages`, `web` and `all` (`wire_proxy_engine.go`) and the background engine of `worker`, `api`, `web` and `all`
  (`mirrorHostBuilder.buildEngine` in `wire_mirror_host.go`). The engine of `api` only lends the upstream catalogue its upstream, so its
  tracker stays empty and its publisher only sweeps.
- The management API of every listener reads the store (`app.go`, the `v1.Options` of each endpoint); its handlers are registered on
  every role that serves `/api/v1`.
- The publisher's `Flush` runs in the close step of the shutdown (R-ARCH-024 step 6), after the engines cancelled their fetches: it writes
  their ends and deletes the live row.

## Configuration, metrics, logs

- `proxy.upstream_downloads.publish_interval` (2 s, 1 s to 30 s, [outbound.go](../../internal/config/outbound.go)); the 100 are fixed.
- `quader_remote_upstream_downloads_running`, `quader_remote_upstream_downloads_total{repo,outcome}`,
  `quader_remote_upstream_download_bytes_total{repo}`, `quader_upstream_downloads_publish_failures_total`.
- A failed publication logs `publishing the downloads from upstreams failed` at warning, at most once a minute; a sweep that ended rows
  logs at info. The component's log field is `upstream-downloads`.

## Tests and what they cost

| Tier | Test | Data | Time |
|---|---|---|---|
| Unit | `internal/fills` `TestR_OPS_050_TheSpeedOfARunningDownload`, `TestR_OPS_050_APublicationWritesWhatRunsAndWhatEnded`, `TestR_OPS_051_AStaleLiveRowEndsInterrupted`, `TestR_OPS_049_AFinishedDownloadKeepsOnlyWhatItCanStore` (a fake clock and sink) | none | ~0.1 s |
| Unit | `internal/proxy` `TestR_OPS_049_TheEngineReportsEachArtifactBody` (the begin, the counted body, every outcome) | 64 KiB | ms |
| Unit | `internal/formathost` `TestR_OPS_049_TheCauseOfABackgroundFill` | none | ms |
| Unit | `internal/api/v1` `TestR_OPS_052_TheDownloadsFromUpstreams` (seven callers on both operations, the shape, freshness, elapsed, gone repository and principal, the queues a limit holds with the upstream's name) | in-memory view | ~0.3 s |
| Integration | area `proxy` `TestR_OPS_050_TwoReplicasShowOneRunningDownload`: the engine's path of R-OPS-049 (three concurrent misses are one download, a hit, a `HEAD`, a `404` and a document none, no length, a digest mismatch), a held 64 KiB artifact read by another replica, the trim of 101, the sweep and its replacement, the cascade | one 64 KiB artifact, ~105 tiny rows | ~3 s |

The engine's path is tested in the integration area because the unit tier of `internal/proxy` has no item store; the mapping of every
result is a unit test. No format's client and no e2e set: the hook sits in the engine every format shares, so a format adds nothing.

## The write load, measured

Measured once on 2026-11-25 at 06:30, a measurement and not a test, repeated by

```shell
QUADER_TEST_MEASURE_UPSTREAM_DOWNLOADS=4x500 test/integration/run.sh -run TestMeasureTheWriteLoadOfTheDownloadOverview -v ./test/integration/proxy/
```

([upstream_downloads_load_test.go](../../test/integration/proxy/upstream_downloads_load_test.go); without the variable it is skipped). The set-up:
instance 1 of the integration stack (PostgreSQL 18 in its container, the stack's SeaweedFS), the development host of 20 cores at a load average
of 10 rising to 20 during the run (two other builders), two in-process replicas of the area `proxy` on one upstream stub: one without the
overview (no tracker: the engine's path before T-26, `beginDownload` returns at once) and one with its tracker and its publisher at the default
interval of 2 s, a remote repository each. The reference load of [R-ARCH-027](../spec/architecture.md#r-arch-027--performance-targets) names no
rate of misses; P-10 asks 500 concurrent transfers of a replica. The run takes both ends: 2,000 misses of distinct 1 KiB artifacts per replica,
32 at a time, in four rounds alternating the two replicas, and then 300 downloads held open at once (64 KiB each, 1 KiB every 100 ms).

| What | Without the overview | With it |
|---|---|---|
| 2,000 misses, 32 at a time | 6.0 s, 333 a second | 5.6 s, 360 a second |
| A miss's latency, p50 / p95 / p99 | 85 / 126 / 374 ms | 84 / 117 / 160 ms |
| WAL per miss (the instance's `pg_current_wal_lsn`, the fill's own rows included) | 4,083 B | 3,715 B |

The overview costs a fetch nothing that shows above the host's noise: its path is a map entry and an atomic addition per read, and no statement
runs on it (the p99 without the overview is the one round whose first fetches warmed the replica). What it writes runs on its own loop:

- **Statements.** Over the 4,000 misses the publisher wrote 6 publications: 3 upserts of the live row, 2 deletes of it, 5 inserts of ended
  downloads with their trims, 1 sweep; at most 32 downloads ran in one publication. That is at most three statements (and the transaction's
  `BEGIN` and `COMMIT`) per replica every 2 s, whatever the number of downloads: Q-OPS-420's bound holds.
- **Rows.** A publication writes at most the newest 100 ended downloads (`keepNewest`): of the 2,000 misses of the replica with the overview,
  500 ended rows were written, 0.25 per miss. At 360 misses a second the list shows the last 100 of the last two seconds.
- **WAL of one publication**, measured alone (50 of each in a row, no fetch beside them): an upsert of a full live row of 200 downloads
  22,540 B; an insert of 100 ended downloads with the trim 56,008 B (560 B a row). A replica that keeps 200 downloads running and ends 100
  every 2 s writes about 39 KB of WAL a second, the most the overview can write per replica; at 360 misses a second the fills' own WAL is
  about 1.3 MB a second, so the overview's share at that bound is about 3 %.
- **The live row.** With 300 downloads held open, the row holds 200 (56 counted in `omitted` at the moment read): 48,987 B of JSON text,
  10,636 B stored (`pg_column_size`, compressed in TOAST), not the "about 100 KiB" estimated before. Every one of its 55 updates was HOT.
- **The tables' size.** After the run `upstream_download_finished` held its 100 rows in 200 KiB, and after the 50 inserts of 100 rows in a
  row 1,608 KiB with 5,300 dead rows: the trim deletes as many rows as the insert adds, and autovacuum (one run in the measurement) gives the
  space back. At the most a replica writes, 3,000 dead rows a minute, about 1.5 MiB between two runs of autovacuum at its default naptime of a
  minute. `upstream_download_live` held one row in 1,192 KiB after the 55 upserts (old TOAST chunks of the JSON until autovacuum).

Nothing is repaired: no statement runs per chunk or per fetch, both tables are bounded (200 entries per replica, 100 rows), and the indexes
serve the trim and the list. Q-OPS-420's revisit condition (the writes among PostgreSQL's top statements) is not met. Not measured: the
same on PostgreSQL of the reference environment and through PgBouncer, and with five replicas publishing at once (the bound multiplies by the
replicas, five at most about 200 KB of WAL a second).
