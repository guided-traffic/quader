-- The reads of the history of the stored content for the storage page (storage.md R-STOR-111): the intervals, the hourly counts and
-- the findings of a period, folded where the page needs no single row, one page of findings with the items behind them, and the
-- acknowledgement of a finding; the items behind a page of findings are read by blob_scrub.sql's DamagedBlobItems and
-- DamagedBlobItemCounts (Q-STOR-531). The writes are storage_history.sql's and the triggers' of migration 00151. Times travel as seconds or
-- microseconds since the epoch, never as a driver's type (R-ARCH-002). A finding belongs to a period when it was still open at its start
-- (resolved_at NULL or not before from_unix): an open finding of any age stays in every period (Q-STOR-525).

-- name: HistoryReadEvents :many
-- The intervals that overlap the period: started before its end, not ended before its start; ended_unix 0 while one lasts.
SELECT e.connection_id, e.kind, e.replica, extract(epoch FROM e.started_at)::float8 AS started_unix,
       COALESCE(extract(epoch FROM e.ended_at), 0)::float8 AS ended_unix, e.code, COALESCE(e.step, '')::text AS step,
       e.occurrences::bigint AS occurrences
  FROM storage_events e
 WHERE e.started_at < to_timestamp(sqlc.arg(to_unix)::float8)
   AND (e.ended_at IS NULL OR e.ended_at >= to_timestamp(sqlc.arg(from_unix)::float8))
 ORDER BY e.connection_id, e.kind, e.started_at, e.id;

