-- quader:phase expand
--
-- A bulk of a package takes a range of its versions (remote-curation.md R-CUR-010, Q-CUR-190, Q-CUR-193): from a version (always
-- included), up to a version (included unless to_inclusive is false, then below it), or both, in the format's own order. The bulk keeps
-- its range, so that its expansion reads it at every step and the tab Downloads says what the bulk was asked for. NULL is no bound on
-- that side; a bound is in the format's canonical form (R-CUR-006). A bulk of before, a request's and an allow's bound nothing.
ALTER TABLE remote_download_bulks
    ADD COLUMN from_version text NULL CHECK (char_length(from_version) BETWEEN 1 AND 256),
    ADD COLUMN to_version text NULL CHECK (char_length(to_version) BETWEEN 1 AND 256),
    ADD COLUMN to_inclusive boolean NOT NULL DEFAULT true;
