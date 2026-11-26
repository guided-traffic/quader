-- quader:phase expand
--
-- The history of the stored content (storage.md R-STOR-108 to R-STOR-110, ADR-315): findings of damaged copies, intervals of a
-- connection that a replica could not reach or whose start check failed, and hourly counts of what the reads and writes absorbed.
-- Export class data (kept in backups, like the audit log, Q-STOR-515); bounded by storage.history.retention, which the task
-- storage-history keeps, and by caps per day (Q-STOR-516). No table has a foreign key to blobs (R-STOR-024's reference rule) or to
-- storage_connections: the history outlives a forgotten connection, named by its id.

-- Where a mark came from and why (Q-STOR-511): set by the one mark of every reader (R-STOR-055) in the statement that moves a copy to
-- missing or corrupt. A change of state that does not set them clears them (blob_copies_damage_mark), so a copy that is present again
-- carries none and a later writer that sets a damaged state without them is recorded as source other.
ALTER TABLE blob_copies
    ADD COLUMN damage_source text CHECK (damage_source IN ('download', 'read', 'scrub', 'recheck', 'reconcile', 'drain', 'sweep')),
    ADD COLUMN damage_reason text CHECK (damage_reason ~ '^[a-z_]{1,32}$');

-- One row per copy found damaged (R-STOR-108): opened and resolved by the triggers below, whatever path changed the copy.
CREATE TABLE storage_findings (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    sha256            bytea   NOT NULL CHECK (octet_length(sha256) = 32),
    connection_id     text    NOT NULL,
    status            text    NOT NULL CHECK (status IN ('missing', 'corrupt')),
    source            text    NOT NULL CHECK (source IN ('download', 'read', 'scrub', 'recheck', 'reconcile', 'drain', 'sweep', 'other')),
    reason            text    NOT NULL CHECK (reason ~ '^[a-z_]{1,32}$'),  -- damage_reason, or the status
    stored_size       bigint  NOT NULL CHECK (stored_size >= 0),
    blob_size         bigint  NOT NULL CHECK (blob_size >= 0),
    unavailable       boolean NOT NULL,                                     -- no other present copy when it was found
    detected_at       timestamptz NOT NULL DEFAULT now(),
    resolved_at       timestamptz,
    resolution        text CHECK (resolution IN ('rewritten', 'found_again', 'deleted', 'superseded', 'reconciled')),
    acknowledged_at   timestamptz,
    acknowledged_by   uuid REFERENCES principal (id) ON DELETE SET NULL,
    acknowledged_note text CHECK (length(acknowledged_note) <= 500),
    CHECK ((resolved_at IS NULL) = (resolution IS NULL))
);
CREATE INDEX storage_findings_time ON storage_findings (detected_at);
CREATE INDEX storage_findings_conn ON storage_findings (connection_id, detected_at);
CREATE INDEX storage_findings_open ON storage_findings (sha256, connection_id) WHERE resolved_at IS NULL;

-- Intervals per connection, kind and replica (R-STOR-109): a circuit open on one replica, a start check failing on one replica. Written
-- by the replica's recorder (internal/blob/history): one insert when the interval opens, an update per minute while it lasts and one
-- when it ends; code is a short word, never the object store's text (Q-STOR-517).
CREATE TABLE storage_events (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    connection_id text        NOT NULL,
    kind          text        NOT NULL CHECK (kind IN ('unreachable', 'start_check_failed')),
    replica       text        NOT NULL,
    started_at    timestamptz NOT NULL,
    ended_at      timestamptz,
    last_seen_at  timestamptz NOT NULL,
    code          text        NOT NULL CHECK (code ~ '^[a-z0-9_]{1,48}$'),
    step          text        CHECK (step ~ '^[a-z0-9_]{1,48}$'),
    occurrences   integer     NOT NULL DEFAULT 1 CHECK (occurrences >= 1),
    CHECK (ended_at IS NULL OR ended_at >= started_at)
);
CREATE INDEX storage_events_time ON storage_events (started_at);
CREATE UNIQUE INDEX storage_events_open ON storage_events (connection_id, kind, replica) WHERE ended_at IS NULL;

-- What the reads and writes absorbed, per UTC hour, connection, kind and reason (R-STOR-110): flushed by every replica once a minute
-- as one upsert, and the findings beyond the daily cap from the trigger.
CREATE TABLE storage_counts (
    hour          timestamptz NOT NULL CHECK (extract(epoch FROM hour)::bigint % 3600 = 0),
    connection_id text        NOT NULL,
    kind          text        NOT NULL CHECK (kind IN ('read_fallover', 'read_unavailable', 'write_fallover', 'upload_refused',
                                                       'copy_failed', 'encrypted_read_refused', 'findings_unlisted')),
    reason        text        NOT NULL DEFAULT '' CHECK (reason ~ '^[a-z0-9_]{0,48}$'),
    count         bigint      NOT NULL CHECK (count >= 0),
    bytes         bigint      NOT NULL DEFAULT 0 CHECK (bytes >= 0),
    PRIMARY KEY (hour, connection_id, kind, reason)
);

-- A change of a copy's state keeps damage_source and damage_reason only when the statement set them (Q-STOR-511).
CREATE FUNCTION blob_copies_damage_mark() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.state NOT IN ('missing', 'corrupt')
       OR (NEW.damage_source IS NOT DISTINCT FROM OLD.damage_source AND NEW.damage_reason IS NOT DISTINCT FROM OLD.damage_reason) THEN
        NEW.damage_source := NULL;
        NEW.damage_reason := NULL;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER blob_copies_damage_mark BEFORE UPDATE OF state ON blob_copies
    FOR EACH ROW WHEN (OLD.state IS DISTINCT FROM NEW.state) EXECUTE FUNCTION blob_copies_damage_mark();

-- A copy that becomes damaged opens a finding, at most 1,000 per connection and UTC day, the rest counted as findings_unlisted with
-- their bytes (Q-STOR-516); a copy that leaves the damaged state, or whose row goes, resolves its open finding: deleted when the row
-- goes or is claimed for deletion, superseded on a change between missing and corrupt (which opens the next), rewritten when the row
-- was written after the finding (created_at, which every write of the object moves), found_again otherwise (Q-STOR-510). Fires only
-- on a change of state, so the scrub's stamps of verified_at cost nothing.
CREATE FUNCTION storage_copy_history() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    day_start  timestamptz := date_trunc('day', now(), 'UTC');
    listed     integer;
    blob_bytes bigint;
    why        text;
BEGIN
    IF TG_OP = 'DELETE' THEN
        UPDATE storage_findings SET resolved_at = now(), resolution = 'deleted'
         WHERE sha256 = OLD.sha256 AND connection_id = OLD.connection_id AND resolved_at IS NULL;
        RETURN NULL;
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.state IN ('missing', 'corrupt') THEN
        UPDATE storage_findings f SET resolved_at = now(),
               resolution = CASE
                   WHEN NEW.state = 'deleting' THEN 'deleted'
                   WHEN NEW.state IN ('missing', 'corrupt') THEN 'superseded'
                   WHEN NEW.created_at > f.detected_at THEN 'rewritten'
                   ELSE 'found_again' END
         WHERE f.sha256 = OLD.sha256 AND f.connection_id = OLD.connection_id AND f.resolved_at IS NULL;
    END IF;
    IF NEW.state NOT IN ('missing', 'corrupt') THEN
        RETURN NULL;
    END IF;
    why := COALESCE(NEW.damage_reason, NEW.state);
    SELECT count(*) INTO listed FROM (
        SELECT 1 FROM storage_findings WHERE connection_id = NEW.connection_id AND detected_at >= day_start LIMIT 1000) s;
    IF listed >= 1000 THEN
        INSERT INTO storage_counts (hour, connection_id, kind, reason, count, bytes)
        VALUES (date_trunc('hour', now(), 'UTC'), NEW.connection_id, 'findings_unlisted', why, 1, NEW.stored_size)
        ON CONFLICT (hour, connection_id, kind, reason) DO UPDATE
           SET count = storage_counts.count + 1, bytes = storage_counts.bytes + EXCLUDED.bytes;
        RETURN NULL;
    END IF;
    SELECT b.size INTO blob_bytes FROM blobs b WHERE b.sha256 = NEW.sha256;
    INSERT INTO storage_findings (sha256, connection_id, status, source, reason, stored_size, blob_size, unavailable)
    VALUES (NEW.sha256, NEW.connection_id, NEW.state, COALESCE(NEW.damage_source, 'other'), why, NEW.stored_size, COALESCE(blob_bytes, 0),
            NOT EXISTS (SELECT 1 FROM blob_copies c JOIN storage_connections sc ON sc.id = c.connection_id
                         WHERE c.sha256 = NEW.sha256 AND c.connection_id <> NEW.connection_id AND c.state = 'present'
                           AND sc.retired_at IS NULL));
    RETURN NULL;
END $$;
CREATE TRIGGER blob_copies_history_upd AFTER UPDATE OF state ON blob_copies
    FOR EACH ROW WHEN (OLD.state IS DISTINCT FROM NEW.state) EXECUTE FUNCTION storage_copy_history();
CREATE TRIGGER blob_copies_history_ins AFTER INSERT ON blob_copies
    FOR EACH ROW WHEN (NEW.state IN ('missing', 'corrupt')) EXECUTE FUNCTION storage_copy_history();
CREATE TRIGGER blob_copies_history_del AFTER DELETE ON blob_copies
    FOR EACH ROW WHEN (OLD.state IN ('missing', 'corrupt')) EXECUTE FUNCTION storage_copy_history();

-- Copies damaged before the history existed open their findings now, so that "open" is complete from the first day (Q-STOR-520).
INSERT INTO storage_findings (sha256, connection_id, status, source, reason, stored_size, blob_size, unavailable, detected_at)
SELECT c.sha256, c.connection_id, c.state, 'other', c.state, c.stored_size, b.size, b.content_status <> 'ok', c.state_changed_at
  FROM blob_copies c JOIN blobs b ON b.sha256 = c.sha256
 WHERE c.state IN ('missing', 'corrupt');
