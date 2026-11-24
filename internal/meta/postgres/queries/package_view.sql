-- The package view across repositories (R-API-029): internal/packages/view.
--
-- Every statement counts only the versions the caller may read, in the statement
-- that counts them. The caller's right is passed in as data, three ways:
--   whole        IDs of the repositories whose every package and version the caller
--                may read (authz.ReadAll);
--   rows         one entry "<repository id>\x1f<package name>\x1f<version>" per
--                version the caller may read in the other repositories
--                (authz.ReadRows), decided by the decision point row by row;
--   withheld     ids of stored versions the format does not serve.
-- The versions are those of package_version_listing: the rows of package_versions and the tags of a remote OCI repository, which
-- are read from the repository's cache entries (R-OCI-036); the second kind has a negative id and no lead item.
--
-- A version's downloads and its last download are those of the items whose downloads are fetches of it (R-API-079), by the rule its
-- format declares (formats.VersionFetch), passed in as the formats of each rule: any_file names the formats whose every file of a
-- version is a fetch of it (Debian), document those whose version is fetched as its document by its name or by its digest (OCI: the
-- items of the package that hold the same bytes and belong to no other version, the tag item and the manifest under its digest);
-- every other format counts the version's lead file.
-- A repository in neither list contributes no row. The scope parameters (formats,
-- repos) narrow the rows a request asks about and never widen them.

