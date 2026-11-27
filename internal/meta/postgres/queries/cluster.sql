-- Queries of the change log (R-ARCH-018) and the replica registry (R-OPS-003).

-- The change log is written through meta.Tx (cluster.Changes.Notify): the two
-- statements of R-ARCH-018 step 1 run on the writer's transaction.

-- name: ListClusterEpochs :many
SELECT area, epoch, changed_at FROM cluster_epoch WHERE area = ANY(sqlc.arg(areas)::text[]);

-- name: ListClusterChanges :many
SELECT epoch, payload, created_at FROM cluster_change
 WHERE area = sqlc.arg(area)::text AND epoch >= sqlc.arg(from_epoch)::bigint AND epoch <= sqlc.arg(to_epoch)::bigint
 ORDER BY epoch;

-- name: PruneClusterChanges :execrows
-- cluster-change-prune (R-ARCH-016): a row goes once the next row of its area is
-- older than the retention, so the newest row of every area stays. "Next" is the
-- row with the next higher epoch, whether or not the epochs are contiguous.
DELETE FROM cluster_change c
 USING (SELECT area, epoch, lead(created_at) OVER (PARTITION BY area ORDER BY epoch) AS next_created_at
          FROM cluster_change) n
 WHERE n.area = c.area AND n.epoch = c.epoch
   AND n.next_created_at < now() - make_interval(secs => sqlc.arg(retention_secs)::float8);

-- name: UpsertReplica :exec
-- Registration and heartbeat in one statement: a row deleted by the prune or by
-- a restore comes back with the next heartbeat. heartbeat_at is the database's
-- clock; replica clocks are never compared (R-HA-003). runtime is the replica's
-- measurement of the period since its previous heartbeat (Q-OPS-175). draining is the last state of an instance: a heartbeat
-- that was under way when the drain marked the row does not turn it back (Q-HA-308).
INSERT INTO replica (instance_uid, name, version, revision, roles, state, checks, leases,
                     secret_key_ids, active_secret_key_id, node, valkey, pool, runtime, object_store, started_at, heartbeat_at)
VALUES (sqlc.arg(instance_uid)::text::uuid, sqlc.arg(name)::text, sqlc.arg(version)::text, sqlc.arg(revision)::text,
        sqlc.arg(roles)::text[], sqlc.arg(state)::text, sqlc.arg(checks)::text::jsonb, sqlc.arg(leases)::text::jsonb,
        sqlc.arg(secret_key_ids)::text[], sqlc.arg(active_secret_key_id)::text,
        NULLIF(sqlc.arg(node)::text, ''), sqlc.arg(valkey)::text::jsonb, NULLIF(sqlc.arg(pool)::text, ''),
        sqlc.arg(runtime)::text::jsonb, sqlc.arg(object_store)::text::jsonb, sqlc.arg(started_at)::timestamptz, now())
ON CONFLICT (instance_uid) DO UPDATE
   SET state = CASE WHEN replica.state = 'draining' THEN replica.state ELSE EXCLUDED.state END, checks = EXCLUDED.checks, leases = EXCLUDED.leases,
       secret_key_ids = EXCLUDED.secret_key_ids, active_secret_key_id = EXCLUDED.active_secret_key_id,
       valkey = EXCLUDED.valkey, runtime = EXCLUDED.runtime, object_store = EXCLUDED.object_store, heartbeat_at = now();

-- name: SetReplicaState :execrows
UPDATE replica SET state = sqlc.arg(state)::text, heartbeat_at = now()
 WHERE instance_uid = sqlc.arg(instance_uid)::text::uuid;

-- name: DeleteReplica :exec
DELETE FROM replica WHERE instance_uid = sqlc.arg(instance_uid)::text::uuid;

