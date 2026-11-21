-- quader:phase expand
--
-- Signatures on artifacts, stage 3, package 2: the releases of the gate per repository and the index of the impact (signatures.md
-- R-SIG-070, R-SIG-071; Q-SIG-88, Q-SIG-90):
--
-- signature_releases holds the exceptions of one repository's gate: an artifact (its blob, an image's manifest or index) that a holder of
-- the triage right passed through that repository's gate with a reason, whatever its signatures say, until the release is withdrawn. No
-- other repository's gate is affected. It survives the removal of the artifact's items: the same bytes stored again are released. data for
-- backups: a release is a security decision; one granted after a restore point is lost, which narrows, and a withdrawal is journaled
-- (signature.release.withdrawn) and replayed. The purge of a retired well-known repository keeps it, as it keeps the repository's other
-- decisions (Q-SIG-201).
--
-- signature_trust_anchors serves the impact of a change of anchors (R-SIG-071): the trust rows that name an anchor, of every scope. The
-- index of 00098 covered the installation's rows only; a project anchor's rows are its project's.

CREATE TABLE signature_releases (
    repo_id         bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    subject_sha256  bytea       NOT NULL CHECK (octet_length(subject_sha256) = 32),
    reason          text        NOT NULL CHECK (length(reason) BETWEEN 1 AND 1000),
    refused_reason  text        NOT NULL CHECK (refused_reason IN ('missing', 'untrusted', 'not_chosen', 'invalid', 'blocked', 'not_checked', 'pending')),
    released_by     uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    released_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_id, subject_sha256)
);

DROP INDEX signature_trust_anchors;
CREATE INDEX signature_trust_anchors ON signature_trust USING gin (anchor_ids);
