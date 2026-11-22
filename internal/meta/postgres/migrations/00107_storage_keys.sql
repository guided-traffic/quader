-- quader:phase expand
--
-- The keys of encrypted stored content, stage 2 of the stored content, package 2 (storage.md R-STOR-083, R-STOR-081, R-STOR-075;
-- Q-STOR-309, Q-STOR-310, Q-STOR-324): a storage key per encrypted connection and generation and a name key per connection with keyed
-- names, each 32 random bytes sealed by the key ring under its row's identifier (R-ARCH-021), never deleted; the detail keyed_names of
-- a connection; the sealed data key and the segment counter of an upload session on an encrypted connection and the stored size and
-- SHA-256 of its parts; and the change area every replica's keys follow. Nothing writes the session columns before package 3.

-- One row per key ever drawn or imported (R-STOR-083). No foreign key to storage_connections: that table is transient in a backup,
-- the keys are data, and a key outlives its connection (noncurrent versions and backups' pools hold objects it wrapped, Q-STOR-309).
-- The identifier names the kind, the connection and the generation (Q-STOR-324), so a database writer cannot move a sealed key to
-- another connection without changing the identifier its seal is bound to.
CREATE TABLE storage_keys (
    key_id        text        PRIMARY KEY
                              CHECK (key_id ~ '^(sk-[a-z][a-z0-9-]{0,30}[a-z0-9]-[1-9][0-9]{0,5}|nk-[a-z][a-z0-9-]{0,30}[a-z0-9])-[0-9a-f]{8}$'),
    connection_id text        NOT NULL,
    kind          text        NOT NULL CHECK (kind IN ('storage', 'name')),
    generation    integer     NOT NULL CHECK (generation BETWEEN 1 AND 999999),
    -- the 32 bytes sealed with the key ring: table storage_keys, column sealed_key, row (key_id)
    sealed_key    text        NOT NULL CHECK (length(sealed_key) <= 512),
    -- active: wraps new objects (storage) or names them (name); retired: opens what it wrapped
    state         text        NOT NULL CHECK (state IN ('active', 'retired')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    retired_at    timestamptz,
    UNIQUE (connection_id, kind, generation),
    CHECK (key_id = CASE kind WHEN 'storage' THEN 'sk-' || connection_id || '-' || generation || '-'
                              ELSE 'nk-' || connection_id || '-' END || right(key_id, 8)),
    CHECK (kind = 'storage' OR generation = 1),
    CHECK ((state = 'retired') = (retired_at IS NOT NULL))
);
-- One active key per connection and kind: two replicas that draw at once insert one (R-STOR-083).
CREATE UNIQUE INDEX storage_keys_active ON storage_keys (connection_id, kind) WHERE state = 'active';

-- The detail of an encrypted connection (R-STOR-075, R-STOR-086), fixed with the connection like encrypted.
ALTER TABLE storage_connections ADD COLUMN keyed_names boolean NOT NULL DEFAULT false;

-- A session on an encrypted connection carries its data key sealed under its id and counts the segments it may seal (R-STOR-081,
-- Q-STOR-307, Q-STOR-318); NULL and 0 on a plain connection.
ALTER TABLE upload_sessions ADD COLUMN sealed_data_key text CHECK (length(sealed_data_key) <= 512);
ALTER TABLE upload_sessions ADD COLUMN sealed_segments bigint NOT NULL DEFAULT 0 CHECK (sealed_segments >= 0);

-- The size and SHA-256 of a part as sent (R-STOR-081): on a plain connection equal to size and sha256.
ALTER TABLE upload_session_parts ADD COLUMN stored_size bigint CHECK (stored_size > 0),
                                 ADD COLUMN stored_sha256 bytea CHECK (stored_sha256 IS NULL OR octet_length(stored_sha256) = 32);

-- The change area of the storage keys (R-STOR-083): a new generation or an import reaches every replica within its bound.
INSERT INTO cluster_epoch (area) VALUES ('storage_keys');
