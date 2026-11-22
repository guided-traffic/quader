-- The statements of the reconciliation between the object-store connections (storage.md R-STOR-056): its queue, its pass, the
-- recording of a copy, the drain, the retirement, its gauges and its position. The copy row of a copy it made is written by
-- PutBlobCopy (blob_copies.sql), the lock of a blob by LockBlobForMark. Queries that may find no row are :many.

-- name: ReconcileDueCopies :many
-- The due rows of the queue for the connections the run can write to now, oldest first (R-STOR-056): a row of a connection
-- whose circuit is open on the worker, or that the worker does not configure, waits without spending attempts.
SELECT connection_id, sha256, attempts FROM blob_copy_queue
 WHERE connection_id = ANY(sqlc.arg(connections)::text[]) AND next_attempt_at <= now()
 ORDER BY next_attempt_at, connection_id, sha256
 LIMIT sqlc.arg(page_size)::int;

-- name: ReconcileDeleteQueued :exec
DELETE FROM blob_copy_queue WHERE connection_id = sqlc.arg(connection_id)::text AND sha256 = sqlc.arg(sha256)::bytea;

-- name: ReconcileQueueFailed :exec
-- A failed copy (R-STOR-056): attempts counted, the next attempt after min(2^attempts minutes, retry_max_backoff), the reason kept.
UPDATE blob_copy_queue
   SET attempts = attempts + 1,
       next_attempt_at = now() + make_interval(secs => LEAST(power(2, LEAST(attempts + 1, 20)) * 60, sqlc.arg(max_backoff_seconds)::float8)),
       last_error = sqlc.arg(last_error)::text
 WHERE connection_id = sqlc.arg(connection_id)::text AND sha256 = sqlc.arg(sha256)::bytea;

-- name: ReconcileQueueGaps :exec
-- The gaps the pass or the drain found on one connection (R-STOR-056); a gap that is queued already keeps its row.
INSERT INTO blob_copy_queue (connection_id, sha256)
SELECT sqlc.arg(connection_id)::text, d FROM unnest(sqlc.arg(digests)::bytea[]) AS d
ON CONFLICT DO NOTHING;

-- name: ReconcileQueueFull :one
-- Whether a connection's queue holds queue_limit rows, the pass's reason to wait; it reads at most queue_limit rows.
SELECT (count(*) >= sqlc.arg(queue_limit)::int)::boolean
  FROM (SELECT 1 FROM blob_copy_queue WHERE connection_id = sqlc.arg(connection_id)::text LIMIT sqlc.arg(queue_limit)::int) s;

-- name: ReconcilePassPage :many
-- One batch of the pass (R-STOR-056): the live blobs after a digest in digest order, each with the connections that hold a
-- present copy of it, read with one lookup of their copies by the primary key.
SELECT b.sha256,
       COALESCE(array_agg(c.connection_id ORDER BY c.connection_id) FILTER (WHERE c.state = 'present'), '{}')::text[] AS present
  FROM (SELECT sha256 FROM blobs WHERE state = 'live' AND sha256 > sqlc.arg(after)::bytea ORDER BY sha256 LIMIT sqlc.arg(page_size)::int) b
  LEFT JOIN blob_copies c ON c.sha256 = b.sha256
 GROUP BY b.sha256
 ORDER BY b.sha256;

-- name: ReconcilePassDue :one
-- Whether a new full pass is due (Q-STOR-257): a target connection has never ended one, is filling with an empty queue, or the
-- last one ended at least interval ago.
SELECT EXISTS (
    SELECT 1 FROM storage_connections sc
     WHERE sc.id = ANY(sqlc.arg(targets)::text[])
       AND (sc.last_full_pass_at IS NULL
            OR sc.last_full_pass_at < now() - make_interval(secs => sqlc.arg(interval_seconds)::float8)
            OR (sc.filled_at IS NULL AND NOT EXISTS (SELECT 1 FROM blob_copy_queue q WHERE q.connection_id = sc.id)))
)::boolean;

-- name: ReconcilePassStarted :exec
-- A full pass starts for the target connections; a target that was drained earlier is no longer (it holds every blob again
-- once the reconciliation filled it).
UPDATE storage_connections SET pass_started_at = now(), drained_at = NULL WHERE id = ANY(sqlc.arg(targets)::text[]);

-- name: ReconcilePassEnded :exec
UPDATE storage_connections SET last_full_pass_at = now() WHERE id = ANY(sqlc.arg(targets)::text[]);

-- name: ReconcileFilled :execrows
-- The first full pass without a gap and with an empty queue turns a filling connection active (R-STOR-051).
UPDATE storage_connections SET filled_at = now()
 WHERE id = sqlc.arg(id)::text AND filled_at IS NULL
   AND NOT EXISTS (SELECT 1 FROM blob_copy_queue q WHERE q.connection_id = sqlc.arg(id)::text);

-- name: ReconcileUnfill :exec
-- A connection with drain: true holds every blob no longer: it is draining, and filling again if the drain is taken back.
UPDATE storage_connections SET filled_at = NULL WHERE id = ANY(sqlc.arg(draining)::text[]) AND filled_at IS NOT NULL;

-- name: ReconcileDrainPage :many
-- One batch of a drain's walk over the copies of the draining connection by object name (R-STOR-056).
SELECT sha256, object_key FROM blob_copies
 WHERE connection_id = sqlc.arg(connection_id)::text AND state <> 'deleting' AND object_key > sqlc.arg(after)::text
 ORDER BY object_key
 LIMIT sqlc.arg(page_size)::int;

