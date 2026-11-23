-- The keys of encrypted stored content (storage.md R-STOR-083 to R-STOR-085, R-STOR-097 to R-STOR-103): the statements of
-- internal/blob/keys. A sealed value is read and written as it lies; nothing here opens one. A destroyed key's sealed value is NULL and
-- is read as the empty string.

-- name: StorageKeysOfConnection :many
-- The rows of one connection, for a replica that holds its keys (R-STOR-083) and for the drawing at first sight.
SELECT key_id, connection_id, kind, generation, COALESCE(sealed_key, '')::text AS sealed_key, state, created_at, retired_at, destroyed_at
  FROM storage_keys
 WHERE connection_id = sqlc.arg(connection_id)::text
 ORDER BY kind, generation;

-- name: StorageKeysForExport :many
-- The rows of every connection, or of the one named, for the export and the import (R-STOR-085, R-STOR-103).
SELECT key_id, connection_id, kind, generation, COALESCE(sealed_key, '')::text AS sealed_key, state, created_at, retired_at, destroyed_at
  FROM storage_keys
 WHERE sqlc.arg(connection_id)::text = '' OR connection_id = sqlc.arg(connection_id)::text
 ORDER BY connection_id, kind, generation;

-- name: InsertStorageKey :execrows
-- A key drawn or imported, or a destroyed row an import or the journal's replay inserts (R-STOR-103, R-HA-054); created_at, retired_at
-- and destroyed_at are RFC 3339 text, empty for now() and for NULL, and an empty sealed value is NULL. A row that a unique constraint
-- already holds (the identifier, the connection's generation of the kind, or its active key of the kind) wins: the statement inserts
-- nothing, and the caller reads the winner's (R-STOR-083).
INSERT INTO storage_keys (key_id, connection_id, kind, generation, sealed_key, state, created_at, retired_at, destroyed_at)
VALUES (sqlc.arg(key_id)::text, sqlc.arg(connection_id)::text, sqlc.arg(kind)::text, sqlc.arg(generation)::integer,
        NULLIF(sqlc.arg(sealed_key)::text, ''), sqlc.arg(state)::text, COALESCE(NULLIF(sqlc.arg(created_at)::text, '')::timestamptz, now()),
        NULLIF(sqlc.arg(retired_at)::text, '')::timestamptz, NULLIF(sqlc.arg(destroyed_at)::text, '')::timestamptz)
ON CONFLICT DO NOTHING;

-- name: LockStorageKeysOfKind :many
-- The rows of one kind of a connection, locked, for a new generation (R-STOR-083) and the import's change of the active key: two such
-- writers on one connection run one after the other.
SELECT key_id, generation, state, COALESCE(sealed_key, '')::text AS sealed_key,
       COALESCE((EXTRACT(EPOCH FROM destroyed_at) * 1000000)::bigint, 0)::bigint AS destroyed_us
  FROM storage_keys
 WHERE connection_id = sqlc.arg(connection_id)::text AND kind = sqlc.arg(kind)::text
 ORDER BY generation
   FOR UPDATE;

-- name: LockStorageKeysOfConnection :many
-- The rows of both kinds of a connection, locked, for the destruction (R-STOR-098, R-STOR-099): a new generation, an import and a second
-- destruction of the connection wait for it.
SELECT key_id, connection_id, kind, generation, COALESCE(sealed_key, '')::text AS sealed_key, state, created_at, retired_at, destroyed_at
  FROM storage_keys
 WHERE connection_id = sqlc.arg(connection_id)::text
 ORDER BY kind, generation
   FOR UPDATE;

-- name: RetireStorageKey :execrows
-- An active key becomes retired: it opens what it wrapped and wraps nothing new.
UPDATE storage_keys SET state = 'retired', retired_at = now()
 WHERE key_id = sqlc.arg(key_id)::text AND state = 'active';

-- name: ActivateStorageKey :execrows
-- A retired key becomes active again: the import's highest generation of a connection (Q-STOR-325). A destroyed key never does.
UPDATE storage_keys SET state = 'active', retired_at = NULL
 WHERE key_id = sqlc.arg(key_id)::text AND state = 'retired';

-- name: DestroyStorageKeysOfConnection :many
-- The destruction of every key of a connection that is not destroyed yet (R-STOR-099 step 4): the sealed value goes, the row stays.
UPDATE storage_keys
   SET state = 'destroyed', sealed_key = NULL, destroyed_at = now(), retired_at = COALESCE(retired_at, now())
 WHERE connection_id = sqlc.arg(connection_id)::text AND state <> 'destroyed'
RETURNING key_id;

-- name: DestroyStorageKey :execrows
-- The destruction of one key by the journal's replay (R-HA-054, Q-HA-416), at the time it carries (RFC 3339 text).
UPDATE storage_keys
   SET state = 'destroyed', sealed_key = NULL, destroyed_at = sqlc.arg(destroyed_at)::text::timestamptz,
       retired_at = COALESCE(retired_at, sqlc.arg(destroyed_at)::text::timestamptz)
 WHERE key_id = sqlc.arg(key_id)::text AND state <> 'destroyed';

-- name: StorageKeysSealedAfter :many
-- The re-encryption walk over storage_keys.sealed_key (R-ARCH-021, R-STOR-084): a batch after the identifier, locked. A destroyed key has
-- no sealed value and is not walked (R-STOR-097).
SELECT key_id, COALESCE(sealed_key, '')::text AS sealed_key
  FROM storage_keys
 WHERE key_id > sqlc.arg(after_id)::text AND sealed_key IS NOT NULL
 ORDER BY key_id
 LIMIT sqlc.arg(batch)::bigint
   FOR UPDATE;

-- name: UpdateStorageKeySealed :exec
UPDATE storage_keys SET sealed_key = sqlc.arg(sealed)::text WHERE key_id = sqlc.arg(key_id)::text AND state <> 'destroyed';

-- name: SessionDataKeysSealedAfter :many
-- The re-encryption walk over upload_sessions.sealed_data_key (R-ARCH-021, R-STOR-081): a batch after the id, locked. The text of the
-- id is the row key of the seal; the order is the uuid's.
SELECT id::text AS id, sealed_data_key::text AS sealed_data_key
  FROM upload_sessions
 WHERE sealed_data_key IS NOT NULL AND id > sqlc.arg(after_id)::text::uuid
 ORDER BY id
 LIMIT sqlc.arg(batch)::bigint
   FOR UPDATE;

-- name: UpdateSessionDataKeySealed :exec
UPDATE upload_sessions SET sealed_data_key = sqlc.arg(sealed)::text
 WHERE id = sqlc.arg(id)::text::uuid AND sealed_data_key IS NOT NULL;

-- name: InsertStorageKeyExport :exec
-- The record of an export (R-STOR-103, Q-STOR-408), written in the export's transaction.
INSERT INTO storage_key_exports (instance, key_ids) VALUES (sqlc.arg(instance)::text, sqlc.arg(key_ids)::text[]);

-- name: StorageConnectionDestroyedUs :one
-- When the storage keys of a connection id were destroyed, in microseconds since the epoch, or 0 when none was (R-STOR-102): the start
-- refuses an id with a destroyed key, also after a restore emptied storage_connections.
SELECT COALESCE((EXTRACT(EPOCH FROM max(destroyed_at)) * 1000000)::bigint, 0)::bigint AS destroyed_us
  FROM storage_keys
 WHERE connection_id = sqlc.arg(connection_id)::text AND state = 'destroyed';

-- name: LockStorageConnectionForDestroy :many
-- The row of a connection, locked, with the times its state is read from (R-STOR-098): none after a restore of a backup, which records
-- only the restoring configuration's connections.
SELECT COALESCE((EXTRACT(EPOCH FROM filled_at) * 1000000)::bigint, 0)::bigint AS filled_us,
       COALESCE((EXTRACT(EPOCH FROM drained_at) * 1000000)::bigint, 0)::bigint AS drained_us,
       COALESCE((EXTRACT(EPOCH FROM retired_at) * 1000000)::bigint, 0)::bigint AS retired_us
  FROM storage_connections
 WHERE id = sqlc.arg(id)::text
   FOR UPDATE;

-- name: StorageKeysLiveReplicas :one
-- How many live replicas have the connection in their configuration (R-STOR-098): the replica registry's object-store view, rows whose
-- heartbeat is younger than stale_secs, as StorageConnectionConfiguredLive reads it.
SELECT count(*)::bigint AS replicas
  FROM replica r, jsonb_array_elements(COALESCE(r.object_store -> 'connections', '[]'::jsonb)) c
 WHERE c ->> 'id' = sqlc.arg(id)::text AND r.heartbeat_at >= now() - make_interval(secs => sqlc.arg(stale_secs)::float8);

-- name: StorageKeysHolders :one
-- What still holds a connection's keys (R-STOR-098): copy rows of the connection or under one of its keys, queue rows, upload sessions;
-- and what the destruction reports: the exports that listed one of its keys, and the files whose data key was handed to a Bastion
-- within the audit trail's retention, as the distinct sha256 of the events bastion.data_key.handed of the connection or under one of
-- its keys, beside the count of those events (Q-STOR-475): a file handed twice is one file and two handovers.
SELECT (SELECT count(*) FROM blob_copies c
         WHERE c.connection_id = sqlc.arg(connection_id)::text OR c.storage_key_id = ANY (sqlc.arg(key_ids)::text[]))::bigint AS copies,
       (SELECT count(*) FROM blob_copy_queue q WHERE q.connection_id = sqlc.arg(connection_id)::text)::bigint AS queued,
       (SELECT count(*) FROM upload_sessions s WHERE s.connection_id = sqlc.arg(connection_id)::text)::bigint AS sessions,
       (SELECT count(*) FROM storage_key_exports e WHERE e.key_ids && sqlc.arg(key_ids)::text[])::bigint AS exports,
       COALESCE((SELECT (EXTRACT(EPOCH FROM max(e.exported_at)) * 1000000)::bigint FROM storage_key_exports e
                  WHERE e.key_ids && sqlc.arg(key_ids)::text[]), 0)::bigint AS last_export_us,
       h.files::bigint AS handed_files,
       h.events::bigint AS handovers
  FROM (SELECT count(DISTINCT a.details ->> 'sha256') AS files, count(*) AS events FROM audit_event a
         WHERE a.action = 'bastion.data_key.handed'
           AND (a.details ->> 'connection' = sqlc.arg(connection_id)::text
                OR a.details ->> 'storageKeyId' = ANY (sqlc.arg(key_ids)::text[]))) h;

-- name: StorageKeysBackupRuns :one
-- The backup runs the check of R-STOR-100 reads: how many create and delete runs the database records and whether a target-writing run is
-- active (the rows of backup_run_one_active).
SELECT count(*) FILTER (WHERE operation IN ('create', 'delete'))::bigint AS runs,
       count(*) FILTER (WHERE operation IN ('create', 'delete')
                          AND state IN ('starting', 'snapshot', 'copying', 'finalizing', 'pruning'))::bigint AS active
  FROM backup_run;

-- name: StorageKeysNewestListing :many
-- The newest finished create or delete run and its result (R-STOR-100, Q-STOR-403): its retention listing decides.
SELECT id::text AS id, target, COALESCE(result, 'null'::jsonb) AS result,
       COALESCE((EXTRACT(EPOCH FROM finished_at) * 1000000)::bigint, 0)::bigint AS finished_us
  FROM backup_run
 WHERE operation IN ('create', 'delete') AND finished_at IS NOT NULL
 ORDER BY finished_at DESC, id DESC
 LIMIT 1;
