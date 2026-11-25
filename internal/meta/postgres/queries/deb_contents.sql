-- name: DebContentsFiles :many
-- The live package files of a package in a repository (local or cached by a remote) whose data archive (R-DEB-039) the current reader has not read:
-- no row of deb_contents, or one an older reader wrote. What the job deb-contents reads, with the blob of each.
SELECT b.item_id, b.package, COALESCE(i.blob_sha256, ''::bytea)::bytea AS sha256, COALESCE(i.size, 0)::bigint AS size
  FROM deb_binaries b
  JOIN items i ON i.id = b.item_id AND i.trashed_at IS NULL
 WHERE b.repo_id = sqlc.arg(repo_id)::bigint AND b.package = sqlc.arg(package)::text
   AND NOT EXISTS (SELECT 1 FROM deb_contents c WHERE c.item_id = b.item_id AND c.reader >= sqlc.arg(reader)::int)
 ORDER BY b.item_id;

-- name: DebPutContents :execrows
-- Writes what the reader kept of a package file, or why it refused it; a file whose row of deb_binaries went meanwhile gets none.
INSERT INTO deb_contents (item_id, reader, state, reason, detail, changelog, changelog_file, changelog_truncated, copyright,
                          copyright_truncated, licenses, doc_link, files, file_count, read_at)
SELECT b.item_id, sqlc.arg(reader)::int, sqlc.arg(state)::text, sqlc.arg(reason)::text, sqlc.arg(detail)::text, sqlc.arg(changelog)::text,
       sqlc.arg(changelog_file)::text, sqlc.arg(changelog_truncated)::boolean, sqlc.arg(copyright)::text,
       sqlc.arg(copyright_truncated)::boolean, sqlc.arg(licenses)::text[], sqlc.arg(doc_link)::text, sqlc.arg(files)::jsonb,
       sqlc.arg(file_count)::int, now()
  FROM deb_binaries b
 WHERE b.item_id = sqlc.arg(item_id)::bigint
ON CONFLICT (item_id) DO UPDATE SET
    reader = EXCLUDED.reader, state = EXCLUDED.state, reason = EXCLUDED.reason, detail = EXCLUDED.detail,
    changelog = EXCLUDED.changelog, changelog_file = EXCLUDED.changelog_file, changelog_truncated = EXCLUDED.changelog_truncated,
    copyright = EXCLUDED.copyright, copyright_truncated = EXCLUDED.copyright_truncated, licenses = EXCLUDED.licenses,
    doc_link = EXCLUDED.doc_link, files = EXCLUDED.files, file_count = EXCLUDED.file_count, read_at = EXCLUDED.read_at;

-- name: DebUnreadPackages :many
-- The packages of Debian repositories after the cursor (by package id) that hold a live package file the current reader has not read
-- (R-DEB-039): what the task remote-describe queues the job deb-contents for, at most row_limit of them. A local repository's package
-- and a remote's are both packages rows; a virtual repository stores none.
SELECT p.id, p.repo_id, p.name
  FROM packages p
  JOIN repositories r ON r.id = p.repo_id AND r.format = 'deb'
 WHERE p.id > sqlc.arg(after)::bigint
   AND EXISTS (SELECT 1
                 FROM deb_binaries b
                 JOIN items i ON i.id = b.item_id AND i.trashed_at IS NULL
                WHERE b.repo_id = p.repo_id AND b.package = p.name
                  AND NOT EXISTS (SELECT 1 FROM deb_contents c WHERE c.item_id = b.item_id AND c.reader >= sqlc.arg(reader)::int))
 ORDER BY p.id
 LIMIT sqlc.arg(row_limit)::int;

-- name: DebVersionContents :many
-- The package files of a version in a repository with what the reader kept of each, without the file list: one row per architecture.
-- state is '' for a file the reader has not read yet.
SELECT b.architecture, b.item_id, COALESCE(c.state, '')::text AS state, COALESCE(c.reason, '')::text AS reason,
       COALESCE(c.detail, '')::text AS detail, COALESCE(c.changelog, '')::text AS changelog,
       COALESCE(c.changelog_file, '')::text AS changelog_file, COALESCE(c.changelog_truncated, false)::boolean AS changelog_truncated,
       COALESCE(c.copyright, '')::text AS copyright, COALESCE(c.copyright_truncated, false)::boolean AS copyright_truncated,
       COALESCE(c.licenses, '{}')::text[] AS licenses, COALESCE(c.doc_link, '')::text AS doc_link,
       COALESCE(jsonb_array_length(c.files), 0)::int AS files_kept, COALESCE(c.file_count, 0)::int AS file_count, c.read_at
  FROM deb_binaries b
  JOIN items i ON i.id = b.item_id AND i.trashed_at IS NULL
  LEFT JOIN deb_contents c ON c.item_id = b.item_id
 WHERE b.repo_id = sqlc.arg(repo_id)::bigint AND b.package = sqlc.arg(package)::text AND b.version = sqlc.arg(version)::text
 ORDER BY b.architecture;

-- name: DebContentsFileList :many
-- A page of the kept file list of a package file, in the archive's order, the entries whose path contains the text (compared without
-- case; an empty text matches every entry), each with the number of entries that match.
SELECT f.value::text AS entry, count(*) OVER ()::bigint AS matched
  FROM deb_contents c, jsonb_array_elements(c.files) WITH ORDINALITY AS f(value, ord)
 WHERE c.item_id = sqlc.arg(item_id)::bigint
   AND (sqlc.arg(needle)::text = '' OR strpos(lower(f.value->>'p'), lower(sqlc.arg(needle)::text)) > 0)
 ORDER BY f.ord
OFFSET sqlc.arg(row_offset)::bigint
 LIMIT sqlc.arg(row_limit)::bigint;

-- name: DebFindSourceVersions :many
-- The versions of the repositories whose package file names the source package (R-DEB-038): the binaries of one source, which the package
-- list's keyword filter lists for Debian.
SELECT DISTINCT i.version_id
  FROM deb_binaries b
  JOIN items i ON i.id = b.item_id
  JOIN repositories r ON r.id = b.repo_id
  JOIN project rp ON rp.id = r.project_id
 WHERE (rp.key || '/' || r.key) = ANY(sqlc.arg(repos)::text[]) AND i.trashed_at IS NULL AND i.version_id IS NOT NULL
   AND b.source = sqlc.arg(source)::text
 ORDER BY i.version_id
 LIMIT sqlc.arg(max_rows)::bigint;
