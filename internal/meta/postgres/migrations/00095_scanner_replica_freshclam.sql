-- quader:phase expand
--
-- The updater malware-updater-freshclam reports itself as a third kind of the scanner replicas, freshclam (operations-and-observability.md
-- R-OPS-048, Q-OPS-224; malware-scanning.md R-MAL-032): it names the set its directory holds and the build time of that set's daily, which
-- it reports itself (the set is usually not the one in use yet), and it runs no calls, so it reports no slots. The other kinds keep their
-- bounds: a trivy row names no database, a clamav row the database clamd scans with and no build time of its own (the list joins the
-- one of the version in use), and both at least one slot.

ALTER TABLE scanner_replica DROP CONSTRAINT scanner_replica_kind_check;
ALTER TABLE scanner_replica ADD CONSTRAINT scanner_replica_kind_check CHECK (kind IN ('trivy', 'clamav', 'freshclam'));

ALTER TABLE scanner_replica DROP CONSTRAINT scanner_replica_slots_total_check;
ALTER TABLE scanner_replica ADD CONSTRAINT scanner_replica_slots_total_check
    CHECK ((kind = 'freshclam' AND slots_total = 0) OR (kind <> 'freshclam' AND slots_total BETWEEN 1 AND 64));

ALTER TABLE scanner_replica DROP CONSTRAINT scanner_replica_check1;
ALTER TABLE scanner_replica ADD CONSTRAINT scanner_replica_db_version_kind CHECK (kind IN ('clamav', 'freshclam') OR db_version IS NULL);

-- The build time of the daily of the set the updater's directory holds; NULL for every other kind and while it holds no set.
ALTER TABLE scanner_replica ADD COLUMN db_built_at timestamptz NULL;
ALTER TABLE scanner_replica ADD CONSTRAINT scanner_replica_db_built_at_kind CHECK (db_built_at IS NULL OR (kind = 'freshclam' AND db_version IS NOT NULL));
