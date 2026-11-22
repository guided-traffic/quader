-- The statements of the scrub, the list of damaged blobs and the operator check per connection (storage.md R-STOR-027,
-- R-STOR-058). A timestamp a later statement compares is carried as whole microseconds since the epoch (EXTRACT of a
-- timestamptz is exact), so that a copy's identity survives the trip through Go unchanged.

-- name: ScrubDueUnverified :many
-- The copies of a connection that were never shown whole (a server-side copy, an adoption, a write without an enforced payload
-- checksum): the scrub's first work (Q-STOR-239), by digest after the cursor.
SELECT sha256, object_key, stored_size, (EXTRACT(EPOCH FROM created_at) * 1000000)::bigint AS created_us
  FROM blob_copies
 WHERE connection_id = sqlc.arg(connection_id)::text AND state = 'present' AND verified_at IS NULL
   AND sha256 > sqlc.arg(after)::bytea
 ORDER BY sha256
 LIMIT sqlc.arg(page_size)::int;

-- name: ScrubDueVerified :many
-- The copies of a connection whose last verification is older than its period, oldest first (the index blob_copies_scrub),
-- after the cursor (verified_at in microseconds, digest).
SELECT sha256, object_key, stored_size, (EXTRACT(EPOCH FROM verified_at) * 1000000)::bigint AS verified_us,
       (EXTRACT(EPOCH FROM created_at) * 1000000)::bigint AS created_us
  FROM blob_copies
 WHERE connection_id = sqlc.arg(connection_id)::text AND state = 'present'
   AND verified_at < now() - make_interval(secs => sqlc.arg(period_seconds)::double precision)
   AND (verified_at, sha256) > (timestamptz 'epoch' + sqlc.arg(after_us)::bigint * interval '1 microsecond', sqlc.arg(after)::bytea)
 ORDER BY verified_at, sha256
 LIMIT sqlc.arg(page_size)::int;

-- name: ScrubStampVerified :execrows
-- A read to the end through Quader matched the digest: verified_at and checked_at of the copy the scrub read move, unless the
-- row was written again since (another created_at), whose object the read did not see.
UPDATE blob_copies SET verified_at = now(), checked_at = now()
 WHERE sha256 = sqlc.arg(sha256)::bytea AND connection_id = sqlc.arg(connection_id)::text AND state = 'present'
   AND (EXTRACT(EPOCH FROM created_at) * 1000000)::bigint = sqlc.arg(created_us)::bigint;

-- name: ScrubCopies :many
-- The copies of one blob with what the scrub's repair and the recheck compare: the time of the copy's last change of state and
-- of its write, in microseconds.
SELECT c.connection_id, c.object_key, c.stored_size, c.state, c.verified_at, c.checked_at, c.state_changed_at,
       (EXTRACT(EPOCH FROM c.state_changed_at) * 1000000)::bigint AS state_changed_us,
       (EXTRACT(EPOCH FROM c.created_at) * 1000000)::bigint AS created_us,
       (sc.retired_at IS NOT NULL)::boolean AS retired
  FROM blob_copies c
  JOIN storage_connections sc ON sc.id = c.connection_id
 WHERE c.sha256 = sqlc.arg(sha256)::bytea
 ORDER BY c.connection_id;

-- name: ScrubRecordWhole :execrows
-- A copy marked missing or corrupt was shown whole (written again and read back by the scrub's repair, or read to its end by a
-- recheck): it is present and verified, but only while it is still the row that was found damaged (the same state and the same
-- time of that change), so a row that collection dropped and a commit wrote again is never taken for it.
UPDATE blob_copies SET state = 'present', stored_size = sqlc.arg(stored_size)::bigint, verified_at = now(), checked_at = now(),
       state_changed_at = now()
 WHERE sha256 = sqlc.arg(sha256)::bytea AND connection_id = sqlc.arg(connection_id)::text AND state = sqlc.arg(from_state)::text
   AND state IN ('missing', 'corrupt')
   AND (EXTRACT(EPOCH FROM state_changed_at) * 1000000)::bigint = sqlc.arg(state_changed_us)::bigint;

-- name: ScrubDeleteQueued :exec
-- The repair of a connection's copy is done: its row of the reconciliation's queue goes (R-STOR-056).
DELETE FROM blob_copy_queue WHERE connection_id = sqlc.arg(connection_id)::text AND sha256 = sqlc.arg(sha256)::bytea;

