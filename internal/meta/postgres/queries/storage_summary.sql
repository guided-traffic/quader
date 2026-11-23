-- The storage summary and the object-store connections of the API (storage.md R-STOR-028, R-STOR-061; operations-and-observability.md
-- R-OPS-023): the statements of internal/blob/summary. The task runs the figures one after the other on one connection, outside a
-- transaction (R-ARCH-019), the copies before the live blobs; the result is one row of storage_summary. The API's list of the connections reads the rows, the replica registry
-- and the queue at the time of the request and the figures from the snapshot.

-- name: StorageSummaryTotals :one
-- The installation's figures (R-STOR-028): the live blobs from the folded counter of R-STOR-003 plus the deltas the fold has not taken
-- yet (an exact read, as R-STOR-003 shows the pattern; the deltas are a short table); the trashed items through items_trash; the
-- candidates of collection, the queue's live blobs and the rows in state deleting (blobs_deleting); the damaged blobs (blobs_damaged).
SELECT (COALESCE((SELECT t.blobs FROM storage_usage_totals t WHERE t.id = 1), 0)
        + COALESCE((SELECT sum(d.blobs) FROM storage_usage_deltas d WHERE d.repo_id IS NULL), 0))::bigint AS blobs,
       (COALESCE((SELECT t.physical_bytes FROM storage_usage_totals t WHERE t.id = 1), 0)
        + COALESCE((SELECT sum(d.physical_bytes) FROM storage_usage_deltas d WHERE d.repo_id IS NULL), 0))::bigint AS blob_bytes,
       (SELECT count(*) FROM items i WHERE i.trashed_at IS NOT NULL)::bigint AS trash_items,
       (SELECT COALESCE(sum(i.size), 0) FROM items i WHERE i.trashed_at IS NOT NULL)::bigint AS trash_bytes,
       ((SELECT count(*) FROM blob_gc_queue g JOIN blobs b ON b.sha256 = g.sha256 AND b.state = 'live')
        + (SELECT count(*) FROM blobs b WHERE b.state = 'deleting'))::bigint AS pending_blobs,
       ((SELECT COALESCE(sum(b.size), 0) FROM blob_gc_queue g JOIN blobs b ON b.sha256 = g.sha256 AND b.state = 'live')
        + (SELECT COALESCE(sum(b.size), 0) FROM blobs b WHERE b.state = 'deleting'))::bigint AS pending_bytes,
       (SELECT count(*) FROM blobs b WHERE b.content_status = 'missing' AND b.state = 'live')::bigint AS damaged_missing,
       (SELECT count(*) FROM blobs b WHERE b.content_status = 'corrupt' AND b.state = 'live')::bigint AS damaged_corrupt;

-- name: StorageSummaryRepositories :many
-- One row per repository that is not being deleted (R-STOR-028): its project, key, class and format, its untrashed files, folders and
-- logical bytes from repository_usage plus its unfolded deltas, and its packages and untrashed versions. The caller leaves out a
-- virtual repository without items (Q-STOR-215).
WITH pending AS (
    SELECT d.repo_id, sum(d.logical_bytes) AS logical_bytes, sum(d.files) AS files, sum(d.folders) AS folders
      FROM storage_usage_deltas d
     WHERE d.repo_id IS NOT NULL
     GROUP BY d.repo_id
), pkgs AS (
    SELECT p.repo_id, count(*) AS packages FROM packages p GROUP BY p.repo_id
), versions AS (
    SELECT p.repo_id, count(*) AS versions
      FROM package_versions v JOIN packages p ON p.id = v.package_id
     WHERE v.trashed_at IS NULL
     GROUP BY p.repo_id
)
SELECT pr.key::text AS project, r.key::text AS key, r.class::text AS class, r.format::text AS format,
       (COALESCE(u.files, 0) + COALESCE(d.files, 0))::bigint AS files,
       (COALESCE(u.folders, 0) + COALESCE(d.folders, 0))::bigint AS folders,
       (COALESCE(u.logical_bytes, 0) + COALESCE(d.logical_bytes, 0))::bigint AS logical_bytes,
       COALESCE(pk.packages, 0)::bigint AS packages,
       COALESCE(v.versions, 0)::bigint AS versions
  FROM repositories r
  JOIN project pr ON pr.id = r.project_id
  LEFT JOIN repository_usage u ON u.repo_id = r.id
  LEFT JOIN pending d ON d.repo_id = r.id
  LEFT JOIN pkgs pk ON pk.repo_id = r.id
  LEFT JOIN versions v ON v.repo_id = r.id
 WHERE r.lifecycle = 'active'
 ORDER BY pr.key, r.key;

