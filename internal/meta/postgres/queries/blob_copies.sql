-- The statements of the copies of a blob and of the object-store connections (storage.md R-STOR-050 to R-STOR-055,
-- R-STOR-060, R-STOR-062). Queries that may find no row are :many.

-- name: BlobCopies :many
-- The copies of one blob with whether their connection is retired, whose copy counts for nothing (R-STOR-052).
SELECT c.connection_id, c.object_key, c.stored_size, COALESCE(c.storage_key_id, '')::text AS storage_key_id, c.state,
       c.verified_at, c.checked_at, (sc.retired_at IS NOT NULL)::boolean AS retired
  FROM blob_copies c
  JOIN storage_connections sc ON sc.id = c.connection_id
 WHERE c.sha256 = sqlc.arg(sha256)::bytea
 ORDER BY c.connection_id;

-- name: LockBlobForMark :many
-- The lock of a statement that changes a copy and with it the blob's content state (R-STOR-052): FOR NO KEY UPDATE conflicts
-- with itself and with the commit's FOR SHARE, so two marks of one blob, and a mark and a commit that inserts a copy, derive
-- one after the other and never from each other's stale view.
SELECT state FROM blobs WHERE sha256 = sqlc.arg(sha256)::bytea FOR NO KEY UPDATE;

-- name: PutBlobCopy :exec
-- The copy of an object a commit wrote (R-STOR-053): inserted present, or a missing, corrupt or present row overwritten; a copy
-- claimed for deletion is left to collection. verified_at is now when the write showed the whole object to be the blob. On an
-- encrypted connection storage_key_id names the storage key that wrapped the object's data key (R-STOR-080); empty is NULL.
INSERT INTO blob_copies (sha256, connection_id, object_key, stored_size, storage_key_id, state, created_at, verified_at, checked_at, state_changed_at)
VALUES (sqlc.arg(sha256)::bytea, sqlc.arg(connection_id)::text, sqlc.arg(object_key)::text, sqlc.arg(stored_size)::bigint,
        NULLIF(sqlc.arg(storage_key_id)::text, ''), 'present', now(), CASE WHEN sqlc.arg(verified)::boolean THEN now() END, now(), now())
ON CONFLICT (sha256, connection_id) DO UPDATE
   SET object_key = EXCLUDED.object_key, stored_size = EXCLUDED.stored_size, storage_key_id = EXCLUDED.storage_key_id, created_at = EXCLUDED.created_at,
       verified_at = COALESCE(EXCLUDED.verified_at, CASE WHEN blob_copies.state = 'present' THEN blob_copies.verified_at END),
       checked_at = EXCLUDED.checked_at,
       state_changed_at = CASE WHEN blob_copies.state = 'present' THEN blob_copies.state_changed_at ELSE EXCLUDED.state_changed_at END,
       state = 'present'
 WHERE blob_copies.state <> 'deleting';

-- name: SetBlobCopyState :execrows
-- Evidence about one copy (R-STOR-055): a copy in state from moves to state to; any other state is left as it is, so a repair or
-- a concurrent detection is not overwritten.
UPDATE blob_copies SET state = sqlc.arg(to_state)::text, state_changed_at = now(), checked_at = now()
 WHERE sha256 = sqlc.arg(sha256)::bytea AND connection_id = sqlc.arg(connection_id)::text AND state = sqlc.arg(from_state)::text;

-- name: StampBlobCopyVerified :exec
-- A full read through Quader matched the digest (Q-STOR-233): at most one write per copy and interval.
UPDATE blob_copies SET verified_at = now(), checked_at = now()
 WHERE sha256 = sqlc.arg(sha256)::bytea AND connection_id = sqlc.arg(connection_id)::text AND state = 'present'
   AND (verified_at IS NULL OR verified_at < now() - make_interval(secs => sqlc.arg(interval_seconds)::double precision));

-- name: QueueBlobCopy :exec
-- A gap of a connection for the reconciliation (R-STOR-056).
INSERT INTO blob_copy_queue (connection_id, sha256) VALUES (sqlc.arg(connection_id)::text, sqlc.arg(sha256)::bytea)
ON CONFLICT DO NOTHING;

