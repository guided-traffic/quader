-- quader:phase expand
--
-- The downloads from the upstreams of remote repositories (operations-and-observability.md R-OPS-049 to R-OPS-052, ADR-279).
--
-- upstream_download_live: one row per replica that has running downloads, rewritten every proxy.upstream_downloads.publish_interval
-- (R-OPS-050, Q-OPS-420). A reader takes a row whose written_at is at most three intervals old; a sweep turns a row older than ten
-- intervals into finished rows of the outcome interrupted (R-OPS-051). No reference to replica: the registry's prune is its own.
CREATE TABLE upstream_download_live (
    instance_uid uuid        PRIMARY KEY,
    replica_name text        NOT NULL,
    written_at   timestamptz NOT NULL DEFAULT now(),
    downloads    jsonb       NOT NULL,
    omitted      integer     NOT NULL DEFAULT 0 CHECK (omitted >= 0)
);

-- upstream_download_finished: the last 100 ended downloads, trimmed by every publication that inserts (R-OPS-051, Q-OPS-422). The
-- repository cascades, so a removed repository leaves no row; the principal nulls, so a removed person reads as nobody. No upstream
-- address, header or client address is kept.
CREATE TABLE upstream_download_finished (
    id            uuid        PRIMARY KEY,
    repo_id       bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    name          text        NOT NULL CHECK (char_length(name) <= 1024),
    version       text        NOT NULL CHECK (char_length(version) <= 256),
    path          text        NOT NULL CHECK (char_length(path) <= 4096),
    bytes         bigint      NOT NULL CHECK (bytes >= 0),
    total         bigint      NULL CHECK (total >= 0),
    started_at    timestamptz NOT NULL,
    finished_at   timestamptz NOT NULL,
    duration_ms   bigint      NOT NULL CHECK (duration_ms >= 0),
    outcome       text        NOT NULL CHECK (outcome IN ('completed', 'failed', 'cancelled', 'interrupted')),
    failure_class text        NULL CHECK (char_length(failure_class) <= 64),
    failure_code  text        NULL CHECK (char_length(failure_code) <= 128),
    origin        text        NOT NULL CHECK (origin IN ('client', 'queue', 'mirror', 'scanner_database')),
    queue_origin  text        NULL CHECK (queue_origin IN ('person', 'request', 'allow', 'bulk', 'probe')),
    queue_row     uuid        NULL,
    by_principal  uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    replica_name  text        NOT NULL CHECK (char_length(replica_name) <= 253)
);

CREATE INDEX upstream_download_finished_order ON upstream_download_finished (finished_at DESC, id DESC);
CREATE INDEX upstream_download_finished_repo ON upstream_download_finished (repo_id);
CREATE INDEX upstream_download_finished_by ON upstream_download_finished (by_principal) WHERE by_principal IS NOT NULL;