-- name: PackageViewList :many
-- One row per package (format and normalized name) with at least one visible
-- version, aggregated over the visible versions, in keyset order. sort is name,
-- createdAt or updatedAt; the tie-breakers are name and format, in the direction of
-- the sort. q_ids and keyword_ids are the versions whose description or keywords the
-- format matched (a package matches when one of its visible versions is among them).
-- exact_name, when set, restricts the rows to one package. Names are ordered bytewise
-- (collation C), so the order does not depend on the database's locale and the keyset
-- comparison of a cursor means the same on every replica.
-- The count, the times and the filters read one cheap row per visible version of the scope (vis, no subplan per row). The downloads
-- are summed for the page's packages alone (dl, Q-API-429): of a format whose fetch is the lead file or any file of a version, by a
-- join of the page's visible versions with the download statistics; of a format whose fetch is its document (OCI), from the files of
-- the package that were downloaded, each counted once per visible version that holds its bytes, so a remote's tags are not read a
-- second time. The downloads of a package outside the page are never read.
WITH vis AS (
  SELECT pv.id AS vid, pv.version, pv.created_at, (p.name COLLATE "C")::text AS name, p.display_name, r.format
    FROM package_version_listing pv
    JOIN packages p ON p.id = pv.package_id
    JOIN repositories r ON r.id = p.repo_id
   WHERE pv.trashed_at IS NULL
     AND NOT (pv.id = ANY(sqlc.arg(withheld)::bigint[]))
     AND (cardinality(sqlc.arg(formats)::text[]) = 0 OR r.format = ANY(sqlc.arg(formats)::text[]))
     AND (NOT sqlc.arg(has_repos)::boolean OR p.repo_id = ANY(sqlc.arg(repos)::bigint[]))
     AND (sqlc.arg(name_prefix)::text = '' OR starts_with(lower(p.name), lower(sqlc.arg(name_prefix)::text)))
     AND (sqlc.arg(exact_name)::text = '' OR (r.format = sqlc.arg(exact_format)::text AND p.name = sqlc.arg(exact_name)::text))
     AND (p.repo_id = ANY(sqlc.arg(whole)::bigint[])
          OR concat_ws(chr(31), p.repo_id::text, p.name, pv.version) = ANY(sqlc.arg(rows)::text[]))
), grouped AS (
  SELECT v.format, v.name, min(v.display_name)::text AS display_name,
         min(v.created_at)::timestamptz AS created_at, max(v.created_at)::timestamptz AS updated_at,
         count(DISTINCT v.version)::bigint AS version_count
    FROM vis v
   GROUP BY v.format, v.name
  HAVING (sqlc.arg(q)::text = '' OR position(lower(sqlc.arg(q)::text) in lower(v.name)) > 0 OR bool_or(v.vid = ANY(sqlc.arg(q_ids)::bigint[])))
     AND (sqlc.arg(keyword)::text = '' OR bool_or(v.vid = ANY(sqlc.arg(keyword_ids)::bigint[])))
     AND (NOT sqlc.arg(has_from)::boolean OR min(v.created_at) >= NULLIF(sqlc.arg(created_from)::text, '')::timestamptz)
     AND (NOT sqlc.arg(has_to)::boolean OR min(v.created_at) < NULLIF(sqlc.arg(created_to)::text, '')::timestamptz)
), page AS (
  SELECT g.format, g.name, g.display_name, g.created_at, g.updated_at, g.version_count
    FROM grouped g
   WHERE NOT sqlc.arg(has_after)::boolean
      OR CASE sqlc.arg(sort)::text
           WHEN 'createdAt' THEN CASE WHEN sqlc.arg(descending)::boolean
                THEN (g.created_at, g.name, g.format) < (NULLIF(sqlc.arg(after_time)::text, '')::timestamptz, sqlc.arg(after_name)::text, sqlc.arg(after_format)::text)
                ELSE (g.created_at, g.name, g.format) > (NULLIF(sqlc.arg(after_time)::text, '')::timestamptz, sqlc.arg(after_name)::text, sqlc.arg(after_format)::text) END
           WHEN 'updatedAt' THEN CASE WHEN sqlc.arg(descending)::boolean
                THEN (g.updated_at, g.name, g.format) < (NULLIF(sqlc.arg(after_time)::text, '')::timestamptz, sqlc.arg(after_name)::text, sqlc.arg(after_format)::text)
                ELSE (g.updated_at, g.name, g.format) > (NULLIF(sqlc.arg(after_time)::text, '')::timestamptz, sqlc.arg(after_name)::text, sqlc.arg(after_format)::text) END
           ELSE CASE WHEN sqlc.arg(descending)::boolean
                THEN (g.name, g.format) < (sqlc.arg(after_name)::text, sqlc.arg(after_format)::text)
                ELSE (g.name, g.format) > (sqlc.arg(after_name)::text, sqlc.arg(after_format)::text) END
         END
   ORDER BY
   (CASE WHEN sqlc.arg(sort)::text = 'createdAt' AND NOT sqlc.arg(descending)::boolean THEN g.created_at END) ASC NULLS LAST,
   (CASE WHEN sqlc.arg(sort)::text = 'createdAt' AND sqlc.arg(descending)::boolean THEN g.created_at END) DESC NULLS LAST,
   (CASE WHEN sqlc.arg(sort)::text = 'updatedAt' AND NOT sqlc.arg(descending)::boolean THEN g.updated_at END) ASC NULLS LAST,
   (CASE WHEN sqlc.arg(sort)::text = 'updatedAt' AND sqlc.arg(descending)::boolean THEN g.updated_at END) DESC NULLS LAST,
   (CASE WHEN NOT sqlc.arg(descending)::boolean THEN g.name END) ASC NULLS LAST,
   (CASE WHEN sqlc.arg(descending)::boolean THEN g.name END) DESC NULLS LAST,
   (CASE WHEN NOT sqlc.arg(descending)::boolean THEN g.format END) ASC NULLS LAST,
   (CASE WHEN sqlc.arg(descending)::boolean THEN g.format END) DESC NULLS LAST
   LIMIT sqlc.arg(max_rows)::int
), holders AS (
  -- The package rows of the page's packages in the scope.
  SELECT g.format, g.name, p.id AS package_id, p.repo_id, p.name AS pname
    FROM page g
    JOIN repositories r ON r.format = g.format
    JOIN packages p ON p.repo_id = r.id AND p.name = g.name
   WHERE (NOT sqlc.arg(has_repos)::boolean OR p.repo_id = ANY(sqlc.arg(repos)::bigint[]))
     AND (p.repo_id = ANY(sqlc.arg(whole)::bigint[]) OR cardinality(sqlc.arg(rows)::text[]) > 0)
), dl AS (
  SELECT f.format, f.name, sum(f.downloads)::bigint AS downloads
    FROM (-- the lead file of a visible version
          SELECT h.format, h.name, s.downloads
            FROM holders h
            JOIN package_versions pv ON pv.package_id = h.package_id AND pv.trashed_at IS NULL
            JOIN item_download_stats s ON s.item_id = pv.lead_item_id
           WHERE NOT (h.format = ANY(sqlc.arg(any_file)::text[]) OR h.format = ANY(sqlc.arg(document)::text[]))
             AND NOT (pv.id = ANY(sqlc.arg(withheld)::bigint[]))
             AND (h.repo_id = ANY(sqlc.arg(whole)::bigint[])
                  OR concat_ws(chr(31), h.repo_id::text, h.pname, pv.version) = ANY(sqlc.arg(rows)::text[]))
          UNION ALL
          -- any file of a visible version
          SELECT h.format, h.name, s.downloads
            FROM holders h
            JOIN package_versions pv ON pv.package_id = h.package_id AND pv.trashed_at IS NULL
            JOIN items i ON i.version_id = pv.id AND i.kind = 'file'
            JOIN item_download_stats s ON s.item_id = i.id
           WHERE h.format = ANY(sqlc.arg(any_file)::text[])
             AND NOT (pv.id = ANY(sqlc.arg(withheld)::bigint[]))
             AND (h.repo_id = ANY(sqlc.arg(whole)::bigint[])
                  OR concat_ws(chr(31), h.repo_id::text, h.pname, pv.version) = ANY(sqlc.arg(rows)::text[]))
          UNION ALL
          -- a downloaded file of the package that holds the bytes of a visible version's document: the version's lead item (a stored
          -- tag) or a remote's tag entry, and that belongs to no other version
          SELECT h.format, h.name, s.downloads * (
                 (SELECT count(*) FROM items l JOIN package_versions pv ON pv.id = l.version_id AND pv.lead_item_id = l.id
                   WHERE l.blob_sha256 = i.blob_sha256 AND pv.package_id = h.package_id AND pv.trashed_at IS NULL
                     AND (i.version_id IS NULL OR i.version_id = pv.id)
                     AND NOT (pv.id = ANY(sqlc.arg(withheld)::bigint[]))
                     AND (h.repo_id = ANY(sqlc.arg(whole)::bigint[])
                          OR concat_ws(chr(31), h.repo_id::text, h.pname, pv.version) = ANY(sqlc.arg(rows)::text[])))
               + (SELECT count(*) FROM oci_remote_tags t
                   WHERE t.package_id = h.package_id AND t.manifest_sha256 = i.blob_sha256
                     AND (i.version_id IS NULL OR i.version_id = t.id)
                     AND NOT (t.id = ANY(sqlc.arg(withheld)::bigint[]))
                     AND (h.repo_id = ANY(sqlc.arg(whole)::bigint[])
                          OR concat_ws(chr(31), h.repo_id::text, h.pname, t.version) = ANY(sqlc.arg(rows)::text[]))))
            FROM holders h
            JOIN items i ON i.repo_id = h.repo_id AND i.kind = 'file'
                        AND i.path >= h.pname || '/' AND i.path < h.pname || '0' AND starts_with(i.path, h.pname || '/')
            JOIN item_download_stats s ON s.item_id = i.id
           WHERE h.format = ANY(sqlc.arg(document)::text[])) f
   GROUP BY f.format, f.name
)
SELECT g.format, g.name, g.display_name, g.created_at, g.updated_at, g.version_count, COALESCE(d.downloads, 0)::bigint AS downloads
  FROM page g
  LEFT JOIN dl d ON d.format = g.format AND d.name = g.name
 ORDER BY
   (CASE WHEN sqlc.arg(sort)::text = 'createdAt' AND NOT sqlc.arg(descending)::boolean THEN g.created_at END) ASC NULLS LAST,
   (CASE WHEN sqlc.arg(sort)::text = 'createdAt' AND sqlc.arg(descending)::boolean THEN g.created_at END) DESC NULLS LAST,
   (CASE WHEN sqlc.arg(sort)::text = 'updatedAt' AND NOT sqlc.arg(descending)::boolean THEN g.updated_at END) ASC NULLS LAST,
   (CASE WHEN sqlc.arg(sort)::text = 'updatedAt' AND sqlc.arg(descending)::boolean THEN g.updated_at END) DESC NULLS LAST,
   (CASE WHEN NOT sqlc.arg(descending)::boolean THEN g.name END) ASC NULLS LAST,
   (CASE WHEN sqlc.arg(descending)::boolean THEN g.name END) DESC NULLS LAST,
   (CASE WHEN NOT sqlc.arg(descending)::boolean THEN g.format END) ASC NULLS LAST,
   (CASE WHEN sqlc.arg(descending)::boolean THEN g.format END) DESC NULLS LAST;

