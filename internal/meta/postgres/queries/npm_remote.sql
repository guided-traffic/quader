-- The statements of the npm format's remote repositories (internal/formatdata/npm;
-- R-NPM-024 to R-NPM-026): the records of the stored upstream documents and the
-- facts tarball serving reads. The package is a domain package and imports no driver
-- types, so a time travels as text and is cast to timestamptz here, and a value that
-- may be absent as the empty string.
--
-- The fill statements run in the transaction that upserts the cache entry
-- (remote_cache_entries), after the engine locked that entry, and take the lock of the
-- package's npm_remote_packages row: the records and the stored document describe the
-- same document, whichever fill commits last (R-NPM-024).

-- name: NPMRemoteInsertPackage :execrows
-- The record of a package's stored document. A package without a row inserts it, and the
-- caller has no older document to compare with; one that has a row conflicts and inserts
-- nothing. doc_modified is capped at PostgreSQL's clock in this transaction, so neither a
-- document dated in the future nor a replica with a fast clock can block every later
-- refresh (R-NPM-024).
INSERT INTO npm_remote_packages (repo_id, name, doc_sha256, doc_modified)
VALUES (sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, sqlc.arg(doc_sha256)::bytea,
        LEAST(NULLIF(sqlc.arg(doc_modified)::text, '')::timestamptz, clock_timestamp()))
ON CONFLICT (repo_id, name) DO NOTHING;

-- name: NPMRemoteLockPackage :many
-- The record of a package's stored document under its row lock: the document the stale-copy
-- rule compares with is the one stored now, not the one stored when the fetch began.
SELECT doc_sha256, doc_modified
  FROM npm_remote_packages
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text
   FOR UPDATE;

-- name: NPMRemoteStoredEntry :many
-- The cache entry of the package's stored document as it is now: when it was fetched and
-- whether it is marked gone. Read after the lock of the package's record, so an entry that a
-- concurrent fill has just committed is seen.
SELECT (extract(epoch FROM fetched_at) * 1000000)::bigint AS fetched_us,
       (upstream_gone_at IS NOT NULL)::boolean AS gone
  FROM remote_cache_entries
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND path = sqlc.arg(name)::text COLLATE "C"
   AND variant = 'full' AND kind = 'metadata';

-- name: NPMRemoteSetPackage :exec
UPDATE npm_remote_packages
   SET doc_sha256 = sqlc.arg(doc_sha256)::bytea,
       doc_modified = LEAST(NULLIF(sqlc.arg(doc_modified)::text, '')::timestamptz, clock_timestamp())
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text;

-- name: NPMRemoteUnlist :many
-- The versions the new document no longer lists (R-NPM-026): marked, not deleted. Only the
-- versions that were listed come back, so a duplicate fill finds nothing to report.
UPDATE npm_remote_versions
   SET listed = false
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text
   AND listed AND version <> ALL(sqlc.arg(versions)::text[])
RETURNING version, COALESCE(integrity, '')::text AS integrity;

-- name: NPMRemoteRelist :many
-- The versions the upstream lists again.
UPDATE npm_remote_versions
   SET listed = true
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text
   AND NOT listed AND version = ANY(sqlc.arg(versions)::text[])
RETURNING version;

-- name: NPMRemoteUpsertVersions :exec
-- The facts tarball serving needs, one row per version of the document. listed of an
-- existing row stays what Unlist and Relist made it.
INSERT INTO npm_remote_versions (repo_id, name, version, integrity, shasum, published_at, listed)
SELECT sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, (sqlc.arg(versions)::text[])[i],
       NULLIF((sqlc.arg(integrities)::text[])[i], ''), NULLIF((sqlc.arg(shasums)::text[])[i], ''),
       NULLIF((sqlc.arg(published)::text[])[i], '')::timestamptz, true
  FROM generate_series(1, cardinality(sqlc.arg(versions)::text[])) AS i
ON CONFLICT (repo_id, name, version) DO UPDATE
   SET integrity = EXCLUDED.integrity, shasum = EXCLUDED.shasum, published_at = EXCLUDED.published_at;

