-- The statements of collection per copy and of the orphan sweep (storage.md R-STOR-024, R-STOR-026, R-STOR-057). Queries that may
-- find no row are :many. The blob store imports no driver type (R-ARCH-002): times travel as seconds, a missing row as a flag and
-- an empty value.

-- name: ClaimBlobCopies :exec
-- Step (3) per copy (R-STOR-057): the copies of a blob that collection claims are claimed with it, in the claim's transaction, so
-- that the rows say what is being deleted. A copy a drain claimed already keeps its claim time.
UPDATE blob_copies SET state = 'deleting', state_changed_at = now()
 WHERE sha256 = sqlc.arg(sha256)::bytea AND state <> 'deleting';

-- name: CollectCopies :many
-- The copies of claimed blobs whose objects step (4) deletes, one connection after the other. Only a blob in state deleting is
-- collection's: a deleting copy of a live blob is a drain's claim (R-STOR-056), never a sign that the blob is collected.
SELECT c.sha256, c.connection_id, c.object_key
  FROM blob_copies c
  JOIN blobs b ON b.sha256 = c.sha256 AND b.state = 'deleting'
 WHERE c.sha256 = ANY(sqlc.arg(digests)::bytea[])
 ORDER BY c.connection_id, c.object_key;

-- name: DeleteCollectedCopies :many
-- The copy rows of step (4): each copy whose object was deleted, and each copy that was released because its delete was never sent
-- (Q-STOR-237). Only copies of blobs in state deleting. The rows this statement deleted come back, so that a release is counted once
-- when two runs finish one blob.
DELETE FROM blob_copies c
 USING (SELECT unnest(sqlc.arg(digests)::bytea[]) AS sha256, unnest(sqlc.arg(connections)::text[]) AS connection_id) AS d,
       blobs b
 WHERE c.sha256 = d.sha256 AND c.connection_id = d.connection_id AND b.sha256 = c.sha256 AND b.state = 'deleting'
RETURNING c.sha256, c.connection_id;

-- name: SweepLookup :many
-- The rows of the objects of one page of an orphan sweep's listing (R-STOR-026): the copy by its name on the connection
-- (blob_copies_key) with the digest it stands for, and the blob by the digest the name carries (a keyed name carries none: the sweep
-- asks again with the digest the object's record names, R-STOR-087). An absent row is has_copy false or an empty blob_state.
SELECT k.object_key::text AS object_key, (c.sha256 IS NOT NULL)::boolean AS has_copy, COALESCE(c.state, '')::text AS copy_state,
       COALESCE(c.sha256, '\x'::bytea)::bytea AS copy_sha256,
       COALESCE(c.stored_size, -1)::bigint AS stored_size, COALESCE(b.state, '')::text AS blob_state, COALESCE(b.size, -1)::bigint AS blob_size
  FROM (SELECT unnest(sqlc.arg(keys)::text[]) AS object_key, unnest(sqlc.arg(digests)::bytea[]) AS sha256) AS k
  LEFT JOIN blob_copies c ON c.connection_id = sqlc.arg(connection_id)::text AND c.object_key = k.object_key
  LEFT JOIN blobs b ON b.sha256 = k.sha256;

-- name: SweepAdoptCopy :execrows
-- The adoption of an object of a live blob without a copy row (R-STOR-026), in the transaction that holds the blob's mark lock
-- (LockBlobForMark): present and not verified, or corrupt when its size is not the blob's stored size. created_at is when the store
-- says the object was written. On an encrypted connection storage_key_id is the storage key its record names (R-STOR-087); empty is
-- NULL. A corrupt one names the sweep and the size as its finding's source and reason (storage.md R-STOR-108).
INSERT INTO blob_copies (sha256, connection_id, object_key, stored_size, storage_key_id, state, created_at, checked_at, state_changed_at,
                         damage_source, damage_reason)
VALUES (sqlc.arg(sha256)::bytea, sqlc.arg(connection_id)::text, sqlc.arg(object_key)::text, sqlc.arg(stored_size)::bigint,
        NULLIF(sqlc.arg(storage_key_id)::text, ''), sqlc.arg(state)::text, to_timestamp(sqlc.arg(written_unix)::float8), now(), now(),
        CASE WHEN sqlc.arg(state)::text = 'corrupt' THEN 'sweep' END, CASE WHEN sqlc.arg(state)::text = 'corrupt' THEN 'size' END)
ON CONFLICT DO NOTHING;

-- name: SweepClaimOrphan :one
-- The claim of an orphan (R-STOR-026, Q-STOR-238): a blob row in state deleting, whose digests other than sha256 are zero bytes of
-- their length and whose size is the listed size (a deleting row never counts in the physical totals, R-STOR-003), and the copy row
-- of the object in state deleting, so that collection's step (4) finishes a delete that a sweep left. 0 when the blob row exists:
-- the object is left for the next run.
WITH claimed AS (
    INSERT INTO blobs (sha256, sha1, md5, sha512, size, state, state_changed_at)
    VALUES (sqlc.arg(sha256)::bytea, decode(repeat('00', 20), 'hex'), decode(repeat('00', 16), 'hex'), decode(repeat('00', 64), 'hex'),
            sqlc.arg(size)::bigint, 'deleting', now())
    ON CONFLICT DO NOTHING
    RETURNING sha256
), copied AS (
    INSERT INTO blob_copies (sha256, connection_id, object_key, stored_size, state, created_at, state_changed_at)
    SELECT sha256, sqlc.arg(connection_id)::text, sqlc.arg(object_key)::text, sqlc.arg(size)::bigint, 'deleting',
           to_timestamp(sqlc.arg(written_unix)::float8), now()
      FROM claimed
    ON CONFLICT DO NOTHING
)
SELECT count(*)::bigint FROM claimed;

-- name: SweepResumePoint :one
-- The checkpoint a sweep with resume: true starts at (R-STOR-026, Q-STOR-261): that of the most recent orphan-sweep job with the
-- same connection and dry run that ended cancelled, failed or paused, when it ended after the most recent one with the same
-- arguments that walked to its end. 'null' when there is none.
SELECT COALESCE((
    SELECT j.checkpoint FROM job j
     WHERE j.kind = 'orphan-sweep' AND j.checkpoint IS NOT NULL
       AND COALESCE(j.args ->> 'connection', '') = sqlc.arg(connection)::text
       AND COALESCE((j.args ->> 'dryRun')::boolean, false) = sqlc.arg(dry_run)::boolean
       AND (j.state IN ('cancelled', 'failed') OR (j.state = 'succeeded' AND COALESCE(j.result ->> 'paused', 'false') = 'true'))
       AND j.finished_at > COALESCE((
             SELECT max(c.finished_at) FROM job c
              WHERE c.kind = 'orphan-sweep' AND c.state = 'succeeded' AND COALESCE(c.result ->> 'paused', 'false') <> 'true'
                AND COALESCE(c.args ->> 'connection', '') = sqlc.arg(connection)::text
                AND COALESCE((c.args ->> 'dryRun')::boolean, false) = sqlc.arg(dry_run)::boolean), '-infinity'::timestamptz)
     ORDER BY j.finished_at DESC
     LIMIT 1), 'null'::jsonb)::jsonb AS checkpoint;