-- name: ListReplicas :many
-- A row whose heartbeat went stale while it was draining has signed off (R-OPS-003, Q-OPS-210): its process was stopped on
-- purpose and ended without deleting the row, killed inside its drain or with the deletion cut by the close phase. It is not
-- listed, so no reader counts it as a live or a lost replica; the prune deletes it. Every other stale row is lost.
SELECT r.*, (r.heartbeat_at < now() - make_interval(secs => sqlc.arg(stale_secs)::float8))::bool AS lost
  FROM replica r
 WHERE NOT (r.state = 'draining' AND r.heartbeat_at < now() - make_interval(secs => sqlc.arg(stale_secs)::float8))
 ORDER BY r.roles, r.name;

-- name: PruneReplicas :execrows
-- replica-registry-prune (R-OPS-003): rows stale for stale_secs (a lost replica), and rows that went stale for
-- signed_off_secs while draining (a replica that signed off, Q-OPS-210).
DELETE FROM replica
 WHERE heartbeat_at < now() - make_interval(secs => sqlc.arg(stale_secs)::float8)
    OR (state = 'draining' AND heartbeat_at < now() - make_interval(secs => sqlc.arg(signed_off_secs)::float8));

-- name: CountFillingReplicas :one
-- The n of the fetch share of a remote repository (R-REPO-040, Q-ARCH-297): the live replicas that fill client misses,
-- which are the replicas of packages, their pools included, and of the development roles web and all. A replica of
-- api, which sends the upstream test and nothing else, and of worker holds no share.
SELECT count(*)::int AS live
  FROM replica
 WHERE heartbeat_at >= now() - make_interval(secs => sqlc.arg(stale_secs)::float8)
   AND roles && ARRAY['packages', 'web', 'all']::text[];

-- name: ValkeyPrimaryViews :one
-- quader_valkey_primary_views (R-OPS-007, R-HA-045): the distinct primaries the live replicas name in their registry rows.
-- A replica without a primary (no Valkey, or not yet connected) names none and counts for nothing.
SELECT count(DISTINCT valkey ->> 'primary')::bigint AS views
  FROM replica
 WHERE heartbeat_at > now() - make_interval(secs => sqlc.arg(stale_secs)::float8);

-- The scanner replicas (R-OPS-048, Q-OPS-223): a scanner reports itself on the scan listener, and packages writes its row. kind is
-- the kind the client certificate named; heartbeat_at, started_at and state_since are the database's clock.

-- name: RecordScannerReplica :execrows
-- A report in one statement, as the registry's heartbeat is: a new row only while its kind has fewer than max_rows live rows (the
-- bound against a scanner that invents replicas, H-1161), and never over a row of another kind. draining is the last state of an
-- instance, as in the registry (Q-HA-308); state_since moves when the state or its reason changes. calls are a malware scanner's calls in
-- flight as JSON ('' for none, Q-OPS-460). No row affected: refused.
INSERT INTO scanner_replica (instance_uid, kind, name, node, version, revision, state, reason, state_since, tool_version, db_version,
                             db_built_at, slots_in_use, slots_total, calls, started_at, heartbeat_at)
SELECT sqlc.arg(instance_uid)::text::uuid, sqlc.arg(kind)::text, sqlc.arg(name)::text, NULLIF(sqlc.arg(node)::text, ''),
       sqlc.arg(version)::text, sqlc.arg(revision)::text, sqlc.arg(state)::text, NULLIF(sqlc.arg(reason)::text, ''), now(),
       NULLIF(sqlc.arg(tool_version)::text, ''), NULLIF(sqlc.arg(db_version)::text, ''),
       NULLIF(sqlc.arg(db_built_at)::text, '')::timestamptz, sqlc.arg(slots_in_use)::int, sqlc.arg(slots_total)::int,
       NULLIF(sqlc.arg(calls)::text, '')::jsonb, now(), now()
 WHERE EXISTS (SELECT 1 FROM scanner_replica e WHERE e.instance_uid = sqlc.arg(instance_uid)::text::uuid)
    OR (SELECT count(*) FROM scanner_replica s
         WHERE s.kind = sqlc.arg(kind)::text
           AND s.heartbeat_at >= now() - make_interval(secs => sqlc.arg(stale_secs)::float8)) < sqlc.arg(max_rows)::int
