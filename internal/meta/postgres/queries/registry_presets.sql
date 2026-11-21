-- Queries of the installation's registry presets (R-REPO-076). The list is read in a transaction of its own by the admin page and by
-- getRepositoryDefaults, and cached nowhere; the write replaces it under the row lock.

-- name: RegistryPresetsGet :many
-- The list and its version; the migration inserts the row, so a read always finds it.
SELECT presets, version FROM registry_presets WHERE singleton;

-- name: RegistryPresetsLock :many
-- The same row under its lock, for the write of the list.
SELECT presets, version FROM registry_presets WHERE singleton FOR UPDATE;

-- name: RegistryPresetsSet :many
UPDATE registry_presets
   SET presets = sqlc.arg(presets)::jsonb, version = version + 1, updated_at = now(),
       updated_by = NULLIF(sqlc.arg(actor)::text, '')::uuid
 WHERE singleton
RETURNING version;
