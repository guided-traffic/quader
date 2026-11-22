-- The statements of the blob store (R-STOR-005, R-STOR-024, R-STOR-013). Queries
-- that may find no row are :many, so that the caller needs no driver error
-- value to tell an empty result from a failure.

-- name: LockBlobShare :many
-- The first statement of the commit protocol (R-STOR-005).
SELECT state, content_status FROM blobs WHERE sha256 = $1 FOR SHARE;

-- name: InsertLiveBlob :execrows
INSERT INTO blobs (sha256, sha1, md5, sha512, size, state)
VALUES ($1, $2, $3, $4, $5, 'live')
ON CONFLICT DO NOTHING;

-- name: BlobExists :many
SELECT sha256 FROM blobs WHERE sha256 = $1;

-- name: EnqueueBlobForGC :exec
INSERT INTO blob_gc_queue (sha256, enqueued_at) VALUES ($1, now())
ON CONFLICT (sha256) DO UPDATE SET enqueued_at = EXCLUDED.enqueued_at;

-- name: GetBlob :many
SELECT sha256, sha1, md5, sha512, size, state, content_status, created_at
  FROM blobs WHERE sha256 = $1;

-- name: ClaimMissingRecheck :execrows
-- The recheck of a blob marked missing (Q-STOR-153, R-STOR-055): the statement that moves checked_at, the recheck claim of a
-- blob without a present copy, claims the one look at the store of the interval for every replica; a read inside the interval
-- matches nothing and does not ask the store.
UPDATE blobs SET checked_at = clock_timestamp()
 WHERE sha256 = sqlc.arg(sha256) AND state = 'live' AND content_status = 'missing'
   AND (checked_at IS NULL OR checked_at <= clock_timestamp() - make_interval(secs => sqlc.arg(interval_seconds)::double precision));

-- name: DeletionsPaused :many
SELECT 1::int AS paused FROM system_state
 WHERE key = 'deletions_paused_at' AND value <> 'null'::jsonb;

-- name: BlobStateSize :many
SELECT state, size FROM blobs WHERE sha256 = $1;

-- name: LockBlobUpdate :many
-- Step (1) of the collection protocol: the row lock that makes a committing
-- uploader and the collector see each other.
SELECT state, size FROM blobs WHERE sha256 = $1 FOR UPDATE;

-- name: ClaimBlob :exec
-- Step (3).
UPDATE blobs SET state = 'deleting', state_changed_at = now() WHERE sha256 = $1;

-- name: DequeueBlob :exec
-- Removes the queue entry only if it is the one that was read: an entry that was
-- enqueued again in between stays.
DELETE FROM blob_gc_queue WHERE sha256 = $1 AND enqueued_at = $2;

-- name: BlobQueueCandidates :many
SELECT sha256, enqueued_at FROM blob_gc_queue
 WHERE enqueued_at < now() - make_interval(secs => @grace_secs::float8) AND sha256 > @after
 ORDER BY sha256 LIMIT @batch;

-- name: RecordBlobsDeleted :many
-- Step (5), for a blob without a copy row left (R-STOR-057).
UPDATE blobs SET object_deleted_at = now()
 WHERE sha256 = ANY(@digests::bytea[]) AND state = 'deleting' AND object_deleted_at IS NULL
   AND NOT EXISTS (SELECT 1 FROM blob_copies c WHERE c.sha256 = blobs.sha256)
RETURNING sha256, size;

-- name: DropBlobRows :exec
-- Step (6), with the blob's copy and queue rows, which carry no foreign key to blobs (R-STOR-052): a queue row is found by its
-- primary key through the at most eight connections.
WITH dropped AS (
    DELETE FROM blobs
     WHERE sha256 = ANY(@digests::bytea[]) AND state = 'deleting'
       AND object_deleted_at < now() - make_interval(secs => @settle_secs::float8)
    RETURNING sha256
), copies AS (
    DELETE FROM blob_copies WHERE sha256 IN (SELECT sha256 FROM dropped)
)
DELETE FROM blob_copy_queue
 WHERE (connection_id, sha256) IN (SELECT sc.id, d.sha256 FROM storage_connections sc CROSS JOIN dropped d);

-- name: EligibleBlobs :many
-- The rows earlier runs left: object deleted and settled (step 6), or claimed and
-- not finished within handover_delay (steps 4 to 6).
SELECT sha256, size, (object_deleted_at IS NOT NULL)::boolean AS object_deleted FROM blobs
 WHERE state = 'deleting' AND (
       (object_deleted_at IS NULL AND state_changed_at < now() - make_interval(secs => @handover_secs::float8))
    OR (object_deleted_at IS NOT NULL AND object_deleted_at < now() - make_interval(secs => @settle_secs::float8)))
 ORDER BY state_changed_at LIMIT @batch;

-- name: TakeOverBlobs :many
-- Compare-and-set claim of rows another run left: the row is taken only while it
-- is still as old as the run saw it, and taking it renews state_changed_at, so
-- that of two runs that read the same row exactly one takes it and the other
-- finds it fresh (READ COMMITTED re-evaluates the condition after the lock wait).
UPDATE blobs SET state_changed_at = now()
 WHERE sha256 = ANY(@digests::bytea[]) AND state = 'deleting' AND object_deleted_at IS NULL
   AND state_changed_at < now() - make_interval(secs => @handover_secs::float8)
RETURNING sha256, size;

-- name: CountDeletingSoon :one
SELECT count(*) FROM blobs
 WHERE state = 'deleting' AND (object_deleted_at IS NOT NULL
    OR state_changed_at < now() - make_interval(secs => @handover_secs::float8));

-- name: HandOverBlobs :exec
UPDATE blobs SET state_changed_at = now() - make_interval(secs => @back_secs::float8)
 WHERE sha256 = ANY(@digests::bytea[]) AND state = 'deleting' AND object_deleted_at IS NULL;

-- name: CountDeletingBlobs :one
SELECT count(*) FROM blobs WHERE state = 'deleting';

-- name: BlobReferencingColumns :many
-- Every single-column foreign key to blobs (R-STOR-024 rule 3), so that a table
-- a newer release added is honoured by an older binary. The names are returned
-- unquoted; BlobRefs quotes them with pgx.Identifier.
SELECT n.nspname::text AS schema_name, cl.relname::text AS table_name, a.attname::text AS column_name
  FROM pg_catalog.pg_constraint c
  JOIN pg_catalog.pg_class cl ON cl.oid = c.conrelid
  JOIN pg_catalog.pg_namespace n ON n.oid = cl.relnamespace
  JOIN pg_catalog.pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
 WHERE c.contype = 'f' AND c.confrelid = 'blobs'::regclass AND cardinality(c.conkey) = 1
 ORDER BY n.nspname, cl.relname, a.attname;

-- name: VerifyBlobItems :many
-- The items that reference a blob whose object is missing or of another size, for the per-item report of
-- `quader admin storage verify`: project, repository and path, trashed ones included (a trashed item still holds its blob,
-- R-STOR-022).
SELECT p.key AS project_key, r.key AS repo_key, i.path, (i.trashed_at IS NOT NULL)::boolean AS trashed
  FROM items i
  JOIN repositories r ON r.id = i.repo_id
  JOIN project p ON p.id = r.project_id
 WHERE i.blob_sha256 = sqlc.arg(sha256)::bytea
 ORDER BY p.key, r.key, i.path
 LIMIT sqlc.arg(max_items)::int;

-- name: VerifyBlobItemCount :one
SELECT count(*)::bigint FROM items WHERE blob_sha256 = sqlc.arg(sha256)::bytea;
