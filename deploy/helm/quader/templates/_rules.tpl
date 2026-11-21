{{/*
The alerts of the PrometheusRule (R-HA-045) as a YAML list, before disabledAlerts is applied. Names are
stable. Rendered: the alerts of the vulnerability scanning (R-VULN-024), the two of the mirror mode (R-OPS-046,
Q-OPS-160, Q-OPS-171), QuaderBlobMissing (R-HA-024, Q-STOR-152) and
the sixteen rows of R-HA-045's table whose metrics the binary exports, QuaderBackupCheckFailed (the platform check
task backup-check, R-HA-023), QuaderRevocationJournalBacklog (R-HA-054) and the six QuaderBackup* alerts of the backup runs
(R-BAK-014): every alert of the table. The four of the malware scanning (R-MAL-026) render while config.malware.enabled, the three
of the signatures on artifacts (R-SIG-039) while config.signatures.enabled, the four of the signing identity (R-SIG-057) while config.signing.enabled.

The six alerts of the backup runs are rendered only while backup.enabled is true (Q-HA-17): without the CronJob no run
ever succeeds, and the clause of QuaderBackupStale for a backup that never succeeded would fire one interval after every
installation that leaves the backup off. QuaderBackupCheckFailed and the journal's alerts do not depend on it: the platform
check and the journal serve the platform restore too.

The backup alerts handle the gauges that have no series until the event happened (Q-OPS-180): QuaderBackupFailed reads
the last success as "or vector(0)", and QuaderBackupStale has a clause for a success that never was: no series for
longer than config.backup.expected_interval, which the chart reads for the window, because the alert has no series to
read the interval from before the first success.

Every selector is scoped to the namespace of the release, because one Prometheus can scrape several
installations and a max() over all of them would blend their states. A scrape through the Prometheus
operator (the chart's ServiceMonitor, or a PodMonitor) always carries the namespace label.

The stale threshold is config.vulnerabilities.stale_after, the value the binary reads (default 24h), so that
the alert and the report banner agree (R-VULN-004). It is a duration parsed by quader.seconds.
*/}}
{{- define "quader.rules.all" -}}
{{- $ns := printf "namespace=%q" .Release.Namespace -}}
{{- $roles := printf "%s-(api|packages|worker|vuln-scanner-trivy|malware-scanner-clamav)(-.*)?" (include "quader.fullname" .) -}}
{{- $stale := include "quader.seconds" (dig "vulnerabilities" "stale_after" "24h" .Values.config) -}}
{{- $interval := include "quader.seconds" (dig "backup" "expected_interval" "24h" .Values.config) -}}
{{- $malwareStale := include "quader.seconds" (dig "malware" "database" "stale_after" "48h" .Values.config) -}}
- alert: QuaderReplicasNotReady
  expr: min by (deployment) (kube_deployment_status_replicas_available{ {{ $ns }},deployment=~{{ $roles | quote }} }) < 2
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: The Deployment {{ "{{ $labels.deployment }}" }} has fewer than two available replicas.
    description: A role or pool with fewer than two available replicas has no redundancy, and a node drain or a rollout takes it away (R-HA-001, R-HA-059). Look at the pods of the Deployment, their readiness (GET /readyz?verbose=1) and the events. Needs kube-state-metrics. A Deployment scaled to one replica by choice fires too.
- alert: QuaderDatabaseUnreachable
  expr: sum(rate(quader_db_retries_total{ {{ $ns }},kind="connection" }[1m])) > 0 and sum(rate(quader_http_requests_total{ {{ $ns }},code="503" }[1m])) > 0
  for: 2m
  labels:
    severity: critical
  annotations:
    summary: The replicas keep retrying their connection to PostgreSQL and answer 503.
    description: The primary is unreachable beyond the retry budget (database.retry, R-HA-009, R-HA-015), or the pooler in front of it is. Check the PostgreSQL primary, the Service that points to it and the pooler, and the database check of GET /readyz?verbose=1.
- alert: QuaderObjectStoreUnreachable
  expr: min(quader_storage_s3_circuit_open{ {{ $ns }} }) == 1
  for: 2m
  labels:
    severity: critical
  annotations:
    summary: Every replica has the circuit of the object store open.
    description: Blob reads and writes fail on all replicas (R-STOR-033, R-HA-010). Metadata and cached content that does not need the store still answer. Check the S3 endpoint, its certificate and the credentials in the Secret.
