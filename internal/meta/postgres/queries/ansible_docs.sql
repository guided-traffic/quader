-- The statements of the documentation of Ansible Galaxy collections (R-ANS-033 to R-ANS-036, R-ANS-044). The tables are
-- ansible_doc_tables (ansible_version_docs, ansible_doc_entries, ansible_doc_files) and ansible_doc_fetches. The documentation is derived data.

-- name: AnsibleDocsDeleteVersion :execrows
-- The rows of a version go with their version row: the entries and files cascade.
DELETE FROM ansible_version_docs
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND version = sqlc.arg(version)::text;

-- name: AnsibleDocsInsertVersion :exec
INSERT INTO ansible_version_docs (repo_id, name, version, source, generator, entry_count, total_bytes, truncated, warnings, built_at)
VALUES (sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, sqlc.arg(version)::text, sqlc.arg(source)::text, sqlc.arg(generator)::integer,
        sqlc.arg(entry_count)::integer, sqlc.arg(total_bytes)::bigint, sqlc.arg(truncated)::boolean, sqlc.arg(warnings)::jsonb, now());

-- name: AnsibleDocsInsertEntry :exec
-- One entry. doc is the text of the JSON ('' for none), the readmes are '' for none.
INSERT INTO ansible_doc_entries (repo_id, name, version, content_type, content_name, short_description, has_docs, doc, readme_md, readme_html,
                                 search_text)
VALUES (sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, sqlc.arg(version)::text, sqlc.arg(content_type)::text, sqlc.arg(content_name)::text,
        sqlc.arg(short_description)::text, sqlc.arg(has_docs)::boolean, NULLIF(sqlc.arg(doc)::text, '')::jsonb,
        NULLIF(sqlc.arg(readme_md)::text, ''), NULLIF(sqlc.arg(readme_html)::text, ''), sqlc.arg(search_text)::text);

-- name: AnsibleDocsInsertFile :exec
INSERT INTO ansible_doc_files (repo_id, name, version, file_name, format, body)
VALUES (sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, sqlc.arg(version)::text, sqlc.arg(file_name)::text, sqlc.arg(format)::text,
        sqlc.arg(body)::text);

-- name: AnsibleDocsDeleteFetch :exec
DELETE FROM ansible_doc_fetches
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND version = sqlc.arg(version)::text;

-- name: AnsibleDocsUpsertFetch :exec
INSERT INTO ansible_doc_fetches (repo_id, name, version, state, reason, updated_at)
VALUES (sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, sqlc.arg(version)::text, sqlc.arg(state)::text, sqlc.arg(reason)::text, now())
ON CONFLICT (repo_id, name, version) DO UPDATE SET state = EXCLUDED.state, reason = EXCLUDED.reason, updated_at = now();

-- name: AnsibleDocsGetFetch :one
-- fresh says that a queued row is younger than an hour; an older one counts as none (R-ANS-033).
SELECT state, reason, (updated_at > now() - interval '1 hour')::boolean AS fresh
  FROM ansible_doc_fetches
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND version = sqlc.arg(version)::text;

-- name: AnsibleDocsGetVersion :one
SELECT source, generator, entry_count, total_bytes, truncated, warnings, built_at
  FROM ansible_version_docs
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND version = sqlc.arg(version)::text;

-- name: AnsibleDocsTree :many
-- The entries of a version in type and name order. With has_terms every term must match the name (a case-insensitive substring) or a word of
-- the search vector (a prefix of the term reduced to letters and digits). The terms are parameters and are validated by the caller; nothing
-- of a term is ever put into query text (Q-ANS-67).
SELECT content_type, content_name, short_description, has_docs
  FROM ansible_doc_entries
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND version = sqlc.arg(version)::text
   AND (NOT sqlc.arg(has_terms)::boolean OR NOT EXISTS (
         SELECT 1 FROM unnest(sqlc.arg(terms)::text[]) AS t(term)
          WHERE NOT (strpos(lower(content_name), lower(t.term)) > 0
                     OR (regexp_replace(t.term, '[^[:alnum:]]', '', 'g') <> ''
                         AND search @@ to_tsquery('simple', regexp_replace(t.term, '[^[:alnum:]]', '', 'g') || ':*')))))
 ORDER BY content_type, content_name
 LIMIT 5000;