-- name: SetBlobContentStatus :execrows
-- The blob's content state as its copies give it (R-STOR-052). Leaving ok stamps checked_at, which starts the recheck interval
-- of a blob without a present copy (Q-STOR-153).
UPDATE blobs SET content_status = sqlc.arg(status)::text,
       checked_at = CASE WHEN sqlc.arg(status)::text <> 'ok' THEN clock_timestamp() ELSE checked_at END
 WHERE sha256 = sqlc.arg(sha256)::bytea AND state = 'live' AND content_status <> sqlc.arg(status)::text;

-- name: StorageConnections :many
-- Every connection row (R-STOR-051), in id order.
SELECT id, bucket, prefix, encrypted, capabilities::text AS capabilities, first_seen_at, filled_at, drained_at, retired_at,
       last_full_pass_at
  FROM storage_connections ORDER BY id;

-- name: InsertStorageConnection :execrows
-- The first sight of a connection (R-STOR-051), with its fixed properties; keyed_names is the detail of an encrypted one (R-STOR-075).
INSERT INTO storage_connections (id, bucket, prefix, encrypted, keyed_names)
VALUES (sqlc.arg(id)::text, sqlc.arg(bucket)::text, sqlc.arg(prefix)::text, sqlc.arg(encrypted)::boolean, sqlc.arg(keyed_names)::boolean)
ON CONFLICT (id) DO NOTHING;

-- name: LockStorageConnection :many
-- The row of a connection while a replica compares its fixed properties with its element; the lock makes two replicas that
-- start at once compare one after the other.
SELECT id, bucket, prefix, encrypted, keyed_names, retired_at FROM storage_connections WHERE id = sqlc.arg(id)::text FOR UPDATE;

-- name: StorageConnectionCopies :one
-- How many copies a connection holds, for the refusal of a changed fixed property (R-STOR-051).
SELECT count(*)::bigint FROM blob_copies WHERE connection_id = sqlc.arg(id)::text;

-- name: StorageConnectionHoldsCopy :one
SELECT EXISTS (SELECT 1 FROM blob_copies WHERE connection_id = sqlc.arg(id)::text)::boolean;

-- name: UpdateStorageConnectionLocation :exec
-- A connection that holds no copy takes the bucket, prefix, encrypted and keyed_names of its element (R-STOR-051, R-STOR-075).
UPDATE storage_connections SET bucket = sqlc.arg(bucket)::text, prefix = sqlc.arg(prefix)::text, encrypted = sqlc.arg(encrypted)::boolean,
       keyed_names = sqlc.arg(keyed_names)::boolean
 WHERE id = sqlc.arg(id)::text;

-- name: PrefixUnboundCopyKeys :execrows
-- The copies the migration of the connections recorded without a prefix (00106, Q-STOR-250) get the prefix of the element that
-- binds the row.
UPDATE blob_copies SET object_key = sqlc.arg(prefix)::text || '/' || object_key
 WHERE connection_id = sqlc.arg(id)::text AND object_key LIKE 'sha256/%';

-- name: SetStorageConnectionCapabilities :exec
-- What the start check found (R-STOR-035).
UPDATE storage_connections SET capabilities = sqlc.arg(capabilities)::text::jsonb WHERE id = sqlc.arg(id)::text;

-- name: StorageConnectionConfiguredLive :one
-- Whether a live replica has the connection in its configuration: the replica registry's object-store view (R-OPS-003), rows
-- whose heartbeat is younger than stale_secs.
SELECT EXISTS (
    SELECT 1 FROM replica r, jsonb_array_elements(COALESCE(r.object_store -> 'connections', '[]'::jsonb)) c
     WHERE c ->> 'id' = sqlc.arg(id)::text AND r.heartbeat_at >= now() - make_interval(secs => sqlc.arg(stale_secs)::float8)
)::boolean;

-- name: ConnectionCopyPage :many
-- A page of the copies of a connection by object name, for forget-connection (R-STOR-051).
SELECT sha256 FROM blob_copies WHERE connection_id = sqlc.arg(id)::text ORDER BY object_key LIMIT sqlc.arg(page_size)::int;

-- name: DeleteBlobCopy :execrows
DELETE FROM blob_copies WHERE sha256 = sqlc.arg(sha256)::bytea AND connection_id = sqlc.arg(connection_id)::text;

-- name: DeleteConnectionQueue :execrows
DELETE FROM blob_copy_queue WHERE connection_id = sqlc.arg(id)::text;

-- name: RetireStorageConnection :exec
UPDATE storage_connections SET retired_at = now() WHERE id = sqlc.arg(id)::text AND retired_at IS NULL;
