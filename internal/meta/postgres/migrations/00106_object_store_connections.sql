-- quader:phase expand
--
-- Several object-store connections, stage 1 (storage.md R-STOR-050 to R-STOR-063, ADR-239): every blob lies on every connection
-- that is not draining, and a row per copy records where each object lies, its stored size and what is known about it (R-STOR-052,
-- Q-STOR-226). blobs stays the authority on whether a blob exists; its content_status is derived from the copies by the statements
-- that change them.
--
-- No foreign key from blob_copies or blob_copy_queue to blobs: collection reads every single-column foreign key to blobs as a
-- reference (R-STOR-024 rule 3, queries/blob.sql BlobReferencingColumns), so such a key would keep every blob forever. The rows of a
-- blob go in the statement that drops the blob row (DropBlobRows).

-- One row per connection id ever seen (R-STOR-051). bucket, prefix and encrypted are fixed once the connection holds a copy; the state
-- (filling, active, draining, drained, detached, retired) is derived from the columns, the configuration and the replica registry.
CREATE TABLE storage_connections (
    id                text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9-]{0,30}[a-z0-9]$'),
    bucket            text NOT NULL,
    prefix            text NOT NULL,
    encrypted         boolean NOT NULL DEFAULT false,
    -- what the start check found (R-STOR-035, Q-STOR-248): payloadChecksumEnforced, batchDelete, metadata, copyReplacesMetadata,
    -- serverSideCopyFrom, checkedAt
    capabilities      jsonb NOT NULL DEFAULT '{}',
    first_seen_at     timestamptz NOT NULL DEFAULT now(),
    filled_at         timestamptz,  -- the first full pass of the reconciliation without a gap: filling -> active
    drained_at        timestamptz,  -- drain: true and no copy left
    retired_at        timestamptz,  -- drained and configured nowhere, or forgotten; the id is never used again
    pass_started_at   timestamptz,  -- the reconciliation's current full pass
    last_full_pass_at timestamptz
);

CREATE TABLE blob_copies (
    sha256           bytea NOT NULL CHECK (octet_length(sha256) = 32),
    connection_id    text  NOT NULL REFERENCES storage_connections (id),
    object_key       text  NOT NULL,  -- the object's name on the connection; stage 1: <prefix>/sha256/<h0h1>/<h2h3>/<hex>
    stored_size      bigint NOT NULL CHECK (stored_size >= 0),  -- bytes as stored; stage 1: blobs.size
    storage_key_id   text,            -- NULL in stage 1; stage 2: the storage key that wrapped the object's data key
    state            text NOT NULL CHECK (state IN ('present', 'missing', 'corrupt', 'deleting')),
    created_at       timestamptz NOT NULL DEFAULT now(),  -- when the object was written
    verified_at      timestamptz,     -- the last time the whole stored object was shown to be the blob (Q-STOR-227)
    checked_at       timestamptz,     -- the last time the store answered for it
    state_changed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (sha256, connection_id)
);
-- the orphan sweep's lookup of a listed object by its name (R-STOR-026)
CREATE UNIQUE INDEX blob_copies_key ON blob_copies (connection_id, object_key);
-- the scrub's order and the counts per connection (R-STOR-027)
CREATE INDEX blob_copies_scrub ON blob_copies (connection_id, verified_at NULLS FIRST) WHERE state = 'present';
-- the damaged list, the repair and the counts
CREATE INDEX blob_copies_damaged ON blob_copies (connection_id, state) WHERE state IN ('missing', 'corrupt');
-- collection finishes interrupted copy deletions
CREATE INDEX blob_copies_deleting ON blob_copies (state_changed_at) WHERE state = 'deleting';

-- The gaps the reconciliation fills (R-STOR-056): written by the commit for every connection it did not write, by a copy marked
-- missing or corrupt, and by the pass.
CREATE TABLE blob_copy_queue (
    connection_id   text  NOT NULL REFERENCES storage_connections (id),
    sha256          bytea NOT NULL CHECK (octet_length(sha256) = 32),
    enqueued_at     timestamptz NOT NULL DEFAULT now(),
    attempts        integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    last_error      text,
    PRIMARY KEY (connection_id, sha256)
);
CREATE INDEX blob_copy_queue_due    ON blob_copy_queue (next_attempt_at);
CREATE INDEX blob_copy_queue_oldest ON blob_copy_queue (connection_id, enqueued_at);

-- The connections that hold the journal object of an outbox row (R-STOR-059, Q-STOR-241): the row goes once every connection with
-- drain: false holds it.
CREATE TABLE revocation_outbox_shipped (
    outbox_id     uuid NOT NULL REFERENCES revocation_outbox (id) ON DELETE CASCADE,
    connection_id text NOT NULL REFERENCES storage_connections (id),
    PRIMARY KEY (outbox_id, connection_id)
);

-- A session stays on the connection it started on (R-STOR-054).
ALTER TABLE upload_sessions ADD COLUMN connection_id text REFERENCES storage_connections (id);

-- The replica registry's object-store view (operations-and-observability.md R-OPS-003, storage.md R-STOR-060):
-- {"connections": [{"id", "circuit", "drain"}]}, the ids of the replica's configuration in order. No endpoint, no credential.
ALTER TABLE replica ADD COLUMN object_store jsonb NOT NULL DEFAULT '{}';

-- The blobs that exist lie on the one connection an installation had, which the element without an id names: default
-- (R-STOR-050). Its bucket and prefix are not known here, so the row is written with an empty bucket, which no configured
-- connection can have, and object keys without the prefix; the first replica that configures default binds the row to its
-- element and writes its prefix in front of the keys (Q-STOR-250). A blob whose object collection already deleted gets no copy;
-- a claimed one keeps its claim on the copy.
INSERT INTO storage_connections (id, bucket, prefix)
SELECT 'default', '', ''
 WHERE EXISTS (SELECT 1 FROM blobs) OR EXISTS (SELECT 1 FROM upload_sessions);

INSERT INTO blob_copies (sha256, connection_id, object_key, stored_size, state, created_at, state_changed_at)
SELECT b.sha256, 'default',
       'sha256/' || substr(encode(b.sha256, 'hex'), 1, 2) || '/' || substr(encode(b.sha256, 'hex'), 3, 2) || '/' || encode(b.sha256, 'hex'),
       b.size,
       CASE WHEN b.state = 'deleting' THEN 'deleting' WHEN b.content_status = 'ok' THEN 'present' ELSE b.content_status END,
       b.created_at, now()
  FROM blobs b
 WHERE b.state = 'live' OR b.object_deleted_at IS NULL;

UPDATE upload_sessions SET connection_id = 'default';
ALTER TABLE upload_sessions ALTER COLUMN connection_id SET NOT NULL;

-- The scrub orders copies, not blobs (R-STOR-052): blobs.checked_at keeps one meaning, the recheck claim of a blob without a
-- present copy (Q-STOR-153, R-STOR-055).
DROP INDEX blobs_check;
ALTER TABLE blobs DROP COLUMN deep_checked_at;
