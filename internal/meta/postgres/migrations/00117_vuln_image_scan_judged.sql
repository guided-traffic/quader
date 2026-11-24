-- quader:phase expand
--
-- What a terminal failure of an image scan was judged under (vulnerability-scanning.md R-VULN-034, Q-VULN-270). A code the same bytes cannot
-- get past is not retried after a time: the row names what decided it, and it is due again when that changes and at no other time. A code
-- of a bound (image_too_large, too_many_layers, decompressed_size, too_many_entries) names the bounds of vulnerabilities.images
-- ("limits:max_layers=…,…"); a code of the reading (layer_format, path_traversal, digest_mismatch, manifest_invalid, report_too_large) names
-- the release whose scanner read the image and whose analyser it pins ("release:<version>"). NULL on every other row: a pending row, a
-- scanned one, and a failure that may pass, which keeps its doubling back-off and its attempts. The table is transient for backups, as
-- before: the next run builds the rows again.
ALTER TABLE vuln_image_scan ADD COLUMN judged text CHECK (judged IS NULL OR length(judged) BETWEEN 1 AND 512);
