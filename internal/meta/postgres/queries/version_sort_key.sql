-- The statements of the background data migration data_migration_version_sort_key (internal/packages/sortkey; R-HA-022, Q-HA-492,
-- Q-API-430): it fills the sort key and the pre-release flag of the version rows written without them, before migration 00116 or by a
-- replica of the release before it. The key is the format's code's (ViewAdapter.SortKey); these statements read the rows in keyset
-- batches, write what the caller computed and keep the completion in system_state.

-- name: VersionSortKeyBatch :many
-- The next batch of version rows without a key after the cursor, in id order (the index package_versions_unkeyed), with the format of
-- their repository, locked for the fill. A row another transaction holds is skipped: the fill never waits for a writer, and the check at
-- the end of a pass finds the row again. Trashed rows are read too, so that a version restored from the trash can has its key.
SELECT pv.id, pv.version::text AS version, r.format
  FROM package_versions pv
  JOIN packages p ON p.id = pv.package_id
  JOIN repositories r ON r.id = p.repo_id
 WHERE pv.sort_key IS NULL AND pv.id > sqlc.arg(after)::bigint
 ORDER BY pv.id
 LIMIT sqlc.arg(batch)::int
   FOR UPDATE OF pv SKIP LOCKED;

-- name: VersionSortKeyFill :execrows
-- Writes the keys and the flags of a batch, each row only while it has no key, so a run that repeats a batch changes nothing.
UPDATE package_versions pv
   SET sort_key = x.sort_key, prerelease = x.prerelease
  FROM (SELECT unnest(sqlc.arg(ids)::bigint[]) AS id, unnest(sqlc.arg(sort_keys)::bytea[]) AS sort_key,
               unnest(sqlc.arg(prereleases)::boolean[]) AS prerelease) AS x
 WHERE pv.id = x.id AND pv.sort_key IS NULL;

-- name: VersionSortKeyUnkeyed :one
-- Whether a version row without a key exists (the index package_versions_unkeyed: one probe).
SELECT EXISTS (SELECT 1 FROM package_versions WHERE sort_key IS NULL)::boolean AS unkeyed;

-- name: VersionSortKeyState :many
-- The completion of the fill: the system_state row of its key (R-OPS-024 is the table). No row: it has not completed.
SELECT value::text AS value
  FROM system_state
 WHERE key = sqlc.arg(key)::text;

-- name: VersionSortKeyDone :exec
-- Records the completion.
INSERT INTO system_state (key, value)
VALUES (sqlc.arg(key)::text, sqlc.arg(value)::text::jsonb)
ON CONFLICT (key) DO UPDATE
   SET value = EXCLUDED.value, updated_at = now();

-- name: VersionSortKeyReopen :exec
-- Removes the completion: rows without a key were written after it (a rollback to the release before 00116 and an upgrade again, a
-- restore of a backup taken before it), so the fill runs again and the report shows it pending until it has.
DELETE FROM system_state WHERE key = sqlc.arg(key)::text;
