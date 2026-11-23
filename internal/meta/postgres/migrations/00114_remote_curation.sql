-- quader:phase expand
--
-- The curation of a remote repository (remote-curation.md, T-24, ADR-277): its delivery rules and their revision, the requests for an
-- allow with their askers, the download queue with its bulks, the versions fetched for examination, and what the Debian search reads
-- (formats/debian.md R-DEB-036). Every table is new and empty, so it takes its keys and indexes inline; the two tables that gain columns
-- take them with constant defaults. Nothing reads or writes these tables yet: the packages of T-24 that build the behaviour do
-- (docs/developer/remote-curation.md).
--
-- Every table is export class data (R-BAK-004, R-CUR-021; tables_remote_curation.go): a backup holds the rules, the requests and the
-- queue. deb_remote_descriptions is a remote's cache row, like deb_remote_entries, and goes with the index it was parsed from.

-- The requests for an allow (R-CUR-015): one open entry per repository and subject, a version NULL for the package with its future
-- versions. It comes first because a rule and a queue row name the request that caused them.
CREATE TABLE remote_delivery_requests (
    id              uuid        PRIMARY KEY,                          -- UUIDv7 from the application
    repo_id         bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    name            text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 1024),
    version         text        NULL CHECK (char_length(version) BETWEEN 1 AND 256),
    state           text        NOT NULL CHECK (state IN ('open', 'allowed', 'declined', 'withdrawn')),
    askers          integer     NOT NULL DEFAULT 0 CHECK (askers >= 0),
    refusals        bigint      NOT NULL DEFAULT 0 CHECK (refusals >= 0),
    first_at        timestamptz NOT NULL DEFAULT now(),
    last_at         timestamptz NOT NULL DEFAULT now(),
    decided_by      uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    decided_at      timestamptz NULL,
    decline_reason  text        NULL CHECK (char_length(decline_reason) BETWEEN 1 AND 1000)
);
CREATE UNIQUE INDEX remote_delivery_requests_open
    ON remote_delivery_requests (repo_id, name, coalesce(version, '')) WHERE state = 'open';
CREATE INDEX remote_delivery_requests_list ON remote_delivery_requests (repo_id, state, last_at DESC, id);

-- The people who asked (R-CUR-015); an entry that only clients caused has none.
CREATE TABLE remote_delivery_request_askers (
    request_id  uuid        NOT NULL REFERENCES remote_delivery_requests (id) ON DELETE CASCADE,
    principal   uuid        NOT NULL REFERENCES principal (id) ON DELETE CASCADE,
    note        text        NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
    asked_at    timestamptz NOT NULL DEFAULT now(),
    withdrawn   boolean     NOT NULL DEFAULT false,
    PRIMARY KEY (request_id, principal)
);
-- A person's own entries (the list's mine=true, the bound of open entries per person and repository): the key leads with the
-- request, so the lookup by person needs its own index.
CREATE INDEX remote_delivery_request_askers_principal ON remote_delivery_request_askers (principal, request_id);

-- The delivery rules of a remote repository (R-CUR-001, R-CUR-006): one rule per subject, which is its identity.
CREATE TABLE remote_delivery_rules (
    id          uuid        PRIMARY KEY,                              -- UUIDv7 from the application
    repo_id     bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    kind        text        NOT NULL CHECK (kind IN ('section', 'package', 'version')),
    pattern     text        NULL CHECK (char_length(pattern) BETWEEN 1 AND 1024),
    name        text        NULL CHECK (char_length(name) BETWEEN 1 AND 1024),
    version     text        NULL CHECK (char_length(version) BETWEEN 1 AND 256),
    effect      text        NOT NULL CHECK (effect IN ('allow', 'block')),
    note        text        NOT NULL DEFAULT '' CHECK (char_length(note) <= 1000),
    request_id  uuid        NULL REFERENCES remote_delivery_requests (id) ON DELETE SET NULL,
    created_by  uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CHECK ((kind = 'section' AND pattern IS NOT NULL AND name IS NULL AND version IS NULL)
        OR (kind = 'package' AND pattern IS NULL AND name IS NOT NULL AND version IS NULL)
        OR (kind = 'version' AND pattern IS NULL AND name IS NOT NULL AND version IS NOT NULL))
);
CREATE UNIQUE INDEX remote_delivery_rules_subject
    ON remote_delivery_rules (repo_id, kind, coalesce(pattern, ''), coalesce(name, ''), coalesce(version, ''));
CREATE INDEX remote_delivery_rules_name ON remote_delivery_rules (repo_id, name) WHERE name IS NOT NULL;

-- The revision of a repository's rule set, raised by every write of a rule in its transaction (R-CUR-006): the key and the validator
-- of a filtered document carry it (R-CUR-003), so it is a client-held revision that a restore raises (R-BAK-022).
ALTER TABLE repositories ADD COLUMN delivery_rules_revision bigint NOT NULL DEFAULT 0;

-- A bulk download of every version of a package or of a sub-section (R-CUR-010, R-CUR-011), expanded into rows of remote_download.
CREATE TABLE remote_download_bulks (
    id                  uuid        PRIMARY KEY,                      -- UUIDv7 from the application
    repo_id             bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    level               text        NOT NULL CHECK (level IN ('package', 'section')),
    subject             text        NOT NULL CHECK (char_length(subject) BETWEEN 1 AND 1024), -- the name or the level's path
    max_versions        integer     NOT NULL CHECK (max_versions >= 1),
    max_bytes           bigint      NOT NULL CHECK (max_bytes >= 1),
    include_prereleases boolean     NOT NULL,
    state               text        NOT NULL CHECK (state IN ('expanding', 'queued', 'done', 'bounded', 'cancelled', 'failed')),
    versions_found      integer     NOT NULL DEFAULT 0 CHECK (versions_found >= 0),
    versions_queued     integer     NOT NULL DEFAULT 0 CHECK (versions_queued >= 0),
    cursor              jsonb       NULL CHECK (cursor IS NULL OR octet_length(cursor::text) <= 4096), -- the expansion's position
    failure             jsonb       NULL,                             -- as a row's failure (R-CUR-011)
    started_by          uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    finished_at         timestamptz NULL
);
-- A repository's bulks (the queue's API, the retention that removes a bulk with its last row).
CREATE INDEX remote_download_bulks_repo ON remote_download_bulks (repo_id, created_at DESC, id);

