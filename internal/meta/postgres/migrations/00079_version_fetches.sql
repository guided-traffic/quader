-- quader:phase expand
--
-- What a remote repository lists, and what a version's downloads count (R-API-079, R-API-081, Q-REPO-330, Q-OCI-222).
--
-- A remote OCI repository lists a tag once a client fetched it through the repository, or a mirror pass stored it (mirror_state holds
-- a row exactly for a remote in mode mirror, R-REPO-050). A tag's document is a metadata entry, which a HEAD of the tag stores as
-- well as a GET; a HEAD is a check and no fetch (R-STOR-015). The fetch of a tag is the download of its manifest, by the tag or by
-- its digest, which is counted on the item that holds the manifest under its digest: the fill enters it beside the tag (R-OCI-022).
-- The other formats write a version row only when the content is stored, which in mode pull-through is a client's download.
--
-- package_version_listing gains document_sha256: for a version without a lead file (a remote OCI tag) the digest of the content its
-- listing names, so that the package view finds the items whose downloads are fetches of it (formats.FetchDocument). NULL for the
-- rows of package_versions, whose lead file holds that content.
CREATE OR REPLACE VIEW oci_remote_tags AS
SELECT (-e.id)::bigint            AS id,
       p.id                     AS package_id,
       e.repo_id                AS repo_id,
       p.name                   AS name,
       m.tag::text              AS version,
       e.fetched_at             AS created_at,
       e.blob_sha256            AS manifest_sha256
  FROM remote_cache_entries e
  JOIN repositories r ON r.id = e.repo_id AND r.class = 'remote' AND r.format = 'oci'
 CROSS JOIN LATERAL (SELECT regexp_match(e.path, '^(.+)/_tags/([^/]+)$') AS parts) x
 CROSS JOIN LATERAL (SELECT x.parts[1] AS name, x.parts[2] AS tag) m
  JOIN packages p ON p.repo_id = e.repo_id AND p.name = m.name
  JOIN oci_manifests om ON om.repo_id = e.repo_id AND om.name = m.name AND om.digest = e.blob_sha256
 WHERE e.kind = 'metadata'
   AND e.variant = ''
   AND e.upstream_gone_at IS NULL
   AND e.path LIKE '%/\_tags/%'
   AND x.parts IS NOT NULL
   AND (EXISTS (SELECT 1 FROM mirror_state ms WHERE ms.repo_id = e.repo_id)
        OR EXISTS (SELECT 1
                     FROM items i
                     JOIN item_download_stats s ON s.item_id = i.id
                    WHERE i.repo_id = e.repo_id
                      AND i.path = m.name || '/_manifests/sha256:' || encode(e.blob_sha256, 'hex')
                      AND i.trashed_at IS NULL
                      AND s.downloads > 0));

CREATE OR REPLACE VIEW package_version_listing AS
SELECT pv.id, pv.package_id, pv.version, pv.created_at, pv.lead_item_id, pv.trashed_at, NULL::bytea AS document_sha256
  FROM package_versions pv
UNION ALL
SELECT t.id, t.package_id, t.version, t.created_at, NULL::bigint, NULL::timestamptz, t.manifest_sha256
  FROM oci_remote_tags t;
