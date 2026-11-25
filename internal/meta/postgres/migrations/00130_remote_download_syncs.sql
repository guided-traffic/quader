-- quader:phase expand
--
-- Downloads kept in sync (remote-curation.md R-CUR-027, Q-CUR-240): the stored definition of a bulk that the task
-- remote-downloads-dispatch starts again every remote.syncs.intervalSeconds of its repository, as a bulk of the origin sync. The range
-- is the bulk's of 00129: NULL is no bound on that side, a bound is in the format's canonical form. A sync is a person's decision: export
-- class data, and the purge of a retired repository keeps it with the repository's row (Q-CUR-249, as the rules of Q-CUR-111).
CREATE TABLE remote_download_syncs (
    id                  uuid        PRIMARY KEY,                      -- UUIDv7 from the application
    repo_id             bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    level               text        NOT NULL CHECK (level IN ('package', 'section')),
    subject             text        NOT NULL CHECK (char_length(subject) BETWEEN 1 AND 1024), -- the name or the level's path
    max_versions        integer     NOT NULL CHECK (max_versions >= 1),                       -- the newest so many
    max_bytes           bigint      NOT NULL CHECK (max_bytes >= 1),
    include_prereleases boolean     NOT NULL,
    from_version        text        NULL CHECK (char_length(from_version) BETWEEN 1 AND 256),
    to_version          text        NULL CHECK (char_length(to_version) BETWEEN 1 AND 256),
    to_inclusive        boolean     NOT NULL DEFAULT true,
    paused              boolean     NOT NULL DEFAULT false,
    next_run_at         timestamptz NOT NULL,                         -- the dispatch starts the sync's next run at or after it
    last_run_at         timestamptz NULL,                             -- the last time the dispatch or a person looked at it
    -- what that look did: started a run, or why it did not (R-CUR-027)
    last_outcome        text        NULL CHECK (last_outcome IN ('started', 'overlapped', 'limited', 'bounded', 'disabled', 'paused')),
    created_by          uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now()
);
-- One sync per subject of a repository (Q-CUR-248); the tab's list; the dispatch's due syncs.
CREATE UNIQUE INDEX remote_download_syncs_subject ON remote_download_syncs (repo_id, level, subject);
CREATE INDEX remote_download_syncs_list ON remote_download_syncs (repo_id, created_at DESC, id);
CREATE INDEX remote_download_syncs_due ON remote_download_syncs (next_run_at) WHERE NOT paused;

-- A sync's runs are bulks of the origin sync that name it; a removed sync leaves its runs as bulks (a run in progress goes on). A
-- sync's run counts the versions the rules do not deliver, which its expansion leaves out (Q-CUR-243).
ALTER TABLE remote_download_bulks
    ADD COLUMN sync_id uuid NULL REFERENCES remote_download_syncs (id) ON DELETE SET NULL,
    ADD COLUMN versions_refused integer NOT NULL DEFAULT 0 CHECK (versions_refused >= 0),
    DROP CONSTRAINT remote_download_bulks_origin_check,
    ADD CONSTRAINT remote_download_bulks_origin_check CHECK (origin IN ('bulk', 'request', 'allow', 'sync'));
CREATE INDEX remote_download_bulks_sync ON remote_download_bulks (sync_id, created_at DESC) WHERE sync_id IS NOT NULL;

-- The rows a sync's run queues carry the origin sync (Q-CUR-247); so do the downloads they cause in the top bar's list (R-OPS-049).
ALTER TABLE remote_download
    DROP CONSTRAINT remote_download_origin_check,
    ADD CONSTRAINT remote_download_origin_check CHECK (origin IN ('person', 'request', 'allow', 'bulk', 'sync', 'probe', 'client'));
ALTER TABLE upstream_download_finished
    DROP CONSTRAINT upstream_download_finished_queue_origin_check,
    ADD CONSTRAINT upstream_download_finished_queue_origin_check
        CHECK (queue_origin IN ('person', 'request', 'allow', 'bulk', 'sync', 'probe'));
