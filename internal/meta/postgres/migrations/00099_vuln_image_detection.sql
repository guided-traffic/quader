-- quader:phase expand
--
-- The detection moves into the scanner unit (vulnerability-scanning.md R-VULN-043, R-VULN-048, R-VULN-049, Q-VULN-199; ADR-229). The
-- worker keeps one analysis per manifest digest, as the scanner answered it, with the analyser version that made it and the count of the
-- image's packages its check took while it walked the document; a scanner of another analyser version refuses it and the worker
-- analyses the image again, which replaces the row. Nothing prunes by version any more.
--
-- The stored analyses go (Q-VULN-214): they are the worker's rewrite and have no count of packages, so each image is analysed afresh by
-- the scanner at its next detection, one pull per image. Nothing references the table; the scan rows keep their findings, their count
-- and their state until then. The first statement holds the table's exclusive lock to the end of the step, so the table is empty when the
-- column without a default is added.

ALTER TABLE vuln_image_sbom DROP CONSTRAINT vuln_image_sbom_pkey;
DELETE FROM vuln_image_sbom;
ALTER TABLE vuln_image_sbom ADD PRIMARY KEY (digest);
ALTER TABLE vuln_image_sbom ADD CONSTRAINT vuln_image_sbom_analyser CHECK (length(analyser) BETWEEN 1 AND 32);
-- The library and application components of the analysis (R-VULN-043): the scan row records them with every detection, so a detection
-- against a new database does not decompress and walk the stored document again (Q-VULN-207). Every row states its count.
ALTER TABLE vuln_image_sbom ADD COLUMN packages integer NOT NULL CHECK (packages >= 0);

-- An image scanner reports the database its pod holds (operations-and-observability.md R-OPS-048, Q-OPS-225): the 64 hexadecimal digits
-- of its layer's digest and the build time of its metadata.json, or nothing before its first load. The list answers the build time only
-- while the row names the database in use.
ALTER TABLE scanner_replica DROP CONSTRAINT scanner_replica_db_version_kind;
ALTER TABLE scanner_replica ADD CONSTRAINT scanner_replica_db_version_kind
    CHECK (kind IN ('clamav', 'freshclam') OR (kind = 'trivy' AND (db_version IS NULL OR db_version ~ '^[0-9a-f]{64}$')));
ALTER TABLE scanner_replica DROP CONSTRAINT scanner_replica_db_built_at_kind;
ALTER TABLE scanner_replica ADD CONSTRAINT scanner_replica_db_built_at_kind
    CHECK (db_built_at IS NULL OR (kind IN ('freshclam', 'trivy') AND db_version IS NOT NULL));
