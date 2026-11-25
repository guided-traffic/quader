-- quader:phase expand
--
-- An upstream's rate limit holds a remote repository's download queue (remote-curation.md R-CUR-011 "An upstream's rate limit",
-- Q-CUR-170 to Q-CUR-173). A run that meets a limit longer than it waits out hands its row back to the queue without counting the
-- attempt; this row says until when no row of the repository starts (the upstream's own time, else the queue's back-off over the waits
-- in a row), since when the upstream limits, and its last answer in a row's failure form (status, address without its query, excerpt,
-- the headers of the limit). One row per repository; the first attempt the upstream answers without a limit removes it, and the task
-- remote-downloads-dispatch removes one that ended in a repository whose queue is empty. It is the queue's state, so it goes with the
-- repository and with the purge of a retired well-known repository, whose function below is the one of 00119 with that one line.
CREATE TABLE remote_download_limits (
    repo_id    bigint      PRIMARY KEY REFERENCES repositories (id) ON DELETE CASCADE,
    since      timestamptz NOT NULL,
    until      timestamptz NOT NULL,
    waits      integer     NOT NULL CHECK (waits >= 1),
    hinted     boolean     NOT NULL,
    failure    jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

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