-- name: StorageSummaryConnections :many
-- The figures of every connection that is not retired (R-STOR-028, R-STOR-061): its copies of live blobs per state, the stored bytes of
-- the present ones and the plain bytes of their blobs (the backlog's subtrahend), the present copies read whole within the connection's
-- scrub period, the overdue ones (the period and the grace, Q-STOR-266), and the sole copies, the live blobs whose only present copy
-- lies here (Q-STOR-371). The periods are the worker's configuration (ids and seconds), default_period_seconds for a connection it does
-- not configure. One pass over the copies of live blobs; a sole copy costs one probe of the primary key of blob_copies.
WITH periods AS (
    -- two arrays of one length, unnested side by side
    SELECT unnest(sqlc.arg(ids)::text[]) AS id, unnest(sqlc.arg(period_seconds)::float8[]) AS seconds
), copies AS (
    SELECT c.connection_id, c.state, c.stored_size, b.size, c.verified_at, c.created_at,
           (c.state = 'present' AND NOT EXISTS (
               SELECT 1 FROM blob_copies o
                WHERE o.sha256 = c.sha256 AND o.connection_id <> c.connection_id AND o.state = 'present')) AS sole
      FROM blob_copies c
      JOIN blobs b ON b.sha256 = c.sha256 AND b.state = 'live'
)
SELECT sc.id,
       count(c.connection_id) FILTER (WHERE c.state = 'present')::bigint AS present_copies,
       COALESCE(sum(c.stored_size) FILTER (WHERE c.state = 'present'), 0)::bigint AS present_bytes,
       COALESCE(sum(c.size) FILTER (WHERE c.state = 'present'), 0)::bigint AS present_blob_bytes,
       count(c.connection_id) FILTER (WHERE c.state = 'missing')::bigint AS missing_copies,
       count(c.connection_id) FILTER (WHERE c.state = 'corrupt')::bigint AS corrupt_copies,
       count(c.connection_id) FILTER (
           WHERE c.state = 'present'
             AND c.verified_at >= now() - make_interval(secs => COALESCE(p.seconds, sqlc.arg(default_period_seconds)::float8))
       )::bigint AS verified_within_period,
       count(c.connection_id) FILTER (
           WHERE c.state = 'present'
             AND COALESCE(c.verified_at, c.created_at)
                 < now() - make_interval(secs => COALESCE(p.seconds, sqlc.arg(default_period_seconds)::float8) + sqlc.arg(grace_seconds)::float8)
       )::bigint AS scrub_overdue,
       count(c.connection_id) FILTER (WHERE c.sole)::bigint AS sole_copies
  FROM storage_connections sc
  LEFT JOIN periods p ON p.id = sc.id
  LEFT JOIN copies c ON c.connection_id = sc.id
 WHERE sc.retired_at IS NULL
 GROUP BY sc.id, p.seconds
 ORDER BY sc.id;

-- name: StorageSummaryKeyCopies :many
-- The copies per storage key of each connection (R-STOR-088, Q-API-405): the copies that name a key in storage_key_id. One pass over
-- blob_copies; a plain connection's copies name none.
SELECT c.connection_id, c.storage_key_id::text AS storage_key_id, count(*)::bigint AS copies
  FROM blob_copies c
 WHERE c.storage_key_id IS NOT NULL
 GROUP BY c.connection_id, c.storage_key_id
 ORDER BY c.connection_id, c.storage_key_id;

-- name: StorageSummarySave :exec
-- The latest snapshot replaces the one before; computed_at is RFC 3339 text.
INSERT INTO storage_summary (id, computed_at, duration_ms, summary)
VALUES (1, sqlc.arg(computed_at)::text::timestamptz, sqlc.arg(duration_ms)::integer, sqlc.arg(summary)::jsonb)
ON CONFLICT (id) DO UPDATE SET computed_at = EXCLUDED.computed_at, duration_ms = EXCLUDED.duration_ms, summary = EXCLUDED.summary;

-- name: StorageSummaryLatest :many
-- The latest snapshot: no row before the first computation. computed_us is the time in microseconds since the epoch.
SELECT (EXTRACT(EPOCH FROM s.computed_at) * 1000000)::bigint AS computed_us, s.duration_ms, s.summary
  FROM storage_summary s
 WHERE s.id = 1;

-- name: StorageConnectionsList :many
-- The connections of the API (R-STOR-061) with what PostgreSQL knows at the time of the request: the row of each connection ever seen,
-- the live replicas of the registry (heartbeat younger than stale_secs) that have it in their configuration, how many of them have its
-- circuit open and whether one of them configures it without drain; the number of live replicas that report an object-store view; the
-- queue's rows and the time of the oldest; whether it holds a copy. The times travel as microseconds since the epoch, 0 for none.
WITH live AS (
    SELECT r.object_store
      FROM replica r
     WHERE r.heartbeat_at >= now() - make_interval(secs => sqlc.arg(stale_secs)::float8)
       AND r.object_store ? 'connections'
), views AS (
    SELECT e ->> 'id' AS id, count(*) AS configured,
           count(*) FILTER (WHERE e ->> 'circuit' = 'open') AS circuit_open,
           bool_or(NOT COALESCE((e ->> 'drain')::boolean, false)) AS undrained
      FROM live, jsonb_array_elements(live.object_store -> 'connections') e
     GROUP BY e ->> 'id'
)
SELECT sc.id, sc.bucket, sc.prefix, sc.encrypted, sc.keyed_names, sc.capabilities,
       COALESCE((EXTRACT(EPOCH FROM sc.filled_at) * 1000000)::bigint, 0)::bigint AS filled_us,
       COALESCE((EXTRACT(EPOCH FROM sc.drained_at) * 1000000)::bigint, 0)::bigint AS drained_us,
       COALESCE((EXTRACT(EPOCH FROM sc.retired_at) * 1000000)::bigint, 0)::bigint AS retired_us,
       COALESCE((EXTRACT(EPOCH FROM sc.last_full_pass_at) * 1000000)::bigint, 0)::bigint AS last_full_pass_us,
       (SELECT count(*) FROM live)::int AS live_replicas,
       COALESCE(v.configured, 0)::int AS configured,
       COALESCE(v.circuit_open, 0)::int AS circuit_open,
       COALESCE(v.undrained, false)::boolean AS undrained,
       (SELECT count(*) FROM blob_copy_queue q WHERE q.connection_id = sc.id)::bigint AS queued,
       COALESCE((SELECT (EXTRACT(EPOCH FROM min(q.enqueued_at)) * 1000000)::bigint FROM blob_copy_queue q WHERE q.connection_id = sc.id), 0)::bigint AS oldest_queued_us,
       EXISTS (SELECT 1 FROM blob_copies bc WHERE bc.connection_id = sc.id)::boolean AS holds_copies
  FROM storage_connections sc
  LEFT JOIN views v ON v.id = sc.id
 ORDER BY sc.id;

-- name: StorageConnectionKeys :many
-- The storage keys of every connection by identifier, generation and state (R-STOR-088, Q-API-405); never the sealed value. The times of
-- creation and of a destroyed key's destruction (R-STOR-097, 0 for any other) travel as microseconds since the epoch.
SELECT k.key_id, k.connection_id, k.generation, k.state, (EXTRACT(EPOCH FROM k.created_at) * 1000000)::bigint AS created_us,
       COALESCE((EXTRACT(EPOCH FROM k.destroyed_at) * 1000000)::bigint, 0)::bigint AS destroyed_us
  FROM storage_keys k
 WHERE k.kind = 'storage'
 ORDER BY k.connection_id, k.generation;
