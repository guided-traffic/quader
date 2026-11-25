-- quader:phase expand
--
-- The state of an image's scan by its cause, in every list that shows it (vulnerability-scanning.md R-VULN-034, R-VULN-039, Q-VULN-274):
-- created_at is when discovery wrote the manifest's row, the time a list names as "queued since"; started_at is when the job took the row
-- for a call to the scanner, cleared when the call's result is written, so that a list tells an image being scanned from one that waits
-- (Q-VULN-276). A row of a call that never wrote its result (a lost worker) is read as being scanned only within the call's deadline. The
-- table stays transient for backups: the next run builds the rows again.
ALTER TABLE vuln_image_scan ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE vuln_image_scan ADD COLUMN started_at timestamptz;
