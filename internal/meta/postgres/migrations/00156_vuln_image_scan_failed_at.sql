-- quader:phase expand
--
-- The image scanning's page of the admin area names the last failed scan with everything it carried and when it failed
-- (vulnerability-scanning.md R-VULN-053, Q-VULN-298): a failure records its time. A row that failed before this migration has none
-- until its next failure, and the page says the failure without a time.
ALTER TABLE vuln_image_scan
    ADD COLUMN failed_at timestamptz NULL;
