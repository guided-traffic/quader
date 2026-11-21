-- quader:phase expand
--
-- Signatures on artifacts, stage 1, the formats' wire behaviour (signatures.md R-SIG-026, R-SIG-031, R-SIG-067; Q-SIG-97 to Q-SIG-106).
--
-- signature_fetches is the state of the background fetch of a remote's signatures (R-SIG-026, Q-SIG-33, Q-SIG-106), which stage 3's gate
-- reads as well (R-SIG-067): one row per target a format queued for a remote repository (OCI: "<image name>@sha256:<hex>" of a top-level
-- manifest; npm: "<name>@<version>" of a stored tarball, and "keys"), with the item whose signatures it fetches (subject_sha256; NULL for
-- the keys, which belong to no item). queued_at is set while a run is due or running; fetched_at is the end of the last run that asked
-- the upstream to the end, whether it found something or nothing; attempts counts the runs that failed since; failed_at is set when the
-- job's last attempt failed, and the next fill or refresh queues the target again. The job signature-fetch writes them; an enqueue
-- queues a target once, and again only as a refresh after signatures.remote.refresh_after. The row goes with its manifest (the OCI
-- data port's RemoveManifest) and with its repository.
CREATE TABLE signature_fetches (
    repo_id        bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    target         text        COLLATE "C" NOT NULL CHECK (length(target) BETWEEN 1 AND 1024),
    format         text        NOT NULL CHECK (length(format) BETWEEN 1 AND 32),
    subject_sha256 bytea       NULL CHECK (octet_length(subject_sha256) = 32),
    queued_at      timestamptz NULL,
    fetched_at     timestamptz NULL,
    attempts       integer     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    failed_at      timestamptz NULL,
    PRIMARY KEY (repo_id, target)
);
-- The gate of stage 3 asks for the fetch of an item by its digest (R-SIG-067).
CREATE INDEX signature_fetches_subject ON signature_fetches (repo_id, subject_sha256) WHERE subject_sha256 IS NOT NULL;

-- ansible_version_signatures holds the detached signatures of MANIFEST.json that a client posted for a version of a local Ansible
-- Galaxy repository (R-SIG-031, Q-SIG-37): the armored signature as it was posted, the fingerprint of the key it names, and who posted
-- it. They are served in signatures[] of the version document and recorded in the signature store, whose job checks them. They go
-- with the version.
CREATE TABLE ansible_version_signatures (
    repo_id     bigint      NOT NULL,
    name        text        COLLATE "C" NOT NULL,
    version     text        COLLATE "C" NOT NULL,
    fingerprint text        COLLATE "C" NOT NULL CHECK (fingerprint ~ '^[0-9a-f]{40}$|^[0-9a-f]{64}$'),
    signature   text        NOT NULL CHECK (length(signature) BETWEEN 1 AND 65536),
    posted_by   text        NOT NULL,
    posted_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_id, name, version, fingerprint),
    FOREIGN KEY (repo_id, name, version) REFERENCES ansible_collection_versions (repo_id, name, version) ON DELETE CASCADE
);

-- The purge of a retired well-known repository (upstream_repository_purge, Q-REPO-376) takes the fetch state with the content it belongs
-- to: a purged remote fetches its signatures again when its content comes back. The function is the one of 00093 (00086 with the delete
-- of signature_attachments) with the line of signature_fetches.
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