-- name: ScrubOverdue :one
-- quader_storage_scrub_overdue_copies (R-STOR-027) of one connection: its present copies whose last verification, or their
-- write when never verified, lies more than age_seconds (the connection's period and the grace) back.
SELECT ((SELECT count(*) FROM blob_copies
          WHERE connection_id = sqlc.arg(connection_id)::text AND state = 'present'
            AND verified_at < now() - make_interval(secs => sqlc.arg(age_seconds)::double precision))
      + (SELECT count(*) FROM blob_copies
          WHERE connection_id = sqlc.arg(connection_id)::text AND state = 'present' AND verified_at IS NULL
            AND created_at < now() - make_interval(secs => sqlc.arg(age_seconds)::double precision)))::bigint AS overdue;

-- name: DamagedBlobsByBlob :many
-- The list of damaged blobs, scope blobs (R-STOR-027, Q-API-386): the live blobs no connection holds whole, by digest after the
-- cursor; status narrows the content state, connection the blobs with a copy row there. The index blobs_damaged finds them.
SELECT b.sha256, b.size, b.content_status
  FROM blobs b
 WHERE b.state = 'live' AND b.content_status <> 'ok' AND b.sha256 > sqlc.arg(after)::bytea
   AND (sqlc.arg(status)::text = '' OR b.content_status = sqlc.arg(status)::text)
   AND (sqlc.arg(connection_id)::text = ''
        OR EXISTS (SELECT 1 FROM blob_copies c WHERE c.sha256 = b.sha256 AND c.connection_id = sqlc.arg(connection_id)::text))
 ORDER BY b.sha256
 LIMIT sqlc.arg(page_size)::int;

-- name: DamagedBlobsByCopy :many
-- The list of damaged blobs, scope copies: the live blobs with at least one missing or corrupt copy (the index
-- blob_copies_damaged), the self-healing view; status narrows the copy's state, connection the copy's connection.
SELECT b.sha256, b.size, b.content_status
  FROM blobs b
 WHERE b.state = 'live' AND b.sha256 > sqlc.arg(after)::bytea
   AND b.sha256 IN (SELECT c.sha256 FROM blob_copies c
                     WHERE c.state IN ('missing', 'corrupt') AND c.sha256 > sqlc.arg(after)::bytea
                       AND (sqlc.arg(status)::text = '' OR c.state = sqlc.arg(status)::text)
                       AND (sqlc.arg(connection_id)::text = '' OR c.connection_id = sqlc.arg(connection_id)::text))
 ORDER BY b.sha256
 LIMIT sqlc.arg(page_size)::int;

-- name: DamagedBlobCopies :many
-- The copies of the blobs of one page of the list, every state.
SELECT c.sha256, c.connection_id, c.state, c.verified_at, c.checked_at, c.state_changed_at
  FROM blob_copies c
 WHERE c.sha256 = ANY(sqlc.arg(digests)::bytea[])
 ORDER BY c.sha256, c.connection_id;

-- name: DamagedBlobItems :many
-- The first items that reference each blob of one page of the list (project, repository, path; a trashed one included).
SELECT d.sha256::bytea AS sha256, i.project_key, i.repo_key, i.path, i.trashed
  FROM unnest(sqlc.arg(digests)::bytea[]) AS d(sha256)
 CROSS JOIN LATERAL (
       SELECT p.key AS project_key, r.key AS repo_key, it.path, (it.trashed_at IS NOT NULL)::boolean AS trashed
         FROM items it
         JOIN repositories r ON r.id = it.repo_id
         JOIN project p ON p.id = r.project_id
        WHERE it.blob_sha256 = d.sha256
        ORDER BY p.key, r.key, it.path
        LIMIT sqlc.arg(max_items)::int) i
 ORDER BY 1, 2, 3, 4;

-- name: DamagedBlobItemCounts :many
-- How many items reference each blob of one page of the list.
SELECT blob_sha256::bytea AS sha256, count(*)::bigint AS items
  FROM items
 WHERE blob_sha256 = ANY(sqlc.arg(digests)::bytea[])
 GROUP BY blob_sha256;

-- name: VerifyCopyPage :many
-- One page of `quader admin storage verify` (R-STOR-058, R-HA-024, R-HA-025): the live blobs after the last digest of the
-- previous page, in digest order, each with its copy rows (none, when it has none). since_unix is the --since cut-off in Unix
-- seconds; 0 selects every blob. A blob is selected when it was created or changed state at or after the cut-off.
SELECT b.sha256, b.size, COALESCE(c.connection_id, '')::text AS connection_id, COALESCE(c.object_key, '')::text AS object_key,
       COALESCE(c.stored_size, 0)::bigint AS stored_size, COALESCE(c.state, '')::text AS state
  FROM (SELECT sha256, size FROM blobs
         WHERE state = 'live' AND sha256 > sqlc.arg(after)::bytea
           AND GREATEST(created_at, state_changed_at) >= to_timestamp(sqlc.arg(since_unix)::double precision)
         ORDER BY sha256
         LIMIT sqlc.arg(page_size)::int) b
  LEFT JOIN blob_copies c ON c.sha256 = b.sha256
 ORDER BY b.sha256, c.connection_id;

-- name: ScrubMissingDue :many
-- The copies of a connection marked missing whose store last answered for them longer ago than the interval (checked_at, which
-- the mark sets): the scrub reads them once more, so that a mark a store's wrong "no such object" left heals by itself
-- (Q-STOR-265). By digest after the cursor; the index blob_copies_damaged finds them.
SELECT sha256, object_key, stored_size, (EXTRACT(EPOCH FROM state_changed_at) * 1000000)::bigint AS state_changed_us
  FROM blob_copies
 WHERE connection_id = sqlc.arg(connection_id)::text AND state = 'missing'
   AND (checked_at IS NULL OR checked_at < now() - make_interval(secs => sqlc.arg(interval_seconds)::double precision))
   AND sha256 > sqlc.arg(after)::bytea
 ORDER BY sha256
 LIMIT sqlc.arg(page_size)::int;

-- name: ScrubTouchMissing :exec
-- The store answered for a copy marked missing again and it is still not whole: checked_at moves, so the next look waits the
-- interval.
UPDATE blob_copies SET checked_at = now()
 WHERE sha256 = sqlc.arg(sha256)::bytea AND connection_id = sqlc.arg(connection_id)::text AND state = 'missing';
