-- quader:phase expand
--
-- The storage summary (operations-and-observability.md R-OPS-023, storage.md R-STOR-028): the task storage-summary computes the
-- figures from PostgreSQL and keeps the latest result in this one row; GET /api/v1/storage/summary, GET
-- /api/v1/system/storage-connections and the gauges of the summary read it and never compute. Export class transient: a restore's
-- first run computes it again (internal/blob/summary).
CREATE TABLE storage_summary (
    id          smallint    PRIMARY KEY CHECK (id = 1),
    computed_at timestamptz NOT NULL,
    duration_ms integer     NOT NULL CHECK (duration_ms >= 0),
    summary     jsonb       NOT NULL
);