-- name: ReconcileCopiesOf :many
-- The copies of a batch of blobs with whether each blob is live, one lookup by the primary key of blob_copies.
SELECT c.sha256, c.connection_id, c.object_key, c.stored_size, c.state, c.verified_at, (b.state = 'live')::boolean AS live
  FROM blob_copies c
  JOIN blobs b ON b.sha256 = c.sha256
 WHERE c.sha256 = ANY(sqlc.arg(digests)::bytea[])
 ORDER BY c.sha256, c.connection_id;

-- name: ReconcileClaimDrainCopy :execrows
-- The drain claims a copy for deletion, in the transaction that holds the blob's lock (R-STOR-056).
UPDATE blob_copies SET state = 'deleting', state_changed_at = now()
 WHERE sha256 = sqlc.arg(sha256)::bytea AND connection_id = sqlc.arg(connection_id)::text AND state <> 'deleting';

-- name: ReconcileDropDrainedCopy :execrows
DELETE FROM blob_copies WHERE sha256 = sqlc.arg(sha256)::bytea AND connection_id = sqlc.arg(connection_id)::text AND state = 'deleting';

-- name: ReconcileTakeStaleDrainClaims :many
-- The drain's claims nobody finished within handover_delay: a copy claimed for deletion whose blob is live (collection claims a
-- copy only of a blob it claimed). The compare-and-set of the claim time takes each over, so of two readers one finishes it.
UPDATE blob_copies c SET state_changed_at = now()
  FROM blobs b
 WHERE b.sha256 = c.sha256 AND b.state = 'live' AND c.state = 'deleting'
   AND c.state_changed_at < now() - make_interval(secs => sqlc.arg(handover_seconds)::float8)
   AND (c.sha256, c.connection_id) IN (
       SELECT x.sha256, x.connection_id FROM blob_copies x
         JOIN blobs bx ON bx.sha256 = x.sha256 AND bx.state = 'live'
        WHERE x.state = 'deleting' AND x.state_changed_at < now() - make_interval(secs => sqlc.arg(handover_seconds)::float8)
        ORDER BY x.state_changed_at
        LIMIT sqlc.arg(page_size)::int)
RETURNING c.sha256, c.connection_id, c.object_key;

-- name: ReconcileMarkDrained :execrows
-- A draining connection that holds no copy any more is drained (R-STOR-051).
UPDATE storage_connections SET drained_at = now()
 WHERE id = sqlc.arg(id)::text AND drained_at IS NULL
   AND NOT EXISTS (SELECT 1 FROM blob_copies c WHERE c.connection_id = sqlc.arg(id)::text);

-- name: ReconcileRetire :many
-- A drained connection that no live replica has in its configuration is retired (R-STOR-051): the replica registry's
-- object-store view, rows whose heartbeat is younger than stale_secs. Its queue rows go with it.
WITH retired AS (
    UPDATE storage_connections sc SET retired_at = now()
     WHERE sc.drained_at IS NOT NULL AND sc.retired_at IS NULL
       AND NOT EXISTS (SELECT 1 FROM blob_copies c WHERE c.connection_id = sc.id)
       AND NOT EXISTS (
           SELECT 1 FROM replica r, jsonb_array_elements(COALESCE(r.object_store -> 'connections', '[]'::jsonb)) e
            WHERE e ->> 'id' = sc.id AND r.heartbeat_at >= now() - make_interval(secs => sqlc.arg(stale_secs)::float8))
    RETURNING sc.id
), dropped AS (
    DELETE FROM blob_copy_queue q USING retired WHERE q.connection_id = retired.id
)
SELECT id FROM retired ORDER BY id;

-- name: ReconcileGauges :many
-- quader_storage_reconcile_queue, quader_storage_reconcile_queue_oldest_seconds, quader_storage_connection_filling and
-- quader_storage_connection_detached per connection that is not retired (R-STOR-051, R-STOR-056, R-STOR-060, ha-and-helm.md
-- Q-HA-393), from the replica registry's object-store view of the live replicas (heartbeat younger than stale_secs): filling is
-- a connection without filled_at that a live replica configures without drain; detached is a connection that holds a copy and
-- that no live replica configures.
WITH configured AS (
    SELECT e ->> 'id' AS id, bool_or(NOT COALESCE((e ->> 'drain')::boolean, false)) AS undrained
      FROM replica r, jsonb_array_elements(COALESCE(r.object_store -> 'connections', '[]'::jsonb)) e
     WHERE r.heartbeat_at >= now() - make_interval(secs => sqlc.arg(stale_secs)::float8)
     GROUP BY e ->> 'id'
)
SELECT sc.id,
       (SELECT count(*) FROM blob_copy_queue q WHERE q.connection_id = sc.id)::bigint AS queued,
       COALESCE((SELECT EXTRACT(EPOCH FROM now() - min(q.enqueued_at)) FROM blob_copy_queue q WHERE q.connection_id = sc.id), 0)::float8 AS oldest_seconds,
       (sc.filled_at IS NULL AND COALESCE(c.undrained, false))::boolean AS filling,
       (c.id IS NULL AND EXISTS (SELECT 1 FROM blob_copies bc WHERE bc.connection_id = sc.id))::boolean AS detached
  FROM storage_connections sc
  LEFT JOIN configured c ON c.id = sc.id
 WHERE sc.retired_at IS NULL
 ORDER BY sc.id;

-- name: ReconcileState :one
-- Where the reconciliation's pass and drains stand (Q-STOR-257), a JSON document; '{}' before the first run.
SELECT COALESCE((SELECT value::text FROM system_state WHERE key = 'storage_reconcile_state'), '{}')::text;

-- name: ReconcileStateSet :exec
INSERT INTO system_state (key, value) VALUES ('storage_reconcile_state', sqlc.arg(state)::text::jsonb)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();
