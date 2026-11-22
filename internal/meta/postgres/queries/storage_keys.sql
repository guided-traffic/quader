-- The keys of encrypted stored content (storage.md R-STOR-083 to R-STOR-085): the statements of internal/blob/keys. A sealed value
-- is read and written as it lies; nothing here opens one.

-- name: StorageKeysOfConnection :many
-- The rows of one connection, for a replica that holds its keys (R-STOR-083) and for the drawing at first sight.
SELECT key_id, connection_id, kind, generation, sealed_key, state, created_at, retired_at
  FROM storage_keys
 WHERE connection_id = sqlc.arg(connection_id)::text
 ORDER BY kind, generation;

-- name: StorageKeysForExport :many
-- The rows of every connection, or of the one named, for the export and the import (R-STOR-085).
SELECT key_id, connection_id, kind, generation, sealed_key, state, created_at, retired_at
  FROM storage_keys
 WHERE sqlc.arg(connection_id)::text = '' OR connection_id = sqlc.arg(connection_id)::text
 ORDER BY connection_id, kind, generation;

-- name: InsertStorageKey :execrows
-- A key drawn or imported; created_at and retired_at are RFC 3339 text, empty for now() and for NULL. A row that a unique
-- constraint already holds (the identifier, the connection's generation of the kind, or its active key of the kind) wins: the
-- statement inserts nothing, and the caller reads the winner's (R-STOR-083).
INSERT INTO storage_keys (key_id, connection_id, kind, generation, sealed_key, state, created_at, retired_at)
VALUES (sqlc.arg(key_id)::text, sqlc.arg(connection_id)::text, sqlc.arg(kind)::text, sqlc.arg(generation)::integer,
        sqlc.arg(sealed_key)::text, sqlc.arg(state)::text, COALESCE(NULLIF(sqlc.arg(created_at)::text, '')::timestamptz, now()),
        NULLIF(sqlc.arg(retired_at)::text, '')::timestamptz)
ON CONFLICT DO NOTHING;

-- name: LockStorageKeysOfKind :many
-- The rows of one kind of a connection, locked, for a new generation (R-STOR-083) and the import's change of the active key: two such
-- writers on one connection run one after the other.
SELECT key_id, generation, state, sealed_key
  FROM storage_keys
 WHERE connection_id = sqlc.arg(connection_id)::text AND kind = sqlc.arg(kind)::text
 ORDER BY generation
   FOR UPDATE;

-- name: RetireStorageKey :execrows
-- An active key becomes retired: it opens what it wrapped and wraps nothing new.
UPDATE storage_keys SET state = 'retired', retired_at = now()
 WHERE key_id = sqlc.arg(key_id)::text AND state = 'active';

-- name: ActivateStorageKey :execrows
-- A retired key becomes active again: the import's highest generation of a connection (Q-STOR-325).
UPDATE storage_keys SET state = 'active', retired_at = NULL
 WHERE key_id = sqlc.arg(key_id)::text AND state = 'retired';

-- name: StorageKeysSealedAfter :many
-- The re-encryption walk over storage_keys.sealed_key (R-ARCH-021, R-STOR-084): a batch after the identifier, locked.
SELECT key_id, sealed_key
  FROM storage_keys
 WHERE key_id > sqlc.arg(after_id)::text
 ORDER BY key_id
 LIMIT sqlc.arg(batch)::bigint
   FOR UPDATE;

-- name: UpdateStorageKeySealed :exec
UPDATE storage_keys SET sealed_key = sqlc.arg(sealed)::text WHERE key_id = sqlc.arg(key_id)::text;

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
