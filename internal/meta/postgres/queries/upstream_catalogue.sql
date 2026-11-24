-- The upstream catalogue's state of a row (remote-curation.md R-CUR-009): which versions of the packages of a page the remote repository
-- holds. One statement per answer, by the packages' unique (repo_id, name) and the live versions' index.

-- name: CatalogueStoredVersions :many
-- The versions are those of package_version_listing, the ones the package screens list and open (Q-CUR-61): the live rows of
-- package_versions, and the tags of a remote OCI repository, which are its cache entries and no version rows (R-OCI-036, Q-CUR-156),
-- each entry kept by the rules of the view oci_remote_tags (00113), written here as checks per entry from the package's side. Read
-- through the view, the planner reads every tag entry of every OCI remote before it joins the page's names (measured on 2026-11-24:
-- 1.1 s for three names among 400,000 tag entries, 1.4 ms written this way, 28 ms for 200 names of 40 tags each). A change of the
-- view's rules changes them here too, as in package_view.sql's PackageViewTagPageDesc.
SELECT p.name, v.version
  FROM packages p
  JOIN package_versions v ON v.package_id = p.id AND v.trashed_at IS NULL
 WHERE p.repo_id = sqlc.arg(repo_id)::bigint AND p.name = ANY(sqlc.arg(names)::text[])
UNION ALL
SELECT p.name, e.tag::text AS version
  FROM packages p
  JOIN remote_cache_entries e ON e.repo_id = p.repo_id AND e.tag_name = p.name
   AND e.kind = 'metadata' AND e.variant = '' AND e.upstream_gone_at IS NULL
 WHERE p.repo_id = sqlc.arg(repo_id)::bigint AND p.name = ANY(sqlc.arg(names)::text[])
   AND EXISTS (SELECT 1 FROM repositories r WHERE r.id = p.repo_id AND r.class = 'remote' AND r.format = 'oci')
   AND EXISTS (SELECT 1 FROM oci_manifests om WHERE om.repo_id = e.repo_id AND om.name = e.tag_name AND om.digest = e.blob_sha256 OFFSET 0)
   AND (EXISTS (SELECT 1 FROM mirror_state ms WHERE ms.repo_id = e.repo_id OFFSET 0)
        OR e.background_sha256 = e.blob_sha256
        OR EXISTS (SELECT 1 FROM items i JOIN item_download_stats s ON s.item_id = i.id
                    WHERE i.repo_id = e.repo_id AND i.path = e.tag_name || '/_manifests/sha256:' || encode(e.blob_sha256, 'hex')
                      AND i.trashed_at IS NULL AND s.downloads > 0 OFFSET 0));

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