-- name: PackageViewVersions :many
-- The visible rows of the given packages (by id) and versions, one per version per repository, with the repository, the size of the
-- version's files (when asked), the downloads that are fetches of the version by its format's rule and when the latest of them was
-- (R-API-029, R-API-079). The caller names the versions of one page or one version, so the per-row subplans run for those rows alone
-- (Q-API-417); versions empty names every version, which no caller asks. The caller groups the rows per version.
SELECT pv.id AS version_id, r.format, p.name, p.display_name, pv.version, pv.created_at,
       r.id AS repo_id, r.class AS repo_class,
       CASE WHEN sqlc.arg(with_size)::boolean
            THEN COALESCE((SELECT sum(i.size) FROM items i WHERE i.version_id = pv.id AND i.kind = 'file' AND i.trashed_at IS NULL), 0)
            ELSE 0 END::bigint AS size_bytes,
       COALESCE(st.downloads, 0)::bigint AS downloads,
       st.last_download_at
  FROM package_version_listing pv
  JOIN packages p ON p.id = pv.package_id
  JOIN repositories r ON r.id = p.repo_id
  LEFT JOIN LATERAL (
       SELECT sum(s.downloads) AS downloads, max(s.last_download_at)::timestamptz AS last_download_at
         FROM item_download_stats s
        WHERE s.item_id IN (
              SELECT pv.lead_item_id
               WHERE NOT (r.format = ANY(sqlc.arg(any_file)::text[]) OR r.format = ANY(sqlc.arg(document)::text[]))
              UNION
              SELECT i.id FROM items i
               WHERE r.format = ANY(sqlc.arg(any_file)::text[]) AND i.version_id = pv.id AND i.kind = 'file'
              UNION
              SELECT i.id FROM items i
               WHERE r.format = ANY(sqlc.arg(document)::text[]) AND i.repo_id = p.repo_id AND i.kind = 'file'
                 AND i.blob_sha256 = COALESCE(pv.document_sha256, (SELECT l.blob_sha256 FROM items l WHERE l.id = pv.lead_item_id))
                 AND starts_with(i.path, p.name || '/') AND (i.version_id IS NULL OR i.version_id = pv.id))
  ) st ON true
 WHERE pv.trashed_at IS NULL
   AND pv.package_id = ANY(sqlc.arg(package_ids)::bigint[])
   AND (cardinality(sqlc.arg(versions)::text[]) = 0 OR pv.version = ANY(sqlc.arg(versions)::text[]))
   AND NOT (pv.id = ANY(sqlc.arg(withheld)::bigint[]))
   AND (p.repo_id = ANY(sqlc.arg(whole)::bigint[])
        OR concat_ws(chr(31), p.repo_id::text, p.name, pv.version) = ANY(sqlc.arg(rows)::text[]))
 ORDER BY pv.created_at, pv.id;

