-- quader:phase expand
--
-- Signatures on artifacts, stage 3, the gate per repository (signatures.md R-SIG-063, R-SIG-066, R-SIG-073; Q-SIG-82, Q-SIG-84):
--
-- oci_manifests.attachment is what the OCI format recognised at the commit of a manifest by its content: a carrier of signatures,
-- attestations or SBOMs without an image layer, or an index whose every descriptor carries such an artifact type. The signature gate
-- passes it, so that a client reaches the signatures of an image to verify them; a manifest stored before this migration reads false and
-- is judged as an artifact (fail closed).
--
-- signature_anchors.narrowed_epoch and signature_blocks.anchor_epoch let the gate subtract a narrowing from a trust row computed before
-- it (R-SIG-066): a write that narrows an anchor (it is switched off, its material changes) sets narrowed_epoch to the anchor epoch it
-- raised, and an anchor counts for no row of a lower epoch; a block keeps the epoch its creation raised, and blocks every row of a lower
-- epoch whose signature it matches. 0 is "never narrowed" and "older than every row": the rows the trust job computed hold the blocks of
-- before. Neither is a revision a client holds (R-ARCH-009): they are named so that the rule of revision columns does not claim them, and a
-- restore leaves them as they are (the trust rows are transient and computed again).
--
-- signature_refusals is the list of downloads a gate refused (R-SIG-073): one row per repository and artifact, the reason of the last
-- refusal, the first and the last refusal and their count. The gate writes it at every refusal; a pending answer writes none; the discovery
-- step of the job signature-verify removes the rows older than signatures.gate.refusals_retention. transient for backups: a refusal is a
-- recent fact, and the next refused download writes it again.

ALTER TABLE oci_manifests ADD COLUMN attachment boolean NOT NULL DEFAULT false;

ALTER TABLE signature_anchors ADD COLUMN narrowed_epoch bigint NOT NULL DEFAULT 0 CHECK (narrowed_epoch >= 0);

ALTER TABLE signature_blocks ADD COLUMN anchor_epoch bigint NOT NULL DEFAULT 0 CHECK (anchor_epoch >= 0);

CREATE TABLE signature_refusals (
    repo_id        bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    subject_sha256 bytea       NOT NULL CHECK (octet_length(subject_sha256) = 32),
    path           text        NOT NULL CHECK (length(path) BETWEEN 1 AND 2048),
    reason         text        NOT NULL CHECK (reason IN ('missing', 'untrusted', 'not_chosen', 'invalid', 'blocked', 'not_checked')),
    first_at       timestamptz NOT NULL DEFAULT now(),
    last_at        timestamptz NOT NULL DEFAULT now(),
    count          bigint      NOT NULL DEFAULT 1 CHECK (count >= 1),
    PRIMARY KEY (repo_id, subject_sha256)
);
CREATE INDEX signature_refusals_last ON signature_refusals (last_at);

-- The purge of a retired well-known repository (upstream_repository_purge, Q-REPO-376) takes its refused downloads with its content. The
-- function is the one of 00096 with the line of signature_refusals.
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