-- name: AnsibleDocsTypeCounts :many
SELECT content_type, count(*)::integer AS entries
  FROM ansible_doc_entries
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND version = sqlc.arg(version)::text
 GROUP BY content_type;

-- name: AnsibleDocsFileHeads :many
-- The first 16 KiB of each file: enough for the title (rows.DocFileTitle).
SELECT file_name, format, left(body, 16384)::text AS head
  FROM ansible_doc_files
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND version = sqlc.arg(version)::text
 ORDER BY file_name;

-- name: AnsibleDocsGetEntry :one
SELECT content_type, content_name, short_description, has_docs, doc, readme_md, readme_html, search_text
  FROM ansible_doc_entries
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND version = sqlc.arg(version)::text
   AND content_type = sqlc.arg(content_type)::text AND content_name = sqlc.arg(content_name)::text;

-- name: AnsibleDocsGetFile :one
SELECT file_name, format, body
  FROM ansible_doc_files
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND version = sqlc.arg(version)::text
   AND file_name = sqlc.arg(file_name)::text;

-- name: AnsibleDocsAllEntries :many
SELECT content_type, content_name, short_description, has_docs, doc, readme_md, readme_html, search_text
  FROM ansible_doc_entries
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND version = sqlc.arg(version)::text
 ORDER BY content_type, content_name;

-- name: AnsibleDocsAllFiles :many
SELECT file_name, format, body
  FROM ansible_doc_files
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND version = sqlc.arg(version)::text
 ORDER BY file_name;

-- name: AnsibleDocsStaleVersions :many
-- The step of the reindex: the stored versions of a local repository after the checkpoint (name and version joined by a slash) whose
-- documentation or changelog is missing or was made by a generator older than the given one.
SELECT v.item_id, v.name, v.version, i.path, i.blob_sha256 AS sha256, COALESCE(i.size, 0)::bigint AS size
  FROM ansible_collection_versions v
  JOIN items i ON i.id = v.item_id
  LEFT JOIN ansible_version_docs d ON d.repo_id = v.repo_id AND d.name = v.name AND d.version = v.version
  LEFT JOIN ansible_changelogs c ON c.repo_id = v.repo_id AND c.name = v.name AND c.version = v.version
 WHERE v.repo_id = sqlc.arg(repo_id)::bigint
   AND v.name || '/' || v.version > sqlc.arg(after)::text
   AND i.blob_sha256 IS NOT NULL
   AND (d.generator IS NULL OR d.generator < sqlc.arg(generator)::integer
        OR c.generator IS NULL OR c.generator < sqlc.arg(generator)::integer)
 ORDER BY v.name || '/' || v.version
 LIMIT sqlc.arg(row_limit)::integer;

-- name: AnsibleChangelogStaleVersions :many
-- The changelog step of the reindex of a remote (R-ANS-053): the cached versions after the checkpoint whose changelog was not read, or was
-- read by a generator older than the given one.
SELECT v.item_id, v.name, v.version, i.path, i.blob_sha256 AS sha256, COALESCE(i.size, 0)::bigint AS size
  FROM ansible_collection_versions v
  JOIN items i ON i.id = v.item_id
  LEFT JOIN ansible_changelogs c ON c.repo_id = v.repo_id AND c.name = v.name AND c.version = v.version
 WHERE v.repo_id = sqlc.arg(repo_id)::bigint
   AND v.name || '/' || v.version > sqlc.arg(after)::text
   AND i.blob_sha256 IS NOT NULL
   AND (c.generator IS NULL OR c.generator < sqlc.arg(generator)::integer)
 ORDER BY v.name || '/' || v.version
 LIMIT sqlc.arg(row_limit)::integer;