-- name: PackageViewVersionNames :many
-- The visible versions of the given packages ("<format>\x1f<name>"), one row per version per repository, without the size and the
-- downloads: what the latest version of a listed package, the repositories that hold it and the visible versions a format's details
-- refer to are decided on. It reads every row of the packages, a cheap one each (no subplan per row).
SELECT pv.id AS version_id, r.format, p.name, pv.version, pv.created_at, r.id AS repo_id
  FROM package_version_listing pv
  JOIN packages p ON p.id = pv.package_id
  JOIN repositories r ON r.id = p.repo_id
 WHERE pv.trashed_at IS NULL
   AND NOT (pv.id = ANY(sqlc.arg(withheld)::bigint[]))
   AND concat_ws(chr(31), r.format, p.name) = ANY(sqlc.arg(packages)::text[])
   AND (NOT sqlc.arg(has_repos)::boolean OR p.repo_id = ANY(sqlc.arg(repos)::bigint[]))
   AND (p.repo_id = ANY(sqlc.arg(whole)::bigint[])
        OR concat_ws(chr(31), p.repo_id::text, p.name, pv.version) = ANY(sqlc.arg(rows)::text[]))
 ORDER BY pv.created_at, pv.id;

-- name: PackageViewHolders :many
-- The packages of one format and name in the given repositories, one per repository: the holders of a version list (R-API-029).
SELECT p.id AS package_id, p.repo_id
  FROM packages p
  JOIN repositories r ON r.id = p.repo_id
 WHERE r.format = sqlc.arg(format)::text AND p.name = sqlc.arg(name)::text AND p.repo_id = ANY(sqlc.arg(repos)::bigint[])
 ORDER BY p.repo_id;