- alert: QuaderObjectStoreUnreachableSingleReplica
  expr: max(quader_storage_s3_circuit_open{ {{ $ns }} }) == 1 and min(quader_storage_s3_circuit_open{ {{ $ns }} }) == 0
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: The circuit of the object store is open on some replicas and closed on others.
    description: The store is reachable from part of the replicas, so the cause is on the path of the affected pods (a node, a network policy, DNS, an egress rule), not the store (R-STOR-033, R-HA-010). Compare the pods by the label pod on quader_storage_s3_circuit_open.
- alert: QuaderDatabasePoolSaturated
  expr: histogram_quantile(0.95, rate(quader_db_pool_acquire_duration_seconds_bucket{ {{ $ns }},db_pool="request" }[5m])) > 0.1
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: A replica waits more than 100 ms for a connection of its request pool (95th percentile).
    description: The request pool, or every server connection of the request pooler, is busy (R-HA-016). If the database CPU is idle while the wait grows, raise database.pool.max_conns within the connection budget; otherwise the database is the limit. quader_db_pool_canceled_acquires_total counts the waits that ended in 503 database_timeout.
- alert: QuaderControlConnectionWaits
  expr: histogram_quantile(0.95, rate(quader_db_pool_acquire_duration_seconds_bucket{ {{ $ns }},db_pool="control" }[5m])) > 1
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: A replica waits more than a second for its control connection (95th percentile).
    description: The loops that readiness and job ownership depend on (change-log poll, database check, registry heartbeat, job heartbeats) wait for the one control connection (Q-HA-20). If this goes on, the invalidation and database checks fail on every replica (R-OPS-046). Check the pooler endpoint of the control connection and the database latency.
- alert: QuaderJobsStuck
  expr: max(quader_jobs_oldest_queued_age_seconds{ {{ $ns }} }) > 900
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: A queued job has been waiting for more than 15 minutes.
    description: The queue is not being claimed or the workers are behind (R-ARCH-016). Check that worker pods are ready and claim the queue named by the label kind of the series, and the jobs list on System status. The series is read from PostgreSQL by every replica, so it keeps reporting while no worker runs.
- alert: QuaderLeaseLost
  expr: increase(quader_lease_lost_total{ {{ $ns }} }[15m]) > 3
  labels:
    severity: warning
  annotations:
    summary: The singleton task {{ "{{ $labels.task }}" }} lost its lease more than three times in 15 minutes.
    description: The worker that holds the lease loses its session connection to PostgreSQL repeatedly (R-HA-006, R-ARCH-017), so the task moves between replicas or stops. Look for a pooler in transaction mode on the session endpoint, idle-session timeouts and connection resets between the worker and the database.
- alert: QuaderMixedVersions
  expr: count(count by (version) (quader_build_info{ {{ $ns }} })) > 1
  for: 2h
  labels:
    severity: warning
  annotations:
    summary: Replicas of more than one Quader version have been running for two hours.
    description: A rolling upgrade is stuck, or a Deployment was left on an old image. Compare the label version of quader_build_info by pod and the image of each Deployment.
- alert: QuaderSpoolNearlyFull
  expr: max(quader_spool_used_bytes{ {{ $ns }} } / (quader_spool_used_bytes{ {{ $ns }} } + quader_spool_free_bytes{ {{ $ns }} })) > 0.9
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: The upload spool of a replica is more than 90 percent used.
    description: Running uploads hold more than nine times what the spool volume has left (R-STOR-004). New uploads are refused with 503 once the budget or the minimum free space is exceeded. Raise spool.sizeLimit of the role, or lower config.storage.spool.max_bytes; look for uploads that do not end.
- alert: QuaderVulnerabilityDataStale
  expr: time() - max by (source, ecosystem) (quader_vuln_source_last_success_timestamp_seconds{ {{ $ns }} }) > {{ $stale }}
  for: 15m
  labels:
    severity: warning
  annotations:
    summary: The advisory data of {{ "{{ $labels.source }}" }} {{ "{{ $labels.ecosystem }}" }} is older than vulnerabilities.stale_after.
    description: No sync run of this source and ecosystem ended successfully within vulnerabilities.stale_after. Enforcement and evaluation continue on the stored data (R-VULN-024). A source that never synced has the value 0 and fires too. Check the worker pods, the task vuln-sync-osv or vuln-sync-kev, and the egress to the source.
