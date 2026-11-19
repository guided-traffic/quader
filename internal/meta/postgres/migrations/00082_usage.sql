-- quader:phase expand
--
-- The usage store of the download limits (usage-and-limits.md R-LIM-027, R-LIM-028): what every subject consumed, per
-- credential, place and serving repository, in five-minute buckets kept 25 hours and hourly buckets kept 30 days, the limits
-- it met, and the daily sums without a person that outlive both. Each replica writes its buffer every
-- limits.usage.flush_interval in one transaction; no request reads these tables, the overview does. The daily task
-- limits-cleanup folds, deletes and erases.

CREATE TABLE usage_bucket (
    granularity          text        NOT NULL CHECK (granularity IN ('5m', '1h')),
    bucket_start         timestamptz NOT NULL,
    subject_kind         text        NOT NULL CHECK (subject_kind IN ('user', 'technical_token', 'ci', 'anonymous', 'system', 'overflow')),
    subject_ref          text        NOT NULL,
    credential_ref       text        NOT NULL DEFAULT '',
    place                text        NOT NULL,
    repository_id        bigint      NULL REFERENCES repositories (id) ON DELETE CASCADE,
    requests             bigint      NOT NULL DEFAULT 0,
    refused              bigint      NOT NULL DEFAULT 0,
    cached_bytes         bigint      NOT NULL DEFAULT 0,
    uncached_bytes       bigint      NOT NULL DEFAULT 0,
    upstream_requests    bigint      NOT NULL DEFAULT 0,
    background_fetches   bigint      NOT NULL DEFAULT 0,
    -- The budget key's effective limits when the row was last written; NULL is unlimited.
    limit_requests_hour  bigint      NULL,
    limit_cached_hour    bigint      NULL,
    limit_uncached_hour  bigint      NULL,
    limit_upstream_hour  bigint      NULL,
    limit_bytes_day      bigint      NULL
);
CREATE UNIQUE INDEX usage_bucket_key ON usage_bucket
    (granularity, bucket_start, subject_kind, subject_ref, credential_ref, place, repository_id) NULLS NOT DISTINCT;
CREATE INDEX usage_bucket_subject ON usage_bucket (subject_kind, subject_ref, granularity, bucket_start);
CREATE INDEX usage_bucket_repository ON usage_bucket (repository_id, granularity, bucket_start);
CREATE INDEX usage_bucket_start ON usage_bucket (granularity, bucket_start);

CREATE TABLE usage_limit_event (
    granularity   text        NOT NULL CHECK (granularity IN ('5m', '1h')),
    bucket_start  timestamptz NOT NULL,
    subject_kind  text        NOT NULL,
    subject_ref   text        NOT NULL,
    place         text        NOT NULL,
    repository_id bigint      NULL REFERENCES repositories (id) ON DELETE CASCADE,
    figure        text        NOT NULL,   -- an API figure name, or 'ceiling.anonymous' / 'ceiling.signedIn'
    mode          text        NOT NULL CHECK (mode IN ('audit', 'enforce')),
    count         bigint      NOT NULL
);
CREATE UNIQUE INDEX usage_limit_event_key ON usage_limit_event
    (granularity, bucket_start, subject_kind, subject_ref, place, repository_id, figure, mode) NULLS NOT DISTINCT;
CREATE INDEX usage_limit_event_start ON usage_limit_event (granularity, bucket_start);

CREATE TABLE usage_daily_summary (
    day                 date        NOT NULL,
    repository_id       bigint      NULL REFERENCES repositories (id) ON DELETE CASCADE,
    subject_kind        text        NOT NULL,
    subjects            bigint      NOT NULL,
    requests            bigint      NOT NULL,
    refused             bigint      NOT NULL,
    cached_bytes        bigint      NOT NULL,
    uncached_bytes      bigint      NOT NULL,
    upstream_requests   bigint      NOT NULL,
    background_fetches  bigint      NOT NULL,
    limited_events      bigint      NOT NULL,
    would_limit_events  bigint      NOT NULL
);
CREATE UNIQUE INDEX usage_daily_summary_key ON usage_daily_summary (day, repository_id, subject_kind) NULLS NOT DISTINCT;
