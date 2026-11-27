-- quader:phase expand
--
-- A grade names when, by which scanner and against which database an image was checked (vulnerability-scanning.md R-VULN-051, Q-VULN-290):
-- the detection that writes a row as scanned records the build time of the database it ran with (the watermark the database's run recorded
-- beside its digest on the source row trivy/OCI, Q-VULN-208; null when the row names another digest by then) and the analyser's version the
-- scanner's answer stated (the header Quader-Analyser-Version, which the worker checks against the stored analysis's). The scanner states the
-- version of itself (H-2406): shown, nothing decides on it. A row written before this migration has neither until its next detection, which
-- the next database brings.
ALTER TABLE vuln_image_scan
    ADD COLUMN db_built_at timestamptz NULL,
    ADD COLUMN tool_version text NULL CHECK (length(tool_version) <= 64);