- alert: QuaderVulnerabilitySyncFailing
  expr: max by (source, ecosystem) (quader_vuln_source_consecutive_failures{ {{ $ns }} }) >= 3
  labels:
    severity: warning
  annotations:
    summary: Three consecutive sync runs of {{ "{{ $labels.source }}" }} {{ "{{ $labels.ecosystem }}" }} failed.
    description: The last error is in GET /api/v1/vulnerabilities/sources and in the audit event vulnerability.source.failed (R-VULN-024). The data is not refreshed until a run succeeds.
- alert: QuaderVulnerabilityRecordsRejected
  expr: sum by (source, ecosystem) (increase(quader_vuln_sync_records_total{ {{ $ns }},result="rejected" }[7d])) > 0
  labels:
    severity: warning
  annotations:
    summary: Advisory records of {{ "{{ $labels.source }}" }} {{ "{{ $labels.ecosystem }}" }} were rejected by a size bound within the last seven days.
    description: A record above a bound of one entry is not applied, and a record that was stored before keeps its older copy; the package it names may be missing from the data (R-VULN-004, Q-VULN-116). Records that fail validation are the result "invalid" and do not fire this alert. The worker logs each one at WARN with its entry, size and limit. If a legitimate record is too large, raise vulnerabilities.sources.osv.max_record_size.
- alert: QuaderVulnerabilityRecordsInvalid
  expr: |-
    sum by (source, ecosystem) (increase(quader_vuln_sync_records_total{ {{ $ns }},result="invalid" }[24h])) >= 10
    and
    sum by (source, ecosystem) (increase(quader_vuln_sync_records_total{ {{ $ns }},result="invalid" }[24h]))
    / sum by (source, ecosystem) (increase(quader_vuln_sync_records_total{ {{ $ns }},result=~"written|skipped|rejected|invalid" }[24h])) > 0.01
  labels:
    severity: warning
  annotations:
    summary: More than 1 percent of the advisory records of {{ "{{ $labels.source }}" }} {{ "{{ $labels.ecosystem }}" }} failed validation within a day.
    description: At least ten records, and more than one percent of those the sync processed in 24 hours, failed the rules of R-VULN-006 and were not applied; a record that was stored before keeps its older copy. A single record that fails is only counted and logged. Look at the worker log (message "an advisory record was rejected", attribute reason) for the first (R-VULN-004, Q-VULN-122).
- alert: QuaderVulnerabilityMaliciousRecordRejected
  expr: sum by (source, ecosystem) (increase(quader_vuln_sync_records_total{ {{ $ns }},result="rejected_malicious" }[7d])) > 0
  labels:
    severity: critical
  annotations:
    summary: A malicious-package advisory record of {{ "{{ $labels.source }}" }} {{ "{{ $labels.ecosystem }}" }} was rejected, by a bound or by validation, within the last seven days.
    description: The record named MAL-... is not applied, so the package it names is not flagged or blocked by it (R-VULN-004, Q-VULN-122). Find the entry in the worker log (message "an advisory record was rejected"); the cause is the bound of one entry (its size) or a rule of R-VULN-006 (it failed validation).
- alert: QuaderVulnerabilityClaimedNameMalicious
  expr: max(quader_vuln_claimed_name_matches{ {{ $ns }},kind="malicious" }) > 0
  labels:
    severity: critical
  annotations:
    summary: A malicious-package advisory names a package name that this installation treats as internal.
    description: Look at GET /api/v1/vulnerabilities/claimed-name-matches. A public package of that name exists, which is how a dependency-confusion attempt looks (R-VULN-010, R-VULN-024).
- alert: QuaderVulnerabilityImageScansFailing
  expr: sum(increase(quader_vuln_image_scans_total{ {{ $ns }},result!~"scanned|no_database" }[1h])) > 0 and (sum(increase(quader_vuln_image_scans_total{ {{ $ns }},result="scanned" }[1h])) or vector(0)) == 0
  for: 1h
  labels:
    severity: warning
  annotations:
    summary: Container images are being scanned without one scan finishing.
    description: Images whose scan does not finish show the evaluation scan_failed and are not reported as clean (R-VULN-034). The codes are the results of quader_vuln_image_scans_total; a repository page names the code per image in GET /api/v1/packages/{packageId}/versions/{version}/vulnerabilities.
