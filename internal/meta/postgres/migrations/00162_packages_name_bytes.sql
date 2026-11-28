-- quader:phase expand
-- quader:no-transaction
--
-- The package list reads a page in the order of the name without the rest of the packages (api-and-ui.md R-API-029, H-1683): the
-- list compares names bytewise (collation C), so the order the walk of a page follows and the lookups of the page's packages by
-- their name need an index of that collation. packages_name (00036) has the database's collation, which serves the lookups of the
-- vulnerability evaluation and neither of these. Built beside the table without blocking its writes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS packages_name_bytes ON packages ((name COLLATE "C"));