-- name: PackageViewVersionPageDesc :many
-- One page of the visible versions of one package of one repository, newest first (R-API-010, Q-API-417): the keyset (created_at,
-- version) below the cursor's, in the order of the index package_versions_listing, so the page reads the rows it returns and those the
-- scope or the withheld list drops on the way, and none past its limit. The first page passes the time 'infinity' and an empty
-- version. The time travels as text (RFC 3339 with nanoseconds, 'infinity', '-infinity'), so the service imports no driver
-- type (R-ARCH-002). A version is one row of a package: the caller uses it for a package that one repository of the scope holds. Versions compare
-- bytewise (collation C), as the cursor's do.
SELECT pv.version::text AS version, pv.created_at
  FROM package_versions pv
 WHERE pv.package_id = sqlc.arg(package_id)::bigint AND pv.trashed_at IS NULL
   AND (pv.created_at, (pv.version COLLATE "C")) < (sqlc.arg(after_at)::text::timestamptz, (sqlc.arg(after_version)::text COLLATE "C"))
   AND NOT (pv.id = ANY(sqlc.arg(withheld)::bigint[]))
   AND (sqlc.arg(whole)::boolean OR pv.version = ANY(sqlc.arg(allowed)::text[]))
 ORDER BY pv.created_at DESC, (pv.version COLLATE "C") DESC
 LIMIT sqlc.arg(max_rows)::int;

-- name: PackageViewVersionPageAsc :many
-- PackageViewVersionPageDesc oldest first: the keyset above the cursor's; the first page passes '-infinity' and an empty version.
SELECT pv.version::text AS version, pv.created_at
  FROM package_versions pv
 WHERE pv.package_id = sqlc.arg(package_id)::bigint AND pv.trashed_at IS NULL
   AND (pv.created_at, (pv.version COLLATE "C")) > (sqlc.arg(after_at)::text::timestamptz, (sqlc.arg(after_version)::text COLLATE "C"))
   AND NOT (pv.id = ANY(sqlc.arg(withheld)::bigint[]))
   AND (sqlc.arg(whole)::boolean OR pv.version = ANY(sqlc.arg(allowed)::text[]))
 ORDER BY pv.created_at, (pv.version COLLATE "C")
 LIMIT sqlc.arg(max_rows)::int;

-- name: PackageViewTagPageDesc :many
-- PackageViewVersionPageDesc for the tags of a name in a remote OCI repository (R-OCI-036), which are its cache entries and no version
-- rows: in the order of the index remote_cache_entries_tag_time, each entry kept by the rules of the view oci_remote_tags (00113),
-- written here as checks per entry, each one index lookup. Read through the view, or with the checks as joins, the planner starts from
-- the manifest's side and reads every tag entry of the name before it sorts (measured, Q-OCI-267); OFFSET 0 keeps each check a check per
-- entry. A change of the view's rules changes them here too; the listing tests of R-OCI-036 read both.
SELECT e.tag::text AS version, e.fetched_at AS created_at
  FROM remote_cache_entries e
 WHERE e.repo_id = sqlc.arg(repo_id)::bigint AND e.tag_name = sqlc.arg(name)::text
   AND e.kind = 'metadata' AND e.variant = '' AND e.upstream_gone_at IS NULL
   AND (e.fetched_at, e.tag) < (sqlc.arg(after_at)::text::timestamptz, (sqlc.arg(after_version)::text COLLATE "C"))
   AND NOT (-e.id = ANY(sqlc.arg(withheld)::bigint[]))
   AND (sqlc.arg(whole)::boolean OR e.tag = ANY(sqlc.arg(allowed)::text[]))
   AND EXISTS (SELECT 1 FROM repositories r WHERE r.id = e.repo_id AND r.class = 'remote' AND r.format = 'oci' OFFSET 0)
   AND EXISTS (SELECT 1 FROM oci_manifests om WHERE om.repo_id = e.repo_id AND om.name = e.tag_name AND om.digest = e.blob_sha256 OFFSET 0)
   AND (EXISTS (SELECT 1 FROM mirror_state ms WHERE ms.repo_id = e.repo_id OFFSET 0)
        OR e.background_sha256 = e.blob_sha256
        OR EXISTS (SELECT 1 FROM items i JOIN item_download_stats s ON s.item_id = i.id
                    WHERE i.repo_id = e.repo_id AND i.path = e.tag_name || '/_manifests/sha256:' || encode(e.blob_sha256, 'hex')
                      AND i.trashed_at IS NULL AND s.downloads > 0 OFFSET 0))
 ORDER BY e.fetched_at DESC, e.tag DESC
 LIMIT sqlc.arg(max_rows)::int;

