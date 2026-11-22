-- Queries of `quader backup restore` (R-BAK-020 to R-BAK-023): the schema inspection of check 4, the column and sequence
-- lists the load compares with the manifest, the marker, and the fix-ups of R-BAK-022. They run on the restore's session
-- connection as the schema owner. The statements that name a table by a value taken from the catalog or the registry
-- (TRUNCATE, COPY, the revision columns) are assembled in backup_load.go and backup_restore_state.go, the two files the
-- architecture test lists next to blobrefs.go.

-- name: RestoreSchemaRelations :many
-- The relations of the schema (tables, partitioned tables, views, sequences, foreign tables): check 4 wants none. The first
-- ten names go into the message; total is the count.
SELECT c.relname::text AS name, (count(*) OVER ())::bigint AS total
  FROM pg_catalog.pg_class c
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname = sqlc.arg(schema)::text AND c.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
 ORDER BY c.relname
 LIMIT 10;

-- name: RestoreTableColumns :many
-- The columns the load writes, per table, in column order: stored generated columns are left out, because COPY FROM cannot
-- write them; identity columns are in (R-BAK-004). The type is format_type, the form the manifest records.
SELECT c.relname::text AS table_name, a.attname::text AS column_name,
       pg_catalog.format_type(a.atttypid, a.atttypmod)::text AS column_type
  FROM pg_catalog.pg_class c
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
  JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid
 WHERE n.nspname = sqlc.arg(schema)::text
   AND c.relkind IN ('r', 'p') AND NOT c.relispartition
   AND a.attnum > 0 AND NOT a.attisdropped AND a.attgenerated = ''
 ORDER BY c.relname, a.attnum;

-- name: RestoreSequences :many
SELECT c.relname::text AS name
  FROM pg_catalog.pg_class c
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname = sqlc.arg(schema)::text AND c.relkind = 'S'
 ORDER BY c.relname;

-- name: RestoreSetSequence :one
-- setval of one sequence from the manifest (R-BAK-021 step 4). qualified is the quoted schema.name.
SELECT pg_catalog.setval(sqlc.arg(qualified)::text::regclass, sqlc.arg(last_value)::bigint, sqlc.arg(is_called)::boolean)::bigint AS value;

-- name: InsertRestoreMarker :exec
-- restore_in_progress, committed by the last step of the restore's first migration and again by the load. An existing row
-- stays: a resumed restore keeps its phase.
INSERT INTO system_state (key, value)
VALUES ('restore_in_progress', sqlc.arg(value)::text::jsonb)
ON CONFLICT (key) DO NOTHING;

-- name: SetRestorePhase :execrows
-- A phase change of the marker, fenced on the phase the caller expects, so a step never runs on a marker of another phase.
UPDATE system_state
   SET value = jsonb_set(value, '{phase}', to_jsonb(sqlc.arg(phase)::text)), updated_at = now()
 WHERE key = 'restore_in_progress' AND value ->> 'phase' = sqlc.arg(from_phase)::text;

-- name: DeleteRestoreMarker :execrows
DELETE FROM system_state WHERE key = 'restore_in_progress';

-- name: RestoreSetSystemState :exec
INSERT INTO system_state (key, value)
VALUES (sqlc.arg(key)::text, sqlc.arg(value)::text::jsonb)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();

-- name: RestoreSetRestoredAt :exec
-- restored_at is the transaction's time (R-BAK-022): the single-use CI issuers and the npm remote validation compare with it.
INSERT INTO system_state (key, value)
VALUES ('restored_at', to_jsonb(now()))
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();

-- name: RestoreResetJobs :execrows
-- Rows in running return to queued with the lease cleared and attempt unchanged; rows in cancelling become cancelled, because
-- their cancellation was requested before the snapshot (R-BAK-022). A job that would wait next to another waiting job of its
-- deduplication key ends cancelled, superseded, as the reaper does (R-ARCH-016), so the unique index of waiting jobs holds.
-- Scheduled slots keep their rows, so no slot runs twice.
WITH reset AS (
    SELECT j.id,
           CASE WHEN j.state = 'cancelling' THEN 'cancelled'
                WHEN j.dedup_key IS NOT NULL
                     AND (EXISTS (SELECT 1 FROM job o WHERE o.dedup_key = j.dedup_key AND o.state = 'queued')
                          OR row_number() OVER (PARTITION BY j.dedup_key ORDER BY (j.state = 'cancelling'), j.created_at, j.id) > 1)
                     THEN 'superseded'
                ELSE 'queued' END AS outcome
      FROM job j
     WHERE j.state IN ('running', 'cancelling')
)
UPDATE job
   SET state = CASE reset.outcome WHEN 'queued' THEN 'queued'::job_state ELSE 'cancelled'::job_state END,
       error = CASE reset.outcome WHEN 'superseded' THEN '{"code":"superseded"}'::jsonb
                                  WHEN 'cancelled' THEN '{"code":"cancelled"}'::jsonb ELSE job.error END,
       locked_by = NULL, locked_until = NULL,
       finished_at = CASE reset.outcome WHEN 'queued' THEN NULL ELSE now() END
  FROM reset
 WHERE job.id = reset.id;

-- name: RestoreRecountStorageUsage :exec
-- The fix-up of R-STOR-047: the counters are recomputed from items and blobs and the deltas are emptied.
SELECT storage_usage_recount();

