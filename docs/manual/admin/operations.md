# Monitoring and operations

This page is for the people who keep a Quader installation running. It covers the health and readiness endpoints, the metrics and logs, how to follow one request through them, the System status, Jobs, Backups, Storage and Audit log screens, how to add or remove an object-store connection, what happens when a pod stops or a node is drained, how to scale the two roles, how Quader behaves while Valkey, PostgreSQL or the object store is unavailable, and which signals deserve an alert.

## The ops listener

Every `api`, `packages` and `worker` process opens the ops listener on port 8081 (`server.ops_listen`), served over TLS with the chart's defaults. It has three paths and no sign-in of its own; the chart's NetworkPolicy admits port 8081 only from the namespace `monitoring` (`networkPolicy.ingress.ops.from`) and from `networkPolicy.probeCIDRs`.

| Path | Answer |
|---|---|
| `GET /healthz` | Liveness: `200 {"status":"alive"}` while the replica's internal loops make progress, `503` with the names of stuck loops otherwise |
| `GET /readyz` | Readiness: `200 {"status":"ready"}`, with `"degraded":[…]` when a reported check fails; `503 {"status":"unready","failing":[…]}` otherwise |
| `GET /readyz?verbose=1` | The same, plus the replica, its role and every check with its status, a fixed reason code and its latency; never addresses or error texts |
| `GET /metrics` | Prometheus text format; off with `metrics.enabled: false`; with `metrics.auth: static` a `Bearer` token is required |

The chart's probes use these paths: startup and liveness `/healthz`, readiness `/readyz`, all over HTTPS on the named port `ops`. The kubelet does not verify the certificate.

### Readiness checks

| Check | Holds the replica unready |
|---|---|
| `startup` | until start-up has finished, which includes the wait for Valkey's first answer (reason `waiting_for_valkey`, at most `valkey.startup_answer_timeout`, 30 s) |
| `database`, `schema`, `invalidation` | while PostgreSQL is unreachable, the schema is behind this version, or the change log cannot be read. After the first readiness, an outage shorter than `health.database_outage_grace` (20 s) only shows as degraded |
| `tls` | until every listener certificate has loaded once |
| `outbound` | until the CA files of `proxy.outbound.tls.ca_files` have loaded once |
| `objectStore` | until the start check has passed on one connection of `storage.connections` without `drain: true` and every such connection has answered once; a configuration error of such a connection (a wrong bucket, a changed bucket or prefix, a marker of another installation) ends the process. After the first readiness only with `health.require_object_store: true`, and then only while no connection without `drain: true` answers; otherwise a failing connection shows as degraded with its id (`unavailable: <id>`) |
| `valkey` | never, except a configuration error at start: Valkey refuses the password or the ACL user (`auth_failed`), the certificate or the primary name (`config_error`); reason codes such as `connecting`, `no_sentinel`, `timeout`, `auth_failed`, `circuit_open` |
| `draining` | from the start of a shutdown |
| `vulnScannerTrivy` | a pod of `<fullname>-vuln-scanner-trivy` while its certificate has not loaded or does not allow client authentication (`certificate`), Trivy does not answer its version (`analyser`), its working directory is not writable (`spool`), or it drains (`draining`). It does not wait for the advisory database: analyses need none, and a detection before the pod's container `database` loaded the database in use is answered `database_loading` (the worker's run waits 30 s). The container `database` has a liveness probe on port 8083 and no readiness probe. The role `all` builds no scanner and has no such check |
| `malwareScannerClamav` | a pod of `<fullname>-malware-scanner-clamav` while its certificate has not loaded (`certificate`), no signature database is loaded yet (`database`), `clamd` does not answer (`clamd`), or it drains (`draining`); never a worker, which reports it as degraded with `no_scanner` while `malware.scanner.url` is empty |
| `malwareUpdaterFreshclam` | the pod of `<fullname>-malware-updater-freshclam` while its certificate has not loaded (`certificate`), it has not seeded `freshclam`'s directory from the database in use yet (`seed`, at most a minute), or it drains (`draining`); it does not wait for `freshclam`'s first download, so an updater that cannot reach ClamAV's network stays ready and the database's age raises the alarm. Never a worker, which reports it as degraded with `no_updater` while `malware.updater.url` is empty and the source is `updater` |

A check of a scanner pod carries the name of its Deployment in camel case, and so does the check a worker or the role `all` reports about
that scanner; the System page shows a sentence for each when it is degraded.

## Metrics

The families are named `quader_*`, plus the Go runtime and process families. The label `endpoint` holds the configured endpoint names (`default` for the main listener), so its values are bounded. With the Prometheus operator's CRDs installed, the chart renders the ServiceMonitor:

```yaml
metrics:
  serviceMonitor:
    enabled: true                              # default: false
    namespace: ""                              # default: the release namespace; with tls.enabled it must be the release namespace, because the CA Secret is read from it
    interval: 30s                              # default
    scrapeTimeout: 10s                         # default
    labels: {release: kube-prometheus-stack}   # example: what your Prometheus selects ServiceMonitors by
```

With `tls.enabled` the ServiceMonitor verifies the pods against `ca.crt` of the ops listener's Secret and the name `<fullname>-ops.<namespace>.svc`.

The ops Service selects every `api`, `packages`, `worker` and `web` pod, so each replica is scraped. `quader_build_info{role}` and the `role` of every log record name the role of the process (`api`, `packages`, `worker`, `web` or `all`); the ServiceMonitor copies `role` and `pool` onto every other series. To require a token, set `config.metrics.auth: static` and name a Secret in `secrets.metricsToken`; Quader reads the token file on every scrape, so a rotated token applies without a restart.

<details>
<summary>Metric families by area</summary>

