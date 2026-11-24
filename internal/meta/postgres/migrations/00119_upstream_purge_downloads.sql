-- quader:phase expand
--
-- The purge of a retired well-known repository (upstream_repository_purge, R-REPO-067, Q-REPO-376) decides upstream_download_finished of
-- 00118, the one table since 00114 that references a repository by a cascading key: the finished downloads from the upstream are a record
-- of the repository's content, like remote_download, and go with it (operations-and-observability.md R-OPS-051). upstream_download_live
-- names no repository by a key. The function is the one of 00114 with that one line.
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