-- name: PackageViewTagPageAsc :many
-- PackageViewTagPageDesc oldest first.
SELECT e.tag::text AS version, e.fetched_at AS created_at
  FROM remote_cache_entries e
 WHERE e.repo_id = sqlc.arg(repo_id)::bigint AND e.tag_name = sqlc.arg(name)::text
   AND e.kind = 'metadata' AND e.variant = '' AND e.upstream_gone_at IS NULL
   AND (e.fetched_at, e.tag) > (sqlc.arg(after_at)::text::timestamptz, (sqlc.arg(after_version)::text COLLATE "C"))
   AND NOT (-e.id = ANY(sqlc.arg(withheld)::bigint[]))
   AND (sqlc.arg(whole)::boolean OR e.tag = ANY(sqlc.arg(allowed)::text[]))
   AND EXISTS (SELECT 1 FROM repositories r WHERE r.id = e.repo_id AND r.class = 'remote' AND r.format = 'oci' OFFSET 0)
   AND EXISTS (SELECT 1 FROM oci_manifests om WHERE om.repo_id = e.repo_id AND om.name = e.tag_name AND om.digest = e.blob_sha256 OFFSET 0)
   AND (EXISTS (SELECT 1 FROM mirror_state ms WHERE ms.repo_id = e.repo_id OFFSET 0)
        OR e.background_sha256 = e.blob_sha256
        OR EXISTS (SELECT 1 FROM items i JOIN item_download_stats s ON s.item_id = i.id
                    WHERE i.repo_id = e.repo_id AND i.path = e.tag_name || '/_manifests/sha256:' || encode(e.blob_sha256, 'hex')
                      AND i.trashed_at IS NULL AND s.downloads > 0 OFFSET 0))
 ORDER BY e.fetched_at, e.tag
 LIMIT sqlc.arg(max_rows)::int;

-- name: PackageViewLatest :many
-- The latest visible version of each package row of the given packages ("<format>\x1f<name>") in the scope, as candidates: the highest
-- release of its version rows by the format's sort key, its highest pre-release when it has no visible release, and for a remote OCI
-- repository its highest tag (none is a pre-release), each one walk of an index of its kind (package_versions_latest_release,
-- package_versions_latest_prerelease, remote_cache_entries_tag_key) that stops at the first visible row (Q-API-429). The caller picks
-- the latest of a package among its rows' candidates by the format's order, and a package row with a candidate is a repository that
-- holds a visible version. It reads no version past the ones it returns and those the scope or the withheld list drops on the way.
SELECT p.id AS package_id, r.format, (p.name COLLATE "C")::text AS name, p.repo_id, l.version_id, l.version, l.created_at
  FROM packages p
  JOIN repositories r ON r.id = p.repo_id
  CROSS JOIN LATERAL (
       (SELECT pv.id AS version_id, pv.version::text AS version, pv.created_at
          FROM package_versions pv
         WHERE pv.package_id = p.id AND pv.trashed_at IS NULL AND NOT pv.prerelease
           AND NOT (pv.id = ANY(sqlc.arg(withheld)::bigint[]))
           AND (p.repo_id = ANY(sqlc.arg(whole)::bigint[])
                OR concat_ws(chr(31), p.repo_id::text, p.name, pv.version) = ANY(sqlc.arg(rows)::text[]))
         ORDER BY pv.sort_key DESC
         LIMIT 1)
       UNION ALL
       (SELECT pv.id, pv.version::text, pv.created_at
          FROM package_versions pv
         WHERE pv.package_id = p.id AND pv.trashed_at IS NULL AND pv.prerelease
           AND NOT (pv.id = ANY(sqlc.arg(withheld)::bigint[]))
           AND (p.repo_id = ANY(sqlc.arg(whole)::bigint[])
                OR concat_ws(chr(31), p.repo_id::text, p.name, pv.version) = ANY(sqlc.arg(rows)::text[]))
           AND NOT EXISTS (SELECT 1 FROM package_versions rv
                            WHERE rv.package_id = p.id AND rv.trashed_at IS NULL AND NOT rv.prerelease
                              AND NOT (rv.id = ANY(sqlc.arg(withheld)::bigint[]))
                              AND (p.repo_id = ANY(sqlc.arg(whole)::bigint[])
                                   OR concat_ws(chr(31), p.repo_id::text, p.name, rv.version) = ANY(sqlc.arg(rows)::text[])))
         ORDER BY pv.sort_key DESC
         LIMIT 1)
       UNION ALL
       (SELECT -e.id, e.tag::text, e.fetched_at
          FROM remote_cache_entries e
         WHERE r.class = 'remote' AND r.format = 'oci'
           AND e.repo_id = p.repo_id AND e.tag_name = p.name
           AND e.kind = 'metadata' AND e.variant = '' AND e.upstream_gone_at IS NULL
           AND NOT (-e.id = ANY(sqlc.arg(withheld)::bigint[]))
           AND (p.repo_id = ANY(sqlc.arg(whole)::bigint[])
                OR concat_ws(chr(31), p.repo_id::text, p.name, e.tag) = ANY(sqlc.arg(rows)::text[]))
           AND EXISTS (SELECT 1 FROM oci_manifests om WHERE om.repo_id = e.repo_id AND om.name = e.tag_name AND om.digest = e.blob_sha256 OFFSET 0)
           AND (EXISTS (SELECT 1 FROM mirror_state ms WHERE ms.repo_id = e.repo_id OFFSET 0)
                OR e.background_sha256 = e.blob_sha256
                OR EXISTS (SELECT 1 FROM items i JOIN item_download_stats s ON s.item_id = i.id
                            WHERE i.repo_id = e.repo_id AND i.path = e.tag_name || '/_manifests/sha256:' || encode(e.blob_sha256, 'hex')
                              AND i.trashed_at IS NULL AND s.downloads > 0 OFFSET 0))
         ORDER BY e.tag_sort_key DESC
         LIMIT 1)
  ) l
 WHERE concat_ws(chr(31), r.format, p.name) = ANY(sqlc.arg(packages)::text[])
   AND p.repo_id = ANY(sqlc.arg(scope)::bigint[])
   AND (NOT sqlc.arg(has_repos)::boolean OR p.repo_id = ANY(sqlc.arg(repos)::bigint[]))
 ORDER BY p.repo_id, l.version;