-- name: HistoryReadCellCounts :many
-- The hourly counts of the period summed per connection, kind and cell (cells of cell_seconds counted from from_unix; an hour that began
-- before the period and reaches into it is the first cell's, Q-STOR-527). unknown_key splits the refusals of the stored format whose key
-- is unknown, which mark no copy (R-STOR-110).
SELECT c.connection_id, c.kind, (c.kind = 'encrypted_read_refused' AND c.reason = 'unknown_key')::boolean AS unknown_key,
       GREATEST(0, floor((extract(epoch FROM c.hour) - sqlc.arg(from_unix)::float8) / sqlc.arg(cell_seconds)::float8))::int AS cell,
       sum(c.count)::bigint AS n, sum(c.bytes)::bigint AS bytes
  FROM storage_counts c
 WHERE c.hour > to_timestamp(sqlc.arg(from_unix)::float8) - interval '1 hour'
   AND c.hour < to_timestamp(sqlc.arg(to_unix)::float8)
 GROUP BY 1, 2, 3, 4;

-- name: HistoryReadCellFindings :many
-- The findings detected in the period per connection and cell: how many, their stored bytes and the blobs no other copy held then.
SELECT f.connection_id,
       LEAST(sqlc.arg(cells)::int - 1,
             floor((extract(epoch FROM f.detected_at) - sqlc.arg(from_unix)::float8) / sqlc.arg(cell_seconds)::float8)::int)::int AS cell,
       count(*)::bigint AS n, sum(f.stored_size)::bigint AS bytes,
       count(DISTINCT f.sha256) FILTER (WHERE f.unavailable)::bigint AS unavailable_blobs
  FROM storage_findings f
 WHERE f.detected_at >= to_timestamp(sqlc.arg(from_unix)::float8) AND f.detected_at < to_timestamp(sqlc.arg(to_unix)::float8)
 GROUP BY 1, 2;

-- name: HistoryReadFindingTotals :many
-- The findings of the period per connection, source and outcome (the resolution, or open or acknowledged while unresolved) with their
-- stored bytes: the totals of the section Integrity.
SELECT f.connection_id, f.source,
       COALESCE(f.resolution, CASE WHEN f.acknowledged_at IS NULL THEN 'open' ELSE 'acknowledged' END)::text AS outcome,
       count(*)::bigint AS n, sum(f.stored_size)::bigint AS bytes
  FROM storage_findings f
 WHERE f.resolved_at IS NULL OR f.resolved_at >= to_timestamp(sqlc.arg(from_unix)::float8)
 GROUP BY 1, 2, 3;

-- name: HistoryReadDayFindings :many
-- The findings of the period per connection and UTC day of their detection: one problem each (Q-STOR-525). last_resolved_unix is the
-- latest resolution, 0 when none is resolved.
SELECT f.connection_id, floor(extract(epoch FROM f.detected_at) / 86400)::bigint AS day,
       count(*)::bigint AS n, sum(f.stored_size)::bigint AS bytes,
       min(extract(epoch FROM f.detected_at))::float8 AS first_unix,
       COALESCE(max(extract(epoch FROM f.resolved_at)), 0)::float8 AS last_resolved_unix,
       count(*) FILTER (WHERE f.resolved_at IS NOT NULL)::bigint AS resolved,
       count(*) FILTER (WHERE f.resolved_at IS NULL AND f.acknowledged_at IS NULL)::bigint AS open,
       count(*) FILTER (WHERE f.resolved_at IS NULL AND f.acknowledged_at IS NOT NULL)::bigint AS acknowledged,
       count(*) FILTER (WHERE f.unavailable)::bigint AS unavailable,
       count(*) FILTER (WHERE f.unavailable AND f.resolved_at IS NULL)::bigint AS unavailable_open,
       array_agg(DISTINCT f.source ORDER BY f.source)::text[] AS sources,
       array_agg(DISTINCT f.reason ORDER BY f.reason)::text[] AS reasons
  FROM storage_findings f
 WHERE f.resolved_at IS NULL OR f.resolved_at >= to_timestamp(sqlc.arg(from_unix)::float8)
 GROUP BY 1, 2;

-- name: HistoryReadDayCounts :many
-- The refused uploads, the reads no copy served and the findings beyond the daily cap of the period per connection, kind and UTC day,
-- with the first and the last hour that counted one and the reasons.
SELECT c.connection_id, c.kind, floor(extract(epoch FROM c.hour) / 86400)::bigint AS day,
       sum(c.count)::bigint AS n, sum(c.bytes)::bigint AS bytes,
       min(extract(epoch FROM c.hour))::float8 AS first_hour_unix, max(extract(epoch FROM c.hour))::float8 AS last_hour_unix,
       array_agg(DISTINCT c.reason ORDER BY c.reason)::text[] AS reasons
  FROM storage_counts c
 WHERE c.kind IN ('upload_refused', 'read_unavailable', 'findings_unlisted') AND c.count > 0
   AND c.hour > to_timestamp(sqlc.arg(from_unix)::float8) - interval '1 hour'
   AND c.hour < to_timestamp(sqlc.arg(to_unix)::float8)
 GROUP BY 1, 2, 3;

-- name: HistoryListFindings :many
-- One page of the findings of the period, newest first (detected_at, then id), narrowed by connection (empty: every one) and state
-- (open: unresolved and not acknowledged; acknowledged: unresolved and acknowledged; resolved; all). The page after the cursor
-- (after_id 0: the first) compares the detection in microseconds, exactly.
SELECT f.id, f.sha256, f.connection_id, f.status, f.source, f.reason, f.stored_size, f.blob_size, f.unavailable,
       (extract(epoch FROM f.detected_at) * 1000000)::bigint AS detected_us,
       COALESCE((extract(epoch FROM f.resolved_at) * 1000000)::bigint, 0)::bigint AS resolved_us,
       COALESCE(f.resolution, '')::text AS resolution,
       COALESCE((extract(epoch FROM f.acknowledged_at) * 1000000)::bigint, 0)::bigint AS acknowledged_us,
       COALESCE(f.acknowledged_by::text, '')::text AS acknowledged_by, COALESCE(p.name, '')::text AS acknowledged_by_name,
       COALESCE(f.acknowledged_note, '')::text AS acknowledged_note
  FROM storage_findings f
  LEFT JOIN principal p ON p.id = f.acknowledged_by
 WHERE (f.resolved_at IS NULL OR f.resolved_at >= to_timestamp(sqlc.arg(from_unix)::float8))
   AND (sqlc.arg(connection_id)::text = '' OR f.connection_id = sqlc.arg(connection_id)::text)
   AND CASE sqlc.arg(state)::text
           WHEN 'open' THEN f.resolved_at IS NULL AND f.acknowledged_at IS NULL
           WHEN 'acknowledged' THEN f.resolved_at IS NULL AND f.acknowledged_at IS NOT NULL
           WHEN 'resolved' THEN f.resolved_at IS NOT NULL
           ELSE true END
   AND (sqlc.arg(after_id)::bigint = 0
        OR (f.detected_at, f.id) < (timestamptz 'epoch' + sqlc.arg(after_us)::bigint * interval '1 microsecond', sqlc.arg(after_id)::bigint))
 ORDER BY f.detected_at DESC, f.id DESC
 LIMIT sqlc.arg(page_size)::int;

-- name: HistoryAcknowledgeFinding :many
-- Acknowledges an unresolved finding (R-STOR-111, Q-STOR-530): the time, the person (a principal's id; empty for a caller that is no
-- user or service account) and the note; a second acknowledgement replaces the first. It never resolves. No row: the finding is
-- resolved or unknown.
UPDATE storage_findings
   SET acknowledged_at = now(), acknowledged_by = NULLIF(sqlc.arg(acknowledged_by)::text, '')::uuid, acknowledged_note = NULLIF(sqlc.arg(note)::text, '')
 WHERE id = sqlc.arg(id)::bigint AND resolved_at IS NULL
RETURNING id;

-- name: HistoryFindingResolved :many
-- Whether a finding is resolved; no row when no finding has the id.
SELECT (resolved_at IS NOT NULL)::boolean AS resolved FROM storage_findings WHERE id = sqlc.arg(id)::bigint;

-- name: HistoryFindingByID :many
-- One finding as the list shows it, by id.
SELECT f.id, f.sha256, f.connection_id, f.status, f.source, f.reason, f.stored_size, f.blob_size, f.unavailable,
       (extract(epoch FROM f.detected_at) * 1000000)::bigint AS detected_us,
       COALESCE((extract(epoch FROM f.resolved_at) * 1000000)::bigint, 0)::bigint AS resolved_us,
       COALESCE(f.resolution, '')::text AS resolution,
       COALESCE((extract(epoch FROM f.acknowledged_at) * 1000000)::bigint, 0)::bigint AS acknowledged_us,
       COALESCE(f.acknowledged_by::text, '')::text AS acknowledged_by, COALESCE(p.name, '')::text AS acknowledged_by_name,
       COALESCE(f.acknowledged_note, '')::text AS acknowledged_note
  FROM storage_findings f
  LEFT JOIN principal p ON p.id = f.acknowledged_by
 WHERE f.id = sqlc.arg(id)::bigint;
