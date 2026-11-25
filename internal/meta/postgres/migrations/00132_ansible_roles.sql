-- quader:phase expand
--
-- The facts of the roles a remote Ansible Galaxy repository read from its upstream (formats/ansible.md R-ANS-056, Q-ANS-110): a row per
-- role of the upstream's v1 API, keyed by the upstream's numeric id, written in the transaction that stores a role lookup. The README is
-- the upstream's rendered HTML of the role's content document (`v1/roles/<id>/content/`), written by the fill of that document, which
-- the job ansible-docs-fill fetches; it is untrusted and sanitized on every serving (R-ANS-039). The package of a role keeps its name
-- `role/<id>` (Q-ANS-36): the row is what the screens show of it, never what a pattern, a rule or a grant decides on.
--
-- The rows are derived from upstream documents and are no source of truth: they are not exported (R-BAK-004), go with the cache of a
-- remote (R-BAK-008) and are written again by the next lookup. readme_state is none (never asked), queued, available, absent (the
-- upstream has none) or unavailable (readme_reason says why); readme_commit is the commit the README was fetched for, so a lookup that
-- names another commit asks again.
CREATE TABLE ansible_roles (
    repo_id       bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    role_id       bigint      NOT NULL CHECK (role_id BETWEEN 1 AND 999999999999),
    owner         text        COLLATE "C" NOT NULL CHECK (char_length(owner) BETWEEN 1 AND 64),
    name          text        COLLATE "C" NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    description   text        NOT NULL DEFAULT '' CHECK (char_length(description) <= 4096),
    github_user   text        NOT NULL DEFAULT '' CHECK (char_length(github_user) <= 256),
    github_repo   text        NOT NULL DEFAULT '' CHECK (char_length(github_repo) <= 256),
    github_branch text        NOT NULL DEFAULT '' CHECK (char_length(github_branch) <= 256),
    tags          jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(tags) = 'array'),
    dependencies  jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(dependencies) = 'array'),
    downloads     bigint      NULL CHECK (downloads >= 0),
    commit_sha    text        NOT NULL DEFAULT '' CHECK (char_length(commit_sha) <= 64),
    imported_at   timestamptz NULL,
    readme_state  text        NOT NULL DEFAULT 'none' CHECK (readme_state IN ('none', 'queued', 'available', 'absent', 'unavailable')),
    readme_reason text        NOT NULL DEFAULT '' CHECK (char_length(readme_reason) <= 64),
    readme_file   text        NOT NULL DEFAULT '' CHECK (char_length(readme_file) <= 256),
    readme_html   text        NOT NULL DEFAULT '' CHECK (octet_length(readme_html) <= 524288),
    readme_commit text        NOT NULL DEFAULT '' CHECK (char_length(readme_commit) <= 64),
    recorded_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_id, role_id)
);

-- The rows are content of the remote, so they go with the purge of a retired well-known repository: the function below is the one of
-- 00120 with the delete of ansible_roles.
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