-- name: RestoreResetMirrorState :execrows
-- The pass in progress, the requests and the rate-limit wait end, the bound is forgotten and the first pass is due at once; it
-- continues from the cursor of the last complete pass. A restore without remote caches also forgets that cursor, so the first
-- pass is full (R-BAK-022, R-REPO-058).
UPDATE mirror_state
   SET pass_started_at = NULL, pass_kind = NULL, pass_method = NULL, pass_job_id = NULL, pass_holder = NULL,
       pass_cursor = NULL, pass_discovery_done = false, pass_names = 0, pass_packages_done = 0, pass_packages_failed = 0,
       pass_items_stored = 0, pass_bytes_stored = 0, pass_discovery_complete = true, pass_discovery_note = NULL,
       pass_requested_at = NULL, pass_requested_by = NULL, pass_requested_full = false,
       rate_limited_until = NULL, bound_reached = NULL, bound_reached_at = NULL,
       feed_cursor = CASE WHEN sqlc.arg(forget_cursor)::boolean THEN NULL ELSE feed_cursor END,
       last_full_pass_at = CASE WHEN sqlc.arg(forget_cursor)::boolean THEN NULL ELSE last_full_pass_at END,
       patterns_fingerprint = CASE WHEN sqlc.arg(forget_cursor)::boolean THEN NULL ELSE patterns_fingerprint END,
       next_pass_at = sqlc.arg(now)::text::timestamptz, updated_at = now();

-- name: RestoreMarkDamagedBlobs :execrows
-- The blobs the backup recorded as damaged in the source are restored as damaged (R-BAK-006, R-BAK-021 step 4).
UPDATE blobs SET content_status = sqlc.arg(status)::text
 WHERE sha256 = ANY(sqlc.arg(digests)::bytea[]);

-- name: RestoreCreateAuditPartitions :exec
-- The owner's companion of R-OPS-015: every missing monthly partition that overlaps the range, nothing dropped.
SELECT audit_create_partitions(sqlc.arg(range_from)::timestamptz, sqlc.arg(range_to)::timestamptz);

-- name: RestoreSecretCanaries :many
SELECT key_id, ciphertext FROM secret_key_canary ORDER BY key_id;

-- name: RestoreTablePrimaryKey :many
-- The primary-key columns of a table, in key order: the join of the temporary table that fills a CycleBreak column.
SELECT a.attname::text AS column_name
  FROM pg_catalog.pg_index i
  JOIN pg_catalog.pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY (i.indkey)
 WHERE i.indrelid = to_regclass(sqlc.arg(qualified)::text) AND i.indisprimary
 ORDER BY array_position(i.indkey::int2[], a.attnum);

-- name: RestoreEnsureAuditPartitions :exec
-- The partitions for now and the next three months, on the database's clock: the events of the fix-ups, the replay and the
-- finish need one, and a backup of an earlier month brought none.
SELECT audit_create_partitions(now(), now() + interval '3 months');

-- name: RestoreRecordConnection :exec
-- A connection of the restoring installation's configuration (R-BAK-030): storage_connections is transient and comes back
-- empty, and the fix-up records each connection with the bucket and the prefix the configuration resolves to, so the first
-- replica finds the fixed properties it configures (storage.md R-STOR-051).
INSERT INTO storage_connections (id, bucket, prefix)
VALUES (sqlc.arg(id)::text, sqlc.arg(bucket)::text, sqlc.arg(prefix)::text)
ON CONFLICT (id) DO NOTHING;

-- name: RestoreRecordCopies :execrows
-- A copy of every restored live blob on the connection step 3 wrote into (R-BAK-030): present for a blob whose object it wrote,
-- missing for one the backup listed as damaged, which has no object (a read's recheck heals it when the object appears);
-- verified_at the restore's time when the restore streamed and hashed every blob, NULL otherwise, which the scrub reads first
-- (Q-BAK-72). The name is the connection's naming function of stage 1, <prefix>/sha256/<h0h1>/<h2h3>/<hex> (R-STOR-001). No
-- queue row: the reconciliation's pass finds the other connections' gaps (R-STOR-056).
INSERT INTO blob_copies (sha256, connection_id, object_key, stored_size, state, created_at, verified_at, state_changed_at)
SELECT b.sha256, sqlc.arg(connection_id)::text,
       sqlc.arg(key_prefix)::text || 'sha256/' || substr(encode(b.sha256, 'hex'), 1, 2) || '/' || substr(encode(b.sha256, 'hex'), 3, 2) || '/' || encode(b.sha256, 'hex'),
       b.size,
       CASE WHEN b.content_status = 'ok' THEN 'present' ELSE 'missing' END,
       now(),
       CASE WHEN b.content_status = 'ok' AND sqlc.arg(verified)::boolean THEN sqlc.arg(verified_at)::text::timestamptz END,
       now()
  FROM blobs b
 WHERE b.state = 'live'
ON CONFLICT DO NOTHING;

-- name: RestoreDropCopiesWithoutBlob :execrows
-- The copy rows of the blob rows step 7 deleted because the purge of the remote caches left them without a reference: a copy row
-- has no foreign key to blobs (R-STOR-052), so it goes with its blob here, as collection drops it with its blob row.
DELETE FROM blob_copies c WHERE NOT EXISTS (SELECT 1 FROM blobs b WHERE b.sha256 = c.sha256);
