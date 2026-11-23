-- quader:phase expand
--
-- Destroying a retired connection's keys, package 7 of the encryption of stored content (storage.md R-STOR-097, R-STOR-103;
-- Q-STOR-402, Q-STOR-408; ADR-255): a row of storage_keys may lose its sealed value and take the state destroyed, and every export of the
-- storage keys leaves a row of storage_key_exports, so that a destruction can report the exports made before it.

-- A destroyed key keeps its row (identifier, connection, kind, generation, created_at): an identifier met later answers "destroyed", not
-- "unknown", and the mark survives a restore, since storage_keys is export class data and storage_connections transient. Its sealed value
-- is NULL, never a marker: a value that is no envelope of the key ring would enter the sealed-column registry, and the backup's
-- secrets.keyIds and the walk of secrets-reencrypt would break on it. retired_at is set for every row that is not active: the time the
-- key was retired or, for the key that was active, the destruction's.
ALTER TABLE storage_keys ALTER COLUMN sealed_key DROP NOT NULL,
                         ADD COLUMN destroyed_at timestamptz;
-- The state's CHECKs of 00107 (the column's and the third table CHECK, (state = 'retired') = (retired_at IS NOT NULL)) are replaced.
ALTER TABLE storage_keys DROP CONSTRAINT storage_keys_state_check,
                         DROP CONSTRAINT storage_keys_check2;
ALTER TABLE storage_keys
    ADD CONSTRAINT storage_keys_state_check CHECK (state IN ('active', 'retired', 'destroyed')),
    ADD CONSTRAINT storage_keys_retired_check CHECK ((state = 'active') = (retired_at IS NULL)),
    ADD CONSTRAINT storage_keys_destroyed_sealed_check CHECK ((state = 'destroyed') = (sealed_key IS NULL)),
    ADD CONSTRAINT storage_keys_destroyed_at_check CHECK ((state = 'destroyed') = (destroyed_at IS NOT NULL));

-- One row per export of the storage keys (R-STOR-103, Q-STOR-408): when, from which pod, and which identifiers the document listed. It
-- outlives the audit trail's retention and lands in every backup (export class data), so a destruction reports every export made
-- before it that listed a key it destroys.
CREATE TABLE storage_key_exports (
    id          bigserial   PRIMARY KEY,
    exported_at timestamptz NOT NULL DEFAULT now(),
    instance    text        NOT NULL CHECK (length(instance) <= 253),
    key_ids     text[]      NOT NULL CHECK (cardinality(key_ids) <= 100000)
);