-- name: PackageViewVersionKeyDesc :many
-- One page of the visible versions of one package of one repository in the order of the versions, highest first (R-API-010, Q-API-429):
-- the keyset on the format's sort key below the cursor's, in the order of the index package_versions_by_key, so the page reads the rows
-- it returns and those the scope or the withheld list drops on the way, and none past its limit. The first page passes '\x02', above
-- every key (a key starts with 0x00 or 0x01).
SELECT pv.version::text AS version, pv.created_at, pv.sort_key
  FROM package_versions pv
 WHERE pv.package_id = sqlc.arg(package_id)::bigint AND pv.trashed_at IS NULL
   AND pv.sort_key < sqlc.arg(after_key)::bytea
   AND NOT (pv.id = ANY(sqlc.arg(withheld)::bigint[]))
   AND (sqlc.arg(whole)::boolean OR pv.version = ANY(sqlc.arg(allowed)::text[]))
 ORDER BY pv.sort_key DESC
 LIMIT sqlc.arg(max_rows)::int;

-- name: PackageViewVersionKeyAsc :many
-- PackageViewVersionKeyDesc lowest first: the keyset above the cursor's; the first page passes the empty key, below every key.
SELECT pv.version::text AS version, pv.created_at, pv.sort_key
  FROM package_versions pv
 WHERE pv.package_id = sqlc.arg(package_id)::bigint AND pv.trashed_at IS NULL
   AND pv.sort_key > sqlc.arg(after_key)::bytea
   AND NOT (pv.id = ANY(sqlc.arg(withheld)::bigint[]))
   AND (sqlc.arg(whole)::boolean OR pv.version = ANY(sqlc.arg(allowed)::text[]))
 ORDER BY pv.sort_key
 LIMIT sqlc.arg(max_rows)::int;

