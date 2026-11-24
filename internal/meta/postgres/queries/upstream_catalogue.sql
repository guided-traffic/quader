-- The upstream catalogue's state of a row (remote-curation.md R-CUR-009): which versions of the packages of a page the remote repository
-- holds. One statement per answer, by the packages' unique (repo_id, name) and the live versions' index.

-- name: CatalogueStoredVersions :many
SELECT p.name, v.version
  FROM packages p
  JOIN package_versions v ON v.package_id = p.id AND v.trashed_at IS NULL
 WHERE p.repo_id = sqlc.arg(repo_id)::bigint AND p.name = ANY(sqlc.arg(names)::text[]);

-- The upstream catalogue of a Debian remote (R-DEB-036, Q-CUR-16): Quader searches the binary indexes the remote has parsed, by words, as
-- apt search does. Its levels are the release directories it holds and the binary indexes parsed below them.

-- name: CatalogueDebDirs :many
SELECT DISTINCT dir FROM deb_remote_releases
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND document <> 'absent'
 ORDER BY dir;

-- name: CatalogueDebIndexes :many
SELECT dir, path FROM deb_remote_indexes
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND path LIKE '%/binary-%/Packages'
 ORDER BY dir, path;

-- name: CatalogueDebSearch :many
-- Every pattern must match the name or the Provides, or with names_only false the short description: patterns are ILIKE patterns whose
-- wildcards the caller escaped with a backslash. One row per name, in byte order after the cursor, with the versions its rows list.
SELECT e.package, array_agg(DISTINCT e.version)::text[] AS versions, max(e.description)::text AS description
  FROM deb_remote_entries e
 WHERE e.repo_id = sqlc.arg(repo_id)::bigint AND e.architecture <> 'source'
   AND (sqlc.arg(dir)::text = '' OR e.dir = sqlc.arg(dir)::text)
   AND (sqlc.arg(index_path)::text = '' OR e.index_path = sqlc.arg(index_path)::text)
   AND e.package > sqlc.arg(after)::text
   AND NOT EXISTS (
         SELECT 1 FROM unnest(sqlc.arg(patterns)::text[]) AS w(p)
          WHERE NOT (e.package ILIKE w.p ESCAPE '\' OR e.provides ILIKE w.p ESCAPE '\'
                     OR (NOT sqlc.arg(names_only)::boolean AND e.description ILIKE w.p ESCAPE '\')))
 GROUP BY e.package
 ORDER BY e.package
 LIMIT sqlc.arg(lim)::int;

-- name: CatalogueDebRows :many
SELECT dir, index_path, package, version, architecture, path, sha256, size
  FROM deb_remote_entries
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND package = sqlc.arg(name)::text AND architecture <> 'source'
 ORDER BY dir, index_path, version, architecture;
