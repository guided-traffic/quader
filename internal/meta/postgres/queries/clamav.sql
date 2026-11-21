-- The statements of the clamav format's data port (internal/formatdata/clamav; formats/clamav.md R-CLAMAV-002 to R-CLAMAV-004,
-- R-CLAMAV-007; malware-scanning.md R-MAL-010, R-MAL-034): the side rows of clamav_version (migration 00094). The package is a domain
-- package and imports no driver types, so a time travels as text cast to timestamptz.

-- name: ClamavPutContainer :exec
-- The metadata of a version from its container's commit; a row its detached signature made first keeps the signature's facts.
INSERT INTO clamav_version (version_id, db, version, file, built_at, metadata)
VALUES (sqlc.arg(version_id)::bigint, sqlc.arg(db)::text, sqlc.arg(version)::int, sqlc.arg(file)::text,
        sqlc.arg(built_at)::text::timestamptz, sqlc.arg(metadata)::jsonb)
ON CONFLICT (version_id) DO UPDATE
   SET file = EXCLUDED.file, built_at = EXCLUDED.built_at, metadata = EXCLUDED.metadata, files_removed_at = NULL;

-- name: ClamavPutSignature :exec
-- The detached signature of a version; a version without a row gets one, which its container's commit fills.
INSERT INTO clamav_version (version_id, db, version, sign_name, sign_size, sign_signatures)
VALUES (sqlc.arg(version_id)::bigint, sqlc.arg(db)::text, sqlc.arg(version)::int, sqlc.arg(sign_name)::text,
        sqlc.arg(sign_size)::bigint, sqlc.arg(sign_signatures)::int)
ON CONFLICT (version_id) DO UPDATE
   SET sign_name = EXCLUDED.sign_name, sign_size = EXCLUDED.sign_size, sign_signatures = EXCLUDED.sign_signatures;

-- name: ClamavFilesRemoved :exec
-- The cleanup removed the containers of these versions (R-MAL-034): the rows stay as history, without the marks of a set.
UPDATE clamav_version
   SET files_removed_at = now(), in_use = false, previous = false
 WHERE version_id = ANY (sqlc.arg(version_ids)::bigint[]) AND files_removed_at IS NULL;

-- name: ClamavServed :many
-- The version of a database the set in use names in a repository, and its container's file name (R-CLAMAV-004).
SELECT c.version, COALESCE(c.file, '')::text AS file
  FROM clamav_version c
  JOIN package_versions v ON v.id = c.version_id AND v.trashed_at IS NULL
  JOIN packages p ON p.id = v.package_id
 WHERE p.repo_id = sqlc.arg(repo_id)::bigint AND p.name = sqlc.arg(db)::text AND c.in_use AND c.file IS NOT NULL
 ORDER BY c.version DESC
 LIMIT 1;

-- name: ClamavVersions :many
-- The side rows of the given versions.
SELECT c.version_id, c.db, c.version, COALESCE(c.file, '')::text AS file, c.metadata,
       COALESCE(c.sign_name, '')::text AS sign_name, COALESCE(c.sign_size, 0)::bigint AS sign_size,
       COALESCE(c.sign_signatures, 0)::int AS sign_signatures, c.in_use, c.previous, c.stored_at, c.files_removed_at
  FROM clamav_version c
 WHERE c.version_id = ANY (sqlc.arg(version_ids)::bigint[])
 ORDER BY c.version_id;

-- name: ClamavStored :many
-- The versions of a database in a repository (R-CLAMAV-007): the stored ones newest first, then the history rows without files.
SELECT c.version_id, c.db, c.version, COALESCE(c.file, '')::text AS file, c.metadata,
       COALESCE(c.sign_name, '')::text AS sign_name, COALESCE(c.sign_size, 0)::bigint AS sign_size,
       COALESCE(c.sign_signatures, 0)::int AS sign_signatures, c.in_use, c.previous, c.stored_at, c.files_removed_at
  FROM clamav_version c
  JOIN package_versions v ON v.id = c.version_id AND v.trashed_at IS NULL
  JOIN packages p ON p.id = v.package_id
 WHERE p.repo_id = sqlc.arg(repo_id)::bigint AND p.name = sqlc.arg(db)::text
 ORDER BY (c.files_removed_at IS NOT NULL), c.version DESC
 LIMIT sqlc.arg(max_rows)::int;

-- name: ClamavList :many
-- Every side row of a repository, by database and version: what the database task's cleanup decides on (R-MAL-034).
SELECT c.version_id, c.db, c.version, COALESCE(c.file, '')::text AS file, c.metadata,
       COALESCE(c.sign_name, '')::text AS sign_name, COALESCE(c.sign_size, 0)::bigint AS sign_size,
       COALESCE(c.sign_signatures, 0)::int AS sign_signatures, c.in_use, c.previous, c.stored_at, c.files_removed_at
  FROM clamav_version c
  JOIN package_versions v ON v.id = c.version_id AND v.trashed_at IS NULL
  JOIN packages p ON p.id = v.package_id
 WHERE p.repo_id = sqlc.arg(repo_id)::bigint
 ORDER BY c.db, c.version;

-- name: ClamavMarkSets :exec
-- The marks of the set in use and of the one before it (R-MAL-010 step 3, a put-back): each a list of "<db>:<version>"; every other
-- version of the repository loses its marks. A version whose files are gone takes none.
UPDATE clamav_version c
   SET in_use = (c.db || ':' || c.version::text) = ANY (sqlc.arg(in_use)::text[]) AND c.files_removed_at IS NULL,
       previous = (c.db || ':' || c.version::text) = ANY (sqlc.arg(previous)::text[]) AND c.files_removed_at IS NULL
  FROM package_versions v
  JOIN packages p ON p.id = v.package_id
 WHERE v.id = c.version_id AND p.repo_id = sqlc.arg(repo_id)::bigint;

-- name: ClamavDeleteHistory :many
-- The end of the history (R-MAL-034): the version rows whose files went before the time, with their side rows; it returns the packages,
-- whose revision the caller raises.
DELETE FROM package_versions v
 USING clamav_version c, packages p
 WHERE c.version_id = v.id AND p.id = v.package_id AND p.repo_id = sqlc.arg(repo_id)::bigint
   AND c.files_removed_at < sqlc.arg(before)::text::timestamptz
RETURNING v.package_id;
