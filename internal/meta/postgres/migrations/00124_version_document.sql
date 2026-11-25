-- quader:phase expand
--
-- A version that names content stores its digest, and a list of versions groups by it (api-and-ui.md R-API-029, Q-API-432, ADR-280): the
-- owner's word of 2026-11-25 asked one row per image, an image's tags beside it, on the tab Images of an OCI repository. A format whose
-- version is fetched as its document (formats.FetchDocument: OCI) has versions that are names of content (an image's tags); the
-- grouped list needs, for each version, the content it names and, for a set of contents, the other versions that name them, each an index
-- lookup. A remote's tags are cache entries that store the manifest's digest already (blob_sha256, R-OCI-036); a local tag is a version
-- row whose lead item is the tag item, whose bytes are the manifest's, and that row did not hold the digest.
--
-- 1. The digest of the version's document: the lead item's content, written by the host's package writer for a FetchDocument format
--    (formathost.versionOrder.document) at the insert of the version and when its lead item changes, NULL for every other format.
ALTER TABLE package_versions
    ADD COLUMN document_sha256 bytea CHECK (octet_length(document_sha256) = 32);

-- The rows of the release before: an OCI version's lead item is its tag item, whose content is the manifest. The format's rule is the
-- writer's from now on; this fill is the one place SQL names the format.
UPDATE package_versions pv
   SET document_sha256 = i.blob_sha256
  FROM items i, packages p, repositories r
 WHERE i.id = pv.lead_item_id AND p.id = pv.package_id AND r.id = p.repo_id AND r.format = 'oci';

-- 2. A tag pushed again keeps its version row and its item: the overwrite writes the item's content in place (ReplaceFileContent,
--    R-STOR-005). The digest follows the lead item's content wherever the version stores one. A trigger per row with a WHEN clause costs an
--    update of an item that keeps its content nothing beyond the clause.
CREATE FUNCTION package_versions_document_follow() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE package_versions
       SET document_sha256 = NEW.blob_sha256
     WHERE id = NEW.version_id AND lead_item_id = NEW.id AND document_sha256 IS NOT NULL;
    RETURN NULL;
END $$;

CREATE TRIGGER items_document_follow AFTER UPDATE OF blob_sha256 ON items
    FOR EACH ROW WHEN (OLD.blob_sha256 IS DISTINCT FROM NEW.blob_sha256 AND NEW.version_id IS NOT NULL)
    EXECUTE FUNCTION package_versions_document_follow();

-- 3. The versions of a package that name a content, in the order of the versions: the ranked ones by key, the unranked ones by time; of
--    the version rows and of a remote's tag entries. A lookup of whether a content has an earlier version than a candidate, and of a
--    page's contents' other versions, reads the first entries of one of these.
CREATE INDEX package_versions_by_document ON package_versions (package_id, document_sha256, sort_key)
    WHERE trashed_at IS NULL AND document_sha256 IS NOT NULL;
CREATE INDEX package_versions_document_unranked ON package_versions (package_id, document_sha256, created_at, version COLLATE "C")
    WHERE trashed_at IS NULL AND document_sha256 IS NOT NULL AND sort_key < '\x01'::bytea;
CREATE INDEX remote_cache_entries_tag_digest ON remote_cache_entries (repo_id, tag_name, blob_sha256, tag_sort_key)
    WHERE tag_name IS NOT NULL AND kind = 'metadata' AND variant = '' AND upstream_gone_at IS NULL;
CREATE INDEX remote_cache_entries_tag_digest_unranked ON remote_cache_entries (repo_id, tag_name, blob_sha256, fetched_at, tag)
    WHERE tag_name IS NOT NULL AND kind = 'metadata' AND variant = '' AND upstream_gone_at IS NULL AND tag_sort_key < '\x01'::bytea;

-- 4. The listing's local branch returns the stored digest, as its remote branch returns a tag's manifest digest.
CREATE OR REPLACE VIEW package_version_listing AS
SELECT pv.id, pv.package_id, pv.version, pv.created_at, pv.lead_item_id, pv.trashed_at, pv.document_sha256,
       pv.sort_key, pv.prerelease
  FROM package_versions pv
UNION ALL
SELECT t.id, t.package_id, t.version, t.created_at, NULL::bigint, NULL::timestamptz, t.manifest_sha256, t.sort_key, false
  FROM oci_remote_tags t;
