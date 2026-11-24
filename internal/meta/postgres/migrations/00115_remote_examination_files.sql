-- quader:phase expand
--
-- The requests for an allow and the examination they start (remote-curation.md R-CUR-014 to R-CUR-016, work package R2b of
-- docs/developer/remote-curation.md).
--
-- remote_examinations.files: the repository paths a run of the download queue stored or found for an examined version, so that a discard
-- and the retention task know what to remove without asking the upstream again (Q-CUR-140). A run names at most 1,000 files
-- (R-REPO-065's bound of one download, formats.MaxProbeFiles); a later run adds what it found, up to 2,000 paths in all.
ALTER TABLE remote_examinations
    ADD COLUMN files text[] NOT NULL DEFAULT '{}' CHECK (cardinality(files) <= 2000);

-- A bulk that a request or an allow starts for the newest versions of a package (Q-CUR-141): the origin its rows take, the request
-- whose examination or allow it is, and the asker's charge (limits.Ticket) the rows of an examination carry to the worker. A bulk a
-- person starts keeps the origin bulk, no request and no charge (system work, Q-CUR-17).
ALTER TABLE remote_download_bulks
    ADD COLUMN origin     text  NOT NULL DEFAULT 'bulk' CHECK (origin IN ('bulk', 'request', 'allow')),
    ADD COLUMN request_id uuid  NULL REFERENCES remote_delivery_requests (id) ON DELETE SET NULL,
    ADD COLUMN ticket     jsonb NULL;

-- Why a request's examination did not start: the asker's budget refused it (R-CUR-015 "A refusal by the asker's budget starts nothing
-- and the entry says so"). NULL when it started, when the repository's switch is off, or when no examination was due.
ALTER TABLE remote_delivery_requests
    ADD COLUMN examination_refused text NULL CHECK (char_length(examination_refused) BETWEEN 1 AND 1000);
