-- quader:phase expand
--
-- A range of versions in a package's allow (remote-curation.md R-CUR-026, Q-CUR-220): from a version (always included), up to a version
-- (included unless to_inclusive is false, then below it), or both, in the format's own order, compared by the replica's compiled rule
-- set. NULL is no bound on that side; a bound is in the format's canonical form (R-CUR-006). Only a package rule with the effect allow
-- carries one, and to_inclusive is true where there is no upper bound, the one spelling of a range without one. The columns are named as
-- the bulk's range of 00129.
ALTER TABLE remote_delivery_rules
    ADD COLUMN from_version text NULL CHECK (char_length(from_version) BETWEEN 1 AND 256),
    ADD COLUMN to_version text NULL CHECK (char_length(to_version) BETWEEN 1 AND 256),
    ADD COLUMN to_inclusive boolean NOT NULL DEFAULT true,
    ADD CONSTRAINT remote_delivery_rules_range CHECK (
        ((from_version IS NULL AND to_version IS NULL) OR (kind = 'package' AND effect = 'allow'))
        AND (to_version IS NOT NULL OR to_inclusive));