| Area | Families |
|---|---|
| Build and readiness | `quader_build_info` (version, revision, role), `quader_ready` |
| HTTP | `quader_http_requests_total`, `quader_http_request_duration_seconds` (labels `listener`, `endpoint`, `handler`, `method`, `code`), `quader_http_requests_in_flight`, `quader_http_requests_shed_total` (`reason`: `in_flight`, `memory_slots`, `heap`, `kernel`), `quader_http_memory_slots`, `quader_http_transfers_cut_total` |
| Endpoints and exposure | `quader_endpoint_listener_up`, `quader_endpoint_misdirected_total`, `quader_endpoint_forwarded_host_ignored_total`, `quader_endpoint_credentials_refused_total`, `quader_endpoint_cache_fill_refused_total`, `quader_exposure_rules`, `quader_exposure_hidden_total` |
| TLS | `quader_tls_certificate_expiry_timestamp_seconds`, `quader_tls_reloads_total`, `quader_tls_reload_errors_total`, `quader_tls_temporary_certificate` |
| Sign-in and permissions | `quader_auth_attempts_total`, `quader_auth_group_claim_missing_total` (every sign-in without a usable group claim; the audit log records only when that begins and ends, see [Group synchronization](sign-in-and-identity.md#group-synchronization)), `quader_auth_ratelimited_total`, `quader_auth_csrf_failures_total`, `quader_auth_lastused_flush_failures_total`, `quader_authz_decisions_total`, `quader_authz_index_staleness_seconds`, `quader_authz_stale_waits_total` |
| Rate limits | `quader_api_rate_limited_total`, `quader_ratelimit_decisions_total`, `quader_ratelimit_fallback` |
| Coordination | `quader_cluster_changes_applied_total`, `quader_cluster_poll_errors_total`, `quader_cluster_poll_last_success_timestamp_seconds`, `quader_lease_held`, `quader_lease_lost_total` |
| Valkey | `quader_valkey_available`, `quader_valkey_circuit_open`, `quader_valkey_sentinels_reachable`, `quader_valkey_pubsub_subscribed`, `quader_valkey_primary_changes_total`, `quader_valkey_primary_views`, `quader_valkey_errors_total` |
| Jobs | `quader_jobs_queued`, `quader_jobs_oldest_queued_age_seconds`, `quader_jobs_running`, `quader_jobs_claimed_total`, `quader_jobs_completed_total`, `quader_jobs_duration_seconds`, `quader_jobs_claim_errors_total` |
| PostgreSQL | `quader_db_pool_connections`, `quader_db_pool_max_connections`, `quader_db_pool_empty_acquires_total`, `quader_db_pool_canceled_acquires_total`, `quader_db_pool_connections_opened_total`, `quader_db_pool_acquire_duration_seconds` (label `db_pool`: `request`, `control`, `lease`), `quader_db_retries_total` (`kind`), `quader_db_commit_unknown_total`, `quader_db_slow_queries_total` (`query`: statements slower than `metrics.slow_query_threshold`), `quader_db_query_duration_seconds` (`query`, only with `metrics.db_query_histograms: true`) |
| Spool and garbage collection | `quader_spool_used_bytes`, `quader_spool_free_bytes` (on every pod with a spool: `api`, `packages`, `worker` and `vuln-scanner-trivy`; the label `role` names the unit), `quader_gc_last_success_timestamp_seconds` (no series on a replica until a run there has ended), `quader_gc_deleted_blobs_total`, `quader_gc_reclaimed_bytes_total` |
| Object store and upstreams | `quader_storage_s3_requests_total`, `quader_storage_s3_request_duration_seconds`, `quader_storage_s3_circuit_open` (each by `connection`), `quader_outbound_requests_total`, `quader_outbound_request_duration_seconds`, `quader_outbound_egress_denied_total` |
| Connections and copies | `quader_storage_copies_damaged_total`, `quader_storage_reads_fallen_over_total`, `quader_storage_blobs_damaged_total` (the marks of reads and of the scrub), `quader_storage_writes_fallen_over_total` (`connection`, `reason`), `quader_storage_uploads_unavailable_total`, `quader_storage_reads_unavailable_total` (writes another connection took, uploads and reads refused for want of a copy; the same events are kept per hour in the history of the stored content for `storage.history.retention`, 400 days by default), `quader_storage_reconcile_queue`, `quader_storage_reconcile_queue_oldest_seconds`, `quader_storage_connection_filling`, `quader_storage_connection_detached`, `quader_storage_reconcile_copied_total`, `quader_storage_reconcile_failures_total`, `quader_storage_scrub_copies_total`, `quader_storage_scrub_repairs_total`, `quader_storage_scrub_overdue_copies`, `quader_gc_copies_released_total` (each by `connection`); from the hourly storage summary `quader_storage_copies` (`connection`, `state`), `quader_storage_connection_backlog_blobs`, `quader_storage_connection_backlog_bytes`, `quader_storage_connection_fill_ratio`, `quader_storage_blobs_damaged` (`status`) and `quader_storage_summary_age_seconds` |
| Caches | `quader_cache_requests_total`, `quader_cache_entries`, `quader_cache_bytes`, `quader_cache_flushes_total` |
| Downloads from upstreams | `quader_remote_upstream_downloads_running`, `quader_remote_upstream_downloads_total` (labels `repo`, `outcome`), `quader_remote_upstream_download_bytes_total`, `quader_upstream_downloads_publish_failures_total` ([below](#downloads-from-upstreams)) |
| Vulnerability scanning | `quader_vuln_source_last_success_timestamp_seconds`, `quader_vuln_source_records`, `quader_vuln_source_consecutive_failures` (labels `source`, `ecosystem`), `quader_vuln_sync_runs_total`, `quader_vuln_sync_records_total`, `quader_vuln_unevaluable_ranges_total`, `quader_vuln_evaluations_total`, `quader_vuln_evaluation_duration_seconds`, `quader_vuln_findings` (`severity`, `status`, `kind`), `quader_vuln_downloads_blocked_total`, `quader_vuln_audit_requests_total`, `quader_vuln_claimed_name_matches` (`ecosystem`, `kind`) |
| Audit and telemetry | `quader_audit_events_total`, `quader_audit_events_dropped_total`, `quader_metrics_label_overflow_total`, `quader_tracing_spans_dropped_total` |
| Package formats | Each format adds families named `quader_npm_*`, `quader_nuget_*`, `quader_oci_*` and `quader_deb_*`; [Container image repositories](container-repositories.md), [Debian (APT) repositories](apt-repositories.md) and the guide of a format name the ones an operator reads |

Gauges read from PostgreSQL, such as `quader_jobs_queued`, `quader_jobs_oldest_queued_age_seconds`, `quader_valkey_primary_views` and the storage gauges by `connection`, are reported by every replica; aggregate them with `max`, not `sum`.

</details>

## Logs

Quader writes one JSON object per line to standard output (`log.format: text` for a readable form). Every record has `time`, `level`, `msg`, `replica`, `role` and `log`, which names the stream:

| `log` | Content |
|---|---|
| `app` | start-up, warnings, errors; `component` names the part of Quader |
| `request` | one record per request on a client listener: `endpoint`, `host`, `method`, `path`, `status`, `duration_ms`, `request_bytes`, `response_bytes`, `principal`, `principal_type`, `auth_method`, `token_id`, `repo`, `remote_addr`, `user_agent`, `trace_id` |
| `upstream` | one record per request Quader sends to an upstream registry |
| `audit` | a copy of every audit event |

Secrets are redacted in every record: tokens, passwords, `Authorization` and cookie values, URL credentials and the query parameters of `log.request.redact_query_params`. The address of another system keeps its path and the names of its query; a value stays only where Quader wrote it (a search term, a page), so the signed address an upstream redirects a download to appears as `…?X-Amz-Signature=[REDACTED]&…`. Probe and metrics requests are not logged unless `log.request.ops_listener: true`.

```yaml
log:
  level: info                           # default: debug | info | warn | error
  levels: {}                            # default; per component, example {proxy: debug}
  request:
    enabled: true                       # default
    ops_listener: false                 # default
```

## Follow one request

Every answer carries `X-Request-Id`. Problem answers repeat it as `requestId`, and the web UI shows it as **Request ID** with the hint "Give this ID to your administrator." The same value is the `trace_id` of the request's log records, the trace ID of its spans when tracing is on, and the **Trace ID** of the audit events it caused. To find those events, use `GET /api/v1/audit/events?traceId=<id>`; the screen has no filter for it.

## System status

**Admin area › System › System status** (capability `settings:read`: roles `admin`, `security-admin`, `auditor`) has four tabs: **Status**, described here, [Jobs](#jobs), [Backups](#backups) and [Storage](#storage), each with its own address, so a reload or a link keeps the tab. The tab refreshes every 15 seconds; **Pause automatic refresh** in the page's header stops that for every tab until you resume or leave the page, and **Refresh now** reloads the tab you see. The [Admin dashboard](dashboard.md) has the overview with the load of each role and the counts; this tab has every replica.

- A summary line that says what is wrong first. In red: **No ready replica of the role …** when a role has no ready replica (a role scaled to zero included), and **Replicas unready** when a replica that was ready fails a readiness check. As a warning: **Lost without draining: …, last heartbeat …** when a replica ended without a drain (a crash, a kill, a lost node), **Degraded**, and **No ready replica of vuln-scanner-trivy** (or of `malware-scanner-clamav`) when a kind of scanner has no ready pod, so its scans wait. Otherwise **All roles ready**. After that, without colour: **Replicas are being replaced (n draining, m starting), as during every update** while pods drain or start, and **Not drain-safe** when a role has fewer than two ready replicas or all of them on one node, which on a small installation is its size, not a fault.
- **Installation**: version, **Base URL** and its source, and **Answered by**, the replica that answered. **Database**: server and schema version, and whether this build supports the schema.
- **Replicas by role**: per replica **Name**, **Node**, **Version**, **State** (ready, starting, draining, unready, lost) with failing checks, **Heartbeat** and **Valkey**, which says in words why a replica sees Valkey degraded (not connected yet, no Sentinel answers, timed out, sign-in refused, configuration error, unsupported version, calls paused after errors, health check failed, client closed) and folds the client's own message, which can name addresses, under **Technical details**. A replica that is stopped (an update, a restart, a drain) shows **draining** until it ends, and then disappears: a clean stop deletes its row, and a row left in the state draining, by a pod killed during its drain, is not shown once its heartbeat is 30 seconds old ([Replicas shown as lost](runbook.md#replicas-shown-as-lost)). Notes say when a role has fewer than two ready replicas or all ready replicas share one node; warnings say when no worker is live, in which case jobs and scheduled tasks are queued but nothing runs them. A notice appears while replicas of two versions run side by side.
- **scanner**, after the roles: every pod of `vuln-scanner-trivy` (Trivy, the image scanning) and of `malware-scanner-clamav` (ClamAV, the malware scan), each kind under its own name, per pod **Name**, **Node**, **Version**, **State** with the reason a pod is not ready (**No database loaded yet** for a malware scanner that waits for its first signature database, **clamd does not answer**, **Analyser does not run**), **State changed**, **Heartbeat** and **Tool**, the tool with its version (**Trivy 0.67.2**, **ClamAV 1.5.5**). The pods of `vuln-scanner-trivy` show under their tool the advisory database the pod detects with, which its container `database` loads from Quader. **Database in use** (a green check) is the database Quader uses now, with how long ago it was built; **Older database** means the pod has not loaded the one in use yet, which after every new database takes it under a minute, or its container `database` cannot load it (its log says why: `kubectl logs <pod> -c database`); **No database yet** means its first load has not finished. Pointing at the badge shows the database's digest. While ready pods do not hold the database in use, a line under the kind's name says how many (**1 of 2 ready replicas does not hold the database in use yet**): the detections those pods are asked for wait until they have loaded it. A pod that stays on an older database for minutes is stuck. A scanner has no database connection: each pod reports itself every 10 seconds through the scan listener with the certificate of its kind, and goes from the list as a replica does. A kind without a pod says why: none has reported (check the Deployment and its pods), Quader cannot tell the kind without TLS, or no scanner of the kind is configured (the development role `all` runs none). Every value but the kind and the times is what the pod says of itself.
- **Valkey**: mode, primary name, configured Sentinels, TLS verification, last primary change, and whether replicas disagree about the primary or cannot reach any Sentinel; while replicas see it degraded, how many and why, in words.
- **Stored-secret keys**: the active key ID, whether the key rings of all replicas are compatible, and **Re-encrypt now**.
- **Scheduled tasks**: each task with **Schedule (UTC)**, **Next run** and **Last run**. **Run now** (capability `settings:write`) queues one run; a worker executes it.
- **Recent jobs**: the last ten; the tab **Jobs** has them all.

The screen cannot see disruption budgets or free capacity on other nodes, because Quader holds no Kubernetes permissions.

### Jobs

The tab **Jobs** (`/admin/system/jobs`) lists background jobs newest first, each with its state (**Queued**, **Running**, **Cancelling**, **Succeeded**, **Failed**, **Cancelled**). **Filters** opens a panel from the right with **Kind** (several kinds separated by commas) and **State**; each filter that is set stands as a chip beside the button, and its cross removes it. A holder of `settings:read` sees every job, anyone else only the jobs they started; such a caller reaches the jobs through the link of a repository screen and sees no tabs, because the other three need `settings:read`. A filter stays in the address of the tab: another tab drops it, and the browser's back button brings it back. **Cancel** is offered for queued and running jobs to holders of `settings:write` and to whoever started the job; a running job shows **Cancelling** until it reaches its next checkpoint. Through the API, whoever started the job cancels it with a browser session or a token with all permissions of the owner; a token with a restricted scope needs `settings:write` in its scope (`403` otherwise).

The **Result** column of a job that did not succeed says why in words (the attempt failed, it ran past the job's time, every attempt failed, it was cancelled, or a newer job took its place) and, when there is one, **Attempt *n* of *m*** and **Technical details**, which opens the detail as the server wrote it with the error's code (`job_failed`, `job_timeout`, `attempts_exhausted`, `cancelled` or `superseded`). The detail is the text of the failed attempt's error, cut to 2,000 bytes, and is not translated; it names what failed, for example the archive entry or the limit that a sync crossed. A job that is queued or running again after a failed attempt keeps the error of the attempt before, so the attempt shown can be lower than the job's own. The result of a succeeded job is a short summary of its figures; a list in it, such as the ecosystems of an advisory sync, shows its length.

### Backups

The chart's backup is off until you set `backup.enabled: true` and a bucket of its own in `config.backup.target.bucket` ([Installation](installation.md)); while it is off, the screen below shows **No backup yet** and the command to start a backup carries placeholders. [Backup and restore](backup-and-restore.md) explains how to set the backup up, verify one and restore.

The tab **Backups** of **Admin area › System › System status** (`/admin/system/backups`, capability `settings:read`) shows the backup job and changes nothing: no control starts, deletes, verifies or restores a backup. It refreshes on the same interval as the status tab, with the page's **Pause automatic refresh** and **Refresh now**. The [Admin dashboard](dashboard.md) shows the time of the last success, **Overdue** and **No backup yet** too.

- **Status**: the last success with its age and the **Expected interval**. **Overdue** appears when the last success is older than 1.5 times that interval, the condition of the `QuaderBackupStale` alert; **No backup yet** appears while no run has succeeded. **Last failure** appears when a failed or abandoned run, or a run whose process has stopped, is newer than the last success, with the reason in words and its code. A drain or an eviction ends a run as **Interrupted**, which is not a failure: the Job retries it.
- **Run in progress**: the state, the phase, the tables exported, the blobs and bytes copied against the totals, the pod and the start time. **Stopped** means the run's process has gone without finishing; the next run marks it abandoned.
- **Runs**: the runs newest first, with **Load more**. **Filters** opens a panel from the right with **Operation** and **State**; each filter that is set stands as a chip beside the button. **Details** opens a run with its error and its result. A run that ended **Completed with warnings** wrote its backup; its result says what it left out or could not clean up, and the audit log records it as **Backup completed** with the outcome **Warning** instead of **Success**.
- **Stored backups**: the complete backups that the newest run found in the target, headed "As recorded by run … at …". Backups written or removed outside Quader appear after the next run; `quader backup list` reads the target itself. Next to them: the target, how many of the newest backups are kept, the age below which backups are kept, and the expected interval.
- **Start a backup now**: the `kubectl` command that creates a Job from the CronJob, with a copy button. Run it with access to the cluster. The namespace and the CronJob name come from the chart; if the installation does not provide them, the command shows `<namespace>` and `<cronjob>` for you to replace. The run appears in the list once its pod has started. One backup runs at a time.
- After a restore, a notice names the backup and the snapshot time this installation was restored from.
- The output of the revocation journal replay (`quader backup restore`, `quader admin post-restore`) prints one line per entry with
  its outcome. A repository whose entry could widen what it serves is listed as `suspended` and is **disabled** (the line prints
  the entry); a `vuln-rescan` that a stronger vulnerability setting needs is queued in the restored database and runs once the
  workers are up ([After a restore](repositories.md#after-a-restore)).

### Storage

The tab **Storage** (`/admin/system/storage`, capability `settings:read`) shows where Quader keeps the artifacts: the object-store connections of `config.storage.connections`. It only shows them; a connection is added, drained and removed in the deployment's configuration ([below](#add-or-remove-a-connection)). It refreshes like the other tabs.

- A line over the table says what needs an act, worst first: a connection **unreachable from some replicas** (its circuit is open there), one **in no replica's configuration but holding copies** (detached), how many blobs **no connection holds whole**, copies that **wait for more than an hour** for the reconciliation, copies **not read whole within their period** by the scrub, and, without colour, a connection that is **drained and can be removed from the configuration**. Otherwise it says that every connection is healthy.
- The table has a row per connection, in the order of the answering replica's configuration (the order uploads try them and reads try their copies), then the connections it does not configure:
  - **Connection**: its id, and under it the endpoint's host and `bucket/prefix`.
  - **State**: **Active**; **Filling** with the share of the blobs it holds while the reconciliation copies them to a connection you added; **Draining** with the copies left; **Drained**; **Detached** (warning) with the blobs only it holds; **Unreachable** (error) on *m* of *n* replicas while its circuit is open there; and **Configured on *m* of *n* replicas** while a rollout runs old and new configurations side by side.
  - **Fill**: the share of all blobs it holds and their stored size. **Backlog**: the reconciliation's queue for it, with the age of the oldest gap, and **Behind for over an hour** when `QuaderStorageReplicationBacklog` would fire. **Damaged copies**: missing and corrupt copies. **Scrub**: the share read whole within the connection's `scrub_period`, and **Behind** while copies are overdue. **Encrypted**: **Yes** for a connection with `encrypted: true` ([configuration](configuration.md)), **No** otherwise.
  - Fill, damaged copies and scrub come from the storage summary, which the task `storage-summary` computes every hour; the line under the table says when it last counted. **Run now** of `storage-summary` on the Status tab's scheduled tasks counts again at once.
  - The chevron before a connection's name opens its details: the endpoint, bucket and prefix, its place in the list, the region, the credential source (never a credential), path-style addressing, the checksum modes, batch delete, the storage class, server-side encryption, the scrub's period and speed, what the start check found (payload checksums enforced, metadata kept and replaced, a server-side copy from another connection on the same endpoint), when it was filled and its last full pass, and the storage keys of an encrypted connection by identifier. The settings are those of the replica that answered: a connection it does not configure shows only its bucket and prefix.
- **Blobs no connection holds whole**, a closed section with their number: the first 20 with their digest, size, state (**Missing**, **Corrupt**), the state of each copy and the first item that references them. A holder of `settings:write` (role `admin`) has **Check again**, which reads every copy of the blob now and shows what it found; it writes no object, and a later upload of the same file, a remote repository's refetch or the object's return repairs the blob. Past 20, `quader admin storage verify` lists every one. The API is `GET /api/v1/storage/damaged-blobs`; with `scope=copies` it lists the blobs with a damaged copy that another connection still holds whole, which the scrub and the reconciliation repair on their own.
- **The last runs of the storage tasks**, a closed section: `storage-reconcile`, `scrub`, `orphan-sweep` and `storage-summary` with their last run and their next, each linked to its jobs.

The same figures are in the API: `GET /api/v1/system/storage-connections` (one connection: `…/<id>`) and `GET /api/v1/storage/summary` for the whole summary with every repository's logical size.

#### Add or remove a connection

A connection is one element of `config.storage.connections`, one bucket and prefix on one S3-compatible store; with static keys its credentials are an entry of `secrets.storageConnections` with the same `connectionId` ([Installation](installation.md)). Quader keeps every blob on every connection that is not draining. The bucket, the prefix and `encrypted` of a connection are fixed once it holds a copy; the endpoint, region, credentials, CA and tuning may change at any rollout.

1. **Add a connection**: add an element with an `id` of its own (2 to 32 lower-case letters, digits and hyphens, starting with a letter; an id is never used again), its bucket and its credentials, and roll out. The tab shows it **Filling**: the reconciliation (the task `storage-reconcile`, every minute) copies every blob to it, server-side where both connections lie on one endpoint and the start check found that possible, else through a worker at `storage.reconcile.bytes_per_second`. After its first full pass without a gap it is **Active**. Put the store nearest to the replicas first: the list order is the order of uploads and reads.
2. **Take a connection out**: set `drain: true` on its element and roll out; at least one element must stay without it. The tab shows **Draining**: nothing new is written to it, the reconciliation copies what only it holds to the others and deletes each of its copies once every other connection holds the blob and one of those copies was read whole within its `scrub_period`. A copy the other connection made server-side is read by the scrub within the hour, so a drain can wait up to about two hours for such copies. When it holds nothing the tab shows **Drained**; then remove the element (and its entry of `secrets.storageConnections`) in a later rollout. Once no running replica configures it, it is retired and its id cannot be used again.
3. **Move to another bucket or prefix**: both steps: add the new location as a new connection, wait until it is **Active**, then drain the old one. Do not add one connection and drain another in the same rollout: a backup that runs with the configuration from before can miss a blob the drain deleted ([Known limitations](limitations.md#storage)).
4. **A connection removed without draining** is **Detached**: the replicas start, no copy is read from it, the blobs only it holds answer `503`, and `QuaderStorageConnectionDetached` fires. Put the element back and everything returns. If the store is gone for good, `quader admin storage forget-connection <id>` (in a pod of the release) gives it up: it deletes the connection's copy and queue rows, marks a blob that only it held `missing`, retires the id and writes the audit event `system.storage.connection_forgotten`; it deletes no object.

`quader admin storage check --connection <id>` runs the conformance check of one connection from a pod of the release, and `quader admin storage verify` asks every connection for the object of every copy ([Backup and restore](backup-and-restore.md)).

## Downloads from upstreams

Quader shows what it loads from the upstreams of its remote repositories now, and the last 100 downloads that finished. Only the holders
of the capability `content:metadata-read` see it: the roles `admin` and `auditor`. A project's Owner, a `security-admin` and every other
user do not; a repository's own queue of downloads is its tab **Downloads**.

- **What a download is:** what a person asked for, never one of its files: an image with its manifests, configs, layers and attestations
  (for every platform it fetched), a version's package (a tarball, a `.nupkg`, a `.deb`, a collection's archive), a row of a repository's
  download queue (a person's download ahead, a bulk's version, a probe, a request or an allow), a mirror pass's version, or the vulnerability
  scanning's database. Its files are what Quader fetches from an upstream and stores, whatever caused it; ten clients that ask for the same
  file at once cause one fetch. A client's pull of an image is one download, named by the tag (or the digest it was pulled by), however
  many replicas served its layers; a client that pulls two tags of one image within seconds makes one download of both.
- **What does not count:** the documents that list packages and versions (an npm package document, an APT index, a tag list), a file
  answered from the cache (an image whose layers were all cached is no download; one whose layers partly were counts the bytes fetched),
  a `HEAD`, an upstream's error answer, a search of the upstream, what a Bastion or the ClamAV updater fetches by itself, and the sync of
  the advisory sources.
- **A running download** shows its repository, the package and version, the bytes so far, its size where it is known (and so the
  progress: a download ahead knows the sizes of its files before it fetches them, a single file has the size the upstream announced; a
  client's pull of an image shows only its bytes, because which layers the client lacks is the client's to know), the speed over the last 10 seconds, how long it has run, what caused
  it and who: the client's user, or the person who started the queue row.
- **A finished download** shows its size, its duration, its average speed (size over duration) and how it ended: **completed**;
  **failed** with the reason in the classes of the download queue (the network, the upstream's answer, the integrity check, a size limit,
  the store) and the file it failed at; **cancelled** (a client's request took the slot of a background download, or the job was cancelled, or the replica shut
  down); **interrupted** (the replica that ran it stopped without saying so; a replica that was only slow writes the real end later).
- **How fresh:** every replica writes its running downloads every 2 seconds; a replica that has not written for three of those intervals
  is left out. A pull of an image stays under **Running** for 5 seconds after its last layer, in case the client asks for one more. The
  last 100 are kept until 100 newer downloads push them out, whatever their age. Neither list is in a backup.
- **What is never kept:** the upstream's address (a signed address can hold a credential), headers, the client's address. A queue
  that waits for an upstream's limit names the upstream by its preset's name or its host only.
- **No one clears the finished list** for everybody: no operation removes its rows.

The API is `GET /api/v1/system/upstream-downloads` and `GET /api/v1/system/upstream-downloads/summary` (the count only). One setting
changes the interval of the writes, and with it how often an open view refreshes:

```yaml
config:
  proxy:
    upstream_downloads:
      publish_interval: 2s   # default; 1s to 30s
```

### The button Downloads and its panel

The button **Downloads** (a download arrow) in the top bar, between **Documentation** and the theme, is shown to the same holders, in
the main area and in the admin area. While downloads run, a small number on it counts them (**99+** above 99); it reads that number
every 10 seconds while the window is in view. On a phone below 440 px the bar drops the product name beside the mark to make room for it.

The button opens a panel from the right:

- **Waiting for an upstream**, above **Running** while there is any: each repository whose download queue an upstream's rate limit
  holds, with how many downloads wait, who limits ("Docker Hub limits requests"), and until when, or when Quader asks again where the
  upstream named no time; **Show in queue** opens the repository's tab **Downloads**. Its downloads run nowhere while they wait, which is
  why **Running** can be empty although a queue is not done.
- **Running**, the oldest first, one row per download however many files it takes: the package and its version (an image pulled by its
  digest shows the digest's first 12 digits; an image whose tag is not known yet its name alone), a bar where the size is known, the bytes so far of the size, the speed (a dash in the first second), the time since it
  began, the format, the repository, and what caused it: **Client** with the person who asked, the person who started a queue row (or
  **Bulk**, **Probe**, **Request**, **Allow**), **Mirror pass**, **Vulnerability database**. A queued download links **Show in queue**:
  the repository's tab **Downloads** with that package's rows. With more running than the replicas list (200 each), a line says how
  many more.
- **Finished**, the newest first: the size, the duration, the average speed (none below one second, which reads **< 1 s**) and when it
  ended. A download that did not complete carries **Failed** with the failure's class (**Failed: Network**; pointing at it shows the
  code and the file that failed), **Cancelled** or **Interrupted**.
- **Filter downloads** narrows both lists by name, version, the first file's path or repository, in the browser.
- **Hide all** hides the finished downloads listed now **on this device only** (the browser keeps the time of the newest one); a
  download that ends later shows again, and **Show again** brings the hidden ones back. Nothing is removed for other administrators.
- The open panel refreshes every `publish_interval` while the window is in view; closing it stops that. A refresh that fails keeps the
  last rows and says so. **About this list** leads here.

## Banner and maintenance mode

**Admin area › Settings › General** (capability `settings:read`; changing needs `settings:write`) shows the **Public base URL** (a read-only field, with a line below it that says where it comes from) and sections that are closed until you open them: **Time format**, **Banner** and **Maintenance mode**. Each section has its own **Save changes** button, which stays disabled until you change something in that section. The **Maintenance mode** section opens by itself while the mode is on.

- **Banner.** Tick **Show the banner**, choose a **Severity** (**Notice**, **Warning**, **Critical**), give an optional **Title** and a **Banner message**, and select **Save changes** in the **Banner** section. Every user sees it at the top of every screen, and an anonymous visitor on the sign-in page, within a minute. The message is plain text. A link is written `[text](https://example.com)`; only `https` links are links, anything else (`javascript:`, `http:`, a relative address) stays text exactly as typed. A disabled banner keeps its text.
- **Maintenance mode.** Tick **Read-only maintenance mode**, write the **Maintenance message** (at most 500 characters), choose whether **Remote repositories keep filling their cache** and a **Retry after** in seconds, and select **Save changes** in the **Maintenance mode** section. The screen asks first, because the mode refuses every write of every user until you switch it off: publishing and pushing, deleting, and every change of the configuration answer `503` with the message and a `Retry-After`. Downloads and metadata reads, sign-in, creating and revoking tokens, ending sessions, the CI token exchange, the evaluating calls (policy validation, permission checks) and this switch keep working. Remote repositories serve only what they already cache when cache filling is off. The scheduler enqueues no garbage collection, OCI sweep, cache-invalidation fold or cleanup policy while the mode is on; runs in progress finish. Every replica enforces a change within the poll of the change log, also while Valkey is stopped. While the mode is on the **System status** screen shows a row for it, and every screen shows a notice with the message.
- A backup taken in read-only mode restores an installation in read-only mode. Switch it off after verifying the restore.
- Both changes are audited (`system.banner_changed`, `system.maintenance_mode_changed`). The banner cannot be changed while the mode is on; switch the mode off first.
- **Time format.** The section **Time format** sets the **Default time format**, the form in which Quader writes dates and times for every user who chose none in their preferences and for a visitor without a session, such as on the sign-in page. Each form shows the current time as its example; the default after installation is `26.11.2026 21:39:22 (Thu.)`. Select **Save changes** in the section; every screen follows within a minute, and a user's own **Time format** wins. The change is audited as `system.time_format_changed`. The web interface of a Bastion writes the default after installation, whatever is set here.

## The audit log

**Admin area › System › Audit log** (capability `audit:read`: roles `admin`, `security-admin`, `auditor`) shows events newest first, by default of the last 24 hours, in five columns: **Time** (how long ago; point at it for the moment), **Actor** with the address the event came from under it, **Action**, **Target** with the project of a repository under it, and **Outcome**. Each event shows a plain name of its action (**Token revoked**, **Advisory data imported**) with the event type under it, its **Target** and its **Outcome** (**Success**, **Failure**, **Denied**, and **Warning**: it happened, and you should look at it). Warnings are a provider that stopped sending groups and a group rule that gave a name that is no group name ([Group synchronization](sign-in-and-identity.md#group-synchronization)), every sign-in with the break-glass account, its lock and its unlock ([The break-glass administrator](sign-in-and-identity.md#the-break-glass-administrator)), and a backup that completed with warnings ([Backups](#backups)); filter by the outcome **Warning** to see them all. A target in a repository is the repository and the path; a user or service account that is the event's own actor (a sign-in, for example) is named as the **Actor** column names it, with its kind under it; any other target is its kind with its identifier under it, cut where the column ends: point at it for the whole identifier, or open the event. **Filters** opens a panel from the right with **Time range**, **Actor** (a user or a service account, chosen from a search by its name), **Action** (an exact type or a prefix such as `token.*` or `ci.issuer.*`; the field suggests every type with its name and every prefix), **Category**, **Repository**, **Resource kind** and **Resource ID** (the resource an event is about, such as `bastion` and a registration's ID; the field of the kind suggests the kinds the events name most) and **Outcome**; each filter that is set stands as a chip beside the button. **Show times in UTC** switches the time zone of the moments. Selecting a row opens the event, beside the log in a wide window, where it stays in view while you scroll the log as long as it fits in the window, and above it in a narrower one; **Close** takes you back to the row you opened it from. The event shows its actor, token, **Resource** (its kind and its identifier, which the copy button beside it copies), **Source address**, **Endpoint**, **Replica**, **Event ID**, **Trace ID** and details; a value of the details longer than 200 characters is shortened to its start and its length, and the copy button beside them copies them whole. **Export CSV** and **Export JSON Lines** download the current filter, oldest first, for a range of at most 400 days; every export is itself recorded as `audit.exported`. Reading the log is not recorded.

**A download is one row.** With `audit.record_downloads: true` Quader records every file it serves as an event of its own: a pull of an image is one event per manifest and per layer, and an `npm install` one per package. The log shows the files that one caller fetched of one image or one version as one row: its **Target** names the repository, the image or package with its tag or version (an image pulled by its digest: the digest of the manifest it fetched first) and how many files it holds with their size; **Time** says when it began and how long it went on (**for 12 s**). The chevron before the target opens the row in place with its first ten files: the time to the second, which opens that file's event beside the log, the path, which opens the file, and its SHA-256 digest, which the button beside it copies. **All 32 items** opens the log of that row: every one of its events, oldest first, under the chip **Events of**; remove the chip to return. A row joins the files of one caller (the same user or account, the same token and the same address) that follow each other by at most a minute; two pulls of one image by one caller within a minute are one row, and a pull that pauses longer than a minute is two. A refusal (a download the malware scan, the signature gate, the vulnerability block or a delivery rule refused) is always a row of its own. The data keys handed to a Bastion for one pull are one row beside its download. Events recorded before 2026-11-26 name no download and stay one row each.

**A push, a publish and a delete are one row too.** Quader records every file a push stores, the version it publishes, every file a delete removes and the version or image it removes, each as an event of its own; the log shows one act of one caller on one image, version or package as one row. Its **Action** says what was done in one word: **Pushed** (an image, a NuGet package), **Published** (an npm package, an Ansible collection, a Debian package), **Uploaded** (files that publish no version, such as generic files: the files of one folder are one row), **Deleted**. Its **Target** names the image or package with its tag or version, or how many versions a delete removed (**3 versions**: deleting a package with all its versions is one row), and how many items it holds with the size of its files. Opened, it lists its files with their paths and digests and the version it published or removed by its action (**Package version published 1.4.0**). The permission checks behind a push or a delete (**Write allowed**) are one row beside it, **Writes allowed**: filter by the category **authz** to see them alone. With `authz.audit.reads: all` every allowed read is still a row of its own. Two deletes of two versions of one package by one caller within a minute are one row.

The exports and the API are unchanged: **Export CSV**, **Export JSON Lines** and `GET /api/v1/audit/events` hold one line or one event per file, per version and per check, with the file's path, digest and, from 2026-11-26, its size, package, version and unit. The rows of the screen are `GET /api/v1/audit/rows`, a row's events `GET /api/v1/audit/rows/{id}/events`; both need the same `audit:read` as the events. The filter **Actor** takes a user's or a service account's name, or its ID.

<details>
<summary>What is recorded</summary>

| Area | Events |
|---|---|
| Sign-in | successful and failed logins, break-glass login, lock and unlock, logout, ended sessions, CSRF failures, rejected credentials |
| Tokens | created, rotated, revoked, expiring soon, rejected (also on an endpoint that does not accept the token) |
| Identities | users, groups, group memberships and their sync from the identity provider, service accounts |
| Permissions and settings | policy and role-binding changes, package ownership, authentication and permission settings, refused requests (`authz.denied`), allowed writes |
| Repositories and endpoints | repository created, updated, deleted; exposure rules created, updated, deleted |
| Content | files deployed and deleted; npm publish, unpublish and dist-tag changes; remote repository events such as an upstream integrity mismatch or a refused upstream address |
| System | a task run started by hand, a cancelled job, an audit export |

Repeated denials and rejected tokens are folded into one event with a count per `audit.denial_dedup_window` and `audit.token_rejection_dedup_window`.

</details>

```yaml
audit:
  retention_days: 365                   # default; 0 keeps every event
  record_downloads: false               # default; true records every file served, one event per file
  record_anonymous_denials: false       # default
authz:
  audit:
    reads: denied                       # default: none | denied | all (all also records allowed reads)
```

The retention is applied by `quader migrate`, which the chart runs before every upgrade, and by the daily `audit-retention` task. Each event is also written to the log stream `audit`; ship that stream to a store outside Quader if you need a copy that Quader's own database users cannot change.

## Shutdown and node drains

When a pod receives `SIGTERM`, Quader shuts down in a fixed order within the pod's termination grace period (`api.terminationGracePeriodSeconds` and `packages.terminationGracePeriodSeconds` 120, `worker.terminationGracePeriodSeconds` 300):

1. `/readyz` answers `503` at once and the replica is marked draining.
2. An `api` or `packages` replica keeps serving for `health.shutdown_drain_delay` (5 s) so the ingress controller can remove it; answers carry `Connection: close`.
3. The listeners close and requests in flight finish, until the shutdown timeout (`server.shutdown_timeout`, derived as grace period minus drain delay minus 5 s). An upload that is committing gets up to 3 s more; other requests still running then are cut and counted in `quader_http_transfers_cut_total`.
4. A `worker` stops claiming jobs at once, and a job that is only waiting (a garbage collection run waiting for its deleted objects to settle) is returned to the queue at once. Jobs that are working may finish; after 5 s the log names the ones the shutdown waits for (**shutdown waits for the running jobs**). Shortly before the timeout, those still running are cancelled and returned to the queue without counting an attempt (**running jobs released for shutdown**); another worker continues them from their last checkpoint. A worker with long jobs therefore takes most of its grace period to stop: with the chart's 300 s, about 280 s.
5. Buffered audit events are written, the replica's row is deleted from the replica list, and the connections are closed. While PostgreSQL switches over, the replica retries the mark of step 1 and this deletion for up to 15 s instead of leaving its row behind; the log says when it did.

A second signal ends the process immediately. The chart refuses a grace period shorter than the drain delay plus 15 s. Set the drain delay above the time your ingress controller needs to stop sending to a removed pod; if clients see connection errors during drains, raise it.

For a node drain, the chart's PodDisruptionBudgets (`maxUnavailable: 1` per role) and the spreading of replicas across hosts keep at least one replica per role serving, as long as each role has two or more replicas on different nodes. Check **Not drain-safe** on the System status screen before you drain.

## Scaling api, packages and worker

| Role | Scale it when | Setting |
|---|---|---|
| `api` | the UI or the management API is slow, or requests are shed with `503 server_busy` (`quader_http_requests_shed_total`) | `api.replicaCount` (default 2), `api.resources.limits.memory` |
| `packages` | package clients see `503 server_busy`, or latency grows | `packages.replicaCount` (default 3), `packages.resources.limits.memory` |
| `worker` | `quader_jobs_queued` keeps growing | `worker.replicaCount` (default 2), `config.jobs.workers` (concurrent jobs per queue) |

Any `api` replica can answer any request of its routes, any `packages` replica any package request, and any worker can run any job; scheduled tasks run once for the whole installation. Each `api` and `packages` pod admits as many concurrent requests as its memory limit allows (`quader_http_memory_slots`), so the chart requires `resources.limits.memory` for each. A remote repository's `limits.maxConcurrentFetches` is shared by the `packages` replicas (and `web` and `all` outside the chart): the other roles hold no share of it. With `highAvailability.enforce: true` (default) the chart requires at least two replicas per role.

Change replica counts through `helm upgrade` rather than `kubectl scale`: the migration Job then compares the connections all replicas may open with PostgreSQL's `max_connections` (`database.preflight.connection_budget: enforce`) and stops the upgrade if they do not fit.

The Job counts connections, not memory. Each connection of a serving pod held 6 to 8 MiB of the PostgreSQL primary in one measurement, so when you raise replica counts or `database.pool.max_conns`, give the primary about 8 MiB for every connection of the budget beside its `shared_buffers`. The budget the chart prints after `helm upgrade` already counts the extra pod a rollout starts per role; behind a transaction-mode pooler count the server connections the pooler opens instead.

## When a dependency is unavailable

| Dependency | What happens |
|---|---|
| Valkey | No request fails and replicas stay ready (`/readyz` lists `valkey` as degraded). Remote fetches are collapsed per replica only, the negative cache is per replica, rate limits fall back to a per-replica share of each budget, and changes reach other replicas through the PostgreSQL change log instead of an early message. System status shows **Degraded**. A pod that starts meanwhile stays unready while it waits for Valkey's first answer, at most `valkey.startup_answer_timeout` (30 s), and then becomes ready degraded; its log says `valkey gave no answer within valkey.startup_answer_timeout` with the reason |
| PostgreSQL | Outages shorter than `health.database_outage_grace` (20 s) show as degraded; longer ones make the replicas unready. PostgreSQL holds everything durable, so Quader cannot serve without it |
| Object store | An upload goes to the next connection of `storage.connections` that answers and a download reads another copy, as long as the connections that answer can take `storage.replication.min_copies` copies or hold the blob. Otherwise requests for stored content fail with a retryable `503` on the affected replica; everything else keeps working. After the start, readiness depends on it only with `health.require_object_store: true` |

**When the object store loses objects.** The bucket can lose objects behind Quader's back: a restore of the database to a point older than `storage.gc.grace_period`, a lifecycle rule or a deletion by another tool, an object store that lost its data. The rows that describe the content stay. With several connections, a read that finds one copy missing marks that copy (`quader_storage_copies_damaged_total`), reads the blob from another connection, and treats the blob as missing only when no connection holds it any more; the reconciliation writes the marked copy again from another connection. The first read that finds the last object missing marks its blob `missing`, logs `the object of a blob is missing from the object store` with its digest, and counts `quader_storage_blobs_damaged_total{status="missing"}`, which the alert `QuaderBlobMissing` reads. What the clients see then:

- **A remote repository** serves an item whose object is back without asking its upstream, and otherwise fetches the item again from its upstream on the next request, a cached package document as well as a tarball or a package, and writes the object again; the log line `the stored object of a cached item is not usable` names the repository, the path and the digest. While it cannot (offline, the upstream down, the cache view, a caller who may not fill the cache, also through a virtual repository), the answer is `503 content_unavailable` with `Retry-After`, never `404`. An artifact keeps the digest it was cached with: when the upstream now serves other bytes, nothing is stored, the answer stays `503`, and the audit event `proxy.upstream_digest_changed` is written once an hour; each replica then asks the upstream for that item again only after 5 minutes (`Retry-After` says when). The same holds for a package document that npm keeps because the upstream's copy is older.
- **A local repository** answers `503 content_unavailable` (with `Retry-After: 30`). For 30 seconds per file it answers from the database alone; then the next read asks the object store again, and a file whose object is back at the key its copy names (restored in the bucket, or an endpoint that pointed at another store corrected) is served again and its mark is cleared. A lost object therefore costs the object store one request per file and 30 seconds, however many clients ask. Uploading the same file again also writes the object and clears the mark.
- **After a mistake that hid the objects** (an endpoint that points at another store, objects moved away from their keys): correct it, and the files come back on their own within 30 seconds of their next request; nothing has to be uploaded again. `QuaderBlobMissing` fires for as long as new blobs are found missing. The bucket and the prefix of a connection that holds copies cannot be changed by mistake: the start refuses the changed element and names the number of copies it holds.
- **Something in front of the store answering 404.** Quader takes every `404` of the object store as "no such object", except the codes `NoSuchBucket` and `NoSuchUpload`. A gateway, load balancer or proxy in front of the store that answers `404` during a rollout or a misrouting therefore marks every blob that is read meanwhile, `QuaderBlobMissing` fires, and remote repositories fetch those items again from their upstreams. When `QuaderBlobMissing` fires and the bucket still holds the objects, look at what stands between Quader and the store first; once it answers again, the marks heal within 30 seconds of each file's next read.

## Caches

Each replica keeps caches in memory: sessions, tokens, the permission index, the repository list, rendered package documents and the keys of unknown repositories. A change made on one replica reaches the caches of the others through the PostgreSQL change log, polled every `cluster.epoch_poll_interval` (1 s); Valkey messages only make it faster. Every cache also expires its entries after a safety interval. No cache needs to be cleared by hand.

```yaml
cache:
  rendition_bytes: 256MiB               # default: rendered package documents, per replica
  snapshot_safety_ttl: 5m               # default
  unknown_repo_keys: 10000              # default: per replica
```

The caches of remote repositories (fetched packages and their metadata) are a different matter; see [Managing repositories](repositories.md).

## Alerts

With `metrics.prometheusRule.enabled` the chart renders one PrometheusRule with the alerts below and those of [Vulnerability scanning](#vulnerability-scanning), the mirror mode, the malware scanning and the [signatures](#signatures). It needs the Prometheus operator's custom resource definitions and a scrape of the ops port (the chart's ServiceMonitor, see Metrics above). Every selector is scoped to the release's namespace. Every alert can be left out by name in `metrics.prometheusRule.disabledAlerts`; a name that is no alert of the chart fails the install.

| Alert | Fires when | For | Severity |
|---|---|---|---|
| `QuaderReplicasNotReady` | a Deployment of the chart (a role or a pool) has fewer than two available replicas. Needs kube-state-metrics. A Deployment you scaled to one replica fires too | 5m | critical |
| `QuaderDatabaseUnreachable` | replicas retry their connection to PostgreSQL and answer `503` | 2m | critical |
| `QuaderObjectStoreUnreachable` | every replica has the circuit of every object-store connection open | 2m | critical |
| `QuaderObjectStoreConnectionUnreachable` | every replica has the circuit of one connection open while another connection answers; never fires with one connection | 2m | warning |
| `QuaderObjectStoreUnreachableSingleReplica` | the circuit of a connection is open on some replicas and closed on others: the cause is on the path of those pods | 5m | warning |
| `QuaderStorageReplicationBacklog` | a blob has waited more than an hour for its copy on a connection that is not filling | 30m | warning |
| `QuaderStorageCopyDamaged` | a connection holds a missing or corrupt copy | 1h | warning |
| `QuaderStorageScrubBehind` | copies on a connection have not been read whole within its `scrub_period` | 6h | warning |
| `QuaderStorageConnectionDetached` | a connection holds copies but no replica has it in its configuration | 15m | warning |
| `QuaderStorageUnknownKey` | reads of an encrypted connection keep meeting records of storage keys the database does not hold: those copies are unreadable until the export of the storage keys is imported | 10m | critical |
| `QuaderDatabasePoolSaturated` | the 95th percentile of the wait for a connection of a replica's request pool is above 100 ms | 10m | warning |
| `QuaderControlConnectionWaits` | the 95th percentile of the wait for the control connection is above one second, over five minutes, and stays so for ten: the waits go on for five minutes, so the switchovers of a node rotation do not raise it; readiness checks are close to failing | 10m | warning |
| `QuaderJobsStuck` | a queued job has been waiting for more than 15 minutes | 5m | warning |
| `QuaderLeaseLost` | a singleton task lost its lease more than three times in 15 minutes | at once | warning |
| `QuaderMixedVersions` | replicas of more than one version run for two hours: an upgrade is stuck | 2h | warning |
| `QuaderSpoolNearlyFull` | what a replica's spool holds (running uploads, or the images a scanner pod scans) is more than nine times the free space of its volume; one alert per unit, named by its label `role` | 10m | warning |
| `QuaderGCStale` | the last garbage-collection run that ended without error is more than two days old. A worker that has not completed a run since it started reports nothing, so a freshly started installation does not fire | 1h | warning |
| `QuaderValkeyUnavailable` | every replica runs without Valkey | 2m | critical |
| `QuaderValkeyUnavailableSingleReplica` | some replicas lost Valkey, others did not | 5m | warning |
| `QuaderValkeySentinelsDegraded` | a replica reaches fewer than two Sentinels, so no failover can be elected | 5m | warning |
| `QuaderValkeyFailoverFlapping` | the Valkey primary changed more than twice within an hour | at once | warning |
| `QuaderValkeyPrimaryDisagreement` | replicas name different Valkey primaries | 5m | critical |

`QuaderStorageReplicationBacklog` and `QuaderStorageConnectionDetached` read the reconciliation's gauges. `QuaderStorageScrubBehind` reads `quader_storage_scrub_overdue_copies`, which every replica exports: the present copies of a connection that the scrub has not read whole within the connection's `scrub_period` and one day. `QuaderStorageCopyDamaged` reads `quader_storage_copies` of the hourly storage summary, so it fires at the earliest an hour after the summary that first counted the damaged copy; the tab [Storage](#storage) shows the connection's damaged copies, and `GET /api/v1/storage/damaged-blobs?scope=copies` lists them. `QuaderStorageUnknownKey` reads `quader_storage_encrypted_read_failures_total{reason="unknown_key"}`: such a read marks nothing and falls over to another connection's copy, because importing the export brings the key back ([Runbook](runbook.md#storage-keys-are-missing)); a key destroyed on command (`reason="destroyed_key"`) does not fire it.

The rule also holds the alerts of the backup runs (`QuaderBackupStale` for a backup that is overdue or never succeeded, `QuaderBackupFailed`, `QuaderBackupRunningLong`, `QuaderBackupGlobalHoldLong`, `QuaderBackupSourceDamage`, `QuaderBackupVerifyFindings`), but only while `backup.enabled` is true: with the backup off no run succeeds, and `QuaderBackupStale` would fire one `config.backup.expected_interval` (a day by default) after the install. `QuaderBackupCheckFailed` (a platform protection you declared in `backup.recovery_window` or `backup.object_versioning` is missing) and the revocation journal's `QuaderRevocationJournalBacklog` and `QuaderRevocationJournalQuarantined` are in the rule either way.

```yaml
metrics:
  prometheusRule:
    enabled: true                  # default: false
    labels: {release: kube-prometheus-stack}   # example: the label your Prometheus selects rules by
    disabledAlerts: []             # default; example: [QuaderJobsStuck]
    extraRules: []                 # default: your own rules, appended after the chart's
```

## Vulnerability scanning

The scanning needs the `worker` role: the sync of the advisory data and the evaluation of the stored versions run there. Nothing on this page makes a replica unready, because no replica can repair advisory data; the system information, the report banner and the alerts show it.

**Alerts.** The chart renders a PrometheusRule when you ask for it (see [Alerts](#alerts) for the settings and the other alerts). The alerts of the scanning:

| Alert | Fires when | Severity |
|---|---|---|
| `QuaderVulnerabilityDataStale` | no sync run of a source and ecosystem has ended successfully for longer than `vulnerabilities.stale_after` (default 24h, read from `config.vulnerabilities.stale_after`) for 15 minutes. A source that never synced counts: its last success is 0 | warning |
| `QuaderVulnerabilitySyncFailing` | three consecutive runs of one source and ecosystem failed; a cancelled run does not count. The last error is on the sources page of the vulnerability section | warning |
| `QuaderVulnerabilityRecordsRejected` | the sync rejected an advisory record above a size bound (larger than `max_record_size`) within the last seven days; the worker's log names it. Records that fail validation are counted separately and do not fire it. Stays on for seven days after the last rejection | warning |
| `QuaderVulnerabilityRecordsInvalid` | at least ten records, and more than 1 % of those the sync processed in 24 hours, failed validation and were not applied; a single record that fails is only counted and logged | warning |
| `QuaderVulnerabilityMaliciousRecordRejected` | a malicious-package record (`MAL-…`) was rejected, by a size bound or by validation, so the package it names is not flagged or blocked by it | critical |
| `QuaderVulnerabilityClaimedNameMalicious` | a malicious-package advisory names a package name that a local repository claims | critical |
| `QuaderBlobMissing` | a replica found the object of a blob missing from the object store within the last hour (see "When the object store loses objects" above; a gateway in front of the store that answers 404 fires it too) | critical |
| `QuaderBlobCorrupt` | within the last hour a read found that no connection holds a blob whole any more: every copy it reached had other bytes or another size | critical |

A name in `disabledAlerts` that is no alert of the chart fails the install. If you set `stale_after` only through an environment variable (`extraEnv`), set it in `config.vulnerabilities.stale_after` as well, or the alert keeps the 24-hour default.

**The analyser's image.** The image scanning runs in the pods of `<fullname>-vuln-scanner-trivy`. The Quader image carries no analyser:
each scanner pod starts with an init container `trivy` that copies the analyser from its own image, `trivy.image` (by default
`docker.io/aquasec/trivy:0.76.2` by its digest), into a volume the scanner reads but cannot change, and ends. A node that has not run a
scanner pod before pulls that image from Docker Hub first (about 60 MB). What to look at:

- A scanner pod that stays `Init:0/1`, `Init:ErrImagePull` or `Init:ImagePullBackOff` cannot pull the image: `kubectl describe pod` shows
  the registry's answer in the events. Meanwhile the images wait as `scanner_unavailable` and `QuaderVulnerabilityImageScannerUnavailable`
  fires; nothing else of Quader depends on it.
- If your nodes pull from a mirror of your own, copy the image by its digest (`skopeo copy --all` or `crane copy` keep it) and set
  `trivy.image` to the mirror's name with the same digest; `global.imagePullSecrets` reaches the init container as every container. Do the
  same for `clamav.image` while the malware scanning is on, and for the Quader image. If your nodes reach no registry, load the images into
  each node's container runtime beforehand and keep `trivy.pullPolicy: IfNotPresent`, or switch the image scanning off
  (`config.vulnerabilities.images.enabled: false`). A kubelet that pulled the image under one name pulls it again when it is asked for
  under another name, to check that name (seen with Kubernetes 1.37), so the name you set must be one the node can pull, even where the
  same digest is already on the node.
- The version each scanner runs is on the System page (the analyser's version per scanner pod). A new version of the analyser makes the
  scanners analyse every stored image again; its first detections after the rollout may report `scanner_versions_differ` until every pod
  runs the new one.

**Worker spool.** A sync downloads one archive into the worker's spool. While `vulnerabilities.sources.osv.mode` is `online` or `repository`, the chart refuses an install in which the worker's spool budget (`worker.spool.sizeLimit`, else `spool.sizeLimit`, minus `spool.headroom`) is smaller than `vulnerabilities.sources.osv.max_archive_size` (default `1GiB`; the default worker spool is `5Gi` with `2GiB` headroom). The message names both values.

**After a restore.** The findings and their history are part of a backup; the advisory data is not, because the next sync fetches it again. A restored installation therefore starts with every ecosystem empty:

- every finding keeps its status, and every blocked or malicious version stays refused; a dismissed finding stays dismissed, and its history is intact;
- no finding resolves while the data is missing, and new versions are served unevaluated (or refused, where a repository sets `onMissingData: block`);
- `npm audit` through Quader answers `503` and the NuGet vulnerability resource is not listed;
- the system information shows each source with state `empty`, `QuaderVulnerabilityDataStale` fires after 15 minutes, and the first `vuln-sync-osv` slot (every 15 minutes by default) imports the data and starts the full evaluation. You can start it at once from the task list.

## Malware scanning

Quader compares the bytes of every file a client can download with ClamAV's signatures of known malware, once per content for the whole
installation. A signature scan does not find malware that no signature describes yet, such as a malicious install script published
today; for npm and NuGet, the vulnerability scanning blocks known malicious packages by name. Content in which the scan finds a signature is
refused always while the scanning is on (`malware.enabled`, on by default).

**The page.** **Admin area › Settings › Malware scanning** (`/admin/settings/malware`) is shown to whoever holds `settings:read` while
the scanning is on; changing the switch and **Scan everything again** need `settings:write`. An auditor reads the page; the switch is
then a sentence, not a check box. The page holds the settings and the state of the scanning, and no find: the finds are the instance
auditor's, under **Security audit › Malware** ([Security dashboard](../user/security-dashboard.md#malware)), and the page links
there for whoever may open it. With `malware.enabled: false` the side bar has no entry, and the page says that the scanning is off.

| Section | What it shows |
|---|---|
| **Downloads** | **Only checked artifacts can be downloaded**, off by default. On, a download is served only when the scan found no malware or an auditor released the bytes: content that is not checked yet waits up to `malware.download_wait` (25 seconds) for its scan and is then refused with `503` and `Retry-After`; content nothing could check (too large, a scan limit reached inside it, encrypted, the scans failed) is refused until an auditor releases it. When content waits or cannot be checked, **Save changes** asks first and names how much. Who changed the switch last, and when |
| **Scanning** | **Not checked yet** and **Could not be checked**: what the switch would hold or refuse now. **Scanners**: the ready scanners and the scans they run now, as the replicas below report them and read again every half minute, or **None ready**; under it, closed, **Replicas of malware-scanner-clamav (n)**: each pod with its state and why it is not ready, its ClamAV version, the signature database it scans with and its date, and its slots in use, or a sentence why there is none. **Scan everything again** asks for every verdict to be scanned again, behind the downloads that wait and the new content; each verdict stays in force until its new scan |
| **Signature database** | The version in use, its date with **Current** or **Stale** (older than `malware.database.stale_after`, 48 hours; scanning goes on with it), when Quader stored it, and the repository that keeps it (`upstream/clamav`, on its page of the upstream registries). A failed update names what failed and how often in a row, and folds the task's own message with its code under **Technical details**. Until the first version is in use, nothing is scanned |

**When an artifact is checked again.** Every artifact is checked when it is stored. Afterwards each check has a *scan pressure*: how
much newer the signatures in use are than the ones it was checked with, plus a bonus for how often it was downloaded since, and its check
is repeated once the pressure reaches the rescan cycle, highest pressure first, behind downloads that wait and new content. With the
defaults: an artifact nobody downloads is checked again once the signatures are 30 days newer; each doubling of its downloads
brings that 6 days forward, down to one day, so the most downloaded artifacts are checked again with nearly every new set of signatures;
whatever was downloaded in the last 7 days is checked again at every new set; a check due for more than 7 days counts as overdue. These
are settings of the installation (`rescan_cycle`, `rescan_shortest`, `rescan_demand_step`, `rescan_recent_within`, `rescan_overdue_after`
of `malware_settings`); a change of the scan limits, and **Scan everything again**, make the checks they name due at once. A new ClamAV
version adds no pressure of its own: press **Scan everything again** after raising it when every artifact should be checked by it.

**A failed update** of the signature database is one of:

| Message | Code | What to do |
|---|---|---|
| The updater could not be reached. | `updater_unavailable` | Recorded only once a database was in use: the worker's request to `malware-updater-freshclam` got no answer, a refused handshake or a `5xx`; the updater's row on the System page and the log of its container `quader` say why. Scanning goes on with the database in use |
| No signature database was found. | `database_missing` | Recorded only once a database was in use: the updater `malware-updater-freshclam` offers no complete set (the logs of its containers `quader` and `freshclam` say why); without internet, the repository holds no database at its root |
| A file of the signature database was refused. | `database_invalid` | A file failed the checks (no header, too large, a name the updater may not offer, a version lower than the one in use), or a scanner could not load the version; the version before stays in use and the refused one is not offered again |
| ClamAV's signature of a file did not verify. | `signature_invalid` | A container from the updater does not carry ClamAV's valid signature, or its content differs from what its header states; nothing of that update is stored and the version in use stays. A file damaged on the way is fetched again by `freshclam` at its next check; a lasting failure means the updater, or the mirror it uses, offers files ClamAV did not publish |
| The repository the configuration names cannot hold the database. | `database_repository_invalid` | `upstream/clamav` is missing (run `quader migrate`), or with the source `repository` `malware.database.repository` names no local generic repository |
| A file of the signature database could not be stored. | `store_failed` | The object store refused the write; see [When a dependency is unavailable](#when-a-dependency-is-unavailable) |

**Without internet.** With `malware.database.source: repository` and `malware.database.repository` naming a local generic repository of
your own, the chart runs no updater and the page shows the commands that fill it, with this installation's address and the repository filled in: ClamAV's mirror tool
`cvdupdate` fetches the files on a machine that reaches the network, and the three whole files are uploaded to the root of the repository.
Whoever may deploy to that repository decides what the scanners compare content with: besides the publisher's signed files it also
takes ClamAV's unsigned `.cud` containers, which ClamAV loads without a publisher's signature.

**The updater and your network.** One pod fetches ClamAV's signatures: the updater `<fullname>-malware-updater-freshclam`, which runs
ClamAV's own `freshclam` and holds no credential. No other pod of Quader needs a way to ClamAV's network; the worker asks the updater
inside the cluster. The chart restricts no egress, so where a policy of your cluster denies egress by default, admit the updater's pods by
a label you give them in `malwareUpdaterFreshclam.podLabels`, for:

| Where to | Port | When |
|---|---|---|
| the cluster's DNS resolver | 53, UDP and TCP | always |
| `database.clamav.net` | 443 | by default. ClamAV serves it from Cloudflare's addresses, which change: a policy by address has to admit Cloudflare's published ranges, a policy by name needs a policy engine that matches host names |
| the host and port of `malwareUpdaterFreshclam.privateMirror` | as in the URL (443 for `https`, 80 for `http`) | instead of ClamAV's network, when you run a mirror of whole containers (`cvdupdate`'s `cvd serve`, or the ClamAV definitions endpoint of another Quader) |
| the host and port of `malwareUpdaterFreshclam.proxy` | as in the URL (80 when none is given) | when the way out is an HTTP proxy |

```yaml
malwareUpdaterFreshclam:
  podLabels:
    egress/clamav: "allow"                        # example: the label your egress policy admits
  privateMirror: ""                               # default: ClamAV's network; example: https://clamav-mirror.example.com
  proxy: ""                                       # default: none; example: http://proxy.example.com:3128
```

The updater checks for new signatures every two hours and keeps working with what it has when it cannot reach the network: scanning goes
on with the database in use, and after 48 hours `QuaderMalwareDatabaseStale` fires. The worker asks it every 30 minutes and takes only what
it does not hold yet; a restarted updater first copies the database in use from Quader, so it fetches only what is newer from ClamAV's
network. `upstream/clamav` keeps the files of the database in use and of the one before it; older versions are listed as **Files removed**
for the rescan cycle (30 days by default) and then go. Until the first version is stored no alert fires and
no failure is recorded: the updater's row on the System page and the empty **Signature database** on this page are what show it.

**A repository's own setting.** A repository's settings have a closed section **Malware scan** with **Only checked artifacts can be
downloaded from this repository**: a team that wants checked content without the installation's switch sets it. It only makes a
repository stricter: while the installation's switch is on, it is checked and cannot be changed. On a virtual repository it covers
everything the repository serves from its members, and a strict member stays strict through every virtual repository. An Owner of the
project may switch it on; switching it off needs `repositories:write-config` or `vulnerabilities:triage`, and the section tells an Owner
whom to ask. It is not part of the form that creates a repository.

## Signatures

Quader detects the signatures of every artifact, checks each once per content for the whole installation, and decides whether a trust
anchor knows its signer. It refuses no download for a signature. The checks run in the worker's jobs `signature-verify` and
`signature-trust` on the queue `signatures` (`jobs.workers.signatures`, one slot); the task `signature-verify` runs every five minutes
and finds what was stored before the signatures existed, and the task `signature-trust-root` refreshes the public Sigstore root and npm's
registry keys once a day from `signatures.trust_root.url` through the outbound client. The trust anchors, the switch for project Owners and
the blocked signers are set under **Admin area › Settings › Signatures** ([Signatures](signatures.md)) or through the management API
(`/api/v1/signatures/…`) by a holder of `security-settings:write`; anchors of the Helm values (`signatures.anchors`) are applied by
`quader migrate` and read-only.

| Signal | Meaning | What to do |
|---|---|---|
| `QuaderSignatureInvalid` | a check found a signature that does not match its content within the last hour | **Security audit › Signatures** (`GET /api/v1/signatures/findings`): which repository holds it, the reason, the signer it claims. An invalid signature beside a valid one lowers no badge; one alone reads **Invalid signature**. If the signer is one to stop trusting, block it |
| `QuaderSignatureBacklog` | the oldest due signature has waited over an hour | A worker claims the queue `signatures`; the jobs `signature-verify` on the Jobs screen and their errors; `quader_signatures_backlog` by priority |
| `QuaderSignatureTrustRootStale` | the public Sigstore root in use is older than 14 days (the refresh's time, or the release's build) | The task `signature-trust-root`'s last error (`GET /api/v1/signatures/settings`, `trustRoot.lastError`) and the egress to `tuf-repo-cdn.sigstore.dev`; without internet set `signatures.trust_root.url` to a TUF mirror of your own, or leave out the alert by name and follow the releases |
| `QuaderSignatureGatePending` | downloads from a repository whose setting **Only artifacts with a verified signature** is on got `503` because the checks of their signatures, or a remote's fetch of them, did not decide within `signatures.gate.download_wait` | That a worker claims the queues `signatures` and `index`, `QuaderSignatureBacklog`, and the remote's upstream; Docker and `ansible-galaxy` show the `503`, npm and NuGet retry |
| `quader_signatures_trust_rows_stale` above 0 for long | a change of anchors, blocks or the switch has not reached every signature yet; the API marks such rows `stale` | The jobs `signature-trust` |

## Quader's signing identity

Quader signs with one identity per installation: an ECDSA P-256 key with a code-signing certificate of your company's CA. It comes from the
Helm values (`signing.identity.existingSecret`, or `signing.identity.certManager` for a cert-manager `Certificate` `<fullname>-signing` whose
issuer can issue the usage `code signing`), mounted into the worker pods only; or, without either, from **Admin area › Settings ›
Signatures**, section **Signing identity** ([Signatures](signatures.md#signing-identity)), or the management API: a holder of
`security-settings:write` creates a key (`POST /api/v1/signing/identity/key`), has the CA issue a certificate for the request it answers,
and uploads it with its chain (`PUT /api/v1/signing/identity/certificate`); a key and certificate the CA handed out together are imported
(`POST /api/v1/signing/identity/import`). While the Helm values name an identity, uploads answer `409 signing_identity_managed`. A new
identity beside the current one is listed for `signing.identity.announce_period` (10 minutes) before it signs; the job `signing` on the
queue `signing` switches it. The section and `GET /api/v1/signing/identity` show the identities and a refused pair of the files with its
reason, the answer also the backlog; the trusted root and the chain are public at `/api/v1/signing/trusted-root.json` and `/api/v1/signing/chain.pem`. With the identity
Quader signs npm's registry signatures of local versions and a receipt attestation of every artifact ([Signatures](signatures.md)).

| Signal | Meaning | What to do |
|---|---|---|
| `QuaderSigningIdentityExpiring` | the current certificate ends within 14 days and no successor is announced | With the Helm values, `kubectl describe certificate <fullname>-signing` and its issuer; a renewal is announced within `signing.identity.reload_interval`. With an upload, create a key, have it certified, upload it ([runbook](runbook.md#a-signing-identity-that-expires)) |
| `QuaderSigningIdentityExpired` | the certificate has ended; Quader signs nothing | Install a new identity at once; the job signs what waited |
| `QuaderSigningIdentityNotLoaded` | an identity is current but no worker holds its key | `refused` of `GET /api/v1/signing/identity` (a pair of the files that fails the checks, with its reason), the worker pods' mount at `/var/run/secrets/quader/signing/`, `quader admin secrets status` for an uploaded key |
| `QuaderSigningBacklog` | a local npm version has waited more than five minutes for its signature | A worker claims the queue `signing` (`jobs.workers.signing`); `quader_signing_operations_total` by result; `backlog` of the identity's answer |
| `signing.identity.refused` in the audit log | a pair of the files failed the checks (an RSA key, no `codeSigning`, a chain without its root, less than a day left); the identity in use keeps signing | Fix the Secret or the issuer; the next look of a worker registers the pair |

## What to watch

| Signal | Meaning |
|---|---|
| `quader_ready == 0` for a replica, or **Replicas unready** | the replica takes no traffic; `/readyz?verbose=1` names the check |
| `quader_tls_certificate_expiry_timestamp_seconds - time() < 14 * 86400` | a certificate expires within two weeks |
| `increase(quader_tls_reload_errors_total[15m]) > 0` | a rotated certificate did not load; the old one is still served |
| `quader_valkey_available == 0` | degraded mode, see above |
| `time() - quader_cluster_poll_last_success_timestamp_seconds` above a few seconds | the replica cannot read the change log |
| `increase(quader_http_requests_shed_total[5m]) > 0` | a replica is at its concurrency or memory bound |
| `quader_http_requests_total` with `code` 5xx | server-side failures; the request log names them |
| `quader_jobs_queued` rising, or no live worker on System status | background work is not getting done; `QuaderJobsStuck` fires on the age of the oldest queued job |
| `increase(quader_audit_events_dropped_total[15m]) > 0` | audit events reached only the log stream |
| `increase(quader_db_pool_empty_acquires_total[5m])` high | the database pool is too small for the load |
| `QuaderVulnerabilityDataStale`, `QuaderVulnerabilitySyncFailing` | advisory data of a source is older than `vulnerabilities.stale_after`, or three sync runs in a row failed; see [Vulnerability scanning](#vulnerability-scanning) |
| `QuaderVulnerabilityClaimedNameMalicious` | a malicious-package advisory names a package name your installation treats as internal |
| `quader_endpoint_misdirected_total` rising | clients or the ingress send an endpoint hosts it does not serve |

TLS details are in [TLS and certificates](tls-and-certificates.md); endpoint metrics in [Endpoints and exposure rules](endpoints.md). What to do when one of these fires is in the [Runbook](runbook.md).