-- name: AnsibleDocsStaleRepositories :many
-- The local and remote Ansible Galaxy repositories after the cursor that the task remote-describe queues the reindex for (R-ANS-044,
-- R-ANS-053, Q-ANS-99): those that hold a version whose changelog was not read by the given generator, or whose generated documentation is
-- older than it. A version of a local repository without any documentation has no row of ansible_version_docs and is not stale for that
-- alone, so a store whose archives hold none does not queue a reindex at every start.
SELECT r.id
  FROM repositories r
 WHERE r.format = 'ansible' AND r.class IN ('local', 'remote') AND r.id > sqlc.arg(after)::bigint
   AND EXISTS (
         SELECT 1
           FROM ansible_collection_versions v
           JOIN items i ON i.id = v.item_id AND i.blob_sha256 IS NOT NULL
           LEFT JOIN ansible_changelogs c ON c.repo_id = v.repo_id AND c.name = v.name AND c.version = v.version
           LEFT JOIN ansible_version_docs d ON d.repo_id = v.repo_id AND d.name = v.name AND d.version = v.version
          WHERE v.repo_id = r.id
            AND (c.generator IS NULL OR c.generator < sqlc.arg(generator)::integer
                 OR (d.source = 'generated' AND d.generator < sqlc.arg(generator)::integer)))
 ORDER BY r.id
 LIMIT sqlc.arg(row_limit)::integer;

-- name: AnsibleReindexRepository :many
-- The class of the Ansible Galaxy repository a reindex job names; a job about any other repository finds nothing.
SELECT r.class::text AS class
  FROM repositories r
 WHERE r.id = sqlc.arg(repo_id)::bigint AND r.format = 'ansible';

-- name: AnsibleChangelogUpsert :exec
-- The changelog of a version, or the record that it has none (format none); the version row exists in the same transaction.
INSERT INTO ansible_changelogs (repo_id, name, version, generator, format, source, body, truncated, read_at)
VALUES (sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, sqlc.arg(version)::text, sqlc.arg(generator)::integer, sqlc.arg(format)::text,
        sqlc.arg(source)::text, sqlc.arg(body)::text, sqlc.arg(truncated)::boolean, now())
ON CONFLICT (repo_id, name, version) DO UPDATE
   SET generator = EXCLUDED.generator, format = EXCLUDED.format, source = EXCLUDED.source, body = EXCLUDED.body,
       truncated = EXCLUDED.truncated, read_at = now();

-- name: AnsibleChangelogGet :one
-- The changelog of a version; a version without one has no row of a format other than none.
SELECT format, source, body, truncated
  FROM ansible_changelogs
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND version = sqlc.arg(version)::text AND format <> 'none';

-- name: AnsibleChangelogHead :one
-- Whether a version has a changelog, without its body: the tree names it (R-ANS-053).
SELECT format, source, truncated
  FROM ansible_changelogs
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND version = sqlc.arg(version)::text AND format <> 'none';

-- name: AnsibleDocsRemoteRepository :many
-- The remote Ansible Galaxy repository a documentation job names; a job about any other repository finds nothing.
SELECT r.id
  FROM repositories r
 WHERE r.id = sqlc.arg(repo_id)::bigint AND r.format = 'ansible' AND r.class = 'remote';

-- name: AnsibleDocsPurgeVersions :execrows
-- The documentation rows of a repository, entries and files with them: a remote's, when its caches are dropped (R-BAK-008).
DELETE FROM ansible_version_docs WHERE repo_id = sqlc.arg(repo_id)::bigint;

-- name: AnsibleDocsPurgeFetches :execrows
DELETE FROM ansible_doc_fetches WHERE repo_id = sqlc.arg(repo_id)::bigint;

-- name: AnsibleDocsRemoteWithoutRows :many
-- The step of the reindex of a remote: the versions whose version document is known (a row of ansible_upstream_versions) and that have no
-- documentation rows, after the checkpoint (name and version joined by a slash).
SELECT u.name, u.version
  FROM ansible_upstream_versions u
  LEFT JOIN ansible_version_docs d ON d.repo_id = u.repo_id AND d.name = u.name AND d.version = u.version
 WHERE u.repo_id = sqlc.arg(repo_id)::bigint AND u.name || '/' || u.version > sqlc.arg(after)::text AND d.name IS NULL
 ORDER BY u.name || '/' || u.version
 LIMIT sqlc.arg(row_limit)::integer;
