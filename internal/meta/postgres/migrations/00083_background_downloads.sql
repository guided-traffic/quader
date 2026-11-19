-- quader:phase expand
--
-- A probe starts the download (repositories-and-proxy.md R-REPO-065, R-API-081, Q-OCI-224).
--
-- A request that names exactly one version without fetching its content - the manifest of an OCI tag, an npm version document -
-- starts the background download of that version's content. In npm, NuGet, Ansible Galaxy and Debian a version is listed once its
-- file is stored, which the background download does like a client's fetch. A remote OCI tag is listed once a client downloaded
-- its manifest or the repository is in mode mirror (00079): background_sha256 is the third way, the digest of the tag document
-- whose content a background download stored. It is set when the download ended with everything stored, and only while the entry
-- still holds that digest; a tag that moved upstream holds another one and is listed again once its own content is stored. NULL
-- for every other entry.
ALTER TABLE remote_cache_entries ADD COLUMN background_sha256 bytea NULL;

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
        OR e.background_sha256 = e.blob_sha256
        OR EXISTS (SELECT 1
                     FROM items i
                     JOIN item_download_stats s ON s.item_id = i.id
                    WHERE i.repo_id = e.repo_id
                      AND i.path = m.name || '/_manifests/sha256:' || encode(e.blob_sha256, 'hex')
                      AND i.trashed_at IS NULL
                      AND s.downloads > 0));
