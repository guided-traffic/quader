-- quader:phase expand
--
-- The download limits (usage-and-limits.md, walk item W-26): profiles, rules at the instance, a project and a repository, the
-- ceilings of a remote repository, a project's maximum, the instance's settings and the record of blocks (R-LIM-010, R-LIM-011,
-- R-LIM-015, R-LIM-016, R-LIM-025, R-LIM-026). No request reads these tables: every replica compiles them into an index that a
-- change of the area limits rebuilds (R-LIM-013, R-ARCH-018).

CREATE TABLE limit_profile (
    id                          uuid        PRIMARY KEY,
    key                         text        NOT NULL UNIQUE CHECK (key ~ '^[a-z][a-z0-9-]{1,31}$'),
    name                        text        NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 64),
    description                 text        NOT NULL DEFAULT '' CHECK (length(description) <= 1024),
    builtin                     boolean     NOT NULL DEFAULT false,
    requests_per_second         bigint      NULL CHECK (requests_per_second > 0),
    requests_burst              bigint      NULL CHECK (requests_burst > 0),
    requests_per_hour           bigint      NULL CHECK (requests_per_hour > 0),
    cached_bytes_per_second     bigint      NULL CHECK (cached_bytes_per_second > 0),
    cached_bytes_per_hour       bigint      NULL CHECK (cached_bytes_per_hour > 0),
    uncached_bytes_per_second   bigint      NULL CHECK (uncached_bytes_per_second > 0),
    uncached_bytes_per_hour     bigint      NULL CHECK (uncached_bytes_per_hour > 0),
    upstream_requests_per_hour  bigint      NULL CHECK (upstream_requests_per_hour > 0),
    parallel_downloads          bigint      NULL CHECK (parallel_downloads > 0),
    bytes_per_day               bigint      NULL CHECK (bytes_per_day > 0),
    refused_per_hour            bigint      NULL CHECK (refused_per_hour > 0),
    version                     bigint      NOT NULL DEFAULT 1,
    created_at                  timestamptz NOT NULL DEFAULT now(),
    created_by                  uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    updated_by                  uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    CHECK (requests_burst IS NULL OR (requests_per_second IS NOT NULL AND requests_burst >= requests_per_second))
);

