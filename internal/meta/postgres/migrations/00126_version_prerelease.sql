-- quader:phase expand
--
-- A format says which of its versions is a pre-release beside its order (api-and-ui.md R-API-029, Q-API-433, ADR-282): the view
-- adapter's Prerelease, which the host stores with every version row next to the sort key. Before this step the host asked the exposure
-- adapter, which OCI and Ansible Galaxy do not have, so no image tag and no collection version was ever a pre-release: an image whose
-- tags are 1.16.6 and 2.0.0-rc.1 showed 2.0.0-rc.1 as its latest version (formats/oci.md Q-OCI-270, formats/ansible.md Q-ANS-92).
--
-- 1. Whether an image tag is a pre-release, by the rule of the format's code (internal/formats/oci/tagorder Prerelease and the view
--    adapter's isVersion): a tag that is a version ("v"? and dot-separated numbers) whose suffix after the first "-" starts with alpha,
--    beta, rc, pre or dev in any case and continues with digits and dots only. A variant (1.25-alpine) and a tag that is no version
--    (latest) are none. TestR_OCI_036_ARemotesTagsCarryTheKeyOfTheFormat compares it with the format's over one table of tags.
CREATE FUNCTION oci_tag_prerelease(tag text) RETURNS boolean
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
  SELECT COALESCE(tag ~ '^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$'
                  AND lower((regexp_match(tag, '^v?[0-9]+(?:\.[0-9]+)*-(.+)$'))[1]) ~ '^(alpha|beta|rc|pre|dev)[0-9.]*$', false)
$$;

-- 2. A remote repository's tags are cache entries (R-OCI-036): the flag is derived from the tag like the sort key beside it (00116), NULL
--    for every entry that is no tag. Adding a stored generated column writes the table once.
ALTER TABLE remote_cache_entries
    ADD COLUMN tag_prerelease boolean GENERATED ALWAYS AS (oci_tag_prerelease((regexp_match(path, '^(.+)/_tags/([^/]+)$'))[2])) STORED;

-- The latest tag of a name: the highest release, else the highest pre-release, each the first entry of an index of its kind, as for the
-- version rows (package_versions_latest_release, package_versions_latest_prerelease).
CREATE INDEX remote_cache_entries_tag_latest_release ON remote_cache_entries (repo_id, tag_name, tag_sort_key)
    WHERE tag_name IS NOT NULL AND kind = 'metadata' AND variant = '' AND upstream_gone_at IS NULL AND NOT tag_prerelease;
CREATE INDEX remote_cache_entries_tag_latest_prerelease ON remote_cache_entries (repo_id, tag_name, tag_sort_key)
    WHERE tag_name IS NOT NULL AND kind = 'metadata' AND variant = '' AND upstream_gone_at IS NULL AND tag_prerelease;

-- 3. The views of 00122 and 00124 with the flag of a remote's tag: the same rows, the same rules; a new column goes at the end.
CREATE OR REPLACE VIEW oci_remote_tags AS
SELECT (-e.id)::bigint            AS id,
       p.id                     AS package_id,
       e.repo_id                AS repo_id,
       p.name                   AS name,
       e.tag::text              AS version,
       e.fetched_at             AS created_at,
       e.blob_sha256            AS manifest_sha256,
       e.tag_sort_key           AS sort_key,
       e.tag_prerelease         AS prerelease
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

CREATE OR REPLACE VIEW package_version_listing AS
SELECT pv.id, pv.package_id, pv.version, pv.created_at, pv.lead_item_id, pv.trashed_at, pv.document_sha256,
       pv.sort_key, pv.prerelease
  FROM package_versions pv
UNION ALL
SELECT t.id, t.package_id, t.version, t.created_at, NULL::bigint, NULL::timestamptz, t.manifest_sha256, t.sort_key, t.prerelease
  FROM oci_remote_tags t;

-- 4. The version rows whose stored answer the format's code now changes lose their key and their flag, and the background migration
--    version_sort_key fills both again through the format's code at the next start (formats/view.go Prerelease: "a change of the answer
--    for a stored version is a migration step that sets the format's stored keys to NULL"); the reads give such a row its format's key
--    meanwhile (Q-API-430). Those are an image tag the rule of 1. calls a pre-release and an Ansible collection version with a semantic
--    version's pre-release part (a "-" before any build metadata: stored versions are semantic versions, R-ANS-002). npm and NuGet
--    answered by the same rule before, Debian and the ClamAV definitions have no pre-release (Q-API-434): their rows keep their values.
UPDATE package_versions pv
   SET sort_key = NULL, prerelease = NULL
  FROM packages p
  JOIN repositories r ON r.id = p.repo_id
 WHERE pv.package_id = p.id AND pv.sort_key IS NOT NULL AND NOT pv.prerelease
   AND ((r.format = 'oci' AND oci_tag_prerelease(pv.version))
        OR (r.format = 'ansible' AND split_part(pv.version, '+', 1) LIKE '%-%'));