-- The download queue of a remote repository (R-CUR-011): one version of one package per row, run as one-attempt jobs of the kind
-- remote-download on the queue downloads; a client's failed fetch is a row of origin client (Q-CUR-19).
CREATE TABLE remote_download (
    id              uuid        PRIMARY KEY,                          -- UUIDv7 from the application
    repo_id         bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    name            text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 1024),
    version         text        NOT NULL CHECK (char_length(version) <= 256), -- '' for a file of no version (an OCI blob)
    probe           jsonb       NULL,      -- {files[], document, digest}: what the run starts from, when a probe or a plan named it
    origin          text        NOT NULL CHECK (origin IN ('person', 'request', 'allow', 'bulk', 'probe', 'client')),
    bulk_id         uuid        NULL REFERENCES remote_download_bulks (id) ON DELETE CASCADE,
    request_id      uuid        NULL REFERENCES remote_delivery_requests (id) ON DELETE SET NULL,
    started_by      uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    ticket          jsonb       NULL,      -- the limits.Ticket of a charged download (R-CUR-010)
    state           text        NOT NULL CHECK (state IN ('waiting', 'running', 'done', 'failed', 'cancelled')),
    attempts        integer     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts    integer     NOT NULL CHECK (max_attempts >= 1),
    not_before      timestamptz NULL,      -- the next attempt
    job_id          uuid        NULL,      -- the running attempt's job; no foreign key, the job row is pruned
    occurrences     integer     NOT NULL DEFAULT 1 CHECK (occurrences >= 1), -- a client's failures of the same file, aggregated
    files           integer     NOT NULL DEFAULT 0 CHECK (files >= 0),
    stored          integer     NOT NULL DEFAULT 0 CHECK (stored >= 0),
    present         integer     NOT NULL DEFAULT 0 CHECK (present >= 0),
    refused         integer     NOT NULL DEFAULT 0 CHECK (refused >= 0),
    bytes           bigint      NOT NULL DEFAULT 0 CHECK (bytes >= 0),
    outcome         text        NULL CHECK (outcome IN ('completed', 'bounded', 'refused', 'skipped')),
    failure         jsonb       NULL,      -- R-CUR-011 "The failure"
    created_at      timestamptz NOT NULL DEFAULT now(),
    started_at      timestamptz NULL,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz NULL
);
CREATE INDEX remote_download_list ON remote_download (repo_id, state, created_at DESC, id);
CREATE INDEX remote_download_due ON remote_download (not_before, id) WHERE state = 'waiting';
CREATE UNIQUE INDEX remote_download_one_active ON remote_download (repo_id, name, version) WHERE state IN ('waiting', 'running');
CREATE INDEX remote_download_bulk ON remote_download (bulk_id) WHERE bulk_id IS NOT NULL;
CREATE INDEX remote_download_finished ON remote_download (finished_at) WHERE finished_at IS NOT NULL;

-- The versions a download ahead stored while the rules did not deliver them (R-CUR-016): what the retention task and a discard remove.
CREATE TABLE remote_examinations (
    repo_id     bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    name        text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 1024),
    version     text        NOT NULL CHECK (char_length(version) BETWEEN 1 AND 256),
    fetched_at  timestamptz NOT NULL DEFAULT now(),
    request_id  uuid        NULL REFERENCES remote_delivery_requests (id) ON DELETE SET NULL,
    PRIMARY KEY (repo_id, name, version)
);
CREATE INDEX remote_examinations_age ON remote_examinations (fetched_at);

-- What the Debian search reads (R-DEB-036, Q-CUR-16): the Provides, the short description and the Description-md5 of each parsed
-- Packages row, and the long descriptions of the Translation-en files the remote holds, joined by (repo_id, dir, md5). A short
-- description is cut at 1,000 characters as every text from an upstream is (R-CUR-008). The rows a parse wrote before this step carry
-- the defaults until their index is parsed again.
ALTER TABLE deb_remote_entries
    ADD COLUMN provides        text NOT NULL DEFAULT '' CHECK (octet_length(provides) <= 8192),
    ADD COLUMN description     text NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    ADD COLUMN description_md5 text COLLATE "C" NULL CHECK (description_md5 ~ '^[0-9a-f]{32}$');

-- One long description of a parsed i18n/Translation-en (index_path "main/i18n/Translation-en"), at most 8 KiB, going with its index.
CREATE TABLE deb_remote_descriptions (
    repo_id     bigint NOT NULL,
    dir         text   COLLATE "C" NOT NULL,
    index_path  text   COLLATE "C" NOT NULL,
    md5         text   COLLATE "C" NOT NULL CHECK (md5 ~ '^[0-9a-f]{32}$'),
    text        text   NOT NULL CHECK (octet_length(text) <= 8192),
    PRIMARY KEY (repo_id, dir, index_path, md5),
    FOREIGN KEY (repo_id, dir, index_path) REFERENCES deb_remote_indexes (repo_id, dir, path) ON DELETE CASCADE
);
CREATE INDEX deb_remote_descriptions_md5 ON deb_remote_descriptions (repo_id, dir, md5);
