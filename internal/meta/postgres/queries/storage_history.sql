-- The writes of the history of the stored content and the task that keeps it bounded (storage.md R-STOR-108 to R-STOR-110,
-- migration 00151): the counts and the intervals each replica records (internal/blob/history), and the purge of the task
-- storage-history. The findings are written by the triggers of blob_copies, never here. Times travel as seconds since the epoch.

-- name: HistoryAddCounts :exec
-- One replica's counts of a minute, one row per hour, connection, kind and reason, added to what the other replicas flushed
-- (Q-STOR-513). The keys of one call are distinct: the recorder sums them before.
INSERT INTO storage_counts (hour, connection_id, kind, reason, count, bytes)
SELECT to_timestamp(t.h), t.c, t.k, t.r, t.n, t.b
  FROM (SELECT unnest(sqlc.arg(hours)::bigint[]) AS h, unnest(sqlc.arg(connections)::text[]) AS c, unnest(sqlc.arg(kinds)::text[]) AS k,
               unnest(sqlc.arg(reasons)::text[]) AS r, unnest(sqlc.arg(counts)::bigint[]) AS n, unnest(sqlc.arg(bytes)::bigint[]) AS b) AS t
ON CONFLICT (hour, connection_id, kind, reason) DO UPDATE
   SET count = storage_counts.count + EXCLUDED.count, bytes = storage_counts.bytes + EXCLUDED.bytes;

-- name: HistoryOpenEvent :many
-- An interval opens (R-STOR-109): inserted while the connection, the replica and the UTC day of its start have fewer than cap
-- intervals (Q-STOR-516); an interval of the same connection, kind and replica that is still open (a replica that started again
-- under its name before the task closed it) is taken over and counts one more occurrence. No row: the cap is reached.
INSERT INTO storage_events (connection_id, kind, replica, started_at, last_seen_at, code, step)
SELECT sqlc.arg(connection_id)::text, sqlc.arg(kind)::text, sqlc.arg(replica)::text, to_timestamp(sqlc.arg(started_unix)::float8),
       to_timestamp(sqlc.arg(started_unix)::float8), sqlc.arg(code)::text, NULLIF(sqlc.arg(step)::text, '')
 WHERE (SELECT count(*) FROM storage_events
         WHERE connection_id = sqlc.arg(connection_id)::text AND replica = sqlc.arg(replica)::text
           AND started_at >= date_trunc('day', to_timestamp(sqlc.arg(started_unix)::float8), 'UTC')) < sqlc.arg(day_cap)::int
   OR EXISTS (SELECT 1 FROM storage_events
               WHERE connection_id = sqlc.arg(connection_id)::text AND kind = sqlc.arg(kind)::text
                 AND replica = sqlc.arg(replica)::text AND ended_at IS NULL)
ON CONFLICT (connection_id, kind, replica) WHERE ended_at IS NULL DO UPDATE
   SET last_seen_at = GREATEST(storage_events.last_seen_at, EXCLUDED.last_seen_at), occurrences = storage_events.occurrences + 1
RETURNING id;

-- name: HistoryCountOnLastEvent :exec
-- An interval beyond the day's cap is counted as one more occurrence of the last interval of its connection, kind and replica.
UPDATE storage_events SET occurrences = occurrences + 1
 WHERE id = (SELECT e.id FROM storage_events e
              WHERE e.connection_id = sqlc.arg(connection_id)::text AND e.kind = sqlc.arg(kind)::text AND e.replica = sqlc.arg(replica)::text
              ORDER BY e.started_at DESC LIMIT 1);

-- name: HistoryUpdateEvent :exec
-- What a replica knows of an interval it opened: when it last saw it, how often the connection fell away again within the hysteresis
-- (Q-STOR-514), the latest code, and its end (ended_unix 0 while it lasts).
UPDATE storage_events
   SET last_seen_at = GREATEST(last_seen_at, to_timestamp(sqlc.arg(seen_unix)::float8)),
       occurrences  = GREATEST(occurrences, sqlc.arg(occurrences)::int),
       code         = sqlc.arg(code)::text,
       ended_at     = CASE WHEN sqlc.arg(ended_unix)::float8 > 0
                           THEN GREATEST(started_at, to_timestamp(sqlc.arg(ended_unix)::float8)) END
 WHERE id = sqlc.arg(id)::bigint AND ended_at IS NULL;

-- name: HistoryEndReplicaEvents :execrows
-- A replica that starts ends the intervals an earlier life under its name left open, at the time it last saw them.
UPDATE storage_events SET ended_at = last_seen_at WHERE replica = sqlc.arg(replica)::text AND ended_at IS NULL;

-- name: HistoryPurgeFindings :execrows
-- At most page_size findings resolved and detected before the cutoff (Q-STOR-521); an open finding stays whatever its age.
DELETE FROM storage_findings
 WHERE id IN (SELECT f.id FROM storage_findings f
               WHERE f.detected_at < to_timestamp(sqlc.arg(cutoff_unix)::float8) AND f.resolved_at IS NOT NULL
               LIMIT sqlc.arg(page_size)::int);

-- name: HistoryPurgeEvents :execrows
-- At most page_size intervals that ended before the cutoff.
DELETE FROM storage_events
 WHERE id IN (SELECT e.id FROM storage_events e
               WHERE e.ended_at < to_timestamp(sqlc.arg(cutoff_unix)::float8)
               LIMIT sqlc.arg(page_size)::int);

-- name: HistoryPurgeCounts :execrows
-- At most page_size hourly counts before the cutoff.
DELETE FROM storage_counts
 WHERE (hour, connection_id, kind, reason) IN (
       SELECT c.hour, c.connection_id, c.kind, c.reason FROM storage_counts c
        WHERE c.hour < to_timestamp(sqlc.arg(cutoff_unix)::float8)
        LIMIT sqlc.arg(page_size)::int);

-- name: HistoryCloseDeadReplicas :execrows
-- The open intervals of a replica whose heartbeat is older than stale_secs, or that the registry no longer lists, end at the time it
-- last saw them: nobody is left to end them.
UPDATE storage_events e SET ended_at = e.last_seen_at
 WHERE e.ended_at IS NULL
   AND NOT EXISTS (SELECT 1 FROM replica r
                    WHERE r.name = e.replica AND r.heartbeat_at >= now() - make_interval(secs => sqlc.arg(stale_secs)::float8));

-- name: HistoryReconcileFindings :execrows
-- The safety net (Q-STOR-518): at most page_size open findings whose copy is no longer in the state they record (a restored database,
-- a path the triggers did not see) are resolved as reconciled.
UPDATE storage_findings SET resolved_at = now(), resolution = 'reconciled'
 WHERE id IN (SELECT f.id FROM storage_findings f
               WHERE f.resolved_at IS NULL
                 AND NOT EXISTS (SELECT 1 FROM blob_copies c
                                  WHERE c.sha256 = f.sha256 AND c.connection_id = f.connection_id AND c.state = f.status)
               LIMIT sqlc.arg(page_size)::int);
