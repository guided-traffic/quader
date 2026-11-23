-- quader:phase expand
--
-- The version list reads one page, not every version (api-and-ui.md R-API-010, formats/oci.md H-829, Q-API-417, Q-OCI-267).
--
-- A page of a package's versions in the order of their publication is one keyset query over the rows of the package: the index of the
-- local rows by package and creation, and the index of a remote OCI repository's tag entries by repository, image name and fetch time.
-- A tag entry is told from the other documents by its path, <name>/_tags/<tag> (R-OCI-036): the name and the tag are columns of the
-- entry now, stored when the entry is written, so that an index orders a name's tags and the planner knows how many a name has, which
-- it did not know of the expressions oci_remote_tags derived them by (measured on 2026-11-23: it sorted all 100,001 tags of a name for
-- every page). Both are NULL for every other entry.
CREATE INDEX package_versions_listing ON package_versions (package_id, created_at, version COLLATE "C") WHERE trashed_at IS NULL;

ALTER TABLE remote_cache_entries
    ADD COLUMN tag_name text COLLATE "C" GENERATED ALWAYS AS ((regexp_match(path, '^(.+)/_tags/([^/]+)$'))[1]) STORED,
    ADD COLUMN tag      text COLLATE "C" GENERATED ALWAYS AS ((regexp_match(path, '^(.+)/_tags/([^/]+)$'))[2]) STORED;

-- The tags of a name by fetch time (a page of the version list), and by tag (the rows of a page's versions, one tag).
CREATE INDEX remote_cache_entries_tag_time ON remote_cache_entries (repo_id, tag_name, fetched_at, tag)
    WHERE tag_name IS NOT NULL AND kind = 'metadata' AND variant = '' AND upstream_gone_at IS NULL;
CREATE INDEX remote_cache_entries_tag ON remote_cache_entries (repo_id, tag_name, tag)
    WHERE tag_name IS NOT NULL AND kind = 'metadata' AND variant = '' AND upstream_gone_at IS NULL;

-- The view of 00083 on the stored columns: the same rows, the same rules.
CREATE OR REPLACE VIEW oci_remote_tags AS
SELECT (-e.id)::bigint            AS id,
       p.id                     AS package_id,
       e.repo_id                AS repo_id,
       p.name                   AS name,
       e.tag::text              AS version,
       e.fetched_at             AS created_at,
       e.blob_sha256            AS manifest_sha256
  FROM remote_cache_entries e
  JOIN repositories r ON r.id = e.repo_id AND r.class = 'remote' AND r.format = 'oci'
  JOIN packages p ON p.repo_id = e.repo_id AND p.name = e.tag_name
  JOIN oci_manifests om ON om.repo_id = e.repo_id AND om.name = e.tag_name AND om.digest = e.blob_sha256
 WHERE e.kind = 'metadata'
   AND e.variant = ''
   AND e.upstream_gone_at IS NULL
   AND e.tag_name IS NOT NULL
   AND (EXISTS (SELECT 1 FROM mirror_state ms WHERE ms.repo_id = e.repo_id)
        OR e.background_sha256 = e.blob_sha256
        OR EXISTS (SELECT 1
                     FROM items i
                     JOIN item_download_stats s ON s.item_id = i.id
                    WHERE i.repo_id = e.repo_id
                      AND i.path = e.tag_name || '/_manifests/sha256:' || encode(e.blob_sha256, 'hex')
                      AND i.trashed_at IS NULL
                      AND s.downloads > 0));
