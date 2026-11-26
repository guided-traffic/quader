-- The reading of a stored package's file list (formats/nuget.md R-NUGET-038, R-NUGET-039): the frameworks of its folders and the files
-- that run code when a project uses it, kept as the member contents of nuget_version.metadata. The ingestion and a remote's fill write
-- it; the job nuget-contents writes it for a version stored before, which the task remote-describe selects.

-- name: NuGetUnreadContents :many
-- Live versions after the cursor whose file list was never read, in id order, at most row_limit.
SELECT nv.version_id
  FROM nuget_version nv
  JOIN package_versions pv ON pv.id = nv.version_id
 WHERE nv.version_id > sqlc.arg(after)::bigint
   AND pv.trashed_at IS NULL
   AND NOT (nv.metadata ? 'contents')
 ORDER BY nv.version_id
 LIMIT sqlc.arg(row_limit)::int;

-- name: NuGetContentsTarget :many
-- The ID and the stored package of a live version whose file list was never read: none when it is read, gone or has no package.
SELECT nv.lower_id,
       i.blob_sha256,
       b.size
  FROM nuget_version nv
  JOIN package_versions pv ON pv.id = nv.version_id
  JOIN items i ON i.id = pv.lead_item_id
  JOIN blobs b ON b.sha256 = i.blob_sha256
 WHERE nv.version_id = sqlc.arg(version_id)::bigint
   AND pv.trashed_at IS NULL
   AND NOT (nv.metadata ? 'contents');

-- name: NuGetPutContents :execrows
-- Writes what the file list of a version says, once: a version read meanwhile (by its ingestion's twin, a second job) keeps its own.
UPDATE nuget_version
   SET metadata = jsonb_set(metadata, '{contents}', sqlc.arg(contents)::jsonb)
 WHERE version_id = sqlc.arg(version_id)::bigint
   AND NOT (metadata ? 'contents');
