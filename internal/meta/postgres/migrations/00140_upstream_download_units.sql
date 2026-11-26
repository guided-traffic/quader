-- quader:phase expand
--
-- A download from an upstream is the unit a person asked for, not one file of it (operations-and-observability.md R-OPS-049, the owner's
-- rule of 2026-11-26, ADR-295): an image with its manifests, configs, layers and attestations, a version's package, a queue row's run.
-- A row of upstream_download_finished is one unit of one replica; the rows of one unit's key written by several replicas are merged at
-- the read, and the trim keeps the rows of the newest 100 units (R-OPS-051, Q-OPS-429).
--
-- unit is the unit's key (proxy.DownloadStart.Unit, "p|<id>" for a unit of one file); parts the number of its files on the replica;
-- failure_path the file whose failure failed the unit. The table is transient (R-BAK-004) and its rows were files: they are removed, so
-- that every row the columns describe is a unit.
DELETE FROM upstream_download_finished;

ALTER TABLE upstream_download_finished
    ADD COLUMN unit         text    NOT NULL CHECK (char_length(unit) BETWEEN 1 AND 2048),
    ADD COLUMN parts        integer NOT NULL CHECK (parts >= 1),
    ADD COLUMN failure_path text    NULL CHECK (char_length(failure_path) <= 4096);

CREATE INDEX upstream_download_finished_unit ON upstream_download_finished (unit, finished_at DESC);
