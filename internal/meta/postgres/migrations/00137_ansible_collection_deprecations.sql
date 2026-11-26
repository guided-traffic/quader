-- quader:phase expand
--
-- The deprecation of an Ansible Galaxy collection (formats/ansible.md R-ANS-060, api-and-ui.md R-API-085): a row says that the
-- collection of the repository is deprecated as a whole, every version of it. A local repository's row is set and cleared through the
-- management operation, with the principal who set it and an optional message, which only Quader's screens show (the collection document
-- carries the flag alone); a remote repository's row is the upstream's flag, written by the fill of the collection document, without a
-- principal or a message. No row is a collection that is not deprecated. Data (R-BAK-004): a backup exports it, and a restore without the
-- remote caches deletes the rows of the remote repositories with the caches (R-BAK-008).
CREATE TABLE ansible_collection_deprecations (
    repo_id       bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    name          text        COLLATE "C" NOT NULL CHECK (length(name) BETWEEN 3 AND 129),
    deprecated_at timestamptz NOT NULL DEFAULT now(),
    deprecated_by text        CHECK (length(deprecated_by) <= 256),
    message       text        CHECK (length(message) <= 1024),
    PRIMARY KEY (repo_id, name)
);

-- A remote repository's rows are its upstream's, content of the remote, so they go with the purge of a retired well-known repository:
-- the function below is the one of 00134 with the delete of ansible_collection_deprecations.
CREATE OR REPLACE FUNCTION upstream_repository_purge(rid bigint) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM upstream_repository WHERE repository_id = rid AND retired_at IS NOT NULL) THEN
        RAISE EXCEPTION 'repository % is no retired repository of the built-in project upstream', rid USING ERRCODE = 'AI010';
    END IF;
    DELETE FROM usage_limit_event WHERE repository_id = rid;
    DELETE FROM usage_daily_summary WHERE repository_id = rid;
    DELETE FROM usage_bucket WHERE repository_id = rid;
    DELETE FROM repository_usage WHERE repo_id = rid;
    DELETE FROM vuln_finding WHERE repo_id = rid;
    DELETE FROM signature_attachments WHERE repo_id = rid;
    DELETE FROM signature_fetches WHERE repo_id = rid;
    DELETE FROM signature_refusals WHERE repo_id = rid;
    DELETE FROM remote_download_limits WHERE repo_id = rid;
    DELETE FROM remote_download WHERE repo_id = rid;
    DELETE FROM remote_download_bulks WHERE repo_id = rid;
    DELETE FROM remote_examinations WHERE repo_id = rid;
    DELETE FROM upstream_download_finished WHERE repo_id = rid;
    DELETE FROM remote_cache_invalidations WHERE repo_id = rid;
    DELETE FROM remote_url_map WHERE repo_id = rid;
    DELETE FROM remote_cache_entries WHERE repo_id = rid;
    DELETE FROM remote_upstream_state WHERE repo_id = rid;
    DELETE FROM mirror_pending WHERE repo_id = rid;
    DELETE FROM mirror_state WHERE repo_id = rid;
    DELETE FROM upload_sessions WHERE repo_id = rid OR addressed_repo_id = rid;
    DELETE FROM ansible_collection_deprecations WHERE repo_id = rid;
    DELETE FROM ansible_roles WHERE repo_id = rid;
    DELETE FROM ansible_doc_fetches WHERE repo_id = rid;
    DELETE FROM ansible_version_docs WHERE repo_id = rid;
    DELETE FROM ansible_upstream_versions WHERE repo_id = rid;
    DELETE FROM ansible_import_tasks WHERE repo_id = rid;
    DELETE FROM ansible_tombstones WHERE repo_id = rid;
    DELETE FROM ansible_collection_versions WHERE repo_id = rid;
    DELETE FROM deb_remote_files WHERE repo_id = rid;
    DELETE FROM deb_remote_indexes WHERE repo_id = rid;
    DELETE FROM deb_remote_releases WHERE repo_id = rid;
    DELETE FROM deb_binaries WHERE repo_id = rid;
    DELETE FROM deb_suites WHERE repo_id = rid;
    DELETE FROM deb_tombstones WHERE repo_id = rid;
    DELETE FROM oci_tombstones WHERE repo_id = rid;
    DELETE FROM oci_manifests WHERE repo_id = rid;
    DELETE FROM nuget_symbol_file WHERE repository_id = rid;
    DELETE FROM nuget_tombstone WHERE repository_id = rid;
    DELETE FROM nuget_version WHERE repository_id = rid;
    DELETE FROM npm_tombstones WHERE repo_id = rid;
    DELETE FROM npm_remote_packages WHERE repo_id = rid;
    DELETE FROM npm_packages WHERE repo_id = rid;
    DELETE FROM packages WHERE repo_id = rid;
    DELETE FROM items WHERE repo_id = rid;
END $$;