-- The five built-in profiles (R-LIM-010; the development agent's estimates, which the owner did not change). Bytes are bytes:
-- MB is 10^6, GB 10^9.
INSERT INTO limit_profile (id, key, name, description, builtin,
                           requests_per_second, requests_burst, requests_per_hour,
                           cached_bytes_per_second, cached_bytes_per_hour,
                           uncached_bytes_per_second, uncached_bytes_per_hour,
                           upstream_requests_per_hour, parallel_downloads, bytes_per_day, refused_per_hour)
VALUES
    (gen_random_uuid(), 'unlimited', 'Unlimited', 'No limit on any figure.', true,
     NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
    (gen_random_uuid(), 'standard', 'Standard', 'Signed-in users without a rule of their own.', true,
     200, 1000, 50000, 200000000, 300000000000, 50000000, 30000000000, 2000, 20, 1000000000000, 500),
    (gen_random_uuid(), 'technical', 'Technical', 'Technical tokens and CI projects without a rule of their own.', true,
     400, 2000, 200000, 500000000, 1000000000000, 100000000, 100000000000, 10000, 50, 5000000000000, 2000),
    (gen_random_uuid(), 'restricted', 'Restricted', 'A narrow budget for callers that need little.', true,
     50, 200, 5000, 50000000, 50000000000, 10000000, 5000000000, 200, 6, 100000000000, 100),
    (gen_random_uuid(), 'anonymous', 'Anonymous', 'Callers without a credential, per address.', true,
     20, 100, 2000, 20000000, 10000000000, 5000000, 2000000000, 20, 4, 30000000000, 60);

CREATE TABLE limit_assignment (
    id             uuid        PRIMARY KEY,
    place_kind     text        NOT NULL CHECK (place_kind IN ('instance', 'project', 'repository')),
    project_id     uuid        NULL REFERENCES project (id) ON DELETE CASCADE,
    repository_id  bigint      NULL REFERENCES repositories (id) ON DELETE CASCADE,
    who_kind       text        NOT NULL CHECK (who_kind IN ('user', 'technical_token', 'ci_project', 'address', 'group', 'every')),
    -- The principal ID of a user or group, the technical token's ID, '<issuer ID>:<project ID>', a canonical prefix, or one
    -- of 'user', 'technical_token', 'ci', 'anonymous', 'administrator'. No foreign key: a rule whose "who" is gone matches
    -- nothing, and limits-cleanup deletes it (R-LIM-011).
    who_ref        text        NOT NULL,
    profile_id     uuid        NOT NULL REFERENCES limit_profile (id) ON DELETE RESTRICT,
    overrides      jsonb       NOT NULL DEFAULT '{}',
    capped         boolean     NOT NULL,
    builtin        boolean     NOT NULL DEFAULT false,
    note           text        NOT NULL DEFAULT '' CHECK (length(note) <= 512),
    version        bigint      NOT NULL DEFAULT 1,
    created_at     timestamptz NOT NULL DEFAULT now(),
    created_by     uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    updated_by     uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    CHECK ((place_kind = 'instance' AND project_id IS NULL AND repository_id IS NULL)
        OR (place_kind = 'project' AND project_id IS NOT NULL AND repository_id IS NULL)
        OR (place_kind = 'repository' AND project_id IS NULL AND repository_id IS NOT NULL)),
    CHECK (who_kind <> 'every' OR who_ref IN ('user', 'technical_token', 'ci', 'anonymous', 'administrator')),
    CHECK (place_kind = 'instance' OR NOT (who_kind = 'every' AND who_ref = 'administrator'))
);
CREATE UNIQUE INDEX limit_assignment_who ON limit_assignment
    (place_kind, project_id, repository_id, who_kind, who_ref) NULLS NOT DISTINCT;
CREATE INDEX limit_assignment_profile ON limit_assignment (profile_id);

-- The built-in rule set of the instance (R-LIM-012): known callers Standard, technical tokens and CI Technical, anonymous
-- callers Anonymous, administrators Unlimited.
INSERT INTO limit_assignment (id, place_kind, who_kind, who_ref, profile_id, capped, builtin, note)
SELECT gen_random_uuid(), 'instance', 'every', r.who, p.id, false, true, r.note
  FROM (VALUES ('administrator', 'unlimited', 'Administrators are unlimited.'),
               ('user', 'standard', 'Signed-in users.'),
               ('technical_token', 'technical', 'Technical tokens.'),
               ('ci', 'technical', 'CI projects.'),
               ('anonymous', 'anonymous', 'Callers without a credential.')) AS r (who, profile, note)
  JOIN limit_profile p ON p.key = r.profile;

CREATE TABLE limit_ceiling (
    repository_id               bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    class                       text        NOT NULL CHECK (class IN ('anonymous', 'signed_in')),
    uncached_bytes_per_hour     bigint      NULL CHECK (uncached_bytes_per_hour > 0),
    upstream_requests_per_hour  bigint      NULL CHECK (upstream_requests_per_hour > 0),
    version                     bigint      NOT NULL DEFAULT 1,
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    updated_by                  uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    PRIMARY KEY (repository_id, class)
);

CREATE TABLE limit_project_maximum (
    project_id  uuid        PRIMARY KEY REFERENCES project (id) ON DELETE CASCADE,
    profile_id  uuid        NOT NULL REFERENCES limit_profile (id) ON DELETE RESTRICT,
    overrides   jsonb       NOT NULL DEFAULT '{}',
    version     bigint      NOT NULL DEFAULT 1,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid        NULL REFERENCES principal (id) ON DELETE SET NULL
);
CREATE INDEX limit_project_maximum_profile ON limit_project_maximum (profile_id);

-- The instance's settings: one row (R-LIM-026, R-LIM-025). Audit is the default (owner, decision 17).
CREATE TABLE limit_settings (
    id             boolean     PRIMARY KEY DEFAULT true CHECK (id),
    mode           text        NOT NULL DEFAULT 'audit' CHECK (mode IN ('audit', 'enforce')),
    block_seconds  integer     NOT NULL DEFAULT 900 CHECK (block_seconds BETWEEN 60 AND 86400),
    version        bigint      NOT NULL DEFAULT 1,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    updated_by     uuid        NULL REFERENCES principal (id) ON DELETE SET NULL
);
INSERT INTO limit_settings (id) VALUES (true);

-- The record of blocks (R-LIM-025): written asynchronously when a flush sets a marker, never failing a request.
CREATE TABLE limit_block (
    id             uuid        PRIMARY KEY,
    subject_kind   text        NOT NULL,
    subject_ref    text        NOT NULL,
    place          text        NOT NULL,   -- 'instance' | 'project:<uuid>' | 'repository:<id>'
    started_at     timestamptz NOT NULL,
    until          timestamptz NOT NULL,
    refused_count  bigint      NOT NULL,
    refused_limit  bigint      NOT NULL,
    lifted_at      timestamptz NULL,
    lifted_by      uuid        NULL REFERENCES principal (id) ON DELETE SET NULL
);
CREATE INDEX limit_block_until ON limit_block (until);
CREATE INDEX limit_block_subject ON limit_block (subject_kind, subject_ref);

-- The change area of the limits (R-ARCH-018).
INSERT INTO cluster_epoch (area) VALUES ('limits');