ON CONFLICT (instance_uid) DO UPDATE
   SET name = EXCLUDED.name, node = EXCLUDED.node, version = EXCLUDED.version, revision = EXCLUDED.revision,
       state = CASE WHEN scanner_replica.state = 'draining' THEN scanner_replica.state ELSE EXCLUDED.state END,
       reason = CASE WHEN scanner_replica.state = 'draining' THEN scanner_replica.reason ELSE EXCLUDED.reason END,
       state_since = CASE WHEN scanner_replica.state = 'draining'
                            OR (scanner_replica.state = EXCLUDED.state AND scanner_replica.reason IS NOT DISTINCT FROM EXCLUDED.reason)
                          THEN scanner_replica.state_since ELSE now() END,
       tool_version = EXCLUDED.tool_version, db_version = EXCLUDED.db_version, db_built_at = EXCLUDED.db_built_at,
       slots_in_use = EXCLUDED.slots_in_use, slots_total = EXCLUDED.slots_total, calls = EXCLUDED.calls, heartbeat_at = now()
 WHERE scanner_replica.kind = EXCLUDED.kind;

-- name: DeleteScannerReplica :execrows
-- A scanner's sign-off at its clean shutdown: its own row, of the kind its certificate names.
DELETE FROM scanner_replica WHERE instance_uid = sqlc.arg(instance_uid)::text::uuid AND kind = sqlc.arg(kind)::text;

-- name: ListScannerReplicas :many
-- The rows as GET /api/v1/system/scanners lists them, with the rule of the registry's list (Q-OPS-210): a row that went stale while
-- draining has signed off and is not listed, every other stale row is lost. db_built_at is the build time the updater reported of the set
-- its directory holds (the kind freshclam); for an image scanner the build time it reported of the database its pod holds, while that is
-- the database in use (the source trivy's digest, Q-OPS-225); for a malware scanner the build time of the signature database in use when
-- the row names that version. calls are a malware scanner's calls in flight (Q-OPS-460), null for the other kinds.
SELECT s.instance_uid, s.kind, s.name, s.node, s.version, s.revision, s.state, s.reason, s.state_since, s.tool_version, s.db_version,
       s.slots_in_use, s.slots_total, s.calls, s.started_at, s.heartbeat_at,
       (s.heartbeat_at < now() - make_interval(secs => sqlc.arg(stale_secs)::float8))::bool AS lost,
       (CASE WHEN s.kind = 'trivy' THEN
                  (CASE WHEN s.db_version = (SELECT substr(v.etag, 8) FROM vuln_source_state v
                                              WHERE v.source = 'trivy' AND v.ecosystem = 'OCI' AND v.state = 'complete'
                                                AND v.etag LIKE 'sha256:%') THEN s.db_built_at END)
             ELSE COALESCE(s.db_built_at,
                           (SELECT d.built_at FROM malware_database d WHERE d.version IS NOT NULL AND d.version = s.db_version))
        END)::timestamptz AS db_built_at
  FROM scanner_replica s
 WHERE NOT (s.state = 'draining' AND s.heartbeat_at < now() - make_interval(secs => sqlc.arg(stale_secs)::float8))
 ORDER BY s.kind, s.name, s.instance_uid;

-- name: PruneScannerReplicas :execrows
-- replica-registry-prune for the scanner replicas, by the registry's rule (R-OPS-003, Q-OPS-210).
DELETE FROM scanner_replica
 WHERE heartbeat_at < now() - make_interval(secs => sqlc.arg(stale_secs)::float8)
    OR (state = 'draining' AND heartbeat_at < now() - make_interval(secs => sqlc.arg(signed_off_secs)::float8));