-- name: PackageViewTagKeyDesc :many
-- PackageViewVersionKeyDesc for the tags of a name in a remote OCI repository (R-OCI-036): in the order of the index
-- remote_cache_entries_tag_key, each entry kept by the rules of the view oci_remote_tags as checks per entry, as PackageViewTagPageDesc
-- writes them.
SELECT e.tag::text AS version, e.fetched_at AS created_at, e.tag_sort_key AS sort_key
  FROM remote_cache_entries e
 WHERE e.repo_id = sqlc.arg(repo_id)::bigint AND e.tag_name = sqlc.arg(name)::text
   AND e.kind = 'metadata' AND e.variant = '' AND e.upstream_gone_at IS NULL
   AND e.tag_sort_key < sqlc.arg(after_key)::bytea
   AND NOT (-e.id = ANY(sqlc.arg(withheld)::bigint[]))
   AND (sqlc.arg(whole)::boolean OR e.tag = ANY(sqlc.arg(allowed)::text[]))
   AND EXISTS (SELECT 1 FROM repositories r WHERE r.id = e.repo_id AND r.class = 'remote' AND r.format = 'oci' OFFSET 0)
   AND EXISTS (SELECT 1 FROM oci_manifests om WHERE om.repo_id = e.repo_id AND om.name = e.tag_name AND om.digest = e.blob_sha256 OFFSET 0)
   AND (EXISTS (SELECT 1 FROM mirror_state ms WHERE ms.repo_id = e.repo_id OFFSET 0)
        OR e.background_sha256 = e.blob_sha256
        OR EXISTS (SELECT 1 FROM items i JOIN item_download_stats s ON s.item_id = i.id
                    WHERE i.repo_id = e.repo_id AND i.path = e.tag_name || '/_manifests/sha256:' || encode(e.blob_sha256, 'hex')
                      AND i.trashed_at IS NULL AND s.downloads > 0 OFFSET 0))
 ORDER BY e.tag_sort_key DESC
 LIMIT sqlc.arg(max_rows)::int;

-- name: PackageViewTagKeyAsc :many
-- PackageViewTagKeyDesc lowest first.
SELECT e.tag::text AS version, e.fetched_at AS created_at, e.tag_sort_key AS sort_key
  FROM remote_cache_entries e
 WHERE e.repo_id = sqlc.arg(repo_id)::bigint AND e.tag_name = sqlc.arg(name)::text
   AND e.kind = 'metadata' AND e.variant = '' AND e.upstream_gone_at IS NULL
   AND e.tag_sort_key > sqlc.arg(after_key)::bytea
   AND NOT (-e.id = ANY(sqlc.arg(withheld)::bigint[]))
   AND (sqlc.arg(whole)::boolean OR e.tag = ANY(sqlc.arg(allowed)::text[]))
   AND EXISTS (SELECT 1 FROM repositories r WHERE r.id = e.repo_id AND r.class = 'remote' AND r.format = 'oci' OFFSET 0)
   AND EXISTS (SELECT 1 FROM oci_manifests om WHERE om.repo_id = e.repo_id AND om.name = e.tag_name AND om.digest = e.blob_sha256 OFFSET 0)
   AND (EXISTS (SELECT 1 FROM mirror_state ms WHERE ms.repo_id = e.repo_id OFFSET 0)
        OR e.background_sha256 = e.blob_sha256
        OR EXISTS (SELECT 1 FROM items i JOIN item_download_stats s ON s.item_id = i.id
                    WHERE i.repo_id = e.repo_id AND i.path = e.tag_name || '/_manifests/sha256:' || encode(e.blob_sha256, 'hex')
                      AND i.trashed_at IS NULL AND s.downloads > 0 OFFSET 0))
 ORDER BY e.tag_sort_key
 LIMIT sqlc.arg(max_rows)::int;

-- name: PackageViewFiles :many
-- The files of a version: the live file items linked to it.
SELECT i.path, i.size, encode(i.blob_sha256, 'hex')::text AS sha256
  FROM items i
 WHERE i.version_id = sqlc.arg(version_id)::bigint AND i.kind = 'file' AND i.trashed_at IS NULL
 ORDER BY i.path;

-- name: PackageViewCandidates :many
-- The (repository, package, version) triples of the given repositories, for the
-- repositories whose rows the decision point decides one by one. At most max_rows;
-- the caller asks for one more than it accepts.
SELECT p.repo_id, p.name, pv.version
  FROM package_version_listing pv
  JOIN packages p ON p.id = pv.package_id
  JOIN repositories r ON r.id = p.repo_id
 WHERE pv.trashed_at IS NULL
   AND p.repo_id = ANY(sqlc.arg(keys)::bigint[])
   AND (cardinality(sqlc.arg(formats)::text[]) = 0 OR r.format = ANY(sqlc.arg(formats)::text[]))
   AND (sqlc.arg(name_prefix)::text = '' OR starts_with(lower(p.name), lower(sqlc.arg(name_prefix)::text)))
   AND (sqlc.arg(exact_name)::text = '' OR p.name = sqlc.arg(exact_name)::text)
 ORDER BY p.repo_id, p.name, pv.version
 LIMIT sqlc.arg(max_rows)::int;
