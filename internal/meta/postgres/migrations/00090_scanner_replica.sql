-- quader:phase expand
--
-- The scanner replicas (operations-and-observability.md R-OPS-048, Q-OPS-223, ADR-224): a scanner holds no credential of PostgreSQL
-- (ADR-212), so it reports itself on the scan listener of packages, and packages writes this row. The kind is the one the client
-- certificate names, never what the report says; heartbeat_at, started_at and state_since are PostgreSQL's clock. Everything else is
-- the scanner's own statement, bounded here as the route bounds it: a taken-over scanner can make an administrator read made-up rows of
-- its own kind, and nothing decides on them (H-1161). The table is apart from replica: no reader of the registry (the key rings, the
-- support bundle, the fetch shares, the rate-limit shares) counts a scanner, which shares no state with the other replicas.

CREATE TABLE scanner_replica (
    instance_uid  uuid        PRIMARY KEY,
    -- process.scanner_kind of the certificate's name: trivy (vuln-scanner-trivy) or clamav (malware-scanner-clamav).
    kind          text        NOT NULL CHECK (kind IN ('trivy', 'clamav')),
    name          text        NOT NULL CHECK (length(name) BETWEEN 1 AND 253),
    node          text        NULL CHECK (length(node) BETWEEN 1 AND 253),
    version       text        NOT NULL CHECK (length(version) BETWEEN 1 AND 64),
    revision      text        NOT NULL CHECK (length(revision) <= 64),
    state         text        NOT NULL CHECK (state IN ('starting', 'ready', 'unready', 'draining')),
    -- The reason of the failing readiness check, a code such as database or clamd; NULL while ready.
    reason        text        NULL CHECK (length(reason) BETWEEN 1 AND 32),
    state_since   timestamptz NOT NULL,
    -- The version of the tool the scanner runs: Trivy's, or clamd's; NULL while the scanner does not know it.
    tool_version  text        NULL CHECK (length(tool_version) BETWEEN 1 AND 64),
    -- The signature database clamd scans with (clamav only); NULL before the first load and for trivy.
    db_version    text        NULL CHECK (length(db_version) BETWEEN 1 AND 128),
    slots_in_use  integer     NOT NULL CHECK (slots_in_use >= 0),
    slots_total   integer     NOT NULL CHECK (slots_total BETWEEN 1 AND 64),
    started_at    timestamptz NOT NULL,
    heartbeat_at  timestamptz NOT NULL,
    CHECK (slots_in_use <= slots_total),
    CHECK (kind = 'clamav' OR db_version IS NULL)
);

CREATE INDEX scanner_replica_kind ON scanner_replica (kind, heartbeat_at);