-- name: NPMRemoteTarballFacts :many
-- Everything the serving of one tarball reads, in one statement (R-NPM-025, R-NPM-026): the
-- version's record, the state of the package document's cache entry (stored, gone, current
-- = not expired and validated after the last restore, Q-NPM-13) and the repository's
-- serveRemovedVersions. No row: the repository does not exist.
SELECT (v.version IS NOT NULL)::boolean AS known,
       COALESCE(v.listed, false)::boolean AS listed,
       COALESCE(v.integrity, '')::text AS integrity,
       COALESCE(v.shasum, '')::text AS shasum,
       (e.repo_id IS NOT NULL)::boolean AS doc_stored,
       COALESCE((extract(epoch FROM e.upstream_gone_at) * 1000000)::bigint, 0)::bigint AS gone_us,
       COALESCE(e.expires_at > clock_timestamp()
                AND e.validated_at > COALESCE((SELECT (s.value #>> '{}')::timestamptz FROM system_state s WHERE s.key = 'restored_at'), '-infinity'::timestamptz),
                false)::boolean AS doc_current,
       COALESCE(r.config #>> '{npm,serveRemovedVersions}', '') = 'true' AS serve_removed
  FROM repositories r
  LEFT JOIN remote_cache_entries e
         ON e.repo_id = r.id AND e.path = sqlc.arg(name)::text COLLATE "C" AND e.variant = 'full' AND e.kind = 'metadata'
  LEFT JOIN npm_remote_versions v
         ON v.repo_id = r.id AND v.name = sqlc.arg(name)::text AND v.version = sqlc.arg(version)::text
 WHERE r.id = sqlc.arg(repo_id)::bigint;

-- name: NPMAuditNames :many
-- What decides the forwarded set of an audit request (R-NPM-038), in one statement for the
-- whole body: per distinct name, whether the forwarding remote holds a stored upstream document
-- of it (also one the core marked gone), whether a local npm repository holds the package or a
-- tombstone of it, whether a namespace claim of any scope of a local npm repository covers it,
-- and whether a pin rule of the requesting virtual repository (0: none) selects it. A pattern is
-- an exact name, or a literal and one trailing "*" (nothing after it holds "/") or "**"
-- (R-REPO-023); an npm name holds no backslash (R-NPM-002), so a stored npm pattern holds no
-- escape.
WITH n AS (
  SELECT DISTINCT unnest(sqlc.arg(names)::text[]) AS name
), locals AS (
  SELECT id FROM repositories WHERE format = 'npm' AND class = 'local'
), patterns AS (
  SELECT c.pattern, 'claim'::text AS source
    FROM namespace_claims c JOIN locals l ON l.id = c.repo_id
  UNION ALL
  SELECT unnest(p.package_patterns), 'pin'::text
    FROM virtual_pin_rules p
   WHERE p.virtual_id = sqlc.arg(virtual_id)::bigint
), parsed AS (
  SELECT rtrim(pattern, '*') AS literal,
         CASE WHEN pattern LIKE '%**' THEN 2 WHEN pattern LIKE '%*' THEN 1 ELSE 0 END AS star,
         source
    FROM patterns
)
SELECT n.name::text AS name,
       EXISTS (SELECT 1 FROM npm_remote_packages rp
                WHERE rp.repo_id = sqlc.arg(remote_id)::bigint AND rp.name = n.name)::boolean AS proxied,
       (EXISTS (SELECT 1 FROM packages p JOIN locals l ON l.id = p.repo_id WHERE p.name = n.name)
        OR EXISTS (SELECT 1 FROM npm_tombstones t JOIN locals l ON l.id = t.repo_id WHERE t.name = n.name))::boolean AS local,
       EXISTS (SELECT 1 FROM parsed m
                WHERE m.source = 'claim'
                  AND CASE m.star
                        WHEN 0 THEN n.name = m.literal
                        WHEN 1 THEN starts_with(n.name, m.literal) AND strpos(substr(n.name, length(m.literal) + 1), '/') = 0
                        ELSE starts_with(n.name, m.literal)
                      END)::boolean AS claimed,
       EXISTS (SELECT 1 FROM parsed m
                WHERE m.source = 'pin'
                  AND CASE m.star
                        WHEN 0 THEN n.name = m.literal
                        WHEN 1 THEN starts_with(n.name, m.literal) AND strpos(substr(n.name, length(m.literal) + 1), '/') = 0
                        ELSE starts_with(n.name, m.literal)
                      END)::boolean AS pinned
  FROM n;

-- name: NPMDescribeDocument :many
-- The stored full document of a package of a remote repository, by its blob (R-NPM-050): what the job npm-remote-describe reads the
-- entries of the cached versions from. No row: no document is stored.
SELECT e.blob_sha256 AS sha256, b.size
  FROM remote_cache_entries e
  JOIN blobs b ON b.sha256 = e.blob_sha256
 WHERE e.repo_id = sqlc.arg(repo_id)::bigint AND e.path = sqlc.arg(name)::text COLLATE "C"
   AND e.variant = 'full' AND e.kind = 'metadata';

-- name: NPMDescribeVersions :many
-- The live versions a remote repository holds of a package (the tarballs it cached, R-API-081), with the blob of the cached tarball and
-- whether the version has its row of npm_versions yet (R-NPM-050). A version whose tarball is gone has no blob.
SELECT pv.id AS version_id, pv.version,
       COALESCE(i.blob_sha256, ''::bytea)::bytea AS sha256, COALESCE(i.size, 0)::bigint AS size,
       (nv.version_id IS NOT NULL)::boolean AS described
  FROM packages p
  JOIN repositories r ON r.id = p.repo_id AND r.class = 'remote' AND r.format = 'npm'
  JOIN package_versions pv ON pv.package_id = p.id AND pv.trashed_at IS NULL
  LEFT JOIN items i ON i.id = pv.lead_item_id AND i.trashed_at IS NULL
  LEFT JOIN npm_versions nv ON nv.version_id = pv.id
 WHERE p.repo_id = sqlc.arg(repo_id)::bigint AND p.name = sqlc.arg(name)::text
 ORDER BY pv.id;

-- name: NPMPutCachedVersion :execrows
-- The row of npm_versions of a version a remote repository cached (R-NPM-050): the manifest is the version's entry of the stored
-- document, integrity and shasum are the digests of the cached tarball's blob, and the facts of the tarball (file count, unpacked size,
-- install script, shrinkwrap, the README's file name) come from one read of it when scanned is true. A row that exists keeps those facts
-- unless scanned, and takes the entry's manifest, deprecation and time: the upstream changes a deprecation, never a published tarball.
-- A version trashed or gone meanwhile gets no row. The README's text is never stored for a cached version: the screens read it from the
-- tarball (R-NPM-051).
INSERT INTO npm_versions (version_id, manifest, integrity, shasum, deprecated, file_count, unpacked_size,
                          has_install_script, has_shrinkwrap, readme, readme_filename, publish_tag, published_at)
SELECT pv.id, sqlc.arg(manifest)::text::jsonb, 'sha512-' || translate(encode(b.sha512, 'base64'), E'\n', ''),
       encode(b.sha1, 'hex'), NULLIF(sqlc.arg(deprecated)::text, ''),
       sqlc.arg(file_count)::integer, sqlc.arg(unpacked_size)::bigint,
       sqlc.arg(has_install_script)::boolean, sqlc.arg(has_shrinkwrap)::boolean,
       NULL, NULLIF(sqlc.arg(readme_filename)::text, ''), '',
       COALESCE(NULLIF(sqlc.arg(published_at)::text, '')::timestamptz, now())
  FROM package_versions pv
  JOIN items i ON i.id = pv.lead_item_id AND i.trashed_at IS NULL
  JOIN blobs b ON b.sha256 = i.blob_sha256
 WHERE pv.id = sqlc.arg(version_id)::bigint AND pv.trashed_at IS NULL
ON CONFLICT (version_id) DO UPDATE
   SET manifest = EXCLUDED.manifest, deprecated = EXCLUDED.deprecated, published_at = EXCLUDED.published_at,
       integrity = EXCLUDED.integrity, shasum = EXCLUDED.shasum,
       file_count = CASE WHEN sqlc.arg(scanned)::boolean THEN EXCLUDED.file_count ELSE npm_versions.file_count END,
       unpacked_size = CASE WHEN sqlc.arg(scanned)::boolean THEN EXCLUDED.unpacked_size ELSE npm_versions.unpacked_size END,
       has_install_script = CASE WHEN sqlc.arg(scanned)::boolean THEN EXCLUDED.has_install_script ELSE npm_versions.has_install_script END,
       has_shrinkwrap = CASE WHEN sqlc.arg(scanned)::boolean THEN EXCLUDED.has_shrinkwrap ELSE npm_versions.has_shrinkwrap END,
       readme_filename = CASE WHEN sqlc.arg(scanned)::boolean THEN EXCLUDED.readme_filename ELSE npm_versions.readme_filename END;

-- name: NPMHoldsPackage :one
-- Whether a remote repository holds a live version of a package: the refresh of a stored document describes the cached versions again
-- only then (R-NPM-050).
SELECT EXISTS (
  SELECT 1 FROM packages p JOIN package_versions pv ON pv.package_id = p.id AND pv.trashed_at IS NULL
   WHERE p.repo_id = sqlc.arg(repo_id)::bigint AND p.name = sqlc.arg(name)::text)::boolean;

-- name: NPMUndescribedPackages :many
-- The packages of npm remote repositories after the cursor (by package id) that hold a live version with a live cached tarball and no
-- row of npm_versions (R-NPM-050): what the task remote-describe queues the job npm-remote-describe for, at most row_limit of them. A
-- version without a live tarball is no candidate: nothing could describe it.
SELECT p.id, p.repo_id, p.name
  FROM packages p
  JOIN repositories r ON r.id = p.repo_id AND r.class = 'remote' AND r.format = 'npm'
 WHERE p.id > sqlc.arg(after)::bigint
   AND EXISTS (SELECT 1
                 FROM package_versions pv
                 JOIN items i ON i.id = pv.lead_item_id AND i.trashed_at IS NULL
                WHERE pv.package_id = p.id AND pv.trashed_at IS NULL
                  AND NOT EXISTS (SELECT 1 FROM npm_versions nv WHERE nv.version_id = pv.id))
 ORDER BY p.id
 LIMIT sqlc.arg(row_limit)::int;
