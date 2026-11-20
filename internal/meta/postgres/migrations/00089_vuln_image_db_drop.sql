-- quader:phase expand
--
-- The image scanning's database lies in a repository of the installation (vulnerability-scanning.md R-VULN-044, Q-VULN-173,
-- ADR-214): the database in use is the row of the source trivy, ecosystem OCI, in vuln_source_state (etag the layer's digest
-- "sha256:<hex>", archive_sha256 its SHA-256), and the layer is an item of the repository vulnerabilities.images.database.repository
-- names. The private slot goes with no path from it (the owner's rule of no backward compatibility): its row is deleted first, so that
-- the statement trigger releases its blob reference and the collection removes the archive (storage.md R-STOR-024), then the table.

-- The source's row keeps its etag: it is the digest of the archive, which is the digest of the published layer, so a worker looks the
-- layer up in the repository, finds it missing and wakes vuln-sync-trivy-db at once, which makes the repository hold it (R-VULN-044).
DELETE FROM vuln_image_db;
DROP TABLE vuln_image_db;

-- The exemption of the remote-cache cleanup (storage.md R-STOR-045, repositories-and-proxy.md R-REPO-070, Q-VULN-175, Q-VULN-190):
-- true when content of that SHA-256 is the image scanning's database in use. The workers read the layer through the item store, which
-- is no download, so the cleanup would otherwise judge it by its creation; its statement applies this function to every candidate, in
-- whichever repository the setting names. The function is the scanning's own reading of its state, so the cleanup needs to know nothing
-- of vuln_source_state.
CREATE FUNCTION vuln_image_database_in_use(sha256 bytea) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE AS $$
    SELECT EXISTS (SELECT 1 FROM vuln_source_state s
                   WHERE s.source = 'trivy' AND s.ecosystem = 'OCI' AND s.archive_sha256 = sha256)
$$;
