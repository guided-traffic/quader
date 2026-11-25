-- quader:phase expand
--
-- An attachment tag is no version of a remote OCI repository (signatures.md R-SIG-024, formats/oci.md R-OCI-036, Q-OCI-268): the
-- referrers tag `sha256-<64 hex>` and cosign's tags with `.sig`, `.att` or `.sbom` after it are stored and served as every tag, and a
-- local repository gives them no version row (Q-SIG-32), but the view of 00116 listed a remote's once its rules admitted the entry: in a
-- mirror every stored tag, so the ones the signature fetch stores; elsewhere one whose manifest a client fetched (cosign verify does).
-- The view drops them by the grammar of oci.IsAttachmentTag; package_view.sql and upstream_catalogue.sql restate its rules per entry
-- and drop them by the same check.
CREATE OR REPLACE VIEW oci_remote_tags AS
SELECT (-e.id)::bigint            AS id,
       p.id                     AS package_id,
       e.repo_id                AS repo_id,
       p.name                   AS name,
       e.tag::text              AS version,
       e.fetched_at             AS created_at,
       e.blob_sha256            AS manifest_sha256,
       e.tag_sort_key           AS sort_key
  FROM remote_cache_entries e
  JOIN repositories r ON r.id = e.repo_id AND r.class = 'remote' AND r.format = 'oci'
  JOIN packages p ON p.repo_id = e.repo_id AND p.name = e.tag_name
  JOIN oci_manifests om ON om.repo_id = e.repo_id AND om.name = e.tag_name AND om.digest = e.blob_sha256
 WHERE e.kind = 'metadata'
   AND e.variant = ''
   AND e.upstream_gone_at IS NULL
   AND e.tag_name IS NOT NULL
   AND e.tag !~ '^sha256-[0-9a-f]{64}(\.(sig|att|sbom))?$'
   AND (EXISTS (SELECT 1 FROM mirror_state ms WHERE ms.repo_id = e.repo_id)
        OR e.background_sha256 = e.blob_sha256
        OR EXISTS (SELECT 1
                     FROM items i
                     JOIN item_download_stats s ON s.item_id = i.id
                    WHERE i.repo_id = e.repo_id
                      AND i.path = e.tag_name || '/_manifests/sha256:' || encode(e.blob_sha256, 'hex')
                      AND i.trashed_at IS NULL
                      AND s.downloads > 0));