- alert: QuaderVulnerabilityImageScannerUnavailable
  expr: sum(increase(quader_vuln_image_scans_total{ {{ $ns }},result=~"scanner_unavailable|registry_unavailable|pull_refused" }[30m])) > 0 and (sum(increase(quader_vuln_image_scans_total{ {{ $ns }},result="scanned" }[30m])) or vector(0)) == 0
  for: 15m
  labels:
    severity: warning
  annotations:
    summary: Image scans are put off because the scanner or its pull cannot work, and none finishes.
    description: For half an hour scans were put off for a reason of the installation, not of an image, and no scan finished (R-VULN-041, Q-VULN-162). A put-off scan spends no attempt, so it never shows as scan_failed. scanner_unavailable is no ready scanner pod or a refused handshake (the worker's certificate must allow client authentication); registry_unavailable is a scanner that cannot reach the scan listener of packages on 8082; pull_refused is a scan token the scan listener refused (a key ring that differs between the worker and packages, or a clock more than a minute off).
{{- if include "quader.malwareOn" . }}
- alert: QuaderMalwareFound
  expr: sum(increase(quader_malware_scans_total{ {{ $ns }},result="infected" }[1h])) > 0
  labels:
    severity: warning
  annotations:
    summary: A stored file was found to hold malware within the last hour.
    description: The security dashboard lists the file with the signature ClamAV named, and its downloads are refused (R-MAL-002, R-MAL-024). An auditor looks at it there and releases a false find; the audit event malware.found names the blob and its repository.
- alert: QuaderMalwareDatabaseStale
  expr: max(quader_malware_database_age_seconds{ {{ $ns }} }) > {{ $malwareStale }}
  for: 1h
  labels:
    severity: warning
  annotations:
    summary: The malware scan's signature database is older than malware.database.stale_after.
    description: Scanning goes on with the database in use, and malware published since it was built is not found (R-MAL-010, R-MAL-026). The settings page of the malware scanning names the last failure of the task malware-sync-db; check the freshclam container of the worker pods that claim the queue malware and its egress to database.clamav.net, or, without internet, deploy the files of cvdupdate to the repository malware.database.repository names (R-MAL-012). Before the first version is stored the age is 0 and the alert stays silent (H-1140).
- alert: QuaderMalwareScannerUnavailable
  expr: sum(increase(quader_malware_scans_total{ {{ $ns }},result=~"scanner_unavailable|clamd_unavailable" }[15m])) > 0 and (sum(increase(quader_malware_scans_total{ {{ $ns }},result=~"clean|infected|unscannable" }[15m])) or vector(0)) == 0
  for: 15m
  labels:
    severity: critical
  annotations:
    summary: Malware scans are put off because no malware scanner or its clamd answers, and none finishes.
    description: For a quarter of an hour every call of the job was put off for a reason of the installation and no verdict was made (R-MAL-005, R-MAL-026). New content stays pending, and where only checked artifacts can be downloaded it is refused after the wait. scanner_unavailable is no ready malware scanner pod or a refused handshake; clamd_unavailable is a clamd that does not answer beside a scanner. A malware scanner is not ready until it loaded a signature database (GET /readyz?verbose=1 of the pod says database).
- alert: QuaderMalwareBacklog
  expr: max(quader_malware_oldest_pending_seconds{ {{ $ns }} }) > 3600
  for: 15m
  labels:
    severity: warning
  annotations:
    summary: A stored file has waited more than an hour for its malware scan.
    description: The malware scanners do not keep up with what is stored, or calls are put off (R-MAL-004, R-MAL-026). quader_malware_backlog shows the due rows by priority; raise malwareScannerClamav.replicaCount or config.malware.scanner.slots, or look at the codes of quader_malware_scans_total.
{{- end }}
{{- if include "quader.signaturesOn" . }}
- alert: QuaderSignatureInvalid
  expr: sum(increase(quader_signatures_checks_total{ {{ $ns }},result="invalid" }[1h])) > 0
  labels:
    severity: warning
  annotations:
    summary: A signature on a stored artifact was found invalid within the last hour.
    description: The signature does not match the content it signs, or its chain or key does not verify it, so the artifact's badge says Invalid signature (R-SIG-003, R-SIG-039). The content was changed after signing, or the signature was made for other content or forged. The audit event signature.invalid names the signature, its subject and the repositories that hold it; quader_signatures_invalid counts the invalid signatures still attached to something.
- alert: QuaderSignatureBacklog
  expr: max(quader_signatures_oldest_pending_seconds{ {{ $ns }} }) > 3600
  for: 15m
  labels:
    severity: warning
  annotations:
    summary: A signature has waited more than an hour for its check.
    description: The job signature-verify does not keep up with what is stored, or does not run (R-SIG-011, R-SIG-039). quader_signatures_backlog shows the due signatures by priority. Check that a worker claims the queue signatures (jobs.workers.signatures), the object store, and quader_signatures_checks_total by result; raise config.signatures.concurrency if checks finish but too slowly.
- alert: QuaderSignatureTrustRootStale
  expr: max(quader_signatures_trust_root_age_seconds{ {{ $ns }} }) > 1209600
  for: 1h
  labels:
    severity: warning
  annotations:
    summary: The public Sigstore trusted root and npm's registry keys in use are older than 14 days.
    description: The task signature-trust-root has not stored a newer public root for 14 days, so certificate authorities, logs and keys that Sigstore and npm added since are not known, and their signatures read Signed · signer not trusted (R-SIG-017, R-SIG-039). With config.signatures.trust_root.refresh true, the admin page of the signatures and the audit event signature.trust_root.failed name the failure; check the worker's egress to config.signatures.trust_root.url. Without internet (refresh false) the age is that of the release's snapshot (H-1170); upgrade, point trust_root.url at a TUF mirror of the company, or disable the alert.
- alert: QuaderSignatureGatePending
  expr: sum(increase(quader_signatures_download_wait_seconds_count{ {{ $ns }},outcome="pending" }[15m])) > 0
  for: 15m
  labels:
    severity: warning
  annotations:
    summary: Clients of a repository that serves only verified artifacts get 503 because the signature checks do not keep up.
    description: Downloads from repositories that serve only artifacts with a verified signature waited for the checks of their signatures and got 503 signature_check_pending, because the checks or the background fetch of a remote's signatures did not decide within config.signatures.gate.download_wait (R-SIG-067, R-SIG-075). Check that a worker claims the queues signatures and index (jobs.workers), the alert QuaderSignatureBacklog, quader_signatures_backlog and the upstream of the remote; Docker and ansible-galaxy do not retry the 503 themselves (H-1200).
{{- end }}
{{- if include "quader.signingOn" . }}
- alert: QuaderSigningIdentityExpiring
  expr: min(quader_signing_identity_not_after_seconds{ {{ $ns }},state="current" }) - time() < 14 * 86400 unless on() count(quader_signing_identity_not_after_seconds{ {{ $ns }},state="announced" }) > 0
  for: 1h
  labels:
    severity: warning
  annotations:
    summary: The certificate of Quader's signing identity ends within 14 days and no successor is announced.
    description: When it ends, Quader signs nothing and its attestations stop verifying (R-SIG-045, R-SIG-057). With the Helm values, check that cert-manager renews the Certificate <fullname>-signing (kubectl describe certificate) and that its issuer still issues code signing; a renewal is announced within config.signing.identity.reload_interval. With an uploaded identity, an administrator creates a new key in Admin area › Settings › Signatures and uploads the certificate the CA issues for it (docs/manual/admin/runbook.md).
- alert: QuaderSigningIdentityExpired
  expr: min(quader_signing_identity_not_after_seconds{ {{ $ns }},state="current" }) - time() < 0
  labels:
    severity: critical
  annotations:
    summary: The certificate of Quader's signing identity has ended; Quader signs nothing.
    description: Local npm versions published from now on carry no registry signature and npm audit signatures reports them missing; no attestation is made, and those made with this certificate stop verifying (R-SIG-045, R-SIG-057). Install a new identity at once (the Helm values or Admin area › Settings › Signatures); the job signs what waited (docs/manual/admin/runbook.md).
- alert: QuaderSigningIdentityNotLoaded
  expr: max(quader_signing_identity_loaded{ {{ $ns }} }) == 0 and on() count(quader_signing_identity_not_after_seconds{ {{ $ns }},state="current" }) > 0
  for: 15m
  labels:
    severity: warning
  annotations:
    summary: No worker holds the key of the current signing identity.
    description: An identity is current, but no worker could load its key, so the job signing leaves its work due (R-SIG-049, R-SIG-057). For the Helm values, the worker pods mount the Secret at /var/run/secrets/quader/signing/ and the admin page shows a refused pair of the files with its reason; for an uploaded identity, the key ring of the workers must open signing_identities.sealed_key (quader admin secrets status). A worker started during an announce period holds only the new key until the switch.
- alert: QuaderSigningBacklog
  expr: max(quader_signing_oldest_due_seconds{ {{ $ns }},kind="npm" }) > 300
  for: 15m
  labels:
    severity: warning
  annotations:
    summary: A local npm version has waited more than five minutes for Quader's registry signature.
    description: The job signing does not keep up or does not run (R-SIG-049, R-SIG-057); until a version is signed, npm audit signatures reports it missing. Check that a worker claims the queue signing (jobs.workers.signing), quader_signing_identity_loaded and quader_signing_operations_total by result; raise config.signing.concurrency if operations finish but too slowly.
{{- end }}
- alert: QuaderMirrorFailing
  expr: max by (repo) (quader_mirror_consecutive_failures{ {{ $ns }} }) >= 3
  labels:
    severity: warning
  annotations:
    summary: The mirror {{ "{{ $labels.repo }}" }} has failed three pass attempts in a row.
    description: The last error is in GET /api/v1/projects/{projectKey}/repositories/{repoKey}/mirror and in the audit event mirror.pass_failed (R-REPO-053, R-REPO-057). The pass in progress stays, and the dispatch continues it after the back-off; a complete pass resets the count. A paused mirror is not reported.
- alert: QuaderMirrorStale
  expr: time() - max by (repo) (quader_mirror_next_pass_timestamp_seconds{ {{ $ns }} }) > 3600
  for: 30m
  labels:
    severity: warning
  annotations:
    summary: The mirror {{ "{{ $labels.repo }}" }} is more than an hour behind its schedule.
    description: Its next pass was due an hour ago and nothing started it, or the pass in progress has not written for an hour. No process claims the queue mirror, the passes are held back by a rate limit that lasts hours, or the job is lost (R-REPO-053, R-REPO-054). Check the pods that claim the queue, the status of the repository and quader_mirror_pending_packages. A paused mirror is not reported.
- alert: QuaderBlobMissing
  expr: sum(increase(quader_storage_blobs_damaged_total{ {{ $ns }},status="missing" }[1h])) > 0
  labels:
    severity: critical
  annotations:
    summary: The object store has lost the object of at least one blob within the last hour.
    description: A read found no object behind a blob row and marked the blob missing (R-HA-024, R-STOR-027). The replicas log every such blob with its digest ("the object of a blob is missing from the object store") and every cached item of a remote repository with its repository and path. A remote repository fills its items again from the upstream; a file of a local repository answers 503 content_unavailable until its object is restored or the same content is uploaded again. Check the bucket, its lifecycle rules and who else writes to it.
- alert: QuaderGCStale
  expr: time() - max(quader_gc_last_success_timestamp_seconds{ {{ $ns }} }) > 172800
  for: 1h
  labels:
    severity: warning
  annotations:
    summary: No garbage-collection run has ended successfully for more than two days.
    description: The tasks gc and gc-full do not finish (R-STOR-025). Blobs that nothing references stay in the object store until a run succeeds. The series exists after the first successful run of a worker process, so a worker that never completed a run reports nothing. Check the jobs of the task and the object store.
- alert: QuaderBackupCheckFailed
  expr: min(quader_backup_check_ok{ {{ $ns }} }) == 0
  for: 1h
  labels:
    severity: critical
  annotations:
    summary: A platform protection that the installation declares does not hold.
    description: The task backup-check (R-HA-023) found that the point-in-time recovery of PostgreSQL or the versioning of the blob bucket that backup.recovery_window and backup.object_versioning declare is missing, or that the grace period of the garbage collection does not cover the window. The label check names it (archive_mode, object_versioning, noncurrent_expiry, grace_period); quader admin backup check prints the reason. A check the credential may not read, or that is not declared, has no series and does not fire.
- alert: QuaderRevocationJournalBacklog
  expr: max(quader_revocation_journal_pending{ {{ $ns }} }) > 0
  for: 15m
  labels:
    severity: warning
  annotations:
    summary: Entries of the revocation journal have waited for shipping to the blob bucket for 15 minutes.
    description: The outbox grows while the object store is out and ships afterwards, so a backlog of 15 minutes means the shipper does not write the _journal/ prefix of the blob bucket, or writes slower than entries arrive (R-HA-054). A restore taken now would replay a journal that lacks those entries, and a revocation among them would not be applied again. Check that worker pods are ready, the object store alerts, the shipper's log records and the write access of the credential to the prefix.
- alert: QuaderRevocationJournalQuarantined
  expr: sum(increase(quader_revocation_journal_quarantined_total{ {{ $ns }} }[1h])) > 0
  labels:
    severity: critical
  annotations:
    summary: A change could not be written to the revocation journal and was set aside.
    description: An outbox row that cannot be encoded is moved to system_state.journal_quarantined so that it does not block the shipping of the rows behind it (R-HA-054). Any increase means that a change, possibly a revocation, is missing from the journal, and a restore from a backup or a platform restore would not apply it again. quader admin post-restore prints the quarantined rows; read the shipper's log records for the row and its error, repair or re-create the change, and take a new backup.
- alert: QuaderValkeyUnavailable
  expr: max(quader_valkey_available{ {{ $ns }} }) == 0
  for: 2m
  labels:
    severity: critical
  annotations:
    summary: Every replica runs without Valkey.
    description: Every replica reports its valkey check degraded (R-HA-057, INV-12). Rate limits fall back to per-replica shares, flights run without the cross-replica lock and cache invalidation relies on the change-log poll. A failover of the primary lasts well below two minutes; check the Sentinels, the primary and the credentials.
- alert: QuaderValkeyUnavailableSingleReplica
  expr: min(quader_valkey_available{ {{ $ns }} }) == 0 and max(quader_valkey_available{ {{ $ns }} }) == 1
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: Some replicas have lost Valkey while others have it.
    description: The cause is on the path of the affected pods, not Valkey (a node, a network policy, DNS) (R-HA-057). Compare the pods by the label pod on quader_valkey_available and read the reason of the valkey check in GET /api/v1/system/replicas.
- alert: QuaderValkeySentinelsDegraded
  expr: min(quader_valkey_sentinels_reachable{ {{ $ns }} }) < 2
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: A replica reaches fewer than two Sentinels.
    description: Without a majority of three Sentinels no failover can be elected (R-HA-056). The gauge counts the answers to a probe of every configured Sentinel (R-HA-057). Check the Sentinel pods and the path from the replica to each of them.
- alert: QuaderValkeyFailoverFlapping
  expr: max(max_over_time(quader_valkey_primary_changes_total{ {{ $ns }} }[1h]) - min_over_time(quader_valkey_primary_changes_total{ {{ $ns }} }[1h])) > 2
  labels:
    severity: warning
  annotations:
    summary: The Valkey primary changed more than twice within an hour.
    description: Failovers repeat (Q-OPS-15). Look at the Valkey pods (restarts, memory, the node), the Sentinels' down-after-milliseconds against the network latency, and the log records "valkey primary changed".
- alert: QuaderValkeyPrimaryDisagreement
  expr: max(quader_valkey_primary_views{ {{ $ns }} }) > 1
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: Replicas name different Valkey primaries.
    description: At least one replica is bound to a demoted primary (Q-OPS-15, R-ARCH-031), so locks, rate limits and cache signals are split between two servers. The registry in GET /api/v1/system/replicas shows each replica's primary; a restart of the replica that names the old one rebinds it.
{{- if .Values.backup.enabled }}
- alert: QuaderBackupStale
  expr: time() - max(quader_backup_last_success_timestamp_seconds{ {{ $ns }} }) > 1.5 * max(quader_backup_expected_interval_seconds{ {{ $ns }} }) or (max(absent(quader_backup_last_success_timestamp_seconds{ {{ $ns }} })) and on() max(quader_backup_expected_interval_seconds{ {{ $ns }} } offset {{ $interval }}s))
  labels:
    severity: critical
  annotations:
    summary: The last successful backup is older than one and a half times backup.expected_interval, or none has succeeded in a whole interval.
    description: No create run has ended successfully within 1.5 times config.backup.expected_interval, the condition of the overdue mark in the API and the UI (R-BAK-014). On an installation whose first backup never succeeded the alert fires one expected interval after the replicas first reported. Look at GET /api/v1/system/backups, the CronJob (suspended? missed its starting deadline?) and its Jobs, and the log of the last pod.
- alert: QuaderBackupFailed
  expr: max(quader_backup_last_failure_timestamp_seconds{ {{ $ns }} }) > (max(quader_backup_last_success_timestamp_seconds{ {{ $ns }} }) or vector(0))
  labels:
    severity: critical
  annotations:
    summary: The newest backup run failed and no backup has succeeded since.
    description: A create run ended failed or abandoned, or its lease expired in an active state, after the last success (R-BAK-014). A run interrupted by a drain does not count. The alert resolves with the next success. The cause is in GET /api/v1/system/backups (error code) and the log of the Job's pod; the exit codes are in R-BAK-013.
- alert: QuaderBackupRunningLong
  expr: max(quader_backup_running_seconds{ {{ $ns }} }) > 0.5 * max(quader_backup_expected_interval_seconds{ {{ $ns }} })
  labels:
    severity: warning
  annotations:
    summary: A backup or delete run has been active for more than half of backup.expected_interval.
    description: The run holds the slot of the target and pins blobs against garbage collection while it lasts (R-BAK-007, R-BAK-010). A first backup of a large installation takes long; a run that makes no progress ends itself after config.backup.stall_timeout. Look at the progress in GET /api/v1/system/backups and the log of the pod.
- alert: QuaderBackupGlobalHoldLong
  expr: max(quader_backup_gc_hold{ {{ $ns }},mode="all" }) == 1
  for: 30m
  labels:
    severity: warning
  annotations:
    summary: A backup has held garbage collection for all blobs for 30 minutes.
    description: The hold mode all stops the collection of every blob while a backup run opens its snapshot or writes its pins (R-BAK-007). It is meant to last minutes. Look at the active run in GET /api/v1/system/backups; a run that is stuck ends itself after config.backup.stall_timeout.
- alert: QuaderBackupSourceDamage
  expr: max(quader_backup_source_blobs_damaged{ {{ $ns }} }) > 0
  labels:
    severity: critical
  annotations:
    summary: The newest successful backup found blobs of the installation damaged or missing.
    description: The backup could not copy the objects of these blobs correctly (R-BAK-009, R-BAK-014), so a restore of it lacks them. The list is in the result of the run (GET /api/v1/system/backups) and the log. Restore the objects from the bucket's versions or upload the content again, then take a new backup.
- alert: QuaderBackupVerifyFindings
  expr: max(quader_backup_last_verify_findings{ {{ $ns }} }) > 0
  labels:
    severity: critical
  annotations:
    summary: The newest verification of a backup found missing, damaged or mismatching objects.
    description: quader backup verify exited 5 (R-BAK-019, R-BAK-014). The findings are in its output and the run record. A backup with findings may not restore; take a new one and look at the target (lifecycle rules, deletions, a credential that can delete).
{{- end }}
{{- end -}}

{{/* The names of every alert the chart can render, for the check of disabledAlerts. */}}
{{- define "quader.rules.names" -}}
{{- toJson (list "QuaderReplicasNotReady" "QuaderDatabaseUnreachable" "QuaderObjectStoreUnreachable" "QuaderObjectStoreUnreachableSingleReplica" "QuaderDatabasePoolSaturated" "QuaderControlConnectionWaits" "QuaderJobsStuck" "QuaderLeaseLost" "QuaderMixedVersions" "QuaderSpoolNearlyFull" "QuaderVulnerabilityDataStale" "QuaderVulnerabilitySyncFailing" "QuaderVulnerabilityRecordsRejected" "QuaderVulnerabilityRecordsInvalid" "QuaderVulnerabilityMaliciousRecordRejected" "QuaderVulnerabilityClaimedNameMalicious" "QuaderVulnerabilityImageScansFailing" "QuaderVulnerabilityImageScannerUnavailable" "QuaderMalwareFound" "QuaderMalwareDatabaseStale" "QuaderMalwareScannerUnavailable" "QuaderMalwareBacklog" "QuaderSignatureInvalid" "QuaderSignatureBacklog" "QuaderSignatureTrustRootStale" "QuaderSignatureGatePending" "QuaderSigningIdentityExpiring" "QuaderSigningIdentityExpired" "QuaderSigningIdentityNotLoaded" "QuaderSigningBacklog" "QuaderMirrorFailing" "QuaderMirrorStale" "QuaderBlobMissing" "QuaderGCStale" "QuaderBackupCheckFailed" "QuaderRevocationJournalBacklog" "QuaderRevocationJournalQuarantined" "QuaderValkeyUnavailable" "QuaderValkeyUnavailableSingleReplica" "QuaderValkeySentinelsDegraded" "QuaderValkeyFailoverFlapping" "QuaderValkeyPrimaryDisagreement" "QuaderBackupStale" "QuaderBackupFailed" "QuaderBackupRunningLong" "QuaderBackupGlobalHoldLong" "QuaderBackupSourceDamage" "QuaderBackupVerifyFindings") -}}
{{- end -}}
